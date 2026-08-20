package traffic

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

type hnrEnforcementFixture struct {
	result       HNREnforcementResult
	err          error
	evaluatedAt  time.Time
	batch        int
	failureCode  string
	failureAt    time.Time
	failureCalls int
}

func (fixture *hnrEnforcementFixture) EvaluateHNREnforcement(_ context.Context, at time.Time, batch int) (HNREnforcementResult, error) {
	fixture.evaluatedAt = at
	fixture.batch = batch
	return fixture.result, fixture.err
}

func (fixture *hnrEnforcementFixture) MarkHNREnforcementFailure(_ context.Context, at time.Time, code string) error {
	fixture.failureCalls++
	fixture.failureAt = at
	fixture.failureCode = code
	return nil
}

func TestHNREnforcementRunnerEvaluatesImmediately(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 17, 14, 0, 0, 0, time.UTC)
	fixture := &hnrEnforcementFixture{result: HNREnforcementResult{Examined: 2, Created: 3}}
	runner, err := NewHNREnforcementRunner(
		fixture, fixture, time.Minute, 250,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewHNREnforcementRunner() error = %v", err)
	}
	runner.runOnce(context.Background())
	if !fixture.evaluatedAt.Equal(now) || fixture.batch != 250 || fixture.failureCalls != 0 {
		t.Fatalf("fixture = %+v", fixture)
	}
}

func TestHNREnforcementRunnerRecordsFailureWithoutStopping(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 17, 14, 1, 0, 0, time.UTC)
	fixture := &hnrEnforcementFixture{err: errors.New("database unavailable")}
	runner, err := NewHNREnforcementRunner(
		fixture, fixture, time.Minute, 500,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewHNREnforcementRunner() error = %v", err)
	}
	runner.runOnce(context.Background())
	if fixture.failureCalls != 1 || fixture.failureCode != "evaluation_failed" || !fixture.failureAt.Equal(now) {
		t.Fatalf("fixture = %+v", fixture)
	}
}
