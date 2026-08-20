package legacytorrents

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

const CutoverPreflightSchema = "peergo.legacy-cutover-preflight.v2"

type CutoverPreflightConfig struct {
	Inventory             InventoryConfig
	OccurredAt            time.Time
	CheckedAt             time.Time
	DatabaseDumpBytes     int64
	TorrentArchiveSHA256  [sha256.Size]byte
	TorrentArchiveBytes   int64
	TorrentArchiveObjects int64
	ImageArchiveSHA256    [sha256.Size]byte
	ImageArchiveBytes     int64
	ImageArchiveObjects   int64
	StorageBackendID      string
	StorageDriver         string
	StorageConfigSHA256   [sha256.Size]byte
	Exclusions            TorrentExclusionManifest
}

type CutoverDatabaseReport struct {
	IdentitySHA256      string `json:"identity_sha256"`
	PostgreSQLVersion   int64  `json:"postgresql_version_num"`
	MigrationVersion    int64  `json:"migration_version,omitempty"`
	TransactionReadOnly bool   `json:"transaction_read_only"`
}

type CutoverTargetReport struct {
	CoreUsers        int64 `json:"core_users"`
	VaultCredentials int64 `json:"vault_credentials"`
	Torrents         int64 `json:"torrents"`
	UserMappings     int64 `json:"user_mappings"`
	TorrentMappings  int64 `json:"torrent_mappings"`
	MigrationRuns    int64 `json:"migration_runs"`
}

type CutoverPreflightReport struct {
	Schema                  string                `json:"schema"`
	CheckedAt               time.Time             `json:"checked_at"`
	RunID                   uuid.UUID             `json:"run_id"`
	RunMode                 string                `json:"run_mode"`
	RunState                string                `json:"run_state"`
	SourceSystem            string                `json:"source_system"`
	MappingVersion          string                `json:"mapping_version"`
	OccurredAt              time.Time             `json:"occurred_at"`
	DatabaseDumpSHA256      string                `json:"database_dump_sha256"`
	DatabaseDumpBytes       int64                 `json:"database_dump_bytes"`
	TorrentArchiveSHA256    string                `json:"torrent_archive_sha256"`
	TorrentArchiveBytes     int64                 `json:"torrent_archive_bytes"`
	TorrentArchiveObjects   int64                 `json:"torrent_archive_objects"`
	ImageArchiveSHA256      string                `json:"image_archive_sha256"`
	ImageArchiveBytes       int64                 `json:"image_archive_bytes"`
	ImageArchiveObjects     int64                 `json:"image_archive_objects"`
	ExpectedUsers           int64                 `json:"expected_users"`
	ExpectedTorrents        int64                 `json:"expected_torrents"`
	ExcludedTorrents        int                   `json:"excluded_torrents"`
	ExclusionManifestSHA256 string                `json:"exclusion_manifest_sha256,omitempty"`
	StorageBackendID        string                `json:"storage_backend_id"`
	StorageDriver           string                `json:"storage_driver"`
	StorageConfigSHA256     string                `json:"storage_config_sha256"`
	SourceDatabase          CutoverDatabaseReport `json:"source_database"`
	CoreDatabase            CutoverDatabaseReport `json:"core_database"`
	VaultDatabase           CutoverDatabaseReport `json:"vault_database"`
	Target                  CutoverTargetReport   `json:"target"`
	Ready                   bool                  `json:"ready"`
}

type cutoverDatabaseIdentity struct {
	fingerprint [sha256.Size]byte
	version     int64
	migration   int64
	readOnly    bool
}

type cutoverRun struct {
	id               uuid.UUID
	snapshot         []byte
	mappingVersion   string
	state            string
	expectedUsers    int64
	expectedTorrents int64
	createdAt        time.Time
}

