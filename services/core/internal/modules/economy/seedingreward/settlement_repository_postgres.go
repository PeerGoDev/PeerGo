package seedingreward

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/services/core/internal/modules/economy"
	"github.com/peergo/peergo/services/core/internal/modules/progression"
)

var (
	rewardTransactionNamespace = uuid.MustParse("82d14158-42b7-53ee-a21c-c3c740ab8154")
	rewardExperienceNamespace  = uuid.MustParse("3d9ebdd5-1048-5bf6-a8fc-a89068891053")
)

type PostgresSettlementRepository struct {
	pool        *pgxpool.Pool
	economy     *economy.PostgresRepository
	progression *progression.PostgresRepository
}

func NewPostgresSettlementRepository(pool *pgxpool.Pool) (*PostgresSettlementRepository, error) {
	if pool == nil {
		return nil, ErrInput
	}
	economyRepository, err := economy.NewPostgresRepository(pool)
	if err != nil {
		return nil, err
	}
	progressionRepository, err := progression.NewPostgresRepository(pool)
	if err != nil {
		return nil, err
	}
	return &PostgresSettlementRepository{pool: pool, economy: economyRepository, progression: progressionRepository}, nil
}

func (repository *PostgresSettlementRepository) Claim(ctx context.Context, now time.Time, batchSize int32, leaseDuration time.Duration) ([]PendingReward, error) {
	now = canonicalTime(now)
	if now.IsZero() || batchSize < 1 || batchSize > 100 || leaseDuration <= 0 || leaseDuration > 5*time.Minute {
		return nil, ErrInput
	}
	leaseToken := uuid.New()
	rows, err := repository.pool.Query(ctx, `
WITH candidate AS (
    SELECT work.window_start, work.user_id
    FROM economy.seeding_reward_work_items AS work
    WHERE (
        (work.status = 'pending' AND work.available_at <= $1)
        OR (work.status = 'processing' AND work.lease_until <= $1)
    )
      AND NOT EXISTS (
          SELECT 1
          FROM economy.seeding_reward_work_items AS earlier
          WHERE earlier.user_id = work.user_id
            AND earlier.window_start < work.window_start
            AND earlier.status <> 'completed'
      )
    ORDER BY work.window_start, work.user_id
    FOR UPDATE SKIP LOCKED
    LIMIT $2
)
UPDATE economy.seeding_reward_work_items AS work
SET status = 'processing',
    attempts = work.attempts + 1,
    lease_token = $3,
    lease_until = $1::timestamptz + ($4::bigint * interval '1 microsecond'),
    updated_at = $1
FROM candidate
WHERE work.window_start = candidate.window_start
  AND work.user_id = candidate.user_id
RETURNING work.window_start, work.user_id, work.attempts`, now, batchSize, leaseToken, leaseDuration.Microseconds())
	if err != nil {
		return nil, fmt.Errorf("claim seeding reward work: %w", err)
	}
	defer rows.Close()
	result := make([]PendingReward, 0, batchSize)
	for rows.Next() {
		var item PendingReward
		if err := rows.Scan(&item.WindowStart, &item.UserID, &item.Attempts); err != nil {
			return nil, fmt.Errorf("scan seeding reward work: %w", err)
		}
		item.WindowStart = canonicalTime(item.WindowStart)
		item.LeaseToken = leaseToken
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish claiming seeding reward work: %w", err)
	}
	return result, nil
}

