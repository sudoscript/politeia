package main

import (
	"encoding/base64"
	"testing"
	"time"

	pd "github.com/decred/politeia/politeiad/api/v1"
	www "github.com/decred/politeia/politeiawww/api/v1"
)

func createNewProposal(b *backend, t *testing.T) (*www.NewProposal, *www.NewProposalReply, error) {
	return createNewProposalWithFiles(b, t, 1, 0)
}

func createNewProposalWithFiles(b *backend, t *testing.T, numMDFiles, numImageFiles uint) (*www.NewProposal, *www.NewProposalReply, error) {
	return createNewProposalWithFileSizes(b, t, numMDFiles, numImageFiles, 64, 64)
}

func createNewProposalWithFileSizes(b *backend, t *testing.T, numMDFiles, numImageFiles, mdSize, imageSize uint) (*www.NewProposal, *www.NewProposalReply, error) {
	files := make([]pd.File, 0, numMDFiles+numImageFiles)

	for i := uint(0); i < numMDFiles; i++ {
		var (
			name string
		)

		// @rgeraldes - add at least one index file
		if i == 0 {
			name = indexFile
		} else {
			name = generateRandomString(5) + ".md"
		}

		files = append(files, pd.File{
			Name:    name,
			MIME:    "text/plain; charset=utf-8",
			Payload: base64.StdEncoding.EncodeToString([]byte(generateRandomString(int(mdSize)))),
		})
	}

	for i := uint(0); i < numImageFiles; i++ {
		files = append(files, pd.File{
			Name:    generateRandomString(5) + ".png",
			MIME:    "image/png",
			Payload: base64.StdEncoding.EncodeToString([]byte(generateRandomString(int(imageSize)))),
		})
	}

	np := www.NewProposal{
		Files: convertPropFilesFromPD(files),
	}

	npr, err := b.ProcessNewProposal(np)
	return &np, npr, err
}

func createNewProposalWithInvalidTitle(b *backend, t *testing.T) (*www.NewProposal, *www.NewProposalReply, error) {
	const (
		invalidTitle = "$%&/)Title<<>>"
	)
	files := make([]pd.File, 0, 2)
	filename := indexFile

	payload := base64.StdEncoding.EncodeToString([]byte(invalidTitle))

	files = append(files, pd.File{
		Name:    filename,
		MIME:    "text/plain; charset=utf-8",
		Payload: payload,
	})

	np := www.NewProposal{
		Files: convertPropFilesFromPD(files),
	}

	npr, err := b.ProcessNewProposal(np)
	return &np, npr, err
}

func createNewProposalWithDuplicateFiles(b *backend, t *testing.T) (*www.NewProposal, *www.NewProposalReply, error) {
	files := make([]pd.File, 0, 2)
	filename := indexFile
	payload := base64.StdEncoding.EncodeToString([]byte(generateRandomString(int(64))))

	files = append(files, pd.File{
		Name:    filename,
		MIME:    "text/plain; charset=utf-8",
		Payload: payload,
	})

	files = append(files, pd.File{
		Name:    filename,
		MIME:    "text/plain; charset=utf-8",
		Payload: payload,
	})

	np := www.NewProposal{
		Files: convertPropFilesFromPD(files),
	}

	npr, err := b.ProcessNewProposal(np)
	return &np, npr, err
}

func createNewProposalWithoutIndexFile(b *backend, t *testing.T) (*www.NewProposal, *www.NewProposalReply, error) {
	files := make([]pd.File, 0, 2)

	files = append(files, pd.File{
		Name:    "not_index.md",
		MIME:    "text/plain; charset=utf-8",
		Payload: base64.StdEncoding.EncodeToString([]byte(generateRandomString(int(64)))),
	})

	np := www.NewProposal{
		Files: convertPropFilesFromPD(files),
	}

	npr, err := b.ProcessNewProposal(np)
	return &np, npr, err
}

