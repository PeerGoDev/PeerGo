package hnr

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestWorkerProcessesClaimedInterval(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 10, 8, 0, 0, 0, time.UTC)
	repository := &recordingRepository{pending: PendingWork{IntervalEventID: uuid.New(), LeaseToken: uuid.New(), Attempts: 1}, found: true}
	worker, err := NewWorker(repository, WorkerConfig{}, func() time.Time { return now }, nil)
	if err != nil {
		t.Fatal(err)
	}
	processed, err := worker.RunOnce(context.Background())
	if err != nil || !processed || !repository.processedAt.Equal(now) || repository.released {
		t.Fatalf("RunOnce() processed=%v at=%s released=%v error=%v", processed, repository.processedAt, repository.released, err)
	}
}

func TestWorkerDefersCompletionWithoutPolicyCoverage(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 10, 8, 0, 0, 0, time.UTC)
	repository := &recordingRepository{
		pending: PendingWork{IntervalEventID: uuid.New(), LeaseToken: uuid.New(), Attempts: 3},
		found:   true, processErr: ErrPolicyCoverage,
	}
	worker, err := NewWorker(repository, WorkerConfig{RetryBase: time.Second}, func() time.Time { return now }, nil)
	if err != nil {
		t.Fatal(err)
	}
	processed, err := worker.RunOnce(context.Background())
	if err != nil || !processed || !repository.released || repository.releaseCode != "hnr_policy_coverage_pending" ||
		!repository.availableAt.Equal(now.Add(4*time.Second)) {
		t.Fatalf("RunOnce() processed=%v release=%v code=%q available=%s error=%v",
			processed, repository.released, repository.releaseCode, repository.availableAt, err)
	}
}

func TestWorkerStopsOnAmbiguousTimeline(t *testing.T) {
	t.Parallel()
	repository := &recordingRepository{
		pending: PendingWork{IntervalEventID: uuid.New(), LeaseToken: uuid.New(), Attempts: 1},
		found:   true, processErr: ErrTimelineConflict,
	}
	worker, err := NewWorker(repository, WorkerConfig{}, time.Now, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = worker.RunOnce(context.Background())
	if !errors.Is(err, ErrTimelineConflict) || repository.released {
		t.Fatalf("RunOnce() error=%v released=%v", err, repository.released)
	}
}

type recordingRepository struct {
	pending     PendingWork
	found       bool
	processErr  error
	processedAt time.Time
	released    bool
	availableAt time.Time
	releaseCode string
}

func (repository *recordingRepository) ClaimNext(_ context.Context, _ time.Time, _ time.Duration) (PendingWork, bool, error) {
	return repository.pending, repository.found, nil
}

func (repository *recordingRepository) Process(_ context.Context, _ PendingWork, at time.Time) error {
	repository.processedAt = at
	return repository.processErr
}

func (repository *recordingRepository) Release(_ context.Context, _ PendingWork, at time.Time, code string) error {
	repository.released = true
	repository.availableAt = at
	repository.releaseCode = code
	return nil
}
