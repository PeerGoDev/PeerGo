package control

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/peergo/peergo/contracts/go/trackercontrolv1"
)

var ErrSnapshotFileUnsafe = errors.New("Tracker control snapshot file is unsafe")

func ReadArtifactFile(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) {
		return nil, ErrSnapshotFileUnsafe
	}
	path = filepath.Clean(path)
	linkInfo, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !linkInfo.Mode().IsRegular() || linkInfo.Mode()&os.ModeSymlink != 0 ||
		linkInfo.Mode().Perm()&0o077 != 0 || linkInfo.Size() < 2 ||
		linkInfo.Size() > trackercontrolv1.MaxArtifactBytes {
		return nil, ErrSnapshotFileUnsafe
	}
	handle, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	fileInfo, err := handle.Stat()
	if err != nil || !fileInfo.Mode().IsRegular() || !os.SameFile(linkInfo, fileInfo) {
		return nil, ErrSnapshotFileUnsafe
	}
	encoded, err := io.ReadAll(io.LimitReader(handle, trackercontrolv1.MaxArtifactBytes+1))
	if err != nil || len(encoded) != int(fileInfo.Size()) || len(encoded) > trackercontrolv1.MaxArtifactBytes {
		return nil, ErrSnapshotFileUnsafe
	}
	return encoded, nil
}
