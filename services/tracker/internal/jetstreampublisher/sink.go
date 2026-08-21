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
	"github.com/peergo/peergo/services/tracker/internal/announcepublisher"
)

var (
	ErrConfig = errors.New("Tracker JetStream publisher configuration is invalid")
	ErrAck    = errors.New("Tracker JetStream publish acknowledgement is invalid")
)

type publishClient interface {
	Publish(context.Context, string, []byte, ...jetstream.PublishOpt) (*jetstream.PubAck, error)
	PublishAsync(string, []byte, ...jetstream.PublishOpt) (jetstream.PubAckFuture, error)
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
	if err := validateMessage(eventID, payload); err != nil {
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

// PublishBatch pipelines messages in WAL order and then waits for every
// individual JetStream storage ACK. Internal NATS retries are disabled so a
// failed batch is retried as one stable-ID prefix by announcepublisher; this
// preserves the observable send order without weakening at-least-once safety.
func (sink *Sink) PublishBatch(ctx context.Context, messages []announcepublisher.Message) error {
	if len(messages) == 0 {
		return ErrConfig
	}
	for _, message := range messages {
		if err := validateMessage(message.EventID, message.Payload); err != nil {
			return ErrConfig
		}
	}

	futures := make([]jetstream.PubAckFuture, 0, len(messages))
	for _, message := range messages {
		if err := ctx.Err(); err != nil {
			return err
		}
		future, err := sink.client.PublishAsync(
			sink.subject, message.Payload,
			jetstream.WithMsgID(message.EventID),
			jetstream.WithExpectStream(sink.stream),
			jetstream.WithRetryAttempts(0),
		)
		if err != nil {
			return fmt.Errorf("publish Tracker announce batch to JetStream: %w", err)
		}
		futures = append(futures, future)
	}

	for _, future := range futures {
		select {
		case ack := <-future.Ok():
			if ack == nil || ack.Stream != sink.stream || ack.Sequence == 0 {
				return ErrAck
			}
		case err := <-future.Err():
			if err == nil {
				return ErrAck
			}
			return fmt.Errorf("acknowledge Tracker announce batch in JetStream: %w", err)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func validateMessage(eventID string, payload []byte) error {
	event, err := announceevent.Decode(payload)
	if err != nil || event.EventID != eventID {
		return ErrConfig
	}
	return nil
}

func ValidStreamName(value string) bool {
	return trackerannouncev1.ValidStreamName(value)
}

func ValidLiteralSubject(value string) bool {
	return trackerannouncev1.ValidLiteralSubject(value)
}
