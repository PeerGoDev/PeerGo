package seedingreward

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const policyColumns = `
    revision, formula_version, effective_from,
    curve_hourly_cap_milli, age_saturation_seconds, seeder_decay,
    curve_scale_milli, size_multiplier_bps, official_bonus_bps,
    upload_contribution_bonus_bps, per_torrent_hourly_milli,
    base_linear_torrent_limit, maximum_level_torrent_bonus,
    minimum_torrent_bytes, minimum_active_seconds,
    maximum_snapshot_age_seconds, vip_bonus_bps,
    maximum_medal_bonus_bps, maximum_level_bonus_bps,
    maximum_hourly_reward, experience_per_magic_bps,
    snapshot_json, snapshot_sha256, issued_by,
    authorization_decision_id, reason, created_at`

type PostgresTimelineRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresTimelineRepository(pool *pgxpool.Pool) (*PostgresTimelineRepository, error) {
	if pool == nil {
		return nil, ErrInput
	}
	return &PostgresTimelineRepository{pool: pool}, nil
}

func (repository *PostgresTimelineRepository) Publish(ctx context.Context, command PublishCommand) (PublishedPolicy, error) {
	normalized, snapshot, err := NormalizePolicy(command.Policy)
	if err != nil || command.IssuedBy == uuid.Nil || command.AuthorizationDecisionID == uuid.Nil ||
		strings.TrimSpace(command.Reason) != command.Reason || len(command.SnapshotJSON) == 0 ||
		!bytes.Equal(command.SnapshotJSON, snapshot) {
		return PublishedPolicy{}, ErrInput
	}
	command.Policy = normalized
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PublishedPolicy{}, fmt.Errorf("begin seeding reward policy publish: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('peergo-seeding-reward-policy-v1', 0))`); err != nil {
		return PublishedPolicy{}, fmt.Errorf("lock seeding reward policy timeline: %w", err)
	}

	existing, found, err := readPolicy(ctx, tx, `WHERE revision = $1`, normalized.Revision)
	if err != nil {
		return PublishedPolicy{}, err
	}
	if found {
		if !samePublishedPolicy(existing, command, snapshot) {
			return PublishedPolicy{}, ErrPolicyConflict
		}
		existing.Replayed = true
		if err := tx.Commit(ctx); err != nil {
			return PublishedPolicy{}, fmt.Errorf("commit replayed seeding reward policy: %w", err)
		}
		return existing, nil
	}
	var latest time.Time
	err = tx.QueryRow(ctx, `
SELECT effective_from
FROM economy.seeding_reward_policy_revisions
ORDER BY effective_from DESC
LIMIT 1`).Scan(&latest)
	if err == nil && !normalized.EffectiveFrom.After(latest) {
		return PublishedPolicy{}, ErrPolicyConflict
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return PublishedPolicy{}, fmt.Errorf("read latest seeding reward policy: %w", err)
	}

	_, err = tx.Exec(ctx, `
INSERT INTO economy.seeding_reward_policy_revisions (`+policyColumns+`)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
    $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27
)`, policyArguments(normalized, snapshot, command)...)
	if err != nil {
		return PublishedPolicy{}, classifyPolicyDatabaseError("insert seeding reward policy", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PublishedPolicy{}, classifyPolicyDatabaseError("commit seeding reward policy", err)
	}
	return PublishedPolicy{
		Policy: normalized, IssuedBy: command.IssuedBy,
		AuthorizationDecisionID: command.AuthorizationDecisionID, Reason: command.Reason,
	}, nil
}

func (repository *PostgresTimelineRepository) Get(ctx context.Context, revision string) (PublishedPolicy, error) {
	revision = strings.TrimSpace(revision)
	if !revisionPattern.MatchString(revision) {
		return PublishedPolicy{}, ErrInput
	}
	policy, found, err := readPolicy(ctx, repository.pool, `WHERE revision = $1`, revision)
	if err != nil {
		return PublishedPolicy{}, err
	}
	if !found {
		return PublishedPolicy{}, ErrPolicyNotFound
	}
	return policy, nil
}

func (repository *PostgresTimelineRepository) Resolve(ctx context.Context, effectiveAt time.Time) (PublishedPolicy, error) {
	effectiveAt = canonicalTime(effectiveAt)
	if effectiveAt.IsZero() {
		return PublishedPolicy{}, ErrInput
	}
	policy, found, err := readPolicy(ctx, repository.pool, `
WHERE effective_from <= $1
ORDER BY effective_from DESC
LIMIT 1`, effectiveAt)
	if err != nil {
		return PublishedPolicy{}, err
	}
	if !found {
		return PublishedPolicy{}, ErrPolicyNotFound
	}
	return policy, nil
}

func (repository *PostgresTimelineRepository) List(ctx context.Context, limit, offset int) ([]PublishedPolicy, int64, error) {
	if limit < 1 || limit > MaximumPolicyListLimit || offset < 0 || offset > 1_000_000 {
		return nil, 0, ErrInput
	}
	rows, err := repository.pool.Query(ctx, `
SELECT `+policyColumns+`
FROM economy.seeding_reward_policy_revisions
ORDER BY effective_from DESC, revision DESC
LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list seeding reward policies: %w", err)
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
		return nil, 0, fmt.Errorf("finish seeding reward policies: %w", err)
	}
	rows.Close()
	var total int64
	if err := repository.pool.QueryRow(ctx, `SELECT count(*) FROM economy.seeding_reward_policy_revisions`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count seeding reward policies: %w", err)
	}
	return items, total, nil
}

type policyQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readPolicy(ctx context.Context, querier policyQuerier, suffix string, arguments ...any) (PublishedPolicy, bool, error) {
	query := `SELECT ` + policyColumns + ` FROM economy.seeding_reward_policy_revisions ` + suffix
	result, err := scanPublishedPolicy(querier.QueryRow(ctx, query, arguments...))
	if errors.Is(err, pgx.ErrNoRows) {
		return PublishedPolicy{}, false, nil
	}
	if err != nil {
		return PublishedPolicy{}, false, err
	}
	return result, true, nil
}

type policyScanner interface {
	Scan(...any) error
}

func scanPublishedPolicy(scanner policyScanner) (PublishedPolicy, error) {
	var result PublishedPolicy
	var snapshotJSON string
	var snapshotDigest []byte
	var issuedBy pgtype.UUID
	var authorizationDecisionID pgtype.UUID
	err := scanner.Scan(
		&result.Policy.Revision, &result.Policy.FormulaVersion, &result.Policy.EffectiveFrom,
		&result.Policy.CurveHourlyCapMilli, &result.Policy.AgeSaturationSeconds, &result.Policy.SeederDecay,
		&result.Policy.CurveScaleMilli, &result.Policy.SizeMultiplierBPS, &result.Policy.OfficialBonusBPS,
		&result.Policy.UploadContributionBonusBPS, &result.Policy.PerTorrentHourlyMilli,
		&result.Policy.BaseLinearTorrentLimit, &result.Policy.MaximumLevelTorrentBonus,
		&result.Policy.MinimumTorrentBytes, &result.Policy.MinimumActiveSeconds,
		&result.Policy.MaximumSnapshotAgeSeconds, &result.Policy.VIPBonusBPS,
		&result.Policy.MaximumMedalBonusBPS, &result.Policy.MaximumLevelBonusBPS,
		&result.Policy.MaximumHourlyReward, &result.Policy.ExperiencePerMagicBPS,
		&snapshotJSON, &snapshotDigest, &issuedBy, &authorizationDecisionID,
		&result.Reason, &result.Policy.CreatedAt,
	)
	if err != nil {
		return PublishedPolicy{}, err
	}
	if len(snapshotDigest) != 32 {
		return PublishedPolicy{}, ErrInvariant
	}
	if issuedBy.Valid != authorizationDecisionID.Valid {
		return PublishedPolicy{}, ErrInvariant
	}
	if issuedBy.Valid {
		result.IssuedBy = uuid.UUID(issuedBy.Bytes)
		result.AuthorizationDecisionID = uuid.UUID(authorizationDecisionID.Bytes)
	}
	copy(result.Policy.SnapshotSHA256[:], snapshotDigest)
	normalized, canonicalJSON, err := NormalizePolicy(result.Policy)
	if err != nil || !bytes.Equal(canonicalJSON, []byte(snapshotJSON)) {
		return PublishedPolicy{}, ErrInvariant
	}
	result.Policy = normalized
	return result, nil
}

func policyArguments(policy PolicyRevision, snapshot []byte, command PublishCommand) []any {
	return []any{
		policy.Revision, policy.FormulaVersion, policy.EffectiveFrom,
		policy.CurveHourlyCapMilli, policy.AgeSaturationSeconds, policy.SeederDecay,
		policy.CurveScaleMilli, policy.SizeMultiplierBPS, policy.OfficialBonusBPS,
		policy.UploadContributionBonusBPS, policy.PerTorrentHourlyMilli,
		policy.BaseLinearTorrentLimit, policy.MaximumLevelTorrentBonus,
		policy.MinimumTorrentBytes, policy.MinimumActiveSeconds,
		policy.MaximumSnapshotAgeSeconds, policy.VIPBonusBPS,
		policy.MaximumMedalBonusBPS, policy.MaximumLevelBonusBPS,
		policy.MaximumHourlyReward, policy.ExperiencePerMagicBPS,
		string(snapshot), policy.SnapshotSHA256[:], command.IssuedBy,
		command.AuthorizationDecisionID, command.Reason, policy.CreatedAt,
	}
}

func samePublishedPolicy(existing PublishedPolicy, command PublishCommand, snapshot []byte) bool {
	// created_at is service-owned receipt time, not part of the signed formula
	// snapshot. Authorization decision IDs are also service-owned and a retry
	// receives a new one, so the first persisted decision remains the evidence
	// when every caller-controlled field and digest is identical.
	return existing.Policy.SnapshotSHA256 == command.Policy.SnapshotSHA256 && existing.IssuedBy == command.IssuedBy &&
		existing.Reason == command.Reason && bytes.Equal(snapshot, command.SnapshotJSON)
}

func classifyPolicyDatabaseError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505", "P0001":
			return fmt.Errorf("%w: %s: %v", ErrPolicyConflict, operation, err)
		case "23503", "23514":
			return fmt.Errorf("%w: %s: %v", ErrInvariant, operation, err)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ TimelineRepository = (*PostgresTimelineRepository)(nil)
var _ AdministrationRepository = (*PostgresTimelineRepository)(nil)
