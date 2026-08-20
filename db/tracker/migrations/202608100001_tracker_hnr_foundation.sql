-- +goose Up

-- completion_id is the stable Swarm transition identity, not the announce
-- event UUID. Existing intervals predate this contract and deliberately stay
-- at evidence version 0: they remain valid traffic evidence but are never
-- reinterpreted as H&R facts. New rows use version 1 by default and must carry
-- an identity exactly when they contain a trustworthy left > 0 -> 0 interval.
ALTER TABLE ledger.raw_session_intervals
    ADD COLUMN completion_id bytea CHECK (completion_id IS NULL OR octet_length(completion_id) = 32),
    ADD COLUMN completion_identity_version smallint NOT NULL DEFAULT 0 CHECK (
        completion_identity_version IN (0, 1)
    );

ALTER TABLE ledger.raw_session_intervals
    ALTER COLUMN completion_identity_version SET DEFAULT 1;

ALTER TABLE ledger.raw_session_intervals
    ADD CONSTRAINT raw_interval_completion_identity CHECK (
        (completion_identity_version = 0 AND completion_id IS NULL)
        OR
        (completion_identity_version = 1 AND completed_transition = (completion_id IS NOT NULL))
    );

-- H&R policy is intentionally separate from promotion policy. A free torrent
-- is exempt only when an explicit H&R revision says so.
CREATE TABLE settlement.hnr_policy_timeline_revisions (
    id uuid PRIMARY KEY,
    scope_user_id uuid,
    scope_torrent_id bigint CHECK (scope_torrent_id IS NULL OR scope_torrent_id > 0),
    scope_torrent_control_sequence bigint CHECK (
        scope_torrent_control_sequence IS NULL OR scope_torrent_control_sequence > 0
    ),
    scope_subject_control_sequence bigint CHECK (
        scope_subject_control_sequence IS NULL OR scope_subject_control_sequence > 0
    ),
    effective_at timestamptz NOT NULL,
    rule_id text NOT NULL CHECK (char_length(rule_id) BETWEEN 1 AND 128 AND rule_id = btrim(rule_id)),
    rule_version bigint NOT NULL CHECK (rule_version > 0),
    mode text NOT NULL CHECK (mode IN ('disabled', 'exempt', 'enforced')),
    required_seed_seconds bigint NOT NULL CHECK (required_seed_seconds >= 0),
    required_ratio_basis_points bigint NOT NULL CHECK (
        required_ratio_basis_points BETWEEN 0 AND 1000000
    ),
    assessment_window_seconds bigint NOT NULL CHECK (
        assessment_window_seconds BETWEEN 0 AND 315360000
    ),
    grace_period_seconds bigint NOT NULL CHECK (
        grace_period_seconds BETWEEN 0 AND 31536000
    ),
    max_interval_credit_seconds bigint NOT NULL CHECK (max_interval_credit_seconds >= 0),
    policy_json text NOT NULL CHECK (
        octet_length(policy_json) BETWEEN 3 AND 4096
        AND jsonb_typeof(policy_json::jsonb) = 'object'
    ),
    policy_sha256 bytea NOT NULL CHECK (octet_length(policy_sha256) = 32),
    recorded_at timestamptz NOT NULL,
    UNIQUE NULLS NOT DISTINCT (
        scope_user_id,
        scope_torrent_id,
        scope_torrent_control_sequence,
        scope_subject_control_sequence,
        effective_at
    ),
    CHECK (
        (mode IN ('disabled', 'exempt')
            AND required_seed_seconds = 0
            AND required_ratio_basis_points = 0
            AND assessment_window_seconds = 0
            AND grace_period_seconds = 0
            AND max_interval_credit_seconds = 0)
        OR
        (mode = 'enforced'
            AND (required_seed_seconds > 0 OR required_ratio_basis_points > 0)
            AND assessment_window_seconds >= required_seed_seconds
            AND assessment_window_seconds > 0
            AND max_interval_credit_seconds BETWEEN 60 AND 86400)
    )
);

