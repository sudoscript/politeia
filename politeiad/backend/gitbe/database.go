// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gitbe

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/decred/dcrtime/merkle"
)

// An anchor corresponds to a set of git commit hashes, along with their
// merkle root, that are checkpointed in dcrtime. This provides censorship
// resistance by anchoring activity on politeia to the blockchain.
//
// To help process anchors, we need to look up the last anchor and unconfirmed anchors that
// have not been checkpointed in dcrtime yet. To identify these, we parse the
// git log, which keeps a record of all anchors dropped and anchors confirmed.

// AnchorType discriminates between the various Anchor record types.
type AnchorType uint32

const (
	AnchorInvalid    AnchorType = 0 // Invalid anchor
	AnchorUnverified AnchorType = 1 // Unverified anchor
	AnchorVerified   AnchorType = 2 // Verified anchor
)

// Anchor is stored in a file where the filename is the merkle root of digests.
// This record is pointed at by a the file "lastanchor".
//
// len(Digests) == len(Messages) and index offsets are linked. e.g. Digests[15]
// commit messages is in Messages[15].
type Anchor struct {
	Type     AnchorType // Type of anchor this record represents
	Digests  [][]byte   // All digests that were merkled to get to key of record
	Messages []string   // All one-line Commit messages
	Time     int64      // OS time when record was created

	// dcrtime portion, only valid when Type == AnchorVerified
	ChainTimestamp int64  // Time anchor was confirmed on blockchain
	Transaction    string // Anchor transaction
}

// LastAnchor stores the last commit anchored in dcrtime.
type LastAnchor struct {
	Last   []byte // Last git digest that was anchored
	Time   int64  // OS time when record was created
	Merkle []byte // Merkle root that points to Anchor record, if valid
}

// UnconfirmedAnchor stores Merkle roots of anchors that have not been confirmed
// yet by dcrtime.
type UnconfirmedAnchor struct {
	Merkles [][]byte // List of Merkle root that points to Anchor records
}

// newAnchorRecord creates an Anchor Record and the Merkle Root from the
// provided pieces.  Note that the merkle root is of the git digests!
func newAnchorRecord(t AnchorType, digests []*[sha256.Size]byte, messages []string) (*Anchor, *[sha256.Size]byte, error) {
	if len(digests) != len(messages) {
		return nil, nil, fmt.Errorf("invalid digest and messages length")
	}

	if t == AnchorInvalid {
		return nil, nil, fmt.Errorf("invalid anchor type")
	}

	a := Anchor{
		Type:     t,
		Messages: messages,
		Digests:  make([][]byte, 0, len(digests)),
		Time:     time.Now().Unix(),
	}

	for _, digest := range digests {
		d := make([]byte, sha256.Size)
		copy(d, digest[:])
		a.Digests = append(a.Digests, d)
	}

	return &a, merkle.Root(digests), nil
}

type GitCommit struct {
	Hash    string
	Time    int64
	Message []string
}

var (
	regexCommitHash           = regexp.MustCompile("^commit\\s+(\\S+)")
	regexCommitDate           = regexp.MustCompile("^Date:\\s+(.+)")
	anchorConfirmationPattern = fmt.Sprintf("^\\s*%s\\s*$", markerAnchorConfirmation)
	regexAnchorConfirmation   = regexp.MustCompile(anchorConfirmationPattern)
	anchorPattern             = fmt.Sprintf("^\\s*%s\\s+(\\S+)", markerAnchor)
	regexAnchor               = regexp.MustCompile(anchorPattern)
)

const (
	gitDateTemplate = "Mon Jan 2 15:04:05 2006 -0700"
)

