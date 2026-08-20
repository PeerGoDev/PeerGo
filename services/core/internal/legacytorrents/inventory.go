package legacytorrents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

type InventoryConfig struct {
	RunID          uuid.UUID
	SnapshotSHA256 [sha256.Size]byte
	MappingVersion string
}

type InventoryResult struct {
	RunID                     uuid.UUID
	Users                     int64
	Torrents                  int64
	FileRows                  int64
	Groups                    int64
	MissingFileManifests      int64
	DuplicatePathTorrents     int64
	CaseCollidingPathTorrents int64
	RecoveredExternalIDs      int64
	SkippedExternalIDs        int64
	ExternalIDWarningCounts   map[string]int64
	FacetValues               int64
	CategoryCounts            map[string]int64
	StateCounts               map[string]int64
}

func InspectInventory(
	ctx context.Context,
	source *pgxpool.Pool,
	core *pgxpool.Pool,
	config InventoryConfig,
) (InventoryResult, error) {
	if source == nil || core == nil || config.RunID == uuid.Nil ||
		config.SnapshotSHA256 == ([sha256.Size]byte{}) ||
		strings.TrimSpace(config.MappingVersion) == "" {
		return InventoryResult{}, errors.New("legacy torrent inventory configuration is invalid")
	}
	if err := platformpostgres.RequireCurrentMigration(ctx, core); err != nil {
		return InventoryResult{}, err
	}
	users, torrentsCount, groups, err := sourceInventoryCounts(ctx, source)
	if err != nil {
		return InventoryResult{}, err
	}
	if err := verifyInventoryRun(ctx, core, config, users, torrentsCount); err != nil {
		return InventoryResult{}, err
	}
	userMappings, err := loadUserMappings(ctx, core)
	if err != nil {
		return InventoryResult{}, err
	}
	if int64(len(userMappings)) != users {
		return InventoryResult{}, fmt.Errorf("legacy user mapping count is %d, want %d", len(userMappings), users)
	}
	vocabulary, err := loadLegacyVocabulary(ctx, source)
	if err != nil {
		return InventoryResult{}, err
	}

	torrentRows, err := querySourceTorrents(ctx, source)
	if err != nil {
		return InventoryResult{}, err
	}
	defer torrentRows.Close()
	fileCursor, err := newSourceFileCursor(ctx, source)
	if err != nil {
		return InventoryResult{}, err
	}
	defer fileCursor.Close()

	result := InventoryResult{
		RunID: config.RunID, Users: users, Torrents: torrentsCount, Groups: groups,
		CategoryCounts: make(map[string]int64), StateCounts: make(map[string]int64),
		ExternalIDWarningCounts: make(map[string]int64),
	}
	groupCache := make(map[int64]groupInventory)
	var processed, previousID int64
	for torrentRows.Next() {
		sourceTorrent, scanErr := scanSourceTorrent(torrentRows)
		if scanErr != nil {
			return InventoryResult{}, fmt.Errorf("scan PtYes torrent %d: %w", processed+1, scanErr)
		}
		processed++
		if sourceTorrent.LegacyID <= previousID {
			return InventoryResult{}, sourceTorrentError(sourceTorrent.LegacyID, "non_increasing_id")
		}
		previousID = sourceTorrent.LegacyID
		if sourceTorrent.validate() != nil {
			return InventoryResult{}, sourceTorrentError(sourceTorrent.LegacyID, "invalid_metadata")
		}
		if _, mapped := userMappings[sourceTorrent.UploaderLegacyID]; !mapped {
			return InventoryResult{}, sourceTorrentError(sourceTorrent.LegacyID, "missing_uploader_mapping")
		}
		manifest, manifestErr := fileCursor.ManifestFor(sourceTorrent.LegacyID)
		if manifestErr != nil {
			return InventoryResult{}, manifestErr
		}
		result.FileRows += int64(len(manifest.Files))
		if len(manifest.Files) == 0 {
			result.MissingFileManifests++
		} else if manifest.TotalSize != sourceTorrent.SizeBytes {
			return InventoryResult{}, sourceTorrentError(sourceTorrent.LegacyID, "source_file_size_mismatch")
		}
		duplicate, caseCollision := sourceManifestPathCollisions(manifest)
		if duplicate {
			result.DuplicatePathTorrents++
		}
		if caseCollision {
			result.CaseCollidingPathTorrents++
		}

		facets, attributes, mappingErr := mapLegacyAttributes(
			sourceTorrent.SourceCategory,
			sourceTorrent.Attributes,
			vocabulary,
		)
		if mappingErr != nil {
			return InventoryResult{}, sourceTorrentError(sourceTorrent.LegacyID, taxonomyErrorCode(mappingErr))
		}
		result.FacetValues += int64(len(facets))
		_, warnings, identifierErr := extractTorrentExternalIDs(attributes)
		if identifierErr != nil {
			return InventoryResult{}, sourceTorrentError(sourceTorrent.LegacyID, taxonomyErrorCode(identifierErr))
		}
		addIdentifierWarnings(&result, warnings)

		if sourceTorrent.GroupLegacyID != nil {
			group, seen := groupCache[*sourceTorrent.GroupLegacyID]
			if !seen {
				_, groupWarnings, groupErr := extractGroupExternalIDs(sourceTorrent.GroupExternalIDs)
				if groupErr != nil {
					return InventoryResult{}, sourceTorrentError(sourceTorrent.LegacyID, taxonomyErrorCode(groupErr))
				}
				group = groupInventory{warnings: groupWarnings}
				groupCache[*sourceTorrent.GroupLegacyID] = group
				addIdentifierWarnings(&result, group.warnings)
			}
		}
		categoryID, _ := sourceTorrent.categoryID()
		state, _ := sourceTorrent.state()
		result.CategoryCounts[categoryID]++
		result.StateCounts[string(state)]++
	}
	if err := torrentRows.Err(); err != nil {
		return InventoryResult{}, fmt.Errorf("read PtYes torrents: %w", err)
	}
	if processed != torrentsCount {
		return InventoryResult{}, fmt.Errorf("inspected %d PtYes torrents, expected %d", processed, torrentsCount)
	}
	if err := fileCursor.Finish(); err != nil {
		return InventoryResult{}, err
	}
	if result.FileRows != fileCursor.Processed() {
		return InventoryResult{}, errors.New("PtYes file inventory count changed during inspection")
	}
	return result, nil
}

