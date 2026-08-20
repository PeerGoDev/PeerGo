package control

import (
	"path/filepath"
	"strings"
	"time"
)

type FileLoader struct {
	path  string
	store *Store
}

func NewFileLoader(path string, store *Store) (*FileLoader, error) {
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) || store == nil {
		return nil, ErrStoreConfig
	}
	return &FileLoader{path: filepath.Clean(path), store: store}, nil
}

// LoadOnce is intentionally read-only: Core is the sole publisher. A Tracker
// runtime may call this during startup and after a file notification/poll, but
// all rollback and divergence decisions remain centralized in Store.
func (loader *FileLoader) LoadOnce(now time.Time) (LoadResult, error) {
	encoded, err := ReadArtifactFile(loader.path)
	if err != nil {
		return LoadResult{}, err
	}
	return loader.store.LoadArtifact(encoded, now)
}
