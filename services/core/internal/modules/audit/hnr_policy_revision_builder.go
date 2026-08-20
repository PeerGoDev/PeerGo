package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/peergo/peergo/contracts/go/hnrpolicyv1"
	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/hnradmin"
)

type HNRPolicyRevisionEventBuilder struct {
	pseudonymKey      []byte
	pseudonymKeyEpoch string
	newEventID        func() uuid.UUID
}

func NewHNRPolicyRevisionEventBuilder(config RecorderConfig) (*HNRPolicyRevisionEventBuilder, error) {
	config, err := validatedRecorderConfig(config)
	if err != nil {
		return nil, err
	}
	return &HNRPolicyRevisionEventBuilder{
		pseudonymKey: config.PseudonymKey, pseudonymKeyEpoch: config.PseudonymKeyEpoch,
		newEventID: config.NewEventID,
	}, nil
}

func (builder *HNRPolicyRevisionEventBuilder) BuildHNRPolicyRevisionEvent(input hnradmin.RevisionAuditInput) (auditevent.Event, error) {
	revision := input.Revision
	decision := input.Authorization
	if revision.ID == uuid.Nil || revision.ActorID == uuid.Nil || revision.Reason == "" ||
		revision.CreatedAt.IsZero() || revision.EffectiveAt.IsZero() || hnrpolicyv1.Validate(revision.Policy) != nil ||
		!decision.Allow || decision.Reason != authz.ReasonAllowed || decision.ID == uuid.Nil ||
		decision.PolicyVersion == "" || decision.RoleID == "" || decision.GrantID == uuid.Nil ||
		decision.GrantVersion < 1 || decision.MandateID == uuid.Nil || decision.EffectiveUntil.IsZero() {
		return auditevent.Event{}, errors.New("H&R policy revision audit event is missing required metadata")
	}
	eventID := builder.newEventID()
	if eventID == uuid.Nil {
		return auditevent.Event{}, errors.New("audit event id generator returned nil")
	}
	payloadValue := HNRPolicyRevisionRecordedV1{
		SchemaVersion: HNRPolicyRevisionSchemaVersion, EventType: HNRPolicyRevisionEventType,
		EventID: eventID, OccurredAt: revision.CreatedAt.UTC(), RevisionID: revision.ID,
		RuleID: revision.Policy.Rule.ID, RuleVersion: revision.Policy.Rule.Version,
		Mode: string(revision.Policy.Mode), EffectiveAt: revision.EffectiveAt.UTC(),
		CommandSHA256: hex.EncodeToString(revision.CommandSHA256[:]), ReasonSHA256: digestLabel(revision.Reason),
		ActorPseudonym:    subjectPseudonym(builder.pseudonymKey, revision.ActorID),
		PseudonymKeyEpoch: builder.pseudonymKeyEpoch, DecisionID: decision.ID,
		PolicyVersion: decision.PolicyVersion,
		Authority: DecisionAuthorityV1{
			RoleID: decision.RoleID, GrantID: decision.GrantID, GrantVersion: decision.GrantVersion,
			MandateID: decision.MandateID, EffectiveUntil: decision.EffectiveUntil.UTC(),
		},
	}
	payload, err := json.Marshal(payloadValue)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("encode H&R policy revision audit event: %w", err)
	}
	if len(payload) > MaxEventPayloadBytes {
		return auditevent.Event{}, errors.New("H&R policy revision audit event exceeds outbox limit")
	}
	digest := sha256.Sum256(payload)
	return auditevent.Event{
		ID: eventID, Type: HNRPolicyRevisionEventType, SchemaVersion: HNRPolicyRevisionSchemaVersion,
		OccurredAt: revision.CreatedAt.UTC(), Payload: payload, PayloadSHA256: digest,
	}, nil
}

var _ hnradmin.RevisionEventBuilder = (*HNRPolicyRevisionEventBuilder)(nil)
