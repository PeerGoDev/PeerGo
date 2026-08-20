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

func TestRegistrationPolicyChangeBuilderHashesStateAndReason(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	builder, err := NewRegistrationPolicyChangeEventBuilder(RecorderConfig{
		PseudonymKey: []byte("0123456789abcdef0123456789abcdef"), PseudonymKeyEpoch: "test-2026-08",
	})
	if err != nil {
		t.Fatalf("NewRegistrationPolicyChangeEventBuilder() error = %v", err)
	}
	reason := "维护期间暂时停止创建新账户。"
	event, err := builder.BuildRegistrationPolicyEvent(identity.RegistrationPolicyAuditInput{
		OccurredAt: now, ActorID: uuid.New(), Reason: reason, ExpectedVersion: 3,
		Before: identity.RegistrationPolicyAuditState{Mode: identity.RegistrationModeInvite, Version: 3},
		After:  identity.RegistrationPolicyAuditState{Mode: identity.RegistrationModeClosed, Version: 4},
		Authorization: authz.Decision{
			ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
			GrantID: uuid.New(), GrantVersion: 1, RoleID: "site_admin", MandateID: uuid.New(),
			EffectiveUntil: now.Add(time.Hour),
		},
	})
	if err != nil {
		t.Fatalf("BuildRegistrationPolicyEvent() error = %v", err)
	}
	if event.Type != RegistrationPolicyChangeEventType || bytes.Contains(event.Payload, []byte(reason)) {
		t.Fatalf("event=%+v payload=%s", event, event.Payload)
	}
	var payload RegistrationPolicyChangeRecordedV1
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("Unmarshal(payload) error = %v", err)
	}
	if payload.SettingsSection != identity.RegistrationPolicySettingsSection || payload.ExpectedVersion != 3 ||
		payload.ResultingVersion != 4 || payload.BeforeSHA256 == "" || payload.AfterSHA256 == "" || payload.ReasonSHA256 == "" {
		t.Fatalf("payload = %+v", payload)
	}
}
