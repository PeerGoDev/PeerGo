package identity

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	minRegistrationPolicyReasonRunes = 5
	maxRegistrationPolicyReasonRunes = 500
	maxReservedUsernames             = 200
	maxEmailDomains                  = 100
	maxHumanVerificationSiteKeyBytes = 128
)

var emailDomainPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

type RegistrationPolicyAdministrationRepository interface {
	GetRegistrationPolicy(context.Context) (RegistrationPolicy, error)
	UpdateRegistrationPolicy(context.Context, UpdateRegistrationPolicyCommand) (RegistrationPolicy, error)
}

// RegistrationPolicyAdministrationService authorizes the staff credential,
// validates optimistic concurrency and delegates the atomic row/outbox write.
// It does not share a mutable DTO with catalog site presentation settings.
type RegistrationPolicyAdministrationService struct {
	repository                        RegistrationPolicyAdministrationRepository
	authorizer                        authz.Authorizer
	now                               func() time.Time
	humanVerificationSecretConfigured bool
}

type RegistrationPolicyAdministrationServiceConfig struct {
	HumanVerificationSecretConfigured bool
}

func NewRegistrationPolicyAdministrationService(repository RegistrationPolicyAdministrationRepository, authorizer authz.Authorizer, now func() time.Time, config ...RegistrationPolicyAdministrationServiceConfig) (*RegistrationPolicyAdministrationService, error) {
	if repository == nil || authorizer == nil {
		return nil, errors.New("registration policy administration dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	configured := false
	if len(config) > 1 {
		return nil, errors.New("at most one registration policy administration config is allowed")
	}
	if len(config) == 1 {
		configured = config[0].HumanVerificationSecretConfigured
	}
	return &RegistrationPolicyAdministrationService{
		repository: repository, authorizer: authorizer, now: now,
		humanVerificationSecretConfigured: configured,
	}, nil
}

func (service *RegistrationPolicyAdministrationService) Get(ctx context.Context, actor authz.StaffActor) (RegistrationPolicy, error) {
	now := service.now().UTC()
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionSiteRegistrationManageRead, authz.SiteScope(), now, "identity-registration-policy-administration"); err != nil {
		return RegistrationPolicy{}, err
	}
	policy, err := service.repository.GetRegistrationPolicy(ctx)
	if err != nil {
		return RegistrationPolicy{}, fmt.Errorf("get registration policy: %w", err)
	}
	return service.withRuntimeStatus(policy), nil
}

func (service *RegistrationPolicyAdministrationService) Update(ctx context.Context, actor authz.StaffActor, input UpdateRegistrationPolicyInput) (RegistrationPolicy, error) {
	normalized, err := normalizeRegistrationPolicyInput(input, service.humanVerificationSecretConfigured)
	if err != nil {
		return RegistrationPolicy{}, err
	}
	now := service.now().UTC()
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionSiteRegistrationUpdate, authz.SiteScope(), now, "identity-registration-policy-administration")
	if err != nil {
		return RegistrationPolicy{}, err
	}
	policy, err := service.repository.UpdateRegistrationPolicy(ctx, UpdateRegistrationPolicyCommand{
		UpdateRegistrationPolicyInput: normalized,
		ActorID:                       actor.Subject.ID,
		OccurredAt:                    now,
		Authorization:                 decision,
	})
	if err != nil {
		return RegistrationPolicy{}, fmt.Errorf("update registration policy: %w", err)
	}
	return service.withRuntimeStatus(policy), nil
}

func (service *RegistrationPolicyAdministrationService) withRuntimeStatus(policy RegistrationPolicy) RegistrationPolicy {
	policy.HumanVerificationSecretConfigured = service.humanVerificationSecretConfigured
	return policy
}

