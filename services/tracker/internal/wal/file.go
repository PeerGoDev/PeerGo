// Package wal provides the durable announce-event adapter. Every appended
// record waits for its group commit to fsync before success is returned, while
// the publisher may commit an already storage-acknowledged record prefix with
// one durable checkpoint.
package wal

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/peergo/peergo/contracts/go/trackerannouncev2"
	"github.com/peergo/peergo/services/tracker/internal/announceevent"
	"github.com/peergo/peergo/services/tracker/internal/uuidv7"
)

const (
	recordHeaderBytes   = 8
	recordDigestBytes   = sha256.Size
	appendBatchRecords  = 256
	appendCoalesceDelay = 500 * time.Microsecond
)

var (
	ErrConfig  = errors.New("Tracker WAL configuration is invalid")
	ErrUnsafe  = errors.New("Tracker WAL path is unsafe")
	ErrCorrupt = errors.New("Tracker WAL is corrupt")
	ErrFull    = errors.New("Tracker WAL capacity is exhausted")
	ErrCursor  = errors.New("Tracker WAL acknowledgement cursor is invalid")
	magic      = [4]byte{'P', 'G', 'W', '1'}
)

type Appender interface {
	Append(announceevent.Event) error
	Ready() error
}

// ProducerConfig opts new WAL records into the sequenced v2 envelope. The
// epoch is generated once per process start, while File assigns sequence
// numbers in the exact durable append order. Existing v1 WAL records remain
// replayable alongside newly appended v2 records.
type ProducerConfig struct {
	ID string
}

type File struct {
	mu             sync.Mutex
	handle         *os.File
	checkpointPath string
	parentPath     string
	maxBytes       int64
	size           int64
	ackOffset      int64
	wake           chan struct{}
	fault          error
	producerID     string
	producerEpoch  string
	producerNext   int64

	appendMu      sync.Mutex
	appendQueue   []*appendRequest
	appendRunning bool
	appendClosed  bool
	appendWG      sync.WaitGroup
}

type appendRequest struct {
	event  announceevent.Event
	result chan error
}

type Stats struct {
	Bytes               int64
	AcknowledgedBytes   int64
	UnacknowledgedBytes int64
	CapacityBytes       int64
}

func OpenFile(path string, maxBytes int64, producerConfigs ...ProducerConfig) (*File, error) {
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) || maxBytes < 1<<20 || len(producerConfigs) > 1 {
		return nil, ErrConfig
	}
	producerID := ""
	producerEpoch := ""
	if len(producerConfigs) == 1 {
		producerID = producerConfigs[0].ID
		if !trackerannouncev2.ValidProducerID(producerID) {
			return nil, ErrConfig
		}
		epoch, err := uuidv7.New(time.Now().UTC(), rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("create Tracker announce producer epoch: %w", err)
		}
		producerEpoch = epoch
	}
	path = filepath.Clean(path)
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, fmt.Errorf("create Tracker WAL directory: %w", err)
	}
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return nil, fmt.Errorf("resolve Tracker WAL directory: %w", err)
	}
	parentInfo, err := os.Stat(resolvedParent)
	if err != nil || !parentInfo.IsDir() || parentInfo.Mode().Perm()&0o077 != 0 {
		return nil, ErrUnsafe
	}
	target := filepath.Join(resolvedParent, filepath.Base(path))
	linkInfo, err := os.Lstat(target)
	if err == nil && (!linkInfo.Mode().IsRegular() || linkInfo.Mode()&os.ModeSymlink != 0 || linkInfo.Mode().Perm()&0o077 != 0) {
		return nil, ErrUnsafe
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect Tracker WAL: %w", err)
	}
	handle, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open Tracker WAL: %w", err)
	}
	closeOnError := func(result error) (*File, error) {
		_ = handle.Close()
		return nil, result
	}
	if err := handle.Chmod(0o600); err != nil {
		return closeOnError(fmt.Errorf("protect Tracker WAL: %w", err))
	}
	fileInfo, err := handle.Stat()
	if err != nil || !fileInfo.Mode().IsRegular() {
		return closeOnError(ErrUnsafe)
	}
	if linkInfo != nil && !os.SameFile(linkInfo, fileInfo) {
		return closeOnError(ErrUnsafe)
	}
	if err := syscall.Flock(int(handle.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return closeOnError(fmt.Errorf("lock Tracker WAL: %w", err))
	}
	wal := &File{
		handle: handle, checkpointPath: target + ".checkpoint", parentPath: resolvedParent,
		maxBytes: maxBytes, size: fileInfo.Size(), wake: make(chan struct{}, 1),
		producerID: producerID, producerEpoch: producerEpoch,
	}
	if wal.size > maxBytes {
		_ = wal.Close()
		return nil, ErrFull
	}
	checkpoint, err := readCheckpoint(wal.checkpointPath)
	if err != nil {
		_ = wal.Close()
		return nil, err
	}
	if err := wal.recoverTail(checkpoint); err != nil {
		_ = wal.Close()
		return nil, err
	}
	wal.ackOffset = checkpoint.Offset
	return wal, nil
}

