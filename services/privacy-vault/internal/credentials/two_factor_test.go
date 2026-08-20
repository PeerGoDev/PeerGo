package credentials

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
)

type twoFactorRepositoryFixture struct {
	credential          Credential
	blockedUntil        time.Time
	status              TwoFactorStatus
	enrollment          TOTPEnrollment
	factor              TOTPFactor
	change              StoredTwoFactorChange
	changeErr           error
	lastConsumedStep    int64
	consumedRecovery    bool
	activeCalls         int
	rotateCalls         int
	disableCalls        int
	clearCalls          int
	recordFailureCalls  int
	confirmedEnrollment ConfirmTOTPEnrollmentCommand
}

func (fixture *twoFactorRepositoryFixture) CredentialByReference(context.Context, uuid.UUID) (Credential, error) {
	return fixture.credential, nil
}

func (fixture *twoFactorRepositoryFixture) LoginBlockedUntil(context.Context, []byte, time.Time) (time.Time, error) {
	return fixture.blockedUntil, nil
}

func (fixture *twoFactorRepositoryFixture) RecordLoginFailure(context.Context, []byte, time.Time) error {
	fixture.recordFailureCalls++
	return nil
}

func (fixture *twoFactorRepositoryFixture) ClearLoginFailures(context.Context, []byte) error {
	fixture.clearCalls++
	return nil
}

func (fixture *twoFactorRepositoryFixture) TwoFactorStatus(context.Context, uuid.UUID) (TwoFactorStatus, error) {
	return fixture.status, nil
}

func (fixture *twoFactorRepositoryFixture) CreateTOTPEnrollment(context.Context, TOTPEnrollment) error {
	return nil
}

func (fixture *twoFactorRepositoryFixture) TOTPEnrollment(context.Context, uuid.UUID, uuid.UUID) (TOTPEnrollment, error) {
	return fixture.enrollment, nil
}

func (fixture *twoFactorRepositoryFixture) TwoFactorChange(context.Context, uuid.UUID, uuid.UUID) (StoredTwoFactorChange, error) {
	if fixture.changeErr != nil {
		return StoredTwoFactorChange{}, fixture.changeErr
	}
	return fixture.change, nil
}

func (fixture *twoFactorRepositoryFixture) ConfirmTOTPEnrollment(_ context.Context, command ConfirmTOTPEnrollmentCommand) error {
	fixture.confirmedEnrollment = command
	return nil
}

func (fixture *twoFactorRepositoryFixture) ActiveTOTPFactor(context.Context, uuid.UUID) (TOTPFactor, error) {
	fixture.activeCalls++
	if fixture.factor.CredentialRef == uuid.Nil {
		return TOTPFactor{}, ErrTwoFactorNotEnabled
	}
	return fixture.factor, nil
}

func (fixture *twoFactorRepositoryFixture) ConsumeLoginTOTP(_ context.Context, _ uuid.UUID, step int64, _ time.Time) (bool, error) {
	if step <= fixture.lastConsumedStep {
		return false, nil
	}
	fixture.lastConsumedStep = step
	return true, nil
}

func (fixture *twoFactorRepositoryFixture) ConsumeLoginRecoveryCode(context.Context, uuid.UUID, []byte, time.Time) (bool, error) {
	if fixture.consumedRecovery {
		return false, nil
	}
	fixture.consumedRecovery = true
	return true, nil
}

func (fixture *twoFactorRepositoryFixture) RotateRecoveryCodes(_ context.Context, command RotateRecoveryCodesCommand) (StoredTwoFactorChange, error) {
	fixture.rotateCalls++
	return StoredTwoFactorChange{
		ID: command.ChangeID, CredentialRef: command.CredentialRef,
		Kind: TwoFactorChangeRecoveryCodesRotated, ChangedAt: command.ChangedAt,
		RecoveryBundle:          &command.RecoveryBundle,
		RecoveryBundleExpiresAt: &command.RecoveryBundleExpiresAt,
	}, nil
}

func (fixture *twoFactorRepositoryFixture) DisableTOTP(_ context.Context, command DisableTOTPCommand) (StoredTwoFactorChange, error) {
	fixture.disableCalls++
	return StoredTwoFactorChange{
		ID: command.ChangeID, CredentialRef: command.CredentialRef,
		Kind: TwoFactorChangeDisabled, ChangedAt: command.ChangedAt,
	}, nil
}

func TestTwoFactorLoginConsumesEachTOTPTimeStepOnlyOnce(t *testing.T) {
	now := time.Date(2026, time.August, 7, 9, 30, 0, 0, time.UTC)
	credentialRef, enrollmentID := uuid.New(), uuid.New()
	protector := testSecretProtector(t)
	secret := "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"
	protected, err := protector.Protect(totpSecretRecordKind, credentialRef, enrollmentID, []byte(secret))
	if err != nil {
		t.Fatalf("Protect() error = %v", err)
	}
	code, err := totp.GenerateCode(secret, now)
	if err != nil {
		t.Fatalf("GenerateCode() error = %v", err)
	}
	repository := &twoFactorRepositoryFixture{
		factor: TOTPFactor{
			CredentialRef: credentialRef, EnrollmentID: enrollmentID,
			Secret: protected, EnabledAt: now.Add(-time.Hour), LastUsedStep: -1,
		},
		lastConsumedStep: -1,
	}
	service := testTwoFactorService(t, repository, protector, now)

	enabled, err := service.VerifyForLogin(context.Background(), credentialRef, code, now)
	if err != nil || !enabled {
		t.Fatalf("first VerifyForLogin() enabled=%v error=%v", enabled, err)
	}
	enabled, err = service.VerifyForLogin(context.Background(), credentialRef, code, now)
	if !enabled || !errors.Is(err, ErrTwoFactorVerification) {
		t.Fatalf("replayed VerifyForLogin() enabled=%v error=%v", enabled, err)
	}
}

