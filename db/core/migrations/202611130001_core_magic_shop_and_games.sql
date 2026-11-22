-- +goose Up

-- The magic shop and the magic games are the first member-facing sinks that
-- are not tied to one torrent or post. Every price, payout, escrow movement
-- and burn is still a balanced ledger transaction; these tables only hold the
-- receipts and the game state that explains each transaction.
INSERT INTO economy.magic_accounts (
    id, user_id, account_kind, account_code, balance, version, updated_at
) VALUES
    ('00000000-0000-7000-8000-000000000011', NULL, 'system',
     'system:sink:magic_shop', 0, 1, clock_timestamp()),
    ('00000000-0000-7000-8000-000000000012', NULL, 'system',
     'system:house:magic_games', 0, 1, clock_timestamp()),
    ('00000000-0000-7000-8000-000000000013', NULL, 'system',
     'system:escrow:lottery_pool', 0, 1, clock_timestamp()),
    ('00000000-0000-7000-8000-000000000014', NULL, 'system',
     'system:escrow:pvp_challenges', 0, 1, clock_timestamp());

-- One versioned document configures every shop item and game. Prize tables
-- are variable-length, so the document is validated in Core and every
-- accepted version is kept as an immutable revision.
CREATE TABLE economy.magic_store_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    document jsonb NOT NULL CHECK (jsonb_typeof(document) = 'object'),
    version bigint NOT NULL CHECK (version > 0),
    updated_at timestamptz NOT NULL
);

CREATE TABLE economy.magic_store_settings_revisions (
    version bigint PRIMARY KEY CHECK (version > 0),
    document jsonb NOT NULL CHECK (jsonb_typeof(document) = 'object'),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 500),
    actor_id uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid,
    occurred_at timestamptz NOT NULL,
    CHECK ((actor_id IS NULL) = (authorization_decision_id IS NULL))
);

CREATE TRIGGER magic_store_settings_revisions_immutable
BEFORE UPDATE OR DELETE ON economy.magic_store_settings_revisions
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

INSERT INTO economy.magic_store_settings (singleton, document, version, updated_at)
VALUES (true, '{
  "timezone": "Asia/Shanghai",
  "shop_enabled": true,
  "invite": {"enabled": true, "price": 10000, "monthly_limit": 2},
  "temporary_invite": {"enabled": true, "price": 3000, "valid_days": 7, "monthly_limit": 5},
  "vip": {"enabled": true, "price": 50000, "duration_days": 30},
  "name_color": {"enabled": true, "solid_price": 5000, "gradient_price": 12000, "duration_days": 30},
  "makeup_card": {"enabled": true, "price": 800, "window_days": 7, "maximum_holding": 10},
  "games_enabled": true,
  "scratch": {
    "enabled": true, "price": 100, "daily_limit": 20,
    "prizes": [
      {"label": "谢谢参与", "payout": 0, "weight": 6385},
      {"label": "回本", "payout": 100, "weight": 2000},
      {"label": "双倍", "payout": 200, "weight": 1000},
      {"label": "五倍", "payout": 500, "weight": 500},
      {"label": "十倍", "payout": 1000, "weight": 100},
      {"label": "头奖", "payout": 5000, "weight": 15}
    ]
  },
  "lottery": {
    "enabled": true, "ticket_price": 100, "maximum_tickets_per_round": 50,
    "draw_hour": 21, "burn_bps": 1000,
    "tiers": [
      {"label": "一等奖", "winners": 1, "share_bps": 5000},
      {"label": "二等奖", "winners": 3, "share_bps": 3000},
      {"label": "三等奖", "winners": 10, "share_bps": 2000}
    ]
  },
  "pvp": {
    "enabled": true, "minimum_stake": 100, "maximum_stake": 10000,
    "fee_bps": 500, "maximum_open_per_member": 3, "expiry_hours": 24
  }
}'::jsonb, 1, clock_timestamp());

INSERT INTO economy.magic_store_settings_revisions (
    version, document, reason, occurred_at
)
SELECT version, document, '魔力商店与魔力游戏的初始配置。', updated_at
FROM economy.magic_store_settings;

