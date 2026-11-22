-- +goose Up

CREATE TABLE economy.attendance_streak_grants (
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    effective_on date NOT NULL,
    streak_days integer NOT NULL CHECK (streak_days BETWEEN 1 AND 999999),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 500),
    granted_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, effective_on)
);

CREATE TRIGGER attendance_streak_grants_immutable
BEFORE UPDATE OR DELETE ON economy.attendance_streak_grants
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

REVOKE ALL ON economy.attendance_streak_grants FROM PUBLIC;

-- +goose Down

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM economy.attendance_streak_grants) THEN
        RAISE EXCEPTION '202611220001 cannot roll back after attendance streaks were granted';
    END IF;
END;
$$;
-- +goose StatementEnd

DROP TABLE economy.attendance_streak_grants;
