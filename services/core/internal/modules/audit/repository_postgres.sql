-- name: AppendAuditEvent :exec
INSERT INTO audit.outbox (
    event_id,
    event_type,
    schema_version,
    occurred_at,
    payload_json,
    payload_sha256,
    available_at
) VALUES ($1, $2, $3, $4, $5, $6, now());

-- name: ClaimPendingAuditEvents :many
WITH candidates AS (
    SELECT pending.event_id
    FROM audit.outbox AS pending
    WHERE pending.delivered_at IS NULL
      AND pending.available_at <= $1
      AND (pending.lease_until IS NULL OR pending.lease_until <= $1)
    ORDER BY pending.occurred_at, pending.event_id
    FOR UPDATE SKIP LOCKED
    LIMIT $2
)
UPDATE audit.outbox AS event
SET lease_until = $3,
    attempts = event.attempts + 1,
    last_error = NULL
FROM candidates
WHERE event.event_id = candidates.event_id
RETURNING
    event.event_id,
    event.event_type,
    event.schema_version,
    event.occurred_at,
    event.payload_json,
    event.payload_sha256,
    event.attempts;

-- name: MarkAuditEventDelivered :execrows
UPDATE audit.outbox
SET delivered_at = $2,
    lease_until = NULL,
    last_error = NULL
WHERE event_id = $1
  AND delivered_at IS NULL;

-- name: ReleaseAuditEvent :execrows
UPDATE audit.outbox
SET available_at = $2,
    lease_until = NULL,
    last_error = $3
WHERE event_id = $1
  AND delivered_at IS NULL;
