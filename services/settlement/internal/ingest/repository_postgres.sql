-- name: ClaimInboxEvent :one
INSERT INTO settlement.event_inbox (
    event_id,
    payload_sha256,
    payload_json,
    source_stream,
    source_subject,
    source_sequence,
    delivery_count,
    received_at,
    ingested_at,
    outcome
) VALUES (
    sqlc.arg(event_id)::uuid,
    sqlc.arg(payload_sha256)::bytea,
    sqlc.arg(payload_json)::text,
    sqlc.arg(source_stream)::text,
    sqlc.arg(source_subject)::text,
    sqlc.arg(source_sequence)::bigint,
    sqlc.arg(delivery_count)::bigint,
    sqlc.arg(received_at)::timestamptz,
    sqlc.arg(ingested_at)::timestamptz,
    'processing'
)
ON CONFLICT (event_id) DO NOTHING
RETURNING event_id;

-- name: GetInboxEvent :one
SELECT payload_sha256, payload_json, outcome, session_epoch
FROM settlement.event_inbox
WHERE event_id = sqlc.arg(event_id)::uuid;

-- A transaction-scoped hash lock closes the empty-row race when two service
-- instances receive different events for a session that has no baseline yet.
-- Hash collisions only reduce concurrency; they cannot merge session state.
-- name: LockSettlementSession :exec
SELECT pg_advisory_xact_lock(hashtextextended(
    sqlc.arg(user_id)::text
    || ':' || sqlc.arg(torrent_id)::bigint::text
    || ':' || encode(sqlc.arg(session_token)::bytea, 'hex'),
    0
));

-- name: GetSettlementSessionForUpdate :one
SELECT
    user_id,
    torrent_id,
    session_token,
    info_hash_v1,
    session_epoch,
    version,
    last_event_id,
    last_received_at,
    last_event_kind,
    last_uploaded,
    last_downloaded,
    last_left,
    last_address_family,
    last_credential_version,
    torrent_control_sequence,
    subject_control_sequence
FROM settlement.session_states
WHERE user_id = sqlc.arg(user_id)::uuid
  AND torrent_id = sqlc.arg(torrent_id)::bigint
  AND session_token = sqlc.arg(session_token)::bytea
FOR UPDATE;

-- name: InsertSettlementSession :exec
INSERT INTO settlement.session_states (
    user_id,
    torrent_id,
    session_token,
    info_hash_v1,
    session_epoch,
    version,
    last_event_id,
    last_received_at,
    last_event_kind,
    last_uploaded,
    last_downloaded,
    last_left,
    last_address_family,
    last_credential_version,
    torrent_control_sequence,
    subject_control_sequence,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(user_id)::uuid,
    sqlc.arg(torrent_id)::bigint,
    sqlc.arg(session_token)::bytea,
    sqlc.arg(info_hash_v1)::bytea,
    sqlc.arg(session_epoch)::bigint,
    sqlc.arg(version)::bigint,
    sqlc.arg(last_event_id)::uuid,
    sqlc.arg(last_received_at)::timestamptz,
    sqlc.arg(last_event_kind)::text,
    sqlc.arg(last_uploaded)::bigint,
    sqlc.arg(last_downloaded)::bigint,
    sqlc.arg(last_left)::bigint,
    sqlc.arg(last_address_family)::smallint,
    sqlc.arg(last_credential_version)::bigint,
    sqlc.arg(torrent_control_sequence)::bigint,
    sqlc.arg(subject_control_sequence)::bigint,
    sqlc.arg(created_at)::timestamptz,
    sqlc.arg(updated_at)::timestamptz
);

-- name: UpdateSettlementSession :execrows
UPDATE settlement.session_states
SET
    session_epoch = sqlc.arg(session_epoch)::bigint,
    version = sqlc.arg(new_version)::bigint,
    last_event_id = sqlc.arg(last_event_id)::uuid,
    last_received_at = sqlc.arg(last_received_at)::timestamptz,
    last_event_kind = sqlc.arg(last_event_kind)::text,
    last_uploaded = sqlc.arg(last_uploaded)::bigint,
    last_downloaded = sqlc.arg(last_downloaded)::bigint,
    last_left = sqlc.arg(last_left)::bigint,
    last_address_family = sqlc.arg(last_address_family)::smallint,
    last_credential_version = sqlc.arg(last_credential_version)::bigint,
    torrent_control_sequence = sqlc.arg(torrent_control_sequence)::bigint,
    subject_control_sequence = sqlc.arg(subject_control_sequence)::bigint,
    updated_at = sqlc.arg(updated_at)::timestamptz
