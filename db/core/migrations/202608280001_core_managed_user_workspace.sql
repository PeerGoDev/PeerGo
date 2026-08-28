-- +goose Up

-- User-data administration is intentionally separate from account-access
-- restriction powers.  Both permissions are site-admin-only by default:
-- numeric balances are high-risk writes and IP observations are private
-- operational data.
INSERT INTO authz.permissions (
    action, description, risk_level, relationship, credential_audience,
    grantable, discoverable
) VALUES
    ('user.account.adjust', '增减用户流量、魔力值、经验、邀请和捐赠数据', 'high', 'none', 'staff-session', true, true),
    ('user.network.read', '读取用户有限保留的登录 IP 聚合历史', 'high', 'none', 'staff-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('site_admin', 'user.account.adjust'),
    ('site_admin', 'user.network.read');

-- A balanced economy transaction always has an explicit counterparty.  The
-- account may become negative because it represents administrator-issued
-- credits, while member accounts still cannot be debited below zero.
INSERT INTO economy.magic_accounts (
    id, user_id, account_kind, account_code, balance, version, updated_at
) VALUES (
    '00000000-0000-7000-8000-000000000009',
    NULL,
    'system',
    'system:adjustment:administrator',
    0,
    1,
    '2026-08-28T00:00:00Z'
);

INSERT INTO progression.experience_policy_revisions (
    revision, source_kind, effective_from, payload_sha256, created_at
) VALUES (
    'administrator-adjustment-v1',
    'administrator_adjustment',
    transaction_timestamp(),
    decode('82fc22d5cbf809aef936fe90ab3e30982e752928597775b4a566f0febbb2d781', 'hex'),
    transaction_timestamp()
);

-- Donation is a monetary decimal, never a float.  The small projection is
-- reconciled against one immutable PtYes opening plus later staff events.
CREATE TABLE identity.user_donation_totals (
    user_id uuid PRIMARY KEY REFERENCES identity.users (id) ON DELETE RESTRICT,
    amount numeric(12, 2) NOT NULL DEFAULT 0
        CHECK (amount BETWEEN 0 AND 9999999999.99),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL
);

CREATE TABLE migration.user_donation_openings (
    source_system text NOT NULL DEFAULT 'ptyes' CHECK (source_system = 'ptyes'),
    legacy_user_id bigint PRIMARY KEY CHECK (legacy_user_id > 0),
    user_id uuid NOT NULL UNIQUE REFERENCES identity.users (id) ON DELETE RESTRICT,
    source_donated numeric(12, 2) NOT NULL
        CHECK (source_donated BETWEEN 0 AND 9999999999.99),
    source_fingerprint bytea NOT NULL CHECK (octet_length(source_fingerprint) = 32),
    first_run_id uuid NOT NULL REFERENCES migration.runs (id) ON DELETE RESTRICT,
    imported_at timestamptz NOT NULL,
    FOREIGN KEY (source_system, legacy_user_id, user_id)
        REFERENCES migration.user_id_map (source_system, legacy_user_id, user_id)
        ON DELETE RESTRICT
);

-- One generic immutable command receipt covers every editable user datum.
-- Domain ledgers remain authoritative for magic/experience while this table
-- records the staff actor, reason and account administration version.
CREATE TABLE identity.managed_user_adjustment_events (
    id uuid PRIMARY KEY,
    idempotency_key uuid NOT NULL UNIQUE,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    actor_user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    field text NOT NULL CHECK (field IN (
        'uploaded_bytes',
        'downloaded_bytes',
        'magic_balance',
        'experience',
        'remaining_invites',
        'donation_amount'
    )),
    delta numeric(38, 20) NOT NULL CHECK (delta <> 0),
    balance_after numeric(38, 20) NOT NULL CHECK (balance_after >= 0),
    reason_summary text NOT NULL
        CHECK (char_length(btrim(reason_summary)) BETWEEN 1 AND 500),
    authorization_decision_id uuid NOT NULL,
    linked_ledger_id uuid,
    user_version_before bigint NOT NULL CHECK (user_version_before > 0),
    user_version_after bigint NOT NULL CHECK (user_version_after = user_version_before + 1),
    occurred_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL,
    CHECK (recorded_at >= occurred_at)
);

CREATE INDEX managed_user_adjustment_events_user_time_idx
    ON identity.managed_user_adjustment_events (user_id, occurred_at DESC, id DESC);

-- Traffic projections are adjusted explicitly in addition to the generic
-- receipt so current totals can be reconciled without inventing announces.
CREATE TABLE traffic.user_traffic_adjustments (
    adjustment_id uuid PRIMARY KEY
        REFERENCES identity.managed_user_adjustment_events (id) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    uploaded_delta bigint NOT NULL DEFAULT 0,
    downloaded_delta bigint NOT NULL DEFAULT 0,
    raw_uploaded_after bigint NOT NULL CHECK (raw_uploaded_after >= 0),
    raw_downloaded_after bigint NOT NULL CHECK (raw_downloaded_after >= 0),
    occurred_at timestamptz NOT NULL,
    CHECK (
        (uploaded_delta <> 0 AND downloaded_delta = 0)
        OR (uploaded_delta = 0 AND downloaded_delta <> 0)
    )
);

-- Native invitation issue/revoke events remain unchanged.  A staff balance
-- adjustment has no invitation token, but still gets a domain ledger row.
ALTER TABLE identity.invitation_balance_events
    DROP CONSTRAINT invitation_balance_events_event_kind_check,
    -- The baseline migration left this check unnamed, while this migration's
    -- Down path restores it with an explicit name. Accept both states so a
    -- verified Down -> Up cycle remains possible.
    DROP CONSTRAINT IF EXISTS invitation_balance_events_check,
    DROP CONSTRAINT IF EXISTS invitation_balance_events_delta_check,
    DROP CONSTRAINT invitation_balance_events_source_reference_check,
    ALTER COLUMN invitation_id DROP NOT NULL,
    ALTER COLUMN delta TYPE integer,
    ADD CONSTRAINT invitation_balance_events_event_kind_check
        CHECK (event_kind IN ('issued', 'revoked', 'staff_adjustment')),
    ADD CONSTRAINT invitation_balance_events_delta_check CHECK (
        (event_kind = 'issued' AND delta = -1)
        OR (event_kind = 'revoked' AND delta = 1)
        OR (event_kind = 'staff_adjustment' AND delta <> 0)
    ),
    ADD CONSTRAINT invitation_balance_events_source_reference_check CHECK (
        source_reference ~ '^member-invitation:[0-9a-f-]{36}:(issued|revoked)$'
        OR source_reference ~ '^staff-user-adjustment:[0-9a-f-]{36}$'
    ),
    ADD CONSTRAINT invitation_balance_events_staff_shape CHECK (
        (event_kind = 'staff_adjustment') = (invitation_id IS NULL)
    );

-- IP history stores one bounded aggregate per user/address, not request or
-- user-agent events.  Runtime code prunes each user to the newest 20 rows and
-- the maintenance worker removes rows not seen for 180 days.
CREATE TABLE identity.user_network_observations (
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    ip_address inet NOT NULL,
    first_seen_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    legacy_seen_count bigint NOT NULL DEFAULT 0 CHECK (legacy_seen_count >= 0),
    web_login_seen_count bigint NOT NULL DEFAULT 0 CHECK (web_login_seen_count >= 0),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, ip_address),
    CHECK (first_seen_at <= last_seen_at),
    CHECK (legacy_seen_count + web_login_seen_count > 0)
);

