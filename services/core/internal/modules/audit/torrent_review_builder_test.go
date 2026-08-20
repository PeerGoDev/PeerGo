package audit

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/review"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

func TestTorrentReviewEventHashesHumanTextAndPseudonymizesPeople(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 8, 17, 0, 0, 0, time.UTC)
	eventID := uuid.New()
	builder, err := NewTorrentReviewEventBuilder(RecorderConfig{
		PseudonymKey: []byte("torrent-review-audit-key-32-bytes!!"), PseudonymKeyEpoch: "test-v1",
		NewEventID: func() uuid.UUID { return eventID },
	})
	if err != nil {
		t.Fatal(err)
	}
	reason := "已核对文件树、分类和发布规范，同意本次发布。"
	torrentID := torrents.TorrentID(44)
	event, err := builder.BuildTorrentReviewEvent(review.TorrentReviewAuditInput{
		DecisionID: uuid.New(), ReviewerID: uuid.New(), UploaderID: uuid.New(),
		Decision: review.DecisionApprove, ReasonCode: review.ReasonMeetsRequirements,
		Reason: reason, OccurredAt: now, Authorization: authz.Decision{
			ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
			GrantID: uuid.New(), GrantVersion: 1, RoleID: "torrent_reviewer", MandateID: uuid.New(),
			EffectiveUntil: now.Add(time.Hour),
		},
		Before: review.TorrentReviewAuditState{TorrentID: torrentID, State: torrents.StatePendingReview, Version: 1},
		After:  review.TorrentReviewAuditState{TorrentID: torrentID, State: torrents.StatePublished, Version: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if event.ID != eventID || bytes.Contains(event.Payload, []byte(reason)) {
		t.Fatalf("event leaked reason or used wrong id: %s", event.Payload)
	}
	var payload TorrentReviewRecordedV2
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.TorrentID != int64(torrentID) || payload.ReasonSHA256 == "" || payload.ReviewerPseudonym == "" ||
		payload.UploaderPseudonym == "" || payload.BeforeSHA256 == payload.AfterSHA256 {
		t.Fatalf("payload = %+v", payload)
	}
}
