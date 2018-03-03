// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gitbe

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/decred/dcrtime/merkle"
	"github.com/decred/politeia/politeiad/backend"
	"github.com/decred/politeia/util"
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

const (
	LastAnchorKey  = "lastanchor"
	UnconfirmedKey = "unconfirmed"
)

type GitCommit struct {
	Hash    string
	Time    int64
	Message []string
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

// encodeAnchor encodes Anchor into a JSON byte slice.
func encodeAnchor(anchor Anchor) ([]byte, error) {
	b, err := json.Marshal(anchor)
	if err != nil {
		return nil, err
	}

	return b, nil
}

// DecodeAnchor decodes a JSON byte slice into an Anchor.
func DecodeAnchor(payload []byte) (*Anchor, error) {
	var anchor Anchor

	err := json.Unmarshal(payload, &anchor)
	if err != nil {
		return nil, err
	}

	return &anchor, nil
}

// writeAnchorRecordToFile stores a JSON byte slice to a file in the anchors directory
func (g *gitBackEnd) writeAnchorRecordToFile(anchorJSON []byte, anchorFilename string) error {
	// Make sure directory exists
	anchorDir := filepath.Join(g.vetted, defaultAnchorsDirectory)
	err := os.MkdirAll(anchorDir, 0774)
	if err != nil {
		return err
	}
	anchorFilePath := filepath.Join(anchorDir, anchorFilename)
	return ioutil.WriteFile(anchorFilePath, anchorJSON, 0664)
}

// getAnchorRecordFromFile gets a JSON byte slice from a file in the anchors directory
func (g *gitBackEnd) getAnchorRecordFromFile(anchorFilename string) ([]byte, error) {
	anchorFilePath := filepath.Join(g.vetted, defaultAnchorsDirectory, anchorFilename)
	return ioutil.ReadFile(anchorFilePath)
}

// listAnchorRecords returns a list of files in the anchor directory
func (g *gitBackEnd) listAnchorRecords() ([]backend.File, error) {
	anchorDir := filepath.Join(g.vetted, defaultAnchorsDirectory)
	files, err := ioutil.ReadDir(anchorDir)
	if err != nil {
		return nil, err
	}

	bf := make([]backend.File, 0, len(files))
	// Load all files
	for _, file := range files {
		fn := filepath.Join(anchorDir, file.Name())
		if file.IsDir() {
			return nil, fmt.Errorf("unexpected subdirectory found: %v", fn)
		}

		f := backend.File{Name: file.Name()}
		f.MIME, f.Digest, f.Payload, err = util.LoadFile(fn)
		if err != nil {
			return nil, err
		}
		bf = append(bf, f)
	}

	return bf, nil
}

// writeAnchorRecord encodes and writes the supplied record to the
// anchor directory.
//
// This function must be called with the lock held.
func (g *gitBackEnd) writeAnchorRecord(key [sha256.Size]byte, anchor Anchor) error {
	// make key
	k := make([]byte, sha256.Size)
	copy(k, key[:])

	// Encode
	la, err := encodeAnchor(anchor)
	if err != nil {
		return err
	}

	// Store to file
	filename := hex.EncodeToString(k)
	return g.writeAnchorRecordToFile(la, filename)
}

// readAnchorRecord retrieves the anchor record based on the provided merkle
// root.
//
// This function must be called with the lock held.
func (g *gitBackEnd) readAnchorRecord(key [sha256.Size]byte) (*Anchor, error) {
	// make key
	k := make([]byte, sha256.Size)
	copy(k, key[:])
	filename := hex.EncodeToString(k)

	// Get anchor from file
	payload, err := g.getAnchorRecordFromFile(filename)
	if err != nil {
		return nil, err
	}

	// Decode
	return DecodeAnchor(payload)
}

// encodeLastAnchor encodes LastAnchor into a byte slice.
func encodeLastAnchor(lastAnchor LastAnchor) ([]byte, error) {
	b, err := json.Marshal(lastAnchor)
	if err != nil {
		return nil, err
	}

	return b, nil
}

// DecodeLastAnchor decodes a payload into a LastAnchor.
func DecodeLastAnchor(payload []byte) (*LastAnchor, error) {
	var lastAnchor LastAnchor

	err := json.Unmarshal(payload, &lastAnchor)
	if err != nil {
		return nil, err
	}

	return &lastAnchor, nil
}

// writeLastAnchorRecord encodes and writes the supplied record to the
// anchor directory.
//
// This function must be called with the lock held.
func (g *gitBackEnd) writeLastAnchorRecord(lastAnchor LastAnchor) error {
	// Encode
	la, err := encodeLastAnchor(lastAnchor)
	if err != nil {
		return err
	}

	// Store anchor to file
	return g.writeAnchorRecordToFile(la, LastAnchorKey)
}

var (
	regexCommitHash           = regexp.MustCompile("^commit\\s+(\\S+)")
	regexCommitDate           = regexp.MustCompile("^Date:\\s+(.+)")
	anchorConfirmationPattern = fmt.Sprintf("^\\s*%s\\s+(\\S+)", markerAnchorConfirmation)
	regexAnchorConfirmation   = regexp.MustCompile(anchorConfirmationPattern)
	anchorPattern             = fmt.Sprintf("^\\s*%s\\s+(\\S+)", markerAnchor)
	regexAnchor               = regexp.MustCompile(anchorPattern)
)

const (
	dateTemplate = "Mon Jan 2 15:04:05 2006 -0700"
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
	commitTime, err := time.Parse(dateTemplate, dateStr)
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
		fmt.Printf("%+v\n", nextCommit)
		commits = append(commits, nextCommit)
		currLine = currLine + linesUsed
	}

	return commits, nil
}

