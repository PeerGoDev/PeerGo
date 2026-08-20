// Package journal provides Audit Sink's local durable append-only store. Each
// fsynced record commits to the previous record hash, so mutation, reordering,
// gaps and incomplete writes in retained history are detected on reopen. A
// complete suffix deletion requires an externally anchored head/WORM archive;
// the local journal alone cannot prove that its newest records still exist.
package journal

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	MaxPayloadBytes     = 1 << 20
	maxJournalLineBytes = 2*MaxPayloadBytes + 16*1024
	recordHashDomain    = "peergo:audit-sink:journal-record:v1\x00"
)

var (
	ErrConflict = errors.New("audit event id already exists with different content")
	ErrCorrupt  = errors.New("audit journal integrity check failed")
	ErrInvalid  = errors.New("invalid audit event")
	ErrClosed   = errors.New("audit journal is closed")
)

type diskRecord struct {
	Sequence             uint64    `json:"sequence"`
	EventID              string    `json:"event_id"`
	ReceivedAt           time.Time `json:"received_at"`
	PayloadSHA256        string    `json:"payload_sha256"`
	PreviousRecordSHA256 string    `json:"previous_record_sha256"`
	RecordSHA256         string    `json:"record_sha256"`
	Payload              []byte    `json:"payload"`
}

type Journal struct {
	mu           sync.Mutex
	file         *os.File
	nextSequence uint64
	lastHash     [sha256.Size]byte
	eventHashes  map[string][sha256.Size]byte
	poisoned     error
}

// Open verifies every existing record before accepting new evidence. It never
// repairs or truncates a damaged tail automatically because doing so could hide
// lost audit evidence; an operator must investigate and restore a valid copy.
func Open(path string) (*Journal, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("audit journal path is required")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("audit journal path must not be a symbolic link")
		}
		if info.Mode().Perm()&0o077 != 0 {
			return nil, errors.New("audit journal permissions must not allow group or world access")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect audit journal: %w", err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open audit journal: %w", err)
	}
	journal := &Journal{file: file, nextSequence: 1, eventHashes: make(map[string][sha256.Size]byte)}
	if err := journal.loadAndVerify(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return journal, nil
}

// Append returns created=false for an at-least-once replay with the same event
// ID and payload. Reusing an event ID for different bytes is a hard conflict.
func (journal *Journal) Append(eventID string, payload []byte, receivedAt time.Time) (bool, error) {
	if !validEventID(eventID) || receivedAt.IsZero() || !validPayload(payload) {
		return false, ErrInvalid
	}
	digest := sha256.Sum256(payload)

	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.file == nil {
		return false, ErrClosed
	}
	if journal.poisoned != nil {
		return false, journal.poisoned
	}
	if existing, ok := journal.eventHashes[eventID]; ok {
		if bytes.Equal(existing[:], digest[:]) {
			return false, nil
		}
		return false, ErrConflict
	}

	sequence := journal.nextSequence
	receivedAt = receivedAt.UTC()
	recordDigest := hashRecord(sequence, eventID, receivedAt, digest, journal.lastHash)
	record := diskRecord{
		Sequence:             sequence,
		EventID:              eventID,
		ReceivedAt:           receivedAt,
		PayloadSHA256:        hex.EncodeToString(digest[:]),
		PreviousRecordSHA256: hex.EncodeToString(journal.lastHash[:]),
		RecordSHA256:         hex.EncodeToString(recordDigest[:]),
		Payload:              append([]byte(nil), payload...),
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return false, fmt.Errorf("encode audit journal record: %w", err)
	}
	encoded = append(encoded, '\n')
	written, err := journal.file.Write(encoded)
	if err != nil || written != len(encoded) {
		journal.poisoned = fmt.Errorf("append audit journal: %w", errors.Join(err, io.ErrShortWrite))
		return false, journal.poisoned
	}
	if err := journal.file.Sync(); err != nil {
		journal.poisoned = fmt.Errorf("fsync audit journal: %w", err)
		return false, journal.poisoned
	}

	journal.eventHashes[eventID] = digest
	journal.lastHash = recordDigest
	journal.nextSequence++
	return true, nil
}

func (journal *Journal) Ready() error {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.file == nil {
		return ErrClosed
	}
	return journal.poisoned
}

func (journal *Journal) Close() error {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.file == nil {
		return nil
	}
	err := errors.Join(journal.poisoned, journal.file.Sync(), journal.file.Close())
	journal.file = nil
	return err
}

