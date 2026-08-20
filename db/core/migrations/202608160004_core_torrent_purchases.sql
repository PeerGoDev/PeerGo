-- +goose Up

-- A torrent price is write-side metadata.  Zero means that no purchase is
-- required; all non-zero values are integer magic points, matching PeerGo's
-- ledger and avoiding the fractional balances used by the legacy site.
ALTER TABLE torrents.torrents
    ADD COLUMN purchase_price bigint NOT NULL DEFAULT 0
        CHECK (purchase_price BETWEEN 0 AND 1000000);

-- Purchase policy is an immutable timeline.  Every live receipt records the
-- exact revision it used, so later tax changes cannot reinterpret history.
CREATE TABLE economy.torrent_purchase_policy_revisions (
    revision text PRIMARY KEY CHECK (
        revision ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
    ),
    effective_from timestamptz NOT NULL UNIQUE,
    enabled boolean NOT NULL,
    tax_basis_points integer NOT NULL CHECK (
        tax_basis_points BETWEEN 0 AND 10000
    ),
    snapshot_json text NOT NULL CHECK (
        octet_length(snapshot_json) BETWEEN 2 AND 16384
        AND jsonb_typeof(snapshot_json::jsonb) = 'object'
    ),
    snapshot_sha256 bytea NOT NULL CHECK (octet_length(snapshot_sha256) = 32),
    issued_by uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid,
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    created_at timestamptz NOT NULL,
    CHECK (created_at <= effective_from),
    CHECK (
        (issued_by IS NULL AND authorization_decision_id IS NULL)
        OR (issued_by IS NOT NULL AND authorization_decision_id IS NOT NULL)
    )
);

CREATE INDEX torrent_purchase_policy_effective_idx
    ON economy.torrent_purchase_policy_revisions (effective_from DESC, revision DESC);

CREATE TRIGGER torrent_purchase_policy_immutable
BEFORE UPDATE OR DELETE ON economy.torrent_purchase_policy_revisions
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

INSERT INTO economy.torrent_purchase_policy_revisions (
    revision, effective_from, enabled, tax_basis_points,
    snapshot_json, snapshot_sha256, issued_by,
    authorization_decision_id, reason, created_at
) VALUES (
    'torrent-purchase-v1',
    '2026-08-16T00:00:00Z',
    true,
    1000,
    '{"revision":"torrent-purchase-v1","effective_from":"2026-08-16T00:00:00Z","enabled":true,"tax_basis_points":1000,"currency":"magic"}',
    decode('0d802282086c94f56ab13a2ed2fd7c7819c6c6305cb3292840b8ae556a4a438a', 'hex'),
    NULL,
    NULL,
    'Rousi 购买规则兼容基线：整数魔力值与百分之十站点手续费',
    '2026-08-16T00:00:00Z'
);

-- One entitlement is the durable answer to "may this member download this
-- priced torrent?"  Live purchases join an atomic magic transaction; legacy
-- imports intentionally have no PeerGo transaction because charging again at
-- cutover would corrupt both the member balance and the historical statement.
CREATE TABLE economy.torrent_purchase_entitlements (
    id uuid PRIMARY KEY,
    request_id uuid UNIQUE,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    torrent_id bigint NOT NULL REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    seller_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    source_kind text NOT NULL CHECK (source_kind IN ('live_purchase', 'legacy_import')),
    source_reference text NOT NULL CHECK (
        source_reference ~ '^[a-z0-9][a-z0-9:._-]{0,127}$'
    ),
    price bigint NOT NULL CHECK (price BETWEEN 0 AND 1000000),
    tax bigint NOT NULL CHECK (tax BETWEEN 0 AND price),
    seller_income bigint NOT NULL CHECK (
        seller_income >= 0 AND seller_income + tax = price
    ),
    policy_revision text NOT NULL CHECK (
        policy_revision ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
    ),
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    magic_transaction_id uuid UNIQUE
        REFERENCES economy.magic_transactions (id) ON DELETE RESTRICT,
    purchased_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL,
    UNIQUE (user_id, torrent_id),
    CHECK (recorded_at >= purchased_at),
    CHECK (
        (source_kind = 'live_purchase' AND request_id IS NOT NULL AND magic_transaction_id IS NOT NULL)
        OR (source_kind = 'legacy_import' AND request_id IS NULL AND magic_transaction_id IS NULL)
    )
);

CREATE INDEX torrent_purchase_entitlements_user_time_idx
    ON economy.torrent_purchase_entitlements (user_id, purchased_at DESC, torrent_id DESC);

CREATE TRIGGER torrent_purchase_entitlements_immutable
BEFORE UPDATE OR DELETE ON economy.torrent_purchase_entitlements
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

-- Refunds revoke an entitlement without rewriting its purchase receipt.  A
-- migrated pair still remains entitled when the legacy database contains at
-- least one completed purchase, matching the old site's actual access check.
CREATE TABLE economy.torrent_purchase_refunds (
    id uuid PRIMARY KEY,
    entitlement_id uuid NOT NULL UNIQUE
        REFERENCES economy.torrent_purchase_entitlements (id) ON DELETE RESTRICT,
    source_kind text NOT NULL CHECK (source_kind IN ('live_refund', 'legacy_import')),
    source_reference text NOT NULL CHECK (
        source_reference ~ '^[a-z0-9][a-z0-9:._-]{0,127}$'
    ),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 1 AND 1000),
    refunded_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL,
    CHECK (recorded_at >= refunded_at)
);

