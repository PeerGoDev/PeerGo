package trackercontrol

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/trackerevent"
)

type recordingProjectionRepository struct {
	pending    PendingEvent
	found      bool
	claimAt    time.Time
	lease      time.Duration
	applyAt    time.Time
	applyErr   error
	releasedAt time.Time
	errorCode  string
	releaseErr error
}

func (repository *recordingProjectionRepository) ClaimNext(_ context.Context, now time.Time, lease time.Duration) (PendingEvent, bool, error) {
	repository.claimAt = now
	repository.lease = lease
	return repository.pending, repository.found, nil
}

func (repository *recordingProjectionRepository) Apply(_ context.Context, _ PendingEvent, at time.Time) error {
	repository.applyAt = at
	return repository.applyErr
}

func (repository *recordingProjectionRepository) Release(_ context.Context, _ PendingEvent, availableAt time.Time, code string) error {
	repository.releasedAt = availableAt
	repository.errorCode = code
	return repository.releaseErr
}

func TestProjectorAppliesOneClaimedEvent(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 8, 16, 0, 0, 0, time.UTC)
	repository := &recordingProjectionRepository{found: true, pending: pendingProjectionEvent(t, 7, 1)}
	projector, err := NewProjector(repository, ProjectorConfig{}, func() time.Time { return now }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	processed, err := projector.RunOnce(context.Background())
	if err != nil || !processed || repository.claimAt != now || repository.lease != time.Minute || repository.applyAt != now {
		t.Fatalf("RunOnce() processed=%v claim=%v lease=%v apply=%v error=%v", processed, repository.claimAt, repository.lease, repository.applyAt, err)
	}
	if !repository.releasedAt.IsZero() {
		t.Fatal("successful event was released")
	}
}

func TestProjectorReleasesFailureWithBoundedBackoff(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 8, 16, 30, 0, 0, time.UTC)
	repository := &recordingProjectionRepository{
		found: true, pending: pendingProjectionEvent(t, 8, 4), applyErr: errors.New("projection unavailable"),
	}
	projector, err := NewProjector(repository, ProjectorConfig{RetryBase: 2 * time.Second}, func() time.Time { return now }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	processed, err := projector.RunOnce(context.Background())
	if err != nil || !processed {
		t.Fatalf("RunOnce() processed=%v error=%v", processed, err)
	}
	if want := now.Add(16 * time.Second); repository.releasedAt != want || repository.errorCode != "projection_failed" {
		t.Fatalf("release at=%v code=%q, want %v", repository.releasedAt, repository.errorCode, want)
	}
}

func pendingProjectionEvent(t *testing.T, sequence int64, attempts int32) PendingEvent {
	t.Helper()
	var hash [20]byte
	hash[0] = byte(sequence)
	event, err := trackerevent.NewTorrentEligibilityChanged(trackerevent.TorrentEligibilityInput{
		EventID: uuid.New(), OccurredAt: time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC),
		TorrentID: sequence, InfoHashV1: hash,
		TotalSizeBytes: 1, Enabled: true, TorrentVersion: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	return PendingEvent{Sequence: sequence, LeaseToken: uuid.New(), Attempts: attempts, Event: event}
}
