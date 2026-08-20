package audit

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
)

var auditCategoryIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// CategoryChangeEventBuilder owns the reviewed event JSON and pseudonymization
// rules. It stays pure so the catalog repository can build evidence while its
// mutation transaction still holds the category row lock.
type CategoryChangeEventBuilder struct {
	pseudonymKey      []byte
	pseudonymKeyEpoch string
	newEventID        func() uuid.UUID
}

func NewCategoryChangeEventBuilder(config RecorderConfig) (*CategoryChangeEventBuilder, error) {
	config, err := validatedRecorderConfig(config)
	if err != nil {
		return nil, err
	}
	return &CategoryChangeEventBuilder{
		pseudonymKey: config.PseudonymKey, pseudonymKeyEpoch: config.PseudonymKeyEpoch,
		newEventID: config.NewEventID,
	}, nil
}

func (builder *CategoryChangeEventBuilder) BuildCategoryEvent(input catalog.CategoryAuditInput) (auditevent.Event, error) {
	if err := validateCategoryAuditInput(input); err != nil {
		return auditevent.Event{}, err
	}
	eventID := builder.newEventID()
	if eventID == uuid.Nil {
		return auditevent.Event{}, errors.New("audit event id generator returned nil")
	}
	afterHash, err := categoryStateHash(input.After)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("hash category after state: %w", err)
	}
	event := CategoryChangeRecordedV1{
		SchemaVersion: CategoryChangeSchemaVersion, EventType: CategoryChangeEventType,
		EventID: eventID, OccurredAt: input.OccurredAt.UTC(), CategoryID: input.CategoryID,
		Transition: string(input.Transition), ActorPseudonym: subjectPseudonym(builder.pseudonymKey, input.ActorID),
		PseudonymKeyEpoch: builder.pseudonymKeyEpoch, ReasonSHA256: digestLabel(input.Reason),
		ExpectedVersion: input.ExpectedVersion, ResultingVersion: input.After.Version,
		AfterSHA256: afterHash, DecisionID: input.Authorization.ID,
		PolicyVersion: input.Authorization.PolicyVersion,
		Authority: DecisionAuthorityV1{
			RoleID: input.Authorization.RoleID, GrantID: input.Authorization.GrantID,
			GrantVersion: input.Authorization.GrantVersion, MandateID: input.Authorization.MandateID,
			EffectiveUntil: input.Authorization.EffectiveUntil.UTC(),
		},
	}
	if input.Before != nil {
		event.BeforeSHA256, err = categoryStateHash(*input.Before)
		if err != nil {
			return auditevent.Event{}, fmt.Errorf("hash category before state: %w", err)
		}
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("encode category change event: %w", err)
	}
	if len(payload) > MaxEventPayloadBytes {
		return auditevent.Event{}, errors.New("category change event exceeds outbox limit")
	}
	digest := sha256.Sum256(payload)
	return auditevent.Event{
		ID: eventID, Type: CategoryChangeEventType, SchemaVersion: CategoryChangeSchemaVersion,
		OccurredAt: input.OccurredAt.UTC(), Payload: payload, PayloadSHA256: digest,
	}, nil
}

func validateCategoryAuditInput(input catalog.CategoryAuditInput) error {
	if input.OccurredAt.IsZero() || input.ActorID == uuid.Nil || !auditCategoryIDPattern.MatchString(input.CategoryID) || input.Reason == "" || input.After.ID != input.CategoryID {
		return errors.New("category change event is missing required metadata")
	}
	decision := input.Authorization
	if !decision.Allow || decision.Reason != authz.ReasonAllowed || decision.ID == uuid.Nil || decision.PolicyVersion == "" || decision.GrantID == uuid.Nil || decision.GrantVersion < 1 || decision.RoleID == "" || decision.MandateID == uuid.Nil || decision.EffectiveUntil.IsZero() {
		return errors.New("category change event is missing authorization evidence")
	}
	switch input.Transition {
	case catalog.CategoryTransitionCreated:
		if input.Before != nil || input.ExpectedVersion != 0 || input.After.Version != 1 {
			return errors.New("created category transition has invalid versions")
		}
	case catalog.CategoryTransitionUpdated:
		if input.Before == nil || input.ExpectedVersion < 1 || input.Before.ID != input.CategoryID || input.Before.Version != input.ExpectedVersion || input.After.Version != input.ExpectedVersion+1 {
			return errors.New("updated category transition has invalid versions")
		}
	default:
		return errors.New("category change event has an unknown transition")
	}
	return nil
}

func categoryStateHash(state catalog.CategoryAuditState) (string, error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return digestLabel(string(encoded)), nil
}

var _ catalog.CategoryEventBuilder = (*CategoryChangeEventBuilder)(nil)
