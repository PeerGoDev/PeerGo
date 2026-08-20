package social

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/generated/socialdb"
)

type PostgresCommentModerationRepository struct {
	pool             *pgxpool.Pool
	eventBuilder     CommentModerationEventBuilder
	newAuditAppender func(pgx.Tx) auditevent.Appender
}

func NewPostgresCommentModerationRepository(
	pool *pgxpool.Pool,
	eventBuilder CommentModerationEventBuilder,
	newAuditAppender func(pgx.Tx) auditevent.Appender,
) (*PostgresCommentModerationRepository, error) {
	if pool == nil || eventBuilder == nil || newAuditAppender == nil {
		return nil, errors.New("comment moderation repository dependencies are required")
	}
	return &PostgresCommentModerationRepository{
		pool: pool, eventBuilder: eventBuilder, newAuditAppender: newAuditAppender,
	}, nil
}

// CreateReport locks the comment before looking up or creating its open case.
// Decisions use the same comment -> case lock order, which prevents a report
// arrival and a moderation decision from deadlocking or splitting one case.
func (repository *PostgresCommentModerationRepository) CreateReport(ctx context.Context, command createCommentReportCommand) (CommentReportReceipt, error) {
	if err := validateCreateCommentReportCommand(command); err != nil {
		return CommentReportReceipt{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CommentReportReceipt{}, fmt.Errorf("begin comment report: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := socialdb.New(tx)

	if receipt, found, replayErr := resumeCommentReport(ctx, queries, command); found || replayErr != nil {
		if replayErr != nil {
			return CommentReportReceipt{}, replayErr
		}
		if err := tx.Commit(ctx); err != nil {
			return CommentReportReceipt{}, fmt.Errorf("commit replayed comment report: %w", err)
		}
		return receipt, nil
	}

	comment, err := queries.LockVisibleCommentForReport(ctx, command.CommentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommentReportReceipt{}, ErrCommentReportTargetNotFound
	}
	if err != nil {
		return CommentReportReceipt{}, fmt.Errorf("lock comment report target: %w", err)
	}
	if comment.AuthorID == command.ReporterID {
		return CommentReportReceipt{}, ErrCommentReportSelf
	}

	moderationCase, err := queries.FindOpenModerationCaseForComment(ctx, comment.CommentInternalID)
	var moderationCaseID int64
	if errors.Is(err, pgx.ErrNoRows) {
		createdCase, createErr := queries.CreateCommentModerationCase(ctx, socialdb.CreateCommentModerationCaseParams{
			PublicID: command.CasePublicID, CommentID: comment.CommentInternalID, OpenedAt: timestamp(command.CreatedAt),
		})
		err = createErr
		moderationCaseID = createdCase.ID
	} else if err == nil {
		moderationCaseID = moderationCase.ID
	}
	if err != nil {
		return CommentReportReceipt{}, fmt.Errorf("find or create comment moderation case: %w", err)
	}

	if _, err := queries.FindCommentReportByCaseReporter(ctx, socialdb.FindCommentReportByCaseReporterParams{
		CaseID: moderationCaseID, ReporterID: command.ReporterID,
	}); err == nil {
		return CommentReportReceipt{}, ErrCommentAlreadyReported
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return CommentReportReceipt{}, fmt.Errorf("find reporter in comment moderation case: %w", err)
	}

	affected, err := queries.InsertCommentReport(ctx, socialdb.InsertCommentReportParams{
		PublicID: command.ReportPublicID, CaseID: moderationCaseID, ReporterID: command.ReporterID,
		CreateRequestID: command.RequestID, CreateInputSha256: command.CreateInputHash[:],
		ReasonCode: string(command.ReasonCode), Details: command.Details, CreatedAt: timestamp(command.CreatedAt),
	})
	if err != nil {
		return CommentReportReceipt{}, fmt.Errorf("insert comment report: %w", err)
	}
	if affected == 0 {
		// A request UUID can race on a different comment because those aggregates
		// do not share a row lock. Re-read the immutable request before deciding
		// whether this is an exact replay or a conflicting reuse.
		if receipt, found, replayErr := resumeCommentReport(ctx, queries, command); found || replayErr != nil {
			if replayErr != nil {
				return CommentReportReceipt{}, replayErr
			}
			if err := tx.Commit(ctx); err != nil {
				return CommentReportReceipt{}, fmt.Errorf("commit concurrent comment report replay: %w", err)
			}
			return receipt, nil
		}
		return CommentReportReceipt{}, ErrCommentAlreadyReported
	}
	receipt := CommentReportReceipt{
		ID: command.ReportPublicID, CommentID: command.CommentID,
		ReasonCode: command.ReasonCode, CreatedAt: command.CreatedAt.UTC(),
	}
	if err := tx.Commit(ctx); err != nil {
		return CommentReportReceipt{}, fmt.Errorf("commit comment report: %w", err)
	}
	return receipt, nil
}

// ListOpenCases returns count, cases and their reports from one repeatable-read
// snapshot. Reporter UUIDs never enter the selected columns.
func (repository *PostgresCommentModerationRepository) ListOpenCases(ctx context.Context, limit, offset int) (CommentModerationCasePage, error) {
	if limit < 1 || limit > MaxModerationCaseLimit || offset < 0 || offset > MaxModerationCaseOffset {
		return CommentModerationCasePage{}, ErrCommentReportInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return CommentModerationCasePage{}, fmt.Errorf("begin comment moderation list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := socialdb.New(tx)
	total, err := queries.CountOpenCommentModerationCases(ctx)
	if err != nil {
		return CommentModerationCasePage{}, fmt.Errorf("count open comment moderation cases: %w", err)
	}
	rows, err := queries.ListOpenCommentModerationCases(ctx, socialdb.ListOpenCommentModerationCasesParams{
		ResultLimit: int32(limit), ResultOffset: int32(offset),
	})
	if err != nil {
		return CommentModerationCasePage{}, fmt.Errorf("list open comment moderation cases: %w", err)
	}
	caseIDs := make([]int64, 0, len(rows))
	for _, row := range rows {
		caseIDs = append(caseIDs, row.CaseInternalID)
	}
	reportsByCase := make(map[int64][]CommentModerationReport, len(rows))
	if len(caseIDs) > 0 {
		reportRows, reportErr := queries.ListCommentReportsForCases(ctx, caseIDs)
		if reportErr != nil {
			return CommentModerationCasePage{}, fmt.Errorf("list reports for comment moderation cases: %w", reportErr)
		}
		for _, row := range reportRows {
			if !row.CreatedAt.Valid {
				return CommentModerationCasePage{}, ErrModerationInvariant
			}
			reportsByCase[row.CaseID] = append(reportsByCase[row.CaseID], CommentModerationReport{
				ReasonCode: CommentReportReasonCode(row.ReasonCode), Details: row.Details, CreatedAt: row.CreatedAt.Time.UTC(),
			})
		}
	}
	page := CommentModerationCasePage{Items: make([]CommentModerationCase, 0, len(rows)), Total: total, Limit: limit, Offset: offset}
	for _, row := range rows {
		item, conversionErr := moderationCaseFromRow(row, reportsByCase[row.CaseInternalID])
		if conversionErr != nil {
			return CommentModerationCasePage{}, conversionErr
		}
		if int64(len(item.Reports)) > row.ReportCount {
			return CommentModerationCasePage{}, ErrModerationInvariant
		}
		page.Items = append(page.Items, item)
	}
	if err := tx.Commit(ctx); err != nil {
		return CommentModerationCasePage{}, fmt.Errorf("commit comment moderation list: %w", err)
	}
	return page, nil
}

// Decide commits the case transition, optional comment tombstone, immutable
// decision and external audit outbox event atomically. Audit failure is fail
// closed: no visible content can be hidden without durable evidence.
func (repository *PostgresCommentModerationRepository) Decide(ctx context.Context, command decideCommentModerationCaseCommand) (CommentModerationDecisionResult, error) {
	command, err := normalizeModerationDecisionCommand(command)
	if err != nil {
		return CommentModerationDecisionResult{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CommentModerationDecisionResult{}, fmt.Errorf("begin comment moderation decision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := socialdb.New(tx)

	if result, found, replayErr := resumeCommentModerationDecision(ctx, queries, command); found || replayErr != nil {
		if replayErr != nil {
			return CommentModerationDecisionResult{}, replayErr
		}
		if err := tx.Commit(ctx); err != nil {
			return CommentModerationDecisionResult{}, fmt.Errorf("commit replayed comment moderation decision: %w", err)
		}
		return result, nil
	}

	commentInternalID, err := queries.FindModerationCaseCommentID(ctx, command.CaseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommentModerationDecisionResult{}, ErrModerationCaseNotFound
	}
	if err != nil {
		return CommentModerationDecisionResult{}, fmt.Errorf("find comment moderation target: %w", err)
	}
	comment, err := queries.LockCommentForModeration(ctx, commentInternalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommentModerationDecisionResult{}, ErrModerationCaseNotFound
	}
	if err != nil {
		return CommentModerationDecisionResult{}, fmt.Errorf("lock moderated comment: %w", err)
	}
	moderationCase, err := queries.LockCommentModerationCase(ctx, command.CaseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommentModerationDecisionResult{}, ErrModerationCaseNotFound
	}
	if err != nil {
		return CommentModerationDecisionResult{}, fmt.Errorf("lock comment moderation case: %w", err)
	}
	if moderationCase.CommentID != comment.CommentInternalID {
		return CommentModerationDecisionResult{}, ErrModerationInvariant
	}
	if moderationCase.State != string(CommentModerationCaseOpen) || moderationCase.Version != command.ExpectedCaseVersion {
		if result, found, replayErr := resumeCommentModerationDecision(ctx, queries, command); found || replayErr != nil {
			if replayErr != nil {
				return CommentModerationDecisionResult{}, replayErr
			}
			if err := tx.Commit(ctx); err != nil {
				return CommentModerationDecisionResult{}, fmt.Errorf("commit concurrent moderation replay: %w", err)
			}
			return result, nil
		}
		if moderationCase.Version != command.ExpectedCaseVersion {
			return CommentModerationDecisionResult{}, ErrModerationCaseVersionConflict
		}
		return CommentModerationDecisionResult{}, ErrModerationCaseStateConflict
	}
	if comment.CommentVersion != command.ExpectedCommentVersion {
		return CommentModerationDecisionResult{}, ErrModerationCommentVersionConflict
	}
	target, err := commentTargetFromParts(comment.TargetKind, comment.TargetKey)
	if err != nil {
		return CommentModerationDecisionResult{}, ErrModerationInvariant
	}
	hasReporterConflict, err := queries.CommentModerationCaseHasReporter(ctx, socialdb.CommentModerationCaseHasReporterParams{
		CaseID: moderationCase.ID, ReporterID: command.ModeratorID,
	})
	if err != nil {
		return CommentModerationDecisionResult{}, fmt.Errorf("check moderation reporter conflict: %w", err)
	}
	if comment.AuthorID == command.ModeratorID || hasReporterConflict {
		return CommentModerationDecisionResult{}, ErrModerationConflictOfInterest
	}
	reportCount, err := queries.CountCommentModerationCaseReports(ctx, moderationCase.ID)
	if err != nil {
		return CommentModerationDecisionResult{}, fmt.Errorf("count moderation case reports: %w", err)
	}
	if reportCount < 1 {
		return CommentModerationDecisionResult{}, ErrModerationInvariant
	}

	before := CommentModerationAuditState{
		CaseID: moderationCase.PublicID, CaseState: CommentModerationCaseState(moderationCase.State), CaseVersion: moderationCase.Version,
		CommentID: comment.CommentPublicID, CommentState: CommentState(comment.CommentState), CommentVersion: comment.CommentVersion,
	}
	resultingCaseState := CommentModerationCaseDismissed
	resultingCommentState := CommentState(comment.CommentState)
	resultingCommentVersion := comment.CommentVersion
	if command.Decision == CommentModerationHideComment {
		if resultingCommentState != CommentVisible || comment.Body == "" {
			return CommentModerationDecisionResult{}, ErrModerationCaseStateConflict
		}
		if err := queries.InsertCommentRevision(ctx, socialdb.InsertCommentRevisionParams{
			CommentID: comment.CommentInternalID, Version: comment.CommentVersion, Body: comment.Body,
			BodyFormat: comment.BodyFormat, Reason: "moderator_hide", EditorID: command.ModeratorID,
			CreatedAt: timestamp(command.OccurredAt),
		}); err != nil {
			return CommentModerationDecisionResult{}, fmt.Errorf("append moderated comment revision: %w", err)
		}
		affected, err := queries.TombstoneCommentByModerator(ctx, socialdb.TombstoneCommentByModeratorParams{
			UpdatedAt: timestamp(command.OccurredAt), CommentID: comment.CommentInternalID, ExpectedVersion: comment.CommentVersion,
		})
		if err != nil {
			return CommentModerationDecisionResult{}, fmt.Errorf("tombstone moderated comment: %w", err)
		}
		if affected != 1 {
			return CommentModerationDecisionResult{}, ErrModerationCommentVersionConflict
		}
		resultingCaseState = CommentModerationCaseCommentHidden
		resultingCommentState = CommentModeratorHidden
		resultingCommentVersion++
	}
	caseAffected, err := queries.ResolveCommentModerationCase(ctx, socialdb.ResolveCommentModerationCaseParams{
		ResultingState: string(resultingCaseState), ResolvedAt: timestamp(command.OccurredAt),
		CaseID: moderationCase.ID, ExpectedVersion: command.ExpectedCaseVersion,
	})
	if err != nil {
		return CommentModerationDecisionResult{}, fmt.Errorf("resolve comment moderation case: %w", err)
	}
	if caseAffected != 1 {
		return CommentModerationDecisionResult{}, ErrModerationCaseVersionConflict
	}
	result := CommentModerationDecisionResult{
		DecisionID: command.DecisionID, CaseID: command.CaseID, CommentID: comment.CommentPublicID,
		Decision: command.Decision, ReasonCode: command.ReasonCode, CaseState: resultingCaseState,
		CommentState: resultingCommentState, CaseVersion: command.ExpectedCaseVersion + 1,
		CommentVersion: resultingCommentVersion, DecidedAt: command.OccurredAt.UTC(),
	}
	inserted, err := queries.InsertCommentModerationDecision(ctx, socialdb.InsertCommentModerationDecisionParams{
		DecisionID: command.DecisionID, CaseID: moderationCase.ID, CasePublicID: command.CaseID,
		CommentPublicID: comment.CommentPublicID, TargetKind: string(target.Kind), TargetKey: comment.TargetKey,
		ModeratorID: command.ModeratorID,
		Decision:    string(command.Decision), ReasonCode: string(command.ReasonCode), Note: command.Note,
		ExpectedCaseVersion: command.ExpectedCaseVersion, ResultingCaseVersion: result.CaseVersion,
		ExpectedCommentVersion: command.ExpectedCommentVersion, ResultingCommentVersion: result.CommentVersion,
		ResultingCaseState: string(result.CaseState), ResultingCommentState: string(result.CommentState),
		AuthorizationDecisionID: command.Authorization.ID, DecidedAt: timestamp(command.OccurredAt),
	})
	if err != nil {
		return CommentModerationDecisionResult{}, fmt.Errorf("insert comment moderation decision: %w", err)
	}
	if inserted != 1 {
		return CommentModerationDecisionResult{}, ErrModerationDecisionIdempotency
	}
	after := CommentModerationAuditState{
		CaseID: result.CaseID, CaseState: result.CaseState, CaseVersion: result.CaseVersion,
		CommentID: result.CommentID, CommentState: result.CommentState, CommentVersion: result.CommentVersion,
	}
	event, err := repository.eventBuilder.BuildCommentModerationDecisionEvent(CommentModerationAuditInput{
		DecisionID: command.DecisionID, ModeratorID: command.ModeratorID, CommentAuthorID: comment.AuthorID,
		Target: target, Decision: command.Decision, ReasonCode: command.ReasonCode,
		Note: command.Note, ReportCount: reportCount, OccurredAt: command.OccurredAt,
		Authorization: command.Authorization, Before: before, After: after,
	})
	if err != nil {
		return CommentModerationDecisionResult{}, fmt.Errorf("build comment moderation audit event: %w", err)
	}
	if err := repository.newAuditAppender(tx).Append(ctx, event); err != nil {
		return CommentModerationDecisionResult{}, fmt.Errorf("append comment moderation audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return CommentModerationDecisionResult{}, fmt.Errorf("commit comment moderation decision: %w", err)
	}
	return result, nil
}

func validateCreateCommentReportCommand(command createCommentReportCommand) error {
	normalizedDetails, err := normalizeModerationText(command.Details, 0, MaxReportDetailsRunes)
	if err != nil || command.ReportPublicID == uuid.Nil || command.CasePublicID == uuid.Nil || command.RequestID == uuid.Nil ||
		command.CommentID == uuid.Nil || command.ReporterID == uuid.Nil || !validCommentReportReason(command.ReasonCode) ||
		command.CreatedAt.IsZero() || normalizedDetails != command.Details ||
		command.CreateInputHash != commentReportInputHash(command.CommentID, command.ReasonCode, command.Details) {
		return ErrCommentReportInput
	}
	return nil
}

func normalizeModerationDecisionCommand(command decideCommentModerationCaseCommand) (decideCommentModerationCaseCommand, error) {
	normalized, err := normalizeModerationDecisionInput(command.DecideCommentModerationCaseInput)
	if err != nil || command.ModeratorID == uuid.Nil || command.OccurredAt.IsZero() ||
		!command.Authorization.Allow || command.Authorization.ID == uuid.Nil {
		return decideCommentModerationCaseCommand{}, ErrCommentReportInput
	}
	command.DecideCommentModerationCaseInput = normalized
	return command, nil
}

func resumeCommentReport(ctx context.Context, queries *socialdb.Queries, command createCommentReportCommand) (CommentReportReceipt, bool, error) {
	row, err := queries.FindCommentReportByCreateRequest(ctx, socialdb.FindCommentReportByCreateRequestParams{
		ReporterID: command.ReporterID, CreateRequestID: command.RequestID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return CommentReportReceipt{}, false, nil
	}
	if err != nil {
		return CommentReportReceipt{}, false, fmt.Errorf("find comment report request: %w", err)
	}
	if row.CommentPublicID != command.CommentID || row.ReasonCode != string(command.ReasonCode) ||
		row.Details != command.Details || !bytes.Equal(row.CreateInputSha256, command.CreateInputHash[:]) {
		return CommentReportReceipt{}, true, ErrCommentReportIdempotencyConflict
	}
	if row.PublicID == uuid.Nil || !row.CreatedAt.Valid {
		return CommentReportReceipt{}, true, ErrModerationInvariant
	}
	return CommentReportReceipt{
		ID: row.PublicID, CommentID: row.CommentPublicID,
		ReasonCode: CommentReportReasonCode(row.ReasonCode), CreatedAt: row.CreatedAt.Time.UTC(),
	}, true, nil
}

func resumeCommentModerationDecision(ctx context.Context, queries *socialdb.Queries, command decideCommentModerationCaseCommand) (CommentModerationDecisionResult, bool, error) {
	row, err := queries.FindCommentModerationDecision(ctx, command.DecisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommentModerationDecisionResult{}, false, nil
	}
	if err != nil {
		return CommentModerationDecisionResult{}, false, fmt.Errorf("find comment moderation decision: %w", err)
	}
	if row.CasePublicID != command.CaseID || row.ModeratorID != command.ModeratorID ||
		row.Decision != string(command.Decision) || row.ReasonCode != string(command.ReasonCode) || row.Note != command.Note ||
		row.ExpectedCaseVersion != command.ExpectedCaseVersion || row.ExpectedCommentVersion != command.ExpectedCommentVersion {
		return CommentModerationDecisionResult{}, true, ErrModerationDecisionIdempotency
	}
	// Replays are served from the immutable decision row without relocking the
	// original comment. Validate its typed target columns explicitly so legacy
	// or malformed rows cannot bypass the same closed-target invariant used on
	// first execution.
	if _, err := moderationDecisionTarget(row); err != nil {
		return CommentModerationDecisionResult{}, true, ErrModerationInvariant
	}
	result := CommentModerationDecisionResult{
		DecisionID: row.ID, CaseID: row.CasePublicID, CommentID: row.CommentPublicID,
		Decision: CommentModerationDecision(row.Decision), ReasonCode: CommentModerationReasonCode(row.ReasonCode),
		CaseState: CommentModerationCaseState(row.ResultingCaseState), CommentState: CommentState(row.ResultingCommentState),
		CaseVersion: row.ResultingCaseVersion, CommentVersion: row.ResultingCommentVersion, DecidedAt: row.DecidedAt.Time.UTC(),
	}
	if row.CommentPublicID == uuid.Nil || !row.DecidedAt.Valid || row.ResultingCaseVersion != command.ExpectedCaseVersion+1 ||
		result.Decision != command.Decision || result.ReasonCode != command.ReasonCode || result.CommentVersion < 1 ||
		(command.Decision == CommentModerationDismiss && (result.CaseState != CommentModerationCaseDismissed || result.CommentVersion != command.ExpectedCommentVersion)) ||
		(command.Decision == CommentModerationHideComment && (result.CaseState != CommentModerationCaseCommentHidden || result.CommentState != CommentModeratorHidden || result.CommentVersion != command.ExpectedCommentVersion+1)) {
		return CommentModerationDecisionResult{}, true, ErrModerationInvariant
	}
	return result, true, nil
}

func moderationDecisionTarget(row socialdb.FindCommentModerationDecisionRow) (CommentTarget, error) {
	switch CommentTargetKind(row.TargetKind) {
	case CommentTargetTorrent:
		if !row.TorrentID.Valid || row.TorrentID.Int64 < 1 || row.AnnouncementID.Valid || row.PostPublicID.Valid {
			return CommentTarget{}, ErrModerationInvariant
		}
		return TorrentCommentTarget(row.TorrentID.Int64), nil
	case CommentTargetAnnouncement:
		if row.TorrentID.Valid || !row.AnnouncementID.Valid || row.PostPublicID.Valid {
			return CommentTarget{}, ErrModerationInvariant
		}
		return commentTargetFromParts(row.TargetKind, row.AnnouncementID.String)
	case CommentTargetPost:
		if row.TorrentID.Valid || row.AnnouncementID.Valid || !row.PostPublicID.Valid {
			return CommentTarget{}, ErrModerationInvariant
		}
		return commentTargetFromParts(row.TargetKind, uuid.UUID(row.PostPublicID.Bytes).String())
	default:
		return CommentTarget{}, ErrModerationInvariant
	}
}

func moderationCaseFromRow(row socialdb.ListOpenCommentModerationCasesRow, reports []CommentModerationReport) (CommentModerationCase, error) {
	if !row.OpenedAt.Valid || !row.LatestReportedAt.Valid || row.ReportCount < 1 {
		return CommentModerationCase{}, ErrModerationInvariant
	}
	comment, err := commentFromFields(
		row.CommentInternalID, row.CommentPublicID, row.TargetKind, row.TargetKey, row.ParentPublicID,
		row.AuthorID, row.AuthorDisplayName, row.Body, row.BodyFormat, row.CommentState,
		row.CommentVersion, row.CommentCreatedAt, row.CommentUpdatedAt, row.CommentEditedAt,
	)
	if err != nil {
		return CommentModerationCase{}, err
	}
	item := CommentModerationCase{
		ID: row.CasePublicID, State: CommentModerationCaseState(row.CaseState), Version: row.CaseVersion,
		Target:  CommentModerationTarget{CommentTarget: comment.Target, Title: row.TargetTitle},
		Comment: comment, ReportCount: row.ReportCount, Reports: reports,
		OpenedAt: row.OpenedAt.Time.UTC(), LatestReportedAt: row.LatestReportedAt.Time.UTC(),
	}
	if validateModerationCase(item) != nil {
		return CommentModerationCase{}, ErrModerationInvariant
	}
	return item, nil
}

var _ CommentModerationRepository = (*PostgresCommentModerationRepository)(nil)
