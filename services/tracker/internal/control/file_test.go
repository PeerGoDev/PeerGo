package control

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadArtifactFileReadsRegularFileAndRejectsSymlink(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "control.snapshot")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	encoded, err := ReadArtifactFile(path)
	if err != nil || string(encoded) != "{}" {
		t.Fatalf("ReadArtifactFile() = %q, %v", encoded, err)
	}
	linkPath := filepath.Join(directory, "link.snapshot")
	if err := os.Symlink(path, linkPath); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadArtifactFile(linkPath); !errors.Is(err, ErrSnapshotFileUnsafe) {
		t.Fatalf("symlink error = %v", err)
	}
}
