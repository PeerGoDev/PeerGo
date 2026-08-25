package trackerevent

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTorrentEligibilityEventRoundTrip(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 8, 15, 0, 0, 0, time.UTC)
	var hash [20]byte
	copy(hash[:], []byte("12345678901234567890"))
	event, err := NewTorrentEligibilityChanged(TorrentEligibilityInput{
		EventID: uuid.New(), OccurredAt: now, TorrentID: 42,
		InfoHashV1: hash, TotalSizeBytes: 1234, Enabled: true, TorrentVersion: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := DecodeTorrentEligibilityChanged(event)
	if err != nil {
		t.Fatal(err)
	}
	if payload.TorrentID != 42 || payload.TorrentVersion != 2 || !payload.Enabled || payload.InfoHashV1 != "3132333435363738393031323334353637383930" {
		t.Fatalf("decoded payload = %+v", payload)
	}
}

func TestTorrentEligibilityEventRejectsTampering(t *testing.T) {
	t.Parallel()
	var hash [20]byte
	hash[0] = 1
	event, err := NewTorrentEligibilityChanged(TorrentEligibilityInput{
		EventID: uuid.New(), OccurredAt: time.Now(), TorrentID: 1,
		InfoHashV1: hash, TotalSizeBytes: 1, Enabled: true, TorrentVersion: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	event.Payload[0] = '['
	if err := Validate(event); err == nil {
		t.Fatal("Validate() accepted tampered payload")
	}
	if _, err := DecodeTorrentEligibilityChanged(event); err == nil {
		t.Fatalf("DecodeTorrentEligibilityChanged() error = %v", err)
	}
}

func TestTorrentEligibilityEventAcceptsPostgresTimestampPrecision(t *testing.T) {
	t.Parallel()
	var hash [20]byte
	hash[0] = 1
	event, err := NewTorrentEligibilityChanged(TorrentEligibilityInput{
		EventID:    uuid.New(),
		OccurredAt: time.Date(2026, time.August, 25, 10, 45, 29, 827722974, time.UTC),
		TorrentID:  2017, InfoHashV1: hash, TotalSizeBytes: 1, Enabled: true, TorrentVersion: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	event.OccurredAt = event.OccurredAt.Truncate(time.Microsecond)
	if err := Validate(event); err != nil {
		t.Fatalf("Validate() rejected PostgreSQL timestamp precision: %v", err)
	}
	event.OccurredAt = event.OccurredAt.Add(time.Microsecond)
	if err := Validate(event); err == nil {
		t.Fatal("Validate() accepted a timestamp from a different microsecond")
	}
}
