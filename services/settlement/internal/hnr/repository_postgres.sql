-- name: LockHNRPolicyTimeline :exec
SELECT pg_advisory_xact_lock(hashtextextended('peergo-settlement-hnr-policy-timeline-v1', 0));

-- name: AppendHNRPolicyTimelineRevision :execrows
INSERT INTO settlement.hnr_policy_timeline_revisions (
    id,
    scope_user_id,
    scope_torrent_id,
    scope_torrent_control_sequence,
    scope_subject_control_sequence,
    effective_at,
    rule_id,
    rule_version,
    mode,
    required_seed_seconds,
    required_ratio_basis_points,
    assessment_window_seconds,
    grace_period_seconds,
    max_interval_credit_seconds,
    policy_json,
    policy_sha256,
    recorded_at
) VALUES (
    sqlc.arg(id)::uuid,
    sqlc.narg(scope_user_id)::uuid,
    sqlc.narg(scope_torrent_id)::bigint,
    sqlc.narg(scope_torrent_control_sequence)::bigint,
    sqlc.narg(scope_subject_control_sequence)::bigint,
    sqlc.arg(effective_at)::timestamptz,
    sqlc.arg(rule_id)::text,
    sqlc.arg(rule_version)::bigint,
    sqlc.arg(mode)::text,
    sqlc.arg(required_seed_seconds)::bigint,
    sqlc.arg(required_ratio_basis_points)::bigint,
    sqlc.arg(assessment_window_seconds)::bigint,
    sqlc.arg(grace_period_seconds)::bigint,
    sqlc.arg(max_interval_credit_seconds)::bigint,
    sqlc.arg(policy_json)::text,
    sqlc.arg(policy_sha256)::bytea,
    sqlc.arg(recorded_at)::timestamptz
)
ON CONFLICT (id) DO NOTHING;

-- name: GetHNRPolicyTimelineRevision :one
SELECT *
FROM settlement.hnr_policy_timeline_revisions
WHERE id = sqlc.arg(id)::uuid;

-- name: ClaimNextHNRWork :one
WITH candidate AS (
    SELECT work.interval_event_id
    FROM settlement.hnr_work AS work
    INNER JOIN ledger.raw_session_intervals AS raw ON raw.event_id = work.interval_event_id
    WHERE work.processed_at IS NULL
      AND work.available_at <= sqlc.arg(claimed_at)::timestamptz
      AND (work.lease_until IS NULL OR work.lease_until <= sqlc.arg(claimed_at)::timestamptz)
    ORDER BY raw.ends_at, work.interval_event_id
    LIMIT 1
    FOR UPDATE OF work SKIP LOCKED
)
UPDATE settlement.hnr_work AS work
SET
    lease_token = sqlc.arg(lease_token)::uuid,
    lease_until = sqlc.arg(lease_until)::timestamptz,
    attempts = work.attempts + 1,
    last_error_code = NULL
FROM candidate
WHERE work.interval_event_id = candidate.interval_event_id
RETURNING work.interval_event_id, work.lease_token, work.attempts;

-- name: GetClaimedHNRWorkForUpdate :one
SELECT
    work.interval_event_id,
    work.attempts,
    raw.user_id,
    raw.torrent_id,
    raw.starts_at,
    raw.ends_at,
    raw.torrent_control_sequence,
    raw.subject_control_sequence,
    raw.current_uploaded,
    raw.current_downloaded,
    raw.completed_transition,
    raw.completion_id,
    raw.completion_identity_version
FROM settlement.hnr_work AS work
INNER JOIN ledger.raw_session_intervals AS raw ON raw.event_id = work.interval_event_id
WHERE work.interval_event_id = sqlc.arg(interval_event_id)::uuid
  AND work.lease_token = sqlc.arg(lease_token)::uuid
  AND work.processed_at IS NULL
FOR UPDATE OF work;

-- name: LockHNRAggregate :exec
SELECT pg_advisory_xact_lock(hashtextextended(
    'peergo-hnr-obligation-v1:' || sqlc.arg(user_id)::uuid::text || ':' || sqlc.arg(torrent_id)::bigint::text,
    0
));

-- name: ListHNRPolicyTimelineCandidates :many
SELECT *
FROM settlement.hnr_policy_timeline_revisions
WHERE effective_at <= sqlc.arg(completed_at)::timestamptz
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

-- name: GetHNRCompletionAssessmentByCompletionID :one
SELECT *
FROM ledger.hnr_completion_assessments
WHERE completion_id = sqlc.arg(completion_id)::bytea;

