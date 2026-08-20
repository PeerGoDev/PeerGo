-- +goose Up

ALTER TABLE catalog.torrents
    DROP CONSTRAINT torrents_promotion_check;

ALTER TABLE catalog.torrents
    ADD CONSTRAINT torrents_promotion_check CHECK (
        promotion IN (
            'none',
            'free',
            'double_upload',
            'double_upload_free',
            'half_download',
            'double_upload_half_download',
            'thirty_percent_download'
        )
    ),
    ADD COLUMN promotion_ends_at timestamptz;

COMMENT ON COLUMN catalog.torrents.promotion IS
    'Current public display projection only; immutable Tracker settlement never reads this field.';

COMMENT ON COLUMN catalog.torrents.promotion_ends_at IS
    'Optional expiry for the public display projection; NULL means no scheduled expiry.';

-- +goose Down

UPDATE catalog.torrents
SET promotion = 'none'
WHERE promotion NOT IN ('none', 'free');

ALTER TABLE catalog.torrents
    DROP COLUMN promotion_ends_at,
    DROP CONSTRAINT torrents_promotion_check;

ALTER TABLE catalog.torrents
    ADD CONSTRAINT torrents_promotion_check CHECK (promotion IN ('none', 'free'));
