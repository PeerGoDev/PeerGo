package storagecleanup

import (
	"context"
	"errors"
	"testing"
	"time"
)

type recordingRepository struct {
	cutoffs Cutoffs
	batch   int
	result  Result
	err     error
}

func (repository *recordingRepository) Cleanup(_ context.Context, cutoffs Cutoffs, batch int) (Result, error) {
	repository.cutoffs = cutoffs
	repository.batch = batch
	return repository.result, repository.err
}

func TestWorkerComputesIndependentRetentionCutoffs(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 30, 45, 123456789, time.FixedZone("test", 8*60*60))
	repository := &recordingRepository{result: Result{RawIntervals: 3}}
	worker, err := NewWorker(repository, WorkerConfig{
		RunInterval: time.Minute, TerminalRetention: MinimumTerminalRetention,
		SessionRetention: MinimumSessionRetention, DetailRetention: MinimumDetailRetention,
		AnomalyRetention: MinimumAnomalyRetention, BatchSize: 1000,
	}, func() time.Time { return now }, nil)
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	result, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	canonicalNow := now.UTC().Truncate(time.Microsecond)
	if result.RawIntervals != 3 || repository.batch != 1000 ||
		!repository.cutoffs.TerminalBefore.Equal(canonicalNow.Add(-MinimumTerminalRetention)) ||
		!repository.cutoffs.SessionBefore.Equal(canonicalNow.Add(-MinimumSessionRetention)) ||
		!repository.cutoffs.DetailBefore.Equal(canonicalNow.Add(-MinimumDetailRetention)) ||
		!repository.cutoffs.AnomalyBefore.Equal(canonicalNow.Add(-MinimumAnomalyRetention)) {
		t.Fatalf("unexpected cleanup call: result=%+v batch=%d cutoffs=%+v", result, repository.batch, repository.cutoffs)
	}
}

func TestWorkerRejectsRetentionBelowDatabaseGuard(t *testing.T) {
	_, err := NewWorker(&recordingRepository{}, WorkerConfig{
		RunInterval: time.Minute, TerminalRetention: MinimumTerminalRetention - time.Second,
		SessionRetention: MinimumSessionRetention, DetailRetention: MinimumDetailRetention,
		AnomalyRetention: MinimumAnomalyRetention, BatchSize: 1000,
	}, time.Now, nil)
	if !errors.Is(err, ErrInput) {
		t.Fatalf("NewWorker() error = %v, want ErrInput", err)
	}
}

func TestWorkerPropagatesRepositoryFailure(t *testing.T) {
	repository := &recordingRepository{err: errors.New("database unavailable")}
	worker, err := NewWorker(repository, WorkerConfig{
		RunInterval: time.Minute, TerminalRetention: MinimumTerminalRetention,
		SessionRetention: MinimumSessionRetention, DetailRetention: MinimumDetailRetention,
		AnomalyRetention: MinimumAnomalyRetention, BatchSize: 1000,
	}, time.Now, nil)
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	if _, err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce() error = nil, want repository failure")
	}
}

func TestResultReportsPerCategorySaturation(t *testing.T) {
	t.Parallel()
	if !(Result{SnapshotEntries: 1000}).Saturated(1000) {
		t.Fatal("exact batch limit should report saturation")
	}
	if (Result{SnapshotEntries: 999, RawIntervals: 999}).Saturated(1000) {
		t.Fatal("totals across categories must not masquerade as one saturated query")
	}
}
