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

type EmailVerifiedEventBuilder struct {
	pseudonymKey      []byte
	pseudonymKeyEpoch string
	newEventID        func() uuid.UUID
}

func NewEmailVerifiedEventBuilder(config RecorderConfig) (*EmailVerifiedEventBuilder, error) {
	config, err := validatedRecorderConfig(config)
	if err != nil {
		return nil, err
	}
	return &EmailVerifiedEventBuilder{
		pseudonymKey:      config.PseudonymKey,
		pseudonymKeyEpoch: config.PseudonymKeyEpoch,
		newEventID:        config.NewEventID,
	}, nil
}

func (builder *EmailVerifiedEventBuilder) BuildEmailVerifiedEvent(input identity.EmailVerificationAuditInput) (auditevent.Event, error) {
	if input.VerificationID == uuid.Nil || input.UserID == uuid.Nil || input.OccurredAt.IsZero() {
		return auditevent.Event{}, errors.New("email verified event is missing required metadata")
	}
	eventID := builder.newEventID()
	if eventID == uuid.Nil {
		return auditevent.Event{}, errors.New("audit event id generator returned nil")
	}
	payload, err := json.Marshal(EmailVerifiedV1{
		SchemaVersion:     EmailVerifiedSchemaVersion,
		EventType:         EmailVerifiedEventType,
		EventID:           eventID,
		OccurredAt:        input.OccurredAt.UTC(),
		VerificationID:    input.VerificationID,
		UserPseudonym:     subjectPseudonym(builder.pseudonymKey, input.UserID),
		PseudonymKeyEpoch: builder.pseudonymKeyEpoch,
	})
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("encode email verified event: %w", err)
	}
	if len(payload) > MaxEventPayloadBytes {
		return auditevent.Event{}, errors.New("email verified event exceeds outbox limit")
	}
	digest := sha256.Sum256(payload)
	return auditevent.Event{
		ID: eventID, Type: EmailVerifiedEventType, SchemaVersion: EmailVerifiedSchemaVersion,
		OccurredAt: input.OccurredAt.UTC(), Payload: payload, PayloadSHA256: digest,
	}, nil
}

var _ identity.EmailVerificationEventBuilder = (*EmailVerifiedEventBuilder)(nil)
