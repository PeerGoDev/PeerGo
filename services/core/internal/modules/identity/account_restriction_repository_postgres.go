package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/generated/identitydb"
)

// PostgresAccountRestrictionRepository owns the complete restriction mutation
// transaction. The user row is the serialization point: commands for the same
// account cannot both pass overlap/version checks, and state never commits
// without its immutable audit outbox event.
type PostgresAccountRestrictionRepository struct {
	pool         *pgxpool.Pool
	eventBuilder AccountRestrictionEventBuilder
	newAppender  func(pgx.Tx) auditevent.Appender
}

func NewPostgresAccountRestrictionRepository(pool *pgxpool.Pool, eventBuilder AccountRestrictionEventBuilder, newAppender func(pgx.Tx) auditevent.Appender) (*PostgresAccountRestrictionRepository, error) {
	if pool == nil || eventBuilder == nil || newAppender == nil {
		return nil, errors.New("account restriction repository dependencies are required")
	}
	return &PostgresAccountRestrictionRepository{
		pool: pool, eventBuilder: eventBuilder, newAppender: newAppender,
	}, nil
}

func (repository *PostgresAccountRestrictionRepository) CreateAccountRestriction(ctx context.Context, command CreateAccountRestrictionCommand) (ManagedUserDetail, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("begin account restriction create: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := identitydb.New(tx)

	user, err := queries.LockManagedUserForAccountRestriction(ctx, command.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedUserDetail{}, ErrManagedUserNotFound
	}
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("lock managed user: %w", err)
	}
	if err := validateLockedManagedUser(user); err != nil {
		return ManagedUserDetail{}, err
	}
	if AccountStatus(user.Status) != AccountStatusActive {
		return ManagedUserDetail{}, ErrManagedUserNotActive
	}
	if user.AdministrationVersion != command.ExpectedUserVersion {
		return ManagedUserDetail{}, ErrManagedUserVersionConflict
	}

	expiresAt := command.OccurredAt.Add(time.Duration(command.DurationHours) * time.Hour)
	_, err = queries.GetOverlappingAccountRestrictionForUpdate(ctx, identitydb.GetOverlappingAccountRestrictionForUpdateParams{
		UserID: command.UserID, StartsAt: timestamp(command.OccurredAt), ExpiresAt: timestamp(expiresAt),
	})
	if err == nil {
		return ManagedUserDetail{}, ErrAccountRestrictionAlreadyActive
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ManagedUserDetail{}, fmt.Errorf("check overlapping account restriction: %w", err)
	}

	restriction, err := queries.InsertAccountAccessRestriction(ctx, identitydb.InsertAccountAccessRestrictionParams{
		RestrictionID: uuid.New(), UserID: command.UserID,
		ReasonCode: string(command.ReasonCode), ReasonSummary: command.Reason,
		StartsAt: timestamp(command.OccurredAt), ExpiresAt: timestamp(expiresAt),
		ActorID: pgtype.UUID{Bytes: command.ActorID, Valid: true},
	})
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("insert account restriction: %w", err)
	}
	advanced, err := queries.AdvanceUserAdministrationVersion(ctx, identitydb.AdvanceUserAdministrationVersionParams{
		UpdatedAt: timestamp(command.OccurredAt), UserID: command.UserID,
		ExpectedVersion: command.ExpectedUserVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedUserDetail{}, ErrManagedUserVersionConflict
	}
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("advance user administration version: %w", err)
	}
	if advanced.AdministrationVersion != command.ExpectedUserVersion+1 || !advanced.UpdatedAt.Valid {
		return ManagedUserDetail{}, errors.New("account restriction create returned an invalid user version")
	}
	if _, err := queries.RevokeUserSessionsForAccountRestriction(ctx, identitydb.RevokeUserSessionsForAccountRestrictionParams{
		RevokedAt: timestamp(command.OccurredAt), UserID: command.UserID,
	}); err != nil {
		return ManagedUserDetail{}, fmt.Errorf("revoke restricted user sessions: %w", err)
	}

	after, err := createdRestrictionAuditState(restriction, advanced.AdministrationVersion)
	if err != nil {
		return ManagedUserDetail{}, err
	}
	if err := repository.appendEvent(ctx, tx, AccountRestrictionAuditInput{
		Transition: AccountRestrictionTransitionCreated, OccurredAt: command.OccurredAt,
		ActorID: command.ActorID, TargetUserID: command.UserID, RestrictionID: restriction.ID,
		CommandReasonCode: string(command.ReasonCode), Reason: command.Reason,
		ExpectedUserVersion: command.ExpectedUserVersion, Authorization: command.Authorization,
		After: after,
	}); err != nil {
		return ManagedUserDetail{}, err
	}
	detail, err := managedUserDetailWithQueries(ctx, queries, command.UserID, command.OccurredAt)
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("read created account restriction: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedUserDetail{}, fmt.Errorf("commit account restriction create: %w", err)
	}
	return detail, nil
}

