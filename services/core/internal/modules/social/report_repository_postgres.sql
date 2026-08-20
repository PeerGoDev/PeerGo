-- name: FindCommentReportByCreateRequest :one
SELECT
    report.public_id,
    comment.public_id AS comment_public_id,
    report.create_input_sha256,
    report.reason_code,
    report.details,
    report.created_at
FROM social.comment_reports AS report
JOIN social.comment_moderation_cases AS moderation_case
    ON moderation_case.id = report.case_id
JOIN social.comments AS comment
    ON comment.id = moderation_case.comment_id
WHERE report.reporter_id = sqlc.arg(reporter_id)::uuid
  AND report.create_request_id = sqlc.arg(create_request_id)::uuid;

-- name: LockVisibleCommentForReport :one
SELECT
    comment.id AS comment_internal_id,
    comment.public_id,
    comment.author_id
FROM social.comments AS comment
JOIN social.comment_target_projection AS target
    ON target.thread_id = comment.thread_id
WHERE comment.public_id = sqlc.arg(comment_public_id)::uuid
  AND comment.state = 'visible'
  AND target.target_is_public = true
FOR UPDATE OF comment;

-- name: FindOpenModerationCaseForComment :one
SELECT id, public_id, version, opened_at
FROM social.comment_moderation_cases
WHERE comment_id = sqlc.arg(comment_id)::bigint
  AND state = 'open'
FOR UPDATE;

-- name: CreateCommentModerationCase :one
INSERT INTO social.comment_moderation_cases (
    public_id,
    comment_id,
    state,
    version,
    opened_at,
    updated_at
) VALUES (
    sqlc.arg(public_id)::uuid,
    sqlc.arg(comment_id)::bigint,
    'open',
    1,
    sqlc.arg(opened_at)::timestamptz,
    sqlc.arg(opened_at)::timestamptz
)
RETURNING id, public_id, version, opened_at;

-- name: FindCommentReportByCaseReporter :one
SELECT
    report.public_id,
    comment.public_id AS comment_public_id,
    report.reason_code,
    report.created_at
FROM social.comment_reports AS report
JOIN social.comment_moderation_cases AS moderation_case
    ON moderation_case.id = report.case_id
JOIN social.comments AS comment
    ON comment.id = moderation_case.comment_id
WHERE report.case_id = sqlc.arg(case_id)::bigint
  AND report.reporter_id = sqlc.arg(reporter_id)::uuid;

-- name: InsertCommentReport :execrows
INSERT INTO social.comment_reports (
    public_id,
    case_id,
    reporter_id,
    create_request_id,
    create_input_sha256,
    reason_code,
    details,
    created_at
) VALUES (
    sqlc.arg(public_id)::uuid,
    sqlc.arg(case_id)::bigint,
    sqlc.arg(reporter_id)::uuid,
    sqlc.arg(create_request_id)::uuid,
    sqlc.arg(create_input_sha256)::bytea,
    sqlc.arg(reason_code)::text,
    sqlc.arg(details)::text,
    sqlc.arg(created_at)::timestamptz
)
ON CONFLICT DO NOTHING;

-- name: CountOpenCommentModerationCases :one
SELECT count(*)::bigint
FROM social.comment_moderation_cases
WHERE state = 'open';

-- name: ListOpenCommentModerationCases :many
SELECT
    moderation_case.id AS case_internal_id,
    moderation_case.public_id AS case_public_id,
    moderation_case.state AS case_state,
    moderation_case.version AS case_version,
    moderation_case.opened_at,
    comment.id AS comment_internal_id,
    comment.public_id AS comment_public_id,
    target.target_kind,
    target.target_key,
    target.target_title,
    parent.public_id AS parent_public_id,
    comment.author_id,
    author.display_name AS author_display_name,
    comment.body,
    comment.body_format,
    comment.state AS comment_state,
    comment.version AS comment_version,
    comment.created_at AS comment_created_at,
    comment.updated_at AS comment_updated_at,
    comment.edited_at AS comment_edited_at,
    COALESCE((SELECT max(report.created_at)
       FROM social.comment_reports AS report
      WHERE report.case_id = moderation_case.id), moderation_case.opened_at)::timestamptz AS latest_reported_at,
    (SELECT count(*)::bigint
       FROM social.comment_reports AS report
      WHERE report.case_id = moderation_case.id) AS report_count
FROM social.comment_moderation_cases AS moderation_case
JOIN social.comments AS comment
    ON comment.id = moderation_case.comment_id
JOIN social.comment_target_projection AS target
    ON target.thread_id = comment.thread_id
JOIN identity.users AS author
    ON author.id = comment.author_id
LEFT JOIN social.comments AS parent
    ON parent.id = comment.parent_comment_id
   AND parent.thread_id = comment.thread_id
WHERE moderation_case.state = 'open'
ORDER BY moderation_case.opened_at, moderation_case.id
LIMIT sqlc.arg(result_limit)::integer
OFFSET sqlc.arg(result_offset)::integer;

