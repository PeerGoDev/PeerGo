package identity

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

func TestPostgresTrackerCredentialProjectionIsIdempotentAndRejectsForks(t *testing.T) {
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
	userID, credentialRef := uuid.New(), uuid.New()
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.users (
    id, credential_ref, username, display_name, status, created_at, updated_at
) VALUES ($1, $2, $3, 'Tracker 投影集成测试', 'active', $4, $4)`,
		userID, credentialRef, "tracker-it-"+userID.String()[:8], now,
	); err != nil {
		t.Fatalf("insert integration user: %v", err)
	}

	repository := NewPostgresRepository(tx)
	credentialV1 := TrackerCredential{
		Passkey:   "0123456789abcdef0123456789abcdef",
		Version:   1,
		CreatedAt: now.Add(-time.Hour),
	}
	credentialV1.LookupHMAC[0] = 0x71
	first, err := repository.BindTrackerCredential(ctx, userID, credentialRef, credentialV1, now)
	if err != nil {
		t.Fatalf("first BindTrackerCredential() error = %v", err)
	}
	replayed, err := repository.BindTrackerCredential(ctx, userID, credentialRef, credentialV1, now.Add(time.Second))
	if err != nil || replayed.UserID != first.UserID || replayed.LookupHMAC != first.LookupHMAC || replayed.VaultVersion != 1 {
		t.Fatalf("idempotent projection replay = %+v, error=%v", replayed, err)
	}

	fork := credentialV1
	fork.LookupHMAC[0] = 0x72
	if _, err := repository.BindTrackerCredential(ctx, userID, credentialRef, fork, now.Add(2*time.Second)); !errors.Is(err, ErrTrackerCredentialStateConflict) {
		t.Fatalf("same-version fork error = %v", err)
	}
	credentialV2 := fork
	credentialV2.Version = 2
	advanced, err := repository.BindTrackerCredential(ctx, userID, credentialRef, credentialV2, now.Add(3*time.Second))
	if err != nil || advanced.VaultVersion != 2 || advanced.LookupHMAC != credentialV2.LookupHMAC || advanced.CreatedAt != first.CreatedAt {
		t.Fatalf("advanced projection = %+v, error=%v", advanced, err)
	}
	if _, err := repository.BindTrackerCredential(ctx, userID, credentialRef, credentialV1, now.Add(4*time.Second)); !errors.Is(err, ErrTrackerCredentialStateConflict) {
		t.Fatalf("version rollback error = %v", err)
	}
}
