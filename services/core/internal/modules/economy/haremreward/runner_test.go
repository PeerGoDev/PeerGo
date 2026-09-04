package haremreward

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type runnerRepository struct {
	results      []Settlement
	errors       []error
	err          error
	settleCalls  int
	failureCalls int
	failureCode  string
}

func (repository *runnerRepository) SettleNext(context.Context, time.Time) (Settlement, error) {
	repository.settleCalls++
	if len(repository.errors) > 0 {
		err := repository.errors[0]
		repository.errors = repository.errors[1:]
		if err != nil {
			return Settlement{}, err
		}
	}
	if repository.err != nil {
		return Settlement{}, repository.err
	}
	if len(repository.results) == 0 {
		return Settlement{}, nil
	}
	result := repository.results[0]
	repository.results = repository.results[1:]
	return result, nil
}

func TestRunnerRetriesSerializableConflictWithoutMarkingFailure(t *testing.T) {
	repository := &runnerRepository{
		errors: []error{
			&pgconn.PgError{Code: "40001"},
			fmt.Errorf("wrapped deadlock: %w", &pgconn.PgError{Code: "40P01"}),
			nil,
		},
		results: []Settlement{{Processed: true}, {Processed: false}},
	}
	runner, err := NewRunner(
		repository, time.Minute, 2,
		slog.New(slog.NewTextHandler(io.Discard, nil)), time.Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	var waits []time.Duration
	runner.retryWait = func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	}

	runner.runOnce(context.Background())

	if repository.settleCalls != 4 {
		t.Fatalf("settlement calls = %d, want 4", repository.settleCalls)
	}
	if repository.failureCalls != 0 {
		t.Fatalf("failure calls = %d, want 0", repository.failureCalls)
	}
	if len(waits) != 2 || waits[0] != 100*time.Millisecond || waits[1] != 200*time.Millisecond {
		t.Fatalf("retry waits = %v, want [100ms 200ms]", waits)
	}
}

func TestRunnerExhaustsRetryableConflictsBeforeMarkingFailure(t *testing.T) {
	repository := &runnerRepository{err: &pgconn.PgError{Code: "40001"}}
	runner, err := NewRunner(
		repository, time.Minute, 1,
		slog.New(slog.NewTextHandler(io.Discard, nil)), time.Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	runner.retryWait = func(context.Context, time.Duration) error { return nil }

	runner.runOnce(context.Background())

	if repository.settleCalls != maximumSettlementAttempts || repository.failureCalls != 1 {
		t.Fatalf(
			"calls settle=%d failure=%d, want %d and 1",
			repository.settleCalls, repository.failureCalls, maximumSettlementAttempts,
		)
	}
}

func TestRetryableSettlementConflictRejectsOtherErrors(t *testing.T) {
	if retryableSettlementConflict(errors.New("database unavailable")) {
		t.Fatal("plain database error must not be retried")
	}
	if retryableSettlementConflict(&pgconn.PgError{Code: "23505"}) {
		t.Fatal("unique violation must not be retried")
	}
}

func (repository *runnerRepository) MarkFailure(_ context.Context, _ time.Time, code string) error {
	repository.failureCalls++
	repository.failureCode = code
	return nil
}

func TestRunnerDrainsCompletedWindowsUntilIdle(t *testing.T) {
	repository := &runnerRepository{results: []Settlement{
		{Processed: true},
		{Processed: true},
		{Processed: false},
	}}
	runner, err := NewRunner(
		repository, 10*time.Minute, 96,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		func() time.Time { return time.Date(2026, 9, 4, 6, 0, 0, 0, time.UTC) },
	)
	if err != nil {
		t.Fatal(err)
	}

	runner.runOnce(context.Background())

	if repository.settleCalls != 3 {
		t.Fatalf("settlement calls = %d, want 3", repository.settleCalls)
	}
	if repository.failureCalls != 0 {
		t.Fatalf("failure calls = %d, want 0", repository.failureCalls)
	}
}

func TestRunnerStopsAndMarksFailure(t *testing.T) {
	repository := &runnerRepository{err: errors.New("database unavailable")}
	runner, err := NewRunner(
		repository, time.Minute, 1,
		slog.New(slog.NewTextHandler(io.Discard, nil)), time.Now,
	)
	if err != nil {
		t.Fatal(err)
	}

	runner.runOnce(context.Background())

	if repository.settleCalls != 1 || repository.failureCalls != 1 ||
		repository.failureCode != "settlement_failed" {
		t.Fatalf(
			"calls settle=%d failure=%d code=%q",
			repository.settleCalls, repository.failureCalls, repository.failureCode,
		)
	}
}

func TestNewRunnerRejectsUnsafeBatch(t *testing.T) {
	repository := &runnerRepository{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := NewRunner(repository, time.Minute, MaximumSettlementBatch+1, logger, time.Now); !errors.Is(err, ErrInput) {
		t.Fatalf("NewRunner() error = %v, want ErrInput", err)
	}
}
