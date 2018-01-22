package referendum

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/decred/politeia/politeiad/api/v1"
	"github.com/decred/politeia/politeiad/api/v1/identity"
	"github.com/decred/politeia/politeiad/backend"
)

type Vote struct {
	User     identity.PublicIdentity
	VoteCast v1.VoteT
}

var AllReferendums = make(map[string]Referendum)

type ReferendumResults map[v1.VoteT]int

type Referendum struct {
	Token     string
	Record    *backend.Record
	startTime int64
	endTime   int64
	isActive  bool
	executed  bool
	Votes     map[identity.PublicIdentity]v1.VoteT
}

func (r *Referendum) CastVote(v Vote) error {
	// Check if referendum is active
	if !r.checkActive() {
		return fmt.Errorf("Referendum is closed.")
	}

	// See if they already voted
	user := v.User
	_, voted := r.Votes[user]
	if voted {
		return fmt.Errorf("User has already voted")
	}

	// Set their vote
	r.Votes[user] = v.VoteCast

	return nil
}

func (r *Referendum) checkActive() bool {
	if currTime := time.Now().Unix(); currTime > r.endTime {
		r.isActive = false
	}
	return r.isActive
}

func (r *Referendum) GetResults() (ReferendumResults, backend.MDStatusT, error) {
	var status backend.MDStatusT

	if r.checkActive() {
		return nil, status, fmt.Errorf("Referendum is still active")
	}

	results := make(map[v1.VoteT]int)
	for _, vote := range r.Votes {
		results[vote] += 1
	}

	if results[v1.Approve] > results[v1.NotApprove] {
		status = backend.MDStatusVettedFinal
	} else {
		status = backend.MDStatusCensoredFinal
	}

	return results, status, nil
}

func CreateReferendum(user identity.PublicIdentity, pr *backend.Record) (Referendum, error) {
	// Create Referendum
	refToken := hex.EncodeToString(pr.RecordMetadata.Token)
	ref := Referendum{
		Token:     refToken,
		Record:    pr,
		startTime: time.Now().Unix(),
		endTime:   time.Now().Unix() + v1.VotePeriod,
		isActive:  true,
		Votes:     make(map[identity.PublicIdentity]v1.VoteT),
	}
	AllReferendums[refToken] = ref

	// Set the calling user as already voted
	newVote := Vote{
		User:     user,
		VoteCast: v1.NullVote,
	}
	ref.CastVote(newVote)

	pr.RecordMetadata.Status = backend.MDStatusReferendum

	return ref, nil
}

func getReferendums() []Referendum {
	refs := make([]Referendum, len(AllReferendums))
	for _, r := range AllReferendums {
		refs = append(refs, r)
	}
	return refs
}