// InspectCutoverPreflight is a fail-closed, read-only gate for a new or
// interrupted finite cutover. It compares actual PostgreSQL identities rather
// than URL strings and rejects target data that cannot be attributed to this
// run. The returned report deliberately contains no database URL, role,
// credential, source identifier, host path, bucket, or endpoint.
func InspectCutoverPreflight(
	ctx context.Context,
	source *pgxpool.Pool,
	core *pgxpool.Pool,
	vault *pgxpool.Pool,
	config CutoverPreflightConfig,
) (CutoverPreflightReport, error) {
	config.OccurredAt = config.OccurredAt.UTC().Truncate(time.Microsecond)
	config.CheckedAt = config.CheckedAt.UTC().Truncate(time.Microsecond)
	if source == nil || core == nil || vault == nil || config.Inventory.RunID == uuid.Nil ||
		config.Inventory.SnapshotSHA256 == ([sha256.Size]byte{}) ||
		strings.TrimSpace(config.Inventory.MappingVersion) == "" || config.OccurredAt.IsZero() ||
		config.CheckedAt.IsZero() || config.DatabaseDumpBytes < 1 ||
		config.TorrentArchiveSHA256 == ([sha256.Size]byte{}) || config.TorrentArchiveBytes < 1 ||
		config.TorrentArchiveObjects < 1 || config.ImageArchiveSHA256 == ([sha256.Size]byte{}) ||
		config.ImageArchiveBytes < 1 || config.ImageArchiveObjects < 1 ||
		strings.TrimSpace(config.StorageBackendID) == "" ||
		(config.StorageDriver != "filesystem" && config.StorageDriver != "s3") ||
		config.StorageConfigSHA256 == ([sha256.Size]byte{}) {
		return CutoverPreflightReport{}, errors.New("legacy cutover preflight configuration is invalid")
	}
	if err := platformpostgres.RequireCurrentMigration(ctx, core); err != nil {
		return CutoverPreflightReport{}, err
	}
	if err := requireLegacyVaultVersion(ctx, vault); err != nil {
		return CutoverPreflightReport{}, err
	}

	sourceIdentity, err := inspectCutoverDatabaseIdentity(ctx, source, 0)
	if err != nil {
		return CutoverPreflightReport{}, fmt.Errorf("inspect PtYes source database: %w", err)
	}
	coreIdentity, err := inspectCutoverDatabaseIdentity(ctx, core, platformpostgres.ExpectedMigrationVersion)
	if err != nil {
		return CutoverPreflightReport{}, fmt.Errorf("inspect Core database: %w", err)
	}
	vaultIdentity, err := inspectCutoverDatabaseIdentity(ctx, vault, requiredLegacyVaultMigrationVersion)
	if err != nil {
		return CutoverPreflightReport{}, fmt.Errorf("inspect Vault database: %w", err)
	}
	if !sourceIdentity.readOnly || !coreIdentity.readOnly || !vaultIdentity.readOnly {
		return CutoverPreflightReport{}, errors.New("legacy cutover preflight requires read-only source, Core, and Vault connections")
	}
	if sourceIdentity.fingerprint == coreIdentity.fingerprint ||
		sourceIdentity.fingerprint == vaultIdentity.fingerprint ||
		coreIdentity.fingerprint == vaultIdentity.fingerprint {
		return CutoverPreflightReport{}, errors.New("legacy cutover source, Core, and Vault resolve to the same database identity")
	}

	users, torrentsCount, _, err := sourceInventoryCounts(ctx, source)
	if err != nil {
		return CutoverPreflightReport{}, err
	}
	if users < 1 || torrentsCount < 1 {
		return CutoverPreflightReport{}, errors.New("PtYes source snapshot is missing users or torrents")
	}
	run, exists, err := inspectCutoverRun(ctx, core, config.Inventory)
	if err != nil {
		return CutoverPreflightReport{}, err
	}
	runMode := "new"
	runState := "not_started"
	if exists {
		runMode = "resume"
		runState = run.state
		if !bytes.Equal(run.snapshot, config.Inventory.SnapshotSHA256[:]) ||
			run.mappingVersion != config.Inventory.MappingVersion || run.expectedUsers != users ||
			run.expectedTorrents != torrentsCount || !run.createdAt.Equal(config.OccurredAt) {
			return CutoverPreflightReport{}, errors.New("existing migration run does not match the requested immutable cutover identity")
		}
		if run.state == "failed" || run.state == "reconciled" {
			return CutoverPreflightReport{}, fmt.Errorf("migration run in terminal state %s cannot be started or resumed", run.state)
		}
	}

	target, allowedCredentialRefs, err := inspectCutoverCoreTarget(ctx, core, config.Inventory.RunID)
	if err != nil {
		return CutoverPreflightReport{}, err
	}
	target.VaultCredentials, err = inspectCutoverVaultTarget(ctx, vault, allowedCredentialRefs)
	if err != nil {
		return CutoverPreflightReport{}, err
	}
	if !exists && (target.CoreUsers != 0 || target.VaultCredentials != 0 || target.Torrents != 0 ||
		target.UserMappings != 0 || target.TorrentMappings != 0 || target.MigrationRuns != 0) {
		return CutoverPreflightReport{}, errors.New("new legacy cutover requires empty identity, torrent, Vault credential, mapping, and migration-run targets")
	}
	if exists && target.MigrationRuns != 1 {
		return CutoverPreflightReport{}, errors.New("resumed legacy cutover target contains another migration run")
	}
	if err := requireCutoverBackendConsistency(ctx, core, config.Inventory.RunID, config.StorageBackendID); err != nil {
		return CutoverPreflightReport{}, err
	}

	report := CutoverPreflightReport{
		Schema: CutoverPreflightSchema, CheckedAt: config.CheckedAt, RunID: config.Inventory.RunID,
		RunMode: runMode, RunState: runState, SourceSystem: "ptyes",
		MappingVersion: config.Inventory.MappingVersion, OccurredAt: config.OccurredAt,
		DatabaseDumpSHA256:   hex.EncodeToString(config.Inventory.SnapshotSHA256[:]),
		DatabaseDumpBytes:    config.DatabaseDumpBytes,
		TorrentArchiveSHA256: hex.EncodeToString(config.TorrentArchiveSHA256[:]),
		TorrentArchiveBytes:  config.TorrentArchiveBytes, TorrentArchiveObjects: config.TorrentArchiveObjects,
		ImageArchiveSHA256: hex.EncodeToString(config.ImageArchiveSHA256[:]),
		ImageArchiveBytes:  config.ImageArchiveBytes, ImageArchiveObjects: config.ImageArchiveObjects,
		ExpectedUsers: users, ExpectedTorrents: torrentsCount, ExcludedTorrents: config.Exclusions.Len(),
		StorageBackendID: config.StorageBackendID, StorageDriver: config.StorageDriver,
		StorageConfigSHA256: hex.EncodeToString(config.StorageConfigSHA256[:]),
		SourceDatabase:      cutoverDatabaseReport(sourceIdentity), CoreDatabase: cutoverDatabaseReport(coreIdentity),
		VaultDatabase: cutoverDatabaseReport(vaultIdentity), Target: target, Ready: true,
	}
	if config.Exclusions.Len() > 0 {
		digest := config.Exclusions.ContentSHA256()
		report.ExclusionManifestSHA256 = hex.EncodeToString(digest[:])
	}
	return report, nil
}