func (wal *File) Append(event announceevent.Event) error {
	if _, err := announceevent.Encode(event); err != nil {
		return err
	}

	request := &appendRequest{event: event, result: make(chan error, 1)}
	wal.appendMu.Lock()
	if wal.appendClosed {
		wal.appendMu.Unlock()
		return ErrUnsafe
	}
	wal.appendQueue = append(wal.appendQueue, request)
	if !wal.appendRunning {
		wal.appendRunning = true
		wal.appendWG.Add(1)
		go wal.runAppendBatches()
	}
	wal.appendMu.Unlock()
	return <-request.result
}

// runAppendBatches group-commits concurrent announces. Every caller still
// waits for the shared file sync before receiving success, so batching changes
// throughput only; it does not weaken the durable-response boundary.
func (wal *File) runAppendBatches() {
	defer wal.appendWG.Done()
	for {
		time.Sleep(appendCoalesceDelay)
		wal.appendMu.Lock()
		if len(wal.appendQueue) == 0 {
			wal.appendRunning = false
			wal.appendMu.Unlock()
			return
		}
		count := min(len(wal.appendQueue), appendBatchRecords)
		batch := append([]*appendRequest(nil), wal.appendQueue[:count]...)
		clear(wal.appendQueue[:count])
		wal.appendQueue = wal.appendQueue[count:]
		if len(wal.appendQueue) == 0 {
			wal.appendQueue = nil
		}
		wal.appendMu.Unlock()

		err := wal.appendBatch(batch)
		for _, request := range batch {
			request.result <- err
		}
	}
}

func (wal *File) appendBatch(batch []*appendRequest) error {
	if len(batch) == 0 {
		return ErrConfig
	}
	for _, request := range batch {
		if request == nil {
			return ErrConfig
		}
	}

	wal.mu.Lock()
	defer wal.mu.Unlock()
	if wal.fault != nil {
		return wal.fault
	}
	if wal.handle == nil {
		return ErrUnsafe
	}
	encodedRecords := make([][]byte, len(batch))
	totalBytes := 0
	nextSequence := wal.producerNext
	for index, request := range batch {
		var encoded []byte
		var err error
		if wal.producerID == "" {
			encoded, err = announceevent.Encode(request.event)
		} else {
			if nextSequence == math.MaxInt64 {
				return ErrFull
			}
			nextSequence++
			encoded, err = announceevent.EncodeSequenced(request.event, announceevent.Producer{
				ID: wal.producerID, Epoch: wal.producerEpoch, Sequence: nextSequence,
			})
		}
		if err != nil {
			return err
		}
		record := make([]byte, recordHeaderBytes+len(encoded)+recordDigestBytes)
		copy(record[:4], magic[:])
		binary.BigEndian.PutUint32(record[4:8], uint32(len(encoded)))
		copy(record[8:], encoded)
		digest := sha256.Sum256(encoded)
		copy(record[8+len(encoded):], digest[:])
		encodedRecords[index] = record
		totalBytes += len(record)
	}
	records := make([]byte, 0, totalBytes)
	for _, record := range encodedRecords {
		records = append(records, record...)
	}
	if wal.size+int64(len(records)) > wal.maxBytes {
		wal.fault = ErrFull
		return wal.fault
	}
	if err := writeAll(wal.handle, records); err != nil {
		wal.fault = fmt.Errorf("append Tracker WAL: %w", err)
		return wal.fault
	}
	if err := wal.handle.Sync(); err != nil {
		wal.fault = fmt.Errorf("sync Tracker WAL: %w", err)
		return wal.fault
	}
	wal.size += int64(len(records))
	wal.producerNext = nextSequence
	select {
	case wal.wake <- struct{}{}:
	default:
	}
	return nil
}

