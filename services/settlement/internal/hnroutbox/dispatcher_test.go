package hnroutbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDispatcherMarksHNRPublishedAfterStorageAck(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 10, 9, 0, 0, 0, time.UTC)
	repository := &recordingRepository{pending: PendingEvent{EventID: uuid.New(), LeaseToken: uuid.New(), Attempts: 1}, found: true}
	dispatcher, err := NewDispatcher(repository, recordingPublisher{}, DispatcherConfig{}, func() time.Time { return now }, nil)
	if err != nil {
		t.Fatal(err)
	}
	processed, err := dispatcher.RunOnce(context.Background())
	if err != nil || !processed || !repository.publishedAt.Equal(now) || repository.released {
		t.Fatalf("RunOnce() processed=%v published=%s released=%v error=%v", processed, repository.publishedAt, repository.released, err)
	}
}

func TestDispatcherRetriesTransientHNRPublish(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 10, 9, 0, 0, 0, time.UTC)
	repository := &recordingRepository{pending: PendingEvent{EventID: uuid.New(), LeaseToken: uuid.New(), Attempts: 2}, found: true}
	dispatcher, err := NewDispatcher(repository, recordingPublisher{err: errors.New("NATS unavailable")}, DispatcherConfig{RetryBase: time.Second}, func() time.Time { return now }, nil)
	if err != nil {
		t.Fatal(err)
	}
	processed, err := dispatcher.RunOnce(context.Background())
	if err != nil || !processed || !repository.released || repository.releaseCode != "hnr_publish_failed" ||
		!repository.availableAt.Equal(now.Add(2*time.Second)) {
		t.Fatalf("RunOnce() processed=%v released=%v code=%q at=%s error=%v",
			processed, repository.released, repository.releaseCode, repository.availableAt, err)
	}
}

type recordingRepository struct {
	pending     PendingEvent
	found       bool
	publishedAt time.Time
	released    bool
	availableAt time.Time
	releaseCode string
}

func (repository *recordingRepository) ClaimNext(_ context.Context, _ time.Time, _ time.Duration) (PendingEvent, bool, error) {
	return repository.pending, repository.found, nil
}

func (repository *recordingRepository) MarkPublished(_ context.Context, _ PendingEvent, at time.Time) error {
	repository.publishedAt = at
	return nil
}

func (repository *recordingRepository) Release(_ context.Context, _ PendingEvent, at time.Time, code string) error {
	repository.released = true
	repository.availableAt = at
	repository.releaseCode = code
	return nil
}

type recordingPublisher struct{ err error }

func (publisher recordingPublisher) Publish(_ context.Context, _ PendingEvent) error {
	return publisher.err
}
