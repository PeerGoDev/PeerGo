-- +goose Up

-- A reply keeps its direct parent for precise "replying to" context and also
-- records the owning top-level comment. The latter is structural data used to
-- page whole conversations; it avoids duplicating bodies or storing activity
-- history while making thread reads bounded and indexable.
ALTER TABLE social.comments
    ADD COLUMN root_comment_id bigint;

DROP TRIGGER comment_history_protected ON social.comments;
DROP TRIGGER comment_parent_valid ON social.comments;

WITH RECURSIVE comment_tree AS (
    SELECT comment.id, comment.thread_id, comment.id AS root_comment_id
    FROM social.comments AS comment
    WHERE comment.parent_comment_id IS NULL

    UNION ALL

    SELECT child.id, child.thread_id, tree.root_comment_id
    FROM social.comments AS child
    JOIN comment_tree AS tree
      ON tree.id = child.parent_comment_id
     AND tree.thread_id = child.thread_id
)
UPDATE social.comments AS comment
SET root_comment_id = tree.root_comment_id
FROM comment_tree AS tree
WHERE tree.id = comment.id
  AND comment.parent_comment_id IS NOT NULL;

ALTER TABLE social.comments
    ADD CONSTRAINT comments_root_thread_fk
        FOREIGN KEY (root_comment_id, thread_id)
        REFERENCES social.comments (id, thread_id) ON DELETE RESTRICT,
    ADD CONSTRAINT comments_root_shape_check
        CHECK (
            (parent_comment_id IS NULL AND root_comment_id IS NULL)
            OR
            (parent_comment_id IS NOT NULL AND root_comment_id IS NOT NULL AND root_comment_id <> id)
        );

CREATE INDEX comments_thread_roots_chronological_idx
    ON social.comments (thread_id, created_at, id)
    WHERE root_comment_id IS NULL;

CREATE INDEX comments_root_replies_chronological_idx
    ON social.comments (root_comment_id, created_at, id)
    WHERE root_comment_id IS NOT NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION social.validate_comment_parent()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    parent_state text;
    expected_root_comment_id bigint;
BEGIN
    IF NEW.parent_comment_id IS NULL THEN
        IF NEW.root_comment_id IS NOT NULL THEN
            RAISE EXCEPTION 'top-level comment cannot have a root comment'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    SELECT parent.state, COALESCE(parent.root_comment_id, parent.id)
    INTO parent_state, expected_root_comment_id
    FROM social.comments AS parent
    WHERE parent.id = NEW.parent_comment_id
      AND parent.thread_id = NEW.thread_id;

    IF NOT FOUND OR parent_state <> 'visible' THEN
        RAISE EXCEPTION 'comment parent must be visible in the same thread'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.root_comment_id IS NULL THEN
        NEW.root_comment_id := expected_root_comment_id;
    ELSIF NEW.root_comment_id <> expected_root_comment_id THEN
        RAISE EXCEPTION 'comment root must match the parent conversation'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER comment_parent_valid
BEFORE INSERT ON social.comments
FOR EACH ROW EXECUTE FUNCTION social.validate_comment_parent();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION social.protect_comment_history()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'comments must be tombstoned, not deleted';
    END IF;

    IF OLD.id IS DISTINCT FROM NEW.id
        OR OLD.public_id IS DISTINCT FROM NEW.public_id
        OR OLD.thread_id IS DISTINCT FROM NEW.thread_id
        OR OLD.parent_comment_id IS DISTINCT FROM NEW.parent_comment_id
        OR OLD.root_comment_id IS DISTINCT FROM NEW.root_comment_id
        OR OLD.author_id IS DISTINCT FROM NEW.author_id
        OR OLD.create_request_id IS DISTINCT FROM NEW.create_request_id
        OR OLD.create_body_sha256 IS DISTINCT FROM NEW.create_body_sha256
        OR OLD.body_format IS DISTINCT FROM NEW.body_format
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'comment identity is immutable';
    END IF;
    IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN
        RAISE EXCEPTION 'comment version and update time must advance exactly once';
    END IF;
    IF OLD.state <> 'visible' THEN
        RAISE EXCEPTION 'comment tombstones are terminal';
    END IF;

    IF NEW.state = 'visible' THEN
        IF NEW.body IS NOT DISTINCT FROM OLD.body
            OR NEW.edited_at IS DISTINCT FROM NEW.updated_at
            OR NEW.deleted_at IS NOT NULL THEN
            RAISE EXCEPTION 'visible comment update must be a body edit';
        END IF;
    ELSIF NEW.state IN ('author_deleted', 'moderator_hidden') THEN
        IF NEW.body <> ''
            OR NEW.deleted_at IS DISTINCT FROM NEW.updated_at
            OR NEW.edited_at IS DISTINCT FROM OLD.edited_at THEN
            RAISE EXCEPTION 'comment deletion must create a terminal tombstone';
        END IF;
    ELSE
        RAISE EXCEPTION 'invalid comment state transition';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER comment_history_protected
