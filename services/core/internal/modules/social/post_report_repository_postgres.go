package social

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresPostModerationRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresPostModerationRepository(pool *pgxpool.Pool) (*PostgresPostModerationRepository, error) {
	if pool == nil {
		return nil, errors.New("post moderation repository dependencies are required")
	}
	return &PostgresPostModerationRepository{pool: pool}, nil
}

// CreateReport locks the post before looking up or creating its open case.
// Decisions use the same post -> case lock order, so a report arrival and a
// moderation decision cannot deadlock or split one case.
func (repository *PostgresPostModerationRepository) CreateReport(ctx context.Context, command createPostReportCommand) (PostReportReceipt, error) {
	if err := validateCreatePostReportCommand(command); err != nil {
		return PostReportReceipt{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PostReportReceipt{}, fmt.Errorf("begin post report: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if receipt, found, replayErr := resumePostReport(ctx, tx, command); found || replayErr != nil {
		if replayErr != nil {
			return PostReportReceipt{}, replayErr
		}
		if err := tx.Commit(ctx); err != nil {
			return PostReportReceipt{}, fmt.Errorf("commit replayed post report: %w", err)
		}
		return receipt, nil
	}

	var postInternalID int64
	var authorID uuid.UUID
	err = tx.QueryRow(ctx, `
SELECT post.id, post.author_id
FROM social.posts AS post
JOIN social.boards AS board ON board.id = post.board_id
WHERE post.public_id = $1 AND post.state = 'visible' AND board.enabled
FOR UPDATE OF post`, command.PostID).Scan(&postInternalID, &authorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return PostReportReceipt{}, ErrPostReportTargetNotFound
	}
	if err != nil {
		return PostReportReceipt{}, fmt.Errorf("lock post report target: %w", err)
	}
	if authorID == command.ReporterID {
		return PostReportReceipt{}, ErrPostReportSelf
	}

	var caseInternalID int64
	err = tx.QueryRow(ctx, `
SELECT id
FROM social.post_moderation_cases
WHERE post_id = $1 AND state = 'open'
FOR UPDATE`, postInternalID).Scan(&caseInternalID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
INSERT INTO social.post_moderation_cases (public_id, post_id, state, version, opened_at, updated_at)
VALUES ($1, $2, 'open', 1, $3, $3)
RETURNING id`, command.CasePublicID, postInternalID, command.CreatedAt).Scan(&caseInternalID)
	}
	if err != nil {
		return PostReportReceipt{}, fmt.Errorf("find or create post moderation case: %w", err)
	}

	var alreadyReported bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM social.post_reports WHERE case_id = $1 AND reporter_id = $2
)`, caseInternalID, command.ReporterID).Scan(&alreadyReported); err != nil {
		return PostReportReceipt{}, fmt.Errorf("find reporter in post moderation case: %w", err)
	}
	if alreadyReported {
		return PostReportReceipt{}, ErrPostAlreadyReported
	}

	inserted, err := tx.Exec(ctx, `
INSERT INTO social.post_reports (
    public_id, case_id, reporter_id, create_request_id, create_input_sha256,
    reason_code, details, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT DO NOTHING`,
		command.ReportPublicID, caseInternalID, command.ReporterID, command.RequestID, command.CreateInputHash[:],
		string(command.ReasonCode), command.Details, command.CreatedAt)
	if err != nil {
		return PostReportReceipt{}, fmt.Errorf("insert post report: %w", err)
	}
	if inserted.RowsAffected() == 0 {
		// A request UUID can race on a different post because those aggregates
		// do not share a row lock. Re-read the immutable request before deciding
		// whether this is an exact replay or a conflicting reuse.
		if receipt, found, replayErr := resumePostReport(ctx, tx, command); found || replayErr != nil {
			if replayErr != nil {
				return PostReportReceipt{}, replayErr
			}
			if err := tx.Commit(ctx); err != nil {
				return PostReportReceipt{}, fmt.Errorf("commit concurrent post report replay: %w", err)
			}
			return receipt, nil
		}
		return PostReportReceipt{}, ErrPostAlreadyReported
	}
	receipt := PostReportReceipt{
		ID: command.ReportPublicID, PostID: command.PostID,
		ReasonCode: command.ReasonCode, CreatedAt: command.CreatedAt.UTC(),
	}
	if err := tx.Commit(ctx); err != nil {
		return PostReportReceipt{}, fmt.Errorf("commit post report: %w", err)
	}
	return receipt, nil
}

// ListOpenCases returns count, cases and their reports from one repeatable-read
// snapshot. Reporter UUIDs never enter the selected columns.
func (repository *PostgresPostModerationRepository) ListOpenCases(ctx context.Context, limit, offset int) (PostModerationCasePage, error) {
	if limit < 1 || limit > MaxModerationCaseLimit || offset < 0 || offset > MaxModerationCaseOffset {
		return PostModerationCasePage{}, ErrPostReportInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return PostModerationCasePage{}, fmt.Errorf("begin post moderation list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var total int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM social.post_moderation_cases WHERE state = 'open'`).Scan(&total); err != nil {
		return PostModerationCasePage{}, fmt.Errorf("count open post moderation cases: %w", err)
	}
	rows, err := tx.Query(ctx, `
SELECT
    moderation_case.id,
    moderation_case.public_id,
    moderation_case.state,
    moderation_case.version,
    moderation_case.opened_at,
    post.public_id,
    post.author_id,
    author.username,
    author.display_name,
    post.board_id,
    board.name,
    post.body,
    post.state,
    post.version,
    post.created_at,
    post.edited_at,
    (SELECT count(*) FROM social.post_media AS media WHERE media.post_id = post.id)::integer,
    COALESCE((SELECT max(report.created_at)
                FROM social.post_reports AS report
               WHERE report.case_id = moderation_case.id), moderation_case.opened_at),
    (SELECT count(*) FROM social.post_reports AS report WHERE report.case_id = moderation_case.id)
FROM social.post_moderation_cases AS moderation_case
JOIN social.posts AS post ON post.id = moderation_case.post_id
JOIN identity.users AS author ON author.id = post.author_id
JOIN social.boards AS board ON board.id = post.board_id
WHERE moderation_case.state = 'open'
ORDER BY moderation_case.opened_at, moderation_case.id
LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return PostModerationCasePage{}, fmt.Errorf("list open post moderation cases: %w", err)
	}
	type caseRow struct {
		internalID int64
		item       PostModerationCase
	}
	caseRows := make([]caseRow, 0, limit)
	for rows.Next() {
		var row caseRow
		var caseState, postState string
		var editedAt pgtype.Timestamptz
		if err := rows.Scan(
			&row.internalID, &row.item.ID, &caseState, &row.item.Version, &row.item.OpenedAt,
			&row.item.Post.ID, &row.item.Post.Author.ID, &row.item.Post.Author.Username, &row.item.Post.Author.DisplayName,
			&row.item.Post.BoardID, &row.item.Post.BoardName, &row.item.Post.Body, &postState, &row.item.Post.Version,
			&row.item.Post.CreatedAt, &editedAt, &row.item.Post.MediaCount,
			&row.item.LatestReportedAt, &row.item.ReportCount,
		); err != nil {
			rows.Close()
			return PostModerationCasePage{}, fmt.Errorf("scan open post moderation case: %w", err)
		}
		row.item.State = PostModerationCaseState(caseState)
		row.item.Post.State = PostState(postState)
		row.item.OpenedAt = row.item.OpenedAt.UTC()
		row.item.LatestReportedAt = row.item.LatestReportedAt.UTC()
		row.item.Post.CreatedAt = row.item.Post.CreatedAt.UTC()
		if editedAt.Valid {
			value := editedAt.Time.UTC()
			row.item.Post.EditedAt = &value
		}
		caseRows = append(caseRows, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return PostModerationCasePage{}, fmt.Errorf("list open post moderation cases: %w", err)
	}

	reportsByCase := make(map[int64][]PostModerationReport, len(caseRows))
	if len(caseRows) > 0 {
		caseIDs := make([]int64, 0, len(caseRows))
		for _, row := range caseRows {
			caseIDs = append(caseIDs, row.internalID)
		}
		// Keep the staff projection bounded even when a long-lived case
		// accumulates many reports; report_count above stays exact.
		reportRows, err := tx.Query(ctx, `
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
    FROM social.post_reports AS report
    WHERE report.case_id = ANY($1::bigint[])
) AS recent_report
WHERE case_row_number <= $2
ORDER BY case_id, created_at, id`, caseIDs, MaxModerationReportsPerCase)
		if err != nil {
			return PostModerationCasePage{}, fmt.Errorf("list reports for post moderation cases: %w", err)
		}
		for reportRows.Next() {
			var caseID int64
			var reasonCode string
			var report PostModerationReport
			if err := reportRows.Scan(&caseID, &reasonCode, &report.Details, &report.CreatedAt); err != nil {
				reportRows.Close()
				return PostModerationCasePage{}, fmt.Errorf("scan post moderation report: %w", err)
			}
			report.ReasonCode = PostReportReasonCode(reasonCode)
			report.CreatedAt = report.CreatedAt.UTC()
			reportsByCase[caseID] = append(reportsByCase[caseID], report)
		}
		reportRows.Close()
		if err := reportRows.Err(); err != nil {
			return PostModerationCasePage{}, fmt.Errorf("list reports for post moderation cases: %w", err)
		}
	}

	page := PostModerationCasePage{Items: make([]PostModerationCase, 0, len(caseRows)), Total: total, Limit: limit, Offset: offset}
	for _, row := range caseRows {
		item := row.item
		item.Reports = reportsByCase[row.internalID]
		if validatePostModerationCase(item) != nil {
			return PostModerationCasePage{}, ErrPostModerationInvariant
		}
		page.Items = append(page.Items, item)
	}
	if err := tx.Commit(ctx); err != nil {
		return PostModerationCasePage{}, fmt.Errorf("commit post moderation list: %w", err)
	}
	return page, nil
}

// Decide commits the case transition, optional post hide, management evidence
// and immutable decision atomically. Hiding uses the same staff moderation
// transition and post_management_events record as direct post moderation.
func (repository *PostgresPostModerationRepository) Decide(ctx context.Context, command decidePostModerationCaseCommand) (PostModerationDecisionResult, error) {
	command, err := normalizePostModerationDecisionCommand(command)
	if err != nil {
		return PostModerationDecisionResult{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PostModerationDecisionResult{}, fmt.Errorf("begin post moderation decision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if result, found, replayErr := resumePostModerationDecision(ctx, tx, command); found || replayErr != nil {
		if replayErr != nil {
			return PostModerationDecisionResult{}, replayErr
		}
		if err := tx.Commit(ctx); err != nil {
			return PostModerationDecisionResult{}, fmt.Errorf("commit replayed post moderation decision: %w", err)
		}
		return result, nil
	}

	var postInternalID int64
	err = tx.QueryRow(ctx, `SELECT post_id FROM social.post_moderation_cases WHERE public_id = $1`, command.CaseID).Scan(&postInternalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return PostModerationDecisionResult{}, ErrPostModerationCaseNotFound
	}
	if err != nil {
		return PostModerationDecisionResult{}, fmt.Errorf("find post moderation target: %w", err)
	}
	var postPublicID, authorID uuid.UUID
	var postState, boardID string
	var postVersion int64
	var pinned, featured bool
	err = tx.QueryRow(ctx, `
SELECT public_id, author_id, state, version, board_id, is_pinned, is_featured
FROM social.posts
WHERE id = $1
FOR UPDATE`, postInternalID).Scan(&postPublicID, &authorID, &postState, &postVersion, &boardID, &pinned, &featured)
	if errors.Is(err, pgx.ErrNoRows) {
		return PostModerationDecisionResult{}, ErrPostModerationCaseNotFound
	}
	if err != nil {
		return PostModerationDecisionResult{}, fmt.Errorf("lock moderated post: %w", err)
	}
	var caseInternalID, casePostID, caseVersion int64
	var caseState string
	err = tx.QueryRow(ctx, `
SELECT id, post_id, state, version
FROM social.post_moderation_cases
WHERE public_id = $1
FOR UPDATE`, command.CaseID).Scan(&caseInternalID, &casePostID, &caseState, &caseVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return PostModerationDecisionResult{}, ErrPostModerationCaseNotFound
	}
	if err != nil {
		return PostModerationDecisionResult{}, fmt.Errorf("lock post moderation case: %w", err)
	}
	if casePostID != postInternalID {
		return PostModerationDecisionResult{}, ErrPostModerationInvariant
	}
	if PostModerationCaseState(caseState) != PostModerationCaseOpen || caseVersion != command.ExpectedCaseVersion {
		if result, found, replayErr := resumePostModerationDecision(ctx, tx, command); found || replayErr != nil {
			if replayErr != nil {
				return PostModerationDecisionResult{}, replayErr
			}
			if err := tx.Commit(ctx); err != nil {
				return PostModerationDecisionResult{}, fmt.Errorf("commit concurrent post moderation replay: %w", err)
			}
			return result, nil
		}
		if caseVersion != command.ExpectedCaseVersion {
			return PostModerationDecisionResult{}, ErrPostModerationCaseVersionConflict
		}
		return PostModerationDecisionResult{}, ErrPostModerationCaseStateConflict
	}
	if postVersion != command.ExpectedPostVersion {
		return PostModerationDecisionResult{}, ErrPostModerationPostVersionConflict
	}
	var reporterConflict bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM social.post_reports WHERE case_id = $1 AND reporter_id = $2
)`, caseInternalID, command.ModeratorID).Scan(&reporterConflict); err != nil {
		return PostModerationDecisionResult{}, fmt.Errorf("check post moderation reporter conflict: %w", err)
	}
	if authorID == command.ModeratorID || reporterConflict {
		return PostModerationDecisionResult{}, ErrPostModerationConflictOfInterest
	}
	var reportCount int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM social.post_reports WHERE case_id = $1`, caseInternalID).Scan(&reportCount); err != nil {
		return PostModerationDecisionResult{}, fmt.Errorf("count post moderation case reports: %w", err)
	}
	if reportCount < 1 {
		return PostModerationDecisionResult{}, ErrPostModerationInvariant
	}

	resultingCaseState := PostModerationCaseDismissed
	resultingPostState := PostState(postState)
	resultingPostVersion := postVersion
	if command.Decision == PostModerationHidePost {
		if resultingPostState != PostVisible {
			return PostModerationDecisionResult{}, ErrPostModerationCaseStateConflict
		}
		updated, err := tx.Exec(ctx, `
UPDATE social.posts
SET state = 'moderator_hidden', moderated_at = $2, moderated_by = $3, updated_at = $2, version = version + 1
WHERE id = $1 AND state = 'visible' AND version = $4`,
			postInternalID, command.OccurredAt, command.ModeratorID, postVersion)
		if err != nil {
			return PostModerationDecisionResult{}, fmt.Errorf("hide reported post: %w", err)
		}
		if updated.RowsAffected() != 1 {
			return PostModerationDecisionResult{}, ErrPostModerationPostVersionConflict
		}
		before, err := json.Marshal(map[string]any{"state": postState, "board_id": boardID, "pinned": pinned, "featured": featured})
		if err != nil {
			return PostModerationDecisionResult{}, fmt.Errorf("encode reported post state: %w", err)
		}
		after, err := json.Marshal(map[string]any{"state": string(PostModeratorHidden), "board_id": boardID, "pinned": pinned, "featured": featured})
		if err != nil {
			return PostModerationDecisionResult{}, fmt.Errorf("encode hidden post state: %w", err)
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO social.post_management_events (
    id, post_id, actor_id, reason, expected_version, resulting_version,
    authorization_decision_id, before_state, after_state, occurred_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			uuid.New(), postInternalID, command.ModeratorID, command.Note, postVersion, postVersion+1,
			command.Authorization.ID, before, after, command.OccurredAt); err != nil {
			return PostModerationDecisionResult{}, fmt.Errorf("append hidden post management event: %w", err)
		}
		resultingCaseState = PostModerationCasePostHidden
		resultingPostState = PostModeratorHidden
		resultingPostVersion++
	}

	resolved, err := tx.Exec(ctx, `
UPDATE social.post_moderation_cases
SET state = $2, version = version + 1, updated_at = $3, resolved_at = $3
WHERE id = $1 AND state = 'open' AND version = $4`,
		caseInternalID, string(resultingCaseState), command.OccurredAt, command.ExpectedCaseVersion)
	if err != nil {
		return PostModerationDecisionResult{}, fmt.Errorf("resolve post moderation case: %w", err)
	}
	if resolved.RowsAffected() != 1 {
		return PostModerationDecisionResult{}, ErrPostModerationCaseVersionConflict
	}
	result := PostModerationDecisionResult{
		DecisionID: command.DecisionID, CaseID: command.CaseID, PostID: postPublicID,
		Decision: command.Decision, ReasonCode: command.ReasonCode, CaseState: resultingCaseState,
		PostState: resultingPostState, CaseVersion: command.ExpectedCaseVersion + 1,
		PostVersion: resultingPostVersion, DecidedAt: command.OccurredAt.UTC(),
	}
	inserted, err := tx.Exec(ctx, `
INSERT INTO social.post_moderation_decisions (
    id, case_id, case_public_id, post_public_id, moderator_id,
    decision, reason_code, note,
    expected_case_version, resulting_case_version,
    expected_post_version, resulting_post_version,
    resulting_case_state, resulting_post_state,
    authorization_decision_id, decided_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
ON CONFLICT DO NOTHING`,
		command.DecisionID, caseInternalID, command.CaseID, postPublicID, command.ModeratorID,
		string(command.Decision), string(command.ReasonCode), command.Note,
		command.ExpectedCaseVersion, result.CaseVersion,
		command.ExpectedPostVersion, result.PostVersion,
		string(result.CaseState), string(result.PostState),
		command.Authorization.ID, command.OccurredAt)
	if err != nil {
		return PostModerationDecisionResult{}, fmt.Errorf("insert post moderation decision: %w", err)
	}
	if inserted.RowsAffected() != 1 {
		return PostModerationDecisionResult{}, ErrPostModerationDecisionIdempotency
	}
	if err := tx.Commit(ctx); err != nil {
		return PostModerationDecisionResult{}, fmt.Errorf("commit post moderation decision: %w", err)
	}
	return result, nil
}

func validateCreatePostReportCommand(command createPostReportCommand) error {
	normalizedDetails, err := normalizeModerationText(command.Details, 0, MaxReportDetailsRunes)
	if err != nil || command.ReportPublicID == uuid.Nil || command.CasePublicID == uuid.Nil || command.RequestID == uuid.Nil ||
		command.PostID == uuid.Nil || command.ReporterID == uuid.Nil || !validPostReportReason(command.ReasonCode) ||
		command.CreatedAt.IsZero() || normalizedDetails != command.Details ||
		command.CreateInputHash != postReportInputHash(command.PostID, command.ReasonCode, command.Details) {
		return ErrPostReportInput
	}
	return nil
}

func normalizePostModerationDecisionCommand(command decidePostModerationCaseCommand) (decidePostModerationCaseCommand, error) {
	normalized, err := normalizePostModerationDecisionInput(command.DecidePostModerationCaseInput)
	if err != nil || command.ModeratorID == uuid.Nil || command.OccurredAt.IsZero() ||
		!command.Authorization.Allow || command.Authorization.ID == uuid.Nil {
		return decidePostModerationCaseCommand{}, ErrPostReportInput
	}
	command.DecidePostModerationCaseInput = normalized
	return command, nil
}

func resumePostReport(ctx context.Context, tx pgx.Tx, command createPostReportCommand) (PostReportReceipt, bool, error) {
	var receipt PostReportReceipt
	var reasonCode, details string
	var inputHash []byte
	err := tx.QueryRow(ctx, `
SELECT report.public_id, post.public_id, report.reason_code, report.details,
       report.create_input_sha256, report.created_at
FROM social.post_reports AS report
JOIN social.post_moderation_cases AS moderation_case ON moderation_case.id = report.case_id
JOIN social.posts AS post ON post.id = moderation_case.post_id
WHERE report.reporter_id = $1 AND report.create_request_id = $2`,
		command.ReporterID, command.RequestID).Scan(&receipt.ID, &receipt.PostID, &reasonCode, &details, &inputHash, &receipt.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return PostReportReceipt{}, false, nil
	}
	if err != nil {
		return PostReportReceipt{}, false, fmt.Errorf("find post report request: %w", err)
	}
	if receipt.PostID != command.PostID || reasonCode != string(command.ReasonCode) ||
		details != command.Details || !bytes.Equal(inputHash, command.CreateInputHash[:]) {
		return PostReportReceipt{}, true, ErrPostReportIdempotencyConflict
	}
	if receipt.ID == uuid.Nil || receipt.CreatedAt.IsZero() {
		return PostReportReceipt{}, true, ErrPostModerationInvariant
	}
	receipt.ReasonCode = PostReportReasonCode(reasonCode)
	receipt.CreatedAt = receipt.CreatedAt.UTC()
	return receipt, true, nil
}

func resumePostModerationDecision(ctx context.Context, tx pgx.Tx, command decidePostModerationCaseCommand) (PostModerationDecisionResult, bool, error) {
	var result PostModerationDecisionResult
	var moderatorID uuid.UUID
	var decision, reasonCode, note, caseState, postState string
	var expectedCaseVersion, expectedPostVersion int64
	err := tx.QueryRow(ctx, `
SELECT id, case_public_id, post_public_id, moderator_id, decision, reason_code, note,
       expected_case_version, resulting_case_version,
       expected_post_version, resulting_post_version,
       resulting_case_state, resulting_post_state, decided_at
FROM social.post_moderation_decisions
WHERE id = $1`, command.DecisionID).Scan(
		&result.DecisionID, &result.CaseID, &result.PostID, &moderatorID, &decision, &reasonCode, &note,
		&expectedCaseVersion, &result.CaseVersion, &expectedPostVersion, &result.PostVersion,
		&caseState, &postState, &result.DecidedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PostModerationDecisionResult{}, false, nil
	}
	if err != nil {
		return PostModerationDecisionResult{}, false, fmt.Errorf("find post moderation decision: %w", err)
	}
	if result.CaseID != command.CaseID || moderatorID != command.ModeratorID ||
		decision != string(command.Decision) || reasonCode != string(command.ReasonCode) || note != command.Note ||
		expectedCaseVersion != command.ExpectedCaseVersion || expectedPostVersion != command.ExpectedPostVersion {
		return PostModerationDecisionResult{}, true, ErrPostModerationDecisionIdempotency
	}
	result.Decision = PostModerationDecision(decision)
	result.ReasonCode = PostModerationReasonCode(reasonCode)
	result.CaseState = PostModerationCaseState(caseState)
	result.PostState = PostState(postState)
	result.DecidedAt = result.DecidedAt.UTC()
	if result.PostID == uuid.Nil || result.DecidedAt.IsZero() || result.CaseVersion != command.ExpectedCaseVersion+1 ||
		(command.Decision == PostModerationDismiss && (result.CaseState != PostModerationCaseDismissed || result.PostVersion != command.ExpectedPostVersion)) ||
		(command.Decision == PostModerationHidePost && (result.CaseState != PostModerationCasePostHidden || result.PostState != PostModeratorHidden || result.PostVersion != command.ExpectedPostVersion+1)) {
		return PostModerationDecisionResult{}, true, ErrPostModerationInvariant
	}
	return result, true, nil
}

var _ PostModerationRepository = (*PostgresPostModerationRepository)(nil)
