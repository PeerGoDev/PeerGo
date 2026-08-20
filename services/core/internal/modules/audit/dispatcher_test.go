package audit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
)

type memoryDeliveryRepository struct {
	pending       []PendingEvent
	claimErr      error
	markErr       error
	releaseErr    error
	marked        []uuid.UUID
	released      []uuid.UUID
	releaseAt     time.Time
	releaseReason string
}

func (repository *memoryDeliveryRepository) Claim(_ context.Context, _ time.Time, _ int32, _ time.Duration) ([]PendingEvent, error) {
	return append([]PendingEvent(nil), repository.pending...), repository.claimErr
}

func (repository *memoryDeliveryRepository) MarkDelivered(_ context.Context, eventID uuid.UUID, _ time.Time) error {
	repository.marked = append(repository.marked, eventID)
	return repository.markErr
}

func (repository *memoryDeliveryRepository) Release(_ context.Context, eventID uuid.UUID, availableAt time.Time, reason string) error {
	repository.released = append(repository.released, eventID)
	repository.releaseAt = availableAt
	repository.releaseReason = reason
	return repository.releaseErr
}

type memorySink struct {
	appended []uuid.UUID
	err      error
}

func (sink *memorySink) Append(_ context.Context, event Event) error {
	sink.appended = append(sink.appended, event.ID)
	return sink.err
}

func TestDispatcherMarksSuccessAndReleasesFailureWithBackoff(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	eventID := uuid.New()
	event := PendingEvent{Event: Event{ID: eventID}, Attempts: 1}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("success", func(t *testing.T) {
		repository := &memoryDeliveryRepository{pending: []PendingEvent{event}}
		sink := &memorySink{}
		dispatcher, err := NewDispatcher(repository, sink, DispatcherConfig{Now: func() time.Time { return now }}, logger)
		if err != nil {
			t.Fatalf("NewDispatcher() error = %v", err)
		}
		processed, err := dispatcher.RunOnce(context.Background())
		if err != nil || processed != 1 {
			t.Fatalf("RunOnce() = (%d, %v)", processed, err)
		}
		if len(repository.marked) != 1 || len(repository.released) != 0 || len(sink.appended) != 1 {
			t.Fatalf("marked=%v released=%v appended=%v", repository.marked, repository.released, sink.appended)
		}
	})

	t.Run("delivery failure", func(t *testing.T) {
		repository := &memoryDeliveryRepository{pending: []PendingEvent{event}}
		sink := &memorySink{err: errors.New("sink unavailable")}
		dispatcher, err := NewDispatcher(repository, sink, DispatcherConfig{Now: func() time.Time { return now }}, logger)
		if err != nil {
			t.Fatalf("NewDispatcher() error = %v", err)
		}
		processed, err := dispatcher.RunOnce(context.Background())
		if err == nil || processed != 1 {
			t.Fatalf("RunOnce() = (%d, %v), want delivery failure", processed, err)
		}
		if len(repository.marked) != 0 || len(repository.released) != 1 || !repository.releaseAt.Equal(now.Add(time.Second)) || repository.releaseReason != "sink_delivery_failed" {
			t.Fatalf("marked=%v released=%v releaseAt=%s reason=%q", repository.marked, repository.released, repository.releaseAt, repository.releaseReason)
		}
	})

	t.Run("mark failure relies on idempotent replay", func(t *testing.T) {
		repository := &memoryDeliveryRepository{pending: []PendingEvent{event}, markErr: errors.New("database unavailable")}
		dispatcher, err := NewDispatcher(repository, &memorySink{}, DispatcherConfig{Now: func() time.Time { return now }}, logger)
		if err != nil {
			t.Fatalf("NewDispatcher() error = %v", err)
		}
		_, err = dispatcher.RunOnce(context.Background())
		if err == nil || len(repository.marked) != 1 || len(repository.released) != 0 {
			t.Fatalf("RunOnce() error=%v marked=%v released=%v", err, repository.marked, repository.released)
		}
	})
}

func TestRetryDelayIsBounded(t *testing.T) {
	t.Parallel()

	if retryDelay(1) != time.Second || retryDelay(4) != 8*time.Second || retryDelay(100) != time.Minute {
		t.Fatalf("retry delays = %s, %s, %s", retryDelay(1), retryDelay(4), retryDelay(100))
	}
}