func publishProposal(b *backend, token string, t *testing.T) {
	sps := www.SetProposalStatus{
		Token:          token,
		ProposalStatus: www.PropStatusPublic,
	}
	_, err := b.ProcessSetProposalStatus(sps)
	if err != nil {
		t.Fatal(err)
	}
}

func censorProposal(b *backend, token string, t *testing.T) {
	sps := www.SetProposalStatus{
		Token:          token,
		ProposalStatus: www.PropStatusCensored,
	}
	_, err := b.ProcessSetProposalStatus(sps)
	if err != nil {
		t.Fatal(err)
	}
}

func getProposalDetails(b *backend, token string, t *testing.T) *www.ProposalDetailsReply {
	pd := www.ProposalsDetails{
		Token: token,
	}
	pdr, err := b.ProcessProposalDetails(pd, true)
	if err != nil {
		t.Error(err)
	}

	return pdr
}

func verifyProposalDetails(np *www.NewProposal, p www.ProposalRecord, t *testing.T) {
	if p.Files[0].Payload != np.Files[0].Payload {
		t.Fatalf("proposal descriptions do not match")
	}
}

func verifyProposals(p1 www.ProposalRecord, p2 www.ProposalRecord, t *testing.T) {
	if p1.Name != p2.Name {
		t.Fatalf("proposal names do not match: %v, %v", p1.Name, p2.Name)
	}
	if p1.Files[0].Payload != p2.Files[0].Payload {
		t.Fatalf("proposal descriptions do not match")
	}
}

func verifyProposalsSorted(b *backend, vettedProposals, unvettedProposals []www.ProposalRecord, t *testing.T) {
	// Verify that the proposals are returned sorted correctly.
	allVettedReply := b.ProcessAllVetted(www.GetAllVetted{})
	if len(allVettedReply.Proposals) != len(vettedProposals) {
		t.Fatalf("incorrect number of vetted proposals")
	}
	for i := 0; i < len(allVettedReply.Proposals); i++ {
		verifyProposals(allVettedReply.Proposals[i],
			vettedProposals[len(allVettedReply.Proposals)-i-1], t)
	}

	allUnvettedReply := b.ProcessAllUnvetted(www.GetAllUnvetted{})
	if len(allUnvettedReply.Proposals) != len(unvettedProposals) {
		t.Fatalf("incorrect number of unvetted proposals")
	}
	for i := 0; i < len(allUnvettedReply.Proposals); i++ {
		verifyProposals(allUnvettedReply.Proposals[i],
			unvettedProposals[len(allUnvettedReply.Proposals)-i-1], t)
	}
}

// Tests the policy restrictions applied when attempting to create a new proposal.
func TestNewProposalPolicyRestrictions(t *testing.T) {
	b := createBackend(t)

	p := b.ProcessPolicy(www.Policy{})

	_, _, err := createNewProposalWithFileSizes(b, t, p.MaxMDs, p.MaxImages, p.MaxMDSize, p.MaxImageSize)
	assertSuccess(t, err)

	_, _, err = createNewProposalWithFiles(b, t, p.MaxMDs+1, 0)
	assertError(t, err, www.ErrorStatusMaxMDsExceededPolicy)

	_, _, err = createNewProposalWithFiles(b, t, 1, p.MaxImages+1)
	assertError(t, err, www.ErrorStatusMaxImagesExceededPolicy)

	_, _, err = createNewProposalWithFiles(b, t, 0, 0)
	assertError(t, err, www.ErrorStatusProposalMissingFiles)

	_, _, err = createNewProposalWithFileSizes(b, t, 1, 0, p.MaxMDSize+1, 0)
	assertError(t, err, www.ErrorStatusMaxMDSizeExceededPolicy)

	_, _, err = createNewProposalWithFileSizes(b, t, 1, 1, 64, p.MaxImageSize+1)
	assertError(t, err, www.ErrorStatusMaxImageSizeExceededPolicy)

	_, _, err = createNewProposalWithInvalidTitle(b, t)
	assertErrorWithContext(t, err, www.ErrorStatusProposalInvalidTitle, []string{www.ErrorContextProposalInvalidTitle})

	_, _, err = createNewProposalWithDuplicateFiles(b, t)
	assertErrorWithContext(t, err, www.ErrorStatusProposalDuplicateFilenames, []string{indexFile})

	_, _, err = createNewProposalWithoutIndexFile(b, t)
	assertErrorWithContext(t, err, www.ErrorStatusProposalMissingFiles, []string{indexFile})
}

