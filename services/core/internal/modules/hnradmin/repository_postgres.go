package hnradmin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/hnrcontrolv1"
	"github.com/peergo/peergo/contracts/go/hnrpolicyv1"
	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
)

type PostgresRepository struct {
	pool             *pgxpool.Pool
	eventBuilder     RevisionEventBuilder
	newAuditAppender func(pgx.Tx) auditevent.Appender
}

func NewPostgresRepository(pool *pgxpool.Pool, eventBuilder RevisionEventBuilder, newAuditAppender func(pgx.Tx) auditevent.Appender) (*PostgresRepository, error) {
	if pool == nil || eventBuilder == nil || newAuditAppender == nil {
		return nil, errors.New("H&R administration repository dependencies are required")
	}
	return &PostgresRepository{pool: pool, eventBuilder: eventBuilder, newAuditAppender: newAuditAppender}, nil
}

func (repository *PostgresRepository) List(ctx context.Context, limit, offset int, now time.Time) (Page, error) {
	if limit < 1 || limit > MaxListLimit || offset < 0 || now.IsZero() {
		return Page{}, ErrInput
	}
	rows, err := repository.pool.Query(ctx, `
SELECT
    revision.id, revision.rule_id, revision.rule_version, revision.mode,
    revision.required_seed_seconds, revision.required_ratio_basis_points,
    revision.assessment_window_seconds, revision.grace_period_seconds,
    revision.max_interval_credit_seconds, revision.effective_at,
    revision.reason, revision.actor_id, revision.authorization_decision_id,
    revision.command_sha256, revision.created_at,
    delivery.attempts, COALESCE(delivery.last_error_code, ''), delivery.delivered_at
FROM hnr_control.policy_revisions AS revision
JOIN hnr_control.delivery_outbox AS delivery ON delivery.revision_id = revision.id
ORDER BY revision.effective_at DESC, revision.id DESC
LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return Page{}, fmt.Errorf("list H&R policy revisions: %w", err)
	}
	defer rows.Close()
	items := make([]Revision, 0, limit)
	for rows.Next() {
		revision, err := scanRevision(rows, now)
		if err != nil {
			return Page{}, err
		}
		items = append(items, revision)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("finish H&R policy revision list: %w", err)
	}
	var total int64
	if err := repository.pool.QueryRow(ctx, `SELECT count(*) FROM hnr_control.policy_revisions`).Scan(&total); err != nil {
		return Page{}, fmt.Errorf("count H&R policy revisions: %w", err)
	}
	return Page{
		Items: items, Total: total, Limit: limit, Offset: offset,
		MinimumEffectiveFrom: now.Add(minimumLeadTime),
	}, nil
}

func (repository *PostgresRepository) Issue(ctx context.Context, command IssueCommand) (Revision, error) {
	if command.RevisionID == uuid.Nil || command.ActorID == uuid.Nil || command.Authorization.ID == uuid.Nil ||
		command.OccurredAt.IsZero() || command.BaseRuleVersion < 0 {
		return Revision{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Revision{}, fmt.Errorf("begin H&R policy revision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('peergo-hnr-policy-control-v1', 0))`); err != nil {
		return Revision{}, fmt.Errorf("lock H&R policy control timeline: %w", err)
	}
	if existing, found, err := readRevisionByID(ctx, tx, command.RevisionID, command.OccurredAt); found || err != nil {
		if err != nil {
			return Revision{}, err
		}
		if !sameRevisionRequest(existing, command) {
			return Revision{}, ErrIdempotencyConflict
		}
		existing.Replayed = true
		if err := tx.Commit(ctx); err != nil {
			return Revision{}, fmt.Errorf("commit replayed H&R policy revision: %w", err)
		}
		return existing, nil
	}

	var latestVersion int64
	var latestEffective pgtype.Timestamptz
	var latestMode pgtype.Text
	var latestSeed, latestRatio, latestWindow, latestGrace, latestCredit pgtype.Int8
	err = tx.QueryRow(ctx, `
SELECT rule_version, effective_at, mode, required_seed_seconds,
       required_ratio_basis_points, assessment_window_seconds,
       grace_period_seconds, max_interval_credit_seconds
FROM hnr_control.policy_revisions
WHERE rule_id = $1
ORDER BY rule_version DESC
LIMIT 1`, DefaultRuleID).Scan(
		&latestVersion, &latestEffective, &latestMode, &latestSeed,
		&latestRatio, &latestWindow, &latestGrace, &latestCredit,
	)
	if err == nil {
		if !command.EffectiveAt.After(latestEffective.Time) {
			return Revision{}, ErrConflict
		}
		if latestMode.String == string(command.Policy.Mode) && latestSeed.Int64 == command.Policy.RequiredSeedSeconds &&
			latestRatio.Int64 == command.Policy.RequiredRatioBasisPoints && latestWindow.Int64 == command.Policy.AssessmentWindowSeconds &&
			latestGrace.Int64 == command.Policy.GracePeriodSeconds && latestCredit.Int64 == command.Policy.MaxIntervalCreditSeconds {
			return Revision{}, ErrNoChange
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Revision{}, fmt.Errorf("read latest H&R policy revision: %w", err)
	} else if command.CurrentPolicy != nil && samePolicyInput(*command.CurrentPolicy, command.Policy) {
		return Revision{}, ErrNoChange
	}
	if command.BaseRuleVersion > latestVersion {
		latestVersion = command.BaseRuleVersion
	}
	policy := hnrpolicyv1.Policy{
		Rule: hnrpolicyv1.RuleRef{ID: DefaultRuleID, Version: latestVersion + 1},
		Mode: command.Policy.Mode, RequiredSeedSeconds: command.Policy.RequiredSeedSeconds,
		RequiredRatioBasisPoints: command.Policy.RequiredRatioBasisPoints,
		AssessmentWindowSeconds:  command.Policy.AssessmentWindowSeconds,
		GracePeriodSeconds:       command.Policy.GracePeriodSeconds,
		MaxIntervalCreditSeconds: command.Policy.MaxIntervalCreditSeconds,
	}
	control := hnrcontrolv1.Command{
		SchemaVersion: hnrcontrolv1.SchemaVersion, RevisionID: command.RevisionID.String(),
		EffectiveAt: command.EffectiveAt, Policy: policy,
	}
	encoded, err := hnrcontrolv1.Encode(control)
	if err != nil {
		return Revision{}, ErrInput
	}
	digest, err := hnrcontrolv1.SHA256(encoded)
	if err != nil {
		return Revision{}, ErrInput
	}
	_, err = tx.Exec(ctx, `
INSERT INTO hnr_control.policy_revisions (
    id, rule_id, rule_version, mode, required_seed_seconds,
    required_ratio_basis_points, assessment_window_seconds,
    grace_period_seconds, max_interval_credit_seconds, effective_at,
    reason, actor_id, authorization_decision_id, command_json,
    command_sha256, created_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
)`, command.RevisionID, policy.Rule.ID, policy.Rule.Version, policy.Mode,
		policy.RequiredSeedSeconds, policy.RequiredRatioBasisPoints,
		policy.AssessmentWindowSeconds, policy.GracePeriodSeconds,
		policy.MaxIntervalCreditSeconds, command.EffectiveAt, command.Reason,
		command.ActorID, command.Authorization.ID, string(encoded), digest[:], command.OccurredAt)
	if err != nil {
		return Revision{}, classifyDatabaseError("insert H&R policy revision", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO hnr_control.delivery_outbox (revision_id, available_at, created_at)
VALUES ($1, $2, $2)`, command.RevisionID, command.OccurredAt); err != nil {
		return Revision{}, classifyDatabaseError("enqueue H&R policy revision", err)
	}
	revision := Revision{
		ID: command.RevisionID, Policy: policy, EffectiveAt: command.EffectiveAt,
		Reason: command.Reason, ActorID: command.ActorID, CreatedAt: command.OccurredAt,
		DeliveryState: DeliveryPending, TimelineState: TimelineScheduled,
		CommandSHA256: digest, Authorization: command.Authorization,
		AuthorizationID: command.Authorization.ID,
	}
	auditEvent, err := repository.eventBuilder.BuildHNRPolicyRevisionEvent(RevisionAuditInput{Revision: revision, Authorization: command.Authorization})
	if err != nil {
		return Revision{}, fmt.Errorf("build H&R policy revision audit event: %w", err)
	}
	if err := repository.newAuditAppender(tx).Append(ctx, auditEvent); err != nil {
		return Revision{}, fmt.Errorf("append H&R policy revision audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Revision{}, classifyDatabaseError("commit H&R policy revision", err)
	}
	return revision, nil
}

type rowScanner interface{ Scan(...any) error }

func scanRevision(scanner rowScanner, now time.Time) (Revision, error) {
	var result Revision
	var mode string
	var digest []byte
	var deliveredAt pgtype.Timestamptz
	if err := scanner.Scan(
		&result.ID, &result.Policy.Rule.ID, &result.Policy.Rule.Version, &mode,
		&result.Policy.RequiredSeedSeconds, &result.Policy.RequiredRatioBasisPoints,
		&result.Policy.AssessmentWindowSeconds, &result.Policy.GracePeriodSeconds,
		&result.Policy.MaxIntervalCreditSeconds, &result.EffectiveAt,
		&result.Reason, &result.ActorID, &result.AuthorizationID, &digest,
		&result.CreatedAt, &result.DeliveryAttempts, &result.LastDeliveryError, &deliveredAt,
	); err != nil {
		return Revision{}, err
	}
	result.Policy.Mode = hnrpolicyv1.Mode(mode)
	if result.ID == uuid.Nil || result.ActorID == uuid.Nil || result.AuthorizationID == uuid.Nil ||
		len(digest) != 32 || hnrpolicyv1.Validate(result.Policy) != nil {
		return Revision{}, ErrInvariant
	}
	copy(result.CommandSHA256[:], digest)
	result.DeliveryState = DeliveryPending
	if deliveredAt.Valid {
		at := deliveredAt.Time.UTC()
		result.DeliveredAt = &at
		result.DeliveryState = DeliveryDelivered
	} else if result.DeliveryAttempts > 0 && result.LastDeliveryError != "" {
		result.DeliveryState = DeliveryRetrying
	}
	result.TimelineState = TimelineScheduled
	if !now.Before(result.EffectiveAt) {
		result.TimelineState = TimelineActive
	}
	return result, nil
}

func readRevisionByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, now time.Time) (Revision, bool, error) {
	result, err := scanRevision(tx.QueryRow(ctx, `
SELECT
    revision.id, revision.rule_id, revision.rule_version, revision.mode,
    revision.required_seed_seconds, revision.required_ratio_basis_points,
    revision.assessment_window_seconds, revision.grace_period_seconds,
    revision.max_interval_credit_seconds, revision.effective_at,
    revision.reason, revision.actor_id, revision.authorization_decision_id,
    revision.command_sha256, revision.created_at,
    delivery.attempts, COALESCE(delivery.last_error_code, ''), delivery.delivered_at
FROM hnr_control.policy_revisions AS revision
JOIN hnr_control.delivery_outbox AS delivery ON delivery.revision_id = revision.id
WHERE revision.id = $1`, id), now)
	if errors.Is(err, pgx.ErrNoRows) {
		return Revision{}, false, nil
	}
	if err != nil {
		return Revision{}, false, fmt.Errorf("read existing H&R policy revision: %w", err)
	}
	return result, true, nil
}

func sameRevisionRequest(existing Revision, command IssueCommand) bool {
	return existing.ID == command.RevisionID && existing.ActorID == command.ActorID &&
		existing.EffectiveAt.Equal(command.EffectiveAt) && existing.Reason == command.Reason &&
		existing.Policy.Mode == command.Policy.Mode &&
		existing.Policy.RequiredSeedSeconds == command.Policy.RequiredSeedSeconds &&
		existing.Policy.RequiredRatioBasisPoints == command.Policy.RequiredRatioBasisPoints &&
		existing.Policy.AssessmentWindowSeconds == command.Policy.AssessmentWindowSeconds &&
		existing.Policy.GracePeriodSeconds == command.Policy.GracePeriodSeconds &&
		existing.Policy.MaxIntervalCreditSeconds == command.Policy.MaxIntervalCreditSeconds
}

func samePolicyInput(left, right PolicyInput) bool {
	return left.Mode == right.Mode && left.RequiredSeedSeconds == right.RequiredSeedSeconds &&
		left.RequiredRatioBasisPoints == right.RequiredRatioBasisPoints &&
		left.AssessmentWindowSeconds == right.AssessmentWindowSeconds &&
		left.GracePeriodSeconds == right.GracePeriodSeconds &&
		left.MaxIntervalCreditSeconds == right.MaxIntervalCreditSeconds
}

func classifyDatabaseError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505", "P0001":
			return fmt.Errorf("%w: %s: %v", ErrConflict, operation, err)
		case "23503", "23514":
			return fmt.Errorf("%w: %s: %v", ErrInvariant, operation, err)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ Repository = (*PostgresRepository)(nil)
