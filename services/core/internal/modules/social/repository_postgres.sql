-- name: CountComments :one
SELECT count(comment.id)::bigint
FROM social.comment_target_projection AS target
LEFT JOIN social.comments AS comment
    ON comment.thread_id = target.thread_id
WHERE target.target_kind = sqlc.arg(target_kind)::text
  AND target.target_key = sqlc.arg(target_key)::text
  AND target.target_is_public = true
GROUP BY target.target_kind, target.target_key;

-- name: ListComments :many
SELECT
    comment.id AS comment_internal_id,
    comment.public_id,
    target.target_kind,
    target.target_key,
    parent.public_id AS parent_public_id,
    comment.author_id,
    author.display_name AS author_display_name,
    comment.body,
    comment.body_format,
    comment.state,
    comment.version,
    comment.created_at,
    comment.updated_at,
    comment.edited_at
FROM social.comment_target_projection AS target
JOIN social.comments AS comment
    ON comment.thread_id = target.thread_id
JOIN identity.users AS author
    ON author.id = comment.author_id
LEFT JOIN social.comments AS parent
    ON parent.id = comment.parent_comment_id
   AND parent.thread_id = comment.thread_id
WHERE target.target_kind = sqlc.arg(target_kind)::text
  AND target.target_key = sqlc.arg(target_key)::text
  AND target.target_is_public = true
ORDER BY comment.created_at, comment.id
LIMIT sqlc.arg(result_limit)::integer
OFFSET sqlc.arg(result_offset)::integer;

-- name: CountCommentThreads :one
SELECT
    count(comment.id)::bigint AS comment_total,
    count(comment.id) FILTER (WHERE comment.parent_comment_id IS NULL)::bigint AS thread_total
FROM social.comment_target_projection AS target
LEFT JOIN social.comments AS comment
    ON comment.thread_id = target.thread_id
WHERE target.target_kind = sqlc.arg(target_kind)::text
  AND target.target_key = sqlc.arg(target_key)::text
  AND target.target_is_public = true
GROUP BY target.target_kind, target.target_key;

-- name: ListCommentThreads :many
WITH root_stats AS (
    SELECT
        root.id,
        root.created_at,
        count(reply.id)::bigint AS reply_count
    FROM social.comment_target_projection AS target
    JOIN social.comments AS root
      ON root.thread_id = target.thread_id
     AND root.root_comment_id IS NULL
    LEFT JOIN social.comments AS reply
      ON reply.thread_id = root.thread_id
     AND reply.root_comment_id = root.id
    WHERE target.target_kind = sqlc.arg(target_kind)::text
      AND target.target_key = sqlc.arg(target_key)::text
      AND target.target_is_public = true
    GROUP BY root.id, root.created_at
), ranked_roots AS (
    SELECT
        root_stats.id,
        row_number() OVER (
            ORDER BY
                CASE WHEN sqlc.arg(sort_order)::text = 'hot' THEN root_stats.reply_count END DESC NULLS LAST,
                CASE WHEN sqlc.arg(sort_order)::text IN ('hot', 'newest') THEN root_stats.created_at END DESC NULLS LAST,
                CASE WHEN sqlc.arg(sort_order)::text = 'oldest' THEN root_stats.created_at END ASC NULLS LAST,
                CASE WHEN sqlc.arg(sort_order)::text IN ('hot', 'newest') THEN root_stats.id END DESC NULLS LAST,
                CASE WHEN sqlc.arg(sort_order)::text = 'oldest' THEN root_stats.id END ASC NULLS LAST
        ) AS root_position
    FROM root_stats
), selected_roots AS (
    SELECT ranked_roots.id, ranked_roots.root_position
    FROM ranked_roots
    WHERE ranked_roots.root_position > sqlc.arg(result_offset)::integer
      AND ranked_roots.root_position <= sqlc.arg(result_offset)::integer + sqlc.arg(result_limit)::integer
)
SELECT
    comment.id AS comment_internal_id,
    comment.public_id,
    target.target_kind,
    target.target_key,
    parent.public_id AS parent_public_id,
    owning_root.public_id AS root_public_id,
    comment.author_id,
    author.display_name AS author_display_name,
    comment.body,
    comment.body_format,
    comment.state,
    comment.version,
    comment.created_at,
    comment.updated_at,
    comment.edited_at
FROM selected_roots
JOIN social.comments AS comment
  ON comment.id = selected_roots.id
  OR comment.root_comment_id = selected_roots.id
JOIN social.comment_target_projection AS target
  ON target.thread_id = comment.thread_id
JOIN identity.users AS author
  ON author.id = comment.author_id
LEFT JOIN social.comments AS parent
  ON parent.id = comment.parent_comment_id
 AND parent.thread_id = comment.thread_id
LEFT JOIN social.comments AS owning_root
  ON owning_root.id = comment.root_comment_id
 AND owning_root.thread_id = comment.thread_id
