-- +goose NO TRANSACTION

-- +goose Up
CREATE INDEX CONCURRENTLY seeding_reward_evidence_reseed_pack_idx
    ON economy.seeding_reward_evidence_packs
    USING gin (economy.seeding_reward_pack_seeded_torrents(items));

-- +goose Down
DROP INDEX CONCURRENTLY economy.seeding_reward_evidence_reseed_pack_idx;
