package seedingevidence

import (
	"context"
	"errors"
	"testing"
	"time"
)

type recordingWindowBuilder struct {
	next       time.Time
	result     BuildResult
	buildErr   error
	buildCalls int
}

func (builder *recordingWindowBuilder) NextWindowStart(context.Context, time.Time) (time.Time, error) {
	return builder.next, nil
}

func (builder *recordingWindowBuilder) BuildHour(context.Context, time.Time, time.Time) (BuildResult, error) {
	builder.buildCalls++
	return builder.result, builder.buildErr
}

func TestWorkerWaitsUntilClosureDelay(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	repository := &recordingWindowBuilder{next: start}
	worker, err := NewWorker(repository, WorkerConfig{
		InitialWindowStart: start, ClosureDelay: 5 * time.Minute, IdleInterval: time.Second,
	}, func() time.Time { return start.Add(time.Hour + 4*time.Minute) }, nil)
	if err != nil {
		t.Fatal(err)
	}
	processed, err := worker.RunOnce(context.Background())
	if err != nil || processed || repository.buildCalls != 0 {
		t.Fatalf("RunOnce() processed=%v calls=%d error=%v", processed, repository.buildCalls, err)
	}
}

func TestWorkerTreatsMissingWatermarkAsPending(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	repository := &recordingWindowBuilder{next: start, buildErr: ErrCoveragePending}
	worker, err := NewWorker(repository, WorkerConfig{
		InitialWindowStart: start, ClosureDelay: time.Minute, IdleInterval: time.Second,
	}, func() time.Time { return start.Add(2 * time.Hour) }, nil)
	if err != nil {
		t.Fatal(err)
	}
	processed, err := worker.RunOnce(context.Background())
	if err != nil || processed || repository.buildCalls != 1 {
		t.Fatalf("RunOnce() processed=%v calls=%d error=%v", processed, repository.buildCalls, err)
	}
}

func TestWorkerFailsClosedOnEvidenceDrift(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	repository := &recordingWindowBuilder{next: start, buildErr: ErrEvidenceDrift}
	worker, err := NewWorker(repository, WorkerConfig{
		InitialWindowStart: start, ClosureDelay: time.Minute, IdleInterval: time.Second,
	}, func() time.Time { return start.Add(2 * time.Hour) }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RunOnce(context.Background()); !errors.Is(err, ErrEvidenceDrift) {
		t.Fatalf("RunOnce() error=%v", err)
	}
}
