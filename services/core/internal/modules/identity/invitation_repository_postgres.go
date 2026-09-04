package identity

import (
	"context"
	"crypto/sha256"
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
	remaining, _, err := readInvitationBalance(ctx, tx, userID, false)
	if err != nil {
		return invitationIssuerSnapshot{}, nil, 0, InvitationNetwork{}, err
	}
	used, err := countInvitationCapUsage(ctx, tx, userID, now)
	if err != nil {
		return invitationIssuerSnapshot{}, nil, 0, InvitationNetwork{}, err
	}
	total, err := countInvitationHistory(ctx, tx, userID)
	if err != nil {
		return invitationIssuerSnapshot{}, nil, 0, InvitationNetwork{}, err
	}
	if used < 0 || total < 0 || used > math.MaxInt || total > math.MaxInt {
		return invitationIssuerSnapshot{}, nil, 0, InvitationNetwork{}, ErrInvitationInvariant
	}
	items, err := listInvitationHistory(ctx, tx, userID, now, limit, offset)
	if err != nil {
		return invitationIssuerSnapshot{}, nil, 0, InvitationNetwork{}, fmt.Errorf("list invitation history: %w", err)
	}
	network, err := readInvitationNetwork(ctx, tx, userID, now)
	if err != nil {
		return invitationIssuerSnapshot{}, nil, 0, InvitationNetwork{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return invitationIssuerSnapshot{}, nil, 0, InvitationNetwork{}, fmt.Errorf("commit invitation overview: %w", err)
	}
	return invitationSnapshotFromContext(contextRow, int(used), remaining), items, int(total), network, nil
}

func readInvitationBalance(ctx context.Context, tx pgx.Tx, userID uuid.UUID, forUpdate bool) (int, bool, error) {
	query := `
SELECT remaining_invites::bigint
FROM identity.invitation_accounts
WHERE user_id = $1`
	if forUpdate {
		query += " FOR UPDATE"
	}
	var balance int64
	err := tx.QueryRow(ctx, query, userID).Scan(&balance)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read invitation balance: %w", err)
	}
	if balance < 0 || balance > 1_000_000 || balance > math.MaxInt {
		return 0, true, ErrInvitationInvariant
	}
	return int(balance), true, nil
}

func countInvitationCapUsage(ctx context.Context, tx pgx.Tx, userID uuid.UUID, asOf time.Time) (int64, error) {
	var count int64
	err := tx.QueryRow(ctx, `
SELECT
    (SELECT count(*)::bigint
       FROM identity.registration_invitations AS invitation
      WHERE invitation.issuer_user_id = $1
        AND invitation.source_kind = 'member'
        AND invitation.revoked_at IS NULL
        AND (
            invitation.consumed_at IS NOT NULL
            OR invitation.claimed_by IS NOT NULL
            OR invitation.expires_at > $2
        ))
    +
    (SELECT count(*)::bigint
       FROM migration.legacy_invitation_code_openings AS opening
       LEFT JOIN identity.registration_invitations AS invitation
         ON invitation.id = opening.registration_invitation_id
      WHERE opening.inviter_user_id = $1
        AND (invitation.id IS NULL OR invitation.revoked_at IS NULL)
        AND (
            opening.source_claimed
            OR invitation.consumed_at IS NOT NULL
            OR invitation.claimed_by IS NOT NULL
            OR opening.source_valid_until > $2
        ))`, userID, asOf).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count invitation issuance usage: %w", err)
	}
	return count, nil
}

