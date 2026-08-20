// Command devseed creates only synthetic local credentials. It refuses to run
// outside development and requires the demo password explicitly in the shell.
package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/privacy-vault/internal/credentials"
	"github.com/peergo/peergo/services/privacy-vault/internal/generated/vaultdb"
	platformpostgres "github.com/peergo/peergo/services/privacy-vault/internal/platform/postgres"
)

var demoCredentialRef = uuid.MustParse("0198f20a-6da8-7e51-9c64-222222222222")

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if os.Getenv("PEERGO_ENV") != "development" {
		logger.Error("dev seed requires PEERGO_ENV=development")
		os.Exit(1)
	}
	databaseURL := strings.TrimSpace(os.Getenv("PEERGO_VAULT_DATABASE_URL"))
	identifierKey := []byte(os.Getenv("PEERGO_VAULT_IDENTIFIER_KEY"))
	password := os.Getenv("PEERGO_DEV_PASSWORD")
	if databaseURL == "" || len(identifierKey) < 32 || password == "" {
		logger.Error("PEERGO_VAULT_DATABASE_URL, PEERGO_VAULT_IDENTIFIER_KEY and PEERGO_DEV_PASSWORD are required")
		os.Exit(1)
	}

	username := valueOrDefault("PEERGO_DEV_USERNAME", "demo")
	email := valueOrDefault("PEERGO_DEV_EMAIL", "demo@peergo.local")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		logger.Error("open vault database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		logger.Error("vault database is not ready", "error", err)
		os.Exit(1)
	}

	passwordHash, err := credentials.HashPassword(password)
	if err != nil {
		logger.Error("hash demo password", "error", err)
		os.Exit(1)
	}
	usernameLookup, err := credentials.LookupHMAC(identifierKey, username)
	if err != nil {
		logger.Error("index demo username", "error", err)
		os.Exit(1)
	}
	emailLookup, err := credentials.LookupHMAC(identifierKey, email)
	if err != nil {
		logger.Error("index demo email", "error", err)
		os.Exit(1)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		logger.Error("begin vault seed transaction", "error", err)
		os.Exit(1)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := vaultdb.New(tx)
	now := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	if err := queries.UpsertDevelopmentCredential(ctx, vaultdb.UpsertDevelopmentCredentialParams{
		CredentialRef:     demoCredentialRef,
		PasswordHash:      passwordHash,
		PasswordUpdatedAt: now,
	}); err != nil {
		logger.Error("seed demo credential", "error", err)
		os.Exit(1)
	}
	for _, identifier := range []vaultdb.UpsertDevelopmentIdentifierParams{
		{
			CredentialRef: demoCredentialRef,
			Kind:          "username",
			LookupHmac:    usernameLookup,
			MaskedValue:   credentials.MaskUsername(username),
			VerifiedAt:    now,
		},
		{
			CredentialRef: demoCredentialRef,
			Kind:          "email",
			LookupHmac:    emailLookup,
			MaskedValue:   credentials.MaskEmail(email),
			VerifiedAt:    now,
		},
	} {
		if err := queries.UpsertDevelopmentIdentifier(ctx, identifier); err != nil {
			logger.Error("seed demo identifier", "kind", identifier.Kind, "error", err)
			os.Exit(1)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		logger.Error("commit vault seed transaction", "error", err)
		os.Exit(1)
	}
	logger.Info("synthetic vault credential seeded", "username", username, "email", credentials.MaskEmail(email))
}

func valueOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
