package identity_test

import (
	"context"
	"crypto/sha256"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

func TestPostgresCreateSessionRecordsAndPrunesNetworkObservation(t *testing.T) {
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
	username := "session-network-it-" + userID.String()[:8]
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.users (id, credential_ref, username, display_name, status)
VALUES ($1, $2, $3, '登录网络集成测试', 'active')`, userID, credentialRef, username); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	staleAddress := netip.MustParseAddr("198.51.100.8")
	staleAt := now.Add(-181 * 24 * time.Hour)
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.user_network_observations (
    user_id, ip_address, first_seen_at, last_seen_at,
    legacy_seen_count, web_login_seen_count, updated_at
) VALUES ($1, $2::inet, $3, $3, 1, 0, $3)`, userID, staleAddress.String(), staleAt); err != nil {
		t.Fatalf("insert stale network observation: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM identity.user_network_observations WHERE user_id = $1`, userID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM identity.sessions WHERE user_id = $1`, userID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM identity.users WHERE id = $1`, userID)
	})

	tokenHash := sha256.Sum256([]byte("session-network-" + userID.String()))
	currentAddress := netip.MustParseAddr("203.0.113.10")
	repository := identity.NewPostgresRepository(pool)
	if err := repository.CreateSession(ctx, identity.SessionRecord{
		TokenHash: tokenHash[:], User: identity.User{ID: userID}, ClientAddress: currentAddress,
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	var sessionCount, currentObservationCount, staleObservationCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM identity.sessions WHERE user_id = $1`, userID).Scan(&sessionCount); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM identity.user_network_observations
WHERE user_id = $1 AND ip_address = $2::inet AND web_login_seen_count = 1`,
		userID, currentAddress.String()).Scan(&currentObservationCount); err != nil {
		t.Fatalf("count current network observation: %v", err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM identity.user_network_observations
WHERE user_id = $1 AND ip_address = $2::inet`,
		userID, staleAddress.String()).Scan(&staleObservationCount); err != nil {
		t.Fatalf("count stale network observation: %v", err)
	}
	if sessionCount != 1 || currentObservationCount != 1 || staleObservationCount != 0 {
		t.Fatalf("session=%d current_observation=%d stale_observation=%d", sessionCount, currentObservationCount, staleObservationCount)
	}
}
