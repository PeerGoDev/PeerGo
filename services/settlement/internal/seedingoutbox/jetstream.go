package seedingoutbox

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/contracts/go/settlementseedingv1"
)

type publishClient interface {
	Publish(context.Context, string, []byte, ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

type JetStreamPublisher struct {
	client  publishClient
	stream  string
	subject string
}

func NewJetStreamPublisher(client publishClient, stream, subject string) (*JetStreamPublisher, error) {
	if client == nil || !settlementseedingv1.ValidStreamName(stream) || !settlementseedingv1.ValidLiteralSubject(subject) {
		return nil, ErrInput
	}
	return &JetStreamPublisher{client: client, stream: stream, subject: subject}, nil
}

func (publisher *JetStreamPublisher) Publish(ctx context.Context, pending PendingEvent) error {
	if pending.EventID.String() != pending.Event.EventID || len(pending.Payload) == 0 || settlementseedingv1.Validate(pending.Event) != nil {
		return ErrInput
	}
	ack, err := publisher.client.Publish(ctx, publisher.subject, pending.Payload,
		jetstream.WithMsgID(pending.Event.EventID), jetstream.WithExpectStream(publisher.stream))
	if err != nil {
		return fmt.Errorf("publish Settlement seeding evidence event to JetStream: %w", err)
	}
	if ack == nil || ack.Stream != publisher.stream || ack.Sequence == 0 {
		return ErrPublishAck
	}
	return nil
}

var _ Publisher = (*JetStreamPublisher)(nil)
