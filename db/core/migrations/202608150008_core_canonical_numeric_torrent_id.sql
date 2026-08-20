-- +goose Up

-- A torrent has exactly one canonical identity: torrents.torrents.id. PtYes
-- imports preserve the legacy positive ID and native PeerGo uploads use the
-- same identity sequence. UUIDs remain valid for storage objects, commands and
-- audit events, but no UUID is retained as a second torrent identifier.

DROP VIEW social.comment_target_projection;

-- Convert the public catalog projection and its dependent counters/bookmarks
-- while the old UUID projection is still available for a lossless join.
ALTER TABLE catalog.torrent_bookmarks
    DROP CONSTRAINT torrent_bookmarks_torrent_id_fkey,
    DROP CONSTRAINT torrent_bookmarks_pkey,
    DROP CONSTRAINT torrent_bookmarks_torrent_id_check;
ALTER TABLE catalog.torrent_swarm_stats
    DROP CONSTRAINT torrent_swarm_stats_torrent_id_fkey,
    DROP CONSTRAINT torrent_swarm_stats_pkey;
ALTER TABLE catalog.torrent_completion_stats
    DROP CONSTRAINT torrent_completion_stats_torrent_id_fkey,
    DROP CONSTRAINT torrent_completion_stats_pkey;
ALTER TABLE catalog.torrents
    DROP CONSTRAINT torrents_pkey;

ALTER TABLE catalog.torrents ADD COLUMN canonical_id bigint;
UPDATE catalog.torrents AS projection
SET canonical_id = aggregate.id
FROM torrents.torrents AS aggregate
WHERE aggregate.public_id::text = projection.id;

ALTER TABLE catalog.torrent_bookmarks ADD COLUMN canonical_torrent_id bigint;
UPDATE catalog.torrent_bookmarks AS bookmark
SET canonical_torrent_id = projection.canonical_id
FROM catalog.torrents AS projection
WHERE projection.id = bookmark.torrent_id;

ALTER TABLE catalog.torrent_swarm_stats ADD COLUMN canonical_torrent_id bigint;
UPDATE catalog.torrent_swarm_stats AS swarm
SET canonical_torrent_id = projection.canonical_id
FROM catalog.torrents AS projection
WHERE projection.id = swarm.torrent_id;

ALTER TABLE catalog.torrent_completion_stats ADD COLUMN canonical_torrent_id bigint;
UPDATE catalog.torrent_completion_stats AS completion
SET canonical_torrent_id = projection.canonical_id
FROM catalog.torrents AS projection
WHERE projection.id = completion.torrent_id;

ALTER TABLE catalog.torrents
    ALTER COLUMN canonical_id SET NOT NULL,
    DROP COLUMN id;
ALTER TABLE catalog.torrents RENAME COLUMN canonical_id TO id;
ALTER TABLE catalog.torrents
    ADD CONSTRAINT torrents_pkey PRIMARY KEY (id),
    ADD CONSTRAINT torrents_id_positive CHECK (id > 0);
CREATE INDEX torrents_latest_published_idx
    ON catalog.torrents (published_at DESC, id DESC);

ALTER TABLE catalog.torrent_bookmarks
    ALTER COLUMN canonical_torrent_id SET NOT NULL,
    DROP COLUMN torrent_id;
ALTER TABLE catalog.torrent_bookmarks RENAME COLUMN canonical_torrent_id TO torrent_id;
ALTER TABLE catalog.torrent_bookmarks
    ADD CONSTRAINT torrent_bookmarks_pkey PRIMARY KEY (user_id, torrent_id),
    ADD CONSTRAINT torrent_bookmarks_torrent_id_fkey
        FOREIGN KEY (torrent_id) REFERENCES catalog.torrents (id) ON DELETE CASCADE,
    ADD CONSTRAINT torrent_bookmarks_torrent_id_positive CHECK (torrent_id > 0);
CREATE INDEX torrent_bookmarks_user_recent_idx
    ON catalog.torrent_bookmarks (user_id, created_at DESC, torrent_id DESC);

ALTER TABLE catalog.torrent_swarm_stats
    ALTER COLUMN canonical_torrent_id SET NOT NULL,
    DROP COLUMN torrent_id;
