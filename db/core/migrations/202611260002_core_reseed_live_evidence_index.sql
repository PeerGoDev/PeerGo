-- +goose NO TRANSACTION

-- +goose Up
CREATE INDEX CONCURRENTLY seeding_reward_evidence_reseed_torrent_idx
    ON economy.seeding_reward_evidence_items (torrent_id, user_id)
    WHERE active_seconds > 0;

-- +goose Down
DROP INDEX CONCURRENTLY economy.seeding_reward_evidence_reseed_torrent_idx;
