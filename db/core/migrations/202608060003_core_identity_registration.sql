-- +goose Up
-- Registration policy belongs to identity admission, not catalog presentation.
-- Keeping it as a typed singleton prevents a second, contradictory setting
-- source when the staff identity-settings section is introduced later.
CREATE TABLE identity.registration_policy (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    mode text NOT NULL CHECK (mode IN ('open', 'invite', 'closed')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO identity.registration_policy (singleton, mode, updated_at)
SELECT
    true,
    COALESCE((SELECT registration_mode FROM catalog.site_profile WHERE singleton = true), 'closed'),
    COALESCE((SELECT updated_at FROM catalog.site_profile WHERE singleton = true), now());

ALTER TABLE catalog.site_profile
    DROP COLUMN registration_mode;

-- Only a SHA-256 digest of the high-entropy invitation token is persisted.
-- The raw token may be delivered once by a future operator command, but it
-- must never be recoverable from either Core PostgreSQL or audit evidence.
CREATE TABLE identity.registration_invitations (
    id uuid PRIMARY KEY,
    token_sha256 bytea NOT NULL UNIQUE CHECK (octet_length(token_sha256) = 32),
    note text NOT NULL DEFAULT '' CHECK (char_length(note) <= 120),
    expires_at timestamptz NOT NULL,
    claimed_by uuid,
    claimed_at timestamptz,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (claimed_by IS NULL AND claimed_at IS NULL AND consumed_at IS NULL)
        OR
        (claimed_by IS NOT NULL AND claimed_at IS NOT NULL)
    ),
    CHECK (consumed_at IS NULL OR consumed_at >= claimed_at)
);

-- This table is the durable Core half of the cross-database registration
-- state machine. It contains no email address or password-derived material;
-- those values cross process memory only on their way to Privacy Vault.
CREATE TABLE identity.registrations (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL UNIQUE,
    username text NOT NULL CHECK (
        char_length(username) BETWEEN 3 AND 32
        AND username = lower(username)
        AND username ~ '^[a-z0-9][a-z0-9_-]{2,31}$'
    ),
    display_name text NOT NULL CHECK (char_length(btrim(display_name)) BETWEEN 1 AND 40),
    admission_mode text NOT NULL CHECK (admission_mode IN ('open', 'invite')),
    invitation_id uuid REFERENCES identity.registration_invitations (id) ON DELETE RESTRICT,
    credential_ref uuid UNIQUE,
    state text NOT NULL DEFAULT 'reserved'
        CHECK (state IN ('reserved', 'credential_provisioned', 'completed')),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    completed_at timestamptz,
    CHECK ((admission_mode = 'invite') = (invitation_id IS NOT NULL)),
    CHECK ((state = 'reserved') = (credential_ref IS NULL)),
    CHECK ((state = 'completed') = (completed_at IS NOT NULL))
);

CREATE UNIQUE INDEX registrations_username_casefold_unique
    ON identity.registrations (lower(username));

ALTER TABLE identity.registration_invitations
    ADD CONSTRAINT registration_invitations_claimed_by_fkey
    FOREIGN KEY (claimed_by) REFERENCES identity.registrations (id)
    ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;

-- Existing accounts predate this workflow and are treated as already verified.
-- New registrations remain NULL until the immediately following verification
-- slice proves ownership through a one-time Vault-owned token.
ALTER TABLE identity.users
    ADD COLUMN email_verified_at timestamptz;

UPDATE identity.users
SET email_verified_at = created_at;

INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES (
    'account.register.anonymous',
    '按当前准入策略创建普通账户',
    'medium',
    'none',
    'anonymous',
    false,
    false
);

-- +goose Down
DELETE FROM authz.permissions WHERE action = 'account.register.anonymous';

ALTER TABLE catalog.site_profile
    ADD COLUMN registration_mode text;

UPDATE catalog.site_profile AS profile
SET registration_mode = policy.mode
FROM identity.registration_policy AS policy
WHERE profile.singleton = true
  AND policy.singleton = true;

ALTER TABLE catalog.site_profile
    ALTER COLUMN registration_mode SET NOT NULL,
    ADD CONSTRAINT site_profile_registration_mode_check
        CHECK (registration_mode IN ('open', 'invite', 'closed'));

ALTER TABLE identity.users
    DROP COLUMN email_verified_at;

ALTER TABLE identity.registration_invitations
    DROP CONSTRAINT registration_invitations_claimed_by_fkey;

DROP TABLE identity.registrations;
DROP TABLE identity.registration_invitations;
DROP TABLE identity.registration_policy;
