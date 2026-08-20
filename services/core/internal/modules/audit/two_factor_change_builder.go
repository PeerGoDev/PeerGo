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

type TwoFactorChangeEventBuilder struct {
	pseudonymKey      []byte
	pseudonymKeyEpoch string
	newEventID        func() uuid.UUID
}

func NewTwoFactorChangeEventBuilder(config RecorderConfig) (*TwoFactorChangeEventBuilder, error) {
	config, err := validatedRecorderConfig(config)
	if err != nil {
		return nil, err
	}
	return &TwoFactorChangeEventBuilder{
		pseudonymKey: config.PseudonymKey, pseudonymKeyEpoch: config.PseudonymKeyEpoch,
		newEventID: config.NewEventID,
	}, nil
}

func (builder *TwoFactorChangeEventBuilder) BuildTwoFactorChangeEvent(input identity.TwoFactorChangeAuditInput) (auditevent.Event, error) {
	command, result := input.Command, input.Result
	decision := command.Authorization
	if command.ID == uuid.Nil || command.UserID == uuid.Nil || command.OccurredAt.IsZero() ||
		result.RevokedWebSessions < 0 || result.RevokedStaffSessions < 0 ||
		!decision.Allow || decision.ID == uuid.Nil || decision.PolicyVersion == "" ||
		decision.RoleID == "" || decision.GrantID == uuid.Nil || decision.GrantVersion < 1 ||
		decision.MandateID == uuid.Nil || decision.EffectiveUntil.IsZero() {
		return auditevent.Event{}, errors.New("two-factor change event is missing required evidence")
	}
	if command.Kind != identity.TwoFactorEnabled && command.Kind != identity.TwoFactorRecoveryCodesRotated && command.Kind != identity.TwoFactorDisabled {
		return auditevent.Event{}, errors.New("two-factor change kind is invalid")
	}
	eventID := builder.newEventID()
	if eventID == uuid.Nil {
		return auditevent.Event{}, errors.New("audit event id generator returned nil")
	}
	payload, err := json.Marshal(TwoFactorChangeRecordedV1{
		SchemaVersion: TwoFactorChangeSchemaVersion, EventType: TwoFactorChangeEventType,
		EventID: eventID, OccurredAt: command.OccurredAt.UTC(), ChangeID: command.ID,
		Kind: string(command.Kind), UserPseudonym: subjectPseudonym(builder.pseudonymKey, command.UserID),
		PseudonymKeyEpoch:  builder.pseudonymKeyEpoch,
		RevokedWebSessions: result.RevokedWebSessions, RevokedStaffSessions: result.RevokedStaffSessions,
		DecisionID: decision.ID, PolicyVersion: decision.PolicyVersion,
		Authority: DecisionAuthorityV1{
			RoleID: decision.RoleID, GrantID: decision.GrantID, GrantVersion: decision.GrantVersion,
			MandateID: decision.MandateID, EffectiveUntil: decision.EffectiveUntil.UTC(),
		},
	})
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("encode two-factor change event: %w", err)
	}
	if len(payload) > MaxEventPayloadBytes {
		return auditevent.Event{}, errors.New("two-factor change event exceeds outbox limit")
	}
	digest := sha256.Sum256(payload)
	return auditevent.Event{
		ID: eventID, Type: TwoFactorChangeEventType, SchemaVersion: TwoFactorChangeSchemaVersion,
		OccurredAt: command.OccurredAt.UTC(), Payload: payload, PayloadSHA256: digest,
	}, nil
}

var _ identity.TwoFactorChangeEventBuilder = (*TwoFactorChangeEventBuilder)(nil)