ORDER BY
    selected_roots.root_position,
    (comment.id = selected_roots.id) DESC,
    comment.created_at,
    comment.id;

-- name: FindCommentByCreateRequest :one
SELECT
    comment.id AS comment_internal_id,
    comment.public_id,
    target.target_kind,
    target.target_key,
    parent.public_id AS parent_public_id,
    comment.author_id,
    author.display_name AS author_display_name,
    comment.create_body_sha256,
    comment.body,
    comment.body_format,
    comment.state,
    comment.version,
    comment.created_at,
    comment.updated_at,
    comment.edited_at
FROM social.comments AS comment
JOIN social.comment_target_projection AS target
    ON target.thread_id = comment.thread_id
JOIN identity.users AS author
    ON author.id = comment.author_id
LEFT JOIN social.comments AS parent
    ON parent.id = comment.parent_comment_id
   AND parent.thread_id = comment.thread_id
WHERE comment.author_id = sqlc.arg(author_id)::uuid
  AND comment.create_request_id = sqlc.arg(create_request_id)::uuid;

-- name: FindPublishedTorrentCommentThread :one
SELECT binding.thread_id, thread.state
FROM torrents.torrents AS torrent
JOIN social.torrent_comment_threads AS binding
    ON binding.torrent_id = torrent.id
JOIN social.comment_threads AS thread
    ON thread.id = binding.thread_id
WHERE torrent.id = sqlc.arg(torrent_id)::bigint
  AND torrent.state = 'published';

-- name: LockPublishedTorrentForCommentThread :one
SELECT torrent.id
FROM torrents.torrents AS torrent
WHERE torrent.id = sqlc.arg(torrent_id)::bigint
  AND torrent.state = 'published'
FOR UPDATE;

-- name: FindTorrentCommentThreadByTorrentID :one
SELECT binding.thread_id, thread.state
FROM social.torrent_comment_threads AS binding
JOIN social.comment_threads AS thread
    ON thread.id = binding.thread_id
WHERE binding.torrent_id = sqlc.arg(torrent_id)::bigint;

-- name: FindPublishedAnnouncementCommentThread :one
SELECT binding.thread_id, thread.state
FROM catalog.announcements AS announcement
JOIN social.announcement_comment_threads AS binding
    ON binding.announcement_id = announcement.id
JOIN social.comment_threads AS thread
    ON thread.id = binding.thread_id
WHERE announcement.id = sqlc.arg(announcement_id)::text
  AND announcement.published_at IS NOT NULL
  AND announcement.published_at <= CURRENT_TIMESTAMP;

-- name: LockPublishedAnnouncementForCommentThread :one
SELECT announcement.id
FROM catalog.announcements AS announcement
WHERE announcement.id = sqlc.arg(announcement_id)::text
  AND announcement.published_at IS NOT NULL
  AND announcement.published_at <= sqlc.arg(occurred_at)::timestamptz
FOR UPDATE;

-- name: FindAnnouncementCommentThreadByAnnouncementID :one
SELECT binding.thread_id, thread.state
FROM social.announcement_comment_threads AS binding
JOIN social.comment_threads AS thread
    ON thread.id = binding.thread_id
WHERE binding.announcement_id = sqlc.arg(announcement_id)::text;

-- name: FindVisiblePostCommentThread :one
SELECT binding.thread_id, thread.state
FROM social.posts AS post
JOIN social.post_comment_threads AS binding
    ON binding.post_id = post.id
JOIN social.comment_threads AS thread
    ON thread.id = binding.thread_id
WHERE post.public_id = sqlc.arg(post_public_id)::uuid
  AND post.state = 'visible';

-- name: LockVisiblePostForCommentThread :one
SELECT post.id
FROM social.posts AS post
WHERE post.public_id = sqlc.arg(post_public_id)::uuid
  AND post.state = 'visible'
FOR UPDATE;

-- name: FindPostCommentThreadByPostID :one
SELECT binding.thread_id, thread.state
FROM social.post_comment_threads AS binding
JOIN social.comment_threads AS thread
    ON thread.id = binding.thread_id
WHERE binding.post_id = sqlc.arg(post_id)::bigint;

-- name: CreateCommentThread :one
INSERT INTO social.comment_threads (
    target_kind,
    state,
    version,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(target_kind)::text,
    'open',
    1,
    sqlc.arg(created_at)::timestamptz,
    sqlc.arg(created_at)::timestamptz
)
RETURNING id;

-- name: BindTorrentCommentThread :exec
INSERT INTO social.torrent_comment_threads (
    thread_id,
    target_kind,
    torrent_id,
    created_at
) VALUES (
    sqlc.arg(thread_id)::bigint,
    'torrent',
    sqlc.arg(torrent_id)::bigint,
    sqlc.arg(created_at)::timestamptz
);

