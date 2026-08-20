-- +goose Up

-- Announcements were initially a compact home-page projection. Promote them to
-- a real public object without discarding the stable legacy-compatible text ID:
-- numeric PtYes IDs and PeerGo slugs can both remain canonical route keys.
ALTER TABLE catalog.announcements
    ADD COLUMN body text,
    ADD COLUMN body_format text NOT NULL DEFAULT 'plain_text',
    ADD COLUMN version bigint NOT NULL DEFAULT 1;

UPDATE catalog.announcements
SET body = summary
WHERE body IS NULL;

ALTER TABLE catalog.announcements
    ALTER COLUMN body SET NOT NULL,
    ADD CONSTRAINT announcements_id_route_key_check
        CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,119}$'),
    ADD CONSTRAINT announcements_body_check
        CHECK (char_length(btrim(body)) BETWEEN 1 AND 20000),
    ADD CONSTRAINT announcements_body_format_check
        CHECK (body_format IN ('plain_text', 'legacy_bbcode')),
    ADD CONSTRAINT announcements_version_check
        CHECK (version > 0);

ALTER TABLE social.comment_threads
    DROP CONSTRAINT comment_threads_target_kind_check,
    ADD CONSTRAINT comment_threads_target_kind_check
        CHECK (target_kind IN ('torrent', 'announcement'));

-- Each target kind receives its own binding table and real foreign key. The
-- social module therefore shares comments without accepting an unchecked
-- target_type/target_id pair.
CREATE TABLE social.announcement_comment_threads (
    thread_id bigint PRIMARY KEY,
    target_kind text NOT NULL DEFAULT 'announcement'
        CHECK (target_kind = 'announcement'),
    announcement_id text NOT NULL UNIQUE
        REFERENCES catalog.announcements (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL,
    FOREIGN KEY (thread_id, target_kind)
        REFERENCES social.comment_threads (id, target_kind) ON DELETE RESTRICT
);

-- This read-only projection is the only polymorphic surface. Persisted target
-- identity remains in the typed binding tables above; shared comment/report
-- queries use the projection to avoid duplicating target-independent logic.
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

-- Moderation decisions retain explicit target columns. Existing rows are
-- torrent decisions; the default backfills them without mutating immutable
-- evidence through its protection trigger.
ALTER TABLE social.comment_moderation_decisions
    ADD COLUMN target_kind text NOT NULL DEFAULT 'torrent',
    ADD COLUMN announcement_id text,
    ALTER COLUMN torrent_public_id DROP NOT NULL;

ALTER TABLE social.comment_moderation_decisions
    ALTER COLUMN target_kind DROP DEFAULT,
    ADD CONSTRAINT comment_moderation_decisions_target_kind_check
        CHECK (target_kind IN ('torrent', 'announcement')),
    ADD CONSTRAINT comment_moderation_decisions_target_identity_check
        CHECK (
            (target_kind = 'torrent'
                AND torrent_public_id IS NOT NULL
                AND announcement_id IS NULL)
            OR
            (target_kind = 'announcement'
                AND torrent_public_id IS NULL
                AND announcement_id IS NOT NULL)
        ),
    ADD CONSTRAINT comment_moderation_decisions_announcement_fk
        FOREIGN KEY (announcement_id)
        REFERENCES catalog.announcements (id) ON DELETE RESTRICT;

INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES (
    'announcement.comment.create.self',
    '在已发布公告下发表评论',
    'low',
    'self',
    'web-session',
    true,
    true
);

INSERT INTO authz.role_permissions (role_id, action)
VALUES ('member', 'announcement.comment.create.self');

-- +goose Down

DELETE FROM authz.role_permissions
WHERE role_id = 'member'
  AND action = 'announcement.comment.create.self';

DELETE FROM authz.permissions
WHERE action = 'announcement.comment.create.self';

ALTER TABLE social.comment_moderation_decisions
    DROP CONSTRAINT comment_moderation_decisions_announcement_fk,
    DROP CONSTRAINT comment_moderation_decisions_target_identity_check,
    DROP CONSTRAINT comment_moderation_decisions_target_kind_check,
    ALTER COLUMN torrent_public_id SET NOT NULL,
    DROP COLUMN announcement_id,
    DROP COLUMN target_kind;

DROP VIEW social.comment_target_projection;
DROP TABLE social.announcement_comment_threads;

-- Empty announcement threads can be removed safely. A populated discussion
-- intentionally makes rollback fail through the comments foreign key instead
-- of silently discarding history.
DELETE FROM social.comment_threads AS thread
WHERE thread.target_kind = 'announcement'
  AND NOT EXISTS (
      SELECT 1
      FROM social.comments AS comment
      WHERE comment.thread_id = thread.id
  );

ALTER TABLE social.comment_threads
    DROP CONSTRAINT comment_threads_target_kind_check,
    ADD CONSTRAINT comment_threads_target_kind_check
        CHECK (target_kind IN ('torrent'));

ALTER TABLE catalog.announcements
    DROP CONSTRAINT announcements_version_check,
    DROP CONSTRAINT announcements_body_format_check,
    DROP CONSTRAINT announcements_body_check,
    DROP CONSTRAINT announcements_id_route_key_check,
    DROP COLUMN version,
    DROP COLUMN body_format,
    DROP COLUMN body;
