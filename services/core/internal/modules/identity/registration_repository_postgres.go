package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/generated/identitydb"
)

// PostgresRegistrationRepository owns each Core state transition and commits
// the completed transition with immutable audit evidence in one transaction.
type PostgresRegistrationRepository struct {
	pool         *pgxpool.Pool
	eventBuilder RegistrationEventBuilder
	newAppender  func(pgx.Tx) auditevent.Appender
}

func NewPostgresRegistrationRepository(pool *pgxpool.Pool, eventBuilder RegistrationEventBuilder, newAppender func(pgx.Tx) auditevent.Appender) (*PostgresRegistrationRepository, error) {
	if pool == nil || eventBuilder == nil || newAppender == nil {
		return nil, errors.New("registration repository dependencies are required")
	}
	return &PostgresRegistrationRepository{pool: pool, eventBuilder: eventBuilder, newAppender: newAppender}, nil
}

func (repository *PostgresRegistrationRepository) PublicRegistrationPolicy(ctx context.Context) (RegistrationPublicPolicy, error) {
	row, err := identitydb.New(repository.pool).GetRegistrationPolicy(ctx)
	if err != nil {
		return RegistrationPublicPolicy{}, fmt.Errorf("get public registration policy: %w", err)
	}
	policy, err := registrationPolicyFromValues(
		row.Mode, row.MemberInvitesEnabled, row.InviteValidDays,
		row.MaxInvitesPerMember, row.MinimumInviteAccountAgeDays,
		row.MinimumInviteLevel, row.UsernameMinCharacters, row.UsernameMaxCharacters,
		row.ReservedUsernames, row.EmailDomainMode, row.EmailDomains,
		row.SessionValidHours, row.RememberSessionValidHours,
		row.HumanVerificationProvider, row.HumanVerificationSiteKey,
		row.HumanVerificationRegistrationEnabled, row.HumanVerificationLoginEnabled,
		row.HumanVerificationPasswordRecoveryEnabled,
		row.Version, row.UpdatedAt,
	)
	if err != nil {
		return RegistrationPublicPolicy{}, err
	}
	return RegistrationPublicPolicy{
		Mode: policy.Mode, UsernameMinCharacters: policy.UsernameMinCharacters,
		UsernameMaxCharacters: policy.UsernameMaxCharacters, EmailDomainMode: policy.EmailDomainMode,
		HumanVerificationProvider:                policy.HumanVerificationProvider,
		HumanVerificationSiteKey:                 policy.HumanVerificationSiteKey,
		HumanVerificationRegistrationEnabled:     policy.HumanVerificationRegistrationEnabled,
		HumanVerificationLoginEnabled:            policy.HumanVerificationLoginEnabled,
		HumanVerificationPasswordRecoveryEnabled: policy.HumanVerificationPasswordRecoveryEnabled,
	}, nil
}

