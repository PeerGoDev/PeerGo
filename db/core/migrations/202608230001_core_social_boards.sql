-- +goose Up

-- Boards are a configurable classification layer for the dynamic feed. They
-- never own or delete posts: disabling a board only removes it and its posts
-- from member-facing projections while preserving the complete history for
-- staff administration and later reactivation.
CREATE TABLE social.boards (
    id text PRIMARY KEY
        CHECK (id ~ '^[a-z0-9][a-z0-9-]{0,63}$'),
    name text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 40),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 120),
    icon text NOT NULL DEFAULT 'messages-square'
        CHECK (icon IN ('messages-square', 'coffee', 'folder-open', 'clapperboard', 'megaphone', 'sparkles', 'gamepad-2', 'circle-help')),
    tone text NOT NULL DEFAULT 'coral'
        CHECK (tone IN ('coral', 'green', 'blue', 'violet', 'amber', 'slate')),
    display_order integer NOT NULL DEFAULT 100
        CHECK (display_order BETWEEN 0 AND 1000000),
    enabled boolean NOT NULL DEFAULT true,
    allow_member_posts boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (updated_at >= created_at)
);

CREATE INDEX social_boards_public_order_idx
    ON social.boards (display_order, id)
    WHERE enabled;

INSERT INTO social.boards (
    id, name, description, icon, tone, display_order,
    enabled, allow_member_posts, created_at, updated_at
) VALUES
    ('general', '生活茶馆', '日常、数码与随想', 'coffee', 'coral', 10, true, true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('resources', '资源交流', '分享、求助与经验交流', 'folder-open', 'green', 20, true, true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('media', '影音闲聊', '电影、剧集与音乐', 'clapperboard', 'blue', 30, true, true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('staff', '站务公告', '社区规则与站务通知', 'megaphone', 'violet', 40, true, false, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

ALTER TABLE social.posts
    ADD COLUMN board_id text,
    ADD COLUMN is_pinned boolean NOT NULL DEFAULT false,
    ADD COLUMN is_featured boolean NOT NULL DEFAULT false,
    ADD COLUMN moderated_at timestamptz,
    ADD COLUMN moderated_by uuid REFERENCES identity.users (id) ON DELETE RESTRICT;

UPDATE social.posts SET board_id = 'general' WHERE board_id IS NULL;

ALTER TABLE social.posts
    ALTER COLUMN board_id SET NOT NULL,
    ADD CONSTRAINT social_posts_board_fk
        FOREIGN KEY (board_id) REFERENCES social.boards (id) ON DELETE RESTRICT,
    ADD CONSTRAINT social_posts_moderation_metadata_check
        CHECK (
            (moderated_at IS NULL AND moderated_by IS NULL)
            OR (moderated_at IS NOT NULL AND moderated_by IS NOT NULL AND moderated_at >= created_at AND moderated_at <= updated_at)
        );

ALTER TABLE social.posts
    DROP CONSTRAINT posts_state_check,
    DROP CONSTRAINT posts_check,
    ADD CONSTRAINT social_posts_state_check
        CHECK (state IN ('visible', 'author_deleted', 'moderator_hidden')),
    ADD CONSTRAINT social_posts_state_body_check
        CHECK (
            (state IN ('visible', 'moderator_hidden')
                AND char_length(btrim(body)) BETWEEN 1 AND 2000
                AND deleted_at IS NULL)
            OR
            (state = 'author_deleted' AND body = '' AND deleted_at IS NOT NULL)
        );

DROP INDEX social.social_posts_visible_feed_idx;
CREATE INDEX social_posts_visible_feed_idx
    ON social.posts (is_pinned DESC, created_at DESC, id DESC)
    WHERE state = 'visible';
CREATE INDEX social_posts_board_visible_feed_idx
    ON social.posts (board_id, is_pinned DESC, created_at DESC, id DESC)
    WHERE state = 'visible';
CREATE INDEX social_posts_featured_feed_idx
    ON social.posts (created_at DESC, id DESC)
    WHERE state = 'visible' AND is_featured;

-- Member relationships and lightweight interactions stay independent from a
-- post's edit version. All writes are idempotent through their composite keys.
CREATE TABLE social.follows (
    follower_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    followee_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (follower_id, followee_id),
    CHECK (follower_id <> followee_id)
);
CREATE INDEX social_follows_followee_idx
    ON social.follows (followee_id, created_at DESC);

CREATE TABLE social.post_likes (
    post_id bigint NOT NULL REFERENCES social.posts (id) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (post_id, user_id)
);
CREATE INDEX social_post_likes_user_idx
    ON social.post_likes (user_id, created_at DESC);

CREATE TABLE social.post_reposts (
    post_id bigint NOT NULL REFERENCES social.posts (id) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (post_id, user_id)
);
CREATE INDEX social_post_reposts_user_idx
    ON social.post_reposts (user_id, created_at DESC);

-- Images are uploaded first and attached to exactly one post during publish.
-- Bytes are bounded and immutable; the digest makes retries and corruption
-- checks deterministic. A later object-storage migration can move the bytea
-- payload without changing the public media UUID.
CREATE TABLE social.post_media (
    id uuid PRIMARY KEY,
    uploader_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    post_id bigint REFERENCES social.posts (id) ON DELETE RESTRICT,
    position smallint CHECK (position BETWEEN 0 AND 8),
    content_type text NOT NULL CHECK (content_type IN ('image/jpeg', 'image/png', 'image/webp')),
    content_sha256 bytea NOT NULL CHECK (octet_length(content_sha256) = 32),
    byte_length bigint NOT NULL CHECK (byte_length BETWEEN 1 AND 5242880),
    width integer NOT NULL CHECK (width BETWEEN 1 AND 16384),
    height integer NOT NULL CHECK (height BETWEEN 1 AND 16384),
    payload bytea NOT NULL,
    created_at timestamptz NOT NULL,
    attached_at timestamptz,
    CHECK (octet_length(payload) = byte_length),
    CHECK ((post_id IS NULL AND position IS NULL AND attached_at IS NULL)
        OR (post_id IS NOT NULL AND position IS NOT NULL AND attached_at IS NOT NULL)),
    UNIQUE (post_id, position),
    UNIQUE (post_id, id)
);
CREATE INDEX social_post_media_pending_idx
    ON social.post_media (uploader_id, created_at)
    WHERE post_id IS NULL;

-- +goose StatementBegin
CREATE FUNCTION social.protect_post_media()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.post_id IS NULL THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION 'attached social media is immutable';
    END IF;
    IF OLD.id IS DISTINCT FROM NEW.id
        OR OLD.uploader_id IS DISTINCT FROM NEW.uploader_id
        OR OLD.content_type IS DISTINCT FROM NEW.content_type
        OR OLD.content_sha256 IS DISTINCT FROM NEW.content_sha256
        OR OLD.byte_length IS DISTINCT FROM NEW.byte_length
        OR OLD.width IS DISTINCT FROM NEW.width
        OR OLD.height IS DISTINCT FROM NEW.height
        OR OLD.payload IS DISTINCT FROM NEW.payload
        OR OLD.created_at IS DISTINCT FROM NEW.created_at
        OR OLD.post_id IS NOT NULL
        OR NEW.post_id IS NULL
        OR NEW.position IS NULL
        OR NEW.attached_at IS NULL THEN
        RAISE EXCEPTION 'invalid social media transition';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER social_post_media_protected
BEFORE UPDATE OR DELETE ON social.post_media
FOR EACH ROW EXECUTE FUNCTION social.protect_post_media();

CREATE TABLE social.post_polls (
    post_id bigint PRIMARY KEY REFERENCES social.posts (id) ON DELETE RESTRICT,
    question text NOT NULL CHECK (char_length(btrim(question)) BETWEEN 1 AND 120),
    closes_at timestamptz,
    created_at timestamptz NOT NULL,
    CHECK (closes_at IS NULL OR closes_at > created_at)
);

CREATE TABLE social.post_poll_options (
    id uuid PRIMARY KEY,
    post_id bigint NOT NULL REFERENCES social.post_polls (post_id) ON DELETE RESTRICT,
    position smallint NOT NULL CHECK (position BETWEEN 0 AND 5),
    label text NOT NULL CHECK (char_length(btrim(label)) BETWEEN 1 AND 80),
    UNIQUE (post_id, position),
    UNIQUE (post_id, id)
);

-- The first release intentionally uses one choice per member. A member may
-- change the selected option while the poll remains open.
CREATE TABLE social.post_poll_votes (
    post_id bigint NOT NULL,
    option_id uuid NOT NULL,
    voter_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (post_id, voter_id),
    FOREIGN KEY (post_id, option_id)
        REFERENCES social.post_poll_options (post_id, id) ON DELETE RESTRICT,
    CHECK (updated_at >= created_at)
);

CREATE TABLE social.post_topics (
    post_id bigint NOT NULL REFERENCES social.posts (id) ON DELETE RESTRICT,
    topic text NOT NULL CHECK (char_length(topic) BETWEEN 1 AND 40 AND topic = lower(topic)),
    display_topic text NOT NULL CHECK (char_length(display_topic) BETWEEN 1 AND 40),
    PRIMARY KEY (post_id, topic)
);
CREATE INDEX social_post_topics_topic_idx
    ON social.post_topics (topic, post_id DESC);

-- Red packets reuse the balanced economy ledger. The dedicated system account
-- holds prepaid magic between publish and claim; there is no unfunded promise.
INSERT INTO economy.magic_accounts (
    id, user_id, account_kind, account_code, balance, version, updated_at
) VALUES (
    '00000000-0000-7000-8000-000000000008', NULL, 'system',
    'system:escrow:social_red_packet', 0, 1, CURRENT_TIMESTAMP
);

CREATE TABLE social.post_red_packets (
    post_id bigint PRIMARY KEY REFERENCES social.posts (id) ON DELETE RESTRICT,
    creator_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    total_amount bigint NOT NULL CHECK (total_amount BETWEEN 1 AND 1000000),
    claim_count integer NOT NULL CHECK (claim_count BETWEEN 1 AND 100),
    remaining_amount bigint NOT NULL CHECK (remaining_amount BETWEEN 0 AND total_amount),
    remaining_claims integer NOT NULL CHECK (remaining_claims BETWEEN 0 AND claim_count),
    funding_transaction_id uuid NOT NULL UNIQUE
        REFERENCES economy.magic_transactions (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL,
    CHECK ((remaining_claims = 0 AND remaining_amount = 0)
        OR (remaining_claims > 0 AND remaining_amount >= remaining_claims))
);

CREATE TABLE social.post_red_packet_claims (
    id uuid PRIMARY KEY,
    post_id bigint NOT NULL REFERENCES social.post_red_packets (post_id) ON DELETE RESTRICT,
    claimant_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    amount bigint NOT NULL CHECK (amount > 0),
    magic_transaction_id uuid NOT NULL UNIQUE
        REFERENCES economy.magic_transactions (id) ON DELETE RESTRICT,
    claimed_at timestamptz NOT NULL,
    UNIQUE (post_id, claimant_id)
);

-- Board and post administration changes are immutable internal evidence. The
-- authorization decision is retained beside a bounded before/after snapshot;
-- no reporter or unrelated account information enters these records.
CREATE TABLE social.board_change_events (
    id uuid PRIMARY KEY,
    board_id text NOT NULL REFERENCES social.boards (id) ON DELETE RESTRICT,
    actor_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    transition text NOT NULL CHECK (transition IN ('created', 'updated')),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 500),
    expected_version bigint NOT NULL CHECK (expected_version >= 0),
    resulting_version bigint NOT NULL CHECK (resulting_version = expected_version + 1),
    authorization_decision_id uuid NOT NULL,
    before_state jsonb,
    after_state jsonb NOT NULL CHECK (jsonb_typeof(after_state) = 'object'),
    occurred_at timestamptz NOT NULL,
    CHECK (before_state IS NULL OR jsonb_typeof(before_state) = 'object'),
    CHECK (
        (transition = 'created' AND expected_version = 0 AND before_state IS NULL)
        OR (transition = 'updated' AND expected_version > 0 AND before_state IS NOT NULL)
    )
);

CREATE TABLE social.post_management_events (
    id uuid PRIMARY KEY,
    post_id bigint NOT NULL REFERENCES social.posts (id) ON DELETE RESTRICT,
    actor_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 500),
    expected_version bigint NOT NULL CHECK (expected_version > 0),
    resulting_version bigint NOT NULL CHECK (resulting_version = expected_version + 1),
    authorization_decision_id uuid NOT NULL,
    before_state jsonb NOT NULL CHECK (jsonb_typeof(before_state) = 'object'),
    after_state jsonb NOT NULL CHECK (jsonb_typeof(after_state) = 'object'),
    occurred_at timestamptz NOT NULL
);

-- +goose StatementBegin
CREATE FUNCTION social.reject_social_administration_evidence_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'social administration evidence is immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER board_change_events_immutable
BEFORE UPDATE OR DELETE ON social.board_change_events
FOR EACH ROW EXECUTE FUNCTION social.reject_social_administration_evidence_mutation();

CREATE TRIGGER post_management_events_immutable
BEFORE UPDATE OR DELETE ON social.post_management_events
FOR EACH ROW EXECUTE FUNCTION social.reject_social_administration_evidence_mutation();

-- The original guard allowed only author body edits and author tombstones.
-- The replacement additionally permits one versioned staff metadata change,
-- move, hide, or restore while keeping author content and edit timestamps
-- untouched.
DROP TRIGGER social_post_history_protected ON social.posts;

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

DROP VIEW social.comment_target_projection;
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
    post.state = 'visible' AND board.enabled AS target_is_public
FROM social.posts AS post
JOIN social.boards AS board
  ON board.id = post.board_id
LEFT JOIN social.post_comment_threads AS binding
  ON binding.post_id = post.id;

INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    ('social.follow.write.self', '关注或取消关注其他成员', 'low', 'self', 'web-session', true, true),
    ('social.media.create.self', '上传动态图片', 'low', 'self', 'web-session', true, true),
    ('social.poll.vote.self', '参与动态投票', 'low', 'self', 'web-session', true, true),
    ('social.post.like.self', '点赞或取消点赞动态', 'low', 'self', 'web-session', true, true),
    ('social.post.repost.self', '转发或取消转发动态', 'low', 'self', 'web-session', true, true),
    ('social.redpacket.claim.self', '领取动态红包', 'medium', 'self', 'web-session', true, true),
    ('social.board.manage.read', '读取动态圈板块管理视图', 'medium', 'none', 'staff-session', true, true),
    ('social.board.create', '创建动态圈板块', 'medium', 'none', 'staff-session', true, true),
    ('social.board.update', '更新或停用动态圈板块', 'high', 'none', 'staff-session', true, true),
    ('social.post.manage.read', '读取动态圈内容管理视图', 'medium', 'none', 'staff-session', true, true),
    ('social.post.moderate', '移动、置顶、加精、隐藏或恢复动态', 'high', 'none', 'staff-session', true, true);

UPDATE authz.permissions
SET description = '向动态圈板块发布动态'
WHERE action = 'social.post.create.self';

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('member', 'social.follow.write.self'),
    ('member', 'social.media.create.self'),
    ('member', 'social.poll.vote.self'),
    ('member', 'social.post.like.self'),
    ('member', 'social.post.repost.self'),
    ('member', 'social.redpacket.claim.self'),
    ('community_moderator', 'social.board.manage.read'),
    ('community_moderator', 'social.board.create'),
    ('community_moderator', 'social.board.update'),
    ('community_moderator', 'social.post.manage.read'),
    ('community_moderator', 'social.post.moderate'),
    ('site_admin', 'social.board.manage.read'),
    ('site_admin', 'social.board.create'),
    ('site_admin', 'social.board.update'),
    ('site_admin', 'social.post.manage.read'),
    ('site_admin', 'social.post.moderate');

-- +goose Down

DELETE FROM authz.role_permissions
WHERE role_id IN ('member', 'community_moderator', 'site_admin')
  AND action IN (
      'social.follow.write.self',
      'social.media.create.self',
      'social.poll.vote.self',
      'social.post.like.self',
      'social.post.repost.self',
      'social.redpacket.claim.self',
      'social.board.manage.read',
      'social.board.create',
      'social.board.update',
      'social.post.manage.read',
      'social.post.moderate'
  );

DELETE FROM authz.permissions
WHERE action IN (
    'social.follow.write.self',
    'social.media.create.self',
    'social.poll.vote.self',
    'social.post.like.self',
    'social.post.repost.self',
    'social.redpacket.claim.self',
    'social.board.manage.read',
    'social.board.create',
    'social.board.update',
    'social.post.manage.read',
    'social.post.moderate'
);

UPDATE authz.permissions
SET description = '发布公开纯文本动态'
WHERE action = 'social.post.create.self';

DROP VIEW social.comment_target_projection;
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

DROP TRIGGER social_post_history_protected ON social.posts;
DROP FUNCTION social.protect_post_history();

-- +goose StatementBegin
CREATE FUNCTION social.protect_post_history()
RETURNS trigger
LANGUAGE plpgsql
AS $$
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
    IF OLD.state <> 'visible'
        OR NEW.version <> OLD.version + 1
        OR NEW.updated_at < OLD.updated_at THEN
        RAISE EXCEPTION 'social post transition is invalid';
    END IF;
    IF NEW.state = 'visible' THEN
        IF NEW.body IS NOT DISTINCT FROM OLD.body
            OR NEW.edited_at IS DISTINCT FROM NEW.updated_at
            OR NEW.deleted_at IS NOT NULL THEN
            RAISE EXCEPTION 'visible social post update must be a body edit';
        END IF;
    ELSIF NEW.state = 'author_deleted' THEN
        IF NEW.body <> ''
            OR NEW.deleted_at IS DISTINCT FROM NEW.updated_at
            OR NEW.edited_at IS DISTINCT FROM OLD.edited_at THEN
            RAISE EXCEPTION 'social post deletion must create a terminal tombstone';
        END IF;
    ELSE
        RAISE EXCEPTION 'invalid social post state transition';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER social_post_history_protected
BEFORE UPDATE OR DELETE ON social.posts
FOR EACH ROW EXECUTE FUNCTION social.protect_post_history();

DROP TRIGGER post_management_events_immutable ON social.post_management_events;
DROP TRIGGER board_change_events_immutable ON social.board_change_events;
DROP FUNCTION social.reject_social_administration_evidence_mutation();
DROP TABLE social.post_management_events;
DROP TABLE social.board_change_events;

DROP TABLE social.post_red_packet_claims;
DROP TABLE social.post_red_packets;
DROP TABLE social.post_topics;
DROP TABLE social.post_poll_votes;
DROP TABLE social.post_poll_options;
DROP TABLE social.post_polls;
DROP TRIGGER social_post_media_protected ON social.post_media;
DROP FUNCTION social.protect_post_media();
DROP INDEX social.social_post_media_pending_idx;
DROP TABLE social.post_media;
DROP INDEX social.social_post_reposts_user_idx;
DROP TABLE social.post_reposts;
DROP INDEX social.social_post_likes_user_idx;
DROP TABLE social.post_likes;
DROP INDEX social.social_follows_followee_idx;
DROP TABLE social.follows;

DELETE FROM economy.magic_accounts
WHERE id = '00000000-0000-7000-8000-000000000008'
  AND NOT EXISTS (
      SELECT 1
      FROM economy.magic_postings AS posting
      WHERE posting.account_id = economy.magic_accounts.id
  );

DROP INDEX social.social_posts_featured_feed_idx;
DROP INDEX social.social_posts_board_visible_feed_idx;
DROP INDEX social.social_posts_visible_feed_idx;
CREATE INDEX social_posts_visible_feed_idx
    ON social.posts (created_at DESC, id DESC)
    WHERE state = 'visible';

ALTER TABLE social.posts
    DROP CONSTRAINT social_posts_moderation_metadata_check,
    DROP CONSTRAINT social_posts_state_body_check,
    DROP CONSTRAINT social_posts_state_check,
    DROP CONSTRAINT social_posts_board_fk,
    DROP COLUMN moderated_by,
    DROP COLUMN moderated_at,
    DROP COLUMN is_featured,
    DROP COLUMN is_pinned,
    DROP COLUMN board_id,
    ADD CONSTRAINT posts_state_check CHECK (state IN ('visible', 'author_deleted')),
    ADD CONSTRAINT posts_check CHECK (
        (state = 'visible' AND char_length(btrim(body)) BETWEEN 1 AND 2000 AND deleted_at IS NULL)
        OR (state = 'author_deleted' AND body = '' AND deleted_at IS NOT NULL)
    );

DROP INDEX social.social_boards_public_order_idx;
DROP TABLE social.boards;
