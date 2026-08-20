package catalog

import (
	"strings"
	"time"
)

// TorrentFromProjection is the single conversion guard for every public-safe
// catalog row, including lists, bookmarks and resource-group siblings. All
// surfaces must interpret content and Tracker-derived counters identically.
func TorrentFromProjection(
	id int64,
	name, subtitle, categoryID, categoryName string,
	sizeBytes int64,
	promotionValue string,
	stickyUntil time.Time,
	publishedAt time.Time,
	seeders, leechers, completed int32,
	observedAt time.Time,
) (Torrent, error) {
	promotion := Promotion(promotionValue)
	if id < 1 || !validCatalogID(categoryID) || strings.TrimSpace(name) == "" ||
		strings.TrimSpace(categoryName) == "" || sizeBytes < 0 || publishedAt.IsZero() || observedAt.IsZero() ||
		seeders < 0 || leechers < 0 || completed < 0 || !promotion.Valid() {
		return Torrent{}, errCatalogProjectionInvalid
	}
	var normalizedStickyUntil *time.Time
	if !stickyUntil.IsZero() {
		value := stickyUntil.UTC()
		normalizedStickyUntil = &value
	}
	return Torrent{
		ID: id, Name: name, Subtitle: subtitle,
		Category:  Category{ID: categoryID, Name: categoryName},
		SizeBytes: sizeBytes, Promotion: promotion, StickyUntil: normalizedStickyUntil, UploadedAt: publishedAt.UTC(),
		Swarm: SwarmStats{
			Seeders: int(seeders), Leechers: int(leechers), Completed: int(completed),
			ObservedAt: observedAt.UTC(),
		},
	}, nil
}
