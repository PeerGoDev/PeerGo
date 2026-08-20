package control

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileLoaderReadsVerifiedArtifactIntoStore(t *testing.T) {
	t.Parallel()
	privateKey := controlTestPrivateKey()
	store := controlTestStore(t, privateKey)
	path := filepath.Join(t.TempDir(), "control.snapshot")
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	artifact := controlTestArtifact(t, privateKey, 4, now, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := os.WriteFile(path, artifact.Bytes, 0o600); err != nil {
		t.Fatal(err)
	}
	loader, err := NewFileLoader(path, store)
	if err != nil {
		t.Fatal(err)
	}
	result, err := loader.LoadOnce(now)
	if err != nil || !result.Activated || store.Current().ControlSequence != 4 {
		t.Fatalf("LoadOnce() = %+v, %v; status=%+v", result, err, store.Current())
	}
}
