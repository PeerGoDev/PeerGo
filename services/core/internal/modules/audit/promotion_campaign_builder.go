package audit

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/promotions"
)

type PromotionCampaignEventBuilder struct {
	pseudonymKey      []byte
	pseudonymKeyEpoch string
	newEventID        func() uuid.UUID
}

func NewPromotionCampaignEventBuilder(config RecorderConfig) (*PromotionCampaignEventBuilder, error) {
	config, err := validatedRecorderConfig(config)
	if err != nil {
		return nil, err
	}
	return &PromotionCampaignEventBuilder{
		pseudonymKey: config.PseudonymKey, pseudonymKeyEpoch: config.PseudonymKeyEpoch, newEventID: config.NewEventID,
	}, nil
}

func (builder *PromotionCampaignEventBuilder) BuildPromotionCampaignEvent(input promotions.CampaignAuditInput) (auditevent.Event, error) {
	campaign := input.Campaign
	decision := input.Authorization
	if campaign.ID == uuid.Nil || campaign.ActorID == uuid.Nil || campaign.Reason == "" || campaign.CreatedAt.IsZero() ||
		campaign.StartsAt.IsZero() || !campaign.EndsAt.After(campaign.StartsAt) || !decision.Allow ||
		decision.Reason != authz.ReasonAllowed || decision.ID == uuid.Nil || decision.PolicyVersion == "" ||
		decision.RoleID == "" || decision.GrantID == uuid.Nil || decision.GrantVersion < 1 ||
		decision.MandateID == uuid.Nil || decision.EffectiveUntil.IsZero() {
		return auditevent.Event{}, errors.New("promotion campaign audit event is missing required metadata")
	}
	eventID := builder.newEventID()
	if eventID == uuid.Nil {
		return auditevent.Event{}, errors.New("audit event id generator returned nil")
	}
	payloadValue := PromotionCampaignRecordedV1{
		SchemaVersion: PromotionCampaignSchemaVersion, EventType: PromotionCampaignEventType,
		EventID: eventID, OccurredAt: campaign.CreatedAt.UTC(), CampaignID: campaign.ID,
		Scope: string(campaign.Scope), TorrentID: campaign.TorrentID, Promotion: string(campaign.Promotion),
		StartsAt: campaign.StartsAt.UTC(), EndsAt: campaign.EndsAt.UTC(), OverrideLowerScope: campaign.OverrideLowerScopes,
		ReasonSHA256: digestLabel(campaign.Reason), ActorPseudonym: subjectPseudonym(builder.pseudonymKey, campaign.ActorID),
		PseudonymKeyEpoch: builder.pseudonymKeyEpoch, DecisionID: decision.ID, PolicyVersion: decision.PolicyVersion,
		Authority: DecisionAuthorityV1{
			RoleID: decision.RoleID, GrantID: decision.GrantID, GrantVersion: decision.GrantVersion,
			MandateID: decision.MandateID, EffectiveUntil: decision.EffectiveUntil.UTC(),
		},
	}
	payload, err := json.Marshal(payloadValue)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("encode promotion campaign audit event: %w", err)
	}
	if len(payload) > MaxEventPayloadBytes {
		return auditevent.Event{}, errors.New("promotion campaign audit event exceeds outbox limit")
	}
	digest := sha256.Sum256(payload)
	return auditevent.Event{
		ID: eventID, Type: PromotionCampaignEventType, SchemaVersion: PromotionCampaignSchemaVersion,
		OccurredAt: campaign.CreatedAt.UTC(), Payload: payload, PayloadSHA256: digest,
	}, nil
}

var _ promotions.CampaignEventBuilder = (*PromotionCampaignEventBuilder)(nil)
