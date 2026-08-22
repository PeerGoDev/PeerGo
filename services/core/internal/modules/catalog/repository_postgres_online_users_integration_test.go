package catalog_test

import (
	"context"
	"crypto/sha256"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/catalog"
)

func TestPostgresSiteInfoCountsDistinctRecentlyActiveWebUsers(t *testing.T) {
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

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatalf("begin repeatable-read transaction: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	repository := catalog.NewPostgresRepository(tx)
	asOf := time.Now().UTC().Truncate(time.Microsecond)
	before, err := repository.SiteInfo(ctx, asOf)
	if err != nil {
		t.Fatalf("read baseline site info: %v", err)
	}

	type sessionFixture struct {
		status     string
		lastSeenAt time.Time
		expiresAt  time.Time
		revokedAt  *time.Time
		copies     int
	}
	revokedAt := asOf.Add(-time.Minute)
	fixtures := []sessionFixture{
		{status: "active", lastSeenAt: asOf.Add(-time.Minute), expiresAt: asOf.Add(time.Hour), copies: 2},
		{status: "active", lastSeenAt: asOf.Add(-16 * time.Minute), expiresAt: asOf.Add(time.Hour), copies: 1},
		{status: "active", lastSeenAt: asOf.Add(-time.Minute), expiresAt: asOf.Add(time.Hour), revokedAt: &revokedAt, copies: 1},
		{status: "active", lastSeenAt: asOf.Add(-2 * time.Minute), expiresAt: asOf.Add(-time.Minute), copies: 1},
		{status: "disabled", lastSeenAt: asOf.Add(-time.Minute), expiresAt: asOf.Add(time.Hour), copies: 1},
	}
	for fixtureIndex, fixture := range fixtures {
		userID := uuid.New()
		username := "presence-it-" + uuid.NewString()[:8]
		if _, err := tx.Exec(ctx, `
INSERT INTO identity.users (id, credential_ref, username, display_name, status)
VALUES ($1, $2, $3, $3, $4)`, userID, uuid.New(), username, fixture.status); err != nil {
			t.Fatalf("insert presence user %d: %v", fixtureIndex, err)
		}
		for copyIndex := 0; copyIndex < fixture.copies; copyIndex++ {
			tokenHash := sha256.Sum256([]byte(uuid.NewString()))
			if _, err := tx.Exec(ctx, `
INSERT INTO identity.sessions (
    token_hash, user_id, audience, created_at, last_seen_at, expires_at, revoked_at
) VALUES ($1, $2, 'web', $3, $4, $5, $6)`,
				tokenHash[:], userID, asOf.Add(-2*time.Hour), fixture.lastSeenAt,
				fixture.expiresAt, fixture.revokedAt,
			); err != nil {
				t.Fatalf("insert presence session %d/%d: %v", fixtureIndex, copyIndex, err)
			}
		}
	}

	after, err := repository.SiteInfo(ctx, asOf)
	if err != nil {
		t.Fatalf("read site info with presence fixtures: %v", err)
	}
	if after.OnlineUsers != before.OnlineUsers+1 {
		t.Fatalf("online users before=%d after=%d, want one distinct recent active user", before.OnlineUsers, after.OnlineUsers)
	}
}
