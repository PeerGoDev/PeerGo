package seedingevidence

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
	"github.com/peergo/peergo/services/settlement/internal/jetstreamconsumer"
)

type SnapshotProcessor interface {
	ApplySnapshot(context.Context, SnapshotDelivery) (SnapshotApplyResult, error)
}

type SnapshotRunnerConfig struct {
	Stream         string
	Subject        string
	Durable        string
	ProcessTimeout time.Duration
	AckTimeout     time.Duration
	RetryDelay     time.Duration
}

type SnapshotRunner struct {
	source    jetstreamconsumer.Source
	processor SnapshotProcessor
	config    SnapshotRunnerConfig
	logger    *slog.Logger
}

func NewSnapshotRunner(source jetstreamconsumer.Source, processor SnapshotProcessor, config SnapshotRunnerConfig, logger *slog.Logger) (*SnapshotRunner, error) {
	if source == nil || processor == nil || !trackerswarmv1.ValidStreamName(config.Stream) ||
		!trackerswarmv1.ValidLiteralSubject(config.Subject) || !trackerswarmv1.ValidStreamName(config.Durable) ||
		config.ProcessTimeout < 100*time.Millisecond || config.ProcessTimeout > 10*time.Minute ||
		config.AckTimeout < 100*time.Millisecond || config.AckTimeout > time.Minute ||
		config.RetryDelay < 10*time.Millisecond || config.RetryDelay > time.Minute {
		return nil, ErrInput
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &SnapshotRunner{source: source, processor: processor, config: config, logger: logger}, nil
}

func (runner *SnapshotRunner) Run(ctx context.Context) error {
	for {
		message, err := runner.source.Next(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			runner.logger.Warn("Settlement seeding snapshot fetch failed", "error", err)
			if !waitFor(ctx, runner.config.RetryDelay) {
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

func (runner *SnapshotRunner) processMessage(ctx context.Context, message jetstreamconsumer.Message) error {
	metadata, err := message.Metadata()
	if err != nil || metadata == nil || metadata.Stream != runner.config.Stream ||
		metadata.Consumer != runner.config.Durable || metadata.Sequence.Stream == 0 || metadata.NumDelivered == 0 ||
		message.Subject() != runner.config.Subject {
		return fmt.Errorf("%w: unexpected seeding snapshot delivery metadata", ErrInvariant)
	}
	delivery := SnapshotDelivery{
		Stream: metadata.Stream, Subject: message.Subject(), Sequence: metadata.Sequence.Stream,
		DeliveryCount: metadata.NumDelivered, Payload: bytes.Clone(message.Data()),
	}
	for {
		processCtx, cancel := context.WithTimeout(ctx, runner.config.ProcessTimeout)
		result, processErr := runner.processor.ApplySnapshot(processCtx, delivery)
		cancel()
		if processErr == nil {
			ackCtx, ackCancel := context.WithTimeout(ctx, runner.config.AckTimeout)
			ackErr := message.DoubleAck(ackCtx)
			ackCancel()
			if ackErr != nil {
				if ctx.Err() != nil {
					return nil
				}
				runner.logger.Warn("Settlement seeding snapshot ACK failed; inbox makes replay safe",
					"event_id", result.EventID, "snapshot_id", result.SnapshotID, "error", ackErr)
				return nil
			}
			runner.logger.Debug("Settlement seeding snapshot committed and acknowledged",
				"event_id", result.EventID, "snapshot_id", result.SnapshotID,
				"duplicate", result.Duplicate, "complete", result.Complete)
			return nil
		}
		if IsPermanentSnapshotError(processErr) {
			return fmt.Errorf("permanent Settlement seeding snapshot failure: %w", processErr)
		}
		if ctx.Err() != nil {
			return nil
		}
		runner.logger.Warn("Settlement seeding snapshot projection failed; retrying same delivery",
			"stream_sequence", delivery.Sequence, "delivery_count", delivery.DeliveryCount, "error", processErr)
		if progressErr := message.InProgress(); progressErr != nil {
			runner.logger.Warn("Settlement seeding snapshot in-progress ACK failed",
				"stream_sequence", delivery.Sequence, "error", progressErr)
		}
		if !waitFor(ctx, runner.config.RetryDelay) {
			return nil
		}
	}
}

func waitFor(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
