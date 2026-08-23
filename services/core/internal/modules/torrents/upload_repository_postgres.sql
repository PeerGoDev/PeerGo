-- name: TorrentUploadUserEligible :one
SELECT EXISTS (
    SELECT 1
    FROM identity.users
    WHERE id = sqlc.arg(user_id)::uuid
      AND status = 'active'
      AND email_verified_at IS NOT NULL
);

-- name: TorrentUploadCategoryEnabled :one
SELECT EXISTS (
    SELECT 1
    FROM catalog.categories
    WHERE id = sqlc.arg(category_id)::text
      AND enabled = true
);

-- name: TorrentUploadIdentityExists :one
SELECT EXISTS (
    SELECT 1
    FROM torrents.torrents AS torrent
    JOIN torrents.torrent_objects AS object ON object.id = torrent.object_id
    WHERE torrent.info_hash_v1 = sqlc.arg(info_hash_v1)::bytea
       OR object.content_sha256 = sqlc.arg(content_sha256)::bytea
);

-- name: InsertTorrentUploadReservation :execrows
INSERT INTO torrents.torrent_uploads (
    id,
    uploader_id,
    request_fingerprint,
    object_id,
    category_id,
    info_hash_v1,
    content_sha256,
    byte_length,
    backend_id,
    object_key,
    upload_policy_revision_id,
    cleanup_available_at,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(upload_id)::uuid,
    sqlc.arg(uploader_id)::uuid,
    sqlc.arg(request_fingerprint)::bytea,
    sqlc.arg(object_id)::uuid,
    sqlc.arg(category_id)::text,
    sqlc.arg(info_hash_v1)::bytea,
    sqlc.arg(content_sha256)::bytea,
    sqlc.arg(byte_length)::bigint,
    sqlc.arg(backend_id)::text,
    sqlc.arg(object_key)::text,
    sqlc.arg(upload_policy_revision_id)::uuid,
    sqlc.arg(occurred_at)::timestamptz,
    sqlc.arg(occurred_at)::timestamptz,
    sqlc.arg(occurred_at)::timestamptz
)
ON CONFLICT DO NOTHING;

-- name: GetTorrentUploadForUpdate :one
SELECT
    id,
    uploader_id,
    request_fingerprint,
    object_id,
    category_id,
    info_hash_v1,
    content_sha256,
    byte_length,
    backend_id,
    object_key,
    upload_policy_revision_id,
    state,
    object_created,
    storage_version_id,
    object_verified_at,
    torrent_id,
    completed_at,
    created_at
FROM torrents.torrent_uploads
WHERE id = sqlc.arg(upload_id)::uuid
FOR UPDATE;

-- name: GetActiveTorrentUploadIDByIdentity :one
-- A browser reload creates a fresh idempotency key. The immutable content
-- identity lets the original uploader resume an exact interrupted request;
-- resumeReservation still verifies the uploader and request fingerprint.
SELECT id
FROM torrents.torrent_uploads
WHERE info_hash_v1 = sqlc.arg(info_hash_v1)::bytea
  AND content_sha256 = sqlc.arg(content_sha256)::bytea
  AND state <> 'abandoned'
FOR UPDATE;

-- name: GetCompletedTorrentUploadResult :one
SELECT
    torrent.id,
    torrent.info_hash_v1,
    'pending_review'::text AS state,
    torrent.content_name,
    torrent.total_size_bytes,
    torrent.file_count,
    torrent.submitted_at
FROM torrents.torrent_uploads AS upload
JOIN torrents.torrents AS torrent ON torrent.id = upload.torrent_id
WHERE upload.id = sqlc.arg(upload_id)::uuid
  AND upload.state = 'completed';

-- name: RecordTorrentUploadObjectVerified :execrows
UPDATE torrents.torrent_uploads
SET state = 'object_verified',
    object_created = sqlc.arg(object_created)::boolean,
    storage_version_id = sqlc.narg(storage_version_id)::text,
    object_verified_at = sqlc.arg(verified_at)::timestamptz,
    cleanup_available_at = sqlc.arg(verified_at)::timestamptz,
    last_error_code = NULL,
    updated_at = sqlc.arg(verified_at)::timestamptz,
    version = version + 1
WHERE id = sqlc.arg(upload_id)::uuid
  AND state = 'reserved';

