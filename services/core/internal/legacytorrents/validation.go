package legacytorrents

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

const (
	torrentObjectManifestDomain  = "peergo:migration:ptyes-torrent-objects:v1\x00"
	maxReportedBlockingLegacyIDs = 100
)

type ObjectValidationConfig struct {
	Inventory     InventoryConfig
	TorrentRoot   string
	OccurredAt    time.Time
	ProgressEvery int64
	Exclusions    TorrentExclusionManifest
}

type ObjectValidationProgress struct {
	Processed int64
	Expected  int64
}

type ObjectValidationResult struct {
	RunID                      uuid.UUID
	ArchiveObjects             int64
	UnreferencedArchiveObjects int64
	RecoveredArchiveObjects    int64
	RecoveredArchiveLegacyIDs  []int64
	AmbiguousArchiveObjects    int64
	ExcludedTorrents           int64
	ExcludedLegacyIDs          []int64
	ValidatedTorrents          int64
	ValidatedObjects           int64
	ObjectBytes                int64
	MissingDatabaseManifests   int64
	CompatibilityFlagCounts    map[string]int64
	BlockingIssueCounts        map[string]int64
	BlockingLegacyIDs          map[string][]int64
	BlockingDiagnostics        map[string][]ObjectValidationDiagnostic
	ObjectManifestSHA256       [sha256.Size]byte
}

type ObjectValidationDiagnostic struct {
	LegacyID int64  `json:"legacy_id"`
	Field    string `json:"field"`
	Offset   int    `json:"offset"`
	Reason   string `json:"reason"`
}

// ObjectValidationFailure reports the full scan summary without persisting a
// partial manifest. Legacy numeric IDs are bounded diagnostic references; raw
// filenames, metainfo values, and paths never enter logs.
type ObjectValidationFailure struct {
	IssueCounts map[string]int64
	LegacyIDs   map[string][]int64
	Diagnostics map[string][]ObjectValidationDiagnostic
}

func (problem *ObjectValidationFailure) Error() string {
	return "PtYes torrent object validation found blocking compatibility issues"
}

type validatedObjectRow struct {
	legacyID           int64
	torrentFingerprint [sha256.Size]byte
	objectFingerprint  [sha256.Size]byte
}

type excludedObjectRow struct {
	legacyID           int64
	torrentFingerprint [sha256.Size]byte
	objectFingerprint  [sha256.Size]byte
}