CREATE INDEX hnr_policy_timeline_resolution_idx
    ON settlement.hnr_policy_timeline_revisions (effective_at, id);

CREATE TABLE settlement.hnr_work (
    interval_event_id uuid PRIMARY KEY
        REFERENCES ledger.raw_session_intervals (event_id) ON DELETE RESTRICT,
    available_at timestamptz NOT NULL,
    lease_token uuid,
    lease_until timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error_code text CHECK (
        last_error_code IS NULL OR last_error_code ~ '^[a-z][a-z0-9_]{0,63}$'
    ),
    processed_at timestamptz,
    created_at timestamptz NOT NULL,
    CHECK ((lease_token IS NULL) = (lease_until IS NULL)),
    CHECK (processed_at IS NULL OR (lease_token IS NULL AND last_error_code IS NULL))
);

CREATE INDEX hnr_work_ready_idx
    ON settlement.hnr_work (available_at, interval_event_id)
    WHERE processed_at IS NULL;

-- Every trustworthy completion receives exactly one immutable assessment,
-- including completions resolved under `disabled`. This audit row prevents a
-- later historical policy append from silently turning an already-processed
-- completion into a new obligation.
CREATE TABLE ledger.hnr_completion_assessments (
    id uuid PRIMARY KEY,
    completion_id bytea NOT NULL UNIQUE CHECK (octet_length(completion_id) = 32),
    completion_event_id uuid NOT NULL UNIQUE
        REFERENCES ledger.raw_session_intervals (event_id) ON DELETE RESTRICT,
    user_id uuid NOT NULL,
    torrent_id bigint NOT NULL CHECK (torrent_id > 0),
    torrent_control_sequence bigint NOT NULL CHECK (torrent_control_sequence > 0),
    subject_control_sequence bigint NOT NULL CHECK (subject_control_sequence > 0),
    completed_at timestamptz NOT NULL,
    policy_revision_id uuid NOT NULL
        REFERENCES settlement.hnr_policy_timeline_revisions (id) ON DELETE RESTRICT,
    policy_rule_id text NOT NULL CHECK (
        char_length(policy_rule_id) BETWEEN 1 AND 128 AND policy_rule_id = btrim(policy_rule_id)
    ),
    policy_rule_version bigint NOT NULL CHECK (policy_rule_version > 0),
    policy_sha256 bytea NOT NULL CHECK (octet_length(policy_sha256) = 32),
    policy_mode text NOT NULL CHECK (policy_mode IN ('disabled', 'exempt', 'enforced')),
    required_seed_seconds bigint NOT NULL CHECK (required_seed_seconds >= 0),
    required_ratio_basis_points bigint NOT NULL CHECK (
        required_ratio_basis_points BETWEEN 0 AND 1000000
    ),
    max_interval_credit_seconds bigint NOT NULL CHECK (max_interval_credit_seconds >= 0),
    assessment_due_at timestamptz NOT NULL,
    grace_ends_at timestamptz NOT NULL,
    initial_uploaded bigint NOT NULL CHECK (initial_uploaded >= 0),
    raw_downloaded bigint NOT NULL CHECK (raw_downloaded >= 0),
    decided_at timestamptz NOT NULL CHECK (decided_at >= completed_at),
    CHECK (assessment_due_at >= completed_at),
    CHECK (grace_ends_at >= assessment_due_at),
    CHECK (
        (policy_mode IN ('disabled', 'exempt')
            AND required_seed_seconds = 0
            AND required_ratio_basis_points = 0
            AND max_interval_credit_seconds = 0
            AND assessment_due_at = completed_at
            AND grace_ends_at = completed_at)
        OR
        (policy_mode = 'enforced'
            AND (required_seed_seconds > 0 OR required_ratio_basis_points > 0)
            AND max_interval_credit_seconds BETWEEN 60 AND 86400)
    )
);

