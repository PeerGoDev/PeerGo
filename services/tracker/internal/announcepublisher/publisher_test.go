package announcepublisher

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/peergo/peergo/services/tracker/internal/announceevent"
	"github.com/peergo/peergo/services/tracker/internal/wal"
)

func TestPublisherRetriesInOrderAndCheckpointsOnlyAcknowledgedRecords(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	log := &publisherTestLog{records: []wal.Record{
		{Offset: 0, NextOffset: 10, Event: announceevent.Event{EventID: "first"}, Payload: []byte("first-payload")},
		{Offset: 10, NextOffset: 20, Event: announceevent.Event{EventID: "second"}, Payload: []byte("second-payload")},
	}}
	sink := &publisherTestSink{failures: 1, cancelAfter: 2, cancel: cancel}
	publisher, err := New(log, sink, Config{
		PublishTimeout: 100 * time.Millisecond, RetryMinimum: time.Millisecond,
		RetryMaximum: 4 * time.Millisecond, CompactAtBytes: 1,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if want := []string{"first", "first", "second"}; !reflect.DeepEqual(sink.calls, want) {
		t.Fatalf("publish calls = %v, want %v", sink.calls, want)
	}
	if log.acknowledged != 2 || log.compactions < 1 {
		t.Fatalf("log acknowledged=%d compactions=%d", log.acknowledged, log.compactions)
	}
}

func TestPublisherStopsOnCheckpointInvariantFailure(t *testing.T) {
	log := &publisherTestLog{
		records:  []wal.Record{{Event: announceevent.Event{EventID: "first"}, Payload: []byte("payload")}},
		ackError: wal.ErrCursor,
	}
	publisher, err := New(log, &publisherTestSink{}, Config{
		PublishTimeout: 100 * time.Millisecond, RetryMinimum: time.Millisecond,
		RetryMaximum: 4 * time.Millisecond, CompactAtBytes: 1,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Run(context.Background()); !errors.Is(err, wal.ErrCursor) {
		t.Fatalf("Run() error = %v, want ErrCursor", err)
	}
}

type publisherTestLog struct {
	records      []wal.Record
	acknowledged int
	compactions  int
	ackError     error
}

func (log *publisherTestLog) Next() (wal.Record, bool, error) {
	if log.acknowledged >= len(log.records) {
		return wal.Record{}, false, nil
	}
	return log.records[log.acknowledged], true, nil
}

func (log *publisherTestLog) Acknowledge(wal.Record) error {
	if log.ackError != nil {
		return log.ackError
	}
	log.acknowledged++
	return nil
}

func (log *publisherTestLog) CompactAcknowledged(int64) (bool, error) {
	log.compactions++
	return true, nil
}

func (log *publisherTestLog) Wait(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

type publisherTestSink struct {
	failures    int
	successes   int
	cancelAfter int
	cancel      context.CancelFunc
	calls       []string
}

func (sink *publisherTestSink) Publish(_ context.Context, eventID string, _ []byte) error {
	sink.calls = append(sink.calls, eventID)
	if sink.failures > 0 {
		sink.failures--
		return errors.New("temporary publish failure")
	}
	sink.successes++
	if sink.cancel != nil && sink.successes == sink.cancelAfter {
		sink.cancel()
	}
	return nil
}
