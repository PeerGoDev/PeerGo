package progression

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	minimumLevelPolicyLeadTime = time.Hour
	maximumLevelPolicyCount    = 99
	maximumLevelBonusBPS       = 10_000
	maximumLevelSeedingBonus   = 1_000
)

var (
	ErrLevelPolicyInput           = errors.New("level policy input is invalid")
	ErrLevelPolicyVersionConflict = errors.New("level policy timeline changed")
	ErrLevelPolicyIdempotency     = errors.New("level policy request was reused")
)

// LevelRule is one complete step in an immutable level ladder. Experience
// thresholds are integers for administration even though runtime experience
// entries retain exact decimal precision.
type LevelRule struct {
	Level             int16
	MinimumExperience int64
	KarmaBonusBPS     int64
	SeedingCountBonus int32
	CurrentUserCount  int64
}

type LevelPolicyRevision struct {
	PolicyVersion  string
	Sequence       int64
	EffectiveAt    time.Time
	Levels         []LevelRule
	UserCount      int64
	IssuedBy       *uuid.UUID
	Reason         string
	CreatedAt      time.Time
	SnapshotSHA256 [sha256.Size]byte
	AppliedAt      *time.Time
	AffectedUsers  int64
	ChangedLevels  int64
}

type LevelPolicyOverview struct {
	Items              []LevelPolicyRevision
	MinimumEffectiveAt time.Time
}

type IssueLevelPolicyInput struct {
	RequestID        uuid.UUID
	ExpectedSequence int64
	EffectiveAt      time.Time
	Levels           []LevelRule
	Reason           string
}

type issueLevelPolicyCommand struct {
	IssueLevelPolicyInput
	PolicyVersion  string
	SnapshotJSON   []byte
	SnapshotSHA256 [sha256.Size]byte
	ActorID        uuid.UUID
	OccurredAt     time.Time
	Authorization  authz.Decision
}

type LevelPolicyActivation struct {
	PolicyVersion string
	AffectedUsers int64
	ChangedLevels int64
	AppliedAt     time.Time
}

type LevelPolicyRepository interface {
	LevelPolicyOverview(context.Context, time.Time, int) ([]LevelPolicyRevision, error)
	IssueLevelPolicy(context.Context, issueLevelPolicyCommand) (LevelPolicyRevision, error)
	ActivateDueLevelPolicy(context.Context, time.Time) (LevelPolicyActivation, bool, error)
}

type LevelPolicyService struct {
	repository LevelPolicyRepository
	authorizer authz.Authorizer
	now        func() time.Time
}

func NewLevelPolicyService(repository LevelPolicyRepository, authorizer authz.Authorizer, now func() time.Time) (*LevelPolicyService, error) {
	if repository == nil || authorizer == nil {
		return nil, errors.New("level policy dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &LevelPolicyService{repository: repository, authorizer: authorizer, now: now}, nil
}

func (service *LevelPolicyService) Overview(ctx context.Context, actor authz.StaffActor) (LevelPolicyOverview, error) {
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionProgressionLevelPolicyRead, authz.SiteScope(), now, "level-policy-read"); err != nil {
		return LevelPolicyOverview{}, err
	}
	items, err := service.repository.LevelPolicyOverview(ctx, now, 32)
	if err != nil {
		return LevelPolicyOverview{}, err
	}
	return LevelPolicyOverview{Items: items, MinimumEffectiveAt: minimumLevelPolicyEffectiveAt(now)}, nil
}

func (service *LevelPolicyService) IssueLevelPolicy(ctx context.Context, actor authz.StaffActor, input IssueLevelPolicyInput) (LevelPolicyRevision, error) {
	now := service.now().UTC().Round(0)
	normalized, snapshot, digest, err := normalizeIssueLevelPolicyInput(input, now)
	if err != nil {
		return LevelPolicyRevision{}, err
	}
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionProgressionLevelPolicyIssue, authz.SiteScope(), now, "level-policy-issue")
	if err != nil {
		return LevelPolicyRevision{}, err
	}
	versionSuffix := strings.ReplaceAll(normalized.RequestID.String(), "-", "")[:12]
	policyVersion := "level-" + normalized.EffectiveAt.Format("20060102t150405z") + "-" + versionSuffix
	return service.repository.IssueLevelPolicy(ctx, issueLevelPolicyCommand{
		IssueLevelPolicyInput: normalized,
		PolicyVersion:         policyVersion, SnapshotJSON: snapshot, SnapshotSHA256: digest,
		ActorID: actor.Subject.ID, OccurredAt: now, Authorization: decision,
	})
}