func (journal *Journal) loadAndVerify() error {
	info, err := journal.file.Stat()
	if err != nil {
		return fmt.Errorf("stat audit journal: %w", err)
	}
	if info.Size() > 0 {
		if _, err := journal.file.Seek(-1, io.SeekEnd); err != nil {
			return fmt.Errorf("inspect audit journal tail: %w", err)
		}
		last := []byte{0}
		if _, err := io.ReadFull(journal.file, last); err != nil {
			return fmt.Errorf("read audit journal tail: %w", err)
		}
		if last[0] != '\n' {
			return fmt.Errorf("%w: incomplete final record", ErrCorrupt)
		}
	}
	if _, err := journal.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind audit journal: %w", err)
	}

	scanner := bufio.NewScanner(journal.file)
	scanner.Buffer(make([]byte, 64*1024), maxJournalLineBytes)
	expectedSequence := uint64(1)
	var previousHash [sha256.Size]byte
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		record, err := decodeRecord(line)
		if err != nil {
			return fmt.Errorf("%w at sequence %d: %v", ErrCorrupt, expectedSequence, err)
		}
		payloadDigest := sha256.Sum256(record.Payload)
		storedPayloadDigest, err := decodeDigest(record.PayloadSHA256)
		if err != nil || !bytes.Equal(payloadDigest[:], storedPayloadDigest[:]) {
			return fmt.Errorf("%w at sequence %d: payload digest mismatch", ErrCorrupt, expectedSequence)
		}
		storedPrevious, err := decodeDigest(record.PreviousRecordSHA256)
		if err != nil || !bytes.Equal(previousHash[:], storedPrevious[:]) {
			return fmt.Errorf("%w at sequence %d: previous hash mismatch", ErrCorrupt, expectedSequence)
		}
		expectedRecordHash := hashRecord(record.Sequence, record.EventID, record.ReceivedAt, payloadDigest, previousHash)
		storedRecordHash, err := decodeDigest(record.RecordSHA256)
		if err != nil || !bytes.Equal(expectedRecordHash[:], storedRecordHash[:]) {
			return fmt.Errorf("%w at sequence %d: record hash mismatch", ErrCorrupt, expectedSequence)
		}
		if record.Sequence != expectedSequence || !validEventID(record.EventID) || record.ReceivedAt.IsZero() || !validPayload(record.Payload) {
			return fmt.Errorf("%w at sequence %d: invalid record metadata", ErrCorrupt, expectedSequence)
		}
		if _, duplicate := journal.eventHashes[record.EventID]; duplicate {
			return fmt.Errorf("%w at sequence %d: duplicate event id", ErrCorrupt, expectedSequence)
		}
		journal.eventHashes[record.EventID] = payloadDigest
		previousHash = expectedRecordHash
		expectedSequence++
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan audit journal: %w", err)
	}
	journal.nextSequence = expectedSequence
	journal.lastHash = previousHash
	if _, err := journal.file.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("seek audit journal end: %w", err)
	}
	return nil
}

func decodeRecord(encoded []byte) (diskRecord, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var record diskRecord
	if err := decoder.Decode(&record); err != nil {
		return diskRecord{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return diskRecord{}, errors.New("multiple JSON values")
		}
		return diskRecord{}, err
	}
	return record, nil
}

func hashRecord(sequence uint64, eventID string, receivedAt time.Time, payloadHash, previousHash [sha256.Size]byte) [sha256.Size]byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte(recordHashDomain))
	var sequenceBytes [8]byte
	binary.BigEndian.PutUint64(sequenceBytes[:], sequence)
	_, _ = hash.Write(sequenceBytes[:])
	writeLengthPrefixed(hash, []byte(eventID))
	writeLengthPrefixed(hash, []byte(receivedAt.UTC().Format(time.RFC3339Nano)))
	_, _ = hash.Write(previousHash[:])
	_, _ = hash.Write(payloadHash[:])
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result
}

func writeLengthPrefixed(writer io.Writer, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = writer.Write(size[:])
	_, _ = writer.Write(value)
}

func decodeDigest(encoded string) ([sha256.Size]byte, error) {
	var result [sha256.Size]byte
	decoded, err := hex.DecodeString(encoded)
	if err != nil || len(decoded) != sha256.Size {
		return result, errors.New("invalid SHA-256 digest")
	}
	copy(result[:], decoded)
	return result, nil
}

func validPayload(payload []byte) bool {
	if len(payload) < 2 || len(payload) > MaxPayloadBytes || !json.Valid(payload) {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(payload, &object) == nil && object != nil
}

func validEventID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}