ALTER TABLE catalog.torrent_swarm_stats RENAME COLUMN canonical_torrent_id TO torrent_id;
ALTER TABLE catalog.torrent_swarm_stats
    ADD CONSTRAINT torrent_swarm_stats_pkey PRIMARY KEY (torrent_id),
    ADD CONSTRAINT torrent_swarm_stats_torrent_id_fkey
        FOREIGN KEY (torrent_id) REFERENCES catalog.torrents (id) ON DELETE CASCADE,
    ADD CONSTRAINT torrent_swarm_stats_torrent_id_positive CHECK (torrent_id > 0);

ALTER TABLE catalog.torrent_completion_stats
    ALTER COLUMN canonical_torrent_id SET NOT NULL,
    DROP COLUMN torrent_id;
ALTER TABLE catalog.torrent_completion_stats RENAME COLUMN canonical_torrent_id TO torrent_id;
ALTER TABLE catalog.torrent_completion_stats
    ADD CONSTRAINT torrent_completion_stats_pkey PRIMARY KEY (torrent_id),
    ADD CONSTRAINT torrent_completion_stats_torrent_id_fkey
        FOREIGN KEY (torrent_id) REFERENCES catalog.torrents (id) ON DELETE CASCADE,
    ADD CONSTRAINT torrent_completion_stats_torrent_id_positive CHECK (torrent_id > 0);

-- Replace UUID-only references with the already canonical numeric ID.
ALTER TABLE community.torrent_review_notifications ADD COLUMN torrent_id bigint;
UPDATE community.torrent_review_notifications AS notification
SET torrent_id = aggregate.id
FROM torrents.torrents AS aggregate
WHERE aggregate.public_id = notification.torrent_public_id;
ALTER TABLE community.torrent_review_notifications
    ALTER COLUMN torrent_id SET NOT NULL,
    ADD CONSTRAINT torrent_review_notifications_torrent_id_fkey
        FOREIGN KEY (torrent_id) REFERENCES torrents.torrents (id) ON DELETE RESTRICT;

ALTER TABLE social.comment_moderation_decisions
    DROP CONSTRAINT comment_moderation_decisions_target_identity_check,
    ADD COLUMN torrent_id bigint;
UPDATE social.comment_moderation_decisions AS decision
SET torrent_id = aggregate.id
FROM torrents.torrents AS aggregate
WHERE aggregate.public_id = decision.torrent_public_id;
ALTER TABLE social.comment_moderation_decisions
    ADD CONSTRAINT comment_moderation_decisions_torrent_id_fkey
        FOREIGN KEY (torrent_id) REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    ADD CONSTRAINT comment_moderation_decisions_target_identity_check CHECK (
        (target_kind = 'torrent' AND torrent_id IS NOT NULL
            AND announcement_id IS NULL AND post_public_id IS NULL)
        OR (target_kind = 'announcement' AND torrent_id IS NULL
            AND announcement_id IS NOT NULL AND post_public_id IS NULL)
        OR (target_kind = 'post' AND torrent_id IS NULL
            AND announcement_id IS NULL AND post_public_id IS NOT NULL)
    );

