package audit

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type RegistrationCompletedEventBuilder struct {
	pseudonymKey      []byte
	pseudonymKeyEpoch string
	newEventID        func() uuid.UUID
}

func NewRegistrationCompletedEventBuilder(config RecorderConfig) (*RegistrationCompletedEventBuilder, error) {
	config, err := validatedRecorderConfig(config)
	if err != nil {
		return nil, err
	}
	return &RegistrationCompletedEventBuilder{
		pseudonymKey: config.PseudonymKey, pseudonymKeyEpoch: config.PseudonymKeyEpoch,
		newEventID: config.NewEventID,
	}, nil
}

func (builder *RegistrationCompletedEventBuilder) BuildRegistrationCompletedEvent(input identity.RegistrationAuditInput) (auditevent.Event, error) {
	if input.RegistrationID == uuid.Nil || input.UserID == uuid.Nil || input.OccurredAt.IsZero() {
		return auditevent.Event{}, errors.New("registration audit event is missing required metadata")
	}
	if input.Mode != identity.RegistrationModeOpen && input.Mode != identity.RegistrationModeInvite {
		return auditevent.Event{}, errors.New("registration audit event has invalid admission mode")
	}
	if (input.Mode == identity.RegistrationModeInvite) != (input.InvitationID != nil) {
		return auditevent.Event{}, errors.New("registration audit invitation does not match admission mode")
	}
	eventID := builder.newEventID()
	if eventID == uuid.Nil {
		return auditevent.Event{}, errors.New("audit event id generator returned nil")
	}
	payload, err := json.Marshal(RegistrationCompletedV1{
		SchemaVersion:     RegistrationCompletedSchemaVersion,
		EventType:         RegistrationCompletedEventType,
		EventID:           eventID,
		OccurredAt:        input.OccurredAt.UTC(),
		RegistrationID:    input.RegistrationID,
		UserPseudonym:     subjectPseudonym(builder.pseudonymKey, input.UserID),
		PseudonymKeyEpoch: builder.pseudonymKeyEpoch,
		AdmissionMode:     string(input.Mode),
		InvitationID:      input.InvitationID,
	})
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("encode registration completed event: %w", err)
	}
	if len(payload) > MaxEventPayloadBytes {
		return auditevent.Event{}, errors.New("registration completed event exceeds outbox limit")
	}
	digest := sha256.Sum256(payload)
	return auditevent.Event{
		ID: eventID, Type: RegistrationCompletedEventType,
		SchemaVersion: RegistrationCompletedSchemaVersion,
		OccurredAt:    input.OccurredAt.UTC(), Payload: payload, PayloadSHA256: digest,
	}, nil
}

var _ identity.RegistrationEventBuilder = (*RegistrationCompletedEventBuilder)(nil)
