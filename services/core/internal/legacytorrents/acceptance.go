package legacytorrents

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/contracts/go/signedsnapshotv1"
	"github.com/peergo/peergo/contracts/go/trackercontrolv1"
	"github.com/peergo/peergo/contracts/go/trackersubjectcontrolv1"
	"github.com/peergo/peergo/services/core/internal/legacymedia"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

const (
	CutoverAcceptanceSchema = "peergo.legacy-cutover-acceptance.v6"
	maxCutoverManifestBytes = 1 << 20
)

type CutoverAcceptanceConfig struct {
	Inventory                  InventoryConfig
	OccurredAt                 time.Time
	Now                        func() time.Time
	DatabaseDumpBytes          int64
	TorrentArchiveSHA256       [sha256.Size]byte
	TorrentArchiveBytes        int64
	TorrentArchiveObjects      int64
	ImageArchiveSHA256         [sha256.Size]byte
	ImageArchiveBytes          int64
	ImageArchiveObjects        int64
	StorageBackendID           string
	StorageDriver              string
	StorageConfigSHA256        [sha256.Size]byte
	Exclusions                 TorrentExclusionManifest
	Preflight                  CutoverPreflightReport
	PreflightManifestSHA256    [sha256.Size]byte
	TrackerSnapshotPath        string
	TrackerSubjectSnapshotPath string
	TrackerTrustedKeys         map[string]ed25519.PublicKey
	TrackerSnapshotMaxAge      time.Duration
	TrackerSubjectMaxAge       time.Duration
	TrackerMaxFutureSkew       time.Duration
	// RefreshTrackerSnapshots is a production-cutover freshness barrier. When
	// supplied, it runs after all expensive immutable-object reads and must
	// synchronously publish the three signed snapshots before verification.
	RefreshTrackerSnapshots func(context.Context) error
	// ProgressEvery bounds operational log volume while acceptance streams
	// every immutable torrent and image object through SHA-256 verification.
	// Zero keeps the package default so older callers remain compatible.
	ProgressEvery int64
}

type CutoverAcceptanceProgress struct {
	Phase     string
	Processed int64
	Expected  int64
}

type CutoverAcceptanceReport struct {
	Schema                         string    `json:"schema"`
	CheckedAt                      time.Time `json:"checked_at"`
	RunID                          uuid.UUID `json:"run_id"`
	RunState                       string    `json:"run_state"`
	ReconciledAt                   time.Time `json:"reconciled_at"`
	PreflightManifestSHA256        string    `json:"preflight_manifest_sha256"`
	DatabaseDumpSHA256             string    `json:"database_dump_sha256"`
	TorrentArchiveSHA256           string    `json:"torrent_archive_sha256"`
	ImageArchiveSHA256             string    `json:"image_archive_sha256"`
	StorageBackendID               string    `json:"storage_backend_id"`
	Users                          int64     `json:"users"`
	VaultCredentials               int64     `json:"vault_credentials"`
	AttendanceOpenings             int64     `json:"attendance_openings"`
	AttendanceStatsUsers           int64     `json:"attendance_stats_users"`
	AttendanceSourceRecords        int64     `json:"attendance_source_records"`
	AttendanceTotalDays            int64     `json:"attendance_total_days"`
	AttendanceRetroactiveCards     int64     `json:"attendance_retroactive_cards"`
	Torrents                       int64     `json:"torrents"`
	ExcludedTorrents               int64     `json:"excluded_torrents"`
	TorrentFiles                   int64     `json:"torrent_files"`
	TorrentFacetValues             int64     `json:"torrent_facet_values"`
	TorrentExternalIdentifiers     int64     `json:"torrent_external_identifiers"`
	ResourceGroups                 int64     `json:"resource_groups"`
	ResourceGroupExternalIDs       int64     `json:"resource_group_external_identifiers"`
	PublishedTorrents              int64     `json:"published_torrents"`
	VerifiedStoredObjects          int64     `json:"verified_stored_objects"`
	VerifiedStoredObjectBytes      int64     `json:"verified_stored_object_bytes"`
	TorrentImages                  int64     `json:"torrent_images"`
	VerifiedImageObjects           int64     `json:"verified_image_objects"`
	VerifiedImageObjectBytes       int64     `json:"verified_image_object_bytes"`
	TorrentPurchaseRows            int64     `json:"torrent_purchase_rows"`
	TorrentPurchaseRights          int64     `json:"torrent_purchase_rights"`
	TorrentPurchaseEvidenceOnly    int64     `json:"torrent_purchase_evidence_only"`
	TrackerControlSequence         int64     `json:"tracker_control_sequence"`
	TrackerTorrentCount            int       `json:"tracker_torrent_count"`
	TrackerSnapshotGeneratedAt     time.Time `json:"tracker_snapshot_generated_at"`
	TrackerSnapshotSHA256          string    `json:"tracker_snapshot_sha256"`
	TrackerSubjectSequence         int64     `json:"tracker_subject_sequence"`
	TrackerSubjectCount            int       `json:"tracker_subject_count"`
	TrackerSubjectGeneratedAt      time.Time `json:"tracker_subject_generated_at"`
	TrackerSubjectSnapshotSHA256   string    `json:"tracker_subject_snapshot_sha256"`
	CoreRuntimeDefaultsReady       bool      `json:"core_runtime_defaults_ready"`
	LegacyMemberAuthorizationReady bool      `json:"legacy_member_authorization_ready"`
	ReadyToActivate                bool      `json:"ready_to_activate"`
}

type acceptedSourceCounts struct {
	Torrents                 int64
	Files                    int64
	FacetValues              int64
	ExternalIdentifiers      int64
	Groups                   int64
	GroupExternalIdentifiers int64
	Published                int64
}

type acceptedTargetCounts struct {
	Torrents                 int64
	Objects                  int64
	Locations                int64
	Files                    int64
	FacetValues              int64
	ExternalIdentifiers      int64
	Groups                   int64
	GroupMappings            int64
	GroupExternalIdentifiers int64
	Published                int64
	CatalogRows              int64
	OutboxRows               int64
	PendingOutbox            int64
	Allowlisted              int64
	ProjectionSequence       int64
	OutboxSequence           int64
	SiteProfiles             int64
	RegistrationPolicies     int64
}

