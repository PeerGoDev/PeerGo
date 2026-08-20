package economy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresOverviewRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresOverviewRepository(pool *pgxpool.Pool) (*PostgresOverviewRepository, error) {
	if pool == nil {
		return nil, ErrInput
	}
	return &PostgresOverviewRepository{pool: pool}, nil
}

// Overview reads the balance, progression projection and both immutable
// statements from one repeatable-read snapshot. A concurrent reward therefore
// cannot appear in the summary without its matching statement rows.
func (repository *PostgresOverviewRepository) Overview(ctx context.Context, userID uuid.UUID, limit int) (Overview, error) {
	if userID == uuid.Nil || limit < 1 || limit > MaximumOverviewLimit {
		return Overview{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Overview{}, fmt.Errorf("begin economy overview: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result := Overview{
		MagicEntries:      []MagicStatementEntry{},
		ExperienceEntries: []ExperienceStatementEntry{},
		Rules:             RuleOverview{Levels: []LevelRule{}},
	}
	var magicUpdated pgtype.Timestamptz
	err = tx.QueryRow(ctx, `
SELECT balance, updated_at
FROM economy.magic_accounts
WHERE user_id = $1`, userID).Scan(&result.MagicBalance, &magicUpdated)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Overview{}, fmt.Errorf("read magic account: %w", err)
	}
	if magicUpdated.Valid {
		value := magicUpdated.Time.UTC().Round(0)
		result.MagicUpdatedAt = &value
	}

	rows, err := tx.Query(ctx, `
SELECT
    transaction.ledger_sequence,
    transaction.transaction_type,
    statement.entry_type,
    statement.amount,
    statement.balance_after,
    statement.source_reference,
    COALESCE(transaction.policy_revision, ''),
    statement.occurred_at
FROM economy.magic_ledger_entries AS statement
JOIN economy.magic_transactions AS transaction
  ON transaction.id = statement.transaction_id
WHERE statement.user_id = $1
ORDER BY transaction.ledger_sequence DESC
LIMIT $2`, userID, limit)
	if err != nil {
		return Overview{}, fmt.Errorf("list magic statement: %w", err)
	}
	for rows.Next() {
		var entry MagicStatementEntry
		var transactionType string
		if err := rows.Scan(
			&entry.LedgerSequence, &transactionType, &entry.EntryType,
			&entry.Amount, &entry.BalanceAfter, &entry.SourceReference,
			&entry.PolicyRevision, &entry.OccurredAt,
		); err != nil {
			rows.Close()
			return Overview{}, fmt.Errorf("scan magic statement: %w", err)
		}
		entry.TransactionType = TransactionType(transactionType)
		entry.OccurredAt = entry.OccurredAt.UTC().Round(0)
		result.MagicEntries = append(result.MagicEntries, entry)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Overview{}, fmt.Errorf("finish magic statement: %w", err)
	}
	rows.Close()

	var progressUpdated pgtype.Timestamptz
	err = tx.QueryRow(ctx, `
SELECT
    progress.experience::text,
    progress.level,
    progress.policy_version,
    current_level.minimum_experience::text,
    progress.updated_at
FROM progression.user_progress AS progress
JOIN progression.level_definitions AS current_level
  ON current_level.policy_version = progress.policy_version
 AND current_level.level = progress.level
WHERE progress.user_id = $1`, userID).Scan(
		&result.Progress.Experience, &result.Progress.Level,
		&result.Progress.PolicyVersion, &result.Progress.CurrentMinimumExperience,
		&progressUpdated,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		// Native users without an experience event still start at the explicit
		// zero threshold of the current imported-compatible level policy.
		err = tx.QueryRow(ctx, `
SELECT '0', level, policy_version, minimum_experience::text
FROM progression.level_definitions
WHERE minimum_experience = 0
ORDER BY CASE WHEN policy_version = 'rousi-v1' THEN 0 ELSE 1 END, policy_version DESC
LIMIT 1`).Scan(
			&result.Progress.Experience, &result.Progress.Level,
			&result.Progress.PolicyVersion, &result.Progress.CurrentMinimumExperience,
		)
	}
	if err != nil {
		return Overview{}, fmt.Errorf("read progression projection: %w", err)
	}
	if progressUpdated.Valid {
		value := progressUpdated.Time.UTC().Round(0)
		result.Progress.UpdatedAt = &value
	}

	var nextLevel int16
	var nextMinimum string
	err = tx.QueryRow(ctx, `
SELECT level, minimum_experience::text
FROM progression.level_definitions AS definition
WHERE definition.policy_version = $1
  AND definition.minimum_experience > $2::numeric(38, 20)
ORDER BY definition.minimum_experience
LIMIT 1`, result.Progress.PolicyVersion, result.Progress.Experience).Scan(&nextLevel, &nextMinimum)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Overview{}, fmt.Errorf("read next level target: %w", err)
	}
	if err == nil {
		result.Progress.Next = &LevelTarget{Level: nextLevel, MinimumExperience: nextMinimum}
	}

	var rewardRules SeedingRewardRules
	err = tx.QueryRow(ctx, `
SELECT
    revision, formula_version, effective_from,
    curve_hourly_cap_milli, age_saturation_seconds, seeder_decay,
    curve_scale_milli, size_multiplier_bps, official_bonus_bps,
    upload_contribution_bonus_bps, per_torrent_hourly_milli,
    base_linear_torrent_limit, maximum_level_torrent_bonus,
    minimum_torrent_bytes, minimum_active_seconds,
    maximum_snapshot_age_seconds, vip_bonus_bps,
    maximum_medal_bonus_bps, maximum_level_bonus_bps,
    maximum_hourly_reward, experience_per_magic_bps
FROM economy.seeding_reward_policy_revisions
WHERE effective_from <= CURRENT_TIMESTAMP
ORDER BY effective_from DESC, revision DESC
LIMIT 1`).Scan(
		&rewardRules.Revision, &rewardRules.FormulaVersion, &rewardRules.EffectiveFrom,
		&rewardRules.CurveHourlyCapMilli, &rewardRules.AgeSaturationSeconds, &rewardRules.SeederDecay,
		&rewardRules.CurveScaleMilli, &rewardRules.SizeMultiplierBPS, &rewardRules.OfficialBonusBPS,
		&rewardRules.UploadContributionBonusBPS, &rewardRules.PerTorrentHourlyMilli,
		&rewardRules.BaseLinearTorrentLimit, &rewardRules.MaximumLevelTorrentBonus,
		&rewardRules.MinimumTorrentBytes, &rewardRules.MinimumActiveSeconds,
		&rewardRules.MaximumSnapshotAgeSeconds, &rewardRules.VIPBonusBPS,
		&rewardRules.MaximumMedalBonusBPS, &rewardRules.MaximumLevelBonusBPS,
		&rewardRules.MaximumHourlyReward, &rewardRules.ExperiencePerMagicBPS,
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Overview{}, fmt.Errorf("read effective seeding reward rules: %w", err)
	}
	if err == nil {
		rewardRules.EffectiveFrom = rewardRules.EffectiveFrom.UTC().Round(0)
		result.Rules.SeedingReward = &rewardRules
	}

	err = tx.QueryRow(ctx, `
SELECT
    revision, effective_from,
    experience_per_upload_gib_milli,
    experience_per_torrent_milli,
    experience_per_account_day_milli
FROM progression.contribution_experience_policy_revisions
WHERE effective_from <= CURRENT_TIMESTAMP
ORDER BY effective_from DESC, revision DESC
LIMIT 1`).Scan(
		&result.Rules.ContributionExperience.Revision,
		&result.Rules.ContributionExperience.EffectiveFrom,
		&result.Rules.ContributionExperience.ExperiencePerUploadGiBMilli,
		&result.Rules.ContributionExperience.ExperiencePerTorrentMilli,
		&result.Rules.ContributionExperience.ExperiencePerAccountDayMilli,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Overview{}, fmt.Errorf("read effective contribution experience rules: %w", ErrInvariant)
	}
	if err != nil {
		return Overview{}, fmt.Errorf("read effective contribution experience rules: %w", err)
	}
	result.Rules.ContributionExperience.EffectiveFrom = result.Rules.ContributionExperience.EffectiveFrom.UTC().Round(0)

	result.Rules.LevelPolicyVersion = result.Progress.PolicyVersion
	rows, err = tx.Query(ctx, `
SELECT
    definition.level,
    definition.minimum_experience::text,
    COALESCE(benefit.karma_bonus_bps, 0),
    COALESCE(benefit.seeding_count_bonus, 0)
FROM progression.level_definitions AS definition
LEFT JOIN progression.seeding_reward_level_benefits AS benefit
  ON benefit.policy_version = definition.policy_version
 AND benefit.level = definition.level
WHERE definition.policy_version = $1
ORDER BY definition.level`, result.Progress.PolicyVersion)
	if err != nil {
		return Overview{}, fmt.Errorf("list effective level rules: %w", err)
	}
	for rows.Next() {
		var rule LevelRule
		if err := rows.Scan(
			&rule.Level, &rule.MinimumExperience,
			&rule.KarmaBonusBPS, &rule.SeedingCountBonus,
		); err != nil {
			rows.Close()
			return Overview{}, fmt.Errorf("scan effective level rule: %w", err)
		}
		result.Rules.Levels = append(result.Rules.Levels, rule)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Overview{}, fmt.Errorf("finish effective level rules: %w", err)
	}
	rows.Close()
	if len(result.Rules.Levels) == 0 {
		return Overview{}, fmt.Errorf("read effective level rules: %w", ErrInvariant)
	}

	var latest LatestSeedingRewardCalculation
	err = tx.QueryRow(ctx, `
SELECT
    window_start, policy_revision, eligible_torrent_count,
    curve_reward_milli, linear_reward_milli, base_reward_milli,
    vip_bonus_milli, medal_bonus_milli, level_bonus_milli,
    uncapped_reward, reward, experience_amount::text, capped, calculated_at
FROM economy.seeding_reward_calculations
WHERE user_id = $1
ORDER BY window_start DESC
LIMIT 1`, userID).Scan(
		&latest.WindowStart, &latest.PolicyRevision, &latest.EligibleTorrentCount,
		&latest.CurveRewardMilli, &latest.LinearRewardMilli, &latest.BaseRewardMilli,
		&latest.VIPBonusMilli, &latest.MedalBonusMilli, &latest.LevelBonusMilli,
		&latest.UncappedReward, &latest.Reward, &latest.ExperienceAmount,
		&latest.Capped, &latest.CalculatedAt,
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Overview{}, fmt.Errorf("read latest seeding reward calculation: %w", err)
	}
	if err == nil {
		latest.WindowStart = latest.WindowStart.UTC().Round(0)
		latest.WindowEnd = latest.WindowStart.Add(time.Hour)
		latest.CalculatedAt = latest.CalculatedAt.UTC().Round(0)
		result.LatestSeedingReward = &latest
	}

	rows, err = tx.Query(ctx, `
SELECT
    entry_sequence, entry_type, amount::text, balance_after::text,
    source_kind, COALESCE(policy_revision, ''), level_after, occurred_at
FROM progression.experience_entries
WHERE user_id = $1
ORDER BY entry_sequence DESC
LIMIT $2`, userID, limit)
	if err != nil {
		return Overview{}, fmt.Errorf("list experience statement: %w", err)
	}
	for rows.Next() {
		var entry ExperienceStatementEntry
		if err := rows.Scan(
			&entry.EntrySequence, &entry.EntryType, &entry.Amount,
			&entry.BalanceAfter, &entry.SourceKind, &entry.PolicyRevision,
			&entry.LevelAfter, &entry.OccurredAt,
		); err != nil {
			rows.Close()
			return Overview{}, fmt.Errorf("scan experience statement: %w", err)
		}
		entry.OccurredAt = entry.OccurredAt.UTC().Round(0)
		result.ExperienceEntries = append(result.ExperienceEntries, entry)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Overview{}, fmt.Errorf("finish experience statement: %w", err)
	}
	rows.Close()

	if err := tx.Commit(ctx); err != nil {
		return Overview{}, fmt.Errorf("commit economy overview: %w", err)
	}
	return result, nil
}

var _ OverviewRepository = (*PostgresOverviewRepository)(nil)
