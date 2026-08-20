-- name: LockPolicyTimeline :exec
-- Policy writes and final settlement share one transaction-scoped lock. This
-- prevents a newly appended historical revision from racing a worker that has
-- already resolved the same raw interval but has not committed it yet.
SELECT pg_advisory_xact_lock(hashtextextended('peergo-settlement-policy-timeline-v1', 0));

-- name: AppendPolicyTimelineRevision :execrows
INSERT INTO settlement.policy_timeline_revisions (
    id,
    scope_user_id,
    scope_torrent_id,
    scope_torrent_control_sequence,
    scope_subject_control_sequence,
    effective_at,
    revision_source,
    revision_id,
    revision_version,
    profile,
    snapshot_json,
    snapshot_sha256,
    recorded_at
) VALUES (
    sqlc.arg(id)::uuid,
    sqlc.narg(scope_user_id)::uuid,
    sqlc.narg(scope_torrent_id)::bigint,
    sqlc.narg(scope_torrent_control_sequence)::bigint,
    sqlc.narg(scope_subject_control_sequence)::bigint,
    sqlc.arg(effective_at)::timestamptz,
    sqlc.arg(revision_source)::text,
    sqlc.arg(revision_id)::text,
    sqlc.arg(revision_version)::bigint,
    sqlc.arg(profile)::text,
    sqlc.arg(snapshot_json)::text,
    sqlc.arg(snapshot_sha256)::bytea,
    sqlc.arg(recorded_at)::timestamptz
)
ON CONFLICT (id) DO NOTHING;

-- name: GetPolicyTimelineRevision :one
SELECT
    id,
    scope_user_id,
    scope_torrent_id,
    scope_torrent_control_sequence,
    scope_subject_control_sequence,
    effective_at,
    revision_source,
    revision_id,
    revision_version,
    profile,
    snapshot_json,
    snapshot_sha256,
    recorded_at
FROM settlement.policy_timeline_revisions
WHERE id = sqlc.arg(id)::uuid;

-- name: ListPromotionRulesForInterval :many
SELECT
    id,
    scope_type,
    torrent_id,
    promotion,
    starts_at,
    ends_at,
    override_lower_scopes,
    reason_code,
    command_json,
    command_sha256,
    recorded_at
FROM settlement.promotion_rules
WHERE starts_at < sqlc.arg(interval_ends_at)::timestamptz
  AND ends_at > sqlc.arg(interval_starts_at)::timestamptz
  AND (scope_type = 'global' OR torrent_id = sqlc.arg(torrent_id)::bigint)
ORDER BY starts_at, scope_type, id;

-- name: ListWorkgroupBenefitTransitionsForInterval :many
SELECT
    transition_id,
    user_id,
    group_kind,
    entitlement,
    active,
    state_version,
    effective_at
FROM settlement.workgroup_benefit_transitions
WHERE user_id = sqlc.arg(user_id)::uuid
  AND effective_at < sqlc.arg(interval_ends_at)::timestamptz
ORDER BY effective_at, state_version, transition_id;

-- name: ListVIPBenefitTransitionsForInterval :many
SELECT
    transition_id,
    user_id,
    entitlement,
    enabled,
    active_until,
    state_version,
    effective_at
FROM settlement.vip_benefit_transitions
WHERE user_id = sqlc.arg(user_id)::uuid
  AND effective_at < sqlc.arg(interval_ends_at)::timestamptz
ORDER BY effective_at, state_version, transition_id;

-- name: InsertSpeedObservation :exec
INSERT INTO ledger.speed_observations (
    interval_event_id,
    interval_duration_nanoseconds,
    raw_uploaded,
    average_upload_bytes_per_second,
    outcome,
    observed_at
) VALUES (
    sqlc.arg(interval_event_id)::uuid,
    sqlc.arg(interval_duration_nanoseconds)::bigint,
    sqlc.arg(raw_uploaded)::bigint,
    sqlc.arg(average_upload_bytes_per_second)::bigint,
    sqlc.arg(outcome)::text,
    sqlc.arg(observed_at)::timestamptz
);

