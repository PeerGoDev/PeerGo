package catalog_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/audit"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
)

func TestPostgresCategoryAdministrationCommitsVersionedStateAndAuditTogether(t *testing.T) {
	databaseURL := os.Getenv("PEERGO_TEST_CORE_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PEERGO_TEST_CORE_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)

	eventBuilder, err := audit.NewCategoryChangeEventBuilder(audit.RecorderConfig{
		PseudonymKey:      bytes.Repeat([]byte{0x63}, 32),
		PseudonymKeyEpoch: "integration-2026-08",
	})
	if err != nil {
		t.Fatalf("NewCategoryChangeEventBuilder() error = %v", err)
	}
	repository, err := catalog.NewPostgresCategoryAdministrationRepository(
		pool,
		eventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		t.Fatalf("NewPostgresCategoryAdministrationRepository() error = %v", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	categoryID := "integration-" + uuid.NewString()[:8]
	createdName := "集成测试分类"
	updatedName := "集成测试分类（停用）"
	createReason := "集成测试验证分类创建与审计证据原子提交。"
	updateReason := "集成测试验证分类停用、版本冲突与审计脱敏。"
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		// The category has no torrent references and may be removed as test data.
		// Its immutable audit evidence intentionally remains in the outbox.
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM catalog.categories WHERE id = $1`, categoryID)
	})

	decision := categoryIntegrationDecision(now)
	created, err := repository.CreateCategory(ctx, catalog.CreateCategoryCommand{
		ID: categoryID, Name: createdName, DisplayOrder: 420, Enabled: true,
		Reason: createReason, ActorID: uuid.New(), OccurredAt: now, Authorization: decision,
	})
	if err != nil {
		t.Fatalf("CreateCategory() error = %v", err)
	}
	if created.ID != categoryID || created.Version != 1 || !created.Enabled || created.TorrentCount != 0 {
		t.Fatalf("created category = %+v", created)
	}

	updated, err := repository.UpdateCategory(ctx, catalog.UpdateCategoryCommand{
		ID: categoryID, Name: updatedName, DisplayOrder: 421, Enabled: false,
		ExpectedVersion: 1, Reason: updateReason, ActorID: uuid.New(),
		OccurredAt: now.Add(time.Second), Authorization: categoryIntegrationDecision(now),
	})
	if err != nil {
		t.Fatalf("UpdateCategory() error = %v", err)
	}
	if updated.Version != 2 || updated.Enabled || updated.Name != updatedName || updated.DisplayOrder != 421 {
		t.Fatalf("updated category = %+v", updated)
	}

	_, err = repository.UpdateCategory(ctx, catalog.UpdateCategoryCommand{
		ID: categoryID, Name: "不应写入", DisplayOrder: 422, Enabled: true,
		ExpectedVersion: 1, Reason: "该更新使用旧版本，必须被乐观并发检查拒绝。", ActorID: uuid.New(),
		OccurredAt: now.Add(2 * time.Second), Authorization: categoryIntegrationDecision(now),
	})
	if !errors.Is(err, catalog.ErrCategoryVersionConflict) {
		t.Fatalf("stale UpdateCategory() error = %v, want version conflict", err)
	}

	var storedName string
	var storedVersion int64
	var storedEnabled bool
	if err := pool.QueryRow(ctx, `
SELECT name, version, enabled
FROM catalog.categories
WHERE id = $1`, categoryID).Scan(&storedName, &storedVersion, &storedEnabled); err != nil {
		t.Fatalf("read category: %v", err)
	}
	if storedName != updatedName || storedVersion != 2 || storedEnabled {
		t.Fatalf("stored category name=%q version=%d enabled=%t", storedName, storedVersion, storedEnabled)
	}

	var eventCount int
	var leakedEditableText bool
	if err := pool.QueryRow(ctx, `
SELECT count(*), bool_or(
    payload_json LIKE '%' || $3 || '%'
    OR payload_json LIKE '%' || $4 || '%'
    OR payload_json LIKE '%' || $5 || '%'
    OR payload_json LIKE '%' || $6 || '%'
)
FROM audit.outbox
WHERE event_type = $1
  AND payload_json::jsonb ->> 'category_id' = $2`,
		audit.CategoryChangeEventType,
		categoryID,
		createdName,
		updatedName,
		createReason,
		updateReason,
	).Scan(&eventCount, &leakedEditableText); err != nil {
		t.Fatalf("read category audit events: %v", err)
	}
	if eventCount != 2 || leakedEditableText {
		t.Fatalf("category audit event count=%d leaked editable text=%t", eventCount, leakedEditableText)
	}
}

func categoryIntegrationDecision(now time.Time) authz.Decision {
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
		GrantID: uuid.New(), GrantVersion: 1, RoleID: "category_manager", MandateID: uuid.New(),
		EffectiveUntil: now.Add(time.Hour),
	}
}
