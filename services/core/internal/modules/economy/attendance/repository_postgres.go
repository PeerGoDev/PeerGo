package attendance

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/services/core/internal/modules/economy"
	"github.com/peergo/peergo/services/core/internal/modules/progression"
)

const policyColumns = `
    revision, effective_from, enabled, day_boundary_timezone,
    fixed_enabled, fixed_reward, random_enabled, random_min, random_max,
    streak_enabled, experience_reward, snapshot_json, snapshot_sha256,
    issued_by, authorization_decision_id, reason, created_at`

var (
	recordNamespace      = uuid.MustParse("4c876ae6-cb05-5a30-9051-5123a2659841")
	transactionNamespace = uuid.MustParse("8d871ac4-e57e-5f08-b858-af70e5e588e6")
	experienceNamespace  = uuid.MustParse("ef0a56ac-4003-58ca-8ace-d8a5059026af")
)

type PostgresRepository struct {
	pool        *pgxpool.Pool
	economy     *economy.PostgresRepository
	progression *progression.PostgresRepository
	randomInt   func(int64, int64) (int64, error)
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
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
	return &PostgresRepository{
		pool: pool, economy: economyRepository, progression: progressionRepository,
		randomInt: secureRandomInt,
	}, nil
}

func (repository *PostgresRepository) Overview(ctx context.Context, userID uuid.UUID, now time.Time, limit int) (Overview, error) {
	now = canonicalTime(now)
	if userID == uuid.Nil || now.IsZero() || limit < 1 || limit > 100 {
		return Overview{}, ErrInput
	}
	policy, found, err := resolvePolicy(ctx, repository.pool, now)
	if err != nil {
		return Overview{}, err
	}
	result := Overview{History: make([]Record, 0, limit)}
	if !found {
		return result, nil
	}
	result.Policy = &policy
	location, err := time.LoadLocation(policy.Policy.DayBoundaryTimezone)
	if err != nil {
		return Overview{}, ErrInvariant
	}
	today := now.In(location).Format(time.DateOnly)
	result.Today = today
	opening, openingFound, err := readAttendanceOpening(ctx, repository.pool, userID)
	if err != nil {
		return Overview{}, err
	}
	rows, err := repository.pool.Query(ctx, `
SELECT
    id, request_id, user_id, attendance_date::text, day_boundary_timezone,
    mode, base_reward, streak_reward, total_reward, experience_reward,
    current_streak, total_days, longest_streak, policy_revision,
    payload_sha256, magic_transaction_id, experience_entry_id,
    occurred_at, recorded_at
FROM economy.attendance_records
WHERE user_id = $1
ORDER BY attendance_date DESC, recorded_at DESC
LIMIT $2`, userID, limit)
	if err != nil {
		return Overview{}, fmt.Errorf("list attendance history: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		record, err := scanRecord(rows)
		if err != nil {
			return Overview{}, err
		}
		result.History = append(result.History, record)
	}
	if err := rows.Err(); err != nil {
		return Overview{}, fmt.Errorf("finish attendance history: %w", err)
	}
	if len(result.History) == 0 {
		if openingFound {
			if err := applyAttendanceOpening(&result, opening, today); err != nil {
				return Overview{}, err
			}
		}
		return result, nil
	}
	latest := result.History[0]
	result.TotalDays = latest.TotalDays
	result.LongestStreak = latest.LongestStreak
	if latest.AttendanceDate == today {
		result.ClaimedToday = true
		result.CurrentStreak = latest.CurrentStreak
		record := latest
		result.TodayRecord = &record
		return result, nil
	}
	yesterday := now.In(location).AddDate(0, 0, -1).Format(time.DateOnly)
	if latest.AttendanceDate == yesterday {
		result.CurrentStreak = latest.CurrentStreak
	}
	return result, nil
}

func (repository *PostgresRepository) Claim(ctx context.Context, command ClaimCommand) (Record, error) {
	command.Now = canonicalTime(command.Now)
	if command.RequestID == uuid.Nil || command.UserID == uuid.Nil || command.Now.IsZero() ||
		(command.Mode != ModeFixed && command.Mode != ModeRandom) {
		return Record{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Record{}, fmt.Errorf("begin attendance claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// A per-member lock keeps the replay check, local-day uniqueness check and
	// streak calculation in one critical section without blocking other users.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "peergo-attendance:"+command.UserID.String()); err != nil {
		return Record{}, fmt.Errorf("lock attendance member: %w", err)
	}
	if replay, found, err := readRecord(ctx, tx, "WHERE request_id = $1", command.RequestID); found || err != nil {
		if err != nil {
			return Record{}, err
		}
		if replay.UserID != command.UserID || replay.Mode != command.Mode {
			return Record{}, ErrIdempotencyConflict
		}
		replay.Replayed = true
		if err := tx.Commit(ctx); err != nil {
			return Record{}, classifyDatabaseError("commit attendance replay", err)
		}
		return replay, nil
	}

	policy, found, err := resolvePolicy(ctx, tx, command.Now)
	if err != nil {
		return Record{}, err
	}
	if !found {
		return Record{}, ErrPolicyNotFound
	}
	if !policy.Policy.Enabled {
		return Record{}, ErrDisabled
	}
	if (command.Mode == ModeFixed && !policy.Policy.FixedEnabled) ||
		(command.Mode == ModeRandom && !policy.Policy.RandomEnabled) {
		return Record{}, ErrModeDisabled
	}
	location, err := time.LoadLocation(policy.Policy.DayBoundaryTimezone)
	if err != nil {
		return Record{}, ErrInvariant
	}
	attendanceDate := command.Now.In(location).Format(time.DateOnly)
	if _, found, err := readRecord(ctx, tx, "WHERE user_id = $1 AND attendance_date = $2::date", command.UserID, attendanceDate); err != nil {
		return Record{}, err
	} else if found {
		return Record{}, ErrAlreadyClaimed
	}
	// A same-day claim made on PtYes is already included in the migrated magic
	// and experience opening.  It must block a second PeerGo reward even though
	// no synthetic live-ledger receipt is created for the historical claim.
	if opening, found, err := readAttendanceOpening(ctx, tx, command.UserID); err != nil {
		return Record{}, err
	} else if found && opening.LastAttendanceDate == attendanceDate {
		return Record{}, ErrAlreadyClaimed
	}

	currentStreak, totalDays, longestStreak, err := nextStatistics(ctx, tx, command.UserID, attendanceDate)
	if err != nil {
		return Record{}, err
	}
	baseReward := policy.Policy.FixedReward
	if command.Mode == ModeRandom {
		baseReward, err = repository.randomInt(policy.Policy.RandomMin, policy.Policy.RandomMax)
		if err != nil {
			return Record{}, fmt.Errorf("draw attendance reward: %w", err)
		}
	}
	streakReward := int64(0)
	if policy.Policy.StreakEnabled {
		for _, milestone := range policy.Policy.StreakMilestones {
			if milestone.Days == currentStreak {
				streakReward = milestone.Reward
				break
			}
		}
	}
	totalReward := baseReward + streakReward
	recordID := uuid.NewSHA1(recordNamespace, []byte(command.RequestID.String()))
	transactionID := uuid.NewSHA1(transactionNamespace, []byte(command.RequestID.String()))
	experienceEntryID := uuid.NewSHA1(experienceNamespace, []byte(command.RequestID.String()))
	sourceReference := "attendance:" + command.UserID.String() + ":" + attendanceDate
	payload := recordPayload{
		RecordID: recordID.String(), RequestID: command.RequestID.String(), UserID: command.UserID.String(),
		AttendanceDate: attendanceDate, DayBoundaryTimezone: policy.Policy.DayBoundaryTimezone,
		Mode: string(command.Mode), BaseReward: baseReward, StreakReward: streakReward,
		TotalReward: totalReward, ExperienceReward: policy.Policy.ExperienceReward,
		CurrentStreak: currentStreak, TotalDays: totalDays, LongestStreak: longestStreak,
		PolicyRevision: policy.Policy.Revision, OccurredAt: command.Now.Format(time.RFC3339Nano),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Record{}, ErrInvariant
	}
	payloadSHA256 := sha256.Sum256(encoded)
	magicTransaction, err := repository.economy.RecordInTransaction(ctx, tx, economy.RecordCommand{
		TransactionID: transactionID, TransactionType: economy.TransactionActivityReward,
		IdempotencyKey: "attendance:" + command.RequestID.String(), SourceReference: sourceReference,
		PolicyRevision: policy.Policy.Revision, PayloadSHA256: payloadSHA256,
		OccurredAt: command.Now, RecordedAt: command.Now,
		Postings: []economy.PostingInput{
			{AccountID: command.UserID, Amount: totalReward},
			{AccountID: economy.ActivityMintAccountID(), Amount: -totalReward},
		},
	})
	if err != nil {
		return Record{}, fmt.Errorf("record attendance magic: %w", err)
	}
	var experienceID *uuid.UUID
	if policy.Policy.ExperienceReward > 0 {
		amount, err := progression.ParseAmount(strconv.FormatInt(policy.Policy.ExperienceReward, 10))
		if err != nil {
			return Record{}, ErrInvariant
		}
		levelPolicyVersion, err := currentLevelPolicy(ctx, tx, command.UserID)
		if err != nil {
			return Record{}, err
		}
		entry, err := repository.progression.RecordInTransaction(ctx, tx, progression.RecordCommand{
			EntryID: experienceEntryID, IdempotencyKey: "attendance:" + command.RequestID.String(),
			UserID: command.UserID, EntryType: progression.EntryEarn, Amount: amount,
			SourceReference: sourceReference, SourceKind: progression.SourceActivity,
			PolicyRevision: policy.Policy.Revision, LevelPolicyVersion: levelPolicyVersion,
			PayloadSHA256: payloadSHA256, MagicTransactionID: magicTransaction.ID,
			OccurredAt: command.Now, RecordedAt: command.Now,
		})
		if err != nil {
			return Record{}, fmt.Errorf("record attendance experience: %w", err)
		}
		experienceID = &entry.ID
	}
	result := Record{
		ID: recordID, RequestID: command.RequestID, UserID: command.UserID,
		AttendanceDate: attendanceDate, DayBoundaryTimezone: policy.Policy.DayBoundaryTimezone,
		Mode: command.Mode, BaseReward: baseReward, StreakReward: streakReward,
		TotalReward: totalReward, ExperienceReward: policy.Policy.ExperienceReward,
		CurrentStreak: currentStreak, TotalDays: totalDays, LongestStreak: longestStreak,
		PolicyRevision: policy.Policy.Revision, PayloadSHA256: payloadSHA256,
		MagicTransactionID: magicTransaction.ID, ExperienceEntryID: experienceID,
		OccurredAt: command.Now, RecordedAt: command.Now,
	}
	_, err = tx.Exec(ctx, `
INSERT INTO economy.attendance_records (
    id, request_id, user_id, attendance_date, day_boundary_timezone,
    mode, base_reward, streak_reward, total_reward, experience_reward,
    current_streak, total_days, longest_streak, policy_revision,
    payload_sha256, magic_transaction_id, experience_entry_id,
    occurred_at, recorded_at
) VALUES (
    $1, $2, $3, $4::date, $5, $6, $7, $8, $9, $10,
    $11, $12, $13, $14, $15, $16, $17, $18, $19
)`, result.ID, result.RequestID, result.UserID, result.AttendanceDate, result.DayBoundaryTimezone,
		string(result.Mode), result.BaseReward, result.StreakReward, result.TotalReward, result.ExperienceReward,
		result.CurrentStreak, result.TotalDays, result.LongestStreak, result.PolicyRevision,
		result.PayloadSHA256[:], result.MagicTransactionID, result.ExperienceEntryID,
		result.OccurredAt, result.RecordedAt)
	if err != nil {
		return Record{}, classifyDatabaseError("insert attendance receipt", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Record{}, classifyDatabaseError("commit attendance claim", err)
	}
	return result, nil
}

type recordPayload struct {
	RecordID            string `json:"record_id"`
	RequestID           string `json:"request_id"`
	UserID              string `json:"user_id"`
	AttendanceDate      string `json:"attendance_date"`
	DayBoundaryTimezone string `json:"day_boundary_timezone"`
	Mode                string `json:"mode"`
	BaseReward          int64  `json:"base_reward"`
	StreakReward        int64  `json:"streak_reward"`
	TotalReward         int64  `json:"total_reward"`
	ExperienceReward    int64  `json:"experience_reward"`
	CurrentStreak       int32  `json:"current_streak"`
	TotalDays           int32  `json:"total_days"`
	LongestStreak       int32  `json:"longest_streak"`
	PolicyRevision      string `json:"policy_revision"`
	OccurredAt          string `json:"occurred_at"`
}

func nextStatistics(ctx context.Context, tx pgx.Tx, userID uuid.UUID, attendanceDate string) (int32, int32, int32, error) {
	var previousDate string
	var previousStreak, totalDays, longestStreak int32
	err := tx.QueryRow(ctx, `
SELECT attendance_date::text, current_streak, total_days, longest_streak
FROM economy.attendance_records
WHERE user_id = $1
ORDER BY attendance_date DESC
LIMIT 1`, userID).Scan(&previousDate, &previousStreak, &totalDays, &longestStreak)
	if errors.Is(err, pgx.ErrNoRows) {
		opening, found, err := readAttendanceOpening(ctx, tx, userID)
		if err != nil {
			return 0, 0, 0, err
		}
		if !found {
			return 1, 1, 1, nil
		}
		return statisticsAfterOpening(opening, attendanceDate)
	}
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read previous attendance: %w", err)
	}
	date, err := time.Parse(time.DateOnly, attendanceDate)
	if err != nil {
		return 0, 0, 0, ErrInvariant
	}
	previous, err := time.Parse(time.DateOnly, previousDate)
	if err != nil {
		return 0, 0, 0, ErrInvariant
	}
	currentStreak := int32(1)
	if previous.Equal(date.AddDate(0, 0, -1)) {
		currentStreak = previousStreak + 1
	}
	totalDays++
	if currentStreak > longestStreak {
		longestStreak = currentStreak
	}
	return currentStreak, totalDays, longestStreak, nil
}

type attendanceOpening struct {
	CurrentStreak      int32
	TotalDays          int32
	LongestStreak      int32
	LastAttendanceDate string
}

func readAttendanceOpening(
	ctx context.Context,
	querier queryRower,
	userID uuid.UUID,
) (attendanceOpening, bool, error) {
	var result attendanceOpening
	var lastDate pgtype.Text
	err := querier.QueryRow(ctx, `
SELECT
    source_current_streak,
    source_total_days,
    source_longest_streak,
    source_last_attendance_date::text
FROM migration.user_attendance_openings
WHERE user_id = $1`, userID).Scan(
		&result.CurrentStreak,
		&result.TotalDays,
		&result.LongestStreak,
		&lastDate,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return attendanceOpening{}, false, nil
	}
	if err != nil {
		return attendanceOpening{}, false, fmt.Errorf("read legacy attendance opening: %w", err)
	}
	if lastDate.Valid {
		result.LastAttendanceDate = lastDate.String
	}
	return result, true, nil
}

func applyAttendanceOpening(
	overview *Overview,
	opening attendanceOpening,
	today string,
) error {
	if overview == nil {
		return ErrInvariant
	}
	overview.TotalDays = opening.TotalDays
	overview.LongestStreak = opening.LongestStreak
	if opening.LastAttendanceDate == "" {
		return nil
	}
	date, err := time.Parse(time.DateOnly, today)
	if err != nil {
		return ErrInvariant
	}
	if opening.LastAttendanceDate == today {
		overview.ClaimedToday = true
		overview.CurrentStreak = opening.CurrentStreak
		return nil
	}
	if opening.LastAttendanceDate == date.AddDate(0, 0, -1).Format(time.DateOnly) {
		overview.CurrentStreak = opening.CurrentStreak
	}
	return nil
}

func statisticsAfterOpening(
	opening attendanceOpening,
	attendanceDate string,
) (int32, int32, int32, error) {
	date, err := time.Parse(time.DateOnly, attendanceDate)
	if err != nil {
		return 0, 0, 0, ErrInvariant
	}
	currentStreak := int32(1)
	if opening.LastAttendanceDate != "" {
		previous, err := time.Parse(time.DateOnly, opening.LastAttendanceDate)
		if err != nil {
			return 0, 0, 0, ErrInvariant
		}
		if previous.Equal(date.AddDate(0, 0, -1)) {
			currentStreak = opening.CurrentStreak + 1
		}
	}
	totalDays := opening.TotalDays + 1
	longestStreak := opening.LongestStreak
	if currentStreak > longestStreak {
		longestStreak = currentStreak
	}
	return currentStreak, totalDays, longestStreak, nil
}

func currentLevelPolicy(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (string, error) {
	var policyVersion string
	err := tx.QueryRow(ctx, `SELECT policy_version FROM progression.user_progress WHERE user_id = $1`, userID).Scan(&policyVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return "rousi-v1", nil
	}
	if err != nil {
		return "", fmt.Errorf("read attendance level policy: %w", err)
	}
	return policyVersion, nil
}

func (repository *PostgresRepository) ListPolicies(ctx context.Context, limit, offset int) ([]PublishedPolicy, int64, error) {
	if limit < 1 || limit > MaximumPolicyLimit || offset < 0 || offset > 1_000_000 {
		return nil, 0, ErrInput
	}
	rows, err := repository.pool.Query(ctx, `
SELECT `+policyColumns+`
FROM economy.attendance_policy_revisions
ORDER BY effective_from DESC, revision DESC
LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list attendance policies: %w", err)
	}
	defer rows.Close()
	items := make([]PublishedPolicy, 0, limit)
	for rows.Next() {
		policy, err := scanPublishedPolicy(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, policy)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("finish attendance policies: %w", err)
	}
	rows.Close()
	for index := range items {
		milestones, err := readMilestones(ctx, repository.pool, items[index].Policy.Revision)
		if err != nil {
			return nil, 0, err
		}
		items[index].Policy.StreakMilestones = milestones
	}
	var total int64
	if err := repository.pool.QueryRow(ctx, `SELECT count(*) FROM economy.attendance_policy_revisions`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count attendance policies: %w", err)
	}
	return items, total, nil
}

func (repository *PostgresRepository) LatestPolicy(ctx context.Context) (PublishedPolicy, error) {
	policy, found, err := readPolicy(ctx, repository.pool, `ORDER BY effective_from DESC LIMIT 1`)
	if err != nil {
		return PublishedPolicy{}, err
	}
	if !found {
		return PublishedPolicy{}, ErrPolicyNotFound
	}
	policy.Policy.StreakMilestones, err = readMilestones(ctx, repository.pool, policy.Policy.Revision)
	return policy, err
}

func (repository *PostgresRepository) PublishPolicy(ctx context.Context, command PublishCommand) (PublishedPolicy, error) {
	normalized, snapshot, err := NormalizePolicy(command.Policy)
	if err != nil || command.IssuedBy == uuid.Nil || command.AuthorizationDecisionID == uuid.Nil ||
		strings.TrimSpace(command.Reason) != command.Reason || !bytes.Equal(snapshot, command.SnapshotJSON) {
		return PublishedPolicy{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PublishedPolicy{}, fmt.Errorf("begin attendance policy publish: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('peergo-attendance-policy-v1', 0))`); err != nil {
		return PublishedPolicy{}, fmt.Errorf("lock attendance policy timeline: %w", err)
	}
	if existing, found, err := readPolicy(ctx, tx, `WHERE revision = $1`, normalized.Revision); found || err != nil {
		if err != nil {
			return PublishedPolicy{}, err
		}
		existing.Policy.StreakMilestones, err = readMilestones(ctx, tx, existing.Policy.Revision)
		if err != nil {
			return PublishedPolicy{}, err
		}
		if !samePublishedPolicy(existing, command, snapshot) {
			return PublishedPolicy{}, ErrPolicyConflict
		}
		existing.Replayed = true
		if err := tx.Commit(ctx); err != nil {
			return PublishedPolicy{}, classifyDatabaseError("commit attendance policy replay", err)
		}
		return existing, nil
	}
	var latest time.Time
	err = tx.QueryRow(ctx, `SELECT effective_from FROM economy.attendance_policy_revisions ORDER BY effective_from DESC LIMIT 1`).Scan(&latest)
	if err == nil && !normalized.EffectiveFrom.After(latest) {
		return PublishedPolicy{}, ErrPolicyConflict
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return PublishedPolicy{}, fmt.Errorf("read latest attendance policy: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO economy.attendance_policy_revisions (`+policyColumns+`)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9,
    $10, $11, $12, $13, $14, $15, $16, $17
)`, normalized.Revision, normalized.EffectiveFrom, normalized.Enabled, normalized.DayBoundaryTimezone,
		normalized.FixedEnabled, normalized.FixedReward, normalized.RandomEnabled, normalized.RandomMin, normalized.RandomMax,
		normalized.StreakEnabled, normalized.ExperienceReward, string(snapshot), normalized.SnapshotSHA256[:],
		command.IssuedBy, command.AuthorizationDecisionID, command.Reason, normalized.CreatedAt)
	if err != nil {
		return PublishedPolicy{}, classifyDatabaseError("insert attendance policy", err)
	}
	for index, milestone := range normalized.StreakMilestones {
		if _, err := tx.Exec(ctx, `
INSERT INTO economy.attendance_policy_streak_milestones (
    policy_revision, position, days, reward
) VALUES ($1, $2, $3, $4)`, normalized.Revision, int16(index), milestone.Days, milestone.Reward); err != nil {
			return PublishedPolicy{}, classifyDatabaseError("insert attendance streak milestone", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return PublishedPolicy{}, classifyDatabaseError("commit attendance policy", err)
	}
	issuer, decision := command.IssuedBy, command.AuthorizationDecisionID
	return PublishedPolicy{Policy: normalized, IssuedBy: &issuer, AuthorizationDecisionID: &decision, Reason: command.Reason}, nil
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type rowQuerier interface {
	queryRower
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func resolvePolicy(ctx context.Context, querier rowQuerier, effectiveAt time.Time) (PublishedPolicy, bool, error) {
	policy, found, err := readPolicy(ctx, querier, `WHERE effective_from <= $1 ORDER BY effective_from DESC LIMIT 1`, effectiveAt)
	if err != nil || !found {
		return PublishedPolicy{}, found, err
	}
	policy.Policy.StreakMilestones, err = readMilestones(ctx, querier, policy.Policy.Revision)
	return policy, true, err
}

func readPolicy(ctx context.Context, querier queryRower, suffix string, arguments ...any) (PublishedPolicy, bool, error) {
	result, err := scanPublishedPolicy(querier.QueryRow(ctx, `SELECT `+policyColumns+` FROM economy.attendance_policy_revisions `+suffix, arguments...))
	if errors.Is(err, pgx.ErrNoRows) {
		return PublishedPolicy{}, false, nil
	}
	if err != nil {
		return PublishedPolicy{}, false, err
	}
	return result, true, nil
}

type rowScanner interface{ Scan(...any) error }

func scanPublishedPolicy(scanner rowScanner) (PublishedPolicy, error) {
	var result PublishedPolicy
	var snapshotJSON string
	var snapshotSHA256 []byte
	var issuer, decision pgtype.UUID
	err := scanner.Scan(
		&result.Policy.Revision, &result.Policy.EffectiveFrom, &result.Policy.Enabled, &result.Policy.DayBoundaryTimezone,
		&result.Policy.FixedEnabled, &result.Policy.FixedReward, &result.Policy.RandomEnabled,
		&result.Policy.RandomMin, &result.Policy.RandomMax, &result.Policy.StreakEnabled,
		&result.Policy.ExperienceReward, &snapshotJSON, &snapshotSHA256,
		&issuer, &decision, &result.Reason, &result.Policy.CreatedAt,
	)
	if err != nil {
		return PublishedPolicy{}, err
	}
	if len(snapshotSHA256) != sha256.Size {
		return PublishedPolicy{}, ErrInvariant
	}
	copy(result.Policy.SnapshotSHA256[:], snapshotSHA256)
	if issuer.Valid {
		value := uuid.UUID(issuer.Bytes)
		result.IssuedBy = &value
	}
	if decision.Valid {
		value := uuid.UUID(decision.Bytes)
		result.AuthorizationDecisionID = &value
	}
	result.Policy.EffectiveFrom = canonicalTime(result.Policy.EffectiveFrom)
	result.Policy.CreatedAt = canonicalTime(result.Policy.CreatedAt)
	return result, nil
}

func readMilestones(ctx context.Context, querier rowQuerier, revision string) ([]StreakMilestone, error) {
	rows, err := querier.Query(ctx, `
SELECT days, reward
FROM economy.attendance_policy_streak_milestones
WHERE policy_revision = $1
ORDER BY position`, revision)
	if err != nil {
		return nil, fmt.Errorf("list attendance streak milestones: %w", err)
	}
	defer rows.Close()
	result := make([]StreakMilestone, 0, 8)
	for rows.Next() {
		var milestone StreakMilestone
		if err := rows.Scan(&milestone.Days, &milestone.Reward); err != nil {
			return nil, fmt.Errorf("scan attendance streak milestone: %w", err)
		}
		result = append(result, milestone)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish attendance streak milestones: %w", err)
	}
	return result, nil
}

func readRecord(ctx context.Context, querier queryRower, suffix string, arguments ...any) (Record, bool, error) {
	result, err := scanRecord(querier.QueryRow(ctx, `
SELECT
    id, request_id, user_id, attendance_date::text, day_boundary_timezone,
    mode, base_reward, streak_reward, total_reward, experience_reward,
    current_streak, total_days, longest_streak, policy_revision,
    payload_sha256, magic_transaction_id, experience_entry_id,
    occurred_at, recorded_at
FROM economy.attendance_records `+suffix, arguments...))
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, err
	}
	return result, true, nil
}

func scanRecord(scanner rowScanner) (Record, error) {
	var result Record
	var mode string
	var payload []byte
	var experienceID pgtype.UUID
	err := scanner.Scan(
		&result.ID, &result.RequestID, &result.UserID, &result.AttendanceDate, &result.DayBoundaryTimezone,
		&mode, &result.BaseReward, &result.StreakReward, &result.TotalReward, &result.ExperienceReward,
		&result.CurrentStreak, &result.TotalDays, &result.LongestStreak, &result.PolicyRevision,
		&payload, &result.MagicTransactionID, &experienceID, &result.OccurredAt, &result.RecordedAt,
	)
	if err != nil {
		return Record{}, err
	}
	result.Mode = Mode(mode)
	if len(payload) != sha256.Size {
		return Record{}, ErrInvariant
	}
	copy(result.PayloadSHA256[:], payload)
	if experienceID.Valid {
		value := uuid.UUID(experienceID.Bytes)
		result.ExperienceEntryID = &value
	}
	result.OccurredAt = canonicalTime(result.OccurredAt)
	result.RecordedAt = canonicalTime(result.RecordedAt)
	return result, nil
}

func samePublishedPolicy(existing PublishedPolicy, command PublishCommand, snapshot []byte) bool {
	if existing.IssuedBy == nil || existing.AuthorizationDecisionID == nil {
		return false
	}
	normalized, existingSnapshot, err := NormalizePolicy(existing.Policy)
	return err == nil && normalized.SnapshotSHA256 == command.Policy.SnapshotSHA256 &&
		bytes.Equal(existingSnapshot, snapshot) && *existing.IssuedBy == command.IssuedBy &&
		*existing.AuthorizationDecisionID == command.AuthorizationDecisionID && existing.Reason == command.Reason
}

func secureRandomInt(minimum, maximum int64) (int64, error) {
	if minimum < 1 || maximum < minimum {
		return 0, ErrInput
	}
	width := new(big.Int).SetInt64(maximum - minimum + 1)
	value, err := cryptorand.Int(cryptorand.Reader, width)
	if err != nil {
		return 0, err
	}
	return value.Int64() + minimum, nil
}

func classifyDatabaseError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return fmt.Errorf("%w: %s: %v", ErrPolicyConflict, operation, err)
		case "P0001", "23514", "23503", "40001":
			return fmt.Errorf("%w: %s: %v", ErrInvariant, operation, err)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ Repository = (*PostgresRepository)(nil)
