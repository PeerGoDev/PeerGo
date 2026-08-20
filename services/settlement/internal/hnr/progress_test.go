package hnr

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/contracts/go/settlementhnrv1"
)

func TestEvaluateMergesConcurrentSeedersAndCapsGaps(t *testing.T) {
	t.Parallel()
	completedAt := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	obligation := testObligation(completedAt)
	obligation.RequiredSeedSeconds = 7_200
	intervals := []RawInterval{
		testInterval("0198f20a-6da8-4e51-9c64-111111111111", completedAt, completedAt.Add(2*time.Hour), 0, 100),
		testInterval("0198f20a-6da8-4e51-9c64-222222222222", completedAt.Add(30*time.Minute), completedAt.Add(90*time.Minute), 0, 200),
		testInterval("0198f20a-6da8-4e51-9c64-333333333333", completedAt.Add(3*time.Hour), completedAt.Add(10*time.Hour), 0, 300),
	}
	progress, err := Evaluate(obligation, intervals)
	if err != nil {
		t.Fatal(err)
	}
	// First two overlap into 90 minutes because each interval is capped at 90
	// minutes; the long gap contributes another 90 minutes, never seven hours.
	if progress.SeededSeconds != 3*60*60 || progress.RawUploaded != 610 || progress.State != settlementhnrv1.StateSatisfied {
		t.Fatalf("progress=%+v", progress)
	}
}

func TestEvaluateUsesRawRatioWithoutCreditedTraffic(t *testing.T) {
	t.Parallel()
	completedAt := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	obligation := testObligation(completedAt)
	obligation.RequiredSeedSeconds = 604800
	obligation.RequiredRatioBasisPoints = 10_000
	progress, err := Evaluate(obligation, []RawInterval{
		testInterval("0198f20a-6da8-4e51-9c64-111111111111", completedAt, completedAt.Add(time.Hour), 10, 1_040),
	})
	if err != nil {
		t.Fatal(err)
	}
	if progress.State != settlementhnrv1.StateSatisfied || progress.SatisfiedBy == nil ||
		*progress.SatisfiedBy != settlementhnrv1.SatisfiedByRawRatio || progress.RawRatioBasisPoints != 10_500 {
		t.Fatalf("progress=%+v", progress)
	}
}

func TestEvaluateCanMeetRawRatioAtCompletion(t *testing.T) {
	t.Parallel()
	completedAt := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	obligation := testObligation(completedAt)
	obligation.InitialUploaded = 1_000
	obligation.RequiredRatioBasisPoints = 10_000
	progress, err := Evaluate(obligation, nil)
	if err != nil || progress.State != settlementhnrv1.StateSatisfied || progress.SatisfiedAt == nil ||
		!progress.SatisfiedAt.Equal(completedAt) {
		t.Fatalf("progress=%+v error=%v", progress, err)
	}
}

func TestEvaluateDoesNotInventInfiniteRatioForZeroDownload(t *testing.T) {
	t.Parallel()
	completedAt := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	obligation := testObligation(completedAt)
	obligation.RawDownloaded = 0
	obligation.RequiredSeedSeconds = 604800
	progress, err := Evaluate(obligation, nil)
	if err != nil || progress.State != settlementhnrv1.StateTracking || progress.RawRatioBasisPoints != 0 {
		t.Fatalf("progress=%+v error=%v", progress, err)
	}
}

func testObligation(completedAt time.Time) Obligation {
	return Obligation{
		ID: uuid.MustParse("0198f20a-6da8-4e51-9c64-aaaaaaaaaaaa"), CompletedAt: completedAt,
		InitialUploaded: 10, RawDownloaded: 1_000, RequiredSeedSeconds: 3_600,
		RequiredRatioBasisPoints: 20_000, MaxIntervalCreditSeconds: 5_400,
		State: settlementhnrv1.StateTracking, LastEvidenceAt: completedAt,
	}
}

func testInterval(id string, startsAt, endsAt time.Time, left, uploaded int64) RawInterval {
	return RawInterval{
		EventID: uuid.MustParse(id), StartsAt: startsAt, EndsAt: endsAt,
		PreviousLeft: left, CurrentLeft: left, RawUploaded: uploaded,
	}
}
