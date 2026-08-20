package legacymedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

type sourceTorrentState struct {
	state     string
	errorCode string
}

// Validate performs the expensive source pass without writing image objects:
// every referenced ZIP entry is fully read (therefore CRC-checked), decoded,
// hashed and committed to one canonical manifest. Missing gallery rows block;
// a poster-only path uses the reviewed placeholder rule and remains auditable.
func Validate(
	ctx context.Context,
	source *pgxpool.Pool,
	core *pgxpool.Pool,
	config ValidationConfig,
	progress func(ValidationProgress),
) (ValidationResult, error) {
	config.OccurredAt = config.OccurredAt.UTC().Truncate(time.Microsecond)
	config.ImageArchive = strings.TrimSpace(config.ImageArchive)
	config.Inventory.MappingVersion = strings.TrimSpace(config.Inventory.MappingVersion)
	if ctx == nil || source == nil || core == nil || config.Inventory.RunID == uuid.Nil ||
		config.Inventory.SnapshotSHA256 == ([sha256.Size]byte{}) || config.Inventory.MappingVersion == "" ||
		config.ImageArchive == "" || config.ArchiveSHA256 == ([sha256.Size]byte{}) || config.OccurredAt.IsZero() {
		return ValidationResult{}, errors.New("legacy image validation configuration is invalid")
	}
	if err := platformpostgres.RequireCurrentMigration(ctx, core); err != nil {
		return ValidationResult{}, err
	}
	if config.ProgressEvery < 1 {
		config.ProgressEvery = 250
	}
	if progress == nil {
		progress = func(ValidationProgress) {}
	}
	archive, err := OpenSourceArchive(config.ImageArchive)
	if err != nil {
		return ValidationResult{}, err
	}
	defer archive.Close()
	inspection := archive.Inspection()
	if inspection.SHA256 != config.ArchiveSHA256 {
		return ValidationResult{}, errors.New("PtYes image ZIP does not match the expected SHA-256")
	}
	torrentStates, err := loadTorrentStates(ctx, core, config.Inventory)
	if err != nil {
		return ValidationResult{}, err
	}
	images, err := loadSourceImages(ctx, source)
	if err != nil {
		return ValidationResult{}, err
	}
	posters, err := loadPosterOnlyImages(ctx, source)
	if err != nil {
		return ValidationResult{}, err
	}
	images = append(images, posters...)
	result := ValidationResult{RunID: config.Inventory.RunID, ArchiveImages: inspection.ImageCount}
	result.ReferencedImages = int64(len(images))
	manifest := sha256.New()
	_, _ = manifest.Write([]byte(imageManifestDomain))
	_, _ = manifest.Write(config.ArchiveSHA256[:])
	writeString(manifest, TransformPolicyVersion)
	validated := make([]SourceImage, 0, len(images))
	seenPaths := make(map[string]struct{}, len(images))
	var missingImageRows int64
	for index := range images {
		image := &images[index]
		state, exists := torrentStates[image.LegacyTorrentID]
		if !exists {
			return ValidationResult{}, fmt.Errorf("legacy torrent image %d has no run-scoped torrent checkpoint", image.LegacyID)
		}
		if state.state == "skipped" && state.errorCode == "object_missing_explicitly_excluded" {
			image.State = "skipped"
			image.ErrorCode = "torrent_explicitly_excluded"
			result.ExcludedTorrentImages++
			fingerprint, fingerprintErr := image.fingerprint()
			if fingerprintErr != nil {
				return ValidationResult{}, fingerprintErr
			}
			writeManifestImage(manifest, *image, fingerprint)
			validated = append(validated, *image)
			continue
		}
		if state.state != "imported" {
			return ValidationResult{}, errors.New("legacy image validation requires completed torrent import checkpoints")
		}
		raw, extension, readErr := archive.Read(ctx, image.LegacyPath)
		if errors.Is(readErr, os.ErrNotExist) && image.EntityKind == EntityTorrentPoster {
			image.State = "skipped"
			image.ErrorCode = "poster_source_missing_placeholder"
			result.MissingPosterPlaceholders++
			fingerprint, fingerprintErr := image.fingerprint()
			if fingerprintErr != nil {
				return ValidationResult{}, fingerprintErr
			}
			writeManifestImage(manifest, *image, fingerprint)
			validated = append(validated, *image)
			continue
		}
		if readErr != nil {
			missingImageRows++
			result.MissingImageLegacyIDs = appendBoundedID(result.MissingImageLegacyIDs, image.LegacyID)
			continue
		}
		metadata, inspectErr := ValidateSourceImage(raw, extension)
		if inspectErr != nil {
			missingImageRows++
			result.MissingImageLegacyIDs = appendBoundedID(result.MissingImageLegacyIDs, image.LegacyID)
			continue
		}
		image.OriginalSHA256 = sha256.Sum256(raw)
		image.OriginalBytes = int64(len(raw))
		image.SourceMetadata = metadata
		image.State = "validated"
		if result.OriginalBytes > math.MaxInt64-image.OriginalBytes {
			return ValidationResult{}, errors.New("legacy image byte total exceeds bigint")
		}
		result.OriginalBytes += image.OriginalBytes
		result.ImportableImages++
		seenPaths[image.LegacyPath] = struct{}{}
		fingerprint, fingerprintErr := image.fingerprint()
		if fingerprintErr != nil {
			return ValidationResult{}, fingerprintErr
		}
		writeManifestImage(manifest, *image, fingerprint)
		validated = append(validated, *image)
		processed := int64(index + 1)
		if processed%config.ProgressEvery == 0 {
			progress(ValidationProgress{Processed: processed, Expected: int64(len(images))})
		}
	}
	progress(ValidationProgress{Processed: int64(len(images)), Expected: int64(len(images))})
	if missingImageRows != 0 {
		return result, &ValidationFailure{
			MissingImageRows: missingImageRows,
			LegacyIDs:        append([]int64(nil), result.MissingImageLegacyIDs...),
		}
	}
	result.UnreferencedArchiveImages = inspection.ImageCount - int64(len(seenPaths))
	copy(result.ManifestSHA256[:], manifest.Sum(nil))
	if err := persistValidation(ctx, core, config, result, inspection, validated); err != nil {
		return ValidationResult{}, err
	}
	return result, nil
}

