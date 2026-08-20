package wal

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
)

const (
	checkpointPrefixBytes = 4 + 8 + 36 + sha256.Size
	checkpointBytes       = checkpointPrefixBytes + sha256.Size
)

var checkpointMagic = [4]byte{'P', 'G', 'C', '1'}

type checkpoint struct {
	Offset        int64
	EventID       string
	PayloadSHA256 [sha256.Size]byte
}

func readCheckpoint(path string) (checkpoint, error) {
	linkInfo, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return checkpoint{}, nil
	}
	if err != nil {
		return checkpoint{}, fmt.Errorf("inspect Tracker WAL checkpoint: %w", err)
	}
	if !linkInfo.Mode().IsRegular() || linkInfo.Mode()&os.ModeSymlink != 0 || linkInfo.Mode().Perm()&0o077 != 0 {
		return checkpoint{}, ErrUnsafe
	}
	handle, err := os.Open(path)
	if err != nil {
		return checkpoint{}, fmt.Errorf("open Tracker WAL checkpoint: %w", err)
	}
	defer handle.Close()
	fileInfo, err := handle.Stat()
	if err != nil || !fileInfo.Mode().IsRegular() || !os.SameFile(linkInfo, fileInfo) {
		return checkpoint{}, ErrUnsafe
	}
	if fileInfo.Size() != checkpointBytes {
		return checkpoint{}, ErrCorrupt
	}
	encoded := make([]byte, checkpointBytes)
	if _, err := io.ReadFull(handle, encoded); err != nil {
		return checkpoint{}, ErrCorrupt
	}
	return decodeCheckpoint(encoded)
}

func encodeCheckpoint(value checkpoint) ([]byte, error) {
	if value.Offset < 0 || (value.Offset == 0 && (value.EventID != "" || value.PayloadSHA256 != [sha256.Size]byte{})) ||
		(value.Offset > 0 && len(value.EventID) != 36) {
		return nil, ErrCursor
	}
	encoded := make([]byte, checkpointBytes)
	copy(encoded[:4], checkpointMagic[:])
	binary.BigEndian.PutUint64(encoded[4:12], uint64(value.Offset))
	copy(encoded[12:48], value.EventID)
	copy(encoded[48:checkpointPrefixBytes], value.PayloadSHA256[:])
	digest := sha256.Sum256(encoded[:checkpointPrefixBytes])
	copy(encoded[checkpointPrefixBytes:], digest[:])
	return encoded, nil
}

func decodeCheckpoint(encoded []byte) (checkpoint, error) {
	if len(encoded) != checkpointBytes || !bytes.Equal(encoded[:4], checkpointMagic[:]) {
		return checkpoint{}, ErrCorrupt
	}
	digest := sha256.Sum256(encoded[:checkpointPrefixBytes])
	if !bytes.Equal(digest[:], encoded[checkpointPrefixBytes:]) {
		return checkpoint{}, ErrCorrupt
	}
	offset := binary.BigEndian.Uint64(encoded[4:12])
	if offset > math.MaxInt64 {
		return checkpoint{}, ErrCorrupt
	}
	value := checkpoint{Offset: int64(offset), EventID: string(bytes.TrimRight(encoded[12:48], "\x00"))}
	copy(value.PayloadSHA256[:], encoded[48:checkpointPrefixBytes])
	if value.Offset == 0 {
		if value.EventID != "" || value.PayloadSHA256 != [sha256.Size]byte{} {
			return checkpoint{}, ErrCorrupt
		}
	} else if len(value.EventID) != 36 {
		return checkpoint{}, ErrCorrupt
	}
	return value, nil
}

func persistCheckpoint(path, parent string, value checkpoint) error {
	encoded, err := encodeCheckpoint(value)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(parent, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create Tracker WAL checkpoint temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		_ = temporary.Close()
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("protect Tracker WAL checkpoint temporary file: %w", err)
	}
	if err := writeAll(temporary, encoded); err != nil {
		return fmt.Errorf("write Tracker WAL checkpoint: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync Tracker WAL checkpoint: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close Tracker WAL checkpoint: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("activate Tracker WAL checkpoint: %w", err)
	}
	removeTemporary = false
	directory, err := os.Open(parent)
	if err != nil {
		return fmt.Errorf("open Tracker WAL checkpoint directory: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync Tracker WAL checkpoint directory: %w", err)
	}
	return nil
}
