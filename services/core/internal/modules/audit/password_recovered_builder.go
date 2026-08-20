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

type PasswordRecoveredEventBuilder struct {
	pseudonymKey      []byte
	pseudonymKeyEpoch string
	newEventID        func() uuid.UUID
}

func NewPasswordRecoveredEventBuilder(config RecorderConfig) (*PasswordRecoveredEventBuilder, error) {
	config, err := validatedRecorderConfig(config)
	if err != nil {
		return nil, err
	}
	return &PasswordRecoveredEventBuilder{
		pseudonymKey: config.PseudonymKey, pseudonymKeyEpoch: config.PseudonymKeyEpoch,
		newEventID: config.NewEventID,
	}, nil
}

func (builder *PasswordRecoveredEventBuilder) BuildPasswordRecoveredEvent(input identity.PasswordRecoveryAuditInput) (auditevent.Event, error) {
	if input.RecoveryID == uuid.Nil || input.UserID == uuid.Nil || input.RevokedSessions < 0 || input.OccurredAt.IsZero() {
		return auditevent.Event{}, errors.New("password recovered event is missing required metadata")
	}
	eventID := builder.newEventID()
	if eventID == uuid.Nil {
		return auditevent.Event{}, errors.New("audit event id generator returned nil")
	}
	payload, err := json.Marshal(PasswordRecoveredV1{
		SchemaVersion:     PasswordRecoveredSchemaVersion,
		EventType:         PasswordRecoveredEventType,
		EventID:           eventID,
		OccurredAt:        input.OccurredAt.UTC(),
		RecoveryID:        input.RecoveryID,
		UserPseudonym:     subjectPseudonym(builder.pseudonymKey, input.UserID),
		PseudonymKeyEpoch: builder.pseudonymKeyEpoch,
		RevokedSessions:   input.RevokedSessions,
	})
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("encode password recovered event: %w", err)
	}
	if len(payload) > MaxEventPayloadBytes {
		return auditevent.Event{}, errors.New("password recovered event exceeds outbox limit")
	}
	digest := sha256.Sum256(payload)
	return auditevent.Event{
		ID: eventID, Type: PasswordRecoveredEventType, SchemaVersion: PasswordRecoveredSchemaVersion,
		OccurredAt: input.OccurredAt.UTC(), Payload: payload, PayloadSHA256: digest,
	}, nil
}

var _ identity.PasswordRecoveryEventBuilder = (*PasswordRecoveredEventBuilder)(nil)
