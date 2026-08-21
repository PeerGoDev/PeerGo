package trafficconsumer

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/contracts/go/settlementtrafficv1"
)

var ErrProvisionDrift = errors.New("existing Core JetStream consumer has unsafe configuration drift")

type ConsumerManager interface {
	Get(context.Context, string, string) (jetstream.ConsumerConfig, error)
	Create(context.Context, string, jetstream.ConsumerConfig) error
	Update(context.Context, string, jetstream.ConsumerConfig) error
}

type NATSConsumerManager struct{ js jetstream.JetStream }

func NewNATSConsumerManager(js jetstream.JetStream) (*NATSConsumerManager, error) {
	if js == nil {
		return nil, ErrConfig
	}
	return &NATSConsumerManager{js: js}, nil
}

func (manager *NATSConsumerManager) Get(ctx context.Context, stream, durable string) (jetstream.ConsumerConfig, error) {
	consumer, err := manager.js.Consumer(ctx, stream, durable)
	if err != nil {
		return jetstream.ConsumerConfig{}, err
	}
	info, err := consumer.Info(ctx)
	if err != nil {
		return jetstream.ConsumerConfig{}, err
	}
	return info.Config, nil
}

func (manager *NATSConsumerManager) Create(ctx context.Context, stream string, config jetstream.ConsumerConfig) error {
	_, err := manager.js.CreateConsumer(ctx, stream, config)
	return err
}

func (manager *NATSConsumerManager) Update(ctx context.Context, stream string, config jetstream.ConsumerConfig) error {
	_, err := manager.js.UpdateConsumer(ctx, stream, config)
	return err
}

func EnsureConsumer(ctx context.Context, manager ConsumerManager, stream string, desired jetstream.ConsumerConfig) (bool, error) {
	if manager == nil || !settlementtrafficv1.ValidStreamName(stream) || validateConsumerConfig(desired) != nil {
		return false, ErrConfig
	}
	current, err := manager.Get(ctx, stream, desired.Durable)
	if err == nil {
		if equivalentConsumerConfig(current, desired) {
			return false, nil
		}
		// MaxAckPending is the only mutable drift this provisioner repairs. It
		// changes bounded delivery capacity without replacing the durable or
		// altering its acknowledgement/delivery position. Every other mismatch
		// remains a fail-closed operator error.
		if current.MaxAckPending < 1 || current.MaxAckPending > 32 ||
			!equivalentConsumerConfigExceptMaxAckPending(current, desired) {
			return false, ErrProvisionDrift
		}
		if err := manager.Update(ctx, stream, desired); err != nil {
			return false, fmt.Errorf("update Core JetStream consumer concurrency: %w", err)
		}
		updated, err := manager.Get(ctx, stream, desired.Durable)
		if err != nil {
			return false, fmt.Errorf("verify updated Core JetStream consumer: %w", err)
		}
		if !equivalentConsumerConfig(updated, desired) {
			return false, ErrProvisionDrift
		}
		return false, nil
	}
	if !errors.Is(err, jetstream.ErrConsumerNotFound) {
		return false, fmt.Errorf("inspect Core JetStream consumer: %w", err)
	}
	if err := manager.Create(ctx, stream, desired); err != nil {
		return false, fmt.Errorf("create Core JetStream consumer: %w", err)
	}
	return true, nil
}

func validateConsumerConfig(config jetstream.ConsumerConfig) error {
	if !settlementtrafficv1.ValidStreamName(config.Name) || config.Name != config.Durable ||
		config.DeliverPolicy != jetstream.DeliverAllPolicy || config.AckPolicy != jetstream.AckExplicitPolicy ||
		config.AckWait <= 0 || config.MaxDeliver != -1 || !settlementtrafficv1.ValidLiteralSubject(config.FilterSubject) ||
		len(config.FilterSubjects) != 0 || config.ReplayPolicy != jetstream.ReplayInstantPolicy || config.DeliverSubject != "" ||
		config.DeliverGroup != "" || config.MaxAckPending < 1 || config.MaxAckPending > 32 ||
		config.MaxRequestBatch != 1 || config.MaxRequestExpires <= 0 || config.MaxWaiting < 1 {
		return ErrConfig
	}
	for key := range config.Metadata {
		if strings.HasPrefix(key, "_nats.") {
			return ErrConfig
		}
	}
	return nil
}

func equivalentConsumerConfigExceptMaxAckPending(current, desired jetstream.ConsumerConfig) bool {
	current.MaxAckPending = desired.MaxAckPending
	return equivalentConsumerConfig(current, desired)
}

func equivalentConsumerConfig(current, desired jetstream.ConsumerConfig) bool {
	return current.Name == desired.Name && current.Durable == desired.Durable && current.Description == desired.Description &&
		current.DeliverPolicy == desired.DeliverPolicy && current.AckPolicy == desired.AckPolicy && current.AckWait == desired.AckWait &&
		current.MaxDeliver == desired.MaxDeliver && current.FilterSubject == desired.FilterSubject && reflect.DeepEqual(current.FilterSubjects, desired.FilterSubjects) &&
		current.ReplayPolicy == desired.ReplayPolicy && current.DeliverSubject == desired.DeliverSubject && current.DeliverGroup == desired.DeliverGroup &&
		current.MaxAckPending == desired.MaxAckPending && current.MaxRequestBatch == desired.MaxRequestBatch && current.MaxRequestExpires == desired.MaxRequestExpires &&
		current.MaxWaiting == desired.MaxWaiting && equivalentMetadata(current.Metadata, desired.Metadata)
}

func equivalentMetadata(current, desired map[string]string) bool {
	applicationMetadata := make(map[string]string, len(current))
	for key, value := range current {
		if !strings.HasPrefix(key, "_nats.") {
			applicationMetadata[key] = value
		}
	}
	return reflect.DeepEqual(applicationMetadata, desired)
}
