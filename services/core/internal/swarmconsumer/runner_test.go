package swarmconsumer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
	"github.com/peergo/peergo/services/core/internal/modules/swarmprojection"
	"github.com/peergo/peergo/services/core/internal/trafficconsumer"
)

func TestSnapshotRunnerAcknowledgesOnlyAfterProjection(t *testing.T) {
	t.Parallel()
	binding := testBinding()
	message := testMessage(binding)
	projector := &recordingSnapshotProjector{result: swarmprojection.ApplyResult{EventID: uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111")}}
	runner, err := NewSnapshotRunner(&fakeSource{message: message}, projector, binding, 10*time.Millisecond, time.Now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.processMessage(context.Background(), message); err != nil || projector.calls != 1 || !message.doubleAcked {
		t.Fatalf("processMessage() calls=%d ack=%v error=%v", projector.calls, message.doubleAcked, err)
	}
}

func TestCompletionRunnerRetriesTransientFailureAndSignalsProgress(t *testing.T) {
	t.Parallel()
	binding := testBinding()
	message := testMessage(binding)
	projector := &recordingCompletionProjector{failures: 1}
	runner, err := NewCompletionRunner(&fakeSource{message: message}, projector, binding, 10*time.Millisecond, time.Now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.processMessage(context.Background(), message); err != nil || projector.calls != 2 || message.inProgress != 1 || !message.doubleAcked {
		t.Fatalf("processMessage() calls=%d progress=%d ack=%v error=%v", projector.calls, message.inProgress, message.doubleAcked, err)
	}
}

func TestSnapshotRunnerStopsWithoutAckForConflictingFact(t *testing.T) {
	t.Parallel()
	binding := testBinding()
	message := testMessage(binding)
	runner, err := NewSnapshotRunner(&fakeSource{message: message}, &recordingSnapshotProjector{err: swarmprojection.ErrConflict}, binding, 10*time.Millisecond, time.Now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.processMessage(context.Background(), message); !errors.Is(err, swarmprojection.ErrConflict) || message.doubleAcked {
		t.Fatalf("processMessage() error=%v ack=%v", err, message.doubleAcked)
	}
}

func testBinding() trafficconsumer.BindingConfig {
	return trafficconsumer.BindingConfig{
		Stream: trackerswarmv1.DefaultStream, Subject: trackerswarmv1.DefaultSubject, Durable: "PEERGO_CORE_SWARM_SNAPSHOT_V1",
		FetchWait: time.Second, MaximumProcessingTime: time.Second, MaximumAckTime: time.Second,
	}
}

func testMessage(binding trafficconsumer.BindingConfig) *fakeMessage {
	return &fakeMessage{payload: []byte("payload"), subject: binding.Subject, metadata: &jetstream.MsgMetadata{
		Stream: binding.Stream, Consumer: binding.Durable, Sequence: jetstream.SequencePair{Stream: 1}, NumDelivered: 1,
	}}
}

type fakeSource struct{ message trafficconsumer.Message }

func (source *fakeSource) Next(context.Context) (trafficconsumer.Message, error) {
	return source.message, nil
}

type recordingSnapshotProjector struct {
	result swarmprojection.ApplyResult
	err    error
	calls  int
}

func (projector *recordingSnapshotProjector) ApplySnapshot(context.Context, []byte, time.Time) (swarmprojection.ApplyResult, error) {
	projector.calls++
	return projector.result, projector.err
}

type recordingCompletionProjector struct {
	failures int
	calls    int
}

func (projector *recordingCompletionProjector) ApplyCompletion(context.Context, []byte, time.Time) (swarmprojection.ApplyResult, error) {
	projector.calls++
	if projector.calls <= projector.failures {
		return swarmprojection.ApplyResult{}, errors.New("temporary database failure")
	}
	return swarmprojection.ApplyResult{}, nil
}

type fakeMessage struct {
	payload     []byte
	subject     string
	metadata    *jetstream.MsgMetadata
	doubleAcked bool
	inProgress  int
}

func (message *fakeMessage) Metadata() (*jetstream.MsgMetadata, error) { return message.metadata, nil }
func (message *fakeMessage) Data() []byte                              { return message.payload }
func (message *fakeMessage) Subject() string                           { return message.subject }
func (message *fakeMessage) DoubleAck(context.Context) error {
	message.doubleAcked = true
	return nil
}
func (message *fakeMessage) InProgress() error {
	message.inProgress++
	return nil
}
