-- +goose Up

-- A policy revision is a complete compiled Snapshot, not a reference to a
-- mutable promotion setting. The optional selector fields are explicit
-- wildcards; Settlement picks the most-specific matching selector and rejects
-- equal-specificity overlaps. This lets a raw event's captured control
-- sequences participate in historical resolution without teaching Tracker
-- about economic rules.
CREATE TABLE settlement.policy_timeline_revisions (
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
    revision_source text NOT NULL CHECK (revision_source = 'policy_revision'),
    revision_id text NOT NULL CHECK (
        char_length(revision_id) BETWEEN 1 AND 128
        AND revision_id = btrim(revision_id)
    ),
    revision_version bigint NOT NULL CHECK (revision_version > 0),
    profile text NOT NULL CHECK (profile IN ('peergo-v1', 'ptyes-v1')),
    snapshot_json text NOT NULL CHECK (
        octet_length(snapshot_json) BETWEEN 2 AND 65536
        AND jsonb_typeof(snapshot_json::jsonb) = 'object'
    ),
    snapshot_sha256 bytea NOT NULL CHECK (octet_length(snapshot_sha256) = 32),
    recorded_at timestamptz NOT NULL,
    UNIQUE NULLS NOT DISTINCT (
        scope_user_id,
        scope_torrent_id,
        scope_torrent_control_sequence,
        scope_subject_control_sequence,
        effective_at
    )
);

CREATE INDEX policy_timeline_resolution_idx
    ON settlement.policy_timeline_revisions (effective_at, id);

-- +goose StatementBegin
CREATE FUNCTION settlement.protect_policy_timeline_revision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'Settlement policy timeline revisions are immutable';
    END IF;

    -- A newly appended historical revision must not reinterpret traffic that
    -- already has a final result. Historical imports are allowed before that
    -- traffic is settled; after settlement, correction is a separate explicit
    -- compensating-ledger workflow rather than an in-place policy rewrite.
    IF EXISTS (
        SELECT 1
        FROM ledger.traffic_settlements AS settled
        WHERE settled.interval_ends_at > NEW.effective_at
          AND (NEW.scope_user_id IS NULL OR NEW.scope_user_id = settled.user_id)
          AND (NEW.scope_torrent_id IS NULL OR NEW.scope_torrent_id = settled.torrent_id)
          AND (
              NEW.scope_torrent_control_sequence IS NULL
              OR NEW.scope_torrent_control_sequence = settled.torrent_control_sequence
          )
          AND (
              NEW.scope_subject_control_sequence IS NULL
              OR NEW.scope_subject_control_sequence = settled.subject_control_sequence
          )
    ) THEN
        RAISE EXCEPTION 'policy timeline revision would rewrite settled traffic';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER settlement_policy_timeline_immutable
BEFORE INSERT OR UPDATE OR DELETE ON settlement.policy_timeline_revisions
FOR EACH ROW EXECUTE FUNCTION settlement.protect_policy_timeline_revision();

-- Every immutable raw interval immediately becomes a work item. This trigger
-- means a future ingest path cannot accidentally create raw evidence without
-- also making it discoverable by the policy stage.
CREATE TABLE settlement.policy_work (
    interval_event_id uuid PRIMARY KEY
        REFERENCES ledger.raw_session_intervals (event_id) ON DELETE RESTRICT,
    available_at timestamptz NOT NULL,
    lease_token uuid,
    lease_until timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error_code text CHECK (
        last_error_code IS NULL OR last_error_code ~ '^[a-z][a-z0-9_]{0,63}$'
    ),
    settled_at timestamptz,
    created_at timestamptz NOT NULL,
    CHECK ((lease_token IS NULL) = (lease_until IS NULL)),
    CHECK (settled_at IS NULL OR (lease_token IS NULL AND last_error_code IS NULL))
);

CREATE INDEX policy_work_ready_idx
    ON settlement.policy_work (available_at, interval_event_id)
    WHERE settled_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION settlement.enqueue_policy_work()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO settlement.policy_work (
        interval_event_id,
        available_at,
        created_at
    ) VALUES (
        NEW.event_id,
        NEW.created_at,
        NEW.created_at
    ) ON CONFLICT (interval_event_id) DO NOTHING;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER settlement_raw_interval_policy_work
AFTER INSERT ON ledger.raw_session_intervals
FOR EACH ROW EXECUTE FUNCTION settlement.enqueue_policy_work();

