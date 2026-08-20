package seedingreward

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func TestPostgresTimelinePublishesResolvesAndProtectsPolicies(t *testing.T) {
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

	issuerID := uuid.New()
	username := "seeding-policy-it-" + issuerID.String()[:8]
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.users (id, credential_ref, username, display_name, status)
VALUES ($1, $2, $3, $3, 'active')`, issuerID, uuid.New(), username); err != nil {
		t.Fatalf("insert policy issuer: %v", err)
	}
	repository, err := NewPostgresTimelineRepository(pool)
	if err != nil {
		t.Fatalf("NewPostgresTimelineRepository() error = %v", err)
	}
	service, err := NewTimelineService(repository)
	if err != nil {
		t.Fatalf("NewTimelineService() error = %v", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	first := testPolicy()
	first.Revision = "seeding-it-" + issuerID.String()[:8]
	first.CreatedAt = now
	first.EffectiveFrom = now.Truncate(time.Hour).Add(2 * time.Hour)
	decisionID := uuid.New()
	created, err := service.Publish(ctx, first, issuerID, decisionID, "集成测试签发第一版未来做种奖励政策")
	if err != nil || created.Replayed {
		t.Fatalf("Publish(first) = %+v, %v", created, err)
	}
	replayed, err := service.Publish(ctx, first, issuerID, decisionID, "集成测试签发第一版未来做种奖励政策")
	if err != nil || !replayed.Replayed || replayed.Policy.SnapshotSHA256 != created.Policy.SnapshotSHA256 {
		t.Fatalf("Publish(replay) = %+v, %v", replayed, err)
	}
	if _, err := service.Publish(ctx, first, issuerID, decisionID, "相同修订但签发原因发生改变应当冲突"); !errors.Is(err, ErrPolicyConflict) {
		t.Fatalf("changed replay error = %v", err)
	}

	second := first
	second.Revision += "-next"
	second.EffectiveFrom = first.EffectiveFrom.Add(2 * time.Hour)
	second.CurveHourlyCapMilli++
	createdSecond, err := service.Publish(ctx, second, issuerID, uuid.New(), "集成测试签发第二版未来做种奖励政策")
	if err != nil {
		t.Fatalf("Publish(second) error = %v", err)
	}
	resolvedFirst, err := service.Resolve(ctx, first.EffectiveFrom.Add(30*time.Minute))
	if err != nil || resolvedFirst.Policy.Revision != first.Revision {
		t.Fatalf("Resolve(first) = %+v, %v", resolvedFirst, err)
	}
	resolvedSecond, err := service.Resolve(ctx, second.EffectiveFrom)
	if err != nil || resolvedSecond.Policy.Revision != createdSecond.Policy.Revision {
		t.Fatalf("Resolve(second) = %+v, %v", resolvedSecond, err)
	}

	if _, err := pool.Exec(ctx, `
UPDATE economy.seeding_reward_policy_revisions
SET reason = reason || 'x'
WHERE revision = $1`, first.Revision); err == nil {
		t.Fatal("immutable policy unexpectedly accepted update")
	}
	backfill := second
	backfill.Revision += "-backfill"
	backfill.EffectiveFrom = first.EffectiveFrom.Add(time.Hour)
	backfill.CreatedAt = now
	backfill, snapshot, err := NormalizePolicy(backfill)
	if err != nil {
		t.Fatalf("NormalizePolicy(backfill) error = %v", err)
	}
	_, err = pool.Exec(ctx, `
INSERT INTO economy.seeding_reward_policy_revisions (`+policyColumns+`)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
    $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27
)`, policyArguments(backfill, snapshot, PublishCommand{
		Policy: backfill, IssuedBy: issuerID, AuthorizationDecisionID: uuid.New(),
		Reason: "绕过服务插入旧时间点也必须由数据库拒绝", SnapshotJSON: snapshot,
	})...)
	if err == nil {
		t.Fatal("non-append policy unexpectedly accepted direct insert")
	}

	var count int64
	if err := pool.QueryRow(ctx, `
SELECT count(*)::bigint
FROM economy.seeding_reward_policy_revisions
WHERE issued_by = $1`, issuerID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("policy count = %d, error=%v", count, err)
	}
}