-- name: InsertTorrentObjectFromUpload :exec
INSERT INTO torrents.torrent_objects (
    id,
    content_sha256,
    byte_length,
    parser_version,
    validation_profile,
    compatibility_flags,
    info_offset,
    info_length,
    created_at
) VALUES (
    sqlc.arg(object_id)::uuid,
    sqlc.arg(content_sha256)::bytea,
    sqlc.arg(byte_length)::bigint,
    sqlc.arg(parser_version)::text,
    sqlc.arg(validation_profile)::text,
    sqlc.arg(compatibility_flags)::text[],
    sqlc.arg(info_offset)::bigint,
    sqlc.arg(info_length)::bigint,
    sqlc.arg(created_at)::timestamptz
);

-- name: InsertInitialTorrentObjectLocation :exec
INSERT INTO torrents.torrent_object_locations (
    id,
    object_id,
    backend_id,
    object_key,
    state,
    is_preferred,
    version_id,
    observed_byte_length,
    observed_sha256,
    verified_at,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(location_id)::uuid,
    sqlc.arg(object_id)::uuid,
    sqlc.arg(backend_id)::text,
    sqlc.arg(object_key)::text,
    'verified',
    true,
    sqlc.narg(version_id)::text,
    sqlc.arg(observed_byte_length)::bigint,
    sqlc.arg(observed_sha256)::bytea,
    sqlc.arg(verified_at)::timestamptz,
    sqlc.arg(verified_at)::timestamptz,
    sqlc.arg(verified_at)::timestamptz
);

-- name: InsertPendingTorrentFromUpload :one
INSERT INTO torrents.torrents (
    uploader_id,
    category_id,
    object_id,
    info_hash_v1,
    content_name,
    title,
    subtitle,
    description,
    description_format,
    media_info,
    anonymous,
    total_size_bytes,
    payload_size_bytes,
    file_count,
    padding_file_count,
    piece_length_bytes,
    piece_count,
    state,
    version,
    submitted_at,
    published_at,
    state_changed_at,
    updated_at
) VALUES (
    sqlc.arg(uploader_id)::uuid,
    sqlc.arg(category_id)::text,
    sqlc.arg(object_id)::uuid,
    sqlc.arg(info_hash_v1)::bytea,
    sqlc.arg(content_name)::text,
    sqlc.arg(title)::text,
    sqlc.arg(subtitle)::text,
    sqlc.arg(description)::text,
    sqlc.arg(description_format)::text,
    sqlc.arg(media_info)::text,
    sqlc.arg(anonymous)::boolean,
    sqlc.arg(total_size_bytes)::bigint,
    sqlc.arg(payload_size_bytes)::bigint,
    sqlc.arg(file_count)::integer,
    sqlc.arg(padding_file_count)::integer,
    sqlc.arg(piece_length_bytes)::bigint,
    sqlc.arg(piece_count)::integer,
    'pending_review',
    1,
    sqlc.arg(submitted_at)::timestamptz,
    NULL,
    sqlc.arg(submitted_at)::timestamptz,
    sqlc.arg(submitted_at)::timestamptz
)
RETURNING id;

-- name: InsertTorrentExternalIdentifierFromUpload :exec
INSERT INTO torrents.torrent_external_identifiers (
    torrent_id,
    provider,
    external_id,
    origin,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(torrent_id)::bigint,
    sqlc.arg(provider)::text,
    sqlc.arg(external_id)::text,
    'user',
    sqlc.arg(created_at)::timestamptz,
    sqlc.arg(created_at)::timestamptz
);

-- name: InsertTorrentFacetValueFromUpload :execrows
INSERT INTO torrents.torrent_facet_values (
    torrent_id,
    category_id,
    facet_id,
    option_key,
    selection_mode,
    position,
    created_at
)
SELECT
    sqlc.arg(torrent_id)::bigint,
    allowed.category_id,
    allowed.facet_id,
    allowed.option_key,
    allowed.selection_mode,
    sqlc.arg(position)::integer,
    sqlc.arg(created_at)::timestamptz
FROM catalog.category_facet_options AS allowed
JOIN catalog.facet_definitions AS facet
  ON facet.id = allowed.facet_id
 AND facet.selection_mode = allowed.selection_mode
 AND facet.enabled = true
JOIN catalog.facet_options AS option
  ON option.facet_id = allowed.facet_id
 AND option.option_key = allowed.option_key
 AND option.selection_mode = allowed.selection_mode
 AND option.enabled = true
