package credentials

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/hotp"
	"github.com/pquerna/otp/totp"
)

const (
	totpIssuer                  = "PeerGo"
	totpPeriodSeconds           = 30
	totpEnrollmentTTL           = 10 * time.Minute
	recoveryBundleTTL           = 10 * time.Minute
	recoveryCodeCount           = 10
	recoveryCodeAlphabet        = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	totpSecretRecordKind        = "totp-enrollment-secret"
	recoveryBundleRecordKind    = "totp-recovery-bundle"
	rotationBundleRecordKind    = "totp-rotation-recovery-bundle"
	recoveryCodeHMACDomain      = "peergo:vault:totp-recovery-code:v1\x00"
	sensitiveActionBucketDomain = "peergo:vault:sensitive-action-bucket:v1\x00"
)

var (
	ErrSecondFactorRequired          = errors.New("a second factor is required")
	ErrTwoFactorInput                = errors.New("two-factor input is invalid")
	ErrTwoFactorAlreadyEnabled       = errors.New("two-factor authentication is already enabled")
	ErrTwoFactorNotEnabled           = errors.New("two-factor authentication is not enabled")
	ErrTwoFactorEnrollmentNotFound   = errors.New("two-factor enrollment was not found")
	ErrTwoFactorVerification         = errors.New("two-factor verification failed")
	ErrRecoveryCodeBundleUnavailable = errors.New("recovery code bundle is no longer available")
	ErrTwoFactorChangeNotFound       = errors.New("two-factor change was not found")
	ErrTwoFactorIdempotencyConflict  = errors.New("two-factor idempotency key conflicts with an existing change")
)

type TwoFactorStatus struct {
	Enabled                bool
	EnabledAt              *time.Time
	RecoveryCodesRemaining int64
}

type TOTPEnrollment struct {
	ID                      uuid.UUID
	CredentialRef           uuid.UUID
	Secret                  ProtectedSecret
	CreatedAt               time.Time
	ExpiresAt               time.Time
	ConfirmedAt             *time.Time
	SupersededAt            *time.Time
	RecoveryBundle          *ProtectedSecret
	RecoveryBundleExpiresAt *time.Time
}

type TOTPFactor struct {
	CredentialRef uuid.UUID
	EnrollmentID  uuid.UUID
	Secret        ProtectedSecret
	EnabledAt     time.Time
	LastUsedStep  int64
}

type TOTPEnrollmentStart struct {
	EnrollmentID    uuid.UUID
	Secret          string
	ProvisioningURI string
	ExpiresAt       time.Time
}

type TOTPEnrollmentConfirmation struct {
	ChangeID      uuid.UUID
	EnabledAt     time.Time
	RecoveryCodes []string
}

type TwoFactorChange struct {
	ChangeID      uuid.UUID
	ChangedAt     time.Time
	RecoveryCodes []string
}

type TwoFactorChangeKind string

const (
	TwoFactorChangeRecoveryCodesRotated TwoFactorChangeKind = "recovery_codes_rotated"
	TwoFactorChangeDisabled             TwoFactorChangeKind = "disabled"
)

// StoredTwoFactorChange is the Vault-owned idempotency result. Rotation codes
// remain encrypted and short-lived; a disabled change has no secret payload.
type StoredTwoFactorChange struct {
	ID                      uuid.UUID
	CredentialRef           uuid.UUID
	Kind                    TwoFactorChangeKind
	ChangedAt               time.Time
	RecoveryBundle          *ProtectedSecret
	RecoveryBundleExpiresAt *time.Time
}

type RecoveryCodeRecord struct {
	Ordinal  int16
	CodeHMAC []byte
}

type ConfirmTOTPEnrollmentCommand struct {
	Enrollment              TOTPEnrollment
	ConfirmedAt             time.Time
	GenerationID            uuid.UUID
	RecoveryCodes           []RecoveryCodeRecord
	RecoveryBundle          ProtectedSecret
	RecoveryBundleExpiresAt time.Time
}

type SecondFactorEvidenceKind string

const (
	SecondFactorEvidenceTOTP     SecondFactorEvidenceKind = "totp"
	SecondFactorEvidenceRecovery SecondFactorEvidenceKind = "recovery_code"
)

