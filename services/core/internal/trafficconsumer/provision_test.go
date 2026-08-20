package trafficconsumer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/contracts/go/settlementtrafficv1"
)

func TestEnsureConsumerCreatesAndRejectsDrift(t *testing.T) {
	t.Parallel()
	desired := testConsumerConfig()
	manager := &fakeConsumerManager{getErr: jetstream.ErrConsumerNotFound}
	created, err := EnsureConsumer(context.Background(), manager, settlementtrafficv1.DefaultStream, desired)
	if err != nil || !created || manager.created.Name != desired.Name {
		t.Fatalf("EnsureConsumer(create) = %v, %v", created, err)
	}
	manager.getErr = nil
	manager.current = desired
	created, err = EnsureConsumer(context.Background(), manager, settlementtrafficv1.DefaultStream, desired)
	if err != nil || created {
		t.Fatalf("EnsureConsumer(existing) = %v, %v", created, err)
	}
	manager.current.MaxAckPending = 2
	if _, err := EnsureConsumer(context.Background(), manager, settlementtrafficv1.DefaultStream, desired); !errors.Is(err, ErrProvisionDrift) {
		t.Fatalf("EnsureConsumer(drift) error = %v", err)
	}
}

type fakeConsumerManager struct {
	current jetstream.ConsumerConfig
	created jetstream.ConsumerConfig
	getErr  error
}

func (manager *fakeConsumerManager) Get(context.Context, string, string) (jetstream.ConsumerConfig, error) {
	return manager.current, manager.getErr
}
func (manager *fakeConsumerManager) Create(_ context.Context, _ string, config jetstream.ConsumerConfig) error {
	manager.created = config
	return nil
}

func testConsumerConfig() jetstream.ConsumerConfig {
	return jetstream.ConsumerConfig{
		Name: "PEERGO_CORE_TRAFFIC_V1", Durable: "PEERGO_CORE_TRAFFIC_V1", Description: "PeerGo Core traffic projector v1",
		DeliverPolicy: jetstream.DeliverAllPolicy, AckPolicy: jetstream.AckExplicitPolicy, AckWait: 30 * time.Second,
		MaxDeliver: -1, FilterSubject: settlementtrafficv1.DefaultSubject, ReplayPolicy: jetstream.ReplayInstantPolicy,
		MaxWaiting: 16, MaxAckPending: 1, MaxRequestBatch: 1, MaxRequestExpires: 5 * time.Second,
		Metadata: map[string]string{"peergo.owner": "core", "peergo.schema": settlementtrafficv1.SchemaVersion},
	}
}
