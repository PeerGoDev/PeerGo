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

type AnnouncementChangeEventBuilder struct {
	pseudonymKey      []byte
	pseudonymKeyEpoch string
	newEventID        func() uuid.UUID
}

func NewAnnouncementChangeEventBuilder(config RecorderConfig) (*AnnouncementChangeEventBuilder, error) {
	config, err := validatedRecorderConfig(config)
	if err != nil {
		return nil, err
	}
	return &AnnouncementChangeEventBuilder{
		pseudonymKey: config.PseudonymKey, pseudonymKeyEpoch: config.PseudonymKeyEpoch,
		newEventID: config.NewEventID,
	}, nil
}

func (builder *AnnouncementChangeEventBuilder) BuildAnnouncementEvent(input catalog.AnnouncementAuditInput) (auditevent.Event, error) {
	if err := validateAnnouncementAuditInput(input); err != nil {
		return auditevent.Event{}, err
	}
	eventID := builder.newEventID()
	if eventID == uuid.Nil {
		return auditevent.Event{}, errors.New("audit event id generator returned nil")
	}
	afterHash, err := announcementStateHash(input.After)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("hash announcement after state: %w", err)
	}
	event := AnnouncementChangeRecordedV1{
		SchemaVersion: AnnouncementChangeSchemaVersion, EventType: AnnouncementChangeEventType,
		EventID: eventID, OccurredAt: input.OccurredAt.UTC(), AnnouncementID: input.AnnouncementID,
		Transition: string(input.Transition), ActorPseudonym: subjectPseudonym(builder.pseudonymKey, input.ActorID),
		PseudonymKeyEpoch: builder.pseudonymKeyEpoch, ReasonSHA256: digestLabel(input.Reason),
		ExpectedVersion: input.ExpectedVersion, ResultingVersion: input.After.Version,
		RevisionNumber: input.After.RevisionNumber, AfterSHA256: afterHash,
		DecisionID: input.Authorization.ID, PolicyVersion: input.Authorization.PolicyVersion,
		Authority: DecisionAuthorityV1{
			RoleID: input.Authorization.RoleID, GrantID: input.Authorization.GrantID,
			GrantVersion: input.Authorization.GrantVersion, MandateID: input.Authorization.MandateID,
			EffectiveUntil: input.Authorization.EffectiveUntil.UTC(),
		},
	}
	if input.Before != nil {
		event.BeforeSHA256, err = announcementStateHash(*input.Before)
		if err != nil {
			return auditevent.Event{}, fmt.Errorf("hash announcement before state: %w", err)
		}
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("encode announcement change event: %w", err)
	}
	if len(payload) > MaxEventPayloadBytes {
		return auditevent.Event{}, errors.New("announcement change event exceeds outbox limit")
	}
	digest := sha256.Sum256(payload)
	return auditevent.Event{
		ID: eventID, Type: AnnouncementChangeEventType, SchemaVersion: AnnouncementChangeSchemaVersion,
		OccurredAt: input.OccurredAt.UTC(), Payload: payload, PayloadSHA256: digest,
	}, nil
}

func validateAnnouncementAuditInput(input catalog.AnnouncementAuditInput) error {
	if input.OccurredAt.IsZero() || input.ActorID == uuid.Nil || !catalog.ValidAnnouncementID(input.AnnouncementID) ||
		input.Reason == "" || input.After.ID != input.AnnouncementID || input.After.RevisionNumber < 1 {
		return errors.New("announcement change event is missing required metadata")
	}
	decision := input.Authorization
	if !decision.Allow || decision.Reason != authz.ReasonAllowed || decision.ID == uuid.Nil ||
		decision.PolicyVersion == "" || decision.GrantID == uuid.Nil || decision.GrantVersion < 1 ||
		decision.RoleID == "" || decision.MandateID == uuid.Nil || decision.EffectiveUntil.IsZero() {
		return errors.New("announcement change event is missing authorization evidence")
	}
	switch input.Transition {
	case catalog.AnnouncementTransitionDraftCreated:
		if input.Before != nil || input.ExpectedVersion != 0 || input.After.Version != 1 {
			return errors.New("created announcement transition has invalid versions")
		}
	case catalog.AnnouncementTransitionDraftUpdated,
		catalog.AnnouncementTransitionPublished,
		catalog.AnnouncementTransitionScheduled,
		catalog.AnnouncementTransitionScheduleCanceled,
		catalog.AnnouncementTransitionWithdrawn:
		if input.Before == nil || input.ExpectedVersion < 1 || input.Before.ID != input.AnnouncementID ||
			input.Before.Version != input.ExpectedVersion || input.After.Version != input.ExpectedVersion+1 {
			return errors.New("announcement transition has invalid versions")
		}
	default:
		return errors.New("announcement change event has an unknown transition")
	}
	return nil
}

func announcementStateHash(state catalog.AnnouncementAuditState) (string, error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return digestLabel(string(encoded)), nil
}

var _ catalog.AnnouncementEventBuilder = (*AnnouncementChangeEventBuilder)(nil)
