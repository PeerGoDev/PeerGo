-- name: InsertTrafficSettlementInbox :one
INSERT INTO traffic.settlement_inbox (
    event_id,
    payload_sha256,
    occurred_at,
    received_at,
    applied_at
) VALUES (
    sqlc.arg(event_id)::uuid,
    sqlc.arg(payload_sha256)::bytea,
    sqlc.arg(occurred_at)::timestamptz,
    sqlc.arg(received_at)::timestamptz,
    sqlc.arg(applied_at)::timestamptz
)
ON CONFLICT (event_id) DO NOTHING
RETURNING event_id;

-- name: GetTrafficSettlementInbox :one
SELECT payload_sha256
FROM traffic.settlement_inbox
WHERE event_id = sqlc.arg(event_id)::uuid;

-- name: InsertUserTrafficEntry :exec
INSERT INTO traffic.user_traffic_entries (
    settlement_id,
    user_id,
    torrent_id,
    interval_starts_at,
    interval_ends_at,
    raw_uploaded,
    raw_downloaded,
    credited_uploaded,
    charged_downloaded,
    settlement_sha256,
    occurred_at,
    applied_at
) VALUES (
    sqlc.arg(settlement_id)::uuid,
    sqlc.arg(user_id)::uuid,
    sqlc.arg(torrent_id)::bigint,
    sqlc.arg(interval_starts_at)::timestamptz,
    sqlc.arg(interval_ends_at)::timestamptz,
    sqlc.arg(raw_uploaded)::bigint,
    sqlc.arg(raw_downloaded)::bigint,
    sqlc.arg(credited_uploaded)::bigint,
    sqlc.arg(charged_downloaded)::bigint,
    sqlc.arg(settlement_sha256)::bytea,
    sqlc.arg(occurred_at)::timestamptz,
    sqlc.arg(applied_at)::timestamptz
);

-- name: InsertUserTrafficExplanation :exec
INSERT INTO traffic.user_traffic_entry_explanations (
    settlement_id,
    status,
    segment_count
) VALUES (
    sqlc.arg(settlement_id)::uuid,
    sqlc.arg(status)::text,
    sqlc.arg(segment_count)::integer
);

-- name: InsertUserTrafficExplanationSegment :exec
INSERT INTO traffic.user_traffic_entry_segments (
    settlement_id,
    segment_index,
    starts_at,
    ends_at,
    raw_uploaded,
    raw_downloaded,
    credited_uploaded,
    charged_downloaded
) VALUES (
    sqlc.arg(settlement_id)::uuid,
    sqlc.arg(segment_index)::integer,
    sqlc.arg(starts_at)::timestamptz,
    sqlc.arg(ends_at)::timestamptz,
    sqlc.arg(raw_uploaded)::bigint,
    sqlc.arg(raw_downloaded)::bigint,
    sqlc.arg(credited_uploaded)::bigint,
    sqlc.arg(charged_downloaded)::bigint
);

-- name: UpsertUserTrafficTotals :exec
INSERT INTO traffic.user_totals (
    user_id,
    raw_uploaded,
    raw_downloaded,
    credited_uploaded,
    charged_downloaded,
    entry_count,
    version,
    last_occurred_at,
    updated_at
) VALUES (
    sqlc.arg(user_id)::uuid,
    sqlc.arg(raw_uploaded)::bigint,
    sqlc.arg(raw_downloaded)::bigint,
    sqlc.arg(credited_uploaded)::bigint,
    sqlc.arg(charged_downloaded)::bigint,
    1,
    1,
    sqlc.arg(occurred_at)::timestamptz,
    sqlc.arg(updated_at)::timestamptz
)
ON CONFLICT (user_id) DO UPDATE SET
    raw_uploaded = traffic.user_totals.raw_uploaded + EXCLUDED.raw_uploaded,
    raw_downloaded = traffic.user_totals.raw_downloaded + EXCLUDED.raw_downloaded,
    credited_uploaded = traffic.user_totals.credited_uploaded + EXCLUDED.credited_uploaded,
    charged_downloaded = traffic.user_totals.charged_downloaded + EXCLUDED.charged_downloaded,
    entry_count = traffic.user_totals.entry_count + 1,
    version = traffic.user_totals.version + 1,
    last_occurred_at = GREATEST(traffic.user_totals.last_occurred_at, EXCLUDED.last_occurred_at),
    updated_at = EXCLUDED.updated_at;

