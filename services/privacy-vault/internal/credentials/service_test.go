package credentials

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type fixtureRepository struct {
	lookup     []byte
	credential Credential
}

type loginProtectionRepositoryRecorder struct {
	lookup                []byte
	credential            Credential
	blockedUntil          time.Time
	credentialCalls       int
	failureCalls          int
	clearCalls            int
	rehashCalls           int
	rehashExpected        string
	rehashReplacement     string
	rehashedAt            time.Time
	rehashResult          bool
	appealCredentialCalls int
	enabledReference      uuid.UUID
	enabledAt             time.Time
}

type noSecondFactorVerifier struct{}

func (noSecondFactorVerifier) VerifyForLogin(context.Context, uuid.UUID, string, time.Time) (bool, error) {
	return false, nil
}

type requiredSecondFactorVerifier struct{}

func (requiredSecondFactorVerifier) VerifyForLogin(context.Context, uuid.UUID, string, time.Time) (bool, error) {
	return true, ErrSecondFactorRequired
}

func (r *loginProtectionRepositoryRecorder) ProvisionRegistration(context.Context, RegistrationProvisionRecord) (uuid.UUID, error) {
	return uuid.Nil, ErrRegistrationProvisionNotFound
}

func (r *loginProtectionRepositoryRecorder) ActivateRegistration(context.Context, uuid.UUID, time.Time) (uuid.UUID, error) {
	return uuid.Nil, ErrRegistrationProvisionNotFound
}

func (r *loginProtectionRepositoryRecorder) CredentialByLookupHMAC(_ context.Context, lookup []byte) (Credential, error) {
	r.credentialCalls++
	if string(lookup) != string(r.lookup) {
		return Credential{}, ErrInvalidCredentials
	}
	return r.credential, nil
}

func (r *loginProtectionRepositoryRecorder) CredentialByLookupHMACForAccountAppeal(_ context.Context, lookup []byte) (Credential, error) {
	r.appealCredentialCalls++
	if string(lookup) != string(r.lookup) {
		return Credential{}, ErrInvalidCredentials
	}
	return r.credential, nil
}

func (r *loginProtectionRepositoryRecorder) EnableCredentialAfterAccountAppeal(_ context.Context, credentialRef uuid.UUID, enabledAt time.Time) error {
	r.enabledReference = credentialRef
	r.enabledAt = enabledAt
	return nil
}

func (r *loginProtectionRepositoryRecorder) LoginBlockedUntil(context.Context, []byte, time.Time) (time.Time, error) {
	return r.blockedUntil, nil
}

func (r *loginProtectionRepositoryRecorder) RecordLoginFailure(context.Context, []byte, time.Time) error {
	r.failureCalls++
	return nil
}

func (r *loginProtectionRepositoryRecorder) ClearLoginFailures(context.Context, []byte) error {
	r.clearCalls++
	return nil
}

func (r *loginProtectionRepositoryRecorder) RehashPasswordIfCurrent(_ context.Context, _ uuid.UUID, expected, replacement string, rehashedAt time.Time) (bool, error) {
	r.rehashCalls++
	r.rehashExpected = expected
	r.rehashReplacement = replacement
	r.rehashedAt = rehashedAt
	return r.rehashResult, nil
}

func (r fixtureRepository) ProvisionRegistration(context.Context, RegistrationProvisionRecord) (uuid.UUID, error) {
	return uuid.Nil, ErrRegistrationProvisionNotFound
}

func (r fixtureRepository) ActivateRegistration(context.Context, uuid.UUID, time.Time) (uuid.UUID, error) {
	return uuid.Nil, ErrRegistrationProvisionNotFound
}

func (r fixtureRepository) CredentialByLookupHMAC(_ context.Context, lookup []byte) (Credential, error) {
	if string(lookup) != string(r.lookup) {
		return Credential{}, ErrInvalidCredentials
	}
	return r.credential, nil
}

func (r fixtureRepository) CredentialByLookupHMACForAccountAppeal(ctx context.Context, lookup []byte) (Credential, error) {
	return r.CredentialByLookupHMAC(ctx, lookup)
}

