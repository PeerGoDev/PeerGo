-- +goose Up

-- Dynamic-circle interactions use their own private inbox. They deliberately
-- stay separate from community.* site messages so a busy social feed cannot
-- bury review, ratio or account notices.
CREATE TABLE social.interaction_notifications (
    id uuid PRIMARY KEY,
    recipient_user_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    actor_user_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    kind text NOT NULL
        CHECK (kind IN ('follow', 'post_like', 'post_repost', 'post_comment', 'comment_reply')),
    post_id bigint REFERENCES social.posts (id) ON DELETE RESTRICT,
    comment_id bigint REFERENCES social.comments (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL,
    read_at timestamptz,
    CHECK (recipient_user_id <> actor_user_id),
    CHECK (read_at IS NULL OR read_at >= created_at),
    CHECK (
        (kind = 'follow' AND post_id IS NULL AND comment_id IS NULL)
        OR
        (kind IN ('post_like', 'post_repost') AND post_id IS NOT NULL AND comment_id IS NULL)
        OR
        (kind IN ('post_comment', 'comment_reply') AND post_id IS NOT NULL AND comment_id IS NOT NULL)
    )
);

-- NULL source identities are normalized only for deduplication. Real post and
-- comment IDs are positive, so zero cannot collide with persisted content.
CREATE UNIQUE INDEX social_interaction_notifications_dedupe_idx
    ON social.interaction_notifications (
        recipient_user_id,
        actor_user_id,
        kind,
        COALESCE(post_id, 0),
        COALESCE(comment_id, 0)
    );

CREATE INDEX social_interaction_notifications_recipient_idx
    ON social.interaction_notifications (recipient_user_id, created_at DESC, id DESC);

CREATE INDEX social_interaction_notifications_unread_idx
    ON social.interaction_notifications (recipient_user_id, created_at DESC, id DESC)
    WHERE read_at IS NULL;

-- +goose Down

DROP TABLE social.interaction_notifications;
