-- +goose Up

-- Member medal mutations are optimistic so a stale browser tab cannot undo a
-- newer wear or priority change. Existing Rousi holdings begin at version 1.
ALTER TABLE economy.user_medals
    ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0);

-- Medal purchases use their own balanced-ledger counterparty instead of the
-- generic fee sink, which keeps operational reconciliation unambiguous.
INSERT INTO economy.magic_accounts (
    id, user_id, account_kind, account_code, balance, version, updated_at
) VALUES (
    '00000000-0000-7000-8000-000000000007', NULL, 'system',
    'system:sink:medal_purchase', 0, 1, clock_timestamp()
);

CREATE TABLE economy.medal_purchases (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL UNIQUE,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    medal_id bigint NOT NULL REFERENCES economy.medal_definitions (id) ON DELETE RESTRICT,
    user_medal_id bigint NOT NULL REFERENCES economy.user_medals (id) ON DELETE RESTRICT,
    price bigint NOT NULL CHECK (price >= 0),
    magic_transaction_id uuid UNIQUE
        REFERENCES economy.magic_transactions (id) ON DELETE RESTRICT,
    purchased_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL,
    CHECK ((price = 0) = (magic_transaction_id IS NULL)),
    CHECK (recorded_at >= purchased_at)
);

CREATE INDEX medal_purchases_user_recent_idx
    ON economy.medal_purchases (user_id, purchased_at DESC, id DESC);

CREATE TRIGGER medal_purchases_immutable
BEFORE UPDATE OR DELETE ON economy.medal_purchases
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    ('economy.medal.read.self', '查看自己的勋章、佩戴加成和勋章商店', 'low', 'self', 'web-session', true, true),
    ('economy.medal.purchase.self', '使用自己的整数魔力值购买勋章', 'medium', 'self', 'web-session', true, true),
    ('economy.medal.wear.self', '佩戴、取下并调整自己的勋章展示顺序', 'medium', 'self', 'web-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('member', 'economy.medal.read.self'),
    ('member', 'economy.medal.purchase.self'),
    ('member', 'economy.medal.wear.self');

REVOKE ALL ON economy.medal_purchases FROM PUBLIC;

-- +goose Down

DELETE FROM authz.role_permissions
WHERE role_id = 'member' AND action IN (
    'economy.medal.read.self',
    'economy.medal.purchase.self',
    'economy.medal.wear.self'
);
DELETE FROM authz.permissions WHERE action IN (
    'economy.medal.read.self',
    'economy.medal.purchase.self',
    'economy.medal.wear.self'
);

DROP TRIGGER medal_purchases_immutable ON economy.medal_purchases;
DROP TABLE economy.medal_purchases;
DELETE FROM economy.magic_accounts
WHERE id = '00000000-0000-7000-8000-000000000007';
ALTER TABLE economy.user_medals DROP COLUMN version;