func inspectCutoverDatabaseIdentity(
	ctx context.Context,
	database *pgxpool.Pool,
	migrationVersion int64,
) (cutoverDatabaseIdentity, error) {
	var name, address string
	var oid uint32
	var port, version int64
	var readOnly bool
	if err := database.QueryRow(ctx, `
SELECT
    current_database(),
    COALESCE(inet_server_addr()::text, 'local-socket'),
    COALESCE(inet_server_port(), 0)::bigint,
    database.oid,
    current_setting('server_version_num')::bigint,
    current_setting('transaction_read_only')::boolean
FROM pg_database AS database
WHERE database.datname = current_database()`).Scan(
		&name, &address, &port, &oid, &version, &readOnly,
	); err != nil {
		return cutoverDatabaseIdentity{}, err
	}
	canonical := address + "\x00" + strconv.FormatInt(port, 10) + "\x00" +
		strconv.FormatUint(uint64(oid), 10) + "\x00" + name
	return cutoverDatabaseIdentity{
		fingerprint: sha256.Sum256([]byte(canonical)), version: version,
		migration: migrationVersion, readOnly: readOnly,
	}, nil
}

func cutoverDatabaseReport(identity cutoverDatabaseIdentity) CutoverDatabaseReport {
	return CutoverDatabaseReport{
		IdentitySHA256: hex.EncodeToString(identity.fingerprint[:]), PostgreSQLVersion: identity.version,
		MigrationVersion: identity.migration, TransactionReadOnly: identity.readOnly,
	}
}

