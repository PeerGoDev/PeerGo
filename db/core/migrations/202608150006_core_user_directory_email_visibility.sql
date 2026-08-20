-- +goose Up
-- The administrator directory returns the registered email only after the
-- staff session and user.account.read permission have both been enforced.
-- The address remains owned by Privacy Vault and is never persisted in Core.
UPDATE authz.permissions
SET description = '读取账户目录、完整邮箱、运营统计与当前有效限制'
WHERE action = 'user.account.read';

UPDATE authz.roles
SET description = '读取账户目录、Privacy Vault 按需返回的完整邮箱、运营统计与当前账户访问限制；不包含 IP、passkey、Tracker 凭据或限制写入。'
WHERE id = 'user_reader';

-- Preserve the old user access and membership flags as a separate immutable
-- cutover receipt. Account bans and email verification already map into
-- identity.users; VIP and download restriction receive a canonical Core
-- projection so future policy code never needs to query the legacy database.
CREATE TABLE migration.user_status_openings (
    source_system text NOT NULL CHECK (source_system = 'ptyes'),
    legacy_user_id bigint NOT NULL CHECK (legacy_user_id > 0),
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    source_banned boolean NOT NULL,
    source_ban_reason text,
    source_banned_at timestamptz,
    source_banned_until timestamptz,
    source_download_restricted boolean NOT NULL,
    source_email_verified boolean NOT NULL,
    source_vip_enabled boolean NOT NULL,
    source_vip_until timestamptz,
    source_fingerprint bytea NOT NULL CHECK (octet_length(source_fingerprint) = 32),
    first_run_id uuid NOT NULL REFERENCES migration.runs (id) ON DELETE RESTRICT,
    imported_at timestamptz NOT NULL,
    PRIMARY KEY (source_system, legacy_user_id),
    UNIQUE (user_id),
    FOREIGN KEY (source_system, legacy_user_id, user_id)
        REFERENCES migration.user_id_map (source_system, legacy_user_id, user_id)
        ON DELETE RESTRICT
);

CREATE TRIGGER migration_user_status_openings_immutable
BEFORE UPDATE OR DELETE ON migration.user_status_openings
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TABLE identity.user_access_states (
    user_id uuid PRIMARY KEY REFERENCES identity.users (id) ON DELETE RESTRICT,
    download_restricted boolean NOT NULL DEFAULT false,
    vip_enabled boolean NOT NULL DEFAULT false,
    vip_until timestamptz,
    source_run_id uuid REFERENCES migration.runs (id) ON DELETE RESTRICT,
    source_fingerprint bytea CHECK (
        source_fingerprint IS NULL OR octet_length(source_fingerprint) = 32
    ),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL,
    CHECK ((source_run_id IS NULL) = (source_fingerprint IS NULL))
);

CREATE INDEX user_access_states_download_restricted_idx
    ON identity.user_access_states (user_id)
    WHERE download_restricted;

CREATE INDEX user_access_states_vip_idx
    ON identity.user_access_states (vip_until, user_id)
    WHERE vip_enabled;

-- +goose Down
DROP TABLE identity.user_access_states;
DROP TRIGGER migration_user_status_openings_immutable
    ON migration.user_status_openings;
DROP TABLE migration.user_status_openings;

UPDATE authz.roles
SET description = '读取脱敏账户状态与当前账户访问限制；不包含 PII、凭据、账本或限制写入。'
WHERE id = 'user_reader';

UPDATE authz.permissions
SET description = '读取脱敏账户与当前有效限制'
WHERE action = 'user.account.read';
