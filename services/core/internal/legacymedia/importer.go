package legacymedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

var legacyImageObjectNamespace = uuid.MustParse("f2a9be51-2cbb-4e9c-83a1-19f70ee196dc")

type ImportConfig struct {
	Inventory     InventoryConfig
	ImageArchive  string
	ArchiveSHA256 [sha256.Size]byte
	OccurredAt    time.Time
	BackendID     objectstorage.BackendID
	ProgressEvery int64
	VerifyOnly    bool
}

type ImportProgress struct {
	Processed int64
	Expected  int64
}

type ImportResult struct {
	RunID          uuid.UUID
	ExpectedImages int64
	ImportedImages int64
	VerifiedImages int64
	SkippedImages  int64
	StoredBytes    int64
	Transformed    int64
	ReusedOriginal int64
}

type checkpointState struct {
	fingerprint [sha256.Size]byte
	state       string
	errorCode   string
}

// Import writes each content-addressed original object without overwrite,
// completely reads it back, then commits object metadata, attachment, legacy
// alias, mapping and checkpoint in one Core transaction. A database failure
// can leave only a reusable immutable object, never a half-linked image row.
// WebP display variants are created later by the shared image worker; migration
// never replaces the only retained copy with a lossy transformation.
func Import(
	ctx context.Context,
	source *pgxpool.Pool,
	core *pgxpool.Pool,
	stores *objectstorage.Registry,
	config ImportConfig,
	progress func(ImportProgress),
) (ImportResult, error) {
	config.ImageArchive = strings.TrimSpace(config.ImageArchive)
	config.OccurredAt = config.OccurredAt.UTC().Truncate(time.Microsecond)
	if ctx == nil || source == nil || core == nil || stores == nil ||
		config.Inventory.RunID == uuid.Nil || config.Inventory.SnapshotSHA256 == ([sha256.Size]byte{}) ||
		strings.TrimSpace(config.Inventory.MappingVersion) == "" || config.ImageArchive == "" ||
		config.ArchiveSHA256 == ([sha256.Size]byte{}) || config.OccurredAt.IsZero() || config.BackendID == "" {
		return ImportResult{}, errors.New("legacy image import configuration is invalid")
	}
	if err := platformpostgres.RequireCurrentMigration(ctx, core); err != nil {
		return ImportResult{}, err
	}
	store, exists := stores.Get(config.BackendID)
	if !exists {
		return ImportResult{}, errors.New("legacy image destination backend is not configured")
	}
	if config.ProgressEvery < 1 {
		config.ProgressEvery = 250
	}
	if progress == nil {
		progress = func(ImportProgress) {}
	}
	archive, err := OpenSourceArchive(config.ImageArchive)
	if err != nil {
		return ImportResult{}, err
	}
	defer archive.Close()
	if archive.Inspection().SHA256 != config.ArchiveSHA256 {
		return ImportResult{}, errors.New("PtYes image ZIP does not match the expected SHA-256")
	}
	expectedImages, err := verifyImportReadiness(ctx, core, config, archive.Inspection())
	if err != nil {
		return ImportResult{}, err
	}
	checkpoints, err := loadImageCheckpoints(ctx, core, config.Inventory.RunID)
	if err != nil {
		return ImportResult{}, err
	}
	images, err := loadSourceImages(ctx, source)
	if err != nil {
		return ImportResult{}, err
	}
	posters, err := loadPosterOnlyImages(ctx, source)
	if err != nil {
		return ImportResult{}, err
	}
	images = append(images, posters...)
	if len(checkpoints) != len(images) {
		return ImportResult{}, errors.New("legacy image checkpoints do not cover the current source snapshot")
	}
	result := ImportResult{RunID: config.Inventory.RunID, ExpectedImages: expectedImages}
	for index := range images {
		image := &images[index]
		identity := imageIdentity{kind: image.EntityKind, legacyID: image.LegacyID}
		checkpoint, exists := checkpoints[identity]
		if !exists {
			return ImportResult{}, errors.New("legacy image checkpoint is missing")
		}
		if checkpoint.state == "skipped" {
			if checkpoint.errorCode != "poster_source_missing_placeholder" &&
				checkpoint.errorCode != "torrent_explicitly_excluded" {
				return ImportResult{}, errors.New("legacy image checkpoint has an unsupported skip reason")
			}
			result.SkippedImages++
			reportImportProgress(progress, int64(index+1), int64(len(images)), config.ProgressEvery)
			continue
		}
		raw, extension, err := archive.Read(ctx, image.LegacyPath)
		if err != nil {
			return ImportResult{}, fmt.Errorf("read validated legacy image %d: %w", image.LegacyID, err)
		}
		metadata, err := ValidateSourceImage(raw, extension)
		if err != nil {
			return ImportResult{}, fmt.Errorf("revalidate legacy image %d: %w", image.LegacyID, err)
		}
		image.OriginalSHA256 = sha256.Sum256(raw)
		image.OriginalBytes = int64(len(raw))
		image.SourceMetadata = metadata
		image.State = "validated"
		fingerprint, err := image.fingerprint()
		if err != nil || fingerprint != checkpoint.fingerprint {
			return ImportResult{}, errors.New("legacy image source changed after validation")
		}
		if checkpoint.state == "imported" {
			storedBytes, err := verifyImportedImage(ctx, core, store, config, *image)
			if err != nil {
				return ImportResult{}, err
			}
			result.VerifiedImages++
			result.StoredBytes += storedBytes
			reportImportProgress(progress, int64(index+1), int64(len(images)), config.ProgressEvery)
			continue
		}
		if checkpoint.state != "validated" {
			return ImportResult{}, errors.New("legacy image checkpoint is not importable")
		}
		if config.VerifyOnly {
			return ImportResult{}, errors.New("legacy image verify-only pass found an unimported checkpoint")
		}
		original, err := buildStoredImage(raw, metadata)
		if err != nil {
			return ImportResult{}, fmt.Errorf("prepare legacy image original %d: %w", image.LegacyID, err)
		}
		key := torrents.TorrentScreenshotObjectKey(
			torrents.ObjectSHA256(original.Descriptor.SHA256),
			original.Metadata.Extension,
		)
		writeResult, err := store.PutIfAbsent(ctx, key, bytes.NewReader(original.Bytes), original.Descriptor)
		if err != nil {
			return ImportResult{}, fmt.Errorf("write legacy image %d: %w", image.LegacyID, err)
		}
		opened, err := store.Open(ctx, key, writeResult.VersionID)
		if err != nil || opened.Body == nil {
			return ImportResult{}, fmt.Errorf("open legacy image %d after write", image.LegacyID)
		}
		verified, verifyErr := objectstorage.ReadAllVerified(opened, original.Descriptor)
		closeErr := opened.Body.Close()
		if verifyErr != nil || closeErr != nil || !bytes.Equal(verified, original.Bytes) {
			return ImportResult{}, fmt.Errorf("verify legacy image %d after write", image.LegacyID)
		}
		versionID := opened.VersionID
		if versionID == "" {
			versionID = writeResult.VersionID
		}
		if err := persistImportedImage(
			ctx, core, config, *image, fingerprint, original, key, versionID,
		); err != nil {
			return ImportResult{}, err
		}
		result.ImportedImages++
		result.StoredBytes += original.Descriptor.ByteLength
		result.ReusedOriginal++
		reportImportProgress(progress, int64(index+1), int64(len(images)), config.ProgressEvery)
	}
	progress(ImportProgress{Processed: int64(len(images)), Expected: int64(len(images))})
	if result.ImportedImages+result.VerifiedImages != result.ExpectedImages {
		return ImportResult{}, errors.New("legacy image import count does not match validated manifest")
	}
	return result, nil
}

