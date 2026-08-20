package traffic

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/contracts/go/settlementhnrv1"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	DefaultHNRLimit           = 20
	MaximumHNRLimit           = 50
	MaximumHNRAppealListLimit = 100
)

type HNRStatus string

const (
	HNRStatusTracking  HNRStatus = "tracking"
	HNRStatusGrace     HNRStatus = "grace"
	HNRStatusOverdue   HNRStatus = "overdue"
	HNRStatusSatisfied HNRStatus = "satisfied"
	HNRStatusExempt    HNRStatus = "exempt"
)

// HNRNotificationEvent names member-facing lifecycle and appeal outcome facts.
// Appeal outcomes share the inbox contract with obligation events, while their
// persisted sources remain separate and strongly typed.
type HNRNotificationEvent string

const (
	HNRNotificationGraceStarted       HNRNotificationEvent = "grace_started"
	HNRNotificationDownloadRestricted HNRNotificationEvent = "download_restricted"
	HNRNotificationSatisfied          HNRNotificationEvent = "satisfied"
	HNRNotificationAppealApproved     HNRNotificationEvent = "appeal_approved"
	HNRNotificationAppealRejected     HNRNotificationEvent = "appeal_rejected"
)

type HNRFilter string

const (
	HNRFilterAll       HNRFilter = "all"
	HNRFilterOpen      HNRFilter = "open"
	HNRFilterTracking  HNRFilter = "tracking"
	HNRFilterGrace     HNRFilter = "grace"
	HNRFilterOverdue   HNRFilter = "overdue"
	HNRFilterSatisfied HNRFilter = "satisfied"
	HNRFilterExempt    HNRFilter = "exempt"
)

type HNRApplyResult struct {
	EventID   uuid.UUID
	Duplicate bool
}

type HNRProjector interface {
	ApplyHNR(context.Context, []byte, time.Time) (HNRApplyResult, error)
}

type HNRCursor struct {
	CompletedAt  time.Time
	ObligationID uuid.UUID
}

type HNRQuery struct {
	Filter HNRFilter
	Limit  int
	Cursor *HNRCursor
}

type HNRSummary struct {
	Total     int64
	Tracking  int64
	Grace     int64
	Overdue   int64
	Satisfied int64
	Exempt    int64
}

type HNREntry struct {
	ObligationID             uuid.UUID
	TorrentID                int64
	TorrentTitle             string
	CompletedAt              time.Time
	Status                   HNRStatus
	SeededSeconds            int64
	RequiredSeedSeconds      int64
	RawUploaded              int64
	RawDownloaded            int64
	RawRatioBasisPoints      int64
	RequiredRatioBasisPoints int64
	AssessmentDueAt          time.Time
	GraceEndsAt              time.Time
	SatisfiedBy              *settlementhnrv1.SatisfiedBy
	SatisfiedAt              *time.Time
	UpdatedAt                time.Time
	Appeal                   *MyHNRAppeal
	CanAppeal                bool
}

type HNRPage struct {
	AsOf       time.Time
	Summary    HNRSummary
	Items      []HNREntry
	NextCursor *HNRCursor
}

type HNRRepository interface {
	ListHNR(context.Context, uuid.UUID, HNRQuery) (HNRPage, error)
}

type HNRAppealStatus string

const (
	HNRAppealPending            HNRAppealStatus = "pending"
	HNRAppealApproved           HNRAppealStatus = "approved"
	HNRAppealRejected           HNRAppealStatus = "rejected"
	HNRAppealObligationResolved HNRAppealStatus = "obligation_resolved"
)

type MyHNRAppeal struct {
	Status      HNRAppealStatus
	Statement   string
	SubmittedAt time.Time
	ResolvedAt  *time.Time
	Response    string
}

type SubmitHNRAppealInput struct {
	AppealID     uuid.UUID
	ObligationID uuid.UUID
	Statement    string
}

type SubmitHNRAppealCommand struct {
	SubmitHNRAppealInput
	UserID        uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type HNRAppeal struct {
	ID                       uuid.UUID
	ObligationID             uuid.UUID
	UserID                   uuid.UUID
	UserNumericID            int64
	Username                 string
	TorrentID                int64
	TorrentTitle             string
	Statement                string
	CreatedAt                time.Time
	Status                   HNRAppealStatus
	Response                 string
	ResolvedAt               *time.Time
	ObligationStatus         HNRStatus
	ObligationVersion        int64
	SeededSeconds            int64
	RequiredSeedSeconds      int64
	RawRatioBasisPoints      int64
	RequiredRatioBasisPoints int64
	GraceEndsAt              time.Time
	Replayed                 bool
}

type HNRAppealFilter string

const (
	HNRAppealFilterAll      HNRAppealFilter = "all"
	HNRAppealFilterPending  HNRAppealFilter = "pending"
	HNRAppealFilterResolved HNRAppealFilter = "resolved"
)

type HNRAppealQuery struct {
	Query  string
	Filter HNRAppealFilter
	Limit  int
	Offset int
}

type HNRAppealPage struct {
	Items  []HNRAppeal
	Total  int64
	Limit  int
	Offset int
}

type HNRAppealDecision string

const (
	HNRAppealDecisionApprove HNRAppealDecision = "approved"
	HNRAppealDecisionReject  HNRAppealDecision = "rejected"
)

type DecideHNRAppealInput struct {
	AppealID                  uuid.UUID
	Decision                  HNRAppealDecision
	ExpectedObligationVersion int64
	Response                  string
}

type DecideHNRAppealCommand struct {
	DecideHNRAppealInput
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type HNRAppealRepository interface {
	SubmitHNRAppeal(context.Context, SubmitHNRAppealCommand) (HNRAppeal, error)
	HNRAppeals(context.Context, HNRAppealQuery) (HNRAppealPage, error)
	DecideHNRAppeal(context.Context, DecideHNRAppealCommand) (HNRAppeal, error)
}
