package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/legacyseedboxes"
)

type settings struct {
	SourceDatabaseURL string
	CoreDatabaseURL   string
	RunID             uuid.UUID
	SnapshotSHA256    [sha256.Size]byte
	MappingVersion    string
	ImportedAt        time.Time
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	action := flag.String("action", "import", "import or verify")
	flag.Parse()
	if *action != "import" && *action != "verify" {
		logger.Error("unsupported legacy seedbox action", "action", *action)
		os.Exit(2)
	}
	config, err := loadSettings()
	if err != nil {
		logger.Error("invalid legacy seedbox configuration", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startupCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	source, err := openPool(startupCtx, config.SourceDatabaseURL, true, "peergo-legacy-seedboxes-source")
	if err != nil {
		logger.Error("open read-only PtYes source", "error", err)
		os.Exit(1)
	}
	defer source.Close()
	core, err := openPool(startupCtx, config.CoreDatabaseURL, *action == "verify", "peergo-legacy-seedboxes-core")
	if err != nil {
		logger.Error("open PeerGo Core database", "error", err)
		os.Exit(1)
	}
	defer core.Close()
	importConfig := legacyseedboxes.Config{
		RunID: config.RunID, SnapshotSHA256: config.SnapshotSHA256,
		MappingVersion: config.MappingVersion, ImportedAt: config.ImportedAt,
	}
	var result legacyseedboxes.Result
	if *action == "verify" {
		result, err = legacyseedboxes.Verify(ctx, source, core, importConfig)
	} else {
		result, err = legacyseedboxes.Import(ctx, source, core, importConfig)
	}
	if err != nil {
		logger.Error("legacy seedbox operation failed", "action", *action, "error", err)
		os.Exit(1)
	}
	logger.Info("legacy seedbox operation completed",
		"action", *action, "run_id", result.RunID, "source_rows", result.SourceRows,
		"enabled_rows", result.EnabledRows, "binding_rows", result.BindingRows,
		"policy_sequence", result.PolicySequence, "policy_revision", result.PolicyRevision,
		"standard_speed_limit_bytes_per_second", result.StandardSpeedLimitBytesPerSecond,
		"duplicate", result.Duplicate,
	)
}

func loadSettings() (settings, error) {
	var result settings
	var err error
	result.SourceDatabaseURL = strings.TrimSpace(os.Getenv("PEERGO_LEGACY_SOURCE_DATABASE_URL"))
	result.CoreDatabaseURL = strings.TrimSpace(os.Getenv("PEERGO_CORE_DATABASE_URL"))
	if result.SourceDatabaseURL == "" || result.CoreDatabaseURL == "" || result.SourceDatabaseURL == result.CoreDatabaseURL {
		return settings{}, errors.New("distinct source and Core database URLs are required")
	}
	result.RunID, err = uuid.Parse(strings.TrimSpace(os.Getenv("PEERGO_LEGACY_RUN_ID")))
	if err != nil || result.RunID == uuid.Nil {
		return settings{}, errors.New("PEERGO_LEGACY_RUN_ID must be a non-zero UUID")
	}
	rawDigest := strings.TrimSpace(os.Getenv("PEERGO_LEGACY_SNAPSHOT_SHA256"))
	decoded, err := hex.DecodeString(rawDigest)
	if err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != rawDigest {
		return settings{}, errors.New("PEERGO_LEGACY_SNAPSHOT_SHA256 must be 64 lowercase hex characters")
	}
	copy(result.SnapshotSHA256[:], decoded)
	result.MappingVersion = strings.TrimSpace(os.Getenv("PEERGO_LEGACY_MAPPING_VERSION"))
	if result.MappingVersion == "" {
		result.MappingVersion = "ptyes-v1"
	}
	result.ImportedAt, err = time.Parse(time.RFC3339Nano, strings.TrimSpace(os.Getenv("PEERGO_LEGACY_OCCURRED_AT")))
	if err != nil || result.ImportedAt.IsZero() {
		return settings{}, errors.New("PEERGO_LEGACY_OCCURRED_AT must be a fixed RFC3339 timestamp")
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
