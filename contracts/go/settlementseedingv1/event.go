// Package settlementseedingv1 defines the privacy-minimized, chunked evidence
// projection sent from Settlement to Core for hourly seeding rewards.
//
// Tracker-only identifiers (info hashes, announce event IDs, passkeys, peer
// addresses and session details) remain in Tracker Ledger. Core receives only
// the facts required to enrich and calculate a reward against its own catalog,
// user-benefit timeline and immutable reward policy.
package settlementseedingv1

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"time"

	"github.com/peergo/peergo/contracts/go/jetstreamv1"
	"github.com/peergo/peergo/contracts/go/signedsnapshotv1"
)

const (
	SchemaVersion   = "settlement.seeding.evidence.v1"
	DefaultStream   = "PEERGO_SETTLEMENT_SEEDING_EVIDENCE_V1"
	DefaultSubject  = "peergo.settlement.seeding.evidence.v1"
	MaxEventBytes   = 128 << 10
	MaxChunkEntries = 256
	MaxChunkCount   = 10_000
)

var (
	ErrInvalid    = errors.New("Settlement seeding evidence event is invalid")
	uuidPattern   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Item is the smallest reward evidence unit. EvidenceSHA256 links this
// minimized projection back to the complete immutable item in Tracker Ledger.
type Item struct {
	UserID           string `json:"user_id"`
	TorrentID        int64  `json:"torrent_id"`
	ActiveSeconds    int64  `json:"active_seconds"`
	RawUploadedBytes int64  `json:"raw_uploaded_bytes"`
	SnapshotSeeders  int32  `json:"snapshot_seeders"`
	SnapshotLeechers int32  `json:"snapshot_leechers"`
	EvidenceSHA256   string `json:"evidence_sha256"`
}

// Event carries one chunk of a complete closed UTC-hour projection. Every
// chunk repeats the immutable header. Core assembles all chunks and verifies
// ProjectionSHA256 before it makes the evidence eligible for reward work.
type Event struct {
	SchemaVersion        string    `json:"schema_version"`
	EventID              string    `json:"event_id"`
	WindowStart          time.Time `json:"window_start"`
	WindowEnd            time.Time `json:"window_end"`
	BuiltAt              time.Time `json:"built_at"`
	WindowEvidenceSHA256 string    `json:"window_evidence_sha256"`
	ProjectionSHA256     string    `json:"projection_sha256"`
	SnapshotID           string    `json:"snapshot_id"`
	SnapshotSequence     int64     `json:"snapshot_sequence"`
	SnapshotObservedAt   time.Time `json:"snapshot_observed_at"`
	ItemCount            int32     `json:"item_count"`
	ChunkIndex           int32     `json:"chunk_index"`
	ChunkCount           int32     `json:"chunk_count"`
	Items                []Item    `json:"items"`
}

type projectionDocument struct {
	SchemaVersion        string    `json:"schema_version"`
	WindowStart          time.Time `json:"window_start"`
	WindowEnd            time.Time `json:"window_end"`
	BuiltAt              time.Time `json:"built_at"`
	WindowEvidenceSHA256 string    `json:"window_evidence_sha256"`
	SnapshotID           string    `json:"snapshot_id"`
	SnapshotSequence     int64     `json:"snapshot_sequence"`
	SnapshotObservedAt   time.Time `json:"snapshot_observed_at"`
	ItemCount            int32     `json:"item_count"`
	Items                []Item    `json:"items"`
}

func Validate(event Event) error {
	if event.SchemaVersion != SchemaVersion || !uuidPattern.MatchString(event.EventID) ||
		!uuidPattern.MatchString(event.SnapshotID) || !digestPattern.MatchString(event.WindowEvidenceSHA256) ||
		!digestPattern.MatchString(event.ProjectionSHA256) || !validHeader(event) ||
		event.ChunkCount < 1 || event.ChunkCount > MaxChunkCount ||
		event.ChunkIndex < 0 || event.ChunkIndex >= event.ChunkCount || event.Items == nil ||
		len(event.Items) > MaxChunkEntries || validateItems(event.Items) != nil {
		return ErrInvalid
	}
	if event.ItemCount == 0 {
		if event.ChunkCount != 1 || event.ChunkIndex != 0 || len(event.Items) != 0 {
			return ErrInvalid
		}
		return nil
	}
	if len(event.Items) == 0 || event.ChunkCount > event.ItemCount {
		return ErrInvalid
	}
	return nil
}

func validHeader(event Event) bool {
	return event.WindowStart.Equal(event.WindowStart.Truncate(time.Hour)) &&
		isCanonicalTime(event.WindowStart) && isCanonicalTime(event.WindowEnd) &&
		isCanonicalTime(event.BuiltAt) && isCanonicalTime(event.SnapshotObservedAt) &&
		event.WindowEnd.Equal(event.WindowStart.Add(time.Hour)) && !event.BuiltAt.Before(event.WindowEnd) &&
		!event.SnapshotObservedAt.After(event.WindowEnd) && event.SnapshotSequence > 0 && event.ItemCount >= 0
}

func validateItems(items []Item) error {
	for index, item := range items {
		if !uuidPattern.MatchString(item.UserID) || item.TorrentID < 1 || item.ActiveSeconds < 0 ||
			item.ActiveSeconds > int64(time.Hour/time.Second) || item.RawUploadedBytes < 0 ||
			item.SnapshotSeeders < 0 || item.SnapshotLeechers < 0 || !digestPattern.MatchString(item.EvidenceSHA256) {
			return ErrInvalid
		}
		if index > 0 {
			previous := items[index-1]
			if previous.UserID > item.UserID || (previous.UserID == item.UserID && previous.TorrentID >= item.TorrentID) {
				return ErrInvalid
			}
		}
	}
	return nil
}

// ProjectionDigest hashes the complete, ordered minimized projection. The
// digest is independent of chunk boundaries and event IDs, so Core can detect
// a missing, duplicated, reordered or header-conflicting chunk before use.
func ProjectionDigest(header Event, items []Item) ([sha256.Size]byte, error) {
	if header.SchemaVersion != SchemaVersion || !uuidPattern.MatchString(header.SnapshotID) ||
		!digestPattern.MatchString(header.WindowEvidenceSHA256) || !validHeader(header) ||
		int64(header.ItemCount) != int64(len(items)) || validateItems(items) != nil ||
		!slices.IsSortedFunc(items, func(left, right Item) int {
			if left.UserID < right.UserID {
				return -1
			}
			if left.UserID > right.UserID {
				return 1
			}
			if left.TorrentID < right.TorrentID {
				return -1
			}
			if left.TorrentID > right.TorrentID {
				return 1
			}
			return 0
		}) {
		return [sha256.Size]byte{}, ErrInvalid
	}
	document := projectionDocument{
		SchemaVersion: SchemaVersion, WindowStart: header.WindowStart, WindowEnd: header.WindowEnd,
		BuiltAt: header.BuiltAt, WindowEvidenceSHA256: header.WindowEvidenceSHA256,
		SnapshotID: header.SnapshotID, SnapshotSequence: header.SnapshotSequence,
		SnapshotObservedAt: header.SnapshotObservedAt, ItemCount: header.ItemCount, Items: items,
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return [sha256.Size]byte{}, ErrInvalid
	}
	return sha256.Sum256(encoded), nil
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

func DigestHex(value [sha256.Size]byte) string { return hex.EncodeToString(value[:]) }

func ValidStreamName(value string) bool { return jetstreamv1.ValidStreamName(value) }

func ValidLiteralSubject(value string) bool { return jetstreamv1.ValidLiteralSubject(value) }

func isCanonicalTime(value time.Time) bool {
	_, offset := value.Zone()
	return !value.IsZero() && offset == 0 && value.Nanosecond()%int(time.Microsecond) == 0
}
