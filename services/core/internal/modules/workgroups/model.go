// Package workgroups owns PeerGo's typed operational workgroups, member
// applications and immutable membership timeline. It deliberately does not
// expose arbitrary permission strings: each GroupKind has one code-owned
// business entitlement consumed by another bounded context.
package workgroups

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInput                      = errors.New("workgroup input is invalid")
	ErrGroupNotFound              = errors.New("workgroup was not found")
	ErrApplicationNotAllowed      = errors.New("workgroup does not accept applications")
	ErrApplicationNotEligible     = errors.New("applicant does not meet workgroup requirements")
	ErrApplicationPending         = errors.New("a workgroup application is already pending")
	ErrApplicationNotFound        = errors.New("workgroup application was not found")
	ErrApplicationConflict        = errors.New("workgroup application version changed")
	ErrMembershipAlreadyActive    = errors.New("workgroup membership is already active")
	ErrMembershipNotFound         = errors.New("workgroup membership was not found")
	ErrMembershipConflict         = errors.New("workgroup membership version changed")
	ErrMembershipTransition       = errors.New("workgroup membership transition is invalid")
	ErrContributionPolicyConflict = errors.New("workgroup contribution policy timeline conflicts")
	ErrContributionPolicyNoChange = errors.New("workgroup contribution target did not change")
	ErrContributionReminderExists = errors.New("workgroup contribution reminder already exists")
	ErrContributionReminderDenied = errors.New("workgroup contribution cycle cannot be reminded")
	ErrUserNotFound               = errors.New("workgroup target user was not found")
	ErrIdempotencyConflict        = errors.New("workgroup request id was reused")
)

type GroupKind string

const (
	GroupReseed    GroupKind = "reseed"
	GroupReview    GroupKind = "review"
	GroupRetention GroupKind = "retention"
)

type JoinMode string

const (
	JoinStaffOnly   JoinMode = "staff_only"
	JoinApplication JoinMode = "application"
)

// Entitlement is intentionally closed. Adding a new capability requires an
// explicit domain change instead of a database-only permission invention.
type Entitlement string

const (
	EntitlementTrustedTorrentPublish Entitlement = "torrent.publish.trusted"
	EntitlementTorrentReviewVote     Entitlement = "torrent.review.vote"
	EntitlementDownloadChargeExempt  Entitlement = "traffic.download.charge_exempt"
)

func EntitlementFor(kind GroupKind) (Entitlement, bool) {
	switch kind {
	case GroupReseed:
		return EntitlementTrustedTorrentPublish, true
	case GroupReview:
		return EntitlementTorrentReviewVote, true
	case GroupRetention:
		return EntitlementDownloadChargeExempt, true
	default:
		return "", false
	}
}

type ApplicationStatus string

const (
	ApplicationPending  ApplicationStatus = "pending"
	ApplicationApproved ApplicationStatus = "approved"
	ApplicationRejected ApplicationStatus = "rejected"
)

type MembershipStatus string

const (
	MembershipActive    MembershipStatus = "active"
	MembershipSuspended MembershipStatus = "suspended"
	MembershipEnded     MembershipStatus = "ended"
)

type MembershipTransition string

const (
	TransitionSuspend    MembershipTransition = "suspended"
	TransitionReactivate MembershipTransition = "reactivated"
	TransitionEnd        MembershipTransition = "ended"
)

type Definition struct {
	Kind        GroupKind
	DisplayName string
	Description string
	JoinMode    JoinMode
	Entitlement Entitlement
	Enabled     bool
	SortOrder   int32
	Version     int64
}

type ReviewerEligibility struct {
	PolicyRevision              int64 `json:"policy_revision"`
	Eligible                    bool  `json:"eligible"`
	Level                       int32 `json:"level"`
	MinimumLevel                int32 `json:"minimum_level"`
	CreditedUploaded            int64 `json:"credited_uploaded"`
	MinimumCreditedUploaded     int64 `json:"minimum_credited_uploaded"`
	AccountAgeDays              int32 `json:"account_age_days"`
	MinimumAccountAgeDays       int32 `json:"minimum_account_age_days"`
	EmailVerified               bool  `json:"email_verified"`
	RequireVerifiedEmail        bool  `json:"require_verified_email"`
	DownloadRestricted          bool  `json:"download_restricted"`
	RequireUnrestrictedDownload bool  `json:"require_unrestricted_download"`
	AccountActive               bool  `json:"account_active"`
}

type MyGroup struct {
	Definition  Definition
	Membership  *Membership
	Application *Application
	Eligibility *ReviewerEligibility
}

