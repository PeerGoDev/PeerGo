// Package settlementtrafficv1 defines the immutable traffic result event sent
// from Settlement to Core. It deliberately contains final accounting facts
// only, including an optional bounded public breakdown: Tracker session
// identifiers, peer addresses, passkeys, policy identities and rule bodies
// remain in the Tracker Ledger.
package settlementtrafficv1

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"time"

	"github.com/peergo/peergo/contracts/go/jetstreamv1"
	"github.com/peergo/peergo/contracts/go/signedsnapshotv1"
)

const (
	SchemaVersion  = "settlement.traffic.v1"
	DefaultStream  = "PEERGO_SETTLEMENT_TRAFFIC_V1"
	DefaultSubject = "peergo.settlement.traffic.v1"
	MaxEventBytes  = 8 << 10

	// MaxExplanationSegments keeps the user-facing projection comfortably
	// below the stream and outbox 8 KiB limits. The Tracker Ledger retains all
	// segments; an unusually fragmented interval is represented by an explicit
	// omitted status rather than making final accounting unavailable.
	MaxExplanationSegments = 24
)

type ExplanationStatus string

const (
	ExplanationComplete ExplanationStatus = "complete"
	ExplanationOmitted  ExplanationStatus = "too_many_segments"
)

var (
	ErrInvalid    = errors.New("Settlement traffic event is invalid")
	uuidV7Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	userIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// EventID is the immutable Settlement ID, derived from the raw interval's
// Tracker announce event ID. Replaying this event therefore reaches the same
// Core idempotency fence rather than creating another traffic adjustment.
type Event struct {
	SchemaVersion     string       `json:"schema_version"`
	EventID           string       `json:"event_id"`
	OccurredAt        time.Time    `json:"occurred_at"`
	UserID            string       `json:"user_id"`
	TorrentID         int64        `json:"torrent_id"`
	IntervalStartsAt  time.Time    `json:"interval_starts_at"`
	IntervalEndsAt    time.Time    `json:"interval_ends_at"`
	RawUploaded       int64        `json:"raw_uploaded"`
	RawDownloaded     int64        `json:"raw_downloaded"`
	CreditedUploaded  int64        `json:"credited_uploaded"`
	ChargedDownloaded int64        `json:"charged_downloaded"`
	SettlementSHA256  string       `json:"settlement_sha256"`
	Explanation       *Explanation `json:"explanation,omitempty"`
}

// Explanation is a privacy-minimized projection of the immutable Ledger
// segments. It contains only final accounting values and service timestamps;
// policy identifiers, rule applications and Tracker session evidence never
// cross this contract boundary. A nil explanation remains valid so retained
// v1 events produced before this additive field can still be replayed.
type Explanation struct {
	Status       ExplanationStatus `json:"status"`
	SegmentCount int32             `json:"segment_count"`
	Segments     []Segment         `json:"segments,omitempty"`
}

type Segment struct {
	StartsAt          time.Time `json:"starts_at"`
	EndsAt            time.Time `json:"ends_at"`
	RawUploaded       int64     `json:"raw_uploaded"`
	RawDownloaded     int64     `json:"raw_downloaded"`
	CreditedUploaded  int64     `json:"credited_uploaded"`
	ChargedDownloaded int64     `json:"charged_downloaded"`
}

func Validate(event Event) error {
	if event.SchemaVersion != SchemaVersion || !uuidV7Pattern.MatchString(event.EventID) ||
		!userIDPattern.MatchString(event.UserID) || event.TorrentID < 1 || event.OccurredAt.IsZero() ||
		event.IntervalStartsAt.IsZero() || !event.IntervalEndsAt.After(event.IntervalStartsAt) ||
		event.RawUploaded < 0 || event.RawDownloaded < 0 || event.CreditedUploaded < 0 ||
		event.ChargedDownloaded < 0 || !digestPattern.MatchString(event.SettlementSHA256) {
		return ErrInvalid
	}
	for _, value := range []time.Time{event.OccurredAt, event.IntervalStartsAt, event.IntervalEndsAt} {
		if !isUTC(value) {
			return ErrInvalid
		}
	}
	if event.Explanation != nil && validateExplanation(event, *event.Explanation) != nil {
		return ErrInvalid
	}
	return nil
}

func validateExplanation(event Event, explanation Explanation) error {
	if explanation.SegmentCount < 1 {
		return ErrInvalid
	}
	switch explanation.Status {
	case ExplanationComplete:
		if explanation.SegmentCount > MaxExplanationSegments || int(explanation.SegmentCount) != len(explanation.Segments) {
			return ErrInvalid
		}
	case ExplanationOmitted:
		if explanation.SegmentCount <= MaxExplanationSegments || len(explanation.Segments) != 0 {
			return ErrInvalid
		}
		return nil
	default:
		return ErrInvalid
	}

	cursor := event.IntervalStartsAt
	var rawUploaded, rawDownloaded, creditedUploaded, chargedDownloaded int64
	for _, segment := range explanation.Segments {
		if !segment.StartsAt.Equal(cursor) || !segment.EndsAt.After(segment.StartsAt) ||
			segment.EndsAt.After(event.IntervalEndsAt) || !isUTC(segment.StartsAt) || !isUTC(segment.EndsAt) ||
			segment.RawUploaded < 0 || segment.RawDownloaded < 0 || segment.CreditedUploaded < 0 || segment.ChargedDownloaded < 0 {
			return ErrInvalid
		}
		var ok bool
		if rawUploaded, ok = safeAdd(rawUploaded, segment.RawUploaded); !ok {
			return ErrInvalid
		}
		if rawDownloaded, ok = safeAdd(rawDownloaded, segment.RawDownloaded); !ok {
			return ErrInvalid
		}
		if creditedUploaded, ok = safeAdd(creditedUploaded, segment.CreditedUploaded); !ok {
			return ErrInvalid
		}
		if chargedDownloaded, ok = safeAdd(chargedDownloaded, segment.ChargedDownloaded); !ok {
			return ErrInvalid
		}
		cursor = segment.EndsAt
	}
	if !cursor.Equal(event.IntervalEndsAt) || rawUploaded != event.RawUploaded || rawDownloaded != event.RawDownloaded ||
		creditedUploaded != event.CreditedUploaded || chargedDownloaded != event.ChargedDownloaded {
		return ErrInvalid
	}
	return nil
}

func isUTC(value time.Time) bool {
	_, offset := value.Zone()
	return offset == 0
}

func safeAdd(left, right int64) (int64, bool) {
	if right > math.MaxInt64-left {
		return 0, false
	}
	return left + right, true
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

func ValidStreamName(value string) bool {
	return jetstreamv1.ValidStreamName(value)
}

func ValidLiteralSubject(value string) bool {
	return jetstreamv1.ValidLiteralSubject(value)
}