-- name: BindAnnouncementCommentThread :exec
INSERT INTO social.announcement_comment_threads (
    thread_id,
    target_kind,
    announcement_id,
    created_at
) VALUES (
    sqlc.arg(thread_id)::bigint,
    'announcement',
    sqlc.arg(announcement_id)::text,
    sqlc.arg(created_at)::timestamptz
);

-- name: BindPostCommentThread :exec
INSERT INTO social.post_comment_threads (
    thread_id,
    target_kind,
    post_id,
    created_at
) VALUES (
    sqlc.arg(thread_id)::bigint,
    'post',
    sqlc.arg(post_id)::bigint,
    sqlc.arg(created_at)::timestamptz
);

-- name: FindVisibleCommentForReply :one
SELECT comment.id
FROM social.comments AS comment
WHERE comment.thread_id = sqlc.arg(thread_id)::bigint
  AND comment.public_id = sqlc.arg(parent_public_id)::uuid
  AND comment.state = 'visible'
FOR SHARE OF comment;

-- name: InsertComment :execrows
INSERT INTO social.comments (
    public_id,
    thread_id,
    parent_comment_id,
    author_id,
    create_request_id,
    create_body_sha256,
    body,
    body_format,
    state,
    version,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(public_id)::uuid,
    sqlc.arg(thread_id)::bigint,
    sqlc.narg(parent_comment_id)::bigint,
    sqlc.arg(author_id)::uuid,
    sqlc.arg(create_request_id)::uuid,
    sqlc.arg(create_body_sha256)::bytea,
    sqlc.arg(body)::text,
    'plain_text',
    'visible',
    1,
    sqlc.arg(created_at)::timestamptz,
    sqlc.arg(created_at)::timestamptz
)
ON CONFLICT (author_id, create_request_id) DO NOTHING;

-- name: FindCommentByPublicID :one
SELECT
    comment.id AS comment_internal_id,
    comment.public_id,
    target.target_kind,
    target.target_key,
    parent.public_id AS parent_public_id,
    comment.author_id,
    author.display_name AS author_display_name,
    comment.body,
    comment.body_format,
    comment.state,
    comment.version,
    comment.created_at,
    comment.updated_at,
    comment.edited_at
FROM social.comments AS comment
JOIN social.comment_target_projection AS target
    ON target.thread_id = comment.thread_id
JOIN identity.users AS author
    ON author.id = comment.author_id
LEFT JOIN social.comments AS parent
    ON parent.id = comment.parent_comment_id
   AND parent.thread_id = comment.thread_id
WHERE comment.public_id = sqlc.arg(public_id)::uuid;

-- name: LockCommentForAuthor :one
SELECT
    comment.id AS comment_internal_id,
    comment.public_id,
    comment.thread_id,
    target.target_kind,
    target.target_key,
    target.target_is_public,
    parent.public_id AS parent_public_id,
    comment.author_id,
    author.display_name AS author_display_name,
    comment.body,
    comment.body_format,
    comment.state,
    comment.version,
    comment.created_at,
    comment.updated_at,
    comment.edited_at
FROM social.comments AS comment
JOIN social.comment_target_projection AS target
    ON target.thread_id = comment.thread_id
JOIN identity.users AS author
    ON author.id = comment.author_id
LEFT JOIN social.comments AS parent
    ON parent.id = comment.parent_comment_id
   AND parent.thread_id = comment.thread_id
WHERE comment.public_id = sqlc.arg(public_id)::uuid
  AND comment.author_id = sqlc.arg(author_id)::uuid
FOR UPDATE OF comment;

-- name: InsertCommentRevision :exec
INSERT INTO social.comment_revisions (
    comment_id,
    version,
    body,
    body_format,
    reason,
    editor_id,
    created_at
) VALUES (
    sqlc.arg(comment_id)::bigint,
    sqlc.arg(version)::bigint,
    sqlc.arg(body)::text,
    sqlc.arg(body_format)::text,
    sqlc.arg(reason)::text,
    sqlc.arg(editor_id)::uuid,
    sqlc.arg(created_at)::timestamptz
);

-- name: UpdateCommentBody :execrows
UPDATE social.comments
SET body = sqlc.arg(body)::text,
    version = version + 1,
    updated_at = sqlc.arg(updated_at)::timestamptz,
    edited_at = sqlc.arg(updated_at)::timestamptz
WHERE id = sqlc.arg(comment_id)::bigint
  AND author_id = sqlc.arg(author_id)::uuid
  AND state = 'visible'
  AND version = sqlc.arg(expected_version)::bigint;

-- name: TombstoneCommentByAuthor :execrows
UPDATE social.comments
SET body = '',
    state = 'author_deleted',
    version = version + 1,
    updated_at = sqlc.arg(updated_at)::timestamptz,
    deleted_at = sqlc.arg(updated_at)::timestamptz
WHERE id = sqlc.arg(comment_id)::bigint
  AND author_id = sqlc.arg(author_id)::uuid
  AND state = 'visible'
  AND version = sqlc.arg(expected_version)::bigint;