-- name: InsertHNRCompletionAssessment :exec
INSERT INTO ledger.hnr_completion_assessments (
    id,
    completion_id,
    completion_event_id,
    user_id,
    torrent_id,
    torrent_control_sequence,
    subject_control_sequence,
    completed_at,
    policy_revision_id,
    policy_rule_id,
    policy_rule_version,
    policy_sha256,
    policy_mode,
    required_seed_seconds,
    required_ratio_basis_points,
    max_interval_credit_seconds,
    assessment_due_at,
    grace_ends_at,
    initial_uploaded,
    raw_downloaded,
    decided_at
) VALUES (
    sqlc.arg(id)::uuid,
    sqlc.arg(completion_id)::bytea,
    sqlc.arg(completion_event_id)::uuid,
    sqlc.arg(user_id)::uuid,
    sqlc.arg(torrent_id)::bigint,
    sqlc.arg(torrent_control_sequence)::bigint,
    sqlc.arg(subject_control_sequence)::bigint,
    sqlc.arg(completed_at)::timestamptz,
    sqlc.arg(policy_revision_id)::uuid,
    sqlc.arg(policy_rule_id)::text,
    sqlc.arg(policy_rule_version)::bigint,
    sqlc.arg(policy_sha256)::bytea,
    sqlc.arg(policy_mode)::text,
    sqlc.arg(required_seed_seconds)::bigint,
    sqlc.arg(required_ratio_basis_points)::bigint,
    sqlc.arg(max_interval_credit_seconds)::bigint,
    sqlc.arg(assessment_due_at)::timestamptz,
    sqlc.arg(grace_ends_at)::timestamptz,
    sqlc.arg(initial_uploaded)::bigint,
    sqlc.arg(raw_downloaded)::bigint,
    sqlc.arg(decided_at)::timestamptz
);

-- name: InsertHNRObligation :exec
INSERT INTO ledger.hnr_obligations (
    id,
    assessment_id,
    seeded_seconds,
    raw_uploaded,
    raw_ratio_basis_points,
    state,
    satisfied_by,
    satisfied_at,
    version,
    last_evidence_at,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(id)::uuid,
    sqlc.arg(assessment_id)::uuid,
    sqlc.arg(seeded_seconds)::bigint,
    sqlc.arg(raw_uploaded)::bigint,
    sqlc.arg(raw_ratio_basis_points)::bigint,
    sqlc.arg(state)::text,
    sqlc.narg(satisfied_by)::text,
    sqlc.narg(satisfied_at)::timestamptz,
    sqlc.arg(version)::bigint,
    sqlc.arg(last_evidence_at)::timestamptz,
    sqlc.arg(created_at)::timestamptz,
    sqlc.arg(updated_at)::timestamptz
);

-- name: ListTrackingHNRObligationsForUpdate :many
SELECT
    obligation.id AS obligation_id,
    obligation.assessment_id,
    obligation.seeded_seconds,
    obligation.raw_uploaded,
    obligation.raw_ratio_basis_points,
    obligation.state,
    obligation.satisfied_by,
    obligation.satisfied_at,
    obligation.version,
    obligation.last_evidence_at,
    obligation.created_at,
    obligation.updated_at,
    assessment.completion_id,
    assessment.completion_event_id,
    assessment.user_id,
    assessment.torrent_id,
    assessment.torrent_control_sequence,
    assessment.subject_control_sequence,
    assessment.completed_at,
    assessment.policy_revision_id,
    assessment.policy_rule_id,
    assessment.policy_rule_version,
    assessment.policy_sha256,
    assessment.policy_mode,
    assessment.required_seed_seconds,
    assessment.required_ratio_basis_points,
    assessment.max_interval_credit_seconds,
    assessment.assessment_due_at,
    assessment.grace_ends_at,
    assessment.initial_uploaded,
    assessment.raw_downloaded,
    assessment.decided_at
FROM ledger.hnr_obligations AS obligation
INNER JOIN ledger.hnr_completion_assessments AS assessment
    ON assessment.id = obligation.assessment_id
WHERE assessment.user_id = sqlc.arg(user_id)::uuid
  AND assessment.torrent_id = sqlc.arg(torrent_id)::bigint
  AND obligation.state = 'tracking'
ORDER BY assessment.completed_at, obligation.id
FOR UPDATE OF obligation;

