package reseedreward

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/services/core/internal/modules/economy"
)

var transactionNamespace = uuid.MustParse("342481e6-9b31-4d95-afe0-095721c64eb1")

type PostgresRepository struct {
	pool   *pgxpool.Pool
	ledger *economy.PostgresRepository
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	ledger, err := economy.NewPostgresRepository(pool)
	if err != nil {
		return nil, err
	}
	return &PostgresRepository{pool: pool, ledger: ledger}, nil
}

type awardCandidate struct {
	RequestID      uuid.UUID
	UserID         uuid.UUID
	ActiveSeconds  int64
	PolicyRevision int64
	MagicAmount    int64
}

// SettleNext advances one verified hour after the complete swarm projection
// has observed its end. The round boundaries, progress, award receipts and
// balanced postings are read or written together under one transaction.
func (repository *PostgresRepository) SettleNext(ctx context.Context, now time.Time) (bool, error) {
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return false, fmt.Errorf("begin reseed reward settlement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('peergo-reseed-reward-settlement'))`).Scan(&locked); err != nil || !locked {
		return false, err
	}
	var windowStart, windowEnd time.Time
	err = tx.QueryRow(ctx, `
SELECT evidence.window_start, evidence.window_end
FROM economy.reseed_reward_pending_windows AS pending
JOIN economy.seeding_reward_evidence_windows AS evidence USING (window_start)
CROSS JOIN catalog.swarm_snapshot_projection_state AS swarm
WHERE evidence.status = 'complete'
  AND evidence.window_end <= swarm.observed_at AND evidence.window_end <= $1
ORDER BY pending.window_start LIMIT 1`, now).Scan(&windowStart, &windowEnd)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read pending reseed reward evidence: %w", err)
	}
	rows, err := tx.Query(ctx, addEvidenceAndFindAwards, windowStart, windowEnd)
	if err != nil {
		return false, fmt.Errorf("accumulate reseed reward evidence: %w", err)
	}
	candidates, err := pgx.CollectRows(rows, pgx.RowToStructByPos[awardCandidate])
	if err != nil {
		return false, fmt.Errorf("read reseed reward candidates: %w", err)
	}
	for _, candidate := range candidates {
		key := "reseed_reward:" + candidate.RequestID.String()
		transactionID := uuid.NewSHA1(transactionNamespace, []byte(key))
		version := fmt.Sprintf("reseed-%d", candidate.PolicyRevision)
		digest := sha256.Sum256(fmt.Appendf(nil, "%s\n%s\n%s\n%d\n%d\n%d", key, version, candidate.UserID, windowStart.Unix(), candidate.ActiveSeconds, candidate.MagicAmount))
		if _, err := repository.ledger.RecordInTransaction(ctx, tx, economy.RecordCommand{
			TransactionID: transactionID, TransactionType: economy.TransactionReseedReward,
			IdempotencyKey: key, SourceReference: key, PolicyRevision: version,
			PayloadSHA256: digest, OccurredAt: now, RecordedAt: now,
			Postings: []economy.PostingInput{
				{AccountID: candidate.UserID, Amount: candidate.MagicAmount},
				{AccountID: economy.ReseedMintAccountID(), Amount: -candidate.MagicAmount},
			},
		}); err != nil {
			return false, fmt.Errorf("issue reseed reward: %w", err)
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO economy.reseed_reward_awards (request_id, user_id, window_start, active_seconds, magic_transaction_id)
VALUES ($1, $2, $3, $4, $5)`, candidate.RequestID, candidate.UserID, windowStart, candidate.ActiveSeconds, transactionID); err != nil {
			return false, fmt.Errorf("record reseed reward receipt: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM economy.reseed_reward_pending_windows WHERE window_start = $1`, windowStart); err != nil {
		return false, fmt.Errorf("finish reseed reward evidence: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit reseed reward settlement: %w", err)
	}
	return true, nil
}

// Core evidence contains active seconds per hour but not the exact positions
// of those seconds. Subtract the maximum outside the recovered round for
// any boundary hour so the total includes only time we can prove occurred
// during that round. Unpacked and packed evidence are mutually exclusive.
const addEvidenceAndFindAwards = `
WITH eligible AS MATERIALIZED (
    SELECT round.request_id, round.torrent_id, round.policy_revision,
           3600 - floor(extract(epoch FROM least($2::timestamptz, round.ended_at)
                         - greatest($1::timestamptz, round.started_at)))::bigint AS outside_seconds
    FROM economy.reseed_reward_rounds AS round
    WHERE round.started_at < $2 AND (round.ended_at IS NULL OR round.ended_at > $1)
      AND NOT EXISTS (SELECT 1 FROM economy.reseed_reward_awards AS award WHERE award.request_id = round.request_id)
), contributed AS (
    SELECT eligible.request_id, evidence.user_id, evidence.active_seconds - eligible.outside_seconds AS seconds
    FROM economy.seeding_reward_evidence_items AS evidence
    JOIN eligible ON eligible.torrent_id = evidence.torrent_id
    WHERE evidence.window_start = $1 AND evidence.active_seconds > eligible.outside_seconds
    UNION ALL
    SELECT eligible.request_id, pack.user_id, (source.item->>1)::bigint - eligible.outside_seconds
    FROM economy.seeding_reward_evidence_packs AS pack
    CROSS JOIN LATERAL jsonb_array_elements(pack.items) AS source(item)
    JOIN eligible ON eligible.torrent_id = (source.item->>0)::bigint
    WHERE pack.window_start = $1 AND (source.item->>1)::bigint > eligible.outside_seconds
), progressed AS (
    INSERT INTO economy.reseed_reward_progress AS previous (request_id, user_id, active_seconds)
    SELECT request_id, user_id, seconds FROM contributed
    ON CONFLICT (request_id, user_id) DO UPDATE
    SET active_seconds = previous.active_seconds + EXCLUDED.active_seconds
    RETURNING request_id, user_id, active_seconds
)
SELECT DISTINCT ON (progressed.request_id)
    progressed.request_id, progressed.user_id, progressed.active_seconds, policy.revision, policy.magic_amount
FROM progressed
JOIN eligible ON eligible.request_id = progressed.request_id
JOIN economy.reseed_reward_policies AS policy ON policy.revision = eligible.policy_revision
JOIN identity.users AS member ON member.id = progressed.user_id AND member.status = 'active'
WHERE progressed.active_seconds >= policy.required_hours::bigint * 3600
ORDER BY progressed.request_id, progressed.active_seconds DESC, progressed.user_id`
