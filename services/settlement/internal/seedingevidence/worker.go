package seedingevidence

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"
)

type WorkerConfig struct {
	InitialWindowStart time.Time
	ClosureDelay       time.Duration
	IdleInterval       time.Duration
}

type Worker struct {
	repository WindowBuilder
	config     WorkerConfig
	now        func() time.Time
	logger     *slog.Logger
}

func NewWorker(repository WindowBuilder, config WorkerConfig, now func() time.Time, logger *slog.Logger) (*Worker, error) {
	if repository == nil || !validWindowStart(config.InitialWindowStart) ||
		config.ClosureDelay < 0 || config.ClosureDelay > time.Hour ||
		config.IdleInterval < 100*time.Millisecond || config.IdleInterval > time.Minute {
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
	windowStart, err := worker.repository.NextWindowStart(ctx, worker.config.InitialWindowStart)
	if err != nil {
		return false, err
	}
	now := worker.now().UTC().Round(0)
	if now.Before(windowStart.Add(time.Hour).Add(worker.config.ClosureDelay)) {
		return false, nil
	}
	result, err := worker.repository.BuildHour(ctx, windowStart, now)
	if errors.Is(err, ErrCoveragePending) {
		worker.logger.Debug("seeding evidence window is waiting for source watermarks", "window_start", windowStart)
		return false, nil
	}
	if err != nil {
		return false, err
	}
	worker.logger.Info("seeding evidence window closed",
		"window_start", result.WindowStart, "items", result.ItemCount,
		"announce_fence_sequence", result.AnnounceFenceSequence,
		"snapshot_sequence", result.SelectedSnapshotSequence,
		"duplicate", result.Duplicate)
	return true, nil
}

func (worker *Worker) Run(ctx context.Context) error {
	for {
		processed, err := worker.RunOnce(ctx)
		if err != nil {
			return err
		}
		if processed {
			continue
		}
		timer := time.NewTimer(worker.config.IdleInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
