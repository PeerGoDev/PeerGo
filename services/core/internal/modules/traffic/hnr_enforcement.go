package traffic

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const MaximumHNREnforcementBatch = 5000

type HNREnforcementResult struct {
	Skipped  bool
	Examined int
	Created  int
}

type HNREnforcementEvaluator interface {
	EvaluateHNREnforcement(context.Context, time.Time, int) (HNREnforcementResult, error)
}

type HNREnforcementFailureRecorder interface {
	MarkHNREnforcementFailure(context.Context, time.Time, string) error
}

// EvaluateHNREnforcement projects only deterministic clock boundaries and the
// terminal satisfied fact into the member inbox. Download enforcement itself
// does not depend on this worker: identity.is_download_restricted reads the
// current H&R obligation and PostgreSQL clock directly, so a delayed worker can
// delay a message but can neither delay nor prolong the actual restriction.
func (repository *PostgresRepository) EvaluateHNREnforcement(ctx context.Context, now time.Time, batch int) (HNREnforcementResult, error) {
	if now.IsZero() || batch < 1 || batch > MaximumHNREnforcementBatch {
		return HNREnforcementResult{}, ErrInput
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return HNREnforcementResult{}, fmt.Errorf("begin H&R enforcement evaluation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('peergo-hnr-enforcement-evaluator'))`).Scan(&locked); err != nil {
		return HNREnforcementResult{}, fmt.Errorf("lock H&R enforcement evaluator: %w", err)
	}
	if !locked {
		if err := tx.Commit(ctx); err != nil {
			return HNREnforcementResult{}, fmt.Errorf("commit skipped H&R enforcement evaluation: %w", err)
		}
		return HNREnforcementResult{Skipped: true}, nil
	}
	if _, err := tx.Exec(ctx, `
UPDATE traffic.hnr_enforcement_worker_state
SET last_started_at = $1, updated_at = $1
WHERE singleton = true`, now); err != nil {
		return HNREnforcementResult{}, fmt.Errorf("start H&R enforcement heartbeat: %w", err)
	}

	rows, err := tx.Query(ctx, `
SELECT obligation.obligation_id
FROM traffic.user_hnr_obligations AS obligation
WHERE (
    obligation.grace_ends_at > obligation.assessment_due_at
    AND $1 >= obligation.assessment_due_at
    AND (
        obligation.state = 'tracking'
        OR obligation.satisfied_at > obligation.assessment_due_at
    )
    AND NOT EXISTS (
        SELECT 1 FROM community.hnr_notifications AS notification
        WHERE notification.obligation_id = obligation.obligation_id
          AND notification.event_kind = 'grace_started'
    )
) OR (
    $1 >= obligation.grace_ends_at
    AND (
        obligation.state = 'tracking'
        OR obligation.satisfied_at > obligation.grace_ends_at
    )
    AND NOT EXISTS (
        SELECT 1 FROM community.hnr_notifications AS notification
        WHERE notification.obligation_id = obligation.obligation_id
          AND notification.event_kind = 'download_restricted'
    )
) OR (
    obligation.state = 'satisfied'
    AND obligation.satisfied_at IS NOT NULL
    AND $1 >= obligation.satisfied_at
    AND NOT EXISTS (
        SELECT 1 FROM community.hnr_notifications AS notification
        WHERE notification.obligation_id = obligation.obligation_id
          AND notification.event_kind = 'satisfied'
    )
)
ORDER BY obligation.completed_at, obligation.obligation_id
LIMIT $2
FOR UPDATE OF obligation SKIP LOCKED`, now, batch)
	if err != nil {
		return HNREnforcementResult{}, fmt.Errorf("claim H&R enforcement candidates: %w", err)
	}
	obligationIDs := make([]uuid.UUID, 0, batch)
	for rows.Next() {
		var obligationID uuid.UUID
		if err := rows.Scan(&obligationID); err != nil {
			rows.Close()
			return HNREnforcementResult{}, fmt.Errorf("scan H&R enforcement candidate: %w", err)
		}
		if obligationID == uuid.Nil {
			rows.Close()
			return HNREnforcementResult{}, ErrInvariant
		}
		obligationIDs = append(obligationIDs, obligationID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return HNREnforcementResult{}, fmt.Errorf("iterate H&R enforcement candidates: %w", err)
	}
	rows.Close()

	created := int64(0)
	for _, obligationID := range obligationIDs {
		var inserted int64
		if err := tx.QueryRow(ctx, `
SELECT community.project_hnr_notifications_for_obligation($1, $2)`,
			obligationID, now).Scan(&inserted); err != nil {
			return HNREnforcementResult{}, classifyDatabaseError("project H&R notifications", err)
		}
		if inserted < 0 || inserted > 3 {
			return HNREnforcementResult{}, ErrInvariant
		}
		created += inserted
	}
	if _, err := tx.Exec(ctx, `
UPDATE traffic.hnr_enforcement_worker_state
SET last_succeeded_at = $1,
    last_error_code = NULL,
    last_examined_count = $2,
    last_created_count = $3,
    updated_at = $1
WHERE singleton = true`, now, len(obligationIDs), created); err != nil {
		return HNREnforcementResult{}, fmt.Errorf("complete H&R enforcement heartbeat: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return HNREnforcementResult{}, classifyDatabaseError("commit H&R enforcement evaluation", err)
	}
	return HNREnforcementResult{Examined: len(obligationIDs), Created: int(created)}, nil
}

func (repository *PostgresRepository) MarkHNREnforcementFailure(ctx context.Context, failedAt time.Time, code string) error {
	code = strings.TrimSpace(code)
	if failedAt.IsZero() || code == "" || len(code) > 64 {
		return ErrInput
	}
	_, err := repository.pool.Exec(ctx, `
UPDATE traffic.hnr_enforcement_worker_state
SET last_error_code = $1, updated_at = $2
WHERE singleton = true`, code, failedAt.UTC().Truncate(time.Microsecond))
	if err != nil {
		return fmt.Errorf("record H&R enforcement failure: %w", err)
	}
	return nil
}

type HNREnforcementRunner struct {
	evaluator HNREnforcementEvaluator
	failures  HNREnforcementFailureRecorder
	interval  time.Duration
	batch     int
	logger    *slog.Logger
	now       func() time.Time
}

func NewHNREnforcementRunner(
	evaluator HNREnforcementEvaluator,
	failures HNREnforcementFailureRecorder,
	interval time.Duration,
	batch int,
	logger *slog.Logger,
	now func() time.Time,
) (*HNREnforcementRunner, error) {
	if evaluator == nil || failures == nil || interval < 10*time.Second || interval > time.Hour ||
		batch < 1 || batch > MaximumHNREnforcementBatch || logger == nil {
		return nil, ErrInput
	}
	if now == nil {
		now = time.Now
	}
	return &HNREnforcementRunner{
		evaluator: evaluator, failures: failures, interval: interval,
		batch: batch, logger: logger, now: now,
	}, nil
}

// Run shares the policy-worker process but not the ratio evaluator's lock or
// failure state. A notification projection failure is retried next tick and
// cannot stop promotion/H&R-policy delivery or change the live download gate.
func (runner *HNREnforcementRunner) Run(ctx context.Context) error {
	runner.runOnce(ctx)
	ticker := time.NewTicker(runner.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			runner.runOnce(ctx)
		}
	}
}

func (runner *HNREnforcementRunner) runOnce(ctx context.Context) {
	now := runner.now().UTC().Round(0)
	result, err := runner.evaluator.EvaluateHNREnforcement(ctx, now, runner.batch)
	if err != nil {
		_ = runner.failures.MarkHNREnforcementFailure(ctx, now, "evaluation_failed")
		runner.logger.Error("H&R enforcement evaluation failed", "error", err)
		return
	}
	if result.Skipped {
		return
	}
	runner.logger.Info("H&R enforcement evaluation completed",
		"examined", result.Examined,
		"notifications_created", result.Created,
	)
}

var _ HNREnforcementEvaluator = (*PostgresRepository)(nil)
var _ HNREnforcementFailureRecorder = (*PostgresRepository)(nil)
