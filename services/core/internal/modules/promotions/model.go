// Package promotions owns Core's staff-facing promotion control plane. It
// stores operator intent and delivery state, while Settlement remains the
// authority for immutable traffic accounting.
package promotions

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

var (
	ErrInput               = errors.New("promotion campaign input is invalid")
	ErrNotFound            = errors.New("promotion campaign target was not found")
	ErrTorrentUnavailable  = errors.New("promotion campaign target torrent is unavailable")
	ErrIdempotencyConflict = errors.New("promotion campaign id was reused")
	ErrScopeOverlap        = errors.New("promotion campaign overlaps the same scope")
	ErrProductDisabled     = errors.New("promotion product is disabled")
	ErrEmailUnverified     = errors.New("promotion product buyer email is unverified")
	ErrInsufficientBalance = errors.New("promotion product buyer balance is insufficient")
	ErrVersionConflict     = errors.New("promotion product policy version changed")
	ErrNoChange            = errors.New("promotion product policy did not change")
	ErrInvariant           = errors.New("promotion campaign invariant failed")
)

type Scope string

const (
	ScopeGlobal  Scope = "global"
	ScopeTorrent Scope = "torrent"
)

type Promotion string

const (
	PromotionFree                     Promotion = "free"
	PromotionDoubleUpload             Promotion = "double_upload"
	PromotionDoubleUploadFree         Promotion = "double_upload_free"
	PromotionHalfDownload             Promotion = "half_download"
	PromotionDoubleUploadHalfDownload Promotion = "double_upload_half_download"
	PromotionThirtyPercentDownload    Promotion = "thirty_percent_download"
)

type DeliveryState string

const (
	DeliveryPending   DeliveryState = "pending"
	DeliveryRetrying  DeliveryState = "retrying"
	DeliveryDelivered DeliveryState = "delivered"
)

type TimelineState string

const (
	TimelineScheduled TimelineState = "scheduled"
	TimelineActive    TimelineState = "active"
	TimelineExpired   TimelineState = "expired"
)

type CampaignSource string

const (
	CampaignSourceStaffSchedule  CampaignSource = "staff_schedule"
	CampaignSourceMemberPurchase CampaignSource = "member_purchase"
)

type Campaign struct {
	ID                  uuid.UUID
	Source              CampaignSource
	Scope               Scope
	TorrentID           *int64
	TorrentTitle        string
	Promotion           Promotion
	StartsAt            time.Time
	EndsAt              time.Time
	OverrideLowerScopes bool
	Reason              string
	ActorID             uuid.UUID
	CreatedAt           time.Time
	DeliveryState       DeliveryState
	DeliveryAttempts    int32
	LastDeliveryError   string
	DeliveredAt         *time.Time
	TimelineState       TimelineState
}

type ScheduleInput struct {
	CampaignID uuid.UUID
	Scope      Scope
	TorrentID  *int64
	Promotion  Promotion
	StartsAt   time.Time
	EndsAt     time.Time
	Reason     string
}

type ScheduleCommand struct {
	ScheduleInput
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
	CommandJSON   []byte
	CommandSHA256 [32]byte
}

type Page struct {
	Items []Campaign
	Total int64
}

type CampaignAuditInput struct {
	Campaign      Campaign
	Authorization authz.Decision
}

type CampaignEventBuilder interface {
	BuildPromotionCampaignEvent(CampaignAuditInput) (auditevent.Event, error)
}