type SecondFactorEvidence struct {
	Kind             SecondFactorEvidenceKind
	TOTPTimeStep     int64
	RecoveryCodeHMAC []byte
}

type RotateRecoveryCodesCommand struct {
	CredentialRef           uuid.UUID
	ChangeID                uuid.UUID
	ChangedAt               time.Time
	Evidence                SecondFactorEvidence
	GenerationID            uuid.UUID
	RecoveryCodes           []RecoveryCodeRecord
	RecoveryBundle          ProtectedSecret
	RecoveryBundleExpiresAt time.Time
}

type DisableTOTPCommand struct {
	CredentialRef uuid.UUID
	ChangeID      uuid.UUID
	ChangedAt     time.Time
	Evidence      SecondFactorEvidence
}

// TwoFactorRepository never accepts a raw TOTP secret, password or recovery
// code. Those values are transformed in the service before persistence calls.
type TwoFactorRepository interface {
	CredentialByReference(context.Context, uuid.UUID) (Credential, error)
	LoginBlockedUntil(context.Context, []byte, time.Time) (time.Time, error)
	RecordLoginFailure(context.Context, []byte, time.Time) error
	ClearLoginFailures(context.Context, []byte) error
	TwoFactorStatus(context.Context, uuid.UUID) (TwoFactorStatus, error)
	CreateTOTPEnrollment(context.Context, TOTPEnrollment) error
	TOTPEnrollment(context.Context, uuid.UUID, uuid.UUID) (TOTPEnrollment, error)
	TwoFactorChange(context.Context, uuid.UUID, uuid.UUID) (StoredTwoFactorChange, error)
	ConfirmTOTPEnrollment(context.Context, ConfirmTOTPEnrollmentCommand) error
	ActiveTOTPFactor(context.Context, uuid.UUID) (TOTPFactor, error)
	ConsumeLoginTOTP(context.Context, uuid.UUID, int64, time.Time) (bool, error)
	ConsumeLoginRecoveryCode(context.Context, uuid.UUID, []byte, time.Time) (bool, error)
	RotateRecoveryCodes(context.Context, RotateRecoveryCodesCommand) (StoredTwoFactorChange, error)
	DisableTOTP(context.Context, DisableTOTPCommand) (StoredTwoFactorChange, error)
}

type TwoFactorServiceConfig struct {
	Now    func() time.Time
	Random io.Reader
	NewID  func() uuid.UUID
}

// TwoFactorService is the only component that sees decrypted TOTP seeds or raw
// recovery codes. Results crossing into Core contain only redacted state or the
// one-time values that must be shown to the owning user.
type TwoFactorService struct {
	repository TwoFactorRepository
	protector  *SecretProtector
	hmacKey    []byte
	now        func() time.Time
	random     io.Reader
	newID      func() uuid.UUID
}