// InspectCutoverArchive binds preflight to a strict, non-symlink ZIP snapshot.
// Directory mode remains useful during development validation, but a formal
// cutover needs one immutable archive digest that another operator can verify.
func InspectCutoverArchive(path string) ([sha256.Size]byte, int64, int64, error) {
	var zero [sha256.Size]byte
	if path == "" || !filepath.IsAbs(path) || !strings.EqualFold(filepath.Ext(path), ".zip") {
		return zero, 0, 0, errors.New("cutover torrent source must be an absolute ZIP file")
	}
	cleaned := filepath.Clean(path)
	before, err := os.Lstat(cleaned)
	if err != nil || !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 || before.Size() < 1 {
		return zero, 0, 0, errors.New("cutover torrent ZIP must be a non-symlink regular file")
	}
	root, err := openSourceObjectRoot(cleaned)
	if err != nil {
		return zero, 0, 0, err
	}
	objects := root.archiveObjectCount()
	if err := root.close(); err != nil {
		return zero, 0, 0, errors.New("close cutover torrent ZIP")
	}
	file, err := os.Open(cleaned)
	if err != nil {
		return zero, 0, 0, errors.New("open cutover torrent ZIP for hashing")
	}
	digest := sha256.New()
	written, readErr := io.Copy(digest, file)
	closeErr := file.Close()
	after, statErr := os.Lstat(cleaned)
	if readErr != nil || closeErr != nil || statErr != nil || !os.SameFile(before, after) ||
		written != before.Size() || after.Size() != before.Size() || after.ModTime() != before.ModTime() {
		return zero, 0, 0, errors.New("cutover torrent ZIP changed while it was inspected")
	}
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result, written, objects, nil
}

// InspectCutoverDatabaseDump verifies the gzip stream and hashes the exact
// compressed bytes used by the existing migration snapshot identity.
func InspectCutoverDatabaseDump(path string) ([sha256.Size]byte, int64, error) {
	var zero [sha256.Size]byte
	if path == "" || !filepath.IsAbs(path) || !strings.EqualFold(filepath.Ext(path), ".gz") {
		return zero, 0, errors.New("cutover database dump must be an absolute gzip file")
	}
	cleaned := filepath.Clean(path)
	before, err := os.Lstat(cleaned)
	if err != nil || !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 || before.Size() < 1 {
		return zero, 0, errors.New("cutover database dump must be a non-symlink regular file")
	}
	file, err := os.Open(cleaned)
	if err != nil {
		return zero, 0, errors.New("open cutover database dump")
	}
	compressed, err := gzip.NewReader(file)
	if err != nil {
		_ = file.Close()
		return zero, 0, errors.New("open cutover database dump gzip stream")
	}
	_, streamErr := io.Copy(io.Discard, compressed)
	gzipCloseErr := compressed.Close()
	if streamErr != nil || gzipCloseErr != nil {
		_ = file.Close()
		return zero, 0, errors.New("verify cutover database dump gzip stream")
	}
	if _, err := file.Seek(0, 0); err != nil {
		_ = file.Close()
		return zero, 0, errors.New("rewind cutover database dump")
	}
	digest := sha256.New()
	written, readErr := io.Copy(digest, file)
	closeErr := file.Close()
	after, statErr := os.Lstat(cleaned)
	if readErr != nil || closeErr != nil || statErr != nil || !os.SameFile(before, after) ||
		written != before.Size() || after.Size() != before.Size() || after.ModTime() != before.ModTime() {
		return zero, 0, errors.New("cutover database dump changed while it was inspected")
	}
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result, written, nil
}

