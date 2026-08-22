package jetstreamconsumer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/services/settlement/internal/ingest"
)

func TestProcessMessageDoubleAcksOnlyAfterCommit(t *testing.T) {
	processor := &testProcessor{results: []testProcessResult{{result: ingest.ProcessResult{EventID: "event-1", Outcome: ingest.OutcomeBaseline}}}}
	message := validTestMessage()
	runner := testRunner(t, processor)
	if err := runner.processMessage(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if message.doubleAcks != 1 || message.inProgress != 0 || processor.calls != 1 {
		t.Fatalf("calls: process=%d double_ack=%d progress=%d", processor.calls, message.doubleAcks, message.inProgress)
	}
}

func TestProcessBatchPreservesOrderAndAcknowledgesEveryCommittedMessage(t *testing.T) {
	processor := &testProcessor{results: []testProcessResult{
		{result: ingest.ProcessResult{EventID: "event-1", Outcome: ingest.OutcomeBaseline}},
		{result: ingest.ProcessResult{EventID: "event-2", Outcome: ingest.OutcomeInterval}},
	}}
	first := validTestMessage()
	second := validTestMessage()
	second.metadata.Sequence.Stream = first.metadata.Sequence.Stream + 1
	second.metadata.Sequence.Consumer = first.metadata.Sequence.Consumer + 1
	runner := testRunner(t, processor)
	if err := runner.processBatch(context.Background(), []Message{first, second}); err != nil {
		t.Fatal(err)
	}
	if processor.calls != 2 || first.doubleAcks != 1 || second.doubleAcks != 1 {
		t.Fatalf("batch calls=%d acknowledgements=%d/%d", processor.calls, first.doubleAcks, second.doubleAcks)
	}
}

func TestProcessBatchRestoresStreamOrderForRedelivery(t *testing.T) {
	processor := &testProcessor{results: []testProcessResult{
		{result: ingest.ProcessResult{EventID: "event-1", Outcome: ingest.OutcomeBaseline}},
		{result: ingest.ProcessResult{EventID: "event-2", Outcome: ingest.OutcomeInterval}},
	}}
	older := validTestMessage()
	older.metadata.NumDelivered = 2
	newer := validTestMessage()
	newer.metadata.Sequence.Stream = older.metadata.Sequence.Stream + 1
	newer.metadata.Sequence.Consumer = older.metadata.Sequence.Consumer - 1
	runner := testRunner(t, processor)
	if err := runner.processBatch(context.Background(), []Message{newer, older}); err != nil {
		t.Fatal(err)
	}
	if len(processor.sequences) != 2 || processor.sequences[0] != 7 || processor.sequences[1] != 8 {
		t.Fatalf("processed stream sequences = %v, want [7 8]", processor.sequences)
	}
	if older.doubleAcks != 1 || newer.doubleAcks != 1 {
		t.Fatalf("acknowledgements older/newer=%d/%d", older.doubleAcks, newer.doubleAcks)
	}
}

func TestProcessBatchRejectsDuplicateStreamSequence(t *testing.T) {
	processor := &testProcessor{}
	first := validTestMessage()
	second := validTestMessage()
	second.metadata.Sequence.Consumer++
	runner := testRunner(t, processor)
	err := runner.processBatch(context.Background(), []Message{first, second})
	if !errors.Is(err, ingest.ErrSourceInvariant) || processor.calls != 0 ||
		first.doubleAcks != 0 || second.doubleAcks != 0 {
		t.Fatalf("processBatch() error=%v calls=%d acknowledgements=%d/%d",
			err, processor.calls, first.doubleAcks, second.doubleAcks)
	}
}

func TestProcessMessageRetriesSameDeliveryOnTransientFailure(t *testing.T) {
	processor := &testProcessor{results: []testProcessResult{
		{err: errors.New("database temporarily unavailable")},
		{result: ingest.ProcessResult{EventID: "event-1", Outcome: ingest.OutcomeInterval}},
	}}
	message := validTestMessage()
	runner := testRunner(t, processor)
	if err := runner.processMessage(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if processor.calls != 2 || message.inProgress != 1 || message.doubleAcks != 1 {
		t.Fatalf("calls: process=%d progress=%d double_ack=%d", processor.calls, message.inProgress, message.doubleAcks)
	}
}

func TestProcessMessageLeavesPermanentFailureUnacknowledged(t *testing.T) {
	processor := &testProcessor{results: []testProcessResult{{err: ingest.ErrInvalidInput}}}
	message := validTestMessage()
	runner := testRunner(t, processor)
	err := runner.processMessage(context.Background(), message)
	if !errors.Is(err, ingest.ErrInvalidInput) {
		t.Fatalf("processMessage() error = %v", err)
	}
	if message.doubleAcks != 0 || message.inProgress != 0 {
		t.Fatalf("permanent failure was acknowledged: double_ack=%d progress=%d", message.doubleAcks, message.inProgress)
	}
}

func TestProcessMessageRejectsUnexpectedMetadataBeforeDatabase(t *testing.T) {
	processor := &testProcessor{}
	message := validTestMessage()
	message.metadata.Stream = "OTHER"
	runner := testRunner(t, processor)
	err := runner.processMessage(context.Background(), message)
	if !errors.Is(err, ingest.ErrSourceInvariant) || processor.calls != 0 || message.doubleAcks != 0 {
		t.Fatalf("processMessage() = %v, process calls=%d, double acks=%d", err, processor.calls, message.doubleAcks)
	}
}

func TestProcessMessageDoesNotTreatLostDoubleAckAsDatabaseFailure(t *testing.T) {
	processor := &testProcessor{results: []testProcessResult{{result: ingest.ProcessResult{EventID: "event-1", Outcome: ingest.OutcomeBaseline}}}}
	message := validTestMessage()
	message.doubleAckError = errors.New("ack confirmation timeout")
	runner := testRunner(t, processor)
	if err := runner.processMessage(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if processor.calls != 1 || message.doubleAcks != 1 {
		t.Fatalf("calls: process=%d double_ack=%d", processor.calls, message.doubleAcks)
	}
}

func testRunner(t *testing.T, processor ingest.BatchProcessor) *Runner {
	t.Helper()
	runner, err := NewRunner(&testSource{}, processor, RunnerConfig{
		Stream: "PEERGO_TRACKER_ANNOUNCE_V1", Subject: "peergo.tracker.announce.v1",
		Durable: "PEERGO_SETTLEMENT_V1", ProcessTimeout: time.Second,
		AckTimeout: time.Second, RetryDelay: 10 * time.Millisecond, BatchSize: 64,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

type testProcessResult struct {
	result ingest.ProcessResult
	err    error
}

type testProcessor struct {
	results   []testProcessResult
	sequences []uint64
	calls     int
}

func (processor *testProcessor) ProcessBatch(_ context.Context, deliveries []ingest.Delivery) ([]ingest.ProcessResult, error) {
	results := make([]ingest.ProcessResult, len(deliveries))
	for index := range deliveries {
		processor.sequences = append(processor.sequences, deliveries[index].Sequence)
		resultIndex := processor.calls
		processor.calls++
		if resultIndex >= len(processor.results) {
			return nil, errors.New("unexpected processor call")
		}
		if processor.results[resultIndex].err != nil {
			return nil, processor.results[resultIndex].err
		}
		results[index] = processor.results[resultIndex].result
	}
	return results, nil
}

type testSource struct{}

func (*testSource) Next(context.Context) (Message, error)             { return nil, nil }
func (*testSource) NextBatch(context.Context, int) ([]Message, error) { return nil, nil }

type testMessage struct {
	metadata       jetstream.MsgMetadata
	subject        string
	payload        []byte
	doubleAckError error
	doubleAcks     int
	inProgress     int
}

func validTestMessage() *testMessage {
	return &testMessage{
		metadata: jetstream.MsgMetadata{
			Stream: "PEERGO_TRACKER_ANNOUNCE_V1", Consumer: "PEERGO_SETTLEMENT_V1",
			Sequence: jetstream.SequencePair{Stream: 7, Consumer: 8}, NumDelivered: 1,
		},
		subject: "peergo.tracker.announce.v1", payload: []byte(`{"event":"fixture"}`),
	}
}

func (message *testMessage) Metadata() (*jetstream.MsgMetadata, error) { return &message.metadata, nil }
func (message *testMessage) Data() []byte                              { return message.payload }
func (message *testMessage) Subject() string                           { return message.subject }
func (message *testMessage) DoubleAck(context.Context) error {
	message.doubleAcks++
	return message.doubleAckError
}
func (message *testMessage) InProgress() error {
	message.inProgress++
	return nil
}
