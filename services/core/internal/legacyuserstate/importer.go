// Package legacyuserstate imports the finite, non-secret PtYes user opening
// state after Privacy Vault has established the stable user ID mapping.
package legacyuserstate

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/contracts/go/schemaversionv1"
	"github.com/peergo/peergo/services/core/internal/modules/economy"
)

const (
	// PtYes stored PT coin separately from karma. PeerGo deliberately has one
	// integer asset, so the audited site setting is frozen into this cutover.
	PTCoinToMagicRate int64 = 5000

	fingerprintDomain           = "peergo:migration:ptyes-user-operational:v1\x00"
	statusFingerprintDomain     = "peergo:migration:ptyes-user-status:v1\x00"
	attendanceFingerprintDomain = "peergo:migration:ptyes-user-attendance:v1\x00"
	levelPolicy                 = "rousi-v1"
)

var openingLedgerNamespace = uuid.MustParse("73c0c67c-6e63-5b8a-a191-df9e0eb82ad9")

type Config struct {
	RunID      uuid.UUID
	OccurredAt time.Time
}

type Progress struct {
	Processed int64
	Expected  int64
}

type Result struct {
	RunID                      uuid.UUID
	Users                      int64
	ImportedEvidence           int64
	ImportedStatusEvidence     int64
	ImportedAttendanceEvidence int64
	IntegerMagicTotal          string
	ExactMagicTotal            string
	RoundingDeltaTotal         string
	RawUploadedTotal           int64
	RawDownloadedTotal         int64
	AttendanceTotalDays        int64
	AttendanceRetroactiveCards int64
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
		return nil, errors.New("legacy user state importer configuration is invalid")
	}
	if progress == nil {
		progress = func(Progress) {}
	}
	return &Importer{source: source, core: core, config: config, progress: progress}, nil
}