func (repository *PostgresAccountRestrictionRepository) RevokeAccountRestriction(ctx context.Context, command RevokeAccountRestrictionCommand) (ManagedUserDetail, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("begin account restriction revoke: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := identitydb.New(tx)

	user, err := queries.LockManagedUserForAccountRestriction(ctx, command.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedUserDetail{}, ErrManagedUserNotFound
	}
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("lock managed user: %w", err)
	}
	if err := validateLockedManagedUser(user); err != nil {
		return ManagedUserDetail{}, err
	}
	if user.AdministrationVersion != command.ExpectedUserVersion {
		return ManagedUserDetail{}, ErrManagedUserVersionConflict
	}

	current, err := queries.GetAccountRestrictionForUpdate(ctx, identitydb.GetAccountRestrictionForUpdateParams{
		RestrictionID: command.RestrictionID, UserID: command.UserID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedUserDetail{}, ErrAccountRestrictionNotActive
	}
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("lock account restriction: %w", err)
	}
	if current.RevokedAt.Valid || !current.StartsAt.Valid || !current.ExpiresAt.Valid ||
		command.OccurredAt.Before(current.StartsAt.Time) || !command.OccurredAt.Before(current.ExpiresAt.Time) {
		return ManagedUserDetail{}, ErrAccountRestrictionNotActive
	}
	if current.Version != command.ExpectedRestrictionVersion {
		return ManagedUserDetail{}, ErrAccountRestrictionVersionConflict
	}
	before, err := lockedRestrictionAuditState(current, command.ExpectedUserVersion)
	if err != nil {
		return ManagedUserDetail{}, err
	}

	revoked, err := queries.RevokeAccountAccessRestriction(ctx, identitydb.RevokeAccountAccessRestrictionParams{
		RevokedAt: timestamp(command.OccurredAt), ActorID: pgtype.UUID{Bytes: command.ActorID, Valid: true},
		RevocationReasonCode: pgtype.Text{String: string(command.ReasonCode), Valid: true},
		RevocationReason:     pgtype.Text{String: command.Reason, Valid: true},
		RestrictionID:        command.RestrictionID, UserID: command.UserID,
		ExpectedVersion: command.ExpectedRestrictionVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedUserDetail{}, ErrAccountRestrictionVersionConflict
	}
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("revoke account restriction: %w", err)
	}
	advanced, err := queries.AdvanceUserAdministrationVersion(ctx, identitydb.AdvanceUserAdministrationVersionParams{
		UpdatedAt: timestamp(command.OccurredAt), UserID: command.UserID,
		ExpectedVersion: command.ExpectedUserVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedUserDetail{}, ErrManagedUserVersionConflict
	}
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("advance user administration version: %w", err)
	}
	if advanced.AdministrationVersion != command.ExpectedUserVersion+1 || !advanced.UpdatedAt.Valid {
		return ManagedUserDetail{}, errors.New("account restriction revoke returned an invalid user version")
	}
	after, err := revokedRestrictionAuditState(revoked, advanced.AdministrationVersion)
	if err != nil {
		return ManagedUserDetail{}, err
	}
	if err := repository.appendEvent(ctx, tx, AccountRestrictionAuditInput{
		Transition: AccountRestrictionTransitionRevoked, OccurredAt: command.OccurredAt,
		ActorID: command.ActorID, TargetUserID: command.UserID, RestrictionID: command.RestrictionID,
		CommandReasonCode: string(command.ReasonCode), Reason: command.Reason,
		ExpectedUserVersion:        command.ExpectedUserVersion,
		ExpectedRestrictionVersion: command.ExpectedRestrictionVersion,
		Authorization:              command.Authorization, Before: &before, After: after,
	}); err != nil {
		return ManagedUserDetail{}, err
	}
	detail, err := managedUserDetailWithQueries(ctx, queries, command.UserID, command.OccurredAt)
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("read revoked account restriction: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedUserDetail{}, fmt.Errorf("commit account restriction revoke: %w", err)
	}
	return detail, nil
}

