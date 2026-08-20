// Package announceevent defines the privacy-minimized durable event emitted by
// the HTTP Tracker. It carries absolute counters and exact control versions,
// but never carries the route passkey, full request URL, peer IP or port.
package announceevent

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"time"

	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/services/tracker/internal/protocol"
	"github.com/peergo/peergo/services/tracker/internal/uuidv7"
)

const (
	SchemaVersion = trackerannouncev1.SchemaVersion
	MaxEventBytes = trackerannouncev1.MaxEventBytes
	sessionDomain = "peergo:tracker:announce-session-token:v1\x00"
)

var ErrInvalid = trackerannouncev1.ErrInvalid

type Event = trackerannouncev1.Event

type Input struct {
	ReceivedAt             time.Time
	UserID                 string
	TorrentID              int64
	InfoHash               [20]byte
	PeerID                 [20]byte
	Key                    string
	AddressFamily          int
	Event                  protocol.Event
	Uploaded               int64
	Downloaded             int64
	Left                   int64
	CredentialVersion      int64
	TorrentControlSequence int64
	SubjectControlSequence int64
	CompletionToken        [32]byte
	NetworkEvidence        *trackerannouncev1.NetworkEvidence
}

type Factory struct {
	random io.Reader
}

func NewFactory(random io.Reader) *Factory {
	if random == nil {
		random = rand.Reader
	}
	return &Factory{random: random}
}

func (factory *Factory) New(input Input) (Event, error) {
	if factory == nil || factory.random == nil || input.ReceivedAt.IsZero() ||
		input.UserID == "" || len(input.Key) > protocol.MaxKeyBytes {
		return Event{}, ErrInvalid
	}
	hasCompletion := input.CompletionToken != ([32]byte{})
	if hasCompletion && (input.Event != protocol.EventCompleted || input.Left != 0) {
		return Event{}, ErrInvalid
	}
	eventID, err := uuidv7.New(input.ReceivedAt, factory.random)
	if err != nil {
		return Event{}, err
	}
	event := Event{
		SchemaVersion: SchemaVersion, EventID: eventID, ReceivedAt: input.ReceivedAt.UTC().Round(0),
		UserID: input.UserID, TorrentID: input.TorrentID,
		InfoHashV1:   hex.EncodeToString(input.InfoHash[:]),
		SessionToken: sessionToken(input), AddressFamily: input.AddressFamily,
		Event: eventName(input.Event), Uploaded: input.Uploaded, Downloaded: input.Downloaded, Left: input.Left,
		CredentialVersion:      input.CredentialVersion,
		TorrentControlSequence: input.TorrentControlSequence,
		SubjectControlSequence: input.SubjectControlSequence,
		NetworkEvidence:        cloneNetworkEvidence(input.NetworkEvidence),
	}
	if hasCompletion {
		event.CompletionID = completionID(event.SessionToken, input.CompletionToken)
	}
	if trackerannouncev1.Validate(event) != nil {
		return Event{}, ErrInvalid
	}
	return event, nil
}

func cloneNetworkEvidence(evidence *trackerannouncev1.NetworkEvidence) *trackerannouncev1.NetworkEvidence {
	if evidence == nil {
		return nil
	}
	copy := *evidence
	if evidence.DownloadFactorBasisPoints != nil {
		factor := *evidence.DownloadFactorBasisPoints
		copy.DownloadFactorBasisPoints = &factor
	}
	return &copy
}

func completionID(sessionToken string, transitionToken [32]byte) string {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte("peergo:tracker:completion-id:v1\x00"))
	_, _ = hasher.Write([]byte(sessionToken))
	_, _ = hasher.Write(transitionToken[:])
	return hex.EncodeToString(hasher.Sum(nil))
}

func Encode(event Event) ([]byte, error) {
	return trackerannouncev1.Encode(event)
}

func Decode(encoded []byte) (Event, error) {
	return trackerannouncev1.Decode(encoded)
}

func sessionToken(input Input) string {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(sessionDomain))
	_, _ = hasher.Write(input.InfoHash[:])
	var size [2]byte
	binary.BigEndian.PutUint16(size[:], uint16(len(input.UserID)))
	_, _ = hasher.Write(size[:])
	_, _ = hasher.Write([]byte(input.UserID))
	_, _ = hasher.Write(input.PeerID[:])
	binary.BigEndian.PutUint16(size[:], uint16(len(input.Key)))
	_, _ = hasher.Write(size[:])
	_, _ = hasher.Write([]byte(input.Key))
	return hex.EncodeToString(hasher.Sum(nil))
}

func eventName(event protocol.Event) string {
	switch event {
	case protocol.EventStarted:
		return "started"
	case protocol.EventStopped:
		return "stopped"
	case protocol.EventCompleted:
		return "completed"
	default:
		return ""
	}
}