// Run writes one immutable opening receipt per mapped account and then seeds
// Core's mutable read models. ON CONFLICT is used only for safe retries; every
// existing receipt is compared with the current snapshot before commit.
func (importer *Importer) Run(ctx context.Context) (Result, error) {
	if err := requireCoreMigration(ctx, importer.core); err != nil {
		return Result{}, err
	}
	if err := importer.requireSourceConversionRate(ctx); err != nil {
		return Result{}, err
	}

	var expected int64
	if err := importer.source.QueryRow(ctx, `SELECT count(*)::bigint FROM users`).Scan(&expected); err != nil {
		return Result{}, fmt.Errorf("count PtYes users: %w", err)
	}
	if expected < 1 {
		return Result{}, errors.New("PtYes source contains no users")
	}

	tx, err := importer.core.Begin(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("begin legacy user state import: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := importer.requireRunAndMappings(ctx, tx, expected); err != nil {
		return Result{}, err
	}
	if _, err := tx.Exec(ctx, `
CREATE TEMP TABLE legacy_user_operational_stage (
    legacy_id bigint PRIMARY KEY,
    uploaded bigint NOT NULL,
    downloaded bigint NOT NULL,
    karma_text text NOT NULL,
    pt_coin_text text NOT NULL,
    experience_text text NOT NULL,
    last_active_at timestamptz,
    source_fingerprint bytea NOT NULL,
    banned boolean NOT NULL,
    ban_reason text,
    banned_at timestamptz,
    banned_until timestamptz,
    download_restricted boolean NOT NULL,
    email_verified boolean NOT NULL,
    vip_enabled boolean NOT NULL,
    vip_until timestamptz,
    status_fingerprint bytea NOT NULL,
	attendance_stats_present boolean NOT NULL,
	attendance_current_streak bigint NOT NULL,
	attendance_longest_streak bigint NOT NULL,
	attendance_total_days bigint NOT NULL,
	attendance_retroactive_cards bigint NOT NULL,
	attendance_last_date date,
	attendance_stats_last_at timestamptz,
	attendance_record_days bigint NOT NULL,
	attendance_fingerprint bytea NOT NULL,
    ledger_id uuid NOT NULL
) ON COMMIT DROP`); err != nil {
		return Result{}, fmt.Errorf("create legacy user state stage: %w", err)
	}

	if err := importer.copySourceRows(ctx, tx, expected); err != nil {
		return Result{}, err
	}
	if err := importer.validateStage(ctx, tx, expected); err != nil {
		return Result{}, err
	}
	if err := importer.requireAttendanceOpeningOrder(ctx, tx); err != nil {
		return Result{}, err
	}

	commandTag, err := tx.Exec(ctx, `
INSERT INTO migration.user_operational_openings (
    source_system,
    legacy_user_id,
    user_id,
    source_uploaded,
    source_downloaded,
    source_karma,
    source_pt_coin,
    pt_coin_to_magic_rate,
    magic_balance,
    source_experience,
    source_last_active_at,
    source_fingerprint,
    first_run_id,
    imported_at
)
SELECT
    'ptyes',
    stage.legacy_id,
    mapping.user_id,
    stage.uploaded,
    stage.downloaded,
    stage.karma_text::numeric(38, 20),
    stage.pt_coin_text::numeric(38, 20),
    $1::bigint,
    round(
        stage.karma_text::numeric(38, 20)
        + stage.pt_coin_text::numeric(38, 20) * $1::numeric
    )::bigint,
    stage.experience_text::numeric(38, 20),
    stage.last_active_at,
    stage.source_fingerprint,
    $2,
    $3
FROM legacy_user_operational_stage AS stage
JOIN migration.user_id_map AS mapping
  ON mapping.source_system = 'ptyes'
 AND mapping.legacy_user_id = stage.legacy_id
ON CONFLICT (source_system, legacy_user_id) DO NOTHING`,
		PTCoinToMagicRate, importer.config.RunID, importer.config.OccurredAt,
	)
	if err != nil {
		return Result{}, fmt.Errorf("insert legacy user opening evidence: %w", err)
	}
	importedEvidence := commandTag.RowsAffected()

	statusCommandTag, err := tx.Exec(ctx, `
INSERT INTO migration.user_status_openings (
    source_system,
    legacy_user_id,
    user_id,
    source_banned,
    source_ban_reason,
    source_banned_at,
    source_banned_until,
    source_download_restricted,
    source_email_verified,
    source_vip_enabled,
    source_vip_until,
    source_fingerprint,
    first_run_id,
    imported_at
)
SELECT
    'ptyes',
    stage.legacy_id,
    mapping.user_id,
    stage.banned,
    stage.ban_reason,
    stage.banned_at,
    stage.banned_until,
    stage.download_restricted,
    stage.email_verified,
    stage.vip_enabled,
    stage.vip_until,
    stage.status_fingerprint,
    $1,
    $2
FROM legacy_user_operational_stage AS stage
JOIN migration.user_id_map AS mapping
  ON mapping.source_system = 'ptyes'
 AND mapping.legacy_user_id = stage.legacy_id
ON CONFLICT (source_system, legacy_user_id) DO NOTHING`,
		importer.config.RunID, importer.config.OccurredAt,
	)
	if err != nil {
		return Result{}, fmt.Errorf("insert legacy user status opening evidence: %w", err)
	}
	importedStatusEvidence := statusCommandTag.RowsAffected()

	attendanceCommandTag, err := tx.Exec(ctx, `
INSERT INTO migration.user_attendance_openings (
    source_system,
    legacy_user_id,
    user_id,
    source_stats_present,
    source_current_streak,
    source_longest_streak,
    source_total_days,
    source_retroactive_cards,
    source_last_attendance_date,
    source_stats_last_attendance_at,
    source_record_days,
    source_fingerprint,
    first_run_id,
    imported_at
)
SELECT
    'ptyes',
    stage.legacy_id,
    mapping.user_id,
    stage.attendance_stats_present,
    stage.attendance_current_streak::integer,
    stage.attendance_longest_streak::integer,
    stage.attendance_total_days::integer,
    stage.attendance_retroactive_cards::integer,
    stage.attendance_last_date,
    stage.attendance_stats_last_at,
    stage.attendance_record_days::integer,
    stage.attendance_fingerprint,
    $1,
    $2
FROM legacy_user_operational_stage AS stage
JOIN migration.user_id_map AS mapping
  ON mapping.source_system = 'ptyes'
 AND mapping.legacy_user_id = stage.legacy_id
ON CONFLICT (source_system, legacy_user_id) DO NOTHING`,
		importer.config.RunID, importer.config.OccurredAt,
	)
	if err != nil {
		return Result{}, fmt.Errorf("insert legacy attendance opening evidence: %w", err)
	}
	importedAttendanceEvidence := attendanceCommandTag.RowsAffected()

	if err := importer.verifyExistingEvidence(ctx, tx, expected); err != nil {
		return Result{}, err
	}
	if err := importer.verifyStatusEvidence(ctx, tx, expected); err != nil {
		return Result{}, err
	}
	if err := importer.verifyAttendanceEvidence(ctx, tx, expected); err != nil {
		return Result{}, err
	}
	if err := importer.seedTraffic(ctx, tx); err != nil {
		return Result{}, err
	}
	if err := importer.seedEconomy(ctx, tx); err != nil {
		return Result{}, err
	}
	if err := importer.seedProgressAndActivity(ctx, tx); err != nil {
		return Result{}, err
	}
	if err := importer.seedUserAccessStates(ctx, tx); err != nil {
		return Result{}, err
	}

	result, err := importer.readResult(
		ctx, tx, expected, importedEvidence, importedStatusEvidence, importedAttendanceEvidence,
	)
	if err != nil {
		return Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit legacy user state import: %w", err)
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

func (importer *Importer) requireSourceConversionRate(ctx context.Context) error {
	var raw string
	if err := importer.source.QueryRow(ctx, `
SELECT value
FROM site_settings
WHERE key = 'points.karma_to_credits'`).Scan(&raw); err != nil {
		return fmt.Errorf("read PtYes PT conversion setting: %w", err)
	}
	rate, ok := new(big.Rat).SetString(raw)
	if !ok || rate.Cmp(big.NewRat(PTCoinToMagicRate, 1)) != 0 {
		return fmt.Errorf("PtYes PT conversion rate is %q, want %d", raw, PTCoinToMagicRate)
	}
	return nil
}

func (importer *Importer) requireRunAndMappings(ctx context.Context, tx pgx.Tx, expected int64) error {
	var runCount, mappingCount, identityCount int64
	if err := tx.QueryRow(ctx, `
SELECT
    count(*) FILTER (
        WHERE run.id = $1
          AND run.source_system = 'ptyes'
          AND run.expected_user_rows = $2
    )::bigint,
    (SELECT count(*)::bigint
       FROM migration.user_id_map
      WHERE source_system = 'ptyes'),
    (SELECT count(*)::bigint
       FROM migration.user_id_map AS mapping
       JOIN identity.users AS user_account ON user_account.id = mapping.user_id
      WHERE mapping.source_system = 'ptyes')
FROM migration.runs AS run`, importer.config.RunID, expected).Scan(
		&runCount, &mappingCount, &identityCount,
	); err != nil {
		return fmt.Errorf("verify legacy user state prerequisites: %w", err)
	}
	if runCount != 1 || mappingCount != expected || identityCount != expected {
		return fmt.Errorf(
			"legacy user state prerequisites are incomplete: run=%d mappings=%d identities=%d expected=%d",
			runCount, mappingCount, identityCount, expected,
		)
	}
	return nil
}

func (importer *Importer) copySourceRows(ctx context.Context, tx pgx.Tx, expected int64) error {
	rows, err := importer.source.Query(ctx, `
SELECT
    user_account.id,
    user_account.uploaded,
    user_account.downloaded,
    COALESCE(user_account.karma, 0)::text,
    COALESCE(user_account.credits, 0)::text,
    COALESCE(user_account.experience, 0)::text,
    user_account.last_active_at,
    user_account.banned,
    NULLIF(btrim(user_account.ban_reason), ''),
    user_account.banned_at,
    user_account.banned_until,
    user_account.download_banned,
    user_account.email_verified,
    user_account.is_vip,
    user_account.vip_until,
    attendance.user_id IS NOT NULL,
    COALESCE(attendance.current_streak, 0),
    COALESCE(attendance.longest_streak, 0),
    COALESCE(attendance.total_days, 0),
    COALESCE(attendance.retroactive_cards, 0),
    attendance_history.last_date,
    attendance.last_attendance,
    COALESCE(attendance_history.record_days, 0)
FROM users AS user_account
LEFT JOIN user_attendance_stats AS attendance
  ON attendance.user_id = user_account.id
LEFT JOIN (
    SELECT
        user_id,
        count(*)::bigint AS record_days,
        max(date::date) AS last_date
    FROM attendance_records
    GROUP BY user_id
) AS attendance_history
  ON attendance_history.user_id = user_account.id
ORDER BY user_account.id`)
	if err != nil {
		return fmt.Errorf("query PtYes user operational state: %w", err)
	}
	defer rows.Close()

	var processed int64
	copyCount, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"legacy_user_operational_stage"},
		[]string{
			"legacy_id", "uploaded", "downloaded", "karma_text", "pt_coin_text",
			"experience_text", "last_active_at", "source_fingerprint",
			"banned", "ban_reason", "banned_at", "banned_until",
			"download_restricted", "email_verified", "vip_enabled", "vip_until",
			"status_fingerprint", "ledger_id",
			"attendance_stats_present", "attendance_current_streak",
			"attendance_longest_streak", "attendance_total_days",
			"attendance_retroactive_cards", "attendance_last_date",
			"attendance_stats_last_at", "attendance_record_days",
			"attendance_fingerprint",
		},
		pgx.CopyFromSlice(int(expected), func(_ int) ([]any, error) {
			if !rows.Next() {
				if rows.Err() != nil {
					return nil, rows.Err()
				}
				return nil, errors.New("PtYes user row count changed during import")
			}
			var row sourceRow
			if err := rows.Scan(
				&row.LegacyID,
				&row.Uploaded,
				&row.Downloaded,
				&row.Karma,
				&row.PTCoin,
				&row.Experience,
				&row.LastActiveAt,
				&row.Banned,
				&row.BanReason,
				&row.BannedAt,
				&row.BannedUntil,
				&row.DownloadRestricted,
				&row.EmailVerified,
				&row.VIPEnabled,
				&row.VIPUntil,
				&row.AttendanceStatsPresent,
				&row.AttendanceCurrentStreak,
				&row.AttendanceLongestStreak,
				&row.AttendanceTotalDays,
				&row.AttendanceRetroactiveCards,
				&row.AttendanceLastDate,
				&row.AttendanceStatsLastAt,
				&row.AttendanceRecordDays,
			); err != nil {
				return nil, fmt.Errorf("scan PtYes user operational row %d: %w", processed+1, err)
			}
			processed++
			if err := row.validate(processed); err != nil {
				return nil, err
			}
			fingerprint := row.fingerprint()
			statusFingerprint := row.statusFingerprint()
			attendanceFingerprint := row.attendanceFingerprint()
			ledgerID := uuid.NewSHA1(openingLedgerNamespace, []byte(strconv.FormatInt(row.LegacyID, 10)))
			if processed%250 == 0 || processed == expected {
				importer.progress(Progress{Processed: processed, Expected: expected})
			}
			return []any{
				row.LegacyID,
				row.Uploaded,
				row.Downloaded,
				row.Karma,
				row.PTCoin,
				row.Experience,
				row.LastActiveAt,
				fingerprint[:],
				row.Banned,
				row.BanReason,
				row.BannedAt,
				row.BannedUntil,
				row.DownloadRestricted,
				row.EmailVerified,
				row.VIPEnabled,
				row.VIPUntil,
				statusFingerprint[:],
				ledgerID,
				row.AttendanceStatsPresent,
				row.AttendanceCurrentStreak,
				row.AttendanceLongestStreak,
				row.AttendanceTotalDays,
				row.AttendanceRetroactiveCards,
				row.AttendanceLastDate,
				row.AttendanceStatsLastAt,
				row.AttendanceRecordDays,
				attendanceFingerprint[:],
			}, nil
		}),
	)
	if err != nil {
		return fmt.Errorf("stage PtYes user operational state: %w", err)
	}
	if copyCount != expected || processed != expected || rows.Next() {
		return fmt.Errorf("staged %d PtYes user rows, expected %d", copyCount, expected)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("finish reading PtYes user operational state: %w", err)
	}
	return nil
}

func (importer *Importer) validateStage(ctx context.Context, tx pgx.Tx, expected int64) error {
	var rowsCount, mappedCount int64
	if err := tx.QueryRow(ctx, `
SELECT
    count(*)::bigint,
    count(mapping.legacy_user_id)::bigint
FROM legacy_user_operational_stage AS stage
LEFT JOIN migration.user_id_map AS mapping
  ON mapping.source_system = 'ptyes'
 AND mapping.legacy_user_id = stage.legacy_id`,
	).Scan(&rowsCount, &mappedCount); err != nil {
		return fmt.Errorf("validate staged legacy user state: %w", err)
	}
	if rowsCount != expected || mappedCount != expected {
		return fmt.Errorf("legacy user state stage has rows=%d mapped=%d expected=%d", rowsCount, mappedCount, expected)
	}
	return nil
}

func (importer *Importer) requireAttendanceOpeningOrder(ctx context.Context, tx pgx.Tx) error {
	var unsafeRecords int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM economy.attendance_records AS record
JOIN migration.user_id_map AS mapping
  ON mapping.source_system = 'ptyes'
 AND mapping.user_id = record.user_id
LEFT JOIN migration.user_attendance_openings AS opening
  ON opening.source_system = mapping.source_system
 AND opening.legacy_user_id = mapping.legacy_user_id
WHERE opening.user_id IS NULL`).Scan(&unsafeRecords); err != nil {
		return fmt.Errorf("verify legacy attendance opening order: %w", err)
	}
	if unsafeRecords != 0 {
		return fmt.Errorf(
			"%d PeerGo attendance records exist before their legacy opening; restore into an unopened target",
			unsafeRecords,
		)
	}
	return nil
}

func (importer *Importer) verifyExistingEvidence(ctx context.Context, tx pgx.Tx, expected int64) error {
	var receipts, conflicts int64
	if err := tx.QueryRow(ctx, `
SELECT
    count(opening.legacy_user_id)::bigint,
    count(*) FILTER (WHERE
        opening.legacy_user_id IS NULL
        OR opening.user_id IS DISTINCT FROM mapping.user_id
        OR opening.source_uploaded IS DISTINCT FROM stage.uploaded
        OR opening.source_downloaded IS DISTINCT FROM stage.downloaded
        OR opening.source_karma IS DISTINCT FROM stage.karma_text::numeric(38, 20)
        OR opening.source_pt_coin IS DISTINCT FROM stage.pt_coin_text::numeric(38, 20)
        OR opening.pt_coin_to_magic_rate IS DISTINCT FROM $1
        OR opening.source_experience IS DISTINCT FROM stage.experience_text::numeric(38, 20)
        OR opening.source_last_active_at IS DISTINCT FROM stage.last_active_at
        OR opening.source_fingerprint IS DISTINCT FROM stage.source_fingerprint
    )::bigint
FROM legacy_user_operational_stage AS stage
JOIN migration.user_id_map AS mapping
  ON mapping.source_system = 'ptyes'
 AND mapping.legacy_user_id = stage.legacy_id
LEFT JOIN migration.user_operational_openings AS opening
  ON opening.source_system = 'ptyes'
 AND opening.legacy_user_id = stage.legacy_id`, PTCoinToMagicRate).Scan(&receipts, &conflicts); err != nil {
		return fmt.Errorf("verify legacy user opening evidence: %w", err)
	}
	if receipts != expected || conflicts != 0 {
		return fmt.Errorf("legacy user opening evidence conflicts: receipts=%d conflicts=%d expected=%d", receipts, conflicts, expected)
	}
	return nil
}

func (importer *Importer) verifyStatusEvidence(ctx context.Context, tx pgx.Tx, expected int64) error {
	var receipts, conflicts, identityConflicts int64
	if err := tx.QueryRow(ctx, `
SELECT
    count(opening.legacy_user_id)::bigint,
    count(*) FILTER (WHERE
        opening.legacy_user_id IS NULL
        OR opening.user_id IS DISTINCT FROM mapping.user_id
        OR opening.source_banned IS DISTINCT FROM stage.banned
        OR opening.source_ban_reason IS DISTINCT FROM stage.ban_reason
        OR opening.source_banned_at IS DISTINCT FROM stage.banned_at
        OR opening.source_banned_until IS DISTINCT FROM stage.banned_until
        OR opening.source_download_restricted IS DISTINCT FROM stage.download_restricted
        OR opening.source_email_verified IS DISTINCT FROM stage.email_verified
        OR opening.source_vip_enabled IS DISTINCT FROM stage.vip_enabled
        OR opening.source_vip_until IS DISTINCT FROM stage.vip_until
        OR opening.source_fingerprint IS DISTINCT FROM stage.status_fingerprint
    )::bigint,
    count(*) FILTER (WHERE
        user_account.status IS DISTINCT FROM CASE WHEN stage.banned THEN 'disabled' ELSE 'active' END
        OR (user_account.email_verified_at IS NOT NULL) IS DISTINCT FROM stage.email_verified
    )::bigint
FROM legacy_user_operational_stage AS stage
JOIN migration.user_id_map AS mapping
  ON mapping.source_system = 'ptyes'
 AND mapping.legacy_user_id = stage.legacy_id
JOIN identity.users AS user_account ON user_account.id = mapping.user_id
LEFT JOIN migration.user_status_openings AS opening
  ON opening.source_system = 'ptyes'
 AND opening.legacy_user_id = stage.legacy_id`).Scan(&receipts, &conflicts, &identityConflicts); err != nil {
		return fmt.Errorf("verify legacy user status evidence: %w", err)
	}
	if receipts != expected || conflicts != 0 || identityConflicts != 0 {
		return fmt.Errorf(
			"legacy user status evidence conflicts: receipts=%d conflicts=%d identities=%d expected=%d",
			receipts, conflicts, identityConflicts, expected,
		)
	}
	return nil
}

func (importer *Importer) verifyAttendanceEvidence(ctx context.Context, tx pgx.Tx, expected int64) error {
	var receipts, conflicts int64
	if err := tx.QueryRow(ctx, `
SELECT
    count(opening.legacy_user_id)::bigint,
    count(*) FILTER (WHERE
        opening.legacy_user_id IS NULL
        OR opening.user_id IS DISTINCT FROM mapping.user_id
        OR opening.source_stats_present IS DISTINCT FROM stage.attendance_stats_present
        OR opening.source_current_streak IS DISTINCT FROM stage.attendance_current_streak::integer
        OR opening.source_longest_streak IS DISTINCT FROM stage.attendance_longest_streak::integer
        OR opening.source_total_days IS DISTINCT FROM stage.attendance_total_days::integer
        OR opening.source_retroactive_cards IS DISTINCT FROM stage.attendance_retroactive_cards::integer
        OR opening.source_last_attendance_date IS DISTINCT FROM stage.attendance_last_date
        OR opening.source_stats_last_attendance_at IS DISTINCT FROM stage.attendance_stats_last_at
        OR opening.source_record_days IS DISTINCT FROM stage.attendance_record_days::integer
        OR opening.source_fingerprint IS DISTINCT FROM stage.attendance_fingerprint
    )::bigint
FROM legacy_user_operational_stage AS stage
JOIN migration.user_id_map AS mapping
  ON mapping.source_system = 'ptyes'
 AND mapping.legacy_user_id = stage.legacy_id
LEFT JOIN migration.user_attendance_openings AS opening
  ON opening.source_system = 'ptyes'
 AND opening.legacy_user_id = stage.legacy_id`).Scan(&receipts, &conflicts); err != nil {
		return fmt.Errorf("verify legacy attendance opening evidence: %w", err)
	}
	if receipts != expected || conflicts != 0 {
		return fmt.Errorf(
			"legacy attendance opening evidence conflicts: receipts=%d conflicts=%d expected=%d",
			receipts, conflicts, expected,
		)
	}
	return nil
}

func (importer *Importer) seedTraffic(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO traffic.user_opening_balances (
    user_id,
    raw_uploaded,
    raw_downloaded,
    credited_uploaded,
    charged_downloaded,
    source_run_id,
    source_fingerprint,
    occurred_at,
    imported_at
)
SELECT
    opening.user_id,
    opening.source_uploaded,
    opening.source_downloaded,
    opening.source_uploaded,
    opening.source_downloaded,
    opening.first_run_id,
    opening.source_fingerprint,
    $1,
    opening.imported_at
FROM migration.user_operational_openings AS opening
JOIN legacy_user_operational_stage AS stage
  ON stage.legacy_id = opening.legacy_user_id
WHERE opening.source_system = 'ptyes'
ON CONFLICT (user_id) DO NOTHING`, importer.config.OccurredAt); err != nil {
		return fmt.Errorf("insert legacy traffic openings: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO traffic.user_totals (
    user_id,
    raw_uploaded,
    raw_downloaded,
    credited_uploaded,
    charged_downloaded,
    entry_count,
    version,
    last_occurred_at,
    updated_at
)
SELECT
    opening.user_id,
    opening.raw_uploaded,
    opening.raw_downloaded,
    opening.credited_uploaded,
    opening.charged_downloaded,
    0,
    1,
    opening.occurred_at,
    opening.imported_at
FROM traffic.user_opening_balances AS opening
JOIN legacy_user_operational_stage AS stage
  ON stage.source_fingerprint = opening.source_fingerprint
ON CONFLICT (user_id) DO NOTHING`); err != nil {
		return fmt.Errorf("seed user traffic totals: %w", err)
	}
	var invalid int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM traffic.user_opening_balances AS opening
JOIN legacy_user_operational_stage AS stage
  ON stage.source_fingerprint = opening.source_fingerprint
JOIN traffic.user_totals AS total ON total.user_id = opening.user_id
WHERE total.raw_uploaded < opening.raw_uploaded
   OR total.raw_downloaded < opening.raw_downloaded
   OR total.credited_uploaded < opening.credited_uploaded
   OR total.charged_downloaded < opening.charged_downloaded`).Scan(&invalid); err != nil {
		return fmt.Errorf("verify seeded traffic totals: %w", err)
	}
	if invalid != 0 {
		return fmt.Errorf("%d traffic totals are below their immutable opening", invalid)
	}
	return nil
}

