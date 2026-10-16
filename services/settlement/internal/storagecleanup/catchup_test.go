package storagecleanup

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCatchupCycleBudgetAndCaughtUp(t *testing.T) {
	for _, tc := range []struct {
		name      string
		budget    time.Duration
		exhaustAt int
		want      int
	}{
		{"disabled", 0, 20, 1}, {"time budget", 5 * time.Second, 20, 3},
		{"caught up", 30 * time.Second, 2, 2}, {"pass quota", 30 * time.Second, 20, 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := time.Unix(1, 0)
			calls := 0
			result, passes, err := runCatchupCycle(context.Background(), tc.budget, 100,
				func() time.Time { return clock }, func(context.Context) (Result, error) {
					calls++
					clock = clock.Add(2 * time.Second)
					if calls >= tc.exhaustAt {
						return Result{RawIntervals: 1}, nil
					}
					return Result{RawIntervals: 100}, nil
				})
			if err != nil || passes != tc.want || calls != tc.want {
				t.Fatalf("passes=%d calls=%d err=%v", passes, calls, err)
			}
			if calls >= tc.exhaustAt && result.Saturated(100) {
				t.Fatal("expected caught-up last result")
			}
		})
	}
}

func TestCatchupCyclePassQuotaWithFastClock(t *testing.T) {
	calls := 0
	_, passes, err := runCatchupCycle(context.Background(), 30*time.Second, 100,
		func() time.Time { return time.Unix(1, 0) }, func(context.Context) (Result, error) { calls++; return Result{RawIntervals: 100}, nil })
	if err != nil || passes != maximumCatchupPasses || calls != maximumCatchupPasses {
		t.Fatalf("passes=%d calls=%d err=%v", passes, calls, err)
	}
}

func TestCatchupCycleStopsOnCancellationAndFailure(t *testing.T) {
	want := errors.New("database failed")
	calls := 0
	_, passes, err := runCatchupCycle(context.Background(), time.Second, 100, time.Now,
		func(context.Context) (Result, error) {
			calls++
			if calls == 2 {
				return Result{}, want
			}
			return Result{RawIntervals: 100}, nil
		})
	if !errors.Is(err, want) || calls != 2 || passes != 2 {
		t.Fatalf("calls=%d passes=%d err=%v", calls, passes, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, passes, err = runCatchupCycle(ctx, time.Second, 100, time.Now, func(context.Context) (Result, error) { t.Fatal("called after cancellation"); return Result{}, nil })
	if !errors.Is(err, context.Canceled) || passes != 0 {
		t.Fatalf("passes=%d err=%v", passes, err)
	}
}
