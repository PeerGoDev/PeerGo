package haremreward

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

type runnerRepository struct {
	results      []Settlement
	err          error
	settleCalls  int
	failureCalls int
	failureCode  string
}

func (repository *runnerRepository) SettleNext(context.Context, time.Time) (Settlement, error) {
	repository.settleCalls++
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