func minimumLevelPolicyEffectiveAt(now time.Time) time.Time {
	return now.UTC().Add(minimumLevelPolicyLeadTime).Truncate(time.Hour).Add(time.Hour)
}

type levelPolicySnapshot struct {
	Levels []levelPolicySnapshotRule `json:"levels"`
}

type levelPolicySnapshotRule struct {
	Level             int16  `json:"level"`
	MinimumExperience string `json:"minimum_experience"`
	KarmaBonusBPS     int64  `json:"karma_bonus_bps"`
	SeedingCountBonus int32  `json:"seeding_count_bonus"`
}

func normalizeIssueLevelPolicyInput(input IssueLevelPolicyInput, now time.Time) (IssueLevelPolicyInput, []byte, [sha256.Size]byte, error) {
	var zero [sha256.Size]byte
	input.Reason = strings.TrimSpace(input.Reason)
	input.EffectiveAt = input.EffectiveAt.UTC().Truncate(time.Second)
	if input.RequestID == uuid.Nil || input.ExpectedSequence < 1 ||
		input.EffectiveAt.Before(minimumLevelPolicyEffectiveAt(now)) || input.EffectiveAt.Minute() != 0 || input.EffectiveAt.Second() != 0 ||
		!utf8.ValidString(input.Reason) || utf8.RuneCountInString(input.Reason) < 10 || utf8.RuneCountInString(input.Reason) > 1000 ||
		len(input.Levels) < 2 || len(input.Levels) > maximumLevelPolicyCount {
		return IssueLevelPolicyInput{}, nil, zero, ErrLevelPolicyInput
	}

	levels := make([]LevelRule, len(input.Levels))
	snapshot := levelPolicySnapshot{Levels: make([]levelPolicySnapshotRule, len(input.Levels))}
	var previousMinimum int64 = -1
	var previousKarma int64 = -1
	var previousSeeding int32 = -1
	for index, rule := range input.Levels {
		expectedLevel := int16(index + 1)
		if rule.Level != expectedLevel || rule.MinimumExperience < 0 ||
			(index == 0 && rule.MinimumExperience != 0) || (index > 0 && rule.MinimumExperience <= previousMinimum) ||
			rule.KarmaBonusBPS < 0 || rule.KarmaBonusBPS > maximumLevelBonusBPS || rule.KarmaBonusBPS < previousKarma ||
			rule.SeedingCountBonus < 0 || rule.SeedingCountBonus > maximumLevelSeedingBonus || rule.SeedingCountBonus < previousSeeding {
			return IssueLevelPolicyInput{}, nil, zero, ErrLevelPolicyInput
		}
		levels[index] = rule
		levels[index].CurrentUserCount = 0
		snapshot.Levels[index] = levelPolicySnapshotRule{
			Level: rule.Level, MinimumExperience: strconv.FormatInt(rule.MinimumExperience, 10),
			KarmaBonusBPS: rule.KarmaBonusBPS, SeedingCountBonus: rule.SeedingCountBonus,
		}
		previousMinimum, previousKarma, previousSeeding = rule.MinimumExperience, rule.KarmaBonusBPS, rule.SeedingCountBonus
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return IssueLevelPolicyInput{}, nil, zero, fmt.Errorf("encode level policy snapshot: %w", err)
	}
	input.Levels = levels
	return input, payload, sha256.Sum256(payload), nil
}

type PostgresLevelPolicyRepository struct{ pool *pgxpool.Pool }

func NewPostgresLevelPolicyRepository(pool *pgxpool.Pool) (*PostgresLevelPolicyRepository, error) {
	if pool == nil {
		return nil, errors.New("level policy repository pool is required")
	}
	return &PostgresLevelPolicyRepository{pool: pool}, nil
}

