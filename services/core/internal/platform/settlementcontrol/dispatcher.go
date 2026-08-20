// Package settlementcontrol provides the reusable at-least-once dispatcher
// used by Core-owned control planes that append immutable commands in
// Settlement. Domain repositories still validate their own canonical payloads.
package settlementcontrol

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
)

type PendingCommand struct {
	ID         uuid.UUID
	Payload    []byte
	SHA256     [32]byte
	LeaseToken uuid.UUID
	Attempts   int32
}

type Repository interface {
	Claim(context.Context, time.Time, int32, time.Duration) ([]PendingCommand, error)
	MarkDelivered(context.Context, PendingCommand, time.Time) error
	Release(context.Context, PendingCommand, time.Time, string) error
}

type Sink interface {
	Append(context.Context, PendingCommand) error
}

type DispatcherConfig struct {
	BatchSize     int32
	LeaseDuration time.Duration
	PollInterval  time.Duration
	Now           func() time.Time
	Label         string
	FailureCode   string
}

type Dispatcher struct {
	repository Repository
	sink       Sink
	config     DispatcherConfig
	logger     *slog.Logger
}

func NewDispatcher(repository Repository, sink Sink, config DispatcherConfig, logger *slog.Logger) (*Dispatcher, error) {
	if repository == nil || sink == nil {
		return nil, errors.New("Settlement control dispatcher dependencies are required")
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
	if config.Label == "" {
		config.Label = "Settlement control"
	}
	if config.FailureCode == "" {
		config.FailureCode = "settlement_delivery_failed"
	}
	if config.BatchSize < 1 || config.BatchSize > 100 || config.LeaseDuration <= 0 ||
		config.LeaseDuration > 5*time.Minute || config.PollInterval <= 0 || len(config.FailureCode) > 64 {
		return nil, errors.New("Settlement control dispatcher configuration is invalid")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Dispatcher{repository: repository, sink: sink, config: config, logger: logger}, nil
}

func (dispatcher *Dispatcher) RunOnce(ctx context.Context) (int, error) {
	now := dispatcher.config.Now().UTC()
	pending, err := dispatcher.repository.Claim(ctx, now, dispatcher.config.BatchSize, dispatcher.config.LeaseDuration)
	if err != nil {
		return 0, err
	}
	var failures []error
	for _, command := range pending {
		if err := dispatcher.sink.Append(ctx, command); err != nil {
			next := dispatcher.config.Now().UTC().Add(retryDelay(command.Attempts))
			if releaseErr := dispatcher.repository.Release(ctx, command, next, dispatcher.config.FailureCode); releaseErr != nil {
				failures = append(failures, fmt.Errorf("deliver %s command %s: %w; release: %v", dispatcher.config.Label, command.ID, err, releaseErr))
				continue
			}
			failures = append(failures, fmt.Errorf("deliver %s command %s: %w", dispatcher.config.Label, command.ID, err))
			continue
		}
		if err := dispatcher.repository.MarkDelivered(ctx, command, dispatcher.config.Now().UTC()); err != nil {
			failures = append(failures, fmt.Errorf("mark %s command %s delivered: %w", dispatcher.config.Label, command.ID, err))
		}
	}
	return len(pending), errors.Join(failures...)
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
			dispatcher.logger.ErrorContext(ctx, "Settlement control delivery batch failed", "control", dispatcher.config.Label, "error", err)
		}
		delay := dispatcher.config.PollInterval
		if processed == int(dispatcher.config.BatchSize) {
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
	return time.Second * time.Duration(1<<shift)
}