-- name: GetPromotionRule :one
SELECT
    id,
    scope_type,
    torrent_id,
    promotion,
    starts_at,
    ends_at,
    override_lower_scopes,
    reason_code,
    command_json,
    command_sha256,
    recorded_at
FROM settlement.promotion_rules
WHERE id = sqlc.arg(id)::uuid;

-- name: PromotionRuleScopeOverlaps :one
SELECT EXISTS (
    SELECT 1
    FROM settlement.promotion_rules
    WHERE scope_type = sqlc.arg(scope_type)::text
      AND (
          sqlc.arg(scope_type)::text = 'global'
          OR torrent_id = sqlc.narg(torrent_id)::bigint
      )
      AND starts_at < sqlc.arg(ends_at)::timestamptz
      AND ends_at > sqlc.arg(starts_at)::timestamptz
) AS overlaps;

-- name: AppendPromotionRule :execrows
INSERT INTO settlement.promotion_rules (
    id,
    scope_type,
    torrent_id,
    promotion,
    starts_at,
    ends_at,
    override_lower_scopes,
    reason_code,
    command_json,
    command_sha256,
    recorded_at
) VALUES (
    sqlc.arg(id)::uuid,
    sqlc.arg(scope_type)::text,
    sqlc.narg(torrent_id)::bigint,
    sqlc.arg(promotion)::text,
    sqlc.arg(starts_at)::timestamptz,
    sqlc.arg(ends_at)::timestamptz,
    sqlc.arg(override_lower_scopes)::boolean,
    sqlc.arg(reason_code)::text,
    sqlc.arg(command_json)::text,
    sqlc.arg(command_sha256)::bytea,
    sqlc.arg(recorded_at)::timestamptz
)
ON CONFLICT (id) DO NOTHING;

