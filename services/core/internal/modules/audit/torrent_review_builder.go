package audit

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/review"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

type TorrentReviewEventBuilder struct {
	pseudonymKey      []byte
	pseudonymKeyEpoch string
	newEventID        func() uuid.UUID
}

func NewTorrentReviewEventBuilder(config RecorderConfig) (*TorrentReviewEventBuilder, error) {
	config, err := validatedRecorderConfig(config)
	if err != nil {
		return nil, err
	}
	return &TorrentReviewEventBuilder{
		pseudonymKey: config.PseudonymKey, pseudonymKeyEpoch: config.PseudonymKeyEpoch,
		newEventID: config.NewEventID,
	}, nil
}

func (builder *TorrentReviewEventBuilder) BuildTorrentReviewEvent(input review.TorrentReviewAuditInput) (auditevent.Event, error) {
	if err := validateTorrentReviewAuditInput(input); err != nil {
		return auditevent.Event{}, err
	}
	eventID := builder.newEventID()
	if eventID == uuid.Nil {
		return auditevent.Event{}, errors.New("audit event id generator returned nil")
	}
	beforeHash, err := torrentReviewStateHash(input.Before)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("hash torrent review before state: %w", err)
	}
	afterHash, err := torrentReviewStateHash(input.After)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("hash torrent review after state: %w", err)
	}
	event := TorrentReviewRecordedV2{
		SchemaVersion: TorrentReviewSchemaVersion, EventType: TorrentReviewEventType,
		EventID: eventID, OccurredAt: input.OccurredAt.UTC(), ReviewDecisionID: input.DecisionID,
		TorrentID: int64(input.Before.TorrentID), ReviewDecision: string(input.Decision),
		ReasonCode: string(input.ReasonCode), ReasonSHA256: digestLabel(input.Reason),
		ReviewerPseudonym: subjectPseudonym(builder.pseudonymKey, input.ReviewerID),
		UploaderPseudonym: subjectPseudonym(builder.pseudonymKey, input.UploaderID),
		PseudonymKeyEpoch: builder.pseudonymKeyEpoch, ExpectedVersion: input.Before.Version,
		ResultingVersion: input.After.Version, BeforeSHA256: beforeHash, AfterSHA256: afterHash,
		AuthorizationDecisionID: input.Authorization.ID, PolicyVersion: input.Authorization.PolicyVersion,
		Authority: DecisionAuthorityV1{
			RoleID: input.Authorization.RoleID, GrantID: input.Authorization.GrantID,
			GrantVersion: input.Authorization.GrantVersion, MandateID: input.Authorization.MandateID,
			EffectiveUntil: input.Authorization.EffectiveUntil.UTC(),
		},
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("encode torrent review audit event: %w", err)
	}
	if len(payload) > MaxEventPayloadBytes {
		return auditevent.Event{}, errors.New("torrent review audit event exceeds outbox limit")
	}
	digest := sha256.Sum256(payload)
	return auditevent.Event{
		ID: eventID, Type: TorrentReviewEventType, SchemaVersion: TorrentReviewSchemaVersion,
		OccurredAt: input.OccurredAt.UTC(), Payload: payload, PayloadSHA256: digest,
	}, nil
}

func validateTorrentReviewAuditInput(input review.TorrentReviewAuditInput) error {
	decision := input.Authorization
	if input.DecisionID == uuid.Nil || input.ReviewerID == uuid.Nil || input.UploaderID == uuid.Nil ||
		input.ReviewerID == input.UploaderID || input.Reason == "" || input.OccurredAt.IsZero() ||
		input.Before.TorrentID < 1 || input.Before.TorrentID != input.After.TorrentID ||
		input.Before.State != torrents.StatePendingReview || input.Before.Version < 1 ||
		input.After.Version != input.Before.Version+1 || !decision.Allow || decision.Reason != authz.ReasonAllowed ||
		decision.ID == uuid.Nil || decision.PolicyVersion == "" || decision.GrantID == uuid.Nil ||
		decision.GrantVersion < 1 || decision.RoleID == "" || decision.MandateID == uuid.Nil || decision.EffectiveUntil.IsZero() {
		return errors.New("torrent review audit event is missing required metadata")
	}
	if (input.Decision == review.DecisionApprove && input.ReasonCode == review.ReasonMeetsRequirements && input.After.State == torrents.StatePublished) ||
		(input.Decision == review.DecisionReject && input.ReasonCode != review.ReasonMeetsRequirements && input.After.State == torrents.StateRejected) {
		return nil
	}
	return errors.New("torrent review audit event has an invalid transition")
}

func torrentReviewStateHash(state review.TorrentReviewAuditState) (string, error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return digestLabel(string(encoded)), nil
}

var _ review.AuditEventBuilder = (*TorrentReviewEventBuilder)(nil)
