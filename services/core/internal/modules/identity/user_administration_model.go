package identity

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

var (
	ErrUserAdministrationInput           = errors.New("user administration input is invalid")
	ErrManagedUserNotFound               = errors.New("managed user was not found")
	ErrManagedUserNotActive              = errors.New("managed user is not active")
	ErrManagedUserVersionConflict        = errors.New("managed user administration version changed")
	ErrAccountRestrictionAlreadyActive   = errors.New("an overlapping account restriction is already active")
	ErrAccountRestrictionNotActive       = errors.New("account restriction is not active")
	ErrAccountRestrictionVersionConflict = errors.New("account restriction version changed")
	ErrAccountRestrictionSelfTarget      = errors.New("staff actor cannot target their own account restriction")
	ErrManualDownloadRestrictionActive   = errors.New("manual download restriction is already active")
	ErrManualDownloadRestrictionInactive = errors.New("manual download restriction is not active")
	ErrManualDownloadRestrictionConflict = errors.New("manual download restriction version changed")
	ErrVIPStateConflict                  = errors.New("VIP state version changed")
	ErrVIPAlreadyActive                  = errors.New("VIP is already active with the requested expiry")
	ErrVIPNotActive                      = errors.New("VIP is not active")
	ErrManagedUserContactUnavailable     = errors.New("managed user contact directory is unavailable")
	ErrManagedUserCredentialUnavailable  = errors.New("managed user credential lifecycle is unavailable")
	ErrManagedUserNotDisabled            = errors.New("managed user is not disabled")
)

type AccountStatus string

const (
	AccountStatusActive   AccountStatus = "active"
	AccountStatusDisabled AccountStatus = "disabled"
	AccountStatusPending  AccountStatus = "pending"
)

type ManagedUserDirectoryFilter string

const (
	ManagedUserFilterActive             ManagedUserDirectoryFilter = "active"
	ManagedUserFilterBanned             ManagedUserDirectoryFilter = "banned"
	ManagedUserFilterPending            ManagedUserDirectoryFilter = "pending"
	ManagedUserFilterVIP                ManagedUserDirectoryFilter = "vip"
	ManagedUserFilterDownloadRestricted ManagedUserDirectoryFilter = "download_restricted"
	ManagedUserFilterUnverified         ManagedUserDirectoryFilter = "unverified"
)

type AccountRestrictionKind string

const AccountRestrictionAccountAccess AccountRestrictionKind = "account_access"

type AccountRestrictionReasonCode string

const (
	AccountRestrictionReasonManualReview     AccountRestrictionReasonCode = "manual_review"
	AccountRestrictionReasonSecurityIncident AccountRestrictionReasonCode = "security_incident"
)

type AccountRestrictionRevocationReasonCode string

const (
	AccountRestrictionRevocationReviewCompleted  AccountRestrictionRevocationReasonCode = "review_completed"
	AccountRestrictionRevocationNoLongerRequired AccountRestrictionRevocationReasonCode = "restriction_no_longer_needed"
)

type AccountRestrictionTransition string

const (
	AccountRestrictionTransitionCreated AccountRestrictionTransition = "created"
	AccountRestrictionTransitionRevoked AccountRestrictionTransition = "revoked"
)

