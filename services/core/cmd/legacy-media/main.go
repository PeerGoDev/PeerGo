package main

import (
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
	"github.com/peergo/peergo/services/core/internal/legacymedia"
	platformconfig "github.com/peergo/peergo/services/core/internal/platform/config"
	platformobjectstore "github.com/peergo/peergo/services/core/internal/platform/objectstore"
)

type settings struct {
	SourceDatabaseURL string
	CoreDatabaseURL   string
	RunID             uuid.UUID
	SnapshotSHA256    [32]byte
	MappingVersion    string
	ImageArchive      string
	ImageArchiveSHA   [32]byte
	OccurredAt        time.Time
	ProgressEvery     int64
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	action := flag.String("action", "validate", "validate, import, or reconcile")
	flag.Parse()
	if *action != "validate" && *action != "import" && *action != "reconcile" {
		logger.Error("unsupported legacy media migration action", "action", *action)
		os.Exit(2)
	}
	config, err := loadSettings(*action)
	if err != nil {
		logger.Error("invalid legacy media migration configuration", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startupCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	source, err := openPool(startupCtx, config.SourceDatabaseURL, true, "peergo-legacy-media-source")
	if err != nil {
		logger.Error("open read-only PtYes source", "error", err)
		os.Exit(1)
	}
	defer source.Close()
	core, err := openPool(startupCtx, config.CoreDatabaseURL, false, "peergo-legacy-media-core")
	if err != nil {
		logger.Error("open PeerGo Core database", "error", err)
		os.Exit(1)
	}
	defer core.Close()
	inventory := legacymedia.InventoryConfig{
		RunID: config.RunID, SnapshotSHA256: config.SnapshotSHA256, MappingVersion: config.MappingVersion,
	}
	if *action == "validate" {
		result, err := legacymedia.Validate(ctx, source, core, legacymedia.ValidationConfig{
			Inventory: inventory, ImageArchive: config.ImageArchive, ArchiveSHA256: config.ImageArchiveSHA,
			OccurredAt: config.OccurredAt, ProgressEvery: config.ProgressEvery,
		}, func(progress legacymedia.ValidationProgress) {
			logger.Info("validating legacy torrent images", "processed", progress.Processed, "expected", progress.Expected)
		})
		if err != nil {
			logger.Error("legacy torrent image validation failed", "error", err)
			os.Exit(1)
		}
		logger.Info(
			"legacy torrent image validation complete",
			"run_id", result.RunID,
			"archive_images", result.ArchiveImages,
			"referenced_images", result.ReferencedImages,
			"importable_images", result.ImportableImages,
			"excluded_torrent_images", result.ExcludedTorrentImages,
			"missing_poster_placeholders", result.MissingPosterPlaceholders,
			"unreferenced_archive_images", result.UnreferencedArchiveImages,
			"original_bytes", result.OriginalBytes,
			"manifest_sha256", hex.EncodeToString(result.ManifestSHA256[:]),
		)
		return
	}
	storageSettings, err := platformconfig.LoadTorrentUploadStorageTool()
	if err != nil {
		logger.Error("load legacy image destination storage", "error", err)
		os.Exit(1)
	}
	if storageSettings.DatabaseURL != config.CoreDatabaseURL {
		logger.Error("legacy image storage Core URL does not match migration Core URL")
		os.Exit(1)
	}
	store, err := platformobjectstore.NewConfigured(startupCtx, storageSettings.Store)
	if err != nil {
		logger.Error("compose legacy image destination storage", "error", err)
		os.Exit(1)
	}
	registry, err := objectstorage.NewRegistry(store)
	if err != nil {
		logger.Error("compose legacy image storage registry", "error", err)
		os.Exit(1)
	}
	backendID, err := objectstorage.ParseBackendID(storageSettings.Store.BackendID)
	if err != nil {
		logger.Error("parse legacy image storage backend ID", "error", err)
		os.Exit(1)
	}
	result, err := legacymedia.Import(ctx, source, core, registry, legacymedia.ImportConfig{
		Inventory: inventory, ImageArchive: config.ImageArchive, ArchiveSHA256: config.ImageArchiveSHA,
		OccurredAt: config.OccurredAt, BackendID: backendID, ProgressEvery: config.ProgressEvery,
		VerifyOnly: *action == "reconcile",
	}, func(progress legacymedia.ImportProgress) {
		logger.Info("importing legacy torrent images", "processed", progress.Processed, "expected", progress.Expected)
	})
	if err != nil {
		logger.Error("legacy torrent image import failed", "error", err)
		os.Exit(1)
	}
	logger.Info(
		"legacy torrent image import pass complete",
		"run_id", result.RunID,
		"expected_images", result.ExpectedImages,
		"imported_images", result.ImportedImages,
		"verified_images", result.VerifiedImages,
		"skipped_images", result.SkippedImages,
		"stored_bytes", result.StoredBytes,
		"transformed", result.Transformed,
		"reused_original", result.ReusedOriginal,
	)
	if *action == "reconcile" {
		reconciled, err := legacymedia.Reconcile(ctx, core, result)
		if err != nil {
			logger.Error("legacy torrent image reconciliation failed", "error", err)
			os.Exit(1)
		}
		logger.Info(
			"legacy torrent image reconciliation complete",
			"run_id", reconciled.RunID,
			"imported_images", reconciled.ImportedImages,
			"skipped_posters", reconciled.SkippedPosters,
			"excluded_images", reconciled.ExcludedImages,
			"mapped_images", reconciled.MappedImages,
			"verified_locations", reconciled.VerifiedLocations,
			"legacy_aliases", reconciled.LegacyAliases,
		)
	}
}

func loadSettings(action string) (settings, error) {
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
	if result.SourceDatabaseURL == result.CoreDatabaseURL {
		return settings{}, errors.New("source and Core database URLs must be distinct")
	}
	result.RunID, err = uuid.Parse(strings.TrimSpace(os.Getenv("PEERGO_LEGACY_RUN_ID")))
	if err != nil || result.RunID == uuid.Nil {
		return settings{}, errors.New("PEERGO_LEGACY_RUN_ID must be a non-zero UUID")
	}
	result.SnapshotSHA256, err = decodeSHA256("PEERGO_LEGACY_SNAPSHOT_SHA256")
	if err != nil {
		return settings{}, err
	}
	result.ImageArchiveSHA, err = decodeSHA256("PEERGO_LEGACY_IMAGE_ARCHIVE_SHA256")
	if err != nil {
		return settings{}, err
	}
	result.MappingVersion = strings.TrimSpace(os.Getenv("PEERGO_LEGACY_MAPPING_VERSION"))
	if result.MappingVersion == "" {
		result.MappingVersion = "ptyes-v1"
	}
	result.ImageArchive, err = required("PEERGO_LEGACY_IMAGE_ROOT")
	if err != nil || !filepath.IsAbs(result.ImageArchive) {
		return settings{}, errors.New("PEERGO_LEGACY_IMAGE_ROOT must be an absolute ZIP path")
	}
	result.OccurredAt, err = time.Parse(time.RFC3339Nano, strings.TrimSpace(os.Getenv("PEERGO_LEGACY_OCCURRED_AT")))
	if err != nil || result.OccurredAt.IsZero() {
		return settings{}, errors.New("PEERGO_LEGACY_OCCURRED_AT must be a fixed RFC3339 timestamp")
	}
	result.ProgressEvery = 250
	if raw := strings.TrimSpace(os.Getenv("PEERGO_LEGACY_PROGRESS_EVERY")); raw != "" {
		result.ProgressEvery, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || result.ProgressEvery < 1 || result.ProgressEvery > 10000 {
			return settings{}, errors.New("PEERGO_LEGACY_PROGRESS_EVERY must be between 1 and 10000")
		}
	}
	return result, nil
}

func openPool(ctx context.Context, databaseURL string, readOnly bool, applicationName string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("database URL is invalid")
	}
	config.MaxConns = 4
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

func decodeSHA256(name string) ([32]byte, error) {
	var result [32]byte
	value := strings.TrimSpace(os.Getenv(name))
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != len(result) || hex.EncodeToString(decoded) != value {
		return result, errors.New(name + " must be 64 lowercase hex characters")
	}
	copy(result[:], decoded)
	return result, nil
}
