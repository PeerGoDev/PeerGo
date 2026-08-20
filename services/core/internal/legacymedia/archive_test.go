package legacymedia

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSourceArchiveAcceptsUploadsAndCombinedBundle(t *testing.T) {
	t.Parallel()
	archivePath := filepath.Join(t.TempDir(), "assets.zip")
	writeTestArchive(t, archivePath, map[string][]byte{
		"uploads/images/aa/aa000000-0000-4000-8000-000000000001.jpg": {0xff, 0xd8, 0xff, 0xd9},
		"torrents/bb/bb000000-0000-4000-8000-000000000002.torrent":   []byte("d4:infode"),
	})

	archive, err := OpenSourceArchive(archivePath)
	if err != nil {
		t.Fatalf("OpenSourceArchive() error = %v", err)
	}
	defer archive.Close()
	if archive.Inspection().ImageCount != 1 || archive.Inspection().ByteLength < 1 ||
		archive.Inspection().SHA256 == ([32]byte{}) {
		t.Fatalf("inspection = %+v", archive.Inspection())
	}
	raw, extension, err := archive.Read(
		context.Background(),
		"/uploads/images/aa/aa000000-0000-4000-8000-000000000001.jpg",
	)
	if err != nil || extension != ".jpg" || len(raw) != 4 {
		t.Fatalf("Read() bytes=%d extension=%q error=%v", len(raw), extension, err)
	}
}

func TestSourceArchiveRejectsUnsafeAndUnexpectedEntries(t *testing.T) {
	t.Parallel()
	for name, entry := range map[string]string{
		"traversal":   "../escape.jpg",
		"wrong-shard": "uploads/images/aa/bb000000-0000-4000-8000-000000000001.jpg",
		"unrelated":   "uploads/readme.txt",
	} {
		t.Run(name, func(t *testing.T) {
			archivePath := filepath.Join(t.TempDir(), "uploads.zip")
			writeTestArchive(t, archivePath, map[string][]byte{entry: []byte("bytes")})
			if _, err := OpenSourceArchive(archivePath); err == nil {
				t.Fatal("OpenSourceArchive() unexpectedly accepted unsafe archive")
			}
		})
	}
}

func writeTestArchive(t *testing.T, name string, entries map[string][]byte) {
	t.Helper()
	file, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for entryName, contents := range entries {
		entry, err := writer.Create(entryName)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
