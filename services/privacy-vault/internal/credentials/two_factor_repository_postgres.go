package credentials

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/peergo/peergo/services/privacy-vault/internal/generated/vaultdb"
)

func (repository *PostgresRepository) CredentialByReference(ctx context.Context, credentialRef uuid.UUID) (Credential, error) {
	row, err := repository.queries.GetCredentialByReference(ctx, credentialRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return Credential{}, ErrInvalidCredentials
	}
	if err != nil {
		return Credential{}, fmt.Errorf("load credential by reference: %w", err)
	}
	return Credential{Reference: row.CredentialRef, PasswordHash: row.PasswordHash}, nil
}

func (repository *PostgresRepository) TwoFactorStatus(ctx context.Context, credentialRef uuid.UUID) (TwoFactorStatus, error) {
	row, err := repository.queries.GetTwoFactorStatus(ctx, credentialRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return TwoFactorStatus{}, ErrInvalidCredentials
	}
	if err != nil {
		return TwoFactorStatus{}, fmt.Errorf("read two-factor status: %w", err)
	}
	status := TwoFactorStatus{RecoveryCodesRemaining: row.RecoveryCodesRemaining}
	if row.EnabledAt.Valid {
		enabledAt := row.EnabledAt.Time.UTC()
		status.Enabled = true
		status.EnabledAt = &enabledAt
	}
	return status, nil
}

