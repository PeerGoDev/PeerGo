// Package storagecleanup applies the bounded-retention policy for Tracker
// transport and detailed evidence. Compact accounting facts and user-visible
// H&R obligations are deliberately outside this package.
package storagecleanup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"
)

const (
	MinimumTerminalRetention    = 3 * time.Hour
	MinimumSessionRetention     = 12 * time.Hour
	MinimumDetailRetention      = 12 * time.Hour
	MinimumAnomalyRetention     = 30 * 24 * time.Hour
	minimumBacklogRetryInterval = 30 * time.Second
	maximumCatchupPasses        = 16
)

var (
	ErrInput     = errors.New("Settlement storage cleanup input is invalid")
	ErrInvariant = errors.New("Settlement storage cleanup invariant failed")
)

type Cutoffs struct {
	TerminalBefore time.Time
	SessionBefore  time.Time
	DetailBefore   time.Time
	AnomalyBefore  time.Time
}

type Result struct {
	SnapshotPayloads      int64
	ActivityHours         int64
	HNRCheckpoints        int64
	RawRangeQueued        int64
	TrafficOutbox         int64
	HNROutbox             int64
	SeedingEvidenceOutbox int64
	PolicyWork            int64
	HNRWork               int64
	TrafficSettlements    int64
	TrafficSegments       int64
	SeedingSources        int64
	SnapshotEntries       int64
	SnapshotChunks        int64
	SnapshotInbox         int64
	SnapshotRuns          int64
	SpeedObservations     int64
	SeedingAnomalies      int64
	RawIntervals          int64
	RawQueued             int64
	TrafficQueued         int64
	Sessions              int64
	LegacyInbox           int64
}

func (result Result) Total() int64 {
	return result.SnapshotPayloads + result.ActivityHours + result.HNRCheckpoints + result.RawRangeQueued + result.TrafficOutbox + result.HNROutbox + result.SeedingEvidenceOutbox +
		result.PolicyWork + result.HNRWork + result.TrafficSettlements +
		result.TrafficSegments + result.SeedingSources + result.SnapshotEntries +
		result.SnapshotChunks + result.SnapshotInbox + result.SnapshotRuns +
		result.SpeedObservations + result.SeedingAnomalies + result.RawIntervals +
		result.Sessions + result.LegacyInbox + result.RawQueued + result.TrafficQueued
}

func (result Result) Saturated(batchSize int64) bool {
	if batchSize < 1 {
		return false
	}
	return result.SnapshotPayloads >= 128 || result.RawRangeQueued >= batchSize || result.ActivityHours >= batchSize || result.HNRCheckpoints >= batchSize || result.TrafficOutbox >= batchSize || result.HNROutbox >= batchSize ||
		result.SeedingEvidenceOutbox >= batchSize || result.PolicyWork >= batchSize ||
		result.HNRWork >= batchSize || result.TrafficSettlements >= batchSize ||
		result.SeedingSources >= batchSize || result.SnapshotEntries >= batchSize ||
		result.SnapshotChunks >= batchSize || result.SnapshotInbox >= batchSize ||
		result.SnapshotRuns >= batchSize || result.SpeedObservations >= batchSize ||
		result.SeedingAnomalies >= batchSize || result.RawIntervals >= batchSize || result.RawQueued >= batchSize || result.TrafficQueued >= batchSize ||
		result.Sessions >= batchSize || result.LegacyInbox >= batchSize
}

type Repository interface {
	Cleanup(context.Context, Cutoffs, int) (Result, error)
}

type WorkerConfig struct {
	RunInterval       time.Duration
	CatchupBudget     time.Duration
	TerminalRetention time.Duration
	SessionRetention  time.Duration
	DetailRetention   time.Duration
	AnomalyRetention  time.Duration
	BatchSize         int
}

type Worker struct {
	repository Repository
	config     WorkerConfig
	now        func() time.Time
	logger     *slog.Logger
}

