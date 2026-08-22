package catalog

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/peergo/peergo/services/core/internal/generated/catalogdb"
)

var errCatalogProjectionInvalid = errors.New("catalog projection contains invalid data")

type catalogQueries interface {
	GetSiteInfo(context.Context, pgtype.Timestamptz) (catalogdb.GetSiteInfoRow, error)
	GetLatestAnnouncement(context.Context) (catalogdb.GetLatestAnnouncementRow, error)
	CountPublishedAnnouncements(context.Context) (int64, error)
	ListPublishedAnnouncements(context.Context, catalogdb.ListPublishedAnnouncementsParams) ([]catalogdb.ListPublishedAnnouncementsRow, error)
	GetPublishedAnnouncement(context.Context, string) (catalogdb.CatalogPublicAnnouncementProjection, error)
	ListEnabledCategories(context.Context) ([]catalogdb.ListEnabledCategoriesRow, error)
	EnabledCategoryExists(context.Context, string) (bool, error)
	ListEnabledCategoryFacetOptions(context.Context, string) ([]catalogdb.ListEnabledCategoryFacetOptionsRow, error)
	CountPublishedTorrents(context.Context, catalogdb.CountPublishedTorrentsParams) (int64, error)
	ListPublishedTorrents(context.Context, catalogdb.ListPublishedTorrentsParams) ([]catalogdb.ListPublishedTorrentsRow, error)
	GetPublishedTorrentSwarm(context.Context, int64) (catalogdb.GetPublishedTorrentSwarmRow, error)
}

// TorrentSwarm implements Repository without contacting the Tracker. The
// nullable timestamp distinguishes a legitimate zero-count snapshot from a
// torrent whose first complete snapshot has not arrived yet.
func (r *PostgresRepository) TorrentSwarm(ctx context.Context, torrentID int64) (SwarmStats, bool, error) {
	row, err := r.queries.GetPublishedTorrentSwarm(ctx, torrentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return SwarmStats{}, false, ErrTorrentNotFound
	}
	if err != nil {
		return SwarmStats{}, false, fmt.Errorf("get published torrent swarm: %w", err)
	}
	if row.ID != torrentID || row.Seeders < 0 || row.Leechers < 0 || row.Completed < 0 {
		return SwarmStats{}, false, errCatalogProjectionInvalid
	}
	if !row.StatsAvailable {
		if row.ObservedAt.Valid {
			return SwarmStats{}, false, errCatalogProjectionInvalid
		}
		return SwarmStats{}, false, nil
	}
	if !row.ObservedAt.Valid {
		return SwarmStats{}, false, errCatalogProjectionInvalid
	}
	return SwarmStats{
		Seeders: int(row.Seeders), Leechers: int(row.Leechers),
		Completed: int(row.Completed), ObservedAt: row.ObservedAt.Time.UTC(),
	}, true, nil
}

// PostgresRepository adapts sqlc-owned rows to catalog domain values. Keeping
// the conversion here prevents SQL nullability and pgx types from becoming a
// second set of models used by handlers or business rules.
type PostgresRepository struct {
	queries catalogQueries
}

// NewPostgresRepository creates the production catalog persistence adapter.
func NewPostgresRepository(db catalogdb.DBTX) *PostgresRepository {
	return &PostgresRepository{queries: catalogdb.New(db)}
}

// SiteInfo implements Repository.
func (r *PostgresRepository) SiteInfo(ctx context.Context, asOf time.Time) (SiteInfo, error) {
	row, err := r.queries.GetSiteInfo(ctx, pgtype.Timestamptz{Time: asOf.UTC(), Valid: true})
	if err != nil {
		return SiteInfo{}, fmt.Errorf("get site profile: %w", err)
	}

	defaultView := TorrentView(row.DefaultTorrentView)
	if defaultView != TorrentViewList && defaultView != TorrentViewPoster {
		return SiteInfo{}, fmt.Errorf("%w: default torrent view %q", errCatalogProjectionInvalid, row.DefaultTorrentView)
	}

	return SiteInfo{
		Name:                   row.Name,
		Description:            row.Description,
		OnlineUsers:            int(row.OnlineUsers),
		DefaultTorrentView:     defaultView,
		ShowLatestAnnouncement: row.ShowLatestAnnouncement,
	}, nil
}