-- A shop order is the immutable receipt of one purchase. The effect columns
-- record exactly what the purchase delivered so support can explain it later.
CREATE TABLE economy.magic_shop_orders (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    item_key text NOT NULL CHECK (item_key IN (
        'invite', 'temporary_invite', 'vip',
        'name_color_solid', 'name_color_gradient', 'makeup_card'
    )),
    quantity integer NOT NULL CHECK (quantity BETWEEN 1 AND 100),
    unit_price bigint NOT NULL CHECK (unit_price > 0),
    total_price bigint NOT NULL CHECK (total_price = unit_price * quantity),
    style_key text CHECK (style_key IS NULL OR style_key ~ '^[a-z][a-z0-9-]{0,31}$'),
    effect_until timestamptz,
    settings_version bigint NOT NULL CHECK (settings_version > 0),
    magic_transaction_id uuid NOT NULL UNIQUE
        REFERENCES economy.magic_transactions (id) ON DELETE RESTRICT,
    purchased_at timestamptz NOT NULL,
    UNIQUE (user_id, request_id),
    CHECK ((item_key IN ('name_color_solid', 'name_color_gradient')) = (style_key IS NOT NULL))
);

CREATE INDEX magic_shop_orders_user_item_time_idx
    ON economy.magic_shop_orders (user_id, item_key, purchased_at DESC);

CREATE TRIGGER magic_shop_orders_immutable
BEFORE UPDATE OR DELETE ON economy.magic_shop_orders
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

-- A colored username is a timed cosmetic. Re-purchasing extends it; the
-- member may switch between styles allowed by the purchased tier.
CREATE TABLE economy.member_name_styles (
    user_id uuid PRIMARY KEY REFERENCES identity.users (id) ON DELETE RESTRICT,
    tier text NOT NULL CHECK (tier IN ('solid', 'gradient')),
    style_key text NOT NULL CHECK (style_key ~ '^[a-z][a-z0-9-]{0,31}$'),
    expires_at timestamptz NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL
);

CREATE INDEX member_name_styles_active_idx
    ON economy.member_name_styles (expires_at);

-- Consumable items (currently only attendance make-up cards) keep a small
-- mutable balance plus an append-only event history.
CREATE TABLE economy.member_item_balances (
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    item_key text NOT NULL CHECK (item_key IN ('makeup_card')),
    quantity integer NOT NULL CHECK (quantity BETWEEN 0 AND 1000),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, item_key)
);

CREATE TABLE economy.member_item_events (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    item_key text NOT NULL CHECK (item_key IN ('makeup_card')),
    delta integer NOT NULL CHECK (delta <> 0),
    balance_after integer NOT NULL CHECK (balance_after BETWEEN 0 AND 1000),
    source_reference text NOT NULL UNIQUE CHECK (
        source_reference ~ '^[a-z0-9][a-z0-9:._-]{0,127}$'
    ),
    occurred_at timestamptz NOT NULL
);

CREATE INDEX member_item_events_user_time_idx
    ON economy.member_item_events (user_id, occurred_at DESC);

CREATE TRIGGER member_item_events_immutable
BEFORE UPDATE OR DELETE ON economy.member_item_events
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

ALTER TABLE economy.attendance_records
    ADD COLUMN makeup boolean NOT NULL DEFAULT false;

