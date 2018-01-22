// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"crypto/elliptic"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/decred/politeia/politeiad/api/v1"
	"github.com/decred/politeia/politeiad/api/v1/identity"
	"github.com/decred/politeia/politeiad/backend"
	"github.com/decred/politeia/politeiad/backend/gitbe"
	"github.com/decred/politeia/politeiad/referendum"
	"github.com/decred/politeia/util"
	"github.com/gorilla/mux"
)

// politeia application context.
type politeia struct {
	backend  backend.Backend
	cfg      *config
	router   *mux.Router
	identity *identity.FullIdentity
}

func remoteAddr(r *http.Request) string {
	via := r.RemoteAddr
	xff := r.Header.Get(v1.Forward)
	if xff != "" {
		return fmt.Sprintf("%v via %v", xff, r.RemoteAddr)
	}
	return via
}

// convertBackendMetadataStream converts a backend metadata stream to an API
// metadata stream.
func convertBackendMetadataStream(mds backend.MetadataStream) v1.MetadataStream {
	return v1.MetadataStream{
		ID:      mds.ID,
		Payload: mds.Payload,
	}
}

// convertBackendStatus converts a backend MDStatus to an API status.
func convertBackendStatus(status backend.MDStatusT) v1.RecordStatusT {
	s := v1.RecordStatusInvalid
	switch status {
	case backend.MDStatusInvalid:
		s = v1.RecordStatusInvalid
	case backend.MDStatusUnvetted:
		s = v1.RecordStatusNotReviewed
	case backend.MDStatusVetted:
		s = v1.RecordStatusPublic
	case backend.MDStatusCensored:
		s = v1.RecordStatusCensored
	case backend.MDStatusIterationUnvetted:
		s = v1.RecordStatusUnreviewedChanges
	case backend.MDStatusReferendum:
		s = v1.RecordStatusReferendum
	case backend.MDStatusCensoredFinal:
		s = v1.RecordStatusCensoredFinal
	case backend.MDStatusVettedFinal:
		s = v1.RecordStatusVettedFinal
	}
	return s
}

// convertFrontendStatus convert an API status to a backend MDStatus.
func convertFrontendStatus(status v1.RecordStatusT) backend.MDStatusT {
	s := backend.MDStatusInvalid
	switch status {
	case v1.RecordStatusInvalid:
		s = backend.MDStatusInvalid
	case v1.RecordStatusNotReviewed:
		s = backend.MDStatusUnvetted
	case v1.RecordStatusPublic:
		s = backend.MDStatusVetted
	case v1.RecordStatusCensored:
		s = backend.MDStatusCensored
	}
	return s
}

func convertFrontendFiles(f []v1.File) []backend.File {
	files := make([]backend.File, 0, len(f))
	for _, v := range f {
		files = append(files, backend.File{
			Name:    v.Name,
			MIME:    v.MIME,
			Digest:  v.Digest,
			Payload: v.Payload,
		})
	}
	return files
}

func convertFrontendMetadataStream(mds []v1.MetadataStream) []backend.MetadataStream {
	m := make([]backend.MetadataStream, 0, len(mds))
	for _, v := range mds {
		m = append(m, backend.MetadataStream{
			ID:      v.ID,
			Payload: v.Payload,
		})
	}
	return m
}

func (p *politeia) convertBackendRecord(br backend.Record) v1.Record {
	rm := br.RecordMetadata

	// Calculate signature
	merkleToken := make([]byte, len(rm.Merkle)+len(rm.Token))
	copy(merkleToken, rm.Merkle[:])
	copy(merkleToken[len(rm.Merkle[:]):], rm.Token)
	signature := p.identity.SignMessage(merkleToken)

	// Convert MetadataStream
	md := make([]v1.MetadataStream, 0, len(br.Metadata))
	for k := range br.Metadata {
		md = append(md, convertBackendMetadataStream(br.Metadata[k]))
	}

	// Convert record
	pr := v1.Record{
		Status:    convertBackendStatus(rm.Status),
		Timestamp: rm.Timestamp,
		CensorshipRecord: v1.CensorshipRecord{
			Merkle:    hex.EncodeToString(rm.Merkle[:]),
			Token:     hex.EncodeToString(rm.Token),
			Signature: hex.EncodeToString(signature[:]),
		},
		Metadata: md,
	}
	pr.Files = make([]v1.File, 0, len(br.Files))
	for _, v := range br.Files {
		pr.Files = append(pr.Files,
			v1.File{
				Name:    v.Name,
				MIME:    v.MIME,
				Digest:  v.Digest,
				Payload: v.Payload,
			})
	}

	return pr
}