func (fixtureRepository) EnableCredentialAfterAccountAppeal(context.Context, uuid.UUID, time.Time) error {
	return nil
}

func (fixtureRepository) LoginBlockedUntil(context.Context, []byte, time.Time) (time.Time, error) {
	return time.Time{}, nil
}

func (fixtureRepository) RecordLoginFailure(context.Context, []byte, time.Time) error { return nil }
func (fixtureRepository) ClearLoginFailures(context.Context, []byte) error            { return nil }
func (fixtureRepository) RehashPasswordIfCurrent(context.Context, uuid.UUID, string, string, time.Time) (bool, error) {
	return true, nil
}

func TestServiceReturnsOnlyCredentialReference(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	lookup, err := LookupHMAC(key, "demo")
	if err != nil {
		t.Fatalf("LookupHMAC() error = %v", err)
	}
	passwordHash, err := HashPassword("PeerGo-demo-2026!")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	reference := uuid.MustParse("0198f20a-6da8-7e51-9c64-222222222222")
	service, err := NewService(fixtureRepository{
		lookup:     lookup,
		credential: Credential{Reference: reference, PasswordHash: passwordHash},
	}, noSecondFactorVerifier{}, key)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	result, err := service.Verify(context.Background(), " Demo ", "PeerGo-demo-2026!", "")
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result != reference {
		t.Fatalf("Verify() = %s, want %s", result, reference)
	}

	if _, err := service.Verify(context.Background(), "missing", "PeerGo-demo-2026!", ""); err != ErrInvalidCredentials {
		t.Fatalf("Verify(missing) error = %v, want ErrInvalidCredentials", err)
	}
}

func TestServiceUsesPurposeLimitedLookupAndSeparateEnableCommandForAccountAppeal(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	lookup, err := LookupHMAC(key, "disabled-member")
	if err != nil {
		t.Fatal(err)
	}
	passwordHash, err := HashPassword("PeerGo-disabled-2026!")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 17, 2, 30, 0, 0, time.UTC)
	reference := uuid.New()
	repository := &loginProtectionRepositoryRecorder{
		lookup:     lookup,
		credential: Credential{Reference: reference, PasswordHash: passwordHash},
	}
	service, err := NewService(repository, noSecondFactorVerifier{}, key)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }

	result, err := service.VerifyForAccountAppeal(
		context.Background(), "disabled-member", "PeerGo-disabled-2026!", "",
	)
	if err != nil || result != reference || repository.appealCredentialCalls != 1 || repository.credentialCalls != 0 {
		t.Fatalf("VerifyForAccountAppeal() = %s, %v repository=%+v", result, err, repository)
	}
	if repository.enabledReference != uuid.Nil {
		t.Fatal("credential proof enabled the account before a staff command")
	}
	if err := service.EnableCredentialAfterAccountAppeal(context.Background(), reference); err != nil {
		t.Fatal(err)
	}
	if repository.enabledReference != reference || !repository.enabledAt.Equal(now) {
		t.Fatalf("enable command = %s at %s", repository.enabledReference, repository.enabledAt)
	}
}

func TestServiceEnforcesPersistentThrottleBeforePasswordHash(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	lookup, err := LookupHMAC(key, "member")
	if err != nil {
		t.Fatalf("LookupHMAC() error = %v", err)
	}
	now := time.Date(2026, time.August, 6, 20, 0, 0, 0, time.UTC)
	repository := &loginProtectionRepositoryRecorder{lookup: lookup, blockedUntil: now.Add(time.Minute)}
	service, err := NewService(repository, noSecondFactorVerifier{}, key)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.now = func() time.Time { return now }
	_, err = service.Verify(context.Background(), "member", "any-password", "")
	var throttle *LoginThrottleError
	if !errors.As(err, &throttle) || throttle.RetryAt != repository.blockedUntil || repository.credentialCalls != 0 {
		t.Fatalf("Verify() error=%v repository=%+v", err, repository)
	}
}

