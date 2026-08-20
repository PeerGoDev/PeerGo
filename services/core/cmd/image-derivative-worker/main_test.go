package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/peergo/peergo/services/core/internal/modules/imaging"
)

type retryingProcessor struct {
	processed atomic.Int64
	calls     atomic.Int64
}

func (processor *retryingProcessor) ProcessNext(context.Context) (bool, error) {
	call := processor.calls.Add(1)
	if call == 1 {
		return false, nil
	}
	if call == 2 {
		processor.processed.Add(1)
		return true, nil
	}
	return false, nil
}

type retryingOverview struct {
	processor *retryingProcessor
}

func (overview retryingOverview) Overview(context.Context) (imaging.QueueOverview, error) {
	if overview.processor.processed.Load() == 0 {
		return imaging.QueueOverview{Retrying: 1}, nil
	}
	return imaging.QueueOverview{Ready: 1}, nil
}

type idleProcessor struct{}

func (idleProcessor) ProcessNext(context.Context) (bool, error) { return false, nil }

type deadOverview struct{}

func (deadOverview) Overview(context.Context) (imaging.QueueOverview, error) {
	return imaging.QueueOverview{Dead: 1}, nil
}

func TestDrainEligibleWaitsForRetryBackoff(t *testing.T) {
	t.Parallel()
	processor := &retryingProcessor{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := drainEligible(ctx, processor, retryingOverview{processor: processor}, 1, time.Millisecond, logger); err != nil {
		t.Fatalf("drain delayed derivative: %v", err)
	}
	if got := processor.processed.Load(); got != 1 {
		t.Fatalf("processed = %d, want 1", got)
	}
}

func TestDrainEligibleRejectsDeadWork(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := drainEligible(context.Background(), idleProcessor{}, deadOverview{}, 2, time.Millisecond, logger)
	if err == nil || !strings.Contains(err.Error(), "dead work") {
		t.Fatalf("error = %v, want dead work", err)
	}
}
