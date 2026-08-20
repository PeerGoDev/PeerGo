-- +goose Up

-- Workgroup accounting benefits are immutable facts from Core. Settlement
-- resolves them by announce interval time, never by the member's current group.
CREATE TABLE settlement.workgroup_benefit_transitions (
    transition_id uuid PRIMARY KEY,
    user_id uuid NOT NULL,
    group_kind text NOT NULL CHECK (group_kind = 'retention'),
    entitlement text NOT NULL CHECK (entitlement = 'traffic.download.charge_exempt'),
    active boolean NOT NULL,
    state_version bigint NOT NULL CHECK (state_version > 0),
    effective_at timestamptz NOT NULL,
    command_json text NOT NULL CHECK (
        octet_length(command_json) BETWEEN 2 AND 2048
        AND jsonb_typeof(command_json::jsonb) = 'object'
    ),
    command_sha256 bytea NOT NULL CHECK (octet_length(command_sha256) = 32),
    recorded_at timestamptz NOT NULL,
    UNIQUE (user_id, state_version)
);

CREATE INDEX workgroup_benefit_resolution_idx
    ON settlement.workgroup_benefit_transitions (user_id, effective_at, state_version, transition_id);

-- +goose StatementBegin
CREATE FUNCTION settlement.protect_workgroup_benefit_transition()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'Settlement workgroup benefit transitions are immutable';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM ledger.traffic_settlements AS settled
        WHERE settled.user_id = NEW.user_id
          AND settled.interval_ends_at > NEW.effective_at
    ) THEN
        RAISE EXCEPTION 'workgroup benefit would rewrite settled traffic';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER settlement_workgroup_benefit_immutable
BEFORE INSERT OR UPDATE OR DELETE ON settlement.workgroup_benefit_transitions
FOR EACH ROW EXECUTE FUNCTION settlement.protect_workgroup_benefit_transition();

-- +goose Down

DROP TRIGGER settlement_workgroup_benefit_immutable ON settlement.workgroup_benefit_transitions;
DROP FUNCTION settlement.protect_workgroup_benefit_transition();
DROP INDEX settlement.workgroup_benefit_resolution_idx;
DROP TABLE settlement.workgroup_benefit_transitions;
