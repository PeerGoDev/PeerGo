-- +goose Up

-- Retention membership is an accounting input, so its canonical Settlement
-- command is committed beside the immutable Core membership transition. A
-- worker may retry delivery without inventing or rewriting economic history.
CREATE TABLE workgroups.settlement_benefit_outbox (
    transition_id uuid PRIMARY KEY
        REFERENCES workgroups.membership_transitions (id) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    state_version bigint NOT NULL CHECK (state_version > 0),
    effective_at timestamptz NOT NULL,
    command_json text NOT NULL CHECK (
        octet_length(command_json) BETWEEN 2 AND 2048
        AND jsonb_typeof(command_json::jsonb) = 'object'
    ),
    command_sha256 bytea NOT NULL CHECK (octet_length(command_sha256) = 32),
    available_at timestamptz NOT NULL,
    lease_token uuid,
    lease_until timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error_code text CHECK (
        last_error_code IS NULL OR last_error_code ~ '^[a-z][a-z0-9_]{0,63}$'
    ),
    delivered_at timestamptz,
    created_at timestamptz NOT NULL,
    UNIQUE (user_id, state_version),
    CHECK ((lease_token IS NULL) = (lease_until IS NULL)),
    CHECK (delivered_at IS NULL OR (lease_token IS NULL AND last_error_code IS NULL))
);

CREATE INDEX settlement_benefit_outbox_ready_idx
    ON workgroups.settlement_benefit_outbox (available_at, state_version, transition_id)
    WHERE delivered_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION workgroups.protect_settlement_benefit_outbox()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'workgroup benefit outbox rows cannot be deleted';
    END IF;
    IF OLD.transition_id IS DISTINCT FROM NEW.transition_id
        OR OLD.user_id IS DISTINCT FROM NEW.user_id
        OR OLD.state_version IS DISTINCT FROM NEW.state_version
        OR OLD.effective_at IS DISTINCT FROM NEW.effective_at
        OR OLD.command_json IS DISTINCT FROM NEW.command_json
        OR OLD.command_sha256 IS DISTINCT FROM NEW.command_sha256
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'workgroup benefit outbox payload is immutable';
    END IF;
    IF OLD.delivered_at IS NOT NULL AND OLD IS DISTINCT FROM NEW THEN
        RAISE EXCEPTION 'delivered workgroup benefit command is terminal';
    END IF;
    IF NEW.attempts < OLD.attempts THEN
        RAISE EXCEPTION 'workgroup benefit attempts cannot regress';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER settlement_benefit_outbox_protected
BEFORE UPDATE OR DELETE ON workgroups.settlement_benefit_outbox
FOR EACH ROW EXECUTE FUNCTION workgroups.protect_settlement_benefit_outbox();

REVOKE ALL ON workgroups.settlement_benefit_outbox FROM PUBLIC;

-- +goose Down

DROP TRIGGER IF EXISTS settlement_benefit_outbox_protected
    ON workgroups.settlement_benefit_outbox;
DROP FUNCTION IF EXISTS workgroups.protect_settlement_benefit_outbox();
DROP TABLE IF EXISTS workgroups.settlement_benefit_outbox;
