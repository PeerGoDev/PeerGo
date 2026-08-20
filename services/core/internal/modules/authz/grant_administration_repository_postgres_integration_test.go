package authz_test

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
)

func TestPostgresGrantRevocationRequiresTwoDutyDomainsAndBumpsVersion(t *testing.T) {
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
	proposerID := uuid.New()
	targetID := uuid.New()
	governanceID := uuid.New()
	securityID := uuid.New()
	userIDs := []uuid.UUID{proposerID, targetID, governanceID, securityID}
	mandateIDs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	grantIDs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	roles := []string{"grant_proposer", "member", "grant_governance_reviewer", "grant_security_reviewer"}
	for index, userID := range userIDs {
		username := "grant-it-" + userID.String()[:8]
		if _, err := pool.Exec(ctx, `
INSERT INTO identity.users (id, credential_ref, username, display_name, status)
VALUES ($1, $2, $3, $4, 'active')`, userID, uuid.New(), username, username); err != nil {
			t.Fatalf("insert user %d: %v", index, err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO governance.mandates (
    id, subject_id, source_type, source_reference, scope_type, scope_id,
    starts_at, ends_at, status
) VALUES ($1, $2, 'bootstrap', $3, 'site', 'peergo', $4, $5, 'active')`,
			mandateIDs[index], userID, "grant-integration-test", now.Add(-time.Hour), now.Add(time.Hour)); err != nil {
			t.Fatalf("insert mandate %d: %v", index, err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO authz.grants (
    id, subject_id, role_id, mandate_id, scope_type, scope_id,
    valid_from, valid_until, constraints, version
) VALUES ($1, $2, $3, $4, 'site', 'peergo', $5, $6, '{}'::jsonb, 1)`,
			grantIDs[index], userID, roles[index], mandateIDs[index], now.Add(-time.Hour), now.Add(time.Hour)); err != nil {
			t.Fatalf("insert grant %d: %v", index, err)
		}
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM authz.grant_revocation_reviews WHERE request_id IN (SELECT id FROM authz.grant_revocation_requests WHERE grant_id = ANY($1::uuid[]))`, grantIDs)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM authz.grant_revocation_requests WHERE grant_id = ANY($1::uuid[])`, grantIDs)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM authz.grants WHERE id = ANY($1::uuid[])`, grantIDs)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM governance.mandates WHERE id = ANY($1::uuid[])`, mandateIDs)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM identity.users WHERE id = ANY($1::uuid[])`, userIDs)
	})

	eventBuilder, err := audit.NewGrantRevocationEventBuilder(audit.RecorderConfig{
		PseudonymKey:      bytes.Repeat([]byte{0x33}, 32),
		PseudonymKeyEpoch: "integration-2026-08",
	})
	if err != nil {
		t.Fatalf("NewGrantRevocationEventBuilder() error = %v", err)
	}
	repository, err := authz.NewPostgresGrantAdministrationRepository(
		pool,
		eventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		t.Fatalf("NewPostgresGrantAdministrationRepository() error = %v", err)
	}

	requestID := uuid.New()
	proposalDecision := integrationDecision(now, grantIDs[0], mandateIDs[0], roles[0])
	request, err := repository.CreateRevocation(ctx, authz.CreateGrantRevocationCommand{
		ID:                   requestID,
		GrantID:              grantIDs[1],
		ExpectedGrantVersion: 1,
		ProposerID:           proposerID,
		Reason:               "目标任期已经结束，按测试治理流程撤销授权。",
		CreatedAt:            now,
		ExpiresAt:            now.Add(time.Hour),
		Authorization:        proposalDecision,
	})
	if err != nil {
		t.Fatalf("CreateRevocation() error = %v", err)
	}
	if request.Status != authz.GrantRevocationPendingStatus || len(request.Reviews) != 0 {
		t.Fatalf("created request = %+v", request)
	}
	_, err = repository.ReviewRevocation(ctx, authz.ReviewGrantRevocationCommand{
		ReviewID:      uuid.New(),
		RequestID:     requestID,
		ReviewerID:    proposerID,
		Domain:        authz.GrantReviewGovernance,
		Decision:      authz.GrantReviewApprove,
		Reason:        "申请人不能复核自己的撤权申请，此操作必须失败。",
		CreatedAt:     now.Add(time.Minute),
		Authorization: proposalDecision,
	})
	if !errors.Is(err, authz.ErrSeparationOfDuties) {
		t.Fatalf("self review error = %v", err)
	}

	governanceDecision := integrationDecision(now, grantIDs[2], mandateIDs[2], roles[2])
	request, err = repository.ReviewRevocation(ctx, authz.ReviewGrantRevocationCommand{
		ReviewID:      uuid.New(),
		RequestID:     requestID,
		ReviewerID:    governanceID,
		Domain:        authz.GrantReviewGovernance,
		Decision:      authz.GrantReviewApprove,
		Reason:        "治理职责核对任期和申请理由后同意撤销授权。",
		CreatedAt:     now.Add(2 * time.Minute),
		Authorization: governanceDecision,
	})
	if err != nil || request.Status != authz.GrantRevocationPendingStatus || len(request.Reviews) != 1 {
		t.Fatalf("governance review request=%+v error=%v", request, err)
	}

	securityDecision := integrationDecision(now, grantIDs[3], mandateIDs[3], roles[3])
	request, err = repository.ReviewRevocation(ctx, authz.ReviewGrantRevocationCommand{
		ReviewID:      uuid.New(),
		RequestID:     requestID,
		ReviewerID:    securityID,
		Domain:        authz.GrantReviewSecurity,
		Decision:      authz.GrantReviewApprove,
		Reason:        "安全职责确认没有会话风险并同意完成最终撤权。",
		CreatedAt:     now.Add(3 * time.Minute),
		Authorization: securityDecision,
	})
	if err != nil || request.Status != authz.GrantRevocationAppliedStatus || request.ResultingGrantVersion != 2 || len(request.Reviews) != 2 {
		t.Fatalf("security review request=%+v error=%v", request, err)
	}

	var version int64
	var revokedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT version, revoked_at FROM authz.grants WHERE id = $1`, grantIDs[1]).Scan(&version, &revokedAt); err != nil {
		t.Fatalf("read revoked grant: %v", err)
	}
	if version != 2 || revokedAt.IsZero() {
		t.Fatalf("grant version=%d revoked_at=%s", version, revokedAt)
	}
	var eventCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM audit.outbox
WHERE event_type = $1
  AND payload_json::jsonb ->> 'request_id' = $2`, audit.GrantRevocationEventType, requestID.String()).Scan(&eventCount); err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	if eventCount != 3 {
		t.Fatalf("grant revocation audit event count = %d, want 3", eventCount)
	}
}

func integrationDecision(now time.Time, grantID, mandateID uuid.UUID, roleID string) authz.Decision {
	return authz.Decision{
		ID:             uuid.New(),
		Allow:          true,
		Reason:         authz.ReasonAllowed,
		PolicyVersion:  authz.PolicyVersion,
		GrantID:        grantID,
		GrantVersion:   1,
		RoleID:         roleID,
		MandateID:      mandateID,
		EffectiveUntil: now.Add(time.Hour),
	}
}