func normalizeRegistrationPolicyInput(input UpdateRegistrationPolicyInput, humanVerificationSecretConfigured ...bool) (UpdateRegistrationPolicyInput, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	input.HumanVerificationSiteKey = strings.TrimSpace(input.HumanVerificationSiteKey)
	if input.HumanVerificationProvider == "" {
		input.HumanVerificationProvider = HumanVerificationProviderDisabled
	}
	input.ReservedUsernames = normalizePolicyEntries(input.ReservedUsernames)
	input.EmailDomains = normalizePolicyEntries(input.EmailDomains)
	secretConfigured := false
	if len(humanVerificationSecretConfigured) > 1 {
		return UpdateRegistrationPolicyInput{}, ErrRegistrationPolicyInput
	}
	if len(humanVerificationSecretConfigured) == 1 {
		secretConfigured = humanVerificationSecretConfigured[0]
	}
	reasonRunes := utf8.RuneCountInString(input.Reason)
	if _, err := validRegistrationMode(string(input.Mode)); err != nil ||
		input.InviteValidDays < 1 || input.InviteValidDays > 90 ||
		input.MaxInvitesPerMember < 0 || input.MaxInvitesPerMember > 100 ||
		input.MinimumInviteAccountAgeDays < 0 || input.MinimumInviteAccountAgeDays > 3650 ||
		input.MinimumInviteLevel < 1 || input.MinimumInviteLevel > 99 ||
		input.UsernameMinCharacters < 3 || input.UsernameMinCharacters > 32 ||
		input.UsernameMaxCharacters < input.UsernameMinCharacters || input.UsernameMaxCharacters > 32 ||
		len(input.ReservedUsernames) > maxReservedUsernames ||
		!validReservedUsernames(input.ReservedUsernames) ||
		!validEmailDomainMode(input.EmailDomainMode) || len(input.EmailDomains) > maxEmailDomains ||
		!validEmailDomains(input.EmailDomains) ||
		input.EmailDomainMode != EmailDomainModeAny && len(input.EmailDomains) == 0 ||
		input.SessionValidHours < 1 || input.SessionValidHours > 720 ||
		input.RememberSessionValidHours < 24 || input.RememberSessionValidHours > 2160 ||
		input.SessionValidHours > input.RememberSessionValidHours ||
		!validHumanVerificationPolicy(
			input.HumanVerificationProvider,
			input.HumanVerificationSiteKey,
			input.HumanVerificationRegistrationEnabled,
			input.HumanVerificationLoginEnabled,
			input.HumanVerificationPasswordRecoveryEnabled,
			secretConfigured,
		) ||
		!utf8.ValidString(input.Reason) || reasonRunes < minRegistrationPolicyReasonRunes ||
		reasonRunes > maxRegistrationPolicyReasonRunes || input.ExpectedVersion < 1 {
		return UpdateRegistrationPolicyInput{}, ErrRegistrationPolicyInput
	}
	return input, nil
}

func validHumanVerificationPolicy(provider HumanVerificationProvider, siteKey string, registrationEnabled, loginEnabled, passwordRecoveryEnabled, secretConfigured bool) bool {
	if !validStoredHumanVerificationPolicy(provider, siteKey, registrationEnabled, loginEnabled, passwordRecoveryEnabled) {
		return false
	}
	return provider == HumanVerificationProviderDisabled || secretConfigured
}

func validStoredHumanVerificationPolicy(provider HumanVerificationProvider, siteKey string, registrationEnabled, loginEnabled, passwordRecoveryEnabled bool) bool {
	switch provider {
	case HumanVerificationProviderDisabled:
		return siteKey == "" && !registrationEnabled && !loginEnabled && !passwordRecoveryEnabled
	case HumanVerificationProviderTurnstile:
		return len(siteKey) > 0 && len(siteKey) <= maxHumanVerificationSiteKeyBytes &&
			!strings.ContainsAny(siteKey, " \t\r\n") &&
			(registrationEnabled || loginEnabled || passwordRecoveryEnabled)
	default:
		return false
	}
}

func normalizePolicyEntries(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

func validReservedUsernames(values []string) bool {
	for _, value := range values {
		if !registrationUsernamePattern.MatchString(value) {
			return false
		}
	}
	return true
}

func validEmailDomains(values []string) bool {
	for _, value := range values {
		if len(value) > 253 || !emailDomainPattern.MatchString(value) {
			return false
		}
	}
	return true
}

func validEmailDomainMode(value EmailDomainMode) bool {
	switch value {
	case EmailDomainModeAny, EmailDomainModeAllowlist, EmailDomainModeBlocklist:
		return true
	default:
		return false
	}
}
