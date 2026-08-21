package projectionrunner

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

func TestRunnerRunsFixedLanesConcurrently(t *testing.T) {
	const stream = "PEERGO_TEST_STREAM"
	const subject = "peergo.test.result.v1"
	const durable = "PEERGO_TEST_DURABLE"
	source := &concurrentSource{remaining: 4, stream: stream, subject: subject, durable: durable}
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	var acknowledged atomic.Int32
	runner, err := New[int](source, func(ctx context.Context, _ []byte, _ time.Time) (int, error) {
		started <- struct{}{}
		select {
		case <-release:
			return 1, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}, Config{
		Stream: stream, Subject: subject, Durable: durable,
		ProcessTimeout: time.Second, AckTimeout: time.Second,
		RetryDelay: 10 * time.Millisecond, Concurrency: 4,
	}, Semantics[int]{
		Name: "test", EventID: func(result int) any { return result }, Duplicate: func(int) bool { return false },
		IsPermanent: func(error) bool { return false }, Invariant: errors.New("test invariant"),
	}, time.Now, nil)
	if err != nil {
		t.Fatal(err)
	}
	source.acknowledged = &acknowledged

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	for lane := 0; lane < 4; lane++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("projector did not start all four lanes")
		}
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for acknowledged.Load() != 4 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if acknowledged.Load() != 4 {
		t.Fatalf("acknowledged = %d", acknowledged.Load())
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("projector did not stop after cancellation")
	}
}

type concurrentSource struct {
	mu           sync.Mutex
	remaining    int
	stream       string
	subject      string
	durable      string
	acknowledged *atomic.Int32
}

func (source *concurrentSource) Next(ctx context.Context) (Message, error) {
	source.mu.Lock()
	if source.remaining > 0 {
		sequence := uint64(source.remaining)
		source.remaining--
		source.mu.Unlock()
		return &concurrentMessage{stream: source.stream, subject: source.subject, durable: source.durable,
			sequence: sequence, acknowledged: source.acknowledged}, nil
	}
	source.mu.Unlock()
	<-ctx.Done()
	return nil, ctx.Err()
}

type concurrentMessage struct {
	stream       string
	subject      string
	durable      string
	sequence     uint64
	acknowledged *atomic.Int32
}

func (message *concurrentMessage) Metadata() (*jetstream.MsgMetadata, error) {
	return &jetstream.MsgMetadata{Stream: message.stream, Consumer: message.durable,
		Sequence: jetstream.SequencePair{Stream: message.sequence}, NumDelivered: 1}, nil
}
func (*concurrentMessage) Data() []byte            { return []byte("test") }
func (message *concurrentMessage) Subject() string { return message.subject }
func (message *concurrentMessage) DoubleAck(context.Context) error {
	message.acknowledged.Add(1)
	return nil
}
func (*concurrentMessage) InProgress() error { return nil }
