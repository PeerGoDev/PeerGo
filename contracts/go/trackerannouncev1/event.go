// Package trackerannouncev1 defines the language-neutral Tracker announce
// event consumed by Settlement. It contains only canonical codec and boundary
// validation; Tracker event construction and Settlement persistence remain in
// their owning services.
package trackerannouncev1

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/peergo/peergo/contracts/go/jetstreamv1"
	"github.com/peergo/peergo/contracts/go/signedsnapshotv1"
)

const (
	SchemaVersion  = "tracker.announce.v1"
	MaxEventBytes  = 16 << 10
	DefaultStream  = "PEERGO_TRACKER_ANNOUNCE_V1"
	DefaultSubject = "peergo.tracker.announce.v1"
)

var (
	ErrInvalid          = errors.New("Tracker announce event is invalid")
	uuidV7Pattern       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	userIDPattern       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	infoHashPattern     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sessionTokenPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	revisionPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
)

const (
	NetworkClassStandard = "standard"
	NetworkClassSeedbox  = "seedbox"
)

// NetworkEvidence is the privacy-minimized result of Tracker's signed network
// policy lookup. It intentionally excludes the socket address. Settlement can
// still explain every factor using the immutable policy sequence and rule ID.
type NetworkEvidence struct {
	PolicySequence           int64  `json:"policy_sequence"`
	PolicyRevision           string `json:"policy_revision"`
	Class                    string `json:"class"`
	RuleID                   string `json:"rule_id,omitempty"`
	UploadFactorBasisPoints  int64  `json:"upload_factor_basis_points"`
	SpeedLimitBytesPerSecond int64  `json:"speed_limit_bytes_per_second"`
}

type Event struct {
	SchemaVersion          string           `json:"schema_version"`
	EventID                string           `json:"event_id"`
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

func Validate(event Event) error {
	if event.SchemaVersion != SchemaVersion || !uuidV7Pattern.MatchString(event.EventID) ||
		event.ReceivedAt.IsZero() || !userIDPattern.MatchString(event.UserID) || event.TorrentID < 1 ||
		!infoHashPattern.MatchString(event.InfoHashV1) || !sessionTokenPattern.MatchString(event.SessionToken) ||
		(event.AddressFamily != 4 && event.AddressFamily != 6) ||
		(event.Event != "" && event.Event != "started" && event.Event != "stopped" && event.Event != "completed") ||
		event.Uploaded < 0 || event.Downloaded < 0 || event.Left < 0 || event.CredentialVersion < 1 ||
		event.TorrentControlSequence < 1 || event.SubjectControlSequence < 1 {
		return ErrInvalid
	}
	if event.CompletionID != "" && (!sessionTokenPattern.MatchString(event.CompletionID) || event.Event != "completed" || event.Left != 0) {
		return ErrInvalid
	}
	if event.NetworkEvidence != nil {
		evidence := event.NetworkEvidence
		if evidence.PolicySequence < 1 || !revisionPattern.MatchString(evidence.PolicyRevision) ||
			(evidence.Class != NetworkClassStandard && evidence.Class != NetworkClassSeedbox) ||
			evidence.UploadFactorBasisPoints < 0 || evidence.UploadFactorBasisPoints > 10_000 ||
			evidence.SpeedLimitBytesPerSecond < 0 ||
			(evidence.Class == NetworkClassStandard && (evidence.RuleID != "" || evidence.UploadFactorBasisPoints != 10_000)) ||
			(evidence.Class == NetworkClassSeedbox && !revisionPattern.MatchString(evidence.RuleID)) {
			return ErrInvalid
		}
	}
	_, offset := event.ReceivedAt.Zone()
	if offset != 0 {
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

func DecodeInfoHashV1(value string) ([20]byte, error) {
	var result [20]byte
	if !infoHashPattern.MatchString(value) {
		return result, ErrInvalid
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != len(result) {
		return result, ErrInvalid
	}
	copy(result[:], decoded)
	return result, nil
}

func DecodeSessionToken(value string) ([sha256.Size]byte, error) {
	var result [sha256.Size]byte
	if !sessionTokenPattern.MatchString(value) {
		return result, ErrInvalid
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != len(result) {
		return result, ErrInvalid
	}
	copy(result[:], decoded)
	return result, nil
}

func ValidStreamName(value string) bool {
	return jetstreamv1.ValidStreamName(value)
}

func ValidLiteralSubject(value string) bool {
	return jetstreamv1.ValidLiteralSubject(value)
}
