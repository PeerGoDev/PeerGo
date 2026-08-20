package authz

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/generated/authzdb"
)

type TransactionEventAppenderFactory func(pgx.Tx) auditevent.Appender

// PostgresGrantAdministrationRepository owns the transaction boundary for a
// grant change. Every state transition and its reviewed audit event are
// committed together; an event-construction or outbox failure rolls back the
// business mutation.
type PostgresGrantAdministrationRepository struct {
	pool          *pgxpool.Pool
	eventBuilder  GrantRevocationEventBuilder
	newAppender   TransactionEventAppenderFactory
	overviewQuery *authzdb.Queries
}

func NewPostgresGrantAdministrationRepository(pool *pgxpool.Pool, eventBuilder GrantRevocationEventBuilder, newAppender TransactionEventAppenderFactory) (*PostgresGrantAdministrationRepository, error) {
	if pool == nil || eventBuilder == nil || newAppender == nil {
		return nil, errors.New("grant administration repository dependencies are required")
	}
	return &PostgresGrantAdministrationRepository{
		pool:          pool,
		eventBuilder:  eventBuilder,
		newAppender:   newAppender,
		overviewQuery: authzdb.New(pool),
	}, nil
}

func (repository *PostgresGrantAdministrationRepository) Overview(ctx context.Context, now time.Time) (GrantAdministrationOverview, error) {
	grantRows, err := repository.overviewQuery.ListGrantAdministrationGrants(ctx)
	if err != nil {
		return GrantAdministrationOverview{}, fmt.Errorf("list grants: %w", err)
	}
	grants := make([]GrantAdministrationGrant, 0, len(grantRows))
	for _, row := range grantRows {
		if !row.ValidFrom.Valid || !row.ValidUntil.Valid {
			return GrantAdministrationOverview{}, errors.New("grant administration row contains an invalid timestamp")
		}
		var revokedAt *time.Time
		if row.RevokedAt.Valid {
			value := row.RevokedAt.Time.UTC()
			revokedAt = &value
		}
		grants = append(grants, GrantAdministrationGrant{
			ID:                 row.ID,
			SubjectID:          row.SubjectID,
			SubjectUsername:    row.SubjectUsername,
			SubjectDisplayName: row.SubjectDisplayName,
			RoleID:             row.RoleID,
			RoleName:           row.RoleName,
			MandateID:          row.MandateID,
			MandateStatus:      MandateStatus(row.MandateStatus),
			Scope:              Scope{Type: ScopeType(row.ScopeType), ID: row.ScopeID},
			ValidFrom:          row.ValidFrom.Time.UTC(),
			ValidUntil:         row.ValidUntil.Time.UTC(),
			Version:            row.Version,
			RevokedAt:          revokedAt,
		})
	}

	requestRows, err := repository.overviewQuery.ListGrantRevocationRequests(ctx)
	if err != nil {
		return GrantAdministrationOverview{}, fmt.Errorf("list grant revocation requests: %w", err)
	}
	requests := make([]GrantRevocationRequest, 0, len(requestRows))
	requestIndexes := make(map[uuid.UUID]int, len(requestRows))
	requestIDs := make([]uuid.UUID, 0, len(requestRows))
	for _, row := range requestRows {
		request, err := grantRevocationRequestFromRow(row)
		if err != nil {
			return GrantAdministrationOverview{}, err
		}
		// Expiry is projected immediately for reads. A later authorized write
		// persists this transition and its audit event before reusing the grant.
		if request.Status == GrantRevocationPendingStatus && !now.Before(request.ExpiresAt) {
			request.Status = GrantRevocationExpiredStatus
		}
		requestIndexes[request.ID] = len(requests)
		requestIDs = append(requestIDs, request.ID)
		requests = append(requests, request)
	}
	if len(requestIDs) > 0 {
		reviewRows, err := repository.overviewQuery.ListGrantRevocationReviews(ctx, requestIDs)
		if err != nil {
			return GrantAdministrationOverview{}, fmt.Errorf("list grant revocation reviews: %w", err)
		}
		for _, row := range reviewRows {
			index, ok := requestIndexes[row.RequestID]
			if !ok {
				return GrantAdministrationOverview{}, errors.New("grant revocation review has no listed request")
			}
			review, err := grantRevocationReviewFromValues(row.ID, row.ReviewerID, row.DutyDomain, row.Decision, row.Reason, row.CreatedAt)
			if err != nil {
				return GrantAdministrationOverview{}, err
			}
			requests[index].Reviews = append(requests[index].Reviews, review)
		}
	}

	return GrantAdministrationOverview{Grants: grants, Requests: requests}, nil
}

