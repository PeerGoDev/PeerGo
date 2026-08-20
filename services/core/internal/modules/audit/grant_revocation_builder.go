package audit

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

// GrantRevocationEventBuilder owns the reviewed audit JSON contract while the
// authz repository owns transaction ordering. It is pure and contains no
// persistence handle, so it is safe to invoke after rows are locked.
type GrantRevocationEventBuilder struct {
	pseudonymKey      []byte
	pseudonymKeyEpoch string
	newEventID        func() uuid.UUID
}

func NewGrantRevocationEventBuilder(config RecorderConfig) (*GrantRevocationEventBuilder, error) {
	config, err := validatedRecorderConfig(config)
	if err != nil {
		return nil, err
	}
	return &GrantRevocationEventBuilder{
		pseudonymKey:      config.PseudonymKey,
		pseudonymKeyEpoch: config.PseudonymKeyEpoch,
		newEventID:        config.NewEventID,
	}, nil
}

func (builder *GrantRevocationEventBuilder) BuildGrantRevocationEvent(input authz.GrantRevocationAuditInput) (auditevent.Event, error) {
	if err := validateGrantRevocationAuditInput(input); err != nil {
		return auditevent.Event{}, err
	}
	eventID := builder.newEventID()
	if eventID == uuid.Nil {
		return auditevent.Event{}, errors.New("audit event id generator returned nil")
	}
	beforeHash, err := canonicalStateHash(input.Before)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("hash grant revocation before state: %w", err)
	}
	afterHash, err := canonicalStateHash(input.After)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("hash grant revocation after state: %w", err)
	}
	event := GrantRevocationRecordedV1{
		SchemaVersion:         GrantRevocationSchemaVersion,
		EventType:             GrantRevocationEventType,
		EventID:               eventID,
		OccurredAt:            input.OccurredAt.UTC(),
		RequestID:             input.RequestID,
		GrantID:               input.GrantID,
		ExpectedGrantVersion:  input.ExpectedGrantVersion,
		ResultingGrantVersion: input.ResultingGrantVersion,
		ActorPseudonym:        subjectPseudonym(builder.pseudonymKey, input.ActorID),
		TargetPseudonym:       subjectPseudonym(builder.pseudonymKey, input.TargetSubjectID),
		PseudonymKeyEpoch:     builder.pseudonymKeyEpoch,
		Transition:            string(input.Transition),
		ReasonSHA256:          digestLabel(input.Reason),
		BeforeSHA256:          beforeHash,
		AfterSHA256:           afterHash,
		DecisionID:            input.Authorization.ID,
		PolicyVersion:         input.Authorization.PolicyVersion,
		Authority: DecisionAuthorityV1{
			RoleID:         input.Authorization.RoleID,
			GrantID:        input.Authorization.GrantID,
			GrantVersion:   input.Authorization.GrantVersion,
			MandateID:      input.Authorization.MandateID,
			EffectiveUntil: input.Authorization.EffectiveUntil.UTC(),
		},
	}
	if input.ReviewID != uuid.Nil {
		event.Review = &GrantRevocationReviewV1{
			ID:       input.ReviewID,
			Domain:   string(input.ReviewDomain),
			Decision: string(input.ReviewDecision),
		}
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("encode grant revocation event: %w", err)
	}
	if len(payload) > MaxEventPayloadBytes {
		return auditevent.Event{}, errors.New("grant revocation event exceeds outbox limit")
	}
	digest := sha256.Sum256(payload)
	return auditevent.Event{
		ID:            eventID,
		Type:          GrantRevocationEventType,
		SchemaVersion: GrantRevocationSchemaVersion,
		OccurredAt:    input.OccurredAt.UTC(),
		Payload:       payload,
		PayloadSHA256: digest,
	}, nil
}

