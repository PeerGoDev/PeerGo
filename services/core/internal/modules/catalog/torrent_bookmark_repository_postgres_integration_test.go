package catalog_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/catalog"
)

func TestPostgresTorrentBookmarksAreIdempotentAndPublishedOnly(t *testing.T) {
	databaseURL := os.Getenv("PEERGO_TEST_CORE_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PEERGO_TEST_CORE_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)

	repository, err := catalog.NewPostgresTorrentBookmarkRepository(pool)
	if err != nil {
		t.Fatalf("NewPostgresTorrentBookmarkRepository() error = %v", err)
	}
	userID := uuid.New()
	prefix := uuid.NewString()[:8]
	username := "bookmark-it-" + prefix
	now := time.Now().UTC().Truncate(time.Microsecond)
	var publishedTorrentID, orphanTorrentID int64
	var categoryID string
	if err := pool.QueryRow(ctx, `
SELECT projection.id, projection.category_id
FROM catalog.torrents AS projection
JOIN torrents.torrents AS aggregate
  ON aggregate.id = projection.id
 AND aggregate.state = 'published'
JOIN catalog.categories AS category
  ON category.id = projection.category_id
 AND category.enabled = true
ORDER BY projection.published_at DESC, projection.id DESC
LIMIT 1`).Scan(&publishedTorrentID, &categoryID); err != nil {
		t.Skipf("published torrent fixture is required: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.users (id, credential_ref, username, display_name, status)
VALUES ($1, $2, $3, $3, 'active')`, userID, uuid.New(), username); err != nil {
		t.Fatalf("insert bookmark user: %v", err)
	}
	orphanTorrentID = publishedTorrentID + 1_000_000_000
	if _, err := pool.Exec(ctx, `
INSERT INTO catalog.torrents (
    id, category_id, name, subtitle, size_bytes, promotion, published_at, created_at
) VALUES ($1, $2, 'Orphan bookmark fixture', '', 1024, 'none', $3, $3)`,
		orphanTorrentID, categoryID, now.Add(-time.Hour)); err != nil {
		t.Fatalf("insert orphan bookmark projection: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM catalog.torrents WHERE id = $1`, orphanTorrentID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM identity.users WHERE id = $1`, userID)
	})

	savedAt := now.Add(-30 * time.Minute)
	firstSavedAt, err := repository.Put(ctx, userID, publishedTorrentID, savedAt)
	if err != nil || !firstSavedAt.Equal(savedAt) {
		t.Fatalf("first Put() time=%s error=%v", firstSavedAt, err)
	}
	repeatedSavedAt, err := repository.Put(ctx, userID, publishedTorrentID, now)
	if err != nil || !repeatedSavedAt.Equal(savedAt) {
		t.Fatalf("repeated Put() time=%s error=%v", repeatedSavedAt, err)
	}

	statuses, err := repository.Statuses(ctx, userID, []int64{publishedTorrentID, orphanTorrentID, orphanTorrentID + 1})
	if err != nil || len(statuses) != 1 || statuses[0] != publishedTorrentID {
		t.Fatalf("Statuses()=%v error=%v", statuses, err)
	}
	records, total, err := repository.List(ctx, userID, 1, 0)
	if err != nil || total != 1 || len(records) != 1 || records[0].Torrent.ID != publishedTorrentID || !records[0].BookmarkedAt.Equal(savedAt) {
		t.Fatalf("List() records=%+v total=%d error=%v", records, total, err)
	}

	if err := repository.Delete(ctx, userID, publishedTorrentID); err != nil {
		t.Fatalf("Delete() error=%v", err)
	}
	if err := repository.Delete(ctx, userID, publishedTorrentID); err != nil {
		t.Fatalf("repeated Delete() error=%v", err)
	}
	statuses, err = repository.Statuses(ctx, userID, []int64{publishedTorrentID})
	if err != nil || len(statuses) != 0 {
		t.Fatalf("deleted Statuses()=%v error=%v", statuses, err)
	}

	if _, err := repository.Put(ctx, userID, orphanTorrentID, now); !errors.Is(err, catalog.ErrTorrentBookmarkNotFound) {
		t.Fatalf("orphan projection Put() error=%v, want ErrTorrentBookmarkNotFound", err)
	}

	if _, err := repository.Put(ctx, userID, orphanTorrentID+1, now); !errors.Is(err, catalog.ErrTorrentBookmarkNotFound) {
		t.Fatalf("missing Put() error=%v, want ErrTorrentBookmarkNotFound", err)
	}
}
