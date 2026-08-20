package trackercontrol

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

func TestEligibilityBuilderAcceptsWithdrawalTransitions(t *testing.T) {
	t.Parallel()
	var infoHash torrents.InfoHashV1
	copy(infoHash[:], []byte("01234567890123456789"))
	now := time.Date(2026, time.August, 18, 8, 0, 0, 0, time.UTC)
	builder := NewEligibilityEventBuilder(uuid.New)
	tests := []struct {
		name   string
		action torrents.TorrentAvailabilityAction
		state  torrents.State
	}{
		{name: "request", action: torrents.TorrentAvailabilityWithdrawRequest, state: torrents.StateDisabled},
		{name: "approve", action: torrents.TorrentAvailabilityWithdrawApprove, state: torrents.StateDeleted},
		{name: "reject", action: torrents.TorrentAvailabilityWithdrawReject, state: torrents.StatePublished},
		{name: "report disable", action: torrents.TorrentAvailabilityReportDisable, state: torrents.StateDisabled},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := builder.BuildTorrentLifecycleEligibilityEvent(torrents.TorrentLifecycleEligibilityInput{
				Result: torrents.TorrentAvailabilityResult{
					ChangeID: uuid.New(), TorrentID: 42, Action: test.action,
					State: test.state, Version: 8, ChangedAt: now,
				},
				InfoHashV1: infoHash, TotalSizeBytes: 1024,
			})
			if err != nil {
				t.Fatalf("BuildTorrentLifecycleEligibilityEvent() error=%v", err)
			}
		})
	}
}