func (importer *Importer) seedEconomy(ctx context.Context, tx pgx.Tx) error {
	var unsafeAccounts int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM legacy_user_operational_stage AS stage
JOIN migration.user_id_map AS mapping
  ON mapping.source_system = 'ptyes'
 AND mapping.legacy_user_id = stage.legacy_id
JOIN economy.magic_accounts AS account ON account.user_id = mapping.user_id
LEFT JOIN economy.magic_ledger_entries AS ledger
  ON ledger.entry_type = 'legacy_opening'
 AND ledger.source_reference = 'ptyes:user:' || stage.legacy_id::text
WHERE ledger.id IS NULL`).Scan(&unsafeAccounts); err != nil {
		return fmt.Errorf("verify legacy magic account preconditions: %w", err)
	}
	if unsafeAccounts != 0 {
		return fmt.Errorf("%d mapped magic accounts exist without a legacy opening ledger", unsafeAccounts)
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO economy.magic_accounts (
    id, user_id, account_kind, account_code, balance, version, updated_at
)
SELECT
    opening.user_id,
    opening.user_id,
    'member',
    'member:' || opening.user_id::text,
    opening.magic_balance,
    1,
    opening.imported_at
FROM migration.user_operational_openings AS opening
JOIN legacy_user_operational_stage AS stage
  ON stage.legacy_id = opening.legacy_user_id
WHERE opening.source_system = 'ptyes'
ON CONFLICT (user_id) DO NOTHING`); err != nil {
		return fmt.Errorf("seed integer magic accounts: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO economy.magic_transactions (
    id,
    transaction_type,
    idempotency_key,
    source_reference,
    policy_revision,
    posting_count,
    payload_sha256,
    occurred_at,
    recorded_at
)
SELECT
    stage.ledger_id,
    'legacy_opening',
    'legacy-opening:ptyes:user:' || stage.legacy_id::text,
    'ptyes:user:' || stage.legacy_id::text,
    'rousi-cutover-v1',
    2,
    opening.source_fingerprint,
    $1,
    opening.imported_at
FROM legacy_user_operational_stage AS stage
JOIN migration.user_operational_openings AS opening
  ON opening.source_system = 'ptyes'
 AND opening.legacy_user_id = stage.legacy_id
ORDER BY stage.legacy_id
ON CONFLICT (idempotency_key) DO NOTHING`, importer.config.OccurredAt); err != nil {
		return fmt.Errorf("insert legacy magic opening transactions: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO economy.magic_ledger_entries (
    id,
    transaction_id,
    user_id,
    entry_type,
    amount,
    balance_after,
    source_reference,
    source_run_id,
    occurred_at,
    recorded_at
)
SELECT
    stage.ledger_id,
    stage.ledger_id,
    opening.user_id,
    'legacy_opening',
    opening.magic_balance,
    opening.magic_balance,
    'ptyes:user:' || stage.legacy_id::text,
    opening.first_run_id,
    $1,
    opening.imported_at
FROM legacy_user_operational_stage AS stage
JOIN migration.user_operational_openings AS opening
  ON opening.source_system = 'ptyes'
 AND opening.legacy_user_id = stage.legacy_id
ON CONFLICT (user_id, entry_type, source_reference) DO NOTHING`, importer.config.OccurredAt); err != nil {
		return fmt.Errorf("insert legacy magic opening ledger: %w", err)
	}
	if _, err := tx.Exec(ctx, `
WITH opening_postings AS (
    SELECT
        transaction.id AS transaction_id,
        transaction.ledger_sequence,
        ledger.user_id AS member_account_id,
        ledger.amount,
        ledger.balance_after AS member_balance_after,
        -sum(ledger.amount) OVER (
            ORDER BY transaction.ledger_sequence
            ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
        ) AS migration_balance_after
    FROM economy.magic_ledger_entries AS ledger
    JOIN economy.magic_transactions AS transaction
      ON transaction.id = ledger.transaction_id
    WHERE ledger.entry_type = 'legacy_opening'
)
INSERT INTO economy.magic_postings (
    transaction_id, ledger_sequence, posting_index,
    account_id, amount, balance_after
)
SELECT
    transaction_id,
    ledger_sequence,
    0,
    member_account_id,
    amount,
    member_balance_after
FROM opening_postings
UNION ALL
SELECT
    transaction_id,
    ledger_sequence,
    1,
    $1,
    -amount,
    migration_balance_after
FROM opening_postings
ON CONFLICT (transaction_id, posting_index) DO NOTHING`, economy.RousiMigrationAccountID()); err != nil {
		return fmt.Errorf("insert legacy magic opening postings: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE economy.magic_accounts
SET balance = (
        SELECT COALESCE(sum(posting.amount), 0)::bigint
        FROM economy.magic_postings AS posting
        WHERE posting.account_id = $1
    ),
    version = 1 + (
        SELECT count(*)
        FROM economy.magic_postings AS posting
        WHERE posting.account_id = $1
    ),
    updated_at = COALESCE((
        SELECT max(transaction.recorded_at)
        FROM economy.magic_transactions AS transaction
        JOIN economy.magic_postings AS posting
          ON posting.transaction_id = transaction.id
        WHERE posting.account_id = $1
    ), updated_at)
WHERE id = $1`, economy.RousiMigrationAccountID()); err != nil {
		return fmt.Errorf("project legacy magic migration account: %w", err)
	}

	var conflicts int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM legacy_user_operational_stage AS stage
JOIN migration.user_operational_openings AS opening
  ON opening.source_system = 'ptyes'
 AND opening.legacy_user_id = stage.legacy_id
LEFT JOIN economy.magic_ledger_entries AS ledger
  ON ledger.entry_type = 'legacy_opening'
 AND ledger.source_reference = 'ptyes:user:' || stage.legacy_id::text
LEFT JOIN economy.magic_transactions AS transaction
  ON transaction.id = stage.ledger_id
LEFT JOIN economy.magic_postings AS member_posting
  ON member_posting.transaction_id = stage.ledger_id
 AND member_posting.account_id = opening.user_id
LEFT JOIN economy.magic_postings AS system_posting
  ON system_posting.transaction_id = stage.ledger_id
 AND system_posting.account_id = $1
WHERE ledger.id IS NULL
   OR ledger.id IS DISTINCT FROM stage.ledger_id
   OR ledger.transaction_id IS DISTINCT FROM stage.ledger_id
   OR ledger.user_id IS DISTINCT FROM opening.user_id
   OR ledger.amount IS DISTINCT FROM opening.magic_balance
   OR ledger.balance_after IS DISTINCT FROM opening.magic_balance
   OR ledger.source_run_id IS DISTINCT FROM opening.first_run_id
   OR transaction.id IS NULL
   OR transaction.transaction_type IS DISTINCT FROM 'legacy_opening'
   OR transaction.idempotency_key IS DISTINCT FROM 'legacy-opening:ptyes:user:' || stage.legacy_id::text
   OR transaction.payload_sha256 IS DISTINCT FROM opening.source_fingerprint
   OR member_posting.amount IS DISTINCT FROM opening.magic_balance
   OR member_posting.balance_after IS DISTINCT FROM opening.magic_balance
   OR system_posting.amount IS DISTINCT FROM -opening.magic_balance`, economy.RousiMigrationAccountID()).Scan(&conflicts); err != nil {
		return fmt.Errorf("verify legacy magic opening ledger: %w", err)
	}
	if conflicts != 0 {
		return fmt.Errorf("%d legacy magic opening ledger entries conflict", conflicts)
	}
	return nil
}

