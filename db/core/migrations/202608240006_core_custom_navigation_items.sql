-- +goose Up
-- Custom navigation is important operator-owned configuration, not activity
-- data. Keep it in the existing versioned site singleton and bound both the
-- item count and serialized size so it cannot become an unbounded link store.
ALTER TABLE catalog.site_profile
    ADD COLUMN custom_navigation_items jsonb NOT NULL DEFAULT '[]'::jsonb
        CHECK (
            jsonb_typeof(custom_navigation_items) = 'array'
            AND jsonb_array_length(custom_navigation_items) <= 12
            AND octet_length(custom_navigation_items::text) <= 32768
        );

COMMENT ON COLUMN catalog.site_profile.custom_navigation_items IS
    'Ordered, bounded operator-configured sidebar links; no click or visit history is stored.';

-- +goose Down
ALTER TABLE catalog.site_profile
    DROP COLUMN custom_navigation_items;
