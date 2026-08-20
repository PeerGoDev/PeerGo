package legacymedia

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ReconciliationResult struct {
	RunID             uuid.UUID
	ImportedImages    int64
	SkippedPosters    int64
	ExcludedImages    int64
	MappedImages      int64
	VerifiedLocations int64
	LegacyAliases     int64
}

// Reconcile is a read-only terminal gate over a stable all-verified import
// retry. It does not advance migration.runs; the torrent reconciliation owns
// that single state transition after user, torrent and media evidence agree.
func Reconcile(
	ctx context.Context,
	core *pgxpool.Pool,
	verified ImportResult,
) (ReconciliationResult, error) {
	if ctx == nil || core == nil || verified.RunID == uuid.Nil || verified.ExpectedImages < 1 ||
		verified.ImportedImages != 0 || verified.VerifiedImages != verified.ExpectedImages {
		return ReconciliationResult{}, errors.New("legacy image reconciliation requires a stable verify-only retry")
	}
	result := ReconciliationResult{RunID: verified.RunID}
	var artifactImages, archiveArtifacts, manifestArtifacts, nonterminal int64
	if err := core.QueryRow(ctx, `
SELECT
    count(checkpoint.legacy_id) FILTER (
        WHERE checkpoint.entity_kind IN ('torrent_image', 'torrent_poster')
          AND checkpoint.state = 'imported'
    )::bigint,
    count(checkpoint.legacy_id) FILTER (
        WHERE checkpoint.entity_kind = 'torrent_poster'
          AND checkpoint.state = 'skipped'
          AND checkpoint.error_code = 'poster_source_missing_placeholder'
    )::bigint,
    count(checkpoint.legacy_id) FILTER (
        WHERE checkpoint.entity_kind IN ('torrent_image', 'torrent_poster')
          AND checkpoint.state = 'skipped'
          AND checkpoint.error_code = 'torrent_explicitly_excluded'
    )::bigint,
    count(checkpoint.legacy_id) FILTER (
        WHERE checkpoint.entity_kind IN ('torrent_image', 'torrent_poster')
          AND checkpoint.state NOT IN ('imported', 'skipped')
    )::bigint,
    COALESCE((SELECT item_count FROM migration.run_artifacts
              WHERE run_id = $1 AND kind = 'image_manifest'), 0)::bigint,
    (SELECT count(*)::bigint FROM migration.run_artifacts
     WHERE run_id = $1 AND kind = 'image_manifest'),
    (SELECT count(*)::bigint FROM migration.run_artifacts
     WHERE run_id = $1 AND kind = 'image_archive')
FROM migration.source_rows AS checkpoint
WHERE checkpoint.run_id = $1`, verified.RunID).Scan(
		&result.ImportedImages, &result.SkippedPosters, &result.ExcludedImages,
		&nonterminal, &artifactImages, &manifestArtifacts, &archiveArtifacts,
	); err != nil {
		return ReconciliationResult{}, fmt.Errorf("reconcile legacy image checkpoints: %w", err)
	}
	if result.ImportedImages != verified.ExpectedImages || artifactImages != verified.ExpectedImages ||
		nonterminal != 0 || manifestArtifacts != 1 || archiveArtifacts != 1 {
		return ReconciliationResult{}, errors.New("legacy image checkpoints or artifacts do not reconcile")
	}
	var attachments, objects, aliases int64
	if err := core.QueryRow(ctx, `
SELECT
    count(DISTINCT (mapping.entity_kind, mapping.legacy_id))::bigint,
    count(DISTINCT (mapping.entity_kind, mapping.legacy_id)) FILTER (
        WHERE attachment.object_id IS NOT NULL
    )::bigint,
    count(DISTINCT location.object_id)::bigint,
    count(DISTINCT (mapping.entity_kind, mapping.legacy_id)) FILTER (
        WHERE alias.legacy_path IS NOT NULL
    )::bigint
FROM migration.torrent_image_map AS mapping
JOIN torrents.torrent_screenshots AS attachment
  ON attachment.torrent_id = mapping.torrent_id
 AND attachment.object_id = mapping.object_id
 AND attachment.position = mapping.position
JOIN torrents.torrent_screenshot_objects AS object ON object.id = mapping.object_id
JOIN torrents.torrent_screenshot_object_locations AS location
  ON location.object_id = object.id
 AND location.observed_sha256 = object.content_sha256
 AND location.observed_byte_length = object.byte_length
JOIN torrents.legacy_image_aliases AS alias
  ON alias.legacy_path = mapping.legacy_path
 AND alias.object_id = mapping.object_id
 AND alias.first_run_id = mapping.first_run_id
WHERE mapping.first_run_id = $1`, verified.RunID).Scan(
		&result.MappedImages, &attachments, &objects, &aliases,
	); err != nil {
		return ReconciliationResult{}, fmt.Errorf("reconcile legacy image targets: %w", err)
	}
	result.VerifiedLocations = objects
	result.LegacyAliases = aliases
	if result.MappedImages != verified.ExpectedImages || attachments != verified.ExpectedImages ||
		objects < 1 || aliases != verified.ExpectedImages {
		return ReconciliationResult{}, errors.New("legacy image objects, mappings or aliases do not reconcile")
	}
	return result, nil
}
