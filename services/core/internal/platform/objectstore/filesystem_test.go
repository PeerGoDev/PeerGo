package objectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	platformconfig "github.com/peergo/peergo/services/core/internal/platform/config"
)

func TestFilesystemPublishesWithoutOverwriteAndDeletesExplicitly(t *testing.T) {
	t.Parallel()

	store, err := NewFilesystem("local-primary", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	contents := []byte("immutable torrent object")
	descriptor := torrents.StoredObjectDescriptor{
		SHA256: torrents.ObjectSHA256(sha256.Sum256(contents)), ByteLength: int64(len(contents)),
	}
	key := torrents.TorrentObjectKey(descriptor.SHA256)

	created, err := store.PutIfAbsent(context.Background(), key, bytes.NewReader(contents), descriptor)
	if err != nil || !created.Created {
		t.Fatalf("PutIfAbsent(first) = %+v, %v", created, err)
	}
	repeated, err := store.PutIfAbsent(context.Background(), key, bytes.NewReader(contents), descriptor)
	if err != nil || repeated.Created {
		t.Fatalf("PutIfAbsent(repeated) = %+v, %v", repeated, err)
	}
	object, err := store.Open(context.Background(), key, "")
	if err != nil {
		t.Fatal(err)
	}
	read, err := io.ReadAll(object.Body)
	if err != nil || object.Body.Close() != nil || !bytes.Equal(read, contents) || object.ByteLength != int64(len(contents)) {
		t.Fatalf("Open() bytes=%q length=%d error=%v", read, object.ByteLength, err)
	}
	if err := store.Delete(context.Background(), key, ""); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := store.Delete(context.Background(), key, ""); err != nil {
		t.Fatalf("Delete(idempotent) error = %v", err)
	}
	if _, err := store.Open(context.Background(), key, ""); !errors.Is(err, torrents.ErrStoredObjectNotFound) {
		t.Fatalf("Open(deleted) error = %v", err)
	}
}

func TestFilesystemRejectsMismatchedStreamBeforePublication(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := NewFilesystem("local-primary", root)
	if err != nil {
		t.Fatal(err)
	}
	expected := []byte("expected")
	descriptor := torrents.StoredObjectDescriptor{
		SHA256: torrents.ObjectSHA256(sha256.Sum256(expected)), ByteLength: int64(len(expected)),
	}
	key := torrents.TorrentObjectKey(descriptor.SHA256)
	if _, err := store.PutIfAbsent(context.Background(), key, bytes.NewReader([]byte("corrupt!")), descriptor); !errors.Is(err, torrents.ErrStoredObjectConflict) {
		t.Fatalf("PutIfAbsent(corrupt) error = %v", err)
	}
	if _, err := store.Open(context.Background(), key, ""); !errors.Is(err, torrents.ErrStoredObjectNotFound) {
		t.Fatalf("Open(unpublished) error = %v", err)
	}
}

func TestFilesystemRejectsSymlinkedObjectDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	store, err := NewFilesystem("local-primary", root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "torrents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "torrents", "sha256")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	contents := []byte("object")
	descriptor := torrents.StoredObjectDescriptor{
		SHA256: torrents.ObjectSHA256(sha256.Sum256(contents)), ByteLength: int64(len(contents)),
	}
	if _, err := store.PutIfAbsent(context.Background(), torrents.TorrentObjectKey(descriptor.SHA256), bytes.NewReader(contents), descriptor); !errors.Is(err, torrents.ErrStoredObjectConflict) {
		t.Fatalf("PutIfAbsent(symlinked parent) error = %v", err)
	}
}

func TestProbeConfiguredReadOnlyRequiresProvisionedFilesystemRoot(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "objects")
	settings := platformconfig.ObjectStoreConfig{
		BackendID: "local-primary", Driver: "filesystem", FilesystemRoot: root,
	}
	if err := ProbeConfiguredReadOnly(context.Background(), settings); err == nil {
		t.Fatal("read-only probe accepted an unprovisioned filesystem root")
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ProbeConfiguredReadOnly(context.Background(), settings); err != nil {
		t.Fatalf("read-only probe rejected a provisioned filesystem root: %v", err)
	}
}
