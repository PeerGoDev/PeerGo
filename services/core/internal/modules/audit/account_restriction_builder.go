package audit

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

// AccountRestrictionEventBuilder keeps PII pseudonymization and the reviewed
// wire contract out of identity's transaction while remaining pure and safe to
// call before that transaction commits.
type AccountRestrictionEventBuilder struct {
	pseudonymKey      []byte
	pseudonymKeyEpoch string
	newEventID        func() uuid.UUID
}

func NewAccountRestrictionEventBuilder(config RecorderConfig) (*AccountRestrictionEventBuilder, error) {
	config, err := validatedRecorderConfig(config)
	if err != nil {
		return nil, err
	}
	return &AccountRestrictionEventBuilder{
		pseudonymKey: config.PseudonymKey, pseudonymKeyEpoch: config.PseudonymKeyEpoch,
		newEventID: config.NewEventID,
	}, nil
}

func (builder *AccountRestrictionEventBuilder) BuildAccountRestrictionEvent(input identity.AccountRestrictionAuditInput) (auditevent.Event, error) {
	if err := validateAccountRestrictionAuditInput(input); err != nil {
		return auditevent.Event{}, err
	}
	eventID := builder.newEventID()
	if eventID == uuid.Nil {
		return auditevent.Event{}, errors.New("audit event id generator returned nil")
	}
	afterHash, err := accountRestrictionStateHash(input.After)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("hash account restriction after state: %w", err)
	}
	event := AccountRestrictionRecordedV1{
		SchemaVersion: AccountRestrictionSchemaVersion, EventType: AccountRestrictionEventType,
		EventID: eventID, OccurredAt: input.OccurredAt.UTC(), RestrictionID: input.RestrictionID,
		Transition:        string(input.Transition),
		ActorPseudonym:    subjectPseudonym(builder.pseudonymKey, input.ActorID),
		TargetPseudonym:   subjectPseudonym(builder.pseudonymKey, input.TargetUserID),
		PseudonymKeyEpoch: builder.pseudonymKeyEpoch,
		CommandReasonCode: input.CommandReasonCode, ReasonSHA256: digestLabel(input.Reason),
		ExpectedUserVersion:         input.ExpectedUserVersion,
		ResultingUserVersion:        input.After.UserAdministrationVersion,
		ExpectedRestrictionVersion:  input.ExpectedRestrictionVersion,
		ResultingRestrictionVersion: input.After.RestrictionVersion,
		AfterSHA256:                 afterHash, DecisionID: input.Authorization.ID,
		PolicyVersion: input.Authorization.PolicyVersion,
		Authority: DecisionAuthorityV1{
			RoleID: input.Authorization.RoleID, GrantID: input.Authorization.GrantID,
			GrantVersion: input.Authorization.GrantVersion, MandateID: input.Authorization.MandateID,
			EffectiveUntil: input.Authorization.EffectiveUntil.UTC(),
		},
	}
	if input.Before != nil {
		event.BeforeSHA256, err = accountRestrictionStateHash(*input.Before)
		if err != nil {
			return auditevent.Event{}, fmt.Errorf("hash account restriction before state: %w", err)
		}
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("encode account restriction event: %w", err)
	}
	if len(payload) > MaxEventPayloadBytes {
		return auditevent.Event{}, errors.New("account restriction event exceeds outbox limit")
	}
	digest := sha256.Sum256(payload)
	return auditevent.Event{
		ID: eventID, Type: AccountRestrictionEventType, SchemaVersion: AccountRestrictionSchemaVersion,
		OccurredAt: input.OccurredAt.UTC(), Payload: payload, PayloadSHA256: digest,
	}, nil
}

func validateAccountRestrictionAuditInput(input identity.AccountRestrictionAuditInput) error {
	if input.OccurredAt.IsZero() || input.ActorID == uuid.Nil || input.TargetUserID == uuid.Nil ||
		input.ActorID == input.TargetUserID || input.RestrictionID == uuid.Nil || input.Reason == "" ||
		input.ExpectedUserVersion < 1 || input.After.RestrictionID != input.RestrictionID ||
		input.After.Kind != string(identity.AccountRestrictionAccountAccess) ||
		input.After.ReasonCode == "" || input.After.ReasonSummary == "" ||
		input.After.UserAdministrationVersion != input.ExpectedUserVersion+1 ||
		input.After.RestrictionVersion < 1 || input.After.StartsAt.IsZero() || input.After.ExpiresAt.IsZero() ||
		!input.After.ExpiresAt.After(input.After.StartsAt) ||
		input.After.ExpiresAt.Sub(input.After.StartsAt) > 7*24*time.Hour {
		return errors.New("account restriction event is missing required metadata")
	}
	decision := input.Authorization
	if !decision.Allow || decision.Reason != authz.ReasonAllowed || decision.ID == uuid.Nil ||
		decision.PolicyVersion == "" || decision.GrantID == uuid.Nil || decision.GrantVersion < 1 ||
		decision.RoleID == "" || decision.MandateID == uuid.Nil || decision.EffectiveUntil.IsZero() {
		return errors.New("account restriction event is missing authorization evidence")
	}
	switch input.Transition {
	case identity.AccountRestrictionTransitionCreated:
		validCode := input.CommandReasonCode == string(identity.AccountRestrictionReasonManualReview) ||
			input.CommandReasonCode == string(identity.AccountRestrictionReasonSecurityIncident)
		if !validCode || input.Before != nil || input.ExpectedRestrictionVersion != 0 ||
			input.After.RestrictionVersion != 1 || input.After.RevokedAt != nil ||
			!input.After.StartsAt.Equal(input.OccurredAt) ||
			input.After.ReasonCode != input.CommandReasonCode || input.After.ReasonSummary != input.Reason ||
			input.After.RevocationReasonCode != "" || input.After.RevocationReason != "" {
			return errors.New("created account restriction transition is invalid")
		}
	case identity.AccountRestrictionTransitionRevoked:
		validCode := input.CommandReasonCode == string(identity.AccountRestrictionRevocationReviewCompleted) ||
			input.CommandReasonCode == string(identity.AccountRestrictionRevocationNoLongerRequired)
		if !validCode || input.Before == nil || input.ExpectedRestrictionVersion < 1 ||
			input.Before.RestrictionID != input.RestrictionID ||
			input.Before.RestrictionVersion != input.ExpectedRestrictionVersion ||
			input.Before.UserAdministrationVersion != input.ExpectedUserVersion ||
			input.Before.RevokedAt != nil || input.After.RevokedAt == nil ||
			!input.After.RevokedAt.Equal(input.OccurredAt) ||
			input.After.RestrictionVersion != input.ExpectedRestrictionVersion+1 ||
			input.After.RevocationReasonCode != input.CommandReasonCode ||
			input.After.RevocationReason != input.Reason || !sameRestrictionIdentity(*input.Before, input.After) {
			return errors.New("revoked account restriction transition is invalid")
		}
	default:
		return errors.New("account restriction event has an unknown transition")
	}
	return nil
}

func sameRestrictionIdentity(before, after identity.AccountRestrictionAuditState) bool {
	return before.RestrictionID == after.RestrictionID && before.Kind == after.Kind &&
		before.ReasonCode == after.ReasonCode && before.ReasonSummary == after.ReasonSummary &&
		before.StartsAt.Equal(after.StartsAt) && before.ExpiresAt.Equal(after.ExpiresAt)
}

func accountRestrictionStateHash(state identity.AccountRestrictionAuditState) (string, error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return digestLabel(string(encoded)), nil
}

var _ identity.AccountRestrictionEventBuilder = (*AccountRestrictionEventBuilder)(nil)
