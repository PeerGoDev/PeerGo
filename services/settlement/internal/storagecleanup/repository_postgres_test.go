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
