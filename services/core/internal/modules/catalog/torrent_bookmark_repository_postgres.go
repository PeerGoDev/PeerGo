package catalog

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/generated/catalogdb"
)

type PostgresTorrentBookmarkRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresTorrentBookmarkRepository(pool *pgxpool.Pool) (*PostgresTorrentBookmarkRepository, error) {
	if pool == nil {
		return nil, errors.New("torrent bookmark database is required")
	}
	return &PostgresTorrentBookmarkRepository{pool: pool}, nil
}

// List holds count and rows in one repeatable-read snapshot. A separate count
// is intentional: count(*) over cannot report the true total for an empty page
// whose offset is already beyond the final item.
func (repository *PostgresTorrentBookmarkRepository) List(ctx context.Context, userID uuid.UUID, limit, offset int) ([]torrentBookmarkRecord, int, error) {
	if userID == uuid.Nil || limit < 1 || limit > MaxTorrentBookmarkLimit || offset < 0 || offset > MaxTorrentBookmarkOffset {
		return nil, 0, ErrTorrentBookmarkInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, fmt.Errorf("begin torrent bookmark list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := catalogdb.New(tx)
	total, err := queries.CountTorrentBookmarks(ctx, torrentBookmarkUUID(userID))
	if err != nil {
		return nil, 0, fmt.Errorf("count torrent bookmarks: %w", err)
	}
	if total < 0 || total > math.MaxInt {
		return nil, 0, ErrTorrentBookmarkInvariant
	}
	rows, err := queries.ListTorrentBookmarks(ctx, catalogdb.ListTorrentBookmarksParams{
		UserID: torrentBookmarkUUID(userID), ResultLimit: int32(limit), ResultOffset: int32(offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list torrent bookmarks: %w", err)
	}
	records := make([]torrentBookmarkRecord, 0, len(rows))
	for _, row := range rows {
		if !row.BookmarkedAt.Valid || !row.PublishedAt.Valid || !row.ObservedAt.Valid {
			return nil, 0, ErrTorrentBookmarkInvariant
		}
		torrent, conversionErr := TorrentFromProjection(
			row.TorrentID, row.Name, row.Subtitle, row.CategoryID, row.CategoryName,
			row.SizeBytes, row.Promotion, row.StickyUntil.Time, row.PublishedAt.Time,
			row.Seeders, row.Leechers, row.Completed, row.ObservedAt.Time,
		)
		if conversionErr != nil {
			return nil, 0, conversionErr
		}
		records = append(records, torrentBookmarkRecord{Torrent: torrent, BookmarkedAt: row.BookmarkedAt.Time.UTC()})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, fmt.Errorf("commit torrent bookmark list: %w", err)
	}
	return records, int(total), nil
}

func (repository *PostgresTorrentBookmarkRepository) Statuses(ctx context.Context, userID uuid.UUID, torrentIDs []int64) ([]int64, error) {
	if userID == uuid.Nil {
		return nil, ErrTorrentBookmarkInput
	}
	normalized, err := normalizeTorrentBookmarkIDs(torrentIDs)
	if err != nil {
		return nil, err
	}
	items, err := catalogdb.New(repository.pool).ListTorrentBookmarkStatuses(ctx, catalogdb.ListTorrentBookmarkStatusesParams{
		UserID: torrentBookmarkUUID(userID), TorrentIds: normalized,
	})
	if err != nil {
		return nil, fmt.Errorf("list torrent bookmark statuses: %w", err)
	}
	return items, nil
}

func (repository *PostgresTorrentBookmarkRepository) Put(ctx context.Context, userID uuid.UUID, torrentID int64, createdAt time.Time) (time.Time, error) {
	if userID == uuid.Nil || torrentID < 1 || createdAt.IsZero() {
		return time.Time{}, ErrTorrentBookmarkInput
	}
	value, err := catalogdb.New(repository.pool).PutTorrentBookmark(ctx, catalogdb.PutTorrentBookmarkParams{
		UserID:    torrentBookmarkUUID(userID),
		TorrentID: torrentID,
		CreatedAt: pgtype.Timestamptz{Time: createdAt.UTC(), Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrTorrentBookmarkNotFound
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("put torrent bookmark: %w", err)
	}
	if !value.Valid {
		return time.Time{}, ErrTorrentBookmarkInvariant
	}
	return value.Time.UTC(), nil
}

func (repository *PostgresTorrentBookmarkRepository) Delete(ctx context.Context, userID uuid.UUID, torrentID int64) error {
	if userID == uuid.Nil || torrentID < 1 {
		return ErrTorrentBookmarkInput
	}
	if err := catalogdb.New(repository.pool).DeleteTorrentBookmark(ctx, catalogdb.DeleteTorrentBookmarkParams{
		UserID: torrentBookmarkUUID(userID), TorrentID: torrentID,
	}); err != nil {
		return fmt.Errorf("delete torrent bookmark: %w", err)
	}
	return nil
}

func torrentBookmarkUUID(value uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: value, Valid: true}
}

var _ TorrentBookmarkRepository = (*PostgresTorrentBookmarkRepository)(nil)
