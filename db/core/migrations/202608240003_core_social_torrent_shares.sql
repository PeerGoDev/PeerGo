-- +goose Up

-- PtYes stored a real optional torrent association on a social post. Restore
-- that domain boundary so a share remains a card instead of synthetic text.
DROP TRIGGER social_post_history_protected ON social.posts;

ALTER TABLE social.posts
    DROP CONSTRAINT social_posts_state_body_check,
    ADD COLUMN torrent_id bigint,
    ADD CONSTRAINT social_posts_torrent_fk
        FOREIGN KEY (torrent_id) REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    ADD CONSTRAINT social_posts_state_body_check
        CHECK (
            (state IN ('visible', 'moderator_hidden')
                AND char_length(btrim(body)) <= 2000
                AND (char_length(btrim(body)) > 0 OR torrent_id IS NOT NULL)
                AND deleted_at IS NULL)
            OR
            (state = 'author_deleted' AND body = '' AND deleted_at IS NOT NULL)
        );

ALTER TABLE social.post_revisions
    DROP CONSTRAINT post_revisions_body_check,
    ADD CONSTRAINT post_revisions_body_check
        CHECK (char_length(btrim(body)) BETWEEN 0 AND 2000);

-- Normalize shares created by the temporary PeerGo text-only implementation.
-- The original body is retained as immutable history; the web projection strips
-- only this exact generated suffix once the real association is present.
WITH legacy_share AS (
    SELECT post.id,
           substring(post.body FROM E'/torrents/([1-9][0-9]{0,17})$')::bigint AS torrent_id
    FROM social.posts AS post
    WHERE post.torrent_id IS NULL
      AND post.body ~ E'(^|\\n\\n)分享种子：[^\\n]+\\n\\n/torrents/[1-9][0-9]{0,17}$'
)
UPDATE social.posts AS post
SET torrent_id = legacy_share.torrent_id
FROM legacy_share
JOIN torrents.torrents AS torrent ON torrent.id = legacy_share.torrent_id
WHERE post.id = legacy_share.id;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION social.protect_post_history()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    is_author_edit boolean;
    is_author_delete boolean;
    is_staff_change boolean;
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'social posts must be tombstoned, not deleted';
    END IF;
    IF OLD.id IS DISTINCT FROM NEW.id
        OR OLD.public_id IS DISTINCT FROM NEW.public_id
        OR OLD.author_id IS DISTINCT FROM NEW.author_id
        OR OLD.create_request_id IS DISTINCT FROM NEW.create_request_id
        OR OLD.create_body_sha256 IS DISTINCT FROM NEW.create_body_sha256
        OR OLD.body_format IS DISTINCT FROM NEW.body_format
        OR OLD.torrent_id IS DISTINCT FROM NEW.torrent_id
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'social post identity is immutable';
    END IF;
    IF OLD.state = 'author_deleted'
        OR NEW.version <> OLD.version + 1
        OR NEW.updated_at < OLD.updated_at THEN
        RAISE EXCEPTION 'social post transition is invalid';
    END IF;

    is_author_edit :=
        OLD.state = 'visible' AND NEW.state = 'visible'
        AND NEW.body IS DISTINCT FROM OLD.body
        AND NEW.board_id IS NOT DISTINCT FROM OLD.board_id
        AND NEW.is_pinned IS NOT DISTINCT FROM OLD.is_pinned
        AND NEW.is_featured IS NOT DISTINCT FROM OLD.is_featured
        AND NEW.edited_at IS NOT DISTINCT FROM NEW.updated_at
        AND NEW.deleted_at IS NULL
        AND NEW.moderated_at IS NOT DISTINCT FROM OLD.moderated_at
        AND NEW.moderated_by IS NOT DISTINCT FROM OLD.moderated_by;

    is_author_delete :=
        OLD.state = 'visible' AND NEW.state = 'author_deleted'
        AND NEW.body = ''
        AND NEW.board_id IS NOT DISTINCT FROM OLD.board_id
        AND NEW.is_pinned IS NOT DISTINCT FROM OLD.is_pinned
        AND NEW.is_featured IS NOT DISTINCT FROM OLD.is_featured
        AND NEW.deleted_at IS NOT DISTINCT FROM NEW.updated_at
        AND NEW.edited_at IS NOT DISTINCT FROM OLD.edited_at
        AND NEW.moderated_at IS NOT DISTINCT FROM OLD.moderated_at
        AND NEW.moderated_by IS NOT DISTINCT FROM OLD.moderated_by;

    is_staff_change :=
        OLD.state IN ('visible', 'moderator_hidden')
        AND NEW.state IN ('visible', 'moderator_hidden')
        AND NEW.body IS NOT DISTINCT FROM OLD.body
        AND NEW.edited_at IS NOT DISTINCT FROM OLD.edited_at
        AND NEW.deleted_at IS NOT DISTINCT FROM OLD.deleted_at
        AND NEW.moderated_at IS NOT NULL
        AND NEW.moderated_at IS NOT DISTINCT FROM NEW.updated_at
        AND NEW.moderated_by IS NOT NULL
        AND (
            NEW.state IS DISTINCT FROM OLD.state
            OR NEW.board_id IS DISTINCT FROM OLD.board_id
            OR NEW.is_pinned IS DISTINCT FROM OLD.is_pinned
            OR NEW.is_featured IS DISTINCT FROM OLD.is_featured
        );

    IF NOT (is_author_edit OR is_author_delete OR is_staff_change) THEN
        RAISE EXCEPTION 'invalid social post transition';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER social_post_history_protected