func inspectCutoverRun(
	ctx context.Context,
	core *pgxpool.Pool,
	config InventoryConfig,
) (cutoverRun, bool, error) {
	var run cutoverRun
	err := core.QueryRow(ctx, `
SELECT id, source_snapshot_sha256, mapping_version, state,
       expected_user_rows, expected_torrent_rows, created_at
FROM migration.runs
WHERE id = $1`, config.RunID).Scan(
		&run.id, &run.snapshot, &run.mappingVersion, &run.state,
		&run.expectedUsers, &run.expectedTorrents, &run.createdAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		var conflictingID uuid.UUID
		conflictErr := core.QueryRow(ctx, `
SELECT id
FROM migration.runs
WHERE source_system = 'ptyes'
  AND source_snapshot_sha256 = $1
  AND mapping_version = $2`, config.SnapshotSHA256[:], config.MappingVersion).Scan(&conflictingID)
		if conflictErr == nil {
			return cutoverRun{}, false, errors.New("source snapshot identity is already reserved by a different migration run")
		}
		if !errors.Is(conflictErr, pgx.ErrNoRows) {
			return cutoverRun{}, false, fmt.Errorf("inspect migration snapshot reservation: %w", conflictErr)
		}
		return cutoverRun{}, false, nil
	}
	if err != nil {
		return cutoverRun{}, false, fmt.Errorf("inspect migration run: %w", err)
	}
	return run, true, nil
}

func inspectCutoverCoreTarget(
	ctx context.Context,
	core *pgxpool.Pool,
	runID uuid.UUID,
) (CutoverTargetReport, map[uuid.UUID]struct{}, error) {
	var report CutoverTargetReport
	var foreignUsers, foreignTorrents, foreignUserMappings, foreignTorrentMappings int64
	err := core.QueryRow(ctx, `
SELECT
    (SELECT count(*)::bigint FROM identity.users),
    (SELECT count(*)::bigint FROM torrents.torrents),
    (SELECT count(*)::bigint FROM migration.user_id_map),
    (SELECT count(*)::bigint FROM migration.torrent_id_map),
    (SELECT count(*)::bigint FROM migration.runs),
    (SELECT count(*)::bigint FROM identity.users AS target
     WHERE NOT EXISTS (
         SELECT 1 FROM migration.user_id_map AS mapping
         WHERE mapping.user_id = target.id AND mapping.first_run_id = $1
     )),
    (SELECT count(*)::bigint FROM torrents.torrents AS target
     WHERE NOT EXISTS (
         SELECT 1 FROM migration.torrent_id_map AS mapping
         WHERE mapping.torrent_id = target.id AND mapping.first_run_id = $1
     )),
    (SELECT count(*)::bigint FROM migration.user_id_map WHERE first_run_id <> $1),
    (SELECT count(*)::bigint FROM migration.torrent_id_map WHERE first_run_id <> $1)`, runID).Scan(
		&report.CoreUsers, &report.Torrents, &report.UserMappings, &report.TorrentMappings,
		&report.MigrationRuns, &foreignUsers, &foreignTorrents, &foreignUserMappings, &foreignTorrentMappings,
	)
	if err != nil {
		return CutoverTargetReport{}, nil, fmt.Errorf("inspect Core cutover target: %w", err)
	}
	if foreignUsers != 0 || foreignTorrents != 0 || foreignUserMappings != 0 || foreignTorrentMappings != 0 {
		return CutoverTargetReport{}, nil, errors.New("Core cutover target contains identities, torrents, or mappings outside the requested run")
	}
	rows, err := core.Query(ctx, `
SELECT credential_ref
FROM migration.user_id_map
WHERE first_run_id = $1`, runID)
	if err != nil {
		return CutoverTargetReport{}, nil, fmt.Errorf("read cutover credential references: %w", err)
	}
	defer rows.Close()
	allowed := make(map[uuid.UUID]struct{}, report.UserMappings)
	for rows.Next() {
		var reference uuid.UUID
		if err := rows.Scan(&reference); err != nil {
			return CutoverTargetReport{}, nil, fmt.Errorf("scan cutover credential reference: %w", err)
		}
		allowed[reference] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return CutoverTargetReport{}, nil, fmt.Errorf("read cutover credential references: %w", err)
	}
	return report, allowed, nil
}

