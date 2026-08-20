-- +goose Up
-- Grant administration starts with a deliberately reducing operation only.
-- A revocation request captures the exact grant version reviewed by two
-- independent duty domains; the final approval revokes and increments that
-- same row atomically so stale staff credentials fail on their next request.
ALTER TABLE authz.grants
    ADD CONSTRAINT grants_revocation_target_reference_unique
        UNIQUE (id, subject_id);

CREATE TABLE authz.grant_revocation_requests (
    id uuid PRIMARY KEY,
    grant_id uuid NOT NULL,
    expected_grant_version bigint NOT NULL CHECK (expected_grant_version > 0),
    target_subject_id uuid NOT NULL,
    proposer_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    reason text NOT NULL
        CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'rejected', 'applied', 'conflicted', 'expired')),
    resulting_grant_version bigint CHECK (resulting_grant_version > 0),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    resolved_at timestamptz,
    FOREIGN KEY (grant_id, target_subject_id)
        REFERENCES authz.grants (id, subject_id) ON DELETE RESTRICT,
    UNIQUE (id, proposer_id, target_subject_id),
    CHECK (proposer_id <> target_subject_id),
    CHECK (expires_at > created_at),
    CHECK (expires_at <= created_at + interval '72 hours'),
    CHECK (
        (status = 'pending' AND resolved_at IS NULL AND resulting_grant_version IS NULL)
        OR (status = 'applied' AND resolved_at IS NOT NULL AND resulting_grant_version IS NOT NULL)
        OR (status IN ('rejected', 'conflicted', 'expired')
            AND resolved_at IS NOT NULL AND resulting_grant_version IS NULL)
    )
);

CREATE UNIQUE INDEX grant_revocation_requests_grant_pending_idx
    ON authz.grant_revocation_requests (grant_id)
    WHERE status = 'pending';

CREATE INDEX grant_revocation_requests_recent_idx
    ON authz.grant_revocation_requests (created_at DESC, id DESC);

-- proposer_id and target_subject_id are repeated only to make the separation
-- constraints enforceable by PostgreSQL. The composite foreign key guarantees
-- that callers cannot supply values different from the parent request.
CREATE TABLE authz.grant_revocation_reviews (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL,
    proposer_id uuid NOT NULL,
    target_subject_id uuid NOT NULL,
    reviewer_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    duty_domain text NOT NULL CHECK (duty_domain IN ('governance', 'security')),
    decision text NOT NULL CHECK (decision IN ('approve', 'reject')),
    reason text NOT NULL
        CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    created_at timestamptz NOT NULL,
    FOREIGN KEY (request_id, proposer_id, target_subject_id)
        REFERENCES authz.grant_revocation_requests (id, proposer_id, target_subject_id)
        ON DELETE RESTRICT,
    UNIQUE (request_id, duty_domain),
    UNIQUE (request_id, reviewer_id),
    CHECK (reviewer_id <> proposer_id),
    CHECK (reviewer_id <> target_subject_id)
);

CREATE INDEX grant_revocation_reviews_request_idx
    ON authz.grant_revocation_reviews (request_id, created_at, id);

INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES
    ('authz.grant.read', '读取权限、任期与撤权审批状态', 'high', 'none', 'staff-session', true, false),
    ('authz.grant.revoke.approve.governance', '以治理职责复核 grant 撤销', 'critical', 'none', 'staff-session', true, false),
    ('authz.grant.revoke.approve.security', '以安全职责复核 grant 撤销', 'critical', 'none', 'staff-session', true, false),
    ('authz.grant.revoke.propose', '提议撤销他人的有效 grant', 'high', 'none', 'staff-session', true, false);

INSERT INTO authz.roles (id, name, description, assignable) VALUES
    ('grant_governance_reviewer', '授权治理复核', '只包含 grant 撤销的治理职责复核能力。', true),
    ('grant_proposer', '授权撤销提案', '只包含读取授权状态与提议撤销他人 grant 的能力。', true),
    ('grant_security_reviewer', '授权安全复核', '只包含 grant 撤销的安全职责复核能力。', true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('grant_governance_reviewer', 'authz.grant.read'),
    ('grant_governance_reviewer', 'authz.grant.revoke.approve.governance'),
    ('grant_proposer', 'authz.grant.read'),
    ('grant_proposer', 'authz.grant.revoke.propose'),
    ('grant_security_reviewer', 'authz.grant.read'),
    ('grant_security_reviewer', 'authz.grant.revoke.approve.security');

-- +goose Down
DELETE FROM authz.grants WHERE role_id IN (
    'grant_governance_reviewer',
    'grant_proposer',
    'grant_security_reviewer'
);
DELETE FROM authz.role_permissions WHERE role_id IN (
    'grant_governance_reviewer',
    'grant_proposer',
    'grant_security_reviewer'
);
DELETE FROM authz.roles WHERE id IN (
    'grant_governance_reviewer',
    'grant_proposer',
    'grant_security_reviewer'
);
DELETE FROM authz.permissions WHERE action IN (
    'authz.grant.read',
    'authz.grant.revoke.approve.governance',
    'authz.grant.revoke.approve.security',
    'authz.grant.revoke.propose'
);

DROP TABLE authz.grant_revocation_reviews;
DROP TABLE authz.grant_revocation_requests;

ALTER TABLE authz.grants
    DROP CONSTRAINT grants_revocation_target_reference_unique;