func (p *politeia) respondWithUserError(w http.ResponseWriter,
	errorCode v1.ErrorStatusT, errorContext []string) {
	util.RespondWithJSON(w, http.StatusBadRequest, v1.UserErrorReply{
		ErrorCode:    errorCode,
		ErrorContext: errorContext,
	})
}

func (p *politeia) respondWithServerError(w http.ResponseWriter, errorCode int64) {
	util.RespondWithJSON(w, http.StatusInternalServerError, v1.ServerErrorReply{
		ErrorCode: errorCode,
	})
}

func (p *politeia) getIdentity(w http.ResponseWriter, r *http.Request) {
	var t v1.Identity
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&t); err != nil {
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, nil)
		return
	}
	defer r.Body.Close()

	challenge, err := hex.DecodeString(t.Challenge)
	if err != nil || len(challenge) != v1.ChallengeSize {
		p.respondWithUserError(w, v1.ErrorStatusInvalidChallenge, nil)
		return
	}
	response := p.identity.SignMessage(challenge)

	reply := v1.IdentityReply{
		PublicKey: hex.EncodeToString(p.identity.Public.Key[:]),
		Response:  hex.EncodeToString(response[:]),
	}

	util.RespondWithJSON(w, http.StatusOK, reply)
}

func (p *politeia) newRecord(w http.ResponseWriter, r *http.Request) {
	var t v1.NewRecord
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&t); err != nil {
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, nil)
		return
	}
	defer r.Body.Close()

	challenge, err := hex.DecodeString(t.Challenge)
	if err != nil || len(challenge) != v1.ChallengeSize {
		log.Errorf("%v newRecord: invalid challenge", remoteAddr(r))
		p.respondWithUserError(w, v1.ErrorStatusInvalidChallenge, nil)
		return
	}

	log.Infof("New record submitted %v", remoteAddr(r))

	rm, err := p.backend.New(convertFrontendMetadataStream(t.Metadata),
		convertFrontendFiles(t.Files))
	if err != nil {
		// Check for content error.
		if contentErr, ok := err.(backend.ContentVerificationError); ok {
			log.Errorf("%v New record content error: %v",
				remoteAddr(r), contentErr)
			p.respondWithUserError(w, contentErr.ErrorCode,
				contentErr.ErrorContext)
			return
		}

		// Generic internal error.
		errorCode := time.Now().Unix()
		log.Errorf("%v New record error code %v: %v", remoteAddr(r),
			errorCode, err)
		p.respondWithServerError(w, errorCode)
		return
	}

	// Prepare reply.
	merkleToken := make([]byte, len(rm.Merkle)+len(rm.Token))
	copy(merkleToken, rm.Merkle[:])
	copy(merkleToken[len(rm.Merkle[:]):], rm.Token)
	signature := p.identity.SignMessage(merkleToken)

	response := p.identity.SignMessage(challenge)
	reply := v1.NewRecordReply{
		Response: hex.EncodeToString(response[:]),
		CensorshipRecord: v1.CensorshipRecord{
			Merkle:    hex.EncodeToString(rm.Merkle[:]),
			Token:     hex.EncodeToString(rm.Token),
			Signature: hex.EncodeToString(signature[:]),
		},
	}

	log.Infof("New record accepted %v: token %v", remoteAddr(r),
		reply.CensorshipRecord.Token)

	util.RespondWithJSON(w, http.StatusOK, reply)
}

