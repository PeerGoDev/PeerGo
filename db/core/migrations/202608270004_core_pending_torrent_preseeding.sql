-- +goose Up

-- Pending submissions are not public catalog entries, but their original
-- uploader must be able to seed the immutable payload while review is in
-- progress. New uploads append this event transactionally in Core. This
-- one-time repair admits submissions that were already pending at rollout;
-- rejection appends the next disabled aggregate version.
WITH pending AS (
    SELECT
        torrent.id AS torrent_id,
        torrent.info_hash_v1,
        torrent.total_size_bytes,
        torrent.version AS torrent_version,
        gen_random_uuid() AS event_id,
        CURRENT_TIMESTAMP AS occurred_at
    FROM torrents.torrents AS torrent
    WHERE torrent.state = 'pending_review'
      AND NOT EXISTS (
          SELECT 1
          FROM tracker_control.outbox AS existing
          WHERE existing.aggregate_id = torrent.id
            AND existing.aggregate_version = torrent.version
      )
), payloads AS (
    SELECT
        pending.*,
        jsonb_build_object(
            'schema_version', '2.0.0',
            'event_type', 'tracker.torrent-eligibility.changed',
            'event_id', pending.event_id,
            'occurred_at', pending.occurred_at,
            'torrent_id', pending.torrent_id,
            'info_hash_v1', encode(pending.info_hash_v1, 'hex'),
            'total_size_bytes', pending.total_size_bytes,
            'enabled', true,
            'torrent_version', pending.torrent_version
        )::text AS payload_json
    FROM pending
)
INSERT INTO tracker_control.outbox (
    event_id,
    event_type,
    schema_version,
    aggregate_id,
    aggregate_version,
    occurred_at,
    payload_json,
    payload_sha256,
    available_at
)
SELECT
    payloads.event_id,
    'tracker.torrent-eligibility.changed',
    '2.0.0',
    payloads.torrent_id,
    payloads.torrent_version,
    payloads.occurred_at,
    payloads.payload_json,
    sha256(convert_to(payloads.payload_json, 'UTF8')),
    payloads.occurred_at
FROM payloads
ORDER BY payloads.torrent_id;

-- +goose Down

-- Tracker control evidence is immutable and a rollback to producers that do
-- not disable rejected pre-seeding swarms would be unsafe. Restore a database
-- backup together with the previous application release instead.
DO $$
BEGIN
    RAISE EXCEPTION '202608270004 is irreversible after pending torrent pre-seeding admission';
END;
$$;