func (repository *PostgresAccountRestrictionRepository) appendEvent(ctx context.Context, tx pgx.Tx, input AccountRestrictionAuditInput) error {
	event, err := repository.eventBuilder.BuildAccountRestrictionEvent(input)
	if err != nil {
		return fmt.Errorf("build account restriction audit event: %w", err)
	}
	if err := repository.newAppender(tx).Append(ctx, event); err != nil {
		return fmt.Errorf("append account restriction audit event: %w", err)
	}
	return nil
}

func (repository *PostgresAccountRestrictionRepository) ManagedUserReactivationPreflight(ctx context.Context, userID uuid.UUID) (ManagedUserReactivationPreflight, error) {
	if userID == uuid.Nil {
		return ManagedUserReactivationPreflight{}, ErrUserAdministrationInput
	}
	var result ManagedUserReactivationPreflight
	if err := repository.pool.QueryRow(ctx, `
SELECT credential_ref, status, administration_version
FROM identity.users WHERE id = $1`, userID).Scan(&result.CredentialRef, &result.Status, &result.Version); errors.Is(err, pgx.ErrNoRows) {
		return ManagedUserReactivationPreflight{}, ErrManagedUserNotFound
	} else if err != nil {
		return ManagedUserReactivationPreflight{}, fmt.Errorf("read managed user reactivation preflight: %w", err)
	}
	return result, nil
}

