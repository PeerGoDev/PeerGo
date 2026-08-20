// Package objectstore contains provider adapters for immutable Core objects.
// Domain key, checksum, and migration semantics remain owned by the torrents
// module; adapters only translate those contracts to concrete storage APIs.
package objectstore

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
)

type Filesystem struct {
	backendID objectstorage.BackendID
	root      string
}

// NewFilesystem creates a service-owned root and resolves it once. Production
// deployments must mount this absolute path on durable storage; the adapter is
// intentionally suitable for one Core host, not an uncoordinated replica set.
func NewFilesystem(backendID objectstorage.BackendID, root string) (*Filesystem, error) {
	if backendID == "" || strings.TrimSpace(root) == "" || !filepath.IsAbs(root) {
		return nil, errors.New("filesystem object store requires a backend ID and absolute root")
	}
	root = filepath.Clean(root)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create filesystem object root: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve filesystem object root: %w", err)
	}
	info, err := os.Stat(resolvedRoot)
	if err != nil || !info.IsDir() {
		return nil, errors.New("filesystem object root must be a directory")
	}
	return &Filesystem{backendID: backendID, root: filepath.Clean(resolvedRoot)}, nil
}

func (store *Filesystem) BackendID() objectstorage.BackendID {
	return store.backendID
}

func (store *Filesystem) PutIfAbsent(_ context.Context, key objectstorage.Key, source io.Reader, expected objectstorage.Descriptor) (objectstorage.WriteResult, error) {
	if source == nil || expected.ByteLength <= 0 {
		return objectstorage.WriteResult{}, objectstorage.ErrInputInvalid
	}
	target, err := store.resolve(key)
	if err != nil {
		return objectstorage.WriteResult{}, err
	}
	parent, err := store.ensureParent(target)
	if err != nil {
		return objectstorage.WriteResult{}, err
	}

	temporary, err := os.CreateTemp(parent, ".peergo-object-*")
	if err != nil {
		return objectstorage.WriteResult{}, fmt.Errorf("create temporary object: %w", err)
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return objectstorage.WriteResult{}, fmt.Errorf("protect temporary object: %w", err)
	}

	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(temporary, hasher), io.LimitReader(source, expected.ByteLength+1))
	if err != nil {
		return objectstorage.WriteResult{}, fmt.Errorf("write temporary object: %w", err)
	}
	var observed objectstorage.SHA256
	copy(observed[:], hasher.Sum(nil))
	if written != expected.ByteLength || observed != expected.SHA256 {
		return objectstorage.WriteResult{}, objectstorage.ErrObjectConflict
	}
	if err := temporary.Sync(); err != nil {
		return objectstorage.WriteResult{}, fmt.Errorf("sync temporary object: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return objectstorage.WriteResult{}, fmt.Errorf("close temporary object: %w", err)
	}
	closed = true

	// A hard link publishes the fully synced file without replacing an existing
	// key. The temporary file lives in the target directory, so this remains an
	// atomic same-filesystem operation.
	if err := os.Link(temporaryPath, target); err != nil {
		if errors.Is(err, os.ErrExist) {
			return objectstorage.WriteResult{Created: false}, nil
		}
		return objectstorage.WriteResult{}, fmt.Errorf("publish immutable object: %w", err)
	}
	if err := syncDirectory(parent); err != nil {
		return objectstorage.WriteResult{}, err
	}
	return objectstorage.WriteResult{Created: true}, nil
}

func (store *Filesystem) Open(_ context.Context, key objectstorage.Key, _ string) (objectstorage.Reader, error) {
	target, err := store.resolve(key)
	if err != nil {
		return objectstorage.Reader{}, err
	}
	linkInfo, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return objectstorage.Reader{}, objectstorage.ErrNotFound
	}
	if err != nil {
		return objectstorage.Reader{}, fmt.Errorf("inspect filesystem object: %w", err)
	}
	if !linkInfo.Mode().IsRegular() || linkInfo.Mode()&os.ModeSymlink != 0 {
		return objectstorage.Reader{}, objectstorage.ErrObjectConflict
	}
	file, err := os.Open(target)
	if errors.Is(err, os.ErrNotExist) {
		return objectstorage.Reader{}, objectstorage.ErrNotFound
	}
	if err != nil {
		return objectstorage.Reader{}, fmt.Errorf("open filesystem object: %w", err)
	}
	fileInfo, err := file.Stat()
	if err != nil || !fileInfo.Mode().IsRegular() || !os.SameFile(linkInfo, fileInfo) {
		_ = file.Close()
		return objectstorage.Reader{}, objectstorage.ErrObjectConflict
	}
	return objectstorage.Reader{Body: file, ByteLength: fileInfo.Size()}, nil
}

func (store *Filesystem) Delete(_ context.Context, key objectstorage.Key, _ string) error {
	target, err := store.resolve(key)
	if err != nil {
		return err
	}
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect filesystem object for deletion: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return objectstorage.ErrObjectConflict
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete filesystem object: %w", err)
	}
	return syncDirectory(filepath.Dir(target))
}

func (store *Filesystem) resolve(key objectstorage.Key) (string, error) {
	parsed, err := objectstorage.ParseKey(string(key))
	if err != nil || parsed != key {
		return "", objectstorage.ErrInputInvalid
	}
	target := filepath.Join(store.root, filepath.FromSlash(string(key)))
	relative, err := filepath.Rel(store.root, target)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", objectstorage.ErrInputInvalid
	}
	return target, nil
}

func (store *Filesystem) ensureParent(target string) (string, error) {
	parent := filepath.Dir(target)
	relative, err := filepath.Rel(store.root, parent)
	if err != nil {
		return "", objectstorage.ErrInputInvalid
	}
	current := store.root
	if relative == "." {
		return current, nil
	}
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		if err := os.Mkdir(current, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("create filesystem object directory: %w", err)
		}
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", objectstorage.ErrObjectConflict
		}
	}
	return parent, nil
}

func syncDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open object directory for sync: %w", err)
	}
	defer handle.Close()
	if err := handle.Sync(); err != nil {
		return fmt.Errorf("sync object directory: %w", err)
	}
	return nil
}

var _ objectstorage.Store = (*Filesystem)(nil)