type groupInventory struct {
	warnings []identifierWarning
}

func addIdentifierWarnings(result *InventoryResult, warnings []identifierWarning) {
	for _, warning := range warnings {
		result.ExternalIDWarningCounts[warning.Provider+"."+warning.Code]++
		switch warning.Code {
		case warningExternalIDRecovered:
			result.RecoveredExternalIDs++
		case warningExternalIDSkipped:
			result.SkippedExternalIDs++
		}
	}
}

func taxonomyErrorCode(err error) string {
	var problem taxonomyError
	if errors.As(err, &problem) && problem.code != "" {
		return problem.code
	}
	return "invalid_taxonomy"
}

func sourceInventoryCounts(ctx context.Context, source *pgxpool.Pool) (int64, int64, int64, error) {
	var users, torrentsCount, groups int64
	if err := source.QueryRow(ctx, `
SELECT
    (SELECT count(*)::bigint FROM users),
    (SELECT count(*)::bigint FROM torrents),
    (SELECT count(*)::bigint FROM torrent_groups)`).Scan(
		&users,
		&torrentsCount,
		&groups,
	); err != nil {
		return 0, 0, 0, fmt.Errorf("count PtYes torrent inventory: %w", err)
	}
	return users, torrentsCount, groups, nil
}

func verifyInventoryRun(
	ctx context.Context,
	core *pgxpool.Pool,
	config InventoryConfig,
	users, torrentsCount int64,
) error {
	var snapshot []byte
	var mappingVersion, state string
	var expectedUsers, expectedTorrents, importedUsers int64
	if err := core.QueryRow(ctx, `
SELECT
    run.source_snapshot_sha256,
    run.mapping_version,
    run.state,
    run.expected_user_rows,
    run.expected_torrent_rows,
    count(source.legacy_id) FILTER (
        WHERE source.entity_kind = 'user' AND source.state = 'imported'
    )::bigint
FROM migration.runs AS run
LEFT JOIN migration.source_rows AS source ON source.run_id = run.id
WHERE run.id = $1
GROUP BY run.id`, config.RunID).Scan(
		&snapshot,
		&mappingVersion,
		&state,
		&expectedUsers,
		&expectedTorrents,
		&importedUsers,
	); err != nil {
		return fmt.Errorf("read legacy migration run for torrent inventory: %w", err)
	}
	if !bytes.Equal(snapshot, config.SnapshotSHA256[:]) || mappingVersion != config.MappingVersion ||
		expectedUsers != users || expectedTorrents != torrentsCount || importedUsers != users ||
		(state != "importing" && state != "imported" && state != "reconciled") {
		return errors.New("legacy migration run is not ready for torrent inventory")
	}
	return nil
}