func (repository *PostgresAccountRestrictionRepository) ReactivateManagedUser(ctx context.Context, command ReactivateManagedUserCommand) (ManagedUserDetail, error) {
	if command.ReactivationID == uuid.Nil || command.UserID == uuid.Nil || command.ActorID == uuid.Nil ||
		command.ExpectedUserVersion < 1 || command.Authorization.ID == uuid.Nil || !command.Authorization.Allow {
		return ManagedUserDetail{}, ErrUserAdministrationInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("begin managed user reactivation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := identitydb.New(tx)
	var replayUser, replayActor uuid.UUID
	var replayReason string
	var replayVersion int64
	err = tx.QueryRow(ctx, `
SELECT user_id, reason, actor_id, previous_administration_version
FROM identity.account_reactivations WHERE id = $1`, command.ReactivationID).Scan(
		&replayUser, &replayReason, &replayActor, &replayVersion,
	)
	if err == nil {
		if replayUser != command.UserID || replayReason != command.Reason || replayActor != command.ActorID || replayVersion != command.ExpectedUserVersion {
			return ManagedUserDetail{}, ErrManagedUserVersionConflict
		}
		detail, readErr := managedUserDetailWithQueries(ctx, queries, command.UserID, command.OccurredAt)
		if readErr != nil {
			return ManagedUserDetail{}, fmt.Errorf("read replayed managed user reactivation: %w", readErr)
		}
		if detail.Status != AccountStatusActive || detail.Version != command.ExpectedUserVersion+1 {
			return ManagedUserDetail{}, ErrManagedUserVersionConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return ManagedUserDetail{}, fmt.Errorf("commit managed user reactivation replay: %w", err)
		}
		return detail, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ManagedUserDetail{}, fmt.Errorf("read managed user reactivation replay: %w", err)
	}
	user, err := queries.LockManagedUserForAccountRestriction(ctx, command.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedUserDetail{}, ErrManagedUserNotFound
	}
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("lock managed user reactivation: %w", err)
	}
	if AccountStatus(user.Status) != AccountStatusDisabled {
		return ManagedUserDetail{}, ErrManagedUserNotDisabled
	}
	if user.AdministrationVersion != command.ExpectedUserVersion {
		return ManagedUserDetail{}, ErrManagedUserVersionConflict
	}
	result, err := tx.Exec(ctx, `
UPDATE identity.users
SET status = 'active', administration_version = administration_version + 1,
    updated_at = GREATEST(updated_at, $2)
WHERE id = $1 AND status = 'disabled' AND administration_version = $3`, command.UserID, command.OccurredAt, command.ExpectedUserVersion)
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("reactivate managed user: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ManagedUserDetail{}, ErrManagedUserVersionConflict
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.account_reactivations (
    id, user_id, reason, actor_id, authorization_decision_id,
    previous_administration_version, occurred_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)`, command.ReactivationID, command.UserID,
		command.Reason, command.ActorID, command.Authorization.ID, command.ExpectedUserVersion, command.OccurredAt); err != nil {
		return ManagedUserDetail{}, fmt.Errorf("record managed user reactivation: %w", err)
	}
	detail, err := managedUserDetailWithQueries(ctx, queries, command.UserID, command.OccurredAt)
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("read reactivated managed user: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedUserDetail{}, fmt.Errorf("commit managed user reactivation: %w", err)
	}
	return detail, nil
}

func validateLockedManagedUser(user identitydb.LockManagedUserForAccountRestrictionRow) error {
	status := AccountStatus(user.Status)
	if user.ID == uuid.Nil || user.Username == "" || user.DisplayName == "" ||
		(status != AccountStatusActive && status != AccountStatusDisabled && status != AccountStatusPending) ||
		user.AdministrationVersion < 1 || !user.CreatedAt.Valid || !user.UpdatedAt.Valid {
		return errors.New("locked managed user contains invalid required fields")
	}
	return nil
}

func createdRestrictionAuditState(row identitydb.InsertAccountAccessRestrictionRow, userVersion int64) (AccountRestrictionAuditState, error) {
	if row.ID == uuid.Nil || row.Kind != string(AccountRestrictionAccountAccess) ||
		row.ReasonCode == "" || row.ReasonSummary == "" || row.Version != 1 || userVersion < 2 ||
		!row.StartsAt.Valid || !row.ExpiresAt.Valid || !row.ExpiresAt.Time.After(row.StartsAt.Time) {
		return AccountRestrictionAuditState{}, errors.New("created account restriction contains invalid required fields")
	}
	return AccountRestrictionAuditState{
		RestrictionID: row.ID, Kind: row.Kind, ReasonCode: row.ReasonCode,
		ReasonSummary: row.ReasonSummary, StartsAt: row.StartsAt.Time.UTC(), ExpiresAt: row.ExpiresAt.Time.UTC(),
		RestrictionVersion: row.Version, UserAdministrationVersion: userVersion,
	}, nil
}

func lockedRestrictionAuditState(row identitydb.GetAccountRestrictionForUpdateRow, userVersion int64) (AccountRestrictionAuditState, error) {
	if row.ID == uuid.Nil || row.Kind != string(AccountRestrictionAccountAccess) || row.ReasonCode == "" ||
		row.ReasonSummary == "" || row.Version < 1 || userVersion < 1 || row.RevokedAt.Valid ||
		row.RevocationReasonCode.Valid || row.RevocationReason.Valid || !row.StartsAt.Valid ||
		!row.ExpiresAt.Valid || !row.ExpiresAt.Time.After(row.StartsAt.Time) {
		return AccountRestrictionAuditState{}, errors.New("locked account restriction contains invalid required fields")
	}
	return AccountRestrictionAuditState{
		RestrictionID: row.ID, Kind: row.Kind, ReasonCode: row.ReasonCode,
		ReasonSummary: row.ReasonSummary, StartsAt: row.StartsAt.Time.UTC(), ExpiresAt: row.ExpiresAt.Time.UTC(),
		RestrictionVersion: row.Version, UserAdministrationVersion: userVersion,
	}, nil
}

func revokedRestrictionAuditState(row identitydb.RevokeAccountAccessRestrictionRow, userVersion int64) (AccountRestrictionAuditState, error) {
	if row.ID == uuid.Nil || row.Kind != string(AccountRestrictionAccountAccess) || row.ReasonCode == "" ||
		row.ReasonSummary == "" || row.Version < 2 || userVersion < 2 || !row.StartsAt.Valid ||
		!row.ExpiresAt.Valid || !row.RevokedAt.Valid || !row.RevocationReasonCode.Valid ||
		!row.RevocationReason.Valid || !row.ExpiresAt.Time.After(row.StartsAt.Time) {
		return AccountRestrictionAuditState{}, errors.New("revoked account restriction contains invalid required fields")
	}
	revokedAt := row.RevokedAt.Time.UTC()
	return AccountRestrictionAuditState{
		RestrictionID: row.ID, Kind: row.Kind, ReasonCode: row.ReasonCode,
		ReasonSummary: row.ReasonSummary, StartsAt: row.StartsAt.Time.UTC(), ExpiresAt: row.ExpiresAt.Time.UTC(),
		RevokedAt: &revokedAt, RevocationReasonCode: row.RevocationReasonCode.String,
		RevocationReason: row.RevocationReason.String, RestrictionVersion: row.Version,
		UserAdministrationVersion: userVersion,
	}, nil
}

var _ AccountRestrictionCommandRepository = (*PostgresAccountRestrictionRepository)(nil)