CREATE TRIGGER torrent_purchase_refunds_immutable
BEFORE UPDATE OR DELETE ON economy.torrent_purchase_refunds
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

-- Every legacy purchase row gets a non-PII evidence receipt, including rows
-- that point to a deleted/unmappable torrent.  This lets reconciliation explain
-- omissions without inventing an entitlement for content that was not moved.
CREATE TABLE migration.torrent_purchase_price_openings (
    legacy_torrent_id bigint PRIMARY KEY CHECK (legacy_torrent_id > 0),
    first_run_id uuid NOT NULL REFERENCES migration.runs (id) ON DELETE RESTRICT,
    torrent_id bigint NOT NULL UNIQUE REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    source_price numeric(38, 20) NOT NULL CHECK (source_price >= 0),
    integer_price bigint NOT NULL CHECK (integer_price BETWEEN 0 AND 1000000),
    source_fingerprint bytea NOT NULL CHECK (octet_length(source_fingerprint) = 32),
    imported_at timestamptz NOT NULL
);

CREATE TRIGGER torrent_purchase_price_openings_immutable
BEFORE UPDATE OR DELETE ON migration.torrent_purchase_price_openings
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TABLE migration.torrent_purchase_openings (
    legacy_purchase_id bigint PRIMARY KEY CHECK (legacy_purchase_id > 0),
    first_run_id uuid NOT NULL REFERENCES migration.runs (id) ON DELETE RESTRICT,
    legacy_torrent_id bigint CHECK (legacy_torrent_id IS NULL OR legacy_torrent_id > 0),
    torrent_id bigint REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    legacy_buyer_id bigint NOT NULL CHECK (legacy_buyer_id > 0),
    buyer_id uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    legacy_seller_id bigint NOT NULL CHECK (legacy_seller_id > 0),
    seller_id uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    source_status text NOT NULL CHECK (source_status IN ('completed', 'refunded')),
    source_price numeric(38, 20) NOT NULL CHECK (source_price >= 0),
    source_tax numeric(38, 20) NOT NULL CHECK (source_tax >= 0),
    source_seller_income numeric(38, 20) NOT NULL CHECK (source_seller_income >= 0),
    integer_price bigint NOT NULL CHECK (integer_price BETWEEN 0 AND 1000000),
    disposition text NOT NULL CHECK (disposition IN (
        'entitled', 'duplicate_completed', 'refunded',
        'unresolved_torrent', 'unmapped_torrent', 'unmapped_user'
    )),
    source_fingerprint bytea NOT NULL CHECK (octet_length(source_fingerprint) = 32),
    purchased_at timestamptz NOT NULL,
    imported_at timestamptz NOT NULL
);

CREATE INDEX torrent_purchase_openings_run_disposition_idx
    ON migration.torrent_purchase_openings (first_run_id, disposition, legacy_purchase_id);

CREATE TRIGGER torrent_purchase_openings_immutable
BEFORE UPDATE OR DELETE ON migration.torrent_purchase_openings
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    ('torrent.purchase.read.self', '查看自己对指定种子的购买状态与价格', 'low', 'self', 'web-session', true, true),
    ('torrent.purchase.create.self', '使用整数魔力值购买指定种子的永久下载权限', 'medium', 'self', 'web-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('member', 'torrent.purchase.read.self'),
    ('member', 'torrent.purchase.create.self');

REVOKE ALL ON economy.torrent_purchase_policy_revisions FROM PUBLIC;
REVOKE ALL ON economy.torrent_purchase_entitlements FROM PUBLIC;
REVOKE ALL ON economy.torrent_purchase_refunds FROM PUBLIC;
REVOKE ALL ON migration.torrent_purchase_openings FROM PUBLIC;
REVOKE ALL ON migration.torrent_purchase_price_openings FROM PUBLIC;

-- +goose Down

DELETE FROM authz.role_permissions
WHERE role_id = 'member'
  AND action IN ('torrent.purchase.read.self', 'torrent.purchase.create.self');

DELETE FROM authz.permissions
WHERE action IN ('torrent.purchase.read.self', 'torrent.purchase.create.self');

DROP TRIGGER torrent_purchase_openings_immutable ON migration.torrent_purchase_openings;
DROP INDEX migration.torrent_purchase_openings_run_disposition_idx;
DROP TABLE migration.torrent_purchase_openings;
DROP TRIGGER torrent_purchase_price_openings_immutable ON migration.torrent_purchase_price_openings;
DROP TABLE migration.torrent_purchase_price_openings;
DROP TRIGGER torrent_purchase_refunds_immutable ON economy.torrent_purchase_refunds;
DROP TABLE economy.torrent_purchase_refunds;
DROP TRIGGER torrent_purchase_entitlements_immutable ON economy.torrent_purchase_entitlements;
DROP INDEX economy.torrent_purchase_entitlements_user_time_idx;
DROP TABLE economy.torrent_purchase_entitlements;
DROP TRIGGER torrent_purchase_policy_immutable ON economy.torrent_purchase_policy_revisions;
DROP INDEX economy.torrent_purchase_policy_effective_idx;
DROP TABLE economy.torrent_purchase_policy_revisions;
ALTER TABLE torrents.torrents DROP COLUMN purchase_price;
