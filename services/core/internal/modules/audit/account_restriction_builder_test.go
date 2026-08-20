package audit

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

func TestAccountRestrictionBuilderHashesHumanReasonsAndPseudonymizesUsers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 6, 11, 0, 0, 0, time.UTC)
	eventID := uuid.MustParse("0198f20a-6da8-7e51-9c64-212121212121")
	restrictionID := uuid.MustParse("0198f20a-6da8-7e51-9c64-222222222221")
	actorID := uuid.MustParse("0198f20a-6da8-7e51-9c64-232323232323")
	targetID := uuid.MustParse("0198f20a-6da8-7e51-9c64-242424242424")
	builder, err := NewAccountRestrictionEventBuilder(RecorderConfig{
		PseudonymKey: []byte("0123456789abcdef0123456789abcdef"), PseudonymKeyEpoch: "test-2026-08",
		NewEventID: func() uuid.UUID { return eventID },
	})
	if err != nil {
		t.Fatalf("NewAccountRestrictionEventBuilder() error = %v", err)
	}
	createReason := "发现异常登录行为，临时限制账户并安排人工复核。"
	after := identity.AccountRestrictionAuditState{
		RestrictionID: restrictionID, Kind: string(identity.AccountRestrictionAccountAccess),
		ReasonCode: string(identity.AccountRestrictionReasonSecurityIncident), ReasonSummary: createReason,
		StartsAt: now, ExpiresAt: now.Add(24 * time.Hour), RestrictionVersion: 1,
		UserAdministrationVersion: 4,
	}
	event, err := builder.BuildAccountRestrictionEvent(identity.AccountRestrictionAuditInput{
		Transition: identity.AccountRestrictionTransitionCreated, OccurredAt: now,
		ActorID: actorID, TargetUserID: targetID, RestrictionID: restrictionID,
		CommandReasonCode: string(identity.AccountRestrictionReasonSecurityIncident),
		Reason:            createReason, ExpectedUserVersion: 3, After: after,
		Authorization: accountRestrictionAllowedDecision(now),
	})
	if err != nil {
		t.Fatalf("BuildAccountRestrictionEvent() error = %v", err)
	}
	if event.ID != eventID || event.Type != AccountRestrictionEventType || event.SchemaVersion != AccountRestrictionSchemaVersion {
		t.Fatalf("event envelope = %+v", event)
	}
	for _, forbidden := range [][]byte{[]byte(createReason), []byte(actorID.String()), []byte(targetID.String())} {
		if bytes.Contains(event.Payload, forbidden) {
			t.Fatalf("payload leaked private/editable value %q: %s", forbidden, event.Payload)
		}
	}
	var payload AccountRestrictionRecordedV1
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("Unmarshal(payload) error = %v", err)
	}
	if payload.ExpectedUserVersion != 3 || payload.ResultingUserVersion != 4 ||
		payload.ResultingRestrictionVersion != 1 || payload.ExpectedRestrictionVersion != 0 ||
		payload.BeforeSHA256 != "" || payload.AfterSHA256 == "" || payload.ReasonSHA256 == "" ||
		payload.ActorPseudonym == payload.TargetPseudonym {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestAccountRestrictionBuilderValidatesRevocationVersions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	builder, err := NewAccountRestrictionEventBuilder(RecorderConfig{
		PseudonymKey: []byte("0123456789abcdef0123456789abcdef"), PseudonymKeyEpoch: "test-2026-08",
	})
	if err != nil {
		t.Fatalf("NewAccountRestrictionEventBuilder() error = %v", err)
	}
	restrictionID := uuid.New()
	before := identity.AccountRestrictionAuditState{
		RestrictionID: restrictionID, Kind: string(identity.AccountRestrictionAccountAccess),
		ReasonCode: string(identity.AccountRestrictionReasonManualReview), ReasonSummary: "该账户需要在短期内完成人工资料复核。",
		StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), RestrictionVersion: 2,
		UserAdministrationVersion: 5,
	}
	revokedAt := now
	after := before
	after.RevokedAt = &revokedAt
	after.RevocationReasonCode = string(identity.AccountRestrictionRevocationReviewCompleted)
	after.RevocationReason = "人工复核已经完成，恢复账户访问。"
	after.RestrictionVersion = 3
	after.UserAdministrationVersion = 6
	_, err = builder.BuildAccountRestrictionEvent(identity.AccountRestrictionAuditInput{
		Transition: identity.AccountRestrictionTransitionRevoked, OccurredAt: now,
		ActorID: uuid.New(), TargetUserID: uuid.New(), RestrictionID: restrictionID,
		CommandReasonCode: string(identity.AccountRestrictionRevocationReviewCompleted),
		Reason:            after.RevocationReason, ExpectedUserVersion: 5,
		ExpectedRestrictionVersion: 1, Before: &before, After: after,
		Authorization: accountRestrictionAllowedDecision(now),
	})
	if err == nil {
		t.Fatal("BuildAccountRestrictionEvent() accepted stale restriction version")
	}
}

func TestAccountRestrictionBuilderRejectsUnboundedCreateState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 6, 13, 0, 0, 0, time.UTC)
	builder, err := NewAccountRestrictionEventBuilder(RecorderConfig{
		PseudonymKey: []byte("0123456789abcdef0123456789abcdef"), PseudonymKeyEpoch: "test-2026-08",
	})
	if err != nil {
		t.Fatalf("NewAccountRestrictionEventBuilder() error = %v", err)
	}
	restrictionID := uuid.New()
	reason := "等待人工复核账户状态并记录后续处置结论。"
	_, err = builder.BuildAccountRestrictionEvent(identity.AccountRestrictionAuditInput{
		Transition: identity.AccountRestrictionTransitionCreated, OccurredAt: now,
		ActorID: uuid.New(), TargetUserID: uuid.New(), RestrictionID: restrictionID,
		CommandReasonCode: string(identity.AccountRestrictionReasonManualReview),
		Reason:            reason, ExpectedUserVersion: 1,
		After: identity.AccountRestrictionAuditState{
			RestrictionID: restrictionID, Kind: string(identity.AccountRestrictionAccountAccess),
			ReasonCode: string(identity.AccountRestrictionReasonManualReview), ReasonSummary: reason,
			StartsAt: now, ExpiresAt: now.Add(7*24*time.Hour + time.Minute), RestrictionVersion: 1,
			UserAdministrationVersion: 2,
		},
		Authorization: accountRestrictionAllowedDecision(now),
	})
	if err == nil {
		t.Fatal("BuildAccountRestrictionEvent() accepted a state longer than seven days")
	}
}

func accountRestrictionAllowedDecision(now time.Time) authz.Decision {
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
		GrantID: uuid.New(), GrantVersion: 1, RoleID: "user_access_operator", MandateID: uuid.New(),
		EffectiveUntil: now.Add(time.Hour),
	}
}
