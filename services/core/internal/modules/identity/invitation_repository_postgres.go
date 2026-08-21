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
func (repository *PostgresInvitationRepository) Overview(ctx context.Context, userID uuid.UUID, now time.Time, limit, offset int) (invitationIssuerSnapshot, []MemberInvitation, int, InvitationNetwork, error) {
	if userID == uuid.Nil || now.IsZero() || limit < 1 || limit > MaxInvitationHistoryLimit || offset < 0 || offset > MaxInvitationHistoryOffset {
		return invitationIssuerSnapshot{}, nil, 0, InvitationNetwork{}, ErrInvitationInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return invitationIssuerSnapshot{}, nil, 0, InvitationNetwork{}, fmt.Errorf("begin invitation overview: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := identitydb.New(tx)
	contextRow, err := queries.GetInvitationIssuerContext(ctx, identitydb.GetInvitationIssuerContextParams{
		AsOf: invitationTimestamp(now), UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return invitationIssuerSnapshot{}, nil, 0, InvitationNetwork{}, ErrInvitationIneligible
	}
	if err != nil {
		return invitationIssuerSnapshot{}, nil, 0, InvitationNetwork{}, fmt.Errorf("get invitation issuer context: %w", err)
	}
	used, err := queries.CountInvitationQuotaUsage(ctx, identitydb.CountInvitationQuotaUsageParams{UserID: invitationUUID(userID), AsOf: invitationTimestamp(now)})
	if err != nil {
		return invitationIssuerSnapshot{}, nil, 0, InvitationNetwork{}, fmt.Errorf("count invitation quota usage: %w", err)
	}
	total, err := queries.CountInvitationHistory(ctx, invitationUUID(userID))
	if err != nil {
		return invitationIssuerSnapshot{}, nil, 0, InvitationNetwork{}, fmt.Errorf("count invitation history: %w", err)
	}
	if used < 0 || total < 0 || used > math.MaxInt || total > math.MaxInt {
		return invitationIssuerSnapshot{}, nil, 0, InvitationNetwork{}, ErrInvitationInvariant
	}
	rows, err := queries.ListInvitationHistory(ctx, identitydb.ListInvitationHistoryParams{
		AsOf: invitationTimestamp(now), UserID: invitationUUID(userID),
		ResultLimit: int32(limit), ResultOffset: int32(offset),
	})
	if err != nil {
		return invitationIssuerSnapshot{}, nil, 0, InvitationNetwork{}, fmt.Errorf("list invitation history: %w", err)
	}
	items := make([]MemberInvitation, 0, len(rows))
	for _, row := range rows {
		item, conversionErr := memberInvitationFromHistoryRow(row)
		if conversionErr != nil {
			return invitationIssuerSnapshot{}, nil, 0, InvitationNetwork{}, conversionErr
		}
		items = append(items, item)
	}
	network, err := readInvitationNetwork(ctx, tx, userID)
	if err != nil {
		return invitationIssuerSnapshot{}, nil, 0, InvitationNetwork{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return invitationIssuerSnapshot{}, nil, 0, InvitationNetwork{}, fmt.Errorf("commit invitation overview: %w", err)
	}
	return invitationSnapshotFromContext(contextRow, int(used)), items, int(total), network, nil
}

func readInvitationNetwork(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (InvitationNetwork, error) {
	var result InvitationNetwork
	var directCount, descendantCount int64
	var haremLast, invitationLast pgtype.Timestamptz
	var haremAmount, invitationAmount int64
	var haremRows, invitationRows int64
	err := tx.QueryRow(ctx, `
WITH RECURSIVE descendants AS (
    SELECT relationship.invitee_user_id, 1 AS depth,
           ARRAY[$1::uuid, relationship.invitee_user_id] AS path
    FROM identity.invitation_relationships AS relationship
    WHERE relationship.inviter_user_id = $1
    UNION ALL
    SELECT relationship.invitee_user_id, descendants.depth + 1,
           descendants.path || relationship.invitee_user_id
    FROM descendants
    JOIN identity.invitation_relationships AS relationship
      ON relationship.inviter_user_id = descendants.invitee_user_id
    WHERE descendants.depth < 32
      AND NOT relationship.invitee_user_id = ANY(descendants.path)
), rewards AS (
    SELECT
        COALESCE(sum(rounded_amount) FILTER (WHERE reward_kind = 'harem'), 0)::bigint AS harem_amount,
        COALESCE(sum(source_row_count) FILTER (WHERE reward_kind = 'harem'), 0)::bigint AS harem_rows,
        max(last_rewarded_at) FILTER (WHERE reward_kind = 'harem') AS harem_last,
        COALESCE(sum(rounded_amount) FILTER (WHERE reward_kind = 'invite_reward'), 0)::bigint AS invitation_amount,
        COALESCE(sum(source_row_count) FILTER (WHERE reward_kind = 'invite_reward'), 0)::bigint AS invitation_rows,
        max(last_rewarded_at) FILTER (WHERE reward_kind = 'invite_reward') AS invitation_last
    FROM migration.legacy_invitation_reward_openings
    WHERE user_id = $1 AND disposition = 'preserved'
)
SELECT
    count(descendants.invitee_user_id) FILTER (WHERE descendants.depth = 1)::bigint,
    count(descendants.invitee_user_id)::bigint,
    rewards.harem_amount, rewards.harem_rows, rewards.harem_last,
    rewards.invitation_amount, rewards.invitation_rows, rewards.invitation_last
FROM rewards
LEFT JOIN descendants ON true
GROUP BY rewards.harem_amount, rewards.harem_rows, rewards.harem_last,
         rewards.invitation_amount, rewards.invitation_rows, rewards.invitation_last`, userID).Scan(
		&directCount, &descendantCount,
		&haremAmount, &haremRows, &haremLast,
		&invitationAmount, &invitationRows, &invitationLast,
	)
	if err != nil {
		return InvitationNetwork{}, fmt.Errorf("read invitation network summary: %w", err)
	}
	if directCount < 0 || descendantCount < directCount || directCount > math.MaxInt || descendantCount > math.MaxInt {
		return InvitationNetwork{}, ErrInvitationInvariant
	}
	result.DirectCount = int(directCount)
	result.TotalDescendants = int(descendantCount)
	result.HaremReward = HistoricalInvitationReward{
		Amount: haremAmount, SourceRows: haremRows, LastRewardedAt: invitationOptionalTime(haremLast),
	}
	result.InvitationReward = HistoricalInvitationReward{
		Amount: invitationAmount, SourceRows: invitationRows, LastRewardedAt: invitationOptionalTime(invitationLast),
	}
	rows, err := tx.Query(ctx, `
SELECT invited.numeric_id, invited.username, invited.display_name,
       relationship.source_kind, relationship.established_at
FROM identity.invitation_relationships AS relationship
JOIN identity.users AS invited ON invited.id = relationship.invitee_user_id
WHERE relationship.inviter_user_id = $1
ORDER BY relationship.established_at DESC, relationship.invitee_user_id
LIMIT 100`, userID)
	if err != nil {
		return InvitationNetwork{}, fmt.Errorf("list directly invited members: %w", err)
	}
	defer rows.Close()
	result.DirectMembers = make([]InvitedMember, 0, result.DirectCount)
	for rows.Next() {
		var item InvitedMember
		if err := rows.Scan(&item.NumericID, &item.Username, &item.DisplayName, &item.Source, &item.EstablishedAt); err != nil {
			return InvitationNetwork{}, fmt.Errorf("scan directly invited member: %w", err)
		}
		item.EstablishedAt = item.EstablishedAt.UTC()
		if item.NumericID < 1 || item.Username == "" || item.DisplayName == "" ||
			(item.Source != InvitationRelationshipRegistration && item.Source != InvitationRelationshipLegacyImport) {
			return InvitationNetwork{}, ErrInvitationInvariant
		}
		result.DirectMembers = append(result.DirectMembers, item)
	}
	if err := rows.Err(); err != nil {
		return InvitationNetwork{}, fmt.Errorf("finish directly invited member query: %w", err)
	}
	return result, nil
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
