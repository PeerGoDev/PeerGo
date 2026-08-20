-- +goose Up

-- Staff-signed targets are future calendar-month policy, not mutable task
-- settings. The request, actor, reason and authorization decision remain on
-- the immutable revision so an old target can always be explained later.
ALTER TABLE workgroups.contribution_policy_revisions
    ADD COLUMN request_id uuid,
    ADD COLUMN issued_by uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    ADD COLUMN reason text,
    ADD CONSTRAINT workgroup_contribution_policy_staff_evidence_ck CHECK (
        (source_kind = 'cutover_opening'
            AND request_id IS NULL
            AND issued_by IS NULL
            AND reason IS NULL)
        OR (source_kind = 'staff'
            AND request_id IS NOT NULL
            AND issued_by IS NOT NULL
            AND char_length(btrim(reason)) BETWEEN 10 AND 1000)
    );

CREATE UNIQUE INDEX workgroup_contribution_policy_request_idx
    ON workgroups.contribution_policy_revisions (request_id)
    WHERE request_id IS NOT NULL;

CREATE INDEX workgroup_contribution_policy_timeline_idx
    ON workgroups.contribution_policy_revisions
       (group_kind, effective_from DESC, revision DESC);

INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES (
    'workgroup.contribution.policy.issue',
    '签发未来自然月生效的工作组贡献目标',
    'high', 'none', 'staff-session', true, true
);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('site_admin', 'workgroup.contribution.policy.issue'),
    ('workgroup_manager', 'workgroup.contribution.policy.issue');

-- +goose Down

DELETE FROM authz.role_permissions
WHERE action = 'workgroup.contribution.policy.issue';
DELETE FROM authz.permissions
WHERE action = 'workgroup.contribution.policy.issue';

DROP INDEX workgroups.workgroup_contribution_policy_timeline_idx;
DROP INDEX workgroups.workgroup_contribution_policy_request_idx;

ALTER TABLE workgroups.contribution_policy_revisions
    DROP CONSTRAINT workgroup_contribution_policy_staff_evidence_ck,
    DROP COLUMN reason,
    DROP COLUMN issued_by,
    DROP COLUMN request_id;
