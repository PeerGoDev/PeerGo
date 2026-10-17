package storagecleanup

import (
	"context"
	"errors"
	"testing"
)

func TestDrainSnapshotEntryBacklogUsesBoundedPasses(t *testing.T) {
	t.Parallel()
	calls := 0
	rows, err := drainSnapshotEntryBacklog(context.Background(), func(context.Context) (int64, error) {
		calls++
		return 5_000, nil
	})
	if err != nil {
		t.Fatalf("drainSnapshotEntryBacklog() error = %v", err)
	}
	if calls != snapshotEntryCleanupPassLimit || rows != 20_000 {
		t.Fatalf("drainSnapshotEntryBacklog() calls=%d rows=%d", calls, rows)
	}
}

func TestDrainSnapshotEntryBacklogStopsWhenCaughtUp(t *testing.T) {
	t.Parallel()
	batches := []int64{2_270, 5_000, 0, 5_000}
	calls := 0
	rows, err := drainSnapshotEntryBacklog(context.Background(), func(context.Context) (int64, error) {
		rows := batches[calls]
		calls++
		return rows, nil
	})
	if err != nil {
		t.Fatalf("drainSnapshotEntryBacklog() error = %v", err)
	}
	if calls != 3 || rows != 7_270 {
		t.Fatalf("drainSnapshotEntryBacklog() calls=%d rows=%d", calls, rows)
	}
}

func TestDrainSnapshotEntryBacklogReturnsCommittedRowsWithFailure(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("database unavailable")
	calls := 0
	rows, err := drainSnapshotEntryBacklog(context.Background(), func(context.Context) (int64, error) {
		calls++
		if calls == 2 {
			return 0, wantErr
		}
		return 5_000, nil
	})
	if !errors.Is(err, wantErr) || calls != 2 || rows != 5_000 {
		t.Fatalf("drainSnapshotEntryBacklog() calls=%d rows=%d error=%v", calls, rows, err)
	}
}

func TestDetailBacklogKeepsCommittedProgressOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	n, err := drainDetailBacklog(ctx, func(context.Context) (int64, error) {
		calls++
		cancel()
		return 100, nil
	})
	if n != 100 || calls != 1 || !errors.Is(err, context.Canceled) {
		t.Fatalf("rows=%d calls=%d err=%v", n, calls, err)
	}
}

func TestDetailBacklogStopsAfterFailureOrNoProgress(t *testing.T) {
	failure := errors.New("transaction rolled back")
	for _, secondErr := range []error{nil, failure} {
		calls := 0
		n, err := drainDetailBacklog(context.Background(), func(context.Context) (int64, error) {
			calls++
			if calls == 1 {
				return 100, nil
			}
			return 0, secondErr
		})
		if n != 100 || calls != 2 || !errors.Is(err, secondErr) {
			t.Fatalf("rows=%d calls=%d err=%v", n, calls, err)
		}
	}
}

func TestDetailBacklogYieldsDespiteRemainingWork(t *testing.T) {
	calls := 0
	n, err := drainDetailBacklog(context.Background(), func(context.Context) (int64, error) {
		calls++
		return 100, nil
	})
	if n != 400 || calls != 4 || err != nil {
		t.Fatalf("rows=%d calls=%d err=%v", n, calls, err)
	}
}
