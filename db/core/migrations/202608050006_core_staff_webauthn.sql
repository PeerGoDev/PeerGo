-- +goose Up
-- Staff credentials are deliberately separate from ordinary account login
-- material. A normal Web session may start a WebAuthn assertion, but only the
-- resulting short-lived staff session can authenticate `/api/v1/admin/*`.
CREATE TABLE identity.staff_webauthn_credentials (
    credential_id bytea PRIMARY KEY
        CHECK (octet_length(credential_id) BETWEEN 1 AND 1024),
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    label text NOT NULL CHECK (char_length(label) BETWEEN 1 AND 80),
    -- The complete go-webauthn credential record is encrypted with AES-GCM in
    -- Core before persistence. Keeping only the lookup ID in plaintext avoids
    -- making a database write sufficient to replace a staff public key.
    record_ciphertext bytea NOT NULL
        CHECK (octet_length(record_ciphertext) BETWEEN 17 AND 65536),
    record_nonce bytea NOT NULL CHECK (octet_length(record_nonce) = 12),
    key_epoch text NOT NULL CHECK (char_length(key_epoch) BETWEEN 1 AND 64),
    enrollment_source text NOT NULL
        CHECK (enrollment_source IN ('bootstrap', 'legacy-import')),
    status text NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'revoked')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    revoked_at timestamptz,
    UNIQUE (credential_id, user_id),
    CHECK ((status = 'active' AND revoked_at IS NULL)
        OR (status = 'revoked' AND revoked_at IS NOT NULL))
);

CREATE INDEX staff_webauthn_credentials_user_active_idx
    ON identity.staff_webauthn_credentials (user_id, created_at)
    WHERE status = 'active';

ALTER TABLE identity.sessions
    DROP CONSTRAINT sessions_audience_check,
    ADD COLUMN parent_token_hash bytea,
    ADD COLUMN staff_credential_id bytea,
    ADD COLUMN webauthn_authenticated_at timestamptz,
    ADD CONSTRAINT sessions_token_user_unique UNIQUE (token_hash, user_id),
    ADD CONSTRAINT sessions_parent_user_fk
        FOREIGN KEY (parent_token_hash, user_id)
        REFERENCES identity.sessions (token_hash, user_id) ON DELETE CASCADE,
    ADD CONSTRAINT sessions_staff_credential_user_fk
        FOREIGN KEY (staff_credential_id, user_id)
        REFERENCES identity.staff_webauthn_credentials (credential_id, user_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT sessions_audience_check
        CHECK (audience IN ('web', 'staff')),
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

CREATE INDEX sessions_active_staff_user_idx
    ON identity.sessions (user_id, expires_at DESC)
    WHERE audience = 'staff' AND revoked_at IS NULL;

-- The entire library SessionData record is encrypted and bound to the Web
-- session that initiated it. A challenge is consumed before signature
-- verification, so a failed assertion cannot be replayed or brute-forced.
CREATE TABLE identity.staff_webauthn_challenges (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    parent_token_hash bytea NOT NULL CHECK (octet_length(parent_token_hash) = 32),
    session_ciphertext bytea NOT NULL
        CHECK (octet_length(session_ciphertext) BETWEEN 17 AND 65536),
    session_nonce bytea NOT NULL CHECK (octet_length(session_nonce) = 12),
    key_epoch text NOT NULL CHECK (char_length(key_epoch) BETWEEN 1 AND 64),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    FOREIGN KEY (parent_token_hash, user_id)
        REFERENCES identity.sessions (token_hash, user_id) ON DELETE CASCADE,
    CHECK (expires_at > created_at),
    CHECK (expires_at <= created_at + interval '10 minutes'),
    CHECK (consumed_at IS NULL OR consumed_at >= created_at)
);

-- The update-before-insert query invalidates the previous ceremony. This
-- unique partial index also preserves that invariant under concurrent begin
-- requests, where one contender may otherwise not see the other's new row.
CREATE UNIQUE INDEX staff_webauthn_challenges_active_parent_idx
    ON identity.staff_webauthn_challenges (parent_token_hash)
    WHERE consumed_at IS NULL;

INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES
    ('staff.session.create.self', '通过 WebAuthn 创建自己的后台会话', 'high', 'self', 'web-session', true, true),
    ('staff.session.read.self', '读取自己的当前后台会话', 'low', 'self', 'staff-session', true, false),
    ('staff.session.revoke.self', '撤销自己的当前后台会话', 'medium', 'self', 'staff-session', true, false);

INSERT INTO authz.roles (id, name, description, assignable) VALUES (
    'staff_access',
    '后台基础访问',
    '只允许完成 WebAuthn elevation 和管理自己的短期后台会话，不包含任何后台业务权限。',
    true
);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('staff_access', 'staff.session.create.self'),
    ('staff_access', 'staff.session.read.self'),
    ('staff_access', 'staff.session.revoke.self');

-- +goose Down
DELETE FROM authz.grants WHERE role_id = 'staff_access';
DELETE FROM authz.role_permissions WHERE role_id = 'staff_access';
DELETE FROM authz.roles WHERE id = 'staff_access';
DELETE FROM authz.permissions WHERE action IN (
    'staff.session.create.self',
    'staff.session.read.self',
    'staff.session.revoke.self'
);

DROP TABLE identity.staff_webauthn_challenges;
DELETE FROM identity.sessions WHERE audience = 'staff';
DROP INDEX identity.sessions_active_staff_user_idx;
ALTER TABLE identity.sessions
    DROP CONSTRAINT sessions_audience_shape_check,
    DROP CONSTRAINT sessions_staff_credential_user_fk,
    DROP CONSTRAINT sessions_parent_user_fk,
    DROP CONSTRAINT sessions_token_user_unique,
    DROP CONSTRAINT sessions_audience_check,
    DROP COLUMN webauthn_authenticated_at,
    DROP COLUMN staff_credential_id,
    DROP COLUMN parent_token_hash,
    ADD CONSTRAINT sessions_audience_check CHECK (audience IN ('web'));
DROP TABLE identity.staff_webauthn_credentials;
