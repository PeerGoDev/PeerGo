package workgroups

import (
	"context"
	"log/slog"
	"time"
)

type ContributionEnforcementEvaluator interface {
	EvaluateContributionEnforcement(context.Context, time.Time, int) (ContributionEnforcementResult, error)
}

type ContributionEnforcementFailureRecorder interface {
	MarkContributionEnforcementFailure(context.Context, time.Time, string) error
}

type ContributionEnforcementRunner struct {
	evaluator ContributionEnforcementEvaluator
	failures  ContributionEnforcementFailureRecorder
	interval  time.Duration
	batch     int
	logger    *slog.Logger
	now       func() time.Time
}

func NewContributionEnforcementRunner(evaluator ContributionEnforcementEvaluator, failures ContributionEnforcementFailureRecorder, interval time.Duration, batch int, logger *slog.Logger, now func() time.Time) (*ContributionEnforcementRunner, error) {
	if evaluator == nil || failures == nil || interval < time.Minute || interval > 24*time.Hour ||
		batch < 1 || batch > MaximumContributionEnforcementBatch || logger == nil {
		return nil, ErrInput
	}
	if now == nil {
		now = time.Now
	}
	return &ContributionEnforcementRunner{
		evaluator: evaluator, failures: failures, interval: interval,
		batch: batch, logger: logger, now: now,
	}, nil
}

func (runner *ContributionEnforcementRunner) Run(ctx context.Context) error {
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

func (runner *ContributionEnforcementRunner) runOnce(ctx context.Context) {
	now := canonicalTime(runner.now())
	result, err := runner.evaluator.EvaluateContributionEnforcement(ctx, now, runner.batch)
	if err != nil {
		_ = runner.failures.MarkContributionEnforcementFailure(ctx, now, "evaluation_failed")
		runner.logger.Error("workgroup contribution enforcement failed", "error", err)
		return
	}
	if result.Skipped {
		return
	}
	runner.logger.Info("workgroup contribution enforcement completed",
		"examined", result.Examined,
		"recorded", result.Recorded,
		"marked", result.Marked,
		"ended", result.Ended,
	)
}