func NewTwoFactorService(repository TwoFactorRepository, protector *SecretProtector, hmacKey []byte, config TwoFactorServiceConfig) (*TwoFactorService, error) {
	if repository == nil || protector == nil {
		return nil, errors.New("two-factor service dependencies are required")
	}
	if len(hmacKey) < sha256.Size {
		return nil, errors.New("two-factor HMAC key must contain at least 32 bytes")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.NewID == nil {
		config.NewID = uuid.New
	}
	return &TwoFactorService{
		repository: repository,
		protector:  protector,
		hmacKey:    append([]byte(nil), hmacKey...),
		now:        config.Now,
		random:     config.Random,
		newID:      config.NewID,
	}, nil
}

func (service *TwoFactorService) Status(ctx context.Context, credentialRef uuid.UUID) (TwoFactorStatus, error) {
	if credentialRef == uuid.Nil {
		return TwoFactorStatus{}, ErrTwoFactorInput
	}
	return service.repository.TwoFactorStatus(ctx, credentialRef)
}

func (service *TwoFactorService) StartEnrollment(ctx context.Context, credentialRef uuid.UUID, password, accountName string) (TOTPEnrollmentStart, error) {
	accountName = strings.TrimSpace(accountName)
	if credentialRef == uuid.Nil || password == "" || len(password) > maxPasswordBytes ||
		accountName == "" || utf8.RuneCountInString(accountName) > 128 {
		return TOTPEnrollmentStart{}, ErrTwoFactorInput
	}
	now := service.now().UTC()
	if err := service.verifyCurrentPassword(ctx, credentialRef, password, now); err != nil {
		return TOTPEnrollmentStart{}, err
	}
	status, err := service.repository.TwoFactorStatus(ctx, credentialRef)
	if err != nil {
		return TOTPEnrollmentStart{}, fmt.Errorf("read two-factor status before enrollment: %w", err)
	}
	if status.Enabled {
		return TOTPEnrollmentStart{}, ErrTwoFactorAlreadyEnabled
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer: totpIssuer, AccountName: accountName, Period: totpPeriodSeconds,
		SecretSize: 20, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
		Rand: service.random,
	})
	if err != nil {
		return TOTPEnrollmentStart{}, fmt.Errorf("generate TOTP key: %w", err)
	}
	enrollmentID := service.newID()
	if enrollmentID == uuid.Nil {
		return TOTPEnrollmentStart{}, errors.New("TOTP enrollment id generator returned nil")
	}
	protected, err := service.protector.Protect(totpSecretRecordKind, credentialRef, enrollmentID, []byte(key.Secret()))
	if err != nil {
		return TOTPEnrollmentStart{}, fmt.Errorf("protect TOTP enrollment secret: %w", err)
	}
	expiresAt := now.Add(totpEnrollmentTTL)
	if err := service.repository.CreateTOTPEnrollment(ctx, TOTPEnrollment{
		ID: enrollmentID, CredentialRef: credentialRef, Secret: protected,
		CreatedAt: now, ExpiresAt: expiresAt,
	}); err != nil {
		return TOTPEnrollmentStart{}, err
	}
	if err := service.clearSensitiveActionFailures(ctx, credentialRef); err != nil {
		return TOTPEnrollmentStart{}, err
	}
	return TOTPEnrollmentStart{
		EnrollmentID:    enrollmentID,
		Secret:          key.Secret(),
		ProvisioningURI: key.URL(),
		ExpiresAt:       expiresAt,
	}, nil
}

func (service *TwoFactorService) ConfirmEnrollment(ctx context.Context, credentialRef, enrollmentID uuid.UUID, code string) (TOTPEnrollmentConfirmation, error) {
	if credentialRef == uuid.Nil || enrollmentID == uuid.Nil || !isSixDigitCode(strings.TrimSpace(code)) {
		return TOTPEnrollmentConfirmation{}, ErrTwoFactorInput
	}
	now := service.now().UTC()
	bucket := sensitiveActionBucketHMAC(service.hmacKey, credentialRef)
	if err := service.requireSensitiveActionAvailable(ctx, bucket, now); err != nil {
		return TOTPEnrollmentConfirmation{}, err
	}
	enrollment, err := service.repository.TOTPEnrollment(ctx, credentialRef, enrollmentID)
	if err != nil {
		return TOTPEnrollmentConfirmation{}, err
	}
	if enrollment.ConfirmedAt != nil {
		codes, bundleErr := service.recoveryBundle(enrollment, now)
		if bundleErr != nil {
			return TOTPEnrollmentConfirmation{}, bundleErr
		}
		if err := service.clearSensitiveActionFailures(ctx, credentialRef); err != nil {
			return TOTPEnrollmentConfirmation{}, err
		}
		return TOTPEnrollmentConfirmation{ChangeID: enrollment.ID, EnabledAt: enrollment.ConfirmedAt.UTC(), RecoveryCodes: codes}, nil
	}
	if enrollment.SupersededAt != nil || !enrollment.ExpiresAt.After(now) {
		return TOTPEnrollmentConfirmation{}, ErrTwoFactorEnrollmentNotFound
	}
	secret, err := service.unprotectTOTPSecret(enrollment.CredentialRef, enrollment.ID, enrollment.Secret)
	if err != nil {
		return TOTPEnrollmentConfirmation{}, err
	}
	if _, err := matchingTOTPStep(secret, code, now); err != nil {
		if recordErr := service.repository.RecordLoginFailure(ctx, bucket, now); recordErr != nil {
			return TOTPEnrollmentConfirmation{}, fmt.Errorf("record TOTP enrollment verification failure: %w", recordErr)
		}
		return TOTPEnrollmentConfirmation{}, ErrTwoFactorVerification
	}
	rawCodes, records, err := service.newRecoveryCodes(credentialRef)
	if err != nil {
		return TOTPEnrollmentConfirmation{}, err
	}
	bundleJSON, err := json.Marshal(rawCodes)
	if err != nil {
		return TOTPEnrollmentConfirmation{}, fmt.Errorf("encode recovery code bundle: %w", err)
	}
	protectedBundle, err := service.protector.Protect(recoveryBundleRecordKind, credentialRef, enrollmentID, bundleJSON)
	if err != nil {
		return TOTPEnrollmentConfirmation{}, fmt.Errorf("protect recovery code bundle: %w", err)
	}
	command := ConfirmTOTPEnrollmentCommand{
		Enrollment: enrollment, ConfirmedAt: now, GenerationID: service.newID(),
		RecoveryCodes: records, RecoveryBundle: protectedBundle,
		RecoveryBundleExpiresAt: now.Add(recoveryBundleTTL),
	}
	if command.GenerationID == uuid.Nil {
		return TOTPEnrollmentConfirmation{}, errors.New("recovery code generation id generator returned nil")
	}
	if err := service.repository.ConfirmTOTPEnrollment(ctx, command); err != nil {
		return TOTPEnrollmentConfirmation{}, err
	}
	if err := service.repository.ClearLoginFailures(ctx, bucket); err != nil {
		return TOTPEnrollmentConfirmation{}, fmt.Errorf("clear TOTP enrollment verification failures: %w", err)
	}
	return TOTPEnrollmentConfirmation{ChangeID: enrollment.ID, EnabledAt: now, RecoveryCodes: rawCodes}, nil
}

