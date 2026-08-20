package legacyuserstate

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func TestLegacyDownloadRestrictionSeedsTypedStateAndTransition(t *testing.T) {
	databaseURL := os.Getenv("PEERGO_TEST_CORE_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PEERGO_TEST_CORE_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	if err := platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		t.Fatalf("RequireCurrentMigration() error = %v", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	now := time.Now().UTC().Truncate(time.Microsecond)
	runID, userID, credentialID := uuid.New(), uuid.New(), uuid.New()
	legacyID := now.UnixNano()/10 + 1
	fingerprint := sha256.Sum256([]byte(fmt.Sprintf("restricted:%d", legacyID)))
	if _, err := tx.Exec(ctx, `
INSERT INTO migration.runs (
    id, source_system, source_snapshot_sha256, mapping_version, state,
    expected_user_rows, expected_torrent_rows, created_at, state_changed_at
) VALUES ($1, 'ptyes', $2, 'access-state-integration-v1', 'planned', 1, 0, $3, $3)`,
		runID, fingerprint[:], now); err != nil {
		t.Fatalf("insert migration run: %v", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.users (
    id, credential_ref, username, display_name, status, created_at, updated_at
) VALUES ($1,$2,$3,$3,'active',$4,$4)`,
		userID, credentialID, fmt.Sprintf("legacy-access-%s", userID.String()[:8]), now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO migration.user_id_map (
    source_system, legacy_user_id, user_id, credential_ref, first_run_id, created_at
) VALUES ('ptyes',$1,$2,$3,$4,$5)`, legacyID, userID, credentialID, runID, now); err != nil {
		t.Fatalf("insert user mapping: %v", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO migration.user_status_openings (
    source_system, legacy_user_id, user_id, source_banned,
    source_download_restricted, source_email_verified, source_vip_enabled,
    source_fingerprint, first_run_id, imported_at
) VALUES ('ptyes',$1,$2,false,true,true,false,$3,$4,$5)`,
		legacyID, userID, fingerprint[:], runID, now); err != nil {
		t.Fatalf("insert status opening: %v", err)
	}
	if _, err := tx.Exec(ctx, `
CREATE TEMP TABLE legacy_user_operational_stage (
    legacy_id bigint PRIMARY KEY
) ON COMMIT DROP`); err != nil {
		t.Fatalf("create stage: %v", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO legacy_user_operational_stage (legacy_id) VALUES ($1)`, legacyID); err != nil {
		t.Fatalf("seed stage: %v", err)
	}

	importer := &Importer{core: pool, config: Config{RunID: runID, OccurredAt: now}}
	if err := importer.seedUserAccessStates(ctx, tx); err != nil {
		t.Fatalf("first seedUserAccessStates() error = %v", err)
	}
	if err := importer.seedUserAccessStates(ctx, tx); err != nil {
		t.Fatalf("replayed seedUserAccessStates() error = %v", err)
	}

	var restricted bool
	var origin, reasonCode string
	var startedAt time.Time
	if err := tx.QueryRow(ctx, `
SELECT download_restricted, download_restriction_origin,
       download_restriction_reason_code, download_restriction_started_at
FROM identity.user_access_states
WHERE user_id=$1`, userID).Scan(&restricted, &origin, &reasonCode, &startedAt); err != nil {
		t.Fatalf("read access state: %v", err)
	}
	if !restricted || origin != "legacy_migration" || reasonCode != "legacy_download_restriction" || !startedAt.Equal(now) {
		t.Fatalf("access state = restricted=%t origin=%q reason=%q started=%s", restricted, origin, reasonCode, startedAt)
	}
	var transitions int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM identity.manual_download_restriction_transitions
WHERE user_id=$1 AND transition='restricted' AND origin='legacy_migration'
  AND reason_code='legacy_download_restriction'
  AND NOT from_restricted AND to_restricted
  AND from_state_version=0 AND state_version=1`, userID).Scan(&transitions); err != nil {
		t.Fatalf("read restriction transitions: %v", err)
	}
	if transitions != 1 {
		t.Fatalf("restriction transitions = %d, want 1", transitions)
	}
}