WHERE user_id = sqlc.arg(user_id)::uuid
  AND torrent_id = sqlc.arg(torrent_id)::bigint
  AND session_token = sqlc.arg(session_token)::bytea
  AND version = sqlc.arg(expected_version)::bigint;

-- name: InsertRawSessionInterval :exec
INSERT INTO ledger.raw_session_intervals (
    event_id,
    previous_event_id,
    user_id,
    torrent_id,
    session_token,
    info_hash_v1,
    session_epoch,
    starts_at,
    ends_at,
    event_kind,
    address_family,
    credential_version,
    torrent_control_sequence,
    subject_control_sequence,
    previous_uploaded,
    current_uploaded,
    previous_downloaded,
    current_downloaded,
    previous_left,
    current_left,
    raw_uploaded,
    raw_downloaded,
    completed_transition,
    completion_id,
    network_policy_sequence,
    network_policy_revision,
    network_class,
    network_rule_id,
    seedbox_upload_factor_basis_points,
    speed_limit_bytes_per_second,
    created_at
) VALUES (
    sqlc.arg(event_id)::uuid,
    sqlc.arg(previous_event_id)::uuid,
    sqlc.arg(user_id)::uuid,
    sqlc.arg(torrent_id)::bigint,
    sqlc.arg(session_token)::bytea,
    sqlc.arg(info_hash_v1)::bytea,
    sqlc.arg(session_epoch)::bigint,
    sqlc.arg(starts_at)::timestamptz,
    sqlc.arg(ends_at)::timestamptz,
    sqlc.arg(event_kind)::text,
    sqlc.arg(address_family)::smallint,
    sqlc.arg(credential_version)::bigint,
    sqlc.arg(torrent_control_sequence)::bigint,
    sqlc.arg(subject_control_sequence)::bigint,
    sqlc.arg(previous_uploaded)::bigint,
    sqlc.arg(current_uploaded)::bigint,
    sqlc.arg(previous_downloaded)::bigint,
    sqlc.arg(current_downloaded)::bigint,
    sqlc.arg(previous_left)::bigint,
    sqlc.arg(current_left)::bigint,
    sqlc.arg(raw_uploaded)::bigint,
    sqlc.arg(raw_downloaded)::bigint,
    sqlc.arg(completed_transition)::boolean,
    sqlc.narg(completion_id)::bytea,
    sqlc.narg(network_policy_sequence)::bigint,
    sqlc.narg(network_policy_revision)::text,
    sqlc.narg(network_class)::text,
    sqlc.narg(network_rule_id)::text,
    sqlc.narg(seedbox_upload_factor_basis_points)::integer,
    sqlc.narg(speed_limit_bytes_per_second)::bigint,
    sqlc.arg(created_at)::timestamptz
);

-- name: FinalizeInboxWithInterval :execrows
UPDATE settlement.event_inbox
SET
    outcome = 'interval',
    session_epoch = sqlc.arg(session_epoch)::bigint,
    ledger_event_id = event_id,
    processed_at = sqlc.arg(processed_at)::timestamptz
WHERE event_id = sqlc.arg(event_id)::uuid
  AND outcome = 'processing';

-- name: FinalizeInboxWithoutInterval :execrows
UPDATE settlement.event_inbox
SET
    outcome = sqlc.arg(outcome)::text,
    session_epoch = sqlc.arg(session_epoch)::bigint,
    processed_at = sqlc.arg(processed_at)::timestamptz
WHERE event_id = sqlc.arg(event_id)::uuid
  AND outcome = 'processing'
  AND sqlc.arg(outcome)::text IN ('baseline', 'counter_reset', 'out_of_order', 'reopened_baseline');