// VerifyForLogin returns false when no factor is enabled. When enabled it
// atomically consumes either a fresh TOTP time step or one recovery code.
func (service *TwoFactorService) VerifyForLogin(ctx context.Context, credentialRef uuid.UUID, code string, now time.Time) (bool, error) {
	factor, err := service.repository.ActiveTOTPFactor(ctx, credentialRef)
	if errors.Is(err, ErrTwoFactorNotEnabled) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(code) == "" {
		return true, ErrSecondFactorRequired
	}
	evidence, err := service.factorEvidence(factor, code, now.UTC())
	if err != nil {
		return true, ErrTwoFactorVerification
	}
	var consumed bool
	switch evidence.Kind {
	case SecondFactorEvidenceTOTP:
		consumed, err = service.repository.ConsumeLoginTOTP(ctx, credentialRef, evidence.TOTPTimeStep, now.UTC())
	case SecondFactorEvidenceRecovery:
		consumed, err = service.repository.ConsumeLoginRecoveryCode(ctx, credentialRef, evidence.RecoveryCodeHMAC, now.UTC())
	default:
		return true, ErrTwoFactorVerification
	}
	if err != nil {
		return true, err
	}
	if !consumed {
		return true, ErrTwoFactorVerification
	}
	return true, nil
}

func (service *TwoFactorService) RotateRecoveryCodes(ctx context.Context, credentialRef, changeID uuid.UUID, password, code string) (TwoFactorChange, error) {
	if credentialRef == uuid.Nil || changeID == uuid.Nil || password == "" || len(password) > maxPasswordBytes || strings.TrimSpace(code) == "" {
		return TwoFactorChange{}, ErrTwoFactorInput
	}
	now := service.now().UTC()
	if err := service.verifyCurrentPassword(ctx, credentialRef, password, now); err != nil {
		return TwoFactorChange{}, err
	}
	if existing, err := service.repository.TwoFactorChange(ctx, credentialRef, changeID); err == nil {
		result, resultErr := service.rotationChangeResult(existing, now)
		if resultErr != nil {
			return TwoFactorChange{}, resultErr
		}
		if err := service.clearSensitiveActionFailures(ctx, credentialRef); err != nil {
			return TwoFactorChange{}, err
		}
		return result, nil
	} else if !errors.Is(err, ErrTwoFactorChangeNotFound) {
		return TwoFactorChange{}, err
	}
	factor, err := service.repository.ActiveTOTPFactor(ctx, credentialRef)
	if err != nil {
		return TwoFactorChange{}, err
	}
	evidence, err := service.factorEvidence(factor, code, now)
	if err != nil {
		return TwoFactorChange{}, service.recordSensitiveVerificationFailure(ctx, credentialRef, now)
	}
	rawCodes, records, err := service.newRecoveryCodes(credentialRef)
	if err != nil {
		return TwoFactorChange{}, err
	}
	bundleJSON, err := json.Marshal(rawCodes)
	if err != nil {
		return TwoFactorChange{}, fmt.Errorf("encode rotated recovery code bundle: %w", err)
	}
	protectedBundle, err := service.protector.Protect(rotationBundleRecordKind, credentialRef, changeID, bundleJSON)
	if err != nil {
		return TwoFactorChange{}, fmt.Errorf("protect rotated recovery code bundle: %w", err)
	}
	command := RotateRecoveryCodesCommand{
		CredentialRef: credentialRef, ChangeID: changeID, ChangedAt: now,
		Evidence: evidence, GenerationID: service.newID(), RecoveryCodes: records,
		RecoveryBundle: protectedBundle, RecoveryBundleExpiresAt: now.Add(recoveryBundleTTL),
	}
	if command.GenerationID == uuid.Nil {
		return TwoFactorChange{}, errors.New("two-factor change id generator returned nil")
	}
	stored, err := service.repository.RotateRecoveryCodes(ctx, command)
	if err != nil {
		if errors.Is(err, ErrTwoFactorVerification) {
			return TwoFactorChange{}, service.recordSensitiveVerificationFailure(ctx, credentialRef, now)
		}
		return TwoFactorChange{}, err
	}
	if err := service.clearSensitiveActionFailures(ctx, credentialRef); err != nil {
		return TwoFactorChange{}, err
	}
	return service.rotationChangeResult(stored, now)
}

