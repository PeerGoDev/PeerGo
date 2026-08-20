-- +goose Up

-- Older runtime revisions predate an explicit box download multiplier. They
-- keep neutral 1x semantics in the codec; the finite Rousi importer appends a
-- new revision instead of rewriting any historical policy row.
ALTER TABLE tracker_control.runtime_policy_revisions
    DROP CONSTRAINT runtime_policy_revisions_seedbox_policy_check;

ALTER TABLE tracker_control.runtime_policy_revisions
    ADD CONSTRAINT runtime_policy_revisions_seedbox_policy_check CHECK (
        jsonb_typeof(seedbox_policy) = 'object'
        AND jsonb_typeof(seedbox_policy -> 'enabled') = 'boolean'
        AND jsonb_typeof(seedbox_policy -> 'upload_factor_basis_points') = 'number'
        AND (seedbox_policy ->> 'upload_factor_basis_points')::integer BETWEEN 0 AND 10000
        AND (
            NOT (seedbox_policy ? 'download_factor_basis_points')
            OR (
                jsonb_typeof(seedbox_policy -> 'download_factor_basis_points') = 'number'
                AND (seedbox_policy ->> 'download_factor_basis_points')::integer BETWEEN 10000 AND 100000
            )
        )
        AND jsonb_typeof(seedbox_policy -> 'seedbox_speed_limit_bytes_per_second') = 'number'
        AND (seedbox_policy ->> 'seedbox_speed_limit_bytes_per_second')::bigint >= 0
        AND jsonb_typeof(seedbox_policy -> 'standard_speed_limit_bytes_per_second') = 'number'
        AND (seedbox_policy ->> 'standard_speed_limit_bytes_per_second')::bigint >= 0
        AND jsonb_typeof(seedbox_policy -> 'rules') = 'array'
        AND jsonb_array_length(seedbox_policy -> 'rules') <= 4096
    );

-- This run-scoped receipt binds the active rule set back to the immutable SQL
-- snapshot. It stores aggregate evidence only; individual addresses live in
-- the binding table because Tracker needs those exact reviewed prefixes.
CREATE TABLE migration.legacy_seedbox_imports (
    run_id uuid PRIMARY KEY REFERENCES migration.runs (id) ON DELETE RESTRICT,
    source_snapshot_sha256 bytea NOT NULL CHECK (octet_length(source_snapshot_sha256) = 32),
    source_rows bigint NOT NULL CHECK (source_rows >= 0),
    enabled_rows bigint NOT NULL CHECK (enabled_rows >= 0 AND enabled_rows <= source_rows),
    binding_rows bigint NOT NULL CHECK (binding_rows >= enabled_rows),
    source_evidence_sha256 bytea NOT NULL CHECK (octet_length(source_evidence_sha256) = 32),
    policy_sequence bigint NOT NULL UNIQUE
        REFERENCES tracker_control.runtime_policy_revisions (sequence) ON DELETE RESTRICT,
    policy_revision text NOT NULL UNIQUE CHECK (policy_revision ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    upload_factor_basis_points integer NOT NULL CHECK (upload_factor_basis_points BETWEEN 0 AND 10000),
    download_factor_basis_points integer NOT NULL CHECK (download_factor_basis_points BETWEEN 10000 AND 100000),
    seedbox_speed_limit_bytes_per_second bigint NOT NULL
        CHECK (seedbox_speed_limit_bytes_per_second = 0),
    standard_speed_limit_bytes_per_second bigint NOT NULL
        CHECK (standard_speed_limit_bytes_per_second > 0),
    imported_at timestamptz NOT NULL
);

CREATE TABLE migration.legacy_seedbox_bindings (
    run_id uuid NOT NULL REFERENCES migration.legacy_seedbox_imports (run_id) ON DELETE RESTRICT,
    legacy_seedbox_id bigint NOT NULL CHECK (legacy_seedbox_id > 0),
    binding_kind text NOT NULL CHECK (binding_kind IN ('ip', 'cidr')),
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    user_numeric_id bigint NOT NULL CHECK (user_numeric_id > 0),
    network cidr NOT NULL,
    rule_id text NOT NULL CHECK (rule_id ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    source_fingerprint bytea NOT NULL CHECK (octet_length(source_fingerprint) = 32),
    policy_sequence bigint NOT NULL
        REFERENCES tracker_control.runtime_policy_revisions (sequence) ON DELETE RESTRICT,
    imported_at timestamptz NOT NULL,
    PRIMARY KEY (run_id, legacy_seedbox_id, binding_kind),
    UNIQUE (run_id, user_numeric_id, network),
    UNIQUE (run_id, rule_id)
);

CREATE INDEX legacy_seedbox_bindings_user_idx
    ON migration.legacy_seedbox_bindings (user_id, network);

CREATE TRIGGER legacy_seedbox_imports_immutable
BEFORE UPDATE OR DELETE ON migration.legacy_seedbox_imports
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TRIGGER legacy_seedbox_bindings_immutable
BEFORE UPDATE OR DELETE ON migration.legacy_seedbox_bindings
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

REVOKE ALL ON migration.legacy_seedbox_imports FROM PUBLIC;
REVOKE ALL ON migration.legacy_seedbox_bindings FROM PUBLIC;

-- +goose Down

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM migration.legacy_seedbox_imports) THEN
        RAISE EXCEPTION '202608210001 cannot roll back after legacy seedboxes were imported';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM tracker_control.runtime_policy_revisions
        WHERE seedbox_policy ? 'download_factor_basis_points'
    ) THEN
        RAISE EXCEPTION '202608210001 cannot roll back after a new runtime policy was appended';
    END IF;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER legacy_seedbox_bindings_immutable ON migration.legacy_seedbox_bindings;
DROP TRIGGER legacy_seedbox_imports_immutable ON migration.legacy_seedbox_imports;
DROP INDEX migration.legacy_seedbox_bindings_user_idx;
DROP TABLE migration.legacy_seedbox_bindings;
DROP TABLE migration.legacy_seedbox_imports;

ALTER TABLE tracker_control.runtime_policy_revisions
    DROP CONSTRAINT runtime_policy_revisions_seedbox_policy_check;

ALTER TABLE tracker_control.runtime_policy_revisions
    ADD CONSTRAINT runtime_policy_revisions_seedbox_policy_check CHECK (
        jsonb_typeof(seedbox_policy) = 'object'
        AND jsonb_typeof(seedbox_policy -> 'enabled') = 'boolean'
        AND jsonb_typeof(seedbox_policy -> 'upload_factor_basis_points') = 'number'
        AND (seedbox_policy ->> 'upload_factor_basis_points')::integer BETWEEN 0 AND 10000
        AND jsonb_typeof(seedbox_policy -> 'seedbox_speed_limit_bytes_per_second') = 'number'
        AND (seedbox_policy ->> 'seedbox_speed_limit_bytes_per_second')::bigint >= 0
        AND jsonb_typeof(seedbox_policy -> 'standard_speed_limit_bytes_per_second') = 'number'
        AND (seedbox_policy ->> 'standard_speed_limit_bytes_per_second')::bigint >= 0
        AND jsonb_typeof(seedbox_policy -> 'rules') = 'array'
        AND jsonb_array_length(seedbox_policy -> 'rules') <= 4096
    );
