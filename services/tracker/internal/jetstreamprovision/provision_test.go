package jetstreamprovision

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

func TestEnsureCreatesMissingStreamAndAcceptsExactReplay(t *testing.T) {
	desired := provisionTestConfig()
	manager := &provisionTestManager{getError: jetstream.ErrStreamNotFound}
	created, err := Ensure(context.Background(), manager, desired)
	if err != nil || !created || manager.created.Name != desired.Name {
		t.Fatalf("Ensure(create) = %v, %v, created config=%+v", created, err, manager.created)
	}
	manager.getError = nil
	manager.current = desired
	created, err = Ensure(context.Background(), manager, desired)
	if err != nil || created {
		t.Fatalf("Ensure(replay) = %v, %v", created, err)
	}
}

func TestEnsureRejectsExistingRetentionDrift(t *testing.T) {
	desired := provisionTestConfig()
	current := desired
	current.Discard = jetstream.DiscardOld
	created, err := Ensure(context.Background(), &provisionTestManager{current: current}, desired)
	if created || !errors.Is(err, ErrDrift) {
		t.Fatalf("Ensure(drift) = %v, %v", created, err)
	}
}

func TestEnsureUpdatesOnlyMaxAge(t *testing.T) {
	desired := provisionTestConfig()
	current := desired
	current.MaxAge = 7 * 24 * time.Hour
	desired.MaxAge = 12 * time.Hour
	manager := &provisionTestManager{current: current}
	created, err := Ensure(context.Background(), manager, desired)
	if err != nil || created || manager.updated.MaxAge != 12*time.Hour {
		t.Fatalf("Ensure(MaxAge update) = %v, %v, updated=%+v", created, err, manager.updated)
	}
}

func TestEnsureRejectsPersistenceAndRepublishDrift(t *testing.T) {
	desired := provisionTestConfig()
	for name, mutate := range map[string]func(*jetstream.StreamConfig){
		"async persistence": func(config *jetstream.StreamConfig) {
			config.PersistMode = jetstream.AsyncPersistMode
		},
		"republish": func(config *jetstream.StreamConfig) {
			config.RePublish = &jetstream.RePublish{Source: "peergo.tracker.announce.v1", Destination: "unexpected.events"}
		},
		"message TTL": func(config *jetstream.StreamConfig) {
			config.AllowMsgTTL = true
		},
	} {
		t.Run(name, func(t *testing.T) {
			current := desired
			mutate(&current)
			created, err := Ensure(context.Background(), &provisionTestManager{current: current}, desired)
			if created || !errors.Is(err, ErrDrift) {
				t.Fatalf("Ensure(drift) = %v, %v", created, err)
			}
		})
	}
}

func TestEnsureRejectsUnsafeDesiredPersistenceMode(t *testing.T) {
	desired := provisionTestConfig()
	desired.PersistMode = jetstream.AsyncPersistMode
	created, err := Ensure(context.Background(), &provisionTestManager{getError: jetstream.ErrStreamNotFound}, desired)
	if created || !errors.Is(err, ErrConfig) {
		t.Fatalf("Ensure(unsafe config) = %v, %v", created, err)
	}
}

func TestEnsureAcceptsServerOwnedMetadataButRejectsApplicationMetadataDrift(t *testing.T) {
	desired := provisionTestConfig()
	current := desired
	current.Metadata = map[string]string{
		"peergo.schema":   "tracker.announce.v1",
		"_nats.req.level": "0",
	}
	created, err := Ensure(context.Background(), &provisionTestManager{current: current}, desired)
	if err != nil || created {
		t.Fatalf("Ensure(server metadata) = %v, %v", created, err)
	}
	current.Metadata["unexpected.owner"] = "other"
	created, err = Ensure(context.Background(), &provisionTestManager{current: current}, desired)
	if created || !errors.Is(err, ErrDrift) {
		t.Fatalf("Ensure(application metadata drift) = %v, %v", created, err)
	}
}

type provisionTestManager struct {
	current  jetstream.StreamConfig
	created  jetstream.StreamConfig
	updated  jetstream.StreamConfig
	getError error
}

func (manager *provisionTestManager) Get(context.Context, string) (jetstream.StreamConfig, error) {
	return manager.current, manager.getError
}

func (manager *provisionTestManager) Create(_ context.Context, config jetstream.StreamConfig) error {
	manager.created = config
	return nil
}

func (manager *provisionTestManager) Update(_ context.Context, config jetstream.StreamConfig) error {
	manager.updated = config
	return nil
}

func provisionTestConfig() jetstream.StreamConfig {
	return jetstream.StreamConfig{
		Name: "PEERGO_TRACKER_ANNOUNCE_V1", Subjects: []string{"peergo.tracker.announce.v1"},
		Description: "PeerGo immutable Tracker announce events v1",
		Retention:   jetstream.LimitsPolicy, MaxConsumers: 32, MaxMsgs: -1, MaxBytes: 1 << 30,
		Discard: jetstream.DiscardNew, MaxAge: 7 * 24 * time.Hour, MaxMsgsPerSubject: -1,
		MaxMsgSize: 32 << 10, Storage: jetstream.FileStorage, Replicas: 1,
		Duplicates: 10 * time.Minute, DenyDelete: true, DenyPurge: true,
		Compression: jetstream.S2Compression,
		Metadata:    map[string]string{"peergo.schema": "tracker.announce.v1"},
	}
}