BEFORE UPDATE OR DELETE ON social.comments
FOR EACH ROW EXECUTE FUNCTION social.protect_comment_history();

-- +goose Down

DROP TRIGGER comment_history_protected ON social.comments;
DROP TRIGGER comment_parent_valid ON social.comments;

DROP INDEX social.comments_root_replies_chronological_idx;
DROP INDEX social.comments_thread_roots_chronological_idx;

ALTER TABLE social.comments
    DROP CONSTRAINT comments_root_shape_check,
    DROP CONSTRAINT comments_root_thread_fk,
    DROP COLUMN root_comment_id;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION social.validate_comment_parent()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    parent_state text;
BEGIN
    IF NEW.parent_comment_id IS NULL THEN
        RETURN NEW;
    END IF;

    SELECT parent.state
    INTO parent_state
    FROM social.comments AS parent
    WHERE parent.id = NEW.parent_comment_id
      AND parent.thread_id = NEW.thread_id;

    IF NOT FOUND OR parent_state <> 'visible' THEN
        RAISE EXCEPTION 'comment parent must be visible in the same thread'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER comment_parent_valid
BEFORE INSERT ON social.comments
FOR EACH ROW EXECUTE FUNCTION social.validate_comment_parent();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION social.protect_comment_history()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'comments must be tombstoned, not deleted';
    END IF;

    IF OLD.id IS DISTINCT FROM NEW.id
        OR OLD.public_id IS DISTINCT FROM NEW.public_id
        OR OLD.thread_id IS DISTINCT FROM NEW.thread_id
        OR OLD.parent_comment_id IS DISTINCT FROM NEW.parent_comment_id
        OR OLD.author_id IS DISTINCT FROM NEW.author_id
        OR OLD.create_request_id IS DISTINCT FROM NEW.create_request_id
        OR OLD.create_body_sha256 IS DISTINCT FROM NEW.create_body_sha256
        OR OLD.body_format IS DISTINCT FROM NEW.body_format
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'comment identity is immutable';
    END IF;
    IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN
        RAISE EXCEPTION 'comment version and update time must advance exactly once';
    END IF;
    IF OLD.state <> 'visible' THEN
        RAISE EXCEPTION 'comment tombstones are terminal';
    END IF;

    IF NEW.state = 'visible' THEN
        IF NEW.body IS NOT DISTINCT FROM OLD.body
            OR NEW.edited_at IS DISTINCT FROM NEW.updated_at
            OR NEW.deleted_at IS NOT NULL THEN
            RAISE EXCEPTION 'visible comment update must be a body edit';
        END IF;
    ELSIF NEW.state IN ('author_deleted', 'moderator_hidden') THEN
        IF NEW.body <> ''
            OR NEW.deleted_at IS DISTINCT FROM NEW.updated_at
            OR NEW.edited_at IS DISTINCT FROM OLD.edited_at THEN
            RAISE EXCEPTION 'comment deletion must create a terminal tombstone';
        END IF;
    ELSE
        RAISE EXCEPTION 'invalid comment state transition';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER comment_history_protected
BEFORE UPDATE OR DELETE ON social.comments
FOR EACH ROW EXECUTE FUNCTION social.protect_comment_history();
