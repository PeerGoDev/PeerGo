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

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/generated/identitydb"
)

// PostgresStaffEnrollmentRepository owns the transactions that establish
// staff credential provenance. Ticket issuance and credential creation both
// fail closed if their audit event cannot be appended in the same commit.
type PostgresStaffEnrollmentRepository struct {
	db           postgresDB
	eventBuilder StaffBootstrapEventBuilder
	newAppender  func(pgx.Tx) auditevent.Appender
}

func NewPostgresStaffEnrollmentRepository(db postgresDB, eventBuilder StaffBootstrapEventBuilder, newAppender func(pgx.Tx) auditevent.Appender) (*PostgresStaffEnrollmentRepository, error) {
	if db == nil || eventBuilder == nil || newAppender == nil {
		return nil, errors.New("staff enrollment repository dependencies are required")
	}
	return &PostgresStaffEnrollmentRepository{db: db, eventBuilder: eventBuilder, newAppender: newAppender}, nil
}

func (repository *PostgresStaffEnrollmentRepository) IssueStaffBootstrapTicket(ctx context.Context, command IssueStaffBootstrapTicketCommand) (StaffBootstrapTicket, string, error) {
	tx, err := repository.db.Begin(ctx)
	if err != nil {
		return StaffBootstrapTicket{}, "", fmt.Errorf("begin staff bootstrap ticket transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := identitydb.New(tx)
	appender := repository.newAppender(tx)
	if appender == nil {
		return StaffBootstrapTicket{}, "", errors.New("staff bootstrap audit appender factory returned nil")
	}

	user, err := queries.GetActiveUserByUsernameForStaffBootstrap(ctx, identitydb.GetActiveUserByUsernameForStaffBootstrapParams{
		Username: command.TargetUsername,
		AsOf:     timestamp(command.Ticket.CreatedAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return StaffBootstrapTicket{}, "", ErrStaffBootstrapTargetNotFound
	}
	if err != nil {
		return StaffBootstrapTicket{}, "", fmt.Errorf("lock staff bootstrap target: %w", err)
	}
	if err := queries.RevokeActiveStaffBootstrapTickets(ctx, identitydb.RevokeActiveStaffBootstrapTicketsParams{
		RevokedAt: timestamp(command.Ticket.CreatedAt),
		UserID:    user.ID,
	}); err != nil {
		return StaffBootstrapTicket{}, "", fmt.Errorf("revoke previous staff bootstrap ticket: %w", err)
	}
	row, err := queries.InsertStaffBootstrapTicket(ctx, identitydb.InsertStaffBootstrapTicketParams{
		ID:                      command.Ticket.ID,
		UserID:                  user.ID,
		TokenHash:               command.Ticket.TokenHash,
		OperatorReferenceSha256: command.Ticket.OperatorReferenceSHA256,
		CreatedAt:               timestamp(command.Ticket.CreatedAt),
		ExpiresAt:               timestamp(command.Ticket.ExpiresAt),
	})
	if err != nil {
		return StaffBootstrapTicket{}, "", fmt.Errorf("insert staff bootstrap ticket: %w", err)
	}
	ticket, err := staffBootstrapTicketFromValues(row.ID, row.UserID, row.TokenHash, row.OperatorReferenceSha256, row.CreatedAt, row.ExpiresAt)
	if err != nil {
		return StaffBootstrapTicket{}, "", err
	}
	event, err := repository.eventBuilder.BuildStaffBootstrapEvent(StaffBootstrapAuditInput{
		Transition:        StaffBootstrapTicketIssued,
		OccurredAt:        ticket.CreatedAt,
		TicketID:          ticket.ID,
		TargetUserID:      ticket.UserID,
		OperatorReference: command.OperatorReference,
		ExpiresAt:         ticket.ExpiresAt,
	})
	if err != nil {
		return StaffBootstrapTicket{}, "", fmt.Errorf("build staff bootstrap ticket audit event: %w", err)
	}
	if err := appender.Append(ctx, event); err != nil {
		return StaffBootstrapTicket{}, "", fmt.Errorf("append staff bootstrap ticket audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return StaffBootstrapTicket{}, "", fmt.Errorf("commit staff bootstrap ticket: %w", err)
	}
	return ticket, user.Username, nil
}

func (repository *PostgresStaffEnrollmentRepository) CreateStaffEnrollmentChallenge(ctx context.Context, tokenHash []byte, challenge StaffWebAuthnEnrollmentChallenge, asOf time.Time) (StaffWebAuthnEnrollmentChallenge, error) {
	tx, err := repository.db.Begin(ctx)
	if err != nil {
		return StaffWebAuthnEnrollmentChallenge{}, fmt.Errorf("begin staff enrollment challenge transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := identitydb.New(tx)

	ticket, err := queries.GetStaffBootstrapTicketForUpdate(ctx, identitydb.GetStaffBootstrapTicketForUpdateParams{
		TokenHash: tokenHash,
		UserID:    challenge.UserID,
		AsOf:      timestamp(asOf),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return StaffWebAuthnEnrollmentChallenge{}, ErrStaffBootstrapTicketInvalid
	}
	if err != nil {
		return StaffWebAuthnEnrollmentChallenge{}, fmt.Errorf("lock staff bootstrap ticket: %w", err)
	}
	parentExpiry, err := queries.LockActiveWebSessionForEnrollment(ctx, identitydb.LockActiveWebSessionForEnrollmentParams{
		ParentTokenHash: challenge.ParentTokenHash,
		UserID:          challenge.UserID,
		AsOf:            timestamp(asOf),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return StaffWebAuthnEnrollmentChallenge{}, ErrSessionNotFound
	}
	if err != nil {
		return StaffWebAuthnEnrollmentChallenge{}, fmt.Errorf("lock staff enrollment parent session: %w", err)
	}
	if !ticket.ExpiresAt.Valid || !parentExpiry.Valid {
		return StaffWebAuthnEnrollmentChallenge{}, errors.New("staff enrollment authority contains an invalid expiry")
	}
	challenge.TicketID = ticket.ID
	challenge.ExpiresAt = earliestTime(challenge.ExpiresAt, ticket.ExpiresAt.Time.UTC(), parentExpiry.Time.UTC())
	if !challenge.ExpiresAt.After(asOf) {
		return StaffWebAuthnEnrollmentChallenge{}, ErrStaffBootstrapTicketInvalid
	}
	if err := queries.ConsumePreviousStaffEnrollmentChallenges(ctx, identitydb.ConsumePreviousStaffEnrollmentChallengesParams{
		ConsumedAt:      timestamp(asOf),
		TicketID:        challenge.TicketID,
		ParentTokenHash: challenge.ParentTokenHash,
	}); err != nil {
		return StaffWebAuthnEnrollmentChallenge{}, fmt.Errorf("consume previous staff enrollment challenge: %w", err)
	}
	if err := queries.InsertStaffEnrollmentChallenge(ctx, identitydb.InsertStaffEnrollmentChallengeParams{
		ID:                challenge.ID,
		TicketID:          challenge.TicketID,
		UserID:            challenge.UserID,
		ParentTokenHash:   challenge.ParentTokenHash,
		Label:             challenge.Label,
		SessionCiphertext: challenge.Protected.Ciphertext,
		SessionNonce:      challenge.Protected.Nonce,
		KeyEpoch:          challenge.Protected.KeyEpoch,
		CreatedAt:         timestamp(challenge.CreatedAt),
		ExpiresAt:         timestamp(challenge.ExpiresAt),
	}); err != nil {
		return StaffWebAuthnEnrollmentChallenge{}, fmt.Errorf("insert staff enrollment challenge: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return StaffWebAuthnEnrollmentChallenge{}, fmt.Errorf("commit staff enrollment challenge: %w", err)
	}
	return challenge, nil
}

func (repository *PostgresStaffEnrollmentRepository) ConsumeStaffEnrollmentChallenge(ctx context.Context, challengeID, userID uuid.UUID, parentTokenHash, tokenHash []byte, consumedAt time.Time) (StaffWebAuthnEnrollmentChallenge, error) {
	row, err := identitydb.New(repository.db).ConsumeStaffEnrollmentChallenge(ctx, identitydb.ConsumeStaffEnrollmentChallengeParams{
		ConsumedAt:      timestamp(consumedAt),
		ChallengeID:     challengeID,
		UserID:          userID,
		ParentTokenHash: parentTokenHash,
		TokenHash:       tokenHash,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return StaffWebAuthnEnrollmentChallenge{}, ErrStaffEnrollmentChallengeNotFound
	}
	if err != nil {
		return StaffWebAuthnEnrollmentChallenge{}, fmt.Errorf("consume staff enrollment challenge: %w", err)
	}
	if !row.CreatedAt.Valid || !row.ExpiresAt.Valid || !row.ConsumedAt.Valid {
		return StaffWebAuthnEnrollmentChallenge{}, errors.New("staff enrollment challenge contains invalid timestamps")
	}
	consumed := row.ConsumedAt.Time.UTC()
	return StaffWebAuthnEnrollmentChallenge{
		ID:              row.ID,
		TicketID:        row.TicketID,
		UserID:          row.UserID,
		ParentTokenHash: append([]byte(nil), row.ParentTokenHash...),
		Label:           row.Label,
		Protected: ProtectedRecord{
			Ciphertext: append([]byte(nil), row.SessionCiphertext...),
			Nonce:      append([]byte(nil), row.SessionNonce...),
			KeyEpoch:   row.KeyEpoch,
		},
		CreatedAt:  row.CreatedAt.Time.UTC(),
		ExpiresAt:  row.ExpiresAt.Time.UTC(),
		ConsumedAt: &consumed,
	}, nil
}

func (repository *PostgresStaffEnrollmentRepository) CreateStaffCredentialEnrollment(ctx context.Context, command CreateStaffCredentialEnrollmentCommand) error {
	tx, err := repository.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin staff credential enrollment transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := identitydb.New(tx)
	appender := repository.newAppender(tx)
	if appender == nil {
		return errors.New("staff enrollment audit appender factory returned nil")
	}

	ticket, err := queries.GetStaffBootstrapTicketByIDForUpdate(ctx, identitydb.GetStaffBootstrapTicketByIDForUpdateParams{
		TicketID:  command.TicketID,
		TokenHash: command.TokenHash,
		UserID:    command.UserID,
		AsOf:      timestamp(command.CreatedAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrStaffBootstrapTicketInvalid
	}
	if err != nil {
		return fmt.Errorf("lock staff bootstrap ticket for enrollment: %w", err)
	}
	if _, err := queries.GetConsumedStaffEnrollmentChallengeForUpdate(ctx, identitydb.GetConsumedStaffEnrollmentChallengeForUpdateParams{
		ChallengeID:     command.ChallengeID,
		TicketID:        command.TicketID,
		UserID:          command.UserID,
		ParentTokenHash: command.ParentTokenHash,
	}); errors.Is(err, pgx.ErrNoRows) {
		return ErrStaffEnrollmentChallengeNotFound
	} else if err != nil {
		return fmt.Errorf("lock consumed staff enrollment challenge: %w", err)
	}
	if _, err := queries.LockActiveWebSessionForEnrollment(ctx, identitydb.LockActiveWebSessionForEnrollmentParams{
		ParentTokenHash: command.ParentTokenHash,
		UserID:          command.UserID,
		AsOf:            timestamp(command.CreatedAt),
	}); errors.Is(err, pgx.ErrNoRows) {
		return ErrSessionNotFound
	} else if err != nil {
		return fmt.Errorf("lock staff enrollment parent session for completion: %w", err)
	}
	if err := queries.InsertStaffWebAuthnCredential(ctx, identitydb.InsertStaffWebAuthnCredentialParams{
		CredentialID:      command.CredentialID,
		UserID:            command.UserID,
		Label:             command.Label,
		RecordCiphertext:  command.CredentialRecord.Ciphertext,
		RecordNonce:       command.CredentialRecord.Nonce,
		KeyEpoch:          command.CredentialRecord.KeyEpoch,
		BootstrapTicketID: pgtype.UUID{Bytes: command.TicketID, Valid: true},
		CreatedAt:         timestamp(command.CreatedAt),
	}); err != nil {
		if isStaffCredentialEnrollmentConflict(err) {
			return ErrStaffCredentialAlreadyEnrolled
		}
		return fmt.Errorf("insert staff WebAuthn credential: %w", err)
	}
	rows, err := queries.ConsumeStaffBootstrapTicket(ctx, identitydb.ConsumeStaffBootstrapTicketParams{
		ConsumedAt: timestamp(command.CreatedAt),
		TicketID:   command.TicketID,
		UserID:     command.UserID,
	})
	if err != nil {
		return fmt.Errorf("consume staff bootstrap ticket: %w", err)
	}
	if rows != 1 {
		return ErrStaffBootstrapTicketInvalid
	}
	event, err := repository.eventBuilder.BuildStaffBootstrapEvent(StaffBootstrapAuditInput{
		Transition:              StaffBootstrapCredentialEnrolled,
		OccurredAt:              command.CreatedAt,
		TicketID:                command.TicketID,
		TargetUserID:            command.UserID,
		OperatorReferenceSHA256: append([]byte(nil), ticket.OperatorReferenceSha256...),
		ExpiresAt:               ticket.ExpiresAt.Time.UTC(),
		ChallengeID:             command.ChallengeID,
		CredentialID:            command.CredentialID,
		Label:                   command.Label,
		Authorization:           command.Authorization,
	})
	if err != nil {
		return fmt.Errorf("build staff credential enrollment audit event: %w", err)
	}
	if err := appender.Append(ctx, event); err != nil {
		return fmt.Errorf("append staff credential enrollment audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit staff credential enrollment: %w", err)
	}
	return nil
}

func staffBootstrapTicketFromValues(id, userID uuid.UUID, tokenHash, operatorReferenceSHA256 []byte, createdAt, expiresAt pgtype.Timestamptz) (StaffBootstrapTicket, error) {
	if id == uuid.Nil || userID == uuid.Nil || len(tokenHash) != 32 || len(operatorReferenceSHA256) != 32 || !createdAt.Valid || !expiresAt.Valid || !expiresAt.Time.After(createdAt.Time) {
		return StaffBootstrapTicket{}, errors.New("staff bootstrap ticket contains invalid persisted metadata")
	}
	return StaffBootstrapTicket{
		ID:                      id,
		UserID:                  userID,
		TokenHash:               append([]byte(nil), tokenHash...),
		OperatorReferenceSHA256: append([]byte(nil), operatorReferenceSHA256...),
		CreatedAt:               createdAt.Time.UTC(),
		ExpiresAt:               expiresAt.Time.UTC(),
	}, nil
}

func isStaffCredentialEnrollmentConflict(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}

var _ StaffEnrollmentRepository = (*PostgresStaffEnrollmentRepository)(nil)
