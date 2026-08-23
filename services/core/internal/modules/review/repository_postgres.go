package review

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/contracts/trackerevent"
	"github.com/peergo/peergo/services/core/internal/generated/reviewdb"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	"github.com/peergo/peergo/services/core/internal/modules/workgroups"
)

// PostgresRepository owns the review transaction. The aggregate transition,
// immutable decision, public catalog row, audit evidence and Tracker control
// event either commit together or all roll back. No network call occurs while
// the torrent row is locked.
type PostgresRepository struct {
	pool                    *pgxpool.Pool
	queries                 *reviewdb.Queries
	auditBuilder            AuditEventBuilder
	newAuditAppender        func(pgx.Tx) auditevent.Appender
	eligibilityBuilder      EligibilityEventBuilder
	newTrackerAppender      func(pgx.Tx) trackerevent.Appender
	newNotificationAppender func(pgx.Tx) NotificationAppender
}

func NewPostgresRepository(
	pool *pgxpool.Pool,
	auditBuilder AuditEventBuilder,
	newAuditAppender func(pgx.Tx) auditevent.Appender,
	eligibilityBuilder EligibilityEventBuilder,
	newTrackerAppender func(pgx.Tx) trackerevent.Appender,
	newNotificationAppender func(pgx.Tx) NotificationAppender,
) (*PostgresRepository, error) {
	if pool == nil || auditBuilder == nil || newAuditAppender == nil || eligibilityBuilder == nil || newTrackerAppender == nil || newNotificationAppender == nil {
		return nil, errors.New("torrent review repository dependencies are required")
	}
	return &PostgresRepository{
		pool: pool, queries: reviewdb.New(pool), auditBuilder: auditBuilder,
		newAuditAppender: newAuditAppender, eligibilityBuilder: eligibilityBuilder,
		newTrackerAppender:      newTrackerAppender,
		newNotificationAppender: newNotificationAppender,
	}, nil
}

func (repository *PostgresRepository) ListPending(ctx context.Context, limit int32) (PendingTorrentPage, error) {
	if limit < 1 || limit > maxPendingReviewLimit {
		return PendingTorrentPage{}, ErrTorrentReviewInput
	}
	rows, err := repository.queries.ListPendingTorrentReviews(ctx, limit)
	if err != nil {
		return PendingTorrentPage{}, fmt.Errorf("query pending torrent reviews: %w", err)
	}
	page := PendingTorrentPage{Items: make([]PendingTorrent, 0, len(rows))}
	for _, row := range rows {
		item, conversionErr := pendingTorrentFromValues(
			row.ID, row.UploaderID, row.UploaderDisplayName, row.CategoryID, row.CategoryName,
			row.Title, row.Subtitle, row.ContentName, row.InfoHashV1, row.TotalSizeBytes,
			row.FileCount, row.Version, row.SubmittedAt, row.ReviewRequestedAt,
		)
		if conversionErr != nil {
			return PendingTorrentPage{}, conversionErr
		}
		page.Items = append(page.Items, item)
		page.Total = row.TotalCount
	}
	return page, nil
}

func (repository *PostgresRepository) ListAssignments(ctx context.Context, reviewerID uuid.UUID, limit int32) (ReviewAssignmentPage, error) {
	if reviewerID == uuid.Nil || limit < 1 || limit > maxPendingReviewLimit {
		return ReviewAssignmentPage{}, ErrTorrentReviewInput
	}
	rows, err := repository.queries.ListTorrentReviewAssignments(ctx, reviewdb.ListTorrentReviewAssignmentsParams{
		ReviewerID: reviewerID, ResultLimit: limit,
	})
	if err != nil {
		return ReviewAssignmentPage{}, fmt.Errorf("query torrent review assignments: %w", err)
	}
	page := ReviewAssignmentPage{Items: make([]ReviewAssignment, 0, len(rows))}
	for _, row := range rows {
		pending, conversionErr := pendingTorrentFromValues(
			row.ID, row.UploaderID, row.UploaderDisplayName, row.CategoryID, row.CategoryName,
			row.Title, row.Subtitle, row.ContentName, row.InfoHashV1, row.TotalSizeBytes,
			row.FileCount, row.Version, row.SubmittedAt, row.ReviewRequestedAt,
		)
		if conversionErr != nil || row.VotesCast < 0 || row.RequiredVotes != RequiredReviewVotes || row.MaximumVotes != MaximumReviewVotes {
			return ReviewAssignmentPage{}, errors.New("torrent review assignment projection is invalid")
		}
		page.Items = append(page.Items, ReviewAssignment{
			PendingTorrent: pending, VotesCast: int(row.VotesCast),
			RequiredVotes: int(row.RequiredVotes), MaximumVotes: int(row.MaximumVotes),
		})
		page.Total = row.TotalCount
	}
	return page, nil
}

