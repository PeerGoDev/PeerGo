-- +goose Up

-- Category facets are the canonical replacement for PtYes category
-- attributes.  The first administration slice only exposed option editing,
-- which left operators unable to create or configure the attribute itself.
-- Keep reusable facet definitions normalized while allowing each category to
-- own its display label, availability, requirement and ordering.
ALTER TABLE catalog.category_facets
    ADD COLUMN name_override text
        CHECK (
            name_override IS NULL
            OR char_length(btrim(name_override)) BETWEEN 1 AND 40
        ),
    ADD COLUMN enabled boolean NOT NULL DEFAULT true,
    ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    ADD COLUMN updated_at timestamptz;

UPDATE catalog.category_facets
SET updated_at = created_at;

ALTER TABLE catalog.category_facets
    ALTER COLUMN updated_at SET NOT NULL,
    ADD CONSTRAINT category_facets_updated_after_created_check
        CHECK (updated_at >= created_at);

CREATE TABLE catalog.category_facet_changes (
    id uuid PRIMARY KEY,
    category_id text NOT NULL,
    facet_id text NOT NULL,
    transition text NOT NULL CHECK (transition IN ('created', 'updated')),
    actor_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 500),
    expected_version bigint NOT NULL CHECK (expected_version >= 0),
    resulting_version bigint NOT NULL CHECK (resulting_version > 0),
    before_state jsonb,
    after_state jsonb NOT NULL,
    authorization_decision_id uuid NOT NULL,
    occurred_at timestamptz NOT NULL,
    FOREIGN KEY (category_id, facet_id)
        REFERENCES catalog.category_facets (category_id, facet_id)
        ON DELETE RESTRICT,
    CHECK (
        (transition = 'created' AND expected_version = 0
            AND resulting_version = 1 AND before_state IS NULL)
        OR
        (transition = 'updated' AND expected_version > 0
            AND resulting_version = expected_version + 1 AND before_state IS NOT NULL)
    )
);

CREATE INDEX category_facet_changes_history_idx
    ON catalog.category_facet_changes (
        category_id, facet_id, occurred_at DESC, id DESC
    );

-- +goose StatementBegin
CREATE FUNCTION catalog.reject_category_facet_change_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'category facet change history is immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER category_facet_changes_immutable
BEFORE UPDATE OR DELETE ON catalog.category_facet_changes
FOR EACH ROW EXECUTE FUNCTION catalog.reject_category_facet_change_mutation();

REVOKE ALL ON catalog.category_facet_changes FROM PUBLIC;

-- +goose Down

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM catalog.category_facet_changes) THEN
        RAISE EXCEPTION 'cannot roll back category facet administration after audited changes';
    END IF;
END;
$$;

DROP TRIGGER category_facet_changes_immutable
    ON catalog.category_facet_changes;
DROP FUNCTION catalog.reject_category_facet_change_mutation();
DROP TABLE catalog.category_facet_changes;

ALTER TABLE catalog.category_facets
    DROP CONSTRAINT category_facets_updated_after_created_check,
    DROP COLUMN updated_at,
    DROP COLUMN version,
    DROP COLUMN enabled,
    DROP COLUMN name_override;
