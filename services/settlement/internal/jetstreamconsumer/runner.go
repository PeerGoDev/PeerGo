// Package jetstreamconsumer owns the ACK boundary between JetStream and the
// Tracker Ledger. A message is acknowledged only after PostgreSQL has committed
// its idempotency fence and raw interval in one transaction.
package jetstreamconsumer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/services/settlement/internal/ingest"
)

var (
	ErrConfig        = errors.New("Settlement JetStream consumer configuration is invalid")
	ErrConsumerDrift = errors.New("existing Settlement JetStream consumer has unsafe configuration drift")
)

type RunnerConfig struct {
	Stream         string
	Subject        string
	Durable        string
	ProcessTimeout time.Duration
	AckTimeout     time.Duration
	RetryDelay     time.Duration
}

type Runner struct {
	source    Source
	processor ingest.Processor
	config    RunnerConfig
	logger    *slog.Logger
}

func NewRunner(source Source, processor ingest.Processor, config RunnerConfig, logger *slog.Logger) (*Runner, error) {
	if source == nil || processor == nil || !trackerannouncev1.ValidStreamName(config.Stream) ||
		!trackerannouncev1.ValidLiteralSubject(config.Subject) ||
		!trackerannouncev1.ValidStreamName(config.Durable) ||
		config.ProcessTimeout < 100*time.Millisecond || config.ProcessTimeout > 10*time.Minute ||
		config.AckTimeout < 100*time.Millisecond || config.AckTimeout > time.Minute ||
		config.RetryDelay < 10*time.Millisecond || config.RetryDelay > time.Minute {
		return nil, ErrConfig
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Runner{source: source, processor: processor, config: config, logger: logger}, nil
}

func (runner *Runner) Run(ctx context.Context) error {
	for {
		message, err := runner.source.Next(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			runner.logger.Warn("Settlement JetStream fetch failed", "error", err)
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
		if err := runner.processMessage(ctx, message); err != nil {
			return err
		}
	}
}

func (runner *Runner) processMessage(ctx context.Context, message Message) error {
	metadata, err := message.Metadata()
	if err != nil || metadata == nil || metadata.Stream != runner.config.Stream ||
		metadata.Consumer != runner.config.Durable || metadata.Sequence.Stream == 0 || metadata.NumDelivered == 0 ||
		message.Subject() != runner.config.Subject {
		return fmt.Errorf("%w: unexpected JetStream delivery metadata", ingest.ErrSourceInvariant)
	}
	delivery := ingest.Delivery{
		Stream: metadata.Stream, Subject: message.Subject(), Sequence: metadata.Sequence.Stream,
		DeliveryCount: metadata.NumDelivered, Payload: bytes.Clone(message.Data()),
	}

	// Retrying the same in-flight message preserves absolute-counter order.
	// Delayed NAK followed by another pull could let a later session sample
	// overtake this one and permanently turn the earlier sample out-of-order.
	for {
		processCtx, cancel := context.WithTimeout(ctx, runner.config.ProcessTimeout)
		result, processErr := runner.processor.Process(processCtx, delivery)
		cancel()
		if processErr == nil {
			ackCtx, ackCancel := context.WithTimeout(ctx, runner.config.AckTimeout)
			ackErr := message.DoubleAck(ackCtx)
			ackCancel()
			if ackErr != nil {
				if ctx.Err() != nil {
					return nil
				}
				// PostgreSQL already committed. If the server did not process this
				// ACK it will redeliver, and the inbox fence returns a duplicate.
				runner.logger.Warn("Settlement JetStream ACK confirmation failed", "event_id", result.EventID, "error", ackErr)
				return nil
			}
			runner.logger.Debug("Settlement announce event committed and acknowledged",
				"event_id", result.EventID, "outcome", result.Outcome, "duplicate", result.Duplicate)
			return nil
		}
		if ingest.IsPermanent(processErr) {
			// Never Term a malformed accounting event automatically. Leaving it
			// unacknowledged keeps evidence available while the process fails
			// closed for operator investigation.
			return fmt.Errorf("permanent Settlement ingest failure: %w", processErr)
		}
		if ctx.Err() != nil {
			return nil
		}
		runner.logger.Warn("Settlement ingest failed; retrying the same delivery",
			"stream_sequence", delivery.Sequence, "delivery_count", delivery.DeliveryCount, "error", processErr)
		if progressErr := message.InProgress(); progressErr != nil {
			runner.logger.Warn("Settlement JetStream in-progress acknowledgement failed",
				"stream_sequence", delivery.Sequence, "error", progressErr)
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