func (importer *Importer) seedProgressAndActivity(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO progression.user_progress (
    user_id, experience, level, policy_version, version, updated_at
)
SELECT
    opening.user_id,
    opening.source_experience,
    definition.level,
    $1,
    1,
    opening.imported_at
FROM migration.user_operational_openings AS opening
JOIN legacy_user_operational_stage AS stage
  ON stage.legacy_id = opening.legacy_user_id
JOIN LATERAL (
    SELECT level
    FROM progression.level_definitions
    WHERE policy_version = $1
      AND minimum_experience <= opening.source_experience
    ORDER BY minimum_experience DESC
    LIMIT 1
) AS definition ON true
WHERE opening.source_system = 'ptyes'
ON CONFLICT (user_id) DO NOTHING`, levelPolicy); err != nil {
		return fmt.Errorf("seed legacy user progression: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO progression.experience_entries (
    id,
    idempotency_key,
    user_id,
    entry_type,
    amount,
    balance_after,
    source_reference,
    source_kind,
    policy_revision,
    level_policy_version,
    level_after,
    payload_sha256,
    magic_transaction_id,
    source_run_id,
    occurred_at,
    recorded_at
)
SELECT
    stage.ledger_id,
    'legacy-opening:ptyes:user:' || stage.legacy_id::text,
    opening.user_id,
    'legacy_opening',
    opening.source_experience,
    opening.source_experience,
    'ptyes:user:' || stage.legacy_id::text,
    'legacy_opening',
    'rousi-cutover-v1',
    definition.policy_version,
    definition.level,
    opening.source_fingerprint,
    stage.ledger_id,
    opening.first_run_id,
    $1,
    opening.imported_at
FROM migration.user_operational_openings AS opening
JOIN legacy_user_operational_stage AS stage
  ON stage.legacy_id = opening.legacy_user_id
JOIN progression.user_progress AS definition
  ON definition.user_id = opening.user_id
WHERE opening.source_system = 'ptyes'
ON CONFLICT (user_id, entry_type, source_reference) DO NOTHING`, importer.config.OccurredAt); err != nil {
		return fmt.Errorf("seed legacy experience opening entries: %w", err)
	}
	var progressionConflicts int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM migration.user_operational_openings AS opening
