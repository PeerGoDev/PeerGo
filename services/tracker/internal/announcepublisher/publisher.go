// Package announcepublisher drains the durable Tracker WAL in order. A record
// advances only after the sink returns a storage ACK and the local checkpoint
// is durably replaced. Publish failures are retried without affecting announce
// availability; local WAL integrity failures stop the process fail closed.
package announcepublisher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/peergo/peergo/services/tracker/internal/wal"
)

var ErrConfig = errors.New("Tracker announce publisher configuration is invalid")

type EventLog interface {
	Next() (wal.Record, bool, error)
	Acknowledge(wal.Record) error
	CompactAcknowledged(int64) (bool, error)
	Wait(context.Context) error
}

type Sink interface {
	Publish(context.Context, string, []byte) error
}

type Config struct {
	PublishTimeout time.Duration
	RetryMinimum   time.Duration
	RetryMaximum   time.Duration
	CompactAtBytes int64
}

type Publisher struct {
	log    EventLog
	sink   Sink
	config Config
	logger *slog.Logger
}

func New(log EventLog, sink Sink, config Config, logger *slog.Logger) (*Publisher, error) {
	if log == nil || sink == nil || config.PublishTimeout < time.Millisecond || config.PublishTimeout > time.Minute ||
		config.RetryMinimum < time.Millisecond || config.RetryMaximum < config.RetryMinimum ||
		config.RetryMaximum > 5*time.Minute || config.CompactAtBytes < 1 {
		return nil, ErrConfig
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Publisher{log: log, sink: sink, config: config, logger: logger}, nil
}

func (publisher *Publisher) Run(ctx context.Context) error {
	backoff := publisher.config.RetryMinimum
	for {
		record, found, err := publisher.log.Next()
		if err != nil {
			return fmt.Errorf("read Tracker announce WAL: %w", err)
		}
		if !found {
			if _, err := publisher.log.CompactAcknowledged(publisher.config.CompactAtBytes); err != nil {
				publisher.logger.Warn("Tracker WAL compaction deferred", "error", err, "retry_in", backoff)
				if !wait(ctx, backoff) {
					return nil
				}
				backoff = nextBackoff(backoff, publisher.config.RetryMaximum)
				continue
			}
			backoff = publisher.config.RetryMinimum
			if err := publisher.log.Wait(ctx); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return nil
				}
				return fmt.Errorf("wait for Tracker announce WAL: %w", err)
			}
			continue
		}

		for {
			attemptCtx, cancel := context.WithTimeout(ctx, publisher.config.PublishTimeout)
			err := publisher.sink.Publish(attemptCtx, record.Event.EventID, record.Payload)
			cancel()
			if err == nil {
				backoff = publisher.config.RetryMinimum
				break
			}
			publisher.logger.Warn("Tracker announce event publish failed", "error", err, "retry_in", backoff)
			if !wait(ctx, backoff) {
				return nil
			}
			backoff = nextBackoff(backoff, publisher.config.RetryMaximum)
		}

		for {
			if err := publisher.log.Acknowledge(record); err == nil {
				backoff = publisher.config.RetryMinimum
				break
			} else if errors.Is(err, wal.ErrCursor) || errors.Is(err, wal.ErrCorrupt) || errors.Is(err, wal.ErrUnsafe) {
				return fmt.Errorf("advance Tracker announce WAL checkpoint: %w", err)
			} else {
				// The JetStream ACK is already authoritative. Retrying only the
				// checkpoint avoids unnecessary live duplicates; a crash before
				// success still intentionally replays the same event ID.
				publisher.logger.Error("Tracker WAL checkpoint update failed", "error", err, "retry_in", backoff)
				if !wait(ctx, backoff) {
					return nil
				}
				backoff = nextBackoff(backoff, publisher.config.RetryMaximum)
			}
		}
	}
}

func nextBackoff(current, maximum time.Duration) time.Duration {
	if current >= maximum/2 {
		return maximum
	}
	return current * 2
}

func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
