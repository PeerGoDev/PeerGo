package audit

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

func TestRegistrationCompletedBuilderKeepsIdentityDataOutOfEvidence(t *testing.T) {
	t.Parallel()
	eventID := uuid.MustParse("0198f20a-6da8-7e51-9c64-515151515151")
	registrationID := uuid.MustParse("0198f20a-6da8-7e51-9c64-525252525252")
	userID := uuid.MustParse("0198f20a-6da8-7e51-9c64-535353535353")
	invitationID := uuid.MustParse("0198f20a-6da8-7e51-9c64-545454545454")
	now := time.Date(2026, time.August, 6, 9, 0, 0, 0, time.UTC)
	builder, err := NewRegistrationCompletedEventBuilder(RecorderConfig{
		PseudonymKey:      []byte("0123456789abcdef0123456789abcdef"),
		PseudonymKeyEpoch: "test-2026-08",
		NewEventID:        func() uuid.UUID { return eventID },
	})
	if err != nil {
		t.Fatalf("NewRegistrationCompletedEventBuilder() error = %v", err)
	}
	event, err := builder.BuildRegistrationCompletedEvent(identity.RegistrationAuditInput{
		RegistrationID: registrationID, UserID: userID,
		Mode: identity.RegistrationModeInvite, InvitationID: &invitationID, OccurredAt: now,
	})
	if err != nil {
		t.Fatalf("BuildRegistrationCompletedEvent() error = %v", err)
	}
	if event.ID != eventID || event.Type != RegistrationCompletedEventType || event.SchemaVersion != RegistrationCompletedSchemaVersion {
		t.Fatalf("event envelope = %+v", event)
	}
	if bytes.Contains(event.Payload, []byte(userID.String())) {
		t.Fatalf("registration event contains raw user UUID: %s", event.Payload)
	}
	var payload RegistrationCompletedV1
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("Unmarshal(payload) error = %v", err)
	}
	if payload.UserPseudonym == "" || payload.InvitationID == nil || *payload.InvitationID != invitationID {
		t.Fatalf("payload = %+v", payload)
	}
}
