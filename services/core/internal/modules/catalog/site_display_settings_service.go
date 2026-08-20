package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	maxSiteNameRunes         = 80
	maxSiteDescriptionRunes  = 500
	minSiteChangeReasonRunes = 10
	maxSiteChangeReasonRunes = 500
)

type SiteDisplaySettingsRepository interface {
	GetSiteDisplaySettings(context.Context) (SiteDisplaySettings, error)
	UpdateSiteDisplaySettings(context.Context, UpdateSiteDisplaySettingsCommand) (SiteDisplaySettings, error)
}

type SiteDisplaySettingsService struct {
	repository SiteDisplaySettingsRepository
	authorizer catalogStaffAuthorizer
	now        func() time.Time
}

func NewSiteDisplaySettingsService(repository SiteDisplaySettingsRepository, authorizer catalogStaffAuthorizer, now func() time.Time) (*SiteDisplaySettingsService, error) {
	if repository == nil || authorizer == nil {
		return nil, errors.New("site display settings dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &SiteDisplaySettingsService{repository: repository, authorizer: authorizer, now: now}, nil
}

func (service *SiteDisplaySettingsService) Get(ctx context.Context, actor authz.StaffActor) (SiteDisplaySettings, error) {
	now := service.now().UTC()
	if _, err := authorizeCatalogStaff(ctx, service.authorizer, actor, authz.ActionSiteDisplayManageRead, now, "catalog-site-display-settings"); err != nil {
		return SiteDisplaySettings{}, err
	}
	settings, err := service.repository.GetSiteDisplaySettings(ctx)
	if err != nil {
		return SiteDisplaySettings{}, fmt.Errorf("get site display settings: %w", err)
	}
	return settings, nil
}

func (service *SiteDisplaySettingsService) Update(ctx context.Context, actor authz.StaffActor, input UpdateSiteDisplaySettingsInput) (SiteDisplaySettings, error) {
	normalized, err := normalizeSiteDisplaySettingsInput(input)
	if err != nil {
		return SiteDisplaySettings{}, err
	}
	now := service.now().UTC()
	decision, err := authorizeCatalogStaff(ctx, service.authorizer, actor, authz.ActionSiteDisplayUpdate, now, "catalog-site-display-settings")
	if err != nil {
		return SiteDisplaySettings{}, err
	}
	settings, err := service.repository.UpdateSiteDisplaySettings(ctx, UpdateSiteDisplaySettingsCommand{
		UpdateSiteDisplaySettingsInput: normalized,
		ActorID:                        actor.Subject.ID, OccurredAt: now, Authorization: decision,
	})
	if err != nil {
		return SiteDisplaySettings{}, fmt.Errorf("update site display settings: %w", err)
	}
	return settings, nil
}

func normalizeSiteDisplaySettingsInput(input UpdateSiteDisplaySettingsInput) (UpdateSiteDisplaySettingsInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.Reason = strings.TrimSpace(input.Reason)
	nameRunes := utf8.RuneCountInString(input.Name)
	descriptionRunes := utf8.RuneCountInString(input.Description)
	reasonRunes := utf8.RuneCountInString(input.Reason)
	validView := input.DefaultTorrentView == TorrentViewList || input.DefaultTorrentView == TorrentViewPoster
	if !utf8.ValidString(input.Name) || !utf8.ValidString(input.Description) || !utf8.ValidString(input.Reason) ||
		nameRunes < 1 || nameRunes > maxSiteNameRunes || descriptionRunes > maxSiteDescriptionRunes ||
		reasonRunes < minSiteChangeReasonRunes || reasonRunes > maxSiteChangeReasonRunes ||
		input.ExpectedVersion < 1 || !validView {
		return UpdateSiteDisplaySettingsInput{}, ErrSiteDisplaySettingsInput
	}
	return input, nil
}
