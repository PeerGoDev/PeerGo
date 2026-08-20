package trafficconsumer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/contracts/go/settlementtrafficv1"
	"github.com/peergo/peergo/services/core/internal/modules/traffic"
)

func TestRunnerAcknowledgesOnlyAfterProjection(t *testing.T) {
	t.Parallel()
	event, payload := testTrafficEvent(t)
	message := &fakeMessage{payload: payload, subject: settlementtrafficv1.DefaultSubject, metadata: &jetstream.MsgMetadata{
		Stream: settlementtrafficv1.DefaultStream, Consumer: "PEERGO_CORE_TRAFFIC_V1", Sequence: jetstream.SequencePair{Stream: 1}, NumDelivered: 1,
	}}
	projector := &recordingProjector{result: traffic.ApplyResult{EventID: uuid.MustParse(event.EventID)}}
	runner, err := NewRunner(&fakeSource{message: message}, projector, RunnerConfig{
		Stream: settlementtrafficv1.DefaultStream, Subject: settlementtrafficv1.DefaultSubject, Durable: "PEERGO_CORE_TRAFFIC_V1",
		ProcessTimeout: time.Second, AckTimeout: time.Second, RetryDelay: time.Millisecond * 10,
	}, time.Now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.processMessage(context.Background(), message); err != nil || !projector.called || !message.doubleAcked {
		t.Fatalf("processMessage() called=%v ack=%v error=%v", projector.called, message.doubleAcked, err)
	}
}

func TestRunnerStopsForConflictingEvent(t *testing.T) {
	t.Parallel()
	_, payload := testTrafficEvent(t)
	message := &fakeMessage{payload: payload, subject: settlementtrafficv1.DefaultSubject, metadata: &jetstream.MsgMetadata{
		Stream: settlementtrafficv1.DefaultStream, Consumer: "PEERGO_CORE_TRAFFIC_V1", Sequence: jetstream.SequencePair{Stream: 1}, NumDelivered: 1,
	}}
	runner, err := NewRunner(&fakeSource{message: message}, &recordingProjector{err: traffic.ErrConflict}, RunnerConfig{
		Stream: settlementtrafficv1.DefaultStream, Subject: settlementtrafficv1.DefaultSubject, Durable: "PEERGO_CORE_TRAFFIC_V1",
		ProcessTimeout: time.Second, AckTimeout: time.Second, RetryDelay: 10 * time.Millisecond,
	}, time.Now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.processMessage(context.Background(), message); !errors.Is(err, traffic.ErrConflict) || message.doubleAcked {
		t.Fatalf("processMessage() error=%v ack=%v", err, message.doubleAcked)
	}
}

type fakeSource struct{ message Message }

func (source *fakeSource) Next(context.Context) (Message, error) { return source.message, nil }

type recordingProjector struct {
	result traffic.ApplyResult
	err    error
	called bool
}

func (projector *recordingProjector) Apply(_ context.Context, _ []byte, _ time.Time) (traffic.ApplyResult, error) {
	projector.called = true
	return projector.result, projector.err
}

type fakeMessage struct {
	payload     []byte
	subject     string
	metadata    *jetstream.MsgMetadata
	doubleAcked bool
}

func (message *fakeMessage) Metadata() (*jetstream.MsgMetadata, error) { return message.metadata, nil }
func (message *fakeMessage) Data() []byte                              { return message.payload }
func (message *fakeMessage) Subject() string                           { return message.subject }
func (message *fakeMessage) DoubleAck(context.Context) error {
	message.doubleAcked = true
	return nil
}
func (message *fakeMessage) InProgress() error { return nil }

func testTrafficEvent(t *testing.T) (settlementtrafficv1.Event, []byte) {
	t.Helper()
	start := time.Date(2026, time.August, 9, 16, 0, 0, 0, time.UTC)
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
