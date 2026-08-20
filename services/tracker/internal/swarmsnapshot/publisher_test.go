package swarmsnapshot

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/peergo/peergo/services/tracker/internal/swarm"
)

func TestPublisherRetriesIdenticalChunkAndPreservesOrder(t *testing.T) {
	t.Parallel()
	factory, err := NewFactory("tracker-primary", 4, 1, bytes.NewReader(bytes.Repeat([]byte{0x61}, 96)))
	if err != nil {
		t.Fatal(err)
	}
	sequence := &fixedSequence{next: 7}
	sink := &recordingSink{failures: 1}
	publisher, err := NewPublisher(
		fixedSnapshotSource{entries: []swarm.SnapshotEntry{
			{InfoHash: [20]byte{2}, Seeders: 3, Leechers: 1},
			{InfoHash: [20]byte{1}, Seeders: 2, Leechers: 4},
		}},
		factory,
		sequence,
		sink,
		PublisherConfig{Interval: time.Second, PublishTimeout: time.Second, RetryMinimum: 10 * time.Millisecond, RetryMaximum: 20 * time.Millisecond},
		func() time.Time { return time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC) },
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.publishSnapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sequence.calls != 1 || len(sink.attempts) != 3 {
		t.Fatalf("sequence calls=%d sink attempts=%d", sequence.calls, len(sink.attempts))
	}
	if sink.attempts[0].Event.EventID != sink.attempts[1].Event.EventID ||
		!bytes.Equal(sink.attempts[0].Payload, sink.attempts[1].Payload) {
		t.Fatal("retry changed the chunk identity or canonical payload")
	}
	if sink.attempts[1].Event.ChunkIndex != 0 || sink.attempts[2].Event.ChunkIndex != 1 ||
		sink.attempts[1].Event.SnapshotID != sink.attempts[2].Event.SnapshotID ||
		sink.attempts[2].Event.SnapshotSequence != 7 {
		t.Fatalf("attempts = %+v", sink.attempts)
	}
}

type fixedSnapshotSource struct{ entries []swarm.SnapshotEntry }

func (source fixedSnapshotSource) Snapshot() []swarm.SnapshotEntry { return source.entries }

type fixedSequence struct {
	next  int64
	calls int
}

func (sequence *fixedSequence) Reserve() (int64, error) {
	sequence.calls++
	return sequence.next, nil
}

type recordingSink struct {
	failures int
	attempts []EncodedChunk
}

func (sink *recordingSink) Publish(_ context.Context, chunk EncodedChunk) error {
	sink.attempts = append(sink.attempts, chunk)
	if len(sink.attempts) <= sink.failures {
		return errors.New("temporary publish failure")
	}
	return nil
}
