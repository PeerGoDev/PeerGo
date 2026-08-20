// Package swarmsnapshot owns the Tracker-side construction and reliable
// publication of chunked full Swarm Engine snapshots. It never reads Core or a
// database and it never places peer identity or endpoint data in a snapshot.
package swarmsnapshot

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"slices"
	"time"

	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
	"github.com/peergo/peergo/services/tracker/internal/swarm"
	"github.com/peergo/peergo/services/tracker/internal/uuidv7"
)

var ErrConfig = errors.New("Tracker swarm snapshot publisher configuration is invalid")

type EncodedChunk struct {
	Event   trackerswarmv1.SnapshotChunk
	Payload []byte
}

type Factory struct {
	sourceID        string
	routingEpoch    int64
	maxChunkEntries int
	random          io.Reader
}

func NewFactory(sourceID string, routingEpoch int64, maxChunkEntries int, random io.Reader) (*Factory, error) {
	if !trackerswarmv1.ValidSourceID(sourceID) || routingEpoch < 1 || maxChunkEntries < 1 ||
		maxChunkEntries > trackerswarmv1.MaxChunkEntries {
		return nil, ErrConfig
	}
	if random == nil {
		random = rand.Reader
	}
	return &Factory{sourceID: sourceID, routingEpoch: routingEpoch, maxChunkEntries: maxChunkEntries, random: random}, nil
}

func (factory *Factory) Build(sequence int64, observedAt time.Time, source []swarm.SnapshotEntry) ([]EncodedChunk, error) {
	if factory == nil || factory.random == nil || sequence < 1 || observedAt.IsZero() {
		return nil, ErrConfig
	}
	// Snapshot consumers persist this value in PostgreSQL timestamptz columns,
	// whose precision is microseconds. Canonicalize before encoding so replayed
	// events compare identically after a database round trip.
	observedAt = observedAt.UTC().Truncate(time.Microsecond)
	entries := slices.Clone(source)
	slices.SortFunc(entries, func(left, right swarm.SnapshotEntry) int {
		return slices.Compare(left.InfoHash[:], right.InfoHash[:])
	})
	for index, entry := range entries {
		if entry.Seeders < 0 || entry.Seeders > math.MaxInt32 || entry.Leechers < 0 || entry.Leechers > math.MaxInt32 ||
			(index > 0 && entry.InfoHash == entries[index-1].InfoHash) {
			return nil, ErrConfig
		}
	}
	chunkCount := max(1, (len(entries)+factory.maxChunkEntries-1)/factory.maxChunkEntries)
	if chunkCount > trackerswarmv1.MaxChunkCount {
		return nil, ErrConfig
	}
	snapshotID, err := uuidv7.New(observedAt, factory.random)
	if err != nil {
		return nil, err
	}
	result := make([]EncodedChunk, 0, chunkCount)
	for chunkIndex := 0; chunkIndex < chunkCount; chunkIndex++ {
		start := min(chunkIndex*factory.maxChunkEntries, len(entries))
		end := min(start+factory.maxChunkEntries, len(entries))
		items := make([]trackerswarmv1.Entry, 0, end-start)
		for _, entry := range entries[start:end] {
			items = append(items, trackerswarmv1.Entry{
				InfoHashV1: hex.EncodeToString(entry.InfoHash[:]),
				Seeders:    int32(entry.Seeders), Leechers: int32(entry.Leechers),
			})
		}
		eventID, err := uuidv7.New(observedAt, factory.random)
		if err != nil {
			return nil, err
		}
		event := trackerswarmv1.SnapshotChunk{
			SchemaVersion: trackerswarmv1.SchemaVersion, EventID: eventID, SnapshotID: snapshotID,
			SourceID: factory.sourceID, RoutingEpoch: factory.routingEpoch, SnapshotSequence: sequence,
			ObservedAt: observedAt, Scope: trackerswarmv1.ScopeAll,
			ChunkIndex: int32(chunkIndex), ChunkCount: int32(chunkCount), Entries: items,
		}
		payload, err := trackerswarmv1.Encode(event)
		if err != nil {
			return nil, err
		}
		result = append(result, EncodedChunk{Event: event, Payload: payload})
	}
	return result, nil
}
