package credentials

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/peergo/peergo/services/privacy-vault/internal/generated/vaultdb"
)

// PostgresRepository is the only runtime adapter that can read password hashes.
type PostgresRepository struct {
	db      vaultDB
	queries *vaultdb.Queries
}

type vaultDB interface {
	vaultdb.DBTX
	Begin(context.Context) (pgx.Tx, error)
}

// NewPostgresRepository creates the Vault credential persistence adapter.
func NewPostgresRepository(db vaultDB) *PostgresRepository {
	return &PostgresRepository{db: db, queries: vaultdb.New(db)}
}

func (r *PostgresRepository) IdentifierExists(ctx context.Context, lookup []byte) (bool, error) {
	if len(lookup) != sha256.Size {
		return false, ErrRegistrationInput
	}
	var exists bool
	if err := r.db.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM vault.direct_identifiers WHERE kind = 'email' AND lookup_hmac = $1
	)`, lookup).Scan(&exists); err != nil {
		return false, fmt.Errorf("check direct email identifier: %w", err)
	}
	return exists, nil
}

func (r *PostgresRepository) EmailOperations(ctx context.Context, now time.Time) (EmailOperationsStats, error) {
	if now.IsZero() {
		return EmailOperationsStats{}, errors.New("email operations timestamp is required")
	}
	var result EmailOperationsStats
	err := r.db.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM vault.email_verification_challenges
      WHERE delivery_status = 'pending' AND verified_at IS NULL AND expires_at > $1),
    (SELECT count(*) FROM vault.email_verification_challenges WHERE delivery_status = 'sent'),
    (SELECT count(*) FROM vault.email_verification_challenges WHERE delivery_status = 'failed'),
    (SELECT count(*) FROM vault.email_verification_challenges WHERE verified_at IS NOT NULL),
    (SELECT count(*) FROM vault.password_recovery_challenges
      WHERE delivery_status = 'pending' AND consumed_at IS NULL AND superseded_at IS NULL AND expires_at > $1),
    (SELECT count(*) FROM vault.password_recovery_challenges WHERE delivery_status = 'sent'),
    (SELECT count(*) FROM vault.password_recovery_challenges WHERE delivery_status = 'failed'),
    (SELECT count(*) FROM vault.password_recovery_challenges WHERE consumed_at IS NOT NULL)`, now).Scan(
		&result.VerificationPending,
		&result.VerificationSent,
		&result.VerificationFailed,
		&result.VerificationVerified,
		&result.RecoveryPending,
		&result.RecoverySent,
		&result.RecoveryFailed,
		&result.RecoveryCompleted,
	)
	if err != nil {
		return EmailOperationsStats{}, fmt.Errorf("read email operations status: %w", err)
	}
	return result, nil
}

