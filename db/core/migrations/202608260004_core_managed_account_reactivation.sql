-- +goose Up

-- A reactivation is rare and security-relevant, so keep exactly one compact
-- immutable fact per action rather than copying mutable user snapshots.
CREATE TABLE identity.account_reactivations (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    reason text NOT NULL CHECK (
        reason = btrim(reason) AND char_length(reason) BETWEEN 10 AND 500
    ),
    actor_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid NOT NULL,
    previous_administration_version bigint NOT NULL CHECK (
        previous_administration_version > 0
    ),
    occurred_at timestamptz NOT NULL
);

CREATE INDEX account_reactivations_user_time_idx
    ON identity.account_reactivations (user_id, occurred_at DESC);

-- +goose StatementBegin
CREATE FUNCTION identity.reject_account_reactivation_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'account reactivation evidence is immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER account_reactivations_immutable
BEFORE UPDATE OR DELETE ON identity.account_reactivations
FOR EACH ROW EXECUTE FUNCTION identity.reject_account_reactivation_mutation();

-- +goose Down

DROP TRIGGER account_reactivations_immutable ON identity.account_reactivations;
DROP FUNCTION identity.reject_account_reactivation_mutation();
DROP INDEX identity.account_reactivations_user_time_idx;
DROP TABLE identity.account_reactivations;
