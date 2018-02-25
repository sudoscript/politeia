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
	"time"

	"github.com/decred/dcrtime/merkle"
	"github.com/decred/politeia/politeiad/backend"
	"github.com/decred/politeia/util"
)

// The database contains 3 types of records:
//	[lastanchor][LastAnchor]
//	[Merkle Root][Anchor]
//	[unconfirmed][UnconfirmedAnchor]
//
// The LastAnchor record is used to persist the last committed anchor.  The
// information that is contained in the record allows us to create a git log
// range to calculate the new LastAnchor.  There is always one and only one
// LastAnchor record in the database (with the exception when bootstrapping the
// system).
//
// The anchor records simply contain all information that went into creating an
// anchor and are essentially redundant from a data perspective.  We keep this
// information for caching purposes so that we don't have to parse git output.
//
// The unconfirmed anchor records are a list of all anchor merkle roots that
// have not been confirmed by dcrtime.  This record is used at startup time to
// identify what anchors have not been confirmed by dcrtime and to resume
// waiting for their confirmation.  Once an anchor is confirmed it should be
// removed from this list; this operation SHALL be atomic.

// AnchorType discriminates between the various Anchor record types.
type AnchorType uint32

const (
	AnchorInvalid    AnchorType = 0 // Invalid anchor
	AnchorUnverified AnchorType = 1 // Unverified anchor
	AnchorVerified   AnchorType = 2 // Verified anchor
)

// Anchor is a database record where the merkle root of digests is the key.
// This record is pointed at by LastAnchor.Root.
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
// database.
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
// database.
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

// readLastAnchorRecord retrieves the last anchor record.
//
// This function must be called with the lock held.
func (g *gitBackEnd) readLastAnchorRecord() (*LastAnchor, error) {
	// Get anchor from file
	payload, err := g.getAnchorRecordFromFile(LastAnchorKey)
	if err != nil {
		// If one doesn't exist, create an empty LastAnchor
		if os.IsNotExist(err) {
			return &LastAnchor{}, nil
		}
		return nil, err
	}

	// Decode
	return DecodeLastAnchor(payload)
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
// database.
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
	// Get anchor from file
	payload, err := g.getAnchorRecordFromFile(UnconfirmedKey)
	if err != nil {
		// If one doesn't exist, create an empty UnconfimredAnchor
		if os.IsNotExist(err) {
			return &UnconfirmedAnchor{}, nil
		}
		return nil, err
	}

	// Decode
	return DecodeUnconfirmedAnchor(payload)
}
