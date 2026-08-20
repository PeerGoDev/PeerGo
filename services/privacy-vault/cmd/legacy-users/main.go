package main

import (
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/privacy-vault/internal/credentials"
	"github.com/peergo/peergo/services/privacy-vault/internal/legacyusers"
)

type settings struct {
	SourceDatabaseURL         string
	CoreDatabaseURL           string
	VaultDatabaseURL          string
	Mode                      string
	RunID                     uuid.UUID
	SnapshotSHA256            [32]byte
	MappingVersion            string
	OccurredAt                time.Time
	FingerprintKey            []byte
	IdentifierKey             []byte
	PasskeyEncryptionKey      []byte
	PasskeyEncryptionKeyEpoch string
	PasskeyLookupKey          []byte
	ProgressEvery             int
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	config, err := loadSettings()
	if err != nil {
		logger.Error("invalid legacy user migration configuration", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	source, err := openPool(ctx, config.SourceDatabaseURL, true, "peergo-legacy-users-source")
	if err != nil {
		logger.Error("open read-only PtYes source", "error", err)
		os.Exit(1)
	}
	defer source.Close()
	core, err := openPool(ctx, config.CoreDatabaseURL, false, "peergo-legacy-users-core")
	if err != nil {
		logger.Error("open PeerGo Core database", "error", err)
		os.Exit(1)
	}
	defer core.Close()
	vault, err := openPool(ctx, config.VaultDatabaseURL, false, "peergo-legacy-users-vault")
	if err != nil {
		logger.Error("open PeerGo Vault database", "error", err)
		os.Exit(1)
	}
	defer vault.Close()

	protector, err := credentials.NewSecretProtector(
		config.PasskeyEncryptionKey,
		config.PasskeyEncryptionKeyEpoch,
		nil,
	)
	if err != nil {
		logger.Error("compose legacy Tracker passkey protector", "error", err)
		os.Exit(1)
	}
	credentialImporter, err := credentials.NewLegacyCredentialImporter(
		vault,
		protector,
		config.PasskeyLookupKey,
	)
	if err != nil {
		logger.Error("compose legacy Vault credential importer", "error", err)
		os.Exit(1)
	}
	importer, err := legacyusers.NewImporter(
		source,
		core,
		vault,
		credentialImporter,
		legacyusers.Config{
			Mode:             config.Mode,
			RunID:            config.RunID,
			SnapshotSHA256:   config.SnapshotSHA256,
			MappingVersion:   config.MappingVersion,
			FingerprintKey:   config.FingerprintKey,
			IdentifierKey:    config.IdentifierKey,
			PasskeyLookupKey: config.PasskeyLookupKey,
			OccurredAt:       config.OccurredAt,
			ProgressEvery:    config.ProgressEvery,
		},
		func(progress legacyusers.Progress) {
			// Numeric aggregate progress is intentionally the only per-run output.
			// Never add source identifiers, hashes, passkeys, email or row dumps.
			logger.Info(
				"legacy user migration progress",
				"phase", progress.Phase,
				"processed", progress.Processed,
				"imported", progress.Imported,
				"skipped", progress.Skipped,
			)
		},
	)
	if err != nil {
		logger.Error("compose legacy user importer", "error", err)
		os.Exit(1)
	}
	result, err := importer.Run(ctx)
	if err != nil {
		logger.Error("legacy user migration failed", "error", err)
		os.Exit(1)
	}
	logger.Info(
		"legacy user migration completed",
		"mode", config.Mode,
		"run_id", result.RunID,
		"expected_users", result.ExpectedUsers,
		"expected_torrents", result.ExpectedTorrents,
		"validated_users", result.ValidatedUsers,
		"imported_users", result.ImportedUsers,
		"skipped_users", result.SkippedUsers,
	)
}

func loadSettings() (settings, error) {
	var result settings
	var err error
	result.SourceDatabaseURL, err = required("PEERGO_LEGACY_SOURCE_DATABASE_URL")
	if err != nil {
		return settings{}, err
	}
	result.CoreDatabaseURL, err = required("PEERGO_CORE_DATABASE_URL")
	if err != nil {
		return settings{}, err
	}
	result.VaultDatabaseURL, err = required("PEERGO_VAULT_DATABASE_URL")
	if err != nil {
		return settings{}, err
	}
	if result.SourceDatabaseURL == result.CoreDatabaseURL ||
		result.SourceDatabaseURL == result.VaultDatabaseURL ||
		result.CoreDatabaseURL == result.VaultDatabaseURL {
		return settings{}, errors.New("source, Core and Vault database URLs must be distinct")
	}
	result.Mode = strings.TrimSpace(os.Getenv("PEERGO_LEGACY_MODE"))
	if result.Mode == "" {
		result.Mode = legacyusers.ModeValidate
	}
	result.RunID, err = uuid.Parse(strings.TrimSpace(os.Getenv("PEERGO_LEGACY_RUN_ID")))
	if err != nil || result.RunID == uuid.Nil {
		return settings{}, errors.New("PEERGO_LEGACY_RUN_ID must be a non-zero UUID")
	}
	result.SnapshotSHA256, err = decodeSHA256(os.Getenv("PEERGO_LEGACY_SNAPSHOT_SHA256"))
	if err != nil {
		return settings{}, err
	}
	result.MappingVersion = strings.TrimSpace(os.Getenv("PEERGO_LEGACY_MAPPING_VERSION"))
	if result.MappingVersion == "" {
		result.MappingVersion = "ptyes-v1"
	}
	result.OccurredAt, err = time.Parse(time.RFC3339Nano, strings.TrimSpace(os.Getenv("PEERGO_LEGACY_OCCURRED_AT")))
	if err != nil || result.OccurredAt.IsZero() {
		return settings{}, errors.New("PEERGO_LEGACY_OCCURRED_AT must be a fixed RFC3339 timestamp")
	}
	result.FingerprintKey, err = requiredBytes("PEERGO_LEGACY_FINGERPRINT_KEY", 32, -1)
	if err != nil {
		return settings{}, err
	}
	result.IdentifierKey, err = requiredBytes("PEERGO_VAULT_IDENTIFIER_KEY", 32, -1)
	if err != nil {
		return settings{}, err
	}
	result.PasskeyEncryptionKey, err = requiredBytes("PEERGO_VAULT_TRACKER_PASSKEY_ENCRYPTION_KEY", 32, 32)
	if err != nil {
		return settings{}, err
	}
	result.PasskeyEncryptionKeyEpoch, err = required("PEERGO_VAULT_TRACKER_PASSKEY_KEY_EPOCH")
	if err != nil {
		return settings{}, err
	}
	result.PasskeyLookupKey, err = requiredBytes("PEERGO_TRACKER_PASSKEY_LOOKUP_KEY", 32, -1)
	if err != nil {
		return settings{}, err
	}
	result.ProgressEvery = 250
	if raw := strings.TrimSpace(os.Getenv("PEERGO_LEGACY_PROGRESS_EVERY")); raw != "" {
		value, parseErr := strconv.Atoi(raw)
		if parseErr != nil || value < 1 || value > 10000 {
			return settings{}, errors.New("PEERGO_LEGACY_PROGRESS_EVERY must be between 1 and 10000")
		}
		result.ProgressEvery = value
	}
	return result, nil
}

func openPool(ctx context.Context, databaseURL string, readOnly bool, applicationName string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("database URL is invalid")
	}
	config.MaxConns = 4
	config.MinConns = 0
	config.ConnConfig.RuntimeParams["application_name"] = applicationName
	if readOnly {
		config.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func required(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", errors.New(name + " is required")
	}
	return value, nil
}

func requiredBytes(name string, minimum, exact int) ([]byte, error) {
	value, err := required(name)
	if err != nil {
		return nil, err
	}
	encoded := []byte(value)
	if len(encoded) < minimum || (exact > 0 && len(encoded) != exact) {
		return nil, errors.New(name + " has an invalid byte length")
	}
	return encoded, nil
}

func decodeSHA256(value string) ([32]byte, error) {
	var result [32]byte
	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil || len(decoded) != len(result) {
		return result, errors.New("PEERGO_LEGACY_SNAPSHOT_SHA256 must be 64 lowercase hex characters")
	}
	if hex.EncodeToString(decoded) != strings.TrimSpace(value) {
		return result, errors.New("PEERGO_LEGACY_SNAPSHOT_SHA256 must use lowercase hex")
	}
	copy(result[:], decoded)
	return result, nil
}
