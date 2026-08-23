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
	MinimumTerminalRetention = 72 * time.Hour
	MinimumSessionRetention  = 48 * time.Hour
	MinimumDetailRetention   = 30 * 24 * time.Hour
	MinimumAnomalyRetention  = 180 * 24 * time.Hour
	backlogRetryInterval     = time.Second
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
	Sessions              int64
	LegacyInbox           int64
}

func (result Result) Total() int64 {
	return result.TrafficOutbox + result.HNROutbox + result.SeedingEvidenceOutbox +
		result.PolicyWork + result.HNRWork + result.TrafficSettlements +
		result.TrafficSegments + result.SeedingSources + result.SnapshotEntries +
		result.SnapshotChunks + result.SnapshotInbox + result.SnapshotRuns +
		result.SpeedObservations + result.SeedingAnomalies + result.RawIntervals +
		result.Sessions + result.LegacyInbox
}

func (result Result) Saturated(batchSize int64) bool {
	if batchSize < 1 {
		return false
	}
	return result.TrafficOutbox >= batchSize || result.HNROutbox >= batchSize ||
		result.SeedingEvidenceOutbox >= batchSize || result.PolicyWork >= batchSize ||
		result.HNRWork >= batchSize || result.TrafficSettlements >= batchSize ||
		result.SeedingSources >= batchSize || result.SnapshotEntries >= batchSize ||
		result.SnapshotChunks >= batchSize || result.SnapshotInbox >= batchSize ||
		result.SnapshotRuns >= batchSize || result.SpeedObservations >= batchSize ||
		result.SeedingAnomalies >= batchSize || result.RawIntervals >= batchSize ||
		result.Sessions >= batchSize || result.LegacyInbox >= batchSize
}

type Repository interface {
	Cleanup(context.Context, Cutoffs, int) (Result, error)
}

type WorkerConfig struct {
	RunInterval       time.Duration
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
	if repository == nil || config.RunInterval < 10*time.Second || config.RunInterval > time.Hour ||
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
			"raw_intervals", result.RawIntervals,
			"traffic_details", result.TrafficSettlements+result.TrafficSegments,
			"snapshot_entries", result.SnapshotEntries,
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
		result, err := worker.RunOnce(ctx)
		if err != nil {
			return err
		}
		nextRun := worker.config.RunInterval
		if result.Saturated(int64(worker.config.BatchSize)) {
			// Each DELETE remains bounded by BatchSize. Only remove the idle
			// interval while a category is still saturated so a finite backlog
			// cannot grow faster merely because every successful batch waits the
			// normal steady-state polling interval.
			nextRun = backlogRetryInterval
		}
		timer.Reset(nextRun)
	}
}