func validateGrantRevocationAuditInput(input authz.GrantRevocationAuditInput) error {
	if input.OccurredAt.IsZero() || input.RequestID == uuid.Nil || input.GrantID == uuid.Nil || input.ExpectedGrantVersion < 1 || input.ActorID == uuid.Nil || input.TargetSubjectID == uuid.Nil || input.ActorID == input.TargetSubjectID || input.Reason == "" {
		return errors.New("grant revocation event is missing required metadata")
	}
	decision := input.Authorization
	if !decision.Allow || decision.Reason != authz.ReasonAllowed || decision.ID == uuid.Nil || decision.PolicyVersion == "" || decision.GrantID == uuid.Nil || decision.GrantVersion < 1 || decision.RoleID == "" || decision.MandateID == uuid.Nil || decision.EffectiveUntil.IsZero() {
		return errors.New("grant revocation event is missing authorization evidence")
	}
	reviewRequired := false
	switch input.Transition {
	case authz.GrantTransitionProposed:
		if input.Before.Status != "" || input.After.Status != authz.GrantRevocationPendingStatus || input.Before.GrantVersion != input.After.GrantVersion || input.After.GrantRevoked {
			return errors.New("proposed transition has invalid state")
		}
	case authz.GrantTransitionConflicted:
		if input.Before.Status != authz.GrantRevocationPendingStatus || input.After.Status != authz.GrantRevocationConflictedStatus {
			return errors.New("conflicted transition has invalid state")
		}
	case authz.GrantTransitionExpired:
		if input.Before.Status != authz.GrantRevocationPendingStatus || input.After.Status != authz.GrantRevocationExpiredStatus {
			return errors.New("expired transition has invalid state")
		}
	case authz.GrantTransitionGovernanceApproved:
		reviewRequired = true
		if input.ReviewDomain != authz.GrantReviewGovernance || input.ReviewDecision != authz.GrantReviewApprove || input.Before.Status != authz.GrantRevocationPendingStatus || input.After.Status != authz.GrantRevocationPendingStatus || input.After.GovernanceDecision != authz.GrantReviewApprove {
			return errors.New("governance approval transition has invalid review evidence")
		}
	case authz.GrantTransitionSecurityApproved:
		reviewRequired = true
		if input.ReviewDomain != authz.GrantReviewSecurity || input.ReviewDecision != authz.GrantReviewApprove || input.Before.Status != authz.GrantRevocationPendingStatus || input.After.Status != authz.GrantRevocationPendingStatus || input.After.SecurityDecision != authz.GrantReviewApprove {
			return errors.New("security approval transition has invalid review evidence")
		}
	case authz.GrantTransitionRejected:
		reviewRequired = true
		if input.ReviewDecision != authz.GrantReviewReject || input.Before.Status != authz.GrantRevocationPendingStatus || input.After.Status != authz.GrantRevocationRejectedStatus {
			return errors.New("rejection transition has invalid review evidence")
		}
	case authz.GrantTransitionApplied:
		reviewRequired = true
		if input.ReviewDecision != authz.GrantReviewApprove || input.ResultingGrantVersion != input.ExpectedGrantVersion+1 || input.After.Status != authz.GrantRevocationAppliedStatus || !input.After.GrantRevoked {
			return errors.New("applied transition has invalid resulting state")
		}
	default:
		return errors.New("grant revocation event has an unknown transition")
	}
	if reviewRequired {
		if input.ReviewID == uuid.Nil || (input.ReviewDomain != authz.GrantReviewGovernance && input.ReviewDomain != authz.GrantReviewSecurity) || (input.ReviewDecision != authz.GrantReviewApprove && input.ReviewDecision != authz.GrantReviewReject) {
			return errors.New("grant revocation event is missing review evidence")
		}
	} else if input.ReviewID != uuid.Nil || input.ReviewDomain != "" || input.ReviewDecision != "" {
		return errors.New("grant revocation event contains unexpected review evidence")
	}
	if input.Transition != authz.GrantTransitionApplied && input.ResultingGrantVersion != 0 {
		return errors.New("non-applied grant revocation event has a resulting version")
	}
	if input.Before.GrantVersion < 1 || input.After.GrantVersion < 1 {
		return errors.New("grant revocation event has an invalid state version")
	}
	return nil
}

func canonicalStateHash(state authz.GrantRevocationAuditState) (string, error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return digestLabel(string(encoded)), nil
}

var _ authz.GrantRevocationEventBuilder = (*GrantRevocationEventBuilder)(nil)
