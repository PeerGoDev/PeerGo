package settlementseedingv1

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	t.Parallel()
	event := validEvent()
	encoded, err := Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil || !reflect.DeepEqual(decoded, event) {
		t.Fatalf("Decode() = %+v, %v", decoded, err)
	}
}

func TestProjectionDigestIsChunkIndependent(t *testing.T) {
	t.Parallel()
	header := validEvent()
	header.ItemCount = 2
	items := []Item{header.Items[0], {
		UserID: "0198f20a-6da8-4e51-9c64-222222222222", TorrentID: 9,
		ActiveSeconds: 1800, RawUploadedBytes: 7, SnapshotSeeders: 2, SnapshotLeechers: 1,
		EvidenceSHA256: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
	}}
	first, err := ProjectionDigest(header, items)
	if err != nil {
		t.Fatal(err)
	}
	header.ChunkIndex = 7
	header.ChunkCount = 9
	header.EventID = "a6e39703-2678-55ac-89d1-333333333333"
	header.Items = items[1:]
	second, err := ProjectionDigest(header, items)
	if err != nil || first != second {
		t.Fatalf("ProjectionDigest() second=%x error=%v want=%x", second, err, first)
	}
}

func TestDecodeRejectsUnknownAndNonCanonicalJSON(t *testing.T) {
	t.Parallel()
	encoded, err := Encode(validEvent())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(append([]byte(" "), encoded...)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Decode(noncanonical) error=%v", err)
	}
	if _, err := Decode(append(encoded[:len(encoded)-1], []byte(`,"extra":true}`)...)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Decode(unknown) error=%v", err)
	}
}

func TestValidateRejectsPrivacyAndAssemblyDrift(t *testing.T) {
	t.Parallel()
	tests := []func(*Event){
		func(event *Event) { event.WindowStart = event.WindowStart.Add(time.Minute) },
		func(event *Event) { event.ChunkCount = 2; event.Items = nil },
		func(event *Event) { event.Items[0].UserID = "not-a-user" },
		func(event *Event) { event.Items[0].ActiveSeconds = 3601 },
		func(event *Event) { event.ItemCount = 0 },
	}
	for index, mutate := range tests {
		event := validEvent()
		mutate(&event)
		if err := Validate(event); !errors.Is(err, ErrInvalid) {
			t.Fatalf("case %d error=%v", index, err)
		}
	}
}

func TestEmptyWindowIsOneExplicitChunk(t *testing.T) {
	t.Parallel()
	event := validEvent()
	event.ItemCount = 0
	event.Items = []Item{}
	if err := Validate(event); err != nil {
		t.Fatal(err)
	}
	event.ChunkCount = 2
	if err := Validate(event); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Validate(two empty chunks) error=%v", err)
	}
}

func validEvent() Event {
	start := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	return Event{
		SchemaVersion: SchemaVersion, EventID: "a6e39703-2678-55ac-89d1-111111111111",
		WindowStart: start, WindowEnd: start.Add(time.Hour), BuiltAt: start.Add(time.Hour + time.Minute),
		WindowEvidenceSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ProjectionSHA256:     "123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0",
		SnapshotID:           "0198f20a-6da8-7e51-9c64-111111111111", SnapshotSequence: 7,
		SnapshotObservedAt: start.Add(-time.Minute), ItemCount: 1, ChunkIndex: 0, ChunkCount: 1,
		Items: []Item{{
			UserID: "0198f20a-6da8-4e51-9c64-111111111111", TorrentID: 7,
			ActiveSeconds: 3600, RawUploadedBytes: 42, SnapshotSeeders: 3, SnapshotLeechers: 1,
			EvidenceSHA256: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		}},
	}
}
