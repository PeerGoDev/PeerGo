package trackerswarmv1

import (
	"testing"
	"time"
)

func TestSnapshotChunkCanonicalRoundTrip(t *testing.T) {
	t.Parallel()
	chunk := validChunk()
	encoded, err := Encode(chunk)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil || decoded.SnapshotID != chunk.SnapshotID || len(decoded.Entries) != 2 {
		t.Fatalf("decoded=%+v error=%v", decoded, err)
	}
}

func TestSnapshotChunkRejectsPartialOrAmbiguousShape(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*SnapshotChunk){
		"unordered":           func(chunk *SnapshotChunk) { chunk.Entries[0], chunk.Entries[1] = chunk.Entries[1], chunk.Entries[0] },
		"duplicate":           func(chunk *SnapshotChunk) { chunk.Entries[1].InfoHashV1 = chunk.Entries[0].InfoHashV1 },
		"empty middle chunk":  func(chunk *SnapshotChunk) { chunk.ChunkCount = 2; chunk.Entries = nil },
		"ambiguous null list": func(chunk *SnapshotChunk) { chunk.Entries = nil },
		"local time":          func(chunk *SnapshotChunk) { chunk.ObservedAt = chunk.ObservedAt.In(time.FixedZone("offset", 3600)) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			chunk := validChunk()
			mutate(&chunk)
			if _, err := Encode(chunk); err == nil {
				t.Fatal("invalid chunk encoded successfully")
			}
		})
	}
}

func TestSnapshotChunkAcceptsExplicitEmptyFullSnapshot(t *testing.T) {
	t.Parallel()
	chunk := validChunk()
	chunk.Entries = []Entry{}
	encoded, err := Encode(chunk)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil || decoded.Entries == nil || len(decoded.Entries) != 0 {
		t.Fatalf("decoded=%+v error=%v", decoded, err)
	}
}

func validChunk() SnapshotChunk {
	return SnapshotChunk{
		SchemaVersion: SchemaVersion,
		EventID:       "0198f20a-6da8-7e51-9c64-111111111111", SnapshotID: "0198f20a-6da8-7e51-9c64-222222222222",
		SourceID: "tracker-primary", RoutingEpoch: 1, SnapshotSequence: 7,
		ObservedAt: time.Date(2026, 8, 9, 9, 0, 0, 0, time.UTC), Scope: ScopeAll,
		ChunkIndex: 0, ChunkCount: 1,
		Entries: []Entry{
			{InfoHashV1: "00112233445566778899aabbccddeeff00112233", Seeders: 2, Leechers: 1},
			{InfoHashV1: "10112233445566778899aabbccddeeff00112233", Seeders: 4, Leechers: 3},
		},
	}
}