WHERE allowed.category_id = sqlc.arg(category_id)::text
  AND allowed.facet_id = sqlc.arg(facet_id)::text
  AND allowed.option_key = sqlc.arg(option_key)::text
  AND allowed.enabled = true;

-- name: ResolveTorrentScreenshotObjectFromUpload :one
WITH inserted AS (
    INSERT INTO torrents.torrent_screenshot_objects (
        id,
        content_sha256,
        byte_length,
        content_type,
        width,
        height,
        created_at
    ) VALUES (
        sqlc.arg(object_id)::uuid,
        sqlc.arg(content_sha256)::bytea,
        sqlc.arg(byte_length)::bigint,
        sqlc.arg(content_type)::text,
        sqlc.arg(width)::integer,
        sqlc.arg(height)::integer,
        sqlc.arg(created_at)::timestamptz
    )
    ON CONFLICT (content_sha256) DO NOTHING
    RETURNING id
)
SELECT id FROM inserted
UNION ALL
SELECT existing.id
FROM torrents.torrent_screenshot_objects AS existing
WHERE existing.content_sha256 = sqlc.arg(content_sha256)::bytea
LIMIT 1;

-- name: InsertTorrentScreenshotLocationFromUpload :execrows
INSERT INTO torrents.torrent_screenshot_object_locations (
    id,
    object_id,
    backend_id,
    object_key,
    version_id,
    observed_byte_length,
    observed_sha256,
    verified_at,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(location_id)::uuid,
    sqlc.arg(object_id)::uuid,
    sqlc.arg(backend_id)::text,
    sqlc.arg(object_key)::text,
    sqlc.narg(version_id)::text,
    sqlc.arg(observed_byte_length)::bigint,
    sqlc.arg(observed_sha256)::bytea,
    sqlc.arg(verified_at)::timestamptz,
    sqlc.arg(verified_at)::timestamptz,
    sqlc.arg(verified_at)::timestamptz
)
ON CONFLICT (object_id, backend_id) DO NOTHING;

-- name: TorrentScreenshotLocationMatches :one
SELECT EXISTS (
    SELECT 1
    FROM torrents.torrent_screenshot_object_locations
    WHERE object_id = sqlc.arg(object_id)::uuid
      AND backend_id = sqlc.arg(backend_id)::text
      AND object_key = sqlc.arg(object_key)::text
      AND observed_byte_length = sqlc.arg(observed_byte_length)::bigint
      AND observed_sha256 = sqlc.arg(observed_sha256)::bytea
);

-- name: InsertTorrentScreenshotFromUpload :exec
INSERT INTO torrents.torrent_screenshots (
    torrent_id,
    object_id,
    position,
    created_at
) VALUES (
    sqlc.arg(torrent_id)::bigint,
    sqlc.arg(object_id)::uuid,
    sqlc.arg(position)::smallint,
    sqlc.arg(created_at)::timestamptz
);

-- name: TorrentUploadRequiredFacetsSatisfied :one
SELECT NOT EXISTS (
    SELECT 1
    FROM catalog.category_facets AS required_facet
    JOIN catalog.facet_definitions AS facet
      ON facet.id = required_facet.facet_id
     AND facet.selection_mode = required_facet.selection_mode
     AND facet.enabled = true
    WHERE required_facet.category_id = sqlc.arg(category_id)::text
      AND required_facet.required = true
      AND NOT EXISTS (
          SELECT 1
          FROM torrents.torrent_facet_values AS value
          WHERE value.torrent_id = sqlc.arg(torrent_id)::bigint
            AND value.category_id = required_facet.category_id
            AND value.facet_id = required_facet.facet_id
    )
) AND NOT EXISTS (
    SELECT 1
    FROM (
        SELECT grouped_facet.requirement_group
        FROM catalog.category_facets AS grouped_facet
        JOIN catalog.facet_definitions AS facet
          ON facet.id = grouped_facet.facet_id
         AND facet.selection_mode = grouped_facet.selection_mode
         AND facet.enabled = true
        WHERE grouped_facet.category_id = sqlc.arg(category_id)::text
          AND grouped_facet.requirement_group IS NOT NULL
        GROUP BY grouped_facet.requirement_group
    ) AS required_group
    WHERE NOT EXISTS (
        SELECT 1
        FROM torrents.torrent_facet_values AS value
        JOIN catalog.category_facets AS member
          ON member.category_id = value.category_id
         AND member.facet_id = value.facet_id
         AND member.selection_mode = value.selection_mode
        WHERE value.torrent_id = sqlc.arg(torrent_id)::bigint
          AND value.category_id = sqlc.arg(category_id)::text
          AND member.requirement_group = required_group.requirement_group
    )
)::boolean AS satisfied;