type acceptedAttendanceCounts struct {
	Openings         int64
	StatsUsers       int64
	SourceRecords    int64
	TotalDays        int64
	RetroactiveCards int64
}

// LoadCutoverPreflightManifest reads the exact non-secret gate produced before
// mutation. Acceptance binds current inputs, databases and storage back to this
// artifact instead of trusting a fresh set of environment variables alone.
func LoadCutoverPreflightManifest(path string) (CutoverPreflightReport, [sha256.Size]byte, error) {
	var zero [sha256.Size]byte
	raw, err := readCutoverArtifact(path, maxCutoverManifestBytes)
	if err != nil {
		return CutoverPreflightReport{}, zero, fmt.Errorf("read cutover preflight manifest: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var report CutoverPreflightReport
	if err := decoder.Decode(&report); err != nil {
		return CutoverPreflightReport{}, zero, errors.New("cutover preflight manifest JSON is invalid")
	}
	if err := ensureJSONEOF(decoder); err != nil || report.Schema != CutoverPreflightSchema || !report.Ready {
		return CutoverPreflightReport{}, zero, errors.New("cutover preflight manifest is invalid")
	}
	return report, sha256.Sum256(raw), nil
}

// InspectCutoverAcceptance is the final read-only migration gate. It verifies
// target relations, every stored .torrent byte stream and both authenticated
// Tracker admission snapshots. Passing it does not enable traffic or authorize
// deletion of PtYes data; those remain explicit operator decisions.
func InspectCutoverAcceptance(
	ctx context.Context,
	source *pgxpool.Pool,
	core *pgxpool.Pool,
	vault *pgxpool.Pool,
	store torrents.ObjectStore,
	config CutoverAcceptanceConfig,
	progress func(CutoverAcceptanceProgress),
) (CutoverAcceptanceReport, error) {
	config.OccurredAt = config.OccurredAt.UTC().Truncate(time.Microsecond)
	if config.Now == nil {
		return CutoverAcceptanceReport{}, errors.New("legacy cutover acceptance clock is required")
	}
	startedAt := config.Now().UTC().Truncate(time.Microsecond)
	if source == nil || core == nil || vault == nil || store == nil ||
		config.Inventory.RunID == uuid.Nil || config.OccurredAt.IsZero() || startedAt.IsZero() ||
		config.Inventory.SnapshotSHA256 == ([sha256.Size]byte{}) || config.DatabaseDumpBytes < 1 ||
		config.TorrentArchiveSHA256 == ([sha256.Size]byte{}) || config.TorrentArchiveBytes < 1 ||
		config.TorrentArchiveObjects < 1 || config.ImageArchiveSHA256 == ([sha256.Size]byte{}) ||
		config.ImageArchiveBytes < 1 || config.ImageArchiveObjects < 1 ||
		config.StorageConfigSHA256 == ([sha256.Size]byte{}) ||
		config.PreflightManifestSHA256 == ([sha256.Size]byte{}) ||
		config.StorageDriver == "" || string(store.BackendID()) != config.StorageBackendID ||
		len(config.TrackerTrustedKeys) == 0 ||
		config.TrackerSnapshotMaxAge <= 0 || config.TrackerSubjectMaxAge <= 0 ||
		config.TrackerMaxFutureSkew < 0 {
		return CutoverAcceptanceReport{}, errors.New("legacy cutover acceptance configuration is invalid")
	}
	if progress == nil {
		progress = func(CutoverAcceptanceProgress) {}
	}
	progressEvery := config.ProgressEvery
	if progressEvery < 1 {
		progressEvery = 250
	}
	if err := platformpostgres.RequireCurrentMigration(ctx, core); err != nil {
		return CutoverAcceptanceReport{}, err
	}
	if err := requireLegacyVaultVersion(ctx, vault); err != nil {
		return CutoverAcceptanceReport{}, err
	}

	sourceIdentity, err := inspectCutoverDatabaseIdentity(ctx, source, 0)
	if err != nil {
		return CutoverAcceptanceReport{}, fmt.Errorf("inspect acceptance source database: %w", err)
	}
	coreIdentity, err := inspectCutoverDatabaseIdentity(ctx, core, platformpostgres.ExpectedMigrationVersion)
	if err != nil {
		return CutoverAcceptanceReport{}, fmt.Errorf("inspect acceptance Core database: %w", err)
	}
	vaultIdentity, err := inspectCutoverDatabaseIdentity(ctx, vault, requiredLegacyVaultMigrationVersion)
	if err != nil {
		return CutoverAcceptanceReport{}, fmt.Errorf("inspect acceptance Vault database: %w", err)
	}
	if !sourceIdentity.readOnly || !coreIdentity.readOnly || !vaultIdentity.readOnly {
		return CutoverAcceptanceReport{}, errors.New("cutover acceptance requires read-only source, Core, and Vault connections")
	}
	if sourceIdentity.fingerprint == coreIdentity.fingerprint || sourceIdentity.fingerprint == vaultIdentity.fingerprint ||
		coreIdentity.fingerprint == vaultIdentity.fingerprint {
		return CutoverAcceptanceReport{}, errors.New("cutover acceptance databases do not have distinct identities")
	}

	users, torrentsCount, _, err := sourceInventoryCounts(ctx, source)
	if err != nil {
		return CutoverAcceptanceReport{}, err
	}
	if err := bindAcceptanceToPreflight(
		config, startedAt, sourceIdentity, coreIdentity, vaultIdentity, users, torrentsCount,
	); err != nil {
		return CutoverAcceptanceReport{}, err
	}
	status, err := InspectMigrationStatus(ctx, core, config.Inventory)
	if err != nil {
		return CutoverAcceptanceReport{}, err
	}
	if status.State != "reconciled" || !status.CheckpointsComplete() {
		return CutoverAcceptanceReport{}, errors.New("legacy migration is not reconciled with complete checkpoints")
	}
	if status.PendingWorkgroupBenefits != 0 {
		return CutoverAcceptanceReport{}, fmt.Errorf(
			"legacy workgroup benefits are not fully delivered: pending=%d",
			status.PendingWorkgroupBenefits,
		)
	}
	purchases, err := reconcileLegacyPurchases(ctx, source, core, config.Inventory.RunID, status.ImportedTorrents)
	if err != nil {
		return CutoverAcceptanceReport{}, err
	}
	var reconciledAt time.Time
	if err := core.QueryRow(ctx, `
SELECT completed_at
FROM migration.runs
WHERE id = $1 AND state = 'reconciled'`, config.Inventory.RunID).Scan(&reconciledAt); err != nil {
		return CutoverAcceptanceReport{}, fmt.Errorf("read reconciled cutover time: %w", err)
	}

	inventory, err := InspectInventory(ctx, source, core, config.Inventory)
	if err != nil {
		return CutoverAcceptanceReport{}, err
	}
	coreTarget, allowedCredentials, err := inspectCutoverCoreTarget(ctx, core, config.Inventory.RunID)
	if err != nil {
		return CutoverAcceptanceReport{}, err
	}
	if coreTarget.MigrationRuns != 1 || coreTarget.CoreUsers != status.ExpectedUsers ||
		coreTarget.Torrents != status.ImportedTorrents {
		return CutoverAcceptanceReport{}, errors.New("cutover target contains counts outside the reconciled run")
	}
	vaultCredentials, err := inspectCutoverVaultTarget(ctx, vault, allowedCredentials)
	if err != nil {
		return CutoverAcceptanceReport{}, err
	}
	credentialRefs, err := reconcileCoreUsers(ctx, core, status.ExpectedUsers)
	if err != nil {
		return CutoverAcceptanceReport{}, err
	}
	if _, err := reconcileVaultUsers(ctx, vault, credentialRefs); err != nil {
		return CutoverAcceptanceReport{}, err
	}
	attendanceCounts, err := reconcileAcceptedAttendance(
		ctx, source, core, config.Inventory.RunID, status.ExpectedUsers,
	)
	if err != nil {
		return CutoverAcceptanceReport{}, err
	}

	sourceCounts, err := inspectAcceptedSourceCounts(ctx, source, core, config.Inventory.RunID)
	if err != nil {
		return CutoverAcceptanceReport{}, err
	}
	if sourceCounts.Groups != inventory.Groups || sourceCounts.Torrents != status.ImportedTorrents {
		return CutoverAcceptanceReport{}, errors.New("accepted source projection does not match the reconciled inventory")
	}
	targetCounts, err := inspectAcceptedTargetCounts(ctx, core, config.Inventory.RunID, config.StorageBackendID)
	if err != nil {
		return CutoverAcceptanceReport{}, err
	}
	if err := compareAcceptedCounts(sourceCounts, targetCounts); err != nil {
		return CutoverAcceptanceReport{}, err
	}

	verifiedObjects, verifiedBytes, err := verifyAcceptedStoredObjects(
		ctx, core, store, config.Inventory.RunID, status.ImportedTorrents,
		progressEvery, progress,
	)
	if err != nil {
		return CutoverAcceptanceReport{}, err
	}
	verifiedImages, err := legacymedia.VerifyStoredObjects(
		ctx, core, store, config.Inventory.RunID,
		func(processed, expected int64) {
			reportCutoverAcceptanceProgress(
				progress, "image_objects", processed, expected, progressEvery,
			)
		},
	)
	if err != nil {
		return CutoverAcceptanceReport{}, err
	}
	if config.RefreshTrackerSnapshots != nil {
		if err := config.RefreshTrackerSnapshots(ctx); err != nil {
			return CutoverAcceptanceReport{}, fmt.Errorf("refresh Tracker snapshots at acceptance barrier: %w", err)
		}
	}
	checkedAt := config.Now().UTC().Truncate(time.Microsecond)
	if checkedAt.Before(startedAt) {
		return CutoverAcceptanceReport{}, errors.New("cutover acceptance clock moved backwards")
	}
	controlSnapshot, err := verifyAcceptedTrackerSnapshot(
		ctx, core, config, targetCounts, reconciledAt, checkedAt,
	)
	if err != nil {
		return CutoverAcceptanceReport{}, err
	}
	subjectSnapshot, err := verifyAcceptedSubjectSnapshot(ctx, core, config, reconciledAt, checkedAt)
	if err != nil {
		return CutoverAcceptanceReport{}, err
	}
	completedAt := config.Now().UTC().Truncate(time.Microsecond)
	if completedAt.Before(checkedAt) {
		return CutoverAcceptanceReport{}, errors.New("cutover acceptance clock moved backwards")
	}
	if err := requireAcceptedSnapshotTime(
		controlSnapshot.Snapshot.GeneratedAt, reconciledAt, completedAt,
		config.TrackerSnapshotMaxAge, config.TrackerMaxFutureSkew,
	); err != nil {
		return CutoverAcceptanceReport{}, err
	}
	if err := requireAcceptedSnapshotTime(
		subjectSnapshot.Snapshot.GeneratedAt, reconciledAt, completedAt,
		config.TrackerSubjectMaxAge, config.TrackerMaxFutureSkew,
	); err != nil {
		return CutoverAcceptanceReport{}, err
	}

	return CutoverAcceptanceReport{
		Schema: CutoverAcceptanceSchema, CheckedAt: completedAt, RunID: config.Inventory.RunID,
		RunState: status.State, ReconciledAt: reconciledAt.UTC(),
		PreflightManifestSHA256: hex.EncodeToString(config.PreflightManifestSHA256[:]),
		DatabaseDumpSHA256:      hex.EncodeToString(config.Inventory.SnapshotSHA256[:]),
		TorrentArchiveSHA256:    hex.EncodeToString(config.TorrentArchiveSHA256[:]),
		ImageArchiveSHA256:      hex.EncodeToString(config.ImageArchiveSHA256[:]),
		StorageBackendID:        config.StorageBackendID, Users: status.ExpectedUsers,
		VaultCredentials: vaultCredentials, Torrents: status.ImportedTorrents,
		AttendanceOpenings:         attendanceCounts.Openings,
		AttendanceStatsUsers:       attendanceCounts.StatsUsers,
		AttendanceSourceRecords:    attendanceCounts.SourceRecords,
		AttendanceTotalDays:        attendanceCounts.TotalDays,
		AttendanceRetroactiveCards: attendanceCounts.RetroactiveCards,
		ExcludedTorrents:           status.ExcludedTorrents, TorrentFiles: sourceCounts.Files,
		TorrentFacetValues: sourceCounts.FacetValues, TorrentExternalIdentifiers: sourceCounts.ExternalIdentifiers,
		ResourceGroups: sourceCounts.Groups, ResourceGroupExternalIDs: sourceCounts.GroupExternalIdentifiers,
		PublishedTorrents: sourceCounts.Published, VerifiedStoredObjects: verifiedObjects,
		VerifiedStoredObjectBytes:      verifiedBytes,
		TorrentImages:                  verifiedImages.MappedImages,
		VerifiedImageObjects:           verifiedImages.UniqueObjects,
		VerifiedImageObjectBytes:       verifiedImages.VerifiedBytes,
		TorrentPurchaseRows:            purchases.SourceRows,
		TorrentPurchaseRights:          purchases.Entitlements,
		TorrentPurchaseEvidenceOnly:    purchases.EvidenceOnly,
		TrackerControlSequence:         controlSnapshot.Snapshot.ControlSequence,
		TrackerTorrentCount:            len(controlSnapshot.Snapshot.Torrents),
		TrackerSnapshotGeneratedAt:     controlSnapshot.Snapshot.GeneratedAt,
		TrackerSnapshotSHA256:          hex.EncodeToString(controlSnapshot.ArtifactSHA256[:]),
		TrackerSubjectSequence:         subjectSnapshot.Snapshot.ControlSequence,
		TrackerSubjectCount:            len(subjectSnapshot.Snapshot.Subjects),
		TrackerSubjectGeneratedAt:      subjectSnapshot.Snapshot.GeneratedAt,
		TrackerSubjectSnapshotSHA256:   hex.EncodeToString(subjectSnapshot.ArtifactSHA256[:]),
		CoreRuntimeDefaultsReady:       true,
		LegacyMemberAuthorizationReady: true,
		ReadyToActivate:                true,
	}, nil
}

func bindAcceptanceToPreflight(
	config CutoverAcceptanceConfig,
	checkedAt time.Time,
	source, core, vault cutoverDatabaseIdentity,
	users, torrentsCount int64,
) error {
	preflight := config.Preflight
	exclusionSHA := ""
	if config.Exclusions.Len() > 0 {
		digest := config.Exclusions.ContentSHA256()
		exclusionSHA = hex.EncodeToString(digest[:])
	}
	if preflight.Schema != CutoverPreflightSchema || !preflight.Ready ||
		preflight.RunID != config.Inventory.RunID || preflight.MappingVersion != config.Inventory.MappingVersion ||
		!preflight.OccurredAt.Equal(config.OccurredAt) ||
		preflight.DatabaseDumpSHA256 != hex.EncodeToString(config.Inventory.SnapshotSHA256[:]) ||
		preflight.DatabaseDumpBytes != config.DatabaseDumpBytes ||
		preflight.TorrentArchiveSHA256 != hex.EncodeToString(config.TorrentArchiveSHA256[:]) ||
		preflight.TorrentArchiveBytes != config.TorrentArchiveBytes ||
		preflight.TorrentArchiveObjects != config.TorrentArchiveObjects ||
		preflight.ImageArchiveSHA256 != hex.EncodeToString(config.ImageArchiveSHA256[:]) ||
		preflight.ImageArchiveBytes != config.ImageArchiveBytes ||
		preflight.ImageArchiveObjects != config.ImageArchiveObjects ||
		preflight.StorageBackendID != config.StorageBackendID || preflight.StorageDriver != config.StorageDriver ||
		preflight.StorageConfigSHA256 != hex.EncodeToString(config.StorageConfigSHA256[:]) ||
		preflight.ExcludedTorrents != config.Exclusions.Len() ||
		preflight.ExclusionManifestSHA256 != exclusionSHA || preflight.ExpectedUsers != users ||
		preflight.ExpectedTorrents != torrentsCount || preflight.SourceDatabase.IdentitySHA256 != hex.EncodeToString(source.fingerprint[:]) ||
		preflight.CoreDatabase.IdentitySHA256 != hex.EncodeToString(core.fingerprint[:]) ||
		preflight.VaultDatabase.IdentitySHA256 != hex.EncodeToString(vault.fingerprint[:]) ||
		preflight.CheckedAt.After(checkedAt) {
		return errors.New("cutover acceptance does not match the preflight manifest")
	}
	return nil
}

func inspectAcceptedSourceCounts(
	ctx context.Context,
	source, core *pgxpool.Pool,
	runID uuid.UUID,
) (acceptedSourceCounts, error) {
	rows, err := core.Query(ctx, `
SELECT mapping.legacy_torrent_id
FROM migration.torrent_id_map AS mapping
JOIN migration.source_rows AS checkpoint
  ON checkpoint.run_id = mapping.first_run_id
 AND checkpoint.entity_kind = 'torrent'
 AND checkpoint.legacy_id = mapping.legacy_torrent_id
 AND checkpoint.state = 'imported'
WHERE mapping.first_run_id = $1
ORDER BY mapping.legacy_torrent_id`, runID)
	if err != nil {
		return acceptedSourceCounts{}, fmt.Errorf("read accepted legacy torrent IDs: %w", err)
	}
	accepted := make(map[int64]struct{})
	for rows.Next() {
		var legacyID int64
		if err := rows.Scan(&legacyID); err != nil {
			rows.Close()
			return acceptedSourceCounts{}, fmt.Errorf("scan accepted legacy torrent ID: %w", err)
		}
		accepted[legacyID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return acceptedSourceCounts{}, fmt.Errorf("read accepted legacy torrent IDs: %w", err)
	}
	rows.Close()

	vocabulary, err := loadLegacyVocabulary(ctx, source)
	if err != nil {
		return acceptedSourceCounts{}, err
	}
	torrentRows, err := querySourceTorrents(ctx, source)
	if err != nil {
		return acceptedSourceCounts{}, err
	}
	defer torrentRows.Close()
	files, err := newSourceFileCursor(ctx, source)
	if err != nil {
		return acceptedSourceCounts{}, err
	}
	defer files.Close()
	result := acceptedSourceCounts{}
	for torrentRows.Next() {
		sourceTorrent, err := scanSourceTorrent(torrentRows)
		if err != nil {
			return acceptedSourceCounts{}, fmt.Errorf("scan accepted source torrent: %w", err)
		}
		manifest, err := files.ManifestFor(sourceTorrent.LegacyID)
		if err != nil {
			return acceptedSourceCounts{}, err
		}
		if _, exists := accepted[sourceTorrent.LegacyID]; !exists {
			continue
		}
		facets, attributes, err := mapLegacyAttributes(
			sourceTorrent.SourceCategory, sourceTorrent.Attributes, vocabulary,
		)
		if err != nil {
			return acceptedSourceCounts{}, sourceTorrentError(sourceTorrent.LegacyID, taxonomyErrorCode(err))
		}
		externalIDs, _, err := extractTorrentExternalIDs(attributes)
		if err != nil {
			return acceptedSourceCounts{}, sourceTorrentError(sourceTorrent.LegacyID, taxonomyErrorCode(err))
		}
		state, err := sourceTorrent.state()
		if err != nil {
			return acceptedSourceCounts{}, sourceTorrentError(sourceTorrent.LegacyID, "unknown_state")
		}
		result.Torrents++
		result.Files += int64(len(manifest.Files))
		result.FacetValues += int64(len(facets))
		result.ExternalIdentifiers += int64(len(externalIDs))
		if state == torrents.StatePublished {
			result.Published++
		}
	}
	if err := torrentRows.Err(); err != nil {
		return acceptedSourceCounts{}, fmt.Errorf("read accepted source torrents: %w", err)
	}
	if err := files.Finish(); err != nil {
		return acceptedSourceCounts{}, err
	}
	if result.Torrents != int64(len(accepted)) {
		return acceptedSourceCounts{}, errors.New("accepted source torrent mapping count changed")
	}
	groups, _, _, err := collectSourceGroups(ctx, source)
	if err != nil {
		return acceptedSourceCounts{}, err
	}
	result.Groups = int64(len(groups))
	for _, group := range groups {
		result.GroupExternalIdentifiers += int64(len(group.ExternalID))
	}
	return result, nil
}

func inspectAcceptedTargetCounts(
	ctx context.Context,
	core *pgxpool.Pool,
	runID uuid.UUID,
	backendID string,
) (acceptedTargetCounts, error) {
	var result acceptedTargetCounts
	err := core.QueryRow(ctx, `
SELECT
    count(DISTINCT torrent.id)::bigint,
    count(DISTINCT object.id)::bigint,
    count(DISTINCT location.id)::bigint,
    (SELECT count(*)::bigint FROM torrents.torrent_files AS child
     JOIN migration.torrent_id_map AS mapped ON mapped.torrent_id = child.torrent_id
     WHERE mapped.first_run_id = $1),
    (SELECT count(*)::bigint FROM torrents.torrent_facet_values AS child
     JOIN migration.torrent_id_map AS mapped ON mapped.torrent_id = child.torrent_id
     WHERE mapped.first_run_id = $1),
    (SELECT count(*)::bigint FROM torrents.torrent_external_identifiers AS child
     JOIN migration.torrent_id_map AS mapped ON mapped.torrent_id = child.torrent_id
     WHERE mapped.first_run_id = $1),
    (SELECT count(*)::bigint FROM torrents.resource_groups),
    (SELECT count(*)::bigint FROM migration.torrent_group_id_map WHERE first_run_id = $1),
    (SELECT count(*)::bigint FROM torrents.resource_group_external_identifiers),
    count(DISTINCT torrent.id) FILTER (WHERE torrent.state = 'published')::bigint,
    (SELECT count(*)::bigint FROM catalog.torrents AS catalog
     JOIN migration.torrent_id_map AS mapped ON mapped.torrent_id = catalog.id
     WHERE mapped.first_run_id = $1),
    (SELECT count(*)::bigint FROM tracker_control.outbox AS event
     JOIN migration.torrent_id_map AS mapped ON mapped.torrent_id = event.aggregate_id
     WHERE mapped.first_run_id = $1),
    (SELECT count(*)::bigint FROM tracker_control.outbox WHERE projected_at IS NULL),
    (SELECT count(*)::bigint FROM tracker_control.torrent_allowlist_projection WHERE enabled),
    (SELECT last_sequence FROM tracker_control.projection_state WHERE singleton),
    (SELECT COALESCE(max(sequence), 0)::bigint FROM tracker_control.outbox),
    (SELECT count(*)::bigint FROM catalog.site_profile WHERE singleton),
    (SELECT count(*)::bigint FROM identity.registration_policy WHERE singleton)
FROM migration.torrent_id_map AS mapping
JOIN torrents.torrents AS torrent ON torrent.id = mapping.torrent_id
JOIN torrents.torrent_objects AS object ON object.id = torrent.object_id
JOIN torrents.torrent_object_locations AS location
  ON location.object_id = object.id
 AND location.backend_id = $2
 AND location.state = 'verified'
 AND location.is_preferred
 AND location.observed_sha256 = object.content_sha256
 AND location.observed_byte_length = object.byte_length
WHERE mapping.first_run_id = $1`, runID, backendID).Scan(
		&result.Torrents, &result.Objects, &result.Locations, &result.Files,
		&result.FacetValues, &result.ExternalIdentifiers, &result.Groups,
		&result.GroupMappings, &result.GroupExternalIdentifiers, &result.Published,
		&result.CatalogRows, &result.OutboxRows, &result.PendingOutbox,
		&result.Allowlisted, &result.ProjectionSequence, &result.OutboxSequence,
		&result.SiteProfiles, &result.RegistrationPolicies,
	)
	if err != nil {
		return acceptedTargetCounts{}, fmt.Errorf("inspect accepted Core target: %w", err)
	}
	return result, nil
}

func compareAcceptedCounts(source acceptedSourceCounts, target acceptedTargetCounts) error {
	if target.Torrents != source.Torrents || target.Objects != source.Torrents || target.Locations != source.Torrents ||
		target.Files != source.Files || target.FacetValues != source.FacetValues ||
		target.ExternalIdentifiers != source.ExternalIdentifiers || target.Groups != source.Groups ||
		target.GroupMappings != source.Groups || target.GroupExternalIdentifiers != source.GroupExternalIdentifiers ||
		target.Published != source.Published || target.CatalogRows != source.Published ||
		target.OutboxRows != source.Published || target.PendingOutbox != 0 ||
		target.Allowlisted != source.Published || target.ProjectionSequence != target.OutboxSequence ||
		target.SiteProfiles != 1 || target.RegistrationPolicies != 1 {
		return fmt.Errorf(
			"accepted Core metadata or Tracker projection does not reconcile: source=%+v target=%+v",
			source, target,
		)
	}
	return nil
}

func verifyAcceptedStoredObjects(
	ctx context.Context,
	core *pgxpool.Pool,
	store torrents.ObjectStore,
	runID uuid.UUID,
	expected int64,
	progressEvery int64,
	progress func(CutoverAcceptanceProgress),
) (int64, int64, error) {
	rows, err := core.Query(ctx, `
SELECT object.content_sha256, object.byte_length, location.object_key,
       COALESCE(location.version_id, '')
FROM migration.torrent_id_map AS mapping
JOIN torrents.torrents AS torrent ON torrent.id = mapping.torrent_id
JOIN torrents.torrent_objects AS object ON object.id = torrent.object_id
JOIN torrents.torrent_object_locations AS location
  ON location.object_id = object.id
 AND location.backend_id = $2
 AND location.state = 'verified'
 AND location.is_preferred
 AND location.observed_sha256 = object.content_sha256
 AND location.observed_byte_length = object.byte_length
WHERE mapping.first_run_id = $1
ORDER BY mapping.legacy_torrent_id`, runID, string(store.BackendID()))
	if err != nil {
		return 0, 0, fmt.Errorf("query accepted stored objects: %w", err)
	}
	defer rows.Close()
	var processed, totalBytes int64
	for rows.Next() {
		var rawSHA []byte
		var byteLength int64
		var keyValue, versionID string
		if err := rows.Scan(&rawSHA, &byteLength, &keyValue, &versionID); err != nil {
			return 0, 0, fmt.Errorf("scan accepted stored object: %w", err)
		}
		if len(rawSHA) != sha256.Size {
			return 0, 0, errors.New("accepted stored object has an invalid SHA-256")
		}
		var objectSHA torrents.ObjectSHA256
		copy(objectSHA[:], rawSHA)
		key, err := torrents.ParseObjectKey(keyValue)
		if err != nil {
			return 0, 0, errors.New("accepted stored object has an invalid key")
		}
		object, err := store.Open(ctx, key, versionID)
		if err != nil {
			return 0, 0, fmt.Errorf("open accepted stored object %d: %w", processed+1, err)
		}
		if object.Body == nil {
			return 0, 0, fmt.Errorf("open accepted stored object %d: storage returned an empty reader", processed+1)
		}
		descriptor := torrents.StoredObjectDescriptor{SHA256: objectSHA, ByteLength: byteLength}
		_, verifyErr := torrents.VerifyStoredObject(object, descriptor)
		closeErr := object.Body.Close()
		if verifyErr != nil {
			return 0, 0, fmt.Errorf("verify accepted stored object %d: %w", processed+1, verifyErr)
		}
		if closeErr != nil {
			return 0, 0, fmt.Errorf("close accepted stored object %d: %w", processed+1, closeErr)
		}
		processed++
		totalBytes += byteLength
		reportCutoverAcceptanceProgress(
			progress, "stored_objects", processed, expected, progressEvery,
		)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("read accepted stored objects: %w", err)
	}
	if processed != expected {
		return 0, 0, errors.New("accepted stored object count does not reconcile")
	}
	return processed, totalBytes, nil
}

func reportCutoverAcceptanceProgress(
	progress func(CutoverAcceptanceProgress),
	phase string,
	processed int64,
	expected int64,
	every int64,
) {
	if progress == nil || every < 1 || processed < 1 {
		return
	}
	if processed%every != 0 && processed != expected {
		return
	}
	progress(CutoverAcceptanceProgress{
		Phase: phase, Processed: processed, Expected: expected,
	})
}

func verifyAcceptedTrackerSnapshot(
	ctx context.Context,
	core *pgxpool.Pool,
	config CutoverAcceptanceConfig,
	target acceptedTargetCounts,
	reconciledAt time.Time,
	checkedAt time.Time,
) (trackercontrolv1.VerifiedSnapshot, error) {
	raw, err := readCutoverArtifact(config.TrackerSnapshotPath, signedsnapshotv1.MaxArtifactBytes)
	if err != nil {
		return trackercontrolv1.VerifiedSnapshot{}, fmt.Errorf("read Tracker control snapshot: %w", err)
	}
	verified, err := trackercontrolv1.Verify(raw, config.TrackerTrustedKeys)
	if err != nil {
		return trackercontrolv1.VerifiedSnapshot{}, fmt.Errorf("verify Tracker control snapshot: %w", err)
	}
	if err := requireAcceptedSnapshotTime(
		verified.Snapshot.GeneratedAt, reconciledAt, checkedAt,
		config.TrackerSnapshotMaxAge, config.TrackerMaxFutureSkew,
	); err != nil {
		return trackercontrolv1.VerifiedSnapshot{}, err
	}
	if verified.Snapshot.ControlSequence != target.ProjectionSequence ||
		int64(len(verified.Snapshot.Torrents)) != target.Allowlisted {
		return trackercontrolv1.VerifiedSnapshot{}, errors.New("Tracker control snapshot does not match the Core projection")
	}
	rows, err := core.Query(ctx, `
SELECT torrent_id, info_hash_v1, total_size_bytes,
       torrent_version, control_sequence
FROM tracker_control.torrent_allowlist_projection
WHERE enabled
ORDER BY info_hash_v1`)
	if err != nil {
		return trackercontrolv1.VerifiedSnapshot{}, fmt.Errorf("query Tracker allowlist acceptance: %w", err)
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		if index >= len(verified.Snapshot.Torrents) {
			return trackercontrolv1.VerifiedSnapshot{}, errors.New("Tracker control snapshot is missing an allowlist row")
		}
		var id, size, version, sequence int64
		var infoHash []byte
		if err := rows.Scan(&id, &infoHash, &size, &version, &sequence); err != nil {
			return trackercontrolv1.VerifiedSnapshot{}, fmt.Errorf("scan Tracker allowlist acceptance: %w", err)
		}
		entry := verified.Snapshot.Torrents[index]
		if entry.TorrentID != id || entry.InfoHashV1 != hex.EncodeToString(infoHash) || entry.TotalSizeBytes != size ||
			entry.TorrentVersion != version || entry.ControlSequence != sequence {
			return trackercontrolv1.VerifiedSnapshot{}, errors.New("Tracker control snapshot entry does not match Core")
		}
		index++
	}
	if err := rows.Err(); err != nil {
		return trackercontrolv1.VerifiedSnapshot{}, fmt.Errorf("read Tracker allowlist acceptance: %w", err)
	}
	if index != len(verified.Snapshot.Torrents) {
		return trackercontrolv1.VerifiedSnapshot{}, errors.New("Tracker control snapshot contains an unexpected row")
	}
	return verified, nil
}

func verifyAcceptedSubjectSnapshot(
	ctx context.Context,
	core *pgxpool.Pool,
	config CutoverAcceptanceConfig,
	reconciledAt time.Time,
	checkedAt time.Time,
) (trackersubjectcontrolv1.VerifiedSnapshot, error) {
	raw, err := readCutoverArtifact(config.TrackerSubjectSnapshotPath, signedsnapshotv1.MaxArtifactBytes)
	if err != nil {
		return trackersubjectcontrolv1.VerifiedSnapshot{}, fmt.Errorf("read Tracker subject snapshot: %w", err)
	}
	verified, err := trackersubjectcontrolv1.Verify(raw, config.TrackerTrustedKeys)
	if err != nil {
		return trackersubjectcontrolv1.VerifiedSnapshot{}, fmt.Errorf("verify Tracker subject snapshot: %w", err)
	}
	if err := requireAcceptedSnapshotTime(
		verified.Snapshot.GeneratedAt, reconciledAt, checkedAt,
		config.TrackerSubjectMaxAge, config.TrackerMaxFutureSkew,
	); err != nil {
		return trackersubjectcontrolv1.VerifiedSnapshot{}, err
	}
	var sequence int64
	var generatedAt time.Time
	if err := core.QueryRow(ctx, `
SELECT last_sequence, updated_at
FROM tracker_control.subject_snapshot_state
WHERE singleton`).Scan(&sequence, &generatedAt); err != nil {
		return trackersubjectcontrolv1.VerifiedSnapshot{}, fmt.Errorf("read Tracker subject snapshot state: %w", err)
	}
	if verified.Snapshot.ControlSequence != sequence || !verified.Snapshot.GeneratedAt.Equal(generatedAt) {
		return trackersubjectcontrolv1.VerifiedSnapshot{}, errors.New("Tracker subject snapshot does not match the Core revision")
	}
	rows, err := core.Query(ctx, `
SELECT
    passkey.user_id,
    passkey.lookup_hmac,
    passkey.vault_version,
    identity.is_download_restricted(users.id)
FROM identity.tracker_passkey_hmac AS passkey
JOIN identity.users AS users ON users.id = passkey.user_id
WHERE users.status = 'active'
  AND NOT EXISTS (
      SELECT 1 FROM identity.account_restrictions AS restriction
      WHERE restriction.user_id = users.id
        AND restriction.kind = 'account_access'
        AND restriction.revoked_at IS NULL
        AND restriction.starts_at <= $1
        AND restriction.expires_at > $1
  )
ORDER BY passkey.lookup_hmac`, verified.Snapshot.GeneratedAt)
	if err != nil {
		return trackersubjectcontrolv1.VerifiedSnapshot{}, fmt.Errorf("query Tracker subject acceptance: %w", err)
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		if index >= len(verified.Snapshot.Subjects) {
			return trackersubjectcontrolv1.VerifiedSnapshot{}, errors.New("Tracker subject snapshot is missing an eligible user")
		}
		var userID uuid.UUID
		var lookup []byte
		var version int64
		var downloadRestricted bool
		if err := rows.Scan(&userID, &lookup, &version, &downloadRestricted); err != nil {
			return trackersubjectcontrolv1.VerifiedSnapshot{}, fmt.Errorf("scan Tracker subject acceptance: %w", err)
		}
		entry := verified.Snapshot.Subjects[index]
		if entry.UserID != userID.String() || entry.LookupHMAC != hex.EncodeToString(lookup) ||
			entry.CredentialVersion != version || entry.DownloadRestricted != downloadRestricted {
			return trackersubjectcontrolv1.VerifiedSnapshot{}, errors.New("Tracker subject snapshot entry does not match Core")
		}
		index++
	}
	if err := rows.Err(); err != nil {
		return trackersubjectcontrolv1.VerifiedSnapshot{}, fmt.Errorf("read Tracker subject acceptance: %w", err)
	}
	if index != len(verified.Snapshot.Subjects) {
		return trackersubjectcontrolv1.VerifiedSnapshot{}, errors.New("Tracker subject snapshot contains an unexpected user")
	}
	return verified, nil
}

func requireAcceptedSnapshotTime(
	generatedAt, reconciledAt, checkedAt time.Time,
	maxAge, maxFutureSkew time.Duration,
) error {
	if generatedAt.Before(reconciledAt) || generatedAt.After(checkedAt.Add(maxFutureSkew)) ||
		checkedAt.Sub(generatedAt) > maxAge {
		return errors.New("Tracker snapshot is outside the post-reconciliation freshness window")
	}
	return nil
}

func readCutoverArtifact(path string, maximum int64) ([]byte, error) {
	if path == "" || !filepath.IsAbs(path) || maximum < 1 {
		return nil, errors.New("cutover artifact path is invalid")
	}
	cleaned := filepath.Clean(path)
	linkInfo, err := os.Lstat(cleaned)
	if err != nil || !linkInfo.Mode().IsRegular() || linkInfo.Mode()&os.ModeSymlink != 0 ||
		linkInfo.Mode().Perm()&0o077 != 0 || linkInfo.Size() < 2 || linkInfo.Size() > maximum {
		return nil, errors.New("cutover artifact is not a protected regular file")
	}
	file, err := os.Open(cleaned)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	fileInfo, err := file.Stat()
	if err != nil || !os.SameFile(linkInfo, fileInfo) {
		return nil, errors.New("cutover artifact changed before read")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(raw)) != fileInfo.Size() || int64(len(raw)) > maximum {
		return nil, errors.New("cutover artifact changed during read")
	}
	return raw, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing JSON")
	}
	return nil
}

func WriteCutoverAcceptanceManifest(path string, report CutoverAcceptanceReport) ([sha256.Size]byte, error) {
	if report.Schema != CutoverAcceptanceSchema || !report.CoreRuntimeDefaultsReady ||
		!report.LegacyMemberAuthorizationReady || !report.ReadyToActivate ||
		report.AttendanceOpenings != report.Users ||
		report.TorrentPurchaseRows != report.TorrentPurchaseRights+report.TorrentPurchaseEvidenceOnly {
		return [sha256.Size]byte{}, errors.New("cutover acceptance manifest is invalid")
	}
	return writeExclusiveCutoverJSON(path, report)
}

func reconcileAcceptedAttendance(
	ctx context.Context,
	source, core *pgxpool.Pool,
	runID uuid.UUID,
	expectedUsers int64,
) (acceptedAttendanceCounts, error) {
	var sourceUsers, statsUsers, sourceRecords, totalDays, retroactiveCards int64
	if err := source.QueryRow(ctx, `
SELECT
    count(*)::bigint,
    count(attendance.user_id)::bigint,
    COALESCE(sum(attendance.total_days), 0)::bigint,
    COALESCE(sum(attendance.retroactive_cards), 0)::bigint,
    (SELECT count(*)::bigint
       FROM attendance_records AS record
       JOIN users AS mapped_user ON mapped_user.id = record.user_id)
FROM users AS user_account
LEFT JOIN user_attendance_stats AS attendance
  ON attendance.user_id = user_account.id`).Scan(
		&sourceUsers, &statsUsers, &totalDays, &retroactiveCards, &sourceRecords,
	); err != nil {
		return acceptedAttendanceCounts{}, fmt.Errorf("read accepted PtYes attendance totals: %w", err)
	}

	var target acceptedAttendanceCounts
	if err := core.QueryRow(ctx, `
SELECT
    count(*)::bigint,
    count(*) FILTER (WHERE source_stats_present)::bigint,
    COALESCE(sum(source_record_days), 0)::bigint,
    COALESCE(sum(source_total_days), 0)::bigint,
    COALESCE(sum(source_retroactive_cards), 0)::bigint
FROM migration.user_attendance_openings
WHERE source_system = 'ptyes'
  AND first_run_id = $1`, runID).Scan(
		&target.Openings,
		&target.StatsUsers,
		&target.SourceRecords,
		&target.TotalDays,
		&target.RetroactiveCards,
	); err != nil {
		return acceptedAttendanceCounts{}, fmt.Errorf("read accepted PeerGo attendance openings: %w", err)
	}
	if sourceUsers != expectedUsers || target.Openings != expectedUsers ||
		target.StatsUsers != statsUsers || target.SourceRecords != sourceRecords ||
		target.TotalDays != totalDays || target.RetroactiveCards != retroactiveCards {
		return acceptedAttendanceCounts{}, fmt.Errorf(
			"legacy attendance totals do not reconcile: source users/stats/records/days/cards=%d/%d/%d/%d/%d target=%d/%d/%d/%d/%d",
			sourceUsers, statsUsers, sourceRecords, totalDays, retroactiveCards,
			target.Openings, target.StatsUsers, target.SourceRecords,
			target.TotalDays, target.RetroactiveCards,
		)
	}
	return target, nil
}

func writeExclusiveCutoverJSON(path string, value any) ([sha256.Size]byte, error) {
	var zero [sha256.Size]byte
	if path == "" || !filepath.IsAbs(path) {
		return zero, errors.New("cutover manifest output path must be absolute")
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return zero, fmt.Errorf("encode cutover manifest: %w", err)
	}
	raw = append(raw, '\n')
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return zero, fmt.Errorf("create cutover manifest: %w", err)
	}
	complete := false
	defer func() {
		_ = file.Close()
		if !complete {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(raw); err != nil {
		return zero, fmt.Errorf("write cutover manifest: %w", err)
	}
	if err := file.Sync(); err != nil {
		return zero, fmt.Errorf("sync cutover manifest: %w", err)
	}
	if err := file.Close(); err != nil {
		return zero, fmt.Errorf("close cutover manifest: %w", err)
	}
	complete = true
	return sha256.Sum256(raw), nil
}