// Wait blocks until an unacknowledged record is available. The readiness
// check and channel capture happen under the same mutex, so an append cannot
// be lost between observing EOF and beginning the wait.
func (wal *File) Wait(ctx context.Context) error {
	for {
		wal.mu.Lock()
		if wal.fault != nil {
			err := wal.fault
			wal.mu.Unlock()
			return err
		}
		if wal.handle == nil {
			wal.mu.Unlock()
			return ErrUnsafe
		}
		if wal.ackOffset < wal.size {
			wal.mu.Unlock()
			return nil
		}
		wake := wal.wake
		wal.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wake:
		}
	}
}

func (wal *File) Ready() error {
	wal.mu.Lock()
	defer wal.mu.Unlock()
	if wal.fault != nil {
		return wal.fault
	}
	if wal.handle == nil {
		return ErrUnsafe
	}
	return nil
}

// Stats returns only aggregate queue pressure. It never reads or exposes WAL
// payloads, event IDs, passkeys, user IDs, peer IDs or info hashes.
func (wal *File) Stats() Stats {
	wal.mu.Lock()
	defer wal.mu.Unlock()
	pending := wal.size - wal.ackOffset
	if pending < 0 {
		pending = 0
	}
	return Stats{
		Bytes: wal.size, AcknowledgedBytes: wal.ackOffset,
		UnacknowledgedBytes: pending, CapacityBytes: wal.maxBytes,
	}
}

func (wal *File) Close() error {
	wal.appendMu.Lock()
	wal.appendClosed = true
	wal.appendMu.Unlock()
	wal.appendWG.Wait()

	wal.mu.Lock()
	defer wal.mu.Unlock()
	if wal.handle == nil {
		return nil
	}
	_ = syscall.Flock(int(wal.handle.Fd()), syscall.LOCK_UN)
	err := wal.handle.Close()
	wal.handle = nil
	return err
}

// recoverTail truncates only an incomplete final record, which can be left by
// process or host failure during append. A complete record with bad magic,
// schema or digest is not repaired automatically and fails startup closed.
func (wal *File) recoverTail(checkpoint checkpoint) error {
	offset := int64(0)
	checkpointMatched := checkpoint.Offset == 0
	header := make([]byte, recordHeaderBytes)
	for offset < wal.size {
		remaining := wal.size - offset
		if remaining < recordHeaderBytes {
			if checkpoint.Offset > offset {
				return ErrCorrupt
			}
			return wal.truncateIncompleteTail(offset)
		}
		if _, err := wal.handle.ReadAt(header, offset); err != nil {
			return fmt.Errorf("read Tracker WAL header: %w", err)
		}
		if !bytes.Equal(header[:4], magic[:]) {
			return ErrCorrupt
		}
		payloadSize := int64(binary.BigEndian.Uint32(header[4:8]))
		if payloadSize < 2 || payloadSize > announceevent.MaxEventBytes {
			return ErrCorrupt
		}
		recordSize := int64(recordHeaderBytes) + payloadSize + recordDigestBytes
		if remaining < recordSize {
			if checkpoint.Offset > offset {
				return ErrCorrupt
			}
			return wal.truncateIncompleteTail(offset)
		}
		body := make([]byte, payloadSize+recordDigestBytes)
		if _, err := wal.handle.ReadAt(body, offset+recordHeaderBytes); err != nil {
			return fmt.Errorf("read Tracker WAL record: %w", err)
		}
		digest := sha256.Sum256(body[:payloadSize])
		if !bytes.Equal(digest[:], body[payloadSize:]) {
			return ErrCorrupt
		}
		decoded, err := announceevent.DecodeAny(body[:payloadSize])
		if err != nil {
			return ErrCorrupt
		}
		offset += recordSize
		if offset == checkpoint.Offset {
			if decoded.Event.EventID != checkpoint.EventID || digest != checkpoint.PayloadSHA256 {
				return ErrCorrupt
			}
			checkpointMatched = true
		} else if checkpoint.Offset > offset-recordSize && checkpoint.Offset < offset {
			return ErrCorrupt
		}
	}
	if !checkpointMatched {
		return ErrCorrupt
	}
	return nil
}

func (wal *File) truncateIncompleteTail(offset int64) error {
	if err := wal.handle.Truncate(offset); err != nil {
		return fmt.Errorf("truncate incomplete Tracker WAL tail: %w", err)
	}
	if err := wal.handle.Sync(); err != nil {
		return fmt.Errorf("sync recovered Tracker WAL: %w", err)
	}
	wal.size = offset
	return nil
}

func writeAll(destination io.Writer, value []byte) error {
	for len(value) > 0 {
		written, err := destination.Write(value)
		if err != nil {
			return err
		}
		if written < 1 {
			return io.ErrShortWrite
		}
		value = value[written:]
	}
	return nil
}
