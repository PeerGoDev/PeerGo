// Package notifications owns private, monotonic inbox state. Notification
// sources remain typed business bindings instead of arbitrary payloads.
package notifications

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/economy/contenttip"
	"github.com/peergo/peergo/services/core/internal/modules/ratiowatch"
	"github.com/peergo/peergo/services/core/internal/modules/review"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	"github.com/peergo/peergo/services/core/internal/modules/traffic"
	"github.com/peergo/peergo/services/core/internal/modules/workgroups"
)

const (
	DefaultLimit  = 20
	MaximumLimit  = 50
	MaximumOffset = 99_999
)

var (
	ErrInput     = errors.New("notification input is invalid")
	ErrNotFound  = errors.New("notification was not found")
	ErrInvariant = errors.New("notification projection violates persisted invariants")
)

type Kind string

const (
	KindTorrentReview         Kind = "torrent_review"
	KindRatioWatch            Kind = "ratio_watch"
	KindRatioAppeal           Kind = "ratio_appeal"
	KindHNR                   Kind = "hnr"
	KindHNRAppeal             Kind = "hnr_appeal"
	KindWorkgroupContribution Kind = "workgroup_contribution"
	KindMemberGift            Kind = "member_gift"
	KindContentTip            Kind = "content_tip"
)

// Notification is a private inbox envelope around exactly one typed source.
// Staff identity and authorization/audit evidence never enter this user-safe
// projection; adding another source requires a concrete payload and validator.
type Notification struct {
	ID        uuid.UUID
	Kind      Kind
	CreatedAt time.Time
	ReadAt    *time.Time

	TorrentReview         *TorrentReviewNotification
	RatioWatch            *RatioWatchNotification
	RatioAppeal           *RatioAppealNotification
	HNR                   *HNRNotification
	WorkgroupContribution *WorkgroupContributionNotification
	MemberGift            *MemberGiftNotification
	ContentTip            *ContentTipNotification
}

type TorrentReviewNotification struct {
	TorrentID    torrents.TorrentID
	TorrentTitle string
	Outcome      torrents.State
	ReasonCode   review.ReasonCode
	Reason       string
}

type RatioWatchNotification struct {
	Status                      ratiowatch.AssessmentStatus
	RatioBasisPoints            int64
	MinimumRatioBasisPoints     int64
	RestrictionRatioBasisPoints int64
	DeadlineAt                  time.Time
}

type RatioAppealNotification struct {
	Status   ratiowatch.AppealStatus
	Response string
}

type HNRNotification struct {
	TorrentID    torrents.TorrentID
	TorrentTitle string
	Event        traffic.HNRNotificationEvent
	GraceEndsAt  time.Time
	Response     string
}

type WorkgroupContributionNotification struct {
	GroupKind          workgroups.GroupKind
	Metric             workgroups.ContributionMetric
	PolicyRevision     int64
	PeriodStartsAt     time.Time
	PeriodEndsAt       time.Time
	ObservedAt         time.Time
	EvidenceState      workgroups.ContributionEvidenceState
	CurrentValue       int64
	TargetValue        int64
	AssessmentState    workgroups.ContributionAssessmentState
	ExplanationCode    workgroups.ContributionExplanationCode
	Reason             string
	MissCount          *int32
	AllowedMisses      *int32
	DisciplinaryAction *workgroups.ContributionDisciplinaryAction
}

// MemberGiftNotification contains only the recipient-safe projection needed
// to understand one incoming transfer. Internal user, gift and ledger UUIDs
// remain in their owning tables and never enter the inbox contract.
type MemberGiftNotification struct {
	SenderNumericID   int64
	SenderUsername    string
	SenderDisplayName string
	NetAmount         int64
	Message           string
}

// ContentTipNotification exposes the incoming amount, sender's public member
// identity and typed content link. Receipt, account and transaction UUIDs stay
// private to the source aggregates.
type ContentTipNotification struct {
	SenderNumericID   int64
	SenderUsername    string
	SenderDisplayName string
	NetAmount         int64
	Target            contenttip.Target
}

// ListQuery is one indivisible inbox page definition. UnreadOnly belongs here
// instead of the browser because totals and offsets must describe the filtered
// database result, not whichever mixed page the client happened to receive.
type ListQuery struct {
	Limit      int
	Offset     int
	UnreadOnly bool
}

type Page struct {
	Items       []Notification
	Total       int
	UnreadCount int
	Limit       int
	Offset      int
}

type Summary struct {
	UnreadCount int
}

type ReadReceipt struct {
	NotificationID uuid.UUID
	ReadAt         time.Time
	AlreadyRead    bool
}

type ReadAllReceipt struct {
	UpdatedCount int
	ReadAt       time.Time
}

type ArchiveAllReceipt struct {
	UpdatedCount int
	ArchivedAt   time.Time
}

type CreateFeedbackInput struct {
	Title   string
	Content string
}

type FeedbackReceipt struct {
	FeedbackID uuid.UUID
	CreatedAt  time.Time
}
