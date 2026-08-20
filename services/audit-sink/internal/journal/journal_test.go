package journal

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	testEventID  = "0198f20a-6da8-7e51-9c64-555555555551"
	testEventID2 = "0198f20a-6da8-7e51-9c64-555555555552"
)

func TestJournalPersistsHashChainAndHandlesIdempotentReplay(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "audit.jsonl")
	journalStore, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	payload := []byte(`{"event_id":"` + testEventID + `","result":"allow"}`)

	created, err := journalStore.Append(testEventID, payload, now)
	if err != nil || !created {
		t.Fatalf("Append() = (%t, %v), want created", created, err)
	}
	created, err = journalStore.Append(testEventID, append([]byte(nil), payload...), now.Add(time.Second))
	if err != nil || created {
		t.Fatalf("duplicate Append() = (%t, %v), want idempotent", created, err)
	}
	if _, err := journalStore.Append(testEventID, []byte(`{"different":true}`), now); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting Append() error = %v, want ErrConflict", err)
	}
	if created, err := journalStore.Append(testEventID2, []byte(`{"event_id":"`+testEventID2+`"}`), now.Add(time.Second)); err != nil || !created {
		t.Fatalf("second Append() = (%t, %v), want created", created, err)
	}
	if err := journalStore.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	created, err = reopened.Append(testEventID, payload, now.Add(2*time.Second))
	if err != nil || created {
		t.Fatalf("reopened duplicate Append() = (%t, %v), want idempotent", created, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("journal permissions = %o, want private", info.Mode().Perm())
	}
}

func TestJournalRefusesTamperedOrIncompleteHistory(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	t.Run("tampered payload", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "audit.jsonl")
		store, err := Open(path)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		if _, err := store.Append(testEventID, []byte(`{"event_id":"`+testEventID+`"}`), now); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		encoded, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile() error = %v", err)
		}
		var record diskRecord
		if err := json.Unmarshal(bytes.TrimSpace(encoded), &record); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		record.Payload = []byte(`{"tampered":true}`)
		encoded, err = json.Marshal(record)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		encoded = append(encoded, '\n')
		if err := os.WriteFile(path, encoded, 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		if _, err := Open(path); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("Open(tampered) error = %v, want ErrCorrupt", err)
		}
	})

	t.Run("incomplete tail", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "audit.jsonl")
		if err := os.WriteFile(path, []byte(`{"sequence":1}`), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		if _, err := Open(path); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("Open(incomplete) error = %v, want ErrCorrupt", err)
		}
	})
}
