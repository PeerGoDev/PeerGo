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

func TestLegacyEconomySeedCreatesBalancedOpeningsAndReplaysExactly(t *testing.T) {
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

	// Opening evidence is immutable. Run this only on a disposable migrated
	// database; keeping production triggers intact is part of the test.
	now := time.Now().UTC().Truncate(time.Microsecond)
	runID := uuid.New()
	userIDs := []uuid.UUID{uuid.New(), uuid.New()}
	credentialIDs := []uuid.UUID{uuid.New(), uuid.New()}
	ledgerIDs := []uuid.UUID{uuid.New(), uuid.New()}
	legacyBase := now.UnixNano() / 10
	snapshotDigest := sha256.Sum256([]byte(runID.String()))
	if _, err := pool.Exec(ctx, `
INSERT INTO migration.runs (
    id, source_system, source_snapshot_sha256, mapping_version, state,
    expected_user_rows, expected_torrent_rows, created_at, state_changed_at
) VALUES ($1, 'ptyes', $2, 'economy-integration-v1', 'planned', 2, 0, $3, $3)`,
		runID, snapshotDigest[:], now); err != nil {
		t.Fatalf("insert migration run: %v", err)
	}
	for index, userID := range userIDs {
		username := fmt.Sprintf("legacy-economy-%s", userID.String()[:8])
		legacyID := legacyBase + int64(index) + 1
		if _, err := pool.Exec(ctx, `
INSERT INTO identity.users (id, credential_ref, username, display_name, status, created_at, updated_at)
VALUES ($1, $2, $3, $3, 'active', $4, $4)`, userID, credentialIDs[index], username, now); err != nil {
			t.Fatalf("insert identity user %d: %v", index, err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO migration.user_id_map (
    source_system, legacy_user_id, user_id, credential_ref, first_run_id, created_at
) VALUES ('ptyes', $1, $2, $3, $4, $5)`, legacyID, userID, credentialIDs[index], runID, now); err != nil {
			t.Fatalf("insert user mapping %d: %v", index, err)
		}
		karma := "100.4"
		experience := "1500.25"
		fingerprint := sha256.Sum256([]byte(fmt.Sprintf("positive:%d", legacyID)))
		if index == 1 {
			karma = "-25.4"
			experience = "12.5"
			fingerprint = sha256.Sum256([]byte(fmt.Sprintf("negative:%d", legacyID)))
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO migration.user_operational_openings (
    source_system, legacy_user_id, user_id, source_uploaded, source_downloaded,
    source_karma, source_pt_coin, pt_coin_to_magic_rate, magic_balance,
    source_experience, source_last_active_at, source_fingerprint,
    first_run_id, imported_at
) VALUES (
    'ptyes', $1, $2, 0, 0, $3::numeric, 0, 5000, round($3::numeric)::bigint,
    $4::numeric, NULL, $5, $6, $7
)`, legacyID, userID, karma, experience, fingerprint[:], runID, now.Add(time.Minute)); err != nil {
			t.Fatalf("insert operational opening %d: %v", index, err)
		}
	}

	seed := func() {
		t.Helper()
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			t.Fatalf("begin seed transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `
CREATE TEMP TABLE legacy_user_operational_stage (
    legacy_id bigint PRIMARY KEY,
    ledger_id uuid NOT NULL
) ON COMMIT DROP`); err != nil {
			t.Fatalf("create temporary legacy stage: %v", err)
		}
		for index := range userIDs {
			if _, err := tx.Exec(ctx, `
INSERT INTO legacy_user_operational_stage (legacy_id, ledger_id)
VALUES ($1, $2)`, legacyBase+int64(index)+1, ledgerIDs[index]); err != nil {
				t.Fatalf("insert temporary legacy stage %d: %v", index, err)
			}
		}
		importer := &Importer{core: pool, config: Config{RunID: runID, OccurredAt: now}}
		if err := importer.seedEconomy(ctx, tx); err != nil {
			t.Fatalf("seedEconomy() error = %v", err)
		}
		if err := importer.seedProgressAndActivity(ctx, tx); err != nil {
			t.Fatalf("seedProgressAndActivity() error = %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit legacy seed: %v", err)
		}
	}
	seed()
	seed()

	var accounts, transactions, postings, statements, experienceEntries, brokenTransactions int64
	if err := pool.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM economy.magic_accounts WHERE user_id = ANY($1::uuid[])),
    (SELECT count(*) FROM economy.magic_transactions WHERE id = ANY($2::uuid[])),
    (SELECT count(*) FROM economy.magic_postings WHERE transaction_id = ANY($2::uuid[])),
    (SELECT count(*) FROM economy.magic_ledger_entries WHERE transaction_id = ANY($2::uuid[])),
    (SELECT count(*) FROM progression.experience_entries WHERE id = ANY($2::uuid[])),
    (SELECT count(*) FROM (
        SELECT transaction_id
        FROM economy.magic_postings
        WHERE transaction_id = ANY($2::uuid[])
        GROUP BY transaction_id
        HAVING count(*) <> 2 OR sum(amount) <> 0
    ) AS broken)`, userIDs, ledgerIDs).Scan(
		&accounts, &transactions, &postings, &statements, &experienceEntries, &brokenTransactions,
	); err != nil {
		t.Fatalf("read legacy economy graph: %v", err)
	}
	if accounts != 2 || transactions != 2 || postings != 4 || statements != 2 || experienceEntries != 2 || brokenTransactions != 0 {
		t.Fatalf("accounts=%d transactions=%d postings=%d statements=%d experience=%d broken=%d",
			accounts, transactions, postings, statements, experienceEntries, brokenTransactions)
	}
	var positiveMagic, negativeMagic int64
	var positiveExperience, negativeExperience string
	if err := pool.QueryRow(ctx, `
SELECT
    (SELECT balance FROM economy.magic_accounts WHERE user_id = $1),
    (SELECT balance FROM economy.magic_accounts WHERE user_id = $2),
    (SELECT balance_after::text FROM progression.experience_entries WHERE user_id = $1),
    (SELECT balance_after::text FROM progression.experience_entries WHERE user_id = $2)`,
		userIDs[0], userIDs[1]).Scan(
		&positiveMagic, &negativeMagic, &positiveExperience, &negativeExperience,
	); err != nil {
		t.Fatalf("read migrated balances: %v", err)
	}
	if positiveMagic != 100 || negativeMagic != -25 ||
		positiveExperience != "1500.25000000000000000000" || negativeExperience != "12.50000000000000000000" {
		t.Fatalf("magic=(%d,%d) experience=(%s,%s)", positiveMagic, negativeMagic, positiveExperience, negativeExperience)
	}
}
