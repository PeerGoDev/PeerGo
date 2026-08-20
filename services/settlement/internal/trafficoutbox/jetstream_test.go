package trafficoutbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/contracts/go/settlementtrafficv1"
)

func TestJetStreamPublisherRequiresMatchingStorageAck(t *testing.T) {
	t.Parallel()
	event, payload := testEvent(t)
	publisher, err := NewJetStreamPublisher(&fakePublishClient{ack: &jetstream.PubAck{Stream: settlementtrafficv1.DefaultStream, Sequence: 3}}, settlementtrafficv1.DefaultStream, settlementtrafficv1.DefaultSubject)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(context.Background(), PendingEvent{EventID: uuid.MustParse(event.EventID), Event: event, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	publisher, err = NewJetStreamPublisher(&fakePublishClient{ack: &jetstream.PubAck{Stream: "other", Sequence: 3}}, settlementtrafficv1.DefaultStream, settlementtrafficv1.DefaultSubject)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(context.Background(), PendingEvent{EventID: uuid.MustParse(event.EventID), Event: event, Payload: payload}); !errors.Is(err, ErrPublishAck) {
		t.Fatalf("Publish() error = %v", err)
	}
}

type fakePublishClient struct {
	ack *jetstream.PubAck
	err error
}

func (client *fakePublishClient) Publish(_ context.Context, _ string, _ []byte, _ ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	return client.ack, client.err
}

func testEvent(t *testing.T) (settlementtrafficv1.Event, []byte) {
	t.Helper()
	start := time.Date(2026, time.August, 9, 15, 0, 0, 0, time.UTC)
	event := settlementtrafficv1.Event{
		SchemaVersion: settlementtrafficv1.SchemaVersion, EventID: "0198f20a-6da8-7e51-9c64-111111111111", OccurredAt: start.Add(time.Minute),
		UserID: "0198f20a-6da8-4e51-9c64-111111111111", TorrentID: 7,
		IntervalStartsAt: start, IntervalEndsAt: start.Add(30 * time.Second),
		RawUploaded: 100, RawDownloaded: 100, CreditedUploaded: 200, ChargedDownloaded: 0,
		SettlementSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	payload, err := settlementtrafficv1.Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	return event, payload
}