-- name: UpsertUserTorrentTrafficTotals :exec
INSERT INTO traffic.user_torrent_totals (
    user_id,
    torrent_id,
    raw_uploaded,
    raw_downloaded,
    credited_uploaded,
    charged_downloaded,
    entry_count,
    version,
    last_occurred_at,
    updated_at
) VALUES (
    sqlc.arg(user_id)::uuid,
    sqlc.arg(torrent_id)::bigint,
    sqlc.arg(raw_uploaded)::bigint,
    sqlc.arg(raw_downloaded)::bigint,
    sqlc.arg(credited_uploaded)::bigint,
    sqlc.arg(charged_downloaded)::bigint,
    1,
    1,
    sqlc.arg(occurred_at)::timestamptz,
    sqlc.arg(updated_at)::timestamptz
)
ON CONFLICT (user_id, torrent_id) DO UPDATE SET
    raw_uploaded = traffic.user_torrent_totals.raw_uploaded + EXCLUDED.raw_uploaded,
    raw_downloaded = traffic.user_torrent_totals.raw_downloaded + EXCLUDED.raw_downloaded,
    credited_uploaded = traffic.user_torrent_totals.credited_uploaded + EXCLUDED.credited_uploaded,
    charged_downloaded = traffic.user_torrent_totals.charged_downloaded + EXCLUDED.charged_downloaded,
    entry_count = traffic.user_torrent_totals.entry_count + 1,
    version = traffic.user_torrent_totals.version + 1,
    last_occurred_at = GREATEST(traffic.user_torrent_totals.last_occurred_at, EXCLUDED.last_occurred_at),
    updated_at = EXCLUDED.updated_at;

-- name: GetUserTrafficTotals :one
SELECT
    raw_uploaded,
    raw_downloaded,
    credited_uploaded,
    charged_downloaded,
    entry_count,
    last_occurred_at,
    updated_at
FROM traffic.user_totals
WHERE user_id = sqlc.arg(user_id)::uuid;

-- name: ListUserTrafficEntries :many
SELECT
    entry.rollup_id AS settlement_id,
    torrent.id AS torrent_id,
    torrent.title AS torrent_title,
    entry.interval_starts_at,
    entry.interval_ends_at,
    entry.raw_uploaded,
    entry.raw_downloaded,
    entry.credited_uploaded,
    entry.charged_downloaded,
    entry.last_occurred_at AS occurred_at,
    NULL::text AS explanation_status,
    NULL::integer AS explanation_segment_count
FROM traffic.user_traffic_three_hour_rollups AS entry
JOIN torrents.torrents AS torrent ON torrent.id = entry.torrent_id
WHERE entry.user_id = sqlc.arg(user_id)::uuid
ORDER BY entry.interval_ends_at DESC, entry.bucket_start DESC, entry.rollup_id DESC
LIMIT sqlc.arg(result_limit)::integer;

-- name: ListUserTrafficExplanationSegments :many
SELECT
    settlement_id,
    segment_index,
    starts_at,
    ends_at,
    raw_uploaded,
    raw_downloaded,
    credited_uploaded,
    charged_downloaded
FROM traffic.user_traffic_entry_segments
WHERE settlement_id = ANY(sqlc.arg(settlement_ids)::uuid[])
ORDER BY settlement_id, segment_index;

-- name: ListTrafficProjectionCleanupCandidates :many
SELECT entry.settlement_id
FROM traffic.user_traffic_entries AS entry
CROSS JOIN progression.contribution_upload_cursor AS cursor
WHERE cursor.singleton = true
  AND entry.applied_at < sqlc.arg(detail_before)::timestamptz
  AND (
      entry.raw_uploaded = 0
      OR NOT EXISTS (
          SELECT 1
          FROM progression.contribution_experience_policy_revisions
      )
      OR entry.occurred_at < (
          SELECT min(policy.effective_from)
          FROM progression.contribution_experience_policy_revisions AS policy
      )
      OR entry.projection_sequence <= cursor.last_projection_sequence
  )
ORDER BY entry.applied_at, entry.projection_sequence
LIMIT sqlc.arg(batch_size)::integer
FOR UPDATE OF entry SKIP LOCKED;

-- name: DeleteTrafficProjectionSegments :execrows
DELETE FROM traffic.user_traffic_entry_segments
WHERE settlement_id = ANY(sqlc.arg(settlement_ids)::uuid[]);

-- name: DeleteTrafficProjectionExplanations :execrows
DELETE FROM traffic.user_traffic_entry_explanations
WHERE settlement_id = ANY(sqlc.arg(settlement_ids)::uuid[]);

-- name: DeleteTrafficProjectionEntries :execrows
DELETE FROM traffic.user_traffic_entries
WHERE settlement_id = ANY(sqlc.arg(settlement_ids)::uuid[]);

