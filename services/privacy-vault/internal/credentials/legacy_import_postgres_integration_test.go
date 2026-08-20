package credentials

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/peergo/peergo/contracts/go/trackerpasskeyv1"
	platformpostgres "github.com/peergo/peergo/services/privacy-vault/internal/platform/postgres"
)

func TestPostgresLegacyCredentialImportIsAtomicIdempotentAndForkSafe(t *testing.T) {
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
	outer, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = outer.Rollback(context.Background()) })

	identifierKey := bytes.Repeat([]byte{0x41}, 32)
	usernameLookup, _ := LookupHMAC(identifierKey, "legacy-member")
	emailLookup, _ := LookupHMAC(identifierKey, "legacy@example.test")
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("PtYes-member-password"), 10)
	if err != nil {
		t.Fatal(err)
	}
	protector, err := NewSecretProtector(
		bytes.Repeat([]byte{0x42}, 32),
		"legacy-integration-2026-08",
		bytes.NewReader(bytes.Repeat([]byte{0x43}, 64)),
	)
	if err != nil {
		t.Fatal(err)
	}
	lookupKey := bytes.Repeat([]byte{0x44}, 32)
	importer, err := NewLegacyCredentialImporter(outer, protector, lookupKey)
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)
	importedAt := time.Date(2026, time.August, 10, 15, 30, 0, 0, time.UTC)
	record := LegacyCredentialImport{
		CredentialRef:     uuid.New(),
		PasswordHash:      string(passwordHash),
		UsernameLookup:    usernameLookup,
		UsernameMasked:    "l***",
		EmailLookup:       emailLookup,
		EmailMasked:       "l***@example.test",
		EmailAddress:      "legacy@example.test",
		EmailVerifiedAt:   &createdAt,
		Passkey:           "PtYesLegacyPasskey2026ABCDEF1234",
		PasskeyProfile:    trackerpasskeyv1.ProfilePtYesAlnum32V1,
		PasswordUpdatedAt: createdAt,
		CreatedAt:         createdAt,
		ImportedAt:        importedAt,
	}
	if err := importer.Import(ctx, record); err != nil {
		t.Fatalf("first Import() error = %v", err)
	}
	if err := importer.Import(ctx, record); err != nil {
		t.Fatalf("idempotent Import() error = %v", err)
	}

	var credentialCount, identifierCount, passkeyCount int
	if err := outer.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM vault.credentials WHERE credential_ref = $1),
    (SELECT count(*) FROM vault.direct_identifiers WHERE credential_ref = $1),
    (SELECT count(*) FROM vault.tracker_passkeys WHERE credential_ref = $1)`,
		record.CredentialRef,
	).Scan(&credentialCount, &identifierCount, &passkeyCount); err != nil {
		t.Fatal(err)
	}
	if credentialCount != 1 || identifierCount != 2 || passkeyCount != 1 {
		t.Fatalf("imported row counts = credential %d identifiers %d passkeys %d", credentialCount, identifierCount, passkeyCount)
	}

	fork := record
	fork.EmailMasked = "f***@example.test"
	if err := importer.Import(ctx, fork); !errors.Is(err, ErrLegacyCredentialConflict) {
		t.Fatalf("forked Import() error = %v", err)
	}
}
