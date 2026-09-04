package trackercontrol

import (
	"errors"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/trackerevent"
	"github.com/peergo/peergo/services/core/internal/modules/review"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

type EligibilityEventBuilder struct {
	newEventID func() uuid.UUID
}

func NewEligibilityEventBuilder(newEventID func() uuid.UUID) *EligibilityEventBuilder {
	if newEventID == nil {
		newEventID = uuid.New
	}
	return &EligibilityEventBuilder{newEventID: newEventID}
}

func (builder *EligibilityEventBuilder) BuildTorrentEligibilityEvent(result review.DecisionResult, pending review.PendingTorrent) (trackerevent.Event, error) {
	if result.TorrentID < 1 || result.TorrentID != pending.ID || result.Version < 2 {
		return trackerevent.Event{}, errors.New("reviewed torrent eligibility event has invalid metadata")
	}
	enabled := false
	switch result.Decision {
	case review.DecisionApprove:
		if result.State != torrents.StatePublished {
			return trackerevent.Event{}, errors.New("approved torrent eligibility event has invalid state")
		}
		enabled = true
	case review.DecisionReject:
		if result.State != torrents.StateRejected {
			return trackerevent.Event{}, errors.New("rejected torrent eligibility event has invalid state")
		}
	default:
		return trackerevent.Event{}, errors.New("reviewed torrent eligibility event has invalid decision")
	}
	return trackerevent.NewTorrentEligibilityChanged(trackerevent.TorrentEligibilityInput{
		EventID: builder.newEventID(), OccurredAt: result.OccurredAt,
		TorrentID:  int64(result.TorrentID),
		InfoHashV1: pending.InfoHashV1, TotalSizeBytes: pending.TotalSizeBytes,
		Enabled: enabled, TorrentVersion: result.Version,
	})
}

// BuildTorrentLifecycleEligibilityEvent emits both disable and restore
// transitions. The same canonical event advances the allowlist projection, so
// an operational action cannot bypass the review publication control plane.
func (builder *EligibilityEventBuilder) BuildTorrentLifecycleEligibilityEvent(input torrents.TorrentLifecycleEligibilityInput) (trackerevent.Event, error) {
	result := input.Result
	if result.TorrentID < 1 || result.Version < 2 || input.InfoHashV1 == (torrents.InfoHashV1{}) || input.TotalSizeBytes < 1 {
		return trackerevent.Event{}, errors.New("torrent lifecycle eligibility event has invalid metadata")
	}
	enabled := false
	switch result.Action {
	case torrents.TorrentAvailabilityDisable:
		if result.State != torrents.StateDisabled {
			return trackerevent.Event{}, errors.New("disable eligibility event has invalid state")
		}
	case torrents.TorrentAvailabilityRestore:
		if result.State != torrents.StatePublished {
			return trackerevent.Event{}, errors.New("restore eligibility event has invalid state")
		}
		enabled = true
	case torrents.TorrentAvailabilityWithdrawRequest:
		if result.State != torrents.StateDisabled {
			return trackerevent.Event{}, errors.New("withdrawal request eligibility event has invalid state")
		}
	case torrents.TorrentAvailabilityWithdrawApprove:
		if result.State != torrents.StateDeleted {
			return trackerevent.Event{}, errors.New("withdrawal approval eligibility event has invalid state")
		}
	case torrents.TorrentAvailabilityWithdrawReject:
		if result.State != torrents.StatePublished {
			return trackerevent.Event{}, errors.New("withdrawal rejection eligibility event has invalid state")
		}
		enabled = true
	case torrents.TorrentAvailabilityReportDisable:
		if result.State != torrents.StateDisabled {
			return trackerevent.Event{}, errors.New("report disable eligibility event has invalid state")
		}
	default:
		return trackerevent.Event{}, errors.New("torrent lifecycle eligibility event has invalid action")
	}
	return trackerevent.NewTorrentEligibilityChanged(trackerevent.TorrentEligibilityInput{
		EventID: builder.newEventID(), OccurredAt: result.ChangedAt, TorrentID: int64(result.TorrentID),
		InfoHashV1: input.InfoHashV1, TotalSizeBytes: input.TotalSizeBytes,
		Enabled: enabled, TorrentVersion: result.Version,
	})
}

var _ review.EligibilityEventBuilder = (*EligibilityEventBuilder)(nil)
var _ torrents.TorrentLifecycleEligibilityEventBuilder = (*EligibilityEventBuilder)(nil)