type MyOverview struct {
	Items []MyGroup
}

type Application struct {
	ID                   uuid.UUID
	GroupKind            GroupKind
	ApplicantID          uuid.UUID
	ApplicantNumericID   int64
	ApplicantUsername    string
	ApplicantDisplayName string
	Statement            string
	Status               ApplicationStatus
	PolicyRevision       *int64
	Eligibility          ReviewerEligibility
	Version              int64
	SubmittedAt          time.Time
	DecidedAt            *time.Time
}

type ApplicationPage struct {
	Items  []Application
	Total  int64
	Limit  int
	Offset int
}

type Membership struct {
	ID             uuid.UUID
	GroupKind      GroupKind
	UserID         uuid.UUID
	UserNumericID  int64
	Username       string
	DisplayName    string
	Status         MembershipStatus
	Source         string
	Version        int64
	StartedAt      time.Time
	EndedAt        *time.Time
	UpdatedAt      time.Time
	Contribution   *ContributionProgress
	LegacyReviewer *LegacyReviewerEvidence
}

type LegacyReviewerEvidence struct {
	Status         string
	ActivityStatus string
	TotalReviews   int64
	AccurateCount  int64
	LastActivityAt *time.Time
}

// ContributionMetric is deliberately closed and maps one-to-one to a group.
// Adding a metric requires a domain evidence query, contract and UI change.
type ContributionMetric string

const (
	MetricTrustedTorrentsPublished ContributionMetric = "trusted_torrents_published"
	MetricTorrentReviewVotes       ContributionMetric = "torrent_review_votes"
	MetricSeedingActiveSeconds     ContributionMetric = "seeding_active_seconds"
)

type ContributionEnforcementMode string

const (
	ContributionEnforcementObserve   ContributionEnforcementMode = "observe"
	ContributionEnforcementMissLimit ContributionEnforcementMode = "miss_limit"
)

type ContributionDisciplinaryAction string

const (
	ContributionDisciplinaryNone            ContributionDisciplinaryAction = "none"
	ContributionDisciplinaryMarked          ContributionDisciplinaryAction = "marked"
	ContributionDisciplinaryMembershipEnded ContributionDisciplinaryAction = "membership_ended"
)

type ContributionPolicy struct {
	GroupKind       GroupKind
	Revision        int64
	Metric          ContributionMetric
	PeriodKind      string
	TargetValue     int64
	EnforcementMode ContributionEnforcementMode
	AllowedMisses   int32
	EffectiveFrom   *time.Time
	Opening         bool
	Reason          string
	IssuedBy        *uuid.UUID
	RequestID       *uuid.UUID
	CreatedAt       time.Time
	TimelineState   ContributionPolicyTimelineState
	Replayed        bool
}

type ContributionPolicyTimelineState string

const (
	ContributionPolicyActive    ContributionPolicyTimelineState = "active"
	ContributionPolicyScheduled ContributionPolicyTimelineState = "scheduled"
)

type ContributionPolicyPage struct {
	Items                []ContributionPolicy
	Total                int64
	Limit                int
	Offset               int
	MinimumEffectiveFrom time.Time
	Current              *ContributionPolicy
}

// ContributionSummary aggregates only currently active members. Historical
// policy revisions and member evidence remain separate so a group total cannot
// be mistaken for an accounting ledger.
type ContributionSummary struct {
	GroupKind           GroupKind
	Metric              ContributionMetric
	PolicyRevision      int64
	PeriodStartsAt      time.Time
	PeriodEndsAt        time.Time
	ObservedAt          time.Time
	EvidenceThrough     *time.Time
	ActiveMembers       int64
	ContributingMembers int64
	MetMembers          int64
	TotalValue          int64
	TargetValue         int64
}

// ContributionProgress is a read projection over immutable publish, vote and
// seeding evidence. MissCount is scoped to the latest active membership tenure
// so a staff-approved reactivation starts a new, auditable probation window.
type ContributionProgress struct {
	GroupKind       GroupKind
	Metric          ContributionMetric
	PolicyRevision  int64
	PeriodKind      string
	PeriodStartsAt  time.Time
	PeriodEndsAt    time.Time
	ObservedAt      time.Time
	EvidenceThrough *time.Time
	CurrentValue    int64
	TargetValue     int64
	Met             bool
	EnforcementMode ContributionEnforcementMode
	AllowedMisses   int32
	MissCount       int32
}

// ContributionEvidenceState distinguishes an open period from a broken or
// absent evidence chain. Staff must never treat "not complete" as "zero".
type ContributionEvidenceState string

