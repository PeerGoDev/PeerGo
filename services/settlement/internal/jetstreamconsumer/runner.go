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
	"sort"
	"time"

	"github.com/nats-io/nats.go/jetstream"
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
	BatchSize      int
}

type Runner struct {
	source    Source
	processor ingest.BatchProcessor
	config    RunnerConfig
	logger    *slog.Logger
}

func NewRunner(source Source, processor ingest.BatchProcessor, config RunnerConfig, logger *slog.Logger) (*Runner, error) {
	if source == nil || processor == nil || !trackerannouncev1.ValidStreamName(config.Stream) ||
		!trackerannouncev1.ValidLiteralSubject(config.Subject) ||
		!trackerannouncev1.ValidStreamName(config.Durable) ||
		config.ProcessTimeout < 100*time.Millisecond || config.ProcessTimeout > 10*time.Minute ||
		config.AckTimeout < 100*time.Millisecond || config.AckTimeout > time.Minute ||
		config.RetryDelay < 10*time.Millisecond || config.RetryDelay > time.Minute ||
		config.BatchSize < 1 || config.BatchSize > 512 {
		return nil, ErrConfig
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Runner{source: source, processor: processor, config: config, logger: logger}, nil
}

func (runner *Runner) Run(ctx context.Context) error {
	for {
		messages, err := runner.source.NextBatch(ctx, runner.config.BatchSize)
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
		if len(messages) == 0 {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		if err := runner.processBatch(ctx, messages); err != nil {
			return err
		}
	}
}

func (runner *Runner) processMessage(ctx context.Context, message Message) error {
	return runner.processBatch(ctx, []Message{message})
}

func (runner *Runner) processBatch(ctx context.Context, messages []Message) error {
	if len(messages) == 0 || len(messages) > runner.config.BatchSize {
		return fmt.Errorf("%w: unexpected JetStream batch size", ingest.ErrSourceInvariant)
	}
	type orderedMessage struct {
		message  Message
		metadata *jetstream.MsgMetadata
	}
	ordered := make([]orderedMessage, len(messages))
	for index, message := range messages {
		metadata, err := message.Metadata()
		if err != nil || metadata == nil || metadata.Stream != runner.config.Stream ||
			metadata.Consumer != runner.config.Durable || metadata.Sequence.Stream == 0 || metadata.NumDelivered == 0 ||
			message.Subject() != runner.config.Subject {
			return fmt.Errorf("%w: unexpected JetStream delivery metadata", ingest.ErrSourceInvariant)
		}
		ordered[index] = orderedMessage{message: message, metadata: metadata}
	}

	// A durable pull consumer can redeliver an older unacknowledged message in
	// the same fetch as newer messages after a process restart or a lost ACK.
	// Consumer delivery order is therefore not a safe accounting order. The
	// immutable stream sequence is authoritative, so restore that order before
	// applying absolute counters in one PostgreSQL transaction.
	sort.SliceStable(ordered, func(left, right int) bool {
		return ordered[left].metadata.Sequence.Stream < ordered[right].metadata.Sequence.Stream
	})
	deliveries := make([]ingest.Delivery, len(ordered))
	var previousSequence uint64
	for index, item := range ordered {
		if previousSequence != 0 && item.metadata.Sequence.Stream <= previousSequence {
			return fmt.Errorf("%w: duplicate JetStream stream sequence in one batch", ingest.ErrSourceInvariant)
		}
		previousSequence = item.metadata.Sequence.Stream
		deliveries[index] = ingest.Delivery{
			Stream: item.metadata.Stream, Subject: item.message.Subject(), Sequence: item.metadata.Sequence.Stream,
			DeliveryCount: item.metadata.NumDelivered, Payload: bytes.Clone(item.message.Data()),
		}
	}

	// Retrying the same ordered in-flight batch preserves absolute-counter order.
	// PostgreSQL commits the whole batch or none of it, so a delayed retry can
	// never expose a later absolute counter without its predecessor.
	for {
		processCtx, cancel := context.WithTimeout(ctx, runner.config.ProcessTimeout)
		results, processErr := runner.processor.ProcessBatch(processCtx, deliveries)
		cancel()
		if processErr == nil {
			if len(results) != len(messages) {
				return fmt.Errorf("%w: Settlement batch result cardinality changed", ingest.ErrSourceInvariant)
			}
			for index, item := range ordered {
				ackCtx, ackCancel := context.WithTimeout(ctx, runner.config.AckTimeout)
				ackErr := item.message.DoubleAck(ackCtx)
				ackCancel()
				if ackErr != nil && ctx.Err() == nil {
					// PostgreSQL already committed. Any unconfirmed ACK is safely
					// redelivered and resolved by the inbox idempotency fence.
					runner.logger.Warn("Settlement JetStream ACK confirmation failed", "event_id", results[index].EventID, "error", ackErr)
				}
				runner.logger.Debug("Settlement announce event committed and acknowledged",
					"event_id", results[index].EventID, "outcome", results[index].Outcome, "duplicate", results[index].Duplicate)
			}
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
		runner.logger.Warn("Settlement ingest failed; retrying the same ordered batch",
			"first_stream_sequence", deliveries[0].Sequence,
			"last_stream_sequence", deliveries[len(deliveries)-1].Sequence,
			"batch_size", len(deliveries), "error", processErr)
		for index, item := range ordered {
			if progressErr := item.message.InProgress(); progressErr != nil {
				runner.logger.Warn("Settlement JetStream in-progress acknowledgement failed",
					"stream_sequence", deliveries[index].Sequence, "error", progressErr)
			}
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
