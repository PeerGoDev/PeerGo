package seedingreward_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/settlementseedingv1"
	"github.com/peergo/peergo/services/core/internal/modules/economy"
	"github.com/peergo/peergo/services/core/internal/modules/economy/seedingreward"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

// This test uses a freshly migrated disposable database.  It proves that one
// closed evidence window either commits metadata, benefits, magic, experience
// and completion together, or commits none of them.
func TestIntegrationSettlesRewardAndExperienceAtomically(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("PEERGO_TEST_CORE_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("PEERGO_TEST_CORE_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		t.Fatal(err)
	}

	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	windowStart := createdAt.Truncate(time.Hour).Add(2 * time.Hour)
	fixtureAt := windowStart.Add(-2 * time.Hour)
	userID, torrentIDs := insertEvidenceFixture(t, ctx, pool, fixtureAt)

	policyRepository, err := seedingreward.NewPostgresTimelineRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	policyService, err := seedingreward.NewTimelineService(policyRepository, func() time.Time { return createdAt })
	if err != nil {
		t.Fatal(err)
	}
	policy := integrationRewardPolicy(windowStart, "reward-it-"+strings.ReplaceAll(uuid.NewString(), "-", "")[:10])
	if _, err := policyService.Publish(ctx, policy, userID, uuid.New(), "做种奖励原子入账集成测试政策"); err != nil {
		t.Fatal(err)
	}

	item := settlementseedingv1.Item{
		UserID: userID.String(), TorrentID: torrentIDs[0], ActiveSeconds: 3600,
		RawUploadedBytes: 4096, SnapshotSeeders: 1, SnapshotLeechers: 0,
		EvidenceSHA256: digestText("settlement-reward-item"),
	}
	header := settlementseedingv1.Event{
		SchemaVersion: settlementseedingv1.SchemaVersion,
		EventID:       mustV7(t).String(), WindowStart: windowStart, WindowEnd: windowStart.Add(time.Hour),
		BuiltAt:              windowStart.Add(time.Hour + 2*time.Minute),
		WindowEvidenceSHA256: digestText("settlement-reward-window"),
		SnapshotID:           mustV7(t).String(), SnapshotSequence: 10,
		SnapshotObservedAt: windowStart.Add(55 * time.Minute),
		ItemCount:          1, ChunkIndex: 0, ChunkCount: 1, Items: []settlementseedingv1.Item{item},
	}
	projectionDigest, err := settlementseedingv1.ProjectionDigest(header, header.Items)
	if err != nil {
		t.Fatal(err)
	}
	header.ProjectionSHA256 = settlementseedingv1.DigestHex(projectionDigest)
	evidenceNow := windowStart.Add(time.Hour + 3*time.Minute)
	evidenceRepository, err := seedingreward.NewPostgresEvidenceRepository(pool, func() time.Time { return evidenceNow }, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := evidenceRepository.ApplyEvidence(ctx, encodeEvidence(t, header), header.BuiltAt)
	if err != nil || !applied.Complete {
		t.Fatalf("ApplyEvidence() = %+v, %v", applied, err)
	}

	settlementRepository, err := seedingreward.NewPostgresSettlementRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := seedingreward.NewWorker(settlementRepository, seedingreward.WorkerConfig{
		Now: func() time.Time { return evidenceNow.Add(time.Minute) },
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	processed, err := worker.RunOnce(ctx)
	if err != nil || processed != 1 {
		t.Fatalf("RunOnce() processed=%d error=%v", processed, err)
	}
	processed, err = worker.RunOnce(ctx)
	if err != nil || processed != 0 {
		t.Fatalf("RunOnce(replay) processed=%d error=%v", processed, err)
	}

	var status string
	var reward int64
	var experience string
	var transactionID, experienceID uuid.UUID
	err = pool.QueryRow(ctx, `
SELECT work.status, calculation.reward, calculation.experience_amount::text,
       calculation.magic_transaction_id, calculation.experience_entry_id
FROM economy.seeding_reward_work_items AS work
JOIN economy.seeding_reward_calculations AS calculation
  ON calculation.window_start = work.window_start AND calculation.user_id = work.user_id
WHERE work.window_start = $1 AND work.user_id = $2`, windowStart, userID).
		Scan(&status, &reward, &experience, &transactionID, &experienceID)
	if err != nil || status != "completed" || reward <= 0 || experience == "0" ||
		transactionID == uuid.Nil || experienceID == uuid.Nil {
		t.Fatalf("settlement status=%s reward=%d experience=%s transaction=%s entry=%s error=%v",
			status, reward, experience, transactionID, experienceID, err)
	}

	var postingCount int
	var postingSum int64
	if err := pool.QueryRow(ctx, `
SELECT count(*)::integer, sum(amount)::bigint
FROM economy.magic_postings
WHERE transaction_id = $1`, transactionID).Scan(&postingCount, &postingSum); err != nil || postingCount != 2 || postingSum != 0 {
		t.Fatalf("postings count=%d sum=%d error=%v", postingCount, postingSum, err)
	}
	var memberAmount, mintAmount int64
	if err := pool.QueryRow(ctx, `
SELECT
    sum(amount) FILTER (WHERE account_id = $1)::bigint,
    sum(amount) FILTER (WHERE account_id = $2)::bigint
FROM economy.magic_postings
WHERE transaction_id = $3`, userID, economy.SeedingMintAccountID(), transactionID).
		Scan(&memberAmount, &mintAmount); err != nil || memberAmount != reward || mintAmount != -reward {
		t.Fatalf("member=%d mint=%d reward=%d error=%v", memberAmount, mintAmount, reward, err)
	}

	var metadataCount, benefitCount int
	if err := pool.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM economy.seeding_reward_metadata_snapshots WHERE window_start = $1),
    (SELECT count(*) FROM economy.seeding_reward_benefit_snapshots WHERE window_start = $1 AND user_id = $2)`,
		windowStart, userID).Scan(&metadataCount, &benefitCount); err != nil || metadataCount != 1 || benefitCount != 1 {
		t.Fatalf("metadata=%d benefits=%d error=%v", metadataCount, benefitCount, err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE economy.seeding_reward_benefit_snapshots
SET vip_active = NOT vip_active
WHERE window_start = $1 AND user_id = $2`, windowStart, userID); err == nil {
		t.Fatal("immutable benefit snapshot unexpectedly accepted update")
	}
}

func integrationRewardPolicy(effectiveAt time.Time, revision string) seedingreward.PolicyRevision {
	return seedingreward.PolicyRevision{
		Revision: revision, FormulaVersion: seedingreward.FormulaVersion,
		EffectiveFrom:        effectiveAt,
		CurveHourlyCapMilli:  100_000,
		AgeSaturationSeconds: int64((4 * 7 * 24 * time.Hour) / time.Second),
		SeederDecay:          7, CurveScaleMilli: 300_000, SizeMultiplierBPS: 10_000,
		OfficialBonusBPS: 10_000, UploadContributionBonusBPS: 5_000,
		PerTorrentHourlyMilli: 1_000, BaseLinearTorrentLimit: 50,
		MaximumLevelTorrentBonus: 100,
		MinimumTorrentBytes:      1, MinimumActiveSeconds: 300,
		MaximumSnapshotAgeSeconds: 900,
		VIPBonusBPS:               2_000, MaximumMedalBonusBPS: 10_000,
		MaximumLevelBonusBPS: 10_000, MaximumHourlyReward: 1_000,
		ExperiencePerMagicBPS: 1_000,
	}
}
