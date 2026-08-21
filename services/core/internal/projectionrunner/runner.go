// Package projectionrunner owns Core's shared ordered JetStream
// commit-before-ACK loop. Event families supply typed projection results and
// permanent-error classification; they do not duplicate transport behavior.
package projectionrunner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	contract "github.com/peergo/peergo/contracts/go/jetstreamv1"
)

var ErrConfig = errors.New("Core projection runner configuration is invalid")

type Message interface {
	Metadata() (*jetstream.MsgMetadata, error)
	Data() []byte
	Subject() string
	DoubleAck(context.Context) error
	InProgress() error
}

type Source interface {
	Next(context.Context) (Message, error)
}

type Config struct {
	Stream         string
	Subject        string
	Durable        string
	ProcessTimeout time.Duration
	AckTimeout     time.Duration
	RetryDelay     time.Duration
	Concurrency    int
}

type Semantics[R any] struct {
	Name        string
	EventID     func(R) any
	Duplicate   func(R) bool
	IsPermanent func(error) bool
	Invariant   error
}

type ApplyFunc[R any] func(context.Context, []byte, time.Time) (R, error)

type Runner[R any] struct {
	source    Source
	apply     ApplyFunc[R]
	config    Config
	semantics Semantics[R]
	now       func() time.Time
	logger    *slog.Logger
}

func New[R any](source Source, apply ApplyFunc[R], config Config, semantics Semantics[R], now func() time.Time, logger *slog.Logger) (*Runner[R], error) {
	if config.Concurrency == 0 {
		config.Concurrency = 1
	}
	if source == nil || apply == nil || !contract.ValidStreamName(config.Stream) ||
		!contract.ValidLiteralSubject(config.Subject) || !contract.ValidStreamName(config.Durable) ||
		config.ProcessTimeout < 100*time.Millisecond || config.ProcessTimeout > 10*time.Minute ||
		config.AckTimeout < 100*time.Millisecond || config.AckTimeout > time.Minute ||
		config.RetryDelay < 10*time.Millisecond || config.RetryDelay > time.Minute ||
		config.Concurrency < 1 || config.Concurrency > 32 ||
		strings.TrimSpace(semantics.Name) == "" || semantics.EventID == nil || semantics.Duplicate == nil ||
		semantics.IsPermanent == nil || semantics.Invariant == nil {
		return nil, ErrConfig
	}
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Runner[R]{source: source, apply: apply, config: config, semantics: semantics, now: now, logger: logger}, nil
}

func (runner *Runner[R]) Run(ctx context.Context) error {
	if runner.config.Concurrency == 1 {
		return runner.runLane(ctx)
	}

	// JetStream distributes distinct explicit-ACK deliveries across these
	// fixed lanes. The projection transaction and event inbox remain the
	// exactly-once boundary; any permanent failure cancels every sibling lane
	// without acknowledging the offending event.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, runner.config.Concurrency)
	for lane := 0; lane < runner.config.Concurrency; lane++ {
		go func() {
			err := runner.runLane(runCtx)
			results <- err
			if err != nil {
				cancel()
			}
		}()
	}
	var firstErr error
	for lane := 0; lane < runner.config.Concurrency; lane++ {
		if err := <-results; firstErr == nil && err != nil {
			firstErr = err
		}
	}
	return firstErr
}

func (runner *Runner[R]) runLane(ctx context.Context) error {
	for {
		message, err := runner.source.Next(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			runner.logger.Warn("Core JetStream fetch failed", "family", runner.semantics.Name, "error", err)
			if !wait(ctx, runner.config.RetryDelay) {
				return nil
			}
			continue
		}
		if message == nil {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		if err := runner.ProcessMessage(ctx, message); err != nil {
			return err
		}
	}
}

func (runner *Runner[R]) ProcessMessage(ctx context.Context, message Message) error {
	metadata, err := message.Metadata()
	if err != nil || metadata == nil || metadata.Stream != runner.config.Stream || metadata.Consumer != runner.config.Durable ||
		metadata.Sequence.Stream == 0 || metadata.NumDelivered == 0 || message.Subject() != runner.config.Subject {
		return fmt.Errorf("%w: unexpected JetStream delivery metadata", runner.semantics.Invariant)
	}
	payload := bytes.Clone(message.Data())
	for {
		processCtx, cancel := context.WithTimeout(ctx, runner.config.ProcessTimeout)
		result, processErr := runner.apply(processCtx, payload, runner.now().UTC().Truncate(time.Microsecond))
		cancel()
		if processErr == nil {
			ackCtx, ackCancel := context.WithTimeout(ctx, runner.config.AckTimeout)
			ackErr := message.DoubleAck(ackCtx)
			ackCancel()
			if ackErr != nil {
				if ctx.Err() != nil {
					return nil
				}
				runner.logger.Warn("Core ACK confirmation failed; inbox makes redelivery safe",
					"family", runner.semantics.Name, "event_id", runner.semantics.EventID(result), "error", ackErr)
				return nil
			}
			runner.logger.Debug("Core projection committed and acknowledged", "family", runner.semantics.Name,
				"event_id", runner.semantics.EventID(result), "duplicate", runner.semantics.Duplicate(result))
			return nil
		}
		if runner.semantics.IsPermanent(processErr) {
			// Do not Term accounting events automatically. They remain available
			// for explicit operator reconciliation while this consumer fails closed.
			return fmt.Errorf("permanent Core %s projection failure: %w", runner.semantics.Name, processErr)
		}
		if ctx.Err() != nil {
			return nil
		}
		runner.logger.Warn("Core projection failed; retrying same delivery", "family", runner.semantics.Name,
			"stream_sequence", metadata.Sequence.Stream, "delivery_count", metadata.NumDelivered, "error", processErr)
		if progressErr := message.InProgress(); progressErr != nil {
			runner.logger.Warn("Core in-progress acknowledgement failed", "family", runner.semantics.Name,
				"stream_sequence", metadata.Sequence.Stream, "error", progressErr)
		}
		if !wait(ctx, runner.config.RetryDelay) {
			return nil
		}
	}
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