func (service *TwoFactorService) Disable(ctx context.Context, credentialRef, changeID uuid.UUID, password, code string) (TwoFactorChange, error) {
	if credentialRef == uuid.Nil || changeID == uuid.Nil || password == "" || len(password) > maxPasswordBytes || strings.TrimSpace(code) == "" {
		return TwoFactorChange{}, ErrTwoFactorInput
	}
	now := service.now().UTC()
	if err := service.verifyCurrentPassword(ctx, credentialRef, password, now); err != nil {
		return TwoFactorChange{}, err
	}
	if existing, err := service.repository.TwoFactorChange(ctx, credentialRef, changeID); err == nil {
		if existing.Kind != TwoFactorChangeDisabled {
			return TwoFactorChange{}, ErrTwoFactorIdempotencyConflict
		}
		if err := service.clearSensitiveActionFailures(ctx, credentialRef); err != nil {
			return TwoFactorChange{}, err
		}
		return TwoFactorChange{ChangeID: existing.ID, ChangedAt: existing.ChangedAt}, nil
	} else if !errors.Is(err, ErrTwoFactorChangeNotFound) {
		return TwoFactorChange{}, err
	}
	factor, err := service.repository.ActiveTOTPFactor(ctx, credentialRef)
	if err != nil {
		return TwoFactorChange{}, err
	}
	evidence, err := service.factorEvidence(factor, code, now)
	if err != nil {
		return TwoFactorChange{}, service.recordSensitiveVerificationFailure(ctx, credentialRef, now)
	}
	command := DisableTOTPCommand{
		CredentialRef: credentialRef, ChangeID: changeID, ChangedAt: now, Evidence: evidence,
	}
	stored, err := service.repository.DisableTOTP(ctx, command)
	if err != nil {
		if errors.Is(err, ErrTwoFactorVerification) {
			return TwoFactorChange{}, service.recordSensitiveVerificationFailure(ctx, credentialRef, now)
		}
		return TwoFactorChange{}, err
	}
	if err := service.clearSensitiveActionFailures(ctx, credentialRef); err != nil {
		return TwoFactorChange{}, err
	}
	if stored.Kind != TwoFactorChangeDisabled {
		return TwoFactorChange{}, ErrTwoFactorIdempotencyConflict
	}
	return TwoFactorChange{ChangeID: stored.ID, ChangedAt: stored.ChangedAt}, nil
}

