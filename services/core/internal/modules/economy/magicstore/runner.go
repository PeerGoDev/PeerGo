package magicstore

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

const expiryBatch = 200

type RunnerRepository interface {
	DrawDueRound(context.Context, time.Time) (bool, error)
	ExpireChallenges(context.Context, time.Time, int) (int, error)
}

// Runner draws due lottery rounds and refunds expired PvP challenges. It is
// safe to run in several processes: both steps serialize in the database.
type Runner struct {
	repository RunnerRepository
	interval   time.Duration
	logger     *slog.Logger
	now        func() time.Time
}

func NewRunner(repository RunnerRepository, interval time.Duration, logger *slog.Logger, now func() time.Time) (*Runner, error) {
	if repository == nil || logger == nil || interval < 10*time.Second || interval > time.Hour {
		return nil, errors.New("magic store runner configuration is invalid")
	}
	if now == nil {
		now = time.Now
	}
	return &Runner{repository: repository, interval: interval, logger: logger, now: now}, nil
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
	// A long outage can leave at most one overdue round, because a new round
	// only opens when the previous one is drawn.
	now := runner.now().UTC().Truncate(time.Microsecond)
	drawn, err := runner.repository.DrawDueRound(ctx, now)
	if err != nil && ctx.Err() == nil {
		runner.logger.Error("magic lottery draw failed", "error", err)
	} else if drawn {
		runner.logger.Info("magic lottery round drawn")
	}
	for {
		count, err := runner.repository.ExpireChallenges(ctx, now, expiryBatch)
		if err != nil {
			if ctx.Err() == nil {
				runner.logger.Error("magic pvp expiry failed", "error", err)
			}
			return
		}
		if count > 0 {
			runner.logger.Info("magic pvp challenges expired", "count", count)
		}
		if count < expiryBatch {
			return
		}
	}
}