CREATE INDEX user_network_observations_user_recent_idx
    ON identity.user_network_observations (user_id, last_seen_at DESC, ip_address);
CREATE INDEX user_network_observations_address_recent_idx
    ON identity.user_network_observations (ip_address, last_seen_at DESC, user_id);
CREATE INDEX user_network_observations_retention_idx
    ON identity.user_network_observations (last_seen_at, user_id, ip_address);

-- One compact receipt proves the finite legacy import without duplicating the
-- retained network rows in another table.
CREATE TABLE migration.legacy_user_administration_imports (
    run_id uuid PRIMARY KEY REFERENCES migration.runs (id) ON DELETE RESTRICT,
    observed_at timestamptz NOT NULL,
    donation_source_rows bigint NOT NULL CHECK (donation_source_rows >= 0),
    positive_donation_users bigint NOT NULL CHECK (
        positive_donation_users BETWEEN 0 AND donation_source_rows
    ),
    donation_total numeric(20, 2) NOT NULL CHECK (donation_total >= 0),
    network_source_rows bigint NOT NULL CHECK (network_source_rows >= 0),
    retained_network_rows bigint NOT NULL CHECK (
        retained_network_rows BETWEEN 0 AND network_source_rows
    ),
    source_evidence_sha256 bytea NOT NULL CHECK (octet_length(source_evidence_sha256) = 32),
    imported_at timestamptz NOT NULL CHECK (imported_at >= observed_at)
);