func (service *TwoFactorService) rotationChangeResult(change StoredTwoFactorChange, now time.Time) (TwoFactorChange, error) {
	if change.Kind != TwoFactorChangeRecoveryCodesRotated {
		return TwoFactorChange{}, ErrTwoFactorIdempotencyConflict
	}
	if change.RecoveryBundle == nil || change.RecoveryBundleExpiresAt == nil || !change.RecoveryBundleExpiresAt.After(now) {
		return TwoFactorChange{}, ErrRecoveryCodeBundleUnavailable
	}
	plaintext, err := service.protector.Unprotect(rotationBundleRecordKind, change.CredentialRef, change.ID, *change.RecoveryBundle)
	if err != nil {
		return TwoFactorChange{}, fmt.Errorf("unprotect rotated recovery code bundle: %w", err)
	}
	var codes []string
	if err := json.Unmarshal(plaintext, &codes); err != nil || len(codes) != recoveryCodeCount {
		return TwoFactorChange{}, errors.New("decrypted rotated recovery code bundle is invalid")
	}
	return TwoFactorChange{ChangeID: change.ID, ChangedAt: change.ChangedAt, RecoveryCodes: codes}, nil
}

func (service *TwoFactorService) verifyCurrentPassword(ctx context.Context, credentialRef uuid.UUID, password string, now time.Time) error {
	bucket := sensitiveActionBucketHMAC(service.hmacKey, credentialRef)
	if err := service.requireSensitiveActionAvailable(ctx, bucket, now); err != nil {
		return err
	}
	credential, err := service.repository.CredentialByReference(ctx, credentialRef)
	if err != nil {
		return ErrTwoFactorVerification
	}
	match, _, err := VerifyPassword(credential.PasswordHash, password)
	if err != nil {
		return fmt.Errorf("verify current password for two-factor action: %w", err)
	}
	if !match {
		if err := service.repository.RecordLoginFailure(ctx, bucket, now); err != nil {
			return fmt.Errorf("record two-factor password verification failure: %w", err)
		}
		return ErrTwoFactorVerification
	}
	return nil
}

func (service *TwoFactorService) factorEvidence(factor TOTPFactor, code string, now time.Time) (SecondFactorEvidence, error) {
	code = strings.TrimSpace(code)
	if isSixDigitCode(code) {
		secret, err := service.unprotectTOTPSecret(factor.CredentialRef, factor.EnrollmentID, factor.Secret)
		if err != nil {
			return SecondFactorEvidence{}, err
		}
		step, err := matchingTOTPStep(secret, code, now)
		if err != nil {
			return SecondFactorEvidence{}, err
		}
		return SecondFactorEvidence{Kind: SecondFactorEvidenceTOTP, TOTPTimeStep: step}, nil
	}
	canonical, err := canonicalRecoveryCode(code)
	if err != nil {
		return SecondFactorEvidence{}, err
	}
	return SecondFactorEvidence{
		Kind:             SecondFactorEvidenceRecovery,
		RecoveryCodeHMAC: recoveryCodeHMAC(service.hmacKey, factor.CredentialRef, canonical),
	}, nil
}

func (service *TwoFactorService) unprotectTOTPSecret(credentialRef, enrollmentID uuid.UUID, protected ProtectedSecret) (string, error) {
	plaintext, err := service.protector.Unprotect(totpSecretRecordKind, credentialRef, enrollmentID, protected)
	if err != nil {
		return "", fmt.Errorf("unprotect TOTP secret: %w", err)
	}
	secret := string(plaintext)
	if len(secret) < 16 || len(secret) > 128 {
		return "", errors.New("decrypted TOTP secret has invalid length")
	}
	return secret, nil
}

func (service *TwoFactorService) recoveryBundle(enrollment TOTPEnrollment, now time.Time) ([]string, error) {
	if enrollment.RecoveryBundle == nil || enrollment.RecoveryBundleExpiresAt == nil || !enrollment.RecoveryBundleExpiresAt.After(now) {
		return nil, ErrRecoveryCodeBundleUnavailable
	}
	plaintext, err := service.protector.Unprotect(recoveryBundleRecordKind, enrollment.CredentialRef, enrollment.ID, *enrollment.RecoveryBundle)
	if err != nil {
		return nil, fmt.Errorf("unprotect recovery code bundle: %w", err)
	}
	var codes []string
	if err := json.Unmarshal(plaintext, &codes); err != nil || len(codes) != recoveryCodeCount {
		return nil, errors.New("decrypted recovery code bundle is invalid")
	}
	return codes, nil
}

