package workgroups

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const taskSelect = `
SELECT task.id, task.group_kind, task.task_type, task.title,
       task.description, task.starts_at, task.due_at,
       (SELECT count(*) FROM workgroups.task_assignments AS assignment
        WHERE assignment.task_id = task.id),
       (SELECT count(*) FROM workgroups.task_assignments AS assignment
        WHERE assignment.task_id = task.id
          AND EXISTS (
              SELECT 1 FROM workgroups.task_submissions AS submission
              WHERE submission.assignment_id = assignment.id
          )),
       (SELECT count(*)
        FROM workgroups.task_assignments AS assignment
        JOIN LATERAL (
            SELECT submission.id
            FROM workgroups.task_submissions AS submission
            WHERE submission.assignment_id = assignment.id
            ORDER BY submission.sequence DESC
            LIMIT 1
        ) AS latest ON true
        LEFT JOIN workgroups.task_reviews AS review
          ON review.submission_id = latest.id
        WHERE assignment.task_id = task.id AND review.id IS NULL),
       (SELECT count(*)
        FROM workgroups.task_assignments AS assignment
        JOIN LATERAL (
            SELECT submission.id
            FROM workgroups.task_submissions AS submission
            WHERE submission.assignment_id = assignment.id
            ORDER BY submission.sequence DESC
            LIMIT 1
        ) AS latest ON true
        JOIN workgroups.task_reviews AS review
          ON review.submission_id = latest.id
         AND review.decision = 'accepted'
        WHERE assignment.task_id = task.id),
       task.created_at
FROM workgroups.tasks AS task`

const taskAssignmentSelect = `
SELECT assignment.id, assignment.membership_id, assignment.user_id,
       users.numeric_id, users.username, users.display_name, membership.status,
       task.id, task.group_kind, task.task_type, task.title,
       task.description, task.starts_at, task.due_at,
       (SELECT count(*) FROM workgroups.task_assignments AS all_assignment
        WHERE all_assignment.task_id = task.id),
       (SELECT count(*) FROM workgroups.task_assignments AS all_assignment
        WHERE all_assignment.task_id = task.id
          AND EXISTS (
              SELECT 1 FROM workgroups.task_submissions AS any_submission
              WHERE any_submission.assignment_id = all_assignment.id
          )),
       (SELECT count(*)
        FROM workgroups.task_assignments AS all_assignment
        JOIN LATERAL (
            SELECT pending_submission.id
            FROM workgroups.task_submissions AS pending_submission
            WHERE pending_submission.assignment_id = all_assignment.id
            ORDER BY pending_submission.sequence DESC
            LIMIT 1
        ) AS latest_pending ON true
        LEFT JOIN workgroups.task_reviews AS pending_review
          ON pending_review.submission_id = latest_pending.id
        WHERE all_assignment.task_id = task.id AND pending_review.id IS NULL),
       (SELECT count(*)
        FROM workgroups.task_assignments AS all_assignment
        JOIN LATERAL (
            SELECT accepted_submission.id
            FROM workgroups.task_submissions AS accepted_submission
            WHERE accepted_submission.assignment_id = all_assignment.id
            ORDER BY accepted_submission.sequence DESC
            LIMIT 1
        ) AS latest_accepted ON true
        JOIN workgroups.task_reviews AS accepted_review
          ON accepted_review.submission_id = latest_accepted.id
         AND accepted_review.decision = 'accepted'
        WHERE all_assignment.task_id = task.id),
       task.created_at,
       submission.id, submission.sequence, submission.statement,
       submission.submitted_at, review.decision, review.reason,
       review.decided_at
FROM workgroups.task_assignments AS assignment
JOIN workgroups.memberships AS membership ON membership.id = assignment.membership_id
JOIN identity.users AS users ON users.id = assignment.user_id
JOIN workgroups.tasks AS task ON task.id = assignment.task_id
LEFT JOIN LATERAL (
    SELECT candidate.id, candidate.sequence, candidate.statement,
           candidate.submitted_at
    FROM workgroups.task_submissions AS candidate
    WHERE candidate.assignment_id = assignment.id
    ORDER BY candidate.sequence DESC
    LIMIT 1
) AS submission ON true
LEFT JOIN workgroups.task_reviews AS review
  ON review.submission_id = submission.id`

