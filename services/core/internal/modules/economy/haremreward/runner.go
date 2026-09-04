package haremreward

import (
	"context"
	"log/slog"
	"time"
)

type Runner struct {
	repository Repository
	interval   time.Duration
	batch      int
	logger     *slog.Logger
	now        func() time.Time
}

func NewRunner(repository Repository, interval time.Duration, batch int, logger *slog.Logger, now func() time.Time) (*Runner, error) {
	if repository == nil || interval < time.Minute || interval > time.Hour ||
		batch < 1 || batch > MaximumSettlementBatch || logger == nil {
		return nil, ErrInput
	}
	if now == nil {
		now = time.Now
	}
	return &Runner{repository: repository, interval: interval, batch: batch, logger: logger, now: now}, nil
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
		result, err := runner.repository.SettleNext(ctx, now)
		if err != nil {
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