func countInvitationHistory(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (int64, error) {
	var count int64
	err := tx.QueryRow(ctx, `
SELECT
    (SELECT count(*)::bigint
       FROM identity.registration_invitations
      WHERE issuer_user_id = $1 AND source_kind = 'member')
    +
    (SELECT count(*)::bigint
       FROM migration.legacy_invitation_code_openings
      WHERE inviter_user_id = $1)`, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count invitation history: %w", err)
	}
	return count, nil
}

func listInvitationHistory(ctx context.Context, tx pgx.Tx, userID uuid.UUID, asOf time.Time, limit, offset int) ([]MemberInvitation, error) {
	rows, err := tx.Query(ctx, `
WITH history AS (
    SELECT
        invitation.id,
        'member'::text AS record_source,
        CASE
            WHEN invitation.revoked_at IS NOT NULL THEN 'revoked'
            WHEN invitation.consumed_at IS NOT NULL THEN 'used'
            WHEN invitation.claimed_by IS NOT NULL THEN 'claimed'
            WHEN invitation.expires_at <= $2 THEN 'expired'
            ELSE 'available'
        END::text AS status,
        COALESCE(CASE
            WHEN invitation.consumed_at IS NOT NULL THEN registration.username
            ELSE NULL
        END, '')::text AS invitee_username,
        invitation.created_at,
        invitation.expires_at,
        invitation.claimed_at,
        invitation.consumed_at,
        invitation.revoked_at,
        invitation.email_binding_hmac IS NOT NULL AS email_bound
    FROM identity.registration_invitations AS invitation
    LEFT JOIN identity.registrations AS registration
      ON registration.id = invitation.claimed_by
    WHERE invitation.issuer_user_id = $1
      AND invitation.source_kind = 'member'

    UNION ALL

    SELECT
        opening.invitation_id,
        'legacy_import'::text AS record_source,
        CASE
            WHEN native_invitation.revoked_at IS NOT NULL THEN 'revoked'
            WHEN native_invitation.consumed_at IS NOT NULL THEN 'used'
            WHEN native_invitation.claimed_by IS NOT NULL THEN 'claimed'
            WHEN opening.source_claimed THEN 'used'
            WHEN opening.source_valid_until <= $2 THEN 'expired'
            ELSE 'available'
        END::text AS status,
        COALESCE(native_registration.username, legacy_invitee.username, '')::text,
        opening.source_created_at,
        opening.source_valid_until,
        COALESCE(native_invitation.claimed_at, opening.source_claimed_at),
        COALESCE(native_invitation.consumed_at, opening.source_claimed_at),
        native_invitation.revoked_at,
        native_invitation.email_binding_hmac IS NOT NULL AS email_bound
    FROM migration.legacy_invitation_code_openings AS opening
    LEFT JOIN identity.users AS legacy_invitee
      ON legacy_invitee.id = opening.invitee_user_id
    LEFT JOIN identity.registration_invitations AS native_invitation
      ON native_invitation.id = opening.registration_invitation_id
    LEFT JOIN identity.registrations AS native_registration
      ON native_registration.id = native_invitation.claimed_by
    WHERE opening.inviter_user_id = $1
)
SELECT id, record_source, status, invitee_username,
       created_at, expires_at, claimed_at, consumed_at, revoked_at, email_bound
FROM history
ORDER BY created_at DESC, id DESC
LIMIT $3 OFFSET $4`, userID, asOf, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]MemberInvitation, 0, limit)
	for rows.Next() {
		var item MemberInvitation
		var source, status, inviteeUsername string
		var claimedAt, consumedAt, revokedAt pgtype.Timestamptz
		if err := rows.Scan(
			&item.ID, &source, &status, &inviteeUsername,
			&item.CreatedAt, &item.ExpiresAt, &claimedAt, &consumedAt, &revokedAt, &item.EmailBound,
		); err != nil {
			return nil, err
		}
		item.Source = InvitationRecordSource(source)
		item.Status = InvitationStatus(status)
		item.CreatedAt = item.CreatedAt.UTC()
		item.ExpiresAt = item.ExpiresAt.UTC()
		item.ClaimedAt = invitationOptionalTime(claimedAt)
		item.ConsumedAt = invitationOptionalTime(consumedAt)
		item.RevokedAt = invitationOptionalTime(revokedAt)
		if inviteeUsername != "" {
			item.InviteeUsername = &inviteeUsername
		}
		if err := validateInvitationHistoryItem(item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func validateInvitationHistoryItem(item MemberInvitation) error {
	validSource := item.Source == InvitationRecordMember || item.Source == InvitationRecordLegacyImport
	validStatus := item.Status == InvitationStatusAvailable || item.Status == InvitationStatusClaimed ||
		item.Status == InvitationStatusUsed || item.Status == InvitationStatusExpired || item.Status == InvitationStatusRevoked
	if item.ID == uuid.Nil || !validSource || !validStatus || item.CreatedAt.IsZero() || !item.ExpiresAt.After(item.CreatedAt) {
		return ErrInvitationInvariant
	}
	return nil
}

func readInvitationNetwork(ctx context.Context, tx pgx.Tx, userID uuid.UUID, asOf time.Time) (InvitationNetwork, error) {
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
	if err := readLiveHaremReward(ctx, tx, userID, asOf, &result); err != nil {
		return InvitationNetwork{}, err
	}
	rows, err := tx.Query(ctx, `
SELECT invited.numeric_id, invited.username, invited.display_name,
       relationship.source_kind, relationship.established_at,
       activity.last_active_at,
       latest.window_start, COALESCE(latest.eligible_torrent_count, 0)::integer,
       COALESCE(latest.reward, 0)::bigint,
       CASE
           WHEN policy.enabled
            AND invited.status = 'active'
            AND relationship.established_at <= latest.window_start + interval '1 hour'
            AND latest.reward > 0
            AND latest.eligible_torrent_count >= policy.minimum_seed_count
            AND (
                policy.activity_days = 0
                OR activity.last_active_at >= $2::timestamptz - (policy.activity_days::bigint * interval '1 day')
            )
           THEN floor((latest.reward::numeric * policy.reward_bps::numeric + 5) / 10)::bigint
           ELSE 0::bigint
       END AS contribution_milli,
       CASE
           WHEN policy.enabled
            AND invited.status = 'active'
            AND relationship.established_at <= latest.window_start + interval '1 hour'
            AND latest.reward > 0
            AND latest.eligible_torrent_count >= policy.minimum_seed_count
            AND (
                policy.activity_days = 0
                OR activity.last_active_at >= $2::timestamptz - (policy.activity_days::bigint * interval '1 day')
            )
           THEN true ELSE false
       END AS harem_eligible
FROM identity.invitation_relationships AS relationship
JOIN identity.users AS invited ON invited.id = relationship.invitee_user_id
LEFT JOIN identity.user_activity AS activity ON activity.user_id = invited.id
LEFT JOIN LATERAL (
    SELECT calculation.window_start, calculation.eligible_torrent_count,
           calculation.reward
    FROM economy.seeding_reward_calculations AS calculation
    WHERE calculation.user_id = invited.id
      AND calculation.window_start < date_trunc('hour', $2::timestamptz)
    ORDER BY calculation.window_start DESC
    LIMIT 1
) AS latest ON true
JOIN LATERAL (
    SELECT enabled, reward_bps, minimum_seed_count, activity_days
    FROM economy.harem_reward_policy_revisions
    WHERE effective_from <= $2
    ORDER BY effective_from DESC, revision DESC
    LIMIT 1
) AS policy ON true
WHERE relationship.inviter_user_id = $1
ORDER BY relationship.established_at DESC, relationship.invitee_user_id
LIMIT 100`, userID, asOf)
	if err != nil {
		return InvitationNetwork{}, fmt.Errorf("list directly invited members: %w", err)
	}
	defer rows.Close()
	result.DirectMembers = make([]InvitedMember, 0, result.DirectCount)
	for rows.Next() {
		var item InvitedMember
		var lastActiveAt, latestRewardWindow pgtype.Timestamptz
		if err := rows.Scan(
			&item.NumericID, &item.Username, &item.DisplayName,
			&item.Source, &item.EstablishedAt, &lastActiveAt,
			&latestRewardWindow, &item.CurrentSeedingCount,
			&item.CurrentSeedingReward, &item.CurrentContributionMilli,
			&item.HaremEligible,
		); err != nil {
			return InvitationNetwork{}, fmt.Errorf("scan directly invited member: %w", err)
		}
		item.EstablishedAt = item.EstablishedAt.UTC()
		item.LastActiveAt = invitationOptionalTime(lastActiveAt)
		item.LatestRewardWindow = invitationOptionalTime(latestRewardWindow)
		if item.NumericID < 1 || item.Username == "" || item.DisplayName == "" ||
			item.CurrentSeedingCount < 0 || item.CurrentSeedingReward < 0 || item.CurrentContributionMilli < 0 ||
			(item.Source != InvitationRelationshipRegistration && item.Source != InvitationRelationshipLegacyImport) {
			return InvitationNetwork{}, ErrInvitationInvariant
		}
		result.DirectMembers = append(result.DirectMembers, item)
	}
	if err := rows.Err(); err != nil {
		return InvitationNetwork{}, fmt.Errorf("finish directly invited member query: %w", err)
	}
	ancestorRows, err := tx.Query(ctx, `
WITH RECURSIVE ancestors AS (
    SELECT relationship.inviter_user_id AS user_id,
           relationship.source_kind, relationship.established_at,
           1 AS depth, ARRAY[$1::uuid, relationship.inviter_user_id] AS path
    FROM identity.invitation_relationships AS relationship
    WHERE relationship.invitee_user_id = $1
    UNION ALL
    SELECT relationship.inviter_user_id,
           relationship.source_kind, relationship.established_at,
           ancestors.depth + 1,
           ancestors.path || relationship.inviter_user_id
    FROM ancestors
    JOIN identity.invitation_relationships AS relationship
      ON relationship.invitee_user_id = ancestors.user_id
    WHERE ancestors.depth < 32
      AND NOT relationship.inviter_user_id = ANY(ancestors.path)
)
SELECT users.numeric_id, users.username, users.display_name,
       ancestors.source_kind, ancestors.established_at
FROM ancestors
JOIN identity.users AS users ON users.id = ancestors.user_id
ORDER BY ancestors.depth ASC`, userID)
	if err != nil {
		return InvitationNetwork{}, fmt.Errorf("list invitation ancestor chain: %w", err)
	}
	defer ancestorRows.Close()
	result.AncestorMembers = make([]InvitedMember, 0, 8)
	for ancestorRows.Next() {
		var item InvitedMember
		if err := ancestorRows.Scan(&item.NumericID, &item.Username, &item.DisplayName, &item.Source, &item.EstablishedAt); err != nil {
			return InvitationNetwork{}, fmt.Errorf("scan invitation ancestor: %w", err)
		}
		item.EstablishedAt = item.EstablishedAt.UTC()
		if item.NumericID < 1 || item.Username == "" || item.DisplayName == "" ||
			(item.Source != InvitationRelationshipRegistration && item.Source != InvitationRelationshipLegacyImport) {
			return InvitationNetwork{}, ErrInvitationInvariant
		}
		result.AncestorMembers = append(result.AncestorMembers, item)
	}
	if err := ancestorRows.Err(); err != nil {
		return InvitationNetwork{}, fmt.Errorf("finish invitation ancestor query: %w", err)
	}
	return result, nil
}

func readLiveHaremReward(ctx context.Context, tx pgx.Tx, userID uuid.UUID, asOf time.Time, network *InvitationNetwork) error {
	if network == nil || asOf.IsZero() {
		return ErrInvitationInvariant
	}
	var lastSettledAt pgtype.Timestamptz
	err := tx.QueryRow(ctx, `
WITH policy AS (
    SELECT revision, enabled, reward_bps, depth, minimum_seed_count,
           hourly_cap, activity_days, settlement_hours, effective_from
    FROM economy.harem_reward_policy_revisions
    WHERE effective_from <= $2
    ORDER BY effective_from DESC, revision DESC
    LIMIT 1
), live AS (
    SELECT COALESCE(sum(reward), 0)::bigint AS awarded_amount,
           count(*)::bigint AS settlement_count,
           max(reward_window.window_end) AS last_settled_at
    FROM economy.harem_reward_payouts AS payout
    JOIN economy.harem_reward_windows AS reward_window
      ON reward_window.window_start = payout.window_start
    WHERE payout.inviter_user_id = $1
), latest_sources AS (
    SELECT latest.reward
    FROM identity.invitation_relationships AS relationship
    JOIN identity.users AS invitee ON invitee.id = relationship.invitee_user_id
    LEFT JOIN identity.user_activity AS activity ON activity.user_id = invitee.id
    JOIN LATERAL (
        SELECT calculation.window_start, calculation.eligible_torrent_count,
               calculation.reward
        FROM economy.seeding_reward_calculations AS calculation
        WHERE calculation.user_id = relationship.invitee_user_id
          AND calculation.window_start < date_trunc('hour', $2::timestamptz)
        ORDER BY calculation.window_start DESC
        LIMIT 1
    ) AS latest ON true
    CROSS JOIN policy
    WHERE relationship.inviter_user_id = $1
      AND relationship.established_at <= latest.window_start + interval '1 hour'
      AND latest.reward > 0
      AND latest.eligible_torrent_count >= policy.minimum_seed_count
      AND invitee.status = 'active'
      AND (
          policy.activity_days = 0
          OR activity.last_active_at >= $2::timestamptz - (policy.activity_days::bigint * interval '1 day')
      )
)
SELECT policy.revision, policy.enabled, policy.reward_bps, policy.depth,
       policy.minimum_seed_count, policy.hourly_cap, policy.activity_days,
       policy.settlement_hours, policy.effective_from,
       CASE
           WHEN NOT policy.enabled THEN 0::bigint
           WHEN policy.hourly_cap = 0 THEN
               floor((COALESCE((SELECT sum(reward) FROM latest_sources), 0)::numeric * policy.reward_bps + 5) / 10)::bigint
           ELSE LEAST(
               floor((COALESCE((SELECT sum(reward) FROM latest_sources), 0)::numeric * policy.reward_bps + 5) / 10)::bigint,
               policy.hourly_cap * 1000
           )
       END AS current_estimate_milli,
       live.awarded_amount, live.settlement_count, live.last_settled_at
FROM policy CROSS JOIN live`, userID, asOf).Scan(
		&network.LiveHaremReward.Policy.Revision,
		&network.LiveHaremReward.Policy.Enabled,
		&network.LiveHaremReward.Policy.RewardBPS,
		&network.LiveHaremReward.Policy.Depth,
		&network.LiveHaremReward.Policy.MinimumSeedCount,
		&network.LiveHaremReward.Policy.HourlyCap,
		&network.LiveHaremReward.Policy.ActivityDays,
		&network.LiveHaremReward.Policy.SettlementHours,
		&network.LiveHaremReward.Policy.EffectiveFrom,
		&network.LiveHaremReward.CurrentHourlyEstimateMilli,
		&network.LiveHaremReward.AwardedAmount,
		&network.LiveHaremReward.SettlementCount,
		&lastSettledAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvitationInvariant
	}
	if err != nil {
		return fmt.Errorf("read live harem reward: %w", err)
	}
	network.LiveHaremReward.Policy.EffectiveFrom = network.LiveHaremReward.Policy.EffectiveFrom.UTC()
	network.LiveHaremReward.LastSettledAt = invitationOptionalTime(lastSettledAt)
	if network.LiveHaremReward.Policy.Revision == "" || network.LiveHaremReward.Policy.RewardBPS < 0 ||
		network.LiveHaremReward.Policy.Depth < 1 || network.LiveHaremReward.Policy.MinimumSeedCount < 0 ||
		network.LiveHaremReward.Policy.HourlyCap < 0 || network.LiveHaremReward.Policy.SettlementHours < 1 ||
		network.LiveHaremReward.CurrentHourlyEstimateMilli < 0 || network.LiveHaremReward.AwardedAmount < 0 ||
		network.LiveHaremReward.SettlementCount < 0 {
		return ErrInvitationInvariant
	}
	return nil
}

// Issue locks both the policy singleton and member row before checking quota.
// Concurrent requests from one member therefore cannot over-issue codes.
func (repository *PostgresInvitationRepository) Issue(ctx context.Context, command IssueInvitationCommand) (MemberInvitation, error) {
	if command.ID == uuid.Nil || command.UserID == uuid.Nil || len(command.TokenSHA256) != invitationTokenDigestBytes ||
		len(command.EmailBindingHMAC) != sha256.Size ||
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
	remaining, accountExists, err := readInvitationBalance(ctx, tx, command.UserID, true)
	if err != nil {
		return MemberInvitation{}, err
	}
	used, err := countInvitationCapUsage(ctx, tx, command.UserID, command.OccurredAt)
	if err != nil || used < 0 || used > math.MaxInt {
		if err != nil {
			return MemberInvitation{}, err
		}
		return MemberInvitation{}, ErrInvitationInvariant
	}
	snapshot := invitationSnapshotFromLockedContext(contextRow, int(used), remaining)
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
		ID: command.ID, TokenSha256: command.TokenSHA256, EmailBindingHmac: command.EmailBindingHMAC,
		ExpiresAt: invitationTimestamp(command.OccurredAt.AddDate(0, 0, snapshot.InviteValidDays)),
		UserID:    invitationUUID(command.UserID), AuthorizationDecisionID: invitationUUID(command.Authorization.ID),
		CreatedAt: invitationTimestamp(command.OccurredAt),
	})
	if err != nil {
		return MemberInvitation{}, fmt.Errorf("insert member invitation: %w", err)
	}
	if !accountExists {
		return MemberInvitation{}, ErrInvitationQuota
	}
	var balanceAfter int
	if err := tx.QueryRow(ctx, `
UPDATE identity.invitation_accounts
SET remaining_invites = remaining_invites - 1,
    version = version + 1,
    updated_at = $2
WHERE user_id = $1 AND remaining_invites > 0
RETURNING remaining_invites`, command.UserID, command.OccurredAt).Scan(&balanceAfter); errors.Is(err, pgx.ErrNoRows) {
		return MemberInvitation{}, ErrInvitationQuota
	} else if err != nil {
		return MemberInvitation{}, fmt.Errorf("debit invitation balance: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.invitation_balance_events (
    id, user_id, invitation_id, event_kind, delta, balance_after,
    authorization_decision_id, source_reference, occurred_at, recorded_at
) VALUES ($1,$2,$3,'issued',-1,$4,$5,$6,$7,$7)`,
		uuid.New(), command.UserID, command.ID, balanceAfter,
		command.Authorization.ID, "member-invitation:"+command.ID.String()+":issued",
		command.OccurredAt,
	); err != nil {
		return MemberInvitation{}, fmt.Errorf("record invitation balance debit: %w", err)
	}
	item, err := memberInvitationFromIssue(row.ID, row.CreatedAt, row.ExpiresAt)
	if err != nil {
		return MemberInvitation{}, err
	}
	item.EmailBound = true
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
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return MemberInvitation{}, fmt.Errorf("begin invitation revocation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id uuid.UUID
	var sourceKind string
	var createdAt, expiresAt, revokedAt pgtype.Timestamptz
	var emailBound bool
	err = tx.QueryRow(ctx, `
UPDATE identity.registration_invitations
SET revoked_at = $1,
    revoked_by = $2,
    revoked_authorization_decision_id = $3
WHERE id = $4
  AND issuer_user_id = $2
  AND source_kind IN ('member', 'legacy')
  AND claimed_by IS NULL
  AND consumed_at IS NULL
  AND revoked_at IS NULL
RETURNING id, source_kind, created_at, expires_at, revoked_at,
          email_binding_hmac IS NOT NULL`,
		command.OccurredAt, command.UserID, command.Authorization.ID, command.InvitationID,
	).Scan(&id, &sourceKind, &createdAt, &expiresAt, &revokedAt, &emailBound)
	if errors.Is(err, pgx.ErrNoRows) {
		return MemberInvitation{}, ErrInvitationUnavailable
	}
	if err != nil {
		return MemberInvitation{}, fmt.Errorf("revoke member invitation: %w", err)
	}
	var balanceAfter int
	if err := tx.QueryRow(ctx, `
UPDATE identity.invitation_accounts
SET remaining_invites = remaining_invites + 1,
    version = version + 1,
    updated_at = $2
WHERE user_id = $1 AND remaining_invites < 1000000
RETURNING remaining_invites`, command.UserID, command.OccurredAt).Scan(&balanceAfter); errors.Is(err, pgx.ErrNoRows) {
		return MemberInvitation{}, ErrInvitationInvariant
	} else if err != nil {
		return MemberInvitation{}, fmt.Errorf("refund invitation balance: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.invitation_balance_events (
    id, user_id, invitation_id, event_kind, delta, balance_after,
    authorization_decision_id, source_reference, occurred_at, recorded_at
) VALUES ($1,$2,$3,'revoked',1,$4,$5,$6,$7,$7)`,
		uuid.New(), command.UserID, command.InvitationID, balanceAfter,
		command.Authorization.ID, "member-invitation:"+command.InvitationID.String()+":revoked",
		command.OccurredAt,
	); err != nil {
		return MemberInvitation{}, fmt.Errorf("record invitation balance refund: %w", err)
	}
	item, err := memberInvitationFromIssue(id, createdAt, expiresAt)
	if err != nil || !revokedAt.Valid {
		return MemberInvitation{}, ErrInvitationInvariant
	}
	if sourceKind == "legacy" {
		item.Source = InvitationRecordLegacyImport
	} else if sourceKind != "member" {
		return MemberInvitation{}, ErrInvitationInvariant
	}
	revokedAtValue := revokedAt.Time.UTC()
	item.EmailBound = emailBound
	item.Status = InvitationStatusRevoked
	item.RevokedAt = &revokedAtValue
	if err := tx.Commit(ctx); err != nil {
		return MemberInvitation{}, fmt.Errorf("commit invitation revocation: %w", err)
	}
	return item, nil
}

func invitationSnapshotFromContext(row identitydb.GetInvitationIssuerContextRow, used, remaining int) invitationIssuerSnapshot {
	return invitationIssuerSnapshot{
		MemberInvitesEnabled: row.MemberInvitesEnabled, InviteValidDays: int(row.InviteValidDays),
		MaxInvitesPerMember: int(row.MaxInvitesPerMember), MinimumInviteAccountAgeDays: int(row.MinimumInviteAccountAgeDays),
		MinimumInviteLevel: int(row.MinimumInviteLevel), Status: row.Status, EmailVerified: row.EmailVerified,
		CreatedAt: row.CreatedAt.Time.UTC(), CurrentLevel: int(row.CurrentLevel), AccountRestricted: row.AccountRestricted,
		UsedInvites: used, RemainingInvites: remaining,
	}
}

func invitationSnapshotFromLockedContext(row identitydb.GetInvitationIssuerContextForUpdateRow, used, remaining int) invitationIssuerSnapshot {
	return invitationIssuerSnapshot{
		MemberInvitesEnabled: row.MemberInvitesEnabled, InviteValidDays: int(row.InviteValidDays),
		MaxInvitesPerMember: int(row.MaxInvitesPerMember), MinimumInviteAccountAgeDays: int(row.MinimumInviteAccountAgeDays),
		MinimumInviteLevel: int(row.MinimumInviteLevel), Status: row.Status, EmailVerified: row.EmailVerified,
		CreatedAt: row.CreatedAt.Time.UTC(), CurrentLevel: int(row.CurrentLevel), AccountRestricted: row.AccountRestricted,
		UsedInvites: used, RemainingInvites: remaining,
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
		ID: id, Source: InvitationRecordMember, Status: InvitationStatusAvailable,
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
