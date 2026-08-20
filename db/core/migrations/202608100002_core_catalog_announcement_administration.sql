-- +goose Up

-- Public announcements and editorial drafts have different visibility rules.
-- Immutable revisions keep unfinished edits out of the public row while a
-- stable aggregate coordinates optimistic concurrency and publication state.
CREATE TABLE catalog.announcement_revisions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    announcement_id text NOT NULL
        REFERENCES catalog.announcements (id) ON DELETE RESTRICT,
    revision_number bigint NOT NULL CHECK (revision_number > 0),
    title text NOT NULL CHECK (char_length(btrim(title)) BETWEEN 1 AND 160),
    summary text NOT NULL CHECK (char_length(btrim(summary)) BETWEEN 1 AND 500),
    body text NOT NULL CHECK (char_length(btrim(body)) BETWEEN 1 AND 20000),
    body_format text NOT NULL CHECK (body_format IN ('plain_text', 'legacy_bbcode')),
    created_by_user_id uuid
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    origin text NOT NULL CHECK (origin IN ('migration', 'development_seed', 'staff')),
    created_at timestamptz NOT NULL,
    UNIQUE (announcement_id, revision_number),
    UNIQUE (id, announcement_id)
);

INSERT INTO catalog.announcement_revisions (
    announcement_id,
    revision_number,
    title,
    summary,
    body,
    body_format,
    origin,
    created_at
)
SELECT
    id,
    version,
    title,
    summary,
    body,
    body_format,
    'migration',
    updated_at
FROM catalog.announcements;

ALTER TABLE catalog.announcements
    ADD COLUMN latest_revision_number bigint NOT NULL DEFAULT 0
        CHECK (latest_revision_number >= 0),
    ADD COLUMN draft_revision_id bigint,
    ADD COLUMN published_revision_id bigint,
    ADD COLUMN scheduled_revision_id bigint,
    ADD COLUMN scheduled_for timestamptz,
    ADD COLUMN withdrawn_at timestamptz;

WITH migrated AS (
    SELECT announcement_id, id AS revision_id, revision_number
    FROM catalog.announcement_revisions
)
UPDATE catalog.announcements AS announcement
SET
    latest_revision_number = migrated.revision_number,
    draft_revision_id = CASE
        WHEN announcement.published_at IS NULL THEN migrated.revision_id
        ELSE NULL
    END,
    published_revision_id = CASE
        WHEN announcement.published_at IS NOT NULL
         AND announcement.published_at <= CURRENT_TIMESTAMP
        THEN migrated.revision_id
        ELSE NULL
    END,
    scheduled_revision_id = CASE
        WHEN announcement.published_at > CURRENT_TIMESTAMP THEN migrated.revision_id
        ELSE NULL
    END,
    scheduled_for = CASE
        WHEN announcement.published_at > CURRENT_TIMESTAMP THEN announcement.published_at
        ELSE NULL
    END,
    published_at = CASE
        WHEN announcement.published_at > CURRENT_TIMESTAMP THEN NULL
        ELSE announcement.published_at
    END
FROM migrated
WHERE migrated.announcement_id = announcement.id;