func ValidateObjects(
	ctx context.Context,
	source *pgxpool.Pool,
	core *pgxpool.Pool,
	config ObjectValidationConfig,
	progress func(ObjectValidationProgress),
) (ObjectValidationResult, error) {
	config.OccurredAt = config.OccurredAt.UTC().Truncate(time.Microsecond)
	config.TorrentRoot = strings.TrimSpace(config.TorrentRoot)
	if source == nil || core == nil || config.TorrentRoot == "" || config.OccurredAt.IsZero() {
		return ObjectValidationResult{}, errors.New("legacy torrent object validation configuration is invalid")
	}
	if config.ProgressEvery < 1 {
		config.ProgressEvery = 250
	}
	if progress == nil {
		progress = func(ObjectValidationProgress) {}
	}
	inventory, err := InspectInventory(ctx, source, core, config.Inventory)
	if err != nil {
		return ObjectValidationResult{}, err
	}
	root, err := openSourceObjectRoot(config.TorrentRoot)
	if err != nil {
		return ObjectValidationResult{}, err
	}
	defer func() { _ = root.close() }()
	recovery, err := prepareSourceObjectRecovery(ctx, source, root)
	if err != nil {
		return ObjectValidationResult{}, err
	}

	rows, err := querySourceTorrents(ctx, source)
	if err != nil {
		return ObjectValidationResult{}, err
	}
	defer rows.Close()
	fileCursor, err := newSourceFileCursor(ctx, source)
	if err != nil {
		return ObjectValidationResult{}, err
	}
	defer fileCursor.Close()

	result := ObjectValidationResult{
		RunID:                   config.Inventory.RunID,
		CompatibilityFlagCounts: make(map[string]int64),
		BlockingIssueCounts:     make(map[string]int64),
		BlockingLegacyIDs:       make(map[string][]int64),
		BlockingDiagnostics:     make(map[string][]ObjectValidationDiagnostic),
	}
	result.ArchiveObjects = recovery.ArchiveObjects
	result.UnreferencedArchiveObjects = recovery.UnreferencedObjects
	result.RecoveredArchiveObjects = recovery.RecoveredObjects
	result.RecoveredArchiveLegacyIDs = recovery.RecoveredLegacyIDs
	result.AmbiguousArchiveObjects = recovery.AmbiguousObjects
	manifestDigest := sha256.New()
	_, _ = manifestDigest.Write([]byte(torrentObjectManifestDomain))
	validatedRows := make([]validatedObjectRow, 0, inventory.Torrents)
	excludedRows := make([]excludedObjectRow, 0, config.Exclusions.Len())
	seenExclusions := make(map[int64]struct{}, config.Exclusions.Len())
	var processed int64
	for rows.Next() {
		sourceTorrent, scanErr := scanSourceTorrent(rows)
		if scanErr != nil {
			return ObjectValidationResult{}, fmt.Errorf("scan PtYes torrent object metadata: %w", scanErr)
		}
		manifest, manifestErr := fileCursor.ManifestFor(sourceTorrent.LegacyID)
		if manifestErr != nil {
			return ObjectValidationResult{}, manifestErr
		}
		processed++
		if len(manifest.Files) == 0 {
			result.MissingDatabaseManifests++
		}
		fingerprint, fingerprintErr := sourceTorrent.fingerprint(manifest)
		if fingerprintErr != nil {
			return ObjectValidationResult{}, sourceTorrentError(sourceTorrent.LegacyID, "invalid_metadata_fingerprint")
		}
		publicID, publicIDErr := sourceTorrent.publicID()
		if publicIDErr != nil {
			return ObjectValidationResult{}, sourceTorrentError(sourceTorrent.LegacyID, "invalid_public_id")
		}
		_, exclusionRequested := config.Exclusions.entries[sourceTorrent.LegacyID]
		if exclusionRequested {
			seenExclusions[sourceTorrent.LegacyID] = struct{}{}
			if !config.Exclusions.match(sourceTorrent) {
				addObjectValidationIssue(
					&result, sourceTorrent.LegacyID,
					sourceTorrentError(sourceTorrent.LegacyID, "exclusion_identity_mismatch"),
				)
				reportObjectValidationProgress(progress, processed, inventory.Torrents, config.ProgressEvery)
				continue
			}
		}
		raw, readErr := root.read(publicID)
		if readErr != nil {
			if exclusionRequested && sourceObjectErrorCode(readErr) == "object_missing" {
				objectFingerprint, fingerprintErr := config.Exclusions.objectFingerprint(sourceTorrent)
				if fingerprintErr != nil {
					return ObjectValidationResult{}, sourceTorrentError(sourceTorrent.LegacyID, "invalid_exclusion_fingerprint")
				}
				excludedRows = append(excludedRows, excludedObjectRow{
					legacyID: sourceTorrent.LegacyID, torrentFingerprint: fingerprint,
					objectFingerprint: objectFingerprint,
				})
				result.ExcludedTorrents++
				result.ExcludedLegacyIDs = append(result.ExcludedLegacyIDs, sourceTorrent.LegacyID)
				reportObjectValidationProgress(progress, processed, inventory.Torrents, config.ProgressEvery)
				continue
			}
			addObjectValidationIssue(&result, sourceTorrent.LegacyID, wrapSourceObjectError(sourceTorrent.LegacyID, readErr))
			reportObjectValidationProgress(progress, processed, inventory.Torrents, config.ProgressEvery)
			continue
		}
		if exclusionRequested {
			addObjectValidationIssue(
				&result, sourceTorrent.LegacyID,
				sourceTorrentError(sourceTorrent.LegacyID, "exclusion_object_present"),
			)
			reportObjectValidationProgress(progress, processed, inventory.Torrents, config.ProgressEvery)
			continue
		}
		parsed, parseErr := torrents.InspectLegacyV1OrHybrid(raw)
		if parseErr != nil {
			classified := classifySourceMetainfoError(sourceTorrent.LegacyID, raw, parseErr)
			addObjectValidationIssue(
				&result,
				sourceTorrent.LegacyID,
				classified,
				parseErr,
			)
			reportObjectValidationProgress(progress, processed, inventory.Torrents, config.ProgressEvery)
			continue
		}
		if reconcileErr := reconcileSourceMetainfo(sourceTorrent, manifest, parsed); reconcileErr != nil {
			addObjectValidationIssue(&result, sourceTorrent.LegacyID, reconcileErr)
			reportObjectValidationProgress(progress, processed, inventory.Torrents, config.ProgressEvery)
			continue
		}
		if result.ObjectBytes > math.MaxInt64-parsed.ObjectByteLength {
			return ObjectValidationResult{}, errors.New("PtYes torrent object byte total exceeds bigint")
		}
		result.ObjectBytes += parsed.ObjectByteLength
		result.ValidatedTorrents++
		result.ValidatedObjects++
		for _, flag := range parsed.CompatibilityFlags {
			result.CompatibilityFlagCounts[string(flag)]++
		}
		writeObjectManifestEntry(manifestDigest, sourceTorrent.LegacyID, parsed)
		validatedRows = append(validatedRows, validatedObjectRow{
			legacyID:           sourceTorrent.LegacyID,
			torrentFingerprint: fingerprint,
			objectFingerprint:  parsed.ObjectSHA256,
		})
		reportObjectValidationProgress(progress, processed, inventory.Torrents, config.ProgressEvery)
	}
	if err := rows.Err(); err != nil {
		return ObjectValidationResult{}, fmt.Errorf("read PtYes torrent object metadata: %w", err)
	}
	if err := fileCursor.Finish(); err != nil {
		return ObjectValidationResult{}, err
	}
	if processed != inventory.Torrents {
		return ObjectValidationResult{}, errors.New("PtYes torrent object count changed during validation")
	}
	if len(seenExclusions) != config.Exclusions.Len() {
		return ObjectValidationResult{}, errors.New("torrent exclusion manifest contains an unknown legacy ID")
	}
	progress(ObjectValidationProgress{Processed: processed, Expected: inventory.Torrents})
	if len(result.BlockingIssueCounts) != 0 {
		return result, &ObjectValidationFailure{
			IssueCounts: result.BlockingIssueCounts,
			LegacyIDs:   result.BlockingLegacyIDs,
			Diagnostics: result.BlockingDiagnostics,
		}
	}
	if result.ValidatedTorrents+result.ExcludedTorrents != inventory.Torrents ||
		result.ValidatedObjects+result.ExcludedTorrents != inventory.Torrents {
		return ObjectValidationResult{}, errors.New("PtYes torrent object count changed during validation")
	}
	copy(result.ObjectManifestSHA256[:], manifestDigest.Sum(nil))
	if err := persistObjectValidation(ctx, core, config, result, validatedRows, excludedRows); err != nil {
		return ObjectValidationResult{}, err
	}
	return result, nil
}