type imageIdentity struct {
	kind     string
	legacyID int64
}

func loadImageCheckpoints(ctx context.Context, core *pgxpool.Pool, runID uuid.UUID) (map[imageIdentity]checkpointState, error) {
	rows, err := core.Query(ctx, `
SELECT entity_kind, legacy_id, source_fingerprint, state, COALESCE(error_code, '')
FROM migration.source_rows
WHERE run_id = $1 AND entity_kind IN ('torrent_image', 'torrent_poster')
ORDER BY entity_kind, legacy_id`, runID)
	if err != nil {
		return nil, fmt.Errorf("query legacy image checkpoints: %w", err)
	}
	defer rows.Close()
	result := make(map[imageIdentity]checkpointState)
	for rows.Next() {
		var identity imageIdentity
		var fingerprint []byte
		var state checkpointState
		if err := rows.Scan(&identity.kind, &identity.legacyID, &fingerprint, &state.state, &state.errorCode); err != nil {
			return nil, fmt.Errorf("scan legacy image checkpoint: %w", err)
		}
		if len(fingerprint) != sha256.Size {
			return nil, errors.New("legacy image checkpoint fingerprint is invalid")
		}
		copy(state.fingerprint[:], fingerprint)
		result[identity] = state
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read legacy image checkpoints: %w", err)
	}
	return result, nil
}

