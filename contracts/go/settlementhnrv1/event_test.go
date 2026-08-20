package settlementhnrv1

import (
	"testing"
	"time"
)

func TestEventCanonicalRoundTrip(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	by := SatisfiedBySeedTime
	event := Event{
		SchemaVersion: SchemaVersion, EventID: "0198f20a-6da8-7e51-9c64-111111111111",
		OccurredAt: now.Add(8 * 24 * time.Hour), ObligationID: "0198f20a-6da8-4e51-9c64-222222222222", ObligationVersion: 2,
		UserID: "0198f20a-6da8-4e51-9c64-333333333333", TorrentID: 42, CompletedAt: now,
		State: StateSatisfied, SeededSeconds: 604800, RequiredSeedSeconds: 604800,
		RawUploaded: 200, RawDownloaded: 100, RawRatioBasisPoints: 20000, RequiredRatioBPS: 10000,
		AssessmentDueAt: now.Add(10 * 24 * time.Hour), GraceEndsAt: now.Add(13 * 24 * time.Hour),
		SatisfiedBy: &by, SatisfiedAt: timePointer(now.Add(8 * 24 * time.Hour)),
	}
	encoded, err := Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil || decoded.ObligationID != event.ObligationID || decoded.State != StateSatisfied {
		t.Fatalf("decoded=%+v error=%v", decoded, err)
	}
}

func TestTrackingCannotCarrySatisfaction(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	by := SatisfiedBySeedTime
	event := Event{
		SchemaVersion: SchemaVersion, EventID: "0198f20a-6da8-7e51-9c64-111111111111",
		OccurredAt: now, ObligationID: "0198f20a-6da8-4e51-9c64-222222222222", ObligationVersion: 1,
		UserID: "0198f20a-6da8-4e51-9c64-333333333333", TorrentID: 42, CompletedAt: now,
		State: StateTracking, RequiredSeedSeconds: 1, AssessmentDueAt: now.Add(time.Hour), GraceEndsAt: now.Add(2 * time.Hour),
		SatisfiedBy: &by,
	}
	if Validate(event) == nil {
		t.Fatal("tracking event with satisfaction evidence was accepted")
	}
}

func TestEventRejectsSubMicrosecondTimestamp(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 10, 12, 0, 0, 1, time.UTC)
	event := Event{
		SchemaVersion: SchemaVersion, EventID: "0198f20a-6da8-7e51-9c64-111111111111",
		OccurredAt: now, ObligationID: "0198f20a-6da8-4e51-9c64-222222222222", ObligationVersion: 1,
		UserID: "0198f20a-6da8-4e51-9c64-333333333333", TorrentID: 42, CompletedAt: now,
		State: StateTracking, RequiredSeedSeconds: 1, AssessmentDueAt: now.Add(time.Hour), GraceEndsAt: now.Add(2 * time.Hour),
	}
	if Validate(event) == nil {
		t.Fatal("event with sub-microsecond timestamp was accepted")
	}
}

func timePointer(value time.Time) *time.Time { return &value }