-- name: CompleteTorrentUpload :execrows
UPDATE torrents.torrent_uploads
SET state = 'completed',
    torrent_id = sqlc.arg(torrent_id)::bigint,
    completed_at = sqlc.arg(completed_at)::timestamptz,
    cleanup_available_at = sqlc.arg(completed_at)::timestamptz,
    last_error_code = NULL,
    updated_at = sqlc.arg(completed_at)::timestamptz,
    version = version + 1
WHERE id = sqlc.arg(upload_id)::uuid
  AND state = 'object_verified';

-- name: ClaimTorrentUploadCleanupTasks :many
WITH candidates AS (
    SELECT
        upload.id,
        upload.backend_id,
        upload.object_key,
        upload.storage_version_id,
        (upload.object_created IS TRUE)::boolean AS delete_object
    FROM torrents.torrent_uploads AS upload
    WHERE upload.backend_id = sqlc.arg(backend_id)::text
      AND upload.state IN ('reserved', 'object_verified', 'cleaning')
      AND COALESCE(upload.object_verified_at, upload.created_at)
            <= sqlc.arg(eligible_before)::timestamptz
      AND upload.cleanup_available_at <= sqlc.arg(claimed_at)::timestamptz
      AND (upload.cleanup_lease_until IS NULL
           OR upload.cleanup_lease_until <= sqlc.arg(claimed_at)::timestamptz)
      AND upload.torrent_id IS NULL
      AND NOT EXISTS (
          SELECT 1
          FROM torrents.torrent_objects AS object
          WHERE object.content_sha256 = upload.content_sha256
      )
    ORDER BY upload.cleanup_available_at, upload.created_at, upload.id
    FOR UPDATE OF upload SKIP LOCKED
    LIMIT sqlc.arg(batch_size)::integer
), claimed AS (
    UPDATE torrents.torrent_uploads AS upload
    SET state = 'cleaning',
        cleanup_attempts = upload.cleanup_attempts + 1,
        cleanup_lease_until = sqlc.arg(lease_until)::timestamptz,
        cleanup_lease_token = gen_random_uuid(),
        last_error_code = NULL,
        updated_at = sqlc.arg(claimed_at)::timestamptz,
        version = upload.version + 1
    FROM candidates
    WHERE upload.id = candidates.id
    RETURNING
        upload.id,
        upload.cleanup_lease_token,
        upload.cleanup_attempts,
        candidates.backend_id,
        candidates.object_key,
        candidates.storage_version_id,
        candidates.delete_object
)
SELECT * FROM claimed;

-- name: MarkTorrentUploadAbandoned :execrows
UPDATE torrents.torrent_uploads AS upload
SET state = 'abandoned',
    cleanup_lease_until = NULL,
    cleanup_lease_token = NULL,
    abandoned_at = sqlc.arg(abandoned_at)::timestamptz,
    last_error_code = NULL,
    updated_at = sqlc.arg(abandoned_at)::timestamptz,
    version = upload.version + 1
WHERE upload.id = sqlc.arg(upload_id)::uuid
  AND upload.state = 'cleaning'
  AND upload.cleanup_lease_token = sqlc.arg(lease_token)::uuid
  AND upload.torrent_id IS NULL
  AND NOT EXISTS (
      SELECT 1
      FROM torrents.torrent_objects AS object
      WHERE object.content_sha256 = upload.content_sha256
  );

-- name: ReleaseTorrentUploadCleanupTask :execrows
UPDATE torrents.torrent_uploads
SET state = CASE
        WHEN object_verified_at IS NULL THEN 'reserved'
        ELSE 'object_verified'
    END,
    cleanup_available_at = sqlc.arg(available_at)::timestamptz,
    cleanup_lease_until = NULL,
    cleanup_lease_token = NULL,
    last_error_code = sqlc.arg(last_error_code)::text,
    updated_at = sqlc.arg(released_at)::timestamptz,
    version = version + 1
WHERE id = sqlc.arg(upload_id)::uuid
  AND state = 'cleaning'
  AND cleanup_lease_token = sqlc.arg(lease_token)::uuid;
