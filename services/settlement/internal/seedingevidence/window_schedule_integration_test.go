package seedingevidence

import (
	"context"
	"github.com/jackc/pgx/v5"
	"os"
	"testing"
	"time"
)

// The query fixture is intentionally separate from migrated/production data.
func TestEvidenceRetrySchedulingKeepsRecentHoursCurrentWithoutLosingOldGaps(t *testing.T) {
	url := os.Getenv("PEERGO_TEST_SEEDING_SCHEDULE_DATABASE_URL")
	if url == "" {
		t.Skip("PEERGO_TEST_SEEDING_SCHEDULE_DATABASE_URL is required")
	}
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database != "seeding_schedule_test" {
		t.Fatal("requires disposable seeding_schedule_test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	exec := func(q string) {
		t.Helper()
		if _, err := tx.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE SCHEMA settlement;CREATE SCHEMA ledger;
 CREATE TABLE settlement.seeding_window_scan(source_stream text PRIMARY KEY,next_window timestamptz);
 CREATE TABLE settlement.seeding_window_retries(source_stream text,window_start timestamptz,retry_after timestamptz);
 CREATE TABLE ledger.seeding_evidence_windows(window_start timestamptz PRIMARY KEY);
 INSERT INTO settlement.seeding_window_scan VALUES('A','2026-10-17T10:00Z');
 INSERT INTO settlement.seeding_window_retries VALUES
 ('A','2026-10-01T00:00Z','2026-10-17T10:00Z'),
 ('A','2026-10-17T09:00Z','2026-10-17T10:50Z'),
 ('A','2026-10-17T08:00Z','2026-10-17T11:00Z'),
 ('A','2026-10-17T07:00Z','2026-10-17T09:00Z'),
 ('B','2026-10-17T06:00Z','2026-10-17T09:00Z');
 INSERT INTO ledger.seeding_evidence_windows VALUES('2026-10-17T07:00Z');`)
	now := time.Date(2026, 10, 17, 10, 55, 0, 0, time.UTC)
	check := func(want string) {
		t.Helper()
		var got time.Time
		if err := tx.QueryRow(ctx, nextEvidenceWindowSQL, "A", int64(2700), now).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got.UTC().Format(time.RFC3339) != want {
			t.Fatalf("got %s want %s", got, want)
		}
	}
	check("2026-10-17T09:00:00Z")
	exec(`UPDATE settlement.seeding_window_retries SET retry_after='2026-10-17T11:00Z' WHERE source_stream='A' AND window_start='2026-10-17T09:00Z'`)
	check("2026-10-01T00:00:00Z") // old gaps still run during recent backoff
	exec(`UPDATE settlement.seeding_window_scan SET next_window='2026-10-17T08:00Z' WHERE source_stream='A'`)
	check("2026-10-17T08:00:00Z") // chronological scan always takes precedence
	exec(`UPDATE settlement.seeding_window_scan SET next_window='2026-10-17T10:00Z' WHERE source_stream='A';
 UPDATE settlement.seeding_window_retries SET retry_after='2026-10-17T11:00Z' WHERE source_stream='A'`)
	check("2026-10-17T10:00:00Z") // no due retry: return future scan, do not skip backoff
	var count int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM settlement.seeding_window_retries").Scan(&count); err != nil || count != 5 {
		t.Fatalf("retry history changed: count=%d err=%v", count, err)
	}
}
