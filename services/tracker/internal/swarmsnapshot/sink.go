package swarmsnapshot

import (
	"context"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
)

var ErrAck = errors.New("Tracker swarm snapshot storage acknowledgement is invalid")

type publishClient interface {
	Publish(context.Context, string, []byte, ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

type JetStreamSink struct {
	client  publishClient
	stream  string
	subject string
}

func NewJetStreamSink(client publishClient, stream, subject string) (*JetStreamSink, error) {
	if client == nil || !trackerswarmv1.ValidStreamName(stream) || !trackerswarmv1.ValidLiteralSubject(subject) {
		return nil, ErrConfig
	}
	return &JetStreamSink{client: client, stream: stream, subject: subject}, nil
}

func (sink *JetStreamSink) Publish(ctx context.Context, chunk EncodedChunk) error {
	decoded, err := trackerswarmv1.Decode(chunk.Payload)
	if err != nil || decoded.EventID != chunk.Event.EventID || decoded.SnapshotID != chunk.Event.SnapshotID {
		return ErrConfig
	}
	ack, err := sink.client.Publish(ctx, sink.subject, chunk.Payload,
		jetstream.WithMsgID(chunk.Event.EventID), jetstream.WithExpectStream(sink.stream))
	if err != nil {
		return fmt.Errorf("publish Tracker swarm snapshot chunk: %w", err)
	}
	if ack == nil || ack.Stream != sink.stream || ack.Sequence == 0 {
		return ErrAck
	}
	return nil
}