const (
	ContributionEvidenceComplete    ContributionEvidenceState = "complete"
	ContributionEvidenceCollecting  ContributionEvidenceState = "collecting"
	ContributionEvidenceIncomplete  ContributionEvidenceState = "incomplete"
	ContributionEvidenceUnavailable ContributionEvidenceState = "unavailable"
)

// ContributionAssessmentState is derived from evidence. In particular,
// indeterminate and not_assessable are never disciplinary failures.
type ContributionAssessmentState string

const (
	ContributionAssessmentMet           ContributionAssessmentState = "met"
	ContributionAssessmentInProgress    ContributionAssessmentState = "in_progress"
	ContributionAssessmentNotMet        ContributionAssessmentState = "not_met"
	ContributionAssessmentNotAssessable ContributionAssessmentState = "not_assessable"
	ContributionAssessmentIndeterminate ContributionAssessmentState = "indeterminate"
)

type ContributionExplanationCode string

const (
	ContributionExplanationTargetMet           ContributionExplanationCode = "target_met"
	ContributionExplanationPeriodInProgress    ContributionExplanationCode = "period_in_progress"
	ContributionExplanationBelowTarget         ContributionExplanationCode = "below_target"
	ContributionExplanationNoContribution      ContributionExplanationCode = "no_contribution"
	ContributionExplanationPartialMembership   ContributionExplanationCode = "partial_membership"
	ContributionExplanationMembershipInactive  ContributionExplanationCode = "membership_inactive"
	ContributionExplanationEvidenceIncomplete  ContributionExplanationCode = "evidence_incomplete"
	ContributionExplanationEvidenceUnavailable ContributionExplanationCode = "evidence_unavailable"
)

// ContributionCycle is rebuilt from immutable membership, publish, review and
// seeding evidence. Enforcement points at a frozen assessment when that cycle
// was actually settled; absent evidence never creates an assessment.
type ContributionCycle struct {
	GroupKind        GroupKind
	Metric           ContributionMetric
	PolicyRevision   int64
	PeriodStartsAt   time.Time
	PeriodEndsAt     time.Time
	ObservedAt       time.Time
	EvidenceThrough  *time.Time
	EvidenceState    ContributionEvidenceState
	ActiveSeconds    int64
	FullPeriodActive bool
	CurrentValue     int64
	TargetValue      int64
	AssessmentState  ContributionAssessmentState
	ExplanationCode  ContributionExplanationCode
	EnforcementMode  ContributionEnforcementMode
	AllowedMisses    int32
	Reminder         *ContributionReminder
	Enforcement      *ContributionEnforcementAssessment
}

// ContributionEnforcementAssessment is one compact, immutable monthly result.
// It stores no raw torrent events; those remain in their owning domain.
type ContributionEnforcementAssessment struct {
	ID                     uuid.UUID
	MembershipID           uuid.UUID
	TenureTransitionID     uuid.UUID
	GroupKind              GroupKind
	RecipientUserID        uuid.UUID
	Metric                 ContributionMetric
	PolicyRevision         int64
	PeriodStartsAt         time.Time
	PeriodEndsAt           time.Time
	ObservedAt             time.Time
	EvidenceThrough        time.Time
	EvidenceState          ContributionEvidenceState
	CurrentValue           int64
	TargetValue            int64
	AssessmentState        ContributionAssessmentState
	ExplanationCode        ContributionExplanationCode
	MissCount              int32
	AllowedMisses          int32
	DisciplinaryAction     ContributionDisciplinaryAction
	MembershipTransitionID *uuid.UUID
	Reason                 string
	AssessedAt             time.Time
}

type ContributionEnforcementResult struct {
	Skipped  bool
	Examined int
	Recorded int
	Marked   int
	Ended    int
}

// ContributionReminder is the frozen, member-visible observation that backed
// one manual reminder. It intentionally contains no mutable enforcement state
// and exposes neither staff identity nor the authorization decision.
type ContributionReminder struct {
	ID                 uuid.UUID
	MembershipID       uuid.UUID
	GroupKind          GroupKind
	RecipientUserID    uuid.UUID
	Metric             ContributionMetric
	PolicyRevision     int64
	PeriodStartsAt     time.Time
	PeriodEndsAt       time.Time
	ObservedAt         time.Time
	EvidenceThrough    *time.Time
	EvidenceState      ContributionEvidenceState
	CurrentValue       int64
	TargetValue        int64
	AssessmentState    ContributionAssessmentState
	ExplanationCode    ContributionExplanationCode
	FullPeriodActive   bool
	Reason             string
	IssuedBy           uuid.UUID
	NotificationID     uuid.UUID
	NotificationReadAt *time.Time
	CreatedAt          time.Time
	Replayed           bool
}