-- name: ClaimNextPolicyWork :one
WITH candidate AS (
    SELECT work.interval_event_id
    FROM settlement.policy_work AS work
    WHERE work.settled_at IS NULL
      AND work.available_at <= sqlc.arg(claimed_at)::timestamptz
      AND (work.lease_until IS NULL OR work.lease_until <= sqlc.arg(claimed_at)::timestamptz)
    ORDER BY work.available_at, work.interval_event_id
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE settlement.policy_work AS work
SET
    lease_token = sqlc.arg(lease_token)::uuid,
    lease_until = sqlc.arg(lease_until)::timestamptz,
    attempts = work.attempts + 1,
    last_error_code = NULL
FROM candidate
INNER JOIN ledger.raw_session_intervals AS raw ON raw.event_id = candidate.interval_event_id
WHERE work.interval_event_id = candidate.interval_event_id
RETURNING
    work.interval_event_id,
    work.lease_token,
    work.attempts,
    raw.user_id,
    raw.torrent_id,
    raw.starts_at,
    raw.ends_at,
    raw.raw_uploaded,
    raw.raw_downloaded,
    raw.torrent_control_sequence,
    raw.subject_control_sequence,
    raw.network_policy_sequence,
    raw.network_policy_revision,
    raw.network_class,
    raw.network_rule_id,
    raw.seedbox_upload_factor_basis_points,
    raw.seedbox_download_factor_basis_points,
    raw.seedbox_download_factor_explicit,
    raw.speed_limit_bytes_per_second;

-- name: GetClaimedPolicyWorkForUpdate :one
SELECT
    work.interval_event_id,
    work.attempts,
    raw.user_id,
    raw.torrent_id,
    raw.starts_at,
    raw.ends_at,
    raw.raw_uploaded,
    raw.raw_downloaded,
    raw.torrent_control_sequence,
    raw.subject_control_sequence,
    raw.network_policy_sequence,
    raw.network_policy_revision,
    raw.network_class,
    raw.network_rule_id,
    raw.seedbox_upload_factor_basis_points,
    raw.seedbox_download_factor_basis_points,
    raw.seedbox_download_factor_explicit,
    raw.speed_limit_bytes_per_second
FROM settlement.policy_work AS work
INNER JOIN ledger.raw_session_intervals AS raw ON raw.event_id = work.interval_event_id
WHERE work.interval_event_id = sqlc.arg(interval_event_id)::uuid
  AND work.lease_token = sqlc.arg(lease_token)::uuid
  AND work.settled_at IS NULL
FOR UPDATE;

-- name: ListPolicyTimelineCandidates :many
SELECT
    id,
    scope_user_id,
    scope_torrent_id,
    scope_torrent_control_sequence,
    scope_subject_control_sequence,
    effective_at,
    revision_source,
    revision_id,
    revision_version,
    profile,
    snapshot_json,
    snapshot_sha256,
    recorded_at
FROM settlement.policy_timeline_revisions
WHERE effective_at < sqlc.arg(interval_ends_at)::timestamptz
  AND (scope_user_id IS NULL OR scope_user_id = sqlc.arg(user_id)::uuid)
  AND (scope_torrent_id IS NULL OR scope_torrent_id = sqlc.arg(torrent_id)::bigint)
  AND (
      scope_torrent_control_sequence IS NULL
      OR scope_torrent_control_sequence = sqlc.arg(torrent_control_sequence)::bigint
  )
  AND (
      scope_subject_control_sequence IS NULL
      OR scope_subject_control_sequence = sqlc.arg(subject_control_sequence)::bigint
  )
ORDER BY effective_at, id;

-- name: InsertTrafficSettlement :exec
INSERT INTO ledger.traffic_settlements (
    settlement_id,
    user_id,
    torrent_id,
    torrent_control_sequence,
    subject_control_sequence,
    interval_starts_at,
    interval_ends_at,
    raw_uploaded,
    raw_downloaded,
    credited_uploaded,
    charged_downloaded,
    settlement_sha256,
    settled_at,
    created_at
) VALUES (
    sqlc.arg(settlement_id)::uuid,
    sqlc.arg(user_id)::uuid,
    sqlc.arg(torrent_id)::bigint,
    sqlc.arg(torrent_control_sequence)::bigint,
    sqlc.arg(subject_control_sequence)::bigint,
    sqlc.arg(interval_starts_at)::timestamptz,
    sqlc.arg(interval_ends_at)::timestamptz,
    sqlc.arg(raw_uploaded)::bigint,
    sqlc.arg(raw_downloaded)::bigint,
    sqlc.arg(credited_uploaded)::bigint,
    sqlc.arg(charged_downloaded)::bigint,
    sqlc.arg(settlement_sha256)::bytea,
    sqlc.arg(settled_at)::timestamptz,
    sqlc.arg(created_at)::timestamptz
);

-- name: InsertTrafficSettlementSegment :exec
INSERT INTO ledger.traffic_settlement_segments (
    settlement_id,
    segment_index,
    starts_at,
    ends_at,
    policy_revision_source,
    policy_revision_id,
    policy_revision_version,
    policy_profile,
    policy_snapshot_sha256,
    applications_json,
    applications_sha256,
    raw_uploaded,
    raw_downloaded,
    credited_uploaded,
    charged_downloaded
) VALUES (
    sqlc.arg(settlement_id)::uuid,
    sqlc.arg(segment_index)::integer,
    sqlc.arg(starts_at)::timestamptz,
    sqlc.arg(ends_at)::timestamptz,
    sqlc.arg(policy_revision_source)::text,
    sqlc.arg(policy_revision_id)::text,
    sqlc.arg(policy_revision_version)::bigint,
    sqlc.arg(policy_profile)::text,
    sqlc.arg(policy_snapshot_sha256)::bytea,
    sqlc.arg(applications_json)::text,
    sqlc.arg(applications_sha256)::bytea,
    sqlc.arg(raw_uploaded)::bigint,
    sqlc.arg(raw_downloaded)::bigint,
    sqlc.arg(credited_uploaded)::bigint,
    sqlc.arg(charged_downloaded)::bigint
);

-- name: AppendTrafficOutboxEvent :exec
INSERT INTO settlement.traffic_outbox (
    event_id,
    settlement_id,
    event_type,
    schema_version,
    occurred_at,
    payload_json,
    payload_sha256,
    available_at,
    created_at
) VALUES (
    sqlc.arg(event_id)::uuid,
    sqlc.arg(settlement_id)::uuid,
    sqlc.arg(event_type)::text,
    sqlc.arg(schema_version)::text,
    sqlc.arg(occurred_at)::timestamptz,
    sqlc.arg(payload_json)::text,
    sqlc.arg(payload_sha256)::bytea,
    sqlc.arg(available_at)::timestamptz,
    sqlc.arg(created_at)::timestamptz
);

-- name: MarkPolicyWorkSettled :execrows
UPDATE settlement.policy_work
SET
    settled_at = sqlc.arg(settled_at)::timestamptz,
    lease_token = NULL,
    lease_until = NULL,
    last_error_code = NULL
WHERE interval_event_id = sqlc.arg(interval_event_id)::uuid
  AND lease_token = sqlc.arg(lease_token)::uuid
  AND settled_at IS NULL;

-- name: ReleasePolicyWork :execrows
UPDATE settlement.policy_work
SET
    available_at = sqlc.arg(available_at)::timestamptz,
    lease_token = NULL,
    lease_until = NULL,
    last_error_code = sqlc.arg(last_error_code)::text
WHERE interval_event_id = sqlc.arg(interval_event_id)::uuid
  AND lease_token = sqlc.arg(lease_token)::uuid
  AND settled_at IS NULL;

-- name: ClaimNextTrafficOutboxEvent :one
WITH candidate AS (
    SELECT outbox.event_id
    FROM settlement.traffic_outbox AS outbox
    WHERE outbox.published_at IS NULL
      AND outbox.available_at <= sqlc.arg(claimed_at)::timestamptz
      AND (outbox.lease_until IS NULL OR outbox.lease_until <= sqlc.arg(claimed_at)::timestamptz)
    ORDER BY outbox.available_at, outbox.event_id
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE settlement.traffic_outbox AS outbox
SET
    lease_token = sqlc.arg(lease_token)::uuid,
    lease_until = sqlc.arg(lease_until)::timestamptz,
    attempts = outbox.attempts + 1,
    last_error_code = NULL
FROM candidate
WHERE outbox.event_id = candidate.event_id
RETURNING
    outbox.event_id,
    outbox.settlement_id,
    outbox.event_type,
    outbox.schema_version,
    outbox.occurred_at,
    outbox.payload_json,
    outbox.payload_sha256,
    outbox.lease_token,
    outbox.attempts;

-- name: MarkTrafficOutboxEventPublished :execrows
UPDATE settlement.traffic_outbox
SET
    published_at = sqlc.arg(published_at)::timestamptz,
    lease_token = NULL,
    lease_until = NULL,
    last_error_code = NULL
WHERE event_id = sqlc.arg(event_id)::uuid
  AND lease_token = sqlc.arg(lease_token)::uuid
  AND published_at IS NULL;

-- name: ReleaseTrafficOutboxEvent :execrows
UPDATE settlement.traffic_outbox
SET
    available_at = sqlc.arg(available_at)::timestamptz,
    lease_token = NULL,
    lease_until = NULL,
    last_error_code = sqlc.arg(last_error_code)::text
WHERE event_id = sqlc.arg(event_id)::uuid
  AND lease_token = sqlc.arg(lease_token)::uuid
  AND published_at IS NULL;