func (repository *PostgresGrantAdministrationRepository) CreateRevocation(ctx context.Context, command CreateGrantRevocationCommand) (GrantRevocationRequest, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return GrantRevocationRequest{}, fmt.Errorf("begin grant revocation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := authzdb.New(tx)
	appender := repository.newAppender(tx)
	if appender == nil {
		return GrantRevocationRequest{}, errors.New("grant audit appender factory returned nil")
	}

	target, err := queries.GetGrantAdministrationTargetForUpdate(ctx, command.GrantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return GrantRevocationRequest{}, ErrGrantNotFound
	}
	if err != nil {
		return GrantRevocationRequest{}, fmt.Errorf("lock grant target: %w", err)
	}
	if target.SubjectID == command.ProposerID {
		return GrantRevocationRequest{}, ErrSeparationOfDuties
	}
	if target.RevokedAt.Valid {
		return GrantRevocationRequest{}, ErrGrantAlreadyRevoked
	}
	if target.Version != command.ExpectedGrantVersion {
		return GrantRevocationRequest{}, ErrGrantVersionConflict
	}

	pending, err := queries.GetPendingGrantRevocationForUpdate(ctx, command.GrantID)
	switch {
	case err == nil:
		existing, mapErr := grantRevocationRequestFromRow(pending)
		if mapErr != nil {
			return GrantRevocationRequest{}, mapErr
		}
		if command.CreatedAt.Before(existing.ExpiresAt) {
			return GrantRevocationRequest{}, ErrGrantRevocationPending
		}
		reviews, loadErr := loadGrantRevocationReviews(ctx, queries, existing.ID)
		if loadErr != nil {
			return GrantRevocationRequest{}, loadErr
		}
		before := auditState(existing.Status, target.Version, target.RevokedAt.Valid, reviews)
		if _, err := queries.ExpireGrantRevocationRequest(ctx, authzdb.ExpireGrantRevocationRequestParams{
			ResolvedAt: grantAdminTimestamp(command.CreatedAt),
			RequestID:  existing.ID,
		}); err != nil {
			return GrantRevocationRequest{}, fmt.Errorf("expire previous grant revocation request: %w", err)
		}
		after := before
		after.Status = GrantRevocationExpiredStatus
		if err := repository.appendTransition(ctx, appender, GrantRevocationAuditInput{
			Transition:           GrantTransitionExpired,
			OccurredAt:           command.CreatedAt,
			RequestID:            existing.ID,
			GrantID:              existing.GrantID,
			ExpectedGrantVersion: existing.ExpectedGrantVersion,
			ActorID:              command.ProposerID,
			TargetSubjectID:      existing.TargetSubjectID,
			Reason:               existing.Reason,
			Authorization:        command.Authorization,
			Before:               before,
			After:                after,
		}); err != nil {
			return GrantRevocationRequest{}, err
		}
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return GrantRevocationRequest{}, fmt.Errorf("check pending grant revocation: %w", err)
	}

	row, err := queries.InsertGrantRevocationRequest(ctx, authzdb.InsertGrantRevocationRequestParams{
		ID:                   command.ID,
		GrantID:              command.GrantID,
		ExpectedGrantVersion: command.ExpectedGrantVersion,
		TargetSubjectID:      target.SubjectID,
		ProposerID:           command.ProposerID,
		Reason:               command.Reason,
		CreatedAt:            grantAdminTimestamp(command.CreatedAt),
		ExpiresAt:            grantAdminTimestamp(command.ExpiresAt),
	})
	if err != nil {
		return GrantRevocationRequest{}, mapGrantAdministrationWriteError("insert grant revocation request", err)
	}
	request, err := grantRevocationRequestFromRow(row)
	if err != nil {
		return GrantRevocationRequest{}, err
	}
	if err := repository.appendTransition(ctx, appender, GrantRevocationAuditInput{
		Transition:           GrantTransitionProposed,
		OccurredAt:           command.CreatedAt,
		RequestID:            request.ID,
		GrantID:              request.GrantID,
		ExpectedGrantVersion: request.ExpectedGrantVersion,
		ActorID:              command.ProposerID,
		TargetSubjectID:      request.TargetSubjectID,
		Reason:               request.Reason,
		Authorization:        command.Authorization,
		Before:               GrantRevocationAuditState{GrantVersion: target.Version},
		After: GrantRevocationAuditState{
			Status:       GrantRevocationPendingStatus,
			GrantVersion: target.Version,
		},
	}); err != nil {
		return GrantRevocationRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return GrantRevocationRequest{}, fmt.Errorf("commit grant revocation request: %w", err)
	}
	return request, nil
}

func (repository *PostgresGrantAdministrationRepository) ReviewRevocation(ctx context.Context, command ReviewGrantRevocationCommand) (GrantRevocationRequest, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return GrantRevocationRequest{}, fmt.Errorf("begin grant review transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := authzdb.New(tx)
	appender := repository.newAppender(tx)
	if appender == nil {
		return GrantRevocationRequest{}, errors.New("grant audit appender factory returned nil")
	}

	locked, err := queries.GetGrantRevocationForUpdate(ctx, command.RequestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return GrantRevocationRequest{}, ErrGrantRevocationNotFound
	}
	if err != nil {
		return GrantRevocationRequest{}, fmt.Errorf("lock grant revocation request: %w", err)
	}
	request, err := grantRevocationRequestFromLockedRow(locked)
	if err != nil {
		return GrantRevocationRequest{}, err
	}
	if request.Status != GrantRevocationPendingStatus {
		return GrantRevocationRequest{}, ErrGrantRevocationClosed
	}
	if command.ReviewerID == request.ProposerID || command.ReviewerID == request.TargetSubjectID {
		return GrantRevocationRequest{}, ErrSeparationOfDuties
	}
	reviews, err := loadGrantRevocationReviews(ctx, queries, request.ID)
	if err != nil {
		return GrantRevocationRequest{}, err
	}
	request.Reviews = reviews
	before := auditState(request.Status, locked.CurrentGrantVersion, locked.GrantRevokedAt.Valid, reviews)

	if !command.CreatedAt.Before(request.ExpiresAt) {
		if _, err := queries.ExpireGrantRevocationRequest(ctx, authzdb.ExpireGrantRevocationRequestParams{
			ResolvedAt: grantAdminTimestamp(command.CreatedAt),
			RequestID:  request.ID,
		}); err != nil {
			return GrantRevocationRequest{}, fmt.Errorf("expire grant revocation request: %w", err)
		}
		after := before
		after.Status = GrantRevocationExpiredStatus
		if err := repository.appendTransition(ctx, appender, GrantRevocationAuditInput{
			Transition:           GrantTransitionExpired,
			OccurredAt:           command.CreatedAt,
			RequestID:            request.ID,
			GrantID:              request.GrantID,
			ExpectedGrantVersion: request.ExpectedGrantVersion,
			ActorID:              command.ReviewerID,
			TargetSubjectID:      request.TargetSubjectID,
			Reason:               request.Reason,
			Authorization:        command.Authorization,
			Before:               before,
			After:                after,
		}); err != nil {
			return GrantRevocationRequest{}, err
		}
		request.Status = GrantRevocationExpiredStatus
		resolvedAt := command.CreatedAt
		request.ResolvedAt = &resolvedAt
		if err := tx.Commit(ctx); err != nil {
			return GrantRevocationRequest{}, fmt.Errorf("commit expired grant revocation request: %w", err)
		}
		return request, nil
	}

	if locked.GrantRevokedAt.Valid || locked.CurrentGrantVersion != request.ExpectedGrantVersion {
		if _, err := queries.ConflictGrantRevocationRequest(ctx, authzdb.ConflictGrantRevocationRequestParams{
			ResolvedAt: grantAdminTimestamp(command.CreatedAt),
			RequestID:  request.ID,
		}); err != nil {
			return GrantRevocationRequest{}, fmt.Errorf("conflict grant revocation request: %w", err)
		}
		after := before
		after.Status = GrantRevocationConflictedStatus
		if err := repository.appendTransition(ctx, appender, GrantRevocationAuditInput{
			Transition:           GrantTransitionConflicted,
			OccurredAt:           command.CreatedAt,
			RequestID:            request.ID,
			GrantID:              request.GrantID,
			ExpectedGrantVersion: request.ExpectedGrantVersion,
			ActorID:              command.ReviewerID,
			TargetSubjectID:      request.TargetSubjectID,
			Reason:               command.Reason,
			Authorization:        command.Authorization,
			Before:               before,
			After:                after,
		}); err != nil {
			return GrantRevocationRequest{}, err
		}
		request.Status = GrantRevocationConflictedStatus
		resolvedAt := command.CreatedAt
		request.ResolvedAt = &resolvedAt
		if err := tx.Commit(ctx); err != nil {
			return GrantRevocationRequest{}, fmt.Errorf("commit conflicted grant revocation request: %w", err)
		}
		return request, nil
	}

	for _, review := range reviews {
		if review.Domain == command.Domain || review.ReviewerID == command.ReviewerID {
			return GrantRevocationRequest{}, ErrGrantReviewExists
		}
	}
	inserted, err := queries.InsertGrantRevocationReview(ctx, authzdb.InsertGrantRevocationReviewParams{
		ID:              command.ReviewID,
		RequestID:       request.ID,
		ProposerID:      request.ProposerID,
		TargetSubjectID: request.TargetSubjectID,
		ReviewerID:      command.ReviewerID,
		DutyDomain:      string(command.Domain),
		Decision:        string(command.Decision),
		Reason:          command.Reason,
		CreatedAt:       grantAdminTimestamp(command.CreatedAt),
	})
	if err != nil {
		return GrantRevocationRequest{}, mapGrantAdministrationWriteError("insert grant revocation review", err)
	}
	review, err := grantRevocationReviewFromValues(inserted.ID, inserted.ReviewerID, inserted.DutyDomain, inserted.Decision, inserted.Reason, inserted.CreatedAt)
	if err != nil {
		return GrantRevocationRequest{}, err
	}
	request.Reviews = append(request.Reviews, review)
	after := auditState(GrantRevocationPendingStatus, locked.CurrentGrantVersion, false, request.Reviews)
	transition := GrantTransitionGovernanceApproved
	if command.Domain == GrantReviewSecurity {
		transition = GrantTransitionSecurityApproved
	}

	if command.Decision == GrantReviewReject {
		if _, err := queries.RejectGrantRevocationRequest(ctx, authzdb.RejectGrantRevocationRequestParams{
			ResolvedAt: grantAdminTimestamp(command.CreatedAt),
			RequestID:  request.ID,
		}); err != nil {
			return GrantRevocationRequest{}, fmt.Errorf("reject grant revocation request: %w", err)
		}
		transition = GrantTransitionRejected
		after.Status = GrantRevocationRejectedStatus
		request.Status = GrantRevocationRejectedStatus
		resolvedAt := command.CreatedAt
		request.ResolvedAt = &resolvedAt
	} else if approvalsComplete(request.Reviews) {
		resultingVersion, err := queries.ApplyGrantRevocation(ctx, authzdb.ApplyGrantRevocationParams{
			RevokedAt:            grantAdminTimestamp(command.CreatedAt),
			GrantID:              request.GrantID,
			ExpectedGrantVersion: request.ExpectedGrantVersion,
		})
		if err != nil {
			return GrantRevocationRequest{}, fmt.Errorf("apply grant revocation: %w", err)
		}
		if _, err := queries.ApplyGrantRevocationRequest(ctx, authzdb.ApplyGrantRevocationRequestParams{
			ResultingGrantVersion: pgtype.Int8{Int64: resultingVersion, Valid: true},
			ResolvedAt:            grantAdminTimestamp(command.CreatedAt),
			RequestID:             request.ID,
		}); err != nil {
			return GrantRevocationRequest{}, fmt.Errorf("resolve applied grant revocation request: %w", err)
		}
		transition = GrantTransitionApplied
		after.Status = GrantRevocationAppliedStatus
		after.GrantVersion = resultingVersion
		after.GrantRevoked = true
		request.Status = GrantRevocationAppliedStatus
		request.ResultingGrantVersion = resultingVersion
		resolvedAt := command.CreatedAt
		request.ResolvedAt = &resolvedAt
	}

	if err := repository.appendTransition(ctx, appender, GrantRevocationAuditInput{
		Transition:            transition,
		OccurredAt:            command.CreatedAt,
		RequestID:             request.ID,
		GrantID:               request.GrantID,
		ExpectedGrantVersion:  request.ExpectedGrantVersion,
		ResultingGrantVersion: request.ResultingGrantVersion,
		ActorID:               command.ReviewerID,
		TargetSubjectID:       request.TargetSubjectID,
		Reason:                command.Reason,
		Authorization:         command.Authorization,
		ReviewID:              command.ReviewID,
		ReviewDomain:          command.Domain,
		ReviewDecision:        command.Decision,
		Before:                before,
		After:                 after,
	}); err != nil {
		return GrantRevocationRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return GrantRevocationRequest{}, fmt.Errorf("commit grant revocation review: %w", err)
	}
	return request, nil
}

func (repository *PostgresGrantAdministrationRepository) appendTransition(ctx context.Context, appender auditevent.Appender, input GrantRevocationAuditInput) error {
	event, err := repository.eventBuilder.BuildGrantRevocationEvent(input)
	if err != nil {
		return fmt.Errorf("build grant revocation audit event: %w", err)
	}
	if err := appender.Append(ctx, event); err != nil {
		return fmt.Errorf("append grant revocation audit event: %w", err)
	}
	return nil
}

func loadGrantRevocationReviews(ctx context.Context, queries *authzdb.Queries, requestID uuid.UUID) ([]GrantRevocationReview, error) {
	rows, err := queries.ListGrantRevocationReviewsForRequest(ctx, requestID)
	if err != nil {
		return nil, fmt.Errorf("list grant revocation reviews for request: %w", err)
	}
	reviews := make([]GrantRevocationReview, 0, len(rows))
	for _, row := range rows {
		review, err := grantRevocationReviewFromValues(row.ID, row.ReviewerID, row.DutyDomain, row.Decision, row.Reason, row.CreatedAt)
		if err != nil {
			return nil, err
		}
		reviews = append(reviews, review)
	}
	return reviews, nil
}

func grantRevocationRequestFromLockedRow(row authzdb.GetGrantRevocationForUpdateRow) (GrantRevocationRequest, error) {
	return grantRevocationRequestFromValues(
		row.ID,
		row.GrantID,
		row.ExpectedGrantVersion,
		row.TargetSubjectID,
		row.ProposerID,
		row.Reason,
		row.Status,
		row.ResultingGrantVersion,
		row.CreatedAt,
		row.ExpiresAt,
		row.ResolvedAt,
	)
}

func grantRevocationRequestFromRow(row authzdb.AuthzGrantRevocationRequest) (GrantRevocationRequest, error) {
	return grantRevocationRequestFromValues(
		row.ID,
		row.GrantID,
		row.ExpectedGrantVersion,
		row.TargetSubjectID,
		row.ProposerID,
		row.Reason,
		row.Status,
		row.ResultingGrantVersion,
		row.CreatedAt,
		row.ExpiresAt,
		row.ResolvedAt,
	)
}

func grantRevocationRequestFromValues(id, grantID uuid.UUID, expectedVersion int64, targetID, proposerID uuid.UUID, reason, status string, resultingVersion pgtype.Int8, createdAt, expiresAt, resolvedAt pgtype.Timestamptz) (GrantRevocationRequest, error) {
	if id == uuid.Nil || grantID == uuid.Nil || targetID == uuid.Nil || proposerID == uuid.Nil || expectedVersion < 1 || !createdAt.Valid || !expiresAt.Valid {
		return GrantRevocationRequest{}, errors.New("grant revocation request contains invalid persisted metadata")
	}
	requestStatus := GrantRevocationStatus(status)
	switch requestStatus {
	case GrantRevocationPendingStatus, GrantRevocationRejectedStatus, GrantRevocationAppliedStatus, GrantRevocationConflictedStatus, GrantRevocationExpiredStatus:
	default:
		return GrantRevocationRequest{}, errors.New("grant revocation request contains an unknown status")
	}
	var resolved *time.Time
	if resolvedAt.Valid {
		value := resolvedAt.Time.UTC()
		resolved = &value
	}
	return GrantRevocationRequest{
		ID:                    id,
		GrantID:               grantID,
		ExpectedGrantVersion:  expectedVersion,
		ResultingGrantVersion: resultingVersion.Int64,
		TargetSubjectID:       targetID,
		ProposerID:            proposerID,
		Reason:                reason,
		Status:                requestStatus,
		CreatedAt:             createdAt.Time.UTC(),
		ExpiresAt:             expiresAt.Time.UTC(),
		ResolvedAt:            resolved,
		Reviews:               []GrantRevocationReview{},
	}, nil
}

func grantRevocationReviewFromValues(id, reviewerID uuid.UUID, domain, decision, reason string, createdAt pgtype.Timestamptz) (GrantRevocationReview, error) {
	reviewDomain := GrantReviewDomain(domain)
	if reviewDomain != GrantReviewGovernance && reviewDomain != GrantReviewSecurity {
		return GrantRevocationReview{}, errors.New("grant revocation review contains an unknown duty domain")
	}
	reviewDecision := GrantReviewDecision(decision)
	if !validGrantReviewDecision(reviewDecision) || id == uuid.Nil || reviewerID == uuid.Nil || !createdAt.Valid {
		return GrantRevocationReview{}, errors.New("grant revocation review contains invalid persisted metadata")
	}
	return GrantRevocationReview{
		ID:         id,
		ReviewerID: reviewerID,
		Domain:     reviewDomain,
		Decision:   reviewDecision,
		Reason:     reason,
		CreatedAt:  createdAt.Time.UTC(),
	}, nil
}

func auditState(status GrantRevocationStatus, grantVersion int64, revoked bool, reviews []GrantRevocationReview) GrantRevocationAuditState {
	state := GrantRevocationAuditState{Status: status, GrantVersion: grantVersion, GrantRevoked: revoked}
	for _, review := range reviews {
		switch review.Domain {
		case GrantReviewGovernance:
			state.GovernanceDecision = review.Decision
		case GrantReviewSecurity:
			state.SecurityDecision = review.Decision
		}
	}
	return state
}

func approvalsComplete(reviews []GrantRevocationReview) bool {
	governanceApproved := false
	securityApproved := false
	for _, review := range reviews {
		if review.Decision != GrantReviewApprove {
			continue
		}
		governanceApproved = governanceApproved || review.Domain == GrantReviewGovernance
		securityApproved = securityApproved || review.Domain == GrantReviewSecurity
	}
	return governanceApproved && securityApproved
}

func grantAdminTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func mapGrantAdministrationWriteError(operation string, err error) error {
	// Deterministic row locks perform the friendly conflict checks first. A
	// constraint failure here means an invariant raced or the database rejected
	// malformed state, so preserve it as an internal failure instead of guessing.
	return fmt.Errorf("%s: %w", operation, err)
}

var _ GrantAdministrationRepository = (*PostgresGrantAdministrationRepository)(nil)
