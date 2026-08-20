package audit

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

func TestGrantRevocationEventContainsHashedStateAndDecisionEvidence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	eventID := uuid.New()
	builder, err := NewGrantRevocationEventBuilder(RecorderConfig{
		PseudonymKey:      bytes.Repeat([]byte{0x42}, 32),
		PseudonymKeyEpoch: "test-2026-08",
		NewEventID:        func() uuid.UUID { return eventID },
	})
	if err != nil {
		t.Fatalf("NewGrantRevocationEventBuilder() error = %v", err)
	}
	input := validAppliedGrantRevocationAuditInput(now)
	event, err := builder.BuildGrantRevocationEvent(input)
	if err != nil {
		t.Fatalf("BuildGrantRevocationEvent() error = %v", err)
	}
	if event.ID != eventID || event.Type != GrantRevocationEventType || event.SchemaVersion != GrantRevocationSchemaVersion || !event.OccurredAt.Equal(now) {
		t.Fatalf("event envelope = %+v", event)
	}
	if bytes.Contains(event.Payload, []byte(input.Reason)) {
		t.Fatalf("event payload leaked reason: %s", event.Payload)
	}
	var payload GrantRevocationRecordedV1
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("Unmarshal(payload) error = %v", err)
	}
	if payload.Transition != string(authz.GrantTransitionApplied) || payload.ResultingGrantVersion != 8 || payload.Review == nil || payload.Review.Domain != string(authz.GrantReviewSecurity) || payload.DecisionID != input.Authorization.ID {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.ActorPseudonym == payload.TargetPseudonym || payload.ReasonSHA256 == "" || payload.BeforeSHA256 == payload.AfterSHA256 {
		t.Fatalf("pseudonyms/hashes = actor=%q target=%q reason=%q before=%q after=%q", payload.ActorPseudonym, payload.TargetPseudonym, payload.ReasonSHA256, payload.BeforeSHA256, payload.AfterSHA256)
	}
	if err := validateEvent(event); err != nil {
		t.Fatalf("validateEvent() error = %v", err)
	}
}

func TestGrantRevocationEventRejectsIncompleteAppliedEvidence(t *testing.T) {
	t.Parallel()

	builder, err := NewGrantRevocationEventBuilder(RecorderConfig{
		PseudonymKey:      bytes.Repeat([]byte{0x42}, 32),
		PseudonymKeyEpoch: "test-2026-08",
	})
	if err != nil {
		t.Fatalf("NewGrantRevocationEventBuilder() error = %v", err)
	}
	input := validAppliedGrantRevocationAuditInput(time.Now().UTC())
	input.ReviewID = uuid.Nil
	if _, err := builder.BuildGrantRevocationEvent(input); err == nil {
		t.Fatal("BuildGrantRevocationEvent() error = nil")
	}
}

func validAppliedGrantRevocationAuditInput(now time.Time) authz.GrantRevocationAuditInput {
	return authz.GrantRevocationAuditInput{
		Transition:            authz.GrantTransitionApplied,
		OccurredAt:            now,
		RequestID:             uuid.New(),
		GrantID:               uuid.New(),
		ExpectedGrantVersion:  7,
		ResultingGrantVersion: 8,
		ActorID:               uuid.New(),
		TargetSubjectID:       uuid.New(),
		Reason:                "安全职责确认撤销并完成最终版本变更。",
		Authorization: authz.Decision{
			ID:             uuid.New(),
			Allow:          true,
			Reason:         authz.ReasonAllowed,
			PolicyVersion:  authz.PolicyVersion,
			GrantID:        uuid.New(),
			GrantVersion:   3,
			RoleID:         "grant_security_reviewer",
			MandateID:      uuid.New(),
			EffectiveUntil: now.Add(time.Hour),
		},
		ReviewID:       uuid.New(),
		ReviewDomain:   authz.GrantReviewSecurity,
		ReviewDecision: authz.GrantReviewApprove,
		Before: authz.GrantRevocationAuditState{
			Status:             authz.GrantRevocationPendingStatus,
			GrantVersion:       7,
			GovernanceDecision: authz.GrantReviewApprove,
		},
		After: authz.GrantRevocationAuditState{
			Status:             authz.GrantRevocationAppliedStatus,
			GrantVersion:       8,
			GrantRevoked:       true,
			GovernanceDecision: authz.GrantReviewApprove,
			SecurityDecision:   authz.GrantReviewApprove,
		},
	}
}