// ProvisionRegistration implements the idempotent, fail-closed registration
// write. The provision row is inserted first with a deferred credential FK so
// concurrent callers serialize on registration_id before either creates P0
// material. A losing retry can therefore return the winner's opaque reference.
func (r *PostgresRepository) ProvisionRegistration(ctx context.Context, record RegistrationProvisionRecord) (uuid.UUID, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("begin registration credential provision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := vaultdb.New(tx)
	createdAt := vaultTimestamp(record.CreatedAt)

	reserved, err := queries.ReserveRegistrationProvision(ctx, vaultdb.ReserveRegistrationProvisionParams{
		RegistrationID: record.RegistrationID,
		CredentialRef:  record.CredentialRef,
		RequestHmac:    record.RequestHMAC,
		ExpiresAt:      vaultTimestamp(record.ExpiresAt),
		CreatedAt:      createdAt,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, loadErr := queries.GetRegistrationProvisionForUpdate(ctx, record.RegistrationID)
		if loadErr != nil {
			return uuid.Nil, fmt.Errorf("load existing registration provision: %w", loadErr)
		}
		if len(existing.RequestHmac) != len(record.RequestHMAC) || subtle.ConstantTimeCompare(existing.RequestHmac, record.RequestHMAC) != 1 {
			return uuid.Nil, ErrRegistrationIdempotencyConflict
		}
		return existing.CredentialRef, nil
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("reserve registration provision: %w", err)
	}
	if reserved.CredentialRef != record.CredentialRef {
		return uuid.Nil, errors.New("registration provision returned an unexpected credential reference")
	}

	if err := queries.InsertRegistrationCredential(ctx, vaultdb.InsertRegistrationCredentialParams{
		CredentialRef: record.CredentialRef,
		PasswordHash:  record.PasswordHash,
		CreatedAt:     createdAt,
	}); err != nil {
		return uuid.Nil, registrationWriteError("insert registration credential", err)
	}
	for _, identifier := range []vaultdb.InsertRegistrationIdentifierParams{
		{
			CredentialRef: record.CredentialRef,
			Kind:          "username",
			LookupHmac:    record.UsernameLookup,
			MaskedValue:   record.UsernameMasked,
			VerifiedAt:    createdAt,
			CreatedAt:     createdAt,
		},
		{
			CredentialRef: record.CredentialRef,
			Kind:          "email",
			LookupHmac:    record.EmailLookup,
			MaskedValue:   record.EmailMasked,
			VerifiedAt:    pgtype.Timestamptz{},
			CreatedAt:     createdAt,
		},
	} {
		if err := queries.InsertRegistrationIdentifier(ctx, identifier); err != nil {
			return uuid.Nil, registrationWriteError("insert registration identifier", err)
		}
	}
	if err := ensureEmailAddressRow(
		ctx,
		tx,
		record.CredentialRef,
		record.EmailAddress,
		record.CreatedAt,
	); err != nil {
		return uuid.Nil, registrationWriteError("persist registration email address", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, registrationWriteError("commit registration credential provision", err)
	}
	return record.CredentialRef, nil
}

// ActivateRegistration implements the second half of the Vault gate. It is
// idempotent for an already-active provision and rejects expired/invented IDs.
func (r *PostgresRepository) ActivateRegistration(ctx context.Context, registrationID uuid.UUID, activatedAt time.Time) (uuid.UUID, error) {
	row, err := r.queries.ActivateRegistrationProvision(ctx, vaultdb.ActivateRegistrationProvisionParams{
		RegistrationID: registrationID,
		ActivatedAt:    vaultTimestamp(activatedAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrRegistrationProvisionNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("activate registration provision: %w", err)
	}
	if row.Status != "active" || !row.ActivatedAt.Valid {
		return uuid.Nil, errors.New("activated registration provision contains invalid state")
	}
	return row.CredentialRef, nil
}

// ReserveEmailVerification serializes on the owned email identifier before it
// checks cooldown or replaces the live challenge. This lock order prevents two
// concurrent resend requests from both delivering a usable token.
func (r *PostgresRepository) ReserveEmailVerification(ctx context.Context, command ReserveEmailVerificationCommand) (EmailVerificationReservation, error) {
	if command.VerificationID == uuid.Nil || command.CredentialRef == uuid.Nil || len(command.EmailLookup) != 32 || len(command.TokenDigest) != 32 ||
		command.IssuedAt.IsZero() || !command.ExpiresAt.After(command.IssuedAt) || !command.NextRequestAt.After(command.IssuedAt) {
		return EmailVerificationReservation{}, ErrEmailVerificationInput
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return EmailVerificationReservation{}, fmt.Errorf("begin email verification reservation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := vaultdb.New(tx)
	identifier, err := queries.LockEmailIdentifierForVerification(ctx, command.CredentialRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return EmailVerificationReservation{}, ErrEmailVerificationIdentifierMismatch
	}
	if err != nil {
		return EmailVerificationReservation{}, fmt.Errorf("lock email identifier for verification: %w", err)
	}
	if len(identifier.LookupHmac) != len(command.EmailLookup) || subtle.ConstantTimeCompare(identifier.LookupHmac, command.EmailLookup) != 1 {
		return EmailVerificationReservation{}, ErrEmailVerificationIdentifierMismatch
	}
	if identifier.VerifiedAt.Valid {
		existing, challengeErr := queries.GetEmailVerificationChallengeForUpdate(ctx, command.CredentialRef)
		if challengeErr != nil || !existing.VerifiedAt.Valid {
			return EmailVerificationReservation{}, ErrEmailVerificationStateConflict
		}
		verifiedAt := identifier.VerifiedAt.Time.UTC()
		return EmailVerificationReservation{
			VerificationID: existing.ID, CredentialRef: command.CredentialRef, AlreadyVerified: true,
			VerifiedAt: &verifiedAt, NextRequestAt: command.IssuedAt,
		}, nil
	}

	existing, err := queries.GetEmailVerificationChallengeForUpdate(ctx, command.CredentialRef)
	if err == nil {
		if !existing.NextRequestAt.Valid {
			return EmailVerificationReservation{}, ErrEmailVerificationStateConflict
		}
		if existing.NextRequestAt.Time.After(command.IssuedAt) {
			return EmailVerificationReservation{}, &EmailVerificationCooldownError{NextRequestAt: existing.NextRequestAt.Time.UTC()}
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return EmailVerificationReservation{}, fmt.Errorf("lock existing email verification challenge: %w", err)
	}

	if err := queries.UpsertEmailVerificationChallenge(ctx, vaultdb.UpsertEmailVerificationChallengeParams{
		ID: command.VerificationID, CredentialRef: command.CredentialRef,
		TokenSha256: command.TokenDigest, EmailLookupHmac: command.EmailLookup,
		IssuedAt: vaultTimestamp(command.IssuedAt), ExpiresAt: vaultTimestamp(command.ExpiresAt),
		NextRequestAt: vaultTimestamp(command.NextRequestAt),
	}); err != nil {
		return EmailVerificationReservation{}, fmt.Errorf("upsert email verification challenge: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return EmailVerificationReservation{}, fmt.Errorf("commit email verification reservation: %w", err)
	}
	return EmailVerificationReservation{
		VerificationID: command.VerificationID,
		CredentialRef:  command.CredentialRef,
		NextRequestAt:  command.NextRequestAt.UTC(),
	}, nil
}

func (r *PostgresRepository) MarkEmailVerificationDelivered(ctx context.Context, verificationID uuid.UUID, deliveredAt time.Time) error {
	rows, err := r.queries.MarkEmailVerificationDelivered(ctx, vaultdb.MarkEmailVerificationDeliveredParams{
		DeliveredAt: vaultTimestamp(deliveredAt), ID: verificationID,
	})
	if err != nil {
		return fmt.Errorf("mark email verification delivered: %w", err)
	}
	if rows != 1 {
		return ErrEmailVerificationStateConflict
	}
	return nil
}

func (r *PostgresRepository) MarkEmailVerificationDeliveryFailed(ctx context.Context, verificationID uuid.UUID) error {
	rows, err := r.queries.MarkEmailVerificationDeliveryFailed(ctx, verificationID)
	if err != nil {
		return fmt.Errorf("mark email verification delivery failed: %w", err)
	}
	if rows != 1 {
		return ErrEmailVerificationStateConflict
	}
	return nil
}

// ConfirmEmailVerification keeps a consumed token idempotent. Vault may commit
// before Core is reachable; returning the same opaque credential reference on
// retry lets Core finish its own projection and audit transaction safely.
func (r *PostgresRepository) ConfirmEmailVerification(ctx context.Context, tokenDigest []byte, verifiedAt time.Time) (EmailVerificationConfirmation, error) {
	if len(tokenDigest) != 32 || verifiedAt.IsZero() {
		return EmailVerificationConfirmation{}, ErrEmailVerificationInput
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return EmailVerificationConfirmation{}, fmt.Errorf("begin email verification confirmation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := vaultdb.New(tx)
	challenge, err := queries.GetEmailVerificationChallengeByTokenForUpdate(ctx, tokenDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return EmailVerificationConfirmation{}, ErrEmailVerificationTokenInvalid
	}
	if err != nil {
		return EmailVerificationConfirmation{}, fmt.Errorf("lock email verification challenge by token: %w", err)
	}
	if challenge.VerifiedAt.Valid {
		return EmailVerificationConfirmation{
			VerificationID: challenge.ID,
			CredentialRef:  challenge.CredentialRef,
			VerifiedAt:     challenge.VerifiedAt.Time.UTC(),
		}, nil
	}
	if !challenge.ExpiresAt.Valid || !challenge.ExpiresAt.Time.After(verifiedAt) || len(challenge.EmailLookupHmac) != 32 {
		return EmailVerificationConfirmation{}, ErrEmailVerificationTokenInvalid
	}
	persistedVerifiedAt, err := queries.MarkEmailIdentifierVerified(ctx, vaultdb.MarkEmailIdentifierVerifiedParams{
		VerifiedAt: vaultTimestamp(verifiedAt), CredentialRef: challenge.CredentialRef,
		EmailLookupHmac: challenge.EmailLookupHmac,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return EmailVerificationConfirmation{}, ErrEmailVerificationStateConflict
	}
	if err != nil {
		return EmailVerificationConfirmation{}, fmt.Errorf("mark email identifier verified: %w", err)
	}
	if !persistedVerifiedAt.Valid {
		return EmailVerificationConfirmation{}, ErrEmailVerificationStateConflict
	}
	completedAt, err := queries.CompleteEmailVerificationChallenge(ctx, vaultdb.CompleteEmailVerificationChallengeParams{
		VerifiedAt: persistedVerifiedAt, ID: challenge.ID,
	})
	if err != nil {
		return EmailVerificationConfirmation{}, fmt.Errorf("complete email verification challenge: %w", err)
	}
	if !completedAt.Valid || !completedAt.Time.Equal(persistedVerifiedAt.Time) {
		return EmailVerificationConfirmation{}, ErrEmailVerificationStateConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return EmailVerificationConfirmation{}, fmt.Errorf("commit email verification confirmation: %w", err)
	}
	return EmailVerificationConfirmation{
		VerificationID: challenge.ID,
		CredentialRef:  challenge.CredentialRef,
		VerifiedAt:     persistedVerifiedAt.Time.UTC(),
	}, nil
}

// ReservePasswordRecovery locks the verified email identifier before reading
// cooldown state or superseding a live challenge. Unknown and unverified
// addresses commit no challenge and return the same non-issuing reservation.
func (r *PostgresRepository) ReservePasswordRecovery(ctx context.Context, command ReservePasswordRecoveryCommand) (PasswordRecoveryReservation, error) {
	if command.RecoveryID == uuid.Nil || len(command.EmailLookup) != 32 || len(command.TokenDigest) != 32 ||
		command.IssuedAt.IsZero() || !command.ExpiresAt.After(command.IssuedAt) || !command.NextIssueAt.After(command.IssuedAt) {
		return PasswordRecoveryReservation{}, ErrPasswordRecoveryInput
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return PasswordRecoveryReservation{}, fmt.Errorf("begin password recovery reservation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := vaultdb.New(tx)
	identifier, err := queries.LockVerifiedEmailIdentifierByLookup(ctx, command.EmailLookup)
	if errors.Is(err, pgx.ErrNoRows) {
		return PasswordRecoveryReservation{
			RecoveryID: command.RecoveryID, Issue: false,
			AcceptedAt: command.IssuedAt.UTC(), NextIssueAt: command.NextIssueAt.UTC(),
		}, nil
	}
	if err != nil {
		return PasswordRecoveryReservation{}, fmt.Errorf("lock verified email for password recovery: %w", err)
	}
	if len(identifier.LookupHmac) != 32 || subtle.ConstantTimeCompare(identifier.LookupHmac, command.EmailLookup) != 1 {
		return PasswordRecoveryReservation{}, ErrPasswordRecoveryStateConflict
	}
	limit, err := queries.GetPasswordRecoveryRateLimitForUpdate(ctx, identifier.CredentialRef)
	if err == nil && limit.NextIssueAt.Valid && limit.NextIssueAt.Time.After(command.IssuedAt) {
		return PasswordRecoveryReservation{
			RecoveryID: command.RecoveryID, Issue: false,
			AcceptedAt: command.IssuedAt.UTC(), NextIssueAt: limit.NextIssueAt.Time.UTC(),
		}, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return PasswordRecoveryReservation{}, fmt.Errorf("lock password recovery cooldown: %w", err)
	}
	if err := queries.UpsertPasswordRecoveryRateLimit(ctx, vaultdb.UpsertPasswordRecoveryRateLimitParams{
		CredentialRef: identifier.CredentialRef,
		NextIssueAt:   vaultTimestamp(command.NextIssueAt),
		UpdatedAt:     vaultTimestamp(command.IssuedAt),
	}); err != nil {
		return PasswordRecoveryReservation{}, fmt.Errorf("update password recovery cooldown: %w", err)
	}
	if err := queries.SupersedeLivePasswordRecoveryChallenges(ctx, vaultdb.SupersedeLivePasswordRecoveryChallengesParams{
		SupersededAt: vaultTimestamp(command.IssuedAt), CredentialRef: identifier.CredentialRef,
	}); err != nil {
		return PasswordRecoveryReservation{}, fmt.Errorf("supersede password recovery challenge: %w", err)
	}
	if err := queries.InsertPasswordRecoveryChallenge(ctx, vaultdb.InsertPasswordRecoveryChallengeParams{
		ID: command.RecoveryID, CredentialRef: identifier.CredentialRef,
		TokenSha256: command.TokenDigest, EmailLookupHmac: command.EmailLookup,
		IssuedAt: vaultTimestamp(command.IssuedAt), ExpiresAt: vaultTimestamp(command.ExpiresAt),
	}); err != nil {
		return PasswordRecoveryReservation{}, fmt.Errorf("insert password recovery challenge: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PasswordRecoveryReservation{}, fmt.Errorf("commit password recovery reservation: %w", err)
	}
	return PasswordRecoveryReservation{
		RecoveryID: command.RecoveryID, Issue: true,
		AcceptedAt: command.IssuedAt.UTC(), NextIssueAt: command.NextIssueAt.UTC(),
	}, nil
}

func (r *PostgresRepository) MarkPasswordRecoveryDelivered(ctx context.Context, recoveryID uuid.UUID, deliveredAt time.Time) error {
	rows, err := r.queries.MarkPasswordRecoveryDelivered(ctx, vaultdb.MarkPasswordRecoveryDeliveredParams{
		DeliveredAt: vaultTimestamp(deliveredAt), ID: recoveryID,
	})
	if err != nil {
		return fmt.Errorf("mark password recovery delivered: %w", err)
	}
	if rows != 1 {
		return ErrPasswordRecoveryStateConflict
	}
	return nil
}

func (r *PostgresRepository) MarkPasswordRecoveryDeliveryFailed(ctx context.Context, recoveryID uuid.UUID) error {
	rows, err := r.queries.MarkPasswordRecoveryDeliveryFailed(ctx, recoveryID)
	if err != nil {
		return fmt.Errorf("mark password recovery delivery failed: %w", err)
	}
	if rows != 1 {
		return ErrPasswordRecoveryStateConflict
	}
	return nil
}

// InspectPasswordRecovery rejects random token-shaped input before Argon2id is
// invoked. Completion repeats every state check under the identifier/challenge
// locks, so this preflight never authorizes a stale or superseded token.
func (r *PostgresRepository) InspectPasswordRecovery(ctx context.Context, tokenDigest []byte, asOf time.Time) (PasswordRecoveryInspection, error) {
	if len(tokenDigest) != 32 || asOf.IsZero() {
		return PasswordRecoveryInspection{}, ErrPasswordRecoveryInput
	}
	challenge, err := r.queries.GetPasswordRecoveryChallengeByToken(ctx, tokenDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return PasswordRecoveryInspection{}, ErrPasswordRecoveryTokenInvalid
	}
	if err != nil {
		return PasswordRecoveryInspection{}, fmt.Errorf("inspect password recovery token: %w", err)
	}
	return passwordRecoveryInspection(challenge.ID, challenge.CredentialRef, challenge.DeliveryStatus,
		challenge.ExpiresAt, challenge.SupersededAt, challenge.ConsumedAt, challenge.PasswordChangedAt, asOf)
}

func passwordRecoveryInspection(recoveryID, credentialRef uuid.UUID, deliveryStatus string, expiresAt, supersededAt, consumedAt, passwordChangedAt pgtype.Timestamptz, asOf time.Time) (PasswordRecoveryInspection, error) {
	if recoveryID == uuid.Nil || credentialRef == uuid.Nil {
		return PasswordRecoveryInspection{}, ErrPasswordRecoveryStateConflict
	}
	if consumedAt.Valid {
		if !passwordChangedAt.Valid {
			return PasswordRecoveryInspection{}, ErrPasswordRecoveryStateConflict
		}
		return PasswordRecoveryInspection{
			RecoveryID: recoveryID, CredentialRef: credentialRef, AlreadyCompleted: true,
			PasswordChangedAt: passwordChangedAt.Time.UTC(),
		}, nil
	}
	if !expiresAt.Valid || !expiresAt.Time.After(asOf) || supersededAt.Valid || deliveryStatus != "sent" || passwordChangedAt.Valid {
		return PasswordRecoveryInspection{}, ErrPasswordRecoveryTokenInvalid
	}
	return PasswordRecoveryInspection{RecoveryID: recoveryID, CredentialRef: credentialRef}, nil
}

// CompletePasswordRecovery serializes on the verified email identifier, then
// re-locks the token row before replacing the password. Password change,
// one-time consumption and login-backoff clearing commit in one Vault tx.
func (r *PostgresRepository) CompletePasswordRecovery(ctx context.Context, command CompletePasswordRecoveryCommand) (PasswordRecoveryConfirmation, error) {
	if len(command.TokenDigest) != 32 || command.PasswordHash == "" || command.PasswordChangedAt.IsZero() {
		return PasswordRecoveryConfirmation{}, ErrPasswordRecoveryInput
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return PasswordRecoveryConfirmation{}, fmt.Errorf("begin password recovery completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := vaultdb.New(tx)
	preflight, err := queries.GetPasswordRecoveryChallengeByToken(ctx, command.TokenDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return PasswordRecoveryConfirmation{}, ErrPasswordRecoveryTokenInvalid
	}
	if err != nil {
		return PasswordRecoveryConfirmation{}, fmt.Errorf("load password recovery challenge: %w", err)
	}
	if _, err := queries.LockEmailIdentifierForPasswordRecovery(ctx, preflight.CredentialRef); errors.Is(err, pgx.ErrNoRows) {
		return PasswordRecoveryConfirmation{}, ErrPasswordRecoveryStateConflict
	} else if err != nil {
		return PasswordRecoveryConfirmation{}, fmt.Errorf("lock password recovery identifier: %w", err)
	}
	challenge, err := queries.GetPasswordRecoveryChallengeByTokenForUpdate(ctx, command.TokenDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return PasswordRecoveryConfirmation{}, ErrPasswordRecoveryTokenInvalid
	}
	if err != nil {
		return PasswordRecoveryConfirmation{}, fmt.Errorf("lock password recovery challenge: %w", err)
	}
	inspection, err := passwordRecoveryInspection(challenge.ID, challenge.CredentialRef, challenge.DeliveryStatus,
		challenge.ExpiresAt, challenge.SupersededAt, challenge.ConsumedAt, challenge.PasswordChangedAt, command.PasswordChangedAt)
	if err != nil {
		return PasswordRecoveryConfirmation{}, err
	}
	if inspection.AlreadyCompleted {
		return PasswordRecoveryConfirmation{
			RecoveryID: inspection.RecoveryID, CredentialRef: inspection.CredentialRef,
			PasswordChangedAt: inspection.PasswordChangedAt,
		}, nil
	}
	persistedChangedAt, err := queries.ReplaceCredentialPassword(ctx, vaultdb.ReplaceCredentialPasswordParams{
		PasswordHash: command.PasswordHash, PasswordChangedAt: vaultTimestamp(command.PasswordChangedAt),
		CredentialRef: challenge.CredentialRef,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return PasswordRecoveryConfirmation{}, ErrPasswordRecoveryStateConflict
	}
	if err != nil {
		return PasswordRecoveryConfirmation{}, fmt.Errorf("replace credential password: %w", err)
	}
	if !persistedChangedAt.Valid {
		return PasswordRecoveryConfirmation{}, ErrPasswordRecoveryStateConflict
	}
	completed, err := queries.CompletePasswordRecoveryChallenge(ctx, vaultdb.CompletePasswordRecoveryChallengeParams{
		ConsumedAt: persistedChangedAt, PasswordChangedAt: persistedChangedAt, ID: challenge.ID,
	})
	if err != nil {
		return PasswordRecoveryConfirmation{}, fmt.Errorf("consume password recovery challenge: %w", err)
	}
	if !completed.ConsumedAt.Valid || !completed.PasswordChangedAt.Valid ||
		!completed.PasswordChangedAt.Time.Equal(persistedChangedAt.Time) {
		return PasswordRecoveryConfirmation{}, ErrPasswordRecoveryStateConflict
	}
	if err := queries.ClearCredentialLoginFailureBuckets(ctx, challenge.CredentialRef); err != nil {
		return PasswordRecoveryConfirmation{}, fmt.Errorf("clear credential login failures: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PasswordRecoveryConfirmation{}, fmt.Errorf("commit password recovery completion: %w", err)
	}
	return PasswordRecoveryConfirmation{
		RecoveryID: challenge.ID, CredentialRef: challenge.CredentialRef,
		PasswordChangedAt: persistedChangedAt.Time.UTC(),
	}, nil
}

func registrationWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return ErrIdentifierUnavailable
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func vaultTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

// LoginBlockedUntil reads only keyed failure state. A stale window is treated
// as allowed; the next failed attempt atomically resets it under row lock.
func (r *PostgresRepository) LoginBlockedUntil(ctx context.Context, lookup []byte, asOf time.Time) (time.Time, error) {
	if len(lookup) != 32 || asOf.IsZero() {
		return time.Time{}, ErrInvalidCredentials
	}
	row, err := r.queries.GetLoginFailureBucket(ctx, lookup)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("get login failure bucket: %w", err)
	}
	if !row.LastFailedAt.Valid || !row.BlockedUntil.Valid {
		return time.Time{}, errors.New("login failure bucket contains invalid timestamps")
	}
	if !row.LastFailedAt.Time.Add(loginFailureWindow).After(asOf) {
		return time.Time{}, nil
	}
	return row.BlockedUntil.Time.UTC(), nil
}

// RecordLoginFailure serializes updates per identifier HMAC. The bucket is
// created for unknown identifiers as well, preserving identical throttle
// behavior without persisting the original username or email address.
func (r *PostgresRepository) RecordLoginFailure(ctx context.Context, lookup []byte, failedAt time.Time) error {
	if len(lookup) != 32 || failedAt.IsZero() {
		return ErrInvalidCredentials
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin login failure update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := vaultdb.New(tx)
	failedAt = failedAt.UTC()
	inserted, err := queries.InsertLoginFailureBucket(ctx, vaultdb.InsertLoginFailureBucketParams{
		IdentifierLookupHmac: lookup,
		FailedAttempts:       1,
		WindowStartedAt:      vaultTimestamp(failedAt),
		LastFailedAt:         vaultTimestamp(failedAt),
		BlockedUntil:         vaultTimestamp(failedAt),
		UpdatedAt:            vaultTimestamp(failedAt),
	})
	if err != nil {
		return fmt.Errorf("insert login failure bucket: %w", err)
	}
	if inserted == 1 {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit login failure bucket: %w", err)
		}
		return nil
	}
	current, err := queries.LockLoginFailureBucket(ctx, lookup)
	if err != nil {
		return fmt.Errorf("lock login failure bucket: %w", err)
	}
	if !current.WindowStartedAt.Valid || !current.LastFailedAt.Valid {
		return errors.New("login failure bucket contains invalid state")
	}
	windowStartedAt := current.WindowStartedAt.Time.UTC()
	attempts := current.FailedAttempts + 1
	if !current.LastFailedAt.Time.Add(loginFailureWindow).After(failedAt) {
		windowStartedAt = failedAt
		attempts = 1
	}
	if attempts > 1000 {
		attempts = 1000
	}
	blockedUntil := failedAt.Add(loginFailureBackoff(attempts))
	if err := queries.UpdateLoginFailureBucket(ctx, vaultdb.UpdateLoginFailureBucketParams{
		FailedAttempts: attempts, WindowStartedAt: vaultTimestamp(windowStartedAt),
		LastFailedAt: vaultTimestamp(failedAt), BlockedUntil: vaultTimestamp(blockedUntil),
		UpdatedAt: vaultTimestamp(failedAt), IdentifierLookupHmac: lookup,
	}); err != nil {
		return fmt.Errorf("update login failure bucket: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit login failure update: %w", err)
	}
	return nil
}

func (r *PostgresRepository) ClearLoginFailures(ctx context.Context, lookup []byte) error {
	if len(lookup) != 32 {
		return ErrInvalidCredentials
	}
	if err := r.queries.DeleteLoginFailureBucket(ctx, lookup); err != nil {
		return fmt.Errorf("delete login failure bucket: %w", err)
	}
	return nil
}

// RehashPasswordIfCurrent atomically upgrades a verified legacy or outdated
// hash. A zero-row result means another actor changed/disabled the credential
// after verification; callers must fail closed instead of overwriting it.
func (r *PostgresRepository) RehashPasswordIfCurrent(
	ctx context.Context,
	credentialRef uuid.UUID,
	expectedHash string,
	replacementHash string,
	rehashedAt time.Time,
) (bool, error) {
	if credentialRef == uuid.Nil || expectedHash == "" || rehashedAt.IsZero() {
		return false, ErrInvalidCredentials
	}
	parameters, _, _, err := parsePasswordHash(replacementHash)
	if err != nil || parameters != defaultPasswordParameters {
		return false, ErrInvalidCredentials
	}
	rows, err := r.queries.RehashPasswordIfCurrent(ctx, vaultdb.RehashPasswordIfCurrentParams{
		ReplacementHash: replacementHash,
		RehashedAt:      vaultTimestamp(rehashedAt),
		CredentialRef:   credentialRef,
		ExpectedHash:    expectedHash,
	})
	if err != nil {
		return false, fmt.Errorf("rehash password if current: %w", err)
	}
	return rows == 1, nil
}

// CredentialByLookupHMAC implements Repository.
func (r *PostgresRepository) CredentialByLookupHMAC(ctx context.Context, lookup []byte) (Credential, error) {
	row, err := r.queries.GetCredentialByLookupHMAC(ctx, lookup)
	if errors.Is(err, pgx.ErrNoRows) {
		return Credential{}, ErrInvalidCredentials
	}
	if err != nil {
		return Credential{}, fmt.Errorf("get credential by lookup hmac: %w", err)
	}
	return Credential{Reference: row.CredentialRef, PasswordHash: row.PasswordHash}, nil
}

// CredentialByLookupHMACForAccountAppeal intentionally omits only the
// credential.disabled_at predicate. Registration state and verified-email
// rules remain identical to ordinary login, so the endpoint cannot be reused
// to inspect half-created credentials or unverified email aliases.
func (r *PostgresRepository) CredentialByLookupHMACForAccountAppeal(ctx context.Context, lookup []byte) (Credential, error) {
	var credential Credential
	err := r.db.QueryRow(ctx, `
SELECT credential.credential_ref, credential.password_hash
FROM vault.direct_identifiers AS identifier
JOIN vault.credentials AS credential
  ON credential.credential_ref = identifier.credential_ref
LEFT JOIN vault.registration_provisions AS provision
  ON provision.credential_ref = credential.credential_ref
WHERE identifier.lookup_hmac = $1
  AND (identifier.kind = 'username' OR identifier.verified_at IS NOT NULL)
  AND (provision.registration_id IS NULL OR provision.status = 'active')`, lookup).Scan(
		&credential.Reference,
		&credential.PasswordHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Credential{}, ErrInvalidCredentials
	}
	if err != nil {
		return Credential{}, fmt.Errorf("get credential for account appeal: %w", err)
	}
	return credential, nil
}

// EnableCredentialAfterAccountAppeal is idempotent. Core still blocks login
// until its own disabled-account projection is committed, making a failure
// between the two services fail closed and safe to retry.
func (r *PostgresRepository) EnableCredentialAfterAccountAppeal(ctx context.Context, credentialRef uuid.UUID, enabledAt time.Time) error {
	if credentialRef == uuid.Nil || enabledAt.IsZero() {
		return ErrInvalidCredentials
	}
	var storedRef uuid.UUID
	err := r.db.QueryRow(ctx, `
UPDATE vault.credentials
SET disabled_at = NULL,
    updated_at = GREATEST(updated_at, $2)
WHERE credential_ref = $1
RETURNING credential_ref`, credentialRef, enabledAt.UTC()).Scan(&storedRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidCredentials
	}
	if err != nil {
		return fmt.Errorf("enable credential after account appeal: %w", err)
	}
	if storedRef != credentialRef {
		return errors.New("enabled credential reference changed")
	}
	return nil
}

var _ Repository = (*PostgresRepository)(nil)
var _ EmailVerificationRepository = (*PostgresRepository)(nil)
var _ PasswordRecoveryRepository = (*PostgresRepository)(nil)
