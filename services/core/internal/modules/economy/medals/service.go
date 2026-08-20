package medals

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	minimumReasonRunes = 10
	maximumReasonRunes = 500
	maximumImageRunes  = 2048
)

type Repository interface {
	Overview(context.Context) (Overview, error)
	UpdateSettings(context.Context, UpdateSettingsCommand) (Settings, error)
	Create(context.Context, CreateCommand) (Definition, error)
	Update(context.Context, UpdateCommand) (Definition, error)
}

func (service *Service) UpdateSettings(ctx context.Context, actor authz.StaffActor, input SettingsInput) (Settings, error) {
	normalized, err := normalizeSettingsInput(input)
	if err != nil {
		return Settings{}, err
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionEconomyMedalUpdate, authz.SiteScope(), now, "economy-medal-settings")
	if err != nil {
		return Settings{}, err
	}
	return service.repository.UpdateSettings(ctx, UpdateSettingsCommand{
		SettingsInput: normalized, ActorID: actor.Subject.ID,
		OccurredAt: now, Authorization: decision,
	})
}

type Service struct {
	repository Repository
	authorizer authz.Authorizer
	now        func() time.Time
}

func NewService(repository Repository, authorizer authz.Authorizer, now func() time.Time) (*Service, error) {
	if repository == nil || authorizer == nil {
		return nil, errors.New("medal administration dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{repository: repository, authorizer: authorizer, now: now}, nil
}

func (service *Service) Overview(ctx context.Context, actor authz.StaffActor) (Overview, error) {
	now := service.now().UTC()
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionEconomyMedalManageRead, authz.SiteScope(), now, "economy-medal-administration"); err != nil {
		return Overview{}, err
	}
	return service.repository.Overview(ctx)
}

func (service *Service) Create(ctx context.Context, actor authz.StaffActor, input DefinitionInput) (Definition, error) {
	normalized, err := normalizeInput(input)
	if err != nil {
		return Definition{}, err
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionEconomyMedalCreate, authz.SiteScope(), now, "economy-medal-administration")
	if err != nil {
		return Definition{}, err
	}
	return service.repository.Create(ctx, CreateCommand{
		DefinitionInput: normalized, ActorID: actor.Subject.ID,
		OccurredAt: now, Authorization: decision,
	})
}

func (service *Service) Update(ctx context.Context, actor authz.StaffActor, id, expectedVersion int64, input DefinitionInput) (Definition, error) {
	normalized, err := normalizeInput(input)
	if err != nil || id < 1 || expectedVersion < 1 {
		return Definition{}, ErrInput
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionEconomyMedalUpdate, authz.SiteScope(), now, "economy-medal-administration")
	if err != nil {
		return Definition{}, err
	}
	return service.repository.Update(ctx, UpdateCommand{
		ID: id, DefinitionInput: normalized, ExpectedVersion: expectedVersion,
		ActorID: actor.Subject.ID, OccurredAt: now, Authorization: decision,
	})
}

func normalizeInput(input DefinitionInput) (DefinitionInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Reason = strings.TrimSpace(input.Reason)
	input.Description = normalizedOptionalText(input.Description)
	input.RewardCycle = normalizedOptionalText(input.RewardCycle)
	var err error
	if input.ImageLargePath, err = normalizeImagePath(input.ImageLargePath); err != nil {
		return DefinitionInput{}, ErrInput
	}
	if input.ImageSmallPath, err = normalizeImagePath(input.ImageSmallPath); err != nil {
		return DefinitionInput{}, ErrInput
	}
	if !validAcquisitionMethod(input.AcquisitionMethod) ||
		!utf8.ValidString(input.Name) || utf8.RuneCountInString(input.Name) < 1 || utf8.RuneCountInString(input.Name) > 100 ||
		(input.Description != nil && (!utf8.ValidString(*input.Description) || utf8.RuneCountInString(*input.Description) > 500)) ||
		!utf8.ValidString(input.Reason) || utf8.RuneCountInString(input.Reason) < minimumReasonRunes || utf8.RuneCountInString(input.Reason) > maximumReasonRunes ||
		input.Price < 0 || input.DurationDays < 0 || input.DurationDays > 36500 ||
		input.Priority < 0 || input.Priority > 1_000_000 ||
		!validBPS(input.UploadBonusBPS) || !validBPS(input.DownloadDiscountBPS) || !validBPS(input.MagicBonusBPS) ||
		input.InviteBonus < 0 || input.PeriodicRewardMagic < 0 ||
		(input.Inventory != nil && *input.Inventory < 0) || !validRewardCycle(input.RewardCycle) ||
		(input.SaleBeginAt != nil && input.SaleEndAt != nil && !input.SaleBeginAt.Before(*input.SaleEndAt)) {
		return DefinitionInput{}, ErrInput
	}
	input.SaleBeginAt = normalizedTime(input.SaleBeginAt)
	input.SaleEndAt = normalizedTime(input.SaleEndAt)
	return input, nil
}

func normalizeSettingsInput(input SettingsInput) (SettingsInput, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if input.ExpectedVersion < 1 || input.MaximumWearCount < 0 || input.MaximumWearCount > 100 ||
		!validBPS(input.MaximumUploadBonusBPS) || !validBPS(input.MaximumDownloadDiscountBPS) ||
		!validBPS(input.MaximumMagicBonusBPS) || input.MaximumInviteBonus < 0 || input.MaximumInviteBonus > 1_000_000 ||
		!utf8.ValidString(input.Reason) || utf8.RuneCountInString(input.Reason) < minimumReasonRunes ||
		utf8.RuneCountInString(input.Reason) > maximumReasonRunes {
		return SettingsInput{}, ErrInput
	}
	return input, nil
}

func normalizedOptionalText(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil
	}
	return &normalized
}

// Image paths are only stored, never fetched by Core. Restricting them to a
// same-origin path or HTTPS URL prevents javascript/data schemes and makes a
// later local image-upload endpoint compatible with the same fields.
func normalizeImagePath(value *string) (*string, error) {
	value = normalizedOptionalText(value)
	if value == nil {
		return nil, nil
	}
	if !utf8.ValidString(*value) || utf8.RuneCountInString(*value) > maximumImageRunes || strings.ContainsAny(*value, "\\\r\n\x00") {
		return nil, ErrInput
	}
	parsed, err := url.Parse(*value)
	if err != nil || parsed.Fragment != "" || parsed.User != nil {
		return nil, ErrInput
	}
	if parsed.IsAbs() {
		if parsed.Scheme != "https" || parsed.Host == "" {
			return nil, ErrInput
		}
		return value, nil
	}
	if !strings.HasPrefix(*value, "/") || strings.HasPrefix(*value, "//") || strings.Contains(parsed.Path, "..") {
		return nil, ErrInput
	}
	return value, nil
}

func validAcquisitionMethod(value AcquisitionMethod) bool {
	switch value {
	case AcquisitionPurchase, AcquisitionGrant, AcquisitionSponsor, AcquisitionWorkgroup, AcquisitionDeveloper:
		return true
	default:
		return false
	}
}

func validBPS(value int64) bool { return value >= 0 && value <= 100000 }

func validRewardCycle(value *string) bool {
	if value == nil {
		return true
	}
	switch *value {
	case "daily", "weekly", "monthly":
		return true
	default:
		return false
	}
}

func normalizedTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC().Truncate(time.Microsecond)
	return &normalized
}