CREATE TRIGGER user_donation_openings_immutable
BEFORE UPDATE OR DELETE ON migration.user_donation_openings
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TRIGGER managed_user_adjustment_events_immutable
BEFORE UPDATE OR DELETE ON identity.managed_user_adjustment_events
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TRIGGER user_traffic_adjustments_immutable
BEFORE UPDATE OR DELETE ON traffic.user_traffic_adjustments
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TRIGGER legacy_user_administration_imports_immutable
BEFORE UPDATE OR DELETE ON migration.legacy_user_administration_imports
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

REVOKE ALL ON identity.user_donation_totals FROM PUBLIC;
REVOKE ALL ON migration.user_donation_openings FROM PUBLIC;
REVOKE ALL ON identity.managed_user_adjustment_events FROM PUBLIC;
REVOKE ALL ON traffic.user_traffic_adjustments FROM PUBLIC;
REVOKE ALL ON identity.user_network_observations FROM PUBLIC;
REVOKE ALL ON migration.legacy_user_administration_imports FROM PUBLIC;

-- +goose Down

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM identity.managed_user_adjustment_events)
       OR EXISTS (SELECT 1 FROM migration.legacy_user_administration_imports)
       OR EXISTS (SELECT 1 FROM migration.user_donation_openings)
       OR EXISTS (SELECT 1 FROM identity.user_network_observations) THEN
        RAISE EXCEPTION '202608280001 cannot roll back after user administration data was written';
    END IF;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER legacy_user_administration_imports_immutable
    ON migration.legacy_user_administration_imports;
DROP TABLE migration.legacy_user_administration_imports;
DROP INDEX identity.user_network_observations_retention_idx;
DROP INDEX identity.user_network_observations_address_recent_idx;
DROP INDEX identity.user_network_observations_user_recent_idx;
DROP TABLE identity.user_network_observations;

ALTER TABLE identity.invitation_balance_events
    DROP CONSTRAINT invitation_balance_events_staff_shape,
    DROP CONSTRAINT invitation_balance_events_source_reference_check,
    DROP CONSTRAINT invitation_balance_events_delta_check,
    DROP CONSTRAINT invitation_balance_events_event_kind_check,
    ALTER COLUMN delta TYPE smallint,
    ALTER COLUMN invitation_id SET NOT NULL,
    ADD CONSTRAINT invitation_balance_events_event_kind_check
        CHECK (event_kind IN ('issued', 'revoked')),
    ADD CONSTRAINT invitation_balance_events_delta_check CHECK (
        (event_kind = 'issued' AND delta = -1)
        OR (event_kind = 'revoked' AND delta = 1)
    ),
    ADD CONSTRAINT invitation_balance_events_source_reference_check CHECK (
        source_reference ~ '^member-invitation:[0-9a-f-]{36}:(issued|revoked)$'
    );

DROP TRIGGER user_traffic_adjustments_immutable ON traffic.user_traffic_adjustments;
DROP TABLE traffic.user_traffic_adjustments;
DROP TRIGGER managed_user_adjustment_events_immutable
    ON identity.managed_user_adjustment_events;
DROP INDEX identity.managed_user_adjustment_events_user_time_idx;
DROP TABLE identity.managed_user_adjustment_events;
DROP TRIGGER user_donation_openings_immutable ON migration.user_donation_openings;
DROP TABLE migration.user_donation_openings;
DROP TABLE identity.user_donation_totals;

ALTER TABLE progression.experience_policy_revisions
    DISABLE TRIGGER progression_experience_policy_revisions_immutable;
DELETE FROM progression.experience_policy_revisions
WHERE revision = 'administrator-adjustment-v1';
ALTER TABLE progression.experience_policy_revisions
    ENABLE TRIGGER progression_experience_policy_revisions_immutable;
DELETE FROM economy.magic_accounts
WHERE id = '00000000-0000-7000-8000-000000000009';

DELETE FROM authz.role_permissions
WHERE action IN ('user.account.adjust', 'user.network.read');
DELETE FROM authz.permissions
WHERE action IN ('user.account.adjust', 'user.network.read');
