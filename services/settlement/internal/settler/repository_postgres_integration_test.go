package settler_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/contracts/go/settlementtrafficv1"
	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/services/settlement/internal/ingest"
	"github.com/peergo/peergo/services/settlement/internal/jetstreamconsumer"
	platformpostgres "github.com/peergo/peergo/services/settlement/internal/platform/postgres"
	"github.com/peergo/peergo/services/settlement/internal/policy"
	"github.com/peergo/peergo/services/settlement/internal/settler"
	"github.com/peergo/peergo/services/settlement/internal/timeline"
	"github.com/peergo/peergo/services/settlement/internal/trafficoutbox"
)

// This gated test needs a disposable, migrated Tracker Ledger database. It
// establishes the boundary that unit tests cannot: raw events may commit with
// no coverage, but credited/charged evidence and the Core outbox only appear
// after an immutable snapshot is explicitly appended. Example:
//
//	PEERGO_TEST_TRACKER_DATABASE_URL=postgres://.../peergo_tracker_test?sslmode=disable \
//	go test ./internal/settler -run Integration
func TestIntegrationFinalSettlementWaitsForTimelineAndAppendsOutbox(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("PEERGO_TEST_TRACKER_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("PEERGO_TEST_TRACKER_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		t.Fatal(err)
	}

	baseTime := time.Now().UTC().Add(-time.Minute).Round(0)
	userID := uuid.New()
	stream := "PEERGO_SETTLEMENT_POLICY_IT_" + strings.ToUpper(strings.ReplaceAll(uuid.NewString()[:8], "-", ""))
	subject := "peergo.settlement.policy.it." + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	ingestRepository, err := ingest.NewPostgresRepository(pool, stream, subject, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	baseline, interval := policyIntegrationEvents(t, baseTime, userID)
	for sequence, event := range []trackerannouncev1.Event{baseline, interval} {
		payload, encodeErr := trackerannouncev1.Encode(event)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		if _, processErr := ingestRepository.Process(ctx, ingest.Delivery{
			Stream: stream, Subject: subject, Sequence: uint64(sequence + 1), DeliveryCount: 1, Payload: payload,
		}); processErr != nil {
			t.Fatalf("ingest event %d: %v", sequence+1, processErr)
		}
	}

	repository, err := settler.NewPostgresRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(time.Minute).Round(0)
	worker, err := settler.NewWorker(repository, settler.WorkerConfig{
		LeaseDuration: time.Minute, IdleInterval: 100 * time.Millisecond, RetryBase: 100 * time.Millisecond,
	}, func() time.Time { return now }, nil)
	if err != nil {
		t.Fatal(err)
	}
	processed, err := worker.RunOnce(ctx)
	if err != nil || !processed {
		t.Fatalf("RunOnce(without coverage) = processed %v, error %v", processed, err)
	}
	var errorCode string
	var settlementCount int
	if err := pool.QueryRow(ctx, `
SELECT work.last_error_code, (SELECT count(*) FROM ledger.traffic_settlements WHERE settlement_id = work.interval_event_id)
FROM settlement.policy_work AS work
WHERE work.interval_event_id = $1::uuid`, interval.EventID).Scan(&errorCode, &settlementCount); err != nil {
		t.Fatal(err)
	}
	if errorCode != "policy_coverage_pending" || settlementCount != 0 {
		t.Fatalf("missing coverage result = code %q settlements %d", errorCode, settlementCount)
	}

	policySnapshot := policyIntegrationSnapshot(t, baseTime)
	scopeUserID := userID
	revision := timeline.Revision{
		ID: uuid.New(), Scope: timeline.Scope{UserID: &scopeUserID},
		EffectiveAt: baseTime.Add(-time.Hour), Snapshot: policySnapshot,
	}
	if created, appendErr := repository.AppendRevision(ctx, revision, now); appendErr != nil || !created {
		t.Fatalf("AppendRevision() = created %v, error %v", created, appendErr)
	}
	now = now.Add(time.Second)
	processed, err = worker.RunOnce(ctx)
	if err != nil || !processed {
		t.Fatalf("RunOnce(with coverage) = processed %v, error %v", processed, err)
	}

	var rawUploaded, rawDownloaded, creditedUploaded, chargedDownloaded int64
	var segmentCount int
	var payload string
	if err := pool.QueryRow(ctx, `
SELECT
    settlement.raw_uploaded,
    settlement.raw_downloaded,
    settlement.credited_uploaded,
    settlement.charged_downloaded,
    (SELECT count(*) FROM ledger.traffic_settlement_segments WHERE settlement_id = settlement.settlement_id),
    (SELECT payload_json FROM settlement.traffic_outbox WHERE settlement_id = settlement.settlement_id)
FROM ledger.traffic_settlements AS settlement
WHERE settlement.settlement_id = $1::uuid`, interval.EventID).Scan(
		&rawUploaded, &rawDownloaded, &creditedUploaded, &chargedDownloaded, &segmentCount, &payload,
	); err != nil {
		t.Fatal(err)
	}
	if rawUploaded != 500 || rawDownloaded != 300 || creditedUploaded != 1000 || chargedDownloaded != 0 || segmentCount != 1 {
		t.Fatalf("final settlement = raw %d/%d credited %d/%d segments %d", rawUploaded, rawDownloaded, creditedUploaded, chargedDownloaded, segmentCount)
	}
	event, err := settlementtrafficv1.Decode([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if event.EventID != interval.EventID || event.UserID != userID.String() || event.TorrentID != 42 ||
		event.RawUploaded != 500 || event.RawDownloaded != 300 || event.CreditedUploaded != 1000 || event.ChargedDownloaded != 0 ||
		event.Explanation == nil || event.Explanation.Status != settlementtrafficv1.ExplanationComplete ||
		event.Explanation.SegmentCount != 1 || len(event.Explanation.Segments) != 1 {
		t.Fatalf("outbox event = %+v", event)
	}
	// An operator retry of the exact immutable revision remains idempotent even
	// after the historical-rewrite trigger has begun protecting settled rows.
	// A different revision would still reach the trigger or conflict check.
	if created, appendErr := repository.AppendRevision(ctx, revision, now.Add(time.Second)); appendErr != nil || created {
		t.Fatalf("duplicate AppendRevision() after settlement = created %v, error %v", created, appendErr)
	}
	if _, err := pool.Exec(ctx, `UPDATE ledger.traffic_settlements SET credited_uploaded = credited_uploaded + 1 WHERE settlement_id = $1::uuid`, interval.EventID); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("immutable final settlement update error = %v", err)
	}
	if natsURL := strings.TrimSpace(os.Getenv("PEERGO_TEST_NATS_URL")); natsURL != "" {
		dispatchIntegrationOutbox(t, ctx, pool, natsURL, interval.EventID)
	}
}

func dispatchIntegrationOutbox(t *testing.T, ctx context.Context, pool *pgxpool.Pool, natsURL, eventID string) {
	t.Helper()
	connection, js, err := jetstreamconsumer.Connect(jetstreamconsumer.ConnectionConfig{
		URLs: []string{natsURL}, ConnectTimeout: 2 * time.Second, ReconnectWait: 100 * time.Millisecond,
	}, "peergo-settlement-policy-integration-test", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(connection.Close)
	suffix := strings.ToUpper(strings.ReplaceAll(uuid.NewString()[:12], "-", ""))
	stream := "PEERGO_SETTLEMENT_OUTBOX_IT_" + suffix
	subject := "peergo.settlement.outbox.it." + strings.ToLower(suffix)
	durable := "PEERGO_SETTLEMENT_OUTBOX_IT_" + suffix
	if _, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name: stream, Subjects: []string{subject}, Retention: jetstream.LimitsPolicy,
		MaxConsumers: 1, MaxMsgs: -1, MaxBytes: 1 << 20, Discard: jetstream.DiscardNew,
		MaxAge: time.Minute, MaxMsgsPerSubject: -1, MaxMsgSize: settlementtrafficv1.MaxEventBytes,
		Storage: jetstream.MemoryStorage, Replicas: 1, Duplicates: time.Minute,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := js.DeleteStream(cleanupCtx, stream); err != nil {
			t.Errorf("delete integration stream: %v", err)
		}
	})
	consumer, err := js.CreateConsumer(ctx, stream, jetstream.ConsumerConfig{
		Name: durable, Durable: durable, DeliverPolicy: jetstream.DeliverAllPolicy, AckPolicy: jetstream.AckExplicitPolicy,
		AckWait: 10 * time.Second, MaxDeliver: -1, FilterSubject: subject, ReplayPolicy: jetstream.ReplayInstantPolicy,
		MaxWaiting: 4, MaxAckPending: 1, MaxRequestBatch: 1, MaxRequestExpires: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	repository, err := trafficoutbox.NewPostgresRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := trafficoutbox.NewJetStreamPublisher(js, stream, subject)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := trafficoutbox.NewDispatcher(repository, publisher, trafficoutbox.DispatcherConfig{
		LeaseDuration: time.Minute, IdleInterval: 100 * time.Millisecond, RetryBase: 100 * time.Millisecond,
		// The policy worker above uses a deterministic future clock to make the
		// coverage retry immediate. Keep the dispatcher clock ahead of that final
		// available_at so this test validates publishing rather than wall-clock wait.
	}, func() time.Time { return time.Now().UTC().Add(2 * time.Minute) }, nil)
	if err != nil {
		t.Fatal(err)
	}
	published := false
	for attempt := 0; attempt < 16; attempt++ {
		processed, err := dispatcher.RunOnce(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !processed {
			break
		}
		var publishedAt *time.Time
		if err := pool.QueryRow(ctx, `SELECT published_at FROM settlement.traffic_outbox WHERE event_id = $1::uuid`, eventID).Scan(&publishedAt); err != nil {
			t.Fatal(err)
		}
		if publishedAt != nil {
			published = true
			break
		}
	}
	if !published {
		t.Fatalf("final outbox %s was not marked published", eventID)
	}
	found := false
	for attempt := 0; attempt < 16; attempt++ {
		fetchCtx, fetchCancel := context.WithTimeout(ctx, 500*time.Millisecond)
		message, err := consumer.Next(jetstream.FetchContext(fetchCtx))
		fetchCancel()
		if err != nil {
			t.Fatal(err)
		}
		event, err := settlementtrafficv1.Decode(message.Data())
		if err != nil {
			t.Fatal(err)
		}
		if err := message.DoubleAck(ctx); err != nil {
			t.Fatal(err)
		}
		if event.EventID == eventID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("did not receive dispatched final outbox %s", eventID)
	}
}

func policyIntegrationEvents(t *testing.T, baseTime time.Time, userID uuid.UUID) (trackerannouncev1.Event, trackerannouncev1.Event) {
	t.Helper()
	baselineID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	intervalID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	baseline := trackerannouncev1.Event{
		SchemaVersion: trackerannouncev1.SchemaVersion, EventID: baselineID.String(), ReceivedAt: baseTime,
		UserID: userID.String(), TorrentID: 42, InfoHashV1: strings.Repeat("ab", 20), SessionToken: strings.Repeat("cd", 32),
		AddressFamily: 4, Event: "started", Uploaded: 100, Downloaded: 200, Left: 1000, CredentialVersion: 1,
		TorrentControlSequence: 7, SubjectControlSequence: 9,
	}
	interval := baseline
	interval.EventID = intervalID.String()
	interval.ReceivedAt = baseTime.Add(10 * time.Second)
	interval.Event = "completed"
	interval.CompletionID = strings.Repeat("ef", 32)
	interval.Uploaded = 600
	interval.Downloaded = 500
	interval.Left = 0
	return baseline, interval
}

func policyIntegrationSnapshot(t *testing.T, baseTime time.Time) policy.Snapshot {
	t.Helper()
	promotion, err := policy.ResolvePromotion(policy.ProfilePeerGoV1, baseTime, []policy.PromotionRule{{
		Rule:  policy.RuleRef{Source: policy.SourceTorrentPromotion, ID: "integration-double-free", Version: 1},
		Scope: policy.ScopeTorrent, Promotion: policy.PromotionDoubleUploadFree,
		Window: policy.Window{StartsAt: baseTime.Add(-time.Hour)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return policy.Snapshot{
		Revision: policy.RuleRef{Source: policy.SourcePolicyRevision, ID: "integration-policy-v1", Version: 1},
		Profile:  policy.ProfilePeerGoV1, Promotion: promotion,
	}
}