-- Existing raw rows predate the work trigger during the migration itself.
INSERT INTO settlement.policy_work (interval_event_id, available_at, created_at)
SELECT event_id, created_at, created_at
FROM ledger.raw_session_intervals
ON CONFLICT (interval_event_id) DO NOTHING;

-- +goose StatementBegin
CREATE FUNCTION settlement.protect_policy_work()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'Settlement policy work evidence cannot be deleted';
    END IF;
    IF OLD.interval_event_id IS DISTINCT FROM NEW.interval_event_id
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'Settlement policy work identity is immutable';
    END IF;
    IF OLD.settled_at IS NOT NULL AND OLD IS DISTINCT FROM NEW THEN
        RAISE EXCEPTION 'settled policy work is terminal';
    END IF;
    IF NEW.attempts < OLD.attempts THEN
        RAISE EXCEPTION 'Settlement policy work attempts cannot regress';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER settlement_policy_work_protected
BEFORE UPDATE OR DELETE ON settlement.policy_work
FOR EACH ROW EXECUTE FUNCTION settlement.protect_policy_work();

-- traffic_settlements is the final accounting result for exactly one raw
-- interval. settlement_id intentionally equals the immutable interval event
-- ID, making both the Ledger and Core projection idempotency keys stable.
CREATE TABLE ledger.traffic_settlements (
    settlement_id uuid PRIMARY KEY
        REFERENCES ledger.raw_session_intervals (event_id) ON DELETE RESTRICT,
    user_id uuid NOT NULL,
    torrent_id bigint NOT NULL CHECK (torrent_id > 0),
    torrent_control_sequence bigint NOT NULL CHECK (torrent_control_sequence > 0),
    subject_control_sequence bigint NOT NULL CHECK (subject_control_sequence > 0),
    interval_starts_at timestamptz NOT NULL,
    interval_ends_at timestamptz NOT NULL,
    raw_uploaded bigint NOT NULL CHECK (raw_uploaded >= 0),
    raw_downloaded bigint NOT NULL CHECK (raw_downloaded >= 0),
    credited_uploaded bigint NOT NULL CHECK (credited_uploaded >= 0),
    charged_downloaded bigint NOT NULL CHECK (charged_downloaded >= 0),
    settlement_sha256 bytea NOT NULL CHECK (octet_length(settlement_sha256) = 32),
    settled_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    CHECK (interval_ends_at > interval_starts_at),
    CHECK (settled_at >= interval_ends_at),
    CHECK (created_at = settled_at)
);

CREATE INDEX traffic_settlements_user_time_idx
    ON ledger.traffic_settlements (user_id, interval_ends_at DESC, settlement_id DESC);

CREATE TABLE ledger.traffic_settlement_segments (
    settlement_id uuid NOT NULL
        REFERENCES ledger.traffic_settlements (settlement_id) ON DELETE RESTRICT,
    segment_index integer NOT NULL CHECK (segment_index >= 0),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    policy_revision_source text NOT NULL CHECK (policy_revision_source = 'policy_revision'),
    policy_revision_id text NOT NULL CHECK (
        char_length(policy_revision_id) BETWEEN 1 AND 128
        AND policy_revision_id = btrim(policy_revision_id)
    ),
    policy_revision_version bigint NOT NULL CHECK (policy_revision_version > 0),
    policy_profile text NOT NULL CHECK (policy_profile IN ('peergo-v1', 'ptyes-v1')),
    policy_snapshot_sha256 bytea NOT NULL CHECK (octet_length(policy_snapshot_sha256) = 32),
    applications_json text NOT NULL CHECK (
        octet_length(applications_json) BETWEEN 2 AND 65536
        AND jsonb_typeof(applications_json::jsonb) = 'array'
    ),
    applications_sha256 bytea NOT NULL CHECK (octet_length(applications_sha256) = 32),
    raw_uploaded bigint NOT NULL CHECK (raw_uploaded >= 0),
    raw_downloaded bigint NOT NULL CHECK (raw_downloaded >= 0),
    credited_uploaded bigint NOT NULL CHECK (credited_uploaded >= 0),
    charged_downloaded bigint NOT NULL CHECK (charged_downloaded >= 0),
    PRIMARY KEY (settlement_id, segment_index),
    CHECK (ends_at > starts_at)
);

