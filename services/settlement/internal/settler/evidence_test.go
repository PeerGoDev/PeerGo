package settler

import (
	"testing"
	"time"

	"github.com/peergo/peergo/contracts/go/settlementtrafficv1"
)

func TestPublicExplanationProjectsOnlyFinalAccountingValues(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.August, 9, 0, 0, 0, 0, time.UTC)
	explanation, err := publicExplanation([]finalizedSegment{{
		StartsAt: start, EndsAt: start.Add(time.Minute),
		RawUploaded: 100, RawDownloaded: 80, CreditedUploaded: 200, ChargedDownloaded: 0,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if explanation.Status != settlementtrafficv1.ExplanationComplete || explanation.SegmentCount != 1 ||
		len(explanation.Segments) != 1 || explanation.Segments[0].CreditedUploaded != 200 {
		t.Fatalf("publicExplanation() = %+v", explanation)
	}
}

func TestPublicExplanationOmitsExcessiveFragmentationWithoutFailingSettlement(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.August, 9, 0, 0, 0, 0, time.UTC)
	segments := make([]finalizedSegment, settlementtrafficv1.MaxExplanationSegments+1)
	for index := range segments {
		segments[index] = finalizedSegment{
			StartsAt: start.Add(time.Duration(index) * time.Minute),
			EndsAt:   start.Add(time.Duration(index+1) * time.Minute),
		}
	}
	explanation, err := publicExplanation(segments)
	if err != nil {
		t.Fatal(err)
	}
	if explanation.Status != settlementtrafficv1.ExplanationOmitted ||
		explanation.SegmentCount != settlementtrafficv1.MaxExplanationSegments+1 || len(explanation.Segments) != 0 {
		t.Fatalf("publicExplanation() = %+v", explanation)
	}
}
