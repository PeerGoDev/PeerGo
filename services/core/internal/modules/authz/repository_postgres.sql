-- name: ListPermissionCatalog :many
SELECT
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
FROM authz.permissions
ORDER BY action;

-- name: ListSubjectGrants :many
SELECT
    grant_record.id,
    grant_record.subject_id,
    grant_record.role_id,
    permission.action,
    grant_record.scope_type,
    grant_record.scope_id,
    grant_record.valid_from,
    grant_record.valid_until,
    grant_record.constraints::text AS constraints_json,
    grant_record.version,
    grant_record.revoked_at,
    mandate.id AS mandate_id,
    mandate.subject_id AS mandate_subject_id,
    mandate.scope_type AS mandate_scope_type,
    mandate.scope_id AS mandate_scope_id,
    mandate.starts_at AS mandate_starts_at,
    mandate.ends_at AS mandate_ends_at,
    mandate.status AS mandate_status
FROM authz.grants AS grant_record
JOIN authz.role_permissions AS role_permission
    ON role_permission.role_id = grant_record.role_id
JOIN authz.permissions AS permission
    ON permission.action = role_permission.action
JOIN governance.mandates AS mandate
    ON mandate.id = grant_record.mandate_id
   AND mandate.subject_id = grant_record.subject_id
WHERE grant_record.subject_id = $1
ORDER BY permission.action, grant_record.id;

-- name: ListGrantAdministrationGrants :many
SELECT
    grant_record.id,
    grant_record.subject_id,
    subject.username AS subject_username,
    subject.display_name AS subject_display_name,
    grant_record.role_id,
    role_record.name AS role_name,
    grant_record.mandate_id,
    mandate.status AS mandate_status,
    grant_record.scope_type,
    grant_record.scope_id,
    grant_record.valid_from,
    grant_record.valid_until,
    grant_record.version,
    grant_record.revoked_at
FROM authz.grants AS grant_record
JOIN identity.users AS subject ON subject.id = grant_record.subject_id
JOIN authz.roles AS role_record ON role_record.id = grant_record.role_id
JOIN governance.mandates AS mandate
    ON mandate.id = grant_record.mandate_id
   AND mandate.subject_id = grant_record.subject_id
ORDER BY
    (grant_record.revoked_at IS NULL) DESC,
    grant_record.valid_until DESC,
    grant_record.id DESC
LIMIT 200;

-- name: ListGrantRevocationRequests :many
SELECT
    id,
    grant_id,
    expected_grant_version,
    target_subject_id,
    proposer_id,
    reason,
    status,
    resulting_grant_version,
    created_at,
    expires_at,
    resolved_at
FROM authz.grant_revocation_requests
ORDER BY created_at DESC, id DESC
LIMIT 100;

-- name: ListGrantRevocationReviews :many
SELECT
    id,
    request_id,
    reviewer_id,
    duty_domain,
    decision,
    reason,
    created_at
FROM authz.grant_revocation_reviews
WHERE request_id = ANY(sqlc.arg(request_ids)::uuid[])
ORDER BY request_id, created_at, id;

-- name: GetGrantAdministrationTargetForUpdate :one
SELECT
    grant_record.id,
    grant_record.subject_id,
    grant_record.version,
    grant_record.revoked_at
FROM authz.grants AS grant_record
WHERE grant_record.id = sqlc.arg(grant_id)
FOR UPDATE OF grant_record;

-- name: GetPendingGrantRevocationForUpdate :one
SELECT
    id,
    grant_id,
    expected_grant_version,
    target_subject_id,
    proposer_id,
    reason,
    status,
    resulting_grant_version,
    created_at,
    expires_at,
    resolved_at
FROM authz.grant_revocation_requests
WHERE grant_id = sqlc.arg(grant_id)
  AND status = 'pending'
FOR UPDATE;

