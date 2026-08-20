package identity

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/generated/identitydb"
)

type PostgresInvitationRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresInvitationRepository(pool *pgxpool.Pool) (*PostgresInvitationRepository, error) {
	if pool == nil {
		return nil, errors.New("invitation database is required")
	}
	return &PostgresInvitationRepository{pool: pool}, nil
}

// Overview reads policy, quota and history from one repeatable-read snapshot,
// so the displayed remaining count cannot describe a different point in time
// from the invitation rows beneath it.
func (repository *PostgresInvitationRepository) Overview(ctx context.Context, userID uuid.UUID, now time.Time, limit, offset int) (invitationIssuerSnapshot, []MemberInvitation, int, error) {
	if userID == uuid.Nil || now.IsZero() || limit < 1 || limit > MaxInvitationHistoryLimit || offset < 0 || offset > MaxInvitationHistoryOffset {
		return invitationIssuerSnapshot{}, nil, 0, ErrInvitationInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return invitationIssuerSnapshot{}, nil, 0, fmt.Errorf("begin invitation overview: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := identitydb.New(tx)
	contextRow, err := queries.GetInvitationIssuerContext(ctx, identitydb.GetInvitationIssuerContextParams{
		AsOf: invitationTimestamp(now), UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return invitationIssuerSnapshot{}, nil, 0, ErrInvitationIneligible
	}
	if err != nil {
		return invitationIssuerSnapshot{}, nil, 0, fmt.Errorf("get invitation issuer context: %w", err)
	}
	used, err := queries.CountInvitationQuotaUsage(ctx, identitydb.CountInvitationQuotaUsageParams{UserID: invitationUUID(userID), AsOf: invitationTimestamp(now)})
	if err != nil {
		return invitationIssuerSnapshot{}, nil, 0, fmt.Errorf("count invitation quota usage: %w", err)
	}
	total, err := queries.CountInvitationHistory(ctx, invitationUUID(userID))
	if err != nil {
		return invitationIssuerSnapshot{}, nil, 0, fmt.Errorf("count invitation history: %w", err)
	}
	if used < 0 || total < 0 || used > math.MaxInt || total > math.MaxInt {
		return invitationIssuerSnapshot{}, nil, 0, ErrInvitationInvariant
	}
	rows, err := queries.ListInvitationHistory(ctx, identitydb.ListInvitationHistoryParams{
		AsOf: invitationTimestamp(now), UserID: invitationUUID(userID),
		ResultLimit: int32(limit), ResultOffset: int32(offset),
	})
	if err != nil {
		return invitationIssuerSnapshot{}, nil, 0, fmt.Errorf("list invitation history: %w", err)
	}
	items := make([]MemberInvitation, 0, len(rows))
	for _, row := range rows {
		item, conversionErr := memberInvitationFromHistoryRow(row)
		if conversionErr != nil {
			return invitationIssuerSnapshot{}, nil, 0, conversionErr
		}
		items = append(items, item)
	}
	if err := tx.Commit(ctx); err != nil {
		return invitationIssuerSnapshot{}, nil, 0, fmt.Errorf("commit invitation overview: %w", err)
	}
	return invitationSnapshotFromContext(contextRow, int(used)), items, int(total), nil
}

// Issue locks both the policy singleton and member row before checking quota.
// Concurrent requests from one member therefore cannot over-issue codes.
func (repository *PostgresInvitationRepository) Issue(ctx context.Context, command IssueInvitationCommand) (MemberInvitation, error) {
	if command.ID == uuid.Nil || command.UserID == uuid.Nil || len(command.TokenSHA256) != invitationTokenDigestBytes ||
		command.OccurredAt.IsZero() || command.Authorization.ID == uuid.Nil || !command.Authorization.Allow {
		return MemberInvitation{}, ErrInvitationInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return MemberInvitation{}, fmt.Errorf("begin invitation issue: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := identitydb.New(tx)
	contextRow, err := queries.GetInvitationIssuerContextForUpdate(ctx, identitydb.GetInvitationIssuerContextForUpdateParams{
		AsOf: invitationTimestamp(command.OccurredAt), UserID: command.UserID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return MemberInvitation{}, ErrInvitationIneligible
	}
	if err != nil {
		return MemberInvitation{}, fmt.Errorf("lock invitation issuer context: %w", err)
	}
	used, err := queries.CountInvitationQuotaUsage(ctx, identitydb.CountInvitationQuotaUsageParams{
		UserID: invitationUUID(command.UserID), AsOf: invitationTimestamp(command.OccurredAt),
	})
	if err != nil || used < 0 || used > math.MaxInt {
		if err != nil {
			return MemberInvitation{}, fmt.Errorf("count invitation quota usage: %w", err)
		}
		return MemberInvitation{}, ErrInvitationInvariant
	}
	snapshot := invitationSnapshotFromLockedContext(contextRow, int(used))
	eligibility, err := invitationEligibility(snapshot, command.OccurredAt.UTC())
	if err != nil {
		return MemberInvitation{}, err
	}
	if !eligibility.Eligible {
		switch eligibility.Blocker {
		case InvitationBlockerDisabled:
			return MemberInvitation{}, ErrInvitationDisabled
		case InvitationBlockerQuotaExhausted:
			return MemberInvitation{}, ErrInvitationQuota
		default:
			return MemberInvitation{}, ErrInvitationIneligible
		}
	}
	row, err := queries.InsertMemberInvitation(ctx, identitydb.InsertMemberInvitationParams{
		ID: command.ID, TokenSha256: command.TokenSHA256,
		ExpiresAt: invitationTimestamp(command.OccurredAt.AddDate(0, 0, snapshot.InviteValidDays)),
		UserID:    invitationUUID(command.UserID), AuthorizationDecisionID: invitationUUID(command.Authorization.ID),
		CreatedAt: invitationTimestamp(command.OccurredAt),
	})
	if err != nil {
		return MemberInvitation{}, fmt.Errorf("insert member invitation: %w", err)
	}
	item, err := memberInvitationFromIssue(row.ID, row.CreatedAt, row.ExpiresAt)
	if err != nil {
		return MemberInvitation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MemberInvitation{}, fmt.Errorf("commit invitation issue: %w", err)
	}
	return item, nil
}

func (repository *PostgresInvitationRepository) Revoke(ctx context.Context, command RevokeInvitationCommand) (MemberInvitation, error) {
	if command.InvitationID == uuid.Nil || command.UserID == uuid.Nil || command.OccurredAt.IsZero() ||
		command.Authorization.ID == uuid.Nil || !command.Authorization.Allow {
		return MemberInvitation{}, ErrInvitationInput
	}
	row, err := identitydb.New(repository.pool).RevokeMemberInvitation(ctx, identitydb.RevokeMemberInvitationParams{
		RevokedAt: invitationTimestamp(command.OccurredAt), UserID: invitationUUID(command.UserID),
		AuthorizationDecisionID: invitationUUID(command.Authorization.ID), ID: command.InvitationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return MemberInvitation{}, ErrInvitationUnavailable
	}
	if err != nil {
		return MemberInvitation{}, fmt.Errorf("revoke member invitation: %w", err)
	}
	item, err := memberInvitationFromIssue(row.ID, row.CreatedAt, row.ExpiresAt)
	if err != nil || !row.RevokedAt.Valid {
		return MemberInvitation{}, ErrInvitationInvariant
	}
	revokedAt := row.RevokedAt.Time.UTC()
	item.Status = InvitationStatusRevoked
	item.RevokedAt = &revokedAt
	return item, nil
}

func invitationSnapshotFromContext(row identitydb.GetInvitationIssuerContextRow, used int) invitationIssuerSnapshot {
	return invitationIssuerSnapshot{
		MemberInvitesEnabled: row.MemberInvitesEnabled, InviteValidDays: int(row.InviteValidDays),
		MaxInvitesPerMember: int(row.MaxInvitesPerMember), MinimumInviteAccountAgeDays: int(row.MinimumInviteAccountAgeDays),
		MinimumInviteLevel: int(row.MinimumInviteLevel), Status: row.Status, EmailVerified: row.EmailVerified,
		CreatedAt: row.CreatedAt.Time.UTC(), CurrentLevel: int(row.CurrentLevel), AccountRestricted: row.AccountRestricted,
		UsedInvites: used,
	}
}

func invitationSnapshotFromLockedContext(row identitydb.GetInvitationIssuerContextForUpdateRow, used int) invitationIssuerSnapshot {
	return invitationIssuerSnapshot{
		MemberInvitesEnabled: row.MemberInvitesEnabled, InviteValidDays: int(row.InviteValidDays),
		MaxInvitesPerMember: int(row.MaxInvitesPerMember), MinimumInviteAccountAgeDays: int(row.MinimumInviteAccountAgeDays),
		MinimumInviteLevel: int(row.MinimumInviteLevel), Status: row.Status, EmailVerified: row.EmailVerified,
		CreatedAt: row.CreatedAt.Time.UTC(), CurrentLevel: int(row.CurrentLevel), AccountRestricted: row.AccountRestricted,
		UsedInvites: used,
	}
}

func memberInvitationFromHistoryRow(row identitydb.ListInvitationHistoryRow) (MemberInvitation, error) {
	status := InvitationStatus(row.Status)
	if status != InvitationStatusAvailable && status != InvitationStatusClaimed && status != InvitationStatusUsed &&
		status != InvitationStatusExpired && status != InvitationStatusRevoked {
		return MemberInvitation{}, ErrInvitationInvariant
	}
	item, err := memberInvitationFromIssue(row.ID, row.CreatedAt, row.ExpiresAt)
	if err != nil {
		return MemberInvitation{}, err
	}
	item.Status = status
	item.ClaimedAt = invitationOptionalTime(row.ClaimedAt)
	item.ConsumedAt = invitationOptionalTime(row.ConsumedAt)
	item.RevokedAt = invitationOptionalTime(row.RevokedAt)
	if row.InviteeUsername != "" {
		value := row.InviteeUsername
		item.InviteeUsername = &value
	}
	return item, nil
}

func memberInvitationFromIssue(id uuid.UUID, createdAt, expiresAt pgtype.Timestamptz) (MemberInvitation, error) {
	if id == uuid.Nil || !createdAt.Valid || !expiresAt.Valid || !expiresAt.Time.After(createdAt.Time) {
		return MemberInvitation{}, ErrInvitationInvariant
	}
	return MemberInvitation{
		ID: id, Status: InvitationStatusAvailable,
		CreatedAt: createdAt.Time.UTC(), ExpiresAt: expiresAt.Time.UTC(),
	}, nil
}

func invitationOptionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	normalized := value.Time.UTC()
	return &normalized
}

func invitationTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func invitationUUID(value uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: value, Valid: true}
}

var _ InvitationRepository = (*PostgresInvitationRepository)(nil)