func TestServiceRecordsFailuresAndClearsBucketOnSuccess(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	lookup, err := LookupHMAC(key, "member")
	if err != nil {
		t.Fatalf("LookupHMAC() error = %v", err)
	}
	passwordHash, err := HashPassword("PeerGo-correct-password!")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	repository := &loginProtectionRepositoryRecorder{
		lookup: lookup, credential: Credential{Reference: uuid.New(), PasswordHash: passwordHash},
	}
	service, err := NewService(repository, noSecondFactorVerifier{}, key)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if _, err := service.Verify(context.Background(), "member", "wrong-password", ""); !errors.Is(err, ErrInvalidCredentials) || repository.failureCalls != 1 {
		t.Fatalf("bad Verify() error=%v repository=%+v", err, repository)
	}
	if _, err := service.Verify(context.Background(), "member", "PeerGo-correct-password!", ""); err != nil || repository.clearCalls != 1 {
		t.Fatalf("good Verify() error=%v repository=%+v", err, repository)
	}
}

func TestServiceUpgradesPtYesBcryptOnlyAfterCompleteLogin(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	lookup, err := LookupHMAC(key, "legacy-member")
	if err != nil {
		t.Fatal(err)
	}
	legacyHash, err := bcrypt.GenerateFromPassword([]byte("PtYes-member-password"), 10)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 10, 14, 30, 0, 0, time.UTC)
	reference := uuid.New()
	repository := &loginProtectionRepositoryRecorder{
		lookup:       lookup,
		credential:   Credential{Reference: reference, PasswordHash: string(legacyHash)},
		rehashResult: true,
	}
	service, err := NewService(repository, noSecondFactorVerifier{}, key)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	result, err := service.Verify(context.Background(), "legacy-member", "PtYes-member-password", "")
	if err != nil || result != reference {
		t.Fatalf("Verify() = %s, %v", result, err)
	}
	if repository.rehashCalls != 1 || repository.rehashExpected != string(legacyHash) ||
		repository.rehashedAt != now || repository.clearCalls != 1 {
		t.Fatalf("legacy rehash record = %+v", repository)
	}
	match, needsRehash, err := VerifyPassword(repository.rehashReplacement, "PtYes-member-password")
	if err != nil || !match || needsRehash {
		t.Fatalf("replacement VerifyPassword() = %v, %v, %v", match, needsRehash, err)
	}
}

func TestServiceDoesNotRehashBeforeSecondFactor(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	lookup, _ := LookupHMAC(key, "legacy-member")
	legacyHash, err := bcrypt.GenerateFromPassword([]byte("PtYes-member-password"), 10)
	if err != nil {
		t.Fatal(err)
	}
	repository := &loginProtectionRepositoryRecorder{
		lookup:       lookup,
		credential:   Credential{Reference: uuid.New(), PasswordHash: string(legacyHash)},
		rehashResult: true,
	}
	service, err := NewService(repository, requiredSecondFactorVerifier{}, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Verify(context.Background(), "legacy-member", "PtYes-member-password", ""); !errors.Is(err, ErrSecondFactorRequired) {
		t.Fatalf("Verify() error = %v", err)
	}
	if repository.rehashCalls != 0 || repository.clearCalls != 0 {
		t.Fatalf("incomplete login mutated credentials: %+v", repository)
	}
}

func TestServiceFailsClosedWhenPasswordChangesDuringRehash(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	lookup, _ := LookupHMAC(key, "legacy-member")
	legacyHash, err := bcrypt.GenerateFromPassword([]byte("PtYes-member-password"), 10)
	if err != nil {
		t.Fatal(err)
	}
	repository := &loginProtectionRepositoryRecorder{
		lookup:     lookup,
		credential: Credential{Reference: uuid.New(), PasswordHash: string(legacyHash)},
	}
	service, err := NewService(repository, noSecondFactorVerifier{}, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Verify(context.Background(), "legacy-member", "PtYes-member-password", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Verify() error = %v", err)
	}
	if repository.rehashCalls != 1 || repository.clearCalls != 0 {
		t.Fatalf("CAS failure did not fail closed: %+v", repository)
	}
}
