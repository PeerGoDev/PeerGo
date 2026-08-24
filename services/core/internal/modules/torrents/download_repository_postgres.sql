-- name: GetPublishedTorrentDownloadObject :one
SELECT
    torrent.id AS torrent_id,
    torrent.title,
    site.torrent_filename_prefix,
    object.id AS object_id,
    object.content_sha256,
    object.byte_length,
    object.info_offset,
    object.info_length
FROM torrents.torrents AS torrent
JOIN torrents.torrent_objects AS object ON object.id = torrent.object_id
CROSS JOIN catalog.site_profile AS site
WHERE torrent.id = sqlc.arg(torrent_id)::bigint
  AND torrent.state = 'published'
  AND site.singleton = true;

-- name: ListReadableTorrentObjectLocations :many
SELECT
    location.id,
    location.object_id,
    location.backend_id,
    location.object_key,
    location.state,
    location.is_preferred,
    location.version_id,
    location.observed_byte_length,
    location.observed_sha256,
    location.verified_at
FROM torrents.torrent_object_locations AS location
WHERE location.object_id = sqlc.arg(object_id)::uuid
  AND location.state IN ('verified', 'retiring')
ORDER BY
    location.is_preferred DESC,
    (location.state = 'verified') DESC,
    location.verified_at DESC,
    location.id;