func TestRecoveryCodeRotationReturnsEncryptedIdempotentResultWithoutReusingEvidence(t *testing.T) {
	now := time.Date(2026, time.August, 7, 10, 0, 0, 0, time.UTC)
	credentialRef, changeID := uuid.New(), uuid.New()
	protector := testSecretProtector(t)
	codes := testRecoveryCodes()
	payload, _ := json.Marshal(codes)
	bundle, err := protector.Protect(rotationBundleRecordKind, credentialRef, changeID, payload)
	if err != nil {
		t.Fatalf("Protect() error = %v", err)
	}
	expiresAt := now.Add(recoveryBundleTTL)
	passwordHash, err := HashPassword("PeerGo-current-password!")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	repository := &twoFactorRepositoryFixture{
		credential: Credential{Reference: credentialRef, PasswordHash: passwordHash},
		change: StoredTwoFactorChange{
			ID: changeID, CredentialRef: credentialRef,
			Kind: TwoFactorChangeRecoveryCodesRotated, ChangedAt: now,
			RecoveryBundle: &bundle, RecoveryBundleExpiresAt: &expiresAt,
		},
	}
	service := testTwoFactorService(t, repository, protector, now)

	result, err := service.RotateRecoveryCodes(context.Background(), credentialRef, changeID, "PeerGo-current-password!", "123456")
	if err != nil {
		t.Fatalf("RotateRecoveryCodes() error = %v", err)
	}
	if result.ChangeID != changeID || !reflect.DeepEqual(result.RecoveryCodes, codes) {
		t.Fatalf("RotateRecoveryCodes() = %+v", result)
	}
	if repository.activeCalls != 0 || repository.rotateCalls != 0 || repository.clearCalls != 1 {
		t.Fatalf("repository calls active=%d rotate=%d clear=%d", repository.activeCalls, repository.rotateCalls, repository.clearCalls)
	}
}

func TestDisableReturnsIdempotentResultAfterFactorIsAlreadyGone(t *testing.T) {
	now := time.Date(2026, time.August, 7, 10, 30, 0, 0, time.UTC)
	credentialRef, changeID := uuid.New(), uuid.New()
	passwordHash, err := HashPassword("PeerGo-current-password!")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	repository := &twoFactorRepositoryFixture{
		credential: Credential{Reference: credentialRef, PasswordHash: passwordHash},
		change: StoredTwoFactorChange{
			ID: changeID, CredentialRef: credentialRef,
			Kind: TwoFactorChangeDisabled, ChangedAt: now,
		},
	}
	service := testTwoFactorService(t, repository, testSecretProtector(t), now)

	result, err := service.Disable(context.Background(), credentialRef, changeID, "PeerGo-current-password!", "123456")
	if err != nil || result.ChangeID != changeID || result.ChangedAt != now {
		t.Fatalf("Disable() result=%+v error=%v", result, err)
	}
	if repository.activeCalls != 0 || repository.disableCalls != 0 || repository.clearCalls != 1 {
		t.Fatalf("repository calls active=%d disable=%d clear=%d", repository.activeCalls, repository.disableCalls, repository.clearCalls)
	}
}

func testSecretProtector(t *testing.T) *SecretProtector {
	t.Helper()
	protector, err := NewSecretProtector(
		bytes.Repeat([]byte{0x42}, 32),
		"test-2026-08",
		bytes.NewReader(bytes.Repeat([]byte{0x24}, 1024)),
	)
	if err != nil {
		t.Fatalf("NewSecretProtector() error = %v", err)
	}
	return protector
}

func testTwoFactorService(t *testing.T, repository TwoFactorRepository, protector *SecretProtector, now time.Time) *TwoFactorService {
	t.Helper()
	service, err := NewTwoFactorService(
		repository,
		protector,
		bytes.Repeat([]byte{0x61}, 32),
		TwoFactorServiceConfig{
			Now:    func() time.Time { return now },
			Random: bytes.NewReader(bytes.Repeat([]byte{0x35}, 4096)),
			NewID:  uuid.New,
		},
	)
	if err != nil {
		t.Fatalf("NewTwoFactorService() error = %v", err)
	}
	return service
}

func testRecoveryCodes() []string {
	return []string{
		"ABCD-EFGH-JKLM", "BCDE-FGHJ-KLMN", "CDEF-GHJK-LMNP", "DEFG-HJKL-MNPQ", "EFGH-JKLM-NPQR",
		"FGHJ-KLMN-PQRS", "GHJK-LMNP-QRST", "HJKL-MNPQ-RSTU", "JKLM-NPQR-STUV", "KLMN-PQRS-TUVW",
	}
}
