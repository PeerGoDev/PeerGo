package catalog

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultTorrentLimit      = 20
	maxTorrentLimit          = 50
	maxTorrentOffset         = 1_000_000
	maxQueryRunes            = 100
	swarmFreshness           = 5 * time.Minute
	defaultAnnouncementLimit = 20
	maxAnnouncementLimit     = 50
	maxAnnouncementOffset    = 1_000_000
)

var (
	// ErrInvalidLimit means a caller tried to bypass the bounded list contract.
	ErrInvalidLimit = errors.New("torrent limit must be between 1 and 50")
	// ErrInvalidQuery means a caller provided a search term outside the contract.
	ErrInvalidQuery = errors.New("torrent query must be at most 100 characters")
	// ErrInvalidTorrentPage keeps the catalog list bounded before storage.
	ErrInvalidTorrentPage = errors.New("torrent page is outside the public bounds")
	// ErrInvalidTorrentFilter rejects values outside the stable category and
	// public-promotion vocabulary rather than weakening them in an adapter.
	ErrInvalidTorrentFilter = errors.New("torrent filter is invalid")
	// ErrTorrentNotFound keeps unpublished or unknown catalog records
	// indistinguishable at the public boundary.
	ErrTorrentNotFound = errors.New("published torrent was not found")
	// ErrAnnouncementNotFound deliberately covers unknown, draft and scheduled
	// announcements so the public boundary does not disclose editorial state.
	ErrAnnouncementNotFound = errors.New("published announcement was not found")
	// ErrInvalidAnnouncementPage keeps the public list bounded before storage.
	ErrInvalidAnnouncementPage = errors.New("announcement page is outside the public bounds")
)

