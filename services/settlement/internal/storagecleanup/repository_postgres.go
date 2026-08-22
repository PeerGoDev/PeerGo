package storagecleanup

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/services/settlement/internal/generated/ledgerdb"
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
	if !validCutoffs(cutoffs) || batchSize < 100 || batchSize > 10_000 {
		return Result{}, ErrInput
	}
	queries := ledgerdb.New(repository.pool)
	terminal := cleanupTimestamp(cutoffs.TerminalBefore)
	session := cleanupTimestamp(cutoffs.SessionBefore)
	detail := cleanupTimestamp(cutoffs.DetailBefore)
	anomaly := cleanupTimestamp(cutoffs.AnomalyBefore)
	batch := int32(batchSize)
	var result Result
	var err error

	if result.TrafficOutbox, err = queries.StorageCleanupDeleteTrafficOutbox(ctx, ledgerdb.StorageCleanupDeleteTrafficOutboxParams{TerminalBefore: terminal, BatchSize: batch}); err != nil {
		return result, cleanupError("delete published traffic outbox", err)
	}
	if result.HNROutbox, err = queries.StorageCleanupDeleteHNROutbox(ctx, ledgerdb.StorageCleanupDeleteHNROutboxParams{TerminalBefore: terminal, BatchSize: batch}); err != nil {
		return result, cleanupError("delete published H&R outbox", err)
	}
	if result.SeedingEvidenceOutbox, err = queries.StorageCleanupDeleteSeedingEvidenceOutbox(ctx, ledgerdb.StorageCleanupDeleteSeedingEvidenceOutboxParams{TerminalBefore: terminal, BatchSize: batch}); err != nil {
		return result, cleanupError("delete published seeding evidence outbox", err)
	}
	if result.PolicyWork, err = queries.StorageCleanupDeletePolicyWork(ctx, ledgerdb.StorageCleanupDeletePolicyWorkParams{TerminalBefore: terminal, BatchSize: batch}); err != nil {
		return result, cleanupError("delete settled policy work", err)
	}
	if result.HNRWork, err = queries.StorageCleanupDeleteHNRWork(ctx, ledgerdb.StorageCleanupDeleteHNRWorkParams{TerminalBefore: terminal, BatchSize: batch}); err != nil {
		return result, cleanupError("delete processed H&R work", err)
	}
	result.TrafficSettlements, result.TrafficSegments, err = repository.cleanupTrafficDetails(ctx, detail, batch)
	if err != nil {
		return result, err
	}
	if result.SeedingSources, err = queries.StorageCleanupDeleteSeedingSources(ctx, ledgerdb.StorageCleanupDeleteSeedingSourcesParams{DetailBefore: detail, BatchSize: batch}); err != nil {
		return result, cleanupError("delete old seeding source links", err)
	}
	if result.SnapshotEntries, err = queries.StorageCleanupDeleteSnapshotEntries(ctx, ledgerdb.StorageCleanupDeleteSnapshotEntriesParams{DetailBefore: detail, BatchSize: batch}); err != nil {
		return result, cleanupError("delete redundant snapshot entries", err)
	}
	if result.SnapshotChunks, err = queries.StorageCleanupDeleteSnapshotChunks(ctx, ledgerdb.StorageCleanupDeleteSnapshotChunksParams{DetailBefore: detail, BatchSize: batch}); err != nil {
		return result, cleanupError("delete old snapshot chunks", err)
	}
	if result.SnapshotInbox, err = queries.StorageCleanupDeleteSnapshotInbox(ctx, ledgerdb.StorageCleanupDeleteSnapshotInboxParams{DetailBefore: detail, BatchSize: batch}); err != nil {
		return result, cleanupError("delete old snapshot inbox rows", err)
	}
	if result.SnapshotRuns, err = queries.StorageCleanupDeleteSnapshotRuns(ctx, ledgerdb.StorageCleanupDeleteSnapshotRunsParams{DetailBefore: detail, BatchSize: batch}); err != nil {
		return result, cleanupError("delete redundant snapshot headers", err)
	}
	if result.SpeedObservations, err = queries.StorageCleanupDeleteSpeedObservations(ctx, ledgerdb.StorageCleanupDeleteSpeedObservationsParams{AnomalyBefore: anomaly, BatchSize: batch}); err != nil {
		return result, cleanupError("delete old speed observations", err)
	}
	if result.SeedingAnomalies, err = queries.StorageCleanupDeleteSeedingAnomalies(ctx, ledgerdb.StorageCleanupDeleteSeedingAnomaliesParams{AnomalyBefore: anomaly, BatchSize: batch}); err != nil {
		return result, cleanupError("delete old seeding anomalies", err)
	}
	if result.RawIntervals, err = queries.StorageCleanupDeleteRawIntervals(ctx, ledgerdb.StorageCleanupDeleteRawIntervalsParams{DetailBefore: detail, BatchSize: batch}); err != nil {
		return result, cleanupError("delete unreferenced raw intervals", err)
	}
	if result.Sessions, err = queries.StorageCleanupDeleteSessions(ctx, ledgerdb.StorageCleanupDeleteSessionsParams{SessionBefore: session, BatchSize: batch}); err != nil {
		return result, cleanupError("delete stale session baselines", err)
	}
	if result.LegacyInbox, err = queries.StorageCleanupDeleteLegacyInbox(ctx, ledgerdb.StorageCleanupDeleteLegacyInboxParams{DetailBefore: detail, BatchSize: batch}); err != nil {
		return result, cleanupError("delete unreferenced legacy inbox payloads", err)
	}
	return result, nil
}

func (repository *PostgresRepository) cleanupTrafficDetails(ctx context.Context, detailBefore pgtype.Timestamptz, batchSize int32) (int64, int64, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, 0, cleanupError("begin traffic detail cleanup", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := ledgerdb.New(tx)
	ids, err := queries.StorageCleanupListTrafficSettlements(ctx, ledgerdb.StorageCleanupListTrafficSettlementsParams{
		DetailBefore: detailBefore, BatchSize: batchSize,
	})
	if err != nil {
		return 0, 0, cleanupError("select traffic detail cleanup batch", err)
	}
	if len(ids) == 0 {
		return 0, 0, nil
	}
	segments, err := queries.StorageCleanupDeleteTrafficSettlementSegments(ctx, ids)
	if err != nil {
		return 0, 0, cleanupError("delete traffic settlement segments", err)
	}
	settlements, err := queries.StorageCleanupDeleteTrafficSettlements(ctx, ids)
	if err != nil {
		return 0, 0, cleanupError("delete traffic settlements", err)
	}
	if settlements != int64(len(ids)) {
		return 0, 0, fmt.Errorf("%w: selected %d traffic settlements but deleted %d", ErrInvariant, len(ids), settlements)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, cleanupError("commit traffic detail cleanup", err)
	}
	return settlements, segments, nil
}

func validCutoffs(cutoffs Cutoffs) bool {
	return !cutoffs.TerminalBefore.IsZero() && !cutoffs.SessionBefore.IsZero() &&
		!cutoffs.DetailBefore.IsZero() && !cutoffs.AnomalyBefore.IsZero() &&
		cutoffs.AnomalyBefore.Before(cutoffs.DetailBefore) &&
		!cutoffs.DetailBefore.After(cutoffs.TerminalBefore) &&
		!cutoffs.DetailBefore.After(cutoffs.SessionBefore)
}

func cleanupTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC().Truncate(time.Microsecond), Valid: true}
}

func cleanupError(operation string, err error) error {
	return fmt.Errorf("%s: %w", operation, err)
}
