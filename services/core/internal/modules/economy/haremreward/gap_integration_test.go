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

func TestIntegrationDefersGapsAndBackfillsWithoutDuplicateRewards(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("PEERGO_TEST_CORE_DATABASE_URL"))
	if url == "" {
		t.Skip("PEERGO_TEST_CORE_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 8, 21, 5, 0, 0, 0, time.UTC)
	now := start.Add(25 * time.Hour)
	inviter := insertHaremUser(t, ctx, pool, "gap-inviter", start)
	invitee := insertHaremUser(t, ctx, pool, "gap-invitee", start)
	run := uuid.New()
	digest := sha256.Sum256([]byte(run.String()))
	if _, err := pool.Exec(ctx, `INSERT INTO migration.runs(id,source_system,source_snapshot_sha256,mapping_version,state,
 expected_user_rows,expected_torrent_rows,created_at,state_changed_at)
 VALUES($1,'ptyes',$2,$3,'imported',0,0,$4,$4)`, run, digest[:], "gap-it-"+run.String()[:8], start.Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO identity.invitation_relationships(invitee_user_id,inviter_user_id,source_kind,
 source_reference,source_run_id,source_fingerprint,established_at,recorded_at)
 VALUES($1,$2,'legacy_import',$3,$4,$5,$6,$6)`, invitee, inviter, "harem-gap:"+run.String(), run, digest[:], start.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	ledger, err := economy.NewPostgresRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	addSource := func(hour int) {
		t.Helper()
		h := start.Add(time.Duration(hour) * time.Hour)
		insertCompleteHaremEvidenceWindow(t, ctx, pool, h, int64(hour+1))
		ref := fmt.Sprintf("harem-gap:%s:%d", run, hour)
		sum := sha256.Sum256([]byte(ref))
		tx, err := ledger.Record(ctx, economy.RecordCommand{TransactionID: uuid.New(), TransactionType: economy.TransactionSeedingReward,
			IdempotencyKey: ref, SourceReference: ref, PolicyRevision: "rousi-reward-v1", PayloadSHA256: sum, OccurredAt: h.Add(time.Hour), RecordedAt: h.Add(time.Hour),
			Postings: []economy.PostingInput{{AccountID: invitee, Amount: 500}, {AccountID: economy.SeedingMintAccountID(), Amount: -500}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO economy.seeding_reward_calculations(window_start,user_id,policy_revision,calculation_sha256,
 eligible_torrent_count,value_score_micro,curve_reward_milli,linear_reward_milli,base_reward_milli,vip_bonus_milli,
 medal_bonus_milli,level_bonus_milli,uncapped_reward,reward,experience_amount,capped,magic_transaction_id,calculated_at)
 VALUES($1,$2,'rousi-reward-v1',$3,1,0,0,0,0,0,0,0,500,500,0,false,$4,$5)`, h, invitee, sum[:], tx.ID, h.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO economy.seeding_reward_work_items(window_start,user_id,status,attempts,
 available_at,completed_at,created_at,updated_at) VALUES($1,$2,'completed',1,$3,$3,$3,$3)`, h, invitee, h.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	for h := 0; h < 24; h++ {
		if h != 2 {
			addSource(h)
		}
	}
	// The first three periods are independently pinned by missing evidence,
	// pending work, and dead work. The fourth period must still settle.
	for _, pin := range []struct {
		hour   int
		status string
	}{{8, "pending"}, {15, "dead"}} {
		if _, err := pool.Exec(ctx, `UPDATE economy.seeding_reward_work_items SET status=$2,completed_at=NULL WHERE window_start=$1`, start.Add(time.Duration(pin.hour)*time.Hour), pin.status); err != nil {
			t.Fatal(err)
		}
	}
	repo, err := haremreward.NewPostgresRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	// Abort after ledger writes but before the receipt: all balances and facts
	// must roll back, so a retry can safely pay exactly once.
	if _, err := pool.Exec(ctx, `CREATE FUNCTION economy.harem_test_abort() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN RAISE EXCEPTION 'injected receipt failure'; END; $$;
 CREATE TRIGGER harem_test_abort BEFORE INSERT ON economy.harem_reward_payouts
 FOR EACH ROW EXECUTE FUNCTION economy.harem_test_abort()`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SettleNext(ctx, now); err == nil {
		t.Fatal("injected receipt failure was ignored")
	}
	var balance, count int64
	if err := pool.QueryRow(ctx, `SELECT coalesce((SELECT balance FROM economy.magic_accounts WHERE id=$1),0),(SELECT count(*) FROM economy.harem_reward_windows)`, inviter).Scan(&balance, &count); err != nil {
		t.Fatal(err)
	}
	if balance != 0 || count != 0 {
		t.Fatalf("failed transaction leaked balance=%d windows=%d", balance, count)
	}
	if _, err := pool.Exec(ctx, `DROP TRIGGER harem_test_abort ON economy.harem_reward_payouts;DROP FUNCTION economy.harem_test_abort()`); err != nil {
		t.Fatal(err)
	}
	settle := func(hour, hours int, want int64) {
		t.Helper()
		v, err := repo.SettleNext(ctx, now)
		if err != nil {
			t.Fatal(err)
		}
		if !v.Processed || !v.WindowStart.Equal(start.Add(time.Duration(hour)*time.Hour)) ||
			!v.WindowEnd.Equal(start.Add(time.Duration(hour+hours)*time.Hour)) || v.TotalReward != want ||
			v.SourceCalculationCount != int64(hours) || v.RecipientCount != 1 {
			t.Fatalf("unexpected settlement: %+v", v)
		}
	}
	settle(18, 6, 300)
	waiting, err := repo.SettleNext(ctx, now)
	if err != nil || waiting.Processed || !waiting.WaitingForEvidence || !waiting.WindowStart.Equal(start) {
		t.Fatalf("old gaps were not retained: %+v %v", waiting, err)
	}
	var code *string
	if err := pool.QueryRow(ctx, `SELECT last_error_code FROM economy.harem_reward_worker_state`).Scan(&code); err != nil {
		t.Fatal(err)
	}
	if code == nil || *code != "source_coverage_pending" {
		t.Fatalf("old source gap hidden as healthy idle: %v", code)
	}
	addSource(2)
	settle(0, 6, 300)
	if _, err := pool.Exec(ctx, `UPDATE economy.seeding_reward_work_items SET status='completed',completed_at=window_start+interval '1 hour'
 WHERE window_start=$1`, start.Add(8*time.Hour)); err != nil {
		t.Fatal(err)
	}
	settle(6, 6, 300)
	if _, err := pool.Exec(ctx, `UPDATE economy.seeding_reward_work_items SET status='completed',completed_at=window_start+interval '1 hour'
 WHERE window_start=$1`, start.Add(15*time.Hour)); err != nil {
		t.Fatal(err)
	}
	settle(12, 6, 300)
	idle, err := repo.SettleNext(ctx, now)
	if err != nil || idle.Processed || idle.WaitingForEvidence {
		t.Fatalf("replay is not idle: %+v %v", idle, err)
	}
	// A policy revision changes future window duration and reward rate while
	// retaining the original 05:00 anchor and the immutable settled boundaries.
	digest = sha256.Sum256([]byte("gap-policy"))
	if _, err := pool.Exec(ctx, `INSERT INTO economy.harem_reward_policy_revisions(revision,enabled,reward_bps,depth,
 minimum_seed_count,hourly_cap,activity_days,settlement_hours,effective_from,snapshot_json,snapshot_sha256,
 reason,created_at) VALUES('harem-gap-v2',true,500,1,1,100,30,3,$1,'{}',$2,'integration policy boundary check','2026-09-04T06:00:03Z')`, start.Add(24*time.Hour), digest[:]); err != nil {
		t.Fatal(err)
	}
	for h := 24; h < 30; h++ {
		addSource(h)
	}
	idle, err = repo.SettleNext(ctx, now)
	if err != nil || idle.Processed {
		t.Fatalf("unclosed future period settled: %+v %v", idle, err)
	}
	now = start.Add(30 * time.Hour)
	settle(24, 3, 75)
	settle(27, 3, 75)
	idle, err = repo.SettleNext(ctx, now)
	if err != nil || idle.Processed || idle.WaitingForEvidence {
		t.Fatalf("final replay is not idle: %+v %v", idle, err)
	}
	var total, txs, unbalanced int64
	if err := pool.QueryRow(ctx, `SELECT sum(reward),count(DISTINCT magic_transaction_id),
 (SELECT count(*) FROM(SELECT p.transaction_id,sum(p.amount::numeric) n,count(*) c
 FROM economy.magic_postings p JOIN economy.harem_reward_payouts r ON r.magic_transaction_id=p.transaction_id
 GROUP BY p.transaction_id HAVING sum(p.amount::numeric)<>0 OR count(*)<>2)x)
 FROM economy.harem_reward_payouts`).Scan(&total, &txs, &unbalanced); err != nil {
		t.Fatal(err)
	}
	if total != 1350 || txs != 6 || unbalanced != 0 {
		t.Fatalf("rewards=%d transactions=%d unbalanced=%d", total, txs, unbalanced)
	}
	if err := pool.QueryRow(ctx, `SELECT balance FROM economy.magic_accounts WHERE id=$1`, inviter).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if balance != 1350 {
		t.Fatalf("inviter balance %d !=1350", balance)
	}
}