-- name: InsertGrantRevocationRequest :one
INSERT INTO authz.grant_revocation_requests (
    id,
    grant_id,
    expected_grant_version,
    target_subject_id,
    proposer_id,
    reason,
    status,
    created_at,
    expires_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(grant_id),
    sqlc.arg(expected_grant_version),
    sqlc.arg(target_subject_id),
    sqlc.arg(proposer_id),
    sqlc.arg(reason),
    'pending',
    sqlc.arg(created_at),
    sqlc.arg(expires_at)
)
RETURNING
    id,
    grant_id,
    expected_grant_version,
    target_subject_id,
    proposer_id,
    reason,
    status,
    resulting_grant_version,
    created_at,
    expires_at,
    resolved_at;

-- name: GetGrantRevocationForUpdate :one
SELECT
    request_record.id,
    request_record.grant_id,
    request_record.expected_grant_version,
    request_record.target_subject_id,
    request_record.proposer_id,
    request_record.reason,
    request_record.status,
    request_record.resulting_grant_version,
    request_record.created_at,
    request_record.expires_at,
    request_record.resolved_at,
    grant_record.version AS current_grant_version,
    grant_record.revoked_at AS grant_revoked_at
FROM authz.grant_revocation_requests AS request_record
JOIN authz.grants AS grant_record
  ON grant_record.id = request_record.grant_id
 AND grant_record.subject_id = request_record.target_subject_id
WHERE request_record.id = sqlc.arg(request_id)
FOR UPDATE OF request_record, grant_record;

-- name: GetGrantRevocationRequest :one
SELECT
    id,
    grant_id,
    expected_grant_version,
    target_subject_id,
    proposer_id,
    reason,
    status,
    resulting_grant_version,
    created_at,
    expires_at,
    resolved_at
FROM authz.grant_revocation_requests
WHERE id = sqlc.arg(request_id);

-- name: ListGrantRevocationReviewsForRequest :many
SELECT
    id,
    reviewer_id,
    duty_domain,
    decision,
    reason,
    created_at
FROM authz.grant_revocation_reviews
WHERE request_id = sqlc.arg(request_id)
ORDER BY created_at, id;

-- name: InsertGrantRevocationReview :one
INSERT INTO authz.grant_revocation_reviews (
    id,
    request_id,
    proposer_id,
    target_subject_id,
    reviewer_id,
    duty_domain,
    decision,
    reason,
    created_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(request_id),
    sqlc.arg(proposer_id),
    sqlc.arg(target_subject_id),
    sqlc.arg(reviewer_id),
    sqlc.arg(duty_domain),
    sqlc.arg(decision),
    sqlc.arg(reason),
    sqlc.arg(created_at)
)
RETURNING id, reviewer_id, duty_domain, decision, reason, created_at;

-- name: ExpireGrantRevocationRequest :one
UPDATE authz.grant_revocation_requests
SET status = 'expired', resolved_at = sqlc.arg(resolved_at)
WHERE id = sqlc.arg(request_id)
  AND status = 'pending'
RETURNING id;

-- name: RejectGrantRevocationRequest :one
UPDATE authz.grant_revocation_requests
SET status = 'rejected', resolved_at = sqlc.arg(resolved_at)
WHERE id = sqlc.arg(request_id)
  AND status = 'pending'
RETURNING id;

-- name: ConflictGrantRevocationRequest :one
UPDATE authz.grant_revocation_requests
SET status = 'conflicted', resolved_at = sqlc.arg(resolved_at)
WHERE id = sqlc.arg(request_id)
  AND status = 'pending'
RETURNING id;

-- name: ApplyGrantRevocation :one
UPDATE authz.grants
SET
    revoked_at = sqlc.arg(revoked_at),
    version = version + 1,
    updated_at = sqlc.arg(revoked_at)
WHERE id = sqlc.arg(grant_id)
  AND version = sqlc.arg(expected_grant_version)
  AND revoked_at IS NULL
RETURNING version;

-- name: ApplyGrantRevocationRequest :one
UPDATE authz.grant_revocation_requests
SET
    status = 'applied',
    resulting_grant_version = sqlc.arg(resulting_grant_version),
    resolved_at = sqlc.arg(resolved_at)
WHERE id = sqlc.arg(request_id)
  AND status = 'pending'
RETURNING id;
