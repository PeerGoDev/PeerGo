-- +goose NO TRANSACTION
-- +goose Up
-- Deduplicated status keys keep exact historical counts small and index-only.
CREATE INDEX CONCURRENTLY seeding_reward_work_terminal_counts_idx
    ON economy.seeding_reward_work_items(status) WHERE status IN('completed','dead');
CREATE INDEX CONCURRENTLY seeding_reward_work_last_error_idx
    ON economy.seeding_reward_work_items(last_error_at DESC) WHERE last_error_code IS NOT NULL;
-- +goose Down
DROP INDEX CONCURRENTLY economy.seeding_reward_work_last_error_idx;
DROP INDEX CONCURRENTLY economy.seeding_reward_work_terminal_counts_idx;