// LatestAnnouncement implements Repository.
func (r *PostgresRepository) LatestAnnouncement(ctx context.Context) (*AnnouncementSummary, error) {
	row, err := r.queries.GetLatestAnnouncement(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get latest announcement: %w", err)
	}
	if !row.PublishedAt.Valid {
		return nil, fmt.Errorf("%w: latest announcement has no publish time", errCatalogProjectionInvalid)
	}

	return &AnnouncementSummary{
		ID:          row.ID,
		Title:       row.Title,
		Summary:     row.Summary,
		PublishedAt: row.PublishedAt.Time,
	}, nil
}

// ListAnnouncements implements bounded public paging against the same view as
// latest and detail. Count and rows are separate read-only statements so an
// offset beyond the last page still returns the authoritative total.
func (r *PostgresRepository) ListAnnouncements(ctx context.Context, limit, offset int) ([]AnnouncementSummary, int, error) {
	total, err := r.queries.CountPublishedAnnouncements(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count published announcements: %w", err)
	}
	if total < 0 || total > math.MaxInt {
		return nil, 0, fmt.Errorf("%w: announcement count exceeds platform integer", errCatalogProjectionInvalid)
	}
	rows, err := r.queries.ListPublishedAnnouncements(ctx, catalogdb.ListPublishedAnnouncementsParams{
		ResultLimit: int32(limit), ResultOffset: int32(offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list published announcements: %w", err)
	}
	items := make([]AnnouncementSummary, 0, len(rows))
	for _, row := range rows {
		if !row.PublishedAt.Valid {
			return nil, 0, fmt.Errorf("%w: announcement %q has no public time", errCatalogProjectionInvalid, row.ID)
		}
		items = append(items, AnnouncementSummary{
			ID: row.ID, Title: row.Title, Summary: row.Summary, PublishedAt: row.PublishedAt.Time.UTC(),
		})
	}
	return items, int(total), nil
}

// Announcement implements Repository and keeps draft/scheduled/missing rows
// indistinguishable at the public boundary.
func (r *PostgresRepository) Announcement(ctx context.Context, announcementID string) (Announcement, error) {
	row, err := r.queries.GetPublishedAnnouncement(ctx, announcementID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Announcement{}, ErrAnnouncementNotFound
	}
	if err != nil {
		return Announcement{}, fmt.Errorf("get published announcement: %w", err)
	}
	if !row.PublishedAt.Valid || !row.UpdatedAt.Valid {
		return Announcement{}, fmt.Errorf("%w: announcement has an invalid timestamp", errCatalogProjectionInvalid)
	}
	return Announcement{
		AnnouncementSummary: AnnouncementSummary{
			ID: row.ID, Title: row.Title, Summary: row.Summary, PublishedAt: row.PublishedAt.Time.UTC(),
		},
		Body: row.Body, BodyFormat: AnnouncementBodyFormat(row.BodyFormat), Version: row.Version,
		UpdatedAt: row.UpdatedAt.Time.UTC(),
	}, nil
}

// ListCategories implements Repository.
func (r *PostgresRepository) ListCategories(ctx context.Context) ([]CategorySummary, error) {
	rows, err := r.queries.ListEnabledCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("list enabled categories: %w", err)
	}

	categories := make([]CategorySummary, 0, len(rows))
	for _, row := range rows {
		if row.TorrentCount < 0 || row.TorrentCount > math.MaxInt {
			return nil, fmt.Errorf("%w: category %q has an invalid torrent count", errCatalogProjectionInvalid, row.ID)
		}
		categories = append(categories, CategorySummary{
			Category: Category{ID: row.ID, Name: row.Name}, TorrentCount: int(row.TorrentCount),
		})
	}
	return categories, nil
}

// CategoryFacets groups the flat sqlc projection without letting option rows
// escape into transport code. A real category with no bindings returns an
// empty list; disabled and unknown categories share ErrCategoryNotFound.
func (r *PostgresRepository) CategoryFacets(ctx context.Context, categoryID string) ([]CategoryFacet, error) {
	exists, err := r.queries.EnabledCategoryExists(ctx, categoryID)
	if err != nil {
		return nil, fmt.Errorf("check enabled category: %w", err)
	}
	if !exists {
		return nil, ErrCategoryNotFound
	}
	rows, err := r.queries.ListEnabledCategoryFacetOptions(ctx, categoryID)
	if err != nil {
		return nil, fmt.Errorf("list enabled category facet options: %w", err)
	}

	facets := make([]CategoryFacet, 0)
	for _, row := range rows {
		mode := FacetSelectionMode(row.SelectionMode)
		if (mode != FacetSelectionSingle && mode != FacetSelectionMulti) ||
			!categoryIDPattern.MatchString(row.FacetID) || strings.TrimSpace(row.FacetName) == "" ||
			(row.RequirementGroup != "" && !categoryIDPattern.MatchString(row.RequirementGroup)) ||
			strings.TrimSpace(row.OptionKey) == "" || strings.TrimSpace(row.OptionLabel) == "" {
			return nil, fmt.Errorf("%w: category %q facet projection is invalid", errCatalogProjectionInvalid, categoryID)
		}
		if len(facets) == 0 || facets[len(facets)-1].ID != row.FacetID {
			if len(facets) >= 20 {
				return nil, fmt.Errorf("%w: category %q has too many facets", errCatalogProjectionInvalid, categoryID)
			}
			facets = append(facets, CategoryFacet{
				ID: row.FacetID, Name: row.FacetName, SelectionMode: mode, Required: row.Required,
				RequirementGroup: row.RequirementGroup,
				Options:          make([]CategoryFacetOption, 0),
			})
		}
		facet := &facets[len(facets)-1]
		if facet.Name != row.FacetName || facet.SelectionMode != mode || facet.Required != row.Required ||
			facet.RequirementGroup != row.RequirementGroup || len(facet.Options) >= 200 {
			return nil, fmt.Errorf("%w: category %q facet rows disagree", errCatalogProjectionInvalid, categoryID)
		}
		facet.Options = append(facet.Options, CategoryFacetOption{Key: row.OptionKey, Label: row.OptionLabel})
	}
	return facets, nil
}

// ListTorrents implements Repository. Search and ordering are performed in
// PostgreSQL, while stale-snapshot policy remains in the catalog service.
func (r *PostgresRepository) ListTorrents(ctx context.Context, filter TorrentFilter) ([]Torrent, int, error) {
	params := catalogdb.CountPublishedTorrentsParams{
		SearchText: filter.Query, SearchScope: string(filter.SearchScope),
		CategoryID: filter.CategoryID, Promotion: string(filter.Promotion),
	}
	totalCount, err := r.queries.CountPublishedTorrents(ctx, params)
	if err != nil {
		return nil, 0, fmt.Errorf("count published torrents: %w", err)
	}
	if totalCount < 0 || totalCount > math.MaxInt {
		return nil, 0, fmt.Errorf("%w: torrent count exceeds platform integer", errCatalogProjectionInvalid)
	}
	if filter.Offset >= int(totalCount) {
		return []Torrent{}, int(totalCount), nil
	}
	rows, err := r.queries.ListPublishedTorrents(ctx, catalogdb.ListPublishedTorrentsParams{
		SearchText: filter.Query, SearchScope: string(filter.SearchScope),
		CategoryID: filter.CategoryID, Promotion: string(filter.Promotion),
		SortOrder:   string(filter.Sort),
		ResultLimit: int32(filter.Limit), ResultOffset: int32(filter.Offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list published torrents: %w", err)
	}

	items := make([]Torrent, 0, len(rows))
	for _, row := range rows {
		if !row.PublishedAt.Valid || !row.ObservedAt.Valid {
			return nil, 0, fmt.Errorf("%w: torrent %d has an invalid timestamp", errCatalogProjectionInvalid, row.ID)
		}
		item, conversionErr := TorrentFromProjection(
			row.ID, row.Name, row.Subtitle, row.CategoryID, row.CategoryName,
			row.SizeBytes, row.Promotion, row.StickyUntil.Time, row.PublishedAt.Time,
			row.Seeders, row.Leechers, row.Completed, row.ObservedAt.Time,
		)
		if conversionErr != nil {
			return nil, 0, fmt.Errorf("%w: torrent %d", conversionErr, row.ID)
		}
		items = append(items, item)
	}

	return items, int(totalCount), nil
}

var _ Repository = (*PostgresRepository)(nil)
