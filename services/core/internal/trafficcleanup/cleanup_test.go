package trafficcleanup

import (
	"context"
	"errors"
	"testing"
	"time"
)

type cleanupRepositoryStub struct {
	cutoffs   Cutoffs
	batchSize int
	result    Result
	err       error
}

func (repository *cleanupRepositoryStub) Cleanup(_ context.Context, cutoffs Cutoffs, batchSize int) (Result, error) {
	repository.cutoffs = cutoffs
	repository.batchSize = batchSize
	return repository.result, repository.err
}

func TestWorkerUsesTwelveHourDetailBoundary(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	repository := &cleanupRepositoryStub{result: Result{Inbox: 100, Entries: 100}}
	worker, err := NewWorker(repository, WorkerConfig{
		RunInterval: 15 * time.Second, DetailRetention: 12 * time.Hour,
		HistoryRetention: 30 * 24 * time.Hour, BatchSize: 100,
	}, func() time.Time { return now }, nil)
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	result, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result.Entries != 100 || repository.batchSize != 100 ||
		!repository.cutoffs.DetailBefore.Equal(now.Add(-12*time.Hour)) ||
		!repository.cutoffs.HistoryBefore.Equal(now.Add(-30*24*time.Hour)) {
		t.Fatalf("RunOnce() result = %+v, cutoffs = %+v, batch = %d", result, repository.cutoffs, repository.batchSize)
	}
}

func TestWorkerRejectsShortRetention(t *testing.T) {
	repository := &cleanupRepositoryStub{}
	_, err := NewWorker(repository, WorkerConfig{
		RunInterval: 15 * time.Second, DetailRetention: 11*time.Hour + 59*time.Minute,
		HistoryRetention: 30 * 24 * time.Hour, BatchSize: 100,
	}, time.Now, nil)
	if !errors.Is(err, ErrInput) {
		t.Fatalf("NewWorker() error = %v, want ErrInput", err)
	}
}

func TestBacklogRetryNeverRunsFasterThanConfigured(t *testing.T) {
	t.Parallel()
	if delay := backlogRetryDelay(15 * time.Second); delay != 30*time.Second {
		t.Fatalf("backlogRetryDelay(15s) = %v, want 30s", delay)
	}
	if delay := backlogRetryDelay(time.Minute); delay != time.Minute {
		t.Fatalf("backlogRetryDelay(1m) = %v, want 1m", delay)
	}
}