func classifySourceMetainfoError(legacyID int64, raw []byte, err error) error {
	code, ok := torrents.ValidationCodeOf(err)
	if !ok || code != torrents.CodeUnsupportedVersion {
		return wrapSourceObjectError(legacyID, err)
	}
	kind, detectErr := torrents.DetectMetainfoKind(raw, torrents.ValidationProfileLegacyImport)
	if detectErr != nil {
		return wrapSourceObjectError(legacyID, err)
	}
	switch kind {
	case torrents.MetainfoKindHybridV1V2:
		return sourceTorrentError(legacyID, "unsupported_hybrid_v1_v2")
	case torrents.MetainfoKindV2:
		return sourceTorrentError(legacyID, "unsupported_v2")
	case torrents.MetainfoKindBEP30Merkle:
		return sourceTorrentError(legacyID, "unsupported_bep30_merkle")
	default:
		return wrapSourceObjectError(legacyID, err)
	}
}

func addObjectValidationIssue(
	result *ObjectValidationResult,
	legacyID int64,
	err error,
	diagnosticErrors ...error,
) {
	code, ok := sourceTorrentValidationCode(err)
	if !ok {
		code = "object_validation_failed"
	}
	result.BlockingIssueCounts[code]++
	if len(result.BlockingLegacyIDs[code]) < maxReportedBlockingLegacyIDs {
		result.BlockingLegacyIDs[code] = append(result.BlockingLegacyIDs[code], legacyID)
	}
	if len(result.BlockingDiagnostics[code]) >= maxReportedBlockingLegacyIDs {
		return
	}
	for _, diagnosticErr := range diagnosticErrors {
		diagnostic, ok := torrents.ValidationDiagnosticOf(diagnosticErr)
		if !ok {
			continue
		}
		result.BlockingDiagnostics[code] = append(
			result.BlockingDiagnostics[code],
			ObjectValidationDiagnostic{
				LegacyID: legacyID, Field: diagnostic.Field,
				Offset: diagnostic.Offset, Reason: diagnostic.Reason,
			},
		)
		return
	}
}

func reportObjectValidationProgress(
	progress func(ObjectValidationProgress),
	processed int64,
	expected int64,
	every int64,
) {
	if processed%every == 0 {
		progress(ObjectValidationProgress{Processed: processed, Expected: expected})
	}
}

func writeObjectManifestEntry(writer hash.Hash, legacyID int64, parsed torrents.ParsedMetainfo) {
	writeInt64(writer, legacyID)
	writeInt64(writer, parsed.ObjectByteLength)
	_, _ = writer.Write(parsed.ObjectSHA256[:])
	_, _ = writer.Write(parsed.InfoHashV1[:])
}