-- Trigger bodies are replaced before their former UUID columns disappear.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION torrents.protect_torrent_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'torrent aggregates must be tombstoned, not deleted';
    END IF;

    IF OLD.id IS DISTINCT FROM NEW.id
        OR OLD.uploader_id IS DISTINCT FROM NEW.uploader_id
        OR OLD.object_id IS DISTINCT FROM NEW.object_id
        OR OLD.info_hash_v1 IS DISTINCT FROM NEW.info_hash_v1
        OR OLD.content_name IS DISTINCT FROM NEW.content_name
        OR OLD.total_size_bytes IS DISTINCT FROM NEW.total_size_bytes
        OR OLD.payload_size_bytes IS DISTINCT FROM NEW.payload_size_bytes
        OR OLD.file_count IS DISTINCT FROM NEW.file_count
        OR OLD.padding_file_count IS DISTINCT FROM NEW.padding_file_count
        OR OLD.piece_length_bytes IS DISTINCT FROM NEW.piece_length_bytes
        OR OLD.piece_count IS DISTINCT FROM NEW.piece_count
        OR OLD.submitted_at IS DISTINCT FROM NEW.submitted_at THEN
        RAISE EXCEPTION 'torrent swarm identity is immutable';
    END IF;

    IF NEW.version <> OLD.version + 1 THEN
        RAISE EXCEPTION 'torrent aggregate version must increment exactly once';
    END IF;
    IF NEW.updated_at < OLD.updated_at THEN
        RAISE EXCEPTION 'torrent update time cannot move backwards';
    END IF;
    IF OLD.state IS NOT DISTINCT FROM NEW.state THEN
        IF OLD.state_changed_at IS DISTINCT FROM NEW.state_changed_at
            OR OLD.published_at IS DISTINCT FROM NEW.published_at THEN
            RAISE EXCEPTION 'unchanged torrent state cannot rewrite transition times';
        END IF;
        RETURN NEW;
    END IF;
    IF NOT (
        (OLD.state = 'pending_review' AND NEW.state IN ('published', 'rejected', 'deleted'))
        OR (OLD.state = 'published' AND NEW.state IN ('disabled', 'deleted'))
        OR (OLD.state = 'rejected' AND NEW.state IN ('pending_review', 'deleted'))
        OR (OLD.state = 'disabled' AND NEW.state IN ('published', 'deleted'))
    ) THEN
        RAISE EXCEPTION 'torrent state transition from % to % is invalid', OLD.state, NEW.state;
    END IF;
    IF NEW.state_changed_at < OLD.state_changed_at THEN
        RAISE EXCEPTION 'torrent state time cannot move backwards';
    END IF;
    IF OLD.published_at IS NOT NULL
        AND OLD.published_at IS DISTINCT FROM NEW.published_at THEN
        RAISE EXCEPTION 'torrent first publication time is immutable';
    END IF;
    IF OLD.published_at IS NULL AND NEW.state = 'published'
        AND NEW.published_at IS DISTINCT FROM NEW.state_changed_at THEN
        RAISE EXCEPTION 'first publication time must equal its state transition time';
    END IF;
    IF OLD.published_at IS NULL AND NEW.state <> 'published'
        AND NEW.published_at IS NOT NULL THEN
        RAISE EXCEPTION 'publication time cannot appear outside publication';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION torrents.protect_torrent_upload()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'torrent upload evidence must not be deleted';
    END IF;
    IF OLD.id IS DISTINCT FROM NEW.id
        OR OLD.uploader_id IS DISTINCT FROM NEW.uploader_id
        OR OLD.request_fingerprint IS DISTINCT FROM NEW.request_fingerprint
        OR OLD.object_id IS DISTINCT FROM NEW.object_id
        OR OLD.category_id IS DISTINCT FROM NEW.category_id
        OR OLD.info_hash_v1 IS DISTINCT FROM NEW.info_hash_v1
        OR OLD.content_sha256 IS DISTINCT FROM NEW.content_sha256
        OR OLD.byte_length IS DISTINCT FROM NEW.byte_length
        OR OLD.backend_id IS DISTINCT FROM NEW.backend_id
        OR OLD.object_key IS DISTINCT FROM NEW.object_key
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'torrent upload request identity is immutable';
    END IF;
    IF NEW.version <> OLD.version + 1 THEN
        RAISE EXCEPTION 'torrent upload version must increment exactly once';
    END IF;
    IF OLD.state IN ('completed', 'abandoned') THEN
        RAISE EXCEPTION 'terminal torrent upload evidence is immutable';
    END IF;
    IF OLD.object_verified_at IS NOT NULL AND (
        OLD.object_verified_at IS DISTINCT FROM NEW.object_verified_at
        OR OLD.object_created IS DISTINCT FROM NEW.object_created
        OR OLD.storage_version_id IS DISTINCT FROM NEW.storage_version_id
    ) THEN
        RAISE EXCEPTION 'verified torrent upload object observation is immutable';
    END IF;
    IF OLD.state IS DISTINCT FROM NEW.state AND NOT (
        (OLD.state = 'reserved' AND NEW.state IN ('object_verified', 'cleaning'))
        OR (OLD.state = 'object_verified' AND NEW.state IN ('completed', 'cleaning'))
        OR (OLD.state = 'cleaning' AND NEW.state IN ('reserved', 'object_verified', 'abandoned'))
    ) THEN
        RAISE EXCEPTION 'torrent upload transition from % to % is invalid', OLD.state, NEW.state;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION tracker_control.protect_allowlist_projection()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'Tracker allowlist projection uses explicit disabled entries';
    END IF;
    IF OLD.torrent_id IS DISTINCT FROM NEW.torrent_id
        OR OLD.info_hash_v1 IS DISTINCT FROM NEW.info_hash_v1
        OR OLD.total_size_bytes IS DISTINCT FROM NEW.total_size_bytes THEN
        RAISE EXCEPTION 'Tracker allowlist swarm identity is immutable';
    END IF;
    IF NEW.torrent_version <= OLD.torrent_version
        OR NEW.control_sequence <= OLD.control_sequence
        OR NEW.updated_at < OLD.updated_at THEN
        RAISE EXCEPTION 'Tracker allowlist projection must advance monotonically';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION community.protect_torrent_review_notification()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'torrent review notifications cannot be deleted';
    END IF;
    IF OLD.id IS DISTINCT FROM NEW.id
        OR OLD.recipient_user_id IS DISTINCT FROM NEW.recipient_user_id
        OR OLD.review_decision_id IS DISTINCT FROM NEW.review_decision_id
        OR OLD.torrent_id IS DISTINCT FROM NEW.torrent_id
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'torrent review notification source is immutable';
    END IF;
    IF OLD.read_at IS NOT NULL AND OLD.read_at IS DISTINCT FROM NEW.read_at THEN
        RAISE EXCEPTION 'notification read state is monotonic';
    END IF;
    IF OLD.archived_at IS NOT NULL
        AND OLD.archived_at IS DISTINCT FROM NEW.archived_at THEN
        RAISE EXCEPTION 'notification archive state is monotonic';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

