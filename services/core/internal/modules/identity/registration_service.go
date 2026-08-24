package identity

import (
	"context"
	"crypto/sha256"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

var (
	registrationUsernamePattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,31}$`)
	legacyRegistrationInvitationToken = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type RegistrationRepository interface {
	PublicRegistrationPolicy(context.Context) (RegistrationPublicPolicy, error)
	PrepareRegistration(context.Context, PrepareRegistrationCommand) (RegistrationRecord, error)
	AttachRegistrationCredential(context.Context, uuid.UUID, uuid.UUID, time.Time) (RegistrationRecord, error)
	CompleteRegistration(context.Context, uuid.UUID, time.Time) (RegistrationRecord, error)
}

type RegistrationCredentialVault interface {
	ProvisionRegistration(context.Context, RegistrationInput) (uuid.UUID, error)
	ActivateRegistration(context.Context, uuid.UUID) (uuid.UUID, error)
}

type RegistrationPolicyAdministrator interface {
	Get(context.Context, authz.StaffActor) (RegistrationPolicy, error)
	Update(context.Context, authz.StaffActor, UpdateRegistrationPolicyInput) (RegistrationPolicy, error)
}

// RegistrationService orchestrates a recoverable saga across Core and Vault.
// Invariants are fail-closed: Vault starts provisional, Core starts pending,
// and only the final two idempotent steps make both halves login-eligible.
type RegistrationService struct {
	repository          RegistrationRepository
	vault               RegistrationCredentialVault
	policyAdministrator RegistrationPolicyAdministrator
	now                 func() time.Time
}

func NewRegistrationService(repository RegistrationRepository, vault RegistrationCredentialVault, now func() time.Time, policyAdministrators ...RegistrationPolicyAdministrator) (*RegistrationService, error) {
	if repository == nil || vault == nil {
		return nil, errors.New("registration repository and vault are required")
	}
	if len(policyAdministrators) > 1 || (len(policyAdministrators) == 1 && policyAdministrators[0] == nil) {
		return nil, errors.New("at most one valid registration policy administrator is allowed")
	}
	if now == nil {
		now = time.Now
	}
	var policyAdministrator RegistrationPolicyAdministrator
	if len(policyAdministrators) == 1 {
		policyAdministrator = policyAdministrators[0]
	}
	return &RegistrationService{
		repository: repository, vault: vault, policyAdministrator: policyAdministrator, now: now,
	}, nil
}

func (service *RegistrationService) PublicPolicy(ctx context.Context) (RegistrationPublicPolicy, error) {
	return service.repository.PublicRegistrationPolicy(ctx)
}

func (service *RegistrationService) Policy(ctx context.Context, actor authz.StaffActor) (RegistrationPolicy, error) {
	if service.policyAdministrator == nil {
		return RegistrationPolicy{}, ErrRegistrationServiceUnavailable
	}
	return service.policyAdministrator.Get(ctx, actor)
}

func (service *RegistrationService) UpdatePolicy(ctx context.Context, actor authz.StaffActor, input UpdateRegistrationPolicyInput) (RegistrationPolicy, error) {
	if service.policyAdministrator == nil {
		return RegistrationPolicy{}, ErrRegistrationServiceUnavailable
	}
	return service.policyAdministrator.Update(ctx, actor, input)
}

func (service *RegistrationService) Register(ctx context.Context, input RegistrationInput) (RegistrationResult, error) {
	normalized, invitationDigest, err := normalizeRegistrationInput(input)
	if err != nil {
		return RegistrationResult{}, err
	}
	now := service.now().UTC()
	record, err := service.repository.PrepareRegistration(ctx, PrepareRegistrationCommand{
		ID:               normalized.ID,
		UserID:           uuid.New(),
		Username:         normalized.Username,
		DisplayName:      normalized.DisplayName,
		EmailDomain:      registrationEmailDomain(normalized.Email),
		InvitationDigest: invitationDigest,
		OccurredAt:       now,
	})
	if err != nil {
		return RegistrationResult{}, err
	}

	// Always ask Vault to resume the provision, even for a completed Core row.
	// Vault's keyed request HMAC prevents the same idempotency key from being
	// replayed with a different email or password after a network timeout.
	credentialRef, err := service.vault.ProvisionRegistration(ctx, normalized)
	if err != nil {
		return RegistrationResult{}, err
	}
	record, err = service.repository.AttachRegistrationCredential(ctx, record.ID, credentialRef, now)
	if err != nil {
		return RegistrationResult{}, err
	}
	activatedRef, err := service.vault.ActivateRegistration(ctx, record.ID)
	if err != nil {
		return RegistrationResult{}, err
	}
	if activatedRef != credentialRef {
		return RegistrationResult{}, ErrRegistrationStateConflict
	}
	record, err = service.repository.CompleteRegistration(ctx, record.ID, now)
	if err != nil {
		return RegistrationResult{}, err
	}
	if record.CompletedAt == nil {
		return RegistrationResult{}, ErrRegistrationStateConflict
	}
	return RegistrationResult{
		UserID: record.UserID, Username: record.Username, DisplayName: record.DisplayName,
		RegistrationMode: record.Mode, EmailVerificationRequired: true,
		CompletedAt: *record.CompletedAt,
	}, nil
}

func registrationEmailDomain(email string) string {
	separator := strings.LastIndexByte(email, '@')
	if separator < 0 || separator == len(email)-1 {
		return ""
	}
	return email[separator+1:]
}

// registrationPolicyAllowsNewAccount applies only to a new Core reservation.
// A resumed saga retains the policy decision made when its reservation was
// first committed; Vault still rejects a replay that changes email/password.
func registrationPolicyAllowsNewAccount(policy RegistrationPolicy, username, emailDomain string) error {
	usernameRunes := utf8.RuneCountInString(username)
	if usernameRunes < policy.UsernameMinCharacters || usernameRunes > policy.UsernameMaxCharacters ||
		slices.Contains(policy.ReservedUsernames, username) {
		return ErrRegistrationUnavailable
	}
	domainListed := slices.Contains(policy.EmailDomains, emailDomain)
	switch policy.EmailDomainMode {
	case EmailDomainModeAny:
		return nil
	case EmailDomainModeAllowlist:
		if !domainListed {
			return ErrRegistrationUnavailable
		}
	case EmailDomainModeBlocklist:
		if domainListed {
			return ErrRegistrationUnavailable
		}
	default:
		return ErrRegistrationStateConflict
	}
	return nil
}

func normalizeRegistrationInput(input RegistrationInput) (RegistrationInput, []byte, error) {
	input.Username = strings.ToLower(strings.TrimSpace(input.Username))
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.InvitationToken = strings.TrimSpace(input.InvitationToken)
	if input.ID == uuid.Nil || !registrationUsernamePattern.MatchString(input.Username) ||
		utf8.RuneCountInString(input.DisplayName) < 1 || utf8.RuneCountInString(input.DisplayName) > 40 ||
		len(input.Password) < 12 || len(input.Password) > maxPasswordBytes {
		return RegistrationInput{}, nil, ErrInvalidInput
	}
	normalizedEmail, err := normalizeEmailAddress(input.Email)
	if err != nil || normalizedEmail != input.Email {
		return RegistrationInput{}, nil, ErrInvalidInput
	}
	var invitationDigest []byte
	if input.InvitationToken != "" {
		if len(input.InvitationToken) != 43 && !legacyRegistrationInvitationToken.MatchString(input.InvitationToken) {
			return RegistrationInput{}, nil, ErrInvalidInput
		}
		digest := sha256.Sum256([]byte(input.InvitationToken))
		invitationDigest = append([]byte(nil), digest[:]...)
	}
	return input, invitationDigest, nil
}

var _ interface {
	PublicPolicy(context.Context) (RegistrationPublicPolicy, error)
	Register(context.Context, RegistrationInput) (RegistrationResult, error)
	Policy(context.Context, authz.StaffActor) (RegistrationPolicy, error)
	UpdatePolicy(context.Context, authz.StaffActor, UpdateRegistrationPolicyInput) (RegistrationPolicy, error)
} = (*RegistrationService)(nil)
