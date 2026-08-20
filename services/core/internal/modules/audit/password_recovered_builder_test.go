package audit

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

func TestPasswordRecoveredBuilderKeepsPIIAndCredentialMaterialOutOfEvidence(t *testing.T) {
	eventID := uuid.New()
	recoveryID := uuid.New()
	userID := uuid.New()
	now := time.Date(2026, time.August, 6, 21, 30, 0, 0, time.UTC)
	builder, err := NewPasswordRecoveredEventBuilder(RecorderConfig{
		PseudonymKey:      []byte("0123456789abcdef0123456789abcdef"),
		PseudonymKeyEpoch: "test-2026-08", NewEventID: func() uuid.UUID { return eventID },
	})
	if err != nil {
		t.Fatalf("NewPasswordRecoveredEventBuilder() error = %v", err)
	}
	event, err := builder.BuildPasswordRecoveredEvent(identity.PasswordRecoveryAuditInput{
		RecoveryID: recoveryID, UserID: userID, RevokedSessions: 3, OccurredAt: now,
	})
	if err != nil {
		t.Fatalf("BuildPasswordRecoveredEvent() error = %v", err)
	}
	for _, forbidden := range [][]byte{
		[]byte(userID.String()), []byte("member@example.com"), []byte("credential_ref"),
		[]byte("password_hash"), []byte("token_sha256"),
	} {
		if bytes.Contains(event.Payload, forbidden) {
			t.Fatalf("password recovered event leaked %q: %s", forbidden, event.Payload)
		}
	}
	var payload PasswordRecoveredV1
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("Unmarshal(payload) error = %v", err)
	}
	if payload.RecoveryID != recoveryID || payload.UserPseudonym == "" || payload.RevokedSessions != 3 {
		t.Fatalf("payload=%+v", payload)
	}
}
