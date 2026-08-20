-- +goose Up

-- Historical attendance is already represented in the migrated magic and
-- experience opening balances.  Importing every old claim into the live
-- attendance table would mint those rewards a second time, so cutover keeps
-- one immutable statistical opening per member instead.  Runtime claims add
-- to this opening and continue a streak only when its last source date is the
-- immediately preceding local day.
CREATE TABLE migration.user_attendance_openings (
    source_system text NOT NULL CHECK (source_system = 'ptyes'),
    legacy_user_id bigint NOT NULL CHECK (legacy_user_id > 0),
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    source_stats_present boolean NOT NULL,
    source_current_streak integer NOT NULL CHECK (
        source_current_streak BETWEEN 0 AND 1000000
    ),
    source_longest_streak integer NOT NULL CHECK (
        source_longest_streak BETWEEN source_current_streak AND 1000000
    ),
    source_total_days integer NOT NULL CHECK (
        source_total_days BETWEEN source_longest_streak AND 1000000
    ),
    source_retroactive_cards integer NOT NULL CHECK (
        source_retroactive_cards BETWEEN 0 AND 1000000
    ),
    source_last_attendance_date date,
    source_stats_last_attendance_at timestamptz,
    source_record_days integer NOT NULL CHECK (
        source_record_days BETWEEN 0 AND 1000000
    ),
    source_fingerprint bytea NOT NULL CHECK (octet_length(source_fingerprint) = 32),
    first_run_id uuid NOT NULL REFERENCES migration.runs (id) ON DELETE RESTRICT,
    imported_at timestamptz NOT NULL,
    PRIMARY KEY (source_system, legacy_user_id),
    UNIQUE (user_id),
    FOREIGN KEY (source_system, legacy_user_id, user_id)
        REFERENCES migration.user_id_map (source_system, legacy_user_id, user_id)
        ON DELETE RESTRICT,
    CHECK (source_stats_present OR (
        source_current_streak = 0
        AND source_longest_streak = 0
        AND source_total_days = 0
        AND source_retroactive_cards = 0
        AND source_stats_last_attendance_at IS NULL
    )),
    CHECK ((source_record_days = 0) = (source_last_attendance_date IS NULL)),
    CHECK ((source_total_days = 0) = (source_last_attendance_date IS NULL))
);

CREATE TRIGGER migration_user_attendance_openings_immutable
BEFORE UPDATE OR DELETE ON migration.user_attendance_openings
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

REVOKE ALL ON migration.user_attendance_openings FROM PUBLIC;

-- +goose Down

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM migration.user_attendance_openings) THEN
        RAISE EXCEPTION '202608200003 cannot roll back after legacy attendance was imported';
    END IF;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER migration_user_attendance_openings_immutable
    ON migration.user_attendance_openings;
DROP TABLE migration.user_attendance_openings;
