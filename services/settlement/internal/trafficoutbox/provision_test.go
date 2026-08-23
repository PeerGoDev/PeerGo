package trafficoutbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/contracts/go/settlementtrafficv1"
)

func TestEnsureStreamCreatesAndChecksExistingConfig(t *testing.T) {
	t.Parallel()
	desired := testStreamConfig()
	manager := &fakeStreamManager{getErr: jetstream.ErrStreamNotFound}
	created, err := EnsureStream(context.Background(), manager, desired)
	if err != nil || !created || manager.created.Name != desired.Name {
		t.Fatalf("EnsureStream(create) = %v, %v", created, err)
	}
	manager.getErr = nil
	manager.current = desired
	created, err = EnsureStream(context.Background(), manager, desired)
	if err != nil || created {
		t.Fatalf("EnsureStream(existing) = %v, %v", created, err)
	}
	manager.current.MaxAge += time.Minute
	if created, err := EnsureStream(context.Background(), manager, desired); err != nil || created || manager.updated.MaxAge != desired.MaxAge {
		t.Fatalf("EnsureStream(MaxAge update) = %v, %v, updated=%+v", created, err, manager.updated)
	}
	manager.current = desired
	manager.current.Discard = jetstream.DiscardOld
	if _, err := EnsureStream(context.Background(), manager, desired); !errors.Is(err, ErrStreamDrift) {
		t.Fatalf("EnsureStream(other drift) error = %v", err)
	}
}

type fakeStreamManager struct {
	current jetstream.StreamConfig
	created jetstream.StreamConfig
	updated jetstream.StreamConfig
	getErr  error
}

func (manager *fakeStreamManager) Get(context.Context, string) (jetstream.StreamConfig, error) {
	return manager.current, manager.getErr
}
func (manager *fakeStreamManager) Create(_ context.Context, config jetstream.StreamConfig) error {
	manager.created = config
	return nil
}
func (manager *fakeStreamManager) Update(_ context.Context, config jetstream.StreamConfig) error {
	manager.updated = config
	return nil
}

func testStreamConfig() jetstream.StreamConfig {
	return jetstream.StreamConfig{
		Name: settlementtrafficv1.DefaultStream, Description: "PeerGo final traffic settlement results v1",
		Subjects: []string{settlementtrafficv1.DefaultSubject}, Retention: jetstream.LimitsPolicy, MaxConsumers: 4,
		MaxMsgs: -1, MaxBytes: 1 << 20, Discard: jetstream.DiscardNew, MaxAge: 7 * 24 * time.Hour,
		MaxMsgsPerSubject: -1, MaxMsgSize: settlementtrafficv1.MaxEventBytes, Storage: jetstream.FileStorage,
		Replicas: 1, Duplicates: 10 * time.Minute, Metadata: map[string]string{"peergo.owner": "settlement", "peergo.schema": settlementtrafficv1.SchemaVersion},
	}
}
