package catalog

import (
	"context"
	"sort"
	"strings"
)

// MemoryData is an explicit fixture boundary for local development and tests.
// It is not a substitute for the PostgreSQL adapter used by production.
type MemoryData struct {
	Site           SiteInfo
	Announcements  []Announcement
	Categories     []Category
	CategoryFacets map[string][]CategoryFacet
	Torrents       []Torrent
}

// MemoryRepository is immutable after construction, so concurrent reads need
// no locks. Every slice/pointer is copied at the boundary to preserve that rule.
type MemoryRepository struct {
	data MemoryData
}

// NewMemoryRepository returns a read-only repository backed by copied fixtures.
func NewMemoryRepository(data MemoryData) *MemoryRepository {
	copyData := MemoryData{
		Site:           data.Site,
		Announcements:  append([]Announcement(nil), data.Announcements...),
		Categories:     append([]Category(nil), data.Categories...),
		CategoryFacets: cloneCategoryFacets(data.CategoryFacets),
		Torrents:       append([]Torrent(nil), data.Torrents...),
	}
	sort.Slice(copyData.Announcements, func(left, right int) bool {
		if copyData.Announcements[left].PublishedAt.Equal(copyData.Announcements[right].PublishedAt) {
			return copyData.Announcements[left].ID > copyData.Announcements[right].ID
		}
		return copyData.Announcements[left].PublishedAt.After(copyData.Announcements[right].PublishedAt)
	})

	return &MemoryRepository{data: copyData}
}

func cloneCategoryFacets(values map[string][]CategoryFacet) map[string][]CategoryFacet {
	result := make(map[string][]CategoryFacet, len(values))
	for categoryID, facets := range values {
		copied := make([]CategoryFacet, 0, len(facets))
		for _, facet := range facets {
			facet.Options = append([]CategoryFacetOption(nil), facet.Options...)
			copied = append(copied, facet)
		}
		result[categoryID] = copied
	}
	return result
}

// SiteInfo implements Repository.
func (r *MemoryRepository) SiteInfo(context.Context) (SiteInfo, error) {
	return r.data.Site, nil
}

// LatestAnnouncement implements Repository.
func (r *MemoryRepository) LatestAnnouncement(context.Context) (*AnnouncementSummary, error) {
	if !r.data.Site.ShowLatestAnnouncement || len(r.data.Announcements) == 0 {
		return nil, nil
	}
	announcement := r.data.Announcements[0].AnnouncementSummary
	return &announcement, nil
}

// ListAnnouncements implements the same newest-first paging contract as the
// PostgreSQL adapter. Fixtures are copied at both construction and return.
func (r *MemoryRepository) ListAnnouncements(_ context.Context, limit, offset int) ([]AnnouncementSummary, int, error) {
	total := len(r.data.Announcements)
	if offset >= total {
		return []AnnouncementSummary{}, total, nil
	}
	end := min(offset+limit, total)
	items := make([]AnnouncementSummary, 0, end-offset)
	for _, announcement := range r.data.Announcements[offset:end] {
		items = append(items, announcement.AnnouncementSummary)
	}
	return items, total, nil
}

// Announcement implements Repository for local fixtures.
func (r *MemoryRepository) Announcement(_ context.Context, announcementID string) (Announcement, error) {
	for _, announcement := range r.data.Announcements {
		if announcement.ID == announcementID {
			return announcement, nil
		}
	}
	return Announcement{}, ErrAnnouncementNotFound
}

// ListCategories implements Repository.
func (r *MemoryRepository) ListCategories(context.Context) ([]CategorySummary, error) {
	result := make([]CategorySummary, 0, len(r.data.Categories))
	for _, category := range r.data.Categories {
		count := 0
		for _, torrent := range r.data.Torrents {
			if torrent.Category.ID == category.ID {
				count++
			}
		}
		result = append(result, CategorySummary{Category: category, TorrentCount: count})
	}
	return result, nil
}

// CategoryFacets implements Repository for explicit local fixtures.
func (r *MemoryRepository) CategoryFacets(_ context.Context, categoryID string) ([]CategoryFacet, error) {
	found := false
	for _, category := range r.data.Categories {
		if category.ID == categoryID {
			found = true
			break
		}
	}
	if !found {
		return nil, ErrCategoryNotFound
	}
	return cloneCategoryFacets(map[string][]CategoryFacet{categoryID: r.data.CategoryFacets[categoryID]})[categoryID], nil
}

// ListTorrents implements Repository. Fixtures are stored newest-first to match
// the repository contract; a database adapter will enforce the order in SQL.
func (r *MemoryRepository) ListTorrents(_ context.Context, filter TorrentFilter) ([]Torrent, int, error) {
	normalizedQuery := strings.ToLower(filter.Query)
	matches := make([]Torrent, 0, len(r.data.Torrents))
	for _, torrent := range r.data.Torrents {
		haystack := strings.ToLower(torrent.Name + " " + torrent.Subtitle)
		switch filter.SearchScope {
		case TorrentSearchTitle:
			haystack = strings.ToLower(torrent.Name)
		case TorrentSearchSubtitle:
			haystack = strings.ToLower(torrent.Subtitle)
		}
		queryMatches := normalizedQuery == "" || strings.Contains(haystack, normalizedQuery)
		categoryMatches := filter.CategoryID == "" || torrent.Category.ID == filter.CategoryID
		promotionMatches := filter.Promotion == "" || torrent.Promotion == filter.Promotion
		if queryMatches && categoryMatches && promotionMatches {
			matches = append(matches, torrent)
		}
	}
	sort.SliceStable(matches, func(left, right int) bool {
		switch filter.Sort {
		case TorrentSortPublishedAsc:
			return matches[left].UploadedAt.Before(matches[right].UploadedAt)
		case TorrentSortSizeDesc:
			return matches[left].SizeBytes > matches[right].SizeBytes
		case TorrentSortSizeAsc:
			return matches[left].SizeBytes < matches[right].SizeBytes
		case TorrentSortCompletedDesc:
			return matches[left].Swarm.Completed > matches[right].Swarm.Completed
		default:
			return matches[left].UploadedAt.After(matches[right].UploadedAt)
		}
	})

	total := len(matches)
	if filter.Offset >= total {
		return []Torrent{}, total, nil
	}
	matches = matches[filter.Offset:]
	if len(matches) > filter.Limit {
		matches = matches[:filter.Limit]
	}
	return append([]Torrent(nil), matches...), total, nil
}

// TorrentSwarm implements Repository for deterministic local fixtures.
func (r *MemoryRepository) TorrentSwarm(_ context.Context, torrentID int64) (SwarmStats, bool, error) {
	for _, torrent := range r.data.Torrents {
		if torrent.ID == torrentID {
			available := !torrent.Swarm.ObservedAt.IsZero()
			return torrent.Swarm, available, nil
		}
	}
	return SwarmStats{}, false, ErrTorrentNotFound
}