func (p *politeia) updateUnvetted(w http.ResponseWriter, r *http.Request) {
	var t v1.UpdateUnvetted
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&t); err != nil {
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload,
			nil)
		return
	}
	defer r.Body.Close()

	challenge, err := hex.DecodeString(t.Challenge)
	if err != nil || len(challenge) != v1.ChallengeSize {
		log.Errorf("%v updateRecord: invalid challenge", remoteAddr(r))
		p.respondWithUserError(w, v1.ErrorStatusInvalidChallenge, nil)
		return
	}

	// Validate token
	token, err := util.ConvertStringToken(t.Token)
	if err != nil {
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, nil)
		return
	}

	log.Infof("Update record submitted %v: %x", remoteAddr(r), token)

	rm, err := p.backend.UpdateUnvettedRecord(token,
		convertFrontendMetadataStream(t.MDAppend),
		convertFrontendMetadataStream(t.MDOverwrite),
		convertFrontendFiles(t.FilesAdd), t.FilesDel)
	if err != nil {
		if err == backend.ErrNoChanges {
			log.Errorf("%v update record no changes: %x",
				remoteAddr(r), token)
			p.respondWithUserError(w, v1.ErrorStatusNoChanges, nil)
			return
		}
		// Check for content error.
		if contentErr, ok := err.(backend.ContentVerificationError); ok {
			log.Errorf("%v update record content error: %v",
				remoteAddr(r), contentErr)
			p.respondWithUserError(w, contentErr.ErrorCode,
				contentErr.ErrorContext)
			return
		}

		// Generic internal error.
		errorCode := time.Now().Unix()
		log.Errorf("%v Update record error code %v: %v", remoteAddr(r),
			errorCode, err)
		p.respondWithServerError(w, errorCode)
		return
	}

	// Prepare reply.
	merkleToken := make([]byte, len(rm.Merkle)+len(rm.Token))
	copy(merkleToken, rm.Merkle[:])
	copy(merkleToken[len(rm.Merkle[:]):], rm.Token)
	signature := p.identity.SignMessage(merkleToken)

	response := p.identity.SignMessage(challenge)
	reply := v1.UpdateUnvettedReply{
		Response: hex.EncodeToString(response[:]),
		CensorshipRecord: v1.CensorshipRecord{
			Merkle:    hex.EncodeToString(rm.Merkle[:]),
			Token:     hex.EncodeToString(rm.Token),
			Signature: hex.EncodeToString(signature[:]),
		},
	}

	log.Infof("Update record %v: token %v", remoteAddr(r),
		reply.CensorshipRecord.Token)

	util.RespondWithJSON(w, http.StatusOK, reply)
}

func (p *politeia) getUnvetted(w http.ResponseWriter, r *http.Request) {
	var t v1.GetUnvetted
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&t); err != nil {
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, nil)
		return
	}
	defer r.Body.Close()

	challenge, err := hex.DecodeString(t.Challenge)
	if err != nil || len(challenge) != v1.ChallengeSize {
		p.respondWithUserError(w, v1.ErrorStatusInvalidChallenge, nil)
		return
	}
	response := p.identity.SignMessage(challenge)

	reply := v1.GetUnvettedReply{
		Response: hex.EncodeToString(response[:]),
	}

	// Validate token
	token, err := util.ConvertStringToken(t.Token)
	if err != nil {
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, nil)
		return
	}

	// Ask backend about the censorship token.
	bpr, err := p.backend.GetUnvetted(token)
	if err == backend.ErrRecordNotFound {
		reply.Record.Status = v1.RecordStatusNotFound
		log.Errorf("Get unvetted record %v: token %v not found",
			remoteAddr(r), t.Token)
	} else if err != nil {
		// Generic internal error.
		errorCode := time.Now().Unix()
		log.Errorf("%v Get unvetted record error code %v: %v",
			remoteAddr(r), errorCode, err)

		p.respondWithServerError(w, errorCode)
		return
	} else {
		reply.Record = p.convertBackendRecord(*bpr)

		// Double check record bits before sending them off
		err := v1.Verify(p.identity.Public,
			reply.Record.CensorshipRecord, reply.Record.Files)
		if err != nil {
			// Generic internal error.
			errorCode := time.Now().Unix()
			log.Errorf("%v Get unvetted record CORRUPTION "+
				"error code %v: %v", remoteAddr(r), errorCode,
				err)

			p.respondWithServerError(w, errorCode)
			return
		}

		log.Infof("Get unvetted record %v: token %v", remoteAddr(r),
			t.Token)
	}

	util.RespondWithJSON(w, http.StatusOK, reply)
}

func (p *politeia) getVetted(w http.ResponseWriter, r *http.Request) {
	var t v1.GetVetted
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&t); err != nil {
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, nil)
		return
	}
	defer r.Body.Close()

	challenge, err := hex.DecodeString(t.Challenge)
	if err != nil || len(challenge) != v1.ChallengeSize {
		p.respondWithUserError(w, v1.ErrorStatusInvalidChallenge, nil)
		return
	}
	response := p.identity.SignMessage(challenge)

	reply := v1.GetVettedReply{
		Response: hex.EncodeToString(response[:]),
	}

	// Validate token
	token, err := util.ConvertStringToken(t.Token)
	if err != nil {
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, nil)
		return
	}

	// Ask backend about the censorship token.
	bpr, err := p.backend.GetVetted(token)
	if err == backend.ErrRecordNotFound {
		reply.Record.Status = v1.RecordStatusNotFound
		log.Errorf("Get vetted record %v: token %v not found",
			remoteAddr(r), t.Token)
	} else if err != nil {
		// Generic internal error.
		errorCode := time.Now().Unix()
		log.Errorf("%v Get vetted record error code %v: %v",
			remoteAddr(r), errorCode, err)

		p.respondWithServerError(w, errorCode)
		return
	} else {
		reply.Record = p.convertBackendRecord(*bpr)

		// Double check record bits before sending them off
		err := v1.Verify(p.identity.Public,
			reply.Record.CensorshipRecord, reply.Record.Files)
		if err != nil {
			// Generic internal error.
			errorCode := time.Now().Unix()
			log.Errorf("%v Get vetted record CORRUPTION "+
				"error code %v: %v", remoteAddr(r), errorCode,
				err)

			p.respondWithServerError(w, errorCode)
			return
		}
		log.Infof("Get vetted record %v: token %v", remoteAddr(r),
			t.Token)
	}

	log.Infof("Vetted record %v backend status %v frontend status %v", t.Token, bpr.RecordMetadata.Status, reply.Record.Status)
	util.RespondWithJSON(w, http.StatusOK, reply)
}