// CreateTOTPEnrollment locks the credential and active factor before replacing
// a pending enrollment. A working factor is never overwritten by this path.
func (repository *PostgresRepository) CreateTOTPEnrollment(ctx context.Context, enrollment TOTPEnrollment) error {
	tx, err := repository.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin TOTP enrollment creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := vaultdb.New(tx)
	if _, err := queries.LockCredentialForTwoFactor(ctx, enrollment.CredentialRef); errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidCredentials
	} else if err != nil {
		return fmt.Errorf("lock credential for TOTP enrollment: %w", err)
	}
	if _, err := queries.GetActiveTOTPFactorForUpdate(ctx, enrollment.CredentialRef); err == nil {
		return ErrTwoFactorAlreadyEnabled
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("check active TOTP factor: %w", err)
	}
	if err := queries.SupersedePendingTOTPEnrollments(ctx, vaultdb.SupersedePendingTOTPEnrollmentsParams{
		SupersededAt: vaultTimestamp(enrollment.CreatedAt), CredentialRef: enrollment.CredentialRef,
	}); err != nil {
		return fmt.Errorf("supersede pending TOTP enrollments: %w", err)
	}
	if err := queries.InsertTOTPEnrollment(ctx, vaultdb.InsertTOTPEnrollmentParams{
		ID: enrollment.ID, CredentialRef: enrollment.CredentialRef,
		SecretCiphertext: enrollment.Secret.Ciphertext, SecretNonce: enrollment.Secret.Nonce,
		KeyEpoch: enrollment.Secret.KeyEpoch, CreatedAt: vaultTimestamp(enrollment.CreatedAt),
		ExpiresAt: vaultTimestamp(enrollment.ExpiresAt),
	}); err != nil {
		return fmt.Errorf("insert TOTP enrollment: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit TOTP enrollment creation: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) TOTPEnrollment(ctx context.Context, credentialRef, enrollmentID uuid.UUID) (TOTPEnrollment, error) {
	row, err := repository.queries.GetTOTPEnrollment(ctx, vaultdb.GetTOTPEnrollmentParams{
		ID: enrollmentID, CredentialRef: credentialRef,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return TOTPEnrollment{}, ErrTwoFactorEnrollmentNotFound
	}
	if err != nil {
		return TOTPEnrollment{}, fmt.Errorf("load TOTP enrollment: %w", err)
	}
	return mapTOTPEnrollment(row)
}

func (repository *PostgresRepository) TwoFactorChange(ctx context.Context, credentialRef, changeID uuid.UUID) (StoredTwoFactorChange, error) {
	row, err := repository.queries.GetTOTPChange(ctx, changeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredTwoFactorChange{}, ErrTwoFactorChangeNotFound
	}
	if err != nil {
		return StoredTwoFactorChange{}, fmt.Errorf("load TOTP change: %w", err)
	}
	change, err := mapStoredTwoFactorChange(row)
	if err != nil {
		return StoredTwoFactorChange{}, err
	}
	if change.CredentialRef != credentialRef {
		return StoredTwoFactorChange{}, ErrTwoFactorIdempotencyConflict
	}
	return change, nil
}

// ConfirmTOTPEnrollment moves the already encrypted seed into the active row,
// installs one recovery-code generation and persists the retryable encrypted
// display bundle in the same Vault transaction.
func (repository *PostgresRepository) ConfirmTOTPEnrollment(ctx context.Context, command ConfirmTOTPEnrollmentCommand) error {
	tx, err := repository.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin TOTP enrollment confirmation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := vaultdb.New(tx)
	row, err := queries.GetTOTPEnrollmentForUpdate(ctx, vaultdb.GetTOTPEnrollmentForUpdateParams{
		ID: command.Enrollment.ID, CredentialRef: command.Enrollment.CredentialRef,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTwoFactorEnrollmentNotFound
	}
	if err != nil {
		return fmt.Errorf("lock TOTP enrollment: %w", err)
	}
	enrollment, err := mapTOTPEnrollment(row)
	if err != nil {
		return err
	}
	if enrollment.ConfirmedAt != nil {
		return nil
	}
	if enrollment.SupersededAt != nil || !enrollment.ExpiresAt.After(command.ConfirmedAt) {
		return ErrTwoFactorEnrollmentNotFound
	}
	if active, activeErr := queries.GetActiveTOTPFactorForUpdate(ctx, enrollment.CredentialRef); activeErr == nil {
		if active.EnrollmentID != enrollment.ID {
			return ErrTwoFactorAlreadyEnabled
		}
	} else if !errors.Is(activeErr, pgx.ErrNoRows) {
		return fmt.Errorf("lock active TOTP factor: %w", activeErr)
	}
	if err := queries.UpsertTOTPFactor(ctx, vaultdb.UpsertTOTPFactorParams{
		CredentialRef: enrollment.CredentialRef, EnrollmentID: enrollment.ID,
		SecretCiphertext: enrollment.Secret.Ciphertext, SecretNonce: enrollment.Secret.Nonce,
		KeyEpoch: enrollment.Secret.KeyEpoch, EnabledAt: vaultTimestamp(command.ConfirmedAt),
	}); err != nil {
		return fmt.Errorf("activate TOTP factor: %w", err)
	}
	if err := queries.RevokeActiveTOTPRecoveryCodes(ctx, vaultdb.RevokeActiveTOTPRecoveryCodesParams{
		RevokedAt: vaultTimestamp(command.ConfirmedAt), CredentialRef: enrollment.CredentialRef,
	}); err != nil {
		return fmt.Errorf("revoke previous TOTP recovery codes: %w", err)
	}
	if err := insertRecoveryCodes(ctx, queries, enrollment.CredentialRef, command.GenerationID, command.ConfirmedAt, command.RecoveryCodes); err != nil {
		return err
	}
	rows, err := queries.CompleteTOTPEnrollment(ctx, vaultdb.CompleteTOTPEnrollmentParams{
		ConfirmedAt:              vaultTimestamp(command.ConfirmedAt),
		RecoveryBundleCiphertext: command.RecoveryBundle.Ciphertext,
		RecoveryBundleNonce:      command.RecoveryBundle.Nonce,
		RecoveryBundleExpiresAt:  vaultTimestamp(command.RecoveryBundleExpiresAt),
		ID:                       enrollment.ID, CredentialRef: enrollment.CredentialRef,
	})
	if err != nil {
		return fmt.Errorf("complete TOTP enrollment: %w", err)
	}
	if rows != 1 {
		return ErrTwoFactorEnrollmentNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit TOTP enrollment confirmation: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) ActiveTOTPFactor(ctx context.Context, credentialRef uuid.UUID) (TOTPFactor, error) {
	row, err := repository.queries.GetActiveTOTPFactor(ctx, credentialRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return TOTPFactor{}, ErrTwoFactorNotEnabled
	}
	if err != nil {
		return TOTPFactor{}, fmt.Errorf("load active TOTP factor: %w", err)
	}
	return mapTOTPFactor(row.CredentialRef, row.EnrollmentID, row.SecretCiphertext, row.SecretNonce, row.KeyEpoch, row.EnabledAt.Time, row.LastUsedStep)
}

func (repository *PostgresRepository) ConsumeLoginTOTP(ctx context.Context, credentialRef uuid.UUID, step int64, usedAt time.Time) (bool, error) {
	rows, err := repository.queries.AdvanceTOTPTimeStep(ctx, vaultdb.AdvanceTOTPTimeStepParams{
		LastUsedStep: step, UpdatedAt: vaultTimestamp(usedAt), CredentialRef: credentialRef,
	})
	if err != nil {
		return false, fmt.Errorf("consume TOTP time step: %w", err)
	}
	return rows == 1, nil
}

func (repository *PostgresRepository) ConsumeLoginRecoveryCode(ctx context.Context, credentialRef uuid.UUID, codeHMAC []byte, usedAt time.Time) (bool, error) {
	rows, err := repository.queries.ConsumeTOTPRecoveryCode(ctx, vaultdb.ConsumeTOTPRecoveryCodeParams{
		UsedAt: vaultTimestamp(usedAt), CredentialRef: credentialRef, CodeHmac: codeHMAC,
	})
	if err != nil {
		return false, fmt.Errorf("consume TOTP recovery code: %w", err)
	}
	return rows == 1, nil
}

// RotateRecoveryCodes verifies and consumes the supplied second-factor evidence
// under the factor row lock before replacing the active generation.
func (repository *PostgresRepository) RotateRecoveryCodes(ctx context.Context, command RotateRecoveryCodesCommand) (StoredTwoFactorChange, error) {
	return repository.applyFactorChange(ctx, command.CredentialRef, command.ChangeID, TwoFactorChangeRecoveryCodesRotated, command.ChangedAt, command.Evidence, func(queries *vaultdb.Queries) error {
		if err := queries.RevokeActiveTOTPRecoveryCodes(ctx, vaultdb.RevokeActiveTOTPRecoveryCodesParams{
			RevokedAt: vaultTimestamp(command.ChangedAt), CredentialRef: command.CredentialRef,
		}); err != nil {
			return fmt.Errorf("revoke recovery codes before rotation: %w", err)
		}
		if err := insertRecoveryCodes(ctx, queries, command.CredentialRef, command.GenerationID, command.ChangedAt, command.RecoveryCodes); err != nil {
			return err
		}
		return queries.InsertTOTPRecoveryRotationChange(ctx, vaultdb.InsertTOTPRecoveryRotationChangeParams{
			ID: command.ChangeID, CredentialRef: command.CredentialRef,
			ChangedAt:                vaultTimestamp(command.ChangedAt),
			RecoveryBundleCiphertext: command.RecoveryBundle.Ciphertext,
			RecoveryBundleNonce:      command.RecoveryBundle.Nonce,
			RecoveryBundleKeyEpoch:   pgtype.Text{String: command.RecoveryBundle.KeyEpoch, Valid: true},
			RecoveryBundleExpiresAt:  vaultTimestamp(command.RecoveryBundleExpiresAt),
		})
	})
}

func (repository *PostgresRepository) DisableTOTP(ctx context.Context, command DisableTOTPCommand) (StoredTwoFactorChange, error) {
	return repository.applyFactorChange(ctx, command.CredentialRef, command.ChangeID, TwoFactorChangeDisabled, command.ChangedAt, command.Evidence, func(queries *vaultdb.Queries) error {
		rows, err := queries.DisableTOTPFactor(ctx, vaultdb.DisableTOTPFactorParams{
			DisabledAt: vaultTimestamp(command.ChangedAt), CredentialRef: command.CredentialRef,
		})
		if err != nil {
			return fmt.Errorf("disable TOTP factor: %w", err)
		}
		if rows != 1 {
			return ErrTwoFactorNotEnabled
		}
		if err := queries.RevokeActiveTOTPRecoveryCodes(ctx, vaultdb.RevokeActiveTOTPRecoveryCodesParams{
			RevokedAt: vaultTimestamp(command.ChangedAt), CredentialRef: command.CredentialRef,
		}); err != nil {
			return fmt.Errorf("revoke recovery codes after TOTP disable: %w", err)
		}
		return queries.InsertTOTPDisableChange(ctx, vaultdb.InsertTOTPDisableChangeParams{
			ID: command.ChangeID, CredentialRef: command.CredentialRef, ChangedAt: vaultTimestamp(command.ChangedAt),
		})
	})
}

func (repository *PostgresRepository) applyFactorChange(ctx context.Context, credentialRef, changeID uuid.UUID, kind TwoFactorChangeKind, changedAt time.Time, evidence SecondFactorEvidence, apply func(*vaultdb.Queries) error) (StoredTwoFactorChange, error) {
	tx, err := repository.db.Begin(ctx)
	if err != nil {
		return StoredTwoFactorChange{}, fmt.Errorf("begin TOTP factor change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := vaultdb.New(tx)
	// The credential row serializes idempotency lookup with factor mutation. A
	// duplicate request can therefore return the original result without
	// consuming its already-used TOTP or recovery-code evidence a second time.
	if _, err := queries.LockCredentialForTwoFactor(ctx, credentialRef); errors.Is(err, pgx.ErrNoRows) {
		return StoredTwoFactorChange{}, ErrInvalidCredentials
	} else if err != nil {
		return StoredTwoFactorChange{}, fmt.Errorf("lock credential for TOTP change: %w", err)
	}
	if existingRow, existingErr := queries.GetTOTPChange(ctx, changeID); existingErr == nil {
		existing, mapErr := mapStoredTwoFactorChange(existingRow)
		if mapErr != nil {
			return StoredTwoFactorChange{}, mapErr
		}
		if existing.CredentialRef != credentialRef || existing.Kind != kind {
			return StoredTwoFactorChange{}, ErrTwoFactorIdempotencyConflict
		}
		return existing, nil
	} else if !errors.Is(existingErr, pgx.ErrNoRows) {
		return StoredTwoFactorChange{}, fmt.Errorf("check existing TOTP change: %w", existingErr)
	}
	if _, err := queries.GetActiveTOTPFactorForUpdate(ctx, credentialRef); errors.Is(err, pgx.ErrNoRows) {
		return StoredTwoFactorChange{}, ErrTwoFactorNotEnabled
	} else if err != nil {
		return StoredTwoFactorChange{}, fmt.Errorf("lock active TOTP factor for change: %w", err)
	}
	if err := consumeFactorEvidence(ctx, queries, credentialRef, changedAt, evidence); err != nil {
		return StoredTwoFactorChange{}, err
	}
	if err := apply(queries); err != nil {
		return StoredTwoFactorChange{}, err
	}
	row, err := queries.GetTOTPChange(ctx, changeID)
	if err != nil {
		return StoredTwoFactorChange{}, fmt.Errorf("load inserted TOTP change: %w", err)
	}
	stored, err := mapStoredTwoFactorChange(row)
	if err != nil {
		return StoredTwoFactorChange{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return StoredTwoFactorChange{}, fmt.Errorf("commit TOTP factor change: %w", err)
	}
	return stored, nil
}

func consumeFactorEvidence(ctx context.Context, queries *vaultdb.Queries, credentialRef uuid.UUID, usedAt time.Time, evidence SecondFactorEvidence) error {
	var (
		rows int64
		err  error
	)
	switch evidence.Kind {
	case SecondFactorEvidenceTOTP:
		rows, err = queries.AdvanceTOTPTimeStep(ctx, vaultdb.AdvanceTOTPTimeStepParams{
			LastUsedStep: evidence.TOTPTimeStep, UpdatedAt: vaultTimestamp(usedAt), CredentialRef: credentialRef,
		})
	case SecondFactorEvidenceRecovery:
		rows, err = queries.ConsumeTOTPRecoveryCode(ctx, vaultdb.ConsumeTOTPRecoveryCodeParams{
			UsedAt: vaultTimestamp(usedAt), CredentialRef: credentialRef, CodeHmac: evidence.RecoveryCodeHMAC,
		})
	default:
		return ErrTwoFactorVerification
	}
	if err != nil {
		return fmt.Errorf("consume TOTP change evidence: %w", err)
	}
	if rows != 1 {
		return ErrTwoFactorVerification
	}
	return nil
}

func insertRecoveryCodes(ctx context.Context, queries *vaultdb.Queries, credentialRef, generationID uuid.UUID, createdAt time.Time, records []RecoveryCodeRecord) error {
	if len(records) != recoveryCodeCount {
		return errors.New("recovery code generation has invalid size")
	}
	for _, record := range records {
		if err := queries.InsertTOTPRecoveryCode(ctx, vaultdb.InsertTOTPRecoveryCodeParams{
			CredentialRef: credentialRef, GenerationID: generationID, Ordinal: record.Ordinal,
			CodeHmac: record.CodeHMAC, CreatedAt: vaultTimestamp(createdAt),
		}); err != nil {
			return fmt.Errorf("insert TOTP recovery code: %w", err)
		}
	}
	return nil
}

func mapTOTPEnrollment(row vaultdb.VaultTotpEnrollment) (TOTPEnrollment, error) {
	if row.ID == uuid.Nil || row.CredentialRef == uuid.Nil || !row.CreatedAt.Valid || !row.ExpiresAt.Valid {
		return TOTPEnrollment{}, errors.New("stored TOTP enrollment is invalid")
	}
	enrollment := TOTPEnrollment{
		ID: row.ID, CredentialRef: row.CredentialRef,
		Secret:    ProtectedSecret{Ciphertext: row.SecretCiphertext, Nonce: row.SecretNonce, KeyEpoch: row.KeyEpoch},
		CreatedAt: row.CreatedAt.Time.UTC(), ExpiresAt: row.ExpiresAt.Time.UTC(),
	}
	if row.ConfirmedAt.Valid {
		value := row.ConfirmedAt.Time.UTC()
		enrollment.ConfirmedAt = &value
	}
	if row.SupersededAt.Valid {
		value := row.SupersededAt.Time.UTC()
		enrollment.SupersededAt = &value
	}
	if len(row.RecoveryBundleCiphertext) > 0 || len(row.RecoveryBundleNonce) > 0 || row.RecoveryBundleExpiresAt.Valid {
		if len(row.RecoveryBundleCiphertext) == 0 || len(row.RecoveryBundleNonce) == 0 || !row.RecoveryBundleExpiresAt.Valid {
			return TOTPEnrollment{}, errors.New("stored recovery code bundle is incomplete")
		}
		enrollment.RecoveryBundle = &ProtectedSecret{
			Ciphertext: row.RecoveryBundleCiphertext, Nonce: row.RecoveryBundleNonce, KeyEpoch: row.KeyEpoch,
		}
		value := row.RecoveryBundleExpiresAt.Time.UTC()
		enrollment.RecoveryBundleExpiresAt = &value
	}
	return enrollment, nil
}

func mapTOTPFactor(credentialRef, enrollmentID uuid.UUID, ciphertext, nonce []byte, keyEpoch string, enabledAt time.Time, lastUsedStep int64) (TOTPFactor, error) {
	if credentialRef == uuid.Nil || enrollmentID == uuid.Nil || enabledAt.IsZero() || lastUsedStep < -1 {
		return TOTPFactor{}, errors.New("stored TOTP factor is invalid")
	}
	return TOTPFactor{
		CredentialRef: credentialRef, EnrollmentID: enrollmentID,
		Secret:    ProtectedSecret{Ciphertext: ciphertext, Nonce: nonce, KeyEpoch: keyEpoch},
		EnabledAt: enabledAt.UTC(), LastUsedStep: lastUsedStep,
	}, nil
}

func mapStoredTwoFactorChange(row vaultdb.VaultTotpChange) (StoredTwoFactorChange, error) {
	change := StoredTwoFactorChange{
		ID: row.ID, CredentialRef: row.CredentialRef,
		Kind: TwoFactorChangeKind(row.Kind),
	}
	if change.ID == uuid.Nil || change.CredentialRef == uuid.Nil || !row.ChangedAt.Valid ||
		(change.Kind != TwoFactorChangeRecoveryCodesRotated && change.Kind != TwoFactorChangeDisabled) {
		return StoredTwoFactorChange{}, errors.New("stored TOTP change is invalid")
	}
	change.ChangedAt = row.ChangedAt.Time.UTC()
	if change.Kind == TwoFactorChangeDisabled {
		if len(row.RecoveryBundleCiphertext) != 0 || len(row.RecoveryBundleNonce) != 0 || row.RecoveryBundleKeyEpoch.Valid || row.RecoveryBundleExpiresAt.Valid {
			return StoredTwoFactorChange{}, errors.New("stored TOTP disable change contains a recovery bundle")
		}
		return change, nil
	}
	if len(row.RecoveryBundleCiphertext) == 0 || len(row.RecoveryBundleNonce) == 0 || !row.RecoveryBundleKeyEpoch.Valid || !row.RecoveryBundleExpiresAt.Valid {
		return StoredTwoFactorChange{}, errors.New("stored TOTP rotation bundle is incomplete")
	}
	change.RecoveryBundle = &ProtectedSecret{
		Ciphertext: row.RecoveryBundleCiphertext, Nonce: row.RecoveryBundleNonce,
		KeyEpoch: row.RecoveryBundleKeyEpoch.String,
	}
	expiresAt := row.RecoveryBundleExpiresAt.Time.UTC()
	change.RecoveryBundleExpiresAt = &expiresAt
	return change, nil
}

var _ TwoFactorRepository = (*PostgresRepository)(nil)
