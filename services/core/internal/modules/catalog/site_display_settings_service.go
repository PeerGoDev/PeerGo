package catalog

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	maxSiteNameRunes         = 80
	maxSiteDescriptionRunes  = 500
	maxTorrentFilenamePrefix = 40
	maxCustomNavigationItems = 12
	maxCustomNavigationLabel = 32
	maxCustomNavigationURL   = 2048
	minSiteChangeReasonRunes = 10
	maxSiteChangeReasonRunes = 500
	defaultSiteChangeReason  = "更新站点与展示设置。"
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
	input.TorrentFilenamePrefix = strings.TrimSpace(input.TorrentFilenamePrefix)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" {
		input.Reason = defaultSiteChangeReason
	}
	customNavigationItems, err := normalizeCustomNavigationItems(input.CustomNavigationItems)
	if err != nil {
		return UpdateSiteDisplaySettingsInput{}, ErrSiteDisplaySettingsInput
	}
	input.CustomNavigationItems = customNavigationItems
	nameRunes := utf8.RuneCountInString(input.Name)
	descriptionRunes := utf8.RuneCountInString(input.Description)
	prefixRunes := utf8.RuneCountInString(input.TorrentFilenamePrefix)
	reasonRunes := utf8.RuneCountInString(input.Reason)
	validView := input.DefaultTorrentView == TorrentViewList || input.DefaultTorrentView == TorrentViewPoster
	if !utf8.ValidString(input.Name) || !utf8.ValidString(input.Description) || !utf8.ValidString(input.TorrentFilenamePrefix) || !utf8.ValidString(input.Reason) ||
		nameRunes < 1 || nameRunes > maxSiteNameRunes || descriptionRunes > maxSiteDescriptionRunes ||
		prefixRunes > maxTorrentFilenamePrefix || !validTorrentFilenamePrefix(input.TorrentFilenamePrefix) ||
		reasonRunes < minSiteChangeReasonRunes || reasonRunes > maxSiteChangeReasonRunes ||
		input.ExpectedVersion < 1 || !validView {
		return UpdateSiteDisplaySettingsInput{}, ErrSiteDisplaySettingsInput
	}
	return input, nil
}

func normalizeCustomNavigationItems(items []CustomNavigationItem) ([]CustomNavigationItem, error) {
	if len(items) > maxCustomNavigationItems {
		return nil, ErrSiteDisplaySettingsInput
	}
	normalized := make([]CustomNavigationItem, 0, len(items))
	labels := make(map[string]struct{}, len(items))
	urls := make(map[string]struct{}, len(items))
	for _, item := range items {
		item.Label = strings.TrimSpace(item.Label)
		item.URL = strings.TrimSpace(item.URL)
		labelRunes := utf8.RuneCountInString(item.Label)
		urlRunes := utf8.RuneCountInString(item.URL)
		labelKey := strings.ToLower(item.Label)
		if !utf8.ValidString(item.Label) || !utf8.ValidString(item.URL) ||
			labelRunes < 1 || labelRunes > maxCustomNavigationLabel ||
			urlRunes < 1 || urlRunes > maxCustomNavigationURL ||
			!validCustomNavigationURL(item.URL) {
			return nil, ErrSiteDisplaySettingsInput
		}
		if _, duplicate := labels[labelKey]; duplicate {
			return nil, ErrSiteDisplaySettingsInput
		}
		if _, duplicate := urls[item.URL]; duplicate {
			return nil, ErrSiteDisplaySettingsInput
		}
		labels[labelKey] = struct{}{}
		urls[item.URL] = struct{}{}
		normalized = append(normalized, item)
	}
	return normalized, nil
}

func validCustomNavigationURL(value string) bool {
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.User != nil || strings.Contains(value, "\\") {
		return false
	}
	if strings.HasPrefix(value, "/") {
		return !strings.HasPrefix(value, "//") && parsed.Host == "" && !parsed.IsAbs()
	}
	return parsed.Scheme == "https" && parsed.Host != ""
}

func validTorrentFilenamePrefix(prefix string) bool {
	for _, character := range prefix {
		if unicode.IsControl(character) || strings.ContainsRune(`/\\:*?"<>|`, character) {
			return false
		}
	}
	return prefix == "" || strings.Trim(prefix, ".") != ""
}