func (p *politeia) referendumCall(w http.ResponseWriter, r *http.Request) {
	var t v1.ReferendumRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&t); err != nil {
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, nil)
		return
	}
	defer r.Body.Close()

	challenge, err := hex.DecodeString(t.Challenge)
	if err != nil || len(challenge) != v1.ChallengeSize {
		p.respondWithUserError(w, v1.ErrorStatusInvalidChallenge, nil)
		return
	}
	response := p.identity.SignMessage(challenge)

	reply := v1.ReferendumReply{
		Response: hex.EncodeToString(response[:]),
	}

	// Validate token
	token, err := util.ConvertStringToken(t.Token)
	if err != nil {
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, []string{"Invalid Token"})
		return
	}

	// Validate user's signature on token
	if !t.User.VerifyMessage(token, t.Signature) {
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, []string{"Invalid user signature"})
		return
	}

	// Ask backend about the censorship token.
	bpr, err := p.backend.GetUnvetted(token)
	if err == backend.ErrRecordNotFound {
		reply.Status = v1.RecordStatusNotFound
		log.Errorf("Get unvetted proposal %v: token %v not found",
			remoteAddr(r), t.Token)
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, []string{"Proposal not found. Are you sure it has been censored?"})
		return
	}
	if err != nil {
		// Generic internal error.
		errorCode := time.Now().Unix()
		log.Errorf("%v Get unvetted proposal error code %v: %v",
			remoteAddr(r), errorCode, err)
		p.respondWithServerError(w, errorCode)
		return
	}

	proposal := p.convertBackendRecord(*bpr)
	if proposal.Status != v1.RecordStatusCensored {
		reply.Status = v1.RecordStatusCensored
		log.Errorf("Token %v does not correspond to a censored record",
			remoteAddr(r), t.Token)
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, []string{"Proposal not censored"})
		return
	}

	ref, err := referendum.CreateReferendum(t.User, bpr)
	if err != nil {
		errorCode := time.Now().Unix()
		log.Errorf("%v Unable to create referendum %v: %v",
			remoteAddr(r), errorCode, err)
		p.respondWithServerError(w, errorCode)
		return
	}

	// Ask backend to update unvetted status to referendum
	refStatus := backend.MDStatusReferendum
	status, err := p.backend.SetUnvettedStatus(token, refStatus, nil, nil)
	if err != nil {
		oldStatus := v1.RecordStatus[convertBackendStatus(status)]
		newStatus := v1.RecordStatus[convertBackendStatus(refStatus)]
		// Check for specific errors
		if err == backend.ErrInvalidTransition {
			log.Errorf("%v Invalid status code transition: "+
				"%v %v->%v", remoteAddr(r), t.Token, oldStatus,
				newStatus)
			p.respondWithUserError(w, v1.ErrorStatusInvalidRecordStatusTransition, nil)
			return
		}
		// Generic internal error.
		errorCode := time.Now().Unix()
		log.Errorf("%v Set unvetted status error code %v: %v",
			remoteAddr(r), errorCode, err)

		p.respondWithServerError(w, errorCode)
		return
	}

	reply.Token = ref.Token
	reply.Status = v1.RecordStatusReferendum
	util.RespondWithJSON(w, http.StatusOK, reply)
}

