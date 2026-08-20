package seedingreward

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
)

type settlementRepositoryStub struct {
	pending      []PendingReward
	settleResult SettlementResult
	settleError  error
	released     bool
	releaseCode  string
	releaseDead  bool
}

func (stub *settlementRepositoryStub) Claim(context.Context, time.Time, int32, time.Duration) ([]PendingReward, error) {
	return append([]PendingReward(nil), stub.pending...), nil
}

func (stub *settlementRepositoryStub) Settle(context.Context, PendingReward, time.Time) (SettlementResult, error) {
	return stub.settleResult, stub.settleError
}

func (stub *settlementRepositoryStub) Release(_ context.Context, _ PendingReward, _ time.Time, code string, dead bool) error {
	stub.released, stub.releaseCode, stub.releaseDead = true, code, dead
	return nil
}

func TestWorkerCompletesClaimedReward(t *testing.T) {
	now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	item := PendingReward{WindowStart: now.Add(-time.Hour), UserID: uuid.New(), LeaseToken: uuid.New(), Attempts: 1}
	stub := &settlementRepositoryStub{
		pending: []PendingReward{item},
		settleResult: SettlementResult{
			WindowStart: item.WindowStart, UserID: item.UserID,
			PolicyRevision: "reward-v1", Reward: 10, ExperienceAmount: "1",
		},
	}
	worker, err := NewWorker(stub, WorkerConfig{Now: func() time.Time { return now }}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	processed, err := worker.RunOnce(context.Background())
	if err != nil || processed != 1 || stub.released {
		t.Fatalf("RunOnce() processed=%d released=%t error=%v", processed, stub.released, err)
	}
}

func TestWorkerKeepsMissingPolicyRetryable(t *testing.T) {
	now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	stub := &settlementRepositoryStub{
		pending:     []PendingReward{{WindowStart: now.Add(-time.Hour), UserID: uuid.New(), LeaseToken: uuid.New(), Attempts: 10}},
		settleError: ErrPolicyNotFound,
	}
	worker, _ := NewWorker(stub, WorkerConfig{MaximumAttempts: 10, Now: func() time.Time { return now }}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	processed, err := worker.RunOnce(context.Background())
	if processed != 1 || err == nil || !stub.released || stub.releaseCode != "policy_not_found" || stub.releaseDead {
		t.Fatalf("RunOnce() processed=%d released=%t code=%s dead=%t error=%v",
			processed, stub.released, stub.releaseCode, stub.releaseDead, err)
	}
}

func TestWorkerDeadLettersRepeatedInvariant(t *testing.T) {
	now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	stub := &settlementRepositoryStub{
		pending:     []PendingReward{{WindowStart: now.Add(-time.Hour), UserID: uuid.New(), LeaseToken: uuid.New(), Attempts: 3}},
		settleError: errors.Join(ErrInvariant, errors.New("broken fixture")),
	}
	worker, _ := NewWorker(stub, WorkerConfig{MaximumAttempts: 3, Now: func() time.Time { return now }}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := worker.RunOnce(context.Background())
	if err == nil || !stub.released || stub.releaseCode != "invariant_failed" || !stub.releaseDead {
		t.Fatalf("RunOnce() released=%t code=%s dead=%t error=%v",
			stub.released, stub.releaseCode, stub.releaseDead, err)
	}
}