func (repository *PostgresSettlementRepository) Settle(ctx context.Context, pending PendingReward, now time.Time) (SettlementResult, error) {
	now = canonicalTime(now)
	pending.WindowStart = canonicalTime(pending.WindowStart)
	if pending.WindowStart.IsZero() || pending.UserID == uuid.Nil || pending.LeaseToken == uuid.Nil || pending.Attempts < 1 || now.IsZero() {
		return SettlementResult{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return SettlementResult{}, fmt.Errorf("begin seeding reward settlement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var leaseUntil time.Time
	var status string
	err = tx.QueryRow(ctx, `
SELECT status, lease_until
FROM economy.seeding_reward_work_items
WHERE window_start = $1 AND user_id = $2 AND lease_token = $3
FOR UPDATE`, pending.WindowStart, pending.UserID, pending.LeaseToken).Scan(&status, &leaseUntil)
	if errors.Is(err, pgx.ErrNoRows) || status != "processing" || leaseUntil.Before(now) {
		return SettlementResult{}, ErrWorkLease
	}
	if err != nil {
		return SettlementResult{}, fmt.Errorf("lock seeding reward work: %w", err)
	}

	window, err := readRewardWindow(ctx, tx, pending.WindowStart)
	if err != nil {
		return SettlementResult{}, err
	}
	publishedPolicy, found, err := readPolicy(ctx, tx, `
WHERE effective_from <= $1
ORDER BY effective_from DESC
LIMIT 1`, window.Start)
	if err != nil {
		return SettlementResult{}, err
	}
	if !found {
		return SettlementResult{}, ErrPolicyNotFound
	}

	items, err := repository.snapshotItems(ctx, tx, window, pending.UserID, now)
	if err != nil {
		return SettlementResult{}, err
	}
	benefit, err := repository.snapshotBenefits(ctx, tx, window.Start, pending.UserID, now)
	if err != nil {
		return SettlementResult{}, err
	}
	calculation, err := Calculate(publishedPolicy.Policy, CalculationInput{
		UserID: pending.UserID, WindowStart: window.Start, WindowEnd: window.End,
		WindowEvidenceSHA256: window.EvidenceSHA256,
		SnapshotID:           window.SnapshotID, SnapshotSequence: window.SnapshotSequence,
		SnapshotObservedAt: window.SnapshotObservedAt, Benefits: benefit, Items: items,
	})
	if err != nil {
		return SettlementResult{}, err
	}

	sourceReference := fmt.Sprintf("seeding:%d:%s", window.Start.Unix(), pending.UserID.String())
	transactionID := uuid.Nil
	experienceEntryID := uuid.Nil
	if calculation.Reward > 0 {
		transactionID = uuid.NewSHA1(rewardTransactionNamespace, []byte(sourceReference))
		_, err = repository.economy.RecordInTransaction(ctx, tx, economy.RecordCommand{
			TransactionID: transactionID, TransactionType: economy.TransactionSeedingReward,
			IdempotencyKey:  "seeding_reward:" + pending.UserID.String() + ":" + fmt.Sprint(window.Start.Unix()),
			SourceReference: sourceReference, PolicyRevision: publishedPolicy.Policy.Revision,
			PayloadSHA256: calculation.CalculationSHA256, OccurredAt: window.End, RecordedAt: now,
			Postings: []economy.PostingInput{
				{AccountID: pending.UserID, Amount: calculation.Reward},
				{AccountID: economy.SeedingMintAccountID(), Amount: -calculation.Reward},
			},
		})
		if err != nil {
			return SettlementResult{}, fmt.Errorf("record seeding reward magic: %w", err)
		}
		experienceAmount, parseErr := progression.ParseAmount(calculation.ExperienceAmount)
		if parseErr != nil {
			return SettlementResult{}, ErrInvariant
		}
		if experienceAmount.Sign() > 0 {
			experienceEntryID = uuid.NewSHA1(rewardExperienceNamespace, []byte(sourceReference))
			_, err = repository.progression.RecordInTransaction(ctx, tx, progression.RecordCommand{
				EntryID:        experienceEntryID,
				IdempotencyKey: "seeding_reward:" + pending.UserID.String() + ":" + fmt.Sprint(window.Start.Unix()),
				UserID:         pending.UserID, EntryType: progression.EntryEarn, Amount: experienceAmount,
				SourceReference: sourceReference, SourceKind: progression.SourceSeedingReward,
				PolicyRevision:     publishedPolicy.Policy.Revision,
				LevelPolicyVersion: benefitLevelPolicy(benefit.Revision),
				PayloadSHA256:      calculation.CalculationSHA256, MagicTransactionID: transactionID,
				OccurredAt: window.End, RecordedAt: now,
			})
			if err != nil {
				return SettlementResult{}, fmt.Errorf("record seeding reward experience: %w", err)
			}
		}
	}

	_, err = tx.Exec(ctx, `
INSERT INTO economy.seeding_reward_calculations (
    window_start, user_id, policy_revision, calculation_sha256,
    eligible_torrent_count, value_score_micro, curve_reward_milli,
    linear_reward_milli, base_reward_milli, vip_bonus_milli,
    medal_bonus_milli, level_bonus_milli, uncapped_reward, reward,
    experience_amount, capped, magic_transaction_id, experience_entry_id,
    calculated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
    $11, $12, $13, $14, $15::numeric(38,20), $16,
    NULLIF($17::uuid, '00000000-0000-0000-0000-000000000000'::uuid),
    NULLIF($18::uuid, '00000000-0000-0000-0000-000000000000'::uuid), $19
)`, window.Start, pending.UserID, publishedPolicy.Policy.Revision, calculation.CalculationSHA256[:],
		calculation.EligibleTorrentCount, calculation.ValueScoreMicro, calculation.CurveRewardMilli,
		calculation.LinearRewardMilli, calculation.BaseRewardMilli, calculation.VIPBonusMilli,
		calculation.MedalBonusMilli, calculation.LevelBonusMilli, calculation.UncappedReward,
		calculation.Reward, calculation.ExperienceAmount, calculation.Capped,
		transactionID, experienceEntryID, now)
	if err != nil {
		return SettlementResult{}, classifySettlementDatabaseError("insert seeding reward calculation", err)
	}
	commandTag, err := tx.Exec(ctx, `
UPDATE economy.seeding_reward_work_items
SET status = 'completed', lease_token = NULL, lease_until = NULL,
    last_error_code = NULL, last_error_at = NULL,
    completed_at = $4, updated_at = $4
WHERE window_start = $1 AND user_id = $2
  AND status = 'processing' AND lease_token = $3`,
		window.Start, pending.UserID, pending.LeaseToken, now)
	if err != nil || commandTag.RowsAffected() != 1 {
		return SettlementResult{}, ErrWorkLease
	}
	if err := tx.Commit(ctx); err != nil {
		return SettlementResult{}, classifySettlementDatabaseError("commit seeding reward settlement", err)
	}
	return SettlementResult{
		WindowStart: window.Start, UserID: pending.UserID,
		PolicyRevision: publishedPolicy.Policy.Revision, Reward: calculation.Reward,
		ExperienceAmount:   calculation.ExperienceAmount,
		MagicTransactionID: transactionID, ExperienceEntryID: experienceEntryID,
		CalculationSHA256: calculation.CalculationSHA256,
	}, nil
}

func (repository *PostgresSettlementRepository) Release(ctx context.Context, pending PendingReward, availableAt time.Time, errorCode string, terminal bool) error {
	availableAt = canonicalTime(availableAt)
	errorCode = strings.TrimSpace(errorCode)
	if pending.WindowStart.IsZero() || pending.UserID == uuid.Nil || pending.LeaseToken == uuid.Nil ||
		availableAt.IsZero() || errorCode == "" || len(errorCode) > 64 {
		return ErrInput
	}
	status := "pending"
	if terminal {
		status = "dead"
	}
	commandTag, err := repository.pool.Exec(ctx, `
UPDATE economy.seeding_reward_work_items
SET status = $4, available_at = $5,
    lease_token = NULL, lease_until = NULL,
    last_error_code = $6, last_error_at = $5, updated_at = $5
WHERE window_start = $1 AND user_id = $2
  AND status = 'processing' AND lease_token = $3`,
		pending.WindowStart, pending.UserID, pending.LeaseToken, status, availableAt, errorCode)
	if err != nil {
		return fmt.Errorf("release seeding reward work: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return ErrWorkLease
	}
	return nil
}

type rewardWindow struct {
	Start, End         time.Time
	EvidenceSHA256     [32]byte
	SnapshotID         uuid.UUID
	SnapshotSequence   int64
	SnapshotObservedAt time.Time
}

func readRewardWindow(ctx context.Context, tx pgx.Tx, start time.Time) (rewardWindow, error) {
	var result rewardWindow
	var evidence []byte
	var status string
	err := tx.QueryRow(ctx, `
SELECT window_start, window_end, window_evidence_sha256,
       snapshot_id, snapshot_sequence, snapshot_observed_at, status
FROM economy.seeding_reward_evidence_windows
WHERE window_start = $1
FOR SHARE`, start).Scan(&result.Start, &result.End, &evidence, &result.SnapshotID,
		&result.SnapshotSequence, &result.SnapshotObservedAt, &status)
	if errors.Is(err, pgx.ErrNoRows) || status != "complete" || len(evidence) != sha256.Size {
		return rewardWindow{}, ErrInvariant
	}
	if err != nil {
		return rewardWindow{}, fmt.Errorf("read complete seeding reward window: %w", err)
	}
	copy(result.EvidenceSHA256[:], evidence)
	result.Start, result.End = canonicalTime(result.Start), canonicalTime(result.End)
	result.SnapshotObservedAt = canonicalTime(result.SnapshotObservedAt)
	return result, nil
}

type metadataDocument struct {
	TorrentID   int64  `json:"torrent_id"`
	SizeBytes   int64  `json:"size_bytes"`
	PublishedAt string `json:"published_at"`
	Official    bool   `json:"official"`
}

func (repository *PostgresSettlementRepository) snapshotItems(ctx context.Context, tx pgx.Tx, window rewardWindow, userID uuid.UUID, capturedAt time.Time) ([]ItemInput, error) {
	rows, err := tx.Query(ctx, `
SELECT evidence.torrent_id, torrent.total_size_bytes, torrent.published_at,
       evidence.active_seconds, evidence.raw_uploaded_bytes,
       evidence.snapshot_seeders, evidence.tracker_evidence_sha256
FROM economy.seeding_reward_evidence_items AS evidence
JOIN torrents.torrents AS torrent ON torrent.id = evidence.torrent_id
WHERE evidence.window_start = $1 AND evidence.user_id = $2
ORDER BY evidence.torrent_id`, window.Start, userID)
	if err != nil {
		return nil, fmt.Errorf("read seeding reward item enrichment: %w", err)
	}
	defer rows.Close()
	items := make([]ItemInput, 0)
	for rows.Next() {
		var item ItemInput
		var trackerDigest []byte
		if err := rows.Scan(&item.TorrentID, &item.SizeBytes, &item.PublishedAt,
			&item.ActiveSeconds, &item.RawUploadedBytes, &item.SnapshotSeeders, &trackerDigest); err != nil {
			return nil, fmt.Errorf("scan seeding reward item enrichment: %w", err)
		}
		if len(trackerDigest) != sha256.Size {
			return nil, ErrInvariant
		}
		copy(item.TrackerEvidenceSHA256[:], trackerDigest)
		item.PublishedAt = canonicalTime(item.PublishedAt)
		item.Official = false // PtYes also left SeedingInfo.IsOfficial false; PeerGo has no signed official-torrent model yet.
		encoded, err := json.Marshal(metadataDocument{
			TorrentID: item.TorrentID, SizeBytes: item.SizeBytes,
			PublishedAt: item.PublishedAt.Format(time.RFC3339Nano), Official: item.Official,
		})
		if err != nil {
			return nil, ErrInvariant
		}
		item.MetadataSHA256 = sha256.Sum256(encoded)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish seeding reward item enrichment: %w", err)
	}
	rows.Close()
	if len(items) == 0 {
		return nil, ErrInvariant
	}
	for _, item := range items {
		if err := upsertMetadataSnapshot(ctx, tx, window.Start, item, capturedAt); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func upsertMetadataSnapshot(ctx context.Context, tx pgx.Tx, windowStart time.Time, item ItemInput, capturedAt time.Time) error {
	_, err := tx.Exec(ctx, `
INSERT INTO economy.seeding_reward_metadata_snapshots (
    window_start, torrent_id, size_bytes, published_at,
    official, metadata_sha256, captured_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (window_start, torrent_id) DO NOTHING`, windowStart, item.TorrentID,
		item.SizeBytes, item.PublishedAt, item.Official, item.MetadataSHA256[:], capturedAt)
	if err != nil {
		return classifySettlementDatabaseError("insert seeding reward metadata snapshot", err)
	}
	var size int64
	var publishedAt time.Time
	var official bool
	var digest []byte
	if err := tx.QueryRow(ctx, `
SELECT size_bytes, published_at, official, metadata_sha256
FROM economy.seeding_reward_metadata_snapshots
WHERE window_start = $1 AND torrent_id = $2`, windowStart, item.TorrentID).
		Scan(&size, &publishedAt, &official, &digest); err != nil {
		return fmt.Errorf("verify seeding reward metadata snapshot: %w", err)
	}
	if size != item.SizeBytes || !canonicalTime(publishedAt).Equal(item.PublishedAt) ||
		official != item.Official || !bytes.Equal(digest, item.MetadataSHA256[:]) {
		return ErrEvidenceConflict
	}
	return nil
}

type benefitDocument struct {
	UserID            string `json:"user_id"`
	WindowStart       string `json:"window_start"`
	Entitlement       int64  `json:"entitlement_revision"`
	LevelPolicy       string `json:"level_policy_version"`
	Level             int16  `json:"level"`
	VIPActive         bool   `json:"vip_active"`
	MedalBonusBPS     int64  `json:"medal_bonus_bps"`
	LevelBonusBPS     int64  `json:"level_bonus_bps"`
	LevelTorrentBonus int32  `json:"level_seeding_count_bonus"`
}

type historicalBenefitSnapshot struct {
	EntitlementRevision int64
	LevelPolicyVersion  string
	Level               int16
	Input               BenefitInput
}

func (repository *PostgresSettlementRepository) snapshotBenefits(ctx context.Context, tx pgx.Tx, windowStart time.Time, userID uuid.UUID, capturedAt time.Time) (BenefitInput, error) {
	snapshot, err := repository.readHistoricalBenefit(ctx, tx, windowStart, userID)
	if err != nil {
		return BenefitInput{}, err
	}
	benefit := snapshot.Input
	_, err = tx.Exec(ctx, `
INSERT INTO economy.seeding_reward_benefit_snapshots (
    window_start, user_id, entitlement_revision, level_policy_version,
    level, vip_active, medal_bonus_bps, level_bonus_bps,
    level_seeding_count_bonus, benefit_revision, benefit_sha256, captured_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (window_start, user_id) DO NOTHING`, windowStart, userID,
		snapshot.EntitlementRevision, snapshot.LevelPolicyVersion, snapshot.Level,
		benefit.VIPActive, benefit.MedalBonusBPS, benefit.LevelBonusBPS,
		benefit.LevelLinearTorrentBonus, benefit.Revision, benefit.SnapshotSHA256[:], capturedAt)
	if err != nil {
		return BenefitInput{}, classifySettlementDatabaseError("insert seeding reward benefit snapshot", err)
	}
	var storedRevision string
	var storedDigest []byte
	if err := tx.QueryRow(ctx, `
SELECT benefit_revision, benefit_sha256
FROM economy.seeding_reward_benefit_snapshots
WHERE window_start = $1 AND user_id = $2`, windowStart, userID).Scan(&storedRevision, &storedDigest); err != nil {
		return BenefitInput{}, fmt.Errorf("verify seeding reward benefit snapshot: %w", err)
	}
	if storedRevision != benefit.Revision || !bytes.Equal(storedDigest, benefit.SnapshotSHA256[:]) {
		return BenefitInput{}, ErrEvidenceConflict
	}
	return benefit, nil
}

// readHistoricalBenefit resolves the immutable entitlement and level facts
// that applied at the reward hour without writing a snapshot. The normal
// settlement path persists the returned document; compensation preview uses
// the same resolver so an audit cannot accidentally apply today's VIP, medal
// or level benefits to an old hour.
func (repository *PostgresSettlementRepository) readHistoricalBenefit(ctx context.Context, tx pgx.Tx, windowStart time.Time, userID uuid.UUID) (historicalBenefitSnapshot, error) {
	var entitlementRevision int64
	var vipEnabled bool
	var vipUntil *time.Time
	var medalBonus int64
	err := tx.QueryRow(ctx, `
SELECT revision, vip_enabled, vip_until, medal_bonus_bps
FROM identity.user_reward_benefit_revisions
WHERE user_id = $1 AND effective_from <= $2
ORDER BY effective_from DESC, revision DESC
LIMIT 1`, userID, windowStart).Scan(&entitlementRevision, &vipEnabled, &vipUntil, &medalBonus)
	if errors.Is(err, pgx.ErrNoRows) {
		return historicalBenefitSnapshot{}, ErrBenefitNotFound
	}
	if err != nil {
		return historicalBenefitSnapshot{}, fmt.Errorf("read seeding reward entitlement: %w", err)
	}

	levelPolicy, level, err := rewardLevelAt(ctx, tx, userID, windowStart)
	if err != nil {
		return historicalBenefitSnapshot{}, err
	}
	var levelBonus int64
	var torrentBonus int32
	if err := tx.QueryRow(ctx, `
SELECT karma_bonus_bps, seeding_count_bonus
FROM progression.seeding_reward_level_benefits
WHERE policy_version = $1 AND level = $2`, levelPolicy, level).Scan(&levelBonus, &torrentBonus); errors.Is(err, pgx.ErrNoRows) {
		return historicalBenefitSnapshot{}, ErrBenefitNotFound
	} else if err != nil {
		return historicalBenefitSnapshot{}, fmt.Errorf("read seeding reward level benefit: %w", err)
	}
	vipActive := vipEnabled && (vipUntil == nil || vipUntil.After(windowStart))
	revision := fmt.Sprintf("benefit-v1.e%d.l%d.%s", entitlementRevision, level, levelPolicy)
	document := benefitDocument{
		UserID: userID.String(), WindowStart: windowStart.Format(time.RFC3339Nano),
		Entitlement: entitlementRevision, LevelPolicy: levelPolicy, Level: level,
		VIPActive: vipActive, MedalBonusBPS: medalBonus, LevelBonusBPS: levelBonus,
		LevelTorrentBonus: torrentBonus,
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return historicalBenefitSnapshot{}, ErrInvariant
	}
	digest := sha256.Sum256(encoded)
	return historicalBenefitSnapshot{
		EntitlementRevision: entitlementRevision,
		LevelPolicyVersion:  levelPolicy,
		Level:               level,
		Input: BenefitInput{
			Revision: revision, SnapshotSHA256: digest, VIPActive: vipActive,
			MedalBonusBPS: medalBonus, LevelBonusBPS: levelBonus,
			LevelLinearTorrentBonus: torrentBonus,
		},
	}, nil
}

func rewardLevelAt(ctx context.Context, tx pgx.Tx, userID uuid.UUID, at time.Time) (string, int16, error) {
	var policy string
	var level int16
	err := tx.QueryRow(ctx, `
SELECT level_policy_version, level_after
FROM progression.experience_entries
WHERE user_id = $1 AND occurred_at <= $2
ORDER BY occurred_at DESC, entry_sequence DESC
LIMIT 1`, userID, at).Scan(&policy, &level)
	if err == nil {
		return policy, level, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", 0, fmt.Errorf("read historical seeding reward level: %w", err)
	}
	// Native users without an experience entry start at the minimum level of
	// the current cutover policy; no mutable user_progress row is consulted.
	err = tx.QueryRow(ctx, `
SELECT policy_version, level
FROM progression.level_definitions
WHERE policy_version = 'rousi-v1'
ORDER BY minimum_experience, level
LIMIT 1`).Scan(&policy, &level)
	if err != nil {
		return "", 0, ErrBenefitNotFound
	}
	return policy, level, nil
}

func benefitLevelPolicy(revision string) string {
	parts := strings.Split(revision, ".")
	if len(parts) < 4 {
		return ""
	}
	return strings.Join(parts[3:], ".")
}

func classifySettlementDatabaseError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return fmt.Errorf("%w: %s: %v", ErrEvidenceConflict, operation, err)
		case "40001", "40P01":
			return fmt.Errorf("%w: %s: %v", ErrWorkLease, operation, err)
		case "P0001", "23503", "23514", "22003":
			return fmt.Errorf("%w: %s: %v", ErrInvariant, operation, err)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ SettlementRepository = (*PostgresSettlementRepository)(nil)