-- name: ListHNRRawIntervals :many
SELECT event_id, starts_at, ends_at, previous_left, current_left, raw_uploaded
FROM ledger.raw_session_intervals
WHERE user_id = sqlc.arg(user_id)::uuid
  AND torrent_id = sqlc.arg(torrent_id)::bigint
  AND ends_at > sqlc.arg(completed_at)::timestamptz
ORDER BY ends_at, event_id;

-- name: UpdateHNRObligationProgress :execrows
UPDATE ledger.hnr_obligations
SET
    seeded_seconds = sqlc.arg(seeded_seconds)::bigint,
    raw_uploaded = sqlc.arg(raw_uploaded)::bigint,
    raw_ratio_basis_points = sqlc.arg(raw_ratio_basis_points)::bigint,
    state = sqlc.arg(state)::text,
    satisfied_by = sqlc.narg(satisfied_by)::text,
    satisfied_at = sqlc.narg(satisfied_at)::timestamptz,
    version = sqlc.arg(new_version)::bigint,
    last_evidence_at = sqlc.arg(last_evidence_at)::timestamptz,
    updated_at = sqlc.arg(updated_at)::timestamptz
WHERE id = sqlc.arg(id)::uuid
  AND version = sqlc.arg(expected_version)::bigint
  AND state = 'tracking';

-- name: AppendHNROutboxEvent :exec
INSERT INTO settlement.hnr_outbox (
    event_id,
    obligation_id,
    obligation_version,
    event_type,
    schema_version,
    occurred_at,
    payload_json,
    payload_sha256,
    available_at,
    created_at
) VALUES (
    sqlc.arg(event_id)::uuid,
    sqlc.arg(obligation_id)::uuid,
    sqlc.arg(obligation_version)::bigint,
    'settlement.hnr.updated',
    'settlement.hnr.v1',
    sqlc.arg(occurred_at)::timestamptz,
    sqlc.arg(payload_json)::text,
    sqlc.arg(payload_sha256)::bytea,
    sqlc.arg(available_at)::timestamptz,
    sqlc.arg(created_at)::timestamptz
);

-- name: MarkHNRWorkProcessed :execrows
UPDATE settlement.hnr_work
SET
    processed_at = sqlc.arg(processed_at)::timestamptz,
    lease_token = NULL,
    lease_until = NULL,
    last_error_code = NULL
WHERE interval_event_id = sqlc.arg(interval_event_id)::uuid
  AND lease_token = sqlc.arg(lease_token)::uuid
  AND processed_at IS NULL;

-- name: ReleaseHNRWork :execrows
UPDATE settlement.hnr_work
SET
    available_at = sqlc.arg(available_at)::timestamptz,
    lease_token = NULL,
    lease_until = NULL,
    last_error_code = sqlc.arg(last_error_code)::text
WHERE interval_event_id = sqlc.arg(interval_event_id)::uuid
  AND lease_token = sqlc.arg(lease_token)::uuid
  AND processed_at IS NULL;

-- name: ClaimNextHNROutboxEvent :one
WITH candidate AS (
    SELECT event_id
    FROM settlement.hnr_outbox
    WHERE published_at IS NULL
      AND available_at <= sqlc.arg(claimed_at)::timestamptz
      AND (lease_until IS NULL OR lease_until <= sqlc.arg(claimed_at)::timestamptz)
    ORDER BY available_at, obligation_id, obligation_version
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE settlement.hnr_outbox AS outbox
SET
    lease_token = sqlc.arg(lease_token)::uuid,
    lease_until = sqlc.arg(lease_until)::timestamptz,
    attempts = outbox.attempts + 1,
    last_error_code = NULL
FROM candidate
WHERE outbox.event_id = candidate.event_id
RETURNING outbox.*;

-- name: MarkHNROutboxEventPublished :execrows
UPDATE settlement.hnr_outbox
SET
    published_at = sqlc.arg(published_at)::timestamptz,
    lease_token = NULL,
    lease_until = NULL,
    last_error_code = NULL
WHERE event_id = sqlc.arg(event_id)::uuid
  AND lease_token = sqlc.arg(lease_token)::uuid
  AND published_at IS NULL;

-- name: ReleaseHNROutboxEvent :execrows
UPDATE settlement.hnr_outbox
SET
    available_at = sqlc.arg(available_at)::timestamptz,
    lease_token = NULL,
    lease_until = NULL,
    last_error_code = sqlc.arg(last_error_code)::text
WHERE event_id = sqlc.arg(event_id)::uuid
  AND lease_token = sqlc.arg(lease_token)::uuid
  AND published_at IS NULL;
