package newcomer

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
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, errors.New("newcomer assessment database is required")
	}
	return &PostgresRepository{pool: pool}, nil
}

func (repository *PostgresRepository) MyStatus(ctx context.Context, userID uuid.UUID, now time.Time) (MyStatus, error) {
	if userID == uuid.Nil || now.IsZero() {
		return MyStatus{}, ErrInput
	}
	result := MyStatus{ObservedAt: now.UTC().Round(0)}
	item, err := scanAssessment(repository.pool.QueryRow(ctx, assessmentSelect+`
WHERE assessment.user_id = $1`, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return MyStatus{}, fmt.Errorf("read my newcomer assessment: %w", err)
	}
	result.Assessment = &item
	return result, nil
}

func (repository *PostgresRepository) Policies(ctx context.Context, limit, offset int, now time.Time) (PolicyPage, error) {
	if !validPage(limit, offset) || now.IsZero() {
		return PolicyPage{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return PolicyPage{}, fmt.Errorf("begin newcomer policy read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result := PolicyPage{Items: make([]PolicyRevision, 0, limit), Limit: limit, Offset: offset, MinimumEffectiveFrom: now.Add(minimumLeadTime)}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM newcomer.policy_revisions`).Scan(&result.Total); err != nil {
		return PolicyPage{}, fmt.Errorf("count newcomer policies: %w", err)
	}
	var latestEffective pgtype.Timestamptz
	if err := tx.QueryRow(ctx, `SELECT max(effective_at) FROM newcomer.policy_revisions`).Scan(&latestEffective); err != nil {
		return PolicyPage{}, fmt.Errorf("read latest newcomer policy effective time: %w", err)
	}
	if latestEffective.Valid && !latestEffective.Time.Before(result.MinimumEffectiveFrom) {
		// Revisions form one chronological timeline. Returning the lower bound
		// prevents the UI from offering a version that would overtake an
		// already scheduled revision.
		result.MinimumEffectiveFrom = latestEffective.Time.UTC().Round(0).Add(time.Second)
	}
	rows, err := tx.Query(ctx, policySelect+`
ORDER BY revision.revision DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return PolicyPage{}, fmt.Errorf("list newcomer policies: %w", err)
	}
	for rows.Next() {
		item, scanErr := scanPolicy(rows, now)
		if scanErr != nil {
			rows.Close()
			return PolicyPage{}, scanErr
		}
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return PolicyPage{}, fmt.Errorf("iterate newcomer policies: %w", err)
	}
	rows.Close()
	current, err := scanPolicy(tx.QueryRow(ctx, policySelect+`
WHERE revision.effective_at <= $1
ORDER BY revision.effective_at DESC, revision.revision DESC LIMIT 1`, now), now)
	if err == nil {
		result.Current = &current
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return PolicyPage{}, err
	}
	if err := tx.QueryRow(ctx, `
SELECT
    count(*) FILTER (WHERE status = 'active')::bigint,
    count(*) FILTER (WHERE status = 'download_restricted')::bigint,
    count(*) FILTER (WHERE status = 'passed')::bigint,
    count(*) FILTER (WHERE status = 'exempted')::bigint
FROM newcomer.assessments`).Scan(
		&result.Summary.Active, &result.Summary.DownloadRestricted,
		&result.Summary.Passed, &result.Summary.Exempted,
	); err != nil {
		return PolicyPage{}, fmt.Errorf("summarize newcomer assessments: %w", err)
	}
	var started, completed pgtype.Timestamptz
	var errorCode pgtype.Text
	if err := tx.QueryRow(ctx, `
SELECT last_started_at, last_completed_at, last_error_code,
       last_examined, last_transitioned, run_count
FROM newcomer.worker_state WHERE singleton = true`).Scan(
		&started, &completed, &errorCode, &result.Worker.LastExamined,
		&result.Worker.LastTransitioned, &result.Worker.RunCount,
	); err != nil {
		return PolicyPage{}, fmt.Errorf("read newcomer worker state: %w", err)
	}
	if started.Valid {
		value := started.Time.UTC().Round(0)
		result.Worker.LastStartedAt = &value
	}
	if completed.Valid {
		value := completed.Time.UTC().Round(0)
		result.Worker.LastCompletedAt = &value
	}
	if errorCode.Valid {
		result.Worker.LastErrorCode = errorCode.String
	}
	if err := tx.Commit(ctx); err != nil {
		return PolicyPage{}, fmt.Errorf("commit newcomer policy read: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) Issue(ctx context.Context, command IssueCommand) (PolicyRevision, error) {
	if command.RequestID == uuid.Nil || command.ActorID == uuid.Nil || command.Authorization.ID == uuid.Nil || !command.Authorization.Allow {
		return PolicyRevision{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return PolicyRevision{}, fmt.Errorf("begin newcomer policy issue: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	existing, err := scanPolicy(tx.QueryRow(ctx, policySelect+` WHERE revision.request_id = $1`, command.RequestID), command.OccurredAt)
	if err == nil {
		if !sameIssue(existing, command) {
			return PolicyRevision{}, ErrIdempotencyConflict
		}
		existing.Replayed = true
		if err := tx.Commit(ctx); err != nil {
			return PolicyRevision{}, fmt.Errorf("commit newcomer policy replay: %w", err)
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return PolicyRevision{}, fmt.Errorf("read newcomer policy replay: %w", err)
	}
	latest, err := scanPolicy(tx.QueryRow(ctx, policySelect+` ORDER BY revision.revision DESC LIMIT 1 FOR UPDATE`), command.OccurredAt)
	if err != nil {
		return PolicyRevision{}, fmt.Errorf("lock latest newcomer policy: %w", err)
	}
	if !command.EffectiveAt.After(latest.EffectiveAt) {
		return PolicyRevision{}, ErrConflict
	}
	if latest.PolicyInput == command.Policy {
		return PolicyRevision{}, ErrNoChange
	}
	result := PolicyRevision{
		ID: uuid.New(), RequestID: &command.RequestID, Revision: latest.Revision + 1,
		SourceKind: "staff", PolicyInput: command.Policy, EffectiveAt: command.EffectiveAt,
		Reason: command.Reason, ActorID: &command.ActorID,
		AuthorizationDecisionID: &command.Authorization.ID, CreatedAt: command.OccurredAt,
		TimelineState: "scheduled",
	}
	_, err = tx.Exec(ctx, `
INSERT INTO newcomer.policy_revisions (
    id, request_id, revision, source_kind, enabled, duration_seconds,
    minimum_credited_upload_bytes, minimum_seeding_active_seconds,
    effective_at, reason, actor_id, authorization_decision_id, created_at
) VALUES ($1, $2, $3, 'staff', $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		result.ID, command.RequestID, result.Revision, command.Policy.Enabled, command.Policy.DurationSeconds,
		command.Policy.MinimumCreditedUploadBytes, command.Policy.MinimumSeedingActiveSeconds,
		command.EffectiveAt, command.Reason, command.ActorID, command.Authorization.ID, command.OccurredAt)
	if err != nil {
		return PolicyRevision{}, classifyDatabaseError("insert newcomer policy", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PolicyRevision{}, classifyDatabaseError("commit newcomer policy", err)
	}
	return result, nil
}

func (repository *PostgresRepository) Assessments(ctx context.Context, query AssessmentQuery) (AssessmentPage, error) {
	if !validPage(query.Limit, query.Offset) || !validFilter(query.Filter) {
		return AssessmentPage{}, ErrInput
	}
	filterSQL := assessmentFilterSQL(query.Filter)
	search := "%" + strings.ToLower(query.Query) + "%"
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return AssessmentPage{}, fmt.Errorf("begin newcomer assessment read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var total int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)
FROM newcomer.assessments AS assessment
JOIN identity.users AS users ON users.id = assessment.user_id
WHERE ($1 = '%%' OR lower(users.username) LIKE $1 OR lower(users.display_name) LIKE $1)
  AND `+filterSQL, search).Scan(&total); err != nil {
		return AssessmentPage{}, fmt.Errorf("count newcomer assessments: %w", err)
	}
	rows, err := tx.Query(ctx, assessmentSelect+`
WHERE ($1 = '%%' OR lower(users.username) LIKE $1 OR lower(users.display_name) LIKE $1)
  AND `+filterSQL+`
ORDER BY (assessment.status = 'download_restricted') DESC,
         assessment.deadline_at ASC, users.numeric_id ASC
LIMIT $2 OFFSET $3`, search, query.Limit, query.Offset)
	if err != nil {
		return AssessmentPage{}, fmt.Errorf("list newcomer assessments: %w", err)
	}
	items := make([]Assessment, 0, query.Limit)
	for rows.Next() {
		item, scanErr := scanAssessment(rows)
		if scanErr != nil {
			rows.Close()
			return AssessmentPage{}, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return AssessmentPage{}, fmt.Errorf("iterate newcomer assessments: %w", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return AssessmentPage{}, fmt.Errorf("commit newcomer assessment read: %w", err)
	}
	return AssessmentPage{Items: items, Total: total, Limit: query.Limit, Offset: query.Offset}, nil
}

func (repository *PostgresRepository) Exempt(ctx context.Context, command ExemptCommand) (Assessment, error) {
	if command.ExemptionID == uuid.Nil || command.AssessmentID == uuid.Nil || command.ActorID == uuid.Nil || command.Authorization.ID == uuid.Nil || !command.Authorization.Allow {
		return Assessment{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Assessment{}, fmt.Errorf("begin newcomer exemption: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var existingAssessment uuid.UUID
	var existingReason string
	var existingActor uuid.UUID
	err = tx.QueryRow(ctx, `
SELECT assessment_id, reason, actor_id
FROM newcomer.assessment_exemptions WHERE id = $1`, command.ExemptionID).Scan(&existingAssessment, &existingReason, &existingActor)
	if err == nil {
		if existingAssessment != command.AssessmentID || existingReason != command.Reason || existingActor != command.ActorID {
			return Assessment{}, ErrIdempotencyConflict
		}
		result, scanErr := scanAssessment(tx.QueryRow(ctx, assessmentSelect+` WHERE assessment.id = $1`, command.AssessmentID))
		if scanErr != nil {
			return Assessment{}, scanErr
		}
		if err := tx.Commit(ctx); err != nil {
			return Assessment{}, fmt.Errorf("commit newcomer exemption replay: %w", err)
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Assessment{}, fmt.Errorf("read newcomer exemption replay: %w", err)
	}
	assessment, err := scanAssessment(tx.QueryRow(ctx, assessmentSelect+` WHERE assessment.id = $1 FOR UPDATE OF assessment`, command.AssessmentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Assessment{}, ErrNotFound
	}
	if err != nil {
		return Assessment{}, err
	}
	if assessment.UserID == command.ActorID {
		return Assessment{}, ErrSelfTarget
	}
	if !assessment.Status.Active() || assessment.Version != command.ExpectedVersion {
		return Assessment{}, ErrConflict
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO newcomer.assessment_exemptions (
    id, assessment_id, reason, actor_id, authorization_decision_id, occurred_at
) VALUES ($1, $2, $3, $4, $5, $6)`, command.ExemptionID, command.AssessmentID,
		command.Reason, command.ActorID, command.Authorization.ID, command.OccurredAt); err != nil {
		return Assessment{}, classifyDatabaseError("insert newcomer exemption", err)
	}
	previous := assessment.Status
	if _, err := tx.Exec(ctx, `
UPDATE newcomer.assessments
SET status = 'exempted', resolved_at = $2, resolution_code = 'staff_exempted',
    version = version + 1, updated_at = $2
WHERE id = $1 AND version = $3`, assessment.ID, command.OccurredAt, command.ExpectedVersion); err != nil {
		return Assessment{}, classifyDatabaseError("apply newcomer exemption", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO newcomer.assessment_transitions (
    assessment_id, from_status, to_status, credited_upload_bytes,
    seeding_active_seconds, reason_code, occurred_at
) VALUES ($1, $2, 'exempted', $3, $4, 'staff_exempted', $5)`, assessment.ID,
		previous, assessment.CurrentCreditedUploadBytes,
		assessment.CurrentSeedingActiveSeconds, command.OccurredAt); err != nil {
		return Assessment{}, classifyDatabaseError("append newcomer exemption transition", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Assessment{}, classifyDatabaseError("commit newcomer exemption", err)
	}
	assessment.Status = AssessmentExempted
	assessment.ResolvedAt = timePointer(command.OccurredAt)
	assessment.ResolutionCode = "staff_exempted"
	assessment.Version++
	assessment.UpdatedAt = command.OccurredAt
	return assessment, nil
}

func (repository *PostgresRepository) Evaluate(ctx context.Context, now time.Time, batch int) (EvaluationResult, error) {
	if now.IsZero() || batch < 1 || batch > MaximumWorkerBatch {
		return EvaluationResult{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return EvaluationResult{}, fmt.Errorf("begin newcomer evaluation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('peergo-newcomer-evaluator'))`).Scan(&locked); err != nil {
		return EvaluationResult{}, fmt.Errorf("lock newcomer evaluator: %w", err)
	}
	if !locked {
		return EvaluationResult{Skipped: true}, nil
	}
	if _, err := tx.Exec(ctx, `
UPDATE newcomer.worker_state
SET last_started_at = $1, last_error_code = NULL
WHERE singleton = true`, now); err != nil {
		return EvaluationResult{}, fmt.Errorf("start newcomer worker heartbeat: %w", err)
	}
	rows, err := tx.Query(ctx, `
SELECT
    assessment.id, assessment.status, assessment.version, assessment.deadline_at,
    assessment.current_credited_upload_bytes,
    assessment.current_seeding_active_seconds,
    GREATEST(
        assessment.current_credited_upload_bytes,
        GREATEST(COALESCE(totals.credited_uploaded, 0)
            - assessment.opening_credited_uploaded_bytes, 0)
    )::bigint,
    GREATEST(
        assessment.current_seeding_active_seconds,
        COALESCE((
            SELECT sum(item.active_seconds)::bigint
            FROM economy.seeding_reward_evidence_items AS item
            JOIN economy.seeding_reward_evidence_windows AS evidence_window
              ON evidence_window.window_start = item.window_start
             AND evidence_window.status = 'complete'
            WHERE item.user_id = assessment.user_id
              AND evidence_window.window_start >= assessment.started_at
              AND evidence_window.window_end <= $1
        ), 0)
    )::bigint,
    revision.minimum_credited_upload_bytes,
    revision.minimum_seeding_active_seconds
FROM newcomer.assessments AS assessment
JOIN newcomer.policy_revisions AS revision ON revision.id = assessment.policy_revision_id
LEFT JOIN traffic.user_totals AS totals ON totals.user_id = assessment.user_id
WHERE assessment.status IN ('active', 'download_restricted')
ORDER BY assessment.deadline_at, assessment.id
LIMIT $2
FOR UPDATE OF assessment SKIP LOCKED`, now, batch)
	if err != nil {
		return EvaluationResult{}, fmt.Errorf("claim newcomer assessments: %w", err)
	}
	type row struct {
		id                                             uuid.UUID
		status                                         AssessmentStatus
		version, previousUpload, previousSeeding       int64
		deadline                                       time.Time
		upload, seeding, minimumUpload, minimumSeeding int64
	}
	claimed := make([]row, 0, batch)
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.id, &item.status, &item.version, &item.deadline,
			&item.previousUpload, &item.previousSeeding, &item.upload, &item.seeding,
			&item.minimumUpload, &item.minimumSeeding); err != nil {
			rows.Close()
			return EvaluationResult{}, fmt.Errorf("scan newcomer assessment: %w", err)
		}
		claimed = append(claimed, item)
	}
	if err := rows.Err(); err != nil {
		return EvaluationResult{}, fmt.Errorf("iterate newcomer assessments: %w", err)
	}
	rows.Close()
	result := EvaluationResult{}
	for _, item := range claimed {
		result.Examined++
		next := item.status
		reason := ""
		var restrictionAt, resolvedAt *time.Time
		resolutionCode := ""
		if item.upload >= item.minimumUpload && item.seeding >= item.minimumSeeding {
			next, reason, resolutionCode = AssessmentPassed, "requirements_met", "requirements_met"
			resolvedAt = timePointer(now)
		} else if item.status == AssessmentActive && !now.Before(item.deadline) {
			next, reason = AssessmentDownloadRestricted, "deadline_not_met"
			restrictionAt = timePointer(now)
		}
		if next == item.status && item.upload == item.previousUpload && item.seeding == item.previousSeeding {
			continue
		}
		if _, err := tx.Exec(ctx, `
UPDATE newcomer.assessments
SET status = $2, current_credited_upload_bytes = $3,
    current_seeding_active_seconds = $4,
    restriction_started_at = COALESCE(restriction_started_at, $5),
    resolved_at = $6, resolution_code = NULLIF($7, ''),
    version = version + 1, updated_at = $8
WHERE id = $1 AND version = $9`, item.id, next, item.upload, item.seeding,
			restrictionAt, resolvedAt, resolutionCode, now, item.version); err != nil {
			return EvaluationResult{}, classifyDatabaseError("advance newcomer assessment", err)
		}
		if next != item.status {
			if _, err := tx.Exec(ctx, `
INSERT INTO newcomer.assessment_transitions (
    assessment_id, from_status, to_status, credited_upload_bytes,
    seeding_active_seconds, reason_code, occurred_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)`, item.id, item.status, next,
				item.upload, item.seeding, reason, now); err != nil {
				return EvaluationResult{}, classifyDatabaseError("append newcomer transition", err)
			}
			result.Transitioned++
		}
	}
	if _, err := tx.Exec(ctx, `
UPDATE newcomer.worker_state
SET last_completed_at = $1, last_error_code = NULL,
    last_examined = $2, last_transitioned = $3, run_count = run_count + 1
WHERE singleton = true`, now, result.Examined, result.Transitioned); err != nil {
		return EvaluationResult{}, fmt.Errorf("complete newcomer worker heartbeat: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return EvaluationResult{}, classifyDatabaseError("commit newcomer evaluation", err)
	}
	return result, nil
}

func (repository *PostgresRepository) MarkWorkerFailure(ctx context.Context, occurredAt time.Time, code string) error {
	if occurredAt.IsZero() || code == "" {
		return ErrInput
	}
	_, err := repository.pool.Exec(ctx, `
UPDATE newcomer.worker_state
SET last_error_code = $1, last_started_at = COALESCE(last_started_at, $2)
WHERE singleton = true`, code, occurredAt)
	if err != nil {
		return fmt.Errorf("record newcomer worker failure: %w", err)
	}
	return nil
}

const policySelect = `
SELECT
    revision.id, revision.request_id, revision.revision, revision.source_kind,
    revision.enabled, revision.duration_seconds,
    revision.minimum_credited_upload_bytes,
    revision.minimum_seeding_active_seconds, revision.effective_at,
    revision.reason, revision.actor_id, revision.authorization_decision_id,
    revision.created_at
FROM newcomer.policy_revisions AS revision`

func scanPolicy(scanner interface{ Scan(...any) error }, now time.Time) (PolicyRevision, error) {
	var result PolicyRevision
	var requestID, actorID, authorizationID pgtype.UUID
	if err := scanner.Scan(
		&result.ID, &requestID, &result.Revision, &result.SourceKind,
		&result.Enabled, &result.DurationSeconds, &result.MinimumCreditedUploadBytes,
		&result.MinimumSeedingActiveSeconds, &result.EffectiveAt, &result.Reason,
		&actorID, &authorizationID, &result.CreatedAt,
	); err != nil {
		return PolicyRevision{}, err
	}
	if requestID.Valid {
		value := uuid.UUID(requestID.Bytes)
		result.RequestID = &value
	}
	if actorID.Valid {
		value := uuid.UUID(actorID.Bytes)
		result.ActorID = &value
	}
	if authorizationID.Valid {
		value := uuid.UUID(authorizationID.Bytes)
		result.AuthorizationDecisionID = &value
	}
	if result.ID == uuid.Nil || result.Revision < 1 ||
		(result.SourceKind == "opening") != (result.ActorID == nil) {
		return PolicyRevision{}, ErrInvariant
	}
	result.TimelineState = "scheduled"
	if !now.Before(result.EffectiveAt) {
		result.TimelineState = "active"
	}
	return result, nil
}

const assessmentSelect = `
SELECT
    assessment.id, assessment.user_id, users.numeric_id, users.username,
    users.display_name, assessment.policy_revision_id, revision.revision,
    assessment.status, assessment.started_at, assessment.deadline_at,
    revision.minimum_credited_upload_bytes,
    revision.minimum_seeding_active_seconds,
    assessment.current_credited_upload_bytes,
    assessment.current_seeding_active_seconds,
    assessment.restriction_started_at, assessment.resolved_at,
    COALESCE(assessment.resolution_code, ''), assessment.version,
    assessment.updated_at
FROM newcomer.assessments AS assessment
JOIN newcomer.policy_revisions AS revision ON revision.id = assessment.policy_revision_id
JOIN identity.users AS users ON users.id = assessment.user_id`

func scanAssessment(scanner interface{ Scan(...any) error }) (Assessment, error) {
	var result Assessment
	var restrictionAt, resolvedAt pgtype.Timestamptz
	if err := scanner.Scan(
		&result.ID, &result.UserID, &result.UserNumericID, &result.Username,
		&result.DisplayName, &result.PolicyRevisionID, &result.PolicyRevision,
		&result.Status, &result.StartedAt, &result.DeadlineAt,
		&result.MinimumCreditedUploadBytes, &result.MinimumSeedingActiveSeconds,
		&result.CurrentCreditedUploadBytes, &result.CurrentSeedingActiveSeconds,
		&restrictionAt, &resolvedAt, &result.ResolutionCode, &result.Version,
		&result.UpdatedAt,
	); err != nil {
		return Assessment{}, err
	}
	if restrictionAt.Valid {
		value := restrictionAt.Time.UTC().Round(0)
		result.RestrictionStartedAt = &value
	}
	if resolvedAt.Valid {
		value := resolvedAt.Time.UTC().Round(0)
		result.ResolvedAt = &value
	}
	if result.ID == uuid.Nil || result.UserID == uuid.Nil || result.UserNumericID < 1 ||
		result.PolicyRevision < 1 || result.Version < 1 || !validAssessmentStatus(result.Status) {
		return Assessment{}, ErrInvariant
	}
	return result, nil
}

func validAssessmentStatus(status AssessmentStatus) bool {
	return status == AssessmentActive || status == AssessmentDownloadRestricted ||
		status == AssessmentPassed || status == AssessmentExempted
}

func assessmentFilterSQL(filter AssessmentFilter) string {
	switch filter {
	case AssessmentFilterActive:
		return `assessment.status IN ('active', 'download_restricted')`
	case AssessmentFilterRestricted:
		return `assessment.status = 'download_restricted'`
	case AssessmentFilterResolved:
		return `assessment.status IN ('passed', 'exempted')`
	default:
		return `true`
	}
}

func sameIssue(existing PolicyRevision, command IssueCommand) bool {
	return existing.RequestID != nil && *existing.RequestID == command.RequestID &&
		existing.PolicyInput == command.Policy && existing.EffectiveAt.Equal(command.EffectiveAt) &&
		existing.Reason == command.Reason && existing.ActorID != nil && *existing.ActorID == command.ActorID
}

func classifyDatabaseError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return ErrIdempotencyConflict
		case "23514", "23P01", "40001":
			return ErrConflict
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func timePointer(value time.Time) *time.Time {
	value = value.UTC().Round(0)
	return &value
}

var _ Repository = (*PostgresRepository)(nil)
var _ Evaluator = (*PostgresRepository)(nil)
var _ FailureRecorder = (*PostgresRepository)(nil)
