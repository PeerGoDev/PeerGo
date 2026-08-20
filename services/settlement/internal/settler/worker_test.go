package settler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestWorkerSettlesClaimedInterval(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 9, 14, 0, 0, 0, time.UTC)
	repository := &recordingWorkRepository{pending: PendingWork{IntervalEventID: uuid.New(), LeaseToken: uuid.New(), Attempts: 1}, found: true}
	worker, err := NewWorker(repository, WorkerConfig{}, func() time.Time { return now }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	processed, err := worker.RunOnce(context.Background())
	if err != nil || !processed || !repository.settledAt.Equal(now) || repository.released {
		t.Fatalf("RunOnce() processed=%v settled=%s released=%v error=%v", processed, repository.settledAt, repository.released, err)
	}
}

func TestWorkerDefersMissingPolicyInsteadOfGuessingOneX(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 9, 14, 0, 0, 0, time.UTC)
	repository := &recordingWorkRepository{
		pending: PendingWork{IntervalEventID: uuid.New(), LeaseToken: uuid.New(), Attempts: 3}, found: true, settleErr: ErrPolicyCoverage,
	}
	worker, err := NewWorker(repository, WorkerConfig{RetryBase: time.Second}, func() time.Time { return now }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	processed, err := worker.RunOnce(context.Background())
	if err != nil || !processed || !repository.released || repository.releaseCode != "policy_coverage_pending" || !repository.availableAt.Equal(now.Add(4*time.Second)) {
		t.Fatalf("RunOnce() processed=%v release=%v code=%q available=%s error=%v", processed, repository.released, repository.releaseCode, repository.availableAt, err)
	}
}

func TestWorkerStopsForPermanentPolicyInvariant(t *testing.T) {
	t.Parallel()
	repository := &recordingWorkRepository{
		pending: PendingWork{IntervalEventID: uuid.New(), LeaseToken: uuid.New(), Attempts: 1}, found: true, settleErr: ErrInvariant,
	}
	worker, err := NewWorker(repository, WorkerConfig{}, time.Now, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = worker.RunOnce(context.Background())
	if !errors.Is(err, ErrInvariant) || repository.released {
		t.Fatalf("RunOnce() error=%v released=%v", err, repository.released)
	}
}

type recordingWorkRepository struct {
	pending     PendingWork
	found       bool
	settleErr   error
	settledAt   time.Time
	released    bool
	availableAt time.Time
	releaseCode string
}

func (repository *recordingWorkRepository) ClaimNext(_ context.Context, _ time.Time, _ time.Duration) (PendingWork, bool, error) {
	return repository.pending, repository.found, nil
}

func (repository *recordingWorkRepository) Settle(_ context.Context, _ PendingWork, at time.Time) error {
	repository.settledAt = at
	return repository.settleErr
}

func (repository *recordingWorkRepository) Release(_ context.Context, _ PendingWork, at time.Time, code string) error {
	repository.released = true
	repository.availableAt = at
	repository.releaseCode = code
	return nil
}
