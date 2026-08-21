package outboxdispatch

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestDispatcherRunsFixedLanesConcurrently(t *testing.T) {
	repository := &concurrentRepository{remaining: 4, published: make(chan struct{}, 4)}
	publisher := &blockingPublisher{started: make(chan struct{}, 4), release: make(chan struct{})}
	dispatcher, err := New[int](repository, publisher, Config{
		LeaseDuration: time.Second, IdleInterval: 50 * time.Millisecond,
		RetryBase: 100 * time.Millisecond, PublishTimeout: time.Second, Concurrency: 4,
	}, Semantics[int]{
		Name: "test", RetryCode: "publish_failed", EventID: func(event int) any { return event },
		IsPermanent: func(error) bool { return false },
	}, time.Now, nil)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- dispatcher.Run(ctx) }()
	for lane := 0; lane < 4; lane++ {
		select {
		case <-publisher.started:
		case <-time.After(time.Second):
			t.Fatal("dispatcher did not start all four lanes")
		}
	}
	close(publisher.release)
	for event := 0; event < 4; event++ {
		select {
		case <-repository.published:
		case <-time.After(time.Second):
			t.Fatal("dispatcher did not publish all claimed events")
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("dispatcher did not stop after cancellation")
	}
}

type concurrentRepository struct {
	mu        sync.Mutex
	remaining int
	published chan struct{}
}

func (repository *concurrentRepository) ClaimNext(context.Context, time.Time, time.Duration) (int, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.remaining == 0 {
		return 0, false, nil
	}
	event := repository.remaining
	repository.remaining--
	return event, true, nil
}

func (repository *concurrentRepository) MarkPublished(context.Context, int, time.Time) error {
	repository.published <- struct{}{}
	return nil
}

func (*concurrentRepository) Release(context.Context, int, time.Time, string) error { return nil }

type blockingPublisher struct {
	started chan struct{}
	release chan struct{}
}

func (publisher *blockingPublisher) Publish(ctx context.Context, _ int) error {
	publisher.started <- struct{}{}
	select {
	case <-publisher.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
