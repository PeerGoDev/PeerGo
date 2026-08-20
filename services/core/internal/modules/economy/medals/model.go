// Package medals owns medal definitions, member holdings and their effect on
// the append-only user reward-benefit timeline.
package medals

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

var (
	ErrInput               = errors.New("medal definition input is invalid")
	ErrNotFound            = errors.New("medal definition was not found")
	ErrVersionConflict     = errors.New("medal definition version changed")
	ErrSettingsConflict    = errors.New("medal settings version changed")
	ErrDisabled            = errors.New("medal system is disabled")
	ErrNotPurchasable      = errors.New("medal is not available for purchase")
	ErrAlreadyOwned        = errors.New("medal is already owned")
	ErrWearLimit           = errors.New("medal wear limit reached")
	ErrWorkgroupManaged    = errors.New("workgroup medal is automatically active")
	ErrExpired             = errors.New("medal holding expired")
	ErrNoChange            = errors.New("medal state did not change")
	ErrInsufficientMagic   = errors.New("magic balance is insufficient")
	ErrIdempotencyConflict = errors.New("medal purchase idempotency key was reused")
)

type AcquisitionMethod string

const (
	AcquisitionPurchase  AcquisitionMethod = "purchase"
	AcquisitionGrant     AcquisitionMethod = "grant"
	AcquisitionSponsor   AcquisitionMethod = "sponsor"
	AcquisitionWorkgroup AcquisitionMethod = "workgroup"
	AcquisitionDeveloper AcquisitionMethod = "developer"
)

type Settings struct {
	Enabled                    bool
	MaximumWearCount           int64
	MaximumUploadBonusBPS      int64
	MaximumDownloadDiscountBPS int64
	MaximumMagicBonusBPS       int64
	MaximumInviteBonus         int64
	ConditionCheckDay          int64
	ConditionWarningDays       int64
	Version                    int64
	UpdatedAt                  time.Time
}

// SettingsInput contains the site-wide controls currently enforced by PeerGo.
// The two legacy condition-scan values stay migration-only until their opaque
// JSON rules are replaced by typed, verifiable tasks.
type SettingsInput struct {
	Enabled                    bool
	MaximumWearCount           int64
	MaximumUploadBonusBPS      int64
	MaximumDownloadDiscountBPS int64
	MaximumMagicBonusBPS       int64
	MaximumInviteBonus         int64
	ExpectedVersion            int64
	Reason                     string
}

type UpdateSettingsCommand struct {
	SettingsInput
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

// Definition is the bounded staff projection. Counts help an administrator
// understand the impact of an edit without exposing individual owners.
type Definition struct {
	ID                  int64
	Name                string
	Description         *string
	ImageLargePath      *string
	ImageSmallPath      *string
	AcquisitionMethod   AcquisitionMethod
	Price               int64
	DurationDays        int64
	DisplayOnPage       bool
	Priority            int64
	UploadBonusBPS      int64
	DownloadDiscountBPS int64
	MagicBonusBPS       int64
	InviteBonus         int64
	IsWorkgroup         bool
	PoolEligible        bool
	PeriodicRewardMagic int64
	RewardCycle         *string
	SaleBeginAt         *time.Time
	SaleEndAt           *time.Time
	Inventory           *int64
	ConditionsCount     int64
	PrivilegesCount     int64
	Version             int64
	HolderCount         int64
	ActiveHolderCount   int64
	WearingCount        int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type Overview struct {
	Settings Settings
	Items    []Definition
}

type DefinitionInput struct {
	Name                string
	Description         *string
	ImageLargePath      *string
	ImageSmallPath      *string
	AcquisitionMethod   AcquisitionMethod
	Price               int64
	DurationDays        int64
	DisplayOnPage       bool
	Priority            int64
	UploadBonusBPS      int64
	DownloadDiscountBPS int64
	MagicBonusBPS       int64
	InviteBonus         int64
	PoolEligible        bool
	PeriodicRewardMagic int64
	RewardCycle         *string
	SaleBeginAt         *time.Time
	SaleEndAt           *time.Time
	Inventory           *int64
	Reason              string
}

type CreateCommand struct {
	DefinitionInput
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type UpdateCommand struct {
	ID int64
	DefinitionInput
	ExpectedVersion int64
	ActorID         uuid.UUID
	OccurredAt      time.Time
	Authorization   authz.Decision
}

type BenefitSummary struct {
	UploadBonusBPS      int64
	DownloadDiscountBPS int64
	MagicBonusBPS       int64
	InviteBonus         int64
}

type Holding struct {
	ID         int64
	State      string
	Priority   int64
	ExpiresAt  *time.Time
	AcquiredAt time.Time
	Version    int64
}

// MemberMedal deliberately omits site-wide holder counts from the member
// surface while preserving every field needed by the Rousi-compatible cards.
type MemberMedal struct {
	ID                        int64
	Name                      string
	Description               *string
	ImageLargePath            *string
	ImageSmallPath            *string
	AcquisitionMethod         AcquisitionMethod
	Price                     int64
	DurationDays              int64
	UploadBonusBPS            int64
	DownloadDiscountBPS       int64
	MagicBonusBPS             int64
	InviteBonus               int64
	IsWorkgroup               bool
	SaleBeginAt               *time.Time
	SaleEndAt                 *time.Time
	Inventory                 *int64
	Holding                   *Holding
	Purchasable               bool
	PurchaseUnavailableReason *string
}

type MemberOverview struct {
	Settings     Settings
	MagicBalance int64
	Benefits     BenefitSummary
	OwnedCount   int64
	WearingCount int64
	ShopCount    int64
	Items        []MemberMedal
}

type PurchaseCommand struct {
	RequestID uuid.UUID
	UserID    uuid.UUID
	MedalID   int64
	Now       time.Time
}

type PurchaseReceipt struct {
	ID                 uuid.UUID
	RequestID          uuid.UUID
	MedalID            int64
	UserMedalID        int64
	Price              int64
	BalanceAfter       int64
	MagicTransactionID *uuid.UUID
	PurchasedAt        time.Time
	Replayed           bool
}

type WearCommand struct {
	UserID          uuid.UUID
	MedalID         int64
	ExpectedVersion int64
	Wearing         bool
	Now             time.Time
}

type PriorityDirection string

const (
	PriorityUp   PriorityDirection = "up"
	PriorityDown PriorityDirection = "down"
)

type PriorityCommand struct {
	UserID          uuid.UUID
	MedalID         int64
	ExpectedVersion int64
	Direction       PriorityDirection
	Now             time.Time
}
