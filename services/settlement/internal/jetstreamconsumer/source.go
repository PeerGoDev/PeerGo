package jetstreamconsumer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
)

type Message interface {
	Metadata() (*jetstream.MsgMetadata, error)
	Data() []byte
	Subject() string
	DoubleAck(context.Context) error
	InProgress() error
}

type Source interface {
	Next(context.Context) (Message, error)
	NextBatch(context.Context, int) ([]Message, error)
}

type BindingConfig struct {
	Stream                string
	Subject               string
	Durable               string
	FetchWait             time.Duration
	MaximumProcessingTime time.Duration
	MaximumAckTime        time.Duration
	BatchSize             int
}

type fetchConsumer interface {
	Fetch(int, ...jetstream.FetchOpt) (jetstream.MessageBatch, error)
}

type natsSource struct {
	consumer             fetchConsumer
	fetchWait            time.Duration
	initialRecoveryDelay time.Duration
	recoveryWaited       bool
}

// OpenSource binds to an existing durable consumer. It deliberately uses the
// read-only Consumer lookup API and refuses unsafe runtime drift; creation and
// updates belong exclusively to cmd/consumer-init.
func OpenSource(ctx context.Context, js jetstream.JetStream, config BindingConfig) (Source, error) {
	if js == nil || validateBinding(config) != nil {
		return nil, ErrConfig
	}
	consumer, err := js.Consumer(ctx, config.Stream, config.Durable)
	if err != nil {
		return nil, fmt.Errorf("open existing Settlement consumer: %w", err)
	}
	info, err := consumer.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("inspect existing Settlement consumer: %w", err)
	}
	if validateConsumerBinding(info, config) != nil {
		return nil, ErrConsumerDrift
	}
	return newSource(consumer, config.FetchWait, pendingRedeliveryDelay(info))
}

// A process can stop after PostgreSQL commits an ordered batch but before all
// confirmed ACKs reach JetStream. Until AckWait expires, a replacement process
// may be offered newer messages while those older deliveries are still owned by
// the previous connection. Waiting one complete ACK window when pending
// deliveries exist restores the durable's oldest-first redelivery boundary
// without deleting the consumer, skipping evidence, or weakening the database
// sequence invariant.
func pendingRedeliveryDelay(info *jetstream.ConsumerInfo) time.Duration {
	if info == nil || info.NumAckPending == 0 || info.Config.AckWait <= 0 {
		return 0
	}
	return info.Config.AckWait
}

func validateConsumerBinding(info *jetstream.ConsumerInfo, config BindingConfig) error {
	if info == nil || info.Stream != config.Stream || info.Name != config.Durable ||
		info.Config.Name != config.Durable || info.Config.Durable != config.Durable ||
		info.Config.DeliverPolicy != jetstream.DeliverAllPolicy ||
		info.Config.AckPolicy != jetstream.AckExplicitPolicy ||
		info.Config.AckWait <= config.MaximumProcessingTime+config.MaximumAckTime ||
		info.Config.MaxDeliver != -1 || info.Config.FilterSubject != config.Subject ||
		len(info.Config.FilterSubjects) != 0 || info.Config.ReplayPolicy != jetstream.ReplayInstantPolicy ||
		info.Config.DeliverSubject != "" || info.Config.DeliverGroup != "" ||
		info.Config.MaxAckPending != config.BatchSize || info.Config.MaxRequestBatch != config.BatchSize ||
		(info.Config.MaxRequestExpires > 0 && config.FetchWait > info.Config.MaxRequestExpires) {
		return ErrConsumerDrift
	}
	return nil
}

func newSource(consumer fetchConsumer, fetchWait, initialRecoveryDelay time.Duration) (Source, error) {
	if consumer == nil || fetchWait < 100*time.Millisecond || fetchWait > time.Minute ||
		initialRecoveryDelay < 0 || initialRecoveryDelay > 10*time.Minute {
		return nil, ErrConfig
	}
	return &natsSource{
		consumer: consumer, fetchWait: fetchWait, initialRecoveryDelay: initialRecoveryDelay,
	}, nil
}

func (source *natsSource) NextBatch(ctx context.Context, limit int) ([]Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 512 {
		return nil, ErrConfig
	}
	if !source.recoveryWaited {
		source.recoveryWaited = true
		if source.initialRecoveryDelay > 0 {
			timer := time.NewTimer(source.initialRecoveryDelay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
	}
	requestCtx, cancel := context.WithTimeout(ctx, source.fetchWait)
	defer cancel()
	batch, err := source.consumer.Fetch(limit, jetstream.FetchContext(requestCtx))
	if err == nil {
		messages := make([]Message, 0, limit)
		for message := range batch.Messages() {
			messages = append(messages, message)
		}
		if len(messages) > 0 {
			// A terminal fetch error after partial delivery does not invalidate
			// those messages. They remain ordered and are committed before the
			// next pull; an unreceived tail stays pending in JetStream.
			return messages, nil
		}
		err = batch.Error()
		if err == nil {
			return nil, nil
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if errors.Is(err, nats.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return nil, nil
	}
	return nil, fmt.Errorf("fetch Settlement announce batch: %w", err)
}

func (source *natsSource) Next(ctx context.Context) (Message, error) {
	messages, err := source.NextBatch(ctx, 1)
	if err != nil || len(messages) == 0 {
		return nil, err
	}
	return messages[0], nil
}

func validateBinding(config BindingConfig) error {
	if !trackerannouncev1.ValidStreamName(config.Stream) ||
		!trackerannouncev1.ValidLiteralSubject(config.Subject) ||
		!trackerannouncev1.ValidStreamName(config.Durable) ||
		config.FetchWait < 100*time.Millisecond || config.FetchWait > time.Minute ||
		config.MaximumProcessingTime < 100*time.Millisecond || config.MaximumProcessingTime > 10*time.Minute ||
		config.MaximumAckTime < 100*time.Millisecond || config.MaximumAckTime > time.Minute ||
		config.BatchSize < 1 || config.BatchSize > 512 {
		return ErrConfig
	}
	return nil
}