CREATE INDEX hnr_completion_assessments_user_torrent_idx
    ON ledger.hnr_completion_assessments (user_id, torrent_id, completed_at, id);

-- Only explicit exempt/enforced assessments create a user-visible
-- obligation. Policy identity and thresholds stay normalized in the parent
-- assessment; this row contains only monotonic progress.
CREATE TABLE ledger.hnr_obligations (
    id uuid PRIMARY KEY,
    assessment_id uuid NOT NULL UNIQUE
        REFERENCES ledger.hnr_completion_assessments (id) ON DELETE RESTRICT,
    seeded_seconds bigint NOT NULL DEFAULT 0 CHECK (seeded_seconds >= 0),
    raw_uploaded bigint NOT NULL CHECK (raw_uploaded >= 0),
    raw_ratio_basis_points bigint NOT NULL DEFAULT 0 CHECK (raw_ratio_basis_points >= 0),
    state text NOT NULL CHECK (state IN ('tracking', 'satisfied', 'exempt')),
    satisfied_by text CHECK (satisfied_by IS NULL OR satisfied_by IN ('seed_time', 'raw_ratio', 'exempt')),
    satisfied_at timestamptz,
    version bigint NOT NULL CHECK (version > 0),
    last_evidence_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (updated_at >= created_at),
    CHECK (
        (state = 'tracking' AND satisfied_by IS NULL AND satisfied_at IS NULL)
        OR (state = 'satisfied'
            AND satisfied_by IN ('seed_time', 'raw_ratio') AND satisfied_at IS NOT NULL)
        OR (state = 'exempt' AND satisfied_by = 'exempt' AND satisfied_at IS NOT NULL)
    )
);

CREATE INDEX hnr_obligations_user_torrent_active_idx
    ON ledger.hnr_obligations (assessment_id, id)
    WHERE state = 'tracking';

CREATE TABLE settlement.hnr_outbox (
    event_id uuid PRIMARY KEY,
    obligation_id uuid NOT NULL REFERENCES ledger.hnr_obligations (id) ON DELETE RESTRICT,
    obligation_version bigint NOT NULL CHECK (obligation_version > 0),
    event_type text NOT NULL CHECK (event_type = 'settlement.hnr.updated'),
    schema_version text NOT NULL CHECK (schema_version = 'settlement.hnr.v1'),
    occurred_at timestamptz NOT NULL,
    payload_json text NOT NULL CHECK (
        octet_length(payload_json) BETWEEN 2 AND 8192
        AND jsonb_typeof(payload_json::jsonb) = 'object'
    ),
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    available_at timestamptz NOT NULL,
    lease_token uuid,
    lease_until timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error_code text CHECK (
        last_error_code IS NULL OR last_error_code ~ '^[a-z][a-z0-9_]{0,63}$'
    ),
    published_at timestamptz,
    created_at timestamptz NOT NULL,
    UNIQUE (obligation_id, obligation_version),
    CHECK ((lease_token IS NULL) = (lease_until IS NULL)),
    CHECK (published_at IS NULL OR (lease_token IS NULL AND last_error_code IS NULL))
);

CREATE INDEX hnr_outbox_ready_idx
    ON settlement.hnr_outbox (available_at, event_id)
    WHERE published_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION settlement.enqueue_hnr_work()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO settlement.hnr_work (interval_event_id, available_at, created_at)
    VALUES (NEW.event_id, NEW.created_at, NEW.created_at)
    ON CONFLICT (interval_event_id) DO NOTHING;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER settlement_raw_interval_hnr_work
AFTER INSERT ON ledger.raw_session_intervals
FOR EACH ROW EXECUTE FUNCTION settlement.enqueue_hnr_work();

