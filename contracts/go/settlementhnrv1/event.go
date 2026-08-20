// Package settlementhnrv1 defines the privacy-minimized H&R obligation
// projection emitted by Settlement and consumed by Core. Tracker session
// identities, policy provenance and raw evidence remain in Tracker Ledger.
package settlementhnrv1

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/peergo/peergo/contracts/go/jetstreamv1"
	"github.com/peergo/peergo/contracts/go/signedsnapshotv1"
)

const (
	SchemaVersion  = "settlement.hnr.v1"
	DefaultStream  = "PEERGO_SETTLEMENT_HNR_V1"
	DefaultSubject = "peergo.settlement.hnr.v1"
	MaxEventBytes  = 8 << 10
)

type State string

const (
	StateTracking  State = "tracking"
	StateSatisfied State = "satisfied"
	StateExempt    State = "exempt"
)

type SatisfiedBy string

const (
	SatisfiedBySeedTime SatisfiedBy = "seed_time"
	SatisfiedByRawRatio SatisfiedBy = "raw_ratio"
	SatisfiedByExempt   SatisfiedBy = "exempt"
)

var (
	ErrInvalid    = errors.New("Settlement H&R event is invalid")
	uuidV7Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	uuidPattern   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// Event is a complete versioned snapshot of one obligation. Byte values and
// ratios are raw H&R facts; economic credit and promotion data are excluded.
type Event struct {
	SchemaVersion       string       `json:"schema_version"`
	EventID             string       `json:"event_id"`
	OccurredAt          time.Time    `json:"occurred_at"`
	ObligationID        string       `json:"obligation_id"`
	ObligationVersion   int64        `json:"obligation_version"`
	UserID              string       `json:"user_id"`
	TorrentID           int64        `json:"torrent_id"`
	CompletedAt         time.Time    `json:"completed_at"`
	State               State        `json:"state"`
	SeededSeconds       int64        `json:"seeded_seconds"`
	RequiredSeedSeconds int64        `json:"required_seed_seconds"`
	RawUploaded         int64        `json:"raw_uploaded"`
	RawDownloaded       int64        `json:"raw_downloaded"`
	RawRatioBasisPoints int64        `json:"raw_ratio_basis_points"`
	RequiredRatioBPS    int64        `json:"required_ratio_basis_points"`
	AssessmentDueAt     time.Time    `json:"assessment_due_at"`
	GraceEndsAt         time.Time    `json:"grace_ends_at"`
	SatisfiedBy         *SatisfiedBy `json:"satisfied_by,omitempty"`
	SatisfiedAt         *time.Time   `json:"satisfied_at,omitempty"`
}

func Validate(event Event) error {
	if event.SchemaVersion != SchemaVersion || !uuidV7Pattern.MatchString(event.EventID) ||
		!uuidPattern.MatchString(event.ObligationID) || !uuidPattern.MatchString(event.UserID) ||
		event.ObligationVersion < 1 || event.TorrentID < 1 || event.OccurredAt.IsZero() ||
		event.CompletedAt.IsZero() || event.OccurredAt.Before(event.CompletedAt) ||
		event.AssessmentDueAt.Before(event.CompletedAt) ||
		event.GraceEndsAt.Before(event.AssessmentDueAt) || event.SeededSeconds < 0 ||
		event.RequiredSeedSeconds < 0 || event.RawUploaded < 0 || event.RawDownloaded < 0 ||
		event.RawRatioBasisPoints < 0 || event.RequiredRatioBPS < 0 {
		return ErrInvalid
	}
	for _, value := range []time.Time{event.OccurredAt, event.CompletedAt, event.AssessmentDueAt, event.GraceEndsAt} {
		if !isCanonicalTime(value) {
			return ErrInvalid
		}
	}
	switch event.State {
	case StateTracking:
		if event.SatisfiedBy != nil || event.SatisfiedAt != nil {
			return ErrInvalid
		}
	case StateSatisfied:
		if event.SatisfiedBy == nil || event.SatisfiedAt == nil || event.SatisfiedAt.Before(event.CompletedAt) ||
			event.SatisfiedAt.After(event.OccurredAt) ||
			(*event.SatisfiedBy != SatisfiedBySeedTime && *event.SatisfiedBy != SatisfiedByRawRatio) || !isCanonicalTime(*event.SatisfiedAt) {
			return ErrInvalid
		}
	case StateExempt:
		if event.SatisfiedBy == nil || *event.SatisfiedBy != SatisfiedByExempt || event.SatisfiedAt == nil ||
			!event.SatisfiedAt.Equal(event.CompletedAt) || !isCanonicalTime(*event.SatisfiedAt) {
			return ErrInvalid
		}
	default:
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

func ValidStreamName(value string) bool { return jetstreamv1.ValidStreamName(value) }

func ValidLiteralSubject(value string) bool { return jetstreamv1.ValidLiteralSubject(value) }

func isCanonicalTime(value time.Time) bool {
	_, offset := value.Zone()
	return offset == 0 && value.Nanosecond()%int(time.Microsecond) == 0
}
