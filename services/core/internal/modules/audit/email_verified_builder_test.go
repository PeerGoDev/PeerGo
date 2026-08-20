package audit

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

func TestEmailVerifiedBuilderKeepsPIIAndCredentialMaterialOutOfEvidence(t *testing.T) {
	eventID := uuid.New()
	verificationID := uuid.New()
	userID := uuid.New()
	now := time.Date(2026, time.August, 6, 15, 0, 0, 0, time.UTC)
	builder, err := NewEmailVerifiedEventBuilder(RecorderConfig{
		PseudonymKey:      []byte("0123456789abcdef0123456789abcdef"),
		PseudonymKeyEpoch: "test-2026-08",
		NewEventID:        func() uuid.UUID { return eventID },
	})
	if err != nil {
		t.Fatalf("NewEmailVerifiedEventBuilder() error = %v", err)
	}
	event, err := builder.BuildEmailVerifiedEvent(identity.EmailVerificationAuditInput{
		VerificationID: verificationID, UserID: userID, OccurredAt: now,
	})
	if err != nil {
		t.Fatalf("BuildEmailVerifiedEvent() error = %v", err)
	}
	for _, forbidden := range [][]byte{
		[]byte(userID.String()), []byte("member@example.com"), []byte("credential_ref"), []byte("token_sha256"),
	} {
		if bytes.Contains(event.Payload, forbidden) {
			t.Fatalf("email verified event leaked %q: %s", forbidden, event.Payload)
		}
	}
	var payload EmailVerifiedV1
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("Unmarshal(payload) error = %v", err)
	}
	if payload.VerificationID != verificationID || payload.UserPseudonym == "" || payload.EventID != eventID {
		t.Fatalf("payload = %+v", payload)
	}
}
