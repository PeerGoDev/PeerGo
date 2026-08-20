package credentials

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	platformpostgres "github.com/peergo/peergo/services/privacy-vault/internal/platform/postgres"
)

func TestPostgresLegacyPasswordRehashIsAtomicAndPreservesPasswordChangeTime(t *testing.T) {
	databaseURL := os.Getenv("PEERGO_TEST_VAULT_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PEERGO_TEST_VAULT_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	legacyHash, err := bcrypt.GenerateFromPassword([]byte("PtYes-member-password"), 10)
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := HashPassword("PtYes-member-password")
	if err != nil {
		t.Fatal(err)
	}
	credentialRef := uuid.New()
	createdAt := time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)
	rehashedAt := time.Date(2026, time.August, 10, 14, 30, 0, 0, time.UTC)
	if _, err := tx.Exec(ctx, `
INSERT INTO vault.credentials (
    credential_ref,
    password_hash,
    password_algorithm,
    password_updated_at,
    created_at,
    updated_at
) VALUES ($1, $2, 'bcrypt_ptyes_cost10', $3, $3, $3)`,
		credentialRef, string(legacyHash), createdAt); err != nil {
		t.Fatalf("insert legacy credential: %v", err)
	}

	repository := NewPostgresRepository(tx)
	updated, err := repository.RehashPasswordIfCurrent(
		ctx,
		credentialRef,
		string(legacyHash),
		replacement,
		rehashedAt,
	)
	if err != nil || !updated {
		t.Fatalf("first RehashPasswordIfCurrent() = %v, %v", updated, err)
	}
	updated, err = repository.RehashPasswordIfCurrent(
		ctx,
		credentialRef,
		string(legacyHash),
		replacement,
		rehashedAt.Add(time.Second),
	)
	if err != nil || updated {
		t.Fatalf("stale RehashPasswordIfCurrent() = %v, %v", updated, err)
	}

	var storedHash, algorithm string
	var passwordUpdatedAt, storedRehashedAt, updatedAt time.Time
	if err := tx.QueryRow(ctx, `
SELECT
    password_hash,
    password_algorithm,
    password_updated_at,
    password_rehashed_at,
    updated_at
FROM vault.credentials
WHERE credential_ref = $1`, credentialRef).Scan(
		&storedHash,
		&algorithm,
		&passwordUpdatedAt,
		&storedRehashedAt,
		&updatedAt,
	); err != nil {
		t.Fatal(err)
	}
	if storedHash != replacement || algorithm != "argon2id" ||
		!passwordUpdatedAt.Equal(createdAt) || !storedRehashedAt.Equal(rehashedAt) ||
		!updatedAt.Equal(rehashedAt) {
		t.Fatalf(
			"stored rehash state algorithm=%q password_updated=%s rehashed=%s updated=%s",
			algorithm,
			passwordUpdatedAt,
			storedRehashedAt,
			updatedAt,
		)
	}
}
