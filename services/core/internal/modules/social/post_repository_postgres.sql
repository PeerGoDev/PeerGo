-- name: CountVisiblePosts :one
SELECT count(*)::bigint
FROM social.posts AS post
JOIN identity.users AS author
    ON author.id = post.author_id
WHERE post.state = 'visible'
  AND (
    sqlc.arg(author_username)::text = ''
    OR lower(author.username) = lower(sqlc.arg(author_username)::text)
  );

-- name: ListVisiblePosts :many
SELECT
    post.id AS post_internal_id,
    post.public_id,
    post.author_id,
    author.username AS author_username,
    author.display_name AS author_display_name,
    post.body,
    post.state,
    post.version,
    post.created_at,
    post.updated_at,
    post.edited_at,
    COALESCE(comment_count.value, 0)::bigint AS comment_count
FROM social.posts AS post
JOIN identity.users AS author
    ON author.id = post.author_id
LEFT JOIN social.post_comment_threads AS binding
    ON binding.post_id = post.id
LEFT JOIN LATERAL (
    SELECT count(*)::bigint AS value
    FROM social.comments AS comment
    WHERE comment.thread_id = binding.thread_id
) AS comment_count ON true
WHERE post.state = 'visible'
  AND (
    sqlc.arg(author_username)::text = ''
    OR lower(author.username) = lower(sqlc.arg(author_username)::text)
  )
ORDER BY
    CASE WHEN sqlc.arg(sort_order)::text = 'oldest' THEN post.created_at END ASC,
    CASE WHEN sqlc.arg(sort_order)::text = 'oldest' THEN post.id END ASC,
    CASE WHEN sqlc.arg(sort_order)::text = 'newest' THEN post.created_at END DESC,
    CASE WHEN sqlc.arg(sort_order)::text = 'newest' THEN post.id END DESC
LIMIT sqlc.arg(result_limit)::integer
OFFSET sqlc.arg(result_offset)::integer;

-- name: FindVisiblePost :one
SELECT
    post.id AS post_internal_id,
    post.public_id,
    post.author_id,
    author.username AS author_username,
    author.display_name AS author_display_name,
    post.body,
    post.state,
    post.version,
    post.created_at,
    post.updated_at,
    post.edited_at,
    COALESCE(comment_count.value, 0)::bigint AS comment_count
FROM social.posts AS post
JOIN identity.users AS author
    ON author.id = post.author_id
LEFT JOIN social.post_comment_threads AS binding
    ON binding.post_id = post.id
LEFT JOIN LATERAL (
    SELECT count(*)::bigint AS value
    FROM social.comments AS comment
    WHERE comment.thread_id = binding.thread_id
) AS comment_count ON true
WHERE post.public_id = sqlc.arg(post_public_id)::uuid
  AND post.state = 'visible';

-- name: FindPostByCreateRequest :one
SELECT
    post.id AS post_internal_id,
    post.public_id,
    post.author_id,
    author.username AS author_username,
    author.display_name AS author_display_name,
    post.create_body_sha256,
    post.body,
    post.state,
    post.version,
    post.created_at,
    post.updated_at,
    post.edited_at,
    COALESCE(comment_count.value, 0)::bigint AS comment_count
FROM social.posts AS post
JOIN identity.users AS author
    ON author.id = post.author_id
LEFT JOIN social.post_comment_threads AS binding
    ON binding.post_id = post.id
LEFT JOIN LATERAL (
    SELECT count(*)::bigint AS value
    FROM social.comments AS comment
    WHERE comment.thread_id = binding.thread_id
) AS comment_count ON true
WHERE post.author_id = sqlc.arg(author_id)::uuid
  AND post.create_request_id = sqlc.arg(create_request_id)::uuid;

-- name: InsertPost :execrows
INSERT INTO social.posts (
    public_id,
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

-- name: LockPostForAuthor :one
SELECT
    post.id AS post_internal_id,
    post.public_id,
    post.author_id,
    author.username AS author_username,
    author.display_name AS author_display_name,
    post.body,
    post.state,
    post.version,
    post.created_at,
    post.updated_at,
    post.edited_at,
    COALESCE(comment_count.value, 0)::bigint AS comment_count
FROM social.posts AS post
JOIN identity.users AS author
    ON author.id = post.author_id
LEFT JOIN social.post_comment_threads AS binding
    ON binding.post_id = post.id
LEFT JOIN LATERAL (
    SELECT count(*)::bigint AS value
    FROM social.comments AS comment
    WHERE comment.thread_id = binding.thread_id
) AS comment_count ON true
WHERE post.public_id = sqlc.arg(post_public_id)::uuid
  AND post.author_id = sqlc.arg(author_id)::uuid
FOR UPDATE OF post;

-- name: InsertPostRevision :exec
INSERT INTO social.post_revisions (
    post_id,
    version,
    body,
    reason,
    editor_id,
    created_at
) VALUES (
    sqlc.arg(post_id)::bigint,
    sqlc.arg(version)::bigint,
    sqlc.arg(body)::text,
    sqlc.arg(reason)::text,
    sqlc.arg(editor_id)::uuid,
    sqlc.arg(created_at)::timestamptz
);

-- name: UpdatePostBody :execrows
UPDATE social.posts
SET body = sqlc.arg(body)::text,
    version = version + 1,
    updated_at = sqlc.arg(updated_at)::timestamptz,
    edited_at = sqlc.arg(updated_at)::timestamptz
WHERE id = sqlc.arg(post_id)::bigint
  AND author_id = sqlc.arg(author_id)::uuid
  AND state = 'visible'
  AND version = sqlc.arg(expected_version)::bigint;

-- name: TombstonePostByAuthor :execrows
UPDATE social.posts
SET body = '',
    state = 'author_deleted',
    version = version + 1,
    updated_at = sqlc.arg(updated_at)::timestamptz,
    deleted_at = sqlc.arg(updated_at)::timestamptz
WHERE id = sqlc.arg(post_id)::bigint
  AND author_id = sqlc.arg(author_id)::uuid
  AND state = 'visible'
  AND version = sqlc.arg(expected_version)::bigint;