func (p *politeia) referendumVote(w http.ResponseWriter, r *http.Request) {
	var t v1.ReferendumVoteRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&t); err != nil {
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, nil)
		return
	}
	defer r.Body.Close()

	challenge, err := hex.DecodeString(t.Challenge)
	if err != nil || len(challenge) != v1.ChallengeSize {
		p.respondWithUserError(w, v1.ErrorStatusInvalidChallenge, nil)
		return
	}
	response := p.identity.SignMessage(challenge)

	reply := v1.ReferendumVoteReply{
		Response: hex.EncodeToString(response[:]),
	}

	// Validate token
	token, err := util.ConvertStringToken(t.Token)
	if err != nil {
		errorMsg := fmt.Sprintf("Unable to convert string token to bytes: %v", err)
		log.Errorf(errorMsg)
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, []string{errorMsg})
		return
	}

	// Validate user's signature on token
	if !t.User.VerifyMessage(token, t.Signature) {
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, []string{"Invalid user signature"})
		return
	}

	// Find token in AllReferendums
	ref, found := referendum.AllReferendums[t.Token]
	if !found {
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, []string{"Token does not correspond to a referendum"})
		return
	}

	// Register the vote
	vote := referendum.Vote{
		User:     t.User,
		VoteCast: t.Vote,
	}
	log.Errorf("ID %v", t.User)

	err = ref.CastVote(vote)
	if err != nil {
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, []string{err.Error()})
		return
	}

	log.Errorf("Votes: %v", ref.Votes)
	reply.Status = v1.RecordStatus[v1.RecordStatusReferendum]
	util.RespondWithJSON(w, http.StatusOK, reply)
}

func (p *politeia) referendumResults(w http.ResponseWriter, r *http.Request) {
	var t v1.ReferendumResultsRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&t); err != nil {
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, nil)
		return
	}
	defer r.Body.Close()

	challenge, err := hex.DecodeString(t.Challenge)
	if err != nil || len(challenge) != v1.ChallengeSize {
		p.respondWithUserError(w, v1.ErrorStatusInvalidChallenge, nil)
		return
	}
	response := p.identity.SignMessage(challenge)
	reply := v1.ReferendumResultsReply{
		Response: hex.EncodeToString(response[:]),
	}

	// Validate token
	token, err := util.ConvertStringToken(t.Token)
	if err != nil {
		errorMsg := fmt.Sprintf("Unable to convert string token to bytes: %v", err)
		log.Errorf(errorMsg)
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, []string{errorMsg})
		return
	}

	bpr, err := p.backend.GetUnvetted(token)
	if err == backend.ErrRecordNotFound {
		reply.Status = v1.RecordStatusNotFound
		log.Errorf("Get unvetted record %v: token %v not found",
			remoteAddr(r), t.Token)
	} else if err != nil {
		// Generic internal error.
		errorCode := time.Now().Unix()
		log.Errorf("%v Get unvetted record error code %v: %v",
			remoteAddr(r), errorCode, err)

		p.respondWithServerError(w, errorCode)
		return
	}

	// Check if results were already tabulated
	// If so, extract the votes and return from metadata
	currStatus := bpr.RecordMetadata.Status
	if (currStatus == backend.MDStatusVettedFinal) ||
		(currStatus == backend.MDStatusCensoredFinal) {
		reply.Status = convertBackendStatus(currStatus)
		var foundVotesFor, foundVotesAgainst bool
		for _, md := range bpr.Metadata {
			switch md.ID {
			case v1.ReferendumVotesForMDID:
				reply.VotesFor, err = strconv.Atoi(md.Payload)
				if err != nil {
					errorCode := time.Now().Unix()
					log.Errorf("%v Unable to extract votes from metadata payload %v: %v", remoteAddr(r),
						errorCode, err)
					p.respondWithServerError(w, errorCode)
					return
				}
				foundVotesFor = true

			case v1.ReferendumVotesAgainstMDID:
				reply.VotesAgainst, err = strconv.Atoi(md.Payload)
				if err != nil {
					errorCode := time.Now().Unix()
					log.Errorf("%v Unable to extract votes from metadata payload %v: %v", remoteAddr(r),
						errorCode, err)
					p.respondWithServerError(w, errorCode)
					return
				}
				foundVotesAgainst = true

			default:
				continue
			}
		}

		if foundVotesFor && foundVotesAgainst {
			util.RespondWithJSON(w, http.StatusOK, reply)
			return
		}
	}

	// Otherwise get the results and store them

	// Find token in AllReferendums
	ref := referendum.AllReferendums[t.Token]

	voteResults, newStatus, err := ref.GetResults()
	if err != nil {
		errorMsg := fmt.Sprintf("Unable to get referendum results: %v", err)
		p.respondWithUserError(w, v1.ErrorStatusInvalidRecordStatusTransition, []string{errorMsg})
		return
	}

	// Add vote counts as metadata
	votesMetadata := []backend.MetadataStream{
		backend.MetadataStream{
			ID:      v1.ReferendumVotesForMDID,
			Payload: strconv.Itoa(voteResults[v1.Approve]),
		},
		backend.MetadataStream{
			ID:      v1.ReferendumVotesAgainstMDID,
			Payload: strconv.Itoa(voteResults[v1.NotApprove]),
		},
	}
	// Ask backend to update status
	status, err := p.backend.SetUnvettedStatus(token, newStatus, votesMetadata, nil)
	if err != nil {
		oldStatus := v1.RecordStatus[convertBackendStatus(status)]
		newStatus := v1.RecordStatus[convertBackendStatus(newStatus)]
		// Check for specific errors
		if err == backend.ErrInvalidTransition {
			log.Errorf("%v Invalid status code transition: "+
				"%v %v->%v", remoteAddr(r), t.Token, oldStatus,
				newStatus)
			p.respondWithUserError(w, v1.ErrorStatusInvalidRecordStatusTransition, nil)
			return
		}
		// Generic internal error.
		errorCode := time.Now().Unix()
		log.Errorf("%v Set unvetted status error code %v: %v",
			remoteAddr(r), errorCode, err)

		p.respondWithServerError(w, errorCode)
		return
	}

	reply.VotesFor = voteResults[v1.Approve]
	reply.VotesAgainst = voteResults[v1.NotApprove]
	reply.Status = convertBackendStatus(newStatus)
	util.RespondWithJSON(w, http.StatusOK, reply)
}

