-- +goose Up

-- VIP accounting benefits arrive as immutable state transitions from Core.
-- Settlement resolves the state by interval time and records the winning VIP
-- transition as an ordinary account-tier rule application in ledger evidence.
CREATE TABLE settlement.vip_benefit_transitions (
    transition_id uuid PRIMARY KEY,
    user_id uuid NOT NULL,
    entitlement text NOT NULL CHECK (
        entitlement = 'traffic.download.charge_exempt'
    ),
    enabled boolean NOT NULL,
    active_until timestamptz,
    state_version bigint NOT NULL CHECK (state_version > 0),
    effective_at timestamptz NOT NULL,
    command_json text NOT NULL CHECK (
        octet_length(command_json) BETWEEN 2 AND 2048
        AND jsonb_typeof(command_json::jsonb) = 'object'
    ),
    command_sha256 bytea NOT NULL CHECK (octet_length(command_sha256) = 32),
    recorded_at timestamptz NOT NULL,
    UNIQUE (user_id, state_version),
    CHECK (active_until IS NULL OR enabled)
);

CREATE INDEX vip_benefit_resolution_idx
    ON settlement.vip_benefit_transitions
        (user_id, effective_at, state_version, transition_id);

-- +goose StatementBegin
CREATE FUNCTION settlement.protect_vip_benefit_transition()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'Settlement VIP benefit transitions are immutable';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM ledger.traffic_settlements AS settled
        WHERE settled.user_id = NEW.user_id
          AND settled.interval_ends_at > NEW.effective_at
    ) THEN
        RAISE EXCEPTION 'VIP benefit would rewrite settled traffic';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER settlement_vip_benefit_immutable
BEFORE INSERT OR UPDATE OR DELETE ON settlement.vip_benefit_transitions
FOR EACH ROW EXECUTE FUNCTION settlement.protect_vip_benefit_transition();

-- +goose Down

DROP TRIGGER settlement_vip_benefit_immutable
    ON settlement.vip_benefit_transitions;
DROP FUNCTION settlement.protect_vip_benefit_transition();
DROP INDEX settlement.vip_benefit_resolution_idx;
DROP TABLE settlement.vip_benefit_transitions;
