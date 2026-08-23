package trafficcleanup

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/services/core/internal/generated/trafficdb"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, ErrInput
	}
	return &PostgresRepository{pool: pool}, nil
}

func (repository *PostgresRepository) Cleanup(ctx context.Context, cutoffs Cutoffs, batchSize int) (Result, error) {
	if cutoffs.DetailBefore.IsZero() || cutoffs.HistoryBefore.IsZero() ||
		!cutoffs.HistoryBefore.Before(cutoffs.DetailBefore) || batchSize < 100 || batchSize > 10_000 {
		return Result{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Result{}, cleanupError("begin Core traffic cleanup", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := trafficdb.New(tx)
	ids, err := queries.ListTrafficProjectionCleanupCandidates(ctx, trafficdb.ListTrafficProjectionCleanupCandidatesParams{
		DetailBefore: cleanupTimestamp(cutoffs.DetailBefore), BatchSize: int32(batchSize),
	})
	if err != nil {
		return Result{}, cleanupError("select Core traffic cleanup batch", err)
	}
	var result Result
	if len(ids) > 0 {
		if result.Segments, err = queries.DeleteTrafficProjectionSegments(ctx, ids); err != nil {
			return result, cleanupError("delete Core traffic explanation segments", err)
		}
		if result.Explanations, err = queries.DeleteTrafficProjectionExplanations(ctx, ids); err != nil {
			return result, cleanupError("delete Core traffic explanations", err)
		}
		if result.Entries, err = queries.DeleteTrafficProjectionEntries(ctx, ids); err != nil {
			return result, cleanupError("delete Core traffic entries", err)
		}
		if result.Inbox, err = queries.DeleteTrafficProjectionInbox(ctx, ids); err != nil {
			return result, cleanupError("delete Core traffic inbox rows", err)
		}
		if result.Entries != int64(len(ids)) || result.Inbox != int64(len(ids)) {
			return result, fmt.Errorf("%w: selected %d entries but deleted %d entries and %d inbox rows",
				ErrInvariant, len(ids), result.Entries, result.Inbox)
		}
	}
	result.Rollups, err = queries.DeleteTrafficHistoryRollups(ctx, trafficdb.DeleteTrafficHistoryRollupsParams{
		HistoryBefore: cleanupTimestamp(cutoffs.HistoryBefore), BatchSize: int32(batchSize),
	})
	if err != nil {
		return result, cleanupError("delete Core traffic history rollups", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return result, cleanupError("commit Core traffic cleanup", err)
	}
	return result, nil
}

func cleanupTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC().Truncate(time.Microsecond), Valid: true}
}

func cleanupError(operation string, err error) error {
	return fmt.Errorf("%s: %w", operation, err)
}
