// Package legacymedals imports the finite Rousi/PtYes medal opening after the
// stable user mapping and non-secret user state have been established.
package legacymedals

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/contracts/go/schemaversionv1"
)

type Config struct {
	RunID      uuid.UUID
	OccurredAt time.Time
}

type Progress struct {
	Phase     string
	Processed int64
	Expected  int64
}

type Result struct {
	RunID                  uuid.UUID
	Definitions            int64
	UserMedals             int64
	Wearing                int64
	Expired                int64
	BenefitUsers           int64
	PositiveBenefitUsers   int64
	MaximumMagicBonusBPS   int64
	WorkgroupMemberships   int64
	ReseedMemberships      int64
	ReviewMemberships      int64
	RetentionMemberships   int64
	ImportedDefinitionRows int64
	ImportedUserMedalRows  int64
	ImportedBenefitRows    int64
	ImportedWorkgroupRows  int64
}

type Importer struct {
	source   *pgxpool.Pool
	core     *pgxpool.Pool
	config   Config
	progress func(Progress)
}

func NewImporter(source, core *pgxpool.Pool, config Config, progress func(Progress)) (*Importer, error) {
	config.OccurredAt = config.OccurredAt.UTC().Truncate(time.Microsecond)
	if source == nil || core == nil || config.RunID == uuid.Nil || config.OccurredAt.IsZero() {
		return nil, errors.New("legacy medal importer configuration is invalid")
	}
	if progress == nil {
		progress = func(Progress) {}
	}
	return &Importer{source: source, core: core, config: config, progress: progress}, nil
}

// Run is intentionally one Core transaction. A failure can never leave a
// user's holdings imported without the matching immutable benefit revision.
func (importer *Importer) Run(ctx context.Context) (Result, error) {
	return importer.run(ctx, true)
}

// Verify reconstructs the source stages and compares every receipt/domain row
// without writing the target. Cutover acceptance uses this path.
func (importer *Importer) Verify(ctx context.Context) (Result, error) {
	return importer.run(ctx, false)
}

func (importer *Importer) run(ctx context.Context, mutate bool) (Result, error) {
	if err := requireCoreMigration(ctx, importer.core); err != nil {
		return Result{}, err
	}
	settings, err := importer.readSettings(ctx)
	if err != nil {
		return Result{}, err
	}
	medals, err := importer.readMedals(ctx)
	if err != nil {
		return Result{}, err
	}
	holdings, err := importer.readHoldings(ctx)
	if err != nil {
		return Result{}, err
	}
	userIDs, err := importer.readUserIDs(ctx)
	if err != nil {
		return Result{}, err
	}
	benefits, err := calculateBenefits(userIDs, medals, holdings, settings, importer.config.OccurredAt)
	if err != nil {
		return Result{}, err
	}

	tx, err := importer.core.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Result{}, fmt.Errorf("begin legacy medal import: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := importer.requirePrerequisites(ctx, tx, int64(len(userIDs)), mutate); err != nil {
		return Result{}, err
	}
	targetUserIDs, err := readTargetUserIDs(ctx, tx, int64(len(userIDs)))
	if err != nil {
		return Result{}, err
	}
	workgroupMemberships, err := calculateWorkgroupMemberships(
		medals,
		holdings,
		targetUserIDs,
		importer.config.OccurredAt,
	)
	if err != nil {
		return Result{}, err
	}
	var workgroupOrigins int64
	for _, membership := range workgroupMemberships {
		workgroupOrigins += int64(len(membership.LegacyUserMedalIDs))
	}
	if err := createStages(ctx, tx); err != nil {
		return Result{}, err
	}
	if err := importer.copyMedals(ctx, tx, medals); err != nil {
		return Result{}, err
	}
	if err := importer.copyHoldings(ctx, tx, holdings); err != nil {
		return Result{}, err
	}
	if err := importer.copyBenefits(ctx, tx, benefits); err != nil {
		return Result{}, err
	}
	if err := importer.copyWorkgroupMemberships(ctx, tx, workgroupMemberships); err != nil {
		return Result{}, err
	}
	if err := importer.validateStages(
		ctx,
		tx,
		int64(len(medals)),
		int64(len(holdings)),
		int64(len(userIDs)),
		int64(len(workgroupMemberships)),
		workgroupOrigins,
	); err != nil {
		return Result{}, err
	}

	settingsImported, err := importer.insertAndVerifySettings(ctx, tx, settings, mutate)
	if err != nil {
		return Result{}, err
	}
	definitionRows, holdingRows, benefitRows, workgroupRows := int64(0), int64(0), int64(0), int64(0)
	if mutate {
		definitionRows, err = importer.insertDefinitions(ctx, tx)
		if err != nil {
			return Result{}, err
		}
		holdingRows, err = importer.insertHoldings(ctx, tx)
		if err != nil {
			return Result{}, err
		}
		workgroupRows, err = importer.insertWorkgroupMemberships(ctx, tx)
		if err != nil {
			return Result{}, err
		}
		benefitRows, err = importer.insertBenefits(ctx, tx)
		if err != nil {
			return Result{}, err
		}
	}
	if err := importer.verifyAll(
		ctx,
		tx,
		int64(len(medals)),
		int64(len(holdings)),
		int64(len(userIDs)),
		int64(len(workgroupMemberships)),
	); err != nil {
		return Result{}, err
	}
	if mutate {
		if err := advanceIdentitySequences(ctx, tx); err != nil {
			return Result{}, err
		}
	}

	result, err := readResult(ctx, tx, importer.config, definitionRows, holdingRows, benefitRows, workgroupRows)
	if err != nil {
		return Result{}, err
	}
	if !mutate {
		if err := tx.Rollback(ctx); err != nil {
			return Result{}, fmt.Errorf("close legacy medal verification transaction: %w", err)
		}
		return result, nil
	}
	_ = settingsImported
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit legacy medal import: %w", err)
	}
	return result, nil
}

func requireCoreMigration(ctx context.Context, core *pgxpool.Pool) error {
	var actual int64
	if err := core.QueryRow(ctx, `
SELECT COALESCE(MAX(version_id), 0)
FROM goose_db_version
WHERE is_applied = true`).Scan(&actual); err != nil {
		return fmt.Errorf("read Core migration version: %w", err)
	}
	if actual != schemaversionv1.Core {
		return fmt.Errorf("Core migration version is %d, want %d", actual, schemaversionv1.Core)
	}
	return nil
}