JOIN legacy_user_operational_stage AS stage
  ON stage.legacy_id = opening.legacy_user_id
LEFT JOIN progression.experience_entries AS entry
  ON entry.user_id = opening.user_id
 AND entry.entry_type = 'legacy_opening'
 AND entry.source_reference = 'ptyes:user:' || stage.legacy_id::text
LEFT JOIN progression.user_progress AS progress
  ON progress.user_id = opening.user_id
WHERE opening.source_system = 'ptyes'
  AND (
      entry.id IS NULL
      OR progress.user_id IS NULL
      OR entry.id IS DISTINCT FROM stage.ledger_id
      OR entry.idempotency_key IS DISTINCT FROM 'legacy-opening:ptyes:user:' || stage.legacy_id::text
      OR entry.amount IS DISTINCT FROM opening.source_experience
      OR entry.balance_after IS DISTINCT FROM opening.source_experience
      OR entry.source_kind IS DISTINCT FROM 'legacy_opening'
      OR entry.policy_revision IS DISTINCT FROM 'rousi-cutover-v1'
      OR entry.level_policy_version IS DISTINCT FROM progress.policy_version
      OR entry.level_after IS DISTINCT FROM progress.level
      OR entry.payload_sha256 IS DISTINCT FROM opening.source_fingerprint
      OR entry.magic_transaction_id IS DISTINCT FROM stage.ledger_id
      OR entry.source_run_id IS DISTINCT FROM opening.first_run_id
  )`).Scan(&progressionConflicts); err != nil {
		return fmt.Errorf("verify legacy experience opening entries: %w", err)
	}
	if progressionConflicts != 0 {
		return fmt.Errorf("%d legacy experience opening entries conflict", progressionConflicts)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.user_activity (user_id, last_active_at, version, updated_at)
SELECT opening.user_id, opening.source_last_active_at, 1, opening.imported_at
FROM migration.user_operational_openings AS opening
JOIN legacy_user_operational_stage AS stage
  ON stage.legacy_id = opening.legacy_user_id
WHERE opening.source_system = 'ptyes'
ON CONFLICT (user_id) DO NOTHING`); err != nil {
		return fmt.Errorf("seed legacy user activity: %w", err)
	}
	return nil
}

