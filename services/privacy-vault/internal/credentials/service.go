package credentials

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidCredentials = errors.New("identifier or password is invalid")
	ErrLoginThrottled     = errors.New("login attempts are temporarily throttled")
)

const loginFailureWindow = 15 * time.Minute

type LoginThrottleError struct {
	RetryAt time.Time
}

func (err *LoginThrottleError) Error() string { return ErrLoginThrottled.Error() }
func (err *LoginThrottleError) Unwrap() error { return ErrLoginThrottled }

// Credential is the minimum P0 record needed for a verification decision.
type Credential struct {
	Reference    uuid.UUID
	PasswordHash string
}

// Repository is the Vault-owned persistence port. No method exposes a direct
// identifier or supports listing credential material.
type Repository interface {
	CredentialByLookupHMAC(context.Context, []byte) (Credential, error)
	CredentialByLookupHMACForAccountAppeal(context.Context, []byte) (Credential, error)
	LoginBlockedUntil(context.Context, []byte, time.Time) (time.Time, error)
	RecordLoginFailure(context.Context, []byte, time.Time) error
	ClearLoginFailures(context.Context, []byte) error
	RehashPasswordIfCurrent(context.Context, uuid.UUID, string, string, time.Time) (bool, error)
	EnableCredentialAfterAccountAppeal(context.Context, uuid.UUID, time.Time) error
	ProvisionRegistration(context.Context, RegistrationProvisionRecord) (uuid.UUID, error)
	ActivateRegistration(context.Context, uuid.UUID, time.Time) (uuid.UUID, error)
	IdentifierExists(context.Context, []byte) (bool, error)
}

// SecondFactorVerifier is a purpose-limited decision port. It never returns a
// TOTP seed or recovery-code record to the password verification service.
type SecondFactorVerifier interface {
	VerifyForLogin(context.Context, uuid.UUID, string, time.Time) (bool, error)
}

// Service makes a credential decision without releasing credential material.
type Service struct {
	repository    Repository
	secondFactor  SecondFactorVerifier
	identifierKey []byte
	dummyHash     string
	now           func() time.Time
}

// EmailRegistered returns only an equality decision. The normalized email and
// its keyed lookup value remain inside Vault and are never stored by Core.
func (s *Service) EmailRegistered(ctx context.Context, email string) (bool, error) {
	normalized, err := normalizeEmailAddress(email)
	if err != nil {
		return false, ErrRegistrationInput
	}
	lookup, err := LookupHMAC(s.identifierKey, normalized)
	if err != nil {
		return false, ErrRegistrationInput
	}
	exists, err := s.repository.IdentifierExists(ctx, lookup)
	if err != nil {
		return false, fmt.Errorf("check email registration: %w", err)
	}
	return exists, nil
}

// NewService creates one dummy Argon2id record. Unknown identifiers are checked
// against it so account misses follow the same expensive path as bad passwords.
func NewService(repository Repository, secondFactor SecondFactorVerifier, identifierKey []byte) (*Service, error) {
	if repository == nil {
		return nil, errors.New("credential repository is required")
	}
	if secondFactor == nil {
		return nil, errors.New("second-factor verifier is required")
	}
	if len(identifierKey) < 32 {
		return nil, errors.New("identifier key must contain at least 32 bytes")
	}
	dummyHash, err := HashPassword("peergo-vault-dummy-credential")
	if err != nil {
		return nil, fmt.Errorf("create dummy credential: %w", err)
	}

	return &Service{
		repository:    repository,
		secondFactor:  secondFactor,
		identifierKey: append([]byte(nil), identifierKey...),
		dummyHash:     dummyHash,
		now:           time.Now,
	}, nil
}

// Verify returns only the opaque reference after every enabled factor has been
// proven. Password success never creates a session on its own when TOTP is on.
func (s *Service) Verify(ctx context.Context, identifier, password, secondFactorCode string) (uuid.UUID, error) {
	return s.verify(ctx, identifier, password, secondFactorCode, false)
}

// VerifyForAccountAppeal is the only credential decision allowed to inspect a
// disabled credential. It proves ownership for the public appeal workflow but
// never creates a session or silently re-enables the account.
func (s *Service) VerifyForAccountAppeal(ctx context.Context, identifier, password, secondFactorCode string) (uuid.UUID, error) {
	return s.verify(ctx, identifier, password, secondFactorCode, true)
}

