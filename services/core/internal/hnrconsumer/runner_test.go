package hnrconsumer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/contracts/go/settlementhnrv1"
	"github.com/peergo/peergo/services/core/internal/modules/traffic"
	"github.com/peergo/peergo/services/core/internal/trafficconsumer"
)

func TestRunnerAcknowledgesOnlyAfterHNRProjection(t *testing.T) {
	t.Parallel()
	event, payload := testHNREvent(t)
	message := newHNRMessage(payload)
	projector := &recordingHNRProjector{result: traffic.HNRApplyResult{EventID: uuid.MustParse(event.EventID)}}
	runner, err := NewRunner(&singleHNRSource{message: message}, projector, testHNRRunnerConfig(), time.Now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.ProcessMessage(context.Background(), message); err != nil || !projector.called || !message.doubleAcked {
		t.Fatalf("ProcessMessage() called=%v ack=%v error=%v", projector.called, message.doubleAcked, err)
	}
}

func TestRunnerStopsWithoutAcknowledgingConflictingHNREvent(t *testing.T) {
	t.Parallel()
	_, payload := testHNREvent(t)
	message := newHNRMessage(payload)
	runner, err := NewRunner(&singleHNRSource{message: message}, &recordingHNRProjector{err: traffic.ErrConflict}, testHNRRunnerConfig(), time.Now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.ProcessMessage(context.Background(), message); !errors.Is(err, traffic.ErrConflict) || message.doubleAcked {
		t.Fatalf("ProcessMessage() error=%v ack=%v", err, message.doubleAcked)
	}
}

func testHNRRunnerConfig() RunnerConfig {
	return RunnerConfig{
		Stream: settlementhnrv1.DefaultStream, Subject: settlementhnrv1.DefaultSubject, Durable: "PEERGO_CORE_HNR_V1",
		ProcessTimeout: time.Second, AckTimeout: time.Second, RetryDelay: 10 * time.Millisecond,
	}
}

type singleHNRSource struct{ message trafficconsumer.Message }

func (source *singleHNRSource) Next(context.Context) (trafficconsumer.Message, error) {
	return source.message, nil
}

type recordingHNRProjector struct {
	result traffic.HNRApplyResult
	err    error
	called bool
}

func (projector *recordingHNRProjector) ApplyHNR(_ context.Context, _ []byte, _ time.Time) (traffic.HNRApplyResult, error) {
	projector.called = true
	return projector.result, projector.err
}

type hnrMessage struct {
	payload     []byte
	metadata    *jetstream.MsgMetadata
	doubleAcked bool
}

func newHNRMessage(payload []byte) *hnrMessage {
	return &hnrMessage{payload: payload, metadata: &jetstream.MsgMetadata{
		Stream: settlementhnrv1.DefaultStream, Consumer: "PEERGO_CORE_HNR_V1",
		Sequence: jetstream.SequencePair{Stream: 1}, NumDelivered: 1,
	}}
}

func (message *hnrMessage) Metadata() (*jetstream.MsgMetadata, error) { return message.metadata, nil }
func (message *hnrMessage) Data() []byte                              { return message.payload }
func (message *hnrMessage) Subject() string                           { return settlementhnrv1.DefaultSubject }
func (message *hnrMessage) DoubleAck(context.Context) error {
	message.doubleAcked = true
	return nil
}
func (message *hnrMessage) InProgress() error { return nil }

func testHNREvent(t *testing.T) (settlementhnrv1.Event, []byte) {
	t.Helper()
	completedAt := time.Date(2026, time.August, 10, 8, 0, 0, 0, time.UTC)
	event := settlementhnrv1.Event{
		SchemaVersion:       settlementhnrv1.SchemaVersion,
		EventID:             "0198f20a-6da8-7e51-9c64-111111111111",
		OccurredAt:          completedAt.Add(time.Minute),
		ObligationID:        "0198f20a-6da8-4e51-9c64-222222222222",
		ObligationVersion:   1,
		UserID:              "0198f20a-6da8-4e51-9c64-333333333333",
		TorrentID:           42,
		CompletedAt:         completedAt,
		State:               settlementhnrv1.StateTracking,
		RequiredSeedSeconds: 72 * 60 * 60,
		RawDownloaded:       1024,
		RequiredRatioBPS:    10000,
		AssessmentDueAt:     completedAt.Add(7 * 24 * time.Hour),
		GraceEndsAt:         completedAt.Add(10 * 24 * time.Hour),
	}
	payload, err := settlementhnrv1.Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	return event, payload
}
