// Package swarmconsumer owns Core's commit-before-ACK boundary for Tracker
// swarm snapshots and lifetime completion facts. NATS transport and durable
// consumer management are reused from trafficconsumer; this package contains
// only the swarm-specific projection/error policy.
package swarmconsumer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/peergo/peergo/contracts/go/jetstreamv1"
	"github.com/peergo/peergo/services/core/internal/modules/swarmprojection"
	"github.com/peergo/peergo/services/core/internal/trafficconsumer"
)

var ErrConfig = errors.New("Core swarm JetStream consumer configuration is invalid")

type applyFunc func(context.Context, []byte, time.Time) (swarmprojection.ApplyResult, error)

type Runner struct {
	source     trafficconsumer.Source
	binding    trafficconsumer.BindingConfig
	retryDelay time.Duration
	kind       string
	apply      applyFunc
	now        func() time.Time
	logger     *slog.Logger
}

func NewSnapshotRunner(source trafficconsumer.Source, projector swarmprojection.SnapshotProjector, binding trafficconsumer.BindingConfig, retryDelay time.Duration, now func() time.Time, logger *slog.Logger) (*Runner, error) {
	if projector == nil {
		return nil, ErrConfig
	}
	return newRunner(source, binding, retryDelay, "snapshot", projector.ApplySnapshot, now, logger)
}

func NewCompletionRunner(source trafficconsumer.Source, projector swarmprojection.CompletionProjector, binding trafficconsumer.BindingConfig, retryDelay time.Duration, now func() time.Time, logger *slog.Logger) (*Runner, error) {
	if projector == nil {
		return nil, ErrConfig
	}
	return newRunner(source, binding, retryDelay, "completion", projector.ApplyCompletion, now, logger)
}

func newRunner(source trafficconsumer.Source, binding trafficconsumer.BindingConfig, retryDelay time.Duration, kind string, apply applyFunc, now func() time.Time, logger *slog.Logger) (*Runner, error) {
	if source == nil || apply == nil || (kind != "snapshot" && kind != "completion") ||
		!jetstreamv1.ValidStreamName(binding.Stream) || !jetstreamv1.ValidLiteralSubject(binding.Subject) ||
		!jetstreamv1.ValidStreamName(binding.Durable) || binding.FetchWait < 100*time.Millisecond || binding.FetchWait > time.Minute ||
		binding.MaximumProcessingTime < 100*time.Millisecond || binding.MaximumProcessingTime > 10*time.Minute ||
		binding.MaximumAckTime < 100*time.Millisecond || binding.MaximumAckTime > time.Minute ||
		retryDelay < 10*time.Millisecond || retryDelay > time.Minute {
		return nil, ErrConfig
	}
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Runner{
		source: source, binding: binding, retryDelay: retryDelay,
		kind: kind, apply: apply, now: now, logger: logger,
	}, nil
}

func (runner *Runner) Run(ctx context.Context) error {
	for {
		message, err := runner.source.Next(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			runner.logger.Warn("Core swarm JetStream fetch failed", "kind", runner.kind, "error", err)
			if !wait(ctx, runner.retryDelay) {
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

func (runner *Runner) processMessage(ctx context.Context, message trafficconsumer.Message) error {
	metadata, err := message.Metadata()
	if err != nil || metadata == nil || metadata.Stream != runner.binding.Stream || metadata.Consumer != runner.binding.Durable ||
		metadata.Sequence.Stream == 0 || metadata.NumDelivered == 0 || message.Subject() != runner.binding.Subject {
		return fmt.Errorf("%w: unexpected JetStream delivery metadata", swarmprojection.ErrInvariant)
	}
	payload := bytes.Clone(message.Data())
	for {
		processCtx, cancel := context.WithTimeout(ctx, runner.binding.MaximumProcessingTime)
		result, processErr := runner.apply(processCtx, payload, runner.now().UTC().Round(0))
		cancel()
		if processErr == nil {
			ackCtx, ackCancel := context.WithTimeout(ctx, runner.binding.MaximumAckTime)
			ackErr := message.DoubleAck(ackCtx)
			ackCancel()
			if ackErr != nil {
				if ctx.Err() != nil {
					return nil
				}
				// The database inbox is the idempotency fence. Leaving the
				// message unacknowledged is safe and deliberately invites replay.
				runner.logger.Warn("Core swarm ACK confirmation failed; inbox makes redelivery safe", "kind", runner.kind, "event_id", result.EventID, "error", ackErr)
				return nil
			}
			runner.logger.Debug("Core swarm fact projected and acknowledged",
				"kind", runner.kind, "event_id", result.EventID, "snapshot_id", result.SnapshotID,
				"duplicate", result.Duplicate, "obsolete", result.Obsolete, "applied", result.Applied, "noop", result.Noop)
			return nil
		}
		if errors.Is(processErr, swarmprojection.ErrInput) || errors.Is(processErr, swarmprojection.ErrConflict) || errors.Is(processErr, swarmprojection.ErrInvariant) {
			// Corrupt or contradictory facts are retained for operator
			// reconciliation. Automatically terminating them would silently
			// skip a full snapshot or a lifetime accounting fact.
			return fmt.Errorf("permanent Core swarm %s projection failure: %w", runner.kind, processErr)
		}
		if ctx.Err() != nil {
			return nil
		}
		runner.logger.Warn("Core swarm projection failed; retrying same delivery",
			"kind", runner.kind, "stream_sequence", metadata.Sequence.Stream,
			"delivery_count", metadata.NumDelivered, "error", processErr)
		if progressErr := message.InProgress(); progressErr != nil {
			runner.logger.Warn("Core swarm in-progress acknowledgement failed", "kind", runner.kind, "stream_sequence", metadata.Sequence.Stream, "error", progressErr)
		}
		if !wait(ctx, runner.retryDelay) {
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
