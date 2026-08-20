package legacymedia

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
)

type StoredObjectVerification struct {
	MappedImages  int64
	UniqueObjects int64
	VerifiedBytes int64
}

// VerifyStoredObjects is the final read-only media gate. It proves every
// imported image still has its checkpoint, attachment and compatibility alias,
// then streams each unique object from the selected backend through SHA-256
// verification. Content deduplication means UniqueObjects may be smaller than
// MappedImages without losing any attachment evidence.
func VerifyStoredObjects(
	ctx context.Context,
	core *pgxpool.Pool,
	store objectstorage.Store,
	runID uuid.UUID,
	progress func(processed, expected int64),
) (StoredObjectVerification, error) {
	if ctx == nil || core == nil || store == nil || runID == uuid.Nil || store.BackendID() == "" {
		return StoredObjectVerification{}, errors.New("legacy image stored-object verification configuration is invalid")
	}
	if progress == nil {
		progress = func(int64, int64) {}
	}
	var result StoredObjectVerification
	var manifestImages int64
	if err := core.QueryRow(ctx, `
SELECT
    COALESCE((SELECT item_count
              FROM migration.run_artifacts
              WHERE run_id = $1 AND kind = 'image_manifest'), 0)::bigint,
    count(mapping.legacy_id)::bigint
FROM migration.torrent_image_map AS mapping
JOIN migration.source_rows AS checkpoint
  ON checkpoint.run_id = mapping.first_run_id
 AND checkpoint.entity_kind = mapping.entity_kind
 AND checkpoint.legacy_id = mapping.legacy_id
 AND checkpoint.state = 'imported'
JOIN torrents.torrent_screenshots AS attachment
  ON attachment.torrent_id = mapping.torrent_id
 AND attachment.object_id = mapping.object_id
 AND attachment.position = mapping.position
JOIN torrents.legacy_image_aliases AS alias
  ON alias.legacy_path = mapping.legacy_path
 AND alias.object_id = mapping.object_id
 AND alias.first_run_id = mapping.first_run_id
WHERE mapping.first_run_id = $1`, runID).Scan(&manifestImages, &result.MappedImages); err != nil {
		return StoredObjectVerification{}, fmt.Errorf("inspect accepted legacy image mappings: %w", err)
	}
	if manifestImages < 1 || result.MappedImages != manifestImages {
		return StoredObjectVerification{}, errors.New("accepted legacy image mappings do not match the immutable manifest")
	}
	var expectedObjects int64
	if err := core.QueryRow(ctx, `
SELECT count(DISTINCT mapping.object_id)::bigint
FROM migration.torrent_image_map AS mapping
JOIN torrents.torrent_screenshot_objects AS object ON object.id = mapping.object_id
JOIN torrents.torrent_screenshot_object_locations AS location
  ON location.object_id = object.id
 AND location.backend_id = $2
 AND location.observed_sha256 = object.content_sha256
 AND location.observed_byte_length = object.byte_length
WHERE mapping.first_run_id = $1`, runID, string(store.BackendID())).Scan(&expectedObjects); err != nil {
		return StoredObjectVerification{}, fmt.Errorf("count accepted legacy image objects: %w", err)
	}
	if expectedObjects < 1 {
		return StoredObjectVerification{}, errors.New("accepted legacy image backend has no verified locations")
	}
	rows, err := core.Query(ctx, `
SELECT DISTINCT
    object.id,
    object.content_sha256,
    object.byte_length,
    location.object_key,
    COALESCE(location.version_id, '')
FROM migration.torrent_image_map AS mapping
JOIN torrents.torrent_screenshot_objects AS object ON object.id = mapping.object_id
JOIN torrents.torrent_screenshot_object_locations AS location
  ON location.object_id = object.id
 AND location.backend_id = $2
 AND location.observed_sha256 = object.content_sha256
 AND location.observed_byte_length = object.byte_length
WHERE mapping.first_run_id = $1
ORDER BY object.id`, runID, string(store.BackendID()))
	if err != nil {
		return StoredObjectVerification{}, fmt.Errorf("query accepted legacy image objects: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var objectID uuid.UUID
		var digest []byte
		var byteLength int64
		var rawKey, versionID string
		if err := rows.Scan(&objectID, &digest, &byteLength, &rawKey, &versionID); err != nil {
			return StoredObjectVerification{}, fmt.Errorf("scan accepted legacy image object: %w", err)
		}
		if objectID == uuid.Nil || len(digest) != 32 || byteLength < 1 {
			return StoredObjectVerification{}, errors.New("accepted legacy image object metadata is invalid")
		}
		key, err := objectstorage.ParseKey(rawKey)
		if err != nil {
			return StoredObjectVerification{}, errors.New("accepted legacy image object key is invalid")
		}
		var expectedDigest objectstorage.SHA256
		copy(expectedDigest[:], digest)
		expected := objectstorage.Descriptor{SHA256: expectedDigest, ByteLength: byteLength}
		opened, err := store.Open(ctx, key, versionID)
		if err != nil || opened.Body == nil {
			return StoredObjectVerification{}, fmt.Errorf("open accepted legacy image object %s", objectID)
		}
		_, verifyErr := objectstorage.Verify(opened, expected)
		closeErr := opened.Body.Close()
		if verifyErr != nil || closeErr != nil {
			return StoredObjectVerification{}, fmt.Errorf("verify accepted legacy image object %s", objectID)
		}
		result.UniqueObjects++
		result.VerifiedBytes += byteLength
		progress(result.UniqueObjects, expectedObjects)
	}
	if err := rows.Err(); err != nil {
		return StoredObjectVerification{}, fmt.Errorf("read accepted legacy image objects: %w", err)
	}
	if result.UniqueObjects != expectedObjects || result.VerifiedBytes < 1 {
		return StoredObjectVerification{}, errors.New("accepted legacy image backend has no verified objects")
	}
	return result, nil
}