func persistObjectValidation(
	ctx context.Context,
	core *pgxpool.Pool,
	config ObjectValidationConfig,
	result ObjectValidationResult,
	rows []validatedObjectRow,
	excluded []excludedObjectRow,
) error {
	tx, err := core.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin torrent object validation checkpoint transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
CREATE TEMP TABLE legacy_torrent_object_validation (
    legacy_id bigint PRIMARY KEY,
    torrent_fingerprint bytea NOT NULL,
    object_fingerprint bytea NOT NULL
) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("create torrent object validation staging table: %w", err)
	}
	if _, err := tx.Exec(ctx, `
CREATE TEMP TABLE legacy_torrent_object_exclusion (
    legacy_id bigint PRIMARY KEY,
    torrent_fingerprint bytea NOT NULL,
    object_fingerprint bytea NOT NULL
) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("create torrent object exclusion staging table: %w", err)
	}
	if _, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"legacy_torrent_object_validation"},
		[]string{"legacy_id", "torrent_fingerprint", "object_fingerprint"},
		pgx.CopyFromSlice(len(rows), func(index int) ([]any, error) {
			row := rows[index]
			return []any{row.legacyID, row.torrentFingerprint[:], row.objectFingerprint[:]}, nil
		}),
	); err != nil {
		return fmt.Errorf("stage torrent object validation checkpoints: %w", err)
	}
	if _, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"legacy_torrent_object_exclusion"},
		[]string{"legacy_id", "torrent_fingerprint", "object_fingerprint"},
		pgx.CopyFromSlice(len(excluded), func(index int) ([]any, error) {
			row := excluded[index]
			return []any{row.legacyID, row.torrentFingerprint[:], row.objectFingerprint[:]}, nil
		}),
	); err != nil {
		return fmt.Errorf("stage torrent object exclusion checkpoints: %w", err)
	}
	var conflicts int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM migration.source_rows AS checkpoint
JOIN legacy_torrent_object_validation AS staged
  ON staged.legacy_id = checkpoint.legacy_id
WHERE checkpoint.run_id = $1
  AND checkpoint.entity_kind IN ('torrent', 'torrent_object')
  AND (
      checkpoint.source_fingerprint <> CASE checkpoint.entity_kind
          WHEN 'torrent' THEN staged.torrent_fingerprint
          ELSE staged.object_fingerprint
      END
      OR checkpoint.fingerprint_scheme <> 'sha256-v1'
      OR checkpoint.state IN ('discrepancy', 'skipped')
  )`, config.Inventory.RunID).Scan(&conflicts); err != nil {
		return fmt.Errorf("verify existing torrent object checkpoints: %w", err)
	}
	if conflicts != 0 {
		return errors.New("existing torrent object validation checkpoints conflict with this snapshot")
	}
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM migration.source_rows AS checkpoint
JOIN legacy_torrent_object_exclusion AS staged
  ON staged.legacy_id = checkpoint.legacy_id