func (p *politeia) inventory(w http.ResponseWriter, r *http.Request) {
	var i v1.Inventory
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&i); err != nil {
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, nil)
		return
	}
	defer r.Body.Close()

	challenge, err := hex.DecodeString(i.Challenge)
	if err != nil || len(challenge) != v1.ChallengeSize {
		p.respondWithUserError(w, v1.ErrorStatusInvalidChallenge, nil)
		return
	}
	response := p.identity.SignMessage(challenge)

	reply := v1.InventoryReply{
		Response: hex.EncodeToString(response[:]),
	}

	// Ask backend for inventory
	prs, brs, err := p.backend.Inventory(i.VettedCount, i.BranchesCount,
		i.IncludeFiles)
	if err != nil {
		// Generic internal error.
		errorCode := time.Now().Unix()
		log.Errorf("%v Inventory error code %v: %v", remoteAddr(r),
			errorCode, err)

		p.respondWithServerError(w, errorCode)
		return
	}

	// Convert backend records
	vetted := make([]v1.Record, 0, len(prs))
	for _, v := range prs {
		vetted = append(vetted, p.convertBackendRecord(v))
	}
	reply.Vetted = vetted

	// Convert branches
	unvetted := make([]v1.Record, 0, len(brs))
	for _, v := range brs {
		unvetted = append(unvetted, p.convertBackendRecord(v))
	}
	reply.Branches = unvetted

	util.RespondWithJSON(w, http.StatusOK, reply)
}

func (p *politeia) check(user, pass string) bool {
	if user != p.cfg.RPCUser || pass != p.cfg.RPCPass {
		return false
	}
	return true
}

func (p *politeia) auth(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || !p.check(user, pass) {
			log.Errorf("%v Unauthorized access for: %v",
				remoteAddr(r), user)
			w.Header().Set("WWW-Authenticate",
				`Basic realm="Politeiad"`)
			w.WriteHeader(401)
			w.Write([]byte("401 Unauthorized\n"))
			return
		}
		log.Infof("%v Authorized access for: %v",
			remoteAddr(r), user)
		fn(w, r)
	}
}

