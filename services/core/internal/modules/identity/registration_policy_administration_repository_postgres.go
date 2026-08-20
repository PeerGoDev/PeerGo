package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/generated/identitydb"
)

// PostgresRegistrationPolicyAdministrationRepository keeps the singleton row
// locked until both the new policy version and its audit outbox event commit.
// A stale staff editor can therefore never overwrite a newer admission mode.
type PostgresRegistrationPolicyAdministrationRepository struct {
	pool         *pgxpool.Pool
	queries      *identitydb.Queries
	eventBuilder RegistrationPolicyEventBuilder
	newAppender  func(pgx.Tx) auditevent.Appender
}

func NewPostgresRegistrationPolicyAdministrationRepository(pool *pgxpool.Pool, eventBuilder RegistrationPolicyEventBuilder, newAppender func(pgx.Tx) auditevent.Appender) (*PostgresRegistrationPolicyAdministrationRepository, error) {
	if pool == nil || eventBuilder == nil || newAppender == nil {
		return nil, errors.New("registration policy administration repository dependencies are required")
	}
	return &PostgresRegistrationPolicyAdministrationRepository{
		pool: pool, queries: identitydb.New(pool), eventBuilder: eventBuilder, newAppender: newAppender,
	}, nil
}

func (repository *PostgresRegistrationPolicyAdministrationRepository) GetRegistrationPolicy(ctx context.Context) (RegistrationPolicy, error) {
	row, err := repository.queries.GetRegistrationPolicy(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return RegistrationPolicy{}, ErrRegistrationPolicyNotFound
	}
	if err != nil {
		return RegistrationPolicy{}, fmt.Errorf("query registration policy: %w", err)
	}
	return registrationPolicyFromValues(
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
}

func (repository *PostgresRegistrationPolicyAdministrationRepository) UpdateRegistrationPolicy(ctx context.Context, command UpdateRegistrationPolicyCommand) (RegistrationPolicy, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return RegistrationPolicy{}, fmt.Errorf("begin registration policy update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := identitydb.New(tx)
	locked, err := queries.GetRegistrationPolicyForUpdate(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return RegistrationPolicy{}, ErrRegistrationPolicyNotFound
	}
	if err != nil {
		return RegistrationPolicy{}, fmt.Errorf("lock registration policy: %w", err)
	}
	before, err := registrationPolicyFromValues(
		locked.Mode, locked.MemberInvitesEnabled, locked.InviteValidDays,
		locked.MaxInvitesPerMember, locked.MinimumInviteAccountAgeDays,
		locked.MinimumInviteLevel, locked.UsernameMinCharacters, locked.UsernameMaxCharacters,
		locked.ReservedUsernames, locked.EmailDomainMode, locked.EmailDomains,
		locked.SessionValidHours, locked.RememberSessionValidHours,
		locked.HumanVerificationProvider, locked.HumanVerificationSiteKey,
		locked.HumanVerificationRegistrationEnabled, locked.HumanVerificationLoginEnabled,
		locked.HumanVerificationPasswordRecoveryEnabled,
		locked.Version, locked.UpdatedAt,
	)
	if err != nil {
		return RegistrationPolicy{}, err
	}
	if before.Version != command.ExpectedVersion {
		return RegistrationPolicy{}, ErrRegistrationPolicyVersionConflict
	}

	row, err := queries.UpdateRegistrationPolicy(ctx, identitydb.UpdateRegistrationPolicyParams{
		Mode:                                     string(command.Mode),
		MemberInvitesEnabled:                     command.MemberInvitesEnabled,
		InviteValidDays:                          int16(command.InviteValidDays),
		MaxInvitesPerMember:                      int16(command.MaxInvitesPerMember),
		MinimumInviteAccountAgeDays:              int16(command.MinimumInviteAccountAgeDays),
		MinimumInviteLevel:                       int16(command.MinimumInviteLevel),
		UsernameMinCharacters:                    int16(command.UsernameMinCharacters),
		UsernameMaxCharacters:                    int16(command.UsernameMaxCharacters),
		ReservedUsernames:                        append([]string(nil), command.ReservedUsernames...),
		EmailDomainMode:                          string(command.EmailDomainMode),
		EmailDomains:                             append([]string(nil), command.EmailDomains...),
		SessionValidHours:                        int16(command.SessionValidHours),
		RememberSessionValidHours:                int16(command.RememberSessionValidHours),
		HumanVerificationProvider:                string(command.HumanVerificationProvider),
		HumanVerificationSiteKey:                 command.HumanVerificationSiteKey,
		HumanVerificationRegistrationEnabled:     command.HumanVerificationRegistrationEnabled,
		HumanVerificationLoginEnabled:            command.HumanVerificationLoginEnabled,
		HumanVerificationPasswordRecoveryEnabled: command.HumanVerificationPasswordRecoveryEnabled,
		UpdatedAt:                                registrationPolicyTimestamp(command.OccurredAt),
		ExpectedVersion:                          command.ExpectedVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return RegistrationPolicy{}, ErrRegistrationPolicyVersionConflict
	}
	if err != nil {
		return RegistrationPolicy{}, fmt.Errorf("update registration policy row: %w", err)
	}
	after, err := registrationPolicyFromValues(
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
		return RegistrationPolicy{}, err
	}
	event, err := repository.eventBuilder.BuildRegistrationPolicyEvent(RegistrationPolicyAuditInput{
		OccurredAt: command.OccurredAt, ActorID: command.ActorID, Reason: command.Reason,
		ExpectedVersion: command.ExpectedVersion, Authorization: command.Authorization,
		Before: registrationPolicyAuditState(before), After: registrationPolicyAuditState(after),
	})
	if err != nil {
		return RegistrationPolicy{}, fmt.Errorf("build registration policy audit event: %w", err)
	}
	if err := repository.newAppender(tx).Append(ctx, event); err != nil {
		return RegistrationPolicy{}, fmt.Errorf("append registration policy audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return RegistrationPolicy{}, fmt.Errorf("commit registration policy update: %w", err)
	}
	return after, nil
}

func registrationPolicyFromValues(
	modeValue string,
	memberInvitesEnabled bool,
	inviteValidDays int16,
	maxInvitesPerMember int16,
	minimumInviteAccountAgeDays int16,
	minimumInviteLevel int16,
	usernameMinCharacters int16,
	usernameMaxCharacters int16,
	reservedUsernames []string,
	emailDomainModeValue string,
	emailDomains []string,
	sessionValidHours int16,
	rememberSessionValidHours int16,
	humanVerificationProviderValue string,
	humanVerificationSiteKey string,
	humanVerificationRegistrationEnabled bool,
	humanVerificationLoginEnabled bool,
	humanVerificationPasswordRecoveryEnabled bool,
	version int64,
	updatedAt pgtype.Timestamptz,
) (RegistrationPolicy, error) {
	mode, err := validRegistrationMode(modeValue)
	emailDomainMode := EmailDomainMode(emailDomainModeValue)
	humanVerificationProvider := HumanVerificationProvider(humanVerificationProviderValue)
	normalizedReservedUsernames := normalizePolicyEntries(reservedUsernames)
	normalizedEmailDomains := normalizePolicyEntries(emailDomains)
	if err != nil || inviteValidDays < 1 || inviteValidDays > 90 ||
		maxInvitesPerMember < 0 || maxInvitesPerMember > 100 ||
		minimumInviteAccountAgeDays < 0 || minimumInviteAccountAgeDays > 3650 ||
		minimumInviteLevel < 1 || minimumInviteLevel > 99 ||
		usernameMinCharacters < 3 || usernameMinCharacters > 32 ||
		usernameMaxCharacters < usernameMinCharacters || usernameMaxCharacters > 32 ||
		len(normalizedReservedUsernames) > maxReservedUsernames ||
		!validReservedUsernames(normalizedReservedUsernames) ||
		!validEmailDomainMode(emailDomainMode) || len(normalizedEmailDomains) > maxEmailDomains ||
		!validEmailDomains(normalizedEmailDomains) ||
		emailDomainMode != EmailDomainModeAny && len(normalizedEmailDomains) == 0 ||
		sessionValidHours < 1 || sessionValidHours > 720 ||
		rememberSessionValidHours < 24 || rememberSessionValidHours > 2160 ||
		sessionValidHours > rememberSessionValidHours ||
		!validStoredHumanVerificationPolicy(
			humanVerificationProvider,
			humanVerificationSiteKey,
			humanVerificationRegistrationEnabled,
			humanVerificationLoginEnabled,
			humanVerificationPasswordRecoveryEnabled,
		) || version < 1 || !updatedAt.Valid {
		return RegistrationPolicy{}, fmt.Errorf("%w: invalid registration policy projection", ErrRegistrationPolicyInput)
	}
	return RegistrationPolicy{
		Mode: mode, MemberInvitesEnabled: memberInvitesEnabled,
		InviteValidDays: int(inviteValidDays), MaxInvitesPerMember: int(maxInvitesPerMember),
		MinimumInviteAccountAgeDays: int(minimumInviteAccountAgeDays),
		MinimumInviteLevel:          int(minimumInviteLevel),
		UsernameMinCharacters:       int(usernameMinCharacters),
		UsernameMaxCharacters:       int(usernameMaxCharacters),
		ReservedUsernames:           normalizedReservedUsernames,
		EmailDomainMode:             emailDomainMode,
		EmailDomains:                normalizedEmailDomains,
		SessionValidHours:           int(sessionValidHours),
		RememberSessionValidHours:   int(rememberSessionValidHours), Version: version,
		HumanVerificationProvider:                humanVerificationProvider,
		HumanVerificationSiteKey:                 humanVerificationSiteKey,
		HumanVerificationRegistrationEnabled:     humanVerificationRegistrationEnabled,
		HumanVerificationLoginEnabled:            humanVerificationLoginEnabled,
		HumanVerificationPasswordRecoveryEnabled: humanVerificationPasswordRecoveryEnabled,
		UpdatedAt:                                updatedAt.Time.UTC(),
	}, nil
}

func registrationPolicyAuditState(policy RegistrationPolicy) RegistrationPolicyAuditState {
	return RegistrationPolicyAuditState{
		Mode: policy.Mode, MemberInvitesEnabled: policy.MemberInvitesEnabled,
		InviteValidDays: policy.InviteValidDays, MaxInvitesPerMember: policy.MaxInvitesPerMember,
		MinimumInviteAccountAgeDays: policy.MinimumInviteAccountAgeDays,
		MinimumInviteLevel:          policy.MinimumInviteLevel,
		UsernameMinCharacters:       policy.UsernameMinCharacters,
		UsernameMaxCharacters:       policy.UsernameMaxCharacters,
		ReservedUsernames:           append([]string(nil), policy.ReservedUsernames...),
		EmailDomainMode:             policy.EmailDomainMode,
		EmailDomains:                append([]string(nil), policy.EmailDomains...),
		SessionValidHours:           policy.SessionValidHours,
		RememberSessionValidHours:   policy.RememberSessionValidHours, Version: policy.Version,
		HumanVerificationProvider:                policy.HumanVerificationProvider,
		HumanVerificationSiteKey:                 policy.HumanVerificationSiteKey,
		HumanVerificationRegistrationEnabled:     policy.HumanVerificationRegistrationEnabled,
		HumanVerificationLoginEnabled:            policy.HumanVerificationLoginEnabled,
		HumanVerificationPasswordRecoveryEnabled: policy.HumanVerificationPasswordRecoveryEnabled,
	}
}

func registrationPolicyTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

var _ RegistrationPolicyAdministrationRepository = (*PostgresRegistrationPolicyAdministrationRepository)(nil)