func loadUserMappings(ctx context.Context, core *pgxpool.Pool) (map[int64]uuid.UUID, error) {
	rows, err := core.Query(ctx, `
SELECT legacy_user_id, user_id
FROM migration.user_id_map
WHERE source_system = 'ptyes'
ORDER BY legacy_user_id`)
	if err != nil {
		return nil, fmt.Errorf("query legacy user mappings: %w", err)
	}
	defer rows.Close()
	result := make(map[int64]uuid.UUID)
	for rows.Next() {
		var legacyID int64
		var userID uuid.UUID
		if err := rows.Scan(&legacyID, &userID); err != nil {
			return nil, fmt.Errorf("scan legacy user mapping: %w", err)
		}
		if legacyID < 1 || userID == uuid.Nil {
			return nil, errors.New("legacy user mapping contains an invalid identity")
		}
		result[legacyID] = userID
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read legacy user mappings: %w", err)
	}
	return result, nil
}

func loadLegacyVocabulary(ctx context.Context, source *pgxpool.Pool) (legacyVocabulary, error) {
	rows, err := source.Query(ctx, `
SELECT
    category.name,
    attribute.name,
    option.value,
    COALESCE(NULLIF(btrim(option.label), ''), option.value)
FROM category_attribute_options AS option
JOIN category_attributes AS attribute ON attribute.id = option.attribute_id
JOIN categories AS category ON category.id = attribute.category_id
WHERE btrim(option.value) <> ''
ORDER BY category.name, attribute.name, option.id`)
	if err != nil {
		return nil, fmt.Errorf("query PtYes category vocabulary: %w", err)
	}
	defer rows.Close()
	result := legacyVocabulary{}
	for rows.Next() {
		var category, field, value, label string
		if err := rows.Scan(&category, &field, &value, &label); err != nil {
			return nil, fmt.Errorf("scan PtYes category vocabulary: %w", err)
		}
		result.add(category, field, value, label)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read PtYes category vocabulary: %w", err)
	}
	return result, nil
}

func querySourceTorrents(ctx context.Context, source *pgxpool.Pool) (pgx.Rows, error) {
	rows, err := source.Query(ctx, `
SELECT
    torrent.id::bigint,
    torrent.uuid,
    torrent.info_hash,
    torrent.title,
    torrent.subtitle,
    COALESCE(torrent.description, ''),
    COALESCE(torrent.type, ''),
    COALESCE(torrent.attributes, ''),
    torrent.size,
    torrent.uploaded_by::bigint,
    COALESCE(torrent.anonymous, false),
    COALESCE(torrent.status, ''),
    COALESCE(torrent.promotion_type, 1)::integer,
    COALESCE(torrent.promotion_time_type, 0)::integer,
    torrent.promotion_until,
    torrent.group_id::bigint,
    COALESCE(grouping.external_ids, '{}'::jsonb)::text,
    COALESCE(torrent.media_info, ''),
    COALESCE(torrent.poster, ''),
    torrent.created_at,
    torrent.updated_at,
    torrent.deleted_at
FROM torrents AS torrent
LEFT JOIN torrent_groups AS grouping ON grouping.id = torrent.group_id
ORDER BY torrent.id`)
	if err != nil {
		return nil, fmt.Errorf("query PtYes torrents: %w", err)
	}
	return rows, nil
}