func (p *politeia) setUnvettedStatus(w http.ResponseWriter, r *http.Request) {
	var t v1.SetUnvettedStatus
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&t); err != nil {
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, nil)
		return
	}
	defer r.Body.Close()

	challenge, err := hex.DecodeString(t.Challenge)
	if err != nil || len(challenge) != v1.ChallengeSize {
		p.respondWithUserError(w, v1.ErrorStatusInvalidChallenge, nil)
		return
	}
	response := p.identity.SignMessage(challenge)

	// Validate token
	token, err := util.ConvertStringToken(t.Token)
	if err != nil {
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, nil)
		return
	}

	// Ask backend to update unvetted status
	status, err := p.backend.SetUnvettedStatus(token,
		convertFrontendStatus(t.Status),
		convertFrontendMetadataStream(t.MDAppend),
		convertFrontendMetadataStream(t.MDOverwrite))
	if err != nil {
		oldStatus := v1.RecordStatus[convertBackendStatus(status)]
		newStatus := v1.RecordStatus[t.Status]
		// Check for specific errors
		if err == backend.ErrInvalidTransition {
			log.Errorf("%v Invalid status code transition: "+
				"%v %v->%v", remoteAddr(r), t.Token, oldStatus,
				newStatus)
			p.respondWithUserError(w, v1.ErrorStatusInvalidRecordStatusTransition, nil)
			return
		}
		// Generic internal error.
		errorCode := time.Now().Unix()
		log.Errorf("%v Set unvetted status error code %v: %v",
			remoteAddr(r), errorCode, err)

		p.respondWithServerError(w, errorCode)
		return
	}
	reply := v1.SetUnvettedStatusReply{
		Response: hex.EncodeToString(response[:]),
		Status:   convertBackendStatus(status),
	}

	log.Infof("Set unvetted record status %v: token %v status %v",
		remoteAddr(r), t.Token, v1.RecordStatus[reply.Status])

	util.RespondWithJSON(w, http.StatusOK, reply)
}

func (p *politeia) updateVettedMetadata(w http.ResponseWriter, r *http.Request) {
	var t v1.UpdateVettedMetadata
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&t); err != nil {
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, nil)
		return
	}
	defer r.Body.Close()

	challenge, err := hex.DecodeString(t.Challenge)
	if err != nil || len(challenge) != v1.ChallengeSize {
		p.respondWithUserError(w, v1.ErrorStatusInvalidChallenge, nil)
		return
	}
	response := p.identity.SignMessage(challenge)

	// Validate token
	token, err := util.ConvertStringToken(t.Token)
	if err != nil {
		p.respondWithUserError(w, v1.ErrorStatusInvalidRequestPayload, nil)
		return
	}

	log.Infof("Update vetted metadata submitted %v: %x", remoteAddr(r),
		token)

	err = p.backend.UpdateVettedMetadata(token,
		convertFrontendMetadataStream(t.MDAppend),
		convertFrontendMetadataStream(t.MDOverwrite))
	if err != nil {
		if err == backend.ErrNoChanges {
			log.Errorf("%v update vetted metadata no changes: %x",
				remoteAddr(r), token)
			p.respondWithUserError(w, v1.ErrorStatusNoChanges, nil)
			return
		}
		// Check for content error.
		if contentErr, ok := err.(backend.ContentVerificationError); ok {
			log.Errorf("%v update vetted metadata content error: %v",
				remoteAddr(r), contentErr)
			p.respondWithUserError(w, contentErr.ErrorCode,
				contentErr.ErrorContext)
			return
		}

		// Generic internal error.
		errorCode := time.Now().Unix()
		log.Errorf("%v Update vetted metadata error code %v: %v",
			remoteAddr(r), errorCode, err)
		p.respondWithServerError(w, errorCode)
		return
	}

	// Reply
	reply := v1.UpdateVettedMetadataReply{
		Response: hex.EncodeToString(response[:]),
	}

	log.Infof("Update vetted metadata %v: token %x", remoteAddr(r), token)

	util.RespondWithJSON(w, http.StatusOK, reply)
}

// getError returns the error that is embedded in a JSON reply.
func getError(r io.Reader) (string, error) {
	var e interface{}
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(&e); err != nil {
		return "", err
	}
	m, ok := e.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("Could not decode response")
	}
	rError, ok := m["error"]
	if !ok {
		return "", fmt.Errorf("No error response")
	}
	return fmt.Sprintf("%v", rError), nil
}

func logging(f http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Trace incoming request
		log.Tracef("%v", newLogClosure(func() string {
			trace, err := httputil.DumpRequest(r, true)
			if err != nil {
				trace = []byte(fmt.Sprintf("logging: "+
					"DumpRequest %v", err))
			}
			return string(trace)
		}))

		// Log incoming connection
		log.Infof("%v %v %v %v", remoteAddr(r), r.Method, r.URL, r.Proto)
		f(w, r)
	}
}