-- name: DeleteTrafficProjectionInbox :execrows
DELETE FROM traffic.settlement_inbox
WHERE event_id = ANY(sqlc.arg(settlement_ids)::uuid[]);

-- name: DeleteTrafficHistoryRollups :execrows
WITH candidate AS (
    SELECT rollup.bucket_start, rollup.user_id, rollup.torrent_id
    FROM traffic.user_traffic_three_hour_rollups AS rollup
    WHERE rollup.bucket_start < sqlc.arg(history_before)::timestamptz
    ORDER BY rollup.bucket_start, rollup.user_id, rollup.torrent_id
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE SKIP LOCKED
)
DELETE FROM traffic.user_traffic_three_hour_rollups AS rollup
USING candidate
WHERE rollup.bucket_start = candidate.bucket_start
  AND rollup.user_id = candidate.user_id
  AND rollup.torrent_id = candidate.torrent_id;

-- name: GetHNRProjectionInbox :one
SELECT payload_sha256, payload_json
FROM traffic.hnr_projection_inbox
WHERE event_id = sqlc.arg(event_id)::uuid;

-- name: LockHNRProjectionAggregate :exec
SELECT pg_advisory_xact_lock(hashtextextended(
    'peergo-core-hnr-projection-v1:' || sqlc.arg(obligation_id)::uuid::text,
    0
));

-- name: InsertHNRProjectionInbox :exec
INSERT INTO traffic.hnr_projection_inbox (
    event_id,
    payload_sha256,
    payload_json,
    obligation_id,
    obligation_version,
    occurred_at,
    received_at,
    applied_at
) VALUES (
    sqlc.arg(event_id)::uuid,
    sqlc.arg(payload_sha256)::bytea,
    sqlc.arg(payload_json)::text,
    sqlc.arg(obligation_id)::uuid,
    sqlc.arg(obligation_version)::bigint,
    sqlc.arg(occurred_at)::timestamptz,
    sqlc.arg(received_at)::timestamptz,
    sqlc.arg(applied_at)::timestamptz
);

-- name: GetUserHNRObligationForUpdate :one
SELECT *
FROM traffic.user_hnr_obligations
WHERE obligation_id = sqlc.arg(obligation_id)::uuid
FOR UPDATE;

-- name: InsertUserHNRObligation :exec
INSERT INTO traffic.user_hnr_obligations (
    obligation_id,
    user_id,
    torrent_id,
    completed_at,
    state,
    seeded_seconds,
    required_seed_seconds,
    raw_uploaded,
    raw_downloaded,
    raw_ratio_basis_points,
    required_ratio_basis_points,
    assessment_due_at,
    grace_ends_at,
    satisfied_by,
    satisfied_at,
    version,
    occurred_at,
    applied_at
) VALUES (
    sqlc.arg(obligation_id)::uuid,
    sqlc.arg(user_id)::uuid,
    sqlc.arg(torrent_id)::bigint,
    sqlc.arg(completed_at)::timestamptz,
    sqlc.arg(state)::text,
    sqlc.arg(seeded_seconds)::bigint,
    sqlc.arg(required_seed_seconds)::bigint,
    sqlc.arg(raw_uploaded)::bigint,
    sqlc.arg(raw_downloaded)::bigint,
    sqlc.arg(raw_ratio_basis_points)::bigint,
    sqlc.arg(required_ratio_basis_points)::bigint,
    sqlc.arg(assessment_due_at)::timestamptz,
    sqlc.arg(grace_ends_at)::timestamptz,
    sqlc.narg(satisfied_by)::text,
    sqlc.narg(satisfied_at)::timestamptz,
    sqlc.arg(version)::bigint,
    sqlc.arg(occurred_at)::timestamptz,
    sqlc.arg(applied_at)::timestamptz
);

-- name: UpdateUserHNRObligation :execrows
UPDATE traffic.user_hnr_obligations
SET
    state = sqlc.arg(state)::text,
    seeded_seconds = sqlc.arg(seeded_seconds)::bigint,
    raw_uploaded = sqlc.arg(raw_uploaded)::bigint,
    raw_ratio_basis_points = sqlc.arg(raw_ratio_basis_points)::bigint,
    satisfied_by = sqlc.narg(satisfied_by)::text,
    satisfied_at = sqlc.narg(satisfied_at)::timestamptz,
    version = sqlc.arg(new_version)::bigint,
    occurred_at = sqlc.arg(occurred_at)::timestamptz,
    applied_at = sqlc.arg(applied_at)::timestamptz
WHERE obligation_id = sqlc.arg(obligation_id)::uuid
  AND version = sqlc.arg(expected_version)::bigint
  AND state = 'tracking';

