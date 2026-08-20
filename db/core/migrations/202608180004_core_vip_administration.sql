-- +goose Up

-- VIP is an account entitlement, not a display-only badge. Staff changes are
-- therefore authorized separately from account bans and recorded as an
-- immutable timeline. The current projection remains in user_access_states so
-- ratio enforcement and the user directory see the same state atomically.
INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES (
    'user.vip.manage',
    '签发、续期或撤销一个账户的 VIP 身份',
    'high', 'none', 'staff-session', true, true
);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('site_admin', 'user.vip.manage'),
    ('user_access_operator', 'user.vip.manage');

CREATE TABLE identity.user_vip_transitions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    transition text NOT NULL CHECK (
        transition IN ('granted', 'renewed', 'revoked')
    ),
    origin text NOT NULL CHECK (
        origin IN ('legacy_migration', 'system_backfill', 'staff')
    ),
    reason text NOT NULL CHECK (
        char_length(btrim(reason)) BETWEEN 10 AND 500
    ),
    actor_id uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    from_enabled boolean NOT NULL,
    from_until timestamptz,
    to_enabled boolean NOT NULL,
    to_until timestamptz,
    from_state_version bigint NOT NULL CHECK (from_state_version >= 0),
    state_version bigint NOT NULL CHECK (state_version > 0),
    occurred_at timestamptz NOT NULL,
    CHECK (state_version = from_state_version + 1),
    CHECK (
        (transition = 'granted' AND NOT from_enabled AND to_enabled)
        OR
        (transition = 'renewed' AND from_enabled AND to_enabled)
        OR
        (transition = 'revoked' AND from_enabled AND NOT to_enabled
            AND to_until IS NULL)
    ),
    CHECK (
        (origin IN ('legacy_migration', 'system_backfill') AND actor_id IS NULL)
        OR (origin = 'staff' AND actor_id IS NOT NULL)
    ),
    UNIQUE (user_id, state_version)
);

CREATE INDEX user_vip_transitions_user_timeline_idx
    ON identity.user_vip_transitions
        (user_id, occurred_at DESC, id DESC);

-- Existing Rousi VIP state is kept as the opening event. Its access-state
-- version may also cover a migrated download restriction; that is expected:
-- user_access_states is one optimistic-concurrency aggregate.
INSERT INTO identity.user_vip_transitions (
    user_id, transition, origin, reason,
    from_enabled, from_until, to_enabled, to_until,
    from_state_version, state_version, occurred_at
)
SELECT
    user_id,
    'granted',
    CASE WHEN source_run_id IS NOT NULL
        THEN 'legacy_migration' ELSE 'system_backfill' END,
    CASE WHEN source_run_id IS NOT NULL
        THEN '该 VIP 身份从 Rousi 当前账户状态迁入。'
        ELSE '该 VIP 身份由 PeerGo 历史状态回填。' END,
    false,
    NULL,
    true,
    vip_until,
    version - 1,
    version,
    updated_at
FROM identity.user_access_states
WHERE vip_enabled;

CREATE TRIGGER user_vip_transitions_immutable
BEFORE UPDATE OR DELETE ON identity.user_vip_transitions
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

-- Runtime entitlement changes may occur at any instant. Hourly reward
-- settlement still evaluates the latest revision at the window start, so an
-- in-window grant only affects the following window while multiple legitimate
-- changes in one hour remain representable without rewriting history.
ALTER TABLE identity.user_reward_benefit_revisions
    DROP CONSTRAINT user_reward_benefit_revisions_check;
ALTER TABLE identity.user_reward_benefit_revisions
    ADD CONSTRAINT user_reward_benefit_revisions_source_timeline_check CHECK (
        (source_kind = 'cutover_opening'
            AND revision = 1
            AND effective_from = '-infinity'::timestamptz)
        OR
        (source_kind = 'runtime' AND created_at <= effective_from)
    );

-- +goose Down

ALTER TABLE identity.user_reward_benefit_revisions
    DROP CONSTRAINT user_reward_benefit_revisions_source_timeline_check;
ALTER TABLE identity.user_reward_benefit_revisions
    ADD CONSTRAINT user_reward_benefit_revisions_check CHECK (
        (source_kind = 'cutover_opening'
            AND revision = 1
            AND effective_from = '-infinity'::timestamptz)
        OR
        (source_kind = 'runtime'
            AND effective_from = date_trunc('hour', effective_from)
            AND created_at <= effective_from)
    );

DROP TRIGGER user_vip_transitions_immutable
    ON identity.user_vip_transitions;
DROP TABLE identity.user_vip_transitions;

DELETE FROM authz.role_permissions WHERE action = 'user.vip.manage';
DELETE FROM authz.permissions WHERE action = 'user.vip.manage';
