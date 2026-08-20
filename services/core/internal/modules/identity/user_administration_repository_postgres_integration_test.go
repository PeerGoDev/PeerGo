package identity_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/audit"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

func TestPostgresUserAdministrationProjectsAndEnforcesCurrentRestriction(t *testing.T) {
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

	now := time.Now().UTC().Truncate(time.Microsecond)
	userID := uuid.New()
	credentialRef := uuid.New()
	restrictionID := uuid.New()
	username := "managed-it-" + userID.String()[:8]
	tokenHash := sha256.Sum256(bytes.Repeat([]byte{0x63}, 32))
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.users (id, credential_ref, username, display_name, status)
VALUES ($1, $2, $3, '集成测试账户', 'active')`, userID, credentialRef, username); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.sessions (token_hash, user_id, audience, created_at, last_seen_at, expires_at)
VALUES ($1, $2, 'web', $3, $3, $4)`, tokenHash[:], userID, now.Add(-time.Minute), now.Add(time.Hour)); err != nil {
		t.Fatalf("insert Web session: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.account_restrictions (
    id, user_id, kind, reason_code, reason_summary, starts_at, expires_at, version
) VALUES ($1, $2, 'account_access', 'integration_check', '集成测试：验证有效限制会进入投影并阻断账户访问。', $3, $4, 3)`,
		restrictionID, userID, now.Add(-time.Minute), now.Add(time.Hour)); err != nil {
		t.Fatalf("insert restriction: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM identity.account_restrictions WHERE user_id = $1`, userID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM identity.sessions WHERE user_id = $1`, userID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM identity.users WHERE id = $1`, userID)
	})

	repository := identity.NewPostgresRepository(pool)
	page, err := repository.ListManagedUsers(ctx, identity.ManagedUserListQuery{
		Query: username, Filter: identity.ManagedUserFilterActive,
		Page: 1, PageSize: 20, Offset: 0, AsOf: now,
	})
	if err != nil {
		t.Fatalf("ListManagedUsers() error = %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != userID || page.Items[0].NumericID <= 0 || page.Items[0].Version != 1 || page.Items[0].ActiveRestrictionCount != 1 {
		t.Fatalf("ListManagedUsers() = %+v", page)
	}

	detail, err := repository.GetManagedUser(ctx, userID, now)
	if err != nil {
		t.Fatalf("GetManagedUser() error = %v", err)
	}
	if len(detail.ActiveRestrictions) != 1 || detail.ActiveRestrictions[0].ID != restrictionID ||
		detail.ActiveRestrictions[0].Version != 3 || detail.ActiveRestrictions[0].ReasonCode != "integration_check" {
		t.Fatalf("GetManagedUser() = %+v", detail)
	}

	if _, err := repository.UserByCredentialRef(ctx, credentialRef, now); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("UserByCredentialRef() error = %v, want ErrInvalidCredentials", err)
	}
	if _, err := repository.ActiveSession(ctx, tokenHash[:], now); !errors.Is(err, identity.ErrSessionNotFound) {
		t.Fatalf("ActiveSession() error = %v, want ErrSessionNotFound", err)
	}
}

