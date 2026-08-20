package hnroutbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/contracts/go/settlementhnrv1"
)

func TestJetStreamPublisherRequiresMatchingHNRStorageAck(t *testing.T) {
	t.Parallel()
	event, payload := testEvent(t)
	publisher, err := NewJetStreamPublisher(&fakePublishClient{
		ack: &jetstream.PubAck{Stream: settlementhnrv1.DefaultStream, Sequence: 3},
	}, settlementhnrv1.DefaultStream, settlementhnrv1.DefaultSubject)
	if err != nil {
		t.Fatal(err)
	}
	pending := PendingEvent{EventID: uuid.MustParse(event.EventID), Event: event, Payload: payload}
	if err := publisher.Publish(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	publisher, err = NewJetStreamPublisher(&fakePublishClient{
		ack: &jetstream.PubAck{Stream: "OTHER", Sequence: 3},
	}, settlementhnrv1.DefaultStream, settlementhnrv1.DefaultSubject)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(context.Background(), pending); !errors.Is(err, ErrPublishAck) {
		t.Fatalf("Publish() error=%v", err)
	}
}

type fakePublishClient struct {
	ack *jetstream.PubAck
	err error
}

func (client *fakePublishClient) Publish(_ context.Context, _ string, _ []byte, _ ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	return client.ack, client.err
}

func testEvent(t *testing.T) (settlementhnrv1.Event, []byte) {
	t.Helper()
	completedAt := time.Date(2026, time.August, 10, 8, 0, 0, 0, time.UTC)
	event := settlementhnrv1.Event{
		SchemaVersion: settlementhnrv1.SchemaVersion,
		EventID:       "0198f20a-6da8-7e51-9c64-111111111111", OccurredAt: completedAt.Add(time.Minute),
		ObligationID: "0198f20a-6da8-4e51-9c64-222222222222", ObligationVersion: 1,
		UserID: "0198f20a-6da8-4e51-9c64-333333333333", TorrentID: 42,
		CompletedAt: completedAt, State: settlementhnrv1.StateTracking,
		SeededSeconds: 0, RequiredSeedSeconds: 604800,
		RawUploaded: 100, RawDownloaded: 1000, RawRatioBasisPoints: 1000, RequiredRatioBPS: 10_000,
		AssessmentDueAt: completedAt.Add(7 * 24 * time.Hour), GraceEndsAt: completedAt.Add(8 * 24 * time.Hour),
	}
	payload, err := settlementhnrv1.Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	return event, payload
}
