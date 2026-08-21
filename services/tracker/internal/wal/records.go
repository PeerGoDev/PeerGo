package wal

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"

	"github.com/peergo/peergo/services/tracker/internal/announceevent"
)

// Record is an immutable WAL frame returned to the publisher. Offset fields
// are deliberately opaque progress coordinates; only Acknowledge may advance
// the durable checkpoint after the corresponding JetStream publish ACK.
type Record struct {
	Offset     int64
	NextOffset int64
	Event      announceevent.Event
	Payload    []byte
	digest     [sha256.Size]byte
}

func (wal *File) Next() (Record, bool, error) {
	records, err := wal.NextBatch(1)
	if err != nil {
		return Record{}, false, err
	}
	if len(records) == 0 {
		return Record{}, false, nil
	}
	return records[0], true, nil
}

// NextBatch returns a bounded, consecutive prefix beginning at the durable
// acknowledgement cursor. Reading a batch does not advance that cursor.
func (wal *File) NextBatch(maxRecords int) ([]Record, error) {
	if maxRecords < 1 {
		return nil, ErrConfig
	}
	wal.mu.Lock()
	defer wal.mu.Unlock()
	if wal.fault != nil {
		return nil, wal.fault
	}
	if wal.handle == nil {
		return nil, ErrUnsafe
	}
	records := make([]Record, 0, maxRecords)
	offset := wal.ackOffset
	for len(records) < maxRecords {
		record, found, err := readRecordAt(wal.handle, offset, wal.size)
		if err != nil {
			wal.fault = err
			return nil, err
		}
		if !found {
			break
		}
		records = append(records, record)
		offset = record.NextOffset
	}
	return records, nil
}

func (wal *File) Acknowledge(record Record) error {
	return wal.AcknowledgeBatch([]Record{record})
}

// AcknowledgeBatch durably commits a consecutive batch with one checkpoint
// replacement. The caller must first obtain a storage ACK for every record.
// A crash before this method completes can only replay stable event IDs; it
// cannot skip an unpublished event.
func (wal *File) AcknowledgeBatch(records []Record) error {
	if len(records) == 0 {
		return ErrCursor
	}
	wal.mu.Lock()
	defer wal.mu.Unlock()
	if wal.fault != nil {
		return wal.fault
	}
	if wal.handle == nil {
		return ErrUnsafe
	}
	offset := wal.ackOffset
	var last Record
	for _, record := range records {
		if record.Offset != offset || record.NextOffset <= record.Offset || record.NextOffset > wal.size {
			return ErrCursor
		}
		current, found, err := readRecordAt(wal.handle, offset, wal.size)
		if err != nil {
			wal.fault = err
			return err
		}
		if !found || current.NextOffset != record.NextOffset || current.Event.EventID != record.Event.EventID ||
			current.digest != record.digest || !bytes.Equal(current.Payload, record.Payload) {
			return ErrCursor
		}
		last = current
		offset = current.NextOffset
	}
	value := checkpoint{Offset: last.NextOffset, EventID: last.Event.EventID, PayloadSHA256: last.digest}
	if err := persistCheckpoint(wal.checkpointPath, wal.parentPath, value); err != nil {
		return err
	}
	wal.ackOffset = last.NextOffset
	return nil
}

// CompactAcknowledged reclaims a fully acknowledged WAL only after its durable
// checkpoint has first been reset to zero. A crash between the reset and the
// truncate therefore replays acknowledged records; it can never skip an
// unacknowledged record. Partial-prefix compaction is intentionally deferred to
// a later segmented WAL adapter because it needs a generation manifest.
func (wal *File) CompactAcknowledged(minimumBytes int64) (bool, error) {
	if minimumBytes < 1 {
		return false, ErrConfig
	}
	wal.mu.Lock()
	defer wal.mu.Unlock()
	if wal.fault != nil {
		return false, wal.fault
	}
	if wal.handle == nil {
		return false, ErrUnsafe
	}
	if wal.size < minimumBytes || wal.ackOffset != wal.size {
		return false, nil
	}
	if err := persistCheckpoint(wal.checkpointPath, wal.parentPath, checkpoint{}); err != nil {
		return false, err
	}
	wal.ackOffset = 0
	if err := wal.handle.Truncate(0); err != nil {
		return false, fmt.Errorf("truncate acknowledged Tracker WAL: %w", err)
	}
	wal.size = 0
	if err := wal.handle.Sync(); err != nil {
		wal.fault = fmt.Errorf("sync compacted Tracker WAL: %w", err)
		return false, wal.fault
	}
	return true, nil
}

func (wal *File) PendingBytes() (int64, error) {
	wal.mu.Lock()
	defer wal.mu.Unlock()
	if wal.fault != nil {
		return 0, wal.fault
	}
	if wal.handle == nil || wal.ackOffset < 0 || wal.ackOffset > wal.size {
		return 0, ErrUnsafe
	}
	return wal.size - wal.ackOffset, nil
}

func readRecordAt(handle *os.File, offset, size int64) (Record, bool, error) {
	if offset == size {
		return Record{}, false, nil
	}
	if offset < 0 || offset > size || size-offset < recordHeaderBytes {
		return Record{}, false, ErrCorrupt
	}
	header := make([]byte, recordHeaderBytes)
	if _, err := handle.ReadAt(header, offset); err != nil {
		return Record{}, false, fmt.Errorf("read Tracker WAL header: %w", err)
	}
	if !bytes.Equal(header[:4], magic[:]) {
		return Record{}, false, ErrCorrupt
	}
	payloadSize := int64(binary.BigEndian.Uint32(header[4:8]))
	if payloadSize < 2 || payloadSize > announceevent.MaxEventBytes {
		return Record{}, false, ErrCorrupt
	}
	nextOffset := offset + recordHeaderBytes + payloadSize + recordDigestBytes
	if nextOffset > size {
		return Record{}, false, ErrCorrupt
	}
	body := make([]byte, payloadSize+recordDigestBytes)
	if _, err := handle.ReadAt(body, offset+recordHeaderBytes); err != nil {
		return Record{}, false, fmt.Errorf("read Tracker WAL record: %w", err)
	}
	digest := sha256.Sum256(body[:payloadSize])
	if !bytes.Equal(digest[:], body[payloadSize:]) {
		return Record{}, false, ErrCorrupt
	}
	event, err := announceevent.Decode(body[:payloadSize])
	if err != nil {
		return Record{}, false, ErrCorrupt
	}
	return Record{
		Offset: offset, NextOffset: nextOffset, Event: event,
		Payload: append([]byte(nil), body[:payloadSize]...), digest: digest,
	}, true, nil
}