func (importer *Importer) seedUserAccessStates(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.user_access_states (
    user_id,
    download_restricted,
    download_restriction_origin,
    download_restriction_reason_code,
    download_restriction_reason,
    download_restriction_started_at,
    vip_enabled,
    vip_until,
    source_run_id,
    source_fingerprint,
    version,
    updated_at
)
SELECT
    opening.user_id,
    opening.source_download_restricted,
    CASE WHEN opening.source_download_restricted
        THEN 'legacy_migration' END,
    CASE WHEN opening.source_download_restricted
        THEN 'legacy_download_restriction' END,
    CASE WHEN opening.source_download_restricted
        THEN '该下载限制从旧站当前账户状态迁入，需要由用户管理员单独复核。' END,
    CASE WHEN opening.source_download_restricted
        THEN opening.imported_at END,
    opening.source_vip_enabled,
    opening.source_vip_until,
    opening.first_run_id,
    opening.source_fingerprint,
    1,
    opening.imported_at
FROM migration.user_status_openings AS opening
JOIN legacy_user_operational_stage AS stage
  ON stage.legacy_id = opening.legacy_user_id
WHERE opening.source_system = 'ptyes'
ON CONFLICT (user_id) DO NOTHING`); err != nil {
		return fmt.Errorf("seed legacy user access states: %w", err)
	}

	// The current access-state boolean and the operator-visible transition are
	// one migration fact. Seed both in this transaction so a restricted legacy
	// account can never exist without its typed origin and review evidence.
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.manual_download_restriction_transitions (
    user_id, transition, origin, reason_code, reason,
    from_restricted, to_restricted,
    from_state_version, state_version, occurred_at
)
SELECT
    opening.user_id,
    'restricted',
    'legacy_migration',
    'legacy_download_restriction',
    '该下载限制从旧站当前账户状态迁入，需要由用户管理员单独复核。',
    false,
    true,
    0,
    1,
    opening.imported_at
FROM migration.user_status_openings AS opening
JOIN legacy_user_operational_stage AS stage
  ON stage.legacy_id = opening.legacy_user_id
WHERE opening.source_system = 'ptyes'
  AND opening.source_download_restricted
ON CONFLICT (user_id, state_version) DO NOTHING`); err != nil {
		return fmt.Errorf("seed legacy download restriction transitions: %w", err)
	}

	var conflicts int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM migration.user_status_openings AS opening
