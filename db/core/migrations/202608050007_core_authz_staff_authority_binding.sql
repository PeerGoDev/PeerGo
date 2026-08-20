-- +goose Up
-- A staff session is authority derived from one concrete grant, not a durable
-- badge on the account. The redundant unique key lets the session foreign key
-- enforce that the persisted grant and mandate both belong to the same user.
ALTER TABLE authz.grants
    ADD CONSTRAINT grants_staff_authority_reference_unique
        UNIQUE (id, mandate_id, subject_id);

-- Migration 006 sessions did not record which allowed decision created them,
-- so there is no safe value to backfill. They live for at most fifteen minutes;
-- deleting them during deployment is the only fail-closed migration behavior.
DELETE FROM identity.sessions WHERE audience = 'staff';

ALTER TABLE identity.sessions
    DROP CONSTRAINT sessions_audience_shape_check,
    ADD COLUMN authority_grant_id uuid,
    ADD COLUMN authority_grant_version bigint,
    ADD COLUMN authority_mandate_id uuid,
    ADD CONSTRAINT sessions_staff_authority_grant_fk
        FOREIGN KEY (authority_grant_id, authority_mandate_id, user_id)
        REFERENCES authz.grants (id, mandate_id, subject_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT sessions_audience_shape_check CHECK (
        (
            audience = 'web'
            AND parent_token_hash IS NULL
            AND staff_credential_id IS NULL
            AND webauthn_authenticated_at IS NULL
            AND authority_grant_id IS NULL
            AND authority_grant_version IS NULL
            AND authority_mandate_id IS NULL
        ) OR (
            audience = 'staff'
            AND parent_token_hash IS NOT NULL
            AND staff_credential_id IS NOT NULL
            AND webauthn_authenticated_at IS NOT NULL
            AND webauthn_authenticated_at >= created_at
            AND authority_grant_id IS NOT NULL
            AND authority_grant_version > 0
            AND authority_mandate_id IS NOT NULL
        )
    );

-- +goose Down
-- Rows created by this version cannot satisfy migration 006's narrower shape
-- once the authority columns disappear, so revoke them by deletion first.
DELETE FROM identity.sessions WHERE audience = 'staff';

ALTER TABLE identity.sessions
    DROP CONSTRAINT sessions_audience_shape_check,
    DROP CONSTRAINT sessions_staff_authority_grant_fk,
    DROP COLUMN authority_mandate_id,
    DROP COLUMN authority_grant_version,
    DROP COLUMN authority_grant_id,
    ADD CONSTRAINT sessions_audience_shape_check CHECK (
        (
            audience = 'web'
            AND parent_token_hash IS NULL
            AND staff_credential_id IS NULL
            AND webauthn_authenticated_at IS NULL
        ) OR (
            audience = 'staff'
            AND parent_token_hash IS NOT NULL
            AND staff_credential_id IS NOT NULL
            AND webauthn_authenticated_at IS NOT NULL
            AND webauthn_authenticated_at >= created_at
        )
    );

ALTER TABLE authz.grants
    DROP CONSTRAINT grants_staff_authority_reference_unique;
