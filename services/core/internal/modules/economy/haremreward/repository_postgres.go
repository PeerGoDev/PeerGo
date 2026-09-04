package haremreward

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/services/core/internal/modules/economy"
)

var payoutTransactionNamespace = uuid.MustParse("55fb0a29-e60d-5f6c-bc6d-42a491cbeb47")

type PostgresRepository struct {
	pool   *pgxpool.Pool
	ledger *economy.PostgresRepository
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, ErrInput
	}
	ledger, err := economy.NewPostgresRepository(pool)
	if err != nil {
		return nil, err
	}
	return &PostgresRepository{pool: pool, ledger: ledger}, nil
}

func (repository *PostgresRepository) SettleNext(ctx context.Context, now time.Time) (Settlement, error) {
	now = canonicalTime(now)
	if now.IsZero() {
		return Settlement{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Settlement{}, fmt.Errorf("begin harem reward settlement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('peergo-harem-reward-settlement'))`).Scan(&locked); err != nil {
		return Settlement{}, fmt.Errorf("lock harem reward settlement: %w", err)
	}
	if !locked {
		return Settlement{}, nil
	}
	if _, err := tx.Exec(ctx, `
UPDATE economy.harem_reward_worker_state
SET last_started_at = $1, last_error_code = NULL
WHERE singleton = true`, now); err != nil {
		return Settlement{}, fmt.Errorf("start harem reward heartbeat: %w", err)
	}

	policy, windowStart, found, err := nextPolicyAndWindow(ctx, tx)
	if err != nil {
		return Settlement{}, err
	}
	if !found {
		return repository.finishIdle(ctx, tx, now)
	}
	windowEnd := windowStart.Add(time.Duration(policy.SettlementHours) * time.Hour)
	if windowEnd.After(now.Truncate(time.Hour)) {
		return repository.finishIdle(ctx, tx, now)
	}
	ready, sourceCalculationCount, err := sourceWindowReady(ctx, tx, windowStart, windowEnd, policy.SettlementHours)
	if err != nil {
		return Settlement{}, err
	}
	if !ready {
		return repository.finishIdle(ctx, tx, now)
	}

	payouts := []Payout{}
	eligibleRelationshipCount := int64(0)
	if policy.Enabled {
		payouts, eligibleRelationshipCount, err = readPayouts(ctx, tx, policy, windowStart, windowEnd)
		if err != nil {
			return Settlement{}, err
		}
	}
	var totalReward int64
	for index := range payouts {
		payouts[index].PayloadSHA256, err = payoutDigest(policy, windowStart, windowEnd, payouts[index])
		if err != nil {
			return Settlement{}, err
		}
		if totalReward > int64(^uint64(0)>>1)-payouts[index].Reward {
			return Settlement{}, ErrInvariant
		}
		totalReward += payouts[index].Reward
	}
	windowDigest, err := settlementDigest(policy, windowStart, windowEnd, sourceCalculationCount, eligibleRelationshipCount, payouts, totalReward)
	if err != nil {
		return Settlement{}, err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO economy.harem_reward_windows (
    window_start, window_end, policy_revision,
    source_calculation_count, eligible_relationship_count,
    recipient_count, total_reward, payload_sha256, completed_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		windowStart, windowEnd, policy.Revision, sourceCalculationCount,
		eligibleRelationshipCount, len(payouts), totalReward, windowDigest[:], now,
	); err != nil {
		return Settlement{}, fmt.Errorf("insert harem reward window: %w", err)
	}

	for _, payout := range payouts {
		sourceReference := fmt.Sprintf("harem:%d:%s", windowStart.Unix(), payout.InviterUserID)
		transactionID := uuid.NewSHA1(payoutTransactionNamespace, []byte(sourceReference))
		if _, err := repository.ledger.RecordInTransaction(ctx, tx, economy.RecordCommand{
			TransactionID: transactionID, TransactionType: economy.TransactionHaremReward,
			IdempotencyKey:  "harem_reward:" + payout.InviterUserID.String() + ":" + fmt.Sprint(windowStart.Unix()),
			SourceReference: sourceReference, PolicyRevision: policy.Revision,
			PayloadSHA256: payout.PayloadSHA256, OccurredAt: windowEnd, RecordedAt: now,
			Postings: []economy.PostingInput{
				{AccountID: payout.InviterUserID, Amount: payout.Reward},
				{AccountID: economy.HaremMintAccountID(), Amount: -payout.Reward},
			},
		}); err != nil {
			return Settlement{}, fmt.Errorf("record harem reward: %w", err)
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO economy.harem_reward_payouts (
    window_start, inviter_user_id, policy_revision,
    eligible_invitee_count, eligible_invitee_hours,
    source_seeding_reward, capped_hour_count, reward,
    payload_sha256, magic_transaction_id, settled_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			windowStart, payout.InviterUserID, policy.Revision,
			payout.EligibleInviteeCount, payout.EligibleInviteeHours,
			payout.SourceSeedingReward, payout.CappedHourCount, payout.Reward,
			payout.PayloadSHA256[:], transactionID, now,
		); err != nil {
			return Settlement{}, fmt.Errorf("insert harem reward payout: %w", err)
		}
	}

	result := Settlement{
		Processed: true, WindowStart: windowStart, WindowEnd: windowEnd,
		PolicyRevision: policy.Revision, SourceCalculationCount: sourceCalculationCount,
		EligibleRelationshipCount: eligibleRelationshipCount,
		RecipientCount:            len(payouts), TotalReward: totalReward,
	}
	if _, err := tx.Exec(ctx, `
UPDATE economy.harem_reward_worker_state
SET last_completed_at = $1, last_error_code = NULL,
    last_window_start = $2, last_window_end = $3,
    last_recipient_count = $4, last_total_reward = $5,
    run_count = run_count + 1
WHERE singleton = true`, now, windowStart, windowEnd, len(payouts), totalReward); err != nil {
		return Settlement{}, fmt.Errorf("complete harem reward heartbeat: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Settlement{}, fmt.Errorf("commit harem reward settlement: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) finishIdle(ctx context.Context, tx pgx.Tx, now time.Time) (Settlement, error) {
	if _, err := tx.Exec(ctx, `
UPDATE economy.harem_reward_worker_state
SET last_completed_at = $1, last_error_code = NULL,
    last_recipient_count = 0, last_total_reward = 0,
    run_count = run_count + 1
WHERE singleton = true`, now); err != nil {
		return Settlement{}, fmt.Errorf("complete idle harem reward heartbeat: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Settlement{}, fmt.Errorf("commit idle harem reward heartbeat: %w", err)
	}
	return Settlement{}, nil
}

func nextPolicyAndWindow(ctx context.Context, tx pgx.Tx) (Policy, time.Time, bool, error) {
	var start time.Time
	err := tx.QueryRow(ctx, `
SELECT COALESCE(
    (SELECT max(window_end) FROM economy.harem_reward_windows),
    (SELECT min(effective_from) FROM economy.harem_reward_policy_revisions)
)`).Scan(&start)
	if errors.Is(err, pgx.ErrNoRows) || start.IsZero() {
		return Policy{}, time.Time{}, false, nil
	}
	if err != nil {
		return Policy{}, time.Time{}, false, fmt.Errorf("read next harem reward window: %w", err)
	}
	policy, found, err := readPolicy(ctx, tx, start)
	return policy, canonicalTime(start), found, err
}

func readPolicy(ctx context.Context, tx pgx.Tx, at time.Time) (Policy, bool, error) {
	var policy Policy
	var digest []byte
	err := tx.QueryRow(ctx, `
SELECT revision, enabled, reward_bps, depth, minimum_seed_count,
       hourly_cap, activity_days, settlement_hours, effective_from,
       snapshot_sha256, created_at
FROM economy.harem_reward_policy_revisions
WHERE effective_from <= $1
ORDER BY effective_from DESC, revision DESC
LIMIT 1`, at).Scan(
		&policy.Revision, &policy.Enabled, &policy.RewardBPS, &policy.Depth,
		&policy.MinimumSeedCount, &policy.HourlyCap, &policy.ActivityDays,
		&policy.SettlementHours, &policy.EffectiveFrom, &digest, &policy.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Policy{}, false, nil
	}
	if err != nil {
		return Policy{}, true, fmt.Errorf("read harem reward policy: %w", err)
	}
	if len(digest) != sha256.Size || policy.Revision == "" || policy.RewardBPS < 0 ||
		policy.Depth < 1 || policy.MinimumSeedCount < 0 || policy.HourlyCap < 0 ||
		policy.SettlementHours < 1 || 24%policy.SettlementHours != 0 {
		return Policy{}, true, ErrInvariant
	}
	copy(policy.SnapshotSHA256[:], digest)
	policy.EffectiveFrom, policy.CreatedAt = canonicalTime(policy.EffectiveFrom), canonicalTime(policy.CreatedAt)
	return policy, true, nil
}

func sourceWindowReady(ctx context.Context, tx pgx.Tx, start, end time.Time, expectedHours int16) (bool, int64, error) {
	var completeHours, calculationCount, incompleteWork int64
	err := tx.QueryRow(ctx, `
WITH hours AS (
    SELECT hour_start
    FROM generate_series($1::timestamptz, $2::timestamptz - interval '1 hour', interval '1 hour') AS hour_start
)
SELECT
    count(*) FILTER (WHERE evidence.status = 'complete')::bigint,
    COALESCE((
        SELECT count(*)::bigint
        FROM economy.seeding_reward_calculations
        WHERE window_start >= $1 AND window_start < $2
    ), 0),
    COALESCE((
        SELECT count(*)::bigint
        FROM economy.seeding_reward_work_items
        WHERE window_start >= $1 AND window_start < $2
          AND status <> 'completed'
    ), 0)
FROM hours
LEFT JOIN economy.seeding_reward_evidence_windows AS evidence
  ON evidence.window_start = hours.hour_start`, start, end).Scan(&completeHours, &calculationCount, &incompleteWork)
	if err != nil {
		return false, 0, fmt.Errorf("inspect harem reward source windows: %w", err)
	}
	return completeHours == int64(expectedHours) && incompleteWork == 0, calculationCount, nil
}

func readPayouts(ctx context.Context, tx pgx.Tx, policy Policy, start, end time.Time) ([]Payout, int64, error) {
	rows, err := tx.Query(ctx, `
WITH RECURSIVE ancestry AS (
    SELECT relationship.inviter_user_id,
           relationship.invitee_user_id,
           relationship.established_at,
           1::integer AS depth,
           ARRAY[relationship.inviter_user_id, relationship.invitee_user_id] AS path
    FROM identity.invitation_relationships AS relationship

    UNION ALL

    SELECT relationship.inviter_user_id,
           ancestry.invitee_user_id,
           GREATEST(ancestry.established_at, relationship.established_at),
           ancestry.depth + 1,
           ancestry.path || relationship.inviter_user_id
    FROM ancestry
    JOIN identity.invitation_relationships AS relationship
      ON relationship.invitee_user_id = ancestry.inviter_user_id
    WHERE ancestry.depth < $3
      AND NOT relationship.inviter_user_id = ANY(ancestry.path)
), sources AS (
    SELECT ancestry.inviter_user_id,
           calculation.window_start,
           calculation.user_id AS invitee_user_id,
           calculation.reward
    FROM ancestry
    JOIN economy.seeding_reward_calculations AS calculation
      ON calculation.user_id = ancestry.invitee_user_id
     AND calculation.window_start >= $1
     AND calculation.window_start < $2
    JOIN identity.users AS inviter ON inviter.id = ancestry.inviter_user_id
    JOIN identity.users AS invitee ON invitee.id = ancestry.invitee_user_id
    LEFT JOIN identity.user_activity AS activity ON activity.user_id = ancestry.invitee_user_id
    WHERE ancestry.established_at <= calculation.window_start + interval '1 hour'
      AND calculation.reward > 0
      AND calculation.eligible_torrent_count >= $4
      AND inviter.status = 'active'
      AND invitee.status = 'active'
      AND (
          $5 = 0
          OR activity.last_active_at >= calculation.window_start - ($5::bigint * interval '1 day')
      )
), hourly AS (
    SELECT inviter_user_id, window_start,
           CASE
               WHEN $7 = 0 THEN sum(reward)::numeric * $6::numeric
               ELSE LEAST(sum(reward)::numeric * $6::numeric, $7::numeric * 10000)
           END AS reward_numerator,
           CASE WHEN $7 > 0 AND sum(reward)::numeric * $6::numeric > $7::numeric * 10000
                THEN 1 ELSE 0 END::integer AS capped
    FROM sources
    GROUP BY inviter_user_id, window_start
), source_totals AS (
    SELECT inviter_user_id,
           count(DISTINCT invitee_user_id)::integer AS eligible_invitee_count,
           count(*)::integer AS eligible_invitee_hours,
           sum(reward)::bigint AS source_seeding_reward
    FROM sources
    GROUP BY inviter_user_id
), payouts AS (
    SELECT source_totals.inviter_user_id,
           source_totals.eligible_invitee_count,
           source_totals.eligible_invitee_hours,
           source_totals.source_seeding_reward,
           sum(hourly.capped)::integer AS capped_hour_count,
           floor((sum(hourly.reward_numerator) + 5000) / 10000)::bigint AS reward
    FROM source_totals
    JOIN hourly USING (inviter_user_id)
    GROUP BY source_totals.inviter_user_id,
             source_totals.eligible_invitee_count,
             source_totals.eligible_invitee_hours,
             source_totals.source_seeding_reward
), summary AS (
    SELECT count(DISTINCT (inviter_user_id, invitee_user_id))::bigint AS relationship_count
    FROM sources
)
SELECT COALESCE(payouts.inviter_user_id, '00000000-0000-0000-0000-000000000000'::uuid),
       COALESCE(payouts.eligible_invitee_count, 0)::integer,
       COALESCE(payouts.eligible_invitee_hours, 0)::integer,
       COALESCE(payouts.source_seeding_reward, 0)::bigint,
       COALESCE(payouts.capped_hour_count, 0)::integer,
       COALESCE(payouts.reward, 0)::bigint,
       summary.relationship_count
FROM summary
LEFT JOIN payouts ON payouts.reward > 0
ORDER BY payouts.inviter_user_id`, start, end, policy.Depth, policy.MinimumSeedCount,
		policy.ActivityDays, policy.RewardBPS, policy.HourlyCap)
	if err != nil {
		return nil, 0, fmt.Errorf("calculate harem reward payouts: %w", err)
	}
	defer rows.Close()
	payouts := make([]Payout, 0)
	var relationshipCount int64
	for rows.Next() {
		var payout Payout
		if err := rows.Scan(
			&payout.InviterUserID, &payout.EligibleInviteeCount,
			&payout.EligibleInviteeHours, &payout.SourceSeedingReward,
			&payout.CappedHourCount, &payout.Reward, &relationshipCount,
		); err != nil {
			return nil, 0, fmt.Errorf("scan harem reward payout: %w", err)
		}
		if payout.InviterUserID == uuid.Nil {
			if payout.EligibleInviteeCount != 0 || payout.EligibleInviteeHours != 0 ||
				payout.SourceSeedingReward != 0 || payout.CappedHourCount != 0 || payout.Reward != 0 {
				return nil, 0, ErrInvariant
			}
			continue
		}
		if payout.InviterUserID == uuid.Nil || payout.EligibleInviteeCount < 1 ||
			payout.EligibleInviteeHours < 1 || payout.SourceSeedingReward < 1 || payout.Reward < 1 {
			return nil, 0, ErrInvariant
		}
		payouts = append(payouts, payout)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("finish harem reward payouts: %w", err)
	}
	return payouts, relationshipCount, nil
}

type payoutDocument struct {
	PolicyRevision       string `json:"policy_revision"`
	WindowStart          string `json:"window_start"`
	WindowEnd            string `json:"window_end"`
	InviterUserID        string `json:"inviter_user_id"`
	EligibleInviteeCount int32  `json:"eligible_invitee_count"`
	EligibleInviteeHours int32  `json:"eligible_invitee_hours"`
	SourceSeedingReward  int64  `json:"source_seeding_reward"`
	CappedHourCount      int32  `json:"capped_hour_count"`
	Reward               int64  `json:"reward"`
}

func payoutDigest(policy Policy, start, end time.Time, payout Payout) ([32]byte, error) {
	encoded, err := json.Marshal(payoutDocument{
		PolicyRevision: policy.Revision,
		WindowStart:    start.Format(time.RFC3339Nano), WindowEnd: end.Format(time.RFC3339Nano),
		InviterUserID:        payout.InviterUserID.String(),
		EligibleInviteeCount: payout.EligibleInviteeCount,
		EligibleInviteeHours: payout.EligibleInviteeHours,
		SourceSeedingReward:  payout.SourceSeedingReward,
		CappedHourCount:      payout.CappedHourCount, Reward: payout.Reward,
	})
	if err != nil {
		return [32]byte{}, ErrInvariant
	}
	return sha256.Sum256(encoded), nil
}

type settlementDocument struct {
	PolicyRevision            string   `json:"policy_revision"`
	WindowStart               string   `json:"window_start"`
	WindowEnd                 string   `json:"window_end"`
	SourceCalculationCount    int64    `json:"source_calculation_count"`
	EligibleRelationshipCount int64    `json:"eligible_relationship_count"`
	RecipientCount            int      `json:"recipient_count"`
	TotalReward               int64    `json:"total_reward"`
	PayoutDigests             []string `json:"payout_digests"`
}

func settlementDigest(policy Policy, start, end time.Time, sourceCount, relationshipCount int64, payouts []Payout, total int64) ([32]byte, error) {
	digests := make([]string, 0, len(payouts))
	for _, payout := range payouts {
		digests = append(digests, hex.EncodeToString(payout.PayloadSHA256[:]))
	}
	encoded, err := json.Marshal(settlementDocument{
		PolicyRevision: policy.Revision,
		WindowStart:    start.Format(time.RFC3339Nano), WindowEnd: end.Format(time.RFC3339Nano),
		SourceCalculationCount: sourceCount, EligibleRelationshipCount: relationshipCount,
		RecipientCount: len(payouts), TotalReward: total, PayoutDigests: digests,
	})
	if err != nil {
		return [32]byte{}, ErrInvariant
	}
	return sha256.Sum256(encoded), nil
}

func (repository *PostgresRepository) MarkFailure(ctx context.Context, at time.Time, code string) error {
	at = canonicalTime(at)
	if at.IsZero() || code == "" || len(code) > 64 {
		return ErrInput
	}
	_, err := repository.pool.Exec(ctx, `
UPDATE economy.harem_reward_worker_state
SET last_started_at = COALESCE(last_started_at, $1), last_error_code = $2
WHERE singleton = true`, at, code)
	if err != nil {
		return fmt.Errorf("record harem reward worker failure: %w", err)
	}
	return nil
}

func canonicalTime(value time.Time) time.Time { return value.UTC().Truncate(time.Microsecond) }

var _ Repository = (*PostgresRepository)(nil)
