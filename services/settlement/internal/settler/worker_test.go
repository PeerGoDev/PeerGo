package settler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
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

func TestWorkerRunsOnlyConfiguredNumberOfLanes(t *testing.T) {
	repository := newConcurrencyProbeRepository()
	worker, err := NewWorker(repository, WorkerConfig{Concurrency: 4}, time.Now, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	for lane := 0; lane < 4; lane++ {
		select {
		case <-repository.entered:
		case <-time.After(time.Second):
			cancel()
			t.Fatalf("only %d of 4 configured lanes entered ClaimNext", lane)
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop all lanes after cancellation")
	}
	if maximum := repository.maximum(); maximum != 4 {
		t.Fatalf("maximum concurrent claims=%d, want 4", maximum)
	}
}

func TestWorkerRejectsConcurrencyOutsideBound(t *testing.T) {
	for _, concurrency := range []int{-1, 33} {
		if _, err := NewWorker(&recordingWorkRepository{}, WorkerConfig{Concurrency: concurrency}, time.Now, nil); !errors.Is(err, ErrInput) {
			t.Fatalf("NewWorker() concurrency=%d error=%v, want ErrInput", concurrency, err)
		}
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

type concurrencyProbeRepository struct {
	entered chan struct{}

	mu        sync.Mutex
	active    int
	maxActive int
}

func newConcurrencyProbeRepository() *concurrencyProbeRepository {
	return &concurrencyProbeRepository{entered: make(chan struct{}, 32)}
}

func (repository *concurrencyProbeRepository) ClaimNext(ctx context.Context, _ time.Time, _ time.Duration) (PendingWork, bool, error) {
	repository.mu.Lock()
	repository.active++
	if repository.active > repository.maxActive {
		repository.maxActive = repository.active
	}
	repository.mu.Unlock()
	repository.entered <- struct{}{}

	<-ctx.Done()
	repository.mu.Lock()
	repository.active--
	repository.mu.Unlock()
	return PendingWork{}, false, nil
}

func (*concurrencyProbeRepository) Settle(context.Context, PendingWork, time.Time) error {
	return nil
}

func (*concurrencyProbeRepository) Release(context.Context, PendingWork, time.Time, string) error {
	return nil
}

func (repository *concurrencyProbeRepository) maximum() int {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.maxActive
}
