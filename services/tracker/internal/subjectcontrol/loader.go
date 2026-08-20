package subjectcontrol

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/peergo/peergo/services/tracker/internal/control"
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

func (loader *FileLoader) LoadOnce(now time.Time) (LoadResult, error) {
	encoded, err := control.ReadArtifactFile(loader.path)
	if err != nil {
		return LoadResult{}, err
	}
	return loader.store.LoadArtifact(encoded, now)
}
