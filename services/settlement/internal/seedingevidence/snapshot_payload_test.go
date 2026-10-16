package seedingevidence

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
)

func TestSnapshotPayloadRejectsCorruptionAndDecompressionBomb(t *testing.T) {
	payload := []byte("{}")
	compressed, err := compressSnapshotPayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	if _, err = decodeSnapshotPayload(compressed, len(payload), digest[:]); err == nil {
		t.Fatal("non-contract JSON accepted")
	}
	corrupted := append([]byte(nil), compressed...)
	corrupted[len(corrupted)/2] ^= 1
	if _, err = decodeSnapshotPayload(corrupted, len(payload), digest[:]); err == nil {
		t.Fatal("corruption accepted")
	}
	var bomb bytes.Buffer
	writer := gzip.NewWriter(&bomb)
	_, _ = writer.Write(bytes.Repeat([]byte("x"), trackerswarmv1.MaxEventBytes+1))
	_ = writer.Close()
	if _, err = decodeSnapshotPayload(bomb.Bytes(), 2, digest[:]); err == nil {
		t.Fatal("oversized decompressed payload accepted")
	}
}

func TestSnapshotPayloadRoundTripsCanonicalFullFrame(t *testing.T) {
	frame := trackerswarmv1.SnapshotChunk{SchemaVersion: trackerswarmv1.SchemaVersion, EventID: uuid.Must(uuid.NewV7()).String(),
		SnapshotID: uuid.Must(uuid.NewV7()).String(), SourceID: "payload-test", RoutingEpoch: 1, SnapshotSequence: 1,
		ObservedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Scope: trackerswarmv1.ScopeAll, ChunkCount: 1,
		Entries: make([]trackerswarmv1.Entry, 1000)}
	for i := range frame.Entries {
		frame.Entries[i] = trackerswarmv1.Entry{InfoHashV1: fmt.Sprintf("%040x", i), Seeders: int32(i % 10), Leechers: int32(i % 3)}
	}
	payload, err := trackerswarmv1.Encode(frame)
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := compressSnapshotPayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	restored, err := decodeSnapshotPayload(compressed, len(payload), digest[:])
	if err != nil || !reflect.DeepEqual(restored, frame) {
		t.Fatalf("canonical frame changed: %v", err)
	}
	if len(compressed) >= len(payload)/2 {
		t.Fatalf("fixture did not meaningfully compress: %d -> %d", len(payload), len(compressed))
	}
	t.Logf("canonical 1000-entry fixture bytes: %d -> %d", len(payload), len(compressed))
}