func verifyImportReadiness(
	ctx context.Context,
	core *pgxpool.Pool,
	config ImportConfig,
	inspection ArchiveInspection,
) (int64, error) {
	var snapshot []byte
	var mappingVersion, state string
	var archiveArtifacts, manifestArtifacts, expectedImages int64
	if err := core.QueryRow(ctx, `
SELECT
    run.source_snapshot_sha256,
    run.mapping_version,
    run.state,
    (SELECT count(*)::bigint
     FROM migration.run_artifacts
     WHERE run_id = run.id AND kind = 'image_archive'
       AND content_sha256 = $2 AND byte_length = $3 AND item_count = $4),
    (SELECT count(*)::bigint
     FROM migration.run_artifacts
     WHERE run_id = run.id AND kind = 'image_manifest'),
    COALESCE((SELECT item_count
     FROM migration.run_artifacts
     WHERE run_id = run.id AND kind = 'image_manifest'), 0)::bigint
FROM migration.runs AS run
WHERE run.id = $1`, config.Inventory.RunID, inspection.SHA256[:], inspection.ByteLength, inspection.ImageCount).Scan(
		&snapshot, &mappingVersion, &state, &archiveArtifacts, &manifestArtifacts, &expectedImages,
	); err != nil {
		return 0, fmt.Errorf("read legacy image import readiness: %w", err)
	}
	if !bytes.Equal(snapshot, config.Inventory.SnapshotSHA256[:]) ||
		mappingVersion != config.Inventory.MappingVersion || (state != "imported" && state != "reconciled") ||
		archiveArtifacts != 1 || manifestArtifacts != 1 || expectedImages < 1 {
		return 0, errors.New("legacy image migration run is not ready for import")
	}
	return expectedImages, nil
}

