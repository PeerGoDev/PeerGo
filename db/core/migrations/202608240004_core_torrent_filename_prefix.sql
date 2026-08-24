-- +goose Up
-- Restore the operator-controlled download filename prefix from the legacy
-- Rousi settings model.  It belongs to the bounded, audited site-profile
-- singleton rather than a generic key/value table, and is read for every
-- ordinary or RSS torrent download so a saved change takes effect at once.
ALTER TABLE catalog.site_profile
    ADD COLUMN torrent_filename_prefix text NOT NULL DEFAULT '[ROUSI]'
        CHECK (char_length(torrent_filename_prefix) <= 40);

-- +goose Down
ALTER TABLE catalog.site_profile
    DROP COLUMN torrent_filename_prefix;
