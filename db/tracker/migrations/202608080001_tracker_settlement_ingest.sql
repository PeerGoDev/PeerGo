-- +goose Up

CREATE SCHEMA IF NOT EXISTS settlement;
CREATE SCHEMA IF NOT EXISTS ledger;

-- event_inbox is both the durable idempotency fence and the short-term raw
-- replay evidence. The permanent raw ledger also keys every interval by the
-- same event_id, so pruning the inbox later cannot make an old interval apply
-- twice. Canonical payload bytes are retained until an explicit archive job
-- proves the configured replay/reconciliation window has elapsed.
CREATE TABLE settlement.event_inbox (
    event_id uuid PRIMARY KEY,
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    payload_json text NOT NULL CHECK (
        octet_length(payload_json) BETWEEN 2 AND 16384
        AND jsonb_typeof(payload_json::jsonb) = 'object'
    ),
    source_stream text NOT NULL CHECK (
        char_length(source_stream) BETWEEN 1 AND 255
        AND source_stream !~ '[.*/\\[:space:]]'
    ),
    source_subject text NOT NULL CHECK (
        char_length(source_subject) BETWEEN 1 AND 255
        AND source_subject !~ '[*>[:space:]]'
        AND source_subject !~ '(^\\.|\\.$|\\.\\.)'
    ),
    source_sequence bigint NOT NULL CHECK (source_sequence > 0),
    delivery_count bigint NOT NULL CHECK (delivery_count > 0),
    received_at timestamptz NOT NULL,
    ingested_at timestamptz NOT NULL,
    outcome text NOT NULL CHECK (outcome IN (
        'processing',
        'baseline',
        'interval',
        'counter_reset',
        'out_of_order',
        'reopened_baseline'
    )),
    session_epoch bigint CHECK (session_epoch > 0),
    ledger_event_id uuid UNIQUE,
    processed_at timestamptz,
    UNIQUE (source_stream, source_sequence),
    CHECK (
        (outcome = 'processing' AND session_epoch IS NULL AND ledger_event_id IS NULL AND processed_at IS NULL)
        OR
        (outcome <> 'processing' AND session_epoch IS NOT NULL AND processed_at IS NOT NULL)
    ),
    CHECK ((outcome = 'interval') = (ledger_event_id IS NOT NULL)),
    CHECK (ledger_event_id IS NULL OR ledger_event_id = event_id)
);

-- One row is the latest trustworthy baseline for a privacy-minimized client
-- session. session_token is already a one-way SHA-256 value; no peer ID,
-- passkey, IP or port enters this database.
CREATE TABLE settlement.session_states (
    user_id uuid NOT NULL,
    torrent_id bigint NOT NULL CHECK (torrent_id > 0),
    session_token bytea NOT NULL CHECK (octet_length(session_token) = 32),
    info_hash_v1 bytea NOT NULL CHECK (octet_length(info_hash_v1) = 20),
    session_epoch bigint NOT NULL CHECK (session_epoch > 0),
    version bigint NOT NULL CHECK (version > 0),
    last_event_id uuid NOT NULL UNIQUE
        REFERENCES settlement.event_inbox (event_id) ON DELETE RESTRICT,
    last_received_at timestamptz NOT NULL,
    last_event_kind text NOT NULL CHECK (last_event_kind IN ('', 'started', 'stopped', 'completed')),
    last_uploaded bigint NOT NULL CHECK (last_uploaded >= 0),
    last_downloaded bigint NOT NULL CHECK (last_downloaded >= 0),
    last_left bigint NOT NULL CHECK (last_left >= 0),
    last_address_family smallint NOT NULL CHECK (last_address_family IN (4, 6)),
    last_credential_version bigint NOT NULL CHECK (last_credential_version > 0),
    torrent_control_sequence bigint NOT NULL CHECK (torrent_control_sequence > 0),
    subject_control_sequence bigint NOT NULL CHECK (subject_control_sequence > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, torrent_id, session_token),
    CHECK (updated_at >= created_at)
);

-- Raw intervals are immutable facts, not final credited/charged traffic. A
-- later policy stage must resolve an immutable policy timeline before it can
-- append a settlement result and Core projection outbox event.
CREATE TABLE ledger.raw_session_intervals (
    event_id uuid PRIMARY KEY
        REFERENCES settlement.event_inbox (event_id) ON DELETE RESTRICT,
    previous_event_id uuid NOT NULL
        REFERENCES settlement.event_inbox (event_id) ON DELETE RESTRICT,
    user_id uuid NOT NULL,
    torrent_id bigint NOT NULL CHECK (torrent_id > 0),
    session_token bytea NOT NULL CHECK (octet_length(session_token) = 32),
    info_hash_v1 bytea NOT NULL CHECK (octet_length(info_hash_v1) = 20),
    session_epoch bigint NOT NULL CHECK (session_epoch > 0),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    event_kind text NOT NULL CHECK (event_kind IN ('', 'started', 'stopped', 'completed')),
    address_family smallint NOT NULL CHECK (address_family IN (4, 6)),
    credential_version bigint NOT NULL CHECK (credential_version > 0),
    torrent_control_sequence bigint NOT NULL CHECK (torrent_control_sequence > 0),
    subject_control_sequence bigint NOT NULL CHECK (subject_control_sequence > 0),
    previous_uploaded bigint NOT NULL CHECK (previous_uploaded >= 0),
    current_uploaded bigint NOT NULL CHECK (current_uploaded >= previous_uploaded),
    previous_downloaded bigint NOT NULL CHECK (previous_downloaded >= 0),
    current_downloaded bigint NOT NULL CHECK (current_downloaded >= previous_downloaded),
    previous_left bigint NOT NULL CHECK (previous_left >= 0),
    current_left bigint NOT NULL CHECK (current_left >= 0),
    raw_uploaded bigint NOT NULL CHECK (raw_uploaded >= 0),
    raw_downloaded bigint NOT NULL CHECK (raw_downloaded >= 0),
    completed_transition boolean NOT NULL,
    created_at timestamptz NOT NULL,
    CHECK (ends_at > starts_at),
    CHECK (raw_uploaded = current_uploaded - previous_uploaded),
    CHECK (raw_downloaded = current_downloaded - previous_downloaded),
    CHECK (NOT completed_transition OR (previous_left > 0 AND current_left = 0))
);