var (
	announcementRouteKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,119}$`)
	categoryIDPattern           = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
)

// ValidAnnouncementID is shared by target-owning adapters such as social.
// The catalog module remains the single owner of announcement route-key rules.
func ValidAnnouncementID(value string) bool {
	return value == strings.TrimSpace(value) && announcementRouteKeyPattern.MatchString(value)
}

// Repository is the catalog persistence boundary. Implementations must return
// torrents newest-first and must not perform calls into the Tracker data plane.
type Repository interface {
	SiteInfo(ctx context.Context, asOf time.Time) (SiteInfo, error)
	LatestAnnouncement(ctx context.Context) (*AnnouncementSummary, error)
	ListAnnouncements(ctx context.Context, limit, offset int) ([]AnnouncementSummary, int, error)
	Announcement(ctx context.Context, announcementID string) (Announcement, error)
	ListCategories(ctx context.Context) ([]CategorySummary, error)
	CategoryFacets(ctx context.Context, categoryID string) ([]CategoryFacet, error)
	ListTorrents(ctx context.Context, filter TorrentFilter) ([]Torrent, int, error)
	TorrentSwarm(ctx context.Context, torrentID int64) (SwarmStats, bool, error)
}

// GetTorrentSwarm resolves one independently refreshed aggregate without
// consulting the Tracker hot path. A missing snapshot is a successful,
// explicitly unavailable result so the stable torrent detail remains usable.
func (s *Service) GetTorrentSwarm(ctx context.Context, torrentID int64) (TorrentSwarmOverview, error) {
	if torrentID < 1 {
		return TorrentSwarmOverview{}, ErrTorrentNotFound
	}
	stats, available, err := s.repository.TorrentSwarm(ctx, torrentID)
	if err != nil {
		return TorrentSwarmOverview{}, err
	}
	overview := TorrentSwarmOverview{
		TorrentID:  torrentID,
		SwarmStats: stats,
		Stale:      true,
		Confidence: SwarmConfidenceUnavailable,
	}
	if !available {
		return overview, nil
	}
	overview.Stale = s.now().Sub(stats.ObservedAt) > swarmFreshness
	if overview.Stale {
		overview.Confidence = SwarmConfidenceStale
	} else {
		overview.Confidence = SwarmConfidenceFresh
	}
	return overview, nil
}

// Service owns catalog read rules independently of HTTP and storage adapters.
type Service struct {
	repository Repository
	now        func() time.Time
}

// NewService creates a catalog service. Supplying a clock keeps freshness rules
// deterministic in tests and prevents transport adapters from reimplementing it.
func NewService(repository Repository, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}

	return &Service{repository: repository, now: now}
}

// GetSiteInfo returns non-sensitive shell metadata.
func (s *Service) GetSiteInfo(ctx context.Context) (SiteInfo, error) {
	return s.repository.SiteInfo(ctx, s.now())
}

// GetLatestAnnouncement returns nil when nothing has been published.
func (s *Service) GetLatestAnnouncement(ctx context.Context) (*AnnouncementSummary, error) {
	result, err := s.repository.LatestAnnouncement(ctx)
	if err != nil || result == nil {
		return result, err
	}
	if err := validateAnnouncementSummary(*result); err != nil {
		return nil, err
	}
	return result, nil
}

// ListAnnouncements exposes only the revision currently public at the
// repository clock. Pagination is bounded before SQL and all returned rows are
// validated again so a malformed projection fails closed instead of leaking a
// draft-like partial object to the Web.
func (s *Service) ListAnnouncements(ctx context.Context, limit, offset int) (AnnouncementPage, error) {
	resolvedLimit, resolvedOffset, valid := normalizedAnnouncementPage(limit, offset)
	if !valid {
		return AnnouncementPage{}, ErrInvalidAnnouncementPage
	}
	items, total, err := s.repository.ListAnnouncements(ctx, resolvedLimit, resolvedOffset)
	if err != nil {
		return AnnouncementPage{}, err
	}
	if total < 0 || len(items) > resolvedLimit {
		return AnnouncementPage{}, errCatalogProjectionInvalid
	}
	for _, item := range items {
		if err := validateAnnouncementSummary(item); err != nil {
			return AnnouncementPage{}, err
		}
	}
	return AnnouncementPage{Items: items, Total: total, Limit: resolvedLimit, Offset: resolvedOffset}, nil
}

// GetAnnouncement returns only a currently published object. Stable route-key
// validation is shared with migration/import rules and happens before storage.
func (s *Service) GetAnnouncement(ctx context.Context, announcementID string) (Announcement, error) {
	if !ValidAnnouncementID(announcementID) {
		return Announcement{}, ErrAnnouncementNotFound
	}
	announcement, err := s.repository.Announcement(ctx, announcementID)
	if err != nil {
		return Announcement{}, err
	}
	if err := validateAnnouncementSummary(announcement.AnnouncementSummary); err != nil {
		return Announcement{}, err
	}
	if announcement.ID != announcementID || strings.TrimSpace(announcement.Body) == "" || utf8.RuneCountInString(announcement.Body) > 20_000 ||
		announcement.Version < 1 ||
		announcement.UpdatedAt.IsZero() ||
		(announcement.BodyFormat != AnnouncementBodyPlainText && announcement.BodyFormat != AnnouncementBodyLegacyBBCode) {
		return Announcement{}, errCatalogProjectionInvalid
	}
	return announcement, nil
}

func normalizedAnnouncementPage(limit, offset int) (int, int, bool) {
	if limit == 0 {
		limit = defaultAnnouncementLimit
	}
	if limit < 1 || limit > maxAnnouncementLimit || offset < 0 || offset > maxAnnouncementOffset {
		return 0, 0, false
	}
	return limit, offset, true
}

func validateAnnouncementSummary(value AnnouncementSummary) error {
	if !ValidAnnouncementID(value.ID) || strings.TrimSpace(value.Title) == "" || utf8.RuneCountInString(value.Title) > 160 ||
		strings.TrimSpace(value.Summary) == "" || utf8.RuneCountInString(value.Summary) > 500 {
		return errCatalogProjectionInvalid
	}
	if value.PublishedAt.IsZero() {
		return errCatalogProjectionInvalid
	}
	return nil
}

// ListCategories returns enabled categories in display order.
func (s *Service) ListCategories(ctx context.Context) ([]CategorySummary, error) {
	return s.repository.ListCategories(ctx)
}

// ListCategoryFacets returns only the controlled options accepted by one
// currently enabled category. Empty is a valid vocabulary for a real category.
func (s *Service) ListCategoryFacets(ctx context.Context, categoryID string) ([]CategoryFacet, error) {
	categoryID = strings.TrimSpace(categoryID)
	if !categoryIDPattern.MatchString(categoryID) {
		return nil, ErrCategoryNotFound
	}
	return s.repository.CategoryFacets(ctx, categoryID)
}

// ListTorrents applies the same bounded query and stale-snapshot semantics for
// HTTP, workers, and future CLI entry points. A nil limit means the contract
// default; explicit out-of-range values fail instead of being silently clamped.
func (s *Service) ListTorrents(ctx context.Context, request TorrentListRequest) (TorrentPage, error) {
	resolvedLimit := defaultTorrentLimit
	if request.Limit != nil {
		resolvedLimit = *request.Limit
	}
	if resolvedLimit < 1 || resolvedLimit > maxTorrentLimit {
		return TorrentPage{}, ErrInvalidLimit
	}
	resolvedOffset := 0
	if request.Offset != nil {
		resolvedOffset = *request.Offset
	}
	if resolvedOffset < 0 || resolvedOffset > maxTorrentOffset {
		return TorrentPage{}, ErrInvalidTorrentPage
	}

	normalizedQuery := strings.TrimSpace(request.Query)
	if utf8.RuneCountInString(normalizedQuery) > maxQueryRunes {
		return TorrentPage{}, ErrInvalidQuery
	}
	resolvedSearchScope := TorrentSearchTitleSubtitle
	if request.SearchScope != nil {
		resolvedSearchScope = *request.SearchScope
		if resolvedSearchScope != TorrentSearchTitleSubtitle && resolvedSearchScope != TorrentSearchTitle && resolvedSearchScope != TorrentSearchSubtitle {
			return TorrentPage{}, ErrInvalidTorrentFilter
		}
	}
	normalizedCategoryID := strings.TrimSpace(request.CategoryID)
	if normalizedCategoryID != request.CategoryID || (normalizedCategoryID != "" && !categoryIDPattern.MatchString(normalizedCategoryID)) {
		return TorrentPage{}, ErrInvalidTorrentFilter
	}
	resolvedPromotion := Promotion("")
	if request.Promotion != nil {
		resolvedPromotion = *request.Promotion
		if !resolvedPromotion.Valid() {
			return TorrentPage{}, ErrInvalidTorrentFilter
		}
	}
	resolvedSort := TorrentSortPublishedDesc
	if request.Sort != nil {
		resolvedSort = *request.Sort
		if resolvedSort != TorrentSortPublishedDesc && resolvedSort != TorrentSortPublishedAsc && resolvedSort != TorrentSortSizeDesc && resolvedSort != TorrentSortSizeAsc && resolvedSort != TorrentSortCompletedDesc {
			return TorrentPage{}, ErrInvalidTorrentFilter
		}
	}

	filter := TorrentFilter{
		Limit: resolvedLimit, Offset: resolvedOffset, Query: normalizedQuery,
		SearchScope: resolvedSearchScope, CategoryID: normalizedCategoryID,
		Promotion: resolvedPromotion, Sort: resolvedSort,
	}
	items, total, err := s.repository.ListTorrents(ctx, filter)
	if err != nil {
		return TorrentPage{}, err
	}

	now := s.now()
	summaries := make([]TorrentSummary, 0, len(items))
	for _, item := range items {
		summaries = append(summaries, SummarizeTorrent(item, now))
	}

	return TorrentPage{Items: summaries, Total: total, Limit: resolvedLimit, Offset: resolvedOffset}, nil
}

// SummarizeTorrent applies the catalog-owned freshness rule to a public row.
// Detail-adjacent reads such as resource-group siblings reuse this function so
// list and detail never disagree about the same asynchronous swarm timestamp.
func SummarizeTorrent(item Torrent, now time.Time) TorrentSummary {
	// Future timestamps are treated as fresh here. Clock-skew monitoring belongs
	// to the projector; hiding otherwise usable catalog content would couple the
	// Web availability to a Tracker timing fault.
	return TorrentSummary{
		Torrent:    item,
		SwarmStale: now.Sub(item.Swarm.ObservedAt) > swarmFreshness,
	}
}
