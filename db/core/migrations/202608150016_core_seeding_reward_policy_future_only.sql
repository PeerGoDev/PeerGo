-- +goose Up

-- Runtime signing owns created_at and requires a genuinely future UTC hour;
-- equality is retained only in the previous migration's rollout shape and is
-- tightened here so an immediately-effective policy cannot reinterpret a
-- window that is already being closed.
ALTER TABLE economy.seeding_reward_policy_revisions
    DROP CONSTRAINT seeding_reward_policy_revisions_check,
    ADD CONSTRAINT seeding_reward_policy_future_effective_check
        CHECK (created_at < effective_from);

-- +goose Down

ALTER TABLE economy.seeding_reward_policy_revisions
    DROP CONSTRAINT seeding_reward_policy_future_effective_check,
    ADD CONSTRAINT seeding_reward_policy_revisions_check
        CHECK (created_at <= effective_from);
