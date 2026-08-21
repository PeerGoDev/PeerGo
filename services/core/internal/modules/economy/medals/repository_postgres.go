package medals

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/economy"
)

type PostgresRepository struct {
	pool    *pgxpool.Pool
	economy *economy.PostgresRepository
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, errors.New("medal administration database is required")
	}
	ledger, err := economy.NewPostgresRepository(pool)
	if err != nil {
		return nil, err
	}
	return &PostgresRepository{pool: pool, economy: ledger}, nil
}

func (repository *PostgresRepository) Overview(ctx context.Context) (Overview, error) {
	settings, err := readSettings(ctx, repository.pool)
	if err != nil {
		return Overview{}, err
	}
	rows, err := repository.pool.Query(ctx, definitionSelect+`
ORDER BY definition.display_on_page DESC, definition.priority DESC, definition.id`)
	if err != nil {
		return Overview{}, fmt.Errorf("query medal definitions: %w", err)
	}
	defer rows.Close()
	items := make([]Definition, 0, 32)
	for rows.Next() {
		item, err := scanDefinition(rows)
		if err != nil {
			return Overview{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return Overview{}, fmt.Errorf("finish medal definitions: %w", err)
	}
	return Overview{Settings: settings, Items: items}, nil
}

func (repository *PostgresRepository) UpdateSettings(ctx context.Context, command UpdateSettingsCommand) (Settings, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Settings{}, fmt.Errorf("begin medal settings update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := readSettingsForUpdate(ctx, tx)
	if err != nil {
		return Settings{}, err
	}
	if before.Version != command.ExpectedVersion {
		return Settings{}, ErrSettingsConflict
	}
	row := tx.QueryRow(ctx, `
UPDATE economy.medal_settings
SET enabled = $1,
    maximum_wear_count = $2,
    maximum_upload_bonus_bps = $3,
    maximum_download_discount_bps = $4,
    maximum_magic_bonus_bps = $5,
    maximum_invite_bonus = $6,
    version = version + 1,
    updated_at = $7
WHERE singleton AND version = $8
RETURNING enabled, maximum_wear_count, maximum_upload_bonus_bps,
          maximum_download_discount_bps, maximum_magic_bonus_bps,
          maximum_invite_bonus, condition_check_day, condition_warning_days,
          version, updated_at`,
		command.Enabled, command.MaximumWearCount, command.MaximumUploadBonusBPS,
		command.MaximumDownloadDiscountBPS, command.MaximumMagicBonusBPS,
		command.MaximumInviteBonus, command.OccurredAt, command.ExpectedVersion)
	after, err := scanSettings(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Settings{}, ErrSettingsConflict
	}
	if err != nil {
		return Settings{}, fmt.Errorf("update medal settings: %w", err)
	}
	if err := insertSettingsRevision(ctx, tx, after, command.Reason, command.ActorID, command.Authorization.ID, command.OccurredAt); err != nil {
		return Settings{}, err
	}
	if before.MaximumMagicBonusBPS != after.MaximumMagicBonusBPS {
		if err := appendAllBenefitRevisions(ctx, tx, after.Version, command.OccurredAt); err != nil {
			return Settings{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Settings{}, fmt.Errorf("commit medal settings update: %w", err)
	}
	return after, nil
}

func (repository *PostgresRepository) Create(ctx context.Context, command CreateCommand) (Definition, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Definition{}, fmt.Errorf("begin medal definition create: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id int64
	err = tx.QueryRow(ctx, `
INSERT INTO economy.medal_definitions (
    name, description, image_large_path, image_small_path,
    acquisition_method, price, duration_days, display_on_page, priority,
    upload_bonus_bps, download_discount_bps, magic_bonus_bps, invite_bonus,
    is_workgroup, pool_eligible, periodic_reward_magic, reward_cycle,
    sale_begin_at, sale_end_at, inventory, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9,
    $10, $11, $12, $13, $14, $15, $16, $17,
    $18, $19, $20, $21, $21
)
RETURNING id`, command.Name, command.Description, command.ImageLargePath, command.ImageSmallPath,
		string(command.AcquisitionMethod), command.Price, command.DurationDays, command.DisplayOnPage, command.Priority,
		command.UploadBonusBPS, command.DownloadDiscountBPS, command.MagicBonusBPS, command.InviteBonus,
		command.AcquisitionMethod == AcquisitionWorkgroup, command.PoolEligible, command.PeriodicRewardMagic,
		command.RewardCycle, command.SaleBeginAt, command.SaleEndAt, command.Inventory, command.OccurredAt).Scan(&id)
	if err != nil {
		return Definition{}, fmt.Errorf("insert medal definition: %w", err)
	}
	if err := insertRevision(ctx, tx, id, "created", command.Reason, command.ActorID, command.Authorization.ID, command.OccurredAt); err != nil {
		return Definition{}, err
	}
	result, err := readDefinition(ctx, tx, id)
	if err != nil {
		return Definition{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Definition{}, fmt.Errorf("commit medal definition create: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) Update(ctx context.Context, command UpdateCommand) (Definition, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Definition{}, fmt.Errorf("begin medal definition update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var version, previousMagicBonus int64
	var previousWorkgroup bool
	err = tx.QueryRow(ctx, `
SELECT version, magic_bonus_bps, is_workgroup
FROM economy.medal_definitions
WHERE id = $1
FOR UPDATE`, command.ID).Scan(&version, &previousMagicBonus, &previousWorkgroup)
	if errors.Is(err, pgx.ErrNoRows) {
		return Definition{}, ErrNotFound
	}
	if err != nil {
		return Definition{}, fmt.Errorf("lock medal definition: %w", err)
	}
	if version != command.ExpectedVersion {
		return Definition{}, ErrVersionConflict
	}
	newWorkgroup := command.AcquisitionMethod == AcquisitionWorkgroup
	tag, err := tx.Exec(ctx, `
UPDATE economy.medal_definitions
SET name = $2, description = $3, image_large_path = $4, image_small_path = $5,
    acquisition_method = $6, price = $7, duration_days = $8,
    display_on_page = $9, priority = $10, upload_bonus_bps = $11,
    download_discount_bps = $12, magic_bonus_bps = $13, invite_bonus = $14,
    is_workgroup = $15, pool_eligible = $16, periodic_reward_magic = $17,
    reward_cycle = $18, sale_begin_at = $19, sale_end_at = $20,
    inventory = $21, version = version + 1, updated_at = $22
WHERE id = $1 AND version = $23`, command.ID, command.Name, command.Description,
		command.ImageLargePath, command.ImageSmallPath, string(command.AcquisitionMethod),
		command.Price, command.DurationDays, command.DisplayOnPage, command.Priority,
		command.UploadBonusBPS, command.DownloadDiscountBPS, command.MagicBonusBPS,
		command.InviteBonus, newWorkgroup, command.PoolEligible, command.PeriodicRewardMagic,
		command.RewardCycle, command.SaleBeginAt, command.SaleEndAt, command.Inventory,
		command.OccurredAt, command.ExpectedVersion)
	if err != nil {
		return Definition{}, fmt.Errorf("update medal definition: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return Definition{}, ErrVersionConflict
	}
	if err := insertRevision(ctx, tx, command.ID, "updated", command.Reason, command.ActorID, command.Authorization.ID, command.OccurredAt); err != nil {
		return Definition{}, err
	}
	if previousMagicBonus != command.MagicBonusBPS || previousWorkgroup != newWorkgroup {
		if err := appendAffectedBenefitRevisions(ctx, tx, command.ID, command.ExpectedVersion+1, command.OccurredAt); err != nil {
			return Definition{}, err
		}
	}
	result, err := readDefinition(ctx, tx, command.ID)
	if err != nil {
		return Definition{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Definition{}, fmt.Errorf("commit medal definition update: %w", err)
	}
	return result, nil
}

func readSettings(ctx context.Context, queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (Settings, error) {
	result, err := scanSettings(queryer.QueryRow(ctx, `
SELECT enabled, maximum_wear_count, maximum_upload_bonus_bps,
       maximum_download_discount_bps, maximum_magic_bonus_bps,
       maximum_invite_bonus, condition_check_day, condition_warning_days,
       version, updated_at
FROM economy.medal_settings
WHERE singleton`))
	if err != nil {
		return Settings{}, fmt.Errorf("read medal settings: %w", err)
	}
	return result, nil
}

func readSettingsForUpdate(ctx context.Context, tx pgx.Tx) (Settings, error) {
	result, err := scanSettings(tx.QueryRow(ctx, `
SELECT enabled, maximum_wear_count, maximum_upload_bonus_bps,
       maximum_download_discount_bps, maximum_magic_bonus_bps,
       maximum_invite_bonus, condition_check_day, condition_warning_days,
       version, updated_at
FROM economy.medal_settings
WHERE singleton
FOR UPDATE`))
	if err != nil {
		return Settings{}, fmt.Errorf("lock medal settings: %w", err)
	}
	return result, nil
}

func scanSettings(row rowScanner) (Settings, error) {
	var result Settings
	err := row.Scan(
		&result.Enabled, &result.MaximumWearCount, &result.MaximumUploadBonusBPS,
		&result.MaximumDownloadDiscountBPS, &result.MaximumMagicBonusBPS,
		&result.MaximumInviteBonus, &result.ConditionCheckDay, &result.ConditionWarningDays,
		&result.Version, &result.UpdatedAt,
	)
	return result, err
}

const definitionSelect = `
SELECT
    definition.id, definition.name, definition.description,
    definition.image_large_path, definition.image_small_path,
    definition.acquisition_method, definition.price, definition.duration_days,
    definition.display_on_page, definition.priority, definition.upload_bonus_bps,
    definition.download_discount_bps, definition.magic_bonus_bps,
    definition.invite_bonus, definition.is_workgroup, definition.pool_eligible,
    definition.periodic_reward_magic, definition.reward_cycle,
    definition.sale_begin_at, definition.sale_end_at, definition.inventory,
    jsonb_array_length(definition.conditions),
    jsonb_array_length(definition.privileges), definition.version,
    count(holding.id)::bigint,
    count(holding.id) FILTER (
        WHERE holding.expires_at IS NULL OR holding.expires_at > clock_timestamp()
    )::bigint,
    count(holding.id) FILTER (
        WHERE holding.state = 'wearing'
          AND (holding.expires_at IS NULL OR holding.expires_at > clock_timestamp())
    )::bigint,
    definition.created_at, definition.updated_at
FROM economy.medal_definitions AS definition
LEFT JOIN economy.user_medals AS holding ON holding.medal_id = definition.id
GROUP BY definition.id`

func readDefinition(ctx context.Context, queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id int64) (Definition, error) {
	result, err := scanDefinition(queryer.QueryRow(ctx, definitionSelect+`
HAVING definition.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Definition{}, ErrNotFound
	}
	return result, err
}

type rowScanner interface {
	Scan(...any) error
}

func scanDefinition(row rowScanner) (Definition, error) {
	var result Definition
	var description, largeImage, smallImage, rewardCycle pgtype.Text
	var saleBegin, saleEnd pgtype.Timestamptz
	var inventory pgtype.Int8
	var acquisition string
	if err := row.Scan(
		&result.ID, &result.Name, &description, &largeImage, &smallImage,
		&acquisition, &result.Price, &result.DurationDays, &result.DisplayOnPage,
		&result.Priority, &result.UploadBonusBPS, &result.DownloadDiscountBPS,
		&result.MagicBonusBPS, &result.InviteBonus, &result.IsWorkgroup,
		&result.PoolEligible, &result.PeriodicRewardMagic, &rewardCycle,
		&saleBegin, &saleEnd, &inventory, &result.ConditionsCount,
		&result.PrivilegesCount, &result.Version, &result.HolderCount,
		&result.ActiveHolderCount, &result.WearingCount, &result.CreatedAt,
		&result.UpdatedAt,
	); err != nil {
		return Definition{}, fmt.Errorf("scan medal definition: %w", err)
	}
	result.AcquisitionMethod = AcquisitionMethod(acquisition)
	result.Description = textPointer(description)
	result.ImageLargePath = textPointer(largeImage)
	result.ImageSmallPath = textPointer(smallImage)
	result.RewardCycle = textPointer(rewardCycle)
	result.SaleBeginAt = timePointer(saleBegin)
	result.SaleEndAt = timePointer(saleEnd)
	if inventory.Valid {
		value := inventory.Int64
		result.Inventory = &value
	}
	return result, nil
}

func insertRevision(ctx context.Context, tx pgx.Tx, id int64, transition, reason string, actorID, decisionID uuid.UUID, occurredAt time.Time) error {
	_, err := tx.Exec(ctx, `
INSERT INTO economy.medal_definition_revisions (
    medal_id, version, transition, snapshot_json, reason,
    changed_by, authorization_decision_id, created_at
)
SELECT id, version, $2, to_jsonb(definition) - 'created_at' - 'updated_at',
       $3, $4, $5, $6
FROM economy.medal_definitions AS definition
WHERE id = $1`, id, transition, reason, actorID, decisionID, occurredAt)
	if err != nil {
		return fmt.Errorf("append medal definition revision: %w", err)
	}
	return nil
}

func insertSettingsRevision(ctx context.Context, tx pgx.Tx, settings Settings, reason string, actorID, decisionID uuid.UUID, occurredAt time.Time) error {
	_, err := tx.Exec(ctx, `
INSERT INTO economy.medal_settings_revisions (
    version, enabled, maximum_wear_count, maximum_upload_bonus_bps,
    maximum_download_discount_bps, maximum_magic_bonus_bps,
    maximum_invite_bonus, condition_check_day, condition_warning_days,
    reason, changed_by, authorization_decision_id, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		settings.Version, settings.Enabled, settings.MaximumWearCount,
		settings.MaximumUploadBonusBPS, settings.MaximumDownloadDiscountBPS,
		settings.MaximumMagicBonusBPS, settings.MaximumInviteBonus,
		settings.ConditionCheckDay, settings.ConditionWarningDays, reason,
		actorID, decisionID, occurredAt)
	if err != nil {
		return fmt.Errorf("append medal settings revision: %w", err)
	}
	return nil
}

// Changing the site-wide magic cap affects every active medal holder, not a
// single medal definition. Append a new benefit input only where the computed
// value actually changes; previous seeding settlements stay immutable.
func appendAllBenefitRevisions(ctx context.Context, tx pgx.Tx, version int64, occurredAt time.Time) error {
	_, err := tx.Exec(ctx, `
WITH affected AS MATERIALIZED (
    SELECT DISTINCT user_id
    FROM economy.user_medals
), locked AS MATERIALIZED (
    SELECT user_id,
           pg_advisory_xact_lock(hashtextextended('peergo-user-reward-benefit:' || user_id::text, 0))
    FROM affected
    ORDER BY user_id
), latest AS MATERIALIZED (
    SELECT locked.user_id, benefit.revision, benefit.vip_enabled,
           benefit.vip_until, benefit.medal_bonus_bps
    FROM locked
    JOIN LATERAL (
        SELECT revision, vip_enabled, vip_until, medal_bonus_bps
        FROM identity.user_reward_benefit_revisions
        WHERE user_id = locked.user_id
        ORDER BY revision DESC
        LIMIT 1
    ) AS benefit ON true
), recalculated AS MATERIALIZED (
    SELECT latest.*,
           LEAST(
               settings.maximum_magic_bonus_bps,
               COALESCE(sum(definition.magic_bonus_bps) FILTER (
                   WHERE holding.id IS NOT NULL
                     AND (holding.expires_at IS NULL OR holding.expires_at > $2)
                     AND (definition.is_workgroup OR holding.state = 'wearing')
               ), 0)
           )::bigint AS next_bonus
    FROM latest
    CROSS JOIN economy.medal_settings AS settings
    LEFT JOIN economy.user_medals AS holding ON holding.user_id = latest.user_id
    LEFT JOIN economy.medal_definitions AS definition ON definition.id = holding.medal_id
    WHERE settings.singleton
    GROUP BY latest.user_id, latest.revision, latest.vip_enabled,
             latest.vip_until, latest.medal_bonus_bps,
             settings.maximum_magic_bonus_bps
)
INSERT INTO identity.user_reward_benefit_revisions (
    user_id, revision, effective_from, vip_enabled, vip_until,
    medal_bonus_bps, source_kind, source_reference, created_at
)
SELECT user_id, revision + 1, $2, vip_enabled, vip_until,
       next_bonus, 'runtime', 'medal-settings:v' || $1::text, $2
FROM recalculated
WHERE next_bonus <> medal_bonus_bps
ORDER BY user_id`, version, occurredAt)
	if err != nil {
		return fmt.Errorf("append medal settings benefit revisions: %w", err)
	}
	return nil
}

// A magic-bonus or workgroup semantic change must not rewrite historical
// reward inputs. We lock every affected user's entitlement timeline in UUID
// order, recompute the current medal total, and append only changed values.
func appendAffectedBenefitRevisions(ctx context.Context, tx pgx.Tx, medalID, version int64, occurredAt time.Time) error {
	// Keep the audit reference a Go string. PostgreSQL otherwise infers a text
	// parameter through concatenation, which pgx cannot encode from an int64.
	sourceReference := fmt.Sprintf("medal-definition:%d:v%d", medalID, version)
	_, err := tx.Exec(ctx, `
WITH affected AS MATERIALIZED (
    SELECT DISTINCT user_id
    FROM economy.user_medals
    WHERE medal_id = $1
), locked AS MATERIALIZED (
    SELECT user_id,
           pg_advisory_xact_lock(hashtextextended('peergo-user-reward-benefit:' || user_id::text, 0))
    FROM affected
    ORDER BY user_id
), latest AS MATERIALIZED (
    SELECT locked.user_id, benefit.revision, benefit.vip_enabled,
           benefit.vip_until, benefit.medal_bonus_bps
    FROM locked
    JOIN LATERAL (
        SELECT revision, vip_enabled, vip_until, medal_bonus_bps
        FROM identity.user_reward_benefit_revisions
        WHERE user_id = locked.user_id
        ORDER BY revision DESC
        LIMIT 1
    ) AS benefit ON true
), recalculated AS MATERIALIZED (
    SELECT latest.*,
           LEAST(
               settings.maximum_magic_bonus_bps,
               COALESCE(sum(definition.magic_bonus_bps) FILTER (
                   WHERE holding.id IS NOT NULL
                     AND (holding.expires_at IS NULL OR holding.expires_at > $3)
                     AND (definition.is_workgroup OR holding.state = 'wearing')
               ), 0)
           )::bigint AS next_bonus
    FROM latest
    CROSS JOIN economy.medal_settings AS settings
    LEFT JOIN economy.user_medals AS holding ON holding.user_id = latest.user_id
    LEFT JOIN economy.medal_definitions AS definition ON definition.id = holding.medal_id
    WHERE settings.singleton
    GROUP BY latest.user_id, latest.revision, latest.vip_enabled,
             latest.vip_until, latest.medal_bonus_bps,
             settings.maximum_magic_bonus_bps
)
INSERT INTO identity.user_reward_benefit_revisions (
    user_id, revision, effective_from, vip_enabled, vip_until,
    medal_bonus_bps, source_kind, source_reference, created_at
)
SELECT user_id, revision + 1, $3, vip_enabled, vip_until,
       next_bonus, 'runtime', $2, $3
FROM recalculated
WHERE next_bonus <> medal_bonus_bps
ORDER BY user_id`, medalID, sourceReference, occurredAt)
	if err != nil {
		return fmt.Errorf("append affected medal benefit revisions: %w", err)
	}
	return nil
}

func textPointer(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func timePointer(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}

var _ Repository = (*PostgresRepository)(nil)
