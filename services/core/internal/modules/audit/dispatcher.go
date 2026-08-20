package audit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

const (
	defaultBatchSize     int32 = 20
	defaultLeaseDuration       = 30 * time.Second
	defaultPollInterval        = time.Second
	maxRetryDelay              = time.Minute
)

type DeliveryRepository interface {
	Claim(context.Context, time.Time, int32, time.Duration) ([]PendingEvent, error)
	MarkDelivered(context.Context, uuid.UUID, time.Time) error
	Release(context.Context, uuid.UUID, time.Time, string) error
}

type Sink interface {
	Append(context.Context, Event) error
}

type DispatcherConfig struct {
	BatchSize     int32
	LeaseDuration time.Duration
	PollInterval  time.Duration
	Now           func() time.Time
}

// Dispatcher performs at-least-once delivery. A crash after Sink.Append and
// before MarkDelivered intentionally causes a duplicate; Audit Sink must use
// event_id plus payload hash to make that replay idempotent.
type Dispatcher struct {
	repository    DeliveryRepository
	sink          Sink
	batchSize     int32
	leaseDuration time.Duration
	pollInterval  time.Duration
	now           func() time.Time
	logger        *slog.Logger
}

func NewDispatcher(repository DeliveryRepository, sink Sink, config DispatcherConfig, logger *slog.Logger) (*Dispatcher, error) {
	if repository == nil || sink == nil {
		return nil, errors.New("audit dispatcher repository and sink are required")
	}
	if config.BatchSize == 0 {
		config.BatchSize = defaultBatchSize
	}
	if config.LeaseDuration == 0 {
		config.LeaseDuration = defaultLeaseDuration
	}
	if config.PollInterval == 0 {
		config.PollInterval = defaultPollInterval
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.BatchSize < 1 || config.BatchSize > 100 || config.LeaseDuration <= 0 || config.LeaseDuration > 5*time.Minute || config.PollInterval <= 0 {
		return nil, errors.New("audit dispatcher configuration is invalid")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Dispatcher{
		repository:    repository,
		sink:          sink,
		batchSize:     config.BatchSize,
		leaseDuration: config.LeaseDuration,
		pollInterval:  config.PollInterval,
		now:           config.Now,
		logger:        logger,
	}, nil
}

// RunOnce claims and processes one bounded batch. It continues after individual
// delivery failures so one poisoned destination response cannot starve later
// evidence; all failures are joined for observability.
func (dispatcher *Dispatcher) RunOnce(ctx context.Context) (int, error) {
	now := dispatcher.now().UTC()
	events, err := dispatcher.repository.Claim(ctx, now, dispatcher.batchSize, dispatcher.leaseDuration)
	if err != nil {
		return 0, err
	}

	var failures []error
	for _, pending := range events {
		if err := dispatcher.sink.Append(ctx, pending.Event); err != nil {
			nextAttempt := dispatcher.now().UTC().Add(retryDelay(pending.Attempts))
			if releaseErr := dispatcher.repository.Release(ctx, pending.ID, nextAttempt, "sink_delivery_failed"); releaseErr != nil {
				failures = append(failures, fmt.Errorf("deliver audit event %s: %w; release: %v", pending.ID, err, releaseErr))
				continue
			}
			failures = append(failures, fmt.Errorf("deliver audit event %s: %w", pending.ID, err))
			continue
		}
		if err := dispatcher.repository.MarkDelivered(ctx, pending.ID, dispatcher.now().UTC()); err != nil {
			// Do not call Release here. The sink already fsynced the event, and a
			// lease expiry followed by idempotent replay is the safe recovery path.
			failures = append(failures, fmt.Errorf("mark audit event %s delivered: %w", pending.ID, err))
		}
	}
	return len(events), errors.Join(failures...)
}

func (dispatcher *Dispatcher) Run(ctx context.Context) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}

		processed, err := dispatcher.RunOnce(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			dispatcher.logger.ErrorContext(ctx, "audit dispatch batch failed", "error", err)
		}
		delay := dispatcher.pollInterval
		if processed == int(dispatcher.batchSize) {
			delay = 0
		}
		timer.Reset(delay)
	}
}

func retryDelay(attempt int32) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 6 {
		shift = 6
	}
	delay := time.Second * time.Duration(1<<shift)
	if delay > maxRetryDelay {
		return maxRetryDelay
	}
	return delay
}
