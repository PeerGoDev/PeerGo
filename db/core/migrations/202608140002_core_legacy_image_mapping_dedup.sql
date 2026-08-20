-- +goose Up

-- PtYes can contain two distinct gallery rows whose source files normalize to
-- identical bytes. The live attachment table correctly keeps only one visual
-- per torrent, while this migration-only table must retain every source row as
-- audit evidence. Its primary key already preserves each legacy row identity;
-- the target position therefore cannot also be unique.
ALTER TABLE migration.torrent_image_map
    DROP CONSTRAINT torrent_image_map_torrent_id_position_key;

CREATE INDEX torrent_image_map_torrent_position_idx
    ON migration.torrent_image_map (torrent_id, position);

-- +goose Down

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM migration.torrent_image_map
        GROUP BY torrent_id, position
        HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION
            'cannot restore one-to-one legacy image mapping after content deduplication';
    END IF;
END;
$$;
-- +goose StatementEnd

DROP INDEX migration.torrent_image_map_torrent_position_idx;

ALTER TABLE migration.torrent_image_map
    ADD CONSTRAINT torrent_image_map_torrent_id_position_key
    UNIQUE (torrent_id, position);
