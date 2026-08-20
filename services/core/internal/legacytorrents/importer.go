package legacytorrents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/trackerevent"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	"github.com/peergo/peergo/services/core/internal/modules/trackercontrol"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

var (
	torrentObjectNamespace   = uuid.MustParse("4fcab2db-48bd-50be-a710-014903207b3f")
	torrentLocationNamespace = uuid.MustParse("5f4e95b1-d42b-543c-a6d2-5ea9efae922b")
	trackerEventNamespace    = uuid.MustParse("7ac3bb30-709b-559d-b84c-fc1581f52e0c")
)

type ImportConfig struct {
	Inventory     InventoryConfig
	TorrentRoot   string
	OccurredAt    time.Time
	ProgressEvery int64
	Store         torrents.ObjectStore
	Exclusions    TorrentExclusionManifest
}

type ImportProgress struct {
	Phase     string
	Processed int64
	Expected  int64
	Imported  int64
	Skipped   int64
	Excluded  int64
}

type ImportResult struct {
	RunID               uuid.UUID
	ExpectedTorrents    int64
	ImportedTorrents    int64
	SkippedTorrents     int64
	ExcludedTorrents    int64
	PublishedTorrents   int64
	ObjectBytes         int64
	FacetValues         int64
	ExternalIdentifiers int64
	RecoveredObjects    int64
	Preparation         PreparationResult
}

type checkpointPair struct {
	torrentState string
	objectState  string
}

type torrentImportRecord struct {
	source   sourceTorrent
	manifest sourceFileManifest
	parsed   torrents.ParsedMetainfo
	raw      []byte
	// sourceObjectID is the PtYes archive filename locator. It is consumed at
	// the import boundary and is never persisted as a second torrent identity.
	sourceObjectID      uuid.UUID
	uploaderID          uuid.UUID
	objectID            uuid.UUID
	categoryID          string
	state               torrents.State
	facets              []facetValue
	externalIdentifiers map[string]string
	torrentFingerprint  [sha256.Size]byte
	objectKey           torrents.ObjectKey
	storageVersionID    string
}

