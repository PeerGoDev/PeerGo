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
}

type BindingConfig struct {
	Stream                string
	Subject               string
	Durable               string
	FetchWait             time.Duration
	MaximumProcessingTime time.Duration
	MaximumAckTime        time.Duration
}

type nextConsumer interface {
	Next(...jetstream.FetchOpt) (jetstream.Msg, error)
}

type natsSource struct {
	consumer  nextConsumer
	fetchWait time.Duration
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
	return newSource(consumer, config.FetchWait)
}

func validateConsumerBinding(info *jetstream.ConsumerInfo, config BindingConfig) error {
	if info == nil || info.Stream != config.Stream || info.Name != config.Durable ||
		info.Config.Name != config.Durable || info.Config.Durable != config.Durable ||
		info.Config.DeliverPolicy != jetstream.DeliverAllPolicy ||
		info.Config.AckPolicy != jetstream.AckExplicitPolicy ||
		info.Config.AckWait <= config.MaximumProcessingTime+config.MaximumAckTime ||
		info.Config.MaxDeliver != -1 || info.Config.FilterSubject != config.Subject ||
		len(info.Config.FilterSubjects) != 0 || info.Config.ReplayPolicy != jetstream.ReplayInstantPolicy ||
		info.Config.DeliverSubject != "" || info.Config.DeliverGroup != "" || info.Config.MaxAckPending != 1 ||
		info.Config.MaxRequestBatch != 1 ||
		(info.Config.MaxRequestExpires > 0 && config.FetchWait > info.Config.MaxRequestExpires) {
		return ErrConsumerDrift
	}
	return nil
}

func newSource(consumer nextConsumer, fetchWait time.Duration) (Source, error) {
	if consumer == nil || fetchWait < 100*time.Millisecond || fetchWait > time.Minute {
		return nil, ErrConfig
	}
	return &natsSource{consumer: consumer, fetchWait: fetchWait}, nil
}

func (source *natsSource) Next(ctx context.Context) (Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, source.fetchWait)
	defer cancel()
	message, err := source.consumer.Next(jetstream.FetchContext(requestCtx))
	if err == nil {
		return message, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if errors.Is(err, nats.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return nil, nil
	}
	return nil, fmt.Errorf("fetch Settlement announce event: %w", err)
}

func validateBinding(config BindingConfig) error {
	if !trackerannouncev1.ValidStreamName(config.Stream) ||
		!trackerannouncev1.ValidLiteralSubject(config.Subject) ||
		!trackerannouncev1.ValidStreamName(config.Durable) ||
		config.FetchWait < 100*time.Millisecond || config.FetchWait > time.Minute ||
		config.MaximumProcessingTime < 100*time.Millisecond || config.MaximumProcessingTime > 10*time.Minute ||
		config.MaximumAckTime < 100*time.Millisecond || config.MaximumAckTime > time.Minute {
		return ErrConfig
	}
	return nil
}
