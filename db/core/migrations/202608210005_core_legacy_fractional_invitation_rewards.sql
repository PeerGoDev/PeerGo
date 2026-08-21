-- +goose Up

-- PtYes awarded some harem entries as very small fractional magic amounts.
-- Preserve their exact positive aggregate while allowing PeerGo's integer
-- presentation of that per-user reward kind to round to zero.
ALTER TABLE migration.legacy_invitation_reward_openings
    DROP CONSTRAINT legacy_invitation_reward_openings_rounded_amount_check;

ALTER TABLE migration.legacy_invitation_reward_openings
    ADD CONSTRAINT legacy_invitation_reward_openings_rounded_amount_check
    CHECK (rounded_amount >= 0);

-- +goose Down

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM migration.legacy_invitation_reward_openings
        WHERE rounded_amount = 0
    ) THEN
        RAISE EXCEPTION '202608210005 cannot roll back after sub-unit invitation reward evidence was recorded';
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE migration.legacy_invitation_reward_openings
    DROP CONSTRAINT legacy_invitation_reward_openings_rounded_amount_check;

ALTER TABLE migration.legacy_invitation_reward_openings
    ADD CONSTRAINT legacy_invitation_reward_openings_rounded_amount_check
    CHECK (rounded_amount > 0);