func _main() error {
	// Load configuration and parse command line.  This function also
	// initializes logging and configures it accordingly.
	loadedCfg, _, err := loadConfig()
	if err != nil {
		return fmt.Errorf("Could not load configuration file: %v", err)
	}
	defer func() {
		if logRotator != nil {
			logRotator.Close()
		}
	}()

	log.Infof("Version : %v", version())
	log.Infof("Network : %v", activeNetParams.Params.Name)
	log.Infof("Home dir: %v", loadedCfg.HomeDir)

	// Create the data directory in case it does not exist.
	err = os.MkdirAll(loadedCfg.DataDir, 0700)
	if err != nil {
		return err
	}

	// Generate the TLS cert and key file if both don't already
	// exist.
	if !fileExists(loadedCfg.HTTPSKey) &&
		!fileExists(loadedCfg.HTTPSCert) {
		log.Infof("Generating HTTPS keypair...")

		err := util.GenCertPair(elliptic.P521(), "politeiad",
			loadedCfg.HTTPSCert, loadedCfg.HTTPSKey)
		if err != nil {
			return fmt.Errorf("unable to create https keypair: %v",
				err)
		}

		log.Infof("HTTPS keypair created...")
	}

	// Generate ed25519 identity to save messages, tokens etc.
	if !fileExists(loadedCfg.Identity) {
		log.Infof("Generating signing identity...")
		id, err := identity.New()
		if err != nil {
			return err
		}
		err = id.Save(loadedCfg.Identity)
		if err != nil {
			return err
		}
		log.Infof("Signing identity created...")
	}

	// Setup application context.
	p := &politeia{
		cfg: loadedCfg,
	}

	// Load identity.
	p.identity, err = identity.LoadFullIdentity(loadedCfg.Identity)
	if err != nil {
		return err
	}
	log.Infof("Public key: %x", p.identity.Public.Key)

	// Load certs, if there.  If they aren't there assume OS is used to
	// resolve cert validity.
	if len(loadedCfg.DcrtimeCert) != 0 {
		var certPool *x509.CertPool
		if !fileExists(loadedCfg.DcrtimeCert) {
			return fmt.Errorf("unable to find dcrtime cert %v",
				loadedCfg.DcrtimeCert)
		}
		dcrtimeCert, err := ioutil.ReadFile(loadedCfg.DcrtimeCert)
		if err != nil {
			return fmt.Errorf("unable to read dcrtime cert %v: %v",
				loadedCfg.DcrtimeCert, err)
		}
		certPool = x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(dcrtimeCert) {
			return fmt.Errorf("unable to load cert")
		}
	}

	// Setup backend.
	gitbe.UseLogger(gitbeLog)
	b, err := gitbe.New(loadedCfg.DataDir, loadedCfg.DcrtimeHost, "",
		loadedCfg.GitTrace)
	if err != nil {
		return err
	}
	p.backend = b

	// Setup mux
	p.router = mux.NewRouter()

	// Unprivileged routes
	p.router.HandleFunc(v1.IdentityRoute,
		logging(p.getIdentity)).Methods("POST")
	p.router.HandleFunc(v1.NewRecordRoute,
		logging(p.newRecord)).Methods("POST")
	p.router.HandleFunc(v1.UpdateUnvettedRoute,
		logging(p.updateUnvetted)).Methods("POST")
	p.router.HandleFunc(v1.GetUnvettedRoute,
		logging(p.getUnvetted)).Methods("POST")
	p.router.HandleFunc(v1.GetVettedRoute,
		logging(p.getVetted)).Methods("POST")
	p.router.HandleFunc(v1.ReferendumCallRoute,
		logging(p.referendumCall)).Methods("POST")
	p.router.HandleFunc(v1.ReferendumVoteRoute,
		logging(p.referendumVote)).Methods("POST")
	p.router.HandleFunc(v1.ReferendumResultsRoute,
		logging(p.referendumResults)).Methods("POST")

	// Routes that require auth
	p.router.HandleFunc(v1.InventoryRoute,
		logging(p.auth(p.inventory))).Methods("POST")
	p.router.HandleFunc(v1.SetUnvettedStatusRoute,
		logging(p.auth(p.setUnvettedStatus))).Methods("POST")
	p.router.HandleFunc(v1.UpdateVettedMetadataRoute,
		logging(p.auth(p.updateVettedMetadata))).Methods("POST")

	// Bind to a port and pass our router in
	listenC := make(chan error)
	for _, listener := range loadedCfg.Listeners {
		listen := listener
		go func() {
			log.Infof("Listen: %v", listen)
			listenC <- http.ListenAndServeTLS(listen,
				loadedCfg.HTTPSCert, loadedCfg.HTTPSKey,
				p.router)
		}()
	}

	// Tell user we are ready to go.
	log.Infof("Start of day")

	// Setup OS signals
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGINT)
	for {
		select {
		case sig := <-sigs:
			log.Infof("Terminating with %v", sig)
			goto done
		case err := <-listenC:
			log.Errorf("%v", err)
			goto done
		}
	}
done:
	p.backend.Close()

	log.Infof("Exiting")

	return nil
}

func main() {
	err := _main()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
