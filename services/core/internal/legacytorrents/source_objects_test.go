package legacytorrents

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

func TestSourceObjectRootReadsOnlyExpectedRegularObject(t *testing.T) {
	t.Parallel()

	rootPath := t.TempDir()
	publicID := uuid.MustParse("11111111-2222-4333-8444-555555555555")
	directory := filepath.Join(rootPath, "11")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	expected := []byte("d4:infodee")
	if err := os.WriteFile(filepath.Join(directory, publicID.String()+".torrent"), expected, 0o600); err != nil {
		t.Fatal(err)
	}

	root, err := openSourceObjectRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := root.read(publicID)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(expected) {
		t.Fatalf("read object = %q, want %q", actual, expected)
	}

	_, err = root.read(uuid.MustParse("22222222-2222-4333-8444-555555555555"))
	if sourceObjectErrorCode(err) != "object_missing" {
		t.Fatalf("missing object error = %v", err)
	}
}

func TestSourceObjectRootRejectsSymlinkedObject(t *testing.T) {
	t.Parallel()

	rootPath := t.TempDir()
	publicID := uuid.MustParse("aaaaaaaa-2222-4333-8444-555555555555")
	directory := filepath.Join(rootPath, "aa")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(rootPath, "target.torrent")
	if err := os.WriteFile(target, []byte("d4:infodee"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(directory, publicID.String()+".torrent")); err != nil {
		t.Fatal(err)
	}
	root, err := openSourceObjectRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = root.read(publicID)
	if sourceObjectErrorCode(err) != "invalid_object_file" {
		t.Fatalf("symlinked object error = %v", err)
	}
}

func TestSourceObjectRootReadsStrictZipSnapshot(t *testing.T) {
	t.Parallel()

	publicID := uuid.MustParse("11111111-2222-4333-8444-555555555555")
	expected := []byte("d4:infodee")
	archivePath := writeSourceObjectArchive(t, []sourceArchiveEntry{
		{name: "torrents/", mode: os.ModeDir | 0o700},
		{name: "torrents/11/", mode: os.ModeDir | 0o700},
		{name: "torrents/11/" + publicID.String() + ".torrent", mode: 0o600, content: expected},
	})
	root, err := openSourceObjectRoot(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.close() })
	actual, err := root.read(publicID)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(expected) {
		t.Fatalf("read ZIP object = %q, want %q", actual, expected)
	}
	_, err = root.read(uuid.MustParse("22222222-2222-4333-8444-555555555555"))
	if sourceObjectErrorCode(err) != "object_missing" {
		t.Fatalf("missing ZIP object error = %v", err)
	}
}

func TestSourceObjectRootReadsTorrentFromCombinedAssetSnapshot(t *testing.T) {
	t.Parallel()

	publicID := uuid.MustParse("11111111-2222-4333-8444-555555555555")
	imageID := uuid.MustParse("aaaaaaaa-2222-4333-8444-555555555555")
	expected := []byte("d4:infodee")
	archivePath := writeSourceObjectArchive(t, []sourceArchiveEntry{
		{name: "torrents/11/" + publicID.String() + ".torrent", mode: 0o600, content: expected},
		{name: "uploads/images/aa/" + imageID.String() + ".jpg", mode: 0o600, content: []byte("image")},
	})
	root, err := openSourceObjectRoot(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.close() })
	actual, err := root.read(publicID)
	if err != nil || !bytes.Equal(actual, expected) {
		t.Fatalf("read combined ZIP torrent = %q, %v", actual, err)
	}
}

func TestSourceObjectRootRejectsUnsafeZipSnapshots(t *testing.T) {
	t.Parallel()

	publicID := "11111111-2222-4333-8444-555555555555"
	tests := map[string][]sourceArchiveEntry{
		"path traversal":  {{name: "torrents/../11/" + publicID + ".torrent", mode: 0o600, content: []byte("x")}},
		"symbolic link":   {{name: "torrents/11/" + publicID + ".torrent", mode: os.ModeSymlink | 0o777, content: []byte("target")}},
		"wrong prefix":    {{name: "torrents/22/" + publicID + ".torrent", mode: 0o600, content: []byte("x")}},
		"unexpected file": {{name: "torrents/11/notes.txt", mode: 0o600, content: []byte("x")}},
	}
	for name, entries := range tests {
		name, entries := name, entries
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			archivePath := writeSourceObjectArchive(t, entries)
			root, err := openSourceObjectRoot(archivePath)
			if root != nil {
				_ = root.close()
			}
			if err == nil {
				t.Fatal("unsafe ZIP source was accepted")
			}
		})
	}
}

