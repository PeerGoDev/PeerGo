package wal

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/peergo/peergo/services/tracker/internal/announceevent"
)

func TestFilePersistsReopensAndRecoversIncompleteTail(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "announce.wal")
	log, err := OpenFile(path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	event := walTestEvent(t)
	if err := log.Append(event); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Write([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	_ = handle.Close()
	reopened, err := OpenFile(path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() || reopened.Ready() != nil {
		t.Fatalf("sizes before=%d after=%d ready=%v", before.Size(), after.Size(), reopened.Ready())
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range [][]byte{[]byte("passkey"), []byte("192.0.2.1"), []byte("client-secret")} {
		if bytes.Contains(encoded, forbidden) {
			t.Fatalf("WAL leaked %q", forbidden)
		}
	}
}

func TestFileCheckpointReplaysOnlyUnacknowledgedRecordsAndCompacts(t *testing.T) {
	t.Parallel()
	directory := protectedTempDir(t)
	path := filepath.Join(directory, "announce.wal")
	log, err := OpenFile(path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	first := walTestEventAt(t, 0x81, 0)
	second := walTestEventAt(t, 0x82, time.Second)
	if err := log.Append(first); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(second); err != nil {
		t.Fatal(err)
	}
	beforeAcknowledge := log.Stats()
	if beforeAcknowledge.Bytes <= 0 || beforeAcknowledge.AcknowledgedBytes != 0 ||
		beforeAcknowledge.UnacknowledgedBytes != beforeAcknowledge.Bytes || beforeAcknowledge.CapacityBytes != 1<<20 {
		t.Fatalf("stats before acknowledge = %+v", beforeAcknowledge)
	}
	firstRecord, found, err := log.Next()
	if err != nil || !found || firstRecord.Event.EventID != first.EventID {
		t.Fatalf("first Next() = %+v, %v, %v", firstRecord.Event, found, err)
	}
	if err := log.Acknowledge(firstRecord); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenFile(path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	secondRecord, found, err := reopened.Next()
	if err != nil || !found || secondRecord.Event.EventID != second.EventID {
		t.Fatalf("replayed Next() = %+v, %v, %v", secondRecord.Event, found, err)
	}
	if err := reopened.Acknowledge(firstRecord); !errors.Is(err, ErrCursor) {
		t.Fatalf("stale Acknowledge() error = %v, want ErrCursor", err)
	}
	if err := reopened.Acknowledge(secondRecord); err != nil {
		t.Fatal(err)
	}
	afterAcknowledge := reopened.Stats()
	if afterAcknowledge.Bytes <= 0 || afterAcknowledge.AcknowledgedBytes != afterAcknowledge.Bytes ||
		afterAcknowledge.UnacknowledgedBytes != 0 {
		t.Fatalf("stats after acknowledge = %+v", afterAcknowledge)
	}
	if pending, err := reopened.PendingBytes(); err != nil || pending != 0 {
		t.Fatalf("PendingBytes() = %d, %v", pending, err)
	}
	compacted, err := reopened.CompactAcknowledged(1)
	if err != nil || !compacted {
		t.Fatalf("CompactAcknowledged() = %v, %v", compacted, err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() != 0 {
		t.Fatalf("compacted WAL stat = %+v, %v", info, err)
	}
}

func TestFileCheckpointResetBeforeTruncateCanOnlyCauseReplay(t *testing.T) {
	t.Parallel()
	directory := protectedTempDir(t)
	path := filepath.Join(directory, "announce.wal")
	log, err := OpenFile(path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	event := walTestEvent(t)
	if err := log.Append(event); err != nil {
		t.Fatal(err)
	}
	record, found, err := log.Next()
	if err != nil || !found {
		t.Fatalf("Next() = %v, %v", found, err)
	}
	if err := log.Acknowledge(record); err != nil {
		t.Fatal(err)
	}
	// Simulate a process crash after the durable zero-checkpoint write and
	// before the data-file truncate performed by CompactAcknowledged.
	log.mu.Lock()
	if err := persistCheckpoint(log.checkpointPath, log.parentPath, checkpoint{}); err != nil {
		log.mu.Unlock()
		t.Fatal(err)
	}
	log.ackOffset = 0
	log.mu.Unlock()
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenFile(path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	replayed, found, err := reopened.Next()
	if err != nil || !found || replayed.Event.EventID != event.EventID {
		t.Fatalf("crash replay = %+v, %v, %v", replayed.Event, found, err)
	}
}

func TestFileRejectsCorruptCheckpoint(t *testing.T) {
	t.Parallel()
	directory := protectedTempDir(t)
	path := filepath.Join(directory, "announce.wal")
	log, err := OpenFile(path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Append(walTestEvent(t)); err != nil {
		t.Fatal(err)
	}
	record, found, err := log.Next()
	if err != nil || !found {
		t.Fatalf("Next() = %v, %v", found, err)
	}
	if err := log.Acknowledge(record); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	checkpointPath := path + ".checkpoint"
	handle, err := os.OpenFile(checkpointPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handle.WriteAt([]byte{0xff}, checkpointBytes-1); err != nil {
		t.Fatal(err)
	}
	_ = handle.Close()
	if _, err := OpenFile(path, 1<<20); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("OpenFile() error = %v, want ErrCorrupt", err)
	}
}

func TestFileRejectsTruncatedCheckpoint(t *testing.T) {
	t.Parallel()
	directory := protectedTempDir(t)
	path := filepath.Join(directory, "announce.wal")
	log, err := OpenFile(path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Append(walTestEvent(t)); err != nil {
		t.Fatal(err)
	}
	record, found, err := log.Next()
	if err != nil || !found {
		t.Fatalf("Next() = %v, %v", found, err)
	}
	if err := log.Acknowledge(record); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path+".checkpoint", checkpointBytes-1); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenFile(path, 1<<20); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("OpenFile() error = %v, want ErrCorrupt", err)
	}
}

func TestFileWaitObservesDurableAppend(t *testing.T) {
	t.Parallel()
	log, err := OpenFile(filepath.Join(protectedTempDir(t), "announce.wal"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- log.Wait(ctx) }()
	if err := log.Append(walTestEvent(t)); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func walTestEvent(t *testing.T) announceevent.Event {
	return walTestEventAt(t, 0x91, 0)
}

func walTestEventAt(t *testing.T, randomByte byte, offset time.Duration) announceevent.Event {
	t.Helper()
	event, err := announceevent.NewFactory(bytes.NewReader(bytes.Repeat([]byte{randomByte}, 16))).New(announceevent.Input{
		ReceivedAt: time.Date(2026, 8, 8, 23, 45, 0, 0, time.UTC).Add(offset),
		UserID:     "0198f20a-6da8-7e51-9c64-111111111111", TorrentID: 42,
		InfoHash: [20]byte{1}, PeerID: [20]byte{2}, Key: "client-secret", AddressFamily: 4,
		Uploaded: 1, Downloaded: 2, Left: 3, CredentialVersion: 1,
		TorrentControlSequence: 4, SubjectControlSequence: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func protectedTempDir(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	return directory
}
