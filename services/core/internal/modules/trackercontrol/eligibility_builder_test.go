package trackercontrol

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/trackerevent"
	"github.com/peergo/peergo/services/core/internal/modules/review"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

func TestReviewEligibilityKeepsPendingPreseedOnApprovalAndDisablesItOnRejection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	pending := review.PendingTorrent{
		ID: 42, InfoHashV1: torrents.InfoHashV1{1}, TotalSizeBytes: 4096,
	}
	builder := NewEligibilityEventBuilder(func() uuid.UUID {
		return uuid.MustParse("0198f20a-6da8-7e51-9c64-999999999999")
	})

	tests := []struct {
		name     string
		decision review.Decision
		state    torrents.State
		enabled  bool
	}{
		{name: "approval remains eligible", decision: review.DecisionApprove, state: torrents.StatePublished, enabled: true},
		{name: "rejection removes preseed", decision: review.DecisionReject, state: torrents.StateRejected, enabled: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event, err := builder.BuildTorrentEligibilityEvent(review.DecisionResult{
				TorrentID: pending.ID, Decision: test.decision, State: test.state,
				Version: 2, OccurredAt: now,
			}, pending)
			if err != nil {
				t.Fatal(err)
			}
			payload, err := trackerevent.DecodeTorrentEligibilityChanged(event)
			if err != nil || payload.Enabled != test.enabled || payload.TorrentVersion != 2 || payload.TorrentID != 42 {
				t.Fatalf("eligibility payload = %+v, %v", payload, err)
			}
		})
	}
}