-- +goose StatementBegin
CREATE FUNCTION settlement.protect_hnr_policy_revision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'H&R policy revisions are immutable';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM ledger.hnr_completion_assessments AS assessment
        WHERE assessment.completed_at >= NEW.effective_at
          AND (NEW.scope_user_id IS NULL OR NEW.scope_user_id = assessment.user_id)
          AND (NEW.scope_torrent_id IS NULL OR NEW.scope_torrent_id = assessment.torrent_id)
          AND (
              NEW.scope_torrent_control_sequence IS NULL
              OR NEW.scope_torrent_control_sequence = assessment.torrent_control_sequence
          )
          AND (
              NEW.scope_subject_control_sequence IS NULL
              OR NEW.scope_subject_control_sequence = assessment.subject_control_sequence
          )
    ) THEN
        RAISE EXCEPTION 'H&R policy revision would reinterpret an existing completion assessment';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER hnr_policy_timeline_immutable
BEFORE INSERT OR UPDATE OR DELETE ON settlement.hnr_policy_timeline_revisions
FOR EACH ROW EXECUTE FUNCTION settlement.protect_hnr_policy_revision();

-- +goose StatementBegin
CREATE FUNCTION ledger.protect_hnr_completion_assessment()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'H&R completion assessments are immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER hnr_completion_assessment_immutable
BEFORE UPDATE OR DELETE ON ledger.hnr_completion_assessments
FOR EACH ROW EXECUTE FUNCTION ledger.protect_hnr_completion_assessment();

-- +goose StatementBegin
CREATE FUNCTION ledger.protect_hnr_obligation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    assessment ledger.hnr_completion_assessments%ROWTYPE;
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'H&R obligations cannot be deleted';
    END IF;
    SELECT * INTO STRICT assessment
    FROM ledger.hnr_completion_assessments
    WHERE id = NEW.assessment_id;
    IF NEW.raw_uploaded < assessment.initial_uploaded
        OR NEW.last_evidence_at < assessment.completed_at
        OR (NEW.satisfied_at IS NOT NULL AND (
            NEW.satisfied_at < assessment.completed_at
            OR NEW.satisfied_at > NEW.last_evidence_at
        ))
        OR (assessment.policy_mode = 'disabled')
        OR (assessment.policy_mode = 'exempt' AND (
            NEW.state <> 'exempt'
            OR NEW.satisfied_by <> 'exempt'
            OR NEW.satisfied_at <> assessment.completed_at
        ))
        OR (assessment.policy_mode = 'enforced' AND NEW.state = 'exempt') THEN
        RAISE EXCEPTION 'H&R obligation does not match its immutable assessment';
    END IF;
    IF TG_OP = 'INSERT' THEN
        RETURN NEW;
    END IF;
    IF OLD.id IS DISTINCT FROM NEW.id
        OR OLD.assessment_id IS DISTINCT FROM NEW.assessment_id
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'H&R obligation identity is immutable';
    END IF;
    IF NEW.version <> OLD.version + 1
        OR NEW.seeded_seconds < OLD.seeded_seconds
        OR NEW.raw_uploaded < OLD.raw_uploaded
        OR NEW.raw_ratio_basis_points < OLD.raw_ratio_basis_points
        OR NEW.last_evidence_at < OLD.last_evidence_at
        OR NEW.updated_at < OLD.updated_at THEN
        RAISE EXCEPTION 'H&R obligation progress must be monotonic';
    END IF;
    IF OLD.state <> 'tracking' AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'terminal H&R obligation cannot change';
    END IF;
    IF OLD.state = 'tracking' AND NEW.state NOT IN ('tracking', 'satisfied') THEN
        RAISE EXCEPTION 'invalid H&R state transition';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER hnr_obligation_monotonic
BEFORE INSERT OR UPDATE OR DELETE ON ledger.hnr_obligations
FOR EACH ROW EXECUTE FUNCTION ledger.protect_hnr_obligation();