func (importer *Importer) readSettings(ctx context.Context) (sourceSettings, error) {
	rows, err := importer.source.Query(ctx, `
SELECT key, value
FROM site_settings
WHERE key = ANY($1::text[])
ORDER BY key`, []string{
		"medal.enabled", "medal.max_wear_count", "medal.max_upload_bonus",
		"medal.max_download_discount", "medal.max_karma_bonus", "medal.max_invite_bonus",
		"medal.condition_check_day", "medal.condition_warning_days",
	})
	if err != nil {
		return sourceSettings{}, fmt.Errorf("query PtYes medal settings: %w", err)
	}
	defer rows.Close()
	values := make(map[string]string, 8)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return sourceSettings{}, fmt.Errorf("scan PtYes medal setting: %w", err)
		}
		values[key] = strings.TrimSpace(value)
	}
	if err := rows.Err(); err != nil {
		return sourceSettings{}, fmt.Errorf("finish PtYes medal settings: %w", err)
	}
	if len(values) != 8 {
		return sourceSettings{}, fmt.Errorf("PtYes medal settings are incomplete: got %d, want 8", len(values))
	}
	parseInt := func(key string) (int64, error) {
		value, err := strconv.ParseInt(values[key], 10, 64)
		if err != nil || value < 0 {
			return 0, fmt.Errorf("PtYes setting %s is invalid", key)
		}
		return value, nil
	}
	parseBPS := func(key string) (int64, error) {
		value, err := decimalToBPS(values[key])
		if err != nil {
			return 0, fmt.Errorf("PtYes setting %s: %w", key, err)
		}
		return value, nil
	}
	enabled, err := strconv.ParseBool(values["medal.enabled"])
	if err != nil {
		return sourceSettings{}, errors.New("PtYes setting medal.enabled is invalid")
	}
	result := sourceSettings{Enabled: enabled}
	if result.MaximumWearCount, err = parseInt("medal.max_wear_count"); err != nil {
		return sourceSettings{}, err
	}
	if result.MaximumUploadBonusBPS, err = parseBPS("medal.max_upload_bonus"); err != nil {
		return sourceSettings{}, err
	}
	if result.MaximumDownloadDiscountBPS, err = parseBPS("medal.max_download_discount"); err != nil {
		return sourceSettings{}, err
	}
	if result.MaximumMagicBonusBPS, err = parseBPS("medal.max_karma_bonus"); err != nil {
		return sourceSettings{}, err
	}
	if result.MaximumInviteBonus, err = parseInt("medal.max_invite_bonus"); err != nil {
		return sourceSettings{}, err
	}
	if result.ConditionCheckDay, err = parseInt("medal.condition_check_day"); err != nil {
		return sourceSettings{}, err
	}
	if result.ConditionWarningDays, err = parseInt("medal.condition_warning_days"); err != nil {
		return sourceSettings{}, err
	}
	if result.ConditionCheckDay < 1 || result.ConditionCheckDay > 28 {
		return sourceSettings{}, errors.New("PtYes medal condition check day must be between 1 and 28")
	}
	return result, nil
}

