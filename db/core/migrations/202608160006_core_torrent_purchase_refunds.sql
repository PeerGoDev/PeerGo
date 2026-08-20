-- +goose Up

-- A refunded purchase may be bought again later.  Purchase sequence keeps all
-- immutable receipts while the domain lock guarantees at most one unrefunded
-- entitlement for a user/torrent pair.
ALTER TABLE economy.torrent_purchase_entitlements
    ADD COLUMN purchase_sequence bigint NOT NULL DEFAULT 1
        CHECK (purchase_sequence > 0);

ALTER TABLE economy.torrent_purchase_entitlements
    DROP CONSTRAINT torrent_purchase_entitlements_user_id_torrent_id_key;

ALTER TABLE economy.torrent_purchase_entitlements
    ADD CONSTRAINT torrent_purchase_entitlements_user_torrent_sequence_key
        UNIQUE (user_id, torrent_id, purchase_sequence);

-- Legacy refund rows are historical evidence only.  Online refunds carry the
-- authorisation decision, exact returned amount, stable post-refund balance
-- and ledger evidence needed for idempotent replay.
ALTER TABLE economy.torrent_purchase_refunds
    ADD COLUMN refunded_by uuid
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    ADD COLUMN authorization_decision_id uuid,
    ADD COLUMN refund_amount bigint CHECK (
        refund_amount BETWEEN 0 AND 1000000
    ),
    ADD COLUMN buyer_balance_after bigint,
    ADD COLUMN payload_sha256 bytea CHECK (
        payload_sha256 IS NULL OR octet_length(payload_sha256) = 32
    ),
    ADD COLUMN magic_transaction_id uuid UNIQUE
        REFERENCES economy.magic_transactions (id) ON DELETE RESTRICT,
    ADD CONSTRAINT torrent_purchase_live_refund_evidence_check CHECK (
        (
            source_kind = 'live_refund'
            AND refunded_by IS NOT NULL
            AND authorization_decision_id IS NOT NULL
            AND refund_amount IS NOT NULL
            AND buyer_balance_after IS NOT NULL
            AND payload_sha256 IS NOT NULL
            AND char_length(btrim(reason)) BETWEEN 10 AND 1000
            AND (
                (refund_amount = 0 AND magic_transaction_id IS NULL)
                OR (refund_amount > 0 AND magic_transaction_id IS NOT NULL)
            )
        )
        OR (
            source_kind = 'legacy_import'
            AND refunded_by IS NULL
            AND authorization_decision_id IS NULL
            AND refund_amount IS NULL
            AND buyer_balance_after IS NULL
            AND payload_sha256 IS NULL
            AND magic_transaction_id IS NULL
        )
    );

CREATE INDEX torrent_purchase_refunds_recent_idx
    ON economy.torrent_purchase_refunds (refunded_at DESC, id DESC);

INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    (
        'torrent.purchase.manage.read',
        '查看全站种子购买与退款记录',
        'low', 'none', 'staff-session', true, true
    ),
    (
        'torrent.purchase.manage.refund',
        '撤销种子购买权限并由站点返还整数魔力值',
        'high', 'none', 'staff-session', true, true
    );

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('site_admin', 'torrent.purchase.manage.read'),
    ('site_admin', 'torrent.purchase.manage.refund');

-- +goose Down

DELETE FROM authz.role_permissions
WHERE role_id = 'site_admin'
  AND action IN (
      'torrent.purchase.manage.read',
      'torrent.purchase.manage.refund'
  );

DELETE FROM authz.permissions
WHERE action IN (
    'torrent.purchase.manage.read',
    'torrent.purchase.manage.refund'
);

DROP INDEX economy.torrent_purchase_refunds_recent_idx;

ALTER TABLE economy.torrent_purchase_refunds
    DROP CONSTRAINT torrent_purchase_live_refund_evidence_check,
    DROP COLUMN magic_transaction_id,
    DROP COLUMN payload_sha256,
    DROP COLUMN buyer_balance_after,
    DROP COLUMN refund_amount,
    DROP COLUMN authorization_decision_id,
    DROP COLUMN refunded_by;

ALTER TABLE economy.torrent_purchase_entitlements
    DROP CONSTRAINT torrent_purchase_entitlements_user_torrent_sequence_key;

ALTER TABLE economy.torrent_purchase_entitlements
    ADD CONSTRAINT torrent_purchase_entitlements_user_id_torrent_id_key
        UNIQUE (user_id, torrent_id);

ALTER TABLE economy.torrent_purchase_entitlements
    DROP COLUMN purchase_sequence;