// ManagedUserSummary contains the operational fields needed by the authorized
// user directory. credentialRef stays private to the use case and is used only
// to request the email from Vault after Core has authorized the staff action;
// it never reaches the public transport model.
type ManagedUserSummary struct {
	ID                     uuid.UUID
	NumericID              int64
	credentialRef          uuid.UUID
	Username               string
	DisplayName            string
	Email                  string
	Status                 AccountStatus
	EmailVerified          bool
	Banned                 bool
	DownloadRestricted     bool
	VIPEnabled             bool
	VIPActive              bool
	VIPUntil               *time.Time
	Version                int64
	ActiveRestrictionCount int64
	UploadedBytes          int64
	DownloadedBytes        int64
	MagicBalance           int64
	Level                  int32
	RoleNames              []string
	LastActiveAt           *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type CurrentAccountRestriction struct {
	ID            uuid.UUID
	Kind          AccountRestrictionKind
	ReasonCode    string
	ReasonSummary string
	StartsAt      time.Time
	ExpiresAt     time.Time
	Version       int64
}

type ManagedUserDetail struct {
	ManagedUserSummary
	ActiveRestrictions               []CurrentAccountRestriction
	ManualDownloadRestriction        ManualDownloadRestrictionState
	ManualDownloadRestrictionHistory []ManualDownloadRestrictionTransition
	VIPState                         VIPState
	VIPHistory                       []VIPTransition
}

type VIPTransitionKind string

const (
	VIPTransitionGranted VIPTransitionKind = "granted"
	VIPTransitionRenewed VIPTransitionKind = "renewed"
	VIPTransitionRevoked VIPTransitionKind = "revoked"
)

type VIPTransitionOrigin string

const (
	VIPTransitionOriginLegacyMigration VIPTransitionOrigin = "legacy_migration"
	VIPTransitionOriginSystemBackfill  VIPTransitionOrigin = "system_backfill"
	VIPTransitionOriginStaff           VIPTransitionOrigin = "staff"
)

// VIPState exposes the optimistic-concurrency version of the shared account
// entitlement projection. The same version also advances when the independent
// manual download restriction changes, forcing staff to refresh stale forms.
type VIPState struct {
	Enabled bool
	Active  bool
	Until   *time.Time
	Version int64
}

type VIPTransition struct {
	Transition     VIPTransitionKind
	Origin         VIPTransitionOrigin
	ReasonSummary  string
	Enabled        bool
	Until          *time.Time
	StateVersion   int64
	OccurredAt     time.Time
	ActorNumericID *int64
	ActorUsername  *string
}

type ManualDownloadRestrictionOrigin string

const (
	ManualDownloadRestrictionOriginLegacyMigration ManualDownloadRestrictionOrigin = "legacy_migration"
	ManualDownloadRestrictionOriginSystemBackfill  ManualDownloadRestrictionOrigin = "system_backfill"
	ManualDownloadRestrictionOriginStaff           ManualDownloadRestrictionOrigin = "staff"
	ManualDownloadRestrictionOriginAppeal          ManualDownloadRestrictionOrigin = "appeal"
)

type ManualDownloadRestrictionTransitionKind string

const (
	ManualDownloadRestrictionTransitionRestricted ManualDownloadRestrictionTransitionKind = "restricted"
	ManualDownloadRestrictionTransitionUpdated    ManualDownloadRestrictionTransitionKind = "updated"
	ManualDownloadRestrictionTransitionRevoked    ManualDownloadRestrictionTransitionKind = "revoked"
)

type ManualDownloadRestrictionReasonCode string

const (
	ManualDownloadRestrictionReasonManualReview    ManualDownloadRestrictionReasonCode = "manual_review"
	ManualDownloadRestrictionReasonPolicyViolation ManualDownloadRestrictionReasonCode = "policy_violation"
	ManualDownloadRestrictionReasonAbusePrevention ManualDownloadRestrictionReasonCode = "abuse_prevention"
)

type ManualDownloadRestrictionRevocationReasonCode string

const (
	ManualDownloadRestrictionRevocationReviewCompleted  ManualDownloadRestrictionRevocationReasonCode = "review_completed"
	ManualDownloadRestrictionRevocationNoLongerRequired ManualDownloadRestrictionRevocationReasonCode = "restriction_no_longer_needed"
)

// ManualDownloadRestrictionState is only the legacy/manual source. The
// aggregate ManagedUserSummary.DownloadRestricted may still be true because a
// ratio-watch or H&R source is active after this state has been revoked.
type ManualDownloadRestrictionState struct {
	Active        bool
	Version       int64
	Origin        *ManualDownloadRestrictionOrigin
	ReasonCode    *string
	ReasonSummary *string
	StartedAt     *time.Time
}

type ManualDownloadRestrictionTransition struct {
	Transition     ManualDownloadRestrictionTransitionKind
	Origin         ManualDownloadRestrictionOrigin
	ReasonCode     string
	ReasonSummary  string
	StateVersion   int64
	OccurredAt     time.Time
	ActorNumericID *int64
	ActorUsername  *string
}

type ManagedUserPage struct {
	Items    []ManagedUserSummary
	Total    int64
	Page     int
	PageSize int
	Summary  ManagedUserDirectorySummary
}

type ManagedUserDirectorySummary struct {
	Total              int64
	Active             int64
	Banned             int64
	VIP                int64
	DownloadRestricted int64
	Unverified         int64
}

type ListManagedUsersInput struct {
	Query    string
	Filter   ManagedUserDirectoryFilter
	Page     int
	PageSize int
}

type ManagedUserListQuery struct {
	Query    string
	Filter   ManagedUserDirectoryFilter
	Page     int
	PageSize int
	Offset   int
	AsOf     time.Time
}

type CreateAccountRestrictionInput struct {
	UserID              uuid.UUID
	ReasonCode          AccountRestrictionReasonCode
	Reason              string
	DurationHours       int
	ExpectedUserVersion int64
}

type RevokeAccountRestrictionInput struct {
	UserID                     uuid.UUID
	RestrictionID              uuid.UUID
	ReasonCode                 AccountRestrictionRevocationReasonCode
	Reason                     string
	ExpectedUserVersion        int64
	ExpectedRestrictionVersion int64
}

type CreateAccountRestrictionCommand struct {
	CreateAccountRestrictionInput
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type RevokeAccountRestrictionCommand struct {
	RevokeAccountRestrictionInput
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type CreateManualDownloadRestrictionInput struct {
	UserID               uuid.UUID
	ReasonCode           ManualDownloadRestrictionReasonCode
	Reason               string
	ExpectedUserVersion  int64
	ExpectedStateVersion int64
}

type UpdateManualDownloadRestrictionInput struct {
	UserID               uuid.UUID
	ReasonCode           ManualDownloadRestrictionReasonCode
	Reason               string
	ExpectedUserVersion  int64
	ExpectedStateVersion int64
}

type RevokeManualDownloadRestrictionInput struct {
	UserID               uuid.UUID
	ReasonCode           ManualDownloadRestrictionRevocationReasonCode
	Reason               string
	ExpectedUserVersion  int64
	ExpectedStateVersion int64
}

type ManualDownloadRestrictionCommand struct {
	UserID               uuid.UUID
	ReasonCode           string
	Reason               string
	ExpectedUserVersion  int64
	ExpectedStateVersion int64
	ActorID              uuid.UUID
	OccurredAt           time.Time
	Authorization        authz.Decision
}

type ChangeVIPInput struct {
	UserID               uuid.UUID
	Enabled              bool
	DurationDays         *int
	Reason               string
	ExpectedUserVersion  int64
	ExpectedStateVersion int64
}

type ChangeVIPCommand struct {
	ChangeVIPInput
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type ReactivateManagedUserInput struct {
	ReactivationID      uuid.UUID
	UserID              uuid.UUID
	Reason              string
	ExpectedUserVersion int64
}

type ReactivateManagedUserCommand struct {
	ReactivateManagedUserInput
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type ManagedUserReactivationPreflight struct {
	CredentialRef uuid.UUID
	Status        AccountStatus
	Version       int64
}

// AccountRestrictionAuditState is the canonical security state hashed into
// immutable evidence. Human reasons remain in Core and never leave as clear
// text; timestamps are included because they define the exact access window.
type AccountRestrictionAuditState struct {
	RestrictionID             uuid.UUID  `json:"restriction_id"`
	Kind                      string     `json:"kind"`
	ReasonCode                string     `json:"reason_code"`
	ReasonSummary             string     `json:"reason_summary"`
	StartsAt                  time.Time  `json:"starts_at"`
	ExpiresAt                 time.Time  `json:"expires_at"`
	RevokedAt                 *time.Time `json:"revoked_at,omitempty"`
	RevocationReasonCode      string     `json:"revocation_reason_code,omitempty"`
	RevocationReason          string     `json:"revocation_reason,omitempty"`
	RestrictionVersion        int64      `json:"restriction_version"`
	UserAdministrationVersion int64      `json:"user_administration_version"`
}

type AccountRestrictionAuditInput struct {
	Transition                 AccountRestrictionTransition
	OccurredAt                 time.Time
	ActorID                    uuid.UUID
	TargetUserID               uuid.UUID
	RestrictionID              uuid.UUID
	CommandReasonCode          string
	Reason                     string
	ExpectedUserVersion        int64
	ExpectedRestrictionVersion int64
	Authorization              authz.Decision
	Before                     *AccountRestrictionAuditState
	After                      AccountRestrictionAuditState
}

// AccountRestrictionEventBuilder is implemented by audit. Identity owns the
// transaction and state semantics; audit owns pseudonymization and wire JSON.
type AccountRestrictionEventBuilder interface {
	BuildAccountRestrictionEvent(AccountRestrictionAuditInput) (auditevent.Event, error)
}
