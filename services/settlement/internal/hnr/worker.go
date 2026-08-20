package hnr

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
		return false, fmt.Errorf("claim H&R work: %w", err)
	}
	if !found {
		return false, nil
	}
	if err := worker.repository.Process(ctx, pending, now); err == nil {
		worker.logger.Debug("H&R work committed", "interval_event_id", pending.IntervalEventID, "attempts", pending.Attempts)
		return true, nil
	} else if errors.Is(err, ErrPolicyCoverage) {
		if releaseErr := worker.repository.Release(ctx, pending, now.Add(worker.retryDelay(pending.Attempts)), "hnr_policy_coverage_pending"); releaseErr != nil {
			return false, fmt.Errorf("release H&R work without policy coverage: %w", releaseErr)
		}
		worker.logger.Warn("H&R policy coverage is not yet available; completion remains undecided",
			"interval_event_id", pending.IntervalEventID, "attempts", pending.Attempts)
		return true, nil
	} else if errors.Is(err, ErrInvariant) || errors.Is(err, ErrTimelineConflict) {
		// Ambiguous policy or corrupt immutable evidence is an accounting safety
		// failure. Continuing could make later obligations depend on a different
		// interpretation, so fail closed and require operator reconciliation.
		return false, fmt.Errorf("permanent H&R failure: %w", err)
	} else {
		if releaseErr := worker.repository.Release(ctx, pending, now.Add(worker.retryDelay(pending.Attempts)), "hnr_processing_failed"); releaseErr != nil {
			return false, fmt.Errorf("release failed H&R work: %w", releaseErr)
		}
		worker.logger.Warn("H&R work failed and will retry", "interval_event_id", pending.IntervalEventID,
			"attempts", pending.Attempts, "error", err)
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
