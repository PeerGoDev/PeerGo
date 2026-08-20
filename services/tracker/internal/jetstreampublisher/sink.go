// Package jetstreampublisher adapts the Tracker announce publisher to the
// current nats.go JetStream API. It never provisions or mutates streams; the
// runtime identity only needs permission to publish to one literal subject.
package jetstreampublisher

import (
	"context"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/services/tracker/internal/announceevent"
)

var (
	ErrConfig = errors.New("Tracker JetStream publisher configuration is invalid")
	ErrAck    = errors.New("Tracker JetStream publish acknowledgement is invalid")
)

type publishClient interface {
	Publish(context.Context, string, []byte, ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

type Sink struct {
	client  publishClient
	stream  string
	subject string
}

// PublishEvidence exposes the storage acknowledgement fields needed by
// development corpus and operational verification. Runtime callers can keep
// using Publish; both paths share the exact same validation and publish code.
type PublishEvidence struct {
	Stream    string
	Sequence  uint64
	Duplicate bool
}

func NewSink(client publishClient, stream, subject string) (*Sink, error) {
	if client == nil || !ValidStreamName(stream) || !ValidLiteralSubject(subject) {
		return nil, ErrConfig
	}
	return &Sink{client: client, stream: stream, subject: subject}, nil
}

func (sink *Sink) Publish(ctx context.Context, eventID string, payload []byte) error {
	_, err := sink.PublishWithEvidence(ctx, eventID, payload)
	return err
}

func (sink *Sink) PublishWithEvidence(ctx context.Context, eventID string, payload []byte) (PublishEvidence, error) {
	event, err := announceevent.Decode(payload)
	if err != nil || event.EventID != eventID {
		return PublishEvidence{}, ErrConfig
	}
	ack, err := sink.client.Publish(
		ctx, sink.subject, payload,
		jetstream.WithMsgID(eventID),
		jetstream.WithExpectStream(sink.stream),
	)
	if err != nil {
		return PublishEvidence{}, fmt.Errorf("publish Tracker announce event to JetStream: %w", err)
	}
	if ack == nil || ack.Stream != sink.stream || ack.Sequence == 0 {
		return PublishEvidence{}, ErrAck
	}
	return PublishEvidence{Stream: ack.Stream, Sequence: ack.Sequence, Duplicate: ack.Duplicate}, nil
}

func ValidStreamName(value string) bool {
	return trackerannouncev1.ValidStreamName(value)
}

func ValidLiteralSubject(value string) bool {
	return trackerannouncev1.ValidLiteralSubject(value)
}