func Import(
	ctx context.Context,
	source *pgxpool.Pool,
	core *pgxpool.Pool,
	config ImportConfig,
	progress func(ImportProgress),
) (ImportResult, error) {
	config.TorrentRoot = strings.TrimSpace(config.TorrentRoot)
	config.OccurredAt = config.OccurredAt.UTC().Truncate(time.Microsecond)
	if source == nil || core == nil || config.Store == nil || config.TorrentRoot == "" || config.OccurredAt.IsZero() {
		return ImportResult{}, errors.New("legacy torrent import configuration is invalid")
	}
	if config.ProgressEvery < 1 {
		config.ProgressEvery = 250
	}
	if progress == nil {
		progress = func(ImportProgress) {}
	}
	if err := platformpostgres.RequireCurrentMigration(ctx, core); err != nil {
		return ImportResult{}, err
	}
	inventory, err := InspectInventory(ctx, source, core, config.Inventory)
	if err != nil {
		return ImportResult{}, err
	}
	expectedTorrents := inventory.Torrents - int64(config.Exclusions.Len())
	if expectedTorrents < 0 {
		return ImportResult{}, errors.New("torrent exclusion count exceeds the source inventory")
	}
	if err := verifyImportReadiness(
		ctx, core, config, expectedTorrents, int64(config.Exclusions.Len()),
	); err != nil {
		return ImportResult{}, err
	}
	root, err := openSourceObjectRoot(config.TorrentRoot)
	if err != nil {
		return ImportResult{}, err
	}
	defer func() { _ = root.close() }()
	recovery, err := prepareSourceObjectRecovery(ctx, source, root)
	if err != nil {
		return ImportResult{}, err
	}
	userMappings, err := loadUserMappings(ctx, core)
	if err != nil {
		return ImportResult{}, err
	}
	vocabulary, err := loadLegacyVocabulary(ctx, source)
	if err != nil {
		return ImportResult{}, err
	}
	preparation, err := prepareImportDependencies(
		ctx, source, core, config.Inventory.RunID, config.OccurredAt,
	)
	if err != nil {
		return ImportResult{}, err
	}
	progress(ImportProgress{Phase: "dependencies", Expected: inventory.Torrents})

	rows, err := querySourceTorrents(ctx, source)
	if err != nil {
		return ImportResult{}, err
	}
	defer rows.Close()
	fileCursor, err := newSourceFileCursor(ctx, source)
	if err != nil {
		return ImportResult{}, err
	}
	defer fileCursor.Close()
	result := ImportResult{
		RunID: config.Inventory.RunID, ExpectedTorrents: expectedTorrents,
		RecoveredObjects: recovery.RecoveredObjects, Preparation: preparation,
	}
	for rows.Next() {
		sourceTorrent, scanErr := scanSourceTorrent(rows)
		if scanErr != nil {
			return ImportResult{}, fmt.Errorf("scan PtYes torrent import row: %w", scanErr)
		}
		manifest, manifestErr := fileCursor.ManifestFor(sourceTorrent.LegacyID)
		if manifestErr != nil {
			return ImportResult{}, manifestErr
		}
		if _, excluded := config.Exclusions.entries[sourceTorrent.LegacyID]; excluded {
			if !config.Exclusions.match(sourceTorrent) {
				return ImportResult{}, sourceTorrentError(sourceTorrent.LegacyID, "exclusion_identity_mismatch")
			}
			sourceObjectID, sourceObjectIDErr := sourceTorrent.publicID()
			if sourceObjectIDErr != nil {
				return ImportResult{}, sourceTorrentError(sourceTorrent.LegacyID, "invalid_public_id")
			}
			if _, readErr := root.read(sourceObjectID); sourceObjectErrorCode(readErr) != "object_missing" {
				return ImportResult{}, sourceTorrentError(sourceTorrent.LegacyID, "exclusion_object_present")
			}
			if err := verifyExcludedCheckpoints(
				ctx, core, config.Inventory.RunID, config.Exclusions, sourceTorrent, manifest,
			); err != nil {
				return ImportResult{}, err
			}
			result.ExcludedTorrents++
			processed := result.ImportedTorrents + result.SkippedTorrents + result.ExcludedTorrents
			if processed%config.ProgressEvery == 0 {
				progress(ImportProgress{
					Phase: "torrents", Processed: processed, Expected: inventory.Torrents,
					Imported: result.ImportedTorrents, Skipped: result.SkippedTorrents,
					Excluded: result.ExcludedTorrents,
				})
			}
			continue
		}
		record, recordErr := buildTorrentImportRecord(
			root, sourceTorrent, manifest, userMappings, vocabulary,
		)
		if recordErr != nil {
			return ImportResult{}, recordErr
		}
		checkpoints, checkpointErr := readCheckpointPair(ctx, core, config.Inventory.RunID, record)
		if checkpointErr != nil {
			return ImportResult{}, checkpointErr
		}
		if checkpoints.torrentState == "imported" && checkpoints.objectState == "imported" {
			if err := verifyImportedTorrent(ctx, core, config.Store.BackendID(), record); err != nil {
				return ImportResult{}, err
			}
			result.SkippedTorrents++
		} else {
			objectKey, versionID, storeErr := storeAndVerifyLegacyObject(
				ctx, config.Store, record.raw, record.parsed, record.source.LegacyID,
			)
			if storeErr != nil {
				return ImportResult{}, storeErr
			}
			record.objectKey = objectKey
			record.storageVersionID = versionID
			if err := persistTorrentImport(ctx, core, config, record); err != nil {
				return ImportResult{}, err
			}
			result.ImportedTorrents++
		}
		result.ObjectBytes += record.parsed.ObjectByteLength
		result.FacetValues += int64(len(record.facets))
		result.ExternalIdentifiers += int64(len(record.externalIdentifiers))
		if record.state == torrents.StatePublished {
			result.PublishedTorrents++
		}
		processed := result.ImportedTorrents + result.SkippedTorrents + result.ExcludedTorrents
		if processed%config.ProgressEvery == 0 {
			progress(ImportProgress{
				Phase: "torrents", Processed: processed, Expected: inventory.Torrents,
				Imported: result.ImportedTorrents, Skipped: result.SkippedTorrents,
				Excluded: result.ExcludedTorrents,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return ImportResult{}, fmt.Errorf("read PtYes torrent import rows: %w", err)
	}
	if err := fileCursor.Finish(); err != nil {
		return ImportResult{}, err
	}
	if result.ImportedTorrents+result.SkippedTorrents != result.ExpectedTorrents ||
		result.ExcludedTorrents != int64(config.Exclusions.Len()) ||
		result.ImportedTorrents+result.SkippedTorrents+result.ExcludedTorrents != inventory.Torrents {
		return ImportResult{}, errors.New("PtYes torrent import count changed during processing")
	}
	if err := finalizeTorrentImport(ctx, core, config, result); err != nil {
		return ImportResult{}, err
	}
	return result, nil
}

func buildTorrentImportRecord(
	root *sourceObjectRoot,
	source sourceTorrent,
	manifest sourceFileManifest,
	userMappings map[int64]uuid.UUID,
	vocabulary legacyVocabulary,
) (torrentImportRecord, error) {
	if source.validate() != nil {
		return torrentImportRecord{}, sourceTorrentError(source.LegacyID, "invalid_metadata")
	}
	sourceObjectID, err := source.publicID()
	if err != nil {
		return torrentImportRecord{}, sourceTorrentError(source.LegacyID, "invalid_public_id")
	}
	uploaderID, exists := userMappings[source.UploaderLegacyID]
	if !exists {
		return torrentImportRecord{}, sourceTorrentError(source.LegacyID, "missing_uploader_mapping")
	}
	fingerprint, err := source.fingerprint(manifest)
	if err != nil {
		return torrentImportRecord{}, sourceTorrentError(source.LegacyID, "invalid_metadata_fingerprint")
	}
	raw, err := root.read(sourceObjectID)
	if err != nil {
		return torrentImportRecord{}, wrapSourceObjectError(source.LegacyID, err)
	}
	parsed, err := torrents.InspectLegacyV1OrHybrid(raw)
	if err != nil {
		return torrentImportRecord{}, wrapSourceObjectError(source.LegacyID, err)
	}
	if err := reconcileSourceMetainfo(source, manifest, parsed); err != nil {
		return torrentImportRecord{}, err
	}
	categoryID, err := source.categoryID()
	if err != nil {
		return torrentImportRecord{}, sourceTorrentError(source.LegacyID, "unknown_category")
	}
	state, err := source.state()
	if err != nil {
		return torrentImportRecord{}, sourceTorrentError(source.LegacyID, "unknown_state")
	}
	facets, attributes, err := mapLegacyAttributes(source.SourceCategory, source.Attributes, vocabulary)
	if err != nil {
		return torrentImportRecord{}, sourceTorrentError(source.LegacyID, taxonomyErrorCode(err))
	}
	externalIDs, _, err := extractTorrentExternalIDs(attributes)
	if err != nil {
		return torrentImportRecord{}, sourceTorrentError(source.LegacyID, taxonomyErrorCode(err))
	}
	return torrentImportRecord{
		source: source, manifest: manifest, parsed: parsed, raw: raw,
		sourceObjectID: sourceObjectID, uploaderID: uploaderID,
		objectID:   uuid.NewSHA1(torrentObjectNamespace, []byte(strconv.FormatInt(source.LegacyID, 10))),
		categoryID: categoryID, state: state, facets: facets,
		externalIdentifiers: externalIDs, torrentFingerprint: fingerprint,
	}, nil
}

func verifyImportReadiness(
	ctx context.Context,
	core *pgxpool.Pool,
	config ImportConfig,
	expectedTorrents int64,
	expectedExcluded int64,
) error {
	var state string
	var stateChangedAt time.Time
	var readyTorrents, readyObjects, skippedTorrents, skippedObjects, artifacts, unresolved int64
	if err := core.QueryRow(ctx, `
SELECT
    run.state,
    run.state_changed_at,
    count(checkpoint.legacy_id) FILTER (
        WHERE checkpoint.entity_kind = 'torrent'
          AND checkpoint.state IN ('validated', 'imported')
    )::bigint,
    count(checkpoint.legacy_id) FILTER (
        WHERE checkpoint.entity_kind = 'torrent_object'
          AND checkpoint.state IN ('validated', 'imported')
    )::bigint,
	count(checkpoint.legacy_id) FILTER (
	    WHERE checkpoint.entity_kind = 'torrent'
	      AND checkpoint.state = 'skipped'
	      AND checkpoint.error_code = 'object_missing_explicitly_excluded'
	)::bigint,
	count(checkpoint.legacy_id) FILTER (
	    WHERE checkpoint.entity_kind = 'torrent_object'
	      AND checkpoint.state = 'skipped'
	      AND checkpoint.error_code = 'object_missing_explicitly_excluded'
	)::bigint,
    (SELECT count(*)::bigint
     FROM migration.run_artifacts AS artifact
     WHERE artifact.run_id = run.id
       AND artifact.kind = 'torrent_manifest'
       AND artifact.item_count = $2),
    (SELECT count(*)::bigint
     FROM migration.discrepancies AS problem
     LEFT JOIN migration.discrepancy_resolutions AS resolution
       ON resolution.discrepancy_id = problem.id
     WHERE problem.run_id = run.id
       AND problem.entity_kind IN ('torrent', 'torrent_object')
       AND resolution.discrepancy_id IS NULL)
FROM migration.runs AS run
LEFT JOIN migration.source_rows AS checkpoint ON checkpoint.run_id = run.id
WHERE run.id = $1
GROUP BY run.id`, config.Inventory.RunID, expectedTorrents).Scan(
		&state, &stateChangedAt, &readyTorrents, &readyObjects,
		&skippedTorrents, &skippedObjects, &artifacts, &unresolved,
	); err != nil {
		return fmt.Errorf("read legacy torrent import readiness: %w", err)
	}
	if (state != "importing" && state != "imported" && state != "reconciled") ||
		(state == "importing" && config.OccurredAt.Before(stateChangedAt)) || readyTorrents != expectedTorrents ||
		readyObjects != expectedTorrents || skippedTorrents != expectedExcluded ||
		skippedObjects != expectedExcluded || artifacts != 1 || unresolved != 0 {
		return errors.New("legacy migration run is not ready for torrent import")
	}
	return nil
}

func verifyExcludedCheckpoints(
	ctx context.Context,
	core *pgxpool.Pool,
	runID uuid.UUID,
	exclusions TorrentExclusionManifest,
	source sourceTorrent,
	files sourceFileManifest,
) error {
	torrentFingerprint, err := source.fingerprint(files)
	if err != nil {
		return sourceTorrentError(source.LegacyID, "invalid_metadata_fingerprint")
	}
	objectFingerprint, err := exclusions.objectFingerprint(source)
	if err != nil {
		return sourceTorrentError(source.LegacyID, "invalid_exclusion_fingerprint")
	}
	var matching int64
	if err := core.QueryRow(ctx, `
SELECT count(*)::bigint
FROM migration.source_rows
WHERE run_id = $1
  AND legacy_id = $2
  AND entity_kind IN ('torrent', 'torrent_object')
  AND source_fingerprint = CASE entity_kind
      WHEN 'torrent' THEN $3::bytea
      ELSE $4::bytea
  END
  AND fingerprint_scheme = 'sha256-v1'
  AND state = 'skipped'
  AND error_code = 'object_missing_explicitly_excluded'`,
		runID, source.LegacyID, torrentFingerprint[:], objectFingerprint[:],
	).Scan(&matching); err != nil {
		return fmt.Errorf("verify explicitly excluded torrent checkpoints: %w", err)
	}
	if matching != 2 {
		return sourceTorrentError(source.LegacyID, "exclusion_checkpoint_mismatch")
	}
	return nil
}

func readCheckpointPair(
	ctx context.Context,
	core *pgxpool.Pool,
	runID uuid.UUID,
	record torrentImportRecord,
) (checkpointPair, error) {
	rows, err := core.Query(ctx, `
SELECT entity_kind, source_fingerprint, fingerprint_scheme, state
FROM migration.source_rows
WHERE run_id = $1
  AND legacy_id = $2
  AND entity_kind IN ('torrent', 'torrent_object')
ORDER BY entity_kind`, runID, record.source.LegacyID)
	if err != nil {
		return checkpointPair{}, fmt.Errorf("read legacy torrent checkpoints: %w", err)
	}
	defer rows.Close()
	var result checkpointPair
	var seen int
	for rows.Next() {
		var kind, scheme, state string
		var fingerprint []byte
		if err := rows.Scan(&kind, &fingerprint, &scheme, &state); err != nil {
			return checkpointPair{}, fmt.Errorf("scan legacy torrent checkpoint: %w", err)
		}
		expected := record.torrentFingerprint[:]
		if kind == "torrent_object" {
			expected = record.parsed.ObjectSHA256[:]
		}
		if scheme != "sha256-v1" || !bytes.Equal(fingerprint, expected) ||
			(state != "validated" && state != "imported") {
			return checkpointPair{}, sourceTorrentError(record.source.LegacyID, "checkpoint_conflict")
		}
		switch kind {
		case "torrent":
			result.torrentState = state
		case "torrent_object":
			result.objectState = state
		default:
			return checkpointPair{}, sourceTorrentError(record.source.LegacyID, "checkpoint_kind_invalid")
		}
		seen++
	}
	if err := rows.Err(); err != nil {
		return checkpointPair{}, fmt.Errorf("read legacy torrent checkpoints: %w", err)
	}
	if seen != 2 || result.torrentState == "" || result.objectState == "" || result.torrentState != result.objectState {
		return checkpointPair{}, sourceTorrentError(record.source.LegacyID, "checkpoint_pair_inconsistent")
	}
	return result, nil
}

func storeAndVerifyLegacyObject(
	ctx context.Context,
	store torrents.ObjectStore,
	raw []byte,
	parsed torrents.ParsedMetainfo,
	legacyID int64,
) (torrents.ObjectKey, string, error) {
	if len(raw) < 1 || int64(len(raw)) != parsed.ObjectByteLength {
		return "", "", sourceTorrentError(legacyID, "object_bytes_changed_before_store")
	}
	descriptor := torrents.StoredObjectDescriptor{
		SHA256: parsed.ObjectSHA256, ByteLength: parsed.ObjectByteLength,
	}
	key := torrents.TorrentObjectKey(parsed.ObjectSHA256)
	writeResult, err := store.PutIfAbsent(ctx, key, bytes.NewReader(raw), descriptor)
	if err != nil {
		return "", "", fmt.Errorf("store legacy torrent %d immutable object: %w", legacyID, err)
	}
	object, err := store.Open(ctx, key, writeResult.VersionID)
	if err != nil || object.Body == nil {
		return "", "", fmt.Errorf("open legacy torrent %d object read-back", legacyID)
	}
	verified, verifyErr := torrents.VerifyStoredObject(object, descriptor)
	closeErr := object.Body.Close()
	if verifyErr != nil {
		return "", "", fmt.Errorf("verify legacy torrent %d object read-back: %w", legacyID, verifyErr)
	}
	if closeErr != nil {
		return "", "", fmt.Errorf("close legacy torrent %d object read-back: %w", legacyID, closeErr)
	}
	if verified != descriptor {
		return "", "", sourceTorrentError(legacyID, "stored_object_descriptor_mismatch")
	}
	versionID := object.VersionID
	if versionID == "" {
		versionID = writeResult.VersionID
	}
	if len(versionID) > 1024 {
		return "", "", sourceTorrentError(legacyID, "storage_version_id_too_long")
	}
	return key, versionID, nil
}

func persistTorrentImport(
	ctx context.Context,
	core *pgxpool.Pool,
	config ImportConfig,
	record torrentImportRecord,
) error {
	tx, err := core.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin legacy torrent import transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockValidatedCheckpointPair(ctx, tx, config.Inventory.RunID, record); err != nil {
		return err
	}
	if err := insertTorrentMapping(ctx, tx, config, record); err != nil {
		return err
	}
	if err := insertTorrentObjectAndLocation(ctx, tx, config, record); err != nil {
		return err
	}
	if err := insertTorrentAggregate(ctx, tx, record); err != nil {
		return err
	}
	if err := insertTorrentFiles(ctx, tx, record); err != nil {
		return err
	}
	if err := insertTorrentMetadata(ctx, tx, config.OccurredAt, record); err != nil {
		return err
	}
	if record.state == torrents.StatePublished {
		if err := insertPublishedProjections(ctx, tx, config.OccurredAt, record); err != nil {
			return err
		}
	}
	updated, err := tx.Exec(ctx, `
UPDATE migration.source_rows
SET state = 'imported',
    attempt_count = attempt_count + 1,
    error_code = NULL,
    version = version + 1,
    updated_at = $1
WHERE run_id = $2
  AND legacy_id = $3
  AND entity_kind IN ('torrent', 'torrent_object')
  AND state = 'validated'`, config.OccurredAt, config.Inventory.RunID, record.source.LegacyID)
	if err != nil {
		return fmt.Errorf("complete legacy torrent checkpoints: %w", err)
	}
	if updated.RowsAffected() != 2 {
		return sourceTorrentError(record.source.LegacyID, "checkpoint_finalization_conflict")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit legacy torrent import: %w", err)
	}
	return nil
}

func lockValidatedCheckpointPair(ctx context.Context, tx pgx.Tx, runID uuid.UUID, record torrentImportRecord) error {
	rows, err := tx.Query(ctx, `
SELECT entity_kind, source_fingerprint, state
FROM migration.source_rows
WHERE run_id = $1
  AND legacy_id = $2
  AND entity_kind IN ('torrent', 'torrent_object')
ORDER BY entity_kind
FOR UPDATE`, runID, record.source.LegacyID)
	if err != nil {
		return fmt.Errorf("lock legacy torrent checkpoints: %w", err)
	}
	defer rows.Close()
	var seen int
	for rows.Next() {
		var kind, state string
		var fingerprint []byte
		if err := rows.Scan(&kind, &fingerprint, &state); err != nil {
			return fmt.Errorf("scan locked legacy torrent checkpoint: %w", err)
		}
		expected := record.torrentFingerprint[:]
		if kind == "torrent_object" {
			expected = record.parsed.ObjectSHA256[:]
		}
		if state != "validated" || !bytes.Equal(fingerprint, expected) {
			return sourceTorrentError(record.source.LegacyID, "checkpoint_lock_conflict")
		}
		seen++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read locked legacy torrent checkpoints: %w", err)
	}
	if seen != 2 {
		return sourceTorrentError(record.source.LegacyID, "checkpoint_pair_missing")
	}
	return nil
}

func insertTorrentMapping(ctx context.Context, tx pgx.Tx, config ImportConfig, record torrentImportRecord) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO migration.torrent_id_map (
	 source_system, legacy_torrent_id, torrent_id,
	 object_id, first_run_id, created_at
) VALUES ('ptyes', $1, $1, $2, $3, $4)
ON CONFLICT (source_system, legacy_torrent_id) DO NOTHING`,
		record.source.LegacyID, record.objectID,
		config.Inventory.RunID, config.OccurredAt,
	); err != nil {
		return fmt.Errorf("insert stable legacy torrent mapping: %w", err)
	}
	var torrentID int64
	var objectID uuid.UUID
	if err := tx.QueryRow(ctx, `
SELECT torrent_id, object_id
FROM migration.torrent_id_map
WHERE source_system = 'ptyes' AND legacy_torrent_id = $1`, record.source.LegacyID).Scan(
		&torrentID, &objectID,
	); err != nil {
		return fmt.Errorf("read stable legacy torrent mapping: %w", err)
	}
	if torrentID != record.source.LegacyID || objectID != record.objectID {
		return sourceTorrentError(record.source.LegacyID, "torrent_mapping_conflict")
	}
	return nil
}

func insertTorrentObjectAndLocation(ctx context.Context, tx pgx.Tx, config ImportConfig, record torrentImportRecord) error {
	flags := make([]string, 0, len(record.parsed.CompatibilityFlags))
	for _, flag := range record.parsed.CompatibilityFlags {
		flags = append(flags, string(flag))
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO torrents.torrent_objects (
    id, content_sha256, byte_length, parser_version, validation_profile,
    compatibility_flags, info_offset, info_length, created_at
) VALUES ($1, $2, $3, $4, 'legacy_import', $5, $6, $7, $8)`,
		record.objectID, record.parsed.ObjectSHA256[:], record.parsed.ObjectByteLength,
		record.parsed.ParserVersion, flags, record.parsed.InfoOffset,
		record.parsed.InfoLength, record.source.CreatedAt,
	); err != nil {
		return fmt.Errorf("insert immutable legacy torrent object: %w", err)
	}
	locationSeed := append(append([]byte(nil), record.objectID[:]...), []byte(config.Store.BackendID())...)
	locationID := uuid.NewSHA1(torrentLocationNamespace, locationSeed)
	var versionID any
	if record.storageVersionID != "" {
		versionID = record.storageVersionID
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO torrents.torrent_object_locations (
    id, object_id, backend_id, object_key, state, is_preferred,
    version_id, observed_byte_length, observed_sha256, verified_at,
    created_at, updated_at
) VALUES ($1, $2, $3, $4, 'verified', true, $5, $6, $7, $8, $8, $8)`,
		locationID, record.objectID, string(config.Store.BackendID()), string(record.objectKey),
		versionID, record.parsed.ObjectByteLength, record.parsed.ObjectSHA256[:], config.OccurredAt,
	); err != nil {
		return fmt.Errorf("insert verified legacy torrent object location: %w", err)
	}
	return nil
}

func insertTorrentAggregate(ctx context.Context, tx pgx.Tx, record torrentImportRecord) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO torrents.torrents (
	 id, uploader_id, category_id, object_id, info_hash_v1,
    content_name, title, subtitle, total_size_bytes, payload_size_bytes,
    file_count, padding_file_count, piece_length_bytes, piece_count,
    state, version, submitted_at, published_at, state_changed_at, updated_at,
    description, description_format, media_info, anonymous, resource_group_id
) VALUES (
	 $1, $2, $3, $4, $5,
	 $6, $7, $8, $9, $10,
	 $11, $12, $13, $14,
	 $15, 1, $16, $17, $18, $19,
	 $20, 'markdown', $21, $22, $23
)`,
		record.source.LegacyID, record.uploaderID, record.categoryID,
		record.objectID, record.parsed.InfoHashV1[:], record.parsed.Name,
		record.source.title(), record.source.subtitle(), record.parsed.TotalSizeBytes,
		record.parsed.PayloadSizeBytes, len(record.parsed.Files), record.parsed.PaddingFileCount,
		record.parsed.PieceLengthBytes, record.parsed.PieceCount, string(record.state),
		record.source.CreatedAt, record.source.publishedAt(), record.source.stateChangedAt(),
		record.source.targetUpdatedAt(), record.source.Description, record.source.MediaInfo,
		record.source.Anonymous, record.source.GroupLegacyID,
	); err != nil {
		return fmt.Errorf("insert legacy torrent aggregate: %w", err)
	}
	return nil
}

func insertTorrentFiles(ctx context.Context, tx pgx.Tx, record torrentImportRecord) error {
	// These rows are rebuilt from the verified raw metainfo. PtYes's
	// torrent_files table is only a reconciliation input and is never copied as
	// an independent source of truth, so a later parser can reproduce the same
	// projection from the immutable object.
	rows := make([][]any, 0, len(record.parsed.Files))
	for _, file := range record.parsed.Files {
		rows = append(rows, []any{
			record.source.LegacyID, file.Index, append([]string(nil), file.PathComponents...),
			file.DisplayPath, file.LengthBytes, file.Padding, record.source.CreatedAt,
		})
	}
	copied, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"torrents", "torrent_files"},
		[]string{"torrent_id", "file_index", "path_components", "display_path", "size_bytes", "is_padding", "created_at"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("copy parsed legacy torrent file projection: %w", err)
	}
	if copied != int64(len(rows)) {
		return sourceTorrentError(record.source.LegacyID, "torrent_file_copy_incomplete")
	}
	return nil
}

