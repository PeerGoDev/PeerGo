package identity

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	DefaultInvitationHistoryLimit = 20
	MaxInvitationHistoryLimit     = 100
	MaxInvitationHistoryOffset    = 99_999
	invitationTokenDigestBytes    = 32
)

type InvitationStatus string

const (
	InvitationStatusAvailable InvitationStatus = "available"
	InvitationStatusClaimed   InvitationStatus = "claimed"
	InvitationStatusUsed      InvitationStatus = "used"
	InvitationStatusExpired   InvitationStatus = "expired"
	InvitationStatusRevoked   InvitationStatus = "revoked"
)

type InvitationRecordSource string

const (
	InvitationRecordMember       InvitationRecordSource = "member"
	InvitationRecordLegacyImport InvitationRecordSource = "legacy_import"
)

type InvitationEligibilityBlocker string

const (
	InvitationBlockerNone               InvitationEligibilityBlocker = "none"
	InvitationBlockerDisabled           InvitationEligibilityBlocker = "disabled"
	InvitationBlockerAccountUnavailable InvitationEligibilityBlocker = "account_unavailable"
	InvitationBlockerEmailUnverified    InvitationEligibilityBlocker = "email_unverified"
	InvitationBlockerAccountAge         InvitationEligibilityBlocker = "account_age"
	InvitationBlockerLevel              InvitationEligibilityBlocker = "level"
	InvitationBlockerQuotaExhausted     InvitationEligibilityBlocker = "quota_exhausted"
)

var (
	ErrInvitationInput                     = errors.New("invitation input is invalid")
	ErrInvitationDisabled                  = errors.New("member invitation issuance is disabled")
	ErrInvitationIneligible                = errors.New("member is not eligible to issue invitations")
	ErrInvitationQuota                     = errors.New("member invitation quota is exhausted")
	ErrInvitationNotFound                  = errors.New("member invitation was not found")
	ErrInvitationUnavailable               = errors.New("member invitation cannot be revoked")
	ErrInvitationEmailRegistered           = errors.New("invitation email already belongs to an account")
	ErrInvitationEmailDirectoryUnavailable = errors.New("invitation email directory is unavailable")
	ErrInvitationInvariant                 = errors.New("invitation projection violates persisted invariants")
)

// MemberInvitation never contains the bearer token or its digest. The raw
// token exists only in InvitationIssueResult immediately after a successful
// insertion and cannot be recovered by subsequent reads.
type MemberInvitation struct {
	ID              uuid.UUID
	Source          InvitationRecordSource
	Status          InvitationStatus
	EmailBound      bool
	InviteeUsername *string
	CreatedAt       time.Time
	ExpiresAt       time.Time
	ClaimedAt       *time.Time
	ConsumedAt      *time.Time
	RevokedAt       *time.Time
}

type InvitationEligibility struct {
	Enabled               bool
	Eligible              bool
	Blocker               InvitationEligibilityBlocker
	InviteValidDays       int
	MaxInvitesPerMember   int
	UsedInvites           int
	RemainingInvites      int
	MinimumAccountAgeDays int
	CurrentAccountAgeDays int
	MinimumLevel          int
	CurrentLevel          int
	EmailVerified         bool
}

type InvitationOverview struct {
	Eligibility InvitationEligibility
	Items       []MemberInvitation
	Network     InvitationNetwork
	Total       int
	Limit       int
	Offset      int
	ObservedAt  time.Time
}

type InvitationRelationshipSource string

const (
	InvitationRelationshipRegistration InvitationRelationshipSource = "registration"
	InvitationRelationshipLegacyImport InvitationRelationshipSource = "legacy_import"
)

type InvitedMember struct {
	NumericID                int64
	Username                 string
	DisplayName              string
	Source                   InvitationRelationshipSource
	EstablishedAt            time.Time
	LastActiveAt             *time.Time
	LatestRewardWindow       *time.Time
	CurrentSeedingCount      int32
	CurrentSeedingReward     int64
	CurrentContributionMilli int64
	HaremEligible            bool
}

type HistoricalInvitationReward struct {
	Amount         int64
	SourceRows     int64
	LastRewardedAt *time.Time
}

type HaremRewardPolicySummary struct {
	Revision         string
	Enabled          bool
	RewardBPS        int32
	Depth            int16
	MinimumSeedCount int32
	HourlyCap        int64
	ActivityDays     int32
	SettlementHours  int16
	EffectiveFrom    time.Time
}

type LiveHaremReward struct {
	Policy                     HaremRewardPolicySummary
	CurrentHourlyEstimateMilli int64
	AwardedAmount              int64
	SettlementCount            int64
	LastSettledAt              *time.Time
}

type InvitationNetwork struct {
	DirectMembers    []InvitedMember
	AncestorMembers  []InvitedMember
	DirectCount      int
	TotalDescendants int
	HaremReward      HistoricalInvitationReward
	InvitationReward HistoricalInvitationReward
	LiveHaremReward  LiveHaremReward
}

type InvitationIssueResult struct {
	Invitation MemberInvitation
	Token      string
}

type invitationIssuerSnapshot struct {
	MemberInvitesEnabled        bool
	InviteValidDays             int
	MaxInvitesPerMember         int
	MinimumInviteAccountAgeDays int
	MinimumInviteLevel          int
	Status                      string
	EmailVerified               bool
	CreatedAt                   time.Time
	CurrentLevel                int
	AccountRestricted           bool
	UsedInvites                 int
	RemainingInvites            int
}

type IssueInvitationCommand struct {
	ID               uuid.UUID
	UserID           uuid.UUID
	TokenSHA256      []byte
	EmailBindingHMAC []byte
	OccurredAt       time.Time
	Authorization    authz.Decision
}

type RevokeInvitationCommand struct {
	InvitationID  uuid.UUID
	UserID        uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}