// readLastAnchorRecord retrieves the last anchor record.
//
// This function must be called with the lock held.
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
	la.Merkle = []byte(merkleStr)
	la.Time = anchorCommit.Time

	// The latest commit hash is the top line, and the hash is the first word in the line.
	// There's a blank space in between the marker line and the list of commit hashes.
	topCommitLine := anchorCommit.Message[2]
	topCommitHash := strings.Fields(topCommitLine)[0]
	la.Last = []byte(topCommitHash)

	return &la, nil
}

// encodeUnconfirmedAnchor encodes an UnconfirmedAnchor record into a JSON byte
// slice.
func encodeUnconfirmedAnchor(unconfirmed UnconfirmedAnchor) ([]byte, error) {
	b, err := json.Marshal(unconfirmed)
	if err != nil {
		return nil, err
	}

	return b, nil
}

// DecodeUnconfirmedAnchor decodes a JSON byte slice into an UnconfirmedAnchor
// record.
func DecodeUnconfirmedAnchor(payload []byte) (*UnconfirmedAnchor, error) {
	var unconfirmed UnconfirmedAnchor

	err := json.Unmarshal(payload, &unconfirmed)
	if err != nil {
		return nil, err
	}

	return &unconfirmed, nil
}

// writeUnconfirmedAnchorRecord encodes and writes the supplied record to the
// anchor directory.
//
// This function must be called with the lock held.
func (g *gitBackEnd) writeUnconfirmedAnchorRecord(unconfirmed UnconfirmedAnchor) error {
	// Encode
	ua, err := encodeUnconfirmedAnchor(unconfirmed)
	if err != nil {
		return err
	}

	// Store anchor to file
	return g.writeAnchorRecordToFile(ua, UnconfirmedKey)
}

// readUnconfirmedAnchorRecord retrieves the unconfirmed anchor record.
//
// This function must be called with the lock held.
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
			allAnchors = append(allAnchors, merkleStr)
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
			ua.Merkles = append(ua.Merkles, []byte(merkleStr))
		}
	}

	return &ua, nil
}