WHERE checkpoint.run_id = $1
  AND checkpoint.entity_kind IN ('torrent', 'torrent_object')
  AND (
      checkpoint.source_fingerprint <> CASE checkpoint.entity_kind
          WHEN 'torrent' THEN staged.torrent_fingerprint
          ELSE staged.object_fingerprint
      END
      OR checkpoint.fingerprint_scheme <> 'sha256-v1'
      OR checkpoint.state <> 'skipped'
      OR checkpoint.error_code <> 'object_missing_explicitly_excluded'
  )`, config.Inventory.RunID).Scan(&conflicts); err != nil {
		return fmt.Errorf("verify existing torrent exclusion checkpoints: %w", err)
	}
	if conflicts != 0 {
		return errors.New("existing torrent exclusion checkpoints conflict with this snapshot")
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO migration.source_rows (
    run_id, entity_kind, legacy_id, source_fingerprint,
    fingerprint_scheme, state, attempt_count, version, created_at, updated_at
)
SELECT $1, kind.entity_kind, staged.legacy_id,
       CASE kind.entity_kind
           WHEN 'torrent' THEN staged.torrent_fingerprint
           ELSE staged.object_fingerprint
       END,
       'sha256-v1', 'validated', 1, 1, $2, $2
FROM legacy_torrent_object_validation AS staged
CROSS JOIN (VALUES ('torrent'), ('torrent_object')) AS kind(entity_kind)
ON CONFLICT (run_id, entity_kind, legacy_id) DO NOTHING`, config.Inventory.RunID, config.OccurredAt); err != nil {
		return fmt.Errorf("insert torrent object validation checkpoints: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO migration.source_rows (
    run_id, entity_kind, legacy_id, source_fingerprint,
    fingerprint_scheme, state, attempt_count, error_code, version, created_at, updated_at
)
SELECT $1, kind.entity_kind, staged.legacy_id,
       CASE kind.entity_kind
           WHEN 'torrent' THEN staged.torrent_fingerprint
           ELSE staged.object_fingerprint
       END,
       'sha256-v1', 'skipped', 1, 'object_missing_explicitly_excluded', 1, $2, $2
FROM legacy_torrent_object_exclusion AS staged
CROSS JOIN (VALUES ('torrent'), ('torrent_object')) AS kind(entity_kind)
ON CONFLICT (run_id, entity_kind, legacy_id) DO NOTHING`, config.Inventory.RunID, config.OccurredAt); err != nil {
		return fmt.Errorf("insert torrent exclusion checkpoints: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE migration.source_rows AS checkpoint
SET state = 'validated',
    attempt_count = checkpoint.attempt_count + 1,
    error_code = NULL,
    version = checkpoint.version + 1,
    updated_at = $2
FROM legacy_torrent_object_validation AS staged
WHERE checkpoint.run_id = $1
  AND checkpoint.legacy_id = staged.legacy_id
  AND checkpoint.entity_kind IN ('torrent', 'torrent_object')
  AND checkpoint.state = 'discovered'`, config.Inventory.RunID, config.OccurredAt); err != nil {
		return fmt.Errorf("advance torrent object validation checkpoints: %w", err)
	}
	var ready, skipped int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM migration.source_rows AS checkpoint
JOIN legacy_torrent_object_validation AS staged
  ON staged.legacy_id = checkpoint.legacy_id
WHERE checkpoint.run_id = $1
  AND checkpoint.entity_kind IN ('torrent', 'torrent_object')
  AND checkpoint.state IN ('validated', 'imported')`, config.Inventory.RunID).Scan(&ready); err != nil {
		return fmt.Errorf("count validated torrent object checkpoints: %w", err)
	}
	if ready != int64(len(rows))*2 {
		return errors.New("not every PtYes torrent and object has a validated checkpoint")
	}
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM migration.source_rows AS checkpoint
JOIN legacy_torrent_object_exclusion AS staged
  ON staged.legacy_id = checkpoint.legacy_id
WHERE checkpoint.run_id = $1
  AND checkpoint.entity_kind IN ('torrent', 'torrent_object')
  AND checkpoint.state = 'skipped'
  AND checkpoint.error_code = 'object_missing_explicitly_excluded'`, config.Inventory.RunID).Scan(&skipped); err != nil {
		return fmt.Errorf("count skipped torrent object checkpoints: %w", err)
	}
	if skipped != int64(len(excluded))*2 {
		return errors.New("not every explicitly excluded torrent has a skipped checkpoint")
	}

	var existingArtifacts int64
	var matchingArtifacts int64
	if err := tx.QueryRow(ctx, `
SELECT
    count(*)::bigint,
    count(*) FILTER (
        WHERE content_sha256 = $2 AND byte_length = $3 AND item_count = $4
    )::bigint
FROM migration.run_artifacts
WHERE run_id = $1 AND kind = 'torrent_manifest'`,
		config.Inventory.RunID,
		result.ObjectManifestSHA256[:],
		result.ObjectBytes,
		result.ValidatedObjects,
	).Scan(&existingArtifacts, &matchingArtifacts); err != nil {
		return fmt.Errorf("read existing torrent object manifest: %w", err)
	}
	if existingArtifacts > 0 && (existingArtifacts != 1 || matchingArtifacts != 1) {
		return errors.New("existing torrent object manifest conflicts with validated bytes")
	}
	if existingArtifacts == 0 {
		artifactID := uuid.NewSHA1(config.Inventory.RunID, []byte("ptyes-torrent-object-manifest-v1"))
		if _, err := tx.Exec(ctx, `
INSERT INTO migration.run_artifacts (
    id, run_id, kind, content_sha256, byte_length, item_count, created_at
) VALUES ($1, $2, 'torrent_manifest', $3, $4, $5, $6)`,
			artifactID,
			config.Inventory.RunID,
			result.ObjectManifestSHA256[:],
			result.ObjectBytes,
			result.ValidatedObjects,
			config.OccurredAt,
		); err != nil {
			return fmt.Errorf("insert torrent object manifest: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit torrent object validation checkpoints: %w", err)
	}
	return nil
}
