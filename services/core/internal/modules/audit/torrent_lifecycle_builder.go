package audit

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

type TorrentLifecycleEventBuilder struct {
	pseudonymKey      []byte
	pseudonymKeyEpoch string
	newEventID        func() uuid.UUID
}

func NewTorrentLifecycleEventBuilder(config RecorderConfig) (*TorrentLifecycleEventBuilder, error) {
	config, err := validatedRecorderConfig(config)
	if err != nil {
		return nil, err
	}
	return &TorrentLifecycleEventBuilder{
		pseudonymKey: config.PseudonymKey, pseudonymKeyEpoch: config.PseudonymKeyEpoch, newEventID: config.NewEventID,
	}, nil
}

func (builder *TorrentLifecycleEventBuilder) BuildTorrentLifecycleEvent(input torrents.TorrentLifecycleAuditInput) (auditevent.Event, error) {
	if err := validateTorrentLifecycleAuditInput(input); err != nil {
		return auditevent.Event{}, err
	}
	eventID := builder.newEventID()
	if eventID == uuid.Nil {
		return auditevent.Event{}, errors.New("audit event id generator returned nil")
	}
	beforeHash, err := torrentLifecycleStateHash(input.Before)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("hash torrent lifecycle before state: %w", err)
	}
	afterHash, err := torrentLifecycleStateHash(input.After)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("hash torrent lifecycle after state: %w", err)
	}
	decision := input.Authorization
	payloadValue := TorrentLifecycleChangeRecordedV1{
		SchemaVersion: TorrentLifecycleSchemaVersion, EventType: TorrentLifecycleEventType,
		EventID: eventID, OccurredAt: input.OccurredAt.UTC(), ChangeID: input.ChangeID,
		TorrentID: int64(input.Before.TorrentID), Action: string(input.Action), ReasonSHA256: digestLabel(input.Reason),
		ActorPseudonym: subjectPseudonym(builder.pseudonymKey, input.ActorID), PseudonymKeyEpoch: builder.pseudonymKeyEpoch,
		ExpectedVersion: input.Before.Version, ResultingVersion: input.After.Version,
		BeforeSHA256: beforeHash, AfterSHA256: afterHash, AuthorizationDecisionID: decision.ID,
		PolicyVersion: decision.PolicyVersion, Authority: DecisionAuthorityV1{
			RoleID: decision.RoleID, GrantID: decision.GrantID, GrantVersion: decision.GrantVersion,
			MandateID: decision.MandateID, EffectiveUntil: decision.EffectiveUntil.UTC(),
		},
	}
	payload, err := json.Marshal(payloadValue)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("encode torrent lifecycle audit event: %w", err)
	}
	if len(payload) > MaxEventPayloadBytes {
		return auditevent.Event{}, errors.New("torrent lifecycle audit event exceeds outbox limit")
	}
	digest := sha256.Sum256(payload)
	return auditevent.Event{
		ID: eventID, Type: TorrentLifecycleEventType, SchemaVersion: TorrentLifecycleSchemaVersion,
		OccurredAt: input.OccurredAt.UTC(), Payload: payload, PayloadSHA256: digest,
	}, nil
}

func validateTorrentLifecycleAuditInput(input torrents.TorrentLifecycleAuditInput) error {
	decision := input.Authorization
	if input.ChangeID == uuid.Nil || input.ActorID == uuid.Nil || input.Reason == "" || input.OccurredAt.IsZero() ||
		input.Before.TorrentID < 1 || input.Before.TorrentID != input.After.TorrentID || input.Before.Version < 1 ||
		input.After.Version != input.Before.Version+1 || !decision.Allow || decision.Reason != authz.ReasonAllowed ||
		decision.ID == uuid.Nil || decision.PolicyVersion == "" || decision.GrantID == uuid.Nil || decision.GrantVersion < 1 ||
		decision.RoleID == "" || decision.MandateID == uuid.Nil || decision.EffectiveUntil.IsZero() {
		return errors.New("torrent lifecycle audit event is missing required metadata")
	}
	if input.Action == torrents.TorrentAvailabilityDisable && input.Before.State == torrents.StatePublished &&
		input.Before.TrackerEligible && input.After.State == torrents.StateDisabled && !input.After.TrackerEligible {
		return nil
	}
	if input.Action == torrents.TorrentAvailabilityRestore && input.Before.State == torrents.StateDisabled &&
		!input.Before.TrackerEligible && input.After.State == torrents.StatePublished && input.After.TrackerEligible {
		return nil
	}
	if input.Action == torrents.TorrentAvailabilityWithdrawRequest && input.Before.State == torrents.StatePublished &&
		input.Before.TrackerEligible && input.After.State == torrents.StateDisabled && !input.After.TrackerEligible {
		return nil
	}
	if input.Action == torrents.TorrentAvailabilityWithdrawApprove && input.Before.State == torrents.StateDisabled &&
		!input.Before.TrackerEligible && input.After.State == torrents.StateDeleted && !input.After.TrackerEligible {
		return nil
	}
	if input.Action == torrents.TorrentAvailabilityWithdrawReject && input.Before.State == torrents.StateDisabled &&
		!input.Before.TrackerEligible && input.After.State == torrents.StatePublished && input.After.TrackerEligible {
		return nil
	}
	if input.Action == torrents.TorrentAvailabilityReportDisable && input.Before.State == torrents.StatePublished &&
		input.Before.TrackerEligible && input.After.State == torrents.StateDisabled && !input.After.TrackerEligible {
		return nil
	}
	return errors.New("torrent lifecycle audit event has an invalid transition")
}

func torrentLifecycleStateHash(state torrents.TorrentLifecycleAuditState) (string, error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return digestLabel(string(encoded)), nil
}

var _ torrents.TorrentLifecycleEventBuilder = (*TorrentLifecycleEventBuilder)(nil)
