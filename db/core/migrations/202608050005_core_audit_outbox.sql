-- +goose Up
CREATE SCHEMA IF NOT EXISTS audit;

-- The outbox payload is immutable audit evidence. Delivery metadata is kept on
-- the same row so dispatchers can lease work without changing the evidence
-- bytes that were committed by the authorization or business transaction.
CREATE TABLE audit.outbox (
    event_id uuid PRIMARY KEY,
    event_type text NOT NULL
        CHECK (event_type ~ '^[a-z][a-z0-9]*(\.[a-z][a-z0-9_-]*)+$'),
    schema_version text NOT NULL
        CHECK (schema_version ~ '^[1-9][0-9]*\.[0-9]+\.[0-9]+$'),
    occurred_at timestamptz NOT NULL,
    payload_json text NOT NULL
        CHECK (
            octet_length(payload_json) BETWEEN 2 AND 1048576
            AND jsonb_typeof(payload_json::jsonb) = 'object'
        ),
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    available_at timestamptz NOT NULL DEFAULT now(),
    lease_until timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error text CHECK (last_error IS NULL OR char_length(last_error) <= 1000),
    delivered_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (delivered_at IS NULL OR lease_until IS NULL)
);

CREATE INDEX audit_outbox_pending_idx
    ON audit.outbox (available_at, occurred_at, event_id)
    WHERE delivered_at IS NULL;

-- Application credentials may update only delivery bookkeeping. An accidental
-- UPDATE or DELETE cannot rewrite the event that was part of the original
-- transaction. Production still uses a non-owner runtime role as another layer.
-- +goose StatementBegin
CREATE FUNCTION audit.protect_outbox_evidence()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'audit outbox evidence cannot be deleted';
    END IF;

    IF OLD.event_id IS DISTINCT FROM NEW.event_id
        OR OLD.event_type IS DISTINCT FROM NEW.event_type
        OR OLD.schema_version IS DISTINCT FROM NEW.schema_version
        OR OLD.occurred_at IS DISTINCT FROM NEW.occurred_at
        OR OLD.payload_json IS DISTINCT FROM NEW.payload_json
        OR OLD.payload_sha256 IS DISTINCT FROM NEW.payload_sha256
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'audit outbox evidence is immutable';
    END IF;

    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER audit_outbox_evidence_immutable
BEFORE UPDATE OR DELETE ON audit.outbox
FOR EACH ROW EXECUTE FUNCTION audit.protect_outbox_evidence();

-- +goose Down
DROP SCHEMA IF EXISTS audit CASCADE;
