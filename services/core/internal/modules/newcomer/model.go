// Package newcomer owns the fixed new-registration assessment. It consumes
// Core's final traffic and complete seeding-evidence projections; it never
// reads Tracker hot state or rewrites historical accounting.
package newcomer

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	DefaultListLimit   = 30
	MaximumListLimit   = 100
	DefaultWorkerBatch = 500
	MaximumWorkerBatch = 5000
)

var (
	ErrInput               = errors.New("newcomer assessment input is invalid")
	ErrNotFound            = errors.New("newcomer assessment was not found")
	ErrConflict            = errors.New("newcomer assessment state conflicts with the request")
	ErrIdempotencyConflict = errors.New("newcomer idempotency key was reused")
	ErrNoChange            = errors.New("newcomer policy did not change")
	ErrSelfTarget          = errors.New("staff actor cannot exempt their own assessment")
	ErrInvariant           = errors.New("newcomer assessment invariant failed")
)

type PolicyInput struct {
	Enabled                     bool
	DurationSeconds             int64
	MinimumCreditedUploadBytes  int64
	MinimumSeedingActiveSeconds int64
}

type PolicyRevision struct {
	ID         uuid.UUID
	RequestID  *uuid.UUID
	Revision   int64
	SourceKind string
	PolicyInput
	EffectiveAt             time.Time
	Reason                  string
	ActorID                 *uuid.UUID
	AuthorizationDecisionID *uuid.UUID
	CreatedAt               time.Time
	TimelineState           string
	Replayed                bool
}

type IssueInput struct {
	RequestID   uuid.UUID
	Policy      PolicyInput
	EffectiveAt time.Time
	Reason      string
}

type IssueCommand struct {
	IssueInput
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type AssessmentStatus string

const (
	AssessmentActive             AssessmentStatus = "active"
	AssessmentDownloadRestricted AssessmentStatus = "download_restricted"
	AssessmentPassed             AssessmentStatus = "passed"
	AssessmentExempted           AssessmentStatus = "exempted"
)

func (status AssessmentStatus) Active() bool {
	return status == AssessmentActive || status == AssessmentDownloadRestricted
}

type Assessment struct {
	ID                          uuid.UUID
	UserID                      uuid.UUID
	UserNumericID               int64
	Username                    string
	DisplayName                 string
	PolicyRevisionID            uuid.UUID
	PolicyRevision              int64
	Status                      AssessmentStatus
	StartedAt                   time.Time
	DeadlineAt                  time.Time
	MinimumCreditedUploadBytes  int64
	MinimumSeedingActiveSeconds int64
	CurrentCreditedUploadBytes  int64
	CurrentSeedingActiveSeconds int64
	RestrictionStartedAt        *time.Time
	ResolvedAt                  *time.Time
	ResolutionCode              string
	Version                     int64
	UpdatedAt                   time.Time
}

type MyStatus struct {
	ObservedAt time.Time
	Assessment *Assessment
}

type AssessmentFilter string

const (
	AssessmentFilterAll        AssessmentFilter = "all"
	AssessmentFilterActive     AssessmentFilter = "active"
	AssessmentFilterRestricted AssessmentFilter = "download_restricted"
	AssessmentFilterResolved   AssessmentFilter = "resolved"
)

type AssessmentQuery struct {
	Query  string
	Filter AssessmentFilter
	Limit  int
	Offset int
}

type AssessmentPage struct {
	Items  []Assessment
	Total  int64
	Limit  int
	Offset int
}

type AssessmentSummary struct {
	Active             int64
	DownloadRestricted int64
	Passed             int64
	Exempted           int64
}

type WorkerState struct {
	LastStartedAt    *time.Time
	LastCompletedAt  *time.Time
	LastErrorCode    string
	LastExamined     int64
	LastTransitioned int64
	RunCount         int64
}

type PolicyPage struct {
	Items                []PolicyRevision
	Total                int64
	Limit                int
	Offset               int
	MinimumEffectiveFrom time.Time
	Current              *PolicyRevision
	Summary              AssessmentSummary
	Worker               WorkerState
}

type ExemptInput struct {
	ExemptionID     uuid.UUID
	AssessmentID    uuid.UUID
	ExpectedVersion int64
	Reason          string
}

type ExemptCommand struct {
	ExemptInput
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type AssignInput struct {
	AssignmentID uuid.UUID
	UserID       uuid.UUID
	Reason       string
}

type AssignCommand struct {
	AssignInput
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type EvaluationResult struct {
	Examined     int64
	Transitioned int64
	Skipped      bool
}

type Repository interface {
	MyStatus(context.Context, uuid.UUID, time.Time) (MyStatus, error)
	Policies(context.Context, int, int, time.Time) (PolicyPage, error)
	Issue(context.Context, IssueCommand) (PolicyRevision, error)
	Assessments(context.Context, AssessmentQuery) (AssessmentPage, error)
	Assign(context.Context, AssignCommand) (Assessment, error)
	Exempt(context.Context, ExemptCommand) (Assessment, error)
}

type Evaluator interface {
	Evaluate(context.Context, time.Time, int) (EvaluationResult, error)
}
