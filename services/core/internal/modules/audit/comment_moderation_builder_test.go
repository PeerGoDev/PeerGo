package audit

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/social"
)

func TestCommentModerationDecisionEventHashesTextAndExcludesReporterIdentity(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 9, 21, 0, 0, 0, time.UTC)
	eventID := uuid.New()
	builder, err := NewCommentModerationDecisionEventBuilder(RecorderConfig{
		PseudonymKey: []byte("comment-moderation-audit-key-v1!!"), PseudonymKeyEpoch: "test-v1",
		NewEventID: func() uuid.UUID { return eventID },
	})
	if err != nil {
		t.Fatal(err)
	}
	input := validCommentModerationAuditInput(now)
	event, err := builder.BuildCommentModerationDecisionEvent(input)
	if err != nil {
		t.Fatalf("BuildCommentModerationDecisionEvent() error = %v", err)
	}
	var payload CommentModerationDecisionRecordedV4
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.EventID != eventID || payload.CaseID != input.Before.CaseID || payload.CommentID != input.Before.CommentID ||
		payload.TargetKind != string(social.CommentTargetTorrent) || payload.TorrentID == nil || *payload.TorrentID != input.Target.TorrentID || payload.AnnouncementID != "" ||
		payload.ReportCount != input.ReportCount || payload.NoteSHA256 == "" || payload.BeforeSHA256 == payload.AfterSHA256 ||
		payload.ModeratorPseudonym == "" || payload.CommentAuthorPseudonym == "" ||
		payload.ModeratorPseudonym == payload.CommentAuthorPseudonym {
		t.Fatalf("audit payload = %+v", payload)
	}
	if bytes.Contains(event.Payload, []byte(input.Note)) || bytes.Contains(event.Payload, []byte("reporter")) {
		t.Fatalf("audit payload leaked human text or reporter identity: %s", event.Payload)
	}
}

func TestCommentModerationDecisionEventUsesTypedAnnouncementTarget(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 10, 13, 0, 0, 0, time.UTC)
	builder, err := NewCommentModerationDecisionEventBuilder(RecorderConfig{
		PseudonymKey: []byte("comment-moderation-audit-key-v1!!"), PseudonymKeyEpoch: "test-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	input := validCommentModerationAuditInput(now)
	input.Target = social.AnnouncementCommentTarget("welcome-to-peergo")
	event, err := builder.BuildCommentModerationDecisionEvent(input)
	if err != nil {
		t.Fatalf("BuildCommentModerationDecisionEvent() error = %v", err)
	}
	var payload CommentModerationDecisionRecordedV4
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.TargetKind != string(social.CommentTargetAnnouncement) || payload.AnnouncementID != "welcome-to-peergo" || payload.TorrentID != nil {
		t.Fatalf("audit target = %+v", payload)
	}
}

func TestCommentModerationDecisionEventRejectsUnboundedTransition(t *testing.T) {
	t.Parallel()
	builder, err := NewCommentModerationDecisionEventBuilder(RecorderConfig{
		PseudonymKey: []byte("comment-moderation-audit-key-v1!!"), PseudonymKeyEpoch: "test-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	input := validCommentModerationAuditInput(time.Now().UTC())
	input.ReasonCode = social.CommentModerationReasonCode("account_ban")
	if _, err := builder.BuildCommentModerationDecisionEvent(input); err == nil {
		t.Fatal("BuildCommentModerationDecisionEvent() accepted an unbounded reason")
	}
}

func validCommentModerationAuditInput(now time.Time) social.CommentModerationAuditInput {
	caseID, commentID := uuid.New(), uuid.New()
	return social.CommentModerationAuditInput{
		DecisionID: uuid.New(), ModeratorID: uuid.New(), CommentAuthorID: uuid.New(), Target: social.TorrentCommentTarget(44),
		Decision: social.CommentModerationHideComment, ReasonCode: social.CommentModerationSpam,
		Note: "已核对评论上下文，确认需要隐藏正文。", ReportCount: 3, OccurredAt: now,
		Authorization: authz.Decision{
			ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
			GrantID: uuid.New(), GrantVersion: 2, RoleID: "community_moderator", MandateID: uuid.New(),
			EffectiveUntil: now.Add(time.Hour),
		},
		Before: social.CommentModerationAuditState{
			CaseID: caseID, CaseState: social.CommentModerationCaseOpen, CaseVersion: 4,
			CommentID: commentID, CommentState: social.CommentVisible, CommentVersion: 2,
		},
		After: social.CommentModerationAuditState{
			CaseID: caseID, CaseState: social.CommentModerationCaseCommentHidden, CaseVersion: 5,
			CommentID: commentID, CommentState: social.CommentModeratorHidden, CommentVersion: 3,
		},
	}
}
