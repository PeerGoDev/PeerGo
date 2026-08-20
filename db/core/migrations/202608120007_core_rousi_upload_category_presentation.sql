-- +goose Up

-- Match the current Rousi upload selector, not an inferred taxonomy. The game
-- center is a separate feature in PtYes; software already carries a controlled
-- “游戏” type, so exposing another top-level upload category changes the form
-- and migration semantics without a source equivalent.
UPDATE catalog.categories
SET name = CASE id
        WHEN 'tv' THEN '电视剧'
        ELSE name
    END,
    display_order = CASE id
        WHEN 'movies' THEN 10
        WHEN 'tv' THEN 20
        WHEN 'documentary' THEN 30
        WHEN 'anime' THEN 40
        WHEN 'music' THEN 50
        WHEN 'variety' THEN 60
        WHEN 'sports' THEN 70
        WHEN 'software' THEN 80
        WHEN 'ebooks' THEN 90
        WHEN '9kg' THEN 100
        WHEN 'other' THEN 110
        ELSE display_order
    END,
    enabled = CASE id
        WHEN 'games' THEN false
        ELSE enabled
    END,
    updated_at = now(),
    version = version + 1
WHERE id IN (
    'movies', 'tv', 'documentary', 'anime', 'music', 'variety',
    'sports', 'games', 'software', 'ebooks', '9kg', 'other'
);

-- +goose Down

UPDATE catalog.categories
SET name = CASE id
        WHEN 'tv' THEN '剧集'
        ELSE name
    END,
    display_order = CASE id
        WHEN 'movies' THEN 10
        WHEN 'tv' THEN 20
        WHEN 'documentary' THEN 30
        WHEN 'anime' THEN 40
        WHEN 'variety' THEN 50
        WHEN 'sports' THEN 60
        WHEN 'music' THEN 70
        WHEN 'games' THEN 80
        WHEN 'software' THEN 90
        WHEN 'ebooks' THEN 100
        WHEN '9kg' THEN 110
        WHEN 'other' THEN 120
        ELSE display_order
    END,
    enabled = CASE id
        WHEN 'games' THEN true
        ELSE enabled
    END,
    updated_at = now(),
    version = version + 1
WHERE id IN (
    'movies', 'tv', 'documentary', 'anime', 'music', 'variety',
    'sports', 'games', 'software', 'ebooks', '9kg', 'other'
);
