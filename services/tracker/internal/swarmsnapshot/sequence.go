package swarmsnapshot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/peergo/peergo/contracts/go/signedsnapshotv1"
	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
)

const sequenceSchemaVersion = "tracker.swarm.sequence.v1"

type SequenceStore interface {
	Reserve() (int64, error)
}

type sequenceState struct {
	SchemaVersion string `json:"schema_version"`
	SourceID      string `json:"source_id"`
	RoutingEpoch  int64  `json:"routing_epoch"`
	LastSequence  int64  `json:"last_sequence"`
}

type FileSequenceStore struct {
	mu           sync.Mutex
	path         string
	sourceID     string
	routingEpoch int64
	last         int64
}

func OpenFileSequenceStore(path, sourceID string, routingEpoch int64) (*FileSequenceStore, error) {
	if !filepath.IsAbs(path) || !trackerswarmv1.ValidSourceID(sourceID) || routingEpoch < 1 {
		return nil, ErrConfig
	}
	path = filepath.Clean(path)
	store := &FileSequenceStore{path: path, sourceID: sourceID, routingEpoch: routingEpoch}
	encoded, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Tracker swarm snapshot sequence: %w", err)
	}
	if len(encoded) < 2 || len(encoded) > 4096 {
		return nil, errors.New("Tracker swarm snapshot sequence state is corrupt")
	}
	var state sequenceState
	if err := signedsnapshotv1.StrictJSON(encoded, &state); err != nil || validateSequenceState(state) != nil {
		return nil, errors.New("Tracker swarm snapshot sequence state is corrupt")
	}
	canonical, err := json.Marshal(state)
	if err != nil || !bytes.Equal(canonical, encoded) || state.SourceID != sourceID || state.RoutingEpoch > routingEpoch {
		return nil, errors.New("Tracker swarm snapshot sequence state does not match configured ownership")
	}
	if state.RoutingEpoch == routingEpoch {
		store.last = state.LastSequence
	}
	return store, nil
}

func (store *FileSequenceStore) Reserve() (int64, error) {
	if store == nil {
		return 0, ErrConfig
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.last == math.MaxInt64 {
		return 0, errors.New("Tracker swarm snapshot sequence is exhausted")
	}
	next := store.last + 1
	state := sequenceState{
		SchemaVersion: sequenceSchemaVersion, SourceID: store.sourceID,
		RoutingEpoch: store.routingEpoch, LastSequence: next,
	}
	if err := writeSequenceState(store.path, state); err != nil {
		return 0, err
	}
	store.last = next
	return next, nil
}

func validateSequenceState(state sequenceState) error {
	if state.SchemaVersion != sequenceSchemaVersion || !trackerswarmv1.ValidSourceID(state.SourceID) ||
		state.RoutingEpoch < 1 || state.LastSequence < 1 {
		return ErrConfig
	}
	return nil
}

func writeSequenceState(path string, state sequenceState) error {
	encoded, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode Tracker swarm snapshot sequence: %w", err)
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create Tracker swarm snapshot sequence directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".peergo-swarm-sequence-*")
	if err != nil {
		return fmt.Errorf("create Tracker swarm snapshot sequence temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}
	if err := temporary.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("protect Tracker swarm snapshot sequence: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		cleanup()
		return fmt.Errorf("write Tracker swarm snapshot sequence: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync Tracker swarm snapshot sequence: %w", err)
	}
	if err := temporary.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close Tracker swarm snapshot sequence: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		cleanup()
		return fmt.Errorf("replace Tracker swarm snapshot sequence: %w", err)
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open Tracker swarm snapshot sequence directory: %w", err)
	}
	defer directoryHandle.Close()
	if err := directoryHandle.Sync(); err != nil {
		return fmt.Errorf("sync Tracker swarm snapshot sequence directory: %w", err)
	}
	return nil
}
