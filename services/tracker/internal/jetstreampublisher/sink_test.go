package jetstreampublisher

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/services/tracker/internal/announceevent"
	"github.com/peergo/peergo/services/tracker/internal/announcepublisher"
)

func TestSinkPublishesCanonicalEventWithIdempotencyAndStreamExpectation(t *testing.T) {
	event := jetStreamTestEvent(t)
	payload, err := announceevent.Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	client := &jetStreamTestClient{ack: &jetstream.PubAck{Stream: "PEERGO_TRACKER_ANNOUNCE_V1", Sequence: 9, Duplicate: true}}
	sink, err := NewSink(client, "PEERGO_TRACKER_ANNOUNCE_V1", "peergo.tracker.announce.v1")
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := sink.PublishWithEvidence(context.Background(), event.EventID, payload)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Stream != "PEERGO_TRACKER_ANNOUNCE_V1" || evidence.Sequence != 9 || !evidence.Duplicate {
		t.Fatalf("publish evidence = %+v", evidence)
	}
	if client.subject != "peergo.tracker.announce.v1" || !bytes.Equal(client.payload, payload) || client.optionCount != 2 {
		t.Fatalf("publish subject=%q payload=%q options=%d", client.subject, client.payload, client.optionCount)
	}
}

func TestSinkRejectsMismatchedEventAndInvalidAcknowledgement(t *testing.T) {
	event := jetStreamTestEvent(t)
	payload, err := announceevent.Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	client := &jetStreamTestClient{ack: &jetstream.PubAck{Stream: "other", Sequence: 1}}
	sink, err := NewSink(client, "PEERGO_TRACKER_ANNOUNCE_V1", "peergo.tracker.announce.v1")
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Publish(context.Background(), "0198f20a-6da8-7e51-9c64-222222222222", payload); !errors.Is(err, ErrConfig) {
		t.Fatalf("mismatched event error = %v", err)
	}
	if err := sink.Publish(context.Background(), event.EventID, payload); !errors.Is(err, ErrAck) {
		t.Fatalf("invalid ack error = %v", err)
	}
}

func TestSinkPublishesBatchAndWaitsForEveryStorageAcknowledgement(t *testing.T) {
	event := jetStreamTestEvent(t)
	payload, err := announceevent.Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	client := &jetStreamTestClient{asyncAcks: []*jetstream.PubAck{
		{Stream: "PEERGO_TRACKER_ANNOUNCE_V1", Sequence: 10},
		{Stream: "PEERGO_TRACKER_ANNOUNCE_V1", Sequence: 11},
	}}
	sink, err := NewSink(client, "PEERGO_TRACKER_ANNOUNCE_V1", "peergo.tracker.announce.v1")
	if err != nil {
		t.Fatal(err)
	}
	messages := []announcepublisher.Message{
		{EventID: event.EventID, Payload: payload},
		{EventID: event.EventID, Payload: payload},
	}
	if err := sink.PublishBatch(context.Background(), messages); err != nil {
		t.Fatal(err)
	}
	if client.asyncCalls != 2 || client.asyncOptionCounts[0] != 3 || client.asyncOptionCounts[1] != 3 {
		t.Fatalf("async calls=%d option_counts=%v", client.asyncCalls, client.asyncOptionCounts)
	}
}

func TestSinkValidatesCompleteBatchBeforePublishing(t *testing.T) {
	event := jetStreamTestEvent(t)
	payload, err := announceevent.Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	client := &jetStreamTestClient{}
	sink, err := NewSink(client, "PEERGO_TRACKER_ANNOUNCE_V1", "peergo.tracker.announce.v1")
	if err != nil {
		t.Fatal(err)
	}
	err = sink.PublishBatch(context.Background(), []announcepublisher.Message{
		{EventID: event.EventID, Payload: payload},
		{EventID: "0198f20a-6da8-7e51-9c64-222222222222", Payload: payload},
	})
	if !errors.Is(err, ErrConfig) || client.asyncCalls != 0 {
		t.Fatalf("PublishBatch() error=%v async_calls=%d", err, client.asyncCalls)
	}
}

func TestNamesRejectWildcardsAndPathCharacters(t *testing.T) {
	if !ValidStreamName("PEERGO_TRACKER_ANNOUNCE_V1") || ValidStreamName("bad.name") || ValidStreamName("bad/name") {
		t.Fatal("stream name validation boundary is incorrect")
	}
	if !ValidLiteralSubject("peergo.tracker.announce.v1") || ValidLiteralSubject("peergo.tracker.*") || ValidLiteralSubject("peergo..announce") {
		t.Fatal("literal subject validation boundary is incorrect")
	}
}

type jetStreamTestClient struct {
	ack               *jetstream.PubAck
	err               error
	subject           string
	payload           []byte
	optionCount       int
	asyncAcks         []*jetstream.PubAck
	asyncErrors       []error
	asyncCalls        int
	asyncOptionCounts []int
}

func (client *jetStreamTestClient) Publish(_ context.Context, subject string, payload []byte, options ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	client.subject = subject
	client.payload = append([]byte(nil), payload...)
	client.optionCount = len(options)
	return client.ack, client.err
}

func (client *jetStreamTestClient) PublishAsync(subject string, payload []byte, options ...jetstream.PublishOpt) (jetstream.PubAckFuture, error) {
	index := client.asyncCalls
	client.asyncCalls++
	client.subject = subject
	client.payload = append([]byte(nil), payload...)
	client.asyncOptionCounts = append(client.asyncOptionCounts, len(options))
	if index < len(client.asyncErrors) && client.asyncErrors[index] != nil {
		return nil, client.asyncErrors[index]
	}
	var ack *jetstream.PubAck
	if index < len(client.asyncAcks) {
		ack = client.asyncAcks[index]
	}
	ok := make(chan *jetstream.PubAck, 1)
	ok <- ack
	return &jetStreamTestFuture{ok: ok, err: make(chan error), msg: &nats.Msg{Subject: subject, Data: payload}}, nil
}

type jetStreamTestFuture struct {
	ok  <-chan *jetstream.PubAck
	err <-chan error
	msg *nats.Msg
}

func (future *jetStreamTestFuture) Ok() <-chan *jetstream.PubAck { return future.ok }
func (future *jetStreamTestFuture) Err() <-chan error            { return future.err }
func (future *jetStreamTestFuture) Msg() *nats.Msg               { return future.msg }

func jetStreamTestEvent(t *testing.T) announceevent.Event {
	t.Helper()
	event, err := announceevent.NewFactory(bytes.NewReader(bytes.Repeat([]byte{0x71}, 16))).New(announceevent.Input{
		ReceivedAt: time.Date(2026, 8, 8, 20, 0, 0, 0, time.UTC),
		UserID:     "0198f20a-6da8-7e51-9c64-111111111111", TorrentID: 42,
		InfoHash: [20]byte{1}, PeerID: [20]byte{2}, AddressFamily: 4,
		Uploaded: 1, Downloaded: 2, Left: 3, CredentialVersion: 1,
		TorrentControlSequence: 4, SubjectControlSequence: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	return event
}