ALTER TABLE community.torrent_review_notifications DROP COLUMN torrent_public_id;
ALTER TABLE social.comment_moderation_decisions DROP COLUMN torrent_public_id;
ALTER TABLE review.torrent_decisions DROP COLUMN torrent_public_id;
ALTER TABLE tracker_control.torrent_allowlist_projection DROP COLUMN torrent_public_id;
ALTER TABLE torrents.torrent_uploads DROP COLUMN public_id;
ALTER TABLE migration.torrent_id_map DROP COLUMN public_id;
ALTER TABLE torrents.torrents DROP COLUMN public_id;

CREATE VIEW social.comment_target_projection AS
SELECT
    binding.thread_id,
    'torrent'::text AS target_kind,
    torrent.id::text AS target_key,
    torrent.title AS target_title,
    torrent.state = 'published' AS target_is_public
FROM torrents.torrents AS torrent
LEFT JOIN social.torrent_comment_threads AS binding
  ON binding.torrent_id = torrent.id
UNION ALL
SELECT
    binding.thread_id,
    'announcement'::text AS target_kind,
    announcement.id AS target_key,
    COALESCE(
        CASE WHEN announcement.scheduled_for <= CURRENT_TIMESTAMP
            THEN scheduled_revision.title ELSE published_revision.title END,
        scheduled_revision.title,
        published_revision.title,
        draft_revision.title
    ) AS target_title,
    announcement.withdrawn_at IS NULL
        AND CASE WHEN announcement.scheduled_for <= CURRENT_TIMESTAMP
            THEN announcement.scheduled_revision_id IS NOT NULL
            ELSE announcement.published_revision_id IS NOT NULL END AS target_is_public
FROM catalog.announcements AS announcement
LEFT JOIN catalog.announcement_revisions AS published_revision
  ON published_revision.id = announcement.published_revision_id
LEFT JOIN catalog.announcement_revisions AS scheduled_revision
  ON scheduled_revision.id = announcement.scheduled_revision_id
LEFT JOIN catalog.announcement_revisions AS draft_revision
  ON draft_revision.id = announcement.draft_revision_id
LEFT JOIN social.announcement_comment_threads AS binding
  ON binding.announcement_id = announcement.id
UNION ALL
SELECT
    binding.thread_id,
    'post'::text AS target_kind,
    post.public_id::text AS target_key,
    left(post.body, 160) AS target_title,
    post.state = 'visible' AS target_is_public
FROM social.posts AS post
LEFT JOIN social.post_comment_threads AS binding
  ON binding.post_id = post.id;

-- +goose Down

-- Removing an obsolete public identifier is intentionally irreversible: a
-- rollback cannot recreate the deleted UUID values without restoring a backup.
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION '202608150008 is irreversible; restore a pre-migration backup instead';
END
$$;
-- +goose StatementEnd
