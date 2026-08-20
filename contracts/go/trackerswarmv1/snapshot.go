// Package trackerswarmv1 defines the canonical chunked full-snapshot event
// emitted by one Tracker Swarm Engine. Core applies a snapshot only after every
// chunk has arrived; a partial publication can therefore never zero or replace
// a previously complete public projection.
package trackerswarmv1

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"time"

	"github.com/peergo/peergo/contracts/go/jetstreamv1"
	"github.com/peergo/peergo/contracts/go/signedsnapshotv1"
)

const (
	SchemaVersion   = "tracker.swarm.snapshot.v1"
	DefaultStream   = "PEERGO_TRACKER_SWARM_SNAPSHOT_V1"
	DefaultSubject  = "peergo.tracker.swarm.snapshot.v1"
	ScopeAll        = "all"
	MaxEventBytes   = 512 << 10
	MaxChunkEntries = 2_000
	MaxChunkCount   = 10_000
)

var (
	ErrInvalid      = errors.New("Tracker swarm snapshot event is invalid")
	uuidV7Pattern   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	sourceIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	infoHashPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

type Entry struct {
	InfoHashV1 string `json:"info_hash_v1"`
	Seeders    int32  `json:"seeders"`
	Leechers   int32  `json:"leechers"`
}

type SnapshotChunk struct {
	SchemaVersion    string    `json:"schema_version"`
	EventID          string    `json:"event_id"`
	SnapshotID       string    `json:"snapshot_id"`
	SourceID         string    `json:"source_id"`
	RoutingEpoch     int64     `json:"routing_epoch"`
	SnapshotSequence int64     `json:"snapshot_sequence"`
	ObservedAt       time.Time `json:"observed_at"`
	Scope            string    `json:"scope"`
	ChunkIndex       int32     `json:"chunk_index"`
	ChunkCount       int32     `json:"chunk_count"`
	Entries          []Entry   `json:"entries"`
}

func Validate(chunk SnapshotChunk) error {
	if chunk.SchemaVersion != SchemaVersion || !uuidV7Pattern.MatchString(chunk.EventID) ||
		!uuidV7Pattern.MatchString(chunk.SnapshotID) || !sourceIDPattern.MatchString(chunk.SourceID) ||
		chunk.RoutingEpoch < 1 || chunk.SnapshotSequence < 1 || chunk.ObservedAt.IsZero() ||
		chunk.Scope != ScopeAll || chunk.ChunkCount < 1 || chunk.ChunkCount > MaxChunkCount ||
		chunk.ChunkIndex < 0 || chunk.ChunkIndex >= chunk.ChunkCount || chunk.Entries == nil || len(chunk.Entries) > MaxChunkEntries {
		return ErrInvalid
	}
	_, offset := chunk.ObservedAt.Zone()
	if offset != 0 {
		return ErrInvalid
	}
	if chunk.ChunkCount > 1 && len(chunk.Entries) == 0 {
		return ErrInvalid
	}
	for index, entry := range chunk.Entries {
		if !infoHashPattern.MatchString(entry.InfoHashV1) || entry.Seeders < 0 || entry.Leechers < 0 {
			return ErrInvalid
		}
		if index > 0 && !slices.IsSorted([]string{chunk.Entries[index-1].InfoHashV1, entry.InfoHashV1}) {
			return ErrInvalid
		}
		if index > 0 && chunk.Entries[index-1].InfoHashV1 == entry.InfoHashV1 {
			return ErrInvalid
		}
	}
	return nil
}

func Encode(chunk SnapshotChunk) ([]byte, error) {
	if Validate(chunk) != nil {
		return nil, ErrInvalid
	}
	encoded, err := json.Marshal(chunk)
	if err != nil || len(encoded) < 2 || len(encoded) > MaxEventBytes {
		return nil, ErrInvalid
	}
	return encoded, nil
}

func Decode(encoded []byte) (SnapshotChunk, error) {
	if len(encoded) < 2 || len(encoded) > MaxEventBytes {
		return SnapshotChunk{}, ErrInvalid
	}
	var chunk SnapshotChunk
	if err := signedsnapshotv1.StrictJSON(encoded, &chunk); err != nil || Validate(chunk) != nil {
		return SnapshotChunk{}, ErrInvalid
	}
	canonical, err := json.Marshal(chunk)
	if err != nil || !bytes.Equal(canonical, encoded) {
		return SnapshotChunk{}, ErrInvalid
	}
	return chunk, nil
}

func ValidStreamName(value string) bool { return jetstreamv1.ValidStreamName(value) }

func ValidLiteralSubject(value string) bool { return jetstreamv1.ValidLiteralSubject(value) }

func ValidSourceID(value string) bool { return sourceIDPattern.MatchString(value) }