func persistImportedImage(
	ctx context.Context,
	core *pgxpool.Pool,
	config ImportConfig,
	image SourceImage,
	fingerprint [sha256.Size]byte,
	stored StoredImage,
	key objectstorage.Key,
	versionID string,
) error {
	tx, err := core.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin legacy image target transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var checkpointFingerprint []byte
	var checkpointState string
	if err := tx.QueryRow(ctx, `
SELECT source_fingerprint, state
FROM migration.source_rows
WHERE run_id = $1 AND entity_kind = $2 AND legacy_id = $3
FOR UPDATE`, config.Inventory.RunID, image.EntityKind, image.LegacyID).Scan(&checkpointFingerprint, &checkpointState); err != nil {
		return fmt.Errorf("lock legacy image checkpoint: %w", err)
	}
	if !bytes.Equal(checkpointFingerprint, fingerprint[:]) {
		return errors.New("legacy image checkpoint fingerprint changed")
	}
	if checkpointState == "imported" {
		return tx.Commit(ctx)
	}
	if checkpointState != "validated" {
		return errors.New("legacy image checkpoint cannot transition to imported")
	}
	var torrentID int64
	if err := tx.QueryRow(ctx, `
SELECT torrent_id
FROM migration.torrent_id_map
WHERE source_system = 'ptyes' AND legacy_torrent_id = $1`, image.LegacyTorrentID).Scan(&torrentID); err != nil {
		return fmt.Errorf("resolve legacy image torrent mapping: %w", err)
	}
	objectID, err := resolveImageObject(ctx, tx, config, stored)
	if err != nil {
		return err
	}
	if err := resolveImageLocation(ctx, tx, config, objectID, stored.Descriptor, key, versionID); err != nil {
		return err
	}
	if err := attachImage(ctx, tx, config, image, torrentID, objectID); err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `
UPDATE migration.source_rows
SET state = 'imported', attempt_count = attempt_count + 1,
    version = version + 1, updated_at = $1, error_code = NULL
WHERE run_id = $2 AND entity_kind = $3 AND legacy_id = $4
  AND source_fingerprint = $5 AND state = 'validated'`,
		config.OccurredAt, config.Inventory.RunID, image.EntityKind, image.LegacyID, fingerprint[:],
	)
	if err != nil || command.RowsAffected() != 1 {
		return errors.New("advance legacy image checkpoint to imported")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit legacy image target transaction: %w", err)
	}
	return nil
}

func resolveImageObject(
	ctx context.Context,
	tx pgx.Tx,
	config ImportConfig,
	image StoredImage,
) (uuid.UUID, error) {
	objectID := uuid.NewSHA1(legacyImageObjectNamespace, image.Descriptor.SHA256[:])
	if _, err := tx.Exec(ctx, `
INSERT INTO torrents.torrent_screenshot_objects (
    id, content_sha256, byte_length, content_type, width, height, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (content_sha256) DO NOTHING`, objectID, image.Descriptor.SHA256[:],
		image.Descriptor.ByteLength, image.Metadata.ContentType, image.Metadata.Width, image.Metadata.Height,
		config.OccurredAt,
	); err != nil {
		return uuid.Nil, fmt.Errorf("insert legacy image object: %w", err)
	}
	var resolved uuid.UUID
	var byteLength int64
	var contentType string
	var width, height int
	if err := tx.QueryRow(ctx, `
SELECT id, byte_length, content_type, width, height
FROM torrents.torrent_screenshot_objects
WHERE content_sha256 = $1`, image.Descriptor.SHA256[:]).Scan(
		&resolved, &byteLength, &contentType, &width, &height,
	); err != nil {
		return uuid.Nil, fmt.Errorf("resolve legacy image object: %w", err)
	}
	if byteLength != image.Descriptor.ByteLength || contentType != image.Metadata.ContentType ||
		width != image.Metadata.Width || height != image.Metadata.Height {
		return uuid.Nil, errors.New("legacy image object conflicts with immutable metadata")
	}
	return resolved, nil
}

