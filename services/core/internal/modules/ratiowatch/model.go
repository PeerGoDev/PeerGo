// Package ratiowatch owns the long-term, whole-account share-ratio policy and
// its assessments. It deliberately does not own per-torrent H&R or recalculate
// Settlement history: only final Core traffic totals are eligible inputs.
package ratiowatch

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	DefaultRuleID       = "global-default"
	DefaultListLimit    = 20
	MaximumListLimit    = 100
	DefaultWorkerBatch  = 500
	MaximumWorkerBatch  = 5000
	RatioScale          = int64(10_000)
	MaximumRatioBPS     = int64(1_000_000)
	MinimumDownloadGate = int64(1 << 30)
)

var (
	ErrInput               = errors.New("ratio watch input is invalid")
	ErrConflict            = errors.New("ratio watch state conflicts with current history")
	ErrIdempotencyConflict = errors.New("ratio watch idempotency key was reused")
	ErrNoChange            = errors.New("ratio watch policy did not change")
	ErrNotFound            = errors.New("ratio watch assessment was not found")
	ErrNotActive           = errors.New("ratio watch assessment is not active")
	ErrNoActiveAssessment  = errors.New("ratio watch user has no active assessment")
	ErrAppealExists        = errors.New("ratio watch assessment already has an appeal")
	ErrAppealResolved      = errors.New("ratio watch appeal is already resolved")
	ErrSelfTarget          = errors.New("staff actor cannot clear their own ratio assessment")
	ErrInvariant           = errors.New("ratio watch invariant failed")
)

type PolicyInput struct {
	Enabled                     bool
	DownloadThresholdBytes      int64
	MinimumRatioBasisPoints     int64
	WatchPeriodSeconds          int64
	RestrictionRatioBasisPoints int64
}

type PolicyRevision struct {
	ID          uuid.UUID
	RuleID      string
	RuleVersion int64
	PolicyInput
	VIPExempt               bool
	EffectiveAt             time.Time
	Reason                  string
	ActorID                 uuid.UUID
	AuthorizationDecisionID uuid.UUID
	CommandSHA256           [32]byte
	CreatedAt               time.Time
	TimelineState           TimelineState
	Replayed                bool
}

type TimelineState string

const (
	TimelineScheduled TimelineState = "scheduled"
	TimelineActive    TimelineState = "active"
)

