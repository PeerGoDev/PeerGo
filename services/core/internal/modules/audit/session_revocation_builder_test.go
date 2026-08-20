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

func TestSessionRevocationEventPseudonymizesUserAndTargetSession(t *testing.T) {
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	eventID := uuid.New()
	userID := uuid.New()
	sessionID := uuid.New()
	builder, err := NewSessionRevocationEventBuilder(RecorderConfig{
		PseudonymKey: bytes.Repeat([]byte{0x52}, 32), PseudonymKeyEpoch: "test-2026-08",
		NewEventID: func() uuid.UUID { return eventID },
	})
	if err != nil {
		t.Fatalf("NewSessionRevocationEventBuilder() error = %v", err)
	}
	decision := authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
		GrantID: uuid.New(), GrantVersion: 2, RoleID: "member", MandateID: uuid.New(),
		EffectiveUntil: now.Add(time.Hour),
	}
	event, err := builder.BuildSessionRevocationEvent(identity.SessionRevocationAuditInput{
		Command: identity.SessionRevocationCommand{
			ID: uuid.New(), UserID: userID, CurrentTokenHash: bytes.Repeat([]byte{0x19}, 32),
			TargetSessionID: sessionID, Scope: identity.SessionRevocationSingle,
			OccurredAt: now, Authorization: decision,
		},
		Result: identity.SessionRevocationResult{
			RevokedWebSessions: 1, RevokedStaffSessions: 1, CurrentSessionRevoked: false,
		},
	})
	if err != nil {
		t.Fatalf("BuildSessionRevocationEvent() error = %v", err)
	}
	if event.ID != eventID || event.Type != SessionRevocationEventType || event.SchemaVersion != SessionRevocationSchemaVersion {
		t.Fatalf("event envelope = %+v", event)
	}
	var payload SessionRevocationRecordedV1
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("Unmarshal(event.Payload) error = %v", err)
	}
	if payload.Scope != string(identity.SessionRevocationSingle) || payload.UserPseudonym == "" || payload.TargetSessionPseudonym == "" || payload.UserPseudonym == payload.TargetSessionPseudonym {
		t.Fatalf("payload pseudonyms/scope = %+v", payload)
	}
	encoded := string(event.Payload)
	for _, forbidden := range []string{userID.String(), sessionID.String(), strings.Repeat("19", 32), "token_hash", "ip_address", "user_agent"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("event payload leaked %q: %s", forbidden, encoded)
		}
	}
}