-- name: GetUserHNRSummary :one
WITH projected AS (
    SELECT CASE
        WHEN exemption.obligation_id IS NOT NULL THEN 'exempt'
        WHEN state = 'satisfied' THEN 'satisfied'
        WHEN state = 'exempt' THEN 'exempt'
        WHEN CURRENT_TIMESTAMP < assessment_due_at THEN 'tracking'
        WHEN CURRENT_TIMESTAMP < grace_ends_at THEN 'grace'
        ELSE 'overdue'
    END AS display_status
    FROM traffic.user_hnr_obligations AS obligation
    LEFT JOIN traffic.hnr_appeal_exemptions AS exemption
      ON exemption.obligation_id = obligation.obligation_id
    WHERE obligation.user_id = sqlc.arg(user_id)::uuid
)
SELECT
    CURRENT_TIMESTAMP::timestamptz AS as_of,
    count(*)::bigint AS total,
    count(*) FILTER (WHERE display_status = 'tracking')::bigint AS tracking,
    count(*) FILTER (WHERE display_status = 'grace')::bigint AS grace,
    count(*) FILTER (WHERE display_status = 'overdue')::bigint AS overdue,
    count(*) FILTER (WHERE display_status = 'satisfied')::bigint AS satisfied,
    count(*) FILTER (WHERE display_status = 'exempt')::bigint AS exempt
FROM projected;

-- name: ListUserHNRObligations :many
WITH projected AS (
    SELECT
        obligation.obligation_id,
        torrent.id AS torrent_id,
        torrent.title AS torrent_title,
        obligation.completed_at,
        CASE
            WHEN exemption.obligation_id IS NOT NULL THEN 'exempt'
            WHEN obligation.state = 'satisfied' THEN 'satisfied'
            WHEN obligation.state = 'exempt' THEN 'exempt'
            WHEN CURRENT_TIMESTAMP < obligation.assessment_due_at THEN 'tracking'
            WHEN CURRENT_TIMESTAMP < obligation.grace_ends_at THEN 'grace'
            ELSE 'overdue'
        END AS display_status,
        obligation.seeded_seconds,
        obligation.required_seed_seconds,
        obligation.raw_uploaded,
        obligation.raw_downloaded,
        obligation.raw_ratio_basis_points,
        obligation.required_ratio_basis_points,
        obligation.assessment_due_at,
        obligation.grace_ends_at,
        COALESCE((CASE
            WHEN exemption.obligation_id IS NOT NULL THEN 'exempt'
            ELSE obligation.satisfied_by
        END), '')::text AS satisfied_by,
        COALESCE(exemption.created_at, obligation.satisfied_at) AS satisfied_at,
        GREATEST(obligation.occurred_at, exemption.created_at)::timestamptz AS occurred_at,
        COALESCE((CASE
            WHEN appeal.id IS NULL THEN NULL
            ELSE COALESCE(resolution.outcome, 'pending')
        END), '')::text AS appeal_status,
        appeal.statement AS appeal_statement,
        appeal.created_at AS appeal_created_at,
        resolution.created_at AS appeal_resolved_at,
        resolution.response AS appeal_response,
        COALESCE((
            obligation.state = 'tracking'
            AND CURRENT_TIMESTAMP >= obligation.grace_ends_at
            AND appeal.id IS NULL
            AND exemption.obligation_id IS NULL
        ), false)::boolean AS can_appeal
    FROM traffic.user_hnr_obligations AS obligation
    INNER JOIN torrents.torrents AS torrent ON torrent.id = obligation.torrent_id
    LEFT JOIN traffic.hnr_appeal_exemptions AS exemption
      ON exemption.obligation_id = obligation.obligation_id
    LEFT JOIN traffic.hnr_appeals AS appeal
      ON appeal.obligation_id = obligation.obligation_id
    LEFT JOIN traffic.hnr_appeal_resolutions AS resolution
      ON resolution.appeal_id = appeal.id
    WHERE obligation.user_id = sqlc.arg(user_id)::uuid
      AND (
          sqlc.narg(cursor_completed_at)::timestamptz IS NULL
          OR (obligation.completed_at, obligation.obligation_id) < (
              sqlc.narg(cursor_completed_at)::timestamptz,
              sqlc.narg(cursor_obligation_id)::uuid
          )
      )
)
SELECT *
FROM projected
WHERE sqlc.arg(status_filter)::text = 'all'
   OR (sqlc.arg(status_filter)::text = 'open' AND display_status IN ('tracking', 'grace', 'overdue'))
   OR display_status = sqlc.arg(status_filter)::text
ORDER BY completed_at DESC, obligation_id DESC
LIMIT sqlc.arg(result_limit)::integer;