// extractNextCommit takes a slice of a git log and parses the next commit into a GitCommit struct
func extractNextCommit(logSlice []string) (*GitCommit, int, error) {
	var commit GitCommit

	// Make sure we're at the start of a new commit
	firstLine := logSlice[0]
	if !regexCommitHash.MatchString(firstLine) {
		return nil, 0, fmt.Errorf("Error parsing git log. Commit expected, found %q instead", firstLine)
	}
	commit.Hash = regexCommitHash.FindStringSubmatch(logSlice[0])[1]

	// Skip the next line, which has the commit author

	dateLine := logSlice[2]
	if !regexCommitDate.MatchString(dateLine) {
		return nil, 0, fmt.Errorf("Error parsing git log. Date expected, found %q instead", dateLine)
	}
	dateStr := regexCommitDate.FindStringSubmatch(logSlice[2])[1]
	commitTime, err := time.Parse(gitDateTemplate, dateStr)
	if err != nil {
		return nil, 0, fmt.Errorf("Error parsing git log. Unable to parse date: %v", err)
	}
	commit.Time = commitTime.Unix()

	// The first three lines are the commit hash, the author, and the date.
	// The fourth is a blank line. Start accumulating the message at the 5th line.
	// Append message lines until the start of the next commit is found.
	for _, line := range logSlice[4:] {
		if regexCommitHash.MatchString(line) {
			break
		}

		commit.Message = append(commit.Message, line)
	}

	// In total, we used 4 lines initially, plus the number of lines in the message.
	return &commit, len(commit.Message) + 4, nil
}

func (g *gitBackEnd) getCommitsFromLog() ([]*GitCommit, error) {
	// Get the git log
	gitLog, err := g.gitLog(g.vetted)
	if err != nil {
		return nil, err
	}

	// Parse the log into GitCommit structs for easier processing
	var commits []*GitCommit
	currLine := 0
	for currLine < len(gitLog) {
		nextCommit, linesUsed, err := extractNextCommit(gitLog[currLine:])
		if err != nil {
			return nil, err
		}
		commits = append(commits, nextCommit)
		currLine = currLine + linesUsed
	}

	return commits, nil
}

// extractAnchorDigests returns a list of digest bytes from an anchor GitCommit,
// as well as a list of commit messages for what was commited
func extractAnchorDigests(anchorCommit *GitCommit) ([][]byte, []string, error) {
	// Make sure it is an anchor commit
	firstLine := anchorCommit.Message[0]
	if !regexAnchor.MatchString(firstLine) {
		return nil, nil, fmt.Errorf("Error parsing git log. Expected an anchor commit. Instead got %q", firstLine)
	}

	// Hashes are listed starting from the 3rd line in the commit message
	// The hash is the first word in the line. The commit message is the rest.
	// Ignore the last blank line
	var digests [][]byte
	var messages []string
	for _, line := range anchorCommit.Message[2 : len(anchorCommit.Message)-1] {
		digests = append(digests, []byte(
			strings.Fields(line)[0]),
		)
		messages = append(messages, strings.Join(
			strings.Fields(line)[1:], " "),
		)
	}

	return digests, messages, nil
}

// listAnchorRecords extracts all anchor records from the git log and returns
// an array of Anchor structs and an array of their Merkle roots
func (g *gitBackEnd) listAnchorRecords() ([]*Anchor, []string, error) {
	// Get the commits from the git log
	gitCommits, err := g.getCommitsFromLog()
	if err != nil {
		return nil, nil, err
	}

	// Store anchor commits in an array and confirmed anchors as keys in a map,
	// to check for confirmation status later.
	var merkleStr string
	var anchorCommits []*GitCommit
	confirmedAnchors := make(map[string]struct{})
	for _, commit := range gitCommits {
		// Check the first line of the commit message to see if it is an
		// anchor confirmation or an anchor.
		if regexAnchorConfirmation.MatchString(commit.Message[0]) {
			// There's a blank line between the marker header and the body
			// The Merkle root of the confirmed anchor is the first word in the body
			merkleStr = strings.Fields(commit.Message[2])[0]
			confirmedAnchors[merkleStr] = struct{}{}
		} else if regexAnchor.MatchString(commit.Message[0]) {
			anchorCommits = append(anchorCommits, commit)
		}
	}

	// Create Anchor structs for each anchor record in git
	var anchors []*Anchor
	var keys, messages []string
	var anchorType AnchorType
	var digests [][]byte
	for _, commit := range anchorCommits {
		// The Merkle root is on the same line as the marker header
		merkleStr = regexAnchor.FindStringSubmatch(commit.Message[0])[1]
		keys = append(keys, merkleStr)

		// Check status
		anchorType = AnchorUnverified
		_, confirmed := confirmedAnchors[merkleStr]
		if confirmed {
			anchorType = AnchorVerified
		}

		// Extract commit hashes and messages
		digests, messages, err = extractAnchorDigests(commit)
		if err != nil {
			return nil, nil, err
		}
		anchors = append(anchors, &Anchor{
			Type:     anchorType,
			Digests:  digests,
			Messages: messages,
			Time:     commit.Time,
		})
	}

	return anchors, keys, nil
}

