package promotions

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/peergo/peergo/contracts/go/promotioncontrolv1"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	DefaultListLimit = 50
	MaxListLimit     = 100
	minReasonRunes   = 10
	maxReasonRunes   = 1000
	minDuration      = 5 * time.Minute
	maxDuration      = 30 * 24 * time.Hour
)

type Repository interface {
	List(context.Context, int, int, time.Time) (Page, error)
	Schedule(context.Context, ScheduleCommand) (Campaign, error)
}

type Service struct {
	repository Repository
	authorizer authz.Authorizer
	now        func() time.Time
}

func NewService(repository Repository, authorizer authz.Authorizer, now func() time.Time) (*Service, error) {
	if repository == nil || authorizer == nil {
		return nil, errors.New("promotion service dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{repository: repository, authorizer: authorizer, now: now}, nil
}

func (service *Service) List(ctx context.Context, actor authz.StaffActor, limit, offset int) (Page, error) {
	if limit < 1 || limit > MaxListLimit || offset < 0 || offset > 1_000_000 {
		return Page{}, ErrInput
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionPromotionManageRead, authz.SiteScope(), now, "promotion-administration"); err != nil {
		return Page{}, err
	}
	return service.repository.List(ctx, limit, offset, now)
}

func (service *Service) Schedule(ctx context.Context, actor authz.StaffActor, input ScheduleInput) (Campaign, error) {
	now := service.now().UTC()
	input, err := normalizeScheduleInput(input, now)
	if err != nil {
		return Campaign{}, err
	}
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionPromotionSchedule, authz.SiteScope(), now, "promotion-administration")
	if err != nil {
		return Campaign{}, err
	}
	torrentID := input.TorrentID
	command := promotioncontrolv1.Command{
		SchemaVersion: promotioncontrolv1.SchemaVersion, CampaignID: input.CampaignID.String(),
		Scope: promotioncontrolv1.Scope(input.Scope), TorrentID: torrentID,
		Promotion: promotioncontrolv1.Promotion(input.Promotion),
		StartsAt:  input.StartsAt, EndsAt: input.EndsAt,
		OverrideLowerScopes: input.Scope == ScopeGlobal, ReasonCode: "staff_campaign",
	}
	encoded, err := promotioncontrolv1.Encode(command)
	if err != nil {
		return Campaign{}, ErrInput
	}
	digest, err := promotioncontrolv1.SHA256(encoded)
	if err != nil {
		return Campaign{}, ErrInput
	}
	return service.repository.Schedule(ctx, ScheduleCommand{
		ScheduleInput: input, ActorID: actor.Subject.ID, OccurredAt: now,
		Authorization: decision, CommandJSON: encoded, CommandSHA256: digest,
	})
}

func normalizeScheduleInput(input ScheduleInput, now time.Time) (ScheduleInput, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	input.StartsAt = input.StartsAt.UTC().Round(0)
	input.EndsAt = input.EndsAt.UTC().Round(0)
	reasonLength := utf8.RuneCountInString(input.Reason)
	if input.CampaignID == uuid.Nil || input.StartsAt.Before(now) ||
		input.EndsAt.Sub(input.StartsAt) < minDuration || input.EndsAt.Sub(input.StartsAt) > maxDuration ||
		!utf8.ValidString(input.Reason) || reasonLength < minReasonRunes || reasonLength > maxReasonRunes ||
		!validPromotion(input.Promotion) {
		return ScheduleInput{}, ErrInput
	}
	switch input.Scope {
	case ScopeGlobal:
		if input.TorrentID != nil {
			return ScheduleInput{}, ErrInput
		}
	case ScopeTorrent:
		if input.TorrentID == nil || *input.TorrentID < 1 {
			return ScheduleInput{}, ErrInput
		}
	default:
		return ScheduleInput{}, ErrInput
	}
	return input, nil
}

func validPromotion(value Promotion) bool {
	switch value {
	case PromotionFree, PromotionDoubleUpload, PromotionDoubleUploadFree,
		PromotionHalfDownload, PromotionDoubleUploadHalfDownload,
		PromotionThirtyPercentDownload:
		return true
	default:
		return false
	}
}
