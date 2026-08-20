-- +goose Up

-- Legacy credentials and public identity are migrated by Privacy Vault. This
-- table is the separate, immutable accounting receipt for the non-secret user
-- state that Core owns. PeerGo has one spendable asset: integer magic points.
-- The old PtYes coin balance is converted once at the audited 5000:1 rate and
-- is never exposed as a second runtime currency.
ALTER TABLE migration.user_id_map
    ADD CONSTRAINT user_id_map_source_legacy_user_unique
    UNIQUE (source_system, legacy_user_id, user_id);

CREATE TABLE migration.user_operational_openings (
    source_system text NOT NULL CHECK (source_system = 'ptyes'),
    legacy_user_id bigint NOT NULL CHECK (legacy_user_id > 0),
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    source_uploaded bigint NOT NULL CHECK (source_uploaded >= 0),
    source_downloaded bigint NOT NULL CHECK (source_downloaded >= 0),
    source_karma numeric(38, 20) NOT NULL,
    source_pt_coin numeric(38, 20) NOT NULL,
    pt_coin_to_magic_rate bigint NOT NULL CHECK (pt_coin_to_magic_rate = 5000),
    exact_magic numeric(38, 20) GENERATED ALWAYS AS (
        source_karma + source_pt_coin * pt_coin_to_magic_rate
    ) STORED,
    magic_balance bigint NOT NULL,
    rounding_delta numeric(38, 20) GENERATED ALWAYS AS (
        magic_balance::numeric - (
            source_karma + source_pt_coin * pt_coin_to_magic_rate
        )
    ) STORED,
    source_experience numeric(38, 20) NOT NULL CHECK (source_experience >= 0),
    source_last_active_at timestamptz,
    source_fingerprint bytea NOT NULL CHECK (octet_length(source_fingerprint) = 32),
    first_run_id uuid NOT NULL REFERENCES migration.runs (id) ON DELETE RESTRICT,
    imported_at timestamptz NOT NULL,
    PRIMARY KEY (source_system, legacy_user_id),
    UNIQUE (user_id),
    FOREIGN KEY (source_system, legacy_user_id, user_id)
        REFERENCES migration.user_id_map (source_system, legacy_user_id, user_id)
        ON DELETE RESTRICT,
    CHECK (magic_balance = round(
        source_karma + source_pt_coin * pt_coin_to_magic_rate
    )::bigint)
);

CREATE TRIGGER migration_user_operational_openings_immutable
BEFORE UPDATE OR DELETE ON migration.user_operational_openings
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

-- Opening traffic is kept separate from announce settlements. It seeds the
-- aggregate once, while subsequent settlement entries can continue advancing
-- traffic.user_totals without ever rewriting the cutover evidence.
CREATE TABLE traffic.user_opening_balances (
    user_id uuid PRIMARY KEY REFERENCES identity.users (id) ON DELETE RESTRICT,
    raw_uploaded bigint NOT NULL CHECK (raw_uploaded >= 0),
    raw_downloaded bigint NOT NULL CHECK (raw_downloaded >= 0),
    credited_uploaded bigint NOT NULL CHECK (credited_uploaded >= 0),
    charged_downloaded bigint NOT NULL CHECK (charged_downloaded >= 0),
    source_run_id uuid NOT NULL REFERENCES migration.runs (id) ON DELETE RESTRICT,
    source_fingerprint bytea NOT NULL CHECK (octet_length(source_fingerprint) = 32),
    occurred_at timestamptz NOT NULL,
    imported_at timestamptz NOT NULL,
    CHECK (raw_uploaded = credited_uploaded),
    CHECK (raw_downloaded = charged_downloaded)
);

CREATE TRIGGER traffic_user_opening_balances_immutable
BEFORE UPDATE OR DELETE ON traffic.user_opening_balances
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE SCHEMA economy;

-- This is the only mutable balance projection. All asset movements are integer
-- magic points and must be backed by an immutable ledger row.
CREATE TABLE economy.magic_accounts (
    user_id uuid PRIMARY KEY REFERENCES identity.users (id) ON DELETE RESTRICT,
    balance bigint NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL
);

