-- +goose Up

-- PtYes has one historical `other` torrent carrying the same controlled
-- metadata dimensions as ordinary video categories. Bind those reusable
-- facets explicitly so the importer can preserve that row without creating a
-- one-off JSON escape hatch or reclassifying it by guesswork.
INSERT INTO catalog.category_facets (
    category_id,
    facet_id,
    selection_mode,
    required,
    display_order,
    created_at
) VALUES
    ('other', 'genre', 'multi_option', false, 10, now()),
    ('other', 'region', 'single_option', false, 20, now()),
    ('other', 'resolution', 'single_option', false, 30, now()),
    ('other', 'source-medium', 'single_option', false, 40, now()),
    ('other', 'release-type', 'single_option', false, 50, now())
ON CONFLICT (category_id, facet_id) DO NOTHING;

INSERT INTO catalog.category_facet_options (
    category_id,
    facet_id,
    option_key,
    selection_mode,
    created_at
)
SELECT
    binding.category_id,
    option.facet_id,
    option.option_key,
    option.selection_mode,
    now()
FROM catalog.category_facets AS binding
JOIN catalog.facet_options AS option
  ON option.facet_id = binding.facet_id
 AND option.selection_mode = binding.selection_mode
WHERE binding.category_id = 'other'
ON CONFLICT (category_id, facet_id, option_key) DO NOTHING;

-- +goose Down

DELETE FROM catalog.category_facet_options
WHERE category_id = 'other'
  AND facet_id IN (
      'genre', 'region', 'resolution', 'source-medium', 'release-type'
  );

DELETE FROM catalog.category_facets
WHERE category_id = 'other'
  AND facet_id IN (
      'genre', 'region', 'resolution', 'source-medium', 'release-type'
  );
