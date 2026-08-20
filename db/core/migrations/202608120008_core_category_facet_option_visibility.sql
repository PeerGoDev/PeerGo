-- +goose Up

-- A controlled option may remain referenced by historical/migrated torrents
-- while no longer appearing in a new-upload selector. Visibility therefore
-- belongs to the category binding and must not delete or globally disable the
-- canonical option identity.
ALTER TABLE catalog.category_facet_options
    ADD COLUMN enabled boolean NOT NULL DEFAULT true;

UPDATE catalog.category_facet_options
SET enabled = false
WHERE category_id = 'movies'
  AND facet_id IN ('source-medium', 'release-type');

UPDATE catalog.category_facet_options
SET enabled = true
WHERE category_id = 'movies'
  AND (
      (facet_id = 'source-medium' AND option_key IN (
          'blu-ray', 'uhd-blu-ray', 'other'
      ))
      OR
      (facet_id = 'release-type' AND option_key IN (
          'web-dl', 'hdtv', 'dvdrip', 'cam'
      ))
  );

UPDATE catalog.category_facet_options
SET display_order = CASE
        WHEN facet_id = 'source-medium' AND option_key = 'blu-ray' THEN 10
        WHEN facet_id = 'source-medium' AND option_key = 'uhd-blu-ray' THEN 20
        WHEN facet_id = 'release-type' AND option_key = 'web-dl' THEN 30
        WHEN facet_id = 'release-type' AND option_key = 'hdtv' THEN 40
        WHEN facet_id = 'release-type' AND option_key = 'dvdrip' THEN 50
        WHEN facet_id = 'release-type' AND option_key = 'cam' THEN 60
        WHEN facet_id = 'source-medium' AND option_key = 'other' THEN 70
        ELSE display_order
    END
WHERE category_id = 'movies'
  AND enabled = true
  AND facet_id IN ('source-medium', 'release-type');

-- +goose Down

ALTER TABLE catalog.category_facet_options
    DROP COLUMN enabled;