func (repository *PostgresLevelPolicyRepository) LevelPolicyOverview(ctx context.Context, _ time.Time, limit int) ([]LevelPolicyRevision, error) {
	if limit < 1 || limit > 32 {
		return nil, ErrLevelPolicyInput
	}
	return listLevelPolicies(ctx, repository.pool, limit, "")
}

func (repository *PostgresLevelPolicyRepository) IssueLevelPolicy(ctx context.Context, command issueLevelPolicyCommand) (LevelPolicyRevision, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return LevelPolicyRevision{}, fmt.Errorf("begin level policy issue: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('peergo-level-policy-v1', 0))`); err != nil {
		return LevelPolicyRevision{}, fmt.Errorf("lock level policy timeline: %w", err)
	}

	var existingVersion string
	err = tx.QueryRow(ctx, `SELECT policy_version FROM progression.level_policy_revisions WHERE request_id=$1`, command.RequestID).Scan(&existingVersion)
	if err == nil {
		existing, err := listLevelPolicies(ctx, tx, 1, `WHERE revision.policy_version=$2`, existingVersion)
		if err != nil || len(existing) != 1 {
			return LevelPolicyRevision{}, fmt.Errorf("read replayed level policy: %w", err)
		}
		if !levelPolicyMatchesCommand(existing[0], command) {
			return LevelPolicyRevision{}, ErrLevelPolicyIdempotency
		}
		if err := tx.Commit(ctx); err != nil {
			return LevelPolicyRevision{}, fmt.Errorf("commit replayed level policy: %w", err)
		}
		return existing[0], nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return LevelPolicyRevision{}, fmt.Errorf("read level policy request replay: %w", err)
	}

	var latestSequence int64
	var latestEffectiveAt time.Time
	if err := tx.QueryRow(ctx, `SELECT sequence, effective_at FROM progression.level_policy_revisions ORDER BY sequence DESC LIMIT 1`).Scan(&latestSequence, &latestEffectiveAt); err != nil {
		return LevelPolicyRevision{}, fmt.Errorf("read latest level policy sequence: %w", err)
	}
	// Sequence and time must advance together. This prevents a newer revision
	// from being inserted before an already scheduled revision and makes the
	// worker's effective-time order identical to the administrator's timeline.
	if latestSequence != command.ExpectedSequence || !command.EffectiveAt.After(latestEffectiveAt) {
		return LevelPolicyRevision{}, ErrLevelPolicyVersionConflict
	}
	var sequence int64
	if err := tx.QueryRow(ctx, `
INSERT INTO progression.level_policy_revisions (
    policy_version, request_id, effective_at, snapshot_json, snapshot_sha256,
    issued_by, authorization_decision_id, reason, created_at
) VALUES ($1,$2,$3,$4::jsonb,$5,$6,$7,$8,$9)
RETURNING sequence`, command.PolicyVersion, command.RequestID, command.EffectiveAt,
		string(command.SnapshotJSON), command.SnapshotSHA256[:], command.ActorID,
		command.Authorization.ID, command.Reason, command.OccurredAt).Scan(&sequence); err != nil {
		return LevelPolicyRevision{}, classifyLevelPolicyDatabaseError("insert level policy revision", err)
	}
	for _, rule := range command.Levels {
		if _, err := tx.Exec(ctx, `
INSERT INTO progression.level_definitions (
    policy_version, level, minimum_experience
) VALUES ($1,$2,$3::numeric(38,20))`, command.PolicyVersion, rule.Level, strconv.FormatInt(rule.MinimumExperience, 10)); err != nil {
			return LevelPolicyRevision{}, classifyLevelPolicyDatabaseError("insert level definition", err)
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO progression.seeding_reward_level_benefits (
    policy_version, level, karma_bonus_bps, seeding_count_bonus
) VALUES ($1,$2,$3,$4)`, command.PolicyVersion, rule.Level, rule.KarmaBonusBPS, rule.SeedingCountBonus); err != nil {
			return LevelPolicyRevision{}, classifyLevelPolicyDatabaseError("insert level benefit", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return LevelPolicyRevision{}, classifyLevelPolicyDatabaseError("commit level policy issue", err)
	}
	return LevelPolicyRevision{
		PolicyVersion: command.PolicyVersion, Sequence: sequence, EffectiveAt: command.EffectiveAt,
		Levels: append([]LevelRule(nil), command.Levels...), IssuedBy: &command.ActorID,
		Reason: command.Reason, CreatedAt: command.OccurredAt, SnapshotSHA256: command.SnapshotSHA256,
	}, nil
}

func (repository *PostgresLevelPolicyRepository) ActivateDueLevelPolicy(ctx context.Context, now time.Time) (LevelPolicyActivation, bool, error) {
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return LevelPolicyActivation{}, false, fmt.Errorf("begin level policy activation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('peergo-level-policy-activation-v1', 0))`); err != nil {
		return LevelPolicyActivation{}, false, fmt.Errorf("lock level policy activation: %w", err)
	}
	var policyVersion string
	err = tx.QueryRow(ctx, `
SELECT revision.policy_version
FROM progression.level_policy_revisions AS revision
LEFT JOIN progression.level_policy_activation_runs AS run
  ON run.policy_version = revision.policy_version
WHERE revision.effective_at <= $1
  AND run.policy_version IS NULL
ORDER BY revision.effective_at, revision.sequence
LIMIT 1
FOR UPDATE OF revision`, now).Scan(&policyVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return LevelPolicyActivation{}, false, nil
	}
	if err != nil {
		return LevelPolicyActivation{}, false, fmt.Errorf("read due level policy: %w", err)
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO progression.level_policy_activation_entries (
    policy_version, user_id, experience, from_policy_version,
    from_level, to_level, applied_at
)
SELECT
    $1, progress.user_id, progress.experience, progress.policy_version,
    progress.level, target.level, $2
FROM progression.user_progress AS progress
JOIN LATERAL (
    SELECT definition.level
    FROM progression.level_definitions AS definition
    WHERE definition.policy_version = $1
      AND definition.minimum_experience <= progress.experience
    ORDER BY definition.minimum_experience DESC
    LIMIT 1
) AS target ON true
ORDER BY progress.user_id`, policyVersion, now); err != nil {
		return LevelPolicyActivation{}, false, classifyLevelPolicyDatabaseError("insert level activation evidence", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE progression.user_progress AS progress
SET level = entry.to_level,
    policy_version = entry.policy_version,
    version = progress.version + 1,
    updated_at = $2
FROM progression.level_policy_activation_entries AS entry
WHERE entry.policy_version = $1
  AND entry.user_id = progress.user_id`, policyVersion, now); err != nil {
		return LevelPolicyActivation{}, false, classifyLevelPolicyDatabaseError("apply level policy projection", err)
	}
	var affected, changed int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint,
       count(*) FILTER (WHERE from_level <> to_level)::bigint
FROM progression.level_policy_activation_entries
WHERE policy_version=$1`, policyVersion).Scan(&affected, &changed); err != nil {
		return LevelPolicyActivation{}, false, fmt.Errorf("count level policy activation: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO progression.level_policy_activation_runs (
    policy_version, affected_user_count, changed_level_count, applied_at
) VALUES ($1,$2,$3,$4)`, policyVersion, affected, changed, now); err != nil {
		return LevelPolicyActivation{}, false, classifyLevelPolicyDatabaseError("record level policy activation", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return LevelPolicyActivation{}, false, classifyLevelPolicyDatabaseError("commit level policy activation", err)
	}
	return LevelPolicyActivation{PolicyVersion: policyVersion, AffectedUsers: affected, ChangedLevels: changed, AppliedAt: now}, true, nil
}

type levelPolicyQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func listLevelPolicies(ctx context.Context, querier levelPolicyQuerier, limit int, filter string, arguments ...any) ([]LevelPolicyRevision, error) {
	query := `
WITH selected AS (
    SELECT *
    FROM progression.level_policy_revisions AS revision
    ` + filter + `
    ORDER BY revision.sequence DESC
    LIMIT $1
)
SELECT
    revision.policy_version, revision.sequence, revision.effective_at,
    revision.snapshot_sha256, revision.issued_by, revision.reason,
    revision.created_at, run.applied_at,
    COALESCE(run.affected_user_count, 0), COALESCE(run.changed_level_count, 0),
    definition.level, definition.minimum_experience::text,
    benefit.karma_bonus_bps, benefit.seeding_count_bonus,
    count(progress.user_id)::bigint
FROM selected AS revision
JOIN progression.level_definitions AS definition
  ON definition.policy_version = revision.policy_version
JOIN progression.seeding_reward_level_benefits AS benefit
  ON benefit.policy_version = definition.policy_version
 AND benefit.level = definition.level
LEFT JOIN progression.level_policy_activation_runs AS run
  ON run.policy_version = revision.policy_version
LEFT JOIN progression.user_progress AS progress
  ON progress.policy_version = definition.policy_version
 AND progress.level = definition.level
GROUP BY revision.policy_version, revision.sequence, revision.effective_at,
         revision.snapshot_sha256, revision.issued_by, revision.reason,
         revision.created_at, run.applied_at, run.affected_user_count,
         run.changed_level_count, definition.level,
         definition.minimum_experience, benefit.karma_bonus_bps,
         benefit.seeding_count_bonus
ORDER BY revision.sequence DESC, definition.level`
	queryArgs := append([]any{limit}, arguments...)
	rows, err := querier.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("list level policies: %w", err)
	}
	defer rows.Close()
	policies := make([]LevelPolicyRevision, 0, limit)
	for rows.Next() {
		var version, minimumText string
		var sequence, currentUsers, affected, changed int64
		var effectiveAt, createdAt time.Time
		var reason string
		var digest []byte
		var issuedBy pgtype.UUID
		var appliedAt pgtype.Timestamptz
		var rule LevelRule
		if err := rows.Scan(
			&version, &sequence, &effectiveAt, &digest, &issuedBy, &reason,
			&createdAt, &appliedAt, &affected, &changed, &rule.Level,
			&minimumText, &rule.KarmaBonusBPS, &rule.SeedingCountBonus, &currentUsers,
		); err != nil {
			return nil, fmt.Errorf("scan level policy: %w", err)
		}
		minimum, err := strconv.ParseInt(strings.SplitN(minimumText, ".", 2)[0], 10, 64)
		if err != nil || len(digest) != sha256.Size {
			return nil, fmt.Errorf("persisted level policy is invalid")
		}
		rule.MinimumExperience, rule.CurrentUserCount = minimum, currentUsers
		if len(policies) == 0 || policies[len(policies)-1].PolicyVersion != version {
			policy := LevelPolicyRevision{
				PolicyVersion: version, Sequence: sequence, EffectiveAt: effectiveAt.UTC(),
				Reason: reason, CreatedAt: createdAt.UTC(), AffectedUsers: affected, ChangedLevels: changed,
			}
			copy(policy.SnapshotSHA256[:], digest)
			if issuedBy.Valid {
				value := uuid.UUID(issuedBy.Bytes)
				policy.IssuedBy = &value
			}
			if appliedAt.Valid {
				value := appliedAt.Time.UTC()
				policy.AppliedAt = &value
			}
			policies = append(policies, policy)
		}
		policy := &policies[len(policies)-1]
		policy.Levels = append(policy.Levels, rule)
		policy.UserCount += currentUsers
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish level policies: %w", err)
	}
	return policies, nil
}

func levelPolicyMatchesCommand(policy LevelPolicyRevision, command issueLevelPolicyCommand) bool {
	if !policy.EffectiveAt.Equal(command.EffectiveAt) || policy.Reason != command.Reason || policy.IssuedBy == nil || *policy.IssuedBy != command.ActorID || len(policy.Levels) != len(command.Levels) {
		return false
	}
	for index := range policy.Levels {
		left, right := policy.Levels[index], command.Levels[index]
		if left.Level != right.Level || left.MinimumExperience != right.MinimumExperience || left.KarmaBonusBPS != right.KarmaBonusBPS || left.SeedingCountBonus != right.SeedingCountBonus {
			return false
		}
	}
	return true
}

func classifyLevelPolicyDatabaseError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var postgresError interface{ SQLState() string }
	if errors.As(err, &postgresError) {
		switch postgresError.SQLState() {
		case "23505":
			return fmt.Errorf("%w: %s", ErrLevelPolicyVersionConflict, operation)
		case "23503", "23514", "22003", "22P02":
			return fmt.Errorf("%w: %s", ErrLevelPolicyInput, operation)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ LevelPolicyRepository = (*PostgresLevelPolicyRepository)(nil)