func scanSourceTorrent(rows pgx.Rows) (sourceTorrent, error) {
	var source sourceTorrent
	if err := rows.Scan(
		&source.LegacyID,
		&source.LegacyUUID,
		&source.InfoHash,
		&source.Title,
		&source.Subtitle,
		&source.Description,
		&source.SourceCategory,
		&source.Attributes,
		&source.SizeBytes,
		&source.UploaderLegacyID,
		&source.Anonymous,
		&source.Status,
		&source.PromotionType,
		&source.PromotionTimeType,
		&source.PromotionUntil,
		&source.GroupLegacyID,
		&source.GroupExternalIDs,
		&source.MediaInfo,
		&source.Poster,
		&source.CreatedAt,
		&source.UpdatedAt,
		&source.DeletedAt,
	); err != nil {
		return sourceTorrent{}, err
	}
	source.CreatedAt = source.CreatedAt.UTC().Truncate(pgxTimestampResolution)
	source.UpdatedAt = source.UpdatedAt.UTC().Truncate(pgxTimestampResolution)
	if source.DeletedAt != nil {
		value := source.DeletedAt.UTC().Truncate(pgxTimestampResolution)
		source.DeletedAt = &value
	}
	if source.PromotionUntil != nil {
		value := source.PromotionUntil.UTC().Truncate(pgxTimestampResolution)
		source.PromotionUntil = &value
	}
	return source, nil
}

const pgxTimestampResolution = 1000 // PostgreSQL stores microseconds; time.Duration is nanoseconds.

type sourceFileCursor struct {
	rows           pgx.Rows
	hasPending     bool
	pendingID      int64
	pendingTorrent int64
	pendingPath    string
	pendingSize    int64
	processed      int64
}

func newSourceFileCursor(ctx context.Context, source *pgxpool.Pool) (*sourceFileCursor, error) {
	rows, err := source.Query(ctx, `
SELECT id::bigint, torrent_id::bigint, path, size
FROM torrent_files
ORDER BY torrent_id, id`)
	if err != nil {
		return nil, fmt.Errorf("query PtYes torrent files: %w", err)
	}
	cursor := &sourceFileCursor{rows: rows}
	if err := cursor.advance(); err != nil {
		rows.Close()
		return nil, err
	}
	return cursor, nil
}

func (cursor *sourceFileCursor) Close() {
	if cursor != nil && cursor.rows != nil {
		cursor.rows.Close()
	}
}

func (cursor *sourceFileCursor) Processed() int64 {
	if cursor == nil {
		return 0
	}
	return cursor.processed
}

func (cursor *sourceFileCursor) ManifestFor(torrentID int64) (sourceFileManifest, error) {
	if cursor == nil || torrentID < 1 {
		return sourceFileManifest{}, errInvalidSourceTorrent
	}
	if cursor.hasPending && cursor.pendingTorrent < torrentID {
		return sourceFileManifest{}, sourceTorrentError(cursor.pendingTorrent, "orphan_file_rows")
	}
	files := make([]sourceFile, 0)
	for cursor.hasPending && cursor.pendingTorrent == torrentID {
		files = append(files, sourceFile{
			LegacyID: cursor.pendingID,
			Path:     cursor.pendingPath,
			Size:     cursor.pendingSize,
		})
		cursor.processed++
		if err := cursor.advance(); err != nil {
			return sourceFileManifest{}, err
		}
	}
	return newSourceFileManifest(torrentID, files)
}

func (cursor *sourceFileCursor) Finish() error {
	if cursor == nil {
		return errInvalidSourceTorrent
	}
	if cursor.hasPending {
		return sourceTorrentError(cursor.pendingTorrent, "orphan_file_rows")
	}
	if err := cursor.rows.Err(); err != nil {
		return fmt.Errorf("read PtYes torrent files: %w", err)
	}
	return nil
}

func (cursor *sourceFileCursor) advance() error {
	if !cursor.rows.Next() {
		cursor.hasPending = false
		if err := cursor.rows.Err(); err != nil {
			return fmt.Errorf("read PtYes torrent files: %w", err)
		}
		return nil
	}
	cursor.hasPending = true
	if err := cursor.rows.Scan(
		&cursor.pendingID,
		&cursor.pendingTorrent,
		&cursor.pendingPath,
		&cursor.pendingSize,
	); err != nil {
		return fmt.Errorf("scan PtYes torrent file: %w", err)
	}
	return nil
}

func sourceManifestPathCollisions(manifest sourceFileManifest) (bool, bool) {
	exact := make(map[string]struct{}, len(manifest.Files))
	folded := make(map[string]string, len(manifest.Files))
	var duplicate, caseCollision bool
	for _, file := range manifest.Files {
		if _, exists := exact[file.Path]; exists {
			duplicate = true
		}
		exact[file.Path] = struct{}{}
		key := strings.ToLower(file.Path)
		if previous, exists := folded[key]; exists && previous != file.Path {
			caseCollision = true
		} else {
			folded[key] = file.Path
		}
	}
	return duplicate, caseCollision
}