// Tests fetching an unreviewed proposal's details.
func TestUnreviewedProposal(t *testing.T) {
	b := createBackend(t)
	np, npr, err := createNewProposal(b, t)
	if err != nil {
		t.Fatal(err)
	}
	pdr := getProposalDetails(b, npr.CensorshipRecord.Token, t)
	verifyProposalDetails(np, pdr.Proposal, t)

	b.db.Close()
}

// Tests censoring a proposal and then fetching its details.
func TestCensoredProposal(t *testing.T) {
	b := createBackend(t)
	np, npr, err := createNewProposal(b, t)
	if err != nil {
		t.Fatal(err)
	}
	censorProposal(b, npr.CensorshipRecord.Token, t)
	pdr := getProposalDetails(b, npr.CensorshipRecord.Token, t)
	verifyProposalDetails(np, pdr.Proposal, t)

	b.db.Close()
}

// Tests publishing a proposal and then fetching its details.
func TestPublishedProposal(t *testing.T) {
	b := createBackend(t)
	np, npr, err := createNewProposal(b, t)
	if err != nil {
		t.Fatal(err)
	}
	publishProposal(b, npr.CensorshipRecord.Token, t)
	pdr := getProposalDetails(b, npr.CensorshipRecord.Token, t)
	verifyProposalDetails(np, pdr.Proposal, t)

	b.db.Close()
}

// Tests that the inventory is always sorted by timestamp.
func TestInventorySorted(t *testing.T) {
	b := createBackend(t)

	// Create an array of proposals, some vetted and some unvetted.
	allProposals := make([]www.ProposalRecord, 0, 5)
	vettedProposals := make([]www.ProposalRecord, 0)
	unvettedProposals := make([]www.ProposalRecord, 0)
	for i := 0; i < cap(allProposals); i++ {
		_, npr, err := createNewProposal(b, t)
		if err != nil {
			t.Fatal(err)
		}

		if i%2 == 0 {
			publishProposal(b, npr.CensorshipRecord.Token, t)
		}

		pdr := getProposalDetails(b, npr.CensorshipRecord.Token, t)
		allProposals = append(allProposals, pdr.Proposal)
		if i%2 == 0 {
			vettedProposals = append(vettedProposals, pdr.Proposal)
		} else {
			unvettedProposals = append(unvettedProposals, pdr.Proposal)
		}

		time.Sleep(time.Duration(2) * time.Second)
	}

	/*
		fmt.Printf("Proposals:\n")
		for _, v := range proposals {
			fmt.Printf("%v %v %v\n", v.Name, v.Status, v.Timestamp)
		}
	*/

	// Verify that the proposals are returned sorted correctly.
	verifyProposalsSorted(b, vettedProposals, unvettedProposals, t)

	// Wipe the inventory and fetch it again.
	err := b.LoadInventory()
	if err != nil {
		t.Fatal(err)
	}

	/*
		fmt.Printf("\nInventory:\n")
		for _, v := range b.inventory {
			fmt.Printf("%v %v %v\n", v.Name, v.Status, v.Timestamp)
		}
	*/

	// Verify that the proposals are still sorted correctly.
	verifyProposalsSorted(b, vettedProposals, unvettedProposals, t)

	b.db.Close()
}
