package workgroups

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrTaskNotFound             = errors.New("workgroup task was not found")
	ErrTaskNoMembers            = errors.New("workgroup task has no active members")
	ErrTaskAssignmentNotFound   = errors.New("workgroup task assignment was not found")
	ErrTaskSubmissionNotAllowed = errors.New("workgroup task submission is not allowed")
	ErrTaskSubmissionNotFound   = errors.New("workgroup task submission was not found")
	ErrTaskReviewConflict       = errors.New("workgroup task submission review conflicts")
)

// TaskType is closed because workgroup work is intentionally not a generic
// workflow builder. Both types share the same assignment and review rules;
// the distinction exists for the member-facing information hierarchy.
type TaskType string

const (
	TaskTypeTask     TaskType = "task"
	TaskTypeActivity TaskType = "activity"
)

type TaskTimelineState string

const (
	TaskScheduled TaskTimelineState = "scheduled"
	TaskOpen      TaskTimelineState = "open"
	TaskClosed    TaskTimelineState = "closed"
)

type TaskAssignmentState string

const (
	TaskAssignmentNotSubmitted     TaskAssignmentState = "not_submitted"
	TaskAssignmentPendingReview    TaskAssignmentState = "pending_review"
	TaskAssignmentChangesRequested TaskAssignmentState = "changes_requested"
	TaskAssignmentAccepted         TaskAssignmentState = "accepted"
)

type TaskReviewDecision string

const (
	TaskReviewAccepted TaskReviewDecision = "accepted"
	TaskReviewRejected TaskReviewDecision = "rejected"
)

// Task is an immutable publication. Counts are derived from its frozen
// assignment audience and append-only submission/review facts.
type Task struct {
	ID                 uuid.UUID
	GroupKind          GroupKind
	Type               TaskType
	Title              string
	Description        string
	StartsAt           time.Time
	DueAt              time.Time
	TimelineState      TaskTimelineState
	AssignmentCount    int64
	SubmittedCount     int64
	PendingReviewCount int64
	AcceptedCount      int64
	CreatedAt          time.Time
	Replayed           bool
}

type TaskSubmission struct {
	ID           uuid.UUID
	Sequence     int64
	Statement    string
	SubmittedAt  time.Time
	Decision     *TaskReviewDecision
	ReviewReason string
	DecidedAt    *time.Time
}

// TaskAssignment combines an immutable member snapshot with the latest
// append-only attempt. Staff identity and authorization evidence never cross
// this DTO boundary.
type TaskAssignment struct {
	ID               uuid.UUID
	MembershipID     uuid.UUID
	UserID           uuid.UUID
	UserNumericID    int64
	Username         string
	DisplayName      string
	Task             Task
	State            TaskAssignmentState
	CanSubmit        bool
	LatestSubmission *TaskSubmission
}

type TaskPage struct {
	Items  []Task
	Total  int64
	Limit  int
	Offset int
}

type TaskAssignmentPage struct {
	Items  []TaskAssignment
	Total  int64
	Limit  int
	Offset int
}

type PublishTaskCommand struct {
	TaskID                  uuid.UUID
	RequestID               uuid.UUID
	GroupKind               GroupKind
	Type                    TaskType
	Title                   string
	Description             string
	StartsAt                time.Time
	DueAt                   time.Time
	ActorID                 uuid.UUID
	AuthorizationDecisionID uuid.UUID
	OccurredAt              time.Time
}

type SubmitTaskCommand struct {
	SubmissionID            uuid.UUID
	RequestID               uuid.UUID
	AssignmentID            uuid.UUID
	UserID                  uuid.UUID
	Statement               string
	AuthorizationDecisionID uuid.UUID
	OccurredAt              time.Time
}

type ReviewTaskSubmissionCommand struct {
	ReviewID                uuid.UUID
	RequestID               uuid.UUID
	SubmissionID            uuid.UUID
	Decision                TaskReviewDecision
	Reason                  string
	ActorID                 uuid.UUID
	AuthorizationDecisionID uuid.UUID
	OccurredAt              time.Time
}

func taskTimelineState(startsAt, dueAt, asOf time.Time) TaskTimelineState {
	switch {
	case asOf.Before(startsAt):
		return TaskScheduled
	case asOf.After(dueAt):
		return TaskClosed
	default:
		return TaskOpen
	}
}