func (importer *Importer) readMedals(ctx context.Context) ([]sourceMedal, error) {
	rows, err := importer.source.Query(ctx, `
SELECT
    id, name, NULLIF(btrim(description), ''), NULLIF(btrim(image_large), ''),
    NULLIF(btrim(image_small), ''), get_type, COALESCE(price, 0)::text,
    COALESCE(duration, 0), COALESCE(display_on_page, true), COALESCE(priority, 0),
    COALESCE(upload_bonus, 0)::text, COALESCE(download_discount, 0)::text,
    COALESCE(karma_bonus, 0)::text, COALESCE(invite_bonus, 0),
    COALESCE(is_workgroup, false), conditions, privileges,
    COALESCE(pool_eligible, false), COALESCE(reward_karma, 0)::text,
    COALESCE(reward_credits, 0)::text, NULLIF(btrim(reward_cycle), ''),
    sale_begin_time, sale_end_time, inventory, created_at, updated_at
FROM medals
ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query PtYes medals: %w", err)
	}
	defer rows.Close()
	result := make([]sourceMedal, 0, 32)
	for rows.Next() {
		var row sourceMedal
		if err := rows.Scan(
			&row.LegacyID, &row.Name, &row.Description, &row.ImageLarge, &row.ImageSmall,
			&row.GetType, &row.PriceText, &row.DurationDays, &row.DisplayOnPage, &row.Priority,
			&row.UploadBonusText, &row.DownloadBonusText, &row.MagicBonusText, &row.InviteBonus,
			&row.IsWorkgroup, &row.ConditionsRaw, &row.PrivilegesRaw, &row.PoolEligible,
			&row.RewardMagicText, &row.RewardCreditsText, &row.RewardCycle,
			&row.SaleBeginAt, &row.SaleEndAt, &row.Inventory, &row.CreatedAt, &row.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan PtYes medal row %d: %w", len(result)+1, err)
		}
		if err := row.normalize(len(result) + 1); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish PtYes medals: %w", err)
	}
	if len(result) == 0 {
		return nil, errors.New("PtYes source contains no medal definitions")
	}
	return result, nil
}

func (importer *Importer) readHoldings(ctx context.Context) ([]sourceHolding, error) {
	rows, err := importer.source.Query(ctx, `
SELECT id, user_id, medal_id, COALESCE(status, 1), COALESCE(priority, 0),
       expire_at, NULLIF(granted_by, 0), NULLIF(btrim(note), ''),
       created_at, updated_at, last_reward_at
FROM user_medals
ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query PtYes user medals: %w", err)
	}
	defer rows.Close()
	result := make([]sourceHolding, 0, 50000)
	for rows.Next() {
		var row sourceHolding
		if err := rows.Scan(
			&row.LegacyID, &row.LegacyUserID, &row.LegacyMedalID, &row.Status,
			&row.Priority, &row.ExpiresAt, &row.LegacyGrantedBy, &row.Note,
			&row.CreatedAt, &row.UpdatedAt, &row.LastRewardAt,
		); err != nil {
			return nil, fmt.Errorf("scan PtYes user medal row %d: %w", len(result)+1, err)
		}
		if err := row.normalize(len(result) + 1); err != nil {
			return nil, err
		}
		result = append(result, row)
		if len(result)%1000 == 0 {
			importer.progress(Progress{Phase: "read-user-medals", Processed: int64(len(result)), Expected: 0})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish PtYes user medals: %w", err)
	}
	return result, nil
}

func (importer *Importer) readUserIDs(ctx context.Context) ([]int64, error) {
	rows, err := importer.source.Query(ctx, `SELECT id FROM users ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query PtYes users for medal benefits: %w", err)
	}
	defer rows.Close()
	result := make([]int64, 0, 16000)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan PtYes user for medal benefits: %w", err)
		}
		if id <= 0 {
			return nil, errors.New("PtYes user has a non-positive ID")
		}
		result = append(result, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish PtYes users for medal benefits: %w", err)
	}
	if len(result) == 0 {
		return nil, errors.New("PtYes source contains no users")
	}
	return result, nil
}

func calculateBenefits(userIDs []int64, medals []sourceMedal, holdings []sourceHolding, settings sourceSettings, occurredAt time.Time) ([]sourceBenefit, error) {
	definitions := make(map[int64]sourceMedal, len(medals))
	for _, medal := range medals {
		if _, exists := definitions[medal.LegacyID]; exists {
			return nil, fmt.Errorf("duplicate PtYes medal ID %d", medal.LegacyID)
		}
		definitions[medal.LegacyID] = medal
	}
	byUser := make(map[int64]*sourceBenefit, len(userIDs))
	for _, userID := range userIDs {
		if _, exists := byUser[userID]; exists {
			return nil, fmt.Errorf("duplicate PtYes user ID %d", userID)
		}
		byUser[userID] = &sourceBenefit{LegacyUserID: userID}
	}
	for _, holding := range holdings {
		benefit, exists := byUser[holding.LegacyUserID]
		if !exists {
			return nil, fmt.Errorf("PtYes user medal %d references missing user %d", holding.LegacyID, holding.LegacyUserID)
		}
		medal, exists := definitions[holding.LegacyMedalID]
		if !exists {
			return nil, fmt.Errorf("PtYes user medal %d references missing medal %d", holding.LegacyID, holding.LegacyMedalID)
		}
		active := !holding.ExpiresAt.Valid || holding.ExpiresAt.Time.After(occurredAt)
		if active && (medal.IsWorkgroup || holding.Status == 2) {
			benefit.ActiveContributingMedals++
			if benefit.UncappedMagicBonusBPS > int64(^uint64(0)>>1)-medal.MagicBonusBPS {
				return nil, fmt.Errorf("PtYes user %d medal bonus overflows int64", holding.LegacyUserID)
			}
			benefit.UncappedMagicBonusBPS += medal.MagicBonusBPS
		}
	}
	result := make([]sourceBenefit, 0, len(userIDs))
	for _, userID := range userIDs {
		benefit := *byUser[userID]
		benefit.MagicBonusBPS = benefit.UncappedMagicBonusBPS
		if benefit.MagicBonusBPS > settings.MaximumMagicBonusBPS {
			benefit.MagicBonusBPS = settings.MaximumMagicBonusBPS
		}
		result = append(result, benefit)
	}
	return result, nil
}

func readTargetUserIDs(ctx context.Context, tx pgx.Tx, expected int64) (map[int64]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
SELECT legacy_user_id, user_id
FROM migration.user_id_map
WHERE source_system = 'ptyes'
ORDER BY legacy_user_id`)
	if err != nil {
		return nil, fmt.Errorf("read migrated user IDs for workgroups: %w", err)
	}
	defer rows.Close()
	result := make(map[int64]uuid.UUID, expected)
	for rows.Next() {
		var legacyUserID int64
		var userID uuid.UUID
		if err := rows.Scan(&legacyUserID, &userID); err != nil {
			return nil, fmt.Errorf("scan migrated user ID for workgroups: %w", err)
		}
		if legacyUserID <= 0 || userID == uuid.Nil {
			return nil, errors.New("migrated user ID map contains an invalid workgroup identity")
		}
		result[legacyUserID] = userID
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish migrated user IDs for workgroups: %w", err)
	}
	if int64(len(result)) != expected {
		return nil, fmt.Errorf("migrated workgroup user map has %d rows, want %d", len(result), expected)
	}
	return result, nil
}

func (importer *Importer) requirePrerequisites(ctx context.Context, tx pgx.Tx, expectedUsers int64, mutate bool) error {
	var runCount, mappingCount, accessCount int64
	if err := tx.QueryRow(ctx, `
SELECT
    count(*) FILTER (WHERE id = $1 AND source_system = 'ptyes')::bigint,
    (SELECT count(*) FROM migration.user_id_map WHERE source_system = 'ptyes'),
    (SELECT count(*) FROM migration.user_id_map AS mapping
       JOIN identity.user_access_states AS access ON access.user_id = mapping.user_id
      WHERE mapping.source_system = 'ptyes')
FROM migration.runs`, importer.config.RunID).Scan(&runCount, &mappingCount, &accessCount); err != nil {
		return fmt.Errorf("verify legacy medal prerequisites: %w", err)
	}
	if runCount != 1 || mappingCount != expectedUsers || accessCount != expectedUsers {
		return fmt.Errorf("legacy medal prerequisites are incomplete: run=%d mappings=%d access=%d expected=%d", runCount, mappingCount, accessCount, expectedUsers)
	}
	var existingBenefits, unsafeTimelines int64
	if err := tx.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM migration.medal_benefit_openings WHERE source_system = 'ptyes'),
    count(*) FILTER (WHERE latest.revision <> 1)::bigint
FROM migration.user_id_map AS mapping
JOIN LATERAL (
    SELECT revision
    FROM identity.user_reward_benefit_revisions
    WHERE user_id = mapping.user_id
    ORDER BY revision DESC
    LIMIT 1
) AS latest ON true
WHERE mapping.source_system = 'ptyes'`).Scan(&existingBenefits, &unsafeTimelines); err != nil {
		return fmt.Errorf("verify legacy medal benefit timelines: %w", err)
	}
	if existingBenefits == 0 && unsafeTimelines != 0 {
		return fmt.Errorf("cannot backfill medal benefits after %d migrated user timelines advanced", unsafeTimelines)
	}
	if existingBenefits != 0 && existingBenefits != expectedUsers {
		return fmt.Errorf("legacy medal benefit evidence is partial: got %d, want %d", existingBenefits, expectedUsers)
	}
	if !mutate && existingBenefits != expectedUsers {
		return fmt.Errorf("legacy medal benefit evidence is missing: got %d, want %d", existingBenefits, expectedUsers)
	}
	return nil
}

func createStages(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `
CREATE TEMP TABLE legacy_medal_definition_stage (
    legacy_id bigint PRIMARY KEY, name text NOT NULL, description text,
    image_large text, image_small text, get_type bigint NOT NULL,
    acquisition_method text NOT NULL, price_text text NOT NULL, price bigint NOT NULL,
    duration_days bigint NOT NULL, display_on_page boolean NOT NULL, priority bigint NOT NULL,
    upload_bonus_text text NOT NULL, upload_bonus_bps bigint NOT NULL,
    download_bonus_text text NOT NULL, download_bonus_bps bigint NOT NULL,
    magic_bonus_text text NOT NULL, magic_bonus_bps bigint NOT NULL,
    invite_bonus bigint NOT NULL, is_workgroup boolean NOT NULL,
    conditions_raw text, conditions_json text NOT NULL,
    privileges_raw text, privileges_json text NOT NULL, pool_eligible boolean NOT NULL,
    reward_magic_text text NOT NULL, reward_credits_text text NOT NULL,
    periodic_reward_magic bigint NOT NULL, reward_cycle text,
    sale_begin_at timestamptz, sale_end_at timestamptz, inventory bigint,
    created_at timestamptz, updated_at timestamptz, source_fingerprint bytea NOT NULL
) ON COMMIT DROP;
CREATE TEMP TABLE legacy_user_medal_stage (
    legacy_id bigint PRIMARY KEY, legacy_user_id bigint NOT NULL,
    legacy_medal_id bigint NOT NULL, status bigint NOT NULL, priority bigint NOT NULL,
    expires_at timestamptz, legacy_granted_by bigint, note text,
    created_at timestamptz, updated_at timestamptz, last_reward_at timestamptz,
    source_fingerprint bytea NOT NULL,
    UNIQUE (legacy_user_id, legacy_medal_id)
) ON COMMIT DROP;
CREATE TEMP TABLE legacy_medal_benefit_stage (
    legacy_user_id bigint PRIMARY KEY, active_contributing_medals bigint NOT NULL,
    uncapped_magic_bonus_bps bigint NOT NULL, magic_bonus_bps bigint NOT NULL,
    source_fingerprint bytea NOT NULL
) ON COMMIT DROP;
CREATE TEMP TABLE legacy_workgroup_membership_stage (
    legacy_user_id bigint NOT NULL, user_id uuid NOT NULL,
    group_kind text NOT NULL, membership_id uuid NOT NULL,
    transition_id uuid NOT NULL, started_at timestamptz NOT NULL,
    legacy_user_medal_ids bigint[] NOT NULL,
    legacy_medal_ids bigint[] NOT NULL,
    source_fingerprint bytea NOT NULL,
    command_json text, command_sha256 bytea,
    PRIMARY KEY (legacy_user_id, group_kind),
    UNIQUE (membership_id), UNIQUE (transition_id),
    CHECK (cardinality(legacy_user_medal_ids) > 0),
    CHECK (cardinality(legacy_user_medal_ids) = cardinality(legacy_medal_ids)),
    CHECK ((group_kind = 'retention') = (command_json IS NOT NULL)),
    CHECK ((command_json IS NULL) = (command_sha256 IS NULL))
) ON COMMIT DROP`)
	if err != nil {
		return fmt.Errorf("create legacy medal stages: %w", err)
	}
	return nil
}

func (importer *Importer) copyMedals(ctx context.Context, tx pgx.Tx, medals []sourceMedal) error {
	count, err := tx.CopyFrom(ctx, pgx.Identifier{"legacy_medal_definition_stage"}, []string{
		"legacy_id", "name", "description", "image_large", "image_small", "get_type",
		"acquisition_method", "price_text", "price", "duration_days", "display_on_page", "priority",
		"upload_bonus_text", "upload_bonus_bps", "download_bonus_text", "download_bonus_bps",
		"magic_bonus_text", "magic_bonus_bps", "invite_bonus", "is_workgroup",
		"conditions_raw", "conditions_json", "privileges_raw", "privileges_json", "pool_eligible",
		"reward_magic_text", "reward_credits_text", "periodic_reward_magic", "reward_cycle",
		"sale_begin_at", "sale_end_at", "inventory", "created_at", "updated_at", "source_fingerprint",
	}, pgx.CopyFromSlice(len(medals), func(index int) ([]any, error) {
		row := medals[index]
		fingerprint := row.fingerprint()
		return []any{
			row.LegacyID, row.Name, row.Description, row.ImageLarge, row.ImageSmall, row.GetType,
			row.AcquisitionMethod, row.PriceText, row.Price, row.DurationDays, row.DisplayOnPage, row.Priority,
			row.UploadBonusText, row.UploadBonusBPS, row.DownloadBonusText, row.DownloadBonusBPS,
			row.MagicBonusText, row.MagicBonusBPS, row.InviteBonus, row.IsWorkgroup,
			row.ConditionsRaw, row.ConditionsJSON, row.PrivilegesRaw, row.PrivilegesJSON, row.PoolEligible,
			row.RewardMagicText, row.RewardCreditsText, row.PeriodicRewardMagic, row.RewardCycle,
			row.SaleBeginAt, row.SaleEndAt, row.Inventory, row.CreatedAt, row.UpdatedAt, fingerprint[:],
		}, nil
	}))
	if err != nil {
		return fmt.Errorf("stage PtYes medal definitions: %w", err)
	}
	if count != int64(len(medals)) {
		return fmt.Errorf("staged %d PtYes medals, want %d", count, len(medals))
	}
	return nil
}

func (importer *Importer) copyHoldings(ctx context.Context, tx pgx.Tx, holdings []sourceHolding) error {
	count, err := tx.CopyFrom(ctx, pgx.Identifier{"legacy_user_medal_stage"}, []string{
		"legacy_id", "legacy_user_id", "legacy_medal_id", "status", "priority", "expires_at",
		"legacy_granted_by", "note", "created_at", "updated_at", "last_reward_at", "source_fingerprint",
	}, pgx.CopyFromSlice(len(holdings), func(index int) ([]any, error) {
		row := holdings[index]
		fingerprint := row.fingerprint()
		return []any{
			row.LegacyID, row.LegacyUserID, row.LegacyMedalID, row.Status, row.Priority, row.ExpiresAt,
			row.LegacyGrantedBy, row.Note, row.CreatedAt, row.UpdatedAt, row.LastRewardAt, fingerprint[:],
		}, nil
	}))
	if err != nil {
		return fmt.Errorf("stage PtYes user medals: %w", err)
	}
	if count != int64(len(holdings)) {
		return fmt.Errorf("staged %d PtYes user medals, want %d", count, len(holdings))
	}
	return nil
}

func (importer *Importer) copyBenefits(ctx context.Context, tx pgx.Tx, benefits []sourceBenefit) error {
	count, err := tx.CopyFrom(ctx, pgx.Identifier{"legacy_medal_benefit_stage"}, []string{
		"legacy_user_id", "active_contributing_medals", "uncapped_magic_bonus_bps",
		"magic_bonus_bps", "source_fingerprint",
	}, pgx.CopyFromSlice(len(benefits), func(index int) ([]any, error) {
		row := benefits[index]
		fingerprint := row.fingerprint(importer.config.OccurredAt)
		return []any{row.LegacyUserID, row.ActiveContributingMedals, row.UncappedMagicBonusBPS, row.MagicBonusBPS, fingerprint[:]}, nil
	}))
	if err != nil {
		return fmt.Errorf("stage PtYes medal benefits: %w", err)
	}
	if count != int64(len(benefits)) {
		return fmt.Errorf("staged %d PtYes medal benefits, want %d", count, len(benefits))
	}
	return nil
}

func (importer *Importer) copyWorkgroupMemberships(ctx context.Context, tx pgx.Tx, memberships []sourceWorkgroupMembership) error {
	count, err := tx.CopyFrom(ctx, pgx.Identifier{"legacy_workgroup_membership_stage"}, []string{
		"legacy_user_id", "user_id", "group_kind", "membership_id", "transition_id",
		"started_at", "legacy_user_medal_ids", "legacy_medal_ids", "source_fingerprint",
		"command_json", "command_sha256",
	}, pgx.CopyFromSlice(len(memberships), func(index int) ([]any, error) {
		row := memberships[index]
		fingerprint := row.fingerprint(importer.config.OccurredAt)
		var commandSHA256 any
		if len(row.CommandSHA256) != 0 {
			commandSHA256 = row.CommandSHA256
		}
		return []any{
			row.LegacyUserID, row.UserID, row.GroupKind, row.MembershipID, row.TransitionID,
			row.StartedAt, row.LegacyUserMedalIDs, row.LegacyMedalIDs, fingerprint[:],
			row.CommandJSON, commandSHA256,
		}, nil
	}))
	if err != nil {
		return fmt.Errorf("stage PtYes workgroup memberships: %w", err)
	}
	if count != int64(len(memberships)) {
		return fmt.Errorf("staged %d PtYes workgroup memberships, want %d", count, len(memberships))
	}
	return nil
}

func (importer *Importer) validateStages(ctx context.Context, tx pgx.Tx, definitions, holdings, users, workgroups, workgroupOrigins int64) error {
	var stagedDefinitions, stagedHoldings, mappedHoldings, stagedBenefits, mappedBenefits, mappedGranters int64
	var stagedWorkgroups, mappedWorkgroups, matchedWorkgroupOrigins int64
	if err := tx.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM legacy_medal_definition_stage),
    (SELECT count(*) FROM legacy_user_medal_stage),
    (SELECT count(*) FROM legacy_user_medal_stage AS stage
       JOIN migration.user_id_map AS mapping
         ON mapping.source_system='ptyes' AND mapping.legacy_user_id=stage.legacy_user_id
       JOIN legacy_medal_definition_stage AS medal ON medal.legacy_id=stage.legacy_medal_id),
    (SELECT count(*) FROM legacy_medal_benefit_stage),
    (SELECT count(*) FROM legacy_medal_benefit_stage AS stage
       JOIN migration.user_id_map AS mapping
         ON mapping.source_system='ptyes' AND mapping.legacy_user_id=stage.legacy_user_id),
    (SELECT count(*) FROM legacy_user_medal_stage AS stage
       LEFT JOIN migration.user_id_map AS granter
         ON granter.source_system='ptyes' AND granter.legacy_user_id=stage.legacy_granted_by
      WHERE stage.legacy_granted_by IS NULL OR granter.user_id IS NOT NULL),
    (SELECT count(*) FROM legacy_workgroup_membership_stage),
    (SELECT count(*) FROM legacy_workgroup_membership_stage AS stage
       JOIN migration.user_id_map AS mapping
         ON mapping.source_system='ptyes'
        AND mapping.legacy_user_id=stage.legacy_user_id
        AND mapping.user_id=stage.user_id
      WHERE stage.group_kind IN ('reseed','review','retention')),
    (SELECT count(*)
       FROM legacy_workgroup_membership_stage AS stage
       CROSS JOIN LATERAL unnest(
           stage.legacy_user_medal_ids,
           stage.legacy_medal_ids
       ) AS origin(legacy_user_medal_id, legacy_medal_id)
       JOIN legacy_user_medal_stage AS holding
         ON holding.legacy_id=origin.legacy_user_medal_id
        AND holding.legacy_user_id=stage.legacy_user_id
        AND holding.legacy_medal_id=origin.legacy_medal_id
       JOIN legacy_medal_definition_stage AS medal
         ON medal.legacy_id=origin.legacy_medal_id
        AND medal.is_workgroup=true)`).Scan(
		&stagedDefinitions, &stagedHoldings, &mappedHoldings,
		&stagedBenefits, &mappedBenefits, &mappedGranters,
		&stagedWorkgroups, &mappedWorkgroups, &matchedWorkgroupOrigins,
	); err != nil {
		return fmt.Errorf("validate legacy medal stages: %w", err)
	}
	if stagedDefinitions != definitions || stagedHoldings != holdings || mappedHoldings != holdings ||
		stagedBenefits != users || mappedBenefits != users || mappedGranters != holdings ||
		stagedWorkgroups != workgroups || mappedWorkgroups != workgroups || matchedWorkgroupOrigins != workgroupOrigins {
		return fmt.Errorf("legacy medal stage mismatch: definitions=%d/%d holdings=%d/%d mapped=%d benefits=%d/%d mapped=%d granters=%d workgroups=%d/%d mapped=%d origins=%d/%d", stagedDefinitions, definitions, stagedHoldings, holdings, mappedHoldings, stagedBenefits, users, mappedBenefits, mappedGranters, stagedWorkgroups, workgroups, mappedWorkgroups, matchedWorkgroupOrigins, workgroupOrigins)
	}
	return nil
}

