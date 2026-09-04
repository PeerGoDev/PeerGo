package haremreward

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	maximumSettlementAttempts = 5
	initialSettlementBackoff  = 100 * time.Millisecond
)

type Runner struct {
	repository Repository
	interval   time.Duration
	batch      int
	logger     *slog.Logger
	now        func() time.Time
	retryWait  func(context.Context, time.Duration) error
}

func NewRunner(repository Repository, interval time.Duration, batch int, logger *slog.Logger, now func() time.Time) (*Runner, error) {
	if repository == nil || interval < time.Minute || interval > time.Hour ||
		batch < 1 || batch > MaximumSettlementBatch || logger == nil {
		return nil, ErrInput
	}
	if now == nil {
		now = time.Now
	}
	return &Runner{
		repository: repository, interval: interval, batch: batch,
		logger: logger, now: now, retryWait: waitForSettlementRetry,
	}, nil
}

func (runner *Runner) Run(ctx context.Context) error {
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

func (runner *Runner) runOnce(ctx context.Context) {
	for processed := 0; processed < runner.batch; processed++ {
		now := canonicalTime(runner.now())
		result, err := runner.settleNext(ctx, now)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			_ = runner.repository.MarkFailure(ctx, now, "settlement_failed")
			runner.logger.Error("harem reward settlement failed", "error", err)
			return
		}
		if !result.Processed {
			return
		}
		runner.logger.Info("harem reward window settled",
			"window_start", result.WindowStart,
			"window_end", result.WindowEnd,
			"policy_revision", result.PolicyRevision,
			"source_calculations", result.SourceCalculationCount,
			"eligible_relationships", result.EligibleRelationshipCount,
			"recipients", result.RecipientCount,
			"reward", result.TotalReward,
		)
	}
}

func (runner *Runner) settleNext(ctx context.Context, now time.Time) (Settlement, error) {
	for attempt := 1; attempt <= maximumSettlementAttempts; attempt++ {
		result, err := runner.repository.SettleNext(ctx, now)
		if err == nil || !retryableSettlementConflict(err) || attempt == maximumSettlementAttempts {
			return result, err
		}

		delay := initialSettlementBackoff << (attempt - 1)
		runner.logger.Warn(
			"retrying harem reward settlement after database conflict",
			"attempt", attempt,
			"next_attempt", attempt+1,
			"backoff", delay,
		)
		if err := runner.retryWait(ctx, delay); err != nil {
			return Settlement{}, err
		}
	}
	return Settlement{}, nil
}

func retryableSettlementConflict(err error) bool {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return false
	}
	return postgresError.Code == "40001" || postgresError.Code == "40P01"
}

func waitForSettlementRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
