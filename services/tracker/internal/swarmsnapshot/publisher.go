package swarmsnapshot

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/peergo/peergo/services/tracker/internal/swarm"
)

type SnapshotSource interface {
	Snapshot() []swarm.SnapshotEntry
}

type Sink interface {
	Publish(context.Context, EncodedChunk) error
}

type PublisherConfig struct {
	Interval       time.Duration
	PublishTimeout time.Duration
	RetryMinimum   time.Duration
	RetryMaximum   time.Duration
}

type Publisher struct {
	source   SnapshotSource
	factory  *Factory
	sequence SequenceStore
	sink     Sink
	config   PublisherConfig
	now      func() time.Time
	logger   *slog.Logger
}

func NewPublisher(source SnapshotSource, factory *Factory, sequence SequenceStore, sink Sink, config PublisherConfig, now func() time.Time, logger *slog.Logger) (*Publisher, error) {
	if source == nil || factory == nil || sequence == nil || sink == nil || config.Interval < time.Second ||
		config.Interval > time.Hour || config.PublishTimeout < 100*time.Millisecond || config.PublishTimeout > time.Minute ||
		config.RetryMinimum < 10*time.Millisecond || config.RetryMaximum < config.RetryMinimum || config.RetryMaximum > 5*time.Minute {
		return nil, ErrConfig
	}
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Publisher{source: source, factory: factory, sequence: sequence, sink: sink, config: config, now: now, logger: logger}, nil
}

func (publisher *Publisher) Run(ctx context.Context) error {
	for {
		if err := publisher.publishSnapshot(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			publisher.logger.Error("Tracker swarm snapshot publication deferred", "error", err)
		}
		if !wait(ctx, publisher.config.Interval) {
			return nil
		}
	}
}

func (publisher *Publisher) publishSnapshot(ctx context.Context) error {
	sequence, err := publisher.sequence.Reserve()
	if err != nil {
		return fmt.Errorf("reserve Tracker swarm snapshot sequence: %w", err)
	}
	observedAt := publisher.now().UTC().Round(0)
	chunks, err := publisher.factory.Build(sequence, observedAt, publisher.source.Snapshot())
	if err != nil {
		return fmt.Errorf("build Tracker swarm snapshot: %w", err)
	}
	backoff := publisher.config.RetryMinimum
	for _, chunk := range chunks {
		for {
			attemptCtx, cancel := context.WithTimeout(ctx, publisher.config.PublishTimeout)
			err := publisher.sink.Publish(attemptCtx, chunk)
			cancel()
			if err == nil {
				backoff = publisher.config.RetryMinimum
				break
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			publisher.logger.Warn("Tracker swarm snapshot chunk publish failed", "snapshot_id", chunk.Event.SnapshotID, "chunk_index", chunk.Event.ChunkIndex, "error", err, "retry_in", backoff)
			if !wait(ctx, backoff) {
				return ctx.Err()
			}
			backoff = nextBackoff(backoff, publisher.config.RetryMaximum)
		}
	}
	publisher.logger.Debug("Tracker swarm snapshot published", "snapshot_id", chunks[0].Event.SnapshotID, "snapshot_sequence", sequence, "chunk_count", len(chunks))
	return nil
}

func nextBackoff(current, maximum time.Duration) time.Duration {
	if current >= maximum/2 {
		return maximum
	}
	return current * 2
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
