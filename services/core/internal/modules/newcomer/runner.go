package newcomer

import (
	"context"
	"log/slog"
	"time"
)

type FailureRecorder interface {
	MarkWorkerFailure(context.Context, time.Time, string) error
}

type Runner struct {
	evaluator Evaluator
	failures  FailureRecorder
	interval  time.Duration
	batch     int
	logger    *slog.Logger
	now       func() time.Time
}

func NewRunner(evaluator Evaluator, failures FailureRecorder, interval time.Duration, batch int, logger *slog.Logger, now func() time.Time) (*Runner, error) {
	if evaluator == nil || failures == nil || interval < 10*time.Second || interval > time.Hour || batch < 1 || batch > MaximumWorkerBatch || logger == nil {
		return nil, ErrInput
	}
	if now == nil {
		now = time.Now
	}
	return &Runner{evaluator: evaluator, failures: failures, interval: interval, batch: batch, logger: logger, now: now}, nil
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
	now := runner.now().UTC().Round(0)
	result, err := runner.evaluator.Evaluate(ctx, now, runner.batch)
	if err != nil {
		_ = runner.failures.MarkWorkerFailure(ctx, now, "evaluation_failed")
		runner.logger.Error("newcomer assessment evaluation failed", "error", err)
		return
	}
	if !result.Skipped {
		runner.logger.Info("newcomer assessment evaluation completed", "examined", result.Examined, "transitioned", result.Transitioned)
	}
}
