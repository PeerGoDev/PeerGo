package identity_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

func TestPostgresAccountAccessAppealApprovesManualDownloadRestriction(t *testing.T) {
	databaseURL := os.Getenv("PEERGO_TEST_CORE_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PEERGO_TEST_CORE_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)

	// Keep immutable appeal and transition evidence isolated from the shared
	// integration database while still exercising the repository's nested tx.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("pool.Begin() error = %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	now := time.Now().UTC().Truncate(time.Microsecond)
	actorID := uuid.New()
	targetID := uuid.New()
	appealID := uuid.New()
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.users (id, credential_ref, username, display_name, status)
VALUES
    ($1, $2, $3, '集成测试处置员', 'active'),
    ($4, $5, $6, '集成测试申诉用户', 'active')`,
		actorID, uuid.New(), "appeal-actor-"+actorID.String()[:8],
		targetID, uuid.New(), "appeal-target-"+targetID.String()[:8]); err != nil {
		t.Fatalf("insert appeal users: %v", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.user_access_states (
    user_id, download_restricted, vip_enabled,
    download_restriction_origin, download_restriction_reason_code,
    download_restriction_reason, download_restriction_started_at,
    version, updated_at
) VALUES ($1, true, false, 'legacy_migration', 'legacy_download_restriction',
          '该下载限制从旧站当前账户状态迁入，需要由用户管理员单独复核。', $2, 1, $3)`,
		targetID, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatalf("insert manual download restriction: %v", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.account_access_appeals (
    id, user_id, source_kind, source_restriction_id, source_version,
    source_reason_code, source_reason_summary, source_starts_at,
    source_expires_at, statement, credential_verified_at, created_at
) VALUES ($1, $2, 'manual_download_restriction', NULL, 1,
          'legacy_download_restriction', '旧站迁入的人工下载限制', $3,
          NULL, '新版本未按照站点规则正确限制下载速度，请管理员复核。', NULL, $4)`,
		appealID, targetID, now.Add(-time.Hour), now.Add(-time.Minute)); err != nil {
		t.Fatalf("insert manual download appeal: %v", err)
	}

	response := "复核通过，解除旧站迁入的人工下载限制。"
	repository := identity.NewPostgresRepository(tx)
	result, err := repository.DecideAccountAccessAppeal(ctx, identity.DecideAccountAccessAppealCommand{
		DecideAccountAccessAppealInput: identity.DecideAccountAccessAppealInput{
			AppealID: appealID, Decision: identity.AccountAccessAppealDecisionApprove,
			Response: response, ExpectedSourceVersion: 1,
		},
		ActorID: actorID, DecidedAt: now,
		Authorization: authz.Decision{ID: uuid.New(), Allow: true},
	})
	if err != nil {
		t.Fatalf("DecideAccountAccessAppeal() error = %v", err)
	}
	if result.Status != identity.AccountAccessAppealApproved || result.Response != response ||
		result.SourceActive || result.ResolvedAt == nil || !result.ResolvedAt.Equal(now) {
		t.Fatalf("DecideAccountAccessAppeal() = %+v", result)
	}

	var restricted, metadataCleared bool
	var stateVersion int64
	if err := tx.QueryRow(ctx, `
SELECT download_restricted, version,
       download_restriction_origin IS NULL
       AND download_restriction_reason_code IS NULL
       AND download_restriction_reason IS NULL
       AND download_restriction_started_at IS NULL
       AND download_restriction_created_by IS NULL
FROM identity.user_access_states
WHERE user_id = $1`, targetID).Scan(&restricted, &stateVersion, &metadataCleared); err != nil {
		t.Fatalf("read decided access state: %v", err)
	}
	if restricted || stateVersion != 2 || !metadataCleared {
		t.Fatalf("decided access state restricted=%t version=%d metadata_cleared=%t", restricted, stateVersion, metadataCleared)
	}

	var transition, origin, reasonCode, storedReason string
	var transitionActorID, transitionAppealID uuid.UUID
	var fromRestricted, toRestricted bool
	var fromStateVersion, transitionStateVersion int64
	if err := tx.QueryRow(ctx, `
SELECT transition, origin, reason_code, reason, actor_id, appeal_id,
       from_restricted, to_restricted, from_state_version, state_version
FROM identity.manual_download_restriction_transitions
WHERE appeal_id = $1`, appealID).Scan(
		&transition, &origin, &reasonCode, &storedReason,
		&transitionActorID, &transitionAppealID, &fromRestricted, &toRestricted,
		&fromStateVersion, &transitionStateVersion,
	); err != nil {
		t.Fatalf("read appeal restriction transition: %v", err)
	}
	if transition != "revoked" || origin != "appeal" || reasonCode != "appeal_approved" ||
		storedReason != response || transitionActorID != actorID || transitionAppealID != appealID ||
		!fromRestricted || toRestricted || fromStateVersion != 1 || transitionStateVersion != 2 {
		t.Fatalf("unexpected appeal restriction transition: transition=%q origin=%q code=%q reason=%q actor=%s appeal=%s from=%t to=%t versions=%d->%d",
			transition, origin, reasonCode, storedReason, transitionActorID, transitionAppealID,
			fromRestricted, toRestricted, fromStateVersion, transitionStateVersion)
	}
}