CREATE TABLE economy.magic_ledger_entries (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    entry_type text NOT NULL CHECK (
        entry_type IN ('legacy_opening', 'earn', 'spend', 'adjustment')
    ),
    amount bigint NOT NULL,
    balance_after bigint NOT NULL,
    source_reference text NOT NULL CHECK (
        source_reference ~ '^[a-z0-9][a-z0-9:._-]{0,127}$'
    ),
    source_run_id uuid REFERENCES migration.runs (id) ON DELETE RESTRICT,
    occurred_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL,
    UNIQUE (entry_type, source_reference),
    CHECK (entry_type <> 'legacy_opening' OR source_run_id IS NOT NULL)
);

CREATE INDEX magic_ledger_entries_user_time_idx
    ON economy.magic_ledger_entries (user_id, occurred_at DESC, id DESC);

CREATE TRIGGER economy_magic_ledger_entries_immutable
BEFORE UPDATE OR DELETE ON economy.magic_ledger_entries
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE SCHEMA progression;

-- Rousi's level thresholds are copied as an explicit versioned policy instead
-- of being buried in importer code. Levels are human-facing and start at 1.
CREATE TABLE progression.level_definitions (
    policy_version text NOT NULL CHECK (
        policy_version ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
    ),
    level smallint NOT NULL CHECK (level BETWEEN 1 AND 99),
    minimum_experience numeric(38, 20) NOT NULL CHECK (minimum_experience >= 0),
    PRIMARY KEY (policy_version, level),
    UNIQUE (policy_version, minimum_experience)
);

INSERT INTO progression.level_definitions (
    policy_version, level, minimum_experience
) VALUES
    ('rousi-v1', 1, 0),
    ('rousi-v1', 2, 1000),
    ('rousi-v1', 3, 5000),
    ('rousi-v1', 4, 15000),
    ('rousi-v1', 5, 40000),
    ('rousi-v1', 6, 100000),
    ('rousi-v1', 7, 250000),
    ('rousi-v1', 8, 600000),
    ('rousi-v1', 9, 1200000);

CREATE TRIGGER progression_level_definitions_immutable
BEFORE UPDATE OR DELETE ON progression.level_definitions
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TABLE progression.user_progress (
    user_id uuid PRIMARY KEY REFERENCES identity.users (id) ON DELETE RESTRICT,
    experience numeric(38, 20) NOT NULL CHECK (experience >= 0),
    level smallint NOT NULL,
    policy_version text NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL,
    FOREIGN KEY (policy_version, level)
        REFERENCES progression.level_definitions (policy_version, level)
        ON DELETE RESTRICT
);

-- Activity is a current projection and may advance after cutover. A missing
-- legacy timestamp remains NULL instead of inventing activity at registration.
CREATE TABLE identity.user_activity (
    user_id uuid PRIMARY KEY REFERENCES identity.users (id) ON DELETE RESTRICT,
    last_active_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL
);

REVOKE ALL ON SCHEMA economy FROM PUBLIC;
REVOKE ALL ON SCHEMA progression FROM PUBLIC;
REVOKE ALL ON ALL TABLES IN SCHEMA economy FROM PUBLIC;
REVOKE ALL ON ALL TABLES IN SCHEMA progression FROM PUBLIC;

-- +goose Down

DROP TABLE identity.user_activity;
DROP TABLE progression.user_progress;
DROP TRIGGER progression_level_definitions_immutable ON progression.level_definitions;
DROP TABLE progression.level_definitions;
DROP SCHEMA progression;
DROP TRIGGER economy_magic_ledger_entries_immutable ON economy.magic_ledger_entries;
DROP TABLE economy.magic_ledger_entries;
DROP TABLE economy.magic_accounts;
DROP SCHEMA economy;
DROP TRIGGER traffic_user_opening_balances_immutable ON traffic.user_opening_balances;
DROP TABLE traffic.user_opening_balances;
DROP TRIGGER migration_user_operational_openings_immutable
    ON migration.user_operational_openings;
DROP TABLE migration.user_operational_openings;
ALTER TABLE migration.user_id_map
    DROP CONSTRAINT user_id_map_source_legacy_user_unique;