-- +goose StatementBegin
CREATE FUNCTION ledger.verify_traffic_settlement_raw_interval()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    raw ledger.raw_session_intervals%ROWTYPE;
BEGIN
    SELECT * INTO raw
    FROM ledger.raw_session_intervals
    WHERE event_id = NEW.settlement_id;

    IF NOT FOUND
        OR NEW.user_id IS DISTINCT FROM raw.user_id
        OR NEW.torrent_id IS DISTINCT FROM raw.torrent_id
        OR NEW.torrent_control_sequence IS DISTINCT FROM raw.torrent_control_sequence
        OR NEW.subject_control_sequence IS DISTINCT FROM raw.subject_control_sequence
        OR NEW.interval_starts_at IS DISTINCT FROM raw.starts_at
        OR NEW.interval_ends_at IS DISTINCT FROM raw.ends_at
        OR NEW.raw_uploaded IS DISTINCT FROM raw.raw_uploaded
        OR NEW.raw_downloaded IS DISTINCT FROM raw.raw_downloaded THEN
        RAISE EXCEPTION 'traffic settlement must preserve immutable raw interval facts';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER traffic_settlements_match_raw_interval
BEFORE INSERT ON ledger.traffic_settlements
FOR EACH ROW EXECUTE FUNCTION ledger.verify_traffic_settlement_raw_interval();

-- +goose StatementBegin
CREATE FUNCTION ledger.reject_traffic_settlement_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'final traffic settlements are immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER traffic_settlements_immutable
BEFORE UPDATE OR DELETE ON ledger.traffic_settlements
FOR EACH ROW EXECUTE FUNCTION ledger.reject_traffic_settlement_mutation();

CREATE TRIGGER traffic_settlement_segments_immutable
BEFORE UPDATE OR DELETE ON ledger.traffic_settlement_segments
FOR EACH ROW EXECUTE FUNCTION ledger.reject_traffic_settlement_mutation();

-- The header and all policy slices are inserted in one transaction. A
-- deferred verification makes it impossible to commit an accounting total that
-- omits bytes, double-counts bytes, or has gaps across a policy boundary.
-- +goose StatementBegin
CREATE FUNCTION ledger.require_complete_traffic_settlement()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_settlement_id uuid;
    header ledger.traffic_settlements%ROWTYPE;
    segment_count bigint;
    total_raw_uploaded numeric;
    total_raw_downloaded numeric;
    total_credited_uploaded numeric;
    total_charged_downloaded numeric;
    first_start timestamptz;
    last_end timestamptz;
    contiguous boolean;
BEGIN
    target_settlement_id := NEW.settlement_id;
    SELECT * INTO header
    FROM ledger.traffic_settlements
    WHERE settlement_id = target_settlement_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'traffic settlement header is missing';
    END IF;

    SELECT
        count(*),
        COALESCE(sum(raw_uploaded), 0),
        COALESCE(sum(raw_downloaded), 0),
        COALESCE(sum(credited_uploaded), 0),
        COALESCE(sum(charged_downloaded), 0),
        min(starts_at),
        max(ends_at)
    INTO
        segment_count,
        total_raw_uploaded,
        total_raw_downloaded,
        total_credited_uploaded,
        total_charged_downloaded,
        first_start,
        last_end
    FROM ledger.traffic_settlement_segments
    WHERE settlement_id = target_settlement_id;

    SELECT COALESCE(bool_and(starts_at = expected_start), false)
    INTO contiguous
    FROM (
        SELECT starts_at,
            COALESCE(lag(ends_at) OVER (ORDER BY segment_index), header.interval_starts_at) AS expected_start
        FROM ledger.traffic_settlement_segments
        WHERE settlement_id = target_settlement_id
    ) AS ordered_segments;

    IF segment_count = 0
        OR first_start IS DISTINCT FROM header.interval_starts_at
        OR last_end IS DISTINCT FROM header.interval_ends_at
        OR NOT contiguous
        OR total_raw_uploaded <> header.raw_uploaded
        OR total_raw_downloaded <> header.raw_downloaded
        OR total_credited_uploaded <> header.credited_uploaded
        OR total_charged_downloaded <> header.charged_downloaded THEN
        RAISE EXCEPTION 'traffic settlement segments do not reconcile with header';
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER traffic_settlement_header_complete
AFTER INSERT ON ledger.traffic_settlements
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION ledger.require_complete_traffic_settlement();

