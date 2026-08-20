-- +goose Up

-- The singleton remains the fast current-state projection. Every staff change
-- is additionally frozen here so an operator can prove which limits produced
-- a member-facing result without retaining a mutable generic settings blob.
CREATE TABLE economy.medal_settings_revisions (
    version bigint PRIMARY KEY CHECK (version > 0),
    enabled boolean NOT NULL,
    maximum_wear_count bigint NOT NULL CHECK (maximum_wear_count BETWEEN 0 AND 100),
    maximum_upload_bonus_bps bigint NOT NULL CHECK (maximum_upload_bonus_bps BETWEEN 0 AND 100000),
    maximum_download_discount_bps bigint NOT NULL CHECK (maximum_download_discount_bps BETWEEN 0 AND 100000),
    maximum_magic_bonus_bps bigint NOT NULL CHECK (maximum_magic_bonus_bps BETWEEN 0 AND 100000),
    maximum_invite_bonus bigint NOT NULL CHECK (maximum_invite_bonus BETWEEN 0 AND 1000000),
    condition_check_day bigint NOT NULL CHECK (condition_check_day BETWEEN 1 AND 28),
    condition_warning_days bigint NOT NULL CHECK (condition_warning_days BETWEEN 0 AND 365),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 500),
    changed_by uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid,
    created_at timestamptz NOT NULL,
    CHECK (
        (version = 1 AND changed_by IS NULL AND authorization_decision_id IS NULL)
        OR
        (version > 1 AND changed_by IS NOT NULL AND authorization_decision_id IS NOT NULL)
    )
);

INSERT INTO economy.medal_settings_revisions (
    version, enabled, maximum_wear_count, maximum_upload_bonus_bps,
    maximum_download_discount_bps, maximum_magic_bonus_bps,
    maximum_invite_bonus, condition_check_day, condition_warning_days,
    reason, created_at
)
SELECT version, enabled, maximum_wear_count, maximum_upload_bonus_bps,
       maximum_download_discount_bps, maximum_magic_bonus_bps,
       maximum_invite_bonus, condition_check_day, condition_warning_days,
       '勋章全站规则迁移基线；后续修改均追加不可变修订。', updated_at
FROM economy.medal_settings
WHERE singleton;

CREATE TRIGGER medal_settings_revisions_immutable
BEFORE UPDATE OR DELETE ON economy.medal_settings_revisions
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

REVOKE ALL ON economy.medal_settings_revisions FROM PUBLIC;

-- +goose Down

DROP TRIGGER medal_settings_revisions_immutable
    ON economy.medal_settings_revisions;
DROP TABLE economy.medal_settings_revisions;