-- +goose StatementBegin
CREATE FUNCTION settlement.protect_hnr_work()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'H&R work evidence cannot be deleted';
    END IF;
    IF OLD.interval_event_id IS DISTINCT FROM NEW.interval_event_id
        OR OLD.created_at IS DISTINCT FROM NEW.created_at
        OR NEW.attempts < OLD.attempts THEN
        RAISE EXCEPTION 'H&R work identity is immutable';
    END IF;
    IF OLD.processed_at IS NOT NULL AND OLD IS DISTINCT FROM NEW THEN
        RAISE EXCEPTION 'processed H&R work is terminal';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER settlement_hnr_work_protected
BEFORE UPDATE OR DELETE ON settlement.hnr_work
FOR EACH ROW EXECUTE FUNCTION settlement.protect_hnr_work();

-- +goose StatementBegin
CREATE FUNCTION settlement.protect_hnr_outbox()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'H&R outbox evidence cannot be deleted';
    END IF;
    IF OLD.event_id IS DISTINCT FROM NEW.event_id
        OR OLD.obligation_id IS DISTINCT FROM NEW.obligation_id
        OR OLD.obligation_version IS DISTINCT FROM NEW.obligation_version
        OR OLD.event_type IS DISTINCT FROM NEW.event_type
        OR OLD.schema_version IS DISTINCT FROM NEW.schema_version
        OR OLD.occurred_at IS DISTINCT FROM NEW.occurred_at
        OR OLD.payload_json IS DISTINCT FROM NEW.payload_json
        OR OLD.payload_sha256 IS DISTINCT FROM NEW.payload_sha256
        OR OLD.created_at IS DISTINCT FROM NEW.created_at
        OR NEW.attempts < OLD.attempts THEN
        RAISE EXCEPTION 'H&R outbox evidence is immutable';
    END IF;
    IF OLD.published_at IS NOT NULL AND OLD IS DISTINCT FROM NEW THEN
        RAISE EXCEPTION 'published H&R outbox event is terminal';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER settlement_hnr_outbox_protected
BEFORE UPDATE OR DELETE ON settlement.hnr_outbox
FOR EACH ROW EXECUTE FUNCTION settlement.protect_hnr_outbox();

-- +goose Down
DROP TRIGGER IF EXISTS settlement_hnr_outbox_protected ON settlement.hnr_outbox;
DROP TRIGGER IF EXISTS settlement_hnr_work_protected ON settlement.hnr_work;
DROP TRIGGER IF EXISTS hnr_obligation_monotonic ON ledger.hnr_obligations;
DROP TRIGGER IF EXISTS hnr_completion_assessment_immutable ON ledger.hnr_completion_assessments;
DROP TRIGGER IF EXISTS hnr_policy_timeline_immutable ON settlement.hnr_policy_timeline_revisions;
DROP TRIGGER IF EXISTS settlement_raw_interval_hnr_work ON ledger.raw_session_intervals;
DROP FUNCTION IF EXISTS settlement.protect_hnr_outbox();
DROP FUNCTION IF EXISTS settlement.protect_hnr_work();
DROP FUNCTION IF EXISTS ledger.protect_hnr_obligation();
DROP FUNCTION IF EXISTS ledger.protect_hnr_completion_assessment();
DROP FUNCTION IF EXISTS settlement.protect_hnr_policy_revision();
DROP FUNCTION IF EXISTS settlement.enqueue_hnr_work();
DROP TABLE IF EXISTS settlement.hnr_outbox;
DROP TABLE IF EXISTS ledger.hnr_obligations;
DROP TABLE IF EXISTS ledger.hnr_completion_assessments;
DROP TABLE IF EXISTS settlement.hnr_work;
DROP TABLE IF EXISTS settlement.hnr_policy_timeline_revisions;
ALTER TABLE ledger.raw_session_intervals DROP CONSTRAINT IF EXISTS raw_interval_completion_identity;
ALTER TABLE ledger.raw_session_intervals DROP COLUMN IF EXISTS completion_id;
ALTER TABLE ledger.raw_session_intervals DROP COLUMN IF EXISTS completion_identity_version;