type IssueInput struct {
	RevisionID  uuid.UUID
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

type ImpactPreview struct {
	Policy                  PolicyInput
	EligibleUsers           int64
	WouldEnterWatch         int64
	WouldRestrictAtDeadline int64
	VIPExemptUsers          int64
	LegacyRestrictedUsers   int64
}

type AssessmentStatus string

const (
	AssessmentWatching           AssessmentStatus = "watching"
	AssessmentWarning            AssessmentStatus = "warning"
	AssessmentDownloadRestricted AssessmentStatus = "download_restricted"
	AssessmentSatisfied          AssessmentStatus = "satisfied"
	AssessmentManuallyCleared    AssessmentStatus = "manually_cleared"
	AssessmentVIPExempted        AssessmentStatus = "vip_exempted"
	AssessmentIneligible         AssessmentStatus = "ineligible"
)

func (status AssessmentStatus) Active() bool {
	return status == AssessmentWatching || status == AssessmentWarning || status == AssessmentDownloadRestricted
}

type Assessment struct {
	ID                       uuid.UUID
	UserID                   uuid.UUID
	UserNumericID            int64
	Username                 string
	PolicyRevisionID         uuid.UUID
	PolicyVersion            int64
	Status                   AssessmentStatus
	StartedAt                time.Time
	DeadlineAt               time.Time
	OpeningCreditedUploaded  int64
	OpeningChargedDownloaded int64
	OpeningRatioBasisPoints  int64
	CurrentCreditedUploaded  int64
	CurrentChargedDownloaded int64
	CurrentRatioBasisPoints  int64
	RestrictionStartedAt     *time.Time
	ResolvedAt               *time.Time
	ResolutionCode           string
	ResolutionReason         string
	ResolvedBy               *uuid.UUID
	Version                  int64
	UpdatedAt                time.Time
	LegacyDownloadRestricted bool
}

type AssessmentFilter string

const (
	AssessmentFilterAll        AssessmentFilter = "all"
	AssessmentFilterActive     AssessmentFilter = "active"
	AssessmentFilterWatching   AssessmentFilter = "watching"
	AssessmentFilterWarning    AssessmentFilter = "warning"
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
	Watching           int64
	Warning            int64
	DownloadRestricted int64
	Satisfied          int64
	ManuallyCleared    int64
	VIPExempted        int64
}

type WorkerState struct {
	LastStartedAt    *time.Time
	LastCompletedAt  *time.Time
	LastErrorCode    string
	LastExamined     int64
	LastCreated      int64
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

// RestrictionSource explains only the user-visible source category. It does
// not expose the staff actor, internal reason or immutable transition record.
type RestrictionSource string

const (
	RestrictionNone       RestrictionSource = "none"
	RestrictionRatioWatch RestrictionSource = "ratio_watch"
	RestrictionAccount    RestrictionSource = "account"
	RestrictionBoth       RestrictionSource = "both"
)

// MyPolicy is the minimum safe rule projection needed for a member to
// understand the threshold and recovery target that apply to them.
type MyPolicy struct {
	RuleVersion int64
	PolicyInput
	VIPExempt         bool
	EffectiveAt       time.Time
	BoundToAssessment bool
}

// MyAssessment intentionally contains only the active user-facing state. The
// assessment UUID, policy UUID and staff-only resolution history stay private.
type MyAssessment struct {
	Status               AssessmentStatus
	StartedAt            time.Time
	DeadlineAt           time.Time
	RestrictionStartedAt *time.Time
	UpdatedAt            time.Time
}

type AppealStatus string

const (
	AppealPending            AppealStatus = "pending"
	AppealApproved           AppealStatus = "approved"
	AppealRejected           AppealStatus = "rejected"
	AppealAssessmentResolved AppealStatus = "assessment_resolved"
)

// MyAppeal is the user-safe projection of one appeal. Operator identities and
// authorization evidence stay in the staff/audit boundary.
type MyAppeal struct {
	Status      AppealStatus
	Statement   string
	SubmittedAt time.Time
	ResolvedAt  *time.Time
	Response    string
}

// MyStatus combines final Core traffic totals with the one immutable policy
// version that currently applies to the member. Byte values remain int64 until
// the HTTP adapter serializes them as decimal strings.
type MyStatus struct {
	ObservedAt              time.Time
	CreditedUploaded        int64
	ChargedDownloaded       int64
	CurrentRatioBasisPoints int64
	VIPActive               bool
	DownloadRestricted      bool
	RestrictionSource       RestrictionSource
	ThresholdReached        bool
	MinimumRatioReached     bool
	RecoveryUploadedBytes   int64
	Policy                  *MyPolicy
	Assessment              *MyAssessment
	Appeal                  *MyAppeal
	CanAppeal               bool
}

type SubmitAppealInput struct {
	AppealID  uuid.UUID
	Statement string
}

type SubmitAppealCommand struct {
	SubmitAppealInput
	UserID        uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type Appeal struct {
	ID                       uuid.UUID
	AssessmentID             uuid.UUID
	UserID                   uuid.UUID
	UserNumericID            int64
	Username                 string
	Statement                string
	CreatedAt                time.Time
	Status                   AppealStatus
	Response                 string
	ResolvedAt               *time.Time
	AssessmentStatus         AssessmentStatus
	AssessmentVersion        int64
	CurrentCreditedUploaded  int64
	CurrentChargedDownloaded int64
	CurrentRatioBasisPoints  int64
	DeadlineAt               time.Time
	RestrictionStartedAt     *time.Time
	LegacyDownloadRestricted bool
	Replayed                 bool
}

type AppealFilter string

const (
	AppealFilterAll      AppealFilter = "all"
	AppealFilterPending  AppealFilter = "pending"
	AppealFilterResolved AppealFilter = "resolved"
)

type AppealQuery struct {
	Query  string
	Filter AppealFilter
	Limit  int
	Offset int
}

type AppealPage struct {
	Items  []Appeal
	Total  int64
	Limit  int
	Offset int
}

type AppealDecision string

const (
	AppealDecisionApprove AppealDecision = "approved"
	AppealDecisionReject  AppealDecision = "rejected"
)

type DecideAppealInput struct {
	AppealID                  uuid.UUID
	Decision                  AppealDecision
	ExpectedAssessmentVersion int64
	Response                  string
}

type DecideAppealCommand struct {
	DecideAppealInput
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type ClearInput struct {
	AssessmentID    uuid.UUID
	ExpectedVersion int64
	Reason          string
}

type ClearCommand struct {
	ClearInput
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type EvaluationResult struct {
	Examined     int64
	Created      int64
	Transitioned int64
	Skipped      bool
}

type Repository interface {
	MyStatus(context.Context, uuid.UUID, time.Time) (MyStatus, error)
	Policies(context.Context, int, int, time.Time) (PolicyPage, error)
	Preview(context.Context, PolicyInput, time.Time) (ImpactPreview, error)
	Issue(context.Context, IssueCommand) (PolicyRevision, error)
	Assessments(context.Context, AssessmentQuery) (AssessmentPage, error)
	Clear(context.Context, ClearCommand) (Assessment, error)
	SubmitAppeal(context.Context, SubmitAppealCommand) (Appeal, error)
	Appeals(context.Context, AppealQuery) (AppealPage, error)
	DecideAppeal(context.Context, DecideAppealCommand) (Appeal, error)
}

type Evaluator interface {
	Evaluate(context.Context, time.Time, int) (EvaluationResult, error)
}