CREATE CONSTRAINT TRIGGER traffic_settlement_segments_complete
AFTER INSERT ON ledger.traffic_settlement_segments
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION ledger.require_complete_traffic_settlement();

-- The transactional outbox is separate from the immutable final result: an
-- unavailable bus cannot make a committed result disappear, and a successful
-- publish followed by a database timeout is safe because Core's inbox keys on
-- event_id.
CREATE TABLE settlement.traffic_outbox (
    event_id uuid PRIMARY KEY,
    settlement_id uuid NOT NULL UNIQUE
        REFERENCES ledger.traffic_settlements (settlement_id) ON DELETE RESTRICT,
    event_type text NOT NULL CHECK (event_type = 'settlement.traffic.credited'),
    schema_version text NOT NULL CHECK (schema_version = 'settlement.traffic.v1'),
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
    CHECK ((lease_token IS NULL) = (lease_until IS NULL)),
    CHECK (published_at IS NULL OR (lease_token IS NULL AND last_error_code IS NULL))
);

CREATE INDEX traffic_outbox_ready_idx
    ON settlement.traffic_outbox (available_at, event_id)
    WHERE published_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION settlement.protect_traffic_outbox()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'Settlement traffic outbox evidence cannot be deleted';
    END IF;
    IF OLD.event_id IS DISTINCT FROM NEW.event_id
        OR OLD.settlement_id IS DISTINCT FROM NEW.settlement_id
        OR OLD.event_type IS DISTINCT FROM NEW.event_type
        OR OLD.schema_version IS DISTINCT FROM NEW.schema_version
        OR OLD.occurred_at IS DISTINCT FROM NEW.occurred_at
        OR OLD.payload_json IS DISTINCT FROM NEW.payload_json
        OR OLD.payload_sha256 IS DISTINCT FROM NEW.payload_sha256
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'Settlement traffic outbox event evidence is immutable';
    END IF;
    IF OLD.published_at IS NOT NULL AND OLD IS DISTINCT FROM NEW THEN
        RAISE EXCEPTION 'published Settlement traffic outbox event is terminal';
    END IF;
    IF NEW.attempts < OLD.attempts THEN
        RAISE EXCEPTION 'Settlement traffic outbox attempts cannot regress';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER settlement_traffic_outbox_protected
BEFORE UPDATE OR DELETE ON settlement.traffic_outbox
FOR EACH ROW EXECUTE FUNCTION settlement.protect_traffic_outbox();

-- +goose Down

DROP TRIGGER IF EXISTS settlement_traffic_outbox_protected ON settlement.traffic_outbox;
DROP FUNCTION IF EXISTS settlement.protect_traffic_outbox();
DROP TABLE IF EXISTS settlement.traffic_outbox;

DROP TRIGGER IF EXISTS traffic_settlement_segments_complete ON ledger.traffic_settlement_segments;
DROP TRIGGER IF EXISTS traffic_settlement_header_complete ON ledger.traffic_settlements;
DROP FUNCTION IF EXISTS ledger.require_complete_traffic_settlement();
DROP TRIGGER IF EXISTS traffic_settlement_segments_immutable ON ledger.traffic_settlement_segments;
DROP TRIGGER IF EXISTS traffic_settlements_immutable ON ledger.traffic_settlements;
DROP FUNCTION IF EXISTS ledger.reject_traffic_settlement_mutation();
DROP TRIGGER IF EXISTS traffic_settlements_match_raw_interval ON ledger.traffic_settlements;
DROP FUNCTION IF EXISTS ledger.verify_traffic_settlement_raw_interval();
DROP TABLE IF EXISTS ledger.traffic_settlement_segments;
DROP TABLE IF EXISTS ledger.traffic_settlements;

DROP TRIGGER IF EXISTS settlement_policy_work_protected ON settlement.policy_work;
DROP FUNCTION IF EXISTS settlement.protect_policy_work();
DROP TRIGGER IF EXISTS settlement_raw_interval_policy_work ON ledger.raw_session_intervals;
DROP FUNCTION IF EXISTS settlement.enqueue_policy_work();
DROP TABLE IF EXISTS settlement.policy_work;

DROP TRIGGER IF EXISTS settlement_policy_timeline_immutable ON settlement.policy_timeline_revisions;
DROP FUNCTION IF EXISTS settlement.protect_policy_timeline_revision();
DROP TABLE IF EXISTS settlement.policy_timeline_revisions;
