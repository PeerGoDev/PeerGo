-- +goose Up

-- Existing upload SQL intentionally omits lifecycle bookkeeping columns. Keep
-- those writers provider-neutral by assigning transaction time at the storage
-- boundary; explicit migration writes may still supply their verified time.
ALTER TABLE torrents.torrent_screenshot_object_locations
    ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE identity.user_avatar_object_locations
    ALTER COLUMN created_at SET DEFAULT now(),
    ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE media.image_derivative_object_locations
    ALTER COLUMN created_at SET DEFAULT now(),
    ALTER COLUMN updated_at SET DEFAULT now();

-- +goose Down

ALTER TABLE media.image_derivative_object_locations
    ALTER COLUMN updated_at DROP DEFAULT,
    ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE identity.user_avatar_object_locations
    ALTER COLUMN updated_at DROP DEFAULT,
    ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE torrents.torrent_screenshot_object_locations
    ALTER COLUMN updated_at DROP DEFAULT;
