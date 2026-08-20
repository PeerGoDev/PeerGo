-- +goose Up

-- A closed evidence window and every transport chunk commit together. The
-- dispatcher may retry indefinitely without rebuilding Tracker facts, while
-- Core's inbox and projection digest provide the independent idempotency and
-- completeness fences.
CREATE TABLE settlement.seeding_evidence_outbox (
    event_id uuid PRIMARY KEY,
    window_start timestamptz NOT NULL
        REFERENCES ledger.seeding_evidence_windows (window_start) ON DELETE RESTRICT,
    chunk_index integer NOT NULL CHECK (chunk_index >= 0),
    event_type text NOT NULL CHECK (event_type = 'settlement.seeding.evidence.closed'),
    schema_version text NOT NULL CHECK (schema_version = 'settlement.seeding.evidence.v1'),
    occurred_at timestamptz NOT NULL,
    payload_json text NOT NULL CHECK (
        octet_length(payload_json) BETWEEN 2 AND 131072
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
    UNIQUE (window_start, chunk_index),
    CHECK ((lease_token IS NULL) = (lease_until IS NULL)),
    CHECK (published_at IS NULL OR (lease_token IS NULL AND last_error_code IS NULL)),
    CHECK (occurred_at >= window_start + interval '1 hour')
);

CREATE INDEX seeding_evidence_outbox_ready_idx
    ON settlement.seeding_evidence_outbox (available_at, event_id)
    WHERE published_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION settlement.protect_seeding_evidence_outbox()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'Settlement seeding evidence outbox cannot be deleted';
    END IF;
    IF OLD.event_id IS DISTINCT FROM NEW.event_id
        OR OLD.window_start IS DISTINCT FROM NEW.window_start
        OR OLD.chunk_index IS DISTINCT FROM NEW.chunk_index
        OR OLD.event_type IS DISTINCT FROM NEW.event_type
        OR OLD.schema_version IS DISTINCT FROM NEW.schema_version
        OR OLD.occurred_at IS DISTINCT FROM NEW.occurred_at
        OR OLD.payload_json IS DISTINCT FROM NEW.payload_json
        OR OLD.payload_sha256 IS DISTINCT FROM NEW.payload_sha256
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'Settlement seeding evidence outbox payload is immutable';
    END IF;
    IF OLD.published_at IS NOT NULL AND OLD IS DISTINCT FROM NEW THEN
        RAISE EXCEPTION 'published Settlement seeding evidence outbox event is terminal';
    END IF;
    IF NEW.attempts < OLD.attempts THEN
        RAISE EXCEPTION 'Settlement seeding evidence outbox attempts cannot regress';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER settlement_seeding_evidence_outbox_protected
BEFORE UPDATE OR DELETE ON settlement.seeding_evidence_outbox
FOR EACH ROW EXECUTE FUNCTION settlement.protect_seeding_evidence_outbox();

-- +goose Down

DROP TRIGGER IF EXISTS settlement_seeding_evidence_outbox_protected
    ON settlement.seeding_evidence_outbox;
DROP FUNCTION IF EXISTS settlement.protect_seeding_evidence_outbox();
DROP TABLE IF EXISTS settlement.seeding_evidence_outbox;
