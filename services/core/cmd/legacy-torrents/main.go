package main

import (
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/legacymedia"
	"github.com/peergo/peergo/services/core/internal/legacytorrents"
	platformconfig "github.com/peergo/peergo/services/core/internal/platform/config"
	platformobjectstore "github.com/peergo/peergo/services/core/internal/platform/objectstore"
)

type settings struct {
	SourceDatabaseURL string
	CoreDatabaseURL   string
	VaultDatabaseURL  string
	RunID             uuid.UUID
	SnapshotSHA256    [32]byte
	MappingVersion    string
	TorrentRoot       string
	ImageRoot         string
	OccurredAt        time.Time
	ReconciledAt      time.Time
	ProgressEvery     int64
	Exclusions        legacytorrents.TorrentExclusionManifest
	ExclusionOutput   string
	DatabaseDumpPath  string
	PreflightOutput   string
	PreflightManifest string
	AcceptanceOutput  string
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	action := flag.String(
		"action",
		"inventory",
		"preflight, status, inventory, exclusions-template, validate, import, purchases, reconcile, or acceptance",
	)
	flag.Parse()
	if !validAction(*action) {
		logger.Error("unsupported legacy torrent migration action", "action", *action)
		os.Exit(2)
	}
	config, err := loadSettings(*action)
	if err != nil {
		logger.Error("invalid legacy torrent migration configuration", "error", err)
		os.Exit(1)
	}
	if config.Exclusions.Len() > 0 {
		digest := config.Exclusions.ContentSHA256()
		logger.Info(
			"loaded snapshot-bound torrent exclusions",
			"count", config.Exclusions.Len(),
			"manifest_sha256", hex.EncodeToString(digest[:]),
		)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startupCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	core, err := openPool(
		startupCtx,
		config.CoreDatabaseURL,
		*action == "status" || *action == "preflight" || *action == "acceptance",
		"peergo-legacy-torrents-core",
	)
	if err != nil {
		logger.Error("open PeerGo Core database", "error", err)
		os.Exit(1)
	}
	defer core.Close()

	inventoryConfig := legacytorrents.InventoryConfig{
		RunID:          config.RunID,
		SnapshotSHA256: config.SnapshotSHA256,
		MappingVersion: config.MappingVersion,
	}
	if *action == "status" {
		result, statusErr := legacytorrents.InspectMigrationStatus(ctx, core, inventoryConfig)
		if statusErr != nil {
			logger.Error("read legacy migration status failed", "error", statusErr)
			os.Exit(1)
		}
		logger.Info(
			"legacy migration status",
			"run_id", result.RunID,
			"state", result.State,
			"checkpoints_complete", result.CheckpointsComplete(),
			"expected_users", result.ExpectedUsers,
			"imported_users", result.ImportedUsers,
			"user_mappings", result.UserMappings,
			"expected_torrents", result.ExpectedTorrents,
			"imported_torrents", result.ImportedTorrents,
			"excluded_torrents", result.ExcludedTorrents,
			"imported_torrent_objects", result.ImportedTorrentObjects,
			"excluded_torrent_objects", result.ExcludedTorrentObjects,
			"torrent_mappings", result.TorrentMappings,
			"verified_preferred_objects", result.VerifiedPreferredObjects,
			"purchase_price_openings", result.PurchasePriceOpenings,
			"purchase_rows", result.PurchaseRows,
			"purchase_entitlements", result.PurchaseEntitlements,
			"purchase_unresolved", result.PurchaseUnresolved,
			"medal_definitions", result.MedalDefinitions,
			"user_medals", result.UserMedals,
			"medal_benefit_users", result.MedalBenefitUsers,
			"positive_medal_benefit_users", result.PositiveMedalBenefitUsers,
			"workgroup_memberships", result.WorkgroupMemberships,
			"reseed_memberships", result.ReseedMemberships,
			"review_memberships", result.ReviewMemberships,
			"retention_memberships", result.RetentionMemberships,
			"pending_workgroup_benefits", result.PendingWorkgroupBenefits,
			"unresolved_discrepancies", result.UnresolvedDiscrepancies,
			"tracker_run_outbox_events", result.TrackerRunOutboxEvents,
			"tracker_pending_events", result.TrackerPendingEvents,
			"tracker_projection_sequence", result.TrackerProjectionSequence,
			"tracker_outbox_sequence", result.TrackerOutboxSequence,
			"tracker_enabled_torrents", result.TrackerEnabledTorrents,
			"tracker_subject_sequence", result.TrackerSubjectSequence,
			"tracker_projection_drained", result.TrackerProjectionDrained(),
		)
		return
	}

	source, err := openPool(startupCtx, config.SourceDatabaseURL, true, "peergo-legacy-torrents-source")
	if err != nil {
		logger.Error("open read-only PtYes source", "error", err)
		os.Exit(1)
	}
	defer source.Close()
	if *action == "purchases" {
		result, purchaseErr := legacytorrents.ImportPurchases(
			ctx,
			source,
			core,
			legacytorrents.PurchaseImportConfig{
				Inventory:  inventoryConfig,
				ImportedAt: config.OccurredAt,
			},
			func(progress legacytorrents.PurchaseImportProgress) {
				if progress.Processed%config.ProgressEvery == 0 || progress.Processed == progress.Expected {
					logger.Info("legacy torrent purchase import progress", "processed", progress.Processed, "expected", progress.Expected)
				}
			},
		)
		if purchaseErr != nil {
			logger.Error("legacy torrent purchase import failed", "error", purchaseErr)
			os.Exit(1)
		}
		logger.Info(
			"legacy torrent purchase import completed",
			"run_id", result.RunID,
			"source_rows", result.SourceRows,
			"price_openings", result.PriceOpenings,
			"entitlements", result.Entitlements,
			"duplicate_completed", result.DuplicateCompleted,
			"refunded", result.Refunded,
			"unresolved_torrents", result.UnresolvedTorrents,
			"unmapped_torrents", result.UnmappedTorrents,
			"unmapped_users", result.UnmappedUsers,
		)
		return
	}
	if *action == "acceptance" {
		if err := runCutoverAcceptance(
			ctx, startupCtx, logger, config, inventoryConfig, source, core,
		); err != nil {
			logger.Error("legacy cutover acceptance failed", "error", err)
			os.Exit(1)
		}
		return
	}

	if *action == "preflight" {
		storageSettings, storageSettingsErr := platformconfig.LoadTorrentUploadStorageTool()
		if storageSettingsErr != nil {
			logger.Error("invalid preflight object storage configuration", "error", storageSettingsErr)
			os.Exit(1)
		}
		if storageSettings.DatabaseURL != config.CoreDatabaseURL {
			logger.Error("preflight storage Core URL does not match migration Core URL")
			os.Exit(1)
		}
		for name, databaseURL := range map[string]string{
			"source": config.SourceDatabaseURL,
			"Core":   config.CoreDatabaseURL,
			"Vault":  config.VaultDatabaseURL,
		} {
			if databaseErr := platformconfig.ValidateCutoverDatabaseURL(
				databaseURL,
				storageSettings.Environment,
			); databaseErr != nil {
				logger.Error("unsafe preflight database URL", "database", name, "error", databaseErr)
				os.Exit(1)
			}
		}
		probeCtx, cancelProbe := context.WithTimeout(ctx, 15*time.Second)
		probeErr := platformobjectstore.ProbeConfiguredReadOnly(probeCtx, storageSettings.Store)
		cancelProbe()
		if probeErr != nil {
			logger.Error("read-only destination object storage probe failed", "error", probeErr)
			os.Exit(1)
		}
		vault, vaultErr := openPool(startupCtx, config.VaultDatabaseURL, true, "peergo-legacy-preflight-vault")
		if vaultErr != nil {
			logger.Error("open read-only preflight Privacy Vault", "error", vaultErr)
			os.Exit(1)
		}
		defer vault.Close()
		dumpSHA256, dumpBytes, dumpErr := legacytorrents.InspectCutoverDatabaseDump(config.DatabaseDumpPath)
		if dumpErr != nil {
			logger.Error("inspect immutable cutover database dump failed", "error", dumpErr)
			os.Exit(1)
		}
		if dumpSHA256 != config.SnapshotSHA256 {
			logger.Error("cutover database dump does not match PEERGO_LEGACY_SNAPSHOT_SHA256")
			os.Exit(1)
		}
		archiveSHA256, archiveBytes, archiveObjects, archiveErr := legacytorrents.InspectCutoverArchive(config.TorrentRoot)
		if archiveErr != nil {
			logger.Error("inspect immutable cutover torrent archive failed", "error", archiveErr)
			os.Exit(1)
		}
		imageArchive, imageArchiveErr := legacymedia.InspectSourceArchive(config.ImageRoot)
		if imageArchiveErr != nil {
			logger.Error("inspect immutable cutover image archive failed", "error", imageArchiveErr)
			os.Exit(1)
		}
		storageDigest := platformconfig.ObjectStoreConfigSHA256(storageSettings.Store)
		report, preflightErr := legacytorrents.InspectCutoverPreflight(
			ctx,
			source,
			core,
			vault,
			legacytorrents.CutoverPreflightConfig{
				Inventory: inventoryConfig, OccurredAt: config.OccurredAt, CheckedAt: time.Now(),
				DatabaseDumpBytes: dumpBytes, TorrentArchiveSHA256: archiveSHA256,
				TorrentArchiveBytes: archiveBytes, TorrentArchiveObjects: archiveObjects,
				ImageArchiveSHA256: imageArchive.SHA256, ImageArchiveBytes: imageArchive.ByteLength,
				ImageArchiveObjects: imageArchive.ImageCount,
				StorageBackendID:    storageSettings.Store.BackendID,
				StorageDriver:       storageSettings.Store.Driver, StorageConfigSHA256: storageDigest,
				Exclusions: config.Exclusions,
			},
		)
		if preflightErr != nil {
			logger.Error("legacy cutover preflight failed", "error", preflightErr)
			os.Exit(1)
		}
		manifestSHA256, writeErr := legacytorrents.WriteCutoverPreflightManifest(config.PreflightOutput, report)
		if writeErr != nil {
			logger.Error("write legacy cutover preflight manifest failed", "error", writeErr)
			os.Exit(1)
		}
		logger.Info(
			"legacy cutover preflight passed",
			"run_id", report.RunID,
			"run_mode", report.RunMode,
			"run_state", report.RunState,
			"expected_users", report.ExpectedUsers,
			"expected_torrents", report.ExpectedTorrents,
			"torrent_archive_objects", report.TorrentArchiveObjects,
			"image_archive_objects", report.ImageArchiveObjects,
			"excluded_torrents", report.ExcludedTorrents,
			"storage_backend_id", report.StorageBackendID,
			"manifest", config.PreflightOutput,
			"manifest_sha256", hex.EncodeToString(manifestSHA256[:]),
		)
		return
	}

	if *action == "exclusions-template" {
		result, templateErr := legacytorrents.WriteTorrentExclusionCandidate(
			ctx,
			source,
			config.TorrentRoot,
			config.SnapshotSHA256,
			config.ExclusionOutput,
		)
		if templateErr != nil {
			logger.Error("write torrent exclusion candidate failed", "error", templateErr)
			os.Exit(1)
		}
		logger.Info(
			"torrent exclusion candidate written for operator review",
			"output", config.ExclusionOutput,
			"missing_objects", result.MissingObjects,
			"recovered_archive_objects", result.RecoveredObjects,
			"unreferenced_archive_objects", result.UnreferencedObjects,
			"candidate_sha256", hex.EncodeToString(result.ContentSHA256[:]),
		)
		return
	}
	if *action == "reconcile" {
		storageSettings, storageSettingsErr := platformconfig.LoadTorrentUploadStorageTool()
		if storageSettingsErr != nil {
			logger.Error("invalid reconciliation object storage configuration", "error", storageSettingsErr)
			os.Exit(1)
		}
		if storageSettings.DatabaseURL != config.CoreDatabaseURL {
			logger.Error("reconciliation storage Core URL does not match migration Core URL")
			os.Exit(1)
		}
		store, storeErr := platformobjectstore.NewConfigured(ctx, storageSettings.Store)
		if storeErr != nil {
			logger.Error("compose reconciliation object store", "error", storeErr)
			os.Exit(1)
		}
		vault, vaultErr := openPool(startupCtx, config.VaultDatabaseURL, true, "peergo-legacy-reconciliation-vault")
		if vaultErr != nil {
			logger.Error("open reconciliation Privacy Vault", "error", vaultErr)
			os.Exit(1)
		}
		defer vault.Close()
		torrentVerification, torrentErr := legacytorrents.Import(
			ctx,
			source,
			core,
			legacytorrents.ImportConfig{
				Inventory: inventoryConfig, TorrentRoot: config.TorrentRoot,
				OccurredAt: config.OccurredAt, ProgressEvery: config.ProgressEvery,
				Store: store, Exclusions: config.Exclusions,
			},
			func(progress legacytorrents.ImportProgress) {
				logger.Info(
					"legacy reconciliation torrent verification progress",
					"processed", progress.Processed,
					"expected", progress.Expected,
					"skipped", progress.Skipped,
					"excluded", progress.Excluded,
				)
			},
		)
		if torrentErr != nil {
			logger.Error("legacy reconciliation torrent verification failed", "error", torrentErr)
			os.Exit(1)
		}
		result, reconcileErr := legacytorrents.ReconcileMigration(
			ctx,
			source,
			core,
			vault,
			legacytorrents.ReconciliationConfig{
				Inventory: inventoryConfig, ReconciledAt: config.ReconciledAt,
				BackendID: string(store.BackendID()),
			},
			torrentVerification,
		)
		if reconcileErr != nil {
			logger.Error("legacy migration reconciliation failed", "error", reconcileErr)
			os.Exit(1)
		}
		logger.Info(
			"legacy migration reconciled",
			"run_id", result.RunID,
			"state", result.State,
			"users", result.Users,
			"vault_credentials", result.VaultCredentials,
			"torrents", result.Torrents,
			"excluded_torrents", result.ExcludedTorrents,
			"published_torrents", result.PublishedTorrents,
			"torrent_files", result.TorrentFiles,
			"torrent_objects", result.TorrentObjects,
			"torrent_purchase_rows", result.PurchaseRows,
			"torrent_purchase_rights", result.PurchaseRights,
			"torrent_purchase_evidence_only", result.PurchaseEvidence,
		)
		return
	}
	if *action == "import" {
		storageSettings, storageSettingsErr := platformconfig.LoadTorrentUploadStorageTool()
		if storageSettingsErr != nil {
			logger.Error("invalid legacy torrent destination storage configuration", "error", storageSettingsErr)
			os.Exit(1)
		}
		if storageSettings.DatabaseURL != config.CoreDatabaseURL {
			logger.Error("legacy torrent destination storage Core URL does not match migration Core URL")
			os.Exit(1)
		}
		store, storeErr := platformobjectstore.NewConfigured(ctx, storageSettings.Store)
		if storeErr != nil {
			logger.Error("compose legacy destination object store", "error", storeErr)
			os.Exit(1)
		}
		result, importErr := legacytorrents.Import(
			ctx,
			source,
			core,
			legacytorrents.ImportConfig{
				Inventory: inventoryConfig, TorrentRoot: config.TorrentRoot,
				OccurredAt: config.OccurredAt, ProgressEvery: config.ProgressEvery,
				Store: store, Exclusions: config.Exclusions,
			},
			func(progress legacytorrents.ImportProgress) {
				logger.Info(
					"legacy torrent import progress",
					"phase", progress.Phase,
					"processed", progress.Processed,
					"expected", progress.Expected,
					"imported", progress.Imported,
					"skipped", progress.Skipped,
					"excluded", progress.Excluded,
				)
			},
		)
		if importErr != nil {
			logger.Error("legacy torrent import failed", "error", importErr)
			os.Exit(1)
		}
		logger.Info(
			"legacy torrent import completed",
			"run_id", result.RunID,
			"expected_torrents", result.ExpectedTorrents,
			"imported_torrents", result.ImportedTorrents,
			"skipped_torrents", result.SkippedTorrents,
			"excluded_torrents", result.ExcludedTorrents,
			"published_torrents", result.PublishedTorrents,
			"object_bytes", result.ObjectBytes,
			"facet_values", result.FacetValues,
			"external_identifiers", result.ExternalIdentifiers,
			"recovered_archive_objects", result.RecoveredObjects,
			"prepared_facet_options", result.Preparation.FacetOptions,
			"prepared_category_facet_options", result.Preparation.CategoryFacetOptions,
			"resource_groups", result.Preparation.ResourceGroups,
			"resource_group_external_ids", result.Preparation.ResourceGroupExternalIDs,
			"recovered_group_external_ids", result.Preparation.RecoveredGroupExternalIDs,
			"skipped_group_external_ids", result.Preparation.SkippedGroupExternalIDs,
		)
		return
	}
	if *action == "validate" {
		result, validateErr := legacytorrents.ValidateObjects(
			ctx,
			source,
			core,
			legacytorrents.ObjectValidationConfig{
				Inventory: inventoryConfig, TorrentRoot: config.TorrentRoot,
				OccurredAt: config.OccurredAt, ProgressEvery: config.ProgressEvery,
				Exclusions: config.Exclusions,
			},
			func(progress legacytorrents.ObjectValidationProgress) {
				logger.Info(
					"legacy torrent object validation progress",
					"processed", progress.Processed,
					"expected", progress.Expected,
				)
			},
		)
		if validateErr != nil {
			var blocking *legacytorrents.ObjectValidationFailure
			if errors.As(validateErr, &blocking) {
				logger.Error(
					"legacy torrent object validation found blocking issues",
					"run_id", result.RunID,
					"archive_objects", result.ArchiveObjects,
					"unreferenced_archive_objects", result.UnreferencedArchiveObjects,
					"recovered_archive_objects", result.RecoveredArchiveObjects,
					"recovered_archive_legacy_ids", result.RecoveredArchiveLegacyIDs,
					"ambiguous_archive_objects", result.AmbiguousArchiveObjects,
					"validated_torrents", result.ValidatedTorrents,
					"excluded_torrents", result.ExcludedTorrents,
					"excluded_legacy_ids", result.ExcludedLegacyIDs,
					"blocking_issue_counts", blocking.IssueCounts,
					"blocking_legacy_ids", blocking.LegacyIDs,
					"blocking_diagnostics", blocking.Diagnostics,
				)
				os.Exit(1)
			}
			logger.Error("legacy torrent object validation failed", "error", validateErr)
			os.Exit(1)
		}
		logger.Info(
			"legacy torrent object validation completed",
			"run_id", result.RunID,
			"archive_objects", result.ArchiveObjects,
			"unreferenced_archive_objects", result.UnreferencedArchiveObjects,
			"recovered_archive_objects", result.RecoveredArchiveObjects,
			"recovered_archive_legacy_ids", result.RecoveredArchiveLegacyIDs,
			"ambiguous_archive_objects", result.AmbiguousArchiveObjects,
			"validated_torrents", result.ValidatedTorrents,
			"excluded_torrents", result.ExcludedTorrents,
			"excluded_legacy_ids", result.ExcludedLegacyIDs,
			"validated_objects", result.ValidatedObjects,
			"object_bytes", result.ObjectBytes,
			"missing_database_manifests", result.MissingDatabaseManifests,
			"compatibility_flags", result.CompatibilityFlagCounts,
			"object_manifest_sha256", hex.EncodeToString(result.ObjectManifestSHA256[:]),
		)
		return
	}

	result, err := legacytorrents.InspectInventory(ctx, source, core, inventoryConfig)
	if err != nil {
		logger.Error("legacy torrent inventory failed", "error", err)
		os.Exit(1)
	}
	logger.Info(
		"legacy torrent inventory completed",
		"run_id", result.RunID,
		"users", result.Users,
		"torrents", result.Torrents,
		"torrent_files", result.FileRows,
		"resource_groups", result.Groups,
		"missing_file_manifests", result.MissingFileManifests,
		"duplicate_path_torrents", result.DuplicatePathTorrents,
		"case_colliding_path_torrents", result.CaseCollidingPathTorrents,
		"recovered_external_ids", result.RecoveredExternalIDs,
		"skipped_external_ids", result.SkippedExternalIDs,
		"external_id_warnings", result.ExternalIDWarningCounts,
		"facet_values", result.FacetValues,
		"categories", result.CategoryCounts,
		"states", result.StateCounts,
	)
}

func runCutoverAcceptance(
	ctx context.Context,
	startupCtx context.Context,
	logger *slog.Logger,
	config settings,
	inventoryConfig legacytorrents.InventoryConfig,
	source *pgxpool.Pool,
	core *pgxpool.Pool,
) error {
	storageSettings, err := platformconfig.LoadTorrentUploadStorageTool()
	if err != nil {
		return err
	}
	if storageSettings.DatabaseURL != config.CoreDatabaseURL {
		return errors.New("acceptance storage Core URL does not match migration Core URL")
	}
	acceptanceSettings, err := platformconfig.LoadCutoverAcceptance()
	if err != nil {
		return err
	}
	refreshTrackerSnapshots, err := loadAcceptanceSnapshotRefresher()
	if err != nil {
		return err
	}
	if acceptanceSettings.Environment != storageSettings.Environment {
		return errors.New("acceptance and storage environments do not match")
	}
	for name, databaseURL := range map[string]string{
		"source": config.SourceDatabaseURL,
		"Core":   config.CoreDatabaseURL,
		"Vault":  config.VaultDatabaseURL,
	} {
		if err := platformconfig.ValidateCutoverDatabaseURL(
			databaseURL, acceptanceSettings.Environment,
		); err != nil {
			return errors.New("unsafe acceptance " + name + " database URL: " + err.Error())
		}
	}
	store, err := platformobjectstore.NewConfigured(ctx, storageSettings.Store)
	if err != nil {
		return err
	}
	vault, err := openPool(startupCtx, config.VaultDatabaseURL, true, "peergo-legacy-acceptance-vault")
	if err != nil {
		return err
	}
	defer vault.Close()
	dumpSHA256, dumpBytes, err := legacytorrents.InspectCutoverDatabaseDump(config.DatabaseDumpPath)
	if err != nil {
		return err
	}
	if dumpSHA256 != config.SnapshotSHA256 {
		return errors.New("cutover database dump does not match PEERGO_LEGACY_SNAPSHOT_SHA256")
	}
	archiveSHA256, archiveBytes, archiveObjects, err := legacytorrents.InspectCutoverArchive(config.TorrentRoot)
	if err != nil {
		return err
	}
	imageArchive, err := legacymedia.InspectSourceArchive(config.ImageRoot)
	if err != nil {
		return err
	}
	preflight, preflightSHA256, err := legacytorrents.LoadCutoverPreflightManifest(config.PreflightManifest)
	if err != nil {
		return err
	}
	report, err := legacytorrents.InspectCutoverAcceptance(
		ctx,
		source,
		core,
		vault,
		store,
		legacytorrents.CutoverAcceptanceConfig{
			Inventory: inventoryConfig, OccurredAt: config.OccurredAt, Now: time.Now,
			DatabaseDumpBytes: dumpBytes, TorrentArchiveSHA256: archiveSHA256,
			TorrentArchiveBytes: archiveBytes, TorrentArchiveObjects: archiveObjects,
			ImageArchiveSHA256: imageArchive.SHA256, ImageArchiveBytes: imageArchive.ByteLength,
			ImageArchiveObjects: imageArchive.ImageCount,
			StorageBackendID:    storageSettings.Store.BackendID,
			StorageDriver:       storageSettings.Store.Driver,
			StorageConfigSHA256: platformconfig.ObjectStoreConfigSHA256(storageSettings.Store),
			Exclusions:          config.Exclusions, Preflight: preflight,
			PreflightManifestSHA256:    preflightSHA256,
			TrackerSnapshotPath:        acceptanceSettings.SnapshotPath,
			TrackerSubjectSnapshotPath: acceptanceSettings.SubjectSnapshotPath,
			TrackerTrustedKeys:         acceptanceSettings.TrustedKeys,
			TrackerSnapshotMaxAge:      acceptanceSettings.SnapshotMaxAge,
			TrackerSubjectMaxAge:       acceptanceSettings.SubjectMaxAge,
			TrackerMaxFutureSkew:       acceptanceSettings.MaxFutureSkew,
			RefreshTrackerSnapshots:    refreshTrackerSnapshots,
			ProgressEvery:              config.ProgressEvery,
		},
		func(progress legacytorrents.CutoverAcceptanceProgress) {
			logger.Info(
				"legacy cutover acceptance progress",
				"phase", progress.Phase,
				"processed", progress.Processed,
				"expected", progress.Expected,
			)
		},
	)
	if err != nil {
		return err
	}
	manifestSHA256, err := legacytorrents.WriteCutoverAcceptanceManifest(config.AcceptanceOutput, report)
	if err != nil {
		return err
	}
	logger.Info(
		"legacy cutover acceptance passed",
		"run_id", report.RunID,
		"users", report.Users,
		"torrents", report.Torrents,
		"excluded_torrents", report.ExcludedTorrents,
		"verified_stored_objects", report.VerifiedStoredObjects,
		"verified_stored_object_bytes", report.VerifiedStoredObjectBytes,
		"torrent_images", report.TorrentImages,
		"verified_image_objects", report.VerifiedImageObjects,
		"verified_image_object_bytes", report.VerifiedImageObjectBytes,
		"image_archive_sha256", report.ImageArchiveSHA256,
		"tracker_control_sequence", report.TrackerControlSequence,
		"tracker_torrent_count", report.TrackerTorrentCount,
		"tracker_subject_sequence", report.TrackerSubjectSequence,
		"tracker_subject_count", report.TrackerSubjectCount,
		"ready_to_activate", report.ReadyToActivate,
		"manifest", config.AcceptanceOutput,
		"manifest_sha256", hex.EncodeToString(manifestSHA256[:]),
	)
	return nil
}

// loadAcceptanceSnapshotRefresher keeps the verifier on public keys while an
// immutable, separately built publisher owns the signing key. The child is
// forced into one-shot mode and is invoked synchronously at the exact barrier
// chosen by InspectCutoverAcceptance.
func loadAcceptanceSnapshotRefresher() (func(context.Context) error, error) {
	const environmentName = "PEERGO_LEGACY_ACCEPTANCE_SNAPSHOT_REFRESH_BINARY"
	path := strings.TrimSpace(os.Getenv(environmentName))
	if path == "" {
		return nil, nil
	}
	if !filepath.IsAbs(path) {
		return nil, errors.New(environmentName + " must be absolute")
	}
	path = filepath.Clean(path)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm()&0o022 != 0 || info.Mode().Perm()&0o111 == 0 {
		return nil, errors.New(environmentName + " must be a protected executable regular file")
	}
	return func(ctx context.Context) error {
		refreshCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		command := exec.CommandContext(refreshCtx, path)
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		command.Env = make([]string, 0, len(os.Environ())+1)
		for _, value := range os.Environ() {
			if !strings.HasPrefix(value, "PEERGO_TRACKER_SNAPSHOT_PUBLISH_INTERVAL=") {
				command.Env = append(command.Env, value)
			}
		}
		command.Env = append(command.Env, "PEERGO_TRACKER_SNAPSHOT_PUBLISH_INTERVAL=")
		if err := command.Run(); err != nil {
			return fmt.Errorf("one-shot Tracker snapshot refresh failed: %w", err)
		}
		return nil
	}, nil
}

func loadSettings(action string) (settings, error) {
	var result settings
	var err error
	result.CoreDatabaseURL, err = required("PEERGO_CORE_DATABASE_URL")
	if err != nil {
		return settings{}, err
	}
	if action != "status" {
		result.SourceDatabaseURL, err = required("PEERGO_LEGACY_SOURCE_DATABASE_URL")
		if err != nil {
			return settings{}, err
		}
		if result.SourceDatabaseURL == result.CoreDatabaseURL {
			return settings{}, errors.New("source and Core database URLs must be distinct")
		}
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
	result.ProgressEvery = 250
	if raw := strings.TrimSpace(os.Getenv("PEERGO_LEGACY_PROGRESS_EVERY")); raw != "" {
		value, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || value < 1 || value > 10000 {
			return settings{}, errors.New("PEERGO_LEGACY_PROGRESS_EVERY must be between 1 and 10000")
		}
		result.ProgressEvery = value
	}
	if action == "validate" || action == "import" || action == "exclusions-template" ||
		action == "preflight" || action == "acceptance" {
		result.TorrentRoot, err = required("PEERGO_LEGACY_TORRENT_ROOT")
		if err != nil {
			return settings{}, err
		}
	}
	if action == "preflight" || action == "acceptance" {
		result.ImageRoot, err = required("PEERGO_LEGACY_IMAGE_ROOT")
		if err != nil || !filepath.IsAbs(result.ImageRoot) {
			return settings{}, errors.New("PEERGO_LEGACY_IMAGE_ROOT must be an absolute ZIP path")
		}
	}
	if action == "exclusions-template" {
		result.ExclusionOutput, err = required("PEERGO_LEGACY_TORRENT_EXCLUSIONS_OUTPUT")
		if err != nil || !filepath.IsAbs(result.ExclusionOutput) {
			return settings{}, errors.New("PEERGO_LEGACY_TORRENT_EXCLUSIONS_OUTPUT must be absolute")
		}
	}
	if action == "preflight" || action == "acceptance" {
		result.DatabaseDumpPath, err = required("PEERGO_LEGACY_DUMP_PATH")
		if err != nil || !filepath.IsAbs(result.DatabaseDumpPath) {
			return settings{}, errors.New("PEERGO_LEGACY_DUMP_PATH must be absolute")
		}
	}
	if action == "preflight" {
		result.PreflightOutput, err = required("PEERGO_LEGACY_PREFLIGHT_OUTPUT")
		if err != nil || !filepath.IsAbs(result.PreflightOutput) {
			return settings{}, errors.New("PEERGO_LEGACY_PREFLIGHT_OUTPUT must be absolute")
		}
	}
	if action == "acceptance" {
		result.PreflightManifest, err = required("PEERGO_LEGACY_PREFLIGHT_MANIFEST")
		if err != nil || !filepath.IsAbs(result.PreflightManifest) {
			return settings{}, errors.New("PEERGO_LEGACY_PREFLIGHT_MANIFEST must be absolute")
		}
		result.AcceptanceOutput, err = required("PEERGO_LEGACY_ACCEPTANCE_OUTPUT")
		if err != nil || !filepath.IsAbs(result.AcceptanceOutput) {
			return settings{}, errors.New("PEERGO_LEGACY_ACCEPTANCE_OUTPUT must be absolute")
		}
		if filepath.Clean(result.PreflightManifest) == filepath.Clean(result.AcceptanceOutput) {
			return settings{}, errors.New("preflight manifest and acceptance output paths must differ")
		}
	}
	if action == "validate" || action == "import" || action == "reconcile" ||
		action == "preflight" || action == "acceptance" {
		if exclusionPath := strings.TrimSpace(os.Getenv("PEERGO_LEGACY_TORRENT_EXCLUSIONS")); exclusionPath != "" {
			result.Exclusions, err = legacytorrents.LoadTorrentExclusionManifest(
				exclusionPath, result.SnapshotSHA256,
			)
			if err != nil {
				return settings{}, err
			}
		}
	}
	if action == "reconcile" || action == "preflight" || action == "acceptance" {
		result.VaultDatabaseURL, err = required("PEERGO_VAULT_DATABASE_URL")
		if err != nil {
			return settings{}, err
		}
		if result.VaultDatabaseURL == result.SourceDatabaseURL || result.VaultDatabaseURL == result.CoreDatabaseURL {
			return settings{}, errors.New("Vault, source, and Core database URLs must be distinct")
		}
		if action == "reconcile" {
			result.TorrentRoot, err = required("PEERGO_LEGACY_TORRENT_ROOT")
			if err != nil {
				return settings{}, err
			}
		}
	}
	if action == "validate" || action == "import" || action == "purchases" || action == "reconcile" ||
		action == "preflight" || action == "acceptance" {
		result.OccurredAt, err = time.Parse(
			time.RFC3339Nano,
			strings.TrimSpace(os.Getenv("PEERGO_LEGACY_OCCURRED_AT")),
		)
		if err != nil || result.OccurredAt.IsZero() {
			return settings{}, errors.New("PEERGO_LEGACY_OCCURRED_AT must be a fixed RFC3339 timestamp")
		}
	}
	if action == "reconcile" {
		result.ReconciledAt, err = time.Parse(
			time.RFC3339Nano,
			strings.TrimSpace(os.Getenv("PEERGO_LEGACY_RECONCILED_AT")),
		)
		if err != nil || result.ReconciledAt.IsZero() {
			return settings{}, errors.New("PEERGO_LEGACY_RECONCILED_AT must be a fixed RFC3339 timestamp")
		}
	}
	return result, nil
}

func validAction(action string) bool {
	switch action {
	case "preflight", "status", "inventory", "exclusions-template", "validate", "import", "purchases", "reconcile", "acceptance":
		return true
	default:
		return false
	}
}

func openPool(
	ctx context.Context,
	databaseURL string,
	readOnly bool,
	applicationName string,
) (*pgxpool.Pool, error) {
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

func decodeSHA256(value string) ([32]byte, error) {
	var result [32]byte
	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil || len(decoded) != len(result) ||
		hex.EncodeToString(decoded) != strings.TrimSpace(value) {
		return result, errors.New("PEERGO_LEGACY_SNAPSHOT_SHA256 must be 64 lowercase hex characters")
	}
	copy(result[:], decoded)
	return result, nil
}
