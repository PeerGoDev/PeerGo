package settler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"
)

type WorkerConfig struct {
	LeaseDuration time.Duration
	IdleInterval  time.Duration
	RetryBase     time.Duration
}

type Worker struct {
	repository WorkRepository
	config     WorkerConfig
	now        func() time.Time
	logger     *slog.Logger
}

func NewWorker(repository WorkRepository, config WorkerConfig, now func() time.Time, logger *slog.Logger) (*Worker, error) {
	if repository == nil {
		return nil, ErrInput
	}
	if config.LeaseDuration == 0 {
		config.LeaseDuration = time.Minute
	}
	if config.IdleInterval == 0 {
		config.IdleInterval = time.Second
	}
	if config.RetryBase == 0 {
		config.RetryBase = 2 * time.Second
	}
	if config.LeaseDuration < time.Second || config.LeaseDuration > 10*time.Minute ||
		config.IdleInterval < 50*time.Millisecond || config.IdleInterval > time.Minute ||
		config.RetryBase < 100*time.Millisecond || config.RetryBase > time.Minute {
		return nil, ErrInput
	}
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Worker{repository: repository, config: config, now: now, logger: logger}, nil
}

func (worker *Worker) RunOnce(ctx context.Context) (bool, error) {
	now := worker.now().UTC().Round(0)
	pending, found, err := worker.repository.ClaimNext(ctx, now, worker.config.LeaseDuration)
	if err != nil {
		return false, fmt.Errorf("claim Settlement policy work: %w", err)
	}
	if !found {
		return false, nil
	}
	if err := worker.repository.Settle(ctx, pending, now); err == nil {
		worker.logger.Debug("Settlement policy work committed", "interval_event_id", pending.IntervalEventID, "attempts", pending.Attempts)
		return true, nil
	} else if errors.Is(err, ErrPolicyCoverage) {
		if releaseErr := worker.repository.Release(ctx, pending, now.Add(worker.retryDelay(pending.Attempts)), "policy_coverage_pending"); releaseErr != nil {
			return false, fmt.Errorf("release policy work without coverage: %w", releaseErr)
		}
		worker.logger.Warn("Settlement policy coverage is not yet available; raw interval remains unbilled",
			"interval_event_id", pending.IntervalEventID, "attempts", pending.Attempts)
		return true, nil
	} else if errors.Is(err, ErrInvariant) || errors.Is(err, ErrTimelineConflict) {
		// A corrupt or ambiguous policy is an accounting safety failure. Do not
		// skip it and produce later balances from a different interpretation.
		return false, fmt.Errorf("permanent Settlement policy failure: %w", err)
	} else {
		if releaseErr := worker.repository.Release(ctx, pending, now.Add(worker.retryDelay(pending.Attempts)), "policy_settlement_failed"); releaseErr != nil {
			return false, fmt.Errorf("release failed Settlement policy work: %w", releaseErr)
		}
		worker.logger.Warn("Settlement policy work failed and will retry", "interval_event_id", pending.IntervalEventID, "attempts", pending.Attempts, "error", err)
		return true, nil
	}
}

func (worker *Worker) Run(ctx context.Context) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		processed, err := worker.RunOnce(ctx)
		if err != nil {
			return err
		}
		if processed {
			timer.Reset(0)
		} else {
			timer.Reset(worker.config.IdleInterval)
		}
	}
}

func (worker *Worker) retryDelay(attempt int32) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 6 {
		shift = 6
	}
	delay := worker.config.RetryBase * time.Duration(1<<shift)
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}
