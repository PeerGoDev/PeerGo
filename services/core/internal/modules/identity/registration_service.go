package identity

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
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

const (
	registrationCompensationTimeout = 5 * time.Second
	registrationProvisionTimeout    = 15 * time.Second
	registrationFinalizationTimeout = 10 * time.Second
	// Vault keeps provisional registration credentials for 24 hours. Core waits
	// an additional hour before releasing a reservation whose credential was
	// never attached, so the two stores cannot disagree at the expiry boundary.
	registrationStaleReservationAge = 25 * time.Hour
	registrationRecoveryBatchSize   = 100
)

type RegistrationRepository interface {
	PublicRegistrationPolicy(context.Context) (RegistrationPublicPolicy, error)
	PrepareRegistration(context.Context, PrepareRegistrationCommand) (RegistrationRecord, error)
	CancelRegistration(context.Context, uuid.UUID) error
	AttachRegistrationCredential(context.Context, uuid.UUID, uuid.UUID, time.Time) (RegistrationRecord, error)
	CompleteRegistration(context.Context, uuid.UUID, time.Time) (RegistrationRecord, error)
	ReleaseStaleRegistrationReservations(context.Context, time.Time, int32) (int64, error)
	ListIncompleteRegistrations(context.Context, int32) ([]RegistrationRecord, error)
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

type RegistrationRecoveryResult struct {
	ReleasedReservations   int64
	CompletedRegistrations int
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
	normalized, invitationDigest, invitationEmailBinding, err := normalizeRegistrationInput(input)
	if err != nil {
		return RegistrationResult{}, err
	}
	now := service.now().UTC()
	record, err := service.repository.PrepareRegistration(ctx, PrepareRegistrationCommand{
		ID:                     normalized.ID,
		UserID:                 uuid.New(),
		Username:               normalized.Username,
		DisplayName:            normalized.DisplayName,
		EmailDomain:            registrationEmailDomain(normalized.Email),
		InvitationDigest:       invitationDigest,
		InvitationEmailBinding: invitationEmailBinding,
		OccurredAt:             now,
	})
	if err != nil {
		return RegistrationResult{}, err
	}

	credentialRef := uuid.Nil
	if record.CredentialRef != nil {
		credentialRef = *record.CredentialRef
	}

	// A credential_provisioned row is already bound to the one exact Vault
	// request that created it. Re-hashing the password on every retry only makes
	// the recovery path slow enough for a browser timeout to strand the Core
	// projection again. A recovery discovered under a new browser idempotency
	// key is the exception: ask Vault once with the original registration ID so
	// its keyed request HMAC still proves that all sensitive fields are equal.
	if credentialRef == uuid.Nil || record.ID != normalized.ID {
		vaultInput := normalized
		vaultInput.ID = record.ID
		// The Core reservation is already durable. Keep the bounded Vault call
		// alive if the browser closes so a successful provision is never left
		// behind merely because the HTTP request context was canceled.
		provisionCtx, cancelProvision := context.WithTimeout(context.WithoutCancel(ctx), registrationProvisionTimeout)
		credentialRef, err = service.vault.ProvisionRegistration(provisionCtx, vaultInput)
		cancelProvision()
		if err != nil {
			// No Core identity exists yet while the record is still reserved. Release
			// that reservation for every failed Vault provision, including timeouts
			// and internal failures, so a browser refresh cannot strand the username
			// or invitation. Advanced rows remain recoverable through the same key.
			if record.State == RegistrationStateReserved && record.CredentialRef == nil {
				if cancelErr := service.cancelPreparedRegistration(ctx, record.ID); cancelErr != nil {
					return RegistrationResult{}, ErrRegistrationStateConflict
				}
			}
			return RegistrationResult{}, err
		}
		if record.CredentialRef != nil && credentialRef != *record.CredentialRef {
			return RegistrationResult{}, ErrRegistrationStateConflict
		}
	}

	// Once Vault has durably provisioned a credential, finish both remaining
	// halves on a short context detached from the client connection. The caller
	// may close the page after Vault activates; Core must still make the same
	// account active instead of leaving a permanent pending user behind.
	finalizationCtx, cancelFinalization := context.WithTimeout(context.WithoutCancel(ctx), registrationFinalizationTimeout)
	defer cancelFinalization()
	record, err = service.repository.AttachRegistrationCredential(finalizationCtx, record.ID, credentialRef, now)
	if err != nil {
		return RegistrationResult{}, err
	}
	activatedRef, err := service.vault.ActivateRegistration(finalizationCtx, record.ID)
	if err != nil {
		return RegistrationResult{}, err
	}
	if activatedRef != credentialRef {
		return RegistrationResult{}, ErrRegistrationStateConflict
	}
	record, err = service.repository.CompleteRegistration(finalizationCtx, record.ID, now)
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

// RecoverIncompleteRegistrations resumes durable registrations after a Core
// restart or an HTTP client disconnect. credential_provisioned means Vault
// already accepted the exact username, email and password and Core already
// persisted the pending identity, so activating and completing it is the only
// safe forward transition. Old reserved rows never crossed the Core identity
// boundary and can be released after Vault's provisional TTL has elapsed.
func (service *RegistrationService) RecoverIncompleteRegistrations(ctx context.Context) (RegistrationRecoveryResult, error) {
	now := service.now().UTC()
	released, err := service.repository.ReleaseStaleRegistrationReservations(
		ctx,
		now.Add(-registrationStaleReservationAge),
		registrationRecoveryBatchSize,
	)
	if err != nil {
		return RegistrationRecoveryResult{}, fmt.Errorf("release stale registration reservations: %w", err)
	}
	records, err := service.repository.ListIncompleteRegistrations(ctx, registrationRecoveryBatchSize)
	if err != nil {
		return RegistrationRecoveryResult{ReleasedReservations: released}, fmt.Errorf("list incomplete registrations: %w", err)
	}
	result := RegistrationRecoveryResult{ReleasedReservations: released}
	var recoveryErrors []error
	for _, record := range records {
		if record.ID == uuid.Nil || record.CredentialRef == nil || record.State != RegistrationStateCredentialProvisioned {
			recoveryErrors = append(recoveryErrors, fmt.Errorf("registration %s: %w", record.ID, ErrRegistrationStateConflict))
			continue
		}
		activatedRef, activateErr := service.vault.ActivateRegistration(ctx, record.ID)
		if activateErr != nil {
			recoveryErrors = append(recoveryErrors, fmt.Errorf("registration %s activate: %w", record.ID, activateErr))
			continue
		}
		if activatedRef != *record.CredentialRef {
			recoveryErrors = append(recoveryErrors, fmt.Errorf("registration %s credential mismatch: %w", record.ID, ErrRegistrationStateConflict))
			continue
		}
		completed, completeErr := service.repository.CompleteRegistration(ctx, record.ID, now)
		if completeErr != nil {
			recoveryErrors = append(recoveryErrors, fmt.Errorf("registration %s complete: %w", record.ID, completeErr))
			continue
		}
		if completed.CompletedAt == nil {
			recoveryErrors = append(recoveryErrors, fmt.Errorf("registration %s missing completion: %w", record.ID, ErrRegistrationStateConflict))
			continue
		}
		result.CompletedRegistrations++
	}
	return result, errors.Join(recoveryErrors...)
}

func (service *RegistrationService) cancelPreparedRegistration(ctx context.Context, registrationID uuid.UUID) error {
	// Request cancellation is a common reason for an ambiguous Vault result.
	// Preserve request values for tracing but detach cancellation so the bounded
	// Core compensation can still release an uncommitted invitation claim.
	compensationCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), registrationCompensationTimeout)
	defer cancel()
	return service.repository.CancelRegistration(compensationCtx, registrationID)
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

func normalizeRegistrationInput(input RegistrationInput) (RegistrationInput, []byte, []byte, error) {
	input.Username = strings.ToLower(strings.TrimSpace(input.Username))
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.InvitationToken = strings.TrimSpace(input.InvitationToken)
	if input.ID == uuid.Nil || !registrationUsernamePattern.MatchString(input.Username) ||
		utf8.RuneCountInString(input.DisplayName) < 1 || utf8.RuneCountInString(input.DisplayName) > 40 ||
		len(input.Password) < 12 || len(input.Password) > maxPasswordBytes {
		return RegistrationInput{}, nil, nil, ErrInvalidInput
	}
	normalizedEmail, err := normalizeEmailAddress(input.Email)
	if err != nil || normalizedEmail != input.Email {
		return RegistrationInput{}, nil, nil, ErrInvalidInput
	}
	var invitationDigest []byte
	if input.InvitationToken != "" {
		if len(input.InvitationToken) != 43 && !legacyRegistrationInvitationToken.MatchString(input.InvitationToken) {
			return RegistrationInput{}, nil, nil, ErrInvalidInput
		}
		digest := sha256.Sum256([]byte(input.InvitationToken))
		invitationDigest = append([]byte(nil), digest[:]...)
	}
	var invitationEmailBinding []byte
	if input.InvitationToken != "" {
		invitationEmailBinding = invitationEmailBindingHMAC(input.InvitationToken, input.Email)
	}
	return input, invitationDigest, invitationEmailBinding, nil
}

var _ interface {
	PublicPolicy(context.Context) (RegistrationPublicPolicy, error)
	Register(context.Context, RegistrationInput) (RegistrationResult, error)
	Policy(context.Context, authz.StaffActor) (RegistrationPolicy, error)
	UpdatePolicy(context.Context, authz.StaffActor, UpdateRegistrationPolicyInput) (RegistrationPolicy, error)
	RecoverIncompleteRegistrations(context.Context) (RegistrationRecoveryResult, error)
} = (*RegistrationService)(nil)