func (repository *PostgresRepository) GetAssignment(ctx context.Context, reviewerID uuid.UUID, torrentID torrents.TorrentID) (ReviewAssignment, error) {
	if reviewerID == uuid.Nil || torrentID < 1 {
		return ReviewAssignment{}, ErrTorrentReviewInput
	}
	row, err := repository.queries.GetTorrentReviewAssignment(ctx, reviewdb.GetTorrentReviewAssignmentParams{
		TorrentID: int64(torrentID), ReviewerID: reviewerID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ReviewAssignment{}, ErrTorrentReviewNotFound
	}
	if err != nil {
		return ReviewAssignment{}, fmt.Errorf("query torrent review assignment: %w", err)
	}
	pending, err := pendingTorrentFromValues(
		row.ID, row.UploaderID, row.UploaderDisplayName, row.CategoryID, row.CategoryName,
		row.Title, row.Subtitle, row.ContentName, row.InfoHashV1, row.TotalSizeBytes,
		row.FileCount, row.Version, row.SubmittedAt, row.ReviewRequestedAt,
	)
	if err != nil || row.VotesCast < 0 || row.RequiredVotes != RequiredReviewVotes || row.MaximumVotes != MaximumReviewVotes {
		return ReviewAssignment{}, errors.New("torrent review assignment projection is invalid")
	}
	return ReviewAssignment{
		PendingTorrent: pending, VotesCast: int(row.VotesCast),
		RequiredVotes: int(row.RequiredVotes), MaximumVotes: int(row.MaximumVotes),
	}, nil
}

func (repository *PostgresRepository) ListReviewed(ctx context.Context, reviewerID uuid.UUID, limit int32) (ReviewedTorrentPage, error) {
	if reviewerID == uuid.Nil || limit < 1 || limit > maxPendingReviewLimit {
		return ReviewedTorrentPage{}, ErrTorrentReviewInput
	}
	rows, err := repository.queries.ListReviewedTorrentReviews(ctx, reviewdb.ListReviewedTorrentReviewsParams{
		ReviewerID: reviewerID, ResultLimit: limit,
	})
	if err != nil {
		return ReviewedTorrentPage{}, fmt.Errorf("query reviewed torrents: %w", err)
	}
	page := ReviewedTorrentPage{Items: make([]ReviewedTorrent, 0, len(rows))}
	for _, row := range rows {
		pending, conversionErr := pendingTorrentFromValues(
			row.ID, row.UploaderID, row.UploaderDisplayName, row.CategoryID, row.CategoryName,
			row.Title, row.Subtitle, row.ContentName, row.InfoHashV1, row.TotalSizeBytes,
			row.FileCount, row.Version, row.SubmittedAt, row.ReviewRequestedAt,
		)
		decision, reasonCode, outcome := Decision(row.Decision), ReasonCode(row.ReasonCode), RoundOutcome(row.Outcome)
		if conversionErr != nil || row.VoteID == uuid.Nil || row.RoundID == uuid.Nil ||
			!row.VotedAt.Valid || strings.TrimSpace(row.Reason) == "" ||
			(row.ApproveCount < 0 || row.RejectCount < 0 || int(row.ApproveCount+row.RejectCount) > MaximumReviewVotes) ||
			(decision != DecisionApprove && decision != DecisionReject) ||
			(outcome != RoundWaiting && outcome != RoundPublished && outcome != RoundRejected && outcome != RoundEscalated) ||
			(decision == DecisionApprove && reasonCode != ReasonMeetsRequirements) ||
			(decision == DecisionReject && !validRejectionReasonCode(reasonCode)) {
			return ReviewedTorrentPage{}, errors.New("reviewed torrent projection is invalid")
		}
		page.Items = append(page.Items, ReviewedTorrent{
			PendingTorrent: pending, VoteID: row.VoteID, RoundID: row.RoundID,
			Decision: decision, ReasonCode: reasonCode, Reason: row.Reason,
			VotedAt: row.VotedAt.Time.UTC(), ApproveCount: int(row.ApproveCount),
			RejectCount: int(row.RejectCount), Outcome: outcome,
		})
		page.Total = row.TotalCount
	}
	return page, nil
}

// PublishTrusted is the sole bypass of human review. It still locks the
// pending aggregate and rechecks category/object invariants, then commits the
// catalog projection, audit record and Tracker outbox through finalizeLocked.
// The exact active reseed-workgroup transition is stored on the immutable
// decision so a later suspension cannot rewrite historical authority.
func (repository *PostgresRepository) PublishTrusted(ctx context.Context, command torrents.TrustedPublishCommand) (torrents.TrustedPublishResult, error) {
	if command.DecisionID == uuid.Nil || command.TorrentID < 1 || command.UploaderID == uuid.Nil ||
		command.OccurredAt.IsZero() || !command.Authorization.Allow || command.Authorization.ID == uuid.Nil {
		return torrents.TrustedPublishResult{}, torrents.ErrTorrentInputInvalid
	}
	command.OccurredAt = command.OccurredAt.UTC().Truncate(time.Microsecond)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return torrents.TrustedPublishResult{}, fmt.Errorf("begin trusted torrent publish: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := reviewdb.New(tx)
	membershipTransitionID, err := activeMembershipTransition(
		ctx, tx, command.UploaderID, workgroups.GroupReseed, command.OccurredAt,
	)
	if err != nil {
		if errors.Is(err, ErrTorrentReviewMembership) {
			return torrents.TrustedPublishResult{}, torrents.ErrTorrentUploadStateConflict
		}
		return torrents.TrustedPublishResult{}, err
	}
	decideCommand := DecideCommand{
		DecideInput: DecideInput{
			DecisionID: command.DecisionID, TorrentID: command.TorrentID, ExpectedVersion: 1,
			Decision: DecisionApprove, ReasonCode: ReasonMeetsRequirements,
			Reason: "转种组可信发布：解析、分类、重复与存储校验均已通过。",
		},
		ReviewerID: command.UploaderID, OccurredAt: command.OccurredAt,
		Authorization: command.Authorization, Resolution: "trusted_workgroup",
		MembershipTransitionID: &membershipTransitionID,
	}
	if result, found, replayErr := resumeTorrentReviewDecision(ctx, queries, decideCommand); found || replayErr != nil {
		if replayErr != nil {
			return torrents.TrustedPublishResult{}, trustedPublishError(replayErr)
		}
		if err := tx.Commit(ctx); err != nil {
			return torrents.TrustedPublishResult{}, fmt.Errorf("commit replayed trusted torrent publish: %w", err)
		}
		return torrents.TrustedPublishResult{TorrentID: result.TorrentID, State: result.State, Version: result.Version}, nil
	}
	locked, err := queries.GetPendingTorrentReviewForUpdate(ctx, int64(command.TorrentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return torrents.TrustedPublishResult{}, torrents.ErrTorrentUploadStateConflict
	}
	if err != nil {
		return torrents.TrustedPublishResult{}, fmt.Errorf("lock trusted torrent publish target: %w", err)
	}
	if locked.UploaderID != command.UploaderID || locked.State != string(torrents.StatePendingReview) || locked.Version != 1 {
		return torrents.TrustedPublishResult{}, torrents.ErrTorrentUploadStateConflict
	}
	if !locked.CategoryEnabled {
		return torrents.TrustedPublishResult{}, torrents.ErrTorrentUploadCategoryUnavailable
	}
	if !locked.HasVerifiedLocation {
		return torrents.TrustedPublishResult{}, torrents.ErrTorrentUploadStorageUnavailable
	}
	result, err := repository.finalizeLocked(ctx, tx, queries, decideCommand, locked)
	if err != nil {
		return torrents.TrustedPublishResult{}, trustedPublishError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return torrents.TrustedPublishResult{}, fmt.Errorf("commit trusted torrent publish: %w", err)
	}
	return torrents.TrustedPublishResult{TorrentID: result.TorrentID, State: result.State, Version: result.Version}, nil
}

func (repository *PostgresRepository) Decide(ctx context.Context, command DecideCommand) (DecisionResult, error) {
	command, err := normalizedDecideCommand(command)
	if err != nil {
		return DecisionResult{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return DecisionResult{}, fmt.Errorf("begin torrent review decision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := reviewdb.New(tx)

	if result, found, resumeErr := resumeTorrentReviewDecision(ctx, queries, command); found || resumeErr != nil {
		if resumeErr != nil {
			return DecisionResult{}, resumeErr
		}
		if err := tx.Commit(ctx); err != nil {
			return DecisionResult{}, fmt.Errorf("commit replayed torrent review decision: %w", err)
		}
		return result, nil
	}

	locked, err := queries.GetPendingTorrentReviewForUpdate(ctx, int64(command.TorrentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return DecisionResult{}, ErrTorrentReviewNotFound
	}
	if err != nil {
		return DecisionResult{}, fmt.Errorf("lock torrent review target: %w", err)
	}
	if locked.State != string(torrents.StatePendingReview) || locked.Version != command.ExpectedVersion {
		// A concurrent exact retry may have waited for the first transaction's
		// row lock. Re-read the immutable command record before reporting a
		// conflict so a lost response remains safely recoverable.
		if result, found, resumeErr := resumeTorrentReviewDecision(ctx, queries, command); found || resumeErr != nil {
			if resumeErr != nil {
				return DecisionResult{}, resumeErr
			}
			if err := tx.Commit(ctx); err != nil {
				return DecisionResult{}, fmt.Errorf("commit concurrently replayed torrent review decision: %w", err)
			}
			return result, nil
		}
		if locked.Version != command.ExpectedVersion {
			return DecisionResult{}, ErrTorrentReviewVersionConflict
		}
		return DecisionResult{}, ErrTorrentReviewStateConflict
	}
	if locked.UploaderID == command.ReviewerID {
		return DecisionResult{}, ErrTorrentReviewSelf
	}
	if command.Decision == DecisionApprove && !locked.CategoryEnabled {
		return DecisionResult{}, ErrTorrentReviewCategoryUnavailable
	}
	if command.Decision == DecisionApprove && !locked.HasVerifiedLocation {
		return DecisionResult{}, ErrTorrentReviewObjectUnavailable
	}
	result, err := repository.finalizeLocked(ctx, tx, queries, command, locked)
	if err != nil {
		return DecisionResult{}, err
	}
	if command.Resolution == "staff" {
		if _, err := tx.Exec(ctx, `
UPDATE review.torrent_review_rounds
SET status = 'resolved', final_decision_id = $1, resolved_at = $2,
    version = version + 1, updated_at = $2
WHERE torrent_id = $3 AND expected_torrent_version = $4
  AND status IN ('open', 'escalated')`,
			command.DecisionID, command.OccurredAt, int64(command.TorrentID), command.ExpectedVersion); err != nil {
			return DecisionResult{}, fmt.Errorf("resolve review round by staff: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return DecisionResult{}, fmt.Errorf("commit torrent review decision: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) finalizeLocked(
	ctx context.Context,
	tx pgx.Tx,
	queries *reviewdb.Queries,
	command DecideCommand,
	locked reviewdb.GetPendingTorrentReviewForUpdateRow,
) (DecisionResult, error) {
	pending, err := pendingTorrentFromValues(
		locked.ID, locked.UploaderID, locked.UploaderDisplayName, locked.CategoryID, locked.CategoryName,
		locked.Title, locked.Subtitle, locked.ContentName, locked.InfoHashV1, locked.TotalSizeBytes,
		locked.FileCount, locked.Version, locked.SubmittedAt, locked.StateChangedAt,
	)
	if err != nil || !locked.StateChangedAt.Valid {
		return DecisionResult{}, fmt.Errorf("torrent review target has invalid persisted metadata")
	}
	aggregate := torrents.Torrent{
		ID: torrents.TorrentID(locked.ID), UploaderID: locked.UploaderID,
		State: torrents.State(locked.State), Version: locked.Version,
		SubmittedAt: locked.SubmittedAt.Time.UTC(), StateChangedAt: locked.StateChangedAt.Time.UTC(),
	}
	switch command.Decision {
	case DecisionApprove:
		err = aggregate.Publish(command.OccurredAt)
	case DecisionReject:
		err = aggregate.Reject(command.OccurredAt)
	default:
		err = ErrTorrentReviewInput
	}
	if err != nil {
		return DecisionResult{}, fmt.Errorf("apply torrent review transition: %w", err)
	}
	if err := repository.persistTransition(ctx, queries, command, pending, aggregate); err != nil {
		return DecisionResult{}, err
	}
	result := DecisionResult{
		DecisionID: command.DecisionID, TorrentID: aggregate.ID,
		Decision: command.Decision, ReasonCode: command.ReasonCode, State: aggregate.State,
		Version: aggregate.Version, OccurredAt: command.OccurredAt.UTC(),
	}
	var roundID pgtype.UUID
	if command.ReviewRoundID != nil {
		roundID = pgtype.UUID{Bytes: *command.ReviewRoundID, Valid: true}
	}
	var membershipTransitionID pgtype.UUID
	if command.MembershipTransitionID != nil {
		membershipTransitionID = pgtype.UUID{Bytes: *command.MembershipTransitionID, Valid: true}
	}
	if err := queries.InsertTorrentReviewDecision(ctx, reviewdb.InsertTorrentReviewDecisionParams{
		DecisionID: command.DecisionID, TorrentID: int64(aggregate.ID),
		ReviewerID: command.ReviewerID, ReviewDecision: string(command.Decision),
		ReasonCode: string(command.ReasonCode), Reason: command.Reason,
		ExpectedTorrentVersion: command.ExpectedVersion, ResultingTorrentVersion: aggregate.Version,
		ResultingState: string(aggregate.State), AuthorizationDecisionID: command.Authorization.ID,
		ResolutionSource: command.Resolution, ReviewRoundID: roundID,
		MembershipTransitionID: membershipTransitionID,
		OccurredAt:             reviewTimestamp(command.OccurredAt),
	}); err != nil {
		return DecisionResult{}, mapReviewWriteError("insert torrent review decision", err)
	}
	if err := repository.newNotificationAppender(tx).AppendTorrentReviewNotification(ctx, command.DecisionID); err != nil {
		return DecisionResult{}, fmt.Errorf("append torrent review notification: %w", err)
	}
	if command.Decision == DecisionApprove {
		if err := queries.InsertPublishedTorrentCatalogProjection(ctx, reviewdb.InsertPublishedTorrentCatalogProjectionParams{
			TorrentID: int64(pending.ID), CategoryID: pending.CategoryID,
			Title: pending.Title, Subtitle: pending.Subtitle, SizeBytes: pending.TotalSizeBytes,
			PublishedAt: reviewTimestamp(command.OccurredAt),
		}); err != nil {
			return DecisionResult{}, fmt.Errorf("insert published torrent catalog projection: %w", err)
		}
	}
	before := TorrentReviewAuditState{TorrentID: pending.ID, State: torrents.StatePendingReview, Version: command.ExpectedVersion}
	auditEvent, err := repository.auditBuilder.BuildTorrentReviewEvent(TorrentReviewAuditInput{
		DecisionID: command.DecisionID, ReviewerID: command.ReviewerID, UploaderID: pending.UploaderID,
		Decision: command.Decision, ReasonCode: command.ReasonCode, Reason: command.Reason,
		OccurredAt: command.OccurredAt, Authorization: command.Authorization,
		Before: before, After: TorrentReviewAuditState{TorrentID: pending.ID, State: aggregate.State, Version: aggregate.Version},
	})
	if err != nil {
		return DecisionResult{}, fmt.Errorf("build torrent review audit event: %w", err)
	}
	if err := repository.newAuditAppender(tx).Append(ctx, auditEvent); err != nil {
		return DecisionResult{}, fmt.Errorf("append torrent review audit event: %w", err)
	}
	if command.Decision == DecisionApprove {
		controlEvent, buildErr := repository.eligibilityBuilder.BuildTorrentEligibilityEvent(result, pending)
		if buildErr != nil {
			return DecisionResult{}, fmt.Errorf("build Tracker eligibility event: %w", buildErr)
		}
		if err := repository.newTrackerAppender(tx).Append(ctx, controlEvent); err != nil {
			return DecisionResult{}, fmt.Errorf("append Tracker eligibility event: %w", err)
		}
	}
	return result, nil
}

type lockedReviewRound struct {
	ID           uuid.UUID
	Status       string
	ApproveCount int
	RejectCount  int
	Version      int64
}

func (repository *PostgresRepository) Vote(ctx context.Context, command VoteCommand) (VoteResult, error) {
	command, err := normalizedVoteCommand(command)
	if err != nil {
		return VoteResult{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return VoteResult{}, fmt.Errorf("begin torrent review vote: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := reviewdb.New(tx)

	if result, found, replayErr := resumeTorrentReviewVote(ctx, tx, queries, command); found || replayErr != nil {
		if replayErr != nil {
			return VoteResult{}, replayErr
		}
		if err := tx.Commit(ctx); err != nil {
			return VoteResult{}, fmt.Errorf("commit replayed torrent review vote: %w", err)
		}
		return result, nil
	}

	locked, err := queries.GetPendingTorrentReviewForUpdate(ctx, int64(command.TorrentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return VoteResult{}, ErrTorrentReviewNotFound
	}
	if err != nil {
		return VoteResult{}, fmt.Errorf("lock torrent review vote target: %w", err)
	}
	if locked.State != string(torrents.StatePendingReview) || locked.Version != command.ExpectedVersion {
		if result, found, replayErr := resumeTorrentReviewVote(ctx, tx, queries, command); found || replayErr != nil {
			if replayErr != nil {
				return VoteResult{}, replayErr
			}
			if err := tx.Commit(ctx); err != nil {
				return VoteResult{}, fmt.Errorf("commit concurrently replayed torrent review vote: %w", err)
			}
			return result, nil
		}
		if locked.Version != command.ExpectedVersion {
			return VoteResult{}, ErrTorrentReviewVersionConflict
		}
		return VoteResult{}, ErrTorrentReviewStateConflict
	}
	if locked.UploaderID == command.VoterID {
		return VoteResult{}, ErrTorrentReviewSelf
	}
	if command.Decision == DecisionApprove && !locked.CategoryEnabled {
		return VoteResult{}, ErrTorrentReviewCategoryUnavailable
	}
	if command.Decision == DecisionApprove && !locked.HasVerifiedLocation {
		return VoteResult{}, ErrTorrentReviewObjectUnavailable
	}

	membershipTransitionID, err := activeMembershipTransition(ctx, tx, command.VoterID, workgroups.GroupReview, command.OccurredAt)
	if err != nil {
		return VoteResult{}, err
	}
	round, err := getOrCreateReviewRound(ctx, tx, command)
	if err != nil {
		return VoteResult{}, err
	}
	if round.Status == "escalated" {
		return VoteResult{}, ErrTorrentReviewRoundEscalated
	}
	if round.Status != "open" {
		return VoteResult{}, ErrTorrentReviewStateConflict
	}

	var alreadyVoted bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM review.torrent_review_votes
    WHERE round_id = $1 AND voter_id = $2
)`, round.ID, command.VoterID).Scan(&alreadyVoted); err != nil {
		return VoteResult{}, fmt.Errorf("check existing torrent review vote: %w", err)
	}
	if alreadyVoted {
		return VoteResult{}, ErrTorrentReviewAlreadyVoted
	}

	approveCount, rejectCount := round.ApproveCount, round.RejectCount
	if command.Decision == DecisionApprove {
		approveCount++
	} else {
		rejectCount++
	}
	outcome := resolveRound(approveCount, rejectCount)
	_, err = tx.Exec(ctx, `
INSERT INTO review.torrent_review_votes (
    id, round_id, torrent_id, voter_id, membership_transition_id,
    decision, reason_code, reason, expected_torrent_version,
    authorization_decision_id, outcome_after_vote,
    approve_count_after, reject_count_after, occurred_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		command.VoteID, round.ID, int64(command.TorrentID), command.VoterID, membershipTransitionID,
		command.Decision, command.ReasonCode, command.Reason, command.ExpectedVersion,
		command.Authorization.ID, outcome, approveCount, rejectCount, command.OccurredAt)
	if err != nil {
		return VoteResult{}, mapVoteWriteError(err)
	}

	var finalDecision *DecisionResult
	status := "open"
	var finalDecisionID, resolvedAt, escalatedAt any
	switch outcome {
	case RoundPublished, RoundRejected:
		decision := command.Decision
		reasonCode := command.ReasonCode
		if outcome == RoundPublished {
			decision, reasonCode = DecisionApprove, ReasonMeetsRequirements
		} else {
			decision = DecisionReject
			if reasonCode == ReasonMeetsRequirements {
				reasonCode = ReasonOther
			}
		}
		result, finalizeErr := repository.finalizeLocked(ctx, tx, queries, DecideCommand{
			DecideInput: DecideInput{
				DecisionID: command.VoteID, TorrentID: command.TorrentID,
				ExpectedVersion: command.ExpectedVersion, Decision: decision,
				ReasonCode: reasonCode, Reason: command.Reason,
			},
			ReviewerID: command.VoterID, OccurredAt: command.OccurredAt,
			Authorization: command.Authorization, Resolution: "review_round", ReviewRoundID: &round.ID,
		}, locked)
		if finalizeErr != nil {
			return VoteResult{}, finalizeErr
		}
		status, finalDecisionID, resolvedAt = "resolved", command.VoteID, command.OccurredAt
		finalDecision = &result
	case RoundEscalated:
		status, escalatedAt = "escalated", command.OccurredAt
	case RoundWaiting:
	default:
		return VoteResult{}, ErrTorrentReviewStateConflict
	}
	result, err := tx.Exec(ctx, `
UPDATE review.torrent_review_rounds
SET status = $1, approve_count = $2, reject_count = $3,
    final_decision_id = $4, resolved_at = $5, escalated_at = $6,
    version = version + 1, updated_at = $7
WHERE id = $8 AND status = 'open' AND version = $9`,
		status, approveCount, rejectCount, finalDecisionID, resolvedAt, escalatedAt,
		command.OccurredAt, round.ID, round.Version)
	if err != nil {
		return VoteResult{}, fmt.Errorf("advance torrent review round: %w", err)
	}
	if result.RowsAffected() != 1 {
		return VoteResult{}, ErrTorrentReviewStateConflict
	}

	voteResult := VoteResult{
		VoteID: command.VoteID, RoundID: round.ID, TorrentID: command.TorrentID,
		Decision: command.Decision, VotesCast: approveCount + rejectCount,
		RequiredVotes: RequiredReviewVotes, MaximumVotes: MaximumReviewVotes,
		Outcome: outcome, FinalDecision: finalDecision, VotedAt: command.OccurredAt,
	}
	if err := tx.Commit(ctx); err != nil {
		return VoteResult{}, fmt.Errorf("commit torrent review vote: %w", err)
	}
	return voteResult, nil
}

func activeMembershipTransition(ctx context.Context, tx pgx.Tx, userID uuid.UUID, groupKind workgroups.GroupKind, at time.Time) (uuid.UUID, error) {
	var transitionID uuid.UUID
	var status string
	err := tx.QueryRow(ctx, `
SELECT id, to_status
FROM workgroups.membership_transitions
WHERE user_id = $1 AND group_kind = $2 AND occurred_at <= $3
ORDER BY occurred_at DESC, state_version DESC
LIMIT 1`, userID, groupKind, at).Scan(&transitionID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrTorrentReviewMembership
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve review membership evidence: %w", err)
	}
	if status != "active" {
		return uuid.Nil, ErrTorrentReviewMembership
	}
	return transitionID, nil
}

func getOrCreateReviewRound(ctx context.Context, tx pgx.Tx, command VoteCommand) (lockedReviewRound, error) {
	var round lockedReviewRound
	err := tx.QueryRow(ctx, `
SELECT id, status, approve_count, reject_count, version
FROM review.torrent_review_rounds
WHERE torrent_id = $1 AND expected_torrent_version = $2
FOR UPDATE`, int64(command.TorrentID), command.ExpectedVersion).Scan(
		&round.ID, &round.Status, &round.ApproveCount, &round.RejectCount, &round.Version,
	)
	if err == nil {
		return round, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return lockedReviewRound{}, fmt.Errorf("lock torrent review round: %w", err)
	}
	round = lockedReviewRound{ID: uuid.New(), Status: "open", Version: 1}
	_, err = tx.Exec(ctx, `
INSERT INTO review.torrent_review_rounds (
    id, torrent_id, expected_torrent_version, status,
    required_votes, maximum_votes, opened_at, updated_at
) VALUES ($1, $2, $3, 'open', $4, $5, $6, $6)`,
		round.ID, int64(command.TorrentID), command.ExpectedVersion,
		RequiredReviewVotes, MaximumReviewVotes, command.OccurredAt)
	if err != nil {
		return lockedReviewRound{}, fmt.Errorf("create torrent review round: %w", err)
	}
	return round, nil
}

func resumeTorrentReviewVote(ctx context.Context, tx pgx.Tx, queries *reviewdb.Queries, command VoteCommand) (VoteResult, bool, error) {
	var result VoteResult
	var decision, reasonCode, reason, outcome string
	var voterID uuid.UUID
	var expectedVersion int64
	var approveCount, rejectCount int
	err := tx.QueryRow(ctx, `
SELECT id, round_id, torrent_id, voter_id, decision, reason_code, reason,
       expected_torrent_version, outcome_after_vote,
       approve_count_after, reject_count_after, occurred_at
FROM review.torrent_review_votes
WHERE id = $1`, command.VoteID).Scan(
		&result.VoteID, &result.RoundID, &result.TorrentID, &voterID,
		&decision, &reasonCode, &reason, &expectedVersion, &outcome,
		&approveCount, &rejectCount, &result.VotedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return VoteResult{}, false, nil
	}
	if err != nil {
		return VoteResult{}, false, fmt.Errorf("read torrent review vote: %w", err)
	}
	if result.TorrentID != command.TorrentID || voterID != command.VoterID ||
		Decision(decision) != command.Decision || ReasonCode(reasonCode) != command.ReasonCode ||
		reason != command.Reason || expectedVersion != command.ExpectedVersion {
		return VoteResult{}, true, ErrTorrentReviewIdempotencyConflict
	}
	result.Decision = Decision(decision)
	result.VotesCast = approveCount + rejectCount
	result.RequiredVotes = RequiredReviewVotes
	result.MaximumVotes = MaximumReviewVotes
	result.Outcome = RoundOutcome(outcome)
	if result.Outcome == RoundPublished || result.Outcome == RoundRejected {
		row, rowErr := queries.GetTorrentReviewDecision(ctx, command.VoteID)
		if rowErr != nil {
			return VoteResult{}, true, fmt.Errorf("read vote final decision: %w", rowErr)
		}
		final := DecisionResult{
			DecisionID: row.ID, TorrentID: torrents.TorrentID(row.TorrentID),
			Decision: Decision(row.Decision), ReasonCode: ReasonCode(row.ReasonCode),
			State: torrents.State(row.ResultingState), Version: row.ResultingTorrentVersion,
			OccurredAt: row.OccurredAt.Time.UTC(),
		}
		result.FinalDecision = &final
	}
	return result, true, nil
}

func normalizedVoteCommand(command VoteCommand) (VoteCommand, error) {
	normalized, err := normalizeVoteInput(command.VoteInput)
	if err != nil || command.VoterID == uuid.Nil || command.OccurredAt.IsZero() ||
		!command.Authorization.Allow || command.Authorization.ID == uuid.Nil {
		return VoteCommand{}, ErrTorrentReviewInput
	}
	command.VoteInput = normalized
	command.OccurredAt = command.OccurredAt.UTC().Truncate(time.Microsecond)
	return command, nil
}

func mapVoteWriteError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		switch postgresError.ConstraintName {
		case "torrent_review_votes_pkey":
			return ErrTorrentReviewIdempotencyConflict
		case "torrent_review_votes_round_id_voter_id_key":
			return ErrTorrentReviewAlreadyVoted
		}
	}
	return fmt.Errorf("insert torrent review vote: %w", err)
}

func (repository *PostgresRepository) persistTransition(ctx context.Context, queries *reviewdb.Queries, command DecideCommand, pending PendingTorrent, aggregate torrents.Torrent) error {
	switch command.Decision {
	case DecisionApprove:
		row, err := queries.PublishReviewedTorrent(ctx, reviewdb.PublishReviewedTorrentParams{
			OccurredAt: reviewTimestamp(command.OccurredAt), TorrentID: int64(aggregate.ID), ExpectedVersion: command.ExpectedVersion,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTorrentReviewVersionConflict
		}
		if err != nil {
			return fmt.Errorf("publish reviewed torrent: %w", err)
		}
		if row.ID != int64(pending.ID) || row.State != string(aggregate.State) || row.Version != aggregate.Version ||
			!row.PublishedAt.Valid || !row.StateChangedAt.Valid || !row.StateChangedAt.Time.Equal(command.OccurredAt) {
			return ErrTorrentReviewStateConflict
		}
	case DecisionReject:
		row, err := queries.RejectReviewedTorrent(ctx, reviewdb.RejectReviewedTorrentParams{
			OccurredAt: reviewTimestamp(command.OccurredAt), TorrentID: int64(aggregate.ID), ExpectedVersion: command.ExpectedVersion,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTorrentReviewVersionConflict
		}
		if err != nil {
			return fmt.Errorf("reject reviewed torrent: %w", err)
		}
		if row.ID != int64(pending.ID) || row.State != string(aggregate.State) || row.Version != aggregate.Version ||
			row.PublishedAt.Valid || !row.StateChangedAt.Valid || !row.StateChangedAt.Time.Equal(command.OccurredAt) {
			return ErrTorrentReviewStateConflict
		}
	default:
		return ErrTorrentReviewInput
	}
	return nil
}

func resumeTorrentReviewDecision(ctx context.Context, queries *reviewdb.Queries, command DecideCommand) (DecisionResult, bool, error) {
	row, err := queries.GetTorrentReviewDecision(ctx, command.DecisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return DecisionResult{}, false, nil
	}
	if err != nil {
		return DecisionResult{}, false, fmt.Errorf("read torrent review decision: %w", err)
	}
	if torrents.TorrentID(row.TorrentID) != command.TorrentID || row.ReviewerID != command.ReviewerID ||
		row.Decision != string(command.Decision) || row.ReasonCode != string(command.ReasonCode) ||
		row.Reason != command.Reason || row.ExpectedTorrentVersion != command.ExpectedVersion ||
		row.ResolutionSource != command.Resolution ||
		(command.ReviewRoundID == nil && row.ReviewRoundID.Valid) ||
		(command.ReviewRoundID != nil && (!row.ReviewRoundID.Valid || row.ReviewRoundID.Bytes != *command.ReviewRoundID)) ||
		(command.MembershipTransitionID == nil && row.MembershipTransitionID.Valid) ||
		(command.MembershipTransitionID != nil && (!row.MembershipTransitionID.Valid || row.MembershipTransitionID.Bytes != *command.MembershipTransitionID)) {
		return DecisionResult{}, true, ErrTorrentReviewIdempotencyConflict
	}
	if !row.OccurredAt.Valid || row.TorrentID < 1 || row.ResultingTorrentVersion != command.ExpectedVersion+1 {
		return DecisionResult{}, true, ErrTorrentReviewStateConflict
	}
	state := torrents.State(row.ResultingState)
	if (command.Decision == DecisionApprove && state != torrents.StatePublished) ||
		(command.Decision == DecisionReject && state != torrents.StateRejected) {
		return DecisionResult{}, true, ErrTorrentReviewStateConflict
	}
	return DecisionResult{
		DecisionID: row.ID, TorrentID: torrents.TorrentID(row.TorrentID),
		Decision: Decision(row.Decision), ReasonCode: ReasonCode(row.ReasonCode), State: state,
		Version: row.ResultingTorrentVersion, OccurredAt: row.OccurredAt.Time.UTC(),
	}, true, nil
}

func pendingTorrentFromValues(torrentID int64, uploaderID uuid.UUID, uploaderName, categoryID, categoryName, title, subtitle, contentName string, rawHash []byte, totalSize int64, fileCount int32, version int64, submittedAt, reviewRequestedAt pgtype.Timestamptz) (PendingTorrent, error) {
	if torrentID < 1 || uploaderID == uuid.Nil || uploaderName == "" || categoryID == "" || categoryName == "" ||
		title == "" || contentName == "" || len(rawHash) != 20 || totalSize < 1 || fileCount < 1 || version < 1 ||
		!submittedAt.Valid || !reviewRequestedAt.Valid || reviewRequestedAt.Time.Before(submittedAt.Time) {
		return PendingTorrent{}, errors.New("pending torrent review projection is invalid")
	}
	var infoHash torrents.InfoHashV1
	copy(infoHash[:], rawHash)
	return PendingTorrent{
		ID: torrents.TorrentID(torrentID), UploaderID: uploaderID, UploaderDisplayName: uploaderName,
		CategoryID: categoryID, CategoryName: categoryName, Title: title, Subtitle: subtitle,
		ContentName: contentName, InfoHashV1: infoHash, TotalSizeBytes: totalSize,
		FileCount: int(fileCount), Version: version, SubmittedAt: submittedAt.Time.UTC(),
		ReviewRequestedAt: reviewRequestedAt.Time.UTC(),
	}, nil
}

func normalizedDecideCommand(command DecideCommand) (DecideCommand, error) {
	normalized, err := normalizeDecideInput(command.DecideInput)
	if command.Resolution == "" {
		command.Resolution = "staff"
	}
	if err != nil || command.ReviewerID == uuid.Nil ||
		command.OccurredAt.IsZero() || !command.Authorization.Allow || command.Authorization.ID == uuid.Nil ||
		(command.Resolution != "staff" && command.Resolution != "review_round" && command.Resolution != "trusted_workgroup") ||
		(command.Resolution == "review_round" && command.ReviewRoundID == nil) ||
		(command.Resolution != "review_round" && command.ReviewRoundID != nil) ||
		(command.Resolution == "trusted_workgroup" && command.MembershipTransitionID == nil) ||
		(command.Resolution != "trusted_workgroup" && command.MembershipTransitionID != nil) {
		return DecideCommand{}, ErrTorrentReviewInput
	}
	// Persist and compare the same canonical reason text even if a repository
	// caller is introduced outside the HTTP service in the future.
	command.DecideInput = normalized
	return command, nil
}

func trustedPublishError(err error) error {
	switch {
	case errors.Is(err, ErrTorrentReviewCategoryUnavailable):
		return torrents.ErrTorrentUploadCategoryUnavailable
	case errors.Is(err, ErrTorrentReviewObjectUnavailable):
		return torrents.ErrTorrentUploadStorageUnavailable
	case errors.Is(err, ErrTorrentReviewNotFound), errors.Is(err, ErrTorrentReviewVersionConflict),
		errors.Is(err, ErrTorrentReviewStateConflict), errors.Is(err, ErrTorrentReviewIdempotencyConflict):
		return torrents.ErrTorrentUploadStateConflict
	default:
		return err
	}
}

func mapReviewWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		switch postgresError.ConstraintName {
		case "torrent_decisions_pkey":
			// Two different targets can be locked concurrently. If they race with
			// the same command UUID, this is an idempotency-key conflict rather
			// than a misleading aggregate version conflict.
			return ErrTorrentReviewIdempotencyConflict
		case "torrent_decisions_torrent_id_expected_torrent_version_key":
			return ErrTorrentReviewVersionConflict
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func reviewTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

var _ Repository = (*PostgresRepository)(nil)