JOIN legacy_user_operational_stage AS stage
  ON stage.legacy_id = opening.legacy_user_id
LEFT JOIN identity.user_access_states AS access ON access.user_id = opening.user_id
WHERE opening.source_system = 'ptyes'
  AND (
      access.user_id IS NULL
      OR access.download_restricted IS DISTINCT FROM opening.source_download_restricted
      OR access.download_restriction_origin IS DISTINCT FROM
            CASE WHEN opening.source_download_restricted THEN 'legacy_migration' END
      OR access.download_restriction_reason_code IS DISTINCT FROM
            CASE WHEN opening.source_download_restricted THEN 'legacy_download_restriction' END
      OR access.download_restriction_reason IS DISTINCT FROM
            CASE WHEN opening.source_download_restricted
                THEN '该下载限制从旧站当前账户状态迁入，需要由用户管理员单独复核。' END
      OR access.download_restriction_started_at IS DISTINCT FROM
            CASE WHEN opening.source_download_restricted THEN opening.imported_at END
      OR access.vip_enabled IS DISTINCT FROM opening.source_vip_enabled
      OR access.vip_until IS DISTINCT FROM opening.source_vip_until
      OR access.source_run_id IS DISTINCT FROM opening.first_run_id
      OR access.source_fingerprint IS DISTINCT FROM opening.source_fingerprint
  )`).Scan(&conflicts); err != nil {
		return fmt.Errorf("verify legacy user access states: %w", err)
	}
	if conflicts != 0 {
		return fmt.Errorf("%d legacy user access states conflict with their immutable opening", conflicts)
	}
	var transitionConflicts int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM migration.user_status_openings AS opening
JOIN legacy_user_operational_stage AS stage
  ON stage.legacy_id = opening.legacy_user_id
LEFT JOIN identity.manual_download_restriction_transitions AS transition
  ON transition.user_id = opening.user_id
 AND transition.state_version = 1
WHERE opening.source_system = 'ptyes'
  AND opening.source_download_restricted
  AND (
      transition.id IS NULL
      OR transition.transition IS DISTINCT FROM 'restricted'
      OR transition.origin IS DISTINCT FROM 'legacy_migration'
      OR transition.reason_code IS DISTINCT FROM 'legacy_download_restriction'
      OR transition.reason IS DISTINCT FROM '该下载限制从旧站当前账户状态迁入，需要由用户管理员单独复核。'
      OR transition.from_restricted
      OR NOT transition.to_restricted
      OR transition.from_state_version IS DISTINCT FROM 0
      OR transition.occurred_at IS DISTINCT FROM opening.imported_at
  )`).Scan(&transitionConflicts); err != nil {
		return fmt.Errorf("verify legacy download restriction transitions: %w", err)
	}
	if transitionConflicts != 0 {
		return fmt.Errorf("%d legacy download restriction transitions conflict with their immutable opening", transitionConflicts)
	}
	return nil
}

func (importer *Importer) readResult(
	ctx context.Context,
	tx pgx.Tx,
	expected, importedEvidence, importedStatusEvidence, importedAttendanceEvidence int64,
) (Result, error) {
	result := Result{
		RunID: importer.config.RunID, Users: expected,
		ImportedEvidence: importedEvidence, ImportedStatusEvidence: importedStatusEvidence,
		ImportedAttendanceEvidence: importedAttendanceEvidence,
	}
	if err := tx.QueryRow(ctx, `
SELECT
    COALESCE(sum(opening.magic_balance), 0)::text,
    COALESCE(sum(opening.exact_magic), 0)::text,
    COALESCE(sum(opening.rounding_delta), 0)::text,
    COALESCE(sum(opening.source_uploaded), 0)::bigint,
    COALESCE(sum(opening.source_downloaded), 0)::bigint
FROM migration.user_operational_openings AS opening
JOIN legacy_user_operational_stage AS stage
  ON stage.legacy_id = opening.legacy_user_id
WHERE opening.source_system = 'ptyes'`).Scan(
		&result.IntegerMagicTotal,
		&result.ExactMagicTotal,
		&result.RoundingDeltaTotal,
		&result.RawUploadedTotal,
		&result.RawDownloadedTotal,
	); err != nil {
		return Result{}, fmt.Errorf("read legacy user state totals: %w", err)
	}
	if err := tx.QueryRow(ctx, `
SELECT
    COALESCE(sum(opening.source_total_days), 0)::bigint,
    COALESCE(sum(opening.source_retroactive_cards), 0)::bigint
FROM migration.user_attendance_openings AS opening
JOIN legacy_user_operational_stage AS stage
  ON stage.legacy_id = opening.legacy_user_id
WHERE opening.source_system = 'ptyes'`).Scan(
		&result.AttendanceTotalDays,
		&result.AttendanceRetroactiveCards,
	); err != nil {
		return Result{}, fmt.Errorf("read legacy attendance opening totals: %w", err)
	}
	return result, nil
}

