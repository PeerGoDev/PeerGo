package identity

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/generated/identitydb"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func TestRecordRegistrationInvitationRelationshipAcceptsFreshInsert(t *testing.T) {
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
	if err := platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		t.Fatalf("RequireCurrentMigration() error = %v", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin rollback-only integration transaction: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	now := time.Now().UTC().Truncate(time.Microsecond)
	inviterID, inviteeID := uuid.New(), uuid.New()
	inviterCredentialRef, inviteeCredentialRef := uuid.New(), uuid.New()
	invitationID, registrationID := uuid.New(), uuid.New()
	decisionID := uuid.New()
	tokenDigest := make([]byte, 32)
	copy(tokenDigest, invitationID[:])

	if _, err := tx.Exec(ctx, `
INSERT INTO identity.users (id, credential_ref, username, display_name, status)
VALUES
    ($1, $2, $3, '邀请人集成测试', 'active'),
    ($4, $5, $6, '受邀人集成测试', 'active')`,
		inviterID, inviterCredentialRef, "invite-rel-from-"+inviterID.String()[:8],
		inviteeID, inviteeCredentialRef, "invite-rel-to-"+inviteeID.String()[:8],
	); err != nil {
		t.Fatalf("insert integration users: %v", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.registration_invitations (
    id, token_sha256, expires_at, claimed_by, claimed_at, consumed_at,
    issuer_user_id, source_kind, issued_authorization_decision_id
) VALUES ($1, $2, $3, $4, $5, $5, $6, 'member', $7)`,
		invitationID, tokenDigest, now.Add(24*time.Hour), registrationID, now,
		inviterID, decisionID,
	); err != nil {
		t.Fatalf("insert integration invitation: %v", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.registrations (
    id, user_id, username, display_name, admission_mode, invitation_id,
    credential_ref, state, created_at, updated_at, completed_at
) VALUES ($1, $2, $3, '受邀人集成测试', 'invite', $4, $5, 'completed', $6, $6, $6)`,
		registrationID, inviteeID, "invite-rel-to-"+inviteeID.String()[:8],
		invitationID, inviteeCredentialRef, now,
	); err != nil {
		t.Fatalf("insert completed integration registration: %v", err)
	}

	queries := identitydb.New(tx)
	valid, err := queries.RecordRegistrationInvitationRelationship(ctx, registrationID)
	if err != nil || !valid {
		t.Fatalf("fresh RecordRegistrationInvitationRelationship() = %v, error=%v", valid, err)
	}
	valid, err = queries.RecordRegistrationInvitationRelationship(ctx, registrationID)
	if err != nil || !valid {
		t.Fatalf("replayed RecordRegistrationInvitationRelationship() = %v, error=%v", valid, err)
	}
	var count int
	if err := tx.QueryRow(ctx, `
SELECT count(*)
FROM identity.invitation_relationships
WHERE invitee_user_id = $1
  AND inviter_user_id = $2
  AND invitation_id = $3
  AND source_kind = 'registration'`, inviteeID, inviterID, invitationID).Scan(&count); err != nil {
		t.Fatalf("count integration invitation relationships: %v", err)
	}
	if count != 1 {
		t.Fatalf("invitation relationship count = %d, want 1", count)
	}
}
