package audit

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

func TestTwoFactorChangeEventContainsEvidenceWithoutCredentialMaterial(t *testing.T) {
	now := time.Date(2026, time.August, 7, 11, 30, 0, 0, time.UTC)
	eventID, changeID, userID := uuid.New(), uuid.New(), uuid.New()
	builder, err := NewTwoFactorChangeEventBuilder(RecorderConfig{
		PseudonymKey: bytes.Repeat([]byte{0x71}, 32), PseudonymKeyEpoch: "test-2026-08",
		NewEventID: func() uuid.UUID { return eventID },
	})
	if err != nil {
		t.Fatalf("NewTwoFactorChangeEventBuilder() error = %v", err)
	}
	decision := authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
		GrantID: uuid.New(), GrantVersion: 3, RoleID: "member", MandateID: uuid.New(),
		EffectiveUntil: now.Add(time.Hour),
	}
	event, err := builder.BuildTwoFactorChangeEvent(identity.TwoFactorChangeAuditInput{
		Command: identity.TwoFactorChangeCommand{
			ID: changeID, UserID: userID, CurrentTokenHash: bytes.Repeat([]byte{0x19}, 32),
			Kind: identity.TwoFactorEnabled, OccurredAt: now, Authorization: decision,
		},
		Result: identity.TwoFactorChangeResult{RevokedWebSessions: 2, RevokedStaffSessions: 1},
	})
	if err != nil {
		t.Fatalf("BuildTwoFactorChangeEvent() error = %v", err)
	}
	if event.ID != eventID || event.Type != TwoFactorChangeEventType || event.SchemaVersion != TwoFactorChangeSchemaVersion {
		t.Fatalf("event envelope = %+v", event)
	}
	var payload TwoFactorChangeRecordedV1
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("Unmarshal(event.Payload) error = %v", err)
	}
	if payload.ChangeID != changeID || payload.Kind != string(identity.TwoFactorEnabled) || payload.UserPseudonym == "" || payload.RevokedWebSessions != 2 || payload.RevokedStaffSessions != 1 || payload.DecisionID != decision.ID {
		t.Fatalf("event payload = %+v", payload)
	}
	encoded := string(event.Payload)
	for _, forbidden := range []string{
		userID.String(), strings.Repeat("19", 32), "credential_ref", "secret", "recovery_code", "password", "session_id",
	} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("event payload leaked %q: %s", forbidden, encoded)
		}
	}
}