func loadTorrentStates(ctx context.Context, core *pgxpool.Pool, inventory InventoryConfig) (map[int64]sourceTorrentState, error) {
	var snapshot []byte
	var mappingVersion, runState string
	if err := core.QueryRow(ctx, `
SELECT source_snapshot_sha256, mapping_version, state
FROM migration.runs
WHERE id = $1`, inventory.RunID).Scan(&snapshot, &mappingVersion, &runState); err != nil {
		return nil, fmt.Errorf("read legacy image migration run: %w", err)
	}
	if !bytes.Equal(snapshot, inventory.SnapshotSHA256[:]) || mappingVersion != inventory.MappingVersion ||
		(runState != "imported" && runState != "reconciled") {
		return nil, errors.New("legacy image migration run identity or state is invalid")
	}
	rows, err := core.Query(ctx, `
SELECT legacy_id, state, COALESCE(error_code, '')
FROM migration.source_rows
WHERE run_id = $1 AND entity_kind = 'torrent'
ORDER BY legacy_id`, inventory.RunID)
	if err != nil {
		return nil, fmt.Errorf("query run-scoped torrent checkpoints for images: %w", err)
	}
	defer rows.Close()
	result := make(map[int64]sourceTorrentState)
	for rows.Next() {
		var legacyID int64
		var state sourceTorrentState
		if err := rows.Scan(&legacyID, &state.state, &state.errorCode); err != nil {
			return nil, fmt.Errorf("scan run-scoped torrent checkpoint for images: %w", err)
		}
		result[legacyID] = state
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read run-scoped torrent checkpoints for images: %w", err)
	}
	return result, nil
}

