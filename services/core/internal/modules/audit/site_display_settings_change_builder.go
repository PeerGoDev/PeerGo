package audit

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
)

// SiteDisplaySettingsChangeEventBuilder is pure so catalog can construct the
// reviewed evidence while the singleton row lock and mutation transaction are
// still active.
type SiteDisplaySettingsChangeEventBuilder struct {
	pseudonymKey      []byte
	pseudonymKeyEpoch string
	newEventID        func() uuid.UUID
}

func NewSiteDisplaySettingsChangeEventBuilder(config RecorderConfig) (*SiteDisplaySettingsChangeEventBuilder, error) {
	config, err := validatedRecorderConfig(config)
	if err != nil {
		return nil, err
	}
	return &SiteDisplaySettingsChangeEventBuilder{
		pseudonymKey: config.PseudonymKey, pseudonymKeyEpoch: config.PseudonymKeyEpoch,
		newEventID: config.NewEventID,
	}, nil
}

func (builder *SiteDisplaySettingsChangeEventBuilder) BuildSiteDisplaySettingsEvent(input catalog.SiteDisplaySettingsAuditInput) (auditevent.Event, error) {
	if err := validateSiteDisplaySettingsAuditInput(input); err != nil {
		return auditevent.Event{}, err
	}
	eventID := builder.newEventID()
	if eventID == uuid.Nil {
		return auditevent.Event{}, errors.New("audit event id generator returned nil")
	}
	beforeHash, err := siteDisplaySettingsStateHash(input.Before)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("hash site display settings before state: %w", err)
	}
	afterHash, err := siteDisplaySettingsStateHash(input.After)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("hash site display settings after state: %w", err)
	}
	event := SiteDisplaySettingsChangeRecordedV1{
		SchemaVersion: SiteDisplaySettingsChangeSchemaVersion,
		EventType:     SiteDisplaySettingsChangeEventType,
		EventID:       eventID, OccurredAt: input.OccurredAt.UTC(),
		SettingsSection:   catalog.SiteDisplaySettingsSection,
		ActorPseudonym:    subjectPseudonym(builder.pseudonymKey, input.ActorID),
		PseudonymKeyEpoch: builder.pseudonymKeyEpoch,
		ReasonSHA256:      digestLabel(input.Reason), ExpectedVersion: input.ExpectedVersion,
		ResultingVersion: input.After.Version, BeforeSHA256: beforeHash, AfterSHA256: afterHash,
		DecisionID: input.Authorization.ID, PolicyVersion: input.Authorization.PolicyVersion,
		Authority: DecisionAuthorityV1{
			RoleID: input.Authorization.RoleID, GrantID: input.Authorization.GrantID,
			GrantVersion: input.Authorization.GrantVersion, MandateID: input.Authorization.MandateID,
			EffectiveUntil: input.Authorization.EffectiveUntil.UTC(),
		},
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("encode site display settings change event: %w", err)
	}
	if len(payload) > MaxEventPayloadBytes {
		return auditevent.Event{}, errors.New("site display settings change event exceeds outbox limit")
	}
	digest := sha256.Sum256(payload)
	return auditevent.Event{
		ID: eventID, Type: SiteDisplaySettingsChangeEventType,
		SchemaVersion: SiteDisplaySettingsChangeSchemaVersion,
		OccurredAt:    input.OccurredAt.UTC(), Payload: payload, PayloadSHA256: digest,
	}, nil
}

func validateSiteDisplaySettingsAuditInput(input catalog.SiteDisplaySettingsAuditInput) error {
	if input.OccurredAt.IsZero() || input.ActorID == uuid.Nil || input.Reason == "" ||
		input.ExpectedVersion < 1 || input.Before.Version != input.ExpectedVersion ||
		input.After.Version != input.ExpectedVersion+1 {
		return errors.New("site display settings change event has invalid metadata or versions")
	}
	decision := input.Authorization
	if !decision.Allow || decision.Reason != authz.ReasonAllowed || decision.ID == uuid.Nil ||
		decision.PolicyVersion == "" || decision.GrantID == uuid.Nil || decision.GrantVersion < 1 ||
		decision.RoleID == "" || decision.MandateID == uuid.Nil || decision.EffectiveUntil.IsZero() {
		return errors.New("site display settings change event is missing authorization evidence")
	}
	return nil
}

func siteDisplaySettingsStateHash(state catalog.SiteDisplaySettingsAuditState) (string, error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return digestLabel(string(encoded)), nil
}

var _ catalog.SiteDisplaySettingsEventBuilder = (*SiteDisplaySettingsChangeEventBuilder)(nil)