func (repository *PostgresRepository) ListMyTasks(ctx context.Context, userID uuid.UUID, limit, offset int, asOf time.Time) (TaskAssignmentPage, error) {
	page := TaskAssignmentPage{Items: make([]TaskAssignment, 0), Limit: limit, Offset: offset}
	if err := repository.pool.QueryRow(ctx, `
SELECT count(*) FROM workgroups.task_assignments WHERE user_id = $1`, userID).Scan(&page.Total); err != nil {
		return TaskAssignmentPage{}, fmt.Errorf("count own workgroup task assignments: %w", err)
	}
	rows, err := repository.pool.Query(ctx, taskAssignmentSelect+`
WHERE assignment.user_id = $1
ORDER BY task.due_at DESC, task.id DESC
LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return TaskAssignmentPage{}, fmt.Errorf("list own workgroup task assignments: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		assignment, scanErr := scanTaskAssignment(rows, asOf)
		if scanErr != nil {
			return TaskAssignmentPage{}, fmt.Errorf("scan own workgroup task assignment: %w", scanErr)
		}
		page.Items = append(page.Items, assignment)
	}
	if err := rows.Err(); err != nil {
		return TaskAssignmentPage{}, fmt.Errorf("iterate own workgroup task assignments: %w", err)
	}
	return page, nil
}

func (repository *PostgresRepository) ListTasks(ctx context.Context, kind GroupKind, limit, offset int, asOf time.Time) (TaskPage, error) {
	page := TaskPage{Items: make([]Task, 0), Limit: limit, Offset: offset}
	if err := repository.pool.QueryRow(ctx, `
SELECT count(*) FROM workgroups.tasks WHERE group_kind = $1`, kind).Scan(&page.Total); err != nil {
		return TaskPage{}, fmt.Errorf("count workgroup tasks: %w", err)
	}
	rows, err := repository.pool.Query(ctx, taskSelect+`
WHERE task.group_kind = $1
ORDER BY task.starts_at DESC, task.id DESC
LIMIT $2 OFFSET $3`, kind, limit, offset)
	if err != nil {
		return TaskPage{}, fmt.Errorf("list workgroup tasks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		task, scanErr := scanTask(rows, asOf)
		if scanErr != nil {
			return TaskPage{}, fmt.Errorf("scan workgroup task: %w", scanErr)
		}
		page.Items = append(page.Items, task)
	}
	if err := rows.Err(); err != nil {
		return TaskPage{}, fmt.Errorf("iterate workgroup tasks: %w", err)
	}
	return page, nil
}

func (repository *PostgresRepository) PublishTask(ctx context.Context, command PublishTaskCommand) (Task, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Task{}, fmt.Errorf("begin workgroup task publication: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	existing, err := scanTask(tx.QueryRow(ctx, taskSelect+` WHERE task.request_id = $1`, command.RequestID), command.OccurredAt)
	if err == nil {
		if !sameTaskPublication(existing, command) {
			return Task{}, ErrIdempotencyConflict
		}
		existing.Replayed = true
		if err := tx.Commit(ctx); err != nil {
			return Task{}, fmt.Errorf("commit workgroup task publication replay: %w", err)
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Task{}, fmt.Errorf("read workgroup task publication replay: %w", err)
	}

	rows, err := tx.Query(ctx, `
SELECT id
FROM workgroups.memberships
WHERE group_kind = $1 AND status = 'active'
ORDER BY id
FOR UPDATE`, command.GroupKind)
	if err != nil {
		return Task{}, fmt.Errorf("lock workgroup task members: %w", err)
	}
	memberCount := int64(0)
	for rows.Next() {
		var membershipID uuid.UUID
		if err := rows.Scan(&membershipID); err != nil {
			rows.Close()
			return Task{}, fmt.Errorf("scan workgroup task member lock: %w", err)
		}
		memberCount++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Task{}, fmt.Errorf("iterate workgroup task member locks: %w", err)
	}
	rows.Close()
	if memberCount == 0 {
		return Task{}, ErrTaskNoMembers
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO workgroups.tasks (
    id, request_id, group_kind, task_type, title, description,
    starts_at, due_at, issued_by, authorization_decision_id, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		command.TaskID, command.RequestID, command.GroupKind, command.Type,
		command.Title, command.Description, command.StartsAt, command.DueAt,
		command.ActorID, command.AuthorizationDecisionID, command.OccurredAt,
	); err != nil {
		if constraintName(err) == "tasks_request_id_key" {
			return Task{}, ErrIdempotencyConflict
		}
		return Task{}, fmt.Errorf("insert workgroup task: %w", err)
	}
	result, err := tx.Exec(ctx, `
INSERT INTO workgroups.task_assignments (
    task_id, membership_id, user_id, assigned_at
)
SELECT $1, membership.id, membership.user_id, $2
FROM workgroups.memberships AS membership
WHERE membership.group_kind = $3 AND membership.status = 'active'`,
		command.TaskID, command.OccurredAt, command.GroupKind)
	if err != nil {
		return Task{}, fmt.Errorf("snapshot workgroup task assignments: %w", err)
	}
	if result.RowsAffected() != memberCount {
		return Task{}, errors.New("workgroup task assignment snapshot changed during publication")
	}
	created, err := scanTask(tx.QueryRow(ctx, taskSelect+` WHERE task.id = $1`, command.TaskID), command.OccurredAt)
	if err != nil {
		return Task{}, fmt.Errorf("read published workgroup task: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Task{}, fmt.Errorf("commit workgroup task publication: %w", err)
	}
	return created, nil
}

func (repository *PostgresRepository) SubmitTask(ctx context.Context, command SubmitTaskCommand) (TaskAssignment, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return TaskAssignment{}, fmt.Errorf("begin workgroup task submission: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var replayAssignmentID, replayUserID uuid.UUID
	var replayStatement string
	err = tx.QueryRow(ctx, `
SELECT assignment_id, user_id, statement
FROM workgroups.task_submissions
WHERE request_id = $1`, command.RequestID).Scan(&replayAssignmentID, &replayUserID, &replayStatement)
	if err == nil {
		if replayAssignmentID != command.AssignmentID || replayUserID != command.UserID || replayStatement != command.Statement {
			return TaskAssignment{}, ErrIdempotencyConflict
		}
		replayed, scanErr := scanTaskAssignment(tx.QueryRow(ctx, taskAssignmentSelect+`
WHERE assignment.id = $1`, replayAssignmentID), command.OccurredAt)
		if scanErr != nil {
			return TaskAssignment{}, fmt.Errorf("read workgroup task submission replay: %w", scanErr)
		}
		if err := tx.Commit(ctx); err != nil {
			return TaskAssignment{}, fmt.Errorf("commit workgroup task submission replay: %w", err)
		}
		return replayed, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return TaskAssignment{}, fmt.Errorf("read workgroup task submission idempotency key: %w", err)
	}

	var ownerID uuid.UUID
	var membershipStatus MembershipStatus
	var startsAt, dueAt time.Time
	err = tx.QueryRow(ctx, `
SELECT assignment.user_id, membership.status, task.starts_at, task.due_at
FROM workgroups.task_assignments AS assignment
JOIN workgroups.memberships AS membership ON membership.id = assignment.membership_id
JOIN workgroups.tasks AS task ON task.id = assignment.task_id
WHERE assignment.id = $1
FOR UPDATE OF assignment, membership`, command.AssignmentID).Scan(
		&ownerID, &membershipStatus, &startsAt, &dueAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return TaskAssignment{}, ErrTaskAssignmentNotFound
	}
	if err != nil {
		return TaskAssignment{}, fmt.Errorf("lock workgroup task assignment: %w", err)
	}
	if ownerID != command.UserID || membershipStatus != MembershipActive || command.OccurredAt.Before(startsAt) || command.OccurredAt.After(dueAt) {
		return TaskAssignment{}, ErrTaskSubmissionNotAllowed
	}

	sequence := int64(1)
	var latestSubmissionID uuid.UUID
	var latestDecision pgtype.Text
	err = tx.QueryRow(ctx, `
SELECT submission.id, review.decision
FROM workgroups.task_submissions AS submission
LEFT JOIN workgroups.task_reviews AS review ON review.submission_id = submission.id
WHERE submission.assignment_id = $1
ORDER BY submission.sequence DESC
LIMIT 1
FOR UPDATE OF submission`, command.AssignmentID).Scan(&latestSubmissionID, &latestDecision)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return TaskAssignment{}, fmt.Errorf("read latest workgroup task submission: %w", err)
	case !latestDecision.Valid || latestDecision.String == string(TaskReviewAccepted):
		return TaskAssignment{}, ErrTaskSubmissionNotAllowed
	case latestDecision.String != string(TaskReviewRejected):
		return TaskAssignment{}, errors.New("workgroup task review decision is invalid")
	default:
		if err := tx.QueryRow(ctx, `
SELECT sequence + 1 FROM workgroups.task_submissions WHERE id = $1`, latestSubmissionID).Scan(&sequence); err != nil {
			return TaskAssignment{}, fmt.Errorf("advance workgroup task submission sequence: %w", err)
		}
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO workgroups.task_submissions (
    id, request_id, assignment_id, user_id, sequence, statement,
    authorization_decision_id, submitted_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		command.SubmissionID, command.RequestID, command.AssignmentID,
		command.UserID, sequence, command.Statement,
		command.AuthorizationDecisionID, command.OccurredAt,
	); err != nil {
		if constraintName(err) == "task_submissions_request_id_key" {
			return TaskAssignment{}, ErrIdempotencyConflict
		}
		return TaskAssignment{}, fmt.Errorf("insert workgroup task submission: %w", err)
	}
	created, err := scanTaskAssignment(tx.QueryRow(ctx, taskAssignmentSelect+`
WHERE assignment.id = $1`, command.AssignmentID), command.OccurredAt)
	if err != nil {
		return TaskAssignment{}, fmt.Errorf("read submitted workgroup task assignment: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return TaskAssignment{}, fmt.Errorf("commit workgroup task submission: %w", err)
	}
	return created, nil
}

func (repository *PostgresRepository) ListTaskAssignments(ctx context.Context, kind GroupKind, taskID uuid.UUID, limit, offset int, asOf time.Time) (TaskAssignmentPage, error) {
	var exists bool
	if err := repository.pool.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM workgroups.tasks WHERE id = $1 AND group_kind = $2)`, taskID, kind).Scan(&exists); err != nil {
		return TaskAssignmentPage{}, fmt.Errorf("check workgroup task: %w", err)
	}
	if !exists {
		return TaskAssignmentPage{}, ErrTaskNotFound
	}
	page := TaskAssignmentPage{Items: make([]TaskAssignment, 0), Limit: limit, Offset: offset}
	if err := repository.pool.QueryRow(ctx, `
SELECT count(*) FROM workgroups.task_assignments WHERE task_id = $1`, taskID).Scan(&page.Total); err != nil {
		return TaskAssignmentPage{}, fmt.Errorf("count workgroup task assignments: %w", err)
	}
	rows, err := repository.pool.Query(ctx, taskAssignmentSelect+`
WHERE assignment.task_id = $1
ORDER BY
    CASE
      WHEN submission.id IS NOT NULL AND review.decision IS NULL THEN 0
      WHEN review.decision = 'rejected' THEN 1
      WHEN submission.id IS NULL THEN 2
      ELSE 3
    END,
    users.numeric_id
LIMIT $2 OFFSET $3`, taskID, limit, offset)
	if err != nil {
		return TaskAssignmentPage{}, fmt.Errorf("list workgroup task assignments: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		assignment, scanErr := scanTaskAssignment(rows, asOf)
		if scanErr != nil {
			return TaskAssignmentPage{}, fmt.Errorf("scan workgroup task assignment: %w", scanErr)
		}
		page.Items = append(page.Items, assignment)
	}
	if err := rows.Err(); err != nil {
		return TaskAssignmentPage{}, fmt.Errorf("iterate workgroup task assignments: %w", err)
	}
	return page, nil
}

func (repository *PostgresRepository) ReviewTaskSubmission(ctx context.Context, command ReviewTaskSubmissionCommand) (TaskAssignment, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return TaskAssignment{}, fmt.Errorf("begin workgroup task review: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var replaySubmissionID uuid.UUID
	var replayDecision TaskReviewDecision
	var replayReason string
	err = tx.QueryRow(ctx, `
SELECT submission_id, decision, reason
FROM workgroups.task_reviews
WHERE request_id = $1`, command.RequestID).Scan(&replaySubmissionID, &replayDecision, &replayReason)
	if err == nil {
		if replaySubmissionID != command.SubmissionID || replayDecision != command.Decision || replayReason != command.Reason {
			return TaskAssignment{}, ErrIdempotencyConflict
		}
		var assignmentID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT assignment_id FROM workgroups.task_submissions WHERE id = $1`, replaySubmissionID).Scan(&assignmentID); err != nil {
			return TaskAssignment{}, fmt.Errorf("resolve workgroup task review replay: %w", err)
		}
		replayed, scanErr := scanTaskAssignment(tx.QueryRow(ctx, taskAssignmentSelect+`
WHERE assignment.id = $1`, assignmentID), command.OccurredAt)
		if scanErr != nil {
			return TaskAssignment{}, fmt.Errorf("read workgroup task review replay: %w", scanErr)
		}
		if err := tx.Commit(ctx); err != nil {
			return TaskAssignment{}, fmt.Errorf("commit workgroup task review replay: %w", err)
		}
		return replayed, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return TaskAssignment{}, fmt.Errorf("read workgroup task review idempotency key: %w", err)
	}

	var assignmentID uuid.UUID
	err = tx.QueryRow(ctx, `
SELECT assignment_id
FROM workgroups.task_submissions
WHERE id = $1
FOR UPDATE`, command.SubmissionID).Scan(&assignmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return TaskAssignment{}, ErrTaskSubmissionNotFound
	}
	if err != nil {
		return TaskAssignment{}, fmt.Errorf("lock workgroup task submission: %w", err)
	}
	var latestSubmissionID uuid.UUID
	if err := tx.QueryRow(ctx, `
SELECT id FROM workgroups.task_submissions
WHERE assignment_id = $1
ORDER BY sequence DESC
LIMIT 1`, assignmentID).Scan(&latestSubmissionID); err != nil {
		return TaskAssignment{}, fmt.Errorf("resolve latest workgroup task submission: %w", err)
	}
	if latestSubmissionID != command.SubmissionID {
		return TaskAssignment{}, ErrTaskReviewConflict
	}
	var alreadyReviewed bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM workgroups.task_reviews WHERE submission_id = $1)`, command.SubmissionID).Scan(&alreadyReviewed); err != nil {
		return TaskAssignment{}, fmt.Errorf("check workgroup task review: %w", err)
	}
	if alreadyReviewed {
		return TaskAssignment{}, ErrTaskReviewConflict
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO workgroups.task_reviews (
    id, request_id, submission_id, decision, reason, decided_by,
    authorization_decision_id, decided_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		command.ReviewID, command.RequestID, command.SubmissionID,
		command.Decision, command.Reason, command.ActorID,
		command.AuthorizationDecisionID, command.OccurredAt,
	); err != nil {
		if constraintName(err) == "task_reviews_request_id_key" {
			return TaskAssignment{}, ErrIdempotencyConflict
		}
		if constraintName(err) == "task_reviews_submission_id_key" {
			return TaskAssignment{}, ErrTaskReviewConflict
		}
		return TaskAssignment{}, fmt.Errorf("insert workgroup task review: %w", err)
	}
	reviewed, err := scanTaskAssignment(tx.QueryRow(ctx, taskAssignmentSelect+`
WHERE assignment.id = $1`, assignmentID), command.OccurredAt)
	if err != nil {
		return TaskAssignment{}, fmt.Errorf("read reviewed workgroup task assignment: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return TaskAssignment{}, fmt.Errorf("commit workgroup task review: %w", err)
	}
	return reviewed, nil
}

func scanTask(row scanner, asOf time.Time) (Task, error) {
	var task Task
	if err := row.Scan(
		&task.ID, &task.GroupKind, &task.Type, &task.Title, &task.Description,
		&task.StartsAt, &task.DueAt, &task.AssignmentCount,
		&task.SubmittedCount, &task.PendingReviewCount, &task.AcceptedCount,
		&task.CreatedAt,
	); err != nil {
		return Task{}, err
	}
	if task.ID == uuid.Nil || !validGroupKind(task.GroupKind) || !validTaskType(task.Type) ||
		!validTaskTitle(task.Title) || !validTaskDescription(task.Description) ||
		!task.DueAt.After(task.StartsAt) || task.AssignmentCount < 1 ||
		task.SubmittedCount < 0 || task.PendingReviewCount < 0 || task.AcceptedCount < 0 ||
		task.SubmittedCount > task.AssignmentCount || task.PendingReviewCount > task.SubmittedCount ||
		task.AcceptedCount > task.SubmittedCount {
		return Task{}, errors.New("workgroup task projection is invalid")
	}
	task.StartsAt = canonicalTime(task.StartsAt)
	task.DueAt = canonicalTime(task.DueAt)
	task.CreatedAt = canonicalTime(task.CreatedAt)
	task.TimelineState = taskTimelineState(task.StartsAt, task.DueAt, canonicalTime(asOf))
	return task, nil
}

func scanTaskAssignment(row scanner, asOf time.Time) (TaskAssignment, error) {
	var assignment TaskAssignment
	var membershipStatus MembershipStatus
	var submissionID pgtype.UUID
	var sequence pgtype.Int8
	var statement pgtype.Text
	var submittedAt pgtype.Timestamptz
	var decision pgtype.Text
	var reviewReason pgtype.Text
	var decidedAt pgtype.Timestamptz
	if err := row.Scan(
		&assignment.ID, &assignment.MembershipID, &assignment.UserID,
		&assignment.UserNumericID, &assignment.Username, &assignment.DisplayName,
		&membershipStatus,
		&assignment.Task.ID, &assignment.Task.GroupKind, &assignment.Task.Type,
		&assignment.Task.Title, &assignment.Task.Description,
		&assignment.Task.StartsAt, &assignment.Task.DueAt,
		&assignment.Task.AssignmentCount, &assignment.Task.SubmittedCount,
		&assignment.Task.PendingReviewCount, &assignment.Task.AcceptedCount,
		&assignment.Task.CreatedAt,
		&submissionID, &sequence, &statement, &submittedAt,
		&decision, &reviewReason, &decidedAt,
	); err != nil {
		return TaskAssignment{}, err
	}
	if assignment.ID == uuid.Nil || assignment.MembershipID == uuid.Nil ||
		assignment.UserID == uuid.Nil || assignment.UserNumericID < 1 {
		return TaskAssignment{}, errors.New("workgroup task assignment projection is invalid")
	}
	task, err := validateScannedTask(assignment.Task, asOf)
	if err != nil {
		return TaskAssignment{}, err
	}
	assignment.Task = task
	assignment.State = TaskAssignmentNotSubmitted
	if submissionID.Valid {
		if !sequence.Valid || sequence.Int64 < 1 || !statement.Valid ||
			!validTaskStatement(statement.String) || !submittedAt.Valid {
			return TaskAssignment{}, errors.New("workgroup task submission projection is invalid")
		}
		submission := &TaskSubmission{
			ID: uuid.UUID(submissionID.Bytes), Sequence: sequence.Int64,
			Statement: statement.String, SubmittedAt: canonicalTime(submittedAt.Time),
		}
		assignment.State = TaskAssignmentPendingReview
		if decision.Valid {
			value := TaskReviewDecision(decision.String)
			if !validTaskReviewDecision(value) || !reviewReason.Valid || !validReason(reviewReason.String) || !decidedAt.Valid {
				return TaskAssignment{}, errors.New("workgroup task review projection is invalid")
			}
			submission.Decision = &value
			submission.ReviewReason = reviewReason.String
			valueTime := canonicalTime(decidedAt.Time)
			submission.DecidedAt = &valueTime
			if value == TaskReviewAccepted {
				assignment.State = TaskAssignmentAccepted
			} else {
				assignment.State = TaskAssignmentChangesRequested
			}
		} else if reviewReason.Valid || decidedAt.Valid {
			return TaskAssignment{}, errors.New("workgroup task review projection is incomplete")
		}
		assignment.LatestSubmission = submission
	} else if sequence.Valid || statement.Valid || submittedAt.Valid || decision.Valid || reviewReason.Valid || decidedAt.Valid {
		return TaskAssignment{}, errors.New("workgroup task submission projection is incomplete")
	}
	assignment.CanSubmit = membershipStatus == MembershipActive &&
		assignment.Task.TimelineState == TaskOpen &&
		(assignment.State == TaskAssignmentNotSubmitted || assignment.State == TaskAssignmentChangesRequested)
	return assignment, nil
}

func validateScannedTask(task Task, asOf time.Time) (Task, error) {
	if task.ID == uuid.Nil || !validGroupKind(task.GroupKind) || !validTaskType(task.Type) ||
		!validTaskTitle(task.Title) || !validTaskDescription(task.Description) ||
		!task.DueAt.After(task.StartsAt) || task.AssignmentCount < 1 ||
		task.SubmittedCount < 0 || task.PendingReviewCount < 0 || task.AcceptedCount < 0 ||
		task.SubmittedCount > task.AssignmentCount || task.PendingReviewCount > task.SubmittedCount ||
		task.AcceptedCount > task.SubmittedCount {
		return Task{}, errors.New("workgroup task projection is invalid")
	}
	task.StartsAt = canonicalTime(task.StartsAt)
	task.DueAt = canonicalTime(task.DueAt)
	task.CreatedAt = canonicalTime(task.CreatedAt)
	task.TimelineState = taskTimelineState(task.StartsAt, task.DueAt, canonicalTime(asOf))
	return task, nil
}

func sameTaskPublication(task Task, command PublishTaskCommand) bool {
	return task.GroupKind == command.GroupKind && task.Type == command.Type &&
		task.Title == command.Title &&
		task.Description == command.Description && task.StartsAt.Equal(command.StartsAt) &&
		task.DueAt.Equal(command.DueAt) && task.CreatedAt.Equal(command.OccurredAt)
}
