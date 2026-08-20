-- +goose Up

-- Staff changes reuse the request UUID as an idempotency fence.  The migrated
-- baseline predates online administration and is intentionally the only row
-- without a request identifier.
ALTER TABLE economy.torrent_purchase_policy_revisions
    ADD COLUMN request_id uuid;

CREATE UNIQUE INDEX torrent_purchase_policy_request_idx
    ON economy.torrent_purchase_policy_revisions (request_id)
    WHERE request_id IS NOT NULL;

ALTER TABLE economy.torrent_purchase_policy_revisions
    ADD CONSTRAINT torrent_purchase_policy_staff_request_check CHECK (
        request_id IS NOT NULL OR issued_by IS NULL
    );

-- A torrent price changes the aggregate version but never rewrites an existing
-- purchase receipt.  This append-only command table makes every staff edit
-- replayable and preserves the exact before/after values reviewed by staff.
CREATE TABLE economy.torrent_purchase_price_changes (
    id uuid PRIMARY KEY,
    torrent_id bigint NOT NULL
        REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    actor_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    previous_price bigint NOT NULL CHECK (previous_price BETWEEN 0 AND 1000000),
    resulting_price bigint NOT NULL CHECK (resulting_price BETWEEN 0 AND 1000000),
    expected_torrent_version bigint NOT NULL CHECK (expected_torrent_version > 0),
    resulting_torrent_version bigint NOT NULL CHECK (
        resulting_torrent_version = expected_torrent_version + 1
    ),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    authorization_decision_id uuid NOT NULL,
    occurred_at timestamptz NOT NULL,
    UNIQUE (torrent_id, expected_torrent_version),
    CHECK (previous_price <> resulting_price)
);

CREATE INDEX torrent_purchase_price_changes_recent_idx
    ON economy.torrent_purchase_price_changes (torrent_id, occurred_at DESC, id DESC);

CREATE TRIGGER torrent_purchase_price_changes_immutable
BEFORE UPDATE OR DELETE ON economy.torrent_purchase_price_changes
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES (
    'torrent.purchase.manage.update',
    '更新全站种子购买规则或单个种子的整数魔力值价格',
    'high', 'none', 'staff-session', true, true
);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('site_admin', 'torrent.purchase.manage.update');

REVOKE ALL ON economy.torrent_purchase_price_changes FROM PUBLIC;

-- +goose Down

DELETE FROM authz.role_permissions
WHERE role_id = 'site_admin'
  AND action = 'torrent.purchase.manage.update';

DELETE FROM authz.permissions
WHERE action = 'torrent.purchase.manage.update';

DROP TRIGGER torrent_purchase_price_changes_immutable
    ON economy.torrent_purchase_price_changes;
DROP INDEX economy.torrent_purchase_price_changes_recent_idx;
DROP TABLE economy.torrent_purchase_price_changes;

ALTER TABLE economy.torrent_purchase_policy_revisions
    DROP CONSTRAINT torrent_purchase_policy_staff_request_check;
DROP INDEX economy.torrent_purchase_policy_request_idx;
ALTER TABLE economy.torrent_purchase_policy_revisions DROP COLUMN request_id;
