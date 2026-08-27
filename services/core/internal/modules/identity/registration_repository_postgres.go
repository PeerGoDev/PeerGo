package identity

import (
	"context"
	"crypto/sha256"
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

var registrationMemberAuthorityNamespace = uuid.MustParse("3b1f2af6-dc90-4b82-a15c-0c7da02191da")

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

func (repository *PostgresRegistrationRepository) ListIncompleteRegistrations(ctx context.Context, resultLimit int32) ([]RegistrationRecord, error) {
	if resultLimit < 1 || resultLimit > 1000 {
		return nil, ErrInvalidInput
	}
	rows, err := identitydb.New(repository.pool).ListIncompleteRegistrations(ctx, resultLimit)
	if err != nil {
		return nil, fmt.Errorf("list incomplete registrations: %w", err)
	}
	records := make([]RegistrationRecord, 0, len(rows))
	for _, row := range rows {
		record, conversionErr := registrationRecord(row)
		if conversionErr != nil {
			return nil, conversionErr
		}
		records = append(records, record)
	}
	return records, nil
}

func (repository *PostgresRegistrationRepository) ReleaseStaleRegistrationReservations(ctx context.Context, createdBefore time.Time, resultLimit int32) (int64, error) {
	if createdBefore.IsZero() || resultLimit < 1 || resultLimit > 1000 {
		return 0, ErrInvalidInput
	}
	count, err := identitydb.New(repository.pool).ReleaseStaleRegistrationReservations(ctx, identitydb.ReleaseStaleRegistrationReservationsParams{
		CreatedBefore: timestamp(createdBefore),
		ResultLimit:   resultLimit,
	})
	if err != nil {
		return 0, fmt.Errorf("release stale registration reservations: %w", err)
	}
	return count, nil
}

func (repository *PostgresRegistrationRepository) PrepareRegistration(ctx context.Context, command PrepareRegistrationCommand) (RegistrationRecord, error) {
	if (len(command.InvitationDigest) == 0 && len(command.InvitationEmailBinding) != 0) ||
		(len(command.InvitationDigest) != 0 && (len(command.InvitationDigest) != invitationTokenDigestBytes || len(command.InvitationEmailBinding) != sha256.Size)) {
		return RegistrationRecord{}, ErrRegistrationInvitationInvalid
	}
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
				RegistrationID: pgtype.UUID{Bytes: command.ID, Valid: true}, EmailBindingHmac: command.InvitationEmailBinding,
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
	// A page refresh creates a new browser idempotency key. If the previous
	// invited attempt already crossed the Vault boundary, recover that exact
	// saga by username and prove the invitation plus bound email again. The
	// service subsequently asks Vault to compare the original sensitive request
	// HMAC before it activates anything, so this lookup never substitutes new
	// credentials for the provisioned account.
	if len(command.InvitationDigest) == invitationTokenDigestBytes {
		recoverable, recoverErr := queries.GetRecoverableInvitationRegistrationForUpdate(ctx, command.Username)
		if recoverErr == nil {
			record, conversionErr := registrationRecord(recoverable)
			if conversionErr != nil {
				return RegistrationRecord{}, conversionErr
			}
			if record.DisplayName != command.DisplayName || record.InvitationID == nil {
				return RegistrationRecord{}, ErrRegistrationUnavailable
			}
			matches, matchErr := queries.RegistrationInvitationMatches(ctx, identitydb.RegistrationInvitationMatchesParams{
				InvitationID: *record.InvitationID, TokenSha256: command.InvitationDigest,
				RegistrationID: pgtype.UUID{Bytes: record.ID, Valid: true}, EmailBindingHmac: command.InvitationEmailBinding,
			})
			if matchErr != nil {
				return RegistrationRecord{}, fmt.Errorf("validate recoverable registration invitation: %w", matchErr)
			}
			if !matches {
				return RegistrationRecord{}, ErrRegistrationUnavailable
			}
			return record, nil
		}
		if !errors.Is(recoverErr, pgx.ErrNoRows) {
			return RegistrationRecord{}, fmt.Errorf("load recoverable invitation registration: %w", recoverErr)
		}
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
			TokenSha256: command.InvitationDigest, EmailBindingHmac: command.InvitationEmailBinding,
			AsOf: timestamp(command.OccurredAt),
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

// CancelRegistration releases a claim only while the Core reservation has no
// attached Vault credential. Vault identifier conflicts are terminal and
// leave no provision behind, so retaining this row would otherwise strand a
// reusable invitation token.
func (repository *PostgresRegistrationRepository) CancelRegistration(ctx context.Context, registrationID uuid.UUID) error {
	if registrationID == uuid.Nil {
		return ErrRegistrationStateConflict
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin registration cancellation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var state string
	var credentialRef, invitationID pgtype.UUID
	err = tx.QueryRow(ctx, `
SELECT state, credential_ref, invitation_id
FROM identity.registrations
WHERE id = $1
FOR UPDATE`, registrationID).Scan(&state, &credentialRef, &invitationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock registration for cancellation: %w", err)
	}
	if state != string(RegistrationStateReserved) || credentialRef.Valid {
		return ErrRegistrationStateConflict
	}
	if invitationID.Valid {
		tag, releaseErr := tx.Exec(ctx, `
UPDATE identity.registration_invitations
SET claimed_by = NULL, claimed_at = NULL
WHERE id = $1
  AND claimed_by = $2
  AND consumed_at IS NULL
  AND revoked_at IS NULL`, invitationID.Bytes, registrationID)
		if releaseErr != nil {
			return fmt.Errorf("release registration invitation: %w", releaseErr)
		}
		if tag.RowsAffected() != 1 {
			return ErrRegistrationStateConflict
		}
	}
	tag, err := tx.Exec(ctx, `
DELETE FROM identity.registrations
WHERE id = $1
  AND state = 'reserved'
  AND credential_ref IS NULL`, registrationID)
	if err != nil {
		return fmt.Errorf("delete registration reservation: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrRegistrationStateConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit registration cancellation: %w", err)
	}
	return nil
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
		if err := ensureRegisteredMemberState(ctx, tx, record, occurredAt); err != nil {
			return RegistrationRecord{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return RegistrationRecord{}, fmt.Errorf("commit completed registration repair: %w", err)
		}
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
	if err := ensureRegisteredMemberState(ctx, tx, record, occurredAt); err != nil {
		return RegistrationRecord{}, err
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

// ensureRegisteredMemberState installs the small mutable projections every
// native member needs immediately after registration. IDs derived from the
// registration make retries safe, while the active-grant predicate avoids a
// duplicate member authority if an operator repaired the account first.
func ensureRegisteredMemberState(ctx context.Context, tx pgx.Tx, record RegistrationRecord, occurredAt time.Time) error {
	if record.ID == uuid.Nil || record.UserID == uuid.Nil || record.CreatedAt.IsZero() {
		return ErrRegistrationStateConflict
	}
	mandateID := uuid.NewSHA1(registrationMemberAuthorityNamespace, []byte("mandate:"+record.ID.String()))
	grantID := uuid.NewSHA1(registrationMemberAuthorityNamespace, []byte("grant:"+record.ID.String()))
	startsAt := record.CreatedAt.UTC()
	endsAt := startsAt.AddDate(100, 0, 0)
	if _, err := tx.Exec(ctx, `
INSERT INTO governance.mandates (
    id, subject_id, source_type, source_reference, scope_type, scope_id,
    starts_at, ends_at, status, created_at, updated_at
) VALUES ($1,$2,'bootstrap',$3,'site','peergo',$4,$5,'active',$6,$6)
ON CONFLICT (id) DO NOTHING`, mandateID, record.UserID,
		"registration:"+record.ID.String()+":member", startsAt, endsAt, occurredAt); err != nil {
		return fmt.Errorf("ensure registration member mandate: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO authz.grants (
    id, subject_id, role_id, mandate_id, scope_type, scope_id,
    valid_from, valid_until, constraints, created_at, updated_at
)
SELECT $1,$2,'member',$3,'site','peergo',$4,$5,'{}'::jsonb,$6,$6
WHERE NOT EXISTS (
    SELECT 1
    FROM authz.grants AS existing
    JOIN governance.mandates AS existing_mandate
      ON existing_mandate.id = existing.mandate_id
     AND existing_mandate.subject_id = existing.subject_id
    WHERE existing.subject_id = $2
      AND existing.role_id = 'member'
      AND existing.scope_type = 'site'
      AND existing.scope_id = 'peergo'
      AND existing.revoked_at IS NULL
      AND existing.valid_from <= $6
      AND existing.valid_until > $6
      AND existing_mandate.status = 'active'
      AND existing_mandate.starts_at <= $6
      AND existing_mandate.ends_at > $6
)
ON CONFLICT (id) DO NOTHING`, grantID, record.UserID, mandateID, startsAt, endsAt, occurredAt); err != nil {
		return fmt.Errorf("ensure registration member grant: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.invitation_accounts (user_id, remaining_invites, version, updated_at)
VALUES ($1,0,1,$2)
ON CONFLICT (user_id) DO NOTHING`, record.UserID, occurredAt); err != nil {
		return fmt.Errorf("ensure registration invitation account: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.user_activity (user_id, last_active_at, version, updated_at)
VALUES ($1,NULL,1,$2)
ON CONFLICT (user_id) DO NOTHING`, record.UserID, occurredAt); err != nil {
		return fmt.Errorf("ensure registration activity state: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.user_access_states (
    user_id, download_restricted, vip_enabled, version, updated_at
) VALUES ($1,false,false,1,$2)
ON CONFLICT (user_id) DO NOTHING`, record.UserID, occurredAt); err != nil {
		return fmt.Errorf("ensure registration access state: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO traffic.user_totals (
    user_id, raw_uploaded, raw_downloaded, credited_uploaded,
    charged_downloaded, entry_count, version, last_occurred_at, updated_at
) VALUES ($1,0,0,0,0,0,0,NULL,$2)
ON CONFLICT (user_id) DO NOTHING`, record.UserID, occurredAt); err != nil {
		return fmt.Errorf("ensure registration traffic totals: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO economy.magic_accounts (
    id, user_id, account_kind, account_code, balance, version, updated_at
) VALUES ($1,$1,'member','member:' || $1::text,0,1,$2)
ON CONFLICT (user_id) DO NOTHING`, record.UserID, occurredAt); err != nil {
		return fmt.Errorf("ensure registration magic account: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO progression.user_progress (
    user_id, experience, level, policy_version, version, updated_at
)
SELECT $1, 0, initial_level.level, current_policy.policy_version, 1, $2
FROM LATERAL (
    SELECT revision.policy_version
    FROM progression.level_policy_revisions AS revision
    WHERE revision.effective_at <= $2
    ORDER BY revision.effective_at DESC, revision.sequence DESC
    LIMIT 1
) AS current_policy
JOIN LATERAL (
    SELECT definition.level
    FROM progression.level_definitions AS definition
    WHERE definition.policy_version = current_policy.policy_version
      AND definition.minimum_experience <= 0
    ORDER BY definition.minimum_experience DESC, definition.level DESC
    LIMIT 1
) AS initial_level ON true
ON CONFLICT (user_id) DO NOTHING`, record.UserID, occurredAt); err != nil {
		return fmt.Errorf("ensure registration progression state: %w", err)
	}
	var memberGrantExists bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM authz.grants AS member_grant
    JOIN governance.mandates AS mandate
      ON mandate.id = member_grant.mandate_id
     AND mandate.subject_id = member_grant.subject_id
    WHERE member_grant.subject_id = $1
      AND member_grant.role_id = 'member'
      AND member_grant.scope_type = 'site'
      AND member_grant.scope_id = 'peergo'
      AND member_grant.revoked_at IS NULL
      AND member_grant.valid_from <= $2
      AND member_grant.valid_until > $2
      AND mandate.status = 'active'
      AND mandate.starts_at <= $2
      AND mandate.ends_at > $2
) AND EXISTS (
    SELECT 1 FROM identity.invitation_accounts WHERE user_id = $1
) AND EXISTS (
    SELECT 1 FROM identity.user_activity WHERE user_id = $1
) AND EXISTS (
    SELECT 1 FROM identity.user_access_states WHERE user_id = $1
) AND EXISTS (
    SELECT 1 FROM traffic.user_totals WHERE user_id = $1
) AND EXISTS (
    SELECT 1 FROM economy.magic_accounts WHERE user_id = $1
) AND EXISTS (
    SELECT 1 FROM progression.user_progress WHERE user_id = $1
)`, record.UserID, occurredAt).Scan(&memberGrantExists); err != nil {
		return fmt.Errorf("verify registration member grant: %w", err)
	}
	if !memberGrantExists {
		return ErrRegistrationStateConflict
	}
	return nil
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
