package identity_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/audit"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

func TestPostgresSessionSecurityListsAndRevokesOthersWithAudit(t *testing.T) {
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

	now := time.Now().UTC().Truncate(time.Microsecond)
	userID := uuid.New()
	credentialRef := uuid.New()
	username := "session-it-" + userID.String()[:8]
	currentHash := sha256.Sum256([]byte("current-" + userID.String()))
	otherHash := sha256.Sum256([]byte("other-" + userID.String()))
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.users (id, credential_ref, username, display_name, status)
VALUES ($1, $2, $3, '会话集成测试', 'active')`, userID, credentialRef, username); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.sessions (token_hash, user_id, audience, created_at, last_seen_at, expires_at)
VALUES
    ($1, $3, 'web', $4, $4, $5),
    ($2, $3, 'web', $4, $4, $5)`, currentHash[:], otherHash[:], userID, now.Add(-time.Hour), now.Add(time.Hour)); err != nil {
		t.Fatalf("insert Web sessions: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM identity.sessions WHERE user_id = $1`, userID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM identity.users WHERE id = $1`, userID)
	})

	eventBuilder, err := audit.NewSessionRevocationEventBuilder(audit.RecorderConfig{
		PseudonymKey: bytes.Repeat([]byte{0x74}, 32), PseudonymKeyEpoch: "integration-2026-08",
	})
	if err != nil {
		t.Fatalf("NewSessionRevocationEventBuilder() error = %v", err)
	}
	repository, err := identity.NewPostgresSessionSecurityRepository(
		pool,
		eventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		t.Fatalf("NewPostgresSessionSecurityRepository() error = %v", err)
	}

	items, err := repository.ListActiveSessions(ctx, userID, currentHash[:], now)
	if err != nil {
		t.Fatalf("ListActiveSessions() error = %v", err)
	}
	if len(items) != 2 || !items[0].Current || items[0].ID == uuid.Nil || items[1].Current || items[1].ID == uuid.Nil {
		t.Fatalf("ListActiveSessions() = %+v", items)
	}

	revocationID := uuid.New()
	decision := authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
		GrantID: uuid.New(), GrantVersion: 1, RoleID: "member", MandateID: uuid.New(),
		EffectiveUntil: now.Add(time.Hour),
	}
	result, err := repository.ApplySessionRevocation(ctx, identity.SessionRevocationCommand{
		ID: revocationID, UserID: userID, CurrentTokenHash: currentHash[:],
		Scope: identity.SessionRevocationOthers, OccurredAt: now, Authorization: decision,
	})
	if err != nil {
		t.Fatalf("ApplySessionRevocation() error = %v", err)
	}
	if result.RevokedWebSessions != 1 || result.RevokedStaffSessions != 0 || result.CurrentSessionRevoked {
		t.Fatalf("ApplySessionRevocation() = %+v", result)
	}

	items, err = repository.ListActiveSessions(ctx, userID, currentHash[:], now)
	if err != nil || len(items) != 1 || !items[0].Current {
		t.Fatalf("active sessions after revocation = %+v, error=%v", items, err)
	}
	var payload string
	if err := pool.QueryRow(ctx, `
SELECT payload_json
FROM audit.outbox
WHERE event_type = $1
  AND payload_json::jsonb ->> 'revocation_id' = $2`, audit.SessionRevocationEventType, revocationID.String()).Scan(&payload); err != nil {
		t.Fatalf("read session revocation audit event: %v", err)
	}
	for _, forbidden := range []string{userID.String(), string(currentHash[:]), string(otherHash[:]), "token_hash"} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("audit payload leaked restricted session data %q: %s", forbidden, payload)
		}
	}
}