func inspectCutoverVaultTarget(
	ctx context.Context,
	vault *pgxpool.Pool,
	allowed map[uuid.UUID]struct{},
) (int64, error) {
	rows, err := vault.Query(ctx, `SELECT credential_ref FROM vault.credentials`)
	if err != nil {
		return 0, fmt.Errorf("inspect Vault cutover target: %w", err)
	}
	defer rows.Close()
	var count int64
	for rows.Next() {
		var reference uuid.UUID
		if err := rows.Scan(&reference); err != nil {
			return 0, fmt.Errorf("scan Vault cutover credential: %w", err)
		}
		count++
		if _, exists := allowed[reference]; !exists {
			return 0, errors.New("Vault cutover target contains a credential outside the requested run")
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read Vault cutover credentials: %w", err)
	}
	return count, nil
}

func requireCutoverBackendConsistency(
	ctx context.Context,
	core *pgxpool.Pool,
	runID uuid.UUID,
	backendID string,
) error {
	rows, err := core.Query(ctx, `
SELECT DISTINCT location.backend_id
FROM migration.source_rows AS checkpoint
JOIN migration.torrent_id_map AS mapping
  ON mapping.legacy_torrent_id = checkpoint.legacy_id
 AND mapping.first_run_id = checkpoint.run_id
JOIN torrents.torrents AS torrent ON torrent.id = mapping.torrent_id
JOIN torrents.torrent_object_locations AS location ON location.object_id = torrent.object_id
WHERE checkpoint.run_id = $1
  AND checkpoint.entity_kind = 'torrent'
  AND checkpoint.state = 'imported'
  AND location.state <> 'deleted'`, runID)
	if err != nil {
		return fmt.Errorf("inspect cutover object backends: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var observed string
		if err := rows.Scan(&observed); err != nil {
			return fmt.Errorf("scan cutover object backend: %w", err)
		}
		if observed != backendID {
			return fmt.Errorf("cutover object backend changed from %s to %s", observed, backendID)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read cutover object backends: %w", err)
	}
	return nil
}

// WriteCutoverPreflightManifest creates a new, mode-0600 audit artifact. It
// never overwrites an earlier preflight, because doing so would erase the
// operator-visible evidence for the exact inputs and target observed then.
func WriteCutoverPreflightManifest(path string, report CutoverPreflightReport) ([sha256.Size]byte, error) {
	if report.Schema != CutoverPreflightSchema || !report.Ready {
		return [sha256.Size]byte{}, errors.New("cutover preflight manifest is invalid")
	}
	return writeExclusiveCutoverJSON(path, report)
}