func (importer *Importer) insertAndVerifySettings(ctx context.Context, tx pgx.Tx, settings sourceSettings, mutate bool) (int64, error) {
	fingerprint := settings.fingerprint()
	var inserted int64
	if mutate {
		tag, err := tx.Exec(ctx, `
INSERT INTO migration.medal_system_openings (
    source_system, enabled, maximum_wear_count, maximum_upload_bonus_bps,
    maximum_download_discount_bps, maximum_magic_bonus_bps, maximum_invite_bonus,
    condition_check_day, condition_warning_days, source_fingerprint, first_run_id, imported_at
) VALUES ('ptyes',$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (source_system) DO NOTHING`,
			settings.Enabled, settings.MaximumWearCount, settings.MaximumUploadBonusBPS,
			settings.MaximumDownloadDiscountBPS, settings.MaximumMagicBonusBPS,
			settings.MaximumInviteBonus, settings.ConditionCheckDay, settings.ConditionWarningDays,
			fingerprint[:], importer.config.RunID, importer.config.OccurredAt,
		)
		if err != nil {
			return 0, fmt.Errorf("insert PtYes medal settings evidence: %w", err)
		}
		inserted = tag.RowsAffected()
	}
	var conflicts int64
	if err := tx.QueryRow(ctx, `
SELECT count(*) FILTER (WHERE
    enabled IS DISTINCT FROM $1 OR maximum_wear_count IS DISTINCT FROM $2
    OR maximum_upload_bonus_bps IS DISTINCT FROM $3
    OR maximum_download_discount_bps IS DISTINCT FROM $4
    OR maximum_magic_bonus_bps IS DISTINCT FROM $5
    OR maximum_invite_bonus IS DISTINCT FROM $6
    OR condition_check_day IS DISTINCT FROM $7
    OR condition_warning_days IS DISTINCT FROM $8
    OR source_fingerprint IS DISTINCT FROM $9
) FROM migration.medal_system_openings WHERE source_system='ptyes'`,
		settings.Enabled, settings.MaximumWearCount, settings.MaximumUploadBonusBPS,
		settings.MaximumDownloadDiscountBPS, settings.MaximumMagicBonusBPS,
		settings.MaximumInviteBonus, settings.ConditionCheckDay, settings.ConditionWarningDays,
		fingerprint[:],
	).Scan(&conflicts); err != nil {
		return 0, fmt.Errorf("verify PtYes medal settings evidence: %w", err)
	}
	if conflicts != 0 {
		return 0, errors.New("existing PtYes medal settings evidence conflicts with the snapshot")
	}
	return inserted, nil
}