func loadSourceImages(ctx context.Context, source *pgxpool.Pool) ([]SourceImage, error) {
	rows, err := source.Query(ctx, `
SELECT id, torrent_id, url, COALESCE(is_cover, false), COALESCE(sort_order, 0)
FROM torrent_images
ORDER BY torrent_id, COALESCE(is_cover, false) DESC, COALESCE(sort_order, 0), id`)
	if err != nil {
		return nil, fmt.Errorf("query PtYes torrent images: %w", err)
	}
	defer rows.Close()
	result := make([]SourceImage, 0, 40_000)
	positions := make(map[int64]int16)
	covers := make(map[int64]int)
	seenRows := make(map[int64]struct{})
	seenTorrentPaths := make(map[string]struct{})
	for rows.Next() {
		var image SourceImage
		if err := rows.Scan(&image.LegacyID, &image.LegacyTorrentID, &image.LegacyPath, &image.IsCover, &image.SortOrder); err != nil {
			return nil, fmt.Errorf("scan PtYes torrent image: %w", err)
		}
		image.EntityKind = EntityTorrentImage
		image.LegacyPath = strings.TrimSpace(image.LegacyPath)
		image.Position = positions[image.LegacyTorrentID]
		positions[image.LegacyTorrentID]++
		if image.IsCover {
			covers[image.LegacyTorrentID]++
		}
		key := fmt.Sprintf("%d\x00%s", image.LegacyTorrentID, image.LegacyPath)
		if image.LegacyID < 1 || image.LegacyTorrentID < 1 || image.Position > 5 ||
			!validLegacyImagePath(image.LegacyPath) || image.IsCover != (image.Position == 0) {
			return nil, errors.New("PtYes torrent image ordering or path is invalid")
		}
		if _, duplicate := seenRows[image.LegacyID]; duplicate {
			return nil, errors.New("PtYes torrent image ID is duplicated")
		}
		if _, duplicate := seenTorrentPaths[key]; duplicate {
			return nil, errors.New("PtYes torrent contains a duplicate image path")
		}
		seenRows[image.LegacyID] = struct{}{}
		seenTorrentPaths[key] = struct{}{}
		result = append(result, image)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read PtYes torrent images: %w", err)
	}
	for torrentID, count := range positions {
		if count < 1 || count > 6 || covers[torrentID] != 1 {
			return nil, errors.New("PtYes torrent image group is invalid")
		}
	}
	return result, nil
}

func loadPosterOnlyImages(ctx context.Context, source *pgxpool.Pool) ([]SourceImage, error) {
	rows, err := source.Query(ctx, `
SELECT torrent.id, torrent.poster
FROM torrents AS torrent
WHERE torrent.poster IS NOT NULL
  AND torrent.poster <> ''
  AND NOT EXISTS (
      SELECT 1 FROM torrent_images AS image WHERE image.torrent_id = torrent.id
  )
ORDER BY torrent.id`)
	if err != nil {
		return nil, fmt.Errorf("query PtYes poster-only images: %w", err)
	}
	defer rows.Close()
	result := make([]SourceImage, 0)
	for rows.Next() {
		var image SourceImage
		if err := rows.Scan(&image.LegacyID, &image.LegacyPath); err != nil {
			return nil, fmt.Errorf("scan PtYes poster-only image: %w", err)
		}
		image.EntityKind = EntityTorrentPoster
		image.LegacyTorrentID = image.LegacyID
		image.LegacyPath = strings.TrimSpace(image.LegacyPath)
		image.IsCover = true
		if !validLegacyImagePath(image.LegacyPath) {
			continue
		}
		result = append(result, image)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read PtYes poster-only images: %w", err)
	}
	return result, nil
}

func validLegacyImagePath(value string) bool {
	legacyPath, _, ok := parseImageObjectName(strings.TrimPrefix(value, "/"))
	return ok && legacyPath == value
}

func writeManifestImage(manifest byteWriter, image SourceImage, fingerprint [sha256.Size]byte) {
	writeString(manifest, image.EntityKind)
	writeInt64(manifest, image.LegacyID)
	writeInt64(manifest, image.LegacyTorrentID)
	writeString(manifest, image.LegacyPath)
	_, _ = manifest.Write(fingerprint[:])
}

func appendBoundedID(values []int64, legacyID int64) []int64 {
	if len(values) < 100 {
		return append(values, legacyID)
	}
	return values
}

func persistValidation(
	ctx context.Context,
	core *pgxpool.Pool,
	config ValidationConfig,
	result ValidationResult,
	inspection ArchiveInspection,
	images []SourceImage,
) error {
	tx, err := core.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin legacy image validation persistence: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
CREATE TEMP TABLE legacy_image_validation_stage (
    entity_kind text NOT NULL,
    legacy_id bigint NOT NULL,
    source_fingerprint bytea NOT NULL,
    state text NOT NULL,
    error_code text
) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("create legacy image validation stage: %w", err)
	}
	rows := make([][]any, 0, len(images))
	for _, image := range images {
		fingerprint, fingerprintErr := image.fingerprint()
		if fingerprintErr != nil {
			return fingerprintErr
		}
		var errorCode any
		if image.ErrorCode != "" {
			errorCode = image.ErrorCode
		}
		rows = append(rows, []any{image.EntityKind, image.LegacyID, fingerprint[:], image.State, errorCode})
	}
	if _, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"legacy_image_validation_stage"},
		[]string{"entity_kind", "legacy_id", "source_fingerprint", "state", "error_code"},
		pgx.CopyFromRows(rows),
	); err != nil {
		return fmt.Errorf("stage legacy image validation rows: %w", err)
	}
	var duplicateStage int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint - count(DISTINCT (entity_kind, legacy_id))::bigint
