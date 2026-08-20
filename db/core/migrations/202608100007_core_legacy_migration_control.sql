-- +goose Up

-- Legacy import is an operator-only, finite cutover workflow. This schema keeps
-- replay/idempotency evidence and stable ID allocations, but deliberately does
-- not persist source rows, usernames, email addresses, password hashes,
-- passkeys, descriptions, or file paths. Sensitive source rows are represented
-- only by a keyed fingerprint generated outside Core.
CREATE SCHEMA migration;

REVOKE ALL ON SCHEMA migration FROM PUBLIC;

CREATE TABLE migration.runs (
    id uuid PRIMARY KEY,
    source_system text NOT NULL CHECK (source_system = 'ptyes'),
    source_snapshot_sha256 bytea NOT NULL
        CHECK (octet_length(source_snapshot_sha256) = 32),
    mapping_version text NOT NULL
        CHECK (mapping_version ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    state text NOT NULL DEFAULT 'planned' CHECK (
        state IN ('planned', 'validated', 'importing', 'imported', 'reconciled', 'failed')
    ),
    expected_user_rows bigint NOT NULL CHECK (expected_user_rows >= 0),
    expected_torrent_rows bigint NOT NULL CHECK (expected_torrent_rows >= 0),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL,
    state_changed_at timestamptz NOT NULL,
    completed_at timestamptz,
    UNIQUE (source_system, source_snapshot_sha256, mapping_version),
    CHECK (state_changed_at >= created_at),
    CHECK (
        (state IN ('reconciled', 'failed')
            AND completed_at IS NOT NULL
            AND completed_at = state_changed_at)
        OR (state NOT IN ('reconciled', 'failed') AND completed_at IS NULL)
    )
);

-- Artifacts describe the immutable inputs without retaining a host path or an
-- object-store credential. The import command receives actual locations as
-- ephemeral configuration and proves that it opened the expected bytes here.
CREATE TABLE migration.run_artifacts (
    id uuid PRIMARY KEY,
    run_id uuid NOT NULL
        REFERENCES migration.runs (id) ON DELETE RESTRICT,
    kind text NOT NULL CHECK (
        kind IN ('database_dump', 'torrent_manifest', 'image_manifest')
    ),
    content_sha256 bytea NOT NULL CHECK (octet_length(content_sha256) = 32),
    byte_length bigint NOT NULL CHECK (byte_length >= 0),
    item_count bigint NOT NULL CHECK (item_count >= 0),
    created_at timestamptz NOT NULL,
    UNIQUE (run_id, kind, content_sha256)
);

-- One row per source entity is the resumability checkpoint. source_fingerprint
-- is HMAC-SHA-256 for users, so an offline reader cannot enumerate known email
-- addresses or password hashes. Torrent/asset rows may use a plain SHA-256 over
-- their canonical non-secret export representation.
CREATE TABLE migration.source_rows (
    run_id uuid NOT NULL
        REFERENCES migration.runs (id) ON DELETE RESTRICT,
    entity_kind text NOT NULL CHECK (
        entity_kind IN ('user', 'torrent', 'torrent_object', 'torrent_image')
    ),
    legacy_id bigint NOT NULL CHECK (legacy_id > 0),
    source_fingerprint bytea NOT NULL
        CHECK (octet_length(source_fingerprint) = 32),
    fingerprint_scheme text NOT NULL CHECK (
        fingerprint_scheme IN ('hmac-sha256-v1', 'sha256-v1')
    ),
    state text NOT NULL DEFAULT 'discovered' CHECK (
        state IN ('discovered', 'validated', 'discrepancy', 'imported', 'skipped')
    ),
    attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    error_code text CHECK (
        error_code IS NULL OR error_code ~ '^[a-z0-9][a-z0-9._-]{0,95}$'
    ),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (run_id, entity_kind, legacy_id),
    CHECK (updated_at >= created_at),
    CHECK (entity_kind <> 'user' OR fingerprint_scheme = 'hmac-sha256-v1'),
    CHECK (state <> 'imported' OR error_code IS NULL),
    CHECK (state NOT IN ('discrepancy', 'skipped') OR error_code IS NOT NULL)
);

CREATE INDEX source_rows_pending_idx
    ON migration.source_rows (run_id, entity_kind, state, legacy_id)
    WHERE state NOT IN ('imported', 'skipped');

-- Target IDs are allocated before writes and remain stable across every retry
-- and rehearsal of the same source system. There are intentionally no foreign
-- keys to live aggregates: allocation precedes insertion and is itself the
-- recovery source if an import process stops between databases.
CREATE TABLE migration.user_id_map (
    source_system text NOT NULL CHECK (source_system = 'ptyes'),
    legacy_user_id bigint NOT NULL CHECK (legacy_user_id > 0),
    user_id uuid NOT NULL UNIQUE,
    credential_ref uuid NOT NULL UNIQUE,
    first_run_id uuid NOT NULL
        REFERENCES migration.runs (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (source_system, legacy_user_id)
);

CREATE TABLE migration.torrent_id_map (
    source_system text NOT NULL CHECK (source_system = 'ptyes'),
    legacy_torrent_id bigint NOT NULL CHECK (legacy_torrent_id > 0),
    torrent_id bigint NOT NULL UNIQUE CHECK (torrent_id > 0),
    public_id uuid NOT NULL UNIQUE,
    object_id uuid NOT NULL UNIQUE,
    first_run_id uuid NOT NULL
        REFERENCES migration.runs (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (source_system, legacy_torrent_id),
    -- PtYes numeric torrent IDs are part of stable historical links. A clean
    -- PeerGo cutover reserves those values rather than silently renumbering.
    CHECK (torrent_id = legacy_torrent_id)
);

-- A discrepancy stores a machine-readable code and non-reversible evidence,
-- never a raw offending value. expected/actual_count are only for aggregate or
-- file-tree checks and stay NULL when a count would not explain the problem.
CREATE TABLE migration.discrepancies (
    id bigint GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY CHECK (id > 0),
    run_id uuid NOT NULL,
    entity_kind text NOT NULL,
    legacy_id bigint NOT NULL,
    code text NOT NULL
        CHECK (code ~ '^[a-z0-9][a-z0-9._-]{0,95}$'),
    severity text NOT NULL CHECK (severity IN ('warning', 'error', 'blocking')),
    evidence_sha256 bytea NOT NULL CHECK (octet_length(evidence_sha256) = 32),
    expected_count bigint CHECK (expected_count IS NULL OR expected_count >= 0),
    actual_count bigint CHECK (actual_count IS NULL OR actual_count >= 0),
    detected_at timestamptz NOT NULL,
    FOREIGN KEY (run_id, entity_kind, legacy_id)
        REFERENCES migration.source_rows (run_id, entity_kind, legacy_id)
        ON DELETE RESTRICT,
    UNIQUE (run_id, entity_kind, legacy_id, code, evidence_sha256),
    CHECK (
        (expected_count IS NULL AND actual_count IS NULL)
        OR (expected_count IS NOT NULL AND actual_count IS NOT NULL)
    )
);

CREATE INDEX discrepancies_run_severity_idx
    ON migration.discrepancies (run_id, severity, entity_kind, legacy_id);

-- Resolution is append-only and auditable. resolution_sha256 commits to the
-- reviewed rule/evidence bundle held by the finite migration tooling; it is not
-- a place to copy a raw source value or free-form operator note.
CREATE TABLE migration.discrepancy_resolutions (
    discrepancy_id bigint PRIMARY KEY
        REFERENCES migration.discrepancies (id) ON DELETE RESTRICT,
    outcome text NOT NULL CHECK (
        outcome IN ('accepted', 'mapped', 'source_fixed', 'skipped')
    ),
    rule_version text NOT NULL
        CHECK (rule_version ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    resolution_sha256 bytea NOT NULL
        CHECK (octet_length(resolution_sha256) = 32),
    resolved_by uuid NOT NULL,
    resolved_at timestamptz NOT NULL
);

-- +goose StatementBegin
CREATE FUNCTION migration.protect_run_transition()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'legacy migration runs cannot be deleted';
    END IF;

    IF OLD.id IS DISTINCT FROM NEW.id
        OR OLD.source_system IS DISTINCT FROM NEW.source_system
        OR OLD.source_snapshot_sha256 IS DISTINCT FROM NEW.source_snapshot_sha256
        OR OLD.mapping_version IS DISTINCT FROM NEW.mapping_version
        OR OLD.expected_user_rows IS DISTINCT FROM NEW.expected_user_rows
        OR OLD.expected_torrent_rows IS DISTINCT FROM NEW.expected_torrent_rows
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'legacy migration run identity is immutable';
    END IF;
    IF NEW.version <> OLD.version + 1 OR NEW.state_changed_at < OLD.state_changed_at THEN
        RAISE EXCEPTION 'legacy migration run version and time must advance';
    END IF;
    IF NOT (
        (OLD.state = 'planned' AND NEW.state IN ('validated', 'failed'))
        OR (OLD.state = 'validated' AND NEW.state IN ('importing', 'failed'))
        OR (OLD.state = 'importing' AND NEW.state IN ('imported', 'failed'))
        OR (OLD.state = 'imported' AND NEW.state IN ('reconciled', 'failed'))
    ) THEN
        RAISE EXCEPTION 'invalid legacy migration run transition from % to %',
            OLD.state, NEW.state;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER migration_runs_protected
BEFORE UPDATE OR DELETE ON migration.runs
FOR EACH ROW EXECUTE FUNCTION migration.protect_run_transition();

-- +goose StatementBegin
CREATE FUNCTION migration.protect_source_row_transition()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'legacy source checkpoints cannot be deleted';
    END IF;

    IF OLD.run_id IS DISTINCT FROM NEW.run_id
        OR OLD.entity_kind IS DISTINCT FROM NEW.entity_kind
        OR OLD.legacy_id IS DISTINCT FROM NEW.legacy_id
        OR OLD.source_fingerprint IS DISTINCT FROM NEW.source_fingerprint
        OR OLD.fingerprint_scheme IS DISTINCT FROM NEW.fingerprint_scheme
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'legacy source checkpoint identity is immutable';
    END IF;
    IF NEW.version <> OLD.version + 1
        OR NEW.attempt_count <> OLD.attempt_count + 1
        OR NEW.updated_at < OLD.updated_at THEN
        RAISE EXCEPTION 'legacy source checkpoint must advance exactly once';
    END IF;
    IF OLD.state IN ('imported', 'skipped') THEN
        RAISE EXCEPTION 'legacy source checkpoint terminal state cannot change';
    END IF;

    IF OLD.state = NEW.state THEN
        IF OLD.state NOT IN ('discovered', 'validated', 'discrepancy') THEN
            RAISE EXCEPTION 'invalid legacy source checkpoint retry';
        END IF;
    ELSIF NOT (
        (OLD.state = 'discovered' AND NEW.state IN ('validated', 'discrepancy', 'skipped'))
        OR (OLD.state = 'validated' AND NEW.state IN ('imported', 'discrepancy', 'skipped'))
        OR (OLD.state = 'discrepancy' AND NEW.state IN ('validated', 'skipped'))
    ) THEN
        RAISE EXCEPTION 'invalid legacy source transition from % to %',
            OLD.state, NEW.state;
    END IF;

    IF OLD.state = 'discrepancy' AND NEW.state <> 'discrepancy'
        AND EXISTS (
            SELECT 1
            FROM migration.discrepancies AS problem
            LEFT JOIN migration.discrepancy_resolutions AS resolution
                ON resolution.discrepancy_id = problem.id
            WHERE problem.run_id = OLD.run_id
              AND problem.entity_kind = OLD.entity_kind
              AND problem.legacy_id = OLD.legacy_id
              AND resolution.discrepancy_id IS NULL
        ) THEN
        RAISE EXCEPTION 'all legacy source discrepancies must be resolved first';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER migration_source_rows_protected
BEFORE UPDATE OR DELETE ON migration.source_rows
FOR EACH ROW EXECUTE FUNCTION migration.protect_source_row_transition();

-- +goose StatementBegin
CREATE FUNCTION migration.reject_append_only_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER migration_run_artifacts_immutable
BEFORE UPDATE OR DELETE ON migration.run_artifacts
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TRIGGER migration_user_id_map_immutable
BEFORE UPDATE OR DELETE ON migration.user_id_map
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TRIGGER migration_torrent_id_map_immutable
BEFORE UPDATE OR DELETE ON migration.torrent_id_map
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TRIGGER migration_discrepancies_immutable
BEFORE UPDATE OR DELETE ON migration.discrepancies
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TRIGGER migration_discrepancy_resolutions_immutable
BEFORE UPDATE OR DELETE ON migration.discrepancy_resolutions
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

REVOKE ALL ON ALL TABLES IN SCHEMA migration FROM PUBLIC;
REVOKE ALL ON ALL SEQUENCES IN SCHEMA migration FROM PUBLIC;
REVOKE ALL ON ALL FUNCTIONS IN SCHEMA migration FROM PUBLIC;

-- +goose Down

DROP SCHEMA IF EXISTS migration CASCADE;
