-- +goose Up

-- Public readers share this projection so list, latest and detail can never
-- disagree about which immutable revision is visible. PostgreSQL time is the
-- authority for scheduled activation; browser clocks and background jobs are
-- deliberately outside this decision boundary.
CREATE VIEW catalog.public_announcement_projection AS
SELECT
    announcement.id,
    revision.title,
    revision.summary,
    revision.body,
    revision.body_format,
    revision.revision_number AS version,
    effective.published_at,
    greatest(revision.created_at, effective.published_at)::timestamptz AS updated_at
FROM catalog.announcements AS announcement
CROSS JOIN LATERAL (
    SELECT
        CASE
            WHEN announcement.scheduled_for <= CURRENT_TIMESTAMP
                THEN announcement.scheduled_revision_id
            ELSE announcement.published_revision_id
        END AS revision_id,
        CASE
            WHEN announcement.scheduled_for <= CURRENT_TIMESTAMP
                THEN announcement.scheduled_for
            ELSE announcement.published_at
        END::timestamptz AS published_at
) AS effective
JOIN catalog.announcement_revisions AS revision
  ON revision.id = effective.revision_id
WHERE announcement.withdrawn_at IS NULL
  AND effective.published_at IS NOT NULL;

COMMENT ON VIEW catalog.public_announcement_projection IS
    'Current public revision selected by PostgreSQL time; excludes drafts, future-only schedules and withdrawn announcements.';

-- +goose Down

DROP VIEW catalog.public_announcement_projection;
