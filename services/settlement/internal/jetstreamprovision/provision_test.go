package jetstreamprovision

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

func TestEnsureCreatesMissingConsumerAndAcceptsExactReplay(t *testing.T) {
	desired := provisionTestConfig()
	manager := &provisionTestManager{getError: jetstream.ErrConsumerNotFound}
	created, err := Ensure(context.Background(), manager, "PEERGO_TRACKER_ANNOUNCE_V1", desired)
	if err != nil || !created || manager.created.Name != desired.Name {
		t.Fatalf("Ensure(create) = %v, %v, created config=%+v", created, err, manager.created)
	}
	manager.getError = nil
	manager.current = desired
	created, err = Ensure(context.Background(), manager, "PEERGO_TRACKER_ANNOUNCE_V1", desired)
	if err != nil || created {
		t.Fatalf("Ensure(replay) = %v, %v", created, err)
	}
}

func TestEnsureRejectsAccountingSafetyDrift(t *testing.T) {
	desired := provisionTestConfig()
	for name, mutate := range map[string]func(*jetstream.ConsumerConfig){
		"delivery start": func(config *jetstream.ConsumerConfig) { config.DeliverPolicy = jetstream.DeliverNewPolicy },
		"ack policy":     func(config *jetstream.ConsumerConfig) { config.AckPolicy = jetstream.AckNonePolicy },
		"filter":         func(config *jetstream.ConsumerConfig) { config.FilterSubject = "peergo.other" },
		"parallelism":    func(config *jetstream.ConsumerConfig) { config.MaxAckPending = 2 },
		"finite retry":   func(config *jetstream.ConsumerConfig) { config.MaxDeliver = 5 },
	} {
		t.Run(name, func(t *testing.T) {
			current := desired
			mutate(&current)
			created, err := Ensure(context.Background(), &provisionTestManager{current: current}, "PEERGO_TRACKER_ANNOUNCE_V1", desired)
			if created || !errors.Is(err, ErrDrift) {
				t.Fatalf("Ensure(drift) = %v, %v", created, err)
			}
		})
	}
}

func TestEnsureAcceptsServerMetadataButRejectsApplicationMetadataDrift(t *testing.T) {
	desired := provisionTestConfig()
	current := desired
	current.Metadata = map[string]string{
		"peergo.owner": "settlement", "peergo.schema": "tracker.announce.v1", "_nats.req.level": "0",
	}
	created, err := Ensure(context.Background(), &provisionTestManager{current: current}, "PEERGO_TRACKER_ANNOUNCE_V1", desired)
	if err != nil || created {
		t.Fatalf("Ensure(server metadata) = %v, %v", created, err)
	}
	current.Metadata["unexpected.owner"] = "other"
	created, err = Ensure(context.Background(), &provisionTestManager{current: current}, "PEERGO_TRACKER_ANNOUNCE_V1", desired)
	if created || !errors.Is(err, ErrDrift) {
		t.Fatalf("Ensure(application metadata drift) = %v, %v", created, err)
	}
}

func TestEnsureRejectsUnsafeDesiredConsumer(t *testing.T) {
	desired := provisionTestConfig()
	desired.MaxAckPending = 2
	created, err := Ensure(context.Background(), &provisionTestManager{getError: jetstream.ErrConsumerNotFound}, "PEERGO_TRACKER_ANNOUNCE_V1", desired)
	if created || !errors.Is(err, ErrConfig) {
		t.Fatalf("Ensure(unsafe desired) = %v, %v", created, err)
	}
}

type provisionTestManager struct {
	current  jetstream.ConsumerConfig
	created  jetstream.ConsumerConfig
	getError error
}

func (manager *provisionTestManager) Get(context.Context, string, string) (jetstream.ConsumerConfig, error) {
	return manager.current, manager.getError
}

func (manager *provisionTestManager) Create(_ context.Context, _ string, config jetstream.ConsumerConfig) error {
	manager.created = config
	return nil
}

func provisionTestConfig() jetstream.ConsumerConfig {
	return jetstream.ConsumerConfig{
		Name: "PEERGO_SETTLEMENT_V1", Durable: "PEERGO_SETTLEMENT_V1",
		Description:   "PeerGo Settlement raw Tracker ledger ingest v1",
		DeliverPolicy: jetstream.DeliverAllPolicy, AckPolicy: jetstream.AckExplicitPolicy,
		AckWait: 30 * time.Second, MaxDeliver: -1,
		FilterSubject: "peergo.tracker.announce.v1", ReplayPolicy: jetstream.ReplayInstantPolicy,
		MaxWaiting: 16, MaxAckPending: 1, MaxRequestBatch: 1, MaxRequestExpires: 5 * time.Second,
		Metadata: map[string]string{"peergo.owner": "settlement", "peergo.schema": "tracker.announce.v1"},
	}
}