type ContributionCyclePage struct {
	Items []ContributionCycle
	Limit int
}

type MembershipPage struct {
	Items  []Membership
	Total  int64
	Limit  int
	Offset int
}

type AdminOverview struct {
	Definitions           []Definition
	PendingApplications   int64
	ActiveByKind          map[GroupKind]int64
	ContributionSummaries []ContributionSummary
}

type SubmitApplicationCommand struct {
	ApplicationID           uuid.UUID
	RequestID               uuid.UUID
	ApplicantID             uuid.UUID
	GroupKind               GroupKind
	Statement               string
	AuthorizationDecisionID uuid.UUID
	OccurredAt              time.Time
}

type DecideApplicationCommand struct {
	DecisionID              uuid.UUID
	ApplicationID           uuid.UUID
	ExpectedVersion         int64
	Approve                 bool
	ActorID                 uuid.UUID
	Reason                  string
	AuthorizationDecisionID uuid.UUID
	OccurredAt              time.Time
}

type GrantMembershipCommand struct {
	TransitionID            uuid.UUID
	GroupKind               GroupKind
	UserNumericID           int64
	ActorID                 uuid.UUID
	Reason                  string
	AuthorizationDecisionID uuid.UUID
	OccurredAt              time.Time
}

type ChangeMembershipCommand struct {
	TransitionID            uuid.UUID
	MembershipID            uuid.UUID
	GroupKind               GroupKind
	ExpectedVersion         int64
	Transition              MembershipTransition
	ActorID                 uuid.UUID
	Reason                  string
	AuthorizationDecisionID uuid.UUID
	OccurredAt              time.Time
}

type IssueContributionPolicyCommand struct {
	RequestID               uuid.UUID
	GroupKind               GroupKind
	TargetValue             int64
	EffectiveFrom           time.Time
	ActorID                 uuid.UUID
	Reason                  string
	AuthorizationDecisionID uuid.UUID
	OccurredAt              time.Time
}

type IssueContributionReminderCommand struct {
	RequestID               uuid.UUID
	MembershipID            uuid.UUID
	GroupKind               GroupKind
	PeriodStartsAt          time.Time
	ActorID                 uuid.UUID
	Reason                  string
	AuthorizationDecisionID uuid.UUID
	OccurredAt              time.Time
}

// Repository owns both current projections and append-only history. Commands
// that change an application and membership are committed atomically.
type Repository interface {
	MyOverview(context.Context, uuid.UUID, time.Time) (MyOverview, error)
	SubmitApplication(context.Context, SubmitApplicationCommand) (Application, error)
	AdminOverview(context.Context, time.Time) (AdminOverview, error)
	ListApplications(context.Context, ApplicationStatus, int, int) (ApplicationPage, error)
	DecideApplication(context.Context, DecideApplicationCommand) (Application, error)
	ListMemberships(context.Context, GroupKind, MembershipStatus, int, int, time.Time) (MembershipPage, error)
	GrantMembership(context.Context, GrantMembershipCommand) (Membership, error)
	ChangeMembership(context.Context, ChangeMembershipCommand) (Membership, error)
	ListContributionPolicies(context.Context, GroupKind, int, int, time.Time) (ContributionPolicyPage, error)
	IssueContributionPolicy(context.Context, IssueContributionPolicyCommand) (ContributionPolicy, error)
	IssueContributionReminder(context.Context, IssueContributionReminderCommand) (ContributionReminder, error)
	ListMyContributionCycles(context.Context, uuid.UUID, GroupKind, int, time.Time) (ContributionCyclePage, error)
	ListContributionCycles(context.Context, GroupKind, uuid.UUID, int, time.Time) (ContributionCyclePage, error)
	ListMyTasks(context.Context, uuid.UUID, int, int, time.Time) (TaskAssignmentPage, error)
	SubmitTask(context.Context, SubmitTaskCommand) (TaskAssignment, error)
	ListTasks(context.Context, GroupKind, int, int, time.Time) (TaskPage, error)
	PublishTask(context.Context, PublishTaskCommand) (Task, error)
	ListTaskAssignments(context.Context, GroupKind, uuid.UUID, int, int, time.Time) (TaskAssignmentPage, error)
	ReviewTaskSubmission(context.Context, ReviewTaskSubmissionCommand) (TaskAssignment, error)
	HasEntitlementAt(context.Context, uuid.UUID, Entitlement, time.Time) (bool, error)
}