func insertTorrentMetadata(ctx context.Context, tx pgx.Tx, occurredAt time.Time, record torrentImportRecord) error {
	if len(record.externalIdentifiers) > 0 {
		providers := make([]string, 0, len(record.externalIdentifiers))
		for provider := range record.externalIdentifiers {
			providers = append(providers, provider)
		}
		sortStrings(providers)
		for _, provider := range providers {
			if _, err := tx.Exec(ctx, `
INSERT INTO torrents.torrent_external_identifiers (
    torrent_id, provider, external_id, origin, created_at, updated_at
) VALUES ($1, $2, $3, 'legacy_import', $4, $4)`,
				record.source.LegacyID, provider, record.externalIdentifiers[provider], occurredAt,
			); err != nil {
				return fmt.Errorf("insert legacy torrent external identifier: %w", err)
			}
		}
	}
	if len(record.facets) == 0 {
		return nil
	}
	rows := make([][]any, 0, len(record.facets))
	for _, facet := range record.facets {
		rows = append(rows, []any{
			record.source.LegacyID, facet.CategoryID, facet.FacetID,
			facet.OptionKey, facet.SelectionMode, facet.Position, occurredAt,
		})
	}
	copied, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"torrents", "torrent_facet_values"},
		[]string{"torrent_id", "category_id", "facet_id", "option_key", "selection_mode", "position", "created_at"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("copy legacy torrent facet values: %w", err)
	}
	if copied != int64(len(rows)) {
		return sourceTorrentError(record.source.LegacyID, "torrent_facet_copy_incomplete")
	}
	return nil
}