func (service *TwoFactorService) newRecoveryCodes(credentialRef uuid.UUID) ([]string, []RecoveryCodeRecord, error) {
	raw := make([]string, 0, recoveryCodeCount)
	records := make([]RecoveryCodeRecord, 0, recoveryCodeCount)
	for ordinal := 1; ordinal <= recoveryCodeCount; ordinal++ {
		bytes := make([]byte, 12)
		if _, err := io.ReadFull(service.random, bytes); err != nil {
			return nil, nil, fmt.Errorf("generate recovery code: %w", err)
		}
		for index := range bytes {
			bytes[index] = recoveryCodeAlphabet[int(bytes[index]&31)]
		}
		canonical := string(bytes)
		display := canonical[:4] + "-" + canonical[4:8] + "-" + canonical[8:]
		raw = append(raw, display)
		records = append(records, RecoveryCodeRecord{
			Ordinal: int16(ordinal), CodeHMAC: recoveryCodeHMAC(service.hmacKey, credentialRef, canonical),
		})
	}
	return raw, records, nil
}

func (service *TwoFactorService) requireSensitiveActionAvailable(ctx context.Context, bucket []byte, now time.Time) error {
	blockedUntil, err := service.repository.LoginBlockedUntil(ctx, bucket, now)
	if err != nil {
		return fmt.Errorf("check two-factor action throttle: %w", err)
	}
	if blockedUntil.After(now) {
		return &LoginThrottleError{RetryAt: blockedUntil.UTC()}
	}
	return nil
}

func (service *TwoFactorService) recordSensitiveVerificationFailure(ctx context.Context, credentialRef uuid.UUID, now time.Time) error {
	bucket := sensitiveActionBucketHMAC(service.hmacKey, credentialRef)
	if err := service.repository.RecordLoginFailure(ctx, bucket, now); err != nil {
		return fmt.Errorf("record two-factor verification failure: %w", err)
	}
	return ErrTwoFactorVerification
}

func (service *TwoFactorService) clearSensitiveActionFailures(ctx context.Context, credentialRef uuid.UUID) error {
	if err := service.repository.ClearLoginFailures(ctx, sensitiveActionBucketHMAC(service.hmacKey, credentialRef)); err != nil {
		return fmt.Errorf("clear two-factor action failures: %w", err)
	}
	return nil
}

func matchingTOTPStep(secret, code string, now time.Time) (int64, error) {
	if !isSixDigitCode(strings.TrimSpace(code)) || now.Unix() < 0 {
		return 0, ErrTwoFactorVerification
	}
	current := now.Unix() / totpPeriodSeconds
	for _, candidate := range []int64{current, current - 1, current + 1} {
		if candidate < 0 {
			continue
		}
		valid, err := hotp.ValidateCustom(code, uint64(candidate), secret, hotp.ValidateOpts{
			Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
		})
		if err != nil {
			return 0, ErrTwoFactorVerification
		}
		if valid {
			return candidate, nil
		}
	}
	return 0, ErrTwoFactorVerification
}

func canonicalRecoveryCode(value string) (string, error) {
	canonical := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(value), "-", ""))
	if len(canonical) != 12 {
		return "", ErrTwoFactorVerification
	}
	for _, character := range canonical {
		if !strings.ContainsRune(recoveryCodeAlphabet, character) {
			return "", ErrTwoFactorVerification
		}
	}
	return canonical, nil
}

func isSixDigitCode(value string) bool {
	if len(value) != 6 {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func recoveryCodeHMAC(key []byte, credentialRef uuid.UUID, canonical string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(recoveryCodeHMACDomain))
	_, _ = mac.Write(credentialRef[:])
	_, _ = mac.Write([]byte(canonical))
	return mac.Sum(nil)
}

func sensitiveActionBucketHMAC(key []byte, credentialRef uuid.UUID) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(sensitiveActionBucketDomain))
	_, _ = mac.Write(credentialRef[:])
	return mac.Sum(nil)
}