ALTER TABLE settlement.event_inbox
    ADD CONSTRAINT event_inbox_ledger_event_fk
    FOREIGN KEY (ledger_event_id)
    REFERENCES ledger.raw_session_intervals (event_id)
    ON DELETE RESTRICT;

CREATE INDEX raw_session_intervals_user_time_idx
    ON ledger.raw_session_intervals (user_id, ends_at DESC, event_id DESC);

CREATE INDEX raw_session_intervals_torrent_time_idx
    ON ledger.raw_session_intervals (torrent_id, ends_at DESC, event_id DESC);

-- +goose StatementBegin
CREATE FUNCTION settlement.protect_terminal_inbox()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'Settlement inbox evidence cannot be deleted';
    END IF;

    IF OLD.outcome <> 'processing' THEN
        RAISE EXCEPTION 'terminal Settlement inbox evidence is immutable';
    END IF;

    IF OLD.event_id IS DISTINCT FROM NEW.event_id
        OR OLD.payload_sha256 IS DISTINCT FROM NEW.payload_sha256
        OR OLD.payload_json IS DISTINCT FROM NEW.payload_json
        OR OLD.source_stream IS DISTINCT FROM NEW.source_stream
        OR OLD.source_subject IS DISTINCT FROM NEW.source_subject
        OR OLD.source_sequence IS DISTINCT FROM NEW.source_sequence
        OR OLD.delivery_count IS DISTINCT FROM NEW.delivery_count
        OR OLD.received_at IS DISTINCT FROM NEW.received_at
        OR OLD.ingested_at IS DISTINCT FROM NEW.ingested_at THEN
        RAISE EXCEPTION 'Settlement inbox event evidence is immutable';
    END IF;

    IF NEW.outcome = 'processing' THEN
        RAISE EXCEPTION 'Settlement inbox finalization must be terminal';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER settlement_event_inbox_terminal
BEFORE UPDATE OR DELETE ON settlement.event_inbox
FOR EACH ROW EXECUTE FUNCTION settlement.protect_terminal_inbox();

-- The repository inserts a processing fence before it locks the session. This
-- deferred check makes it impossible to accidentally commit that provisional
-- row if a new code path forgets to finalize the transaction.
-- +goose StatementBegin
CREATE FUNCTION settlement.require_terminal_inbox_at_commit()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    current_outcome text;
BEGIN
    SELECT outcome INTO current_outcome
    FROM settlement.event_inbox
    WHERE event_id = NEW.event_id;

    IF current_outcome = 'processing' THEN
        RAISE EXCEPTION 'Settlement inbox event must be terminal at commit';
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER settlement_event_inbox_terminal_at_commit
AFTER INSERT OR UPDATE ON settlement.event_inbox
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION settlement.require_terminal_inbox_at_commit();

-- +goose StatementBegin
CREATE FUNCTION settlement.protect_session_transition()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'Settlement session state cannot be deleted';
    END IF;

    IF OLD.user_id IS DISTINCT FROM NEW.user_id
        OR OLD.torrent_id IS DISTINCT FROM NEW.torrent_id
        OR OLD.session_token IS DISTINCT FROM NEW.session_token
        OR OLD.info_hash_v1 IS DISTINCT FROM NEW.info_hash_v1
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'Settlement session identity is immutable';
    END IF;

    IF NEW.version <> OLD.version + 1
        OR NEW.last_received_at <= OLD.last_received_at
        OR NEW.updated_at < OLD.updated_at
        OR NEW.session_epoch NOT IN (OLD.session_epoch, OLD.session_epoch + 1) THEN
        RAISE EXCEPTION 'Settlement session transition is not monotonic';
    END IF;

    IF NEW.session_epoch = OLD.session_epoch
        AND (NEW.last_uploaded < OLD.last_uploaded OR NEW.last_downloaded < OLD.last_downloaded) THEN
        RAISE EXCEPTION 'Settlement counters can regress only in a new epoch';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER settlement_session_state_transition
BEFORE UPDATE OR DELETE ON settlement.session_states
FOR EACH ROW EXECUTE FUNCTION settlement.protect_session_transition();

-- +goose StatementBegin
CREATE FUNCTION ledger.reject_raw_interval_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'raw Tracker ledger intervals are immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER raw_session_intervals_immutable
BEFORE UPDATE OR DELETE ON ledger.raw_session_intervals
FOR EACH ROW EXECUTE FUNCTION ledger.reject_raw_interval_mutation();

-- +goose Down
DROP SCHEMA IF EXISTS settlement CASCADE;
DROP SCHEMA IF EXISTS ledger CASCADE;
