-- +goose Up
-- NSFW belongs to the category rather than to individual torrents, so staff
-- can reclassify a whole category without rewriting torrent rows. Browsing
-- lists may exclude these categories; direct detail access is unaffected.
ALTER TABLE catalog.categories
    ADD COLUMN nsfw boolean NOT NULL DEFAULT false;

UPDATE catalog.categories
SET nsfw = true
WHERE id = '9kg';

-- +goose Down
ALTER TABLE catalog.categories
    DROP COLUMN nsfw;