type sourceRow struct {
	LegacyID                   int64
	Uploaded                   int64
	Downloaded                 int64
	Karma                      string
	PTCoin                     string
	Experience                 string
	LastActiveAt               pgtype.Timestamptz
	Banned                     bool
	BanReason                  pgtype.Text
	BannedAt                   pgtype.Timestamptz
	BannedUntil                pgtype.Timestamptz
	DownloadRestricted         bool
	EmailVerified              bool
	VIPEnabled                 bool
	VIPUntil                   pgtype.Timestamptz
	AttendanceStatsPresent     bool
	AttendanceCurrentStreak    int64
	AttendanceLongestStreak    int64
	AttendanceTotalDays        int64
	AttendanceRetroactiveCards int64
	AttendanceLastDate         pgtype.Date
	AttendanceStatsLastAt      pgtype.Timestamptz
	AttendanceRecordDays       int64
}

func (row sourceRow) validate(expectedID int64) error {
	if row.LegacyID != expectedID || row.Uploaded < 0 || row.Downloaded < 0 ||
		!validDecimal(row.Karma, true) || !validDecimal(row.PTCoin, true) ||
		!validDecimal(row.Experience, false) || (row.Banned && !row.BannedAt.Valid) ||
		!row.validAttendance() {
		return fmt.Errorf("legacy user %d operational state is invalid", row.LegacyID)
	}
	return nil
}

func (row sourceRow) validAttendance() bool {
	if row.AttendanceCurrentStreak < 0 || row.AttendanceLongestStreak < row.AttendanceCurrentStreak ||
		row.AttendanceTotalDays < row.AttendanceLongestStreak || row.AttendanceTotalDays > 1_000_000 ||
		row.AttendanceRetroactiveCards < 0 || row.AttendanceRetroactiveCards > 1_000_000 ||
		row.AttendanceRecordDays < 0 || row.AttendanceRecordDays > 1_000_000 {
		return false
	}
	if !row.AttendanceStatsPresent && (row.AttendanceCurrentStreak != 0 ||
		row.AttendanceLongestStreak != 0 || row.AttendanceTotalDays != 0 ||
		row.AttendanceRetroactiveCards != 0 || row.AttendanceStatsLastAt.Valid) {
		return false
	}
	return (row.AttendanceTotalDays == 0) == !row.AttendanceLastDate.Valid &&
		(row.AttendanceRecordDays == 0) == !row.AttendanceLastDate.Valid
}

func validDecimal(raw string, allowNegative bool) bool {
	value, ok := new(big.Rat).SetString(raw)
	if !ok {
		return false
	}
	return allowNegative || value.Sign() >= 0
}

func (row sourceRow) fingerprint() [sha256.Size]byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte(fingerprintDomain))
	writeInt64(digest, row.LegacyID)
	writeInt64(digest, row.Uploaded)
	writeInt64(digest, row.Downloaded)
	writeString(digest, row.Karma)
	writeString(digest, row.PTCoin)
	writeString(digest, row.Experience)
	if row.LastActiveAt.Valid {
		_, _ = digest.Write([]byte{1})
		writeString(digest, row.LastActiveAt.Time.UTC().Format(time.RFC3339Nano))
	} else {
		_, _ = digest.Write([]byte{0})
	}
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

func (row sourceRow) statusFingerprint() [sha256.Size]byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte(statusFingerprintDomain))
	writeInt64(digest, row.LegacyID)
	writeBool(digest, row.Banned)
	writeOptionalText(digest, row.BanReason)
	writeOptionalTimestamp(digest, row.BannedAt)
	writeOptionalTimestamp(digest, row.BannedUntil)
	writeBool(digest, row.DownloadRestricted)
	writeBool(digest, row.EmailVerified)
	writeBool(digest, row.VIPEnabled)
	writeOptionalTimestamp(digest, row.VIPUntil)
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

func (row sourceRow) attendanceFingerprint() [sha256.Size]byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte(attendanceFingerprintDomain))
	writeInt64(digest, row.LegacyID)
	writeBool(digest, row.AttendanceStatsPresent)
	writeInt64(digest, row.AttendanceCurrentStreak)
	writeInt64(digest, row.AttendanceLongestStreak)
	writeInt64(digest, row.AttendanceTotalDays)
	writeInt64(digest, row.AttendanceRetroactiveCards)
	writeOptionalDate(digest, row.AttendanceLastDate)
	writeOptionalTimestamp(digest, row.AttendanceStatsLastAt)
	writeInt64(digest, row.AttendanceRecordDays)
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

type byteWriter interface {
	Write([]byte) (int, error)
}

func writeInt64(writer byteWriter, value int64) {
	var buffer [8]byte
	binary.BigEndian.PutUint64(buffer[:], uint64(value))
	_, _ = writer.Write(buffer[:])
}

func writeString(writer byteWriter, value string) {
	writeInt64(writer, int64(len(value)))
	_, _ = writer.Write([]byte(value))
}

func writeBool(writer byteWriter, value bool) {
	if value {
		_, _ = writer.Write([]byte{1})
		return
	}
	_, _ = writer.Write([]byte{0})
}

func writeOptionalText(writer byteWriter, value pgtype.Text) {
	writeBool(writer, value.Valid)
	if value.Valid {
		writeString(writer, value.String)
	}
}

func writeOptionalTimestamp(writer byteWriter, value pgtype.Timestamptz) {
	writeBool(writer, value.Valid)
	if value.Valid {
		writeString(writer, value.Time.UTC().Format(time.RFC3339Nano))
	}
}

func writeOptionalDate(writer byteWriter, value pgtype.Date) {
	writeBool(writer, value.Valid)
	if value.Valid {
		writeString(writer, value.Time.Format(time.DateOnly))
	}
}
