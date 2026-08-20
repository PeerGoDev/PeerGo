package credentials

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/contracts/go/trackerpasskeyv1"
	platformpostgres "github.com/peergo/peergo/services/privacy-vault/internal/platform/postgres"
)

func TestPostgresTrackerCredentialPersistsOneEncryptedStablePasskey(t *testing.T) {
	databaseURL := os.Getenv("PEERGO_TEST_VAULT_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PEERGO_TEST_VAULT_DATABASE_URL is not set")
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
	credentialRef := uuid.New()
	passwordHash, err := HashPassword("integration-only-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO vault.credentials (
    credential_ref, password_hash, password_updated_at, created_at, updated_at
) VALUES ($1, $2, $3, $3, $3)`, credentialRef, passwordHash, now); err != nil {
		t.Fatalf("insert integration credential: %v", err)
	}

	repository := NewPostgresRepository(tx)
	protector, err := NewSecretProtector(bytes.Repeat([]byte{0x71}, 32), "integration-2026-08", nil)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewTrackerCredentialService(
		repository,
		protector,
		bytes.Repeat([]byte{0x72}, 32),
		TrackerCredentialServiceConfig{Now: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatal(err)
	}

	first, err := service.GetOrCreate(ctx, credentialRef)
	if err != nil {
		t.Fatalf("first GetOrCreate() error = %v", err)
	}
	second, err := service.GetOrCreate(ctx, credentialRef)
	if err != nil {
		t.Fatalf("retry GetOrCreate() error = %v", err)
	}
	if first != second || !validTrackerPasskey(first.Passkey) || first.Version != 1 {
		t.Fatalf("Tracker credential retry changed stable identity")
	}

	var ciphertext, nonce, lookupHMAC []byte
	var keyEpoch string
	var version int64
	var formatProfile string
	if err := tx.QueryRow(ctx, `
SELECT ciphertext, nonce, encryption_key_epoch, lookup_hmac, format_profile, version
FROM vault.tracker_passkeys
WHERE credential_ref = $1`, credentialRef).Scan(
		&ciphertext, &nonce, &keyEpoch, &lookupHMAC, &formatProfile, &version,
	); err != nil {
		t.Fatalf("read encrypted Tracker credential: %v", err)
	}
	if bytes.Contains(ciphertext, []byte(first.Passkey)) || len(ciphertext) != 48 || len(nonce) != 12 ||
		keyEpoch != "integration-2026-08" || len(lookupHMAC) != 32 ||
		formatProfile != trackerpasskeyv1.ProfileCanonicalHexV1 || version != 1 {
		t.Fatalf("invalid encrypted envelope: ciphertext=%d nonce=%d hmac=%d epoch=%q version=%d", len(ciphertext), len(nonce), len(lookupHMAC), keyEpoch, version)
	}
}
