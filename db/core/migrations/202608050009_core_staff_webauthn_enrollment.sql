-- +goose Up
-- A staff credential can only be enrolled with a short-lived operator-issued
-- ticket. The raw ticket is returned once by the CLI; PostgreSQL keeps only
-- its SHA-256 digest, so a database read cannot replay the bootstrap secret.
CREATE TABLE identity.staff_credential_bootstrap_tickets (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    operator_reference_sha256 bytea NOT NULL
        CHECK (octet_length(operator_reference_sha256) = 32),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    revoked_at timestamptz,
    UNIQUE (id, user_id),
    CHECK (expires_at > created_at),
    CHECK (expires_at <= created_at + interval '30 minutes'),
    CHECK (consumed_at IS NULL OR consumed_at >= created_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at),
    CHECK (consumed_at IS NULL OR revoked_at IS NULL)
);

-- Expired rows remain immutable evidence. Issuing a replacement revokes the
-- previous unconsumed row in the same transaction before inserting the new
-- ticket, so this index also closes concurrent double-issuance races.
CREATE UNIQUE INDEX staff_credential_bootstrap_tickets_active_user_idx
    ON identity.staff_credential_bootstrap_tickets (user_id)
    WHERE consumed_at IS NULL AND revoked_at IS NULL;

CREATE INDEX staff_credential_bootstrap_tickets_expiry_idx
    ON identity.staff_credential_bootstrap_tickets (expires_at)
    WHERE consumed_at IS NULL AND revoked_at IS NULL;

-- Registration SessionData receives the same encrypted-at-rest treatment as
-- assertion SessionData. It is bound to the ticket, target user and exact Web
-- session, and is consumed before parsing the browser response.
CREATE TABLE identity.staff_webauthn_enrollment_challenges (
    id uuid PRIMARY KEY,
    ticket_id uuid NOT NULL,
    user_id uuid NOT NULL,
    parent_token_hash bytea NOT NULL CHECK (octet_length(parent_token_hash) = 32),
    label text NOT NULL CHECK (char_length(btrim(label)) BETWEEN 1 AND 80),
    session_ciphertext bytea NOT NULL
        CHECK (octet_length(session_ciphertext) BETWEEN 17 AND 65536),
    session_nonce bytea NOT NULL CHECK (octet_length(session_nonce) = 12),
    key_epoch text NOT NULL CHECK (char_length(key_epoch) BETWEEN 1 AND 64),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    FOREIGN KEY (ticket_id, user_id)
        REFERENCES identity.staff_credential_bootstrap_tickets (id, user_id)
        ON DELETE RESTRICT,
    FOREIGN KEY (parent_token_hash, user_id)
        REFERENCES identity.sessions (token_hash, user_id) ON DELETE CASCADE,
    CHECK (expires_at > created_at),
    CHECK (expires_at <= created_at + interval '10 minutes'),
    CHECK (consumed_at IS NULL OR consumed_at >= created_at)
);

CREATE UNIQUE INDEX staff_webauthn_enrollment_challenges_active_ticket_idx
    ON identity.staff_webauthn_enrollment_challenges (ticket_id)
    WHERE consumed_at IS NULL;

CREATE UNIQUE INDEX staff_webauthn_enrollment_challenges_active_parent_idx
    ON identity.staff_webauthn_enrollment_challenges (parent_token_hash)
    WHERE consumed_at IS NULL;

-- The provenance link is optional for legacy-imported rows but unique for all
-- credentials created by the new bootstrap flow. Keeping it on the credential
-- lets an audit investigation join the durable result back to its ticket.
ALTER TABLE identity.staff_webauthn_credentials
    ADD COLUMN bootstrap_ticket_id uuid,
    ADD CONSTRAINT staff_webauthn_credentials_bootstrap_ticket_fk
        FOREIGN KEY (bootstrap_ticket_id)
        REFERENCES identity.staff_credential_bootstrap_tickets (id)
        ON DELETE RESTRICT;

CREATE UNIQUE INDEX staff_webauthn_credentials_bootstrap_ticket_idx
    ON identity.staff_webauthn_credentials (bootstrap_ticket_id)
    WHERE bootstrap_ticket_id IS NOT NULL;

INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES
(
    'authz.capability.read.staff.self',
    '查看自己的当前后台能力',
    'medium',
    'self',
    'staff-session',
    true,
    false
),
(
    'staff.credential.enroll.self',
    '使用受控票据注册自己的后台 WebAuthn 凭据',
    'critical',
    'self',
    'web-session',
    true,
    true
);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('staff_access', 'authz.capability.read.staff.self'),
    ('staff_access', 'staff.credential.enroll.self');

UPDATE authz.permissions
SET discoverable = true
WHERE action IN (
    'authz.grant.read',
    'authz.grant.revoke.approve.governance',
    'authz.grant.revoke.approve.security',
    'authz.grant.revoke.propose'
);

-- +goose Down
UPDATE authz.permissions
SET discoverable = false
WHERE action IN (
    'authz.grant.read',
    'authz.grant.revoke.approve.governance',
    'authz.grant.revoke.approve.security',
    'authz.grant.revoke.propose'
);
DELETE FROM authz.role_permissions
WHERE role_id = 'staff_access'
  AND action IN (
      'authz.capability.read.staff.self',
      'staff.credential.enroll.self'
  );
DELETE FROM authz.permissions WHERE action IN (
    'authz.capability.read.staff.self',
    'staff.credential.enroll.self'
);

DROP INDEX identity.staff_webauthn_credentials_bootstrap_ticket_idx;
ALTER TABLE identity.staff_webauthn_credentials
    DROP CONSTRAINT staff_webauthn_credentials_bootstrap_ticket_fk,
    DROP COLUMN bootstrap_ticket_id;

DROP TABLE identity.staff_webauthn_enrollment_challenges;
DROP TABLE identity.staff_credential_bootstrap_tickets;
