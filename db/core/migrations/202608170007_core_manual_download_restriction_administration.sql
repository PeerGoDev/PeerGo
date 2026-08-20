-- +goose Up

-- Manual download restriction is an independent gate: it blocks new torrent
-- downloads but does not disable login, erase ratio-watch state, or resolve
-- H&R obligations. Staff therefore receives narrower permissions than the
-- existing account-access restriction commands.
INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    (
        'user.downloadrestriction.restrict',
        '签发或修改一个账户的人工下载限制',
        'high', 'none', 'staff-session', true, true
    ),
    (
        'user.downloadrestriction.revoke',
        '解除一个账户的人工下载限制',
        'high', 'none', 'staff-session', true, true
    );

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('site_admin', 'user.downloadrestriction.restrict'),
    ('site_admin', 'user.downloadrestriction.revoke'),
    ('user_access_operator', 'user.downloadrestriction.restrict'),
    ('user_access_operator', 'user.downloadrestriction.revoke');

-- The opening projection previously stored only a boolean. Keep the migration
-- receipt columns untouched, but attach the exact current reason to the state
-- so a later staff-issued restriction cannot be mistaken for the old import.
ALTER TABLE identity.user_access_states
    ADD COLUMN download_restriction_origin text,
    ADD COLUMN download_restriction_reason_code text,
    ADD COLUMN download_restriction_reason text,
    ADD COLUMN download_restriction_started_at timestamptz,
    ADD COLUMN download_restriction_created_by uuid
        REFERENCES identity.users (id) ON DELETE RESTRICT;

UPDATE identity.user_access_states
SET
    download_restriction_origin = CASE
        WHEN source_run_id IS NOT NULL THEN 'legacy_migration'
        ELSE 'system_backfill'
    END,
    download_restriction_reason_code = CASE
        WHEN source_run_id IS NOT NULL THEN 'legacy_download_restriction'
        ELSE 'manual_review'
    END,
    download_restriction_reason = CASE
        WHEN source_run_id IS NOT NULL
            THEN '该下载限制从旧站当前账户状态迁入，需要由用户管理员单独复核。'
        ELSE '该账户在历史版本中已处于人工下载限制状态，等待管理员复核。'
    END,
    download_restriction_started_at = updated_at
WHERE download_restricted;

ALTER TABLE identity.user_access_states
    ADD CONSTRAINT user_access_states_download_restriction_metadata_check CHECK (
        (
            download_restricted
            AND download_restriction_origin IN (
                'legacy_migration', 'system_backfill', 'staff'
            )
            AND download_restriction_reason_code
                ~ '^[a-z][a-z0-9_]{0,63}$'
            AND char_length(btrim(download_restriction_reason))
                BETWEEN 10 AND 500
            AND download_restriction_started_at IS NOT NULL
            AND (
                (download_restriction_origin = 'staff'
                    AND download_restriction_created_by IS NOT NULL)
                OR
                (download_restriction_origin <> 'staff'
                    AND download_restriction_created_by IS NULL)
            )
        )
        OR
        (
            NOT download_restricted
            AND download_restriction_origin IS NULL
            AND download_restriction_reason_code IS NULL
            AND download_restriction_reason IS NULL
            AND download_restriction_started_at IS NULL
            AND download_restriction_created_by IS NULL
        )
    );

-- This append-only timeline is the operator-facing evidence. State can change,
-- but every imported, staff, or approved-appeal transition remains queryable.
-- Unique (user_id, state_version) also makes retries fail closed.
CREATE TABLE identity.manual_download_restriction_transitions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    transition text NOT NULL CHECK (
        transition IN ('restricted', 'updated', 'revoked')
    ),
    origin text NOT NULL CHECK (
        origin IN ('legacy_migration', 'system_backfill', 'staff', 'appeal')
    ),
    reason_code text NOT NULL CHECK (
        reason_code ~ '^[a-z][a-z0-9_]{0,63}$'
    ),
    reason text NOT NULL CHECK (
        char_length(btrim(reason)) BETWEEN 10 AND 500
    ),
    actor_id uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    appeal_id uuid REFERENCES identity.account_access_appeals (id)
        ON DELETE RESTRICT,
    from_restricted boolean NOT NULL,
    to_restricted boolean NOT NULL,
    from_state_version bigint NOT NULL CHECK (from_state_version >= 0),
    state_version bigint NOT NULL CHECK (state_version > 0),
    occurred_at timestamptz NOT NULL,
    CHECK (state_version = from_state_version + 1),
    CHECK (
        (transition = 'restricted' AND NOT from_restricted AND to_restricted)
        OR
        (transition = 'updated' AND from_restricted AND to_restricted)
        OR
        (transition = 'revoked' AND from_restricted AND NOT to_restricted)
    ),
    CHECK (
        (origin IN ('legacy_migration', 'system_backfill')
            AND actor_id IS NULL AND appeal_id IS NULL)
        OR
        (origin = 'staff' AND actor_id IS NOT NULL AND appeal_id IS NULL)
        OR
        (origin = 'appeal' AND actor_id IS NOT NULL AND appeal_id IS NOT NULL)
    ),
    UNIQUE (user_id, state_version)
);

CREATE INDEX manual_download_restriction_transitions_user_timeline_idx
    ON identity.manual_download_restriction_transitions
        (user_id, occurred_at DESC, id DESC);

INSERT INTO identity.manual_download_restriction_transitions (
    user_id, transition, origin, reason_code, reason,
    from_restricted, to_restricted,
    from_state_version, state_version, occurred_at
)
SELECT
    user_id,
    'restricted',
    download_restriction_origin,
    download_restriction_reason_code,
    download_restriction_reason,
    false,
    true,
    version - 1,
    version,
    download_restriction_started_at
FROM identity.user_access_states
WHERE download_restricted;

-- +goose StatementBegin
CREATE FUNCTION identity.reject_manual_download_restriction_transition_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'manual download restriction transitions are immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER manual_download_restriction_transitions_immutable
BEFORE UPDATE OR DELETE
ON identity.manual_download_restriction_transitions
FOR EACH ROW EXECUTE FUNCTION
    identity.reject_manual_download_restriction_transition_mutation();

-- +goose Down

DROP TRIGGER manual_download_restriction_transitions_immutable
    ON identity.manual_download_restriction_transitions;
DROP FUNCTION identity.reject_manual_download_restriction_transition_mutation();
DROP TABLE identity.manual_download_restriction_transitions;

ALTER TABLE identity.user_access_states
    DROP CONSTRAINT user_access_states_download_restriction_metadata_check,
    DROP COLUMN download_restriction_created_by,
    DROP COLUMN download_restriction_started_at,
    DROP COLUMN download_restriction_reason,
    DROP COLUMN download_restriction_reason_code,
    DROP COLUMN download_restriction_origin;

DELETE FROM authz.role_permissions
WHERE action IN (
    'user.downloadrestriction.restrict',
    'user.downloadrestriction.revoke'
);
DELETE FROM authz.permissions
WHERE action IN (
    'user.downloadrestriction.restrict',
    'user.downloadrestriction.revoke'
);