func TestSourceObjectRootRecoversUniqueRenamedArchiveObjectByIdentity(t *testing.T) {
	t.Parallel()

	expectedID := uuid.MustParse("11111111-2222-4333-8444-555555555555")
	archivedID := uuid.MustParse("22222222-2222-4333-8444-555555555555")
	raw := []byte("d4:infod6:lengthi3e4:name5:a.bin12:piece lengthi16384e6:pieces20:aaaaaaaaaaaaaaaaaaaa7:privatei1eee")
	parsed, err := torrents.InspectLegacyV1OrHybrid(raw)
	if err != nil {
		t.Fatal(err)
	}
	archivePath := writeSourceObjectArchive(t, []sourceArchiveEntry{{
		name: "torrents/22/" + archivedID.String() + ".torrent", mode: 0o600, content: raw,
	}})
	root, err := openSourceObjectRoot(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.close() })
	recovery, err := root.resolveArchiveObjects([]expectedSourceObject{{
		legacyID: 7, publicID: expectedID, infoHash: parsed.InfoHashV1, size: parsed.TotalSizeBytes,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if recovery.RecoveredObjects != 1 || recovery.UnreferencedObjects != 1 ||
		len(recovery.RecoveredLegacyIDs) != 1 || recovery.RecoveredLegacyIDs[0] != 7 {
		t.Fatalf("recovery = %+v", recovery)
	}
	actual, err := root.read(expectedID)
	if err != nil || string(actual) != string(raw) {
		t.Fatalf("recovered read = %q, %v", actual, err)
	}
}

func TestSourceObjectRootDoesNotGuessBetweenAmbiguousRecoveryCandidates(t *testing.T) {
	t.Parallel()

	expectedID := uuid.MustParse("11111111-2222-4333-8444-555555555555")
	firstID := uuid.MustParse("22222222-2222-4333-8444-555555555555")
	secondID := uuid.MustParse("33333333-2222-4333-8444-555555555555")
	raw := []byte("d4:infod6:lengthi3e4:name5:a.bin12:piece lengthi16384e6:pieces20:aaaaaaaaaaaaaaaaaaaa7:privatei1eee")
	parsed, err := torrents.InspectLegacyV1OrHybrid(raw)
	if err != nil {
		t.Fatal(err)
	}
	archivePath := writeSourceObjectArchive(t, []sourceArchiveEntry{
		{name: "torrents/22/" + firstID.String() + ".torrent", mode: 0o600, content: raw},
		{name: "torrents/33/" + secondID.String() + ".torrent", mode: 0o600, content: raw},
	})
	root, err := openSourceObjectRoot(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.close() })
	recovery, err := root.resolveArchiveObjects([]expectedSourceObject{{
		legacyID: 7, publicID: expectedID, infoHash: parsed.InfoHashV1, size: parsed.TotalSizeBytes,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if recovery.RecoveredObjects != 0 || recovery.AmbiguousObjects != 1 {
		t.Fatalf("recovery = %+v", recovery)
	}
	if _, err := root.read(expectedID); sourceObjectErrorCode(err) != "object_missing" {
		t.Fatalf("ambiguous recovery read error = %v", err)
	}
}

type sourceArchiveEntry struct {
	name    string
	mode    os.FileMode
	content []byte
}

func writeSourceObjectArchive(t *testing.T, entries []sourceArchiveEntry) string {
	t.Helper()
	archivePath := filepath.Join(t.TempDir(), "torrents.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Store}
		header.SetMode(entry.mode)
		target, createErr := writer.CreateHeader(header)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := target.Write(entry.content); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return archivePath
}

func TestReconcileSourceMetainfoRequiresExactIdentityAndManifest(t *testing.T) {
	t.Parallel()

	hash, err := torrents.ParseInfoHashV1Hex("1111111111111111111111111111111111111111")
	if err != nil {
		t.Fatal(err)
	}
	source := sourceTorrent{
		LegacyID:  1,
		InfoHash:  hash.Hex(),
		SizeBytes: 3,
	}
	manifest, err := newSourceFileManifest(1, []sourceFile{{LegacyID: 1, Path: "a.bin", Size: 3}})
	if err != nil {
		t.Fatal(err)
	}
	parsed := torrents.ParsedMetainfo{
		InfoHashV1:     hash,
		Private:        true,
		TotalSizeBytes: 3,
		Files:          []torrents.File{{Index: 0, DisplayPath: "a.bin", LengthBytes: 3}},
	}
	if err := reconcileSourceMetainfo(source, manifest, parsed); err != nil {
		t.Fatal(err)
	}

	parsed.Files[0].DisplayPath = "different.bin"
	if err := reconcileSourceMetainfo(source, manifest, parsed); !errors.Is(err, errInvalidSourceTorrent) {
		t.Fatalf("manifest mismatch error = %v", err)
	}

	emptyManifest, err := newSourceFileManifest(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := reconcileSourceMetainfo(source, emptyManifest, parsed); err != nil {
		t.Fatalf("raw-authoritative missing manifest error = %v", err)
	}
}
