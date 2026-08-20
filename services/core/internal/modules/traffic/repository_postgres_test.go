package traffic

import (
	"errors"
	"testing"
	"time"
)

func TestClassifyDatabaseErrorKeepsTransportFailuresRetryable(t *testing.T) {
	t.Parallel()
	err := classifyDatabaseError("apply", errors.New("connection reset"))
	if errors.Is(err, ErrInvariant) {
		t.Fatalf("classifyDatabaseError() made transport failure permanent: %v", err)
	}
}

func TestNewPostgresRepositoryRejectsNilPool(t *testing.T) {
	t.Parallel()
	if _, err := NewPostgresRepository(nil, nil); !errors.Is(err, ErrInput) {
		t.Fatalf("NewPostgresRepository() error = %v", err)
	}
}

func TestValidateExplanationProjectionReconcilesEverySegment(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.August, 9, 0, 0, 0, 0, time.UTC)
	entry := Entry{
		IntervalStartedAt: start, IntervalEndedAt: start.Add(20 * time.Minute),
		RawUploaded: 300, RawDownloaded: 300, CreditedUploaded: 500, ChargedDownloaded: 50,
		Explanation: Explanation{
			Status: ExplanationComplete, SegmentCount: 2,
			Segments: []ExplanationSegment{
				{Index: 0, StartsAt: start, EndsAt: start.Add(10 * time.Minute), RawUploaded: 100, RawDownloaded: 200, CreditedUploaded: 200},
				{Index: 1, StartsAt: start.Add(10 * time.Minute), EndsAt: start.Add(20 * time.Minute), RawUploaded: 200, RawDownloaded: 100, CreditedUploaded: 300, ChargedDownloaded: 50},
			},
		},
	}
	if err := validateExplanationProjection(entry); err != nil {
		t.Fatalf("validateExplanationProjection() error = %v", err)
	}
	entry.Explanation.Segments[1].StartsAt = start.Add(11 * time.Minute)
	if err := validateExplanationProjection(entry); !errors.Is(err, ErrInvariant) {
		t.Fatalf("validateExplanationProjection(gap) error = %v", err)
	}
}

func TestValidateExplanationProjectionAcceptsRetainedLegacyEntry(t *testing.T) {
	t.Parallel()
	entry := Entry{Explanation: Explanation{Status: ExplanationNotAvailable, Segments: []ExplanationSegment{}}}
	if err := validateExplanationProjection(entry); err != nil {
		t.Fatalf("validateExplanationProjection() error = %v", err)
	}
}
