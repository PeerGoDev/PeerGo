-- +goose Up

-- A normalized upload may satisfy one familiar PtYes field through more than
-- one PeerGo facet. For example, “来源” can be a physical source medium or a
-- release type. A named group expresses “at least one member” without making
-- either normalized facet individually mandatory.
ALTER TABLE catalog.category_facets
    ADD COLUMN requirement_group text
        CHECK (
            requirement_group IS NULL
            OR requirement_group ~ '^[a-z0-9][a-z0-9-]{0,63}$'
        ),
    ADD CONSTRAINT category_facets_requirement_kind_valid CHECK (
        NOT (required AND requirement_group IS NOT NULL)
    );

UPDATE catalog.category_facets
SET requirement_group = 'source'
WHERE facet_id IN ('source-medium', 'release-type')
  AND category_id IN (
      'movies', 'tv', 'documentary', 'anime', 'variety', 'sports'
  );

-- +goose Down

ALTER TABLE catalog.category_facets
    DROP CONSTRAINT category_facets_requirement_kind_valid,
    DROP COLUMN requirement_group;
