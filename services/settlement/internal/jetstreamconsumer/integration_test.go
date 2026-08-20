package jetstreamconsumer_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/services/settlement/internal/ingest"
	"github.com/peergo/peergo/services/settlement/internal/jetstreamconsumer"
	platformpostgres "github.com/peergo/peergo/services/settlement/internal/platform/postgres"
)

// This gated test requires real JetStream and a disposable, migrated Tracker
// Ledger database. It verifies the publish -> pull -> PostgreSQL commit ->
// confirmed ACK boundary that mocks cannot establish. Example:
//
//	PEERGO_TEST_NATS_URL=nats://127.0.0.1:4222 \
//	PEERGO_TEST_TRACKER_DATABASE_URL=postgres://.../peergo_tracker_test?sslmode=disable \
//	go test ./internal/jetstreamconsumer -run Integration
func TestIntegrationConsumesOrderedCountersAndAcknowledgesAfterCommit(t *testing.T) {
	natsURL := strings.TrimSpace(os.Getenv("PEERGO_TEST_NATS_URL"))
	databaseURL := strings.TrimSpace(os.Getenv("PEERGO_TEST_TRACKER_DATABASE_URL"))
	if natsURL == "" || databaseURL == "" {
		t.Skip("PEERGO_TEST_NATS_URL and PEERGO_TEST_TRACKER_DATABASE_URL are required")
	}

	operationCtx, operationCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer operationCancel()
	pool, err := pgxpool.New(operationCtx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(operationCtx); err != nil {
		t.Fatal(err)
	}
	if err := platformpostgres.RequireCurrentMigration(operationCtx, pool); err != nil {
		t.Fatal(err)
	}

	connection, js, err := jetstreamconsumer.Connect(jetstreamconsumer.ConnectionConfig{
		URLs: []string{natsURL}, ConnectTimeout: 2 * time.Second, ReconnectWait: 100 * time.Millisecond,
	}, "peergo-settlement-integration-test", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	streamName := "PEERGO_SETTLEMENT_TEST_" + strings.ToUpper(suffix)
	subject := "peergo.settlement.test." + suffix
	durable := "PEERGO_SETTLEMENT_TEST_CONSUMER_" + strings.ToUpper(suffix)
	_, err = js.CreateStream(operationCtx, jetstream.StreamConfig{
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
	_, err = js.CreateConsumer(operationCtx, streamName, jetstream.ConsumerConfig{
		Name: durable, Durable: durable,
		Description:   "PeerGo Settlement integration test consumer",
		DeliverPolicy: jetstream.DeliverAllPolicy, AckPolicy: jetstream.AckExplicitPolicy,
		AckWait: 10 * time.Second, MaxDeliver: -1, FilterSubject: subject,
		ReplayPolicy: jetstream.ReplayInstantPolicy, MaxWaiting: 4,
		MaxAckPending: 1, MaxRequestBatch: 1, MaxRequestExpires: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	baseTime := time.Now().UTC().Add(-time.Minute).Round(0)
	userID := uuid.NewString()
	event1 := integrationEvent(t, baseTime, userID, 100, 200, 1_000, "started")
	event2 := integrationEvent(t, baseTime.Add(10*time.Second), userID, 600, 500, 0, "completed")
	event2.SessionToken = event1.SessionToken
	for _, event := range []trackerannouncev1.Event{event1, event2} {
		payload, encodeErr := trackerannouncev1.Encode(event)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		ack, publishErr := js.Publish(operationCtx, subject, payload,
			jetstream.WithMsgID(event.EventID), jetstream.WithExpectStream(streamName))
		if publishErr != nil || ack == nil || ack.Stream != streamName {
			t.Fatalf("publish %s: ack=%+v error=%v", event.EventID, ack, publishErr)
		}
	}

	source, err := jetstreamconsumer.OpenSource(operationCtx, js, jetstreamconsumer.BindingConfig{
		Stream: streamName, Subject: subject, Durable: durable,
		FetchWait: 500 * time.Millisecond, MaximumProcessingTime: 2 * time.Second,
		MaximumAckTime: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	repository, err := ingest.NewPostgresRepository(pool, streamName, subject, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := jetstreamconsumer.NewRunner(source, repository, jetstreamconsumer.RunnerConfig{
		Stream: streamName, Subject: subject, Durable: durable,
		ProcessTimeout: 2 * time.Second, AckTimeout: time.Second, RetryDelay: 20 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancelRun := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- runner.Run(runCtx) }()
	waitForSettlementRows(t, operationCtx, pool, event1.EventID, event2.EventID)
	cancelRun()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Settlement runner did not stop")
	}

	var rawUploaded, rawDownloaded int64
	var completed bool
	err = pool.QueryRow(operationCtx, `
        SELECT raw_uploaded, raw_downloaded, completed_transition
        FROM ledger.raw_session_intervals
        WHERE event_id = $1::uuid
    `, event2.EventID).Scan(&rawUploaded, &rawDownloaded, &completed)
	if err != nil || rawUploaded != 500 || rawDownloaded != 300 || !completed {
		t.Fatalf("raw interval = uploaded %d, downloaded %d, completed %v, error %v",
			rawUploaded, rawDownloaded, completed, err)
	}

	payload2, err := trackerannouncev1.Encode(event2)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := repository.Process(operationCtx, ingest.Delivery{
		Stream: streamName, Subject: subject, Sequence: 2, DeliveryCount: 2, Payload: payload2,
	})
	if err != nil || !duplicate.Duplicate || duplicate.Outcome != ingest.OutcomeInterval {
		t.Fatalf("duplicate result = %+v, error %v", duplicate, err)
	}
	conflicting := event2
	conflicting.Downloaded++
	conflictingPayload, err := trackerannouncev1.Encode(conflicting)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.Process(operationCtx, ingest.Delivery{
		Stream: streamName, Subject: subject, Sequence: 2, DeliveryCount: 3, Payload: conflictingPayload,
	})
	if !errors.Is(err, ingest.ErrEventConflict) || !ingest.IsPermanent(err) {
		t.Fatalf("conflicting duplicate error = %v", err)
	}
	sequenceConflict := integrationEvent(t, baseTime.Add(20*time.Second), userID, 700, 600, 0, "")
	sequenceConflict.SessionToken = event1.SessionToken
	sequenceConflictPayload, err := trackerannouncev1.Encode(sequenceConflict)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.Process(operationCtx, ingest.Delivery{
		Stream: streamName, Subject: subject, Sequence: 2, DeliveryCount: 1, Payload: sequenceConflictPayload,
	})
	if !errors.Is(err, ingest.ErrSourceInvariant) || !ingest.IsPermanent(err) {
		t.Fatalf("reused stream sequence error = %v", err)
	}

	consumer, err := js.Consumer(operationCtx, streamName, durable)
	if err != nil {
		t.Fatal(err)
	}
	info, err := consumer.Info(operationCtx)
	if err != nil || info.AckFloor.Stream != 2 || info.NumAckPending != 0 {
		t.Fatalf("consumer ACK state = %+v, error %v", info, err)
	}

	if _, err := pool.Exec(operationCtx, `
        UPDATE ledger.raw_session_intervals SET raw_uploaded = raw_uploaded + 1
        WHERE event_id = $1::uuid
    `, event2.EventID); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("immutable raw interval update error = %v", err)
	}
}

func integrationEvent(t *testing.T, receivedAt time.Time, userID string, uploaded, downloaded, left int64, kind string) trackerannouncev1.Event {
	t.Helper()
	eventID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	event := trackerannouncev1.Event{
		SchemaVersion: trackerannouncev1.SchemaVersion, EventID: eventID.String(), ReceivedAt: receivedAt,
		UserID: userID, TorrentID: 42, InfoHashV1: strings.Repeat("ab", 20),
		SessionToken: strings.Repeat("cd", 32), AddressFamily: 4, Event: kind,
		Uploaded: uploaded, Downloaded: downloaded, Left: left, CredentialVersion: 1,
		TorrentControlSequence: 7, SubjectControlSequence: 9,
	}
	if kind == "completed" && left == 0 {
		event.CompletionID = strings.Repeat("ef", 32)
	}
	return event
}

func waitForSettlementRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, firstEventID, secondEventID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var baselines, intervals int
		err := pool.QueryRow(ctx, `
            SELECT
                count(*) FILTER (WHERE outcome = 'baseline'),
                count(*) FILTER (WHERE outcome = 'interval')
            FROM settlement.event_inbox
            WHERE event_id IN ($1::uuid, $2::uuid)
        `, firstEventID, secondEventID).Scan(&baselines, &intervals)
		if err == nil && baselines == 1 && intervals == 1 {
			return
		}
		if err != nil {
			t.Logf("wait for Settlement rows: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(fmt.Sprintf("Settlement did not persist baseline %s and interval %s", firstEventID, secondEventID))
}