func NewWorker(repository Repository, config WorkerConfig, now func() time.Time, logger *slog.Logger) (*Worker, error) {
	if repository == nil || config.CatchupBudget < 0 || config.CatchupBudget > 30*time.Second || config.RunInterval < 10*time.Second || config.RunInterval > time.Hour ||
		config.TerminalRetention < MinimumTerminalRetention || config.TerminalRetention > 30*24*time.Hour ||
		config.SessionRetention < MinimumSessionRetention || config.SessionRetention > 30*24*time.Hour ||
		config.DetailRetention < MinimumDetailRetention || config.DetailRetention > 90*24*time.Hour ||
		config.AnomalyRetention < MinimumAnomalyRetention || config.AnomalyRetention > 365*24*time.Hour ||
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
	started := time.Now()
	now := worker.now().UTC().Truncate(time.Microsecond)
	if now.IsZero() {
		return Result{}, ErrInput
	}
	cutoffs := Cutoffs{
		TerminalBefore: now.Add(-worker.config.TerminalRetention),
		SessionBefore:  now.Add(-worker.config.SessionRetention),
		DetailBefore:   now.Add(-worker.config.DetailRetention),
		AnomalyBefore:  now.Add(-worker.config.AnomalyRetention),
	}
	result, err := worker.repository.Cleanup(ctx, cutoffs, worker.config.BatchSize)
	if err != nil {
		return result, fmt.Errorf("apply Settlement storage retention: %w", err)
	}
	if result.Total() > 0 {
		level := slog.LevelInfo
		message := "Settlement bounded storage cleanup committed"
		if result.Saturated(int64(worker.config.BatchSize)) {
			level = slog.LevelWarn
			message = "Settlement storage cleanup batch saturated; backlog remains"
		}
		worker.logger.Log(ctx, level, message,
			"rows", result.Total(),
			"batch_size", worker.config.BatchSize,
			"duration_ms", time.Since(started).Milliseconds(),
			"raw_intervals", result.RawIntervals,
			"raw_gc_queued", result.RawQueued,
			"traffic_gc_queued", result.TrafficQueued,
			"traffic_details", result.TrafficSettlements+result.TrafficSegments,
			"snapshot_entries", result.SnapshotEntries,
			"snapshot_payloads", result.SnapshotPayloads,
			"activity_hours", result.ActivityHours,
			"hnr_checkpoints", result.HNRCheckpoints,
			"snapshot_transport", result.SnapshotChunks+result.SnapshotInbox+result.SnapshotRuns,
			"legacy_inbox", result.LegacyInbox)
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
		started := time.Now()
		result, passes, err := runCatchupCycle(ctx, worker.config.CatchupBudget,
			int64(worker.config.BatchSize), time.Now, worker.RunOnce)
		if passes > 1 {
			worker.logger.InfoContext(ctx, "Settlement storage catch-up cycle yielded",
				"passes", passes, "duration_ms", time.Since(started).Milliseconds(),
				"catchup_budget", worker.config.CatchupBudget)
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		nextRun := worker.config.RunInterval
		if result.Saturated(int64(worker.config.BatchSize)) {
			// Yield after every bounded catch-up cycle, including when the pass
			// quota or time budget leaves a saturated backlog. Each pass keeps
			// the same transactions and retention guards as RunOnce.
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

// runCatchupCycle amortizes idle waits over a bounded sequence of complete
// passes. Budget is checked between passes; no transaction or retention limit
// is enlarged. Zero preserves one-pass scheduling. The quota also bounds a
// fast queue while its oldest records remain pinned by another consumer.
func runCatchupCycle(ctx context.Context, budget time.Duration, batchSize int64,
	now func() time.Time, run func(context.Context) (Result, error)) (Result, int, error) {
	started := now()
	var result Result
	for pass := 1; pass <= maximumCatchupPasses; pass++ {
		if err := ctx.Err(); err != nil {
			return result, pass - 1, err
		}
		var err error
		result, err = run(ctx)
		if err != nil {
			return result, pass, err
		}
		if !result.Saturated(batchSize) || budget == 0 || now().Sub(started) >= budget {
			return result, pass, nil
		}
	}
	return result, maximumCatchupPasses, nil
}
