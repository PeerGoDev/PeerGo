// Package trackerannouncev2 defines the sequenced Tracker announce envelope.
//
// The producer tuple is the durable business-idempotency key. JetStream's
// stream sequence remains a transport watermark and EventID remains useful for
// tracing, but neither has to be retained once the replay window expires.
package trackerannouncev2

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/peergo/peergo/contracts/go/jetstreamv1"
	"github.com/peergo/peergo/contracts/go/signedsnapshotv1"
	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
)

const (
	SchemaVersion  = "tracker.announce.v2"
	MaxEventBytes  = trackerannouncev1.MaxEventBytes
	DefaultStream  = "PEERGO_TRACKER_ANNOUNCE_V2"
	DefaultSubject = "peergo.tracker.announce.v2"
)

var (
	ErrInvalid        = errors.New("sequenced Tracker announce event is invalid")
	producerIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	uuidV7Pattern     = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

type NetworkEvidence = trackerannouncev1.NetworkEvidence

const (
	NetworkClassStandard = trackerannouncev1.NetworkClassStandard
	NetworkClassSeedbox  = trackerannouncev1.NetworkClassSeedbox
)

// Event deliberately keeps the announce fields flat. This lets operators and
// non-Go consumers inspect the envelope without understanding a nested v1
// payload while ToV1 provides one canonical compatibility boundary internally.
type Event struct {
	SchemaVersion          string           `json:"schema_version"`
	EventID                string           `json:"event_id"`
	ProducerID             string           `json:"producer_id"`
	ProducerEpoch          string           `json:"producer_epoch"`
	ProducerSequence       int64            `json:"producer_sequence"`
	ReceivedAt             time.Time        `json:"received_at"`
	UserID                 string           `json:"user_id"`
	TorrentID              int64            `json:"torrent_id"`
	InfoHashV1             string           `json:"info_hash_v1"`
	SessionToken           string           `json:"session_token"`
	CompletionID           string           `json:"completion_id,omitempty"`
	AddressFamily          int              `json:"address_family"`
	Event                  string           `json:"event"`
	Uploaded               int64            `json:"uploaded"`
	Downloaded             int64            `json:"downloaded"`
	Left                   int64            `json:"left"`
	CredentialVersion      int64            `json:"credential_version"`
	TorrentControlSequence int64            `json:"torrent_control_sequence"`
	SubjectControlSequence int64            `json:"subject_control_sequence"`
	NetworkEvidence        *NetworkEvidence `json:"network_evidence,omitempty"`
}

func FromV1(announce trackerannouncev1.Event, producerID, producerEpoch string, producerSequence int64) (Event, error) {
	event := Event{
		SchemaVersion: SchemaVersion, EventID: announce.EventID,
		ProducerID: producerID, ProducerEpoch: producerEpoch, ProducerSequence: producerSequence,
		ReceivedAt: announce.ReceivedAt, UserID: announce.UserID, TorrentID: announce.TorrentID,
		InfoHashV1: announce.InfoHashV1, SessionToken: announce.SessionToken, CompletionID: announce.CompletionID,
		AddressFamily: announce.AddressFamily, Event: announce.Event,
		Uploaded: announce.Uploaded, Downloaded: announce.Downloaded, Left: announce.Left,
		CredentialVersion:      announce.CredentialVersion,
		TorrentControlSequence: announce.TorrentControlSequence,
		SubjectControlSequence: announce.SubjectControlSequence,
		NetworkEvidence:        announce.NetworkEvidence,
	}
	if Validate(event) != nil {
		return Event{}, ErrInvalid
	}
	return event, nil
}

func (event Event) ToV1() trackerannouncev1.Event {
	return trackerannouncev1.Event{
		SchemaVersion: trackerannouncev1.SchemaVersion, EventID: event.EventID,
		ReceivedAt: event.ReceivedAt, UserID: event.UserID, TorrentID: event.TorrentID,
		InfoHashV1: event.InfoHashV1, SessionToken: event.SessionToken, CompletionID: event.CompletionID,
		AddressFamily: event.AddressFamily, Event: event.Event,
		Uploaded: event.Uploaded, Downloaded: event.Downloaded, Left: event.Left,
		CredentialVersion:      event.CredentialVersion,
		TorrentControlSequence: event.TorrentControlSequence,
		SubjectControlSequence: event.SubjectControlSequence,
		NetworkEvidence:        event.NetworkEvidence,
	}
}

func Validate(event Event) error {
	if event.SchemaVersion != SchemaVersion || !ValidProducerID(event.ProducerID) ||
		!uuidV7Pattern.MatchString(event.ProducerEpoch) || event.ProducerSequence < 1 ||
		trackerannouncev1.Validate(event.ToV1()) != nil {
		return ErrInvalid
	}
	return nil
}

func Encode(event Event) ([]byte, error) {
	if Validate(event) != nil {
		return nil, ErrInvalid
	}
	encoded, err := json.Marshal(event)
	if err != nil || len(encoded) < 2 || len(encoded) > MaxEventBytes {
		return nil, ErrInvalid
	}
	return encoded, nil
}

func Decode(encoded []byte) (Event, error) {
	if len(encoded) < 2 || len(encoded) > MaxEventBytes {
		return Event{}, ErrInvalid
	}
	var event Event
	if err := signedsnapshotv1.StrictJSON(encoded, &event); err != nil || Validate(event) != nil {
		return Event{}, ErrInvalid
	}
	canonical, err := json.Marshal(event)
	if err != nil || !bytes.Equal(canonical, encoded) {
		return Event{}, ErrInvalid
	}
	return event, nil
}

func ValidProducerID(value string) bool { return producerIDPattern.MatchString(value) }

func ValidStreamName(value string) bool { return jetstreamv1.ValidStreamName(value) }

func ValidLiteralSubject(value string) bool { return jetstreamv1.ValidLiteralSubject(value) }