-- name: ListCommentReportsForCases :many
-- Keep the staff projection bounded even when a long-lived case accumulates
-- many reports. CountOpenCommentModerationCases returns the exact total; this
-- query returns only the latest evidence summaries per case and never reporter
-- identity. The limit mirrors MaxModerationReportsPerCase in the social domain.
SELECT case_id, reason_code, details, created_at
FROM (
    SELECT
        report.case_id,
        report.reason_code,
        report.details,
        report.created_at,
        report.id,
        row_number() OVER (
            PARTITION BY report.case_id
            ORDER BY report.created_at DESC, report.id DESC
        ) AS case_row_number
    FROM social.comment_reports AS report
    WHERE report.case_id = ANY(sqlc.arg(case_ids)::bigint[])
) AS recent_report
WHERE case_row_number <= 50
ORDER BY case_id, created_at, id;

-- name: FindModerationCaseCommentID :one
SELECT comment_id
FROM social.comment_moderation_cases
WHERE public_id = sqlc.arg(case_public_id)::uuid;

-- name: LockCommentForModeration :one
SELECT
    comment.id AS comment_internal_id,
    comment.public_id AS comment_public_id,
    comment.author_id,
    comment.body,
    comment.body_format,
    comment.state AS comment_state,
    comment.version AS comment_version,
    target.target_kind,
    target.target_key
FROM social.comments AS comment
JOIN social.comment_target_projection AS target
    ON target.thread_id = comment.thread_id
WHERE comment.id = sqlc.arg(comment_id)::bigint
FOR UPDATE OF comment;

-- name: LockCommentModerationCase :one
SELECT id, public_id, comment_id, state, version, opened_at, updated_at
FROM social.comment_moderation_cases
WHERE public_id = sqlc.arg(case_public_id)::uuid
FOR UPDATE;

-- name: CommentModerationCaseHasReporter :one
SELECT EXISTS (
    SELECT 1
    FROM social.comment_reports
    WHERE case_id = sqlc.arg(case_id)::bigint
      AND reporter_id = sqlc.arg(reporter_id)::uuid
);

-- name: CountCommentModerationCaseReports :one
SELECT count(*)::bigint
FROM social.comment_reports
WHERE case_id = sqlc.arg(case_id)::bigint;

-- name: TombstoneCommentByModerator :execrows
UPDATE social.comments
SET body = '',
    state = 'moderator_hidden',
    version = version + 1,
    updated_at = sqlc.arg(updated_at)::timestamptz,
    deleted_at = sqlc.arg(updated_at)::timestamptz
WHERE id = sqlc.arg(comment_id)::bigint
  AND state = 'visible'
  AND version = sqlc.arg(expected_version)::bigint;

-- name: ResolveCommentModerationCase :execrows
UPDATE social.comment_moderation_cases
SET state = sqlc.arg(resulting_state)::text,
    version = version + 1,
    updated_at = sqlc.arg(resolved_at)::timestamptz,
    resolved_at = sqlc.arg(resolved_at)::timestamptz
WHERE id = sqlc.arg(case_id)::bigint
  AND state = 'open'
  AND version = sqlc.arg(expected_version)::bigint;

-- name: InsertCommentModerationDecision :execrows
INSERT INTO social.comment_moderation_decisions (
    id,
    case_id,
    case_public_id,
    comment_public_id,
    target_kind,
    torrent_id,
    announcement_id,
    post_public_id,
    moderator_id,
    decision,
    reason_code,
    note,
    expected_case_version,
    resulting_case_version,
    expected_comment_version,
    resulting_comment_version,
    resulting_case_state,
    resulting_comment_state,
    authorization_decision_id,
    decided_at
) VALUES (
    sqlc.arg(decision_id)::uuid,
    sqlc.arg(case_id)::bigint,
    sqlc.arg(case_public_id)::uuid,
    sqlc.arg(comment_public_id)::uuid,
    sqlc.arg(target_kind)::text,
    CASE
        WHEN sqlc.arg(target_kind)::text = 'torrent'
        THEN sqlc.arg(target_key)::text::bigint
        ELSE NULL
    END,
    CASE
        WHEN sqlc.arg(target_kind)::text = 'announcement'
        THEN sqlc.arg(target_key)::text
        ELSE NULL
    END,
    CASE
        WHEN sqlc.arg(target_kind)::text = 'post'
        THEN sqlc.arg(target_key)::text::uuid
        ELSE NULL
    END,
    sqlc.arg(moderator_id)::uuid,
    sqlc.arg(decision)::text,
    sqlc.arg(reason_code)::text,
    sqlc.arg(note)::text,
    sqlc.arg(expected_case_version)::bigint,
    sqlc.arg(resulting_case_version)::bigint,
    sqlc.arg(expected_comment_version)::bigint,
    sqlc.arg(resulting_comment_version)::bigint,
    sqlc.arg(resulting_case_state)::text,
    sqlc.arg(resulting_comment_state)::text,
    sqlc.arg(authorization_decision_id)::uuid,
    sqlc.arg(decided_at)::timestamptz
)
ON CONFLICT (id) DO NOTHING;

-- name: FindCommentModerationDecision :one
SELECT
    id,
    case_id,
    case_public_id,
    comment_public_id,
    target_kind,
    torrent_id,
    announcement_id,
    post_public_id,
    moderator_id,
    decision,
    reason_code,
    note,
    expected_case_version,
    resulting_case_version,
    expected_comment_version,
    resulting_comment_version,
    resulting_case_state,
    resulting_comment_state,
    authorization_decision_id,
    decided_at
FROM social.comment_moderation_decisions
WHERE id = sqlc.arg(decision_id)::uuid;