func (s *Service) verify(ctx context.Context, identifier, password, secondFactorCode string, allowDisabled bool) (uuid.UUID, error) {
	lookup, err := LookupHMAC(s.identifierKey, identifier)
	if err != nil || password == "" || len(password) > maxPasswordBytes {
		return uuid.Nil, ErrInvalidCredentials
	}
	now := s.now().UTC()
	blockedUntil, err := s.repository.LoginBlockedUntil(ctx, lookup, now)
	if err != nil {
		return uuid.Nil, fmt.Errorf("check login throttle: %w", err)
	}
	if blockedUntil.After(now) {
		return uuid.Nil, &LoginThrottleError{RetryAt: blockedUntil.UTC()}
	}

	var credential Credential
	if allowDisabled {
		credential, err = s.repository.CredentialByLookupHMACForAccountAppeal(ctx, lookup)
	} else {
		credential, err = s.repository.CredentialByLookupHMAC(ctx, lookup)
	}
	if errors.Is(err, ErrInvalidCredentials) {
		_, _, dummyErr := VerifyPassword(s.dummyHash, password)
		if dummyErr != nil {
			return uuid.Nil, fmt.Errorf("verify dummy credential: %w", dummyErr)
		}
		if recordErr := s.repository.RecordLoginFailure(ctx, lookup, now); recordErr != nil {
			return uuid.Nil, fmt.Errorf("record unknown-identifier login failure: %w", recordErr)
		}
		return uuid.Nil, ErrInvalidCredentials
	}
	if err != nil {
		return uuid.Nil, err
	}

	match, needsRehash, err := VerifyPassword(credential.PasswordHash, password)
	if err != nil {
		return uuid.Nil, fmt.Errorf("verify stored credential: %w", err)
	}
	if !match {
		if recordErr := s.repository.RecordLoginFailure(ctx, lookup, now); recordErr != nil {
			return uuid.Nil, fmt.Errorf("record password login failure: %w", recordErr)
		}
		return uuid.Nil, ErrInvalidCredentials
	}
	_, factorErr := s.secondFactor.VerifyForLogin(ctx, credential.Reference, secondFactorCode, now)
	if errors.Is(factorErr, ErrSecondFactorRequired) {
		return uuid.Nil, ErrSecondFactorRequired
	}
	if factorErr != nil {
		if recordErr := s.repository.RecordLoginFailure(ctx, lookup, now); recordErr != nil {
			return uuid.Nil, fmt.Errorf("record second-factor login failure: %w", recordErr)
		}
		return uuid.Nil, ErrInvalidCredentials
	}
	if needsRehash && !allowDisabled {
		// Rehash only after every enabled factor succeeds. The repository uses
		// compare-and-swap on the verified hash so a concurrent password reset
		// cannot be overwritten or authenticated with a now-stale password.
		replacement, hashErr := HashPassword(password)
		if hashErr != nil {
			return uuid.Nil, fmt.Errorf("rehash verified password: %w", hashErr)
		}
		rehashed, rehashErr := s.repository.RehashPasswordIfCurrent(
			ctx,
			credential.Reference,
			credential.PasswordHash,
			replacement,
			now,
		)
		if rehashErr != nil {
			return uuid.Nil, fmt.Errorf("persist verified password rehash: %w", rehashErr)
		}
		if !rehashed {
			return uuid.Nil, ErrInvalidCredentials
		}
	}
	if err := s.repository.ClearLoginFailures(ctx, lookup); err != nil {
		return uuid.Nil, fmt.Errorf("clear login failures: %w", err)
	}
	return credential.Reference, nil
}

// EnableCredentialAfterAccountAppeal is deliberately separate from
// verification. Only Core's reviewed staff decision calls this idempotent
// command; a successful appeal lookup can never enable login by itself.
func (s *Service) EnableCredentialAfterAccountAppeal(ctx context.Context, credentialRef uuid.UUID) error {
	if credentialRef == uuid.Nil {
		return ErrInvalidCredentials
	}
	if err := s.repository.EnableCredentialAfterAccountAppeal(ctx, credentialRef, s.now().UTC()); err != nil {
		return fmt.Errorf("enable credential after account appeal: %w", err)
	}
	return nil
}

// loginFailureBackoff deliberately avoids a permanent account lockout. The
// first two failures are recorded without delay; subsequent failures receive a
// bounded, increasing pause that resets after the 15-minute failure window.
func loginFailureBackoff(failedAttempts int32) time.Duration {
	switch failedAttempts {
	case 1, 2:
		return 0
	case 3:
		return 5 * time.Second
	case 4:
		return 15 * time.Second
	case 5:
		return time.Minute
	default:
		return 5 * time.Minute
	}
}