// readAnchorRecord matches an anchor by its Merkle root and retrieves it from the git log
func (g *gitBackEnd) readAnchorRecord(key [sha256.Size]byte) (*Anchor, error) {
	anchors, keys, err := g.listAnchorRecords()
	if err != nil {
		return nil, err
	}
	var anchor *Anchor
	merkleStr := hex.EncodeToString(key[:])
	for i, _ := range anchors {
		if merkleStr == keys[i] {
			anchor = anchors[i]
			break
		}
	}
	if anchor == nil {
		return nil, fmt.Errorf("Anchor not found for key %v", key)
	}

	return anchor, nil

}

// readLastAnchorRecord retrieves the last anchor record.
func (g *gitBackEnd) readLastAnchorRecord() (*LastAnchor, error) {
	// Get the commits from the log
	gitCommits, err := g.getCommitsFromLog()
	if err != nil {
		return nil, err
	}

	// Iterate over commits to find the last anchor
	var found bool
	var la LastAnchor
	var anchorCommit *GitCommit
	for _, commit := range gitCommits {
		// Check the first line of the commit message
		// Make sure it is an anchor, not an anchor confirmation
		if !regexAnchorConfirmation.MatchString(commit.Message[0]) &&
			regexAnchor.MatchString(commit.Message[0]) {
			found = true
			anchorCommit = commit
			break
		}
	}
	// If not found, return a blank last anchor
	if !found {
		return &la, nil
	}

	merkleStr := regexAnchor.FindStringSubmatch(anchorCommit.Message[0])[1]
	merkleBytes, err := hex.DecodeString(merkleStr)
	if err != nil {
		return nil, err
	}
	la.Merkle = merkleBytes
	la.Time = anchorCommit.Time

	hashBytes, err := hex.DecodeString(anchorCommit.Hash)
	if err != nil {
		return nil, err
	}
	la.Last = extendSHA1(hashBytes)

	return &la, nil
}

// readUnconfirmedAnchorRecord retrieves the unconfirmed anchor record.
func (g *gitBackEnd) readUnconfirmedAnchorRecord() (*UnconfirmedAnchor, error) {
	// Get the commits from the git log
	gitCommits, err := g.getCommitsFromLog()
	if err != nil {
		return nil, err
	}

	// Iterate over the commits and store the Merkle roots of all anchors in an array and
	// the confirmed anchors as keys in a map, which will make it faster to check
	// membership later.
	var merkleStr string
	var allAnchors []string
	confirmedAnchors := make(map[string]struct{}, len(gitCommits))
	for _, commit := range gitCommits {
		// Check the first line of the commit message to see if it is an
		// anchor confirmation or an anchor.
		if regexAnchorConfirmation.MatchString(commit.Message[0]) {
			// There's a blank line between the marker header and the body
			// The Merkle root of the confirmed anchor is the first word in the body
			merkleStr = strings.Fields(commit.Message[2])[0]
			confirmedAnchors[merkleStr] = struct{}{}
		} else if regexAnchor.MatchString(commit.Message[0]) {
			// The Merkle root is on the same line as the marker header
			merkleStr = regexAnchor.FindStringSubmatch(commit.Message[0])[1]
			allAnchors = append(allAnchors, merkleStr)
		}
	}

	// Now find anchors that haven't been confirmed yet
	var ua UnconfirmedAnchor
	for _, merkleStr := range allAnchors {
		_, confirmed := confirmedAnchors[merkleStr]
		if !confirmed {
			merkleBytes, err := hex.DecodeString(merkleStr)
			if err != nil {
				fmt.Printf("error decoding string: %s", merkleStr)
				return nil, err
			}
			ua.Merkles = append(ua.Merkles, merkleBytes)
		}
	}

	return &ua, nil
}