func insertPublishedProjections(ctx context.Context, tx pgx.Tx, cutoverAt time.Time, record torrentImportRecord) error {
	publishedAt := record.source.publishedAt()
	if publishedAt == nil {
		return sourceTorrentError(record.source.LegacyID, "published_time_missing")
	}
	promotion, promotionEndsAt, err := record.source.catalogPromotion(cutoverAt)
	if err != nil {
		return sourceTorrentError(record.source.LegacyID, "promotion_invalid")
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO catalog.torrents (
    id, category_id, name, subtitle, size_bytes, promotion, promotion_ends_at,
    published_at, created_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)`,
		record.source.LegacyID, record.categoryID, record.source.title(),
		record.source.subtitle(), record.parsed.TotalSizeBytes, string(promotion), promotionEndsAt, *publishedAt,
	); err != nil {
		return fmt.Errorf("insert published legacy catalog projection: %w", err)
	}
	eventID := uuid.NewSHA1(trackerEventNamespace, []byte(strconv.FormatInt(record.source.LegacyID, 10)))
	event, err := trackerevent.NewTorrentEligibilityChanged(trackerevent.TorrentEligibilityInput{
		EventID: eventID, OccurredAt: record.source.stateChangedAt(),
		TorrentID:  record.source.LegacyID,
		InfoHashV1: record.parsed.InfoHashV1, TotalSizeBytes: record.parsed.TotalSizeBytes,
		Enabled: true, TorrentVersion: 1,
	})
	if err != nil {
		return fmt.Errorf("build legacy Tracker eligibility event: %w", err)
	}
	if err := trackercontrol.NewPostgresOutbox(tx).Append(ctx, event); err != nil {
		return err
	}
	return nil
}

func verifyImportedTorrent(
	ctx context.Context,
	core *pgxpool.Pool,
	backendID torrents.StorageBackendID,
	record torrentImportRecord,
) error {
	var matches, files, facets, identifiers int64
	if err := core.QueryRow(ctx, `
SELECT
	count(*) FILTER (
		WHERE mapping.torrent_id = source.id
		  AND mapping.object_id = source.object_id
          AND object.content_sha256 = $2
          AND object.byte_length = $3
          AND location.backend_id = $4
          AND location.state = 'verified'
          AND location.is_preferred
          AND location.observed_sha256 = $2
          AND location.observed_byte_length = $3
          AND source.info_hash_v1 = $5
    )::bigint,
    (SELECT count(*)::bigint FROM torrents.torrent_files WHERE torrent_id = $1),
    (SELECT count(*)::bigint FROM torrents.torrent_facet_values WHERE torrent_id = $1),
    (SELECT count(*)::bigint FROM torrents.torrent_external_identifiers WHERE torrent_id = $1)
FROM migration.torrent_id_map AS mapping
JOIN torrents.torrents AS source ON source.id = mapping.torrent_id
JOIN torrents.torrent_objects AS object ON object.id = source.object_id
JOIN torrents.torrent_object_locations AS location ON location.object_id = object.id
WHERE mapping.source_system = 'ptyes'
  AND mapping.legacy_torrent_id = $1`,
		record.source.LegacyID, record.parsed.ObjectSHA256[:], record.parsed.ObjectByteLength,
		string(backendID), record.parsed.InfoHashV1[:],
	).Scan(&matches, &files, &facets, &identifiers); err != nil {
		return fmt.Errorf("verify imported legacy torrent: %w", err)
	}
	if matches != 1 || files != int64(len(record.parsed.Files)) ||
		facets != int64(len(record.facets)) || identifiers != int64(len(record.externalIdentifiers)) {
		return sourceTorrentError(record.source.LegacyID, "imported_target_mismatch")
	}
	return nil
}

func finalizeTorrentImport(ctx context.Context, core *pgxpool.Pool, config ImportConfig, result ImportResult) error {
	tx, err := core.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin legacy torrent import finalization: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var users, torrentsCount, objects, skippedTorrents, skippedObjects, mappings, published, outbox int64
	if err := tx.QueryRow(ctx, `
SELECT
    count(*) FILTER (WHERE entity_kind = 'user' AND state = 'imported')::bigint,
    count(*) FILTER (WHERE entity_kind = 'torrent' AND state = 'imported')::bigint,
    count(*) FILTER (WHERE entity_kind = 'torrent_object' AND state = 'imported')::bigint,
	count(*) FILTER (
	    WHERE entity_kind = 'torrent' AND state = 'skipped'
	      AND error_code = 'object_missing_explicitly_excluded'
	)::bigint,
	count(*) FILTER (
	    WHERE entity_kind = 'torrent_object' AND state = 'skipped'
	      AND error_code = 'object_missing_explicitly_excluded'
	)::bigint,
    (SELECT count(*)::bigint FROM migration.torrent_id_map WHERE source_system = 'ptyes'),
	    (SELECT count(*)::bigint
	     FROM torrents.torrents AS torrent
	     JOIN migration.torrent_id_map AS mapping ON mapping.torrent_id = torrent.id
	     WHERE mapping.source_system = 'ptyes' AND torrent.state = 'published'),
	    (SELECT count(*)::bigint
	     FROM tracker_control.outbox AS event
	     JOIN migration.torrent_id_map AS mapping ON mapping.torrent_id = event.aggregate_id
	     WHERE mapping.source_system = 'ptyes')
FROM migration.source_rows
WHERE run_id = $1`, config.Inventory.RunID).Scan(
		&users, &torrentsCount, &objects, &skippedTorrents, &skippedObjects,
		&mappings, &published, &outbox,
	); err != nil {
		return fmt.Errorf("count finalized legacy torrent import: %w", err)
	}
	var expectedUsers int64
	if err := tx.QueryRow(ctx, `SELECT expected_user_rows FROM migration.runs WHERE id = $1 FOR UPDATE`, config.Inventory.RunID).Scan(&expectedUsers); err != nil {
		return fmt.Errorf("lock legacy migration run for torrent finalization: %w", err)
	}
	if users != expectedUsers || torrentsCount != result.ExpectedTorrents || objects != result.ExpectedTorrents ||
		skippedTorrents != result.ExcludedTorrents || skippedObjects != result.ExcludedTorrents ||
		mappings != result.ExpectedTorrents || published != result.PublishedTorrents || outbox != result.PublishedTorrents {
		return errors.New("legacy torrent import cannot finalize because target counts do not reconcile")
	}
	if _, err := tx.Exec(ctx, `SELECT setval(
    pg_get_serial_sequence('torrents.torrents', 'id'),
    (SELECT max(id) FROM torrents.torrents),
    true
)`); err != nil {
		return fmt.Errorf("advance post-import torrent identity sequence: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT setval(
    pg_get_serial_sequence('torrents.resource_groups', 'id'),
    (SELECT max(id) FROM torrents.resource_groups),
    true
)`); err != nil {
		return fmt.Errorf("advance post-import resource group identity sequence: %w", err)
	}
	updated, err := tx.Exec(ctx, `
UPDATE migration.runs
SET state = 'imported',
    version = version + 1,
    state_changed_at = $1
WHERE id = $2 AND state = 'importing'`, config.OccurredAt, config.Inventory.RunID)
	if err != nil {
		return fmt.Errorf("mark legacy torrent import complete: %w", err)
	}
	if updated.RowsAffected() == 0 {
		var state string
		if err := tx.QueryRow(ctx, `SELECT state FROM migration.runs WHERE id = $1`, config.Inventory.RunID).Scan(&state); err != nil {
			return fmt.Errorf("read completed legacy migration run: %w", err)
		}
		if state != "imported" && state != "reconciled" {
			return errors.New("legacy migration run did not reach imported state")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit legacy torrent import finalization: %w", err)
	}
	return nil
}

func sortStrings(values []string) {
	for index := 1; index < len(values); index++ {
		for position := index; position > 0 && values[position] < values[position-1]; position-- {
			values[position], values[position-1] = values[position-1], values[position]
		}
	}
}
