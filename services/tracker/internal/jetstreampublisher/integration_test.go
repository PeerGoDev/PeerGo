package jetstreampublisher_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/services/tracker/internal/announceevent"
	"github.com/peergo/peergo/services/tracker/internal/announcepublisher"
	"github.com/peergo/peergo/services/tracker/internal/jetstreampublisher"
	"github.com/peergo/peergo/services/tracker/internal/wal"
)

// This test is gated because it needs a real JetStream server. It verifies the
// storage ACK -> durable checkpoint boundary and the duplicate replay behavior
// that mocks cannot prove. Run it against the local Compose fixture with:
//
//	PEERGO_TEST_NATS_URL=nats://127.0.0.1:4222 go test ./internal/jetstreampublisher -run Integration
func TestIntegrationPublisherCheckpointsStorageACKAndDeduplicatesReplay(t *testing.T) {
	serverURL := strings.TrimSpace(os.Getenv("PEERGO_TEST_NATS_URL"))
	if serverURL == "" {
		t.Skip("PEERGO_TEST_NATS_URL is not configured")
	}

	connection, js, err := jetstreampublisher.Connect(jetstreampublisher.ConnectionConfig{
		URLs: []string{serverURL}, ConnectTimeout: 2 * time.Second, ReconnectWait: 100 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()

	suffix := integrationSuffix(t)
	streamName := "PEERGO_TRACKER_TEST_" + strings.ToUpper(suffix)
	subject := "peergo.tracker.test." + suffix
	operationCtx, operationCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer operationCancel()
	stream, err := js.CreateStream(operationCtx, jetstream.StreamConfig{
		Name: streamName, Subjects: []string{subject}, Retention: jetstream.LimitsPolicy,
		MaxConsumers: 1, MaxMsgs: -1, MaxBytes: 1 << 20, Discard: jetstream.DiscardNew,
		MaxAge: time.Minute, MaxMsgsPerSubject: -1, MaxMsgSize: 20 << 10,
		Storage: jetstream.MemoryStorage, Replicas: 1, Duplicates: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := js.DeleteStream(cleanupCtx, streamName); err != nil {
			t.Errorf("delete integration stream: %v", err)
		}
	}()

	event := integrationEvent(t)
	payload, err := announceevent.Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	walPath := directory + "/announce.wal"
	eventLog, err := wal.OpenFile(walPath, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := eventLog.Append(event); err != nil {
		t.Fatal(err)
	}
	sink, err := jetstreampublisher.NewSink(js, streamName, subject)
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := announcepublisher.New(eventLog, sink, announcepublisher.Config{
		PublishTimeout: 2 * time.Second, RetryMinimum: 10 * time.Millisecond,
		RetryMaximum: 100 * time.Millisecond, CompactAtBytes: 1 << 20,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	publisherCtx, stopPublisher := context.WithCancel(context.Background())
	publisherResult := make(chan error, 1)
	go func() { publisherResult <- publisher.Run(publisherCtx) }()
	waitForCheckpoint(t, eventLog)
	stopPublisher()
	select {
	case err := <-publisherResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("publisher did not stop after cancellation")
	}

	stored, err := stream.GetLastMsgForSubject(operationCtx, subject)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored.Data, payload) || stored.Header.Get(jetstream.MsgIDHeader) != event.EventID {
		t.Fatalf("stored event payload/header mismatch: sequence=%d msg_id=%q", stored.Sequence, stored.Header.Get(jetstream.MsgIDHeader))
	}
	if err := eventLog.Close(); err != nil {
		t.Fatal(err)
	}

	// A durable checkpoint must survive restart and leave no record pending.
	reopened, err := wal.OpenFile(walPath, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, found, err := reopened.Next(); err != nil || found {
		t.Fatalf("reopened WAL Next() found=%v error=%v", found, err)
	}

	// A crash after the server stores a record but before the local checkpoint
	// can replay the same event ID. JetStream must ACK that replay without
	// storing a second business event inside the configured duplicate window.
	if err := sink.Publish(operationCtx, event.EventID, payload); err != nil {
		t.Fatal(err)
	}
	info, err := stream.Info(operationCtx)
	if err != nil {
		t.Fatal(err)
	}
	if info.State.Msgs != 1 {
		t.Fatalf("stream stored %d messages after duplicate replay, want 1", info.State.Msgs)
	}
}

func integrationSuffix(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(raw)
}

func integrationEvent(t *testing.T) announceevent.Event {
	t.Helper()
	event, err := announceevent.NewFactory(bytes.NewReader(bytes.Repeat([]byte{0x42}, 16))).New(announceevent.Input{
		ReceivedAt: time.Now().UTC().Round(0),
		UserID:     "0198f20a-6da8-7e51-9c64-111111111111", TorrentID: 42,
		InfoHash: [20]byte{1}, PeerID: [20]byte{2}, Key: "integration-client", AddressFamily: 4,
		Uploaded: 1, Downloaded: 2, Left: 3, CredentialVersion: 1,
		TorrentControlSequence: 4, SubjectControlSequence: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func waitForCheckpoint(t *testing.T, eventLog *wal.File) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		pending, err := eventLog.PendingBytes()
		if err != nil {
			t.Fatal(err)
		}
		if pending == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("publisher did not durably checkpoint the event")
}
