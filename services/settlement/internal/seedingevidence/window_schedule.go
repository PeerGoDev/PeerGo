package seedingevidence

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

func (repository *PostgresRepository) nextWindowStart(ctx context.Context, initial time.Time) (time.Time, error) {
	if !validWindowStart(initial) {
		return time.Time{}, ErrInput
	}
	if err := repository.ensureActivityProfile(ctx); err != nil {
		return time.Time{}, err
	}
	_, err := repository.pool.Exec(ctx, `INSERT INTO settlement.seeding_window_scan(source_stream,next_window)
  SELECT $1,coalesce((
    SELECT min(h) FROM generate_series($2::timestamptz,
     coalesce((SELECT max(window_end) FROM ledger.seeding_evidence_windows),$2)-interval '1 hour',interval '1 hour') h
    WHERE NOT EXISTS(SELECT 1 FROM ledger.seeding_evidence_windows w WHERE w.window_start=h)
  ),(SELECT max(window_end) FROM ledger.seeding_evidence_windows),$2)
  ON CONFLICT(source_stream) DO NOTHING`, repository.config.AnnounceStream, initial)
	if err != nil {
		return time.Time{}, databaseError("initialize evidence scan cursor", err)
	}
	var next time.Time
	// Drain the chronological scan first. Once current, retry old gaps without
	// allowing one permanently missing fence to stop every subsequent hour.
	err = repository.pool.QueryRow(ctx, `SELECT CASE
  WHEN scan.next_window+interval '1 hour'+($2::bigint*interval '1 second')<=$3 THEN scan.next_window
  ELSE coalesce((
    SELECT r.window_start FROM settlement.seeding_window_retries r
    WHERE r.source_stream=scan.source_stream AND r.retry_after<=$3
      AND NOT EXISTS(SELECT 1 FROM ledger.seeding_evidence_windows w WHERE w.window_start=r.window_start)
    ORDER BY r.retry_after,r.window_start LIMIT 1
  ),scan.next_window) END
  FROM settlement.seeding_window_scan scan WHERE source_stream=$1`,
		repository.config.AnnounceStream, int64(repository.config.ClosureDelay/time.Second), repository.now().UTC()).Scan(&next)
	if err != nil {
		return time.Time{}, databaseError("select evidence scan or retry hour", err)
	}
	return next.UTC().Round(0), nil
}

func (repository *PostgresRepository) DeferWindow(ctx context.Context, hour, at time.Time) error {
	if !validWindowStart(hour) || at.Before(hour.Add(time.Hour).Add(repository.config.ClosureDelay)) {
		return ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(
  'peergo-settlement-seeding-window-v1:'||extract(epoch FROM $1::timestamptz)::bigint::text,0))`, hour); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO settlement.seeding_window_retries
  (source_stream,window_start,retry_after,first_seen_at,last_seen_at,reason_code)
  SELECT $1,$2,$3::timestamptz+interval '5 minutes',$3,$3,'source_coverage_pending'
  WHERE NOT EXISTS(SELECT 1 FROM ledger.seeding_evidence_windows WHERE window_start=$2)
  ON CONFLICT(source_stream,window_start) DO UPDATE
  SET retry_after=EXCLUDED.retry_after,last_seen_at=EXCLUDED.last_seen_at,attempts=settlement.seeding_window_retries.attempts+1`,
		repository.config.AnnounceStream, hour, at)
	if err != nil {
		return databaseError("persist separate evidence coverage retry", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE settlement.seeding_window_scan SET next_window=greatest(next_window,$2::timestamptz+interval '1 hour')
  WHERE source_stream=$1`, repository.config.AnnounceStream, hour); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func finishEvidenceWindow(ctx context.Context, tx pgx.Tx, stream string, hour time.Time) error {
	if _, err := tx.Exec(ctx, `DELETE FROM settlement.seeding_window_retries WHERE source_stream=$1 AND window_start=$2`, stream, hour); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE settlement.seeding_window_scan SET next_window=greatest(next_window,$2::timestamptz+interval '1 hour')
  WHERE source_stream=$1`, stream, hour)
	return err
}
