package jetstreampublisher

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/services/tracker/internal/announceevent"
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

func TestNamesRejectWildcardsAndPathCharacters(t *testing.T) {
	if !ValidStreamName("PEERGO_TRACKER_ANNOUNCE_V1") || ValidStreamName("bad.name") || ValidStreamName("bad/name") {
		t.Fatal("stream name validation boundary is incorrect")
	}
	if !ValidLiteralSubject("peergo.tracker.announce.v1") || ValidLiteralSubject("peergo.tracker.*") || ValidLiteralSubject("peergo..announce") {
		t.Fatal("literal subject validation boundary is incorrect")
	}
}

type jetStreamTestClient struct {
	ack         *jetstream.PubAck
	err         error
	subject     string
	payload     []byte
	optionCount int
}

func (client *jetStreamTestClient) Publish(_ context.Context, subject string, payload []byte, options ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	client.subject = subject
	client.payload = append([]byte(nil), payload...)
	client.optionCount = len(options)
	return client.ack, client.err
}

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
