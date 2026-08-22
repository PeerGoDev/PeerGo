// Package jetstreamprovision owns the operator-side creation check for the
// Settlement durable consumer. The Settlement runtime never imports it and
// therefore does not need consumer management permission.
package jetstreamprovision

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
)

var (
	ErrConfig = errors.New("Settlement durable consumer configuration is invalid")
	ErrDrift  = errors.New("existing Settlement durable consumer configuration differs")
)

type Manager interface {
	Get(context.Context, string, string) (jetstream.ConsumerConfig, error)
	Create(context.Context, string, jetstream.ConsumerConfig) error
	Update(context.Context, string, jetstream.ConsumerConfig) error
}

type NATSManager struct {
	jetStream jetstream.JetStream
}

func NewNATSManager(js jetstream.JetStream) (*NATSManager, error) {
	if js == nil {
		return nil, ErrConfig
	}
	return &NATSManager{jetStream: js}, nil
}

func (manager *NATSManager) Get(ctx context.Context, stream, consumer string) (jetstream.ConsumerConfig, error) {
	current, err := manager.jetStream.Consumer(ctx, stream, consumer)
	if err != nil {
		return jetstream.ConsumerConfig{}, err
	}
	info, err := current.Info(ctx)
	if err != nil {
		return jetstream.ConsumerConfig{}, err
	}
	return info.Config, nil
}

func (manager *NATSManager) Create(ctx context.Context, stream string, config jetstream.ConsumerConfig) error {
	_, err := manager.jetStream.CreateConsumer(ctx, stream, config)
	return err
}

func (manager *NATSManager) Update(ctx context.Context, stream string, config jetstream.ConsumerConfig) error {
	_, err := manager.jetStream.UpdateConsumer(ctx, stream, config)
	return err
}

type Change string

const (
	ChangeNone    Change = "none"
	ChangeCreated Change = "created"
	ChangeUpdated Change = "updated"
)

// Ensure creates a missing consumer and permits only an in-place ordered-batch
// limit change. ACK policy, delivery start or filter drift can silently lose
// or duplicate accounting evidence, so every other change remains rejected.
func Ensure(ctx context.Context, manager Manager, stream string, desired jetstream.ConsumerConfig) (Change, error) {
	if manager == nil || !trackerannouncev1.ValidStreamName(stream) || validate(desired) != nil {
		return ChangeNone, ErrConfig
	}
	current, err := manager.Get(ctx, stream, desired.Name)
	if err == nil {
		if !equivalent(current, desired) {
			if !orderedBatchOnlyDrift(current, desired) {
				return ChangeNone, ErrDrift
			}
			if err := manager.Update(ctx, stream, desired); err != nil {
				return ChangeNone, fmt.Errorf("update Settlement durable consumer ordered batch limits: %w", err)
			}
			return ChangeUpdated, nil
		}
		return ChangeNone, nil
	}
	if !errors.Is(err, jetstream.ErrConsumerNotFound) && !errors.Is(err, jetstream.ErrConsumerDoesNotExist) {
		return ChangeNone, fmt.Errorf("inspect Settlement durable consumer: %w", err)
	}
	if err := manager.Create(ctx, stream, desired); err != nil {
		return ChangeNone, fmt.Errorf("create Settlement durable consumer: %w", err)
	}
	return ChangeCreated, nil
}

func validate(config jetstream.ConsumerConfig) error {
	if config.Name == "" || config.Name != config.Durable || !trackerannouncev1.ValidStreamName(config.Name) ||
		config.DeliverPolicy != jetstream.DeliverAllPolicy || config.OptStartSeq != 0 || config.OptStartTime != nil ||
		config.AckPolicy != jetstream.AckExplicitPolicy || config.AckWait < time.Second || config.AckWait > 10*time.Minute ||
		config.MaxDeliver != -1 || len(config.BackOff) != 0 ||
		!trackerannouncev1.ValidLiteralSubject(config.FilterSubject) || len(config.FilterSubjects) != 0 ||
		config.ReplayPolicy != jetstream.ReplayInstantPolicy || config.RateLimit != 0 || config.HeadersOnly ||
		config.MaxWaiting < 1 || config.MaxAckPending < 1 || config.MaxAckPending > 512 ||
		config.MaxRequestBatch != config.MaxAckPending ||
		config.MaxRequestExpires < 100*time.Millisecond || config.MaxRequestExpires > time.Minute ||
		config.MaxRequestMaxBytes != 0 || config.InactiveThreshold != 0 || config.Replicas != 0 || config.MemoryStorage ||
		config.DeliverSubject != "" || config.DeliverGroup != "" || config.FlowControl || config.IdleHeartbeat != 0 ||
		config.PauseUntil != nil || config.PriorityPolicy != jetstream.PriorityPolicyNone || config.PinnedTTL != 0 ||
		len(config.PriorityGroups) != 0 {
		return ErrConfig
	}
	for key := range config.Metadata {
		if strings.HasPrefix(key, "_nats.") {
			return ErrConfig
		}
	}
	return nil
}

func equivalent(current, desired jetstream.ConsumerConfig) bool {
	current.Metadata = applicationMetadata(current.Metadata)
	return reflect.DeepEqual(current, desired)
}

// orderedBatchOnlyDrift permits the reviewed v1 single-message -> ordered
// transactional-batch transition without allowing a provisioner to alter the
// delivery start, filter, ACK policy, retry semantics, or application metadata.
func orderedBatchOnlyDrift(current, desired jetstream.ConsumerConfig) bool {
	current.Metadata = applicationMetadata(current.Metadata)
	current.MaxAckPending = desired.MaxAckPending
	current.MaxRequestBatch = desired.MaxRequestBatch
	return reflect.DeepEqual(current, desired)
}

// NATS may attach reserved request metadata while normalizing a config. Only
// those server-owned keys are ignored; every application key must match.
func applicationMetadata(metadata map[string]string) map[string]string {
	result := make(map[string]string, len(metadata))
	for key, value := range metadata {
		if !strings.HasPrefix(key, "_nats.") {
			result[key] = value
		}
	}
	return result
}