func (repository *PostgresRegistrationRepository) PrepareRegistration(ctx context.Context, command PrepareRegistrationCommand) (RegistrationRecord, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return RegistrationRecord{}, fmt.Errorf("begin registration reservation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := identitydb.New(tx)

	lockedPolicy, err := queries.LockRegistrationPolicy(ctx)
	if err != nil {
		return RegistrationRecord{}, fmt.Errorf("lock registration policy: %w", err)
	}
	policy, err := registrationPolicyFromValues(
		lockedPolicy.Mode, lockedPolicy.MemberInvitesEnabled, lockedPolicy.InviteValidDays,
		lockedPolicy.MaxInvitesPerMember, lockedPolicy.MinimumInviteAccountAgeDays,
		lockedPolicy.MinimumInviteLevel, lockedPolicy.UsernameMinCharacters, lockedPolicy.UsernameMaxCharacters,
		lockedPolicy.ReservedUsernames, lockedPolicy.EmailDomainMode, lockedPolicy.EmailDomains,
		lockedPolicy.SessionValidHours, lockedPolicy.RememberSessionValidHours,
		lockedPolicy.HumanVerificationProvider, lockedPolicy.HumanVerificationSiteKey,
		lockedPolicy.HumanVerificationRegistrationEnabled, lockedPolicy.HumanVerificationLoginEnabled,
		lockedPolicy.HumanVerificationPasswordRecoveryEnabled,
		lockedPolicy.Version, lockedPolicy.UpdatedAt,
	)
	if err != nil {
		return RegistrationRecord{}, err
	}
	existing, err := queries.GetRegistrationForUpdate(ctx, command.ID)
	if err == nil {
		record, conversionErr := registrationRecord(existing)
		if conversionErr != nil {
			return RegistrationRecord{}, conversionErr
		}
		if record.Username != command.Username || record.DisplayName != command.DisplayName {
			return RegistrationRecord{}, ErrRegistrationIdempotencyConflict
		}
		if record.Mode == RegistrationModeOpen && len(command.InvitationDigest) != 0 {
			return RegistrationRecord{}, ErrRegistrationIdempotencyConflict
		}
		if record.Mode == RegistrationModeInvite {
			if record.InvitationID == nil || len(command.InvitationDigest) != 32 {
				return RegistrationRecord{}, ErrRegistrationIdempotencyConflict
			}
			matches, matchErr := queries.RegistrationInvitationMatches(ctx, identitydb.RegistrationInvitationMatchesParams{
				InvitationID: *record.InvitationID, TokenSha256: command.InvitationDigest,
				RegistrationID: pgtype.UUID{Bytes: command.ID, Valid: true},
			})
			if matchErr != nil {
				return RegistrationRecord{}, fmt.Errorf("validate resumed registration invitation: %w", matchErr)
			}
			if !matches {
				return RegistrationRecord{}, ErrRegistrationIdempotencyConflict
			}
		}
		return record, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return RegistrationRecord{}, fmt.Errorf("load registration reservation: %w", err)
	}
	admissionMode, err := registrationAdmissionMode(policy.Mode, command.InvitationDigest)
	if err != nil {
		return RegistrationRecord{}, err
	}
	if err := registrationPolicyAllowsNewAccount(policy, command.Username, command.EmailDomain); err != nil {
		return RegistrationRecord{}, err
	}
	unavailable, err := queries.RegistrationUsernameUnavailable(ctx, identitydb.RegistrationUsernameUnavailableParams{
		Username: command.Username, RegistrationID: command.ID,
	})
	if err != nil {
		return RegistrationRecord{}, fmt.Errorf("check registration username: %w", err)
	}
	if unavailable {
		return RegistrationRecord{}, ErrRegistrationUnavailable
	}

	var invitationID pgtype.UUID
	if admissionMode == RegistrationModeInvite {
		id, inviteErr := queries.GetAvailableRegistrationInvitationForUpdate(ctx, identitydb.GetAvailableRegistrationInvitationForUpdateParams{
			TokenSha256: command.InvitationDigest, AsOf: timestamp(command.OccurredAt),
		})
		if errors.Is(inviteErr, pgx.ErrNoRows) {
			return RegistrationRecord{}, ErrRegistrationInvitationInvalid
		}
		if inviteErr != nil {
			return RegistrationRecord{}, fmt.Errorf("lock registration invitation: %w", inviteErr)
		}
		invitationID = pgtype.UUID{Bytes: id, Valid: true}
	}

	row, err := queries.InsertRegistration(ctx, identitydb.InsertRegistrationParams{
		RegistrationID: command.ID, UserID: command.UserID,
		Username: command.Username, DisplayName: command.DisplayName,
		AdmissionMode: string(admissionMode), InvitationID: invitationID,
		OccurredAt: timestamp(command.OccurredAt),
	})
	if err != nil {
		return RegistrationRecord{}, registrationCoreWriteError("insert registration reservation", err)
	}
	if invitationID.Valid {
		rows, claimErr := queries.ClaimRegistrationInvitation(ctx, identitydb.ClaimRegistrationInvitationParams{
			RegistrationID: pgtype.UUID{Bytes: command.ID, Valid: true},
			ClaimedAt:      timestamp(command.OccurredAt), InvitationID: invitationID.Bytes,
		})
		if claimErr != nil {
			return RegistrationRecord{}, fmt.Errorf("claim registration invitation: %w", claimErr)
		}
		if rows != 1 {
			return RegistrationRecord{}, ErrRegistrationInvitationInvalid
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return RegistrationRecord{}, registrationCoreWriteError("commit registration reservation", err)
	}
	return registrationRecord(row)
}

// registrationAdmissionMode separates the site's current registration policy
// from the immutable evidence attached to one completed registration. Open
// registration accepts both direct sign-ups and a valid optional invitation;
// the latter is recorded as invite admission so the relationship is not lost.
func registrationAdmissionMode(policyMode RegistrationMode, invitationDigest []byte) (RegistrationMode, error) {
	if len(invitationDigest) != 0 && len(invitationDigest) != invitationTokenDigestBytes {
		return "", ErrRegistrationInvitationInvalid
	}
	switch policyMode {
	case RegistrationModeClosed:
		return "", ErrRegistrationClosed
	case RegistrationModeInvite:
		if len(invitationDigest) != invitationTokenDigestBytes {
			return "", ErrRegistrationInvitationInvalid
		}
		return RegistrationModeInvite, nil
	case RegistrationModeOpen:
		if len(invitationDigest) == invitationTokenDigestBytes {
			return RegistrationModeInvite, nil
		}
		return RegistrationModeOpen, nil
	default:
		return "", ErrRegistrationStateConflict
	}
}

func (repository *PostgresRegistrationRepository) AttachRegistrationCredential(ctx context.Context, registrationID, credentialRef uuid.UUID, occurredAt time.Time) (RegistrationRecord, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return RegistrationRecord{}, fmt.Errorf("begin registration credential attach: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := identitydb.New(tx)
	row, err := queries.GetRegistrationForUpdate(ctx, registrationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return RegistrationRecord{}, ErrRegistrationStateConflict
	}
	if err != nil {
		return RegistrationRecord{}, fmt.Errorf("lock registration for credential attach: %w", err)
	}
	record, err := registrationRecord(row)
	if err != nil {
		return RegistrationRecord{}, err
	}
	if record.CredentialRef != nil {
		if *record.CredentialRef != credentialRef || record.State == RegistrationStateReserved {
			return RegistrationRecord{}, ErrRegistrationStateConflict
		}
		return record, nil
	}
	if record.State != RegistrationStateReserved {
		return RegistrationRecord{}, ErrRegistrationStateConflict
	}
	if err := queries.InsertPendingRegisteredUser(ctx, identitydb.InsertPendingRegisteredUserParams{
		UserID: record.UserID, CredentialRef: credentialRef,
		Username: record.Username, DisplayName: record.DisplayName,
		OccurredAt: timestamp(occurredAt),
	}); err != nil {
		return RegistrationRecord{}, registrationCoreWriteError("insert pending registered user", err)
	}
	updated, err := queries.SetRegistrationCredential(ctx, identitydb.SetRegistrationCredentialParams{
		CredentialRef: pgtype.UUID{Bytes: credentialRef, Valid: true},
		OccurredAt:    timestamp(occurredAt), RegistrationID: registrationID,
	})
	if err != nil {
		return RegistrationRecord{}, fmt.Errorf("attach registration credential: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return RegistrationRecord{}, registrationCoreWriteError("commit registration credential attach", err)
	}
	return registrationRecord(updated)
}

func (repository *PostgresRegistrationRepository) CompleteRegistration(ctx context.Context, registrationID uuid.UUID, occurredAt time.Time) (RegistrationRecord, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return RegistrationRecord{}, fmt.Errorf("begin registration completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := identitydb.New(tx)
	row, err := queries.GetRegistrationForUpdate(ctx, registrationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return RegistrationRecord{}, ErrRegistrationStateConflict
	}
	if err != nil {
		return RegistrationRecord{}, fmt.Errorf("lock registration for completion: %w", err)
	}
	record, err := registrationRecord(row)
	if err != nil {
		return RegistrationRecord{}, err
	}
	if record.State == RegistrationStateCompleted {
		return record, nil
	}
	if record.State != RegistrationStateCredentialProvisioned || record.CredentialRef == nil {
		return RegistrationRecord{}, ErrRegistrationStateConflict
	}
	rows, err := queries.ActivateRegisteredUser(ctx, identitydb.ActivateRegisteredUserParams{
		OccurredAt: timestamp(occurredAt), UserID: record.UserID, CredentialRef: *record.CredentialRef,
	})
	if err != nil {
		return RegistrationRecord{}, fmt.Errorf("activate registered user: %w", err)
	}
	if rows != 1 {
		return RegistrationRecord{}, ErrRegistrationStateConflict
	}
	completed, err := queries.CompleteRegistration(ctx, identitydb.CompleteRegistrationParams{
		OccurredAt: timestamp(occurredAt), RegistrationID: registrationID,
	})
	if err != nil {
		return RegistrationRecord{}, fmt.Errorf("complete registration: %w", err)
	}
	record, err = registrationRecord(completed)
	if err != nil {
		return RegistrationRecord{}, err
	}
	if record.InvitationID != nil {
		rows, err = queries.ConsumeRegistrationInvitation(ctx, identitydb.ConsumeRegistrationInvitationParams{
			ConsumedAt: timestamp(occurredAt), InvitationID: *record.InvitationID,
			RegistrationID: pgtype.UUID{Bytes: registrationID, Valid: true},
		})
		if err != nil {
			return RegistrationRecord{}, fmt.Errorf("consume registration invitation: %w", err)
		}
		if rows != 1 {
			return RegistrationRecord{}, ErrRegistrationStateConflict
		}
		// Invitation ancestry is committed with registration completion. The
		// token row remains credential evidence; this separate immutable edge is
		// what future invitation-tree and reward calculations consume.
		relationshipValid, relationshipErr := queries.RecordRegistrationInvitationRelationship(ctx, registrationID)
		if relationshipErr != nil {
			return RegistrationRecord{}, fmt.Errorf("record registration invitation relationship: %w", relationshipErr)
		}
		if !relationshipValid {
			return RegistrationRecord{}, ErrRegistrationStateConflict
		}
	}
	event, err := repository.eventBuilder.BuildRegistrationCompletedEvent(RegistrationAuditInput{
		RegistrationID: registrationID, UserID: record.UserID, Mode: record.Mode,
		InvitationID: record.InvitationID, OccurredAt: occurredAt,
	})
	if err != nil {
		return RegistrationRecord{}, fmt.Errorf("build registration completed audit event: %w", err)
	}
	if err := repository.newAppender(tx).Append(ctx, event); err != nil {
		return RegistrationRecord{}, fmt.Errorf("append registration completed audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return RegistrationRecord{}, fmt.Errorf("commit registration completion: %w", err)
	}
	return record, nil
}

func registrationRecord(row identitydb.IdentityRegistration) (RegistrationRecord, error) {
	mode, err := validRegistrationMode(row.AdmissionMode)
	if err != nil || mode == RegistrationModeClosed {
		return RegistrationRecord{}, ErrRegistrationStateConflict
	}
	state := RegistrationState(row.State)
	if state != RegistrationStateReserved && state != RegistrationStateCredentialProvisioned && state != RegistrationStateCompleted {
		return RegistrationRecord{}, ErrRegistrationStateConflict
	}
	if !row.CreatedAt.Valid || !row.UpdatedAt.Valid {
		return RegistrationRecord{}, ErrRegistrationStateConflict
	}
	record := RegistrationRecord{
		ID: row.ID, UserID: row.UserID, Username: row.Username, DisplayName: row.DisplayName,
		Mode: mode, State: state, CreatedAt: row.CreatedAt.Time.UTC(), UpdatedAt: row.UpdatedAt.Time.UTC(),
	}
	if row.InvitationID.Valid {
		value := uuid.UUID(row.InvitationID.Bytes)
		record.InvitationID = &value
	}
	if row.CredentialRef.Valid {
		value := uuid.UUID(row.CredentialRef.Bytes)
		record.CredentialRef = &value
	}
	if row.CompletedAt.Valid {
		value := row.CompletedAt.Time.UTC()
		record.CompletedAt = &value
	}
	return record, nil
}

func validRegistrationMode(value string) (RegistrationMode, error) {
	mode := RegistrationMode(value)
	if mode != RegistrationModeOpen && mode != RegistrationModeInvite && mode != RegistrationModeClosed {
		return "", ErrRegistrationStateConflict
	}
	return mode, nil
}

func registrationCoreWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return ErrRegistrationUnavailable
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ RegistrationRepository = (*PostgresRegistrationRepository)(nil)