ALTER TABLE catalog.announcements
    ADD CONSTRAINT announcements_draft_revision_fk
        FOREIGN KEY (draft_revision_id, id)
        REFERENCES catalog.announcement_revisions (id, announcement_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT announcements_published_revision_fk
        FOREIGN KEY (published_revision_id, id)
        REFERENCES catalog.announcement_revisions (id, announcement_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT announcements_scheduled_revision_fk
        FOREIGN KEY (scheduled_revision_id, id)
        REFERENCES catalog.announcement_revisions (id, announcement_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT announcements_schedule_pair_check
        CHECK ((scheduled_revision_id IS NULL) = (scheduled_for IS NULL));

-- Revision rows are editorial evidence. Corrections always append a new row;
-- updates or deletes would make an already reviewed preview impossible to
-- reproduce and are therefore rejected even by ordinary repository code.
-- +goose StatementBegin
CREATE FUNCTION catalog.reject_announcement_revision_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'announcement revisions are immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER announcement_revisions_immutable
BEFORE UPDATE OR DELETE ON catalog.announcement_revisions
FOR EACH ROW EXECUTE FUNCTION catalog.reject_announcement_revision_mutation();

-- Content now lives only in immutable revisions. The aggregate retains the
-- stable route key and publication pointers, rather than a second mutable copy.
DROP VIEW social.comment_target_projection;

ALTER TABLE catalog.announcements
    DROP COLUMN title,
    DROP COLUMN summary,
    DROP COLUMN body,
    DROP COLUMN body_format;

CREATE VIEW catalog.managed_announcement_projection AS
SELECT
    announcement.id,
    editor_revision.title,
    editor_revision.summary,
    editor_revision.body,
    editor_revision.body_format,
    announcement.version,
    editor_revision.revision_number,
    (announcement.draft_revision_id IS NOT NULL)::boolean AS has_unpublished_changes,
    (announcement.published_revision_id IS NOT NULL)::boolean AS has_published_revision,
    (announcement.scheduled_revision_id IS NOT NULL)::boolean AS has_scheduled_revision,
    announcement.published_at,
    announcement.scheduled_for,
    announcement.withdrawn_at,
    announcement.created_at,
    announcement.updated_at
FROM catalog.announcements AS announcement
JOIN catalog.announcement_revisions AS editor_revision
  ON editor_revision.id = COALESCE(
      announcement.draft_revision_id,
      announcement.scheduled_revision_id,
      announcement.published_revision_id
  );

CREATE VIEW social.comment_target_projection AS
SELECT
    binding.thread_id,
    'torrent'::text AS target_kind,
    torrent.public_id::text AS target_key,
    torrent.title AS target_title,
    (torrent.state = 'published') AS target_is_public
FROM torrents.torrents AS torrent
LEFT JOIN social.torrent_comment_threads AS binding
    ON binding.torrent_id = torrent.id
UNION ALL
SELECT
    binding.thread_id,
    'announcement'::text AS target_kind,
    announcement.id AS target_key,
    COALESCE(
        CASE
            WHEN announcement.scheduled_for <= CURRENT_TIMESTAMP
                THEN scheduled_revision.title
            ELSE published_revision.title
        END,
        scheduled_revision.title,
        published_revision.title,
        draft_revision.title
    ) AS target_title,
    (
        announcement.withdrawn_at IS NULL
        AND CASE
            WHEN announcement.scheduled_for <= CURRENT_TIMESTAMP
                THEN announcement.scheduled_revision_id IS NOT NULL
            ELSE announcement.published_revision_id IS NOT NULL
        END
    ) AS target_is_public
FROM catalog.announcements AS announcement
LEFT JOIN catalog.announcement_revisions AS published_revision
    ON published_revision.id = announcement.published_revision_id
LEFT JOIN catalog.announcement_revisions AS scheduled_revision
    ON scheduled_revision.id = announcement.scheduled_revision_id
LEFT JOIN catalog.announcement_revisions AS draft_revision
    ON draft_revision.id = announcement.draft_revision_id
LEFT JOIN social.announcement_comment_threads AS binding
    ON binding.announcement_id = announcement.id;

INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES
    ('announcement.create', '创建公告草稿', 'medium', 'none', 'staff-session', true, true),
    ('announcement.manage.read', '读取公告编辑与版本视图', 'medium', 'none', 'staff-session', true, true),
    ('announcement.publish', '立即或定时发布公告', 'high', 'none', 'staff-session', true, true),
    ('announcement.update', '追加公告草稿版本', 'medium', 'none', 'staff-session', true, true),
    ('announcement.withdraw', '撤回已发布或已排期公告', 'high', 'none', 'staff-session', true, true);

INSERT INTO authz.roles (id, name, description, assignable) VALUES (
    'announcement_manager',
    '公告管理员',
    '维护公告草稿、版本和发布状态；不包含站点设置、评论处置或其他内容管理权限。',
    true
);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('announcement_manager', 'announcement.create'),
    ('announcement_manager', 'announcement.manage.read'),
    ('announcement_manager', 'announcement.publish'),
    ('announcement_manager', 'announcement.update'),
    ('announcement_manager', 'announcement.withdraw');

-- +goose Down

DELETE FROM authz.grants WHERE role_id = 'announcement_manager';
DELETE FROM authz.role_permissions WHERE role_id = 'announcement_manager';
DELETE FROM authz.roles WHERE id = 'announcement_manager';
DELETE FROM authz.permissions WHERE action IN (
    'announcement.create',
    'announcement.manage.read',
    'announcement.publish',
    'announcement.update',
    'announcement.withdraw'
);

DROP VIEW social.comment_target_projection;
DROP VIEW catalog.managed_announcement_projection;

ALTER TABLE catalog.announcements
    ADD COLUMN title text,
    ADD COLUMN summary text,
    ADD COLUMN body text,
    ADD COLUMN body_format text;

WITH selected_revision AS (
    SELECT
        announcement.id AS announcement_id,
        revision.title,
        revision.summary,
        revision.body,
        revision.body_format
    FROM catalog.announcements AS announcement
    JOIN catalog.announcement_revisions AS revision
      ON revision.id = COALESCE(
          announcement.draft_revision_id,
          announcement.scheduled_revision_id,
          announcement.published_revision_id,
          (
              SELECT fallback.id
              FROM catalog.announcement_revisions AS fallback
              WHERE fallback.announcement_id = announcement.id
              ORDER BY fallback.revision_number DESC
              LIMIT 1
          )
      )
)
UPDATE catalog.announcements AS announcement
SET
    title = selected_revision.title,
    summary = selected_revision.summary,
    body = selected_revision.body,
    body_format = selected_revision.body_format,
    published_at = CASE
        WHEN announcement.withdrawn_at IS NOT NULL THEN NULL
        WHEN announcement.scheduled_revision_id IS NOT NULL THEN announcement.scheduled_for
        ELSE announcement.published_at
    END
FROM selected_revision
WHERE selected_revision.announcement_id = announcement.id;

ALTER TABLE catalog.announcements
    ALTER COLUMN title SET NOT NULL,
    ALTER COLUMN summary SET NOT NULL,
    ALTER COLUMN body SET NOT NULL,
    ALTER COLUMN body_format SET NOT NULL,
    ADD CONSTRAINT announcements_title_check
        CHECK (char_length(title) BETWEEN 1 AND 160),
    ADD CONSTRAINT announcements_summary_check
        CHECK (char_length(summary) BETWEEN 1 AND 500),
    ADD CONSTRAINT announcements_body_check
        CHECK (char_length(btrim(body)) BETWEEN 1 AND 20000),
    ADD CONSTRAINT announcements_body_format_check
        CHECK (body_format IN ('plain_text', 'legacy_bbcode'));

CREATE VIEW social.comment_target_projection AS
SELECT
    binding.thread_id,
    'torrent'::text AS target_kind,
    torrent.public_id::text AS target_key,
    torrent.title AS target_title,
    (torrent.state = 'published') AS target_is_public
FROM torrents.torrents AS torrent
LEFT JOIN social.torrent_comment_threads AS binding
    ON binding.torrent_id = torrent.id
UNION ALL
SELECT
    binding.thread_id,
    'announcement'::text AS target_kind,
    announcement.id AS target_key,
    announcement.title AS target_title,
    (
        announcement.published_at IS NOT NULL
        AND announcement.published_at <= CURRENT_TIMESTAMP
    ) AS target_is_public
FROM catalog.announcements AS announcement
LEFT JOIN social.announcement_comment_threads AS binding
    ON binding.announcement_id = announcement.id;

ALTER TABLE catalog.announcements
    DROP CONSTRAINT announcements_schedule_pair_check,
    DROP CONSTRAINT announcements_scheduled_revision_fk,
    DROP CONSTRAINT announcements_published_revision_fk,
    DROP CONSTRAINT announcements_draft_revision_fk,
    DROP COLUMN withdrawn_at,
    DROP COLUMN scheduled_for,
    DROP COLUMN scheduled_revision_id,
    DROP COLUMN published_revision_id,
    DROP COLUMN draft_revision_id,
    DROP COLUMN latest_revision_number;

DROP TRIGGER announcement_revisions_immutable ON catalog.announcement_revisions;
DROP FUNCTION catalog.reject_announcement_revision_mutation();
DROP TABLE catalog.announcement_revisions;
