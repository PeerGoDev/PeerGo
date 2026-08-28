// Package trafficcleanup bounds Core's per-settlement replay and worker
// evidence. User totals remain durable; compact three-hour history is retained
// for a bounded user-visible window.
package trafficcleanup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"
)

const (
	MinimumDetailRetention      = 12 * time.Hour
	MinimumHistoryRetention     = 30 * 24 * time.Hour
	NetworkObservationRetention = 180 * 24 * time.Hour
	minimumBacklogRetryInterval = 30 * time.Second
)

var (
	ErrInput     = errors.New("Core traffic cleanup input is invalid")
	ErrInvariant = errors.New("Core traffic cleanup invariant failed")
)

type Result struct {
	Inbox               int64
	Entries             int64
	Explanations        int64
	Segments            int64
	Rollups             int64
	NetworkObservations int64
}

func (result Result) Total() int64 {
	return result.Inbox + result.Entries + result.Explanations + result.Segments + result.Rollups + result.NetworkObservations
}

func (result Result) Saturated(batchSize int64) bool {
	return batchSize > 0 && (result.Entries >= batchSize || result.Rollups >= batchSize || result.NetworkObservations >= batchSize)
}

type Cutoffs struct {
	DetailBefore  time.Time
	HistoryBefore time.Time
	NetworkBefore time.Time
}

type Repository interface {
	Cleanup(context.Context, Cutoffs, int) (Result, error)
}

type WorkerConfig struct {
	RunInterval      time.Duration
	DetailRetention  time.Duration
	HistoryRetention time.Duration
	BatchSize        int
}

type Worker struct {
	repository Repository
	config     WorkerConfig
	now        func() time.Time
	logger     *slog.Logger
}

func NewWorker(repository Repository, config WorkerConfig, now func() time.Time, logger *slog.Logger) (*Worker, error) {
	if repository == nil || config.RunInterval < 10*time.Second || config.RunInterval > time.Hour ||
		config.DetailRetention < MinimumDetailRetention || config.DetailRetention > 7*24*time.Hour ||
		config.HistoryRetention < MinimumHistoryRetention || config.HistoryRetention > 365*24*time.Hour ||
		config.BatchSize < 100 || config.BatchSize > 10_000 {
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

func (worker *Worker) RunOnce(ctx context.Context) (Result, error) {
	now := worker.now().UTC().Truncate(time.Microsecond)
	if now.IsZero() {
		return Result{}, ErrInput
	}
	cutoffs := Cutoffs{
		DetailBefore:  now.Add(-worker.config.DetailRetention),
		HistoryBefore: now.Add(-worker.config.HistoryRetention),
		NetworkBefore: now.Add(-NetworkObservationRetention),
	}
	result, err := worker.repository.Cleanup(ctx, cutoffs, worker.config.BatchSize)
	if err != nil {
		return result, fmt.Errorf("apply Core traffic storage retention: %w", err)
	}
	if result.Total() > 0 {
		level := slog.LevelInfo
		message := "Core bounded traffic cleanup committed"
		if result.Saturated(int64(worker.config.BatchSize)) {
			level = slog.LevelWarn
			message = "Core traffic cleanup batch saturated; backlog remains"
		}
		worker.logger.Log(ctx, level, message,
			"rows", result.Total(), "batch_size", worker.config.BatchSize,
			"entries", result.Entries, "segments", result.Segments, "rollups", result.Rollups,
			"network_observations", result.NetworkObservations)
	}
	return result, nil
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
		result, err := worker.RunOnce(ctx)
		if err != nil {
			return err
		}
		nextRun := worker.config.RunInterval
		if result.Saturated(int64(worker.config.BatchSize)) {
			nextRun = backlogRetryDelay(worker.config.RunInterval)
		}
		timer.Reset(nextRun)
	}
}

func backlogRetryDelay(runInterval time.Duration) time.Duration {
	if runInterval < minimumBacklogRetryInterval {
		return minimumBacklogRetryInterval
	}
	return runInterval
}