func resolveImageLocation(
	ctx context.Context,
	tx pgx.Tx,
	config ImportConfig,
	objectID uuid.UUID,
	descriptor objectstorage.Descriptor,
	key objectstorage.Key,
	versionID string,
) error {
	locationID := uuid.NewSHA1(objectID, []byte(config.BackendID))
	var nullableVersion any
	if versionID != "" {
		nullableVersion = versionID
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO torrents.torrent_screenshot_object_locations (
    id, object_id, backend_id, object_key, version_id,
    observed_byte_length, observed_sha256, verified_at, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
ON CONFLICT (object_id, backend_id) DO NOTHING`,
		locationID, objectID, string(config.BackendID), string(key), nullableVersion,
		descriptor.ByteLength, descriptor.SHA256[:], config.OccurredAt,
	); err != nil {
		return fmt.Errorf("insert legacy image object location: %w", err)
	}
	var storedKey string
	var storedVersion *string
	var byteLength int64
	var digest []byte
	if err := tx.QueryRow(ctx, `
SELECT object_key, version_id, observed_byte_length, observed_sha256
FROM torrents.torrent_screenshot_object_locations
WHERE object_id = $1 AND backend_id = $2`, objectID, string(config.BackendID)).Scan(
		&storedKey, &storedVersion, &byteLength, &digest,
	); err != nil {
		return fmt.Errorf("resolve legacy image object location: %w", err)
	}
	actualVersion := ""
	if storedVersion != nil {
		actualVersion = *storedVersion
	}
	if storedKey != string(key) || actualVersion != versionID || byteLength != descriptor.ByteLength ||
		!bytes.Equal(digest, descriptor.SHA256[:]) {
		return errors.New("legacy image object location conflicts with verified bytes")
	}
	return nil
}

func attachImage(
	ctx context.Context,
	tx pgx.Tx,
	config ImportConfig,
	image SourceImage,
	torrentID int64,
	objectID uuid.UUID,
) error {
	// PtYes permits two different gallery rows (and paths) to contain identical
	// bytes. PeerGo deliberately stores one visual only once per torrent. Let
	// either uniqueness rule win, then bind every legacy row to the canonical
	// attachment position so its compatibility alias and audit mapping remain
	// complete without rendering duplicate screenshots.
	if _, err := tx.Exec(ctx, `
INSERT INTO torrents.torrent_screenshots (torrent_id, object_id, position, created_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT DO NOTHING`, torrentID, objectID, image.Position, config.OccurredAt); err != nil {
		return fmt.Errorf("attach legacy torrent image: %w", err)
	}
	var attachedObject uuid.UUID
	var attachedPosition int16
	if err := tx.QueryRow(ctx, `
SELECT object_id, position FROM torrents.torrent_screenshots
WHERE torrent_id = $1 AND object_id = $2`, torrentID, objectID).Scan(&attachedObject, &attachedPosition); err != nil ||
		attachedObject != objectID {
		return errors.New("legacy torrent image attachment conflicts with existing object or position")
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO migration.torrent_image_map (
    source_system, entity_kind, legacy_id, torrent_id, object_id, position,
    legacy_path, original_sha256, first_run_id, created_at
) VALUES ('ptyes', $1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (source_system, entity_kind, legacy_id) DO NOTHING`,
		image.EntityKind, image.LegacyID, torrentID, objectID, attachedPosition,
		image.LegacyPath, image.OriginalSHA256[:], config.Inventory.RunID, config.OccurredAt,
	); err != nil {
		return fmt.Errorf("insert legacy torrent image mapping: %w", err)
	}
	var mappedTorrent int64
	var mappedObject uuid.UUID
	var mappedPosition int16
	var mappedPath string
	var mappedOriginal []byte
	var mappedRun uuid.UUID
	if err := tx.QueryRow(ctx, `
SELECT torrent_id, object_id, position, legacy_path, original_sha256, first_run_id
FROM migration.torrent_image_map
WHERE source_system = 'ptyes' AND entity_kind = $1 AND legacy_id = $2`, image.EntityKind, image.LegacyID).Scan(
		&mappedTorrent, &mappedObject, &mappedPosition, &mappedPath, &mappedOriginal, &mappedRun,
	); err != nil || mappedTorrent != torrentID || mappedObject != objectID || mappedPosition != attachedPosition ||
		mappedPath != image.LegacyPath || !bytes.Equal(mappedOriginal, image.OriginalSHA256[:]) || mappedRun != config.Inventory.RunID {
		return errors.New("legacy torrent image mapping conflicts with imported object")
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO torrents.legacy_image_aliases (
    legacy_path, object_id, first_run_id, original_sha256, original_byte_length, created_at
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (legacy_path) DO NOTHING`, image.LegacyPath, objectID, config.Inventory.RunID,
		image.OriginalSHA256[:], image.OriginalBytes, config.OccurredAt,
	); err != nil {
		return fmt.Errorf("insert legacy image path alias: %w", err)
	}
	var aliasedObject uuid.UUID
	var aliasRun uuid.UUID
	var aliasDigest []byte
	var aliasBytes int64
	if err := tx.QueryRow(ctx, `
SELECT object_id, first_run_id, original_sha256, original_byte_length
FROM torrents.legacy_image_aliases
WHERE legacy_path = $1`, image.LegacyPath).Scan(&aliasedObject, &aliasRun, &aliasDigest, &aliasBytes); err != nil ||
		aliasedObject != objectID || aliasRun != config.Inventory.RunID ||
		!bytes.Equal(aliasDigest, image.OriginalSHA256[:]) || aliasBytes != image.OriginalBytes {
		return errors.New("legacy image path alias conflicts with imported object")
	}
	return nil
}

func verifyImportedImage(
	ctx context.Context,
	core *pgxpool.Pool,
	store objectstorage.Store,
	config ImportConfig,
	image SourceImage,
) (int64, error) {
	var digest []byte
	var byteLength int64
	var contentType, key, versionID string
	var width, height int
	if err := core.QueryRow(ctx, `
SELECT object.content_sha256, object.byte_length, object.content_type,
       object.width, object.height, location.object_key, COALESCE(location.version_id, '')
FROM migration.torrent_image_map AS mapping
JOIN torrents.torrent_screenshots AS attachment
  ON attachment.torrent_id = mapping.torrent_id
 AND attachment.object_id = mapping.object_id
 AND attachment.position = mapping.position
JOIN torrents.torrent_screenshot_objects AS object ON object.id = mapping.object_id
JOIN torrents.torrent_screenshot_object_locations AS location
  ON location.object_id = object.id AND location.backend_id = $4
JOIN torrents.legacy_image_aliases AS alias
  ON alias.legacy_path = mapping.legacy_path AND alias.object_id = mapping.object_id
WHERE mapping.source_system = 'ptyes'
  AND mapping.entity_kind = $1 AND mapping.legacy_id = $2
  AND mapping.first_run_id = $3
  AND mapping.legacy_path = $5 AND mapping.original_sha256 = $6`,
		image.EntityKind, image.LegacyID, config.Inventory.RunID, string(config.BackendID),
		image.LegacyPath, image.OriginalSHA256[:],
	).Scan(&digest, &byteLength, &contentType, &width, &height, &key, &versionID); err != nil {
		return 0, fmt.Errorf("read imported legacy image target %d: %w", image.LegacyID, err)
	}
	if len(digest) != sha256.Size || byteLength < 1 || width < 1 || height < 1 {
		return 0, errors.New("imported legacy image target metadata is invalid")
	}
	var objectDigest objectstorage.SHA256
	copy(objectDigest[:], digest)
	descriptor := objectstorage.Descriptor{SHA256: objectDigest, ByteLength: byteLength}
	parsedKey, err := objectstorage.ParseKey(key)
	if err != nil {
		return 0, errors.New("imported legacy image object key is invalid")
	}
	extension := extensionForContentType(contentType)
	if extension == "" || parsedKey != torrents.TorrentScreenshotObjectKey(torrents.ObjectSHA256(objectDigest), extension) {
		return 0, errors.New("imported legacy image object key conflicts with content type")
	}
	opened, err := store.Open(ctx, parsedKey, versionID)
	if err != nil || opened.Body == nil {
		return 0, errors.New("open imported legacy image object")
	}
	_, verifyErr := objectstorage.ReadAllVerified(opened, descriptor)
	closeErr := opened.Body.Close()
	if verifyErr != nil || closeErr != nil {
		return 0, errors.New("verify imported legacy image object")
	}
	return byteLength, nil
}

func extensionForContentType(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ""
	}
}

func reportImportProgress(progress func(ImportProgress), processed, expected, every int64) {
	if processed%every == 0 || processed == expected {
		progress(ImportProgress{Processed: processed, Expected: expected})
	}
}
