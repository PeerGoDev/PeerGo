package swarmsnapshot

import (
	"path/filepath"
	"testing"
)

func TestFileSequenceStorePersistsAndAllowsExplicitEpochAdvance(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state", "swarm-sequence.json")
	store, err := OpenFileSequenceStore(path, "tracker-primary", 4)
	if err != nil {
		t.Fatal(err)
	}
	if first, err := store.Reserve(); err != nil || first != 1 {
		t.Fatalf("first=%d error=%v", first, err)
	}
	reopened, err := OpenFileSequenceStore(path, "tracker-primary", 4)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := reopened.Reserve(); err != nil || second != 2 {
		t.Fatalf("second=%d error=%v", second, err)
	}
	advanced, err := OpenFileSequenceStore(path, "tracker-primary", 5)
	if err != nil {
		t.Fatal(err)
	}
	if firstInEpoch, err := advanced.Reserve(); err != nil || firstInEpoch != 1 {
		t.Fatalf("first in new epoch=%d error=%v", firstInEpoch, err)
	}
	if _, err := OpenFileSequenceStore(path, "other-source", 5); err == nil {
		t.Fatal("source ownership change unexpectedly accepted")
	}
}