func TestPostgresAccountRestrictionCommandsCommitVersionsSessionsAndAuditTogether(t *testing.T) {
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

	eventBuilder, err := audit.NewAccountRestrictionEventBuilder(audit.RecorderConfig{
		PseudonymKey: bytes.Repeat([]byte{0x72}, 32), PseudonymKeyEpoch: "integration-2026-08",
	})
	if err != nil {
		t.Fatalf("NewAccountRestrictionEventBuilder() error = %v", err)
	}
	repository, err := identity.NewPostgresAccountRestrictionRepository(
		pool,
		eventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		t.Fatalf("NewPostgresAccountRestrictionRepository() error = %v", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	actorID := uuid.New()
	targetID := uuid.New()
	actorCredentialRef := uuid.New()
	targetCredentialRef := uuid.New()
	tokenHash := sha256.Sum256(bytes.Repeat([]byte{0x71}, 32))
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.users (id, credential_ref, username, display_name, status)
VALUES
    ($1, $2, $3, '集成测试处置员', 'active'),
    ($4, $5, $6, '集成测试目标', 'active')`,
		actorID, actorCredentialRef, "restriction-actor-"+actorID.String()[:8],
		targetID, targetCredentialRef, "restriction-target-"+targetID.String()[:8]); err != nil {
		t.Fatalf("insert restriction users: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.sessions (token_hash, user_id, audience, created_at, last_seen_at, expires_at)
VALUES ($1, $2, 'web', $3, $3, $4)`, tokenHash[:], targetID, now.Add(-time.Minute), now.Add(time.Hour)); err != nil {
		t.Fatalf("insert target session: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM identity.account_restrictions WHERE user_id = $1`, targetID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM identity.sessions WHERE user_id = $1`, targetID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM identity.users WHERE id = ANY($1::uuid[])`, []uuid.UUID{actorID, targetID})
	})

	createReason := "发现异常访问模式，临时限制账户并安排人工复核。"
	decision := accountRestrictionIntegrationDecision(now)
	created, err := repository.CreateAccountRestriction(ctx, identity.CreateAccountRestrictionCommand{
		CreateAccountRestrictionInput: identity.CreateAccountRestrictionInput{
			UserID: targetID, ReasonCode: identity.AccountRestrictionReasonSecurityIncident,
			Reason: createReason, DurationHours: 24, ExpectedUserVersion: 1,
		},
		ActorID: actorID, OccurredAt: now, Authorization: decision,
	})
	if err != nil {
		t.Fatalf("CreateAccountRestriction() error = %v", err)
	}
	if created.Version != 2 || created.ActiveRestrictionCount != 1 || len(created.ActiveRestrictions) != 1 ||
		created.ActiveRestrictions[0].Version != 1 || !created.UpdatedAt.Equal(now) {
		t.Fatalf("created detail = %+v", created)
	}
	restrictionID := created.ActiveRestrictions[0].ID

	var revokedAt pgtype.Timestamptz
	if err := pool.QueryRow(ctx, `SELECT revoked_at FROM identity.sessions WHERE token_hash = $1`, tokenHash[:]).Scan(&revokedAt); err != nil {
		t.Fatalf("read target session: %v", err)
	}
	if !revokedAt.Valid || !revokedAt.Time.Equal(now) {
		t.Fatalf("target session revoked_at = %v, want %v", revokedAt, now)
	}

	_, err = repository.CreateAccountRestriction(ctx, identity.CreateAccountRestrictionCommand{
		CreateAccountRestrictionInput: identity.CreateAccountRestrictionInput{
			UserID: targetID, ReasonCode: identity.AccountRestrictionReasonManualReview,
			Reason: "同一时间窗口不能重复创建有效账户限制。", DurationHours: 24, ExpectedUserVersion: 2,
		},
		ActorID: actorID, OccurredAt: now.Add(time.Minute), Authorization: accountRestrictionIntegrationDecision(now),
	})
	if !errors.Is(err, identity.ErrAccountRestrictionAlreadyActive) {
		t.Fatalf("overlapping CreateAccountRestriction() error = %v", err)
	}

	revokeReason := "安全人工复核已经完成，显式恢复账户访问。"
	revoked, err := repository.RevokeAccountRestriction(ctx, identity.RevokeAccountRestrictionCommand{
		RevokeAccountRestrictionInput: identity.RevokeAccountRestrictionInput{
			UserID: targetID, RestrictionID: restrictionID,
			ReasonCode: identity.AccountRestrictionRevocationReviewCompleted, Reason: revokeReason,
			ExpectedUserVersion: 2, ExpectedRestrictionVersion: 1,
		},
		ActorID: actorID, OccurredAt: now.Add(2 * time.Minute), Authorization: accountRestrictionIntegrationDecision(now),
	})
	if err != nil {
		t.Fatalf("RevokeAccountRestriction() error = %v", err)
	}
	if revoked.Version != 3 || revoked.ActiveRestrictionCount != 0 || len(revoked.ActiveRestrictions) != 0 {
		t.Fatalf("revoked detail = %+v", revoked)
	}

	var restrictionVersion int64
	var storedRevocationCode, storedRevocationReason string
	if err := pool.QueryRow(ctx, `
SELECT version, revocation_reason_code, revocation_reason
FROM identity.account_restrictions
WHERE id = $1`, restrictionID).Scan(&restrictionVersion, &storedRevocationCode, &storedRevocationReason); err != nil {
		t.Fatalf("read revoked restriction: %v", err)
	}
	if restrictionVersion != 2 || storedRevocationCode != string(identity.AccountRestrictionRevocationReviewCompleted) || storedRevocationReason != revokeReason {
		t.Fatalf("stored restriction version=%d code=%q reason=%q", restrictionVersion, storedRevocationCode, storedRevocationReason)
	}

	var eventCount int
	var leakedPrivateText bool
	if err := pool.QueryRow(ctx, `
SELECT count(*), bool_or(
    payload_json LIKE '%' || $3 || '%'
    OR payload_json LIKE '%' || $4 || '%'
    OR payload_json LIKE '%' || $5 || '%'
    OR payload_json LIKE '%' || $6 || '%'
)
FROM audit.outbox
WHERE event_type = $1
  AND payload_json::jsonb ->> 'restriction_id' = $2`,
		audit.AccountRestrictionEventType, restrictionID.String(), createReason, revokeReason, actorID.String(), targetID.String(),
	).Scan(&eventCount, &leakedPrivateText); err != nil {
		t.Fatalf("read account restriction audit events: %v", err)
	}
	if eventCount != 2 || leakedPrivateText {
		t.Fatalf("account restriction audit count=%d leaked private text=%t", eventCount, leakedPrivateText)
	}
}

func accountRestrictionIntegrationDecision(now time.Time) authz.Decision {
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
		GrantID: uuid.New(), GrantVersion: 1, RoleID: "user_access_operator", MandateID: uuid.New(),
		EffectiveUntil: now.Add(time.Hour),
	}
}
