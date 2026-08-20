package audit

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

// RegistrationPolicyChangeEventBuilder produces bounded evidence while the
// identity policy row is still locked. Clear reasons never enter the outbox.
type RegistrationPolicyChangeEventBuilder struct {
	pseudonymKey      []byte
	pseudonymKeyEpoch string
	newEventID        func() uuid.UUID
}

func NewRegistrationPolicyChangeEventBuilder(config RecorderConfig) (*RegistrationPolicyChangeEventBuilder, error) {
	config, err := validatedRecorderConfig(config)
	if err != nil {
		return nil, err
	}
	return &RegistrationPolicyChangeEventBuilder{
		pseudonymKey: config.PseudonymKey, pseudonymKeyEpoch: config.PseudonymKeyEpoch,
		newEventID: config.NewEventID,
	}, nil
}

func (builder *RegistrationPolicyChangeEventBuilder) BuildRegistrationPolicyEvent(input identity.RegistrationPolicyAuditInput) (auditevent.Event, error) {
	if err := validateRegistrationPolicyAuditInput(input); err != nil {
		return auditevent.Event{}, err
	}
	eventID := builder.newEventID()
	if eventID == uuid.Nil {
		return auditevent.Event{}, errors.New("audit event id generator returned nil")
	}
	beforeHash, err := registrationPolicyStateHash(input.Before)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("hash registration policy before state: %w", err)
	}
	afterHash, err := registrationPolicyStateHash(input.After)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("hash registration policy after state: %w", err)
	}
	event := RegistrationPolicyChangeRecordedV1{
		SchemaVersion:     RegistrationPolicyChangeSchemaVersion,
		EventType:         RegistrationPolicyChangeEventType,
		EventID:           eventID,
		OccurredAt:        input.OccurredAt.UTC(),
		SettingsSection:   identity.RegistrationPolicySettingsSection,
		ActorPseudonym:    subjectPseudonym(builder.pseudonymKey, input.ActorID),
		PseudonymKeyEpoch: builder.pseudonymKeyEpoch,
		ReasonSHA256:      digestLabel(input.Reason),
		ExpectedVersion:   input.ExpectedVersion,
		ResultingVersion:  input.After.Version,
		BeforeSHA256:      beforeHash,
		AfterSHA256:       afterHash,
		DecisionID:        input.Authorization.ID,
		PolicyVersion:     input.Authorization.PolicyVersion,
		Authority: DecisionAuthorityV1{
			RoleID: input.Authorization.RoleID, GrantID: input.Authorization.GrantID,
			GrantVersion: input.Authorization.GrantVersion, MandateID: input.Authorization.MandateID,
			EffectiveUntil: input.Authorization.EffectiveUntil.UTC(),
		},
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("encode registration policy change event: %w", err)
	}
	if len(payload) > MaxEventPayloadBytes {
		return auditevent.Event{}, errors.New("registration policy change event exceeds outbox limit")
	}
	digest := sha256.Sum256(payload)
	return auditevent.Event{
		ID: eventID, Type: RegistrationPolicyChangeEventType,
		SchemaVersion: RegistrationPolicyChangeSchemaVersion,
		OccurredAt:    input.OccurredAt.UTC(), Payload: payload, PayloadSHA256: digest,
	}, nil
}

func validateRegistrationPolicyAuditInput(input identity.RegistrationPolicyAuditInput) error {
	if input.OccurredAt.IsZero() || input.ActorID == uuid.Nil || input.Reason == "" ||
		input.ExpectedVersion < 1 || input.Before.Version != input.ExpectedVersion ||
		input.After.Version != input.ExpectedVersion+1 {
		return errors.New("registration policy change event has invalid metadata or versions")
	}
	decision := input.Authorization
	if !decision.Allow || decision.Reason != authz.ReasonAllowed || decision.ID == uuid.Nil ||
		decision.PolicyVersion == "" || decision.GrantID == uuid.Nil || decision.GrantVersion < 1 ||
		decision.RoleID == "" || decision.MandateID == uuid.Nil || decision.EffectiveUntil.IsZero() {
		return errors.New("registration policy change event is missing authorization evidence")
	}
	return nil
}

func registrationPolicyStateHash(state identity.RegistrationPolicyAuditState) (string, error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return digestLabel(string(encoded)), nil
}

var _ identity.RegistrationPolicyEventBuilder = (*RegistrationPolicyChangeEventBuilder)(nil)