FROM legacy_image_validation_stage`).Scan(&duplicateStage); err != nil || duplicateStage != 0 {
		return errors.New("legacy image validation stage contains duplicate identities")
	}
	var conflicts int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM legacy_image_validation_stage AS staged
JOIN migration.source_rows AS existing
  ON existing.run_id = $1
 AND existing.entity_kind = staged.entity_kind
 AND existing.legacy_id = staged.legacy_id
WHERE existing.source_fingerprint <> staged.source_fingerprint
   OR existing.fingerprint_scheme <> 'sha256-v1'
   OR existing.state NOT IN ('validated', 'imported', 'skipped')
   OR COALESCE(existing.error_code, '') <> COALESCE(staged.error_code, '')`, config.Inventory.RunID).Scan(&conflicts); err != nil {
		return fmt.Errorf("check existing legacy image validation rows: %w", err)
	}
	if conflicts != 0 {
		return errors.New("existing legacy image checkpoints conflict with the immutable snapshot")
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO migration.source_rows (
    run_id, entity_kind, legacy_id, source_fingerprint, fingerprint_scheme,
    state, attempt_count, error_code, version, created_at, updated_at
)
SELECT $1, staged.entity_kind, staged.legacy_id, staged.source_fingerprint,
       'sha256-v1', staged.state, 1, staged.error_code, 1, $2, $2
FROM legacy_image_validation_stage AS staged
ON CONFLICT (run_id, entity_kind, legacy_id) DO NOTHING`, config.Inventory.RunID, config.OccurredAt); err != nil {
		return fmt.Errorf("insert legacy image validation checkpoints: %w", err)
	}
	var matching int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM legacy_image_validation_stage AS staged
JOIN migration.source_rows AS existing
  ON existing.run_id = $1
 AND existing.entity_kind = staged.entity_kind
 AND existing.legacy_id = staged.legacy_id
 AND existing.source_fingerprint = staged.source_fingerprint
 AND existing.fingerprint_scheme = 'sha256-v1'
 AND existing.state IN ('validated', 'imported', 'skipped')
 AND COALESCE(existing.error_code, '') = COALESCE(staged.error_code, '')`, config.Inventory.RunID).Scan(&matching); err != nil {
		return fmt.Errorf("count persisted legacy image validation rows: %w", err)
	}
	if matching != int64(len(images)) {
		return errors.New("not every legacy image has an immutable checkpoint")
	}
	if err := persistImageArtifacts(ctx, tx, config, result, inspection); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit legacy image validation persistence: %w", err)
	}
	return nil
}

func persistImageArtifacts(
	ctx context.Context,
	tx pgx.Tx,
	config ValidationConfig,
	result ValidationResult,
	inspection ArchiveInspection,
) error {
	artifacts := []struct {
		kind   string
		digest [sha256.Size]byte
		bytes  int64
		items  int64
		name   string
	}{
		{kind: "image_archive", digest: inspection.SHA256, bytes: inspection.ByteLength, items: inspection.ImageCount, name: "ptyes-image-archive-v1"},
		{kind: "image_manifest", digest: result.ManifestSHA256, bytes: result.OriginalBytes, items: result.ImportableImages, name: "ptyes-image-manifest-v1"},
	}
	for _, artifact := range artifacts {
		var existing, matching int64
		if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint,
       count(*) FILTER (WHERE content_sha256 = $3 AND byte_length = $4 AND item_count = $5)::bigint
FROM migration.run_artifacts
WHERE run_id = $1 AND kind = $2`, config.Inventory.RunID, artifact.kind, artifact.digest[:], artifact.bytes, artifact.items).Scan(&existing, &matching); err != nil {
			return fmt.Errorf("read existing legacy image artifact: %w", err)
		}
		if existing > 0 && (existing != 1 || matching != 1) {
			return errors.New("existing legacy image artifact conflicts with validated bytes")
		}
		if existing == 0 {
			artifactID := uuid.NewSHA1(config.Inventory.RunID, []byte(artifact.name))
			if _, err := tx.Exec(ctx, `
INSERT INTO migration.run_artifacts (
    id, run_id, kind, content_sha256, byte_length, item_count, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)`, artifactID, config.Inventory.RunID, artifact.kind, artifact.digest[:], artifact.bytes, artifact.items, config.OccurredAt); err != nil {
				return fmt.Errorf("insert legacy image artifact: %w", err)
			}
		}
	}
	return nil
}
