package swarmsnapshot

import (
	"bytes"
	"testing"
	"time"

	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
	"github.com/peergo/peergo/services/tracker/internal/swarm"
)

func TestFactoryBuildsSortedCompleteChunks(t *testing.T) {
	t.Parallel()
	factory, err := NewFactory("tracker-primary", 3, 2, bytes.NewReader(bytes.Repeat([]byte{0x91}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	observedAt := time.Date(2026, 8, 9, 10, 0, 0, 123_456_789, time.FixedZone("UTC+8", 8*60*60))
	wantObservedAt := observedAt.UTC().Truncate(time.Microsecond)
	chunks, err := factory.Build(8, observedAt, []swarm.SnapshotEntry{
		{InfoHash: [20]byte{3}, Seeders: 3, Leechers: 1},
		{InfoHash: [20]byte{1}, Seeders: 1, Leechers: 2},
		{InfoHash: [20]byte{2}, Seeders: 2, Leechers: 3},
	})
	if err != nil || len(chunks) != 2 {
		t.Fatalf("chunks=%+v error=%v", chunks, err)
	}
	if chunks[0].Event.ChunkCount != 2 || chunks[1].Event.ChunkIndex != 1 ||
		chunks[0].Event.SnapshotID != chunks[1].Event.SnapshotID ||
		chunks[0].Event.Entries[0].InfoHashV1[:2] != "01" || chunks[1].Event.Entries[0].InfoHashV1[:2] != "03" {
		t.Fatalf("unexpected chunk metadata: %+v", chunks)
	}
	for _, chunk := range chunks {
		decoded, decodeErr := trackerswarmv1.Decode(chunk.Payload)
		if decodeErr != nil || decoded.EventID != chunk.Event.EventID ||
			!chunk.Event.ObservedAt.Equal(wantObservedAt) || !decoded.ObservedAt.Equal(wantObservedAt) ||
			chunk.Event.ObservedAt.Nanosecond()%1_000 != 0 {
			t.Fatalf("decode=%+v error=%v", decoded, decodeErr)
		}
	}
}

func TestFactoryRepresentsEmptyEngineWithOneCompleteChunk(t *testing.T) {
	t.Parallel()
	factory, err := NewFactory("tracker-primary", 1, 10, bytes.NewReader(bytes.Repeat([]byte{0x71}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	chunks, err := factory.Build(1, time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC), nil)
	if err != nil || len(chunks) != 1 || chunks[0].Event.ChunkCount != 1 || len(chunks[0].Event.Entries) != 0 {
		t.Fatalf("chunks=%+v error=%v", chunks, err)
	}
}