-- A temporary invitation is a purchased issuance credit with its own expiry.
-- Issuing an invitation consumes the earliest credit before the permanent
-- balance; revoking that invitation returns the credit while it is valid.
CREATE TABLE identity.temporary_invitation_credits (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    shop_order_id uuid NOT NULL
        REFERENCES economy.magic_shop_orders (id) ON DELETE RESTRICT,
    expires_at timestamptz NOT NULL,
    invitation_id uuid UNIQUE
        REFERENCES identity.registration_invitations (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (expires_at > created_at)
);

CREATE INDEX temporary_invitation_credits_available_idx
    ON identity.temporary_invitation_credits (user_id, expires_at)
    WHERE invitation_id IS NULL;

-- Shop-purchased VIP time is a system transition without a staff actor.
-- +goose StatementBegin
DO $$
DECLARE
    constraint_name text;
BEGIN
    FOR constraint_name IN
        SELECT conname
        FROM pg_constraint
        WHERE conrelid = 'identity.user_vip_transitions'::regclass
          AND contype = 'c'
          AND pg_get_constraintdef(oid) LIKE '%origin%'
    LOOP
        EXECUTE format(
            'ALTER TABLE identity.user_vip_transitions DROP CONSTRAINT %I',
            constraint_name
        );
    END LOOP;
END
$$;
-- +goose StatementEnd

ALTER TABLE identity.user_vip_transitions
    ADD CONSTRAINT user_vip_transitions_origin_check CHECK (
        origin IN ('legacy_migration', 'system_backfill', 'staff', 'shop')
    ),
    ADD CONSTRAINT user_vip_transitions_origin_actor_check CHECK (
        (origin IN ('legacy_migration', 'system_backfill', 'shop') AND actor_id IS NULL)
        OR (origin = 'staff' AND actor_id IS NOT NULL)
    );

-- Scratch cards settle instantly; the receipt keeps the drawn prize tier.
CREATE TABLE economy.scratch_cards (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    price bigint NOT NULL CHECK (price > 0),
    prize bigint NOT NULL CHECK (prize >= 0),
    prize_index integer NOT NULL CHECK (prize_index BETWEEN 0 AND 31),
    prize_label text NOT NULL CHECK (char_length(prize_label) BETWEEN 1 AND 20),
    settings_version bigint NOT NULL CHECK (settings_version > 0),
    purchase_transaction_id uuid NOT NULL UNIQUE
        REFERENCES economy.magic_transactions (id) ON DELETE RESTRICT,
    payout_transaction_id uuid UNIQUE
        REFERENCES economy.magic_transactions (id) ON DELETE RESTRICT,
    purchased_at timestamptz NOT NULL,
    UNIQUE (user_id, request_id),
    CHECK ((prize = 0) = (payout_transaction_id IS NULL))
);

CREATE INDEX scratch_cards_user_time_idx
    ON economy.scratch_cards (user_id, purchased_at DESC);

CREATE TRIGGER scratch_cards_immutable
BEFORE UPDATE OR DELETE ON economy.scratch_cards
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

-- Daily lottery. The seed digest is published when the round opens and the
-- seed itself is only exposed after the draw, so anyone can recompute the
-- winners and confirm the seed was fixed before the first ticket was sold.
CREATE TABLE economy.lottery_rounds (
    id uuid PRIMARY KEY,
    round_number bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
    status text NOT NULL CHECK (status IN ('open', 'drawn')),
    ticket_price bigint NOT NULL CHECK (ticket_price > 0),
    maximum_tickets_per_member integer NOT NULL
        CHECK (maximum_tickets_per_member BETWEEN 1 AND 1000),
    burn_bps integer NOT NULL CHECK (burn_bps BETWEEN 0 AND 10000),
    tiers jsonb NOT NULL CHECK (jsonb_typeof(tiers) = 'array'),
    opens_at timestamptz NOT NULL,
    draw_at timestamptz NOT NULL,
    seed_sha256 bytea NOT NULL CHECK (octet_length(seed_sha256) = 32),
    seed bytea NOT NULL CHECK (octet_length(seed) = 32),
    carried_in bigint NOT NULL CHECK (carried_in >= 0),
    tickets_sold integer NOT NULL DEFAULT 0 CHECK (tickets_sold >= 0),
    sales_total bigint NOT NULL DEFAULT 0 CHECK (sales_total >= 0),
    burned bigint CHECK (burned >= 0),
    paid_out bigint CHECK (paid_out >= 0),
    carried_out bigint CHECK (carried_out >= 0),
    drawn_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (draw_at > opens_at),
    CHECK (
        (status = 'open' AND drawn_at IS NULL
            AND burned IS NULL AND paid_out IS NULL AND carried_out IS NULL)
        OR
        (status = 'drawn' AND drawn_at IS NOT NULL
            AND burned IS NOT NULL AND paid_out IS NOT NULL AND carried_out IS NOT NULL
            AND burned + paid_out + carried_out = carried_in + sales_total)
    )
);

CREATE UNIQUE INDEX lottery_rounds_single_open_idx
    ON economy.lottery_rounds ((true))
    WHERE status = 'open';

CREATE TABLE economy.lottery_purchases (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL,
    round_id uuid NOT NULL REFERENCES economy.lottery_rounds (id) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    quantity integer NOT NULL CHECK (quantity BETWEEN 1 AND 1000),
    first_ticket integer NOT NULL CHECK (first_ticket >= 1),
    last_ticket integer NOT NULL,
    total_price bigint NOT NULL CHECK (total_price > 0),
    magic_transaction_id uuid NOT NULL UNIQUE
        REFERENCES economy.magic_transactions (id) ON DELETE RESTRICT,
    purchased_at timestamptz NOT NULL,
    UNIQUE (user_id, request_id),
    CHECK (last_ticket = first_ticket + quantity - 1)
);

CREATE INDEX lottery_purchases_round_ticket_idx
    ON economy.lottery_purchases (round_id, first_ticket);
CREATE INDEX lottery_purchases_user_round_idx
    ON economy.lottery_purchases (user_id, round_id);

CREATE TRIGGER lottery_purchases_immutable
BEFORE UPDATE OR DELETE ON economy.lottery_purchases
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TABLE economy.lottery_winners (
    round_id uuid NOT NULL REFERENCES economy.lottery_rounds (id) ON DELETE RESTRICT,
    draw_rank integer NOT NULL CHECK (draw_rank >= 1),
    tier_index integer NOT NULL CHECK (tier_index BETWEEN 0 AND 9),
    tier_label text NOT NULL CHECK (char_length(tier_label) BETWEEN 1 AND 20),
    ticket_number integer NOT NULL CHECK (ticket_number >= 1),
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    prize bigint NOT NULL CHECK (prize >= 0),
    magic_transaction_id uuid UNIQUE
        REFERENCES economy.magic_transactions (id) ON DELETE RESTRICT,
    PRIMARY KEY (round_id, draw_rank),
    UNIQUE (round_id, ticket_number),
    CHECK ((prize = 0) = (magic_transaction_id IS NULL))
);

CREATE INDEX lottery_winners_user_idx ON economy.lottery_winners (user_id);

CREATE TRIGGER lottery_winners_immutable
BEFORE UPDATE OR DELETE ON economy.lottery_winners
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

-- PvP challenges: both stakes sit in escrow until the challenge settles,
-- is cancelled by its creator or expires.
CREATE TABLE economy.pvp_challenges (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL,
    mode text NOT NULL CHECK (mode IN ('dice', 'rps')),
    status text NOT NULL CHECK (status IN ('open', 'settled', 'cancelled', 'expired')),
    creator_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    creator_choice text CHECK (creator_choice IN ('rock', 'paper', 'scissors')),
    stake bigint NOT NULL CHECK (stake > 0),
    fee_bps integer NOT NULL CHECK (fee_bps BETWEEN 0 AND 5000),
    message text CHECK (message IS NULL OR char_length(message) BETWEEN 1 AND 60),
    opponent_id uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    opponent_choice text CHECK (opponent_choice IN ('rock', 'paper', 'scissors')),
    creator_roll integer CHECK (creator_roll BETWEEN 1 AND 100),
    opponent_roll integer CHECK (opponent_roll BETWEEN 1 AND 100),
    winner_id uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    fee bigint CHECK (fee >= 0),
    stake_transaction_id uuid NOT NULL UNIQUE
        REFERENCES economy.magic_transactions (id) ON DELETE RESTRICT,
    accept_transaction_id uuid UNIQUE
        REFERENCES economy.magic_transactions (id) ON DELETE RESTRICT,
    closing_transaction_id uuid UNIQUE
        REFERENCES economy.magic_transactions (id) ON DELETE RESTRICT,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    closed_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL,
    UNIQUE (creator_id, request_id),
    CHECK (expires_at > created_at),
    CHECK ((mode = 'rps') = (creator_choice IS NOT NULL)),
    CHECK (opponent_id IS NULL OR opponent_id <> creator_id),
    CHECK ((status = 'open') = (closed_at IS NULL)),
    CHECK (status <> 'settled' OR (opponent_id IS NOT NULL AND accept_transaction_id IS NOT NULL
        AND closing_transaction_id IS NOT NULL AND fee IS NOT NULL)),
    CHECK (status NOT IN ('cancelled', 'expired') OR (opponent_id IS NULL
        AND closing_transaction_id IS NOT NULL))
);

CREATE INDEX pvp_challenges_open_idx
    ON economy.pvp_challenges (expires_at)
    WHERE status = 'open';
CREATE INDEX pvp_challenges_creator_idx
    ON economy.pvp_challenges (creator_id, created_at DESC);
CREATE INDEX pvp_challenges_opponent_idx
    ON economy.pvp_challenges (opponent_id, created_at DESC)
    WHERE opponent_id IS NOT NULL;
CREATE INDEX pvp_challenges_recent_closed_idx
    ON economy.pvp_challenges (closed_at DESC)
    WHERE status = 'settled';

INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    ('economy.magicshop.read.self', '查看魔力商店、自己的购买记录和彩色 ID', 'low', 'self', 'web-session', true, true),
    ('economy.magicshop.purchase.self', '使用自己的整数魔力值购买商店道具并切换彩色 ID', 'medium', 'self', 'web-session', true, true),
    ('economy.magicgame.read.self', '查看魔力游戏、奖池和自己的游戏记录', 'low', 'self', 'web-session', true, true),
    ('economy.magicgame.play.self', '使用自己的整数魔力值参与刮刮乐、彩票和擂台', 'medium', 'self', 'web-session', true, true),
    ('economy.namestyle.read', '读取全站有效的彩色用户名样式', 'low', 'none', 'web-session', true, true),
    ('economy.magicstore.manage.read', '读取魔力商店与魔力游戏配置和资金账户', 'medium', 'none', 'staff-session', true, true),
    ('economy.magicstore.update', '修改魔力商店价格、限购和游戏赔率', 'high', 'none', 'staff-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('member', 'economy.magicshop.read.self'),
    ('member', 'economy.magicshop.purchase.self'),
    ('member', 'economy.magicgame.read.self'),
    ('member', 'economy.magicgame.play.self'),
    ('member', 'economy.namestyle.read'),
    ('site_admin', 'economy.magicstore.manage.read'),
    ('site_admin', 'economy.magicstore.update');

REVOKE ALL ON economy.magic_store_settings FROM PUBLIC;
REVOKE ALL ON economy.magic_store_settings_revisions FROM PUBLIC;
REVOKE ALL ON economy.magic_shop_orders FROM PUBLIC;
REVOKE ALL ON economy.member_name_styles FROM PUBLIC;
REVOKE ALL ON economy.member_item_balances FROM PUBLIC;
REVOKE ALL ON economy.member_item_events FROM PUBLIC;
REVOKE ALL ON identity.temporary_invitation_credits FROM PUBLIC;
REVOKE ALL ON economy.scratch_cards FROM PUBLIC;
REVOKE ALL ON economy.lottery_rounds FROM PUBLIC;
REVOKE ALL ON economy.lottery_purchases FROM PUBLIC;
REVOKE ALL ON economy.lottery_winners FROM PUBLIC;
REVOKE ALL ON economy.pvp_challenges FROM PUBLIC;

-- +goose Down

DELETE FROM authz.role_permissions WHERE action IN (
    'economy.magicshop.read.self', 'economy.magicshop.purchase.self',
    'economy.magicgame.read.self', 'economy.magicgame.play.self',
    'economy.namestyle.read', 'economy.magicstore.manage.read',
    'economy.magicstore.update'
);
DELETE FROM authz.permissions WHERE action IN (
    'economy.magicshop.read.self', 'economy.magicshop.purchase.self',
    'economy.magicgame.read.self', 'economy.magicgame.play.self',
    'economy.namestyle.read', 'economy.magicstore.manage.read',
    'economy.magicstore.update'
);

DROP TABLE economy.pvp_challenges;
DROP TRIGGER lottery_winners_immutable ON economy.lottery_winners;
DROP TABLE economy.lottery_winners;
DROP TRIGGER lottery_purchases_immutable ON economy.lottery_purchases;
DROP TABLE economy.lottery_purchases;
DROP TABLE economy.lottery_rounds;
DROP TRIGGER scratch_cards_immutable ON economy.scratch_cards;
DROP TABLE economy.scratch_cards;

ALTER TABLE identity.user_vip_transitions
    DROP CONSTRAINT user_vip_transitions_origin_actor_check,
    DROP CONSTRAINT user_vip_transitions_origin_check,
    ADD CONSTRAINT user_vip_transitions_origin_check CHECK (
        origin IN ('legacy_migration', 'system_backfill', 'staff')
    ),
    ADD CONSTRAINT user_vip_transitions_origin_actor_check CHECK (
        (origin IN ('legacy_migration', 'system_backfill') AND actor_id IS NULL)
        OR (origin = 'staff' AND actor_id IS NOT NULL)
    );

DROP TABLE identity.temporary_invitation_credits;
ALTER TABLE economy.attendance_records DROP COLUMN makeup;
DROP TRIGGER member_item_events_immutable ON economy.member_item_events;
DROP TABLE economy.member_item_events;
DROP TABLE economy.member_item_balances;
DROP TABLE economy.member_name_styles;
DROP TRIGGER magic_shop_orders_immutable ON economy.magic_shop_orders;
DROP TABLE economy.magic_shop_orders;
DROP TRIGGER magic_store_settings_revisions_immutable ON economy.magic_store_settings_revisions;
DROP TABLE economy.magic_store_settings_revisions;
DROP TABLE economy.magic_store_settings;

DELETE FROM economy.magic_accounts WHERE id IN (
    '00000000-0000-7000-8000-000000000011',
    '00000000-0000-7000-8000-000000000012',
    '00000000-0000-7000-8000-000000000013',
    '00000000-0000-7000-8000-000000000014'
);