func (importer *Importer) insertDefinitions(ctx context.Context, tx pgx.Tx) (int64, error) {
	if _, err := tx.Exec(ctx, `
INSERT INTO economy.medal_definitions (
    id, name, description, image_large_path, image_small_path, acquisition_method,
    price, duration_days, display_on_page, priority, upload_bonus_bps,
    download_discount_bps, magic_bonus_bps, invite_bonus, is_workgroup,
    conditions, privileges, pool_eligible, periodic_reward_magic, reward_cycle,
    sale_begin_at, sale_end_at, inventory, created_at, updated_at
)
SELECT legacy_id, name, description, NULL, NULL, acquisition_method,
       price, duration_days, display_on_page, priority, upload_bonus_bps,
       download_bonus_bps, magic_bonus_bps, invite_bonus, is_workgroup,
       conditions_json::jsonb, privileges_json::jsonb, pool_eligible,
       periodic_reward_magic, reward_cycle, sale_begin_at, sale_end_at, inventory,
       COALESCE(created_at, $1), COALESCE(updated_at, created_at, $1)
FROM legacy_medal_definition_stage
ORDER BY legacy_id
ON CONFLICT (id) DO NOTHING`, importer.config.OccurredAt); err != nil {
		return 0, fmt.Errorf("insert medal definitions: %w", err)
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO migration.medal_definition_openings (
    source_system, legacy_medal_id, medal_id, source_get_type, source_price,
    source_upload_bonus, source_download_discount, source_magic_bonus,
    source_reward_magic, source_reward_credits, source_conditions, source_privileges,
    source_image_large, source_image_small, source_fingerprint, first_run_id, imported_at
)
SELECT 'ptyes', legacy_id, legacy_id, get_type, price_text::numeric,
       upload_bonus_text::numeric, download_bonus_text::numeric, magic_bonus_text::numeric,
       reward_magic_text::numeric, reward_credits_text::numeric, conditions_raw, privileges_raw,
       image_large, image_small, source_fingerprint, $1, $2
FROM legacy_medal_definition_stage
ORDER BY legacy_id
ON CONFLICT (source_system, legacy_medal_id) DO NOTHING`, importer.config.RunID, importer.config.OccurredAt)
	if err != nil {
		return 0, fmt.Errorf("insert medal definition evidence: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (importer *Importer) insertHoldings(ctx context.Context, tx pgx.Tx) (int64, error) {
	if _, err := tx.Exec(ctx, `
INSERT INTO economy.user_medals (
    id, user_id, medal_id, state, priority, expires_at, granted_by, note,
    acquired_at, updated_at, last_reward_at
)
SELECT stage.legacy_id, owner_map.user_id, stage.legacy_medal_id,
       CASE stage.status WHEN 2 THEN 'wearing' ELSE 'owned' END,
       stage.priority, stage.expires_at, granter_map.user_id, stage.note,
       COALESCE(stage.created_at, $1), COALESCE(stage.updated_at, stage.created_at, $1),
       stage.last_reward_at
FROM legacy_user_medal_stage AS stage
JOIN migration.user_id_map AS owner_map
  ON owner_map.source_system='ptyes' AND owner_map.legacy_user_id=stage.legacy_user_id
LEFT JOIN migration.user_id_map AS granter_map
  ON granter_map.source_system='ptyes' AND granter_map.legacy_user_id=stage.legacy_granted_by
ORDER BY stage.legacy_id
ON CONFLICT (id) DO NOTHING`, importer.config.OccurredAt); err != nil {
		return 0, fmt.Errorf("insert user medals: %w", err)
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO migration.user_medal_openings (
    source_system, legacy_user_medal_id, user_medal_id, legacy_user_id, user_id,
    legacy_medal_id, medal_id, source_status, source_priority, source_expires_at,
    legacy_granted_by, granted_by, source_fingerprint, first_run_id, imported_at
)
SELECT 'ptyes', stage.legacy_id, stage.legacy_id, stage.legacy_user_id, owner_map.user_id,
       stage.legacy_medal_id, stage.legacy_medal_id, stage.status, stage.priority,
       stage.expires_at, stage.legacy_granted_by, granter_map.user_id,
       stage.source_fingerprint, $1, $2
FROM legacy_user_medal_stage AS stage
JOIN migration.user_id_map AS owner_map
  ON owner_map.source_system='ptyes' AND owner_map.legacy_user_id=stage.legacy_user_id
LEFT JOIN migration.user_id_map AS granter_map
  ON granter_map.source_system='ptyes' AND granter_map.legacy_user_id=stage.legacy_granted_by
ORDER BY stage.legacy_id
ON CONFLICT (source_system, legacy_user_medal_id) DO NOTHING`, importer.config.RunID, importer.config.OccurredAt)
	if err != nil {
		return 0, fmt.Errorf("insert user medal evidence: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (importer *Importer) insertWorkgroupMemberships(ctx context.Context, tx pgx.Tx) (int64, error) {
	var unmanagedConflicts int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)
FROM legacy_workgroup_membership_stage AS stage
JOIN workgroups.memberships AS membership
  ON membership.group_kind=stage.group_kind AND membership.user_id=stage.user_id
LEFT JOIN migration.workgroup_membership_openings AS opening
  ON opening.source_system='ptyes'
 AND opening.legacy_user_id=stage.legacy_user_id
 AND opening.group_kind=stage.group_kind
WHERE opening.membership_id IS NULL
   OR opening.membership_id IS DISTINCT FROM membership.id
   OR membership.source IS DISTINCT FROM 'legacy_migration'`).Scan(&unmanagedConflicts); err != nil {
		return 0, fmt.Errorf("check existing workgroup membership conflicts: %w", err)
	}
	if unmanagedConflicts != 0 {
		return 0, fmt.Errorf("%d PtYes workgroup memberships conflict with existing non-migration memberships", unmanagedConflicts)
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO workgroups.memberships (
    id, group_kind, user_id, status, source, version,
    started_at, updated_at
)
SELECT membership_id, group_kind, user_id, 'active', 'legacy_migration', 1,
       started_at, $1
FROM legacy_workgroup_membership_stage
ORDER BY legacy_user_id, group_kind
ON CONFLICT DO NOTHING`, importer.config.OccurredAt); err != nil {
		return 0, fmt.Errorf("insert migrated workgroup memberships: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO workgroups.membership_transitions (
    id, membership_id, group_kind, user_id, transition,
    from_status, to_status, actor_id, source, source_application_id,
    reason, authorization_decision_id, state_version, occurred_at
)
SELECT transition_id, membership_id, group_kind, user_id, 'joined',
       NULL, 'active', NULL, 'legacy_migration', NULL,
       'Rousi 工作组勋章迁移开账。', NULL, 1, $1
FROM legacy_workgroup_membership_stage
ORDER BY legacy_user_id, group_kind
ON CONFLICT DO NOTHING`, importer.config.OccurredAt); err != nil {
		return 0, fmt.Errorf("insert migrated workgroup membership transitions: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO workgroups.settlement_benefit_outbox (
    transition_id, user_id, state_version, effective_at,
    command_json, command_sha256, available_at, created_at
)
SELECT transition_id, user_id, 1, $1,
       command_json, command_sha256, $1, $1
FROM legacy_workgroup_membership_stage
WHERE group_kind='retention'
ORDER BY legacy_user_id
ON CONFLICT DO NOTHING`, importer.config.OccurredAt); err != nil {
		return 0, fmt.Errorf("enqueue migrated retention workgroup benefits: %w", err)
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO migration.workgroup_membership_openings (
    source_system, legacy_user_id, user_id, group_kind,
    membership_id, transition_id, legacy_user_medal_ids, legacy_medal_ids,
    started_at, source_fingerprint, first_run_id, imported_at
)
SELECT 'ptyes', legacy_user_id, user_id, group_kind,
       membership_id, transition_id, legacy_user_medal_ids, legacy_medal_ids,
       started_at, source_fingerprint, $1, $2
FROM legacy_workgroup_membership_stage
ORDER BY legacy_user_id, group_kind
ON CONFLICT (source_system, legacy_user_id, group_kind) DO NOTHING`,
		importer.config.RunID, importer.config.OccurredAt)
	if err != nil {
		return 0, fmt.Errorf("insert migrated workgroup membership evidence: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (importer *Importer) insertBenefits(ctx context.Context, tx pgx.Tx) (int64, error) {
	var existing int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM migration.medal_benefit_openings WHERE source_system='ptyes'`).Scan(&existing); err != nil {
		return 0, fmt.Errorf("count existing medal benefit evidence: %w", err)
	}
	if existing == 0 {
		if _, err := tx.Exec(ctx, `
INSERT INTO identity.user_reward_benefit_revisions (
    user_id, revision, effective_from, vip_enabled, vip_until,
    medal_bonus_bps, source_kind, source_reference, created_at
)
SELECT mapping.user_id, 2, $1, access.vip_enabled, access.vip_until,
       stage.magic_bonus_bps, 'runtime', 'rousi-medals:' || $2::text, $1
FROM legacy_medal_benefit_stage AS stage
JOIN migration.user_id_map AS mapping
  ON mapping.source_system='ptyes' AND mapping.legacy_user_id=stage.legacy_user_id
JOIN identity.user_access_states AS access ON access.user_id=mapping.user_id
ORDER BY mapping.user_id`, importer.config.OccurredAt, importer.config.RunID); err != nil {
			return 0, fmt.Errorf("append migrated medal benefit revisions: %w", err)
		}
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO migration.medal_benefit_openings (
    source_system, legacy_user_id, user_id, active_contributing_medals,
    uncapped_magic_bonus_bps, magic_bonus_bps, benefit_revision,
    effective_from, source_fingerprint, first_run_id, imported_at
)
SELECT 'ptyes', stage.legacy_user_id, mapping.user_id, stage.active_contributing_medals,
       stage.uncapped_magic_bonus_bps, stage.magic_bonus_bps, 2,
       $1, stage.source_fingerprint, $2, $1
FROM legacy_medal_benefit_stage AS stage
JOIN migration.user_id_map AS mapping
  ON mapping.source_system='ptyes' AND mapping.legacy_user_id=stage.legacy_user_id
ORDER BY stage.legacy_user_id
ON CONFLICT (source_system, legacy_user_id) DO NOTHING`, importer.config.OccurredAt, importer.config.RunID)
	if err != nil {
		return 0, fmt.Errorf("insert migrated medal benefit evidence: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (importer *Importer) verifyAll(ctx context.Context, tx pgx.Tx, definitions, holdings, users, workgroups int64) error {
	var definitionReceipts, definitionConflicts int64
	if err := tx.QueryRow(ctx, `
SELECT count(opening.legacy_medal_id), count(*) FILTER (WHERE
    opening.legacy_medal_id IS NULL OR opening.medal_id IS DISTINCT FROM stage.legacy_id
    OR opening.source_get_type IS DISTINCT FROM stage.get_type
    OR opening.source_price IS DISTINCT FROM stage.price_text::numeric
    OR opening.source_upload_bonus IS DISTINCT FROM stage.upload_bonus_text::numeric
    OR opening.source_download_discount IS DISTINCT FROM stage.download_bonus_text::numeric
    OR opening.source_magic_bonus IS DISTINCT FROM stage.magic_bonus_text::numeric
    OR opening.source_reward_magic IS DISTINCT FROM stage.reward_magic_text::numeric
    OR opening.source_reward_credits IS DISTINCT FROM stage.reward_credits_text::numeric
    OR opening.source_conditions IS DISTINCT FROM stage.conditions_raw
    OR opening.source_privileges IS DISTINCT FROM stage.privileges_raw
    OR opening.source_image_large IS DISTINCT FROM stage.image_large
    OR opening.source_image_small IS DISTINCT FROM stage.image_small
    OR opening.source_fingerprint IS DISTINCT FROM stage.source_fingerprint
    OR domain.name IS DISTINCT FROM stage.name
    OR domain.description IS DISTINCT FROM stage.description
    OR domain.acquisition_method IS DISTINCT FROM stage.acquisition_method
    OR domain.price IS DISTINCT FROM stage.price
    OR domain.magic_bonus_bps IS DISTINCT FROM stage.magic_bonus_bps
    OR domain.conditions IS DISTINCT FROM stage.conditions_json::jsonb
    OR domain.privileges IS DISTINCT FROM stage.privileges_json::jsonb
)
FROM legacy_medal_definition_stage AS stage
LEFT JOIN migration.medal_definition_openings AS opening
  ON opening.source_system='ptyes' AND opening.legacy_medal_id=stage.legacy_id
LEFT JOIN economy.medal_definitions AS domain ON domain.id=stage.legacy_id`).Scan(&definitionReceipts, &definitionConflicts); err != nil {
		return fmt.Errorf("verify medal definitions: %w", err)
	}
	var holdingReceipts, holdingConflicts int64
	if err := tx.QueryRow(ctx, `
SELECT count(opening.legacy_user_medal_id), count(*) FILTER (WHERE
    opening.legacy_user_medal_id IS NULL OR opening.user_medal_id IS DISTINCT FROM stage.legacy_id
    OR opening.legacy_user_id IS DISTINCT FROM stage.legacy_user_id
    OR opening.legacy_medal_id IS DISTINCT FROM stage.legacy_medal_id
    OR opening.source_status IS DISTINCT FROM stage.status
    OR opening.source_priority IS DISTINCT FROM stage.priority
    OR opening.source_expires_at IS DISTINCT FROM stage.expires_at
    OR opening.legacy_granted_by IS DISTINCT FROM stage.legacy_granted_by
    OR opening.source_fingerprint IS DISTINCT FROM stage.source_fingerprint
    OR domain.user_id IS DISTINCT FROM owner_map.user_id
    OR domain.medal_id IS DISTINCT FROM stage.legacy_medal_id
    OR domain.state IS DISTINCT FROM CASE stage.status WHEN 2 THEN 'wearing' ELSE 'owned' END
    OR domain.priority IS DISTINCT FROM stage.priority
    OR domain.expires_at IS DISTINCT FROM stage.expires_at
)
FROM legacy_user_medal_stage AS stage
JOIN migration.user_id_map AS owner_map
  ON owner_map.source_system='ptyes' AND owner_map.legacy_user_id=stage.legacy_user_id
LEFT JOIN migration.user_medal_openings AS opening
  ON opening.source_system='ptyes' AND opening.legacy_user_medal_id=stage.legacy_id
LEFT JOIN economy.user_medals AS domain ON domain.id=stage.legacy_id`).Scan(&holdingReceipts, &holdingConflicts); err != nil {
		return fmt.Errorf("verify user medals: %w", err)
	}
	var benefitReceipts, benefitConflicts int64
	if err := tx.QueryRow(ctx, `
SELECT count(opening.legacy_user_id), count(*) FILTER (WHERE
    opening.legacy_user_id IS NULL OR opening.user_id IS DISTINCT FROM mapping.user_id
    OR opening.active_contributing_medals IS DISTINCT FROM stage.active_contributing_medals
    OR opening.uncapped_magic_bonus_bps IS DISTINCT FROM stage.uncapped_magic_bonus_bps
    OR opening.magic_bonus_bps IS DISTINCT FROM stage.magic_bonus_bps
    OR opening.benefit_revision IS DISTINCT FROM 2
    OR opening.effective_from IS DISTINCT FROM $1
    OR opening.source_fingerprint IS DISTINCT FROM stage.source_fingerprint
    OR revision.medal_bonus_bps IS DISTINCT FROM stage.magic_bonus_bps
    OR revision.vip_enabled IS DISTINCT FROM access.vip_enabled
    OR revision.vip_until IS DISTINCT FROM access.vip_until
    OR revision.source_reference IS DISTINCT FROM 'rousi-medals:' || $2::text
)
FROM legacy_medal_benefit_stage AS stage
JOIN migration.user_id_map AS mapping
  ON mapping.source_system='ptyes' AND mapping.legacy_user_id=stage.legacy_user_id
JOIN identity.user_access_states AS access ON access.user_id=mapping.user_id
LEFT JOIN migration.medal_benefit_openings AS opening
  ON opening.source_system='ptyes' AND opening.legacy_user_id=stage.legacy_user_id
LEFT JOIN identity.user_reward_benefit_revisions AS revision
  ON revision.user_id=mapping.user_id AND revision.revision=2`, importer.config.OccurredAt, importer.config.RunID).Scan(&benefitReceipts, &benefitConflicts); err != nil {
		return fmt.Errorf("verify migrated medal benefits: %w", err)
	}
	var workgroupReceipts, workgroupConflicts int64
	if err := tx.QueryRow(ctx, `
SELECT count(opening.legacy_user_id), count(*) FILTER (WHERE
    opening.legacy_user_id IS NULL
    OR opening.user_id IS DISTINCT FROM stage.user_id
    OR opening.group_kind IS DISTINCT FROM stage.group_kind
    OR opening.membership_id IS DISTINCT FROM stage.membership_id
    OR opening.transition_id IS DISTINCT FROM stage.transition_id
    OR opening.legacy_user_medal_ids IS DISTINCT FROM stage.legacy_user_medal_ids
    OR opening.legacy_medal_ids IS DISTINCT FROM stage.legacy_medal_ids
    OR opening.started_at IS DISTINCT FROM stage.started_at
    OR opening.source_fingerprint IS DISTINCT FROM stage.source_fingerprint
    OR opening.imported_at IS DISTINCT FROM $1
    OR membership.group_kind IS DISTINCT FROM stage.group_kind
    OR membership.user_id IS DISTINCT FROM stage.user_id
    OR membership.status IS DISTINCT FROM 'active'
    OR membership.source IS DISTINCT FROM 'legacy_migration'
    OR membership.version IS DISTINCT FROM 1
    OR membership.started_at IS DISTINCT FROM stage.started_at
    OR membership.ended_at IS NOT NULL
    OR membership.updated_at IS DISTINCT FROM $1
    OR transition.membership_id IS DISTINCT FROM stage.membership_id
    OR transition.group_kind IS DISTINCT FROM stage.group_kind
    OR transition.user_id IS DISTINCT FROM stage.user_id
    OR transition.transition IS DISTINCT FROM 'joined'
    OR transition.from_status IS NOT NULL
    OR transition.to_status IS DISTINCT FROM 'active'
    OR transition.actor_id IS NOT NULL
    OR transition.source IS DISTINCT FROM 'legacy_migration'
    OR transition.source_application_id IS NOT NULL
    OR transition.authorization_decision_id IS NOT NULL
    OR transition.state_version IS DISTINCT FROM 1
    OR transition.occurred_at IS DISTINCT FROM $1
    OR ((stage.group_kind = 'retention') IS DISTINCT FROM (outbox.transition_id IS NOT NULL))
    OR (stage.group_kind = 'retention' AND (
        outbox.user_id IS DISTINCT FROM stage.user_id
        OR outbox.state_version IS DISTINCT FROM 1
        OR outbox.effective_at IS DISTINCT FROM $1
        OR outbox.command_json IS DISTINCT FROM stage.command_json
        OR outbox.command_sha256 IS DISTINCT FROM stage.command_sha256
        OR outbox.created_at IS DISTINCT FROM $1
    ))
)
FROM legacy_workgroup_membership_stage AS stage
LEFT JOIN migration.workgroup_membership_openings AS opening
  ON opening.source_system='ptyes'
 AND opening.legacy_user_id=stage.legacy_user_id
 AND opening.group_kind=stage.group_kind
LEFT JOIN workgroups.memberships AS membership
  ON membership.id=stage.membership_id
LEFT JOIN workgroups.membership_transitions AS transition
  ON transition.id=stage.transition_id
LEFT JOIN workgroups.settlement_benefit_outbox AS outbox
  ON outbox.transition_id=stage.transition_id`, importer.config.OccurredAt).Scan(&workgroupReceipts, &workgroupConflicts); err != nil {
		return fmt.Errorf("verify migrated workgroup memberships: %w", err)
	}
	if definitionReceipts != definitions || definitionConflicts != 0 ||
		holdingReceipts != holdings || holdingConflicts != 0 ||
		benefitReceipts != users || benefitConflicts != 0 ||
		workgroupReceipts != workgroups || workgroupConflicts != 0 {
		return fmt.Errorf("legacy medal reconciliation failed: definitions=%d/%d conflicts=%d holdings=%d/%d conflicts=%d benefits=%d/%d conflicts=%d workgroups=%d/%d conflicts=%d", definitionReceipts, definitions, definitionConflicts, holdingReceipts, holdings, holdingConflicts, benefitReceipts, users, benefitConflicts, workgroupReceipts, workgroups, workgroupConflicts)
	}
	return nil
}

func advanceIdentitySequences(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `
SELECT setval(pg_get_serial_sequence('economy.medal_definitions','id'),
              (SELECT GREATEST(MAX(id), 1) FROM economy.medal_definitions), true);
SELECT setval(pg_get_serial_sequence('economy.user_medals','id'),
              (SELECT GREATEST(MAX(id), 1) FROM economy.user_medals), true)`); err != nil {
		return fmt.Errorf("advance medal identity sequences: %w", err)
	}
	return nil
}

func readResult(ctx context.Context, tx pgx.Tx, config Config, definitionRows, holdingRows, benefitRows, workgroupRows int64) (Result, error) {
	result := Result{
		RunID:                  config.RunID,
		ImportedDefinitionRows: definitionRows,
		ImportedUserMedalRows:  holdingRows,
		ImportedBenefitRows:    benefitRows,
		ImportedWorkgroupRows:  workgroupRows,
	}
	if err := tx.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM migration.medal_definition_openings WHERE source_system='ptyes'),
    (SELECT count(*) FROM migration.user_medal_openings WHERE source_system='ptyes'),
    (SELECT count(*) FROM migration.user_medal_openings WHERE source_system='ptyes' AND source_status=2),
    (SELECT count(*) FROM migration.user_medal_openings WHERE source_system='ptyes' AND source_expires_at IS NOT NULL AND source_expires_at <= $1),
	(SELECT count(*) FROM migration.medal_benefit_openings WHERE source_system='ptyes'),
	(SELECT count(*) FROM migration.medal_benefit_openings WHERE source_system='ptyes' AND magic_bonus_bps > 0),
	(SELECT COALESCE(max(magic_bonus_bps),0) FROM migration.medal_benefit_openings WHERE source_system='ptyes'),
	(SELECT count(*) FROM migration.workgroup_membership_openings WHERE source_system='ptyes'),
	(SELECT count(*) FROM migration.workgroup_membership_openings WHERE source_system='ptyes' AND group_kind='reseed'),
	(SELECT count(*) FROM migration.workgroup_membership_openings WHERE source_system='ptyes' AND group_kind='review'),
	(SELECT count(*) FROM migration.workgroup_membership_openings WHERE source_system='ptyes' AND group_kind='retention')`, config.OccurredAt).Scan(
		&result.Definitions, &result.UserMedals, &result.Wearing, &result.Expired,
		&result.BenefitUsers, &result.PositiveBenefitUsers, &result.MaximumMagicBonusBPS,
		&result.WorkgroupMemberships, &result.ReseedMemberships,
		&result.ReviewMemberships, &result.RetentionMemberships,
	); err != nil {
		return Result{}, fmt.Errorf("read legacy medal migration result: %w", err)
	}
	return result, nil
}
