package haremreward_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/economy"
	"github.com/peergo/peergo/services/core/internal/modules/economy/haremreward"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func TestIntegrationSettlesSixHourlyEntitlementsOnce(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("PEERGO_TEST_CORE_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("PEERGO_TEST_CORE_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		t.Fatal(err)
	}

	windowStart := time.Date(2026, 8, 21, 5, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(6 * time.Hour)
	inviterID := insertHaremUser(t, ctx, pool, "inviter", windowStart)
	inviteeIDs := []uuid.UUID{
		insertHaremUser(t, ctx, pool, "invitee-a", windowStart),
		insertHaremUser(t, ctx, pool, "invitee-b", windowStart),
		insertHaremUser(t, ctx, pool, "invitee-c", windowStart),
	}
	runID := uuid.New()
	digest := sha256.Sum256([]byte("harem-integration-run-" + runID.String()))
	if _, err := pool.Exec(ctx, `
INSERT INTO migration.runs (
    id, source_system, source_snapshot_sha256, mapping_version, state,
    expected_user_rows, expected_torrent_rows, created_at, state_changed_at
) VALUES ($1, 'ptyes', $2, $3, 'imported', 0, 0, $4, $4)`,
		runID, digest[:], "harem-it-"+runID.String()[:8], windowStart.Add(-48*time.Hour)); err != nil {
		t.Fatalf("insert migration run: %v", err)
	}
	for index, inviteeID := range inviteeIDs {
		relationshipDigest := sha256.Sum256([]byte(inviteeID.String()))
		if _, err := pool.Exec(ctx, `
INSERT INTO identity.invitation_relationships (
    invitee_user_id, inviter_user_id, source_kind, source_reference,
    source_run_id, source_fingerprint, established_at, recorded_at
) VALUES ($1,$2,'legacy_import',$3,$4,$5,$6,$6)`,
			inviteeID, inviterID, fmt.Sprintf("harem-it:%s:%d", runID, index),
			runID, relationshipDigest[:], windowStart.Add(-24*time.Hour)); err != nil {
			t.Fatalf("insert invitation relationship: %v", err)
		}
	}

	ledger, err := economy.NewPostgresRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	for hour := 0; hour < 6; hour++ {
		sourceWindow := windowStart.Add(time.Duration(hour) * time.Hour)
		insertCompleteHaremEvidenceWindow(t, ctx, pool, sourceWindow, int64(hour+1))
		for index, inviteeID := range inviteeIDs {
			reference := fmt.Sprintf("harem-it-seeding:%s:%d:%d", runID, hour, index)
			transactionDigest := sha256.Sum256([]byte(reference))
			transaction, err := ledger.Record(ctx, economy.RecordCommand{
				TransactionID: uuid.New(), TransactionType: economy.TransactionSeedingReward,
				IdempotencyKey: reference, SourceReference: reference,
				PolicyRevision: "rousi-reward-v1", PayloadSHA256: transactionDigest,
				OccurredAt: sourceWindow.Add(time.Hour), RecordedAt: sourceWindow.Add(time.Hour),
				Postings: []economy.PostingInput{
					{AccountID: inviteeID, Amount: 500},
					{AccountID: economy.SeedingMintAccountID(), Amount: -500},
				},
			})
			if err != nil {
				t.Fatalf("record source seeding reward: %v", err)
			}
			calculationDigest := sha256.Sum256([]byte("calculation:" + reference))
			if _, err := pool.Exec(ctx, `
INSERT INTO economy.seeding_reward_calculations (
    window_start, user_id, policy_revision, calculation_sha256,
    eligible_torrent_count, value_score_micro, curve_reward_milli,
    linear_reward_milli, base_reward_milli, vip_bonus_milli,
    medal_bonus_milli, level_bonus_milli, uncapped_reward, reward,
    experience_amount, capped, magic_transaction_id, calculated_at
) VALUES ($1,$2,'rousi-reward-v1',$3,1,0,0,0,0,0,0,0,500,500,0,false,$4,$5)`,
				sourceWindow, inviteeID, calculationDigest[:], transaction.ID,
				sourceWindow.Add(time.Hour)); err != nil {
				t.Fatalf("insert source calculation: %v", err)
			}
			if _, err := pool.Exec(ctx, `
INSERT INTO economy.seeding_reward_work_items (
    window_start, user_id, status, attempts, available_at,
    completed_at, created_at, updated_at
) VALUES ($1,$2,'completed',1,$3,$3,$3,$3)`,
				sourceWindow, inviteeID, sourceWindow.Add(time.Hour)); err != nil {
				t.Fatalf("insert completed source work: %v", err)
			}
		}
	}

	repository, err := haremreward.NewPostgresRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	settlement, err := repository.SettleNext(ctx, windowEnd.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !settlement.Processed || settlement.WindowStart != windowStart ||
		settlement.WindowEnd != windowEnd || settlement.SourceCalculationCount != 18 ||
		settlement.EligibleRelationshipCount != 3 || settlement.RecipientCount != 1 ||
		settlement.TotalReward != 600 {
		t.Fatalf("unexpected settlement: %+v", settlement)
	}
	replay, err := repository.SettleNext(ctx, windowEnd.Add(time.Minute))
	if err != nil || replay.Processed {
		t.Fatalf("replay settlement = %+v, %v", replay, err)
	}

	var reward int64
	var inviteeCount, inviteeHours, cappedHours int
	var transactionID uuid.UUID
	if err := pool.QueryRow(ctx, `
SELECT reward, eligible_invitee_count, eligible_invitee_hours,
       capped_hour_count, magic_transaction_id
FROM economy.harem_reward_payouts
WHERE window_start = $1 AND inviter_user_id = $2`,
		windowStart, inviterID).Scan(
		&reward, &inviteeCount, &inviteeHours, &cappedHours, &transactionID,
	); err != nil {
		t.Fatal(err)
	}
	if reward != 600 || inviteeCount != 3 || inviteeHours != 18 ||
		cappedHours != 6 || transactionID == uuid.Nil {
		t.Fatalf(
			"payout reward=%d invitees=%d hours=%d capped=%d transaction=%s",
			reward, inviteeCount, inviteeHours, cappedHours, transactionID,
		)
	}
	var postingCount int
	var postingSum int64
	if err := pool.QueryRow(ctx, `
SELECT count(*)::integer, sum(amount)::bigint
FROM economy.magic_postings
WHERE transaction_id = $1`, transactionID).Scan(&postingCount, &postingSum); err != nil {
		t.Fatal(err)
	}
	if postingCount != 2 || postingSum != 0 {
		t.Fatalf("harem ledger postings=%d sum=%d", postingCount, postingSum)
	}
	if _, err := pool.Exec(ctx, `
UPDATE economy.harem_reward_payouts
SET reward = reward + 1
WHERE window_start = $1 AND inviter_user_id = $2`, windowStart, inviterID); err == nil {
		t.Fatal("immutable harem payout unexpectedly accepted update")
	}
}

func insertHaremUser(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	suffix string,
	at time.Time,
) uuid.UUID {
	t.Helper()
	userID := uuid.New()
	username := "harem-it-" + suffix + "-" + userID.String()[:8]
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.users (
    id, credential_ref, username, display_name, status, created_at, updated_at
) VALUES ($1,$2,$3,$3,'active',$4,$4)`,
		userID, uuid.New(), username, at.Add(-72*time.Hour)); err != nil {
		t.Fatalf("insert harem user: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.user_activity (user_id, last_active_at, updated_at)
VALUES ($1,$2,$2)`, userID, at.Add(7*time.Hour)); err != nil {
		t.Fatalf("insert harem user activity: %v", err)
	}
	return userID
}

func insertCompleteHaremEvidenceWindow(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	start time.Time,
	sequence int64,
) {
	t.Helper()
	end := start.Add(time.Hour)
	digest := sha256.Sum256([]byte("harem-window-" + start.Format(time.RFC3339)))
	if _, err := pool.Exec(ctx, `
INSERT INTO economy.seeding_reward_evidence_windows (
    window_start, window_end, built_at, window_evidence_sha256,
    projection_sha256, snapshot_id, snapshot_sequence,
    snapshot_observed_at, item_count, chunk_count,
    received_chunk_count, status, created_at, completed_at
) VALUES ($1,$2,$2,$3,$3,$4,$5,$2,0,1,1,'complete',$2,$2)`,
		start, end, digest[:], uuid.New(), sequence); err != nil {
		t.Fatalf("insert harem evidence window: %v", err)
	}
}