BEFORE UPDATE OR DELETE ON social.posts
FOR EACH ROW EXECUTE FUNCTION social.protect_post_history();

-- +goose Down

DROP TRIGGER social_post_history_protected ON social.posts;
DROP TRIGGER social_post_revisions_immutable ON social.post_revisions;

-- The old schema cannot represent a card-only share. Preserve a readable
-- tombstone-safe body if an operator explicitly rolls this migration back.
UPDATE social.posts
SET body = '分享了一个种子'
WHERE state IN ('visible', 'moderator_hidden') AND btrim(body) = '';

UPDATE social.post_revisions
SET body = '分享了一个种子'
WHERE btrim(body) = '';

ALTER TABLE social.post_revisions
    DROP CONSTRAINT post_revisions_body_check,
    ADD CONSTRAINT post_revisions_body_check
        CHECK (char_length(btrim(body)) BETWEEN 1 AND 2000);

CREATE TRIGGER social_post_revisions_immutable
BEFORE UPDATE OR DELETE ON social.post_revisions
FOR EACH ROW EXECUTE FUNCTION social.reject_post_revision_mutation();

ALTER TABLE social.posts
    DROP CONSTRAINT social_posts_state_body_check,
    DROP CONSTRAINT social_posts_torrent_fk,
    DROP COLUMN torrent_id,
    ADD CONSTRAINT social_posts_state_body_check
        CHECK (
            (state IN ('visible', 'moderator_hidden')
                AND char_length(btrim(body)) BETWEEN 1 AND 2000
                AND deleted_at IS NULL)
            OR
            (state = 'author_deleted' AND body = '' AND deleted_at IS NOT NULL)
        );

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION social.protect_post_history()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    is_author_edit boolean;
    is_author_delete boolean;
    is_staff_change boolean;
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'social posts must be tombstoned, not deleted';
    END IF;
    IF OLD.id IS DISTINCT FROM NEW.id
        OR OLD.public_id IS DISTINCT FROM NEW.public_id
        OR OLD.author_id IS DISTINCT FROM NEW.author_id
        OR OLD.create_request_id IS DISTINCT FROM NEW.create_request_id
        OR OLD.create_body_sha256 IS DISTINCT FROM NEW.create_body_sha256
        OR OLD.body_format IS DISTINCT FROM NEW.body_format
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'social post identity is immutable';
    END IF;
    IF OLD.state = 'author_deleted'
        OR NEW.version <> OLD.version + 1
        OR NEW.updated_at < OLD.updated_at THEN
        RAISE EXCEPTION 'social post transition is invalid';
    END IF;

    is_author_edit :=
        OLD.state = 'visible' AND NEW.state = 'visible'
        AND NEW.body IS DISTINCT FROM OLD.body
        AND NEW.board_id IS NOT DISTINCT FROM OLD.board_id
        AND NEW.is_pinned IS NOT DISTINCT FROM OLD.is_pinned
        AND NEW.is_featured IS NOT DISTINCT FROM OLD.is_featured
        AND NEW.edited_at IS NOT DISTINCT FROM NEW.updated_at
        AND NEW.deleted_at IS NULL
        AND NEW.moderated_at IS NOT DISTINCT FROM OLD.moderated_at
        AND NEW.moderated_by IS NOT DISTINCT FROM OLD.moderated_by;

    is_author_delete :=
        OLD.state = 'visible' AND NEW.state = 'author_deleted'
        AND NEW.body = ''
        AND NEW.board_id IS NOT DISTINCT FROM OLD.board_id
        AND NEW.is_pinned IS NOT DISTINCT FROM OLD.is_pinned
        AND NEW.is_featured IS NOT DISTINCT FROM OLD.is_featured
        AND NEW.deleted_at IS NOT DISTINCT FROM NEW.updated_at
        AND NEW.edited_at IS NOT DISTINCT FROM OLD.edited_at
        AND NEW.moderated_at IS NOT DISTINCT FROM OLD.moderated_at
        AND NEW.moderated_by IS NOT DISTINCT FROM OLD.moderated_by;

    is_staff_change :=
        OLD.state IN ('visible', 'moderator_hidden')
        AND NEW.state IN ('visible', 'moderator_hidden')
        AND NEW.body IS NOT DISTINCT FROM OLD.body
        AND NEW.edited_at IS NOT DISTINCT FROM OLD.edited_at
        AND NEW.deleted_at IS NOT DISTINCT FROM OLD.deleted_at
        AND NEW.moderated_at IS NOT NULL
        AND NEW.moderated_at IS NOT DISTINCT FROM NEW.updated_at
        AND NEW.moderated_by IS NOT NULL
        AND (
            NEW.state IS DISTINCT FROM OLD.state
            OR NEW.board_id IS DISTINCT FROM OLD.board_id
            OR NEW.is_pinned IS DISTINCT FROM OLD.is_pinned
            OR NEW.is_featured IS DISTINCT FROM OLD.is_featured
        );

    IF NOT (is_author_edit OR is_author_delete OR is_staff_change) THEN
        RAISE EXCEPTION 'invalid social post transition';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER social_post_history_protected
BEFORE UPDATE OR DELETE ON social.posts
FOR EACH ROW EXECUTE FUNCTION social.protect_post_history();
