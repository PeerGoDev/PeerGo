package identity

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

var (
	ErrRegistrationPolicyInput           = errors.New("registration policy input is invalid")
	ErrRegistrationPolicyNotFound        = errors.New("registration policy was not found")
	ErrRegistrationPolicyVersionConflict = errors.New("registration policy version changed")
)

const RegistrationPolicySettingsSection = "registration-policy"

type EmailDomainMode string

const (
	EmailDomainModeAny       EmailDomainMode = "any"
	EmailDomainModeAllowlist EmailDomainMode = "allowlist"
	EmailDomainModeBlocklist EmailDomainMode = "blocklist"
)

type HumanVerificationProvider string

const (
	HumanVerificationProviderDisabled  HumanVerificationProvider = "disabled"
	HumanVerificationProviderTurnstile HumanVerificationProvider = "turnstile"
)

// RegistrationPolicy is the complete staff-visible admission policy. It is
// intentionally separate from catalog display settings so public registration
// and staff administration always read the same identity-owned source.
type RegistrationPolicy struct {
	Mode                                     RegistrationMode
	MemberInvitesEnabled                     bool
	InviteValidDays                          int
	MaxInvitesPerMember                      int
	MinimumInviteAccountAgeDays              int
	MinimumInviteLevel                       int
	UsernameMinCharacters                    int
	UsernameMaxCharacters                    int
	ReservedUsernames                        []string
	EmailDomainMode                          EmailDomainMode
	EmailDomains                             []string
	SessionValidHours                        int
	RememberSessionValidHours                int
	HumanVerificationProvider                HumanVerificationProvider
	HumanVerificationSiteKey                 string
	HumanVerificationRegistrationEnabled     bool
	HumanVerificationLoginEnabled            bool
	HumanVerificationPasswordRecoveryEnabled bool
	HumanVerificationSecretConfigured        bool
	Version                                  int64
	UpdatedAt                                time.Time
}

type UpdateRegistrationPolicyInput struct {
	Mode                                     RegistrationMode
	MemberInvitesEnabled                     bool
	InviteValidDays                          int
	MaxInvitesPerMember                      int
	MinimumInviteAccountAgeDays              int
	MinimumInviteLevel                       int
	UsernameMinCharacters                    int
	UsernameMaxCharacters                    int
	ReservedUsernames                        []string
	EmailDomainMode                          EmailDomainMode
	EmailDomains                             []string
	SessionValidHours                        int
	RememberSessionValidHours                int
	HumanVerificationProvider                HumanVerificationProvider
	HumanVerificationSiteKey                 string
	HumanVerificationRegistrationEnabled     bool
	HumanVerificationLoginEnabled            bool
	HumanVerificationPasswordRecoveryEnabled bool
	ExpectedVersion                          int64
	Reason                                   string
}

type UpdateRegistrationPolicyCommand struct {
	UpdateRegistrationPolicyInput
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

// RegistrationPolicyAuditState is hashed before it leaves Core. The clear
// administrative reason remains only in the owning request path.
type RegistrationPolicyAuditState struct {
	Mode                                     RegistrationMode          `json:"mode"`
	MemberInvitesEnabled                     bool                      `json:"member_invites_enabled"`
	InviteValidDays                          int                       `json:"invite_valid_days"`
	MaxInvitesPerMember                      int                       `json:"max_invites_per_member"`
	MinimumInviteAccountAgeDays              int                       `json:"minimum_invite_account_age_days"`
	MinimumInviteLevel                       int                       `json:"minimum_invite_level"`
	UsernameMinCharacters                    int                       `json:"username_min_characters"`
	UsernameMaxCharacters                    int                       `json:"username_max_characters"`
	ReservedUsernames                        []string                  `json:"reserved_usernames"`
	EmailDomainMode                          EmailDomainMode           `json:"email_domain_mode"`
	EmailDomains                             []string                  `json:"email_domains"`
	SessionValidHours                        int                       `json:"session_valid_hours"`
	RememberSessionValidHours                int                       `json:"remember_session_valid_hours"`
	HumanVerificationProvider                HumanVerificationProvider `json:"human_verification_provider"`
	HumanVerificationSiteKey                 string                    `json:"human_verification_site_key"`
	HumanVerificationRegistrationEnabled     bool                      `json:"human_verification_registration_enabled"`
	HumanVerificationLoginEnabled            bool                      `json:"human_verification_login_enabled"`
	HumanVerificationPasswordRecoveryEnabled bool                      `json:"human_verification_password_recovery_enabled"`
	Version                                  int64                     `json:"version"`
}

type RegistrationPolicyAuditInput struct {
	OccurredAt      time.Time
	ActorID         uuid.UUID
	Reason          string
	ExpectedVersion int64
	Authorization   authz.Decision
	Before          RegistrationPolicyAuditState
	After           RegistrationPolicyAuditState
}

type RegistrationPolicyEventBuilder interface {
	BuildRegistrationPolicyEvent(RegistrationPolicyAuditInput) (auditevent.Event, error)
}
