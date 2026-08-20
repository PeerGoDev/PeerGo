package trafficconsumer_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/contracts/go/settlementtrafficv1"
	"github.com/peergo/peergo/services/core/internal/modules/traffic"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
	"github.com/peergo/peergo/services/core/internal/trafficconsumer"
)

// This gated test requires a disposable migrated Core database and a real
// JetStream server. It verifies the production ACK boundary: one final event
// produces one inbox fence, immutable entry and both totals before ACK. Example:
//
//	PEERGO_TEST_NATS_URL=nats://127.0.0.1:4222 \
//	PEERGO_TEST_CORE_DATABASE_URL=postgres://.../peergo_core_test?sslmode=disable \
//	go test ./internal/trafficconsumer -run Integration
func TestIntegrationProjectsFinalTrafficBeforeAcknowledging(t *testing.T) {
	natsURL := strings.TrimSpace(os.Getenv("PEERGO_TEST_NATS_URL"))
	databaseURL := strings.TrimSpace(os.Getenv("PEERGO_TEST_CORE_DATABASE_URL"))
	if natsURL == "" || databaseURL == "" {
		t.Skip("PEERGO_TEST_NATS_URL and PEERGO_TEST_CORE_DATABASE_URL are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		t.Fatal(err)
	}
	userID, torrentID := insertTrafficIntegrationFixture(t, ctx, pool)

	connection, js, err := trafficconsumer.Connect(trafficconsumer.ConnectionConfig{
		URLs: []string{natsURL}, ConnectTimeout: 2 * time.Second, ReconnectWait: 100 * time.Millisecond,
	}, "peergo-core-traffic-integration-test", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(connection.Close)
	suffix := strings.ToUpper(strings.ReplaceAll(uuid.NewString()[:12], "-", ""))
	stream := "PEERGO_CORE_TRAFFIC_IT_" + suffix
	subject := "peergo.core.traffic.it." + strings.ToLower(suffix)
	durable := "PEERGO_CORE_TRAFFIC_IT_" + suffix
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
	consumerConfig := jetstream.ConsumerConfig{
		Name: durable, Durable: durable, Description: "PeerGo Core traffic integration consumer",
		DeliverPolicy: jetstream.DeliverAllPolicy, AckPolicy: jetstream.AckExplicitPolicy,
		AckWait: 10 * time.Second, MaxDeliver: -1, FilterSubject: subject,
		ReplayPolicy: jetstream.ReplayInstantPolicy, MaxWaiting: 4,
		MaxAckPending: 1, MaxRequestBatch: 1, MaxRequestExpires: time.Second,
		Metadata: map[string]string{"peergo.owner": "core", "peergo.schema": settlementtrafficv1.SchemaVersion},
	}
	manager, err := trafficconsumer.NewNATSConsumerManager(js)
	if err != nil {
		t.Fatal(err)
	}
	if created, err := trafficconsumer.EnsureConsumer(ctx, manager, stream, consumerConfig); err != nil || !created {
		t.Fatalf("EnsureConsumer() = created %v, error %v", created, err)
	}

	event, payload := trafficIntegrationEvent(t, userID, torrentID)
	if ack, err := js.Publish(ctx, subject, payload, jetstream.WithMsgID(event.EventID), jetstream.WithExpectStream(stream)); err != nil || ack == nil || ack.Stream != stream {
		t.Fatalf("publish final traffic event: ack=%+v error=%v", ack, err)
	}
	source, err := trafficconsumer.OpenSource(ctx, js, trafficconsumer.BindingConfig{
		Stream: stream, Subject: subject, Durable: durable, FetchWait: 500 * time.Millisecond,
		MaximumProcessingTime: 2 * time.Second, MaximumAckTime: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	projector, err := traffic.NewPostgresRepository(pool, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := trafficconsumer.NewRunner(source, projector, trafficconsumer.RunnerConfig{
		Stream: stream, Subject: subject, Durable: durable, ProcessTimeout: 2 * time.Second, AckTimeout: time.Second, RetryDelay: 20 * time.Millisecond,
	}, time.Now, nil)
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancelRun := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- runner.Run(runCtx) }()
	waitForTrafficProjection(t, ctx, pool, event.EventID)
	cancelRun()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Core traffic projector did not stop")
	}

	consumer, err := js.Consumer(ctx, stream, durable)
	if err != nil {
		t.Fatal(err)
	}
	info, err := consumer.Info(ctx)
	if err != nil || info.AckFloor.Stream != 1 || info.NumAckPending != 0 {
		t.Fatalf("consumer ACK state = %+v, error %v", info, err)
	}
	var rawUploaded, rawDownloaded, creditedUploaded, chargedDownloaded, entries int64
	if err := pool.QueryRow(ctx, `
SELECT raw_uploaded, raw_downloaded, credited_uploaded, charged_downloaded, entry_count
FROM traffic.user_totals
WHERE user_id = $1::uuid`, userID).Scan(&rawUploaded, &rawDownloaded, &creditedUploaded, &chargedDownloaded, &entries); err != nil {
		t.Fatal(err)
	}
	if rawUploaded != 500 || rawDownloaded != 300 || creditedUploaded != 1000 || chargedDownloaded != 0 || entries != 1 {
		t.Fatalf("Core user totals = raw %d/%d credited %d/%d entries %d", rawUploaded, rawDownloaded, creditedUploaded, chargedDownloaded, entries)
	}
	overview, err := projector.Overview(ctx, userID, traffic.DefaultOverviewLimit)
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if overview.Totals.RawUploaded != 500 || overview.Totals.CreditedUploaded != 1000 ||
		len(overview.Entries) != 1 || overview.Entries[0].TorrentID != torrentID ||
		overview.Entries[0].TorrentTitle != "Core Traffic Integration" ||
		overview.Entries[0].Explanation.Status != traffic.ExplanationComplete ||
		len(overview.Entries[0].Explanation.Segments) != 1 {
		t.Fatalf("Overview() = %+v", overview)
	}
	if result, err := projector.Apply(ctx, payload, time.Now()); err != nil || !result.Duplicate || result.EventID.String() != event.EventID {
		t.Fatalf("duplicate Apply() = %+v, error %v", result, err)
	}
	conflicting := event
	conflicting.RawUploaded++
	conflictingPayload, err := settlementtrafficv1.Encode(conflicting)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projector.Apply(ctx, conflictingPayload, time.Now()); !errors.Is(err, traffic.ErrConflict) {
		t.Fatalf("conflicting Apply() error = %v", err)
	}
}

func insertTrafficIntegrationFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (uuid.UUID, int64) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	userID, credentialRef := uuid.New(), uuid.New()
	username := "traffic-it-" + userID.String()[:8]
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.users (
    id, credential_ref, username, display_name, status, email_verified_at, created_at, updated_at, password_changed_at
) VALUES ($1, $2, $3, 'Core 流量投影集成测试', 'active', $4, $4, $4, $4)`, userID, credentialRef, username, now); err != nil {
		t.Fatalf("insert integration user: %v", err)
	}
	categoryID := "traffic-it-" + uuid.NewString()[:8]
	if _, err := pool.Exec(ctx, `
INSERT INTO catalog.categories (id, name, display_order, enabled, created_at, updated_at)
VALUES ($1, 'Core 流量投影集成测试', 100000 + abs(hashtext($1)) % 900000, true, $2, $2)`, categoryID, now); err != nil {
		t.Fatalf("insert integration category: %v", err)
	}
	objectID := uuid.New()
	objectDigest := sha256.Sum256([]byte("traffic-object-" + objectID.String()))
	infoDigest := sha256.Sum256([]byte("traffic-info-" + objectID.String()))
	if _, err := pool.Exec(ctx, `
INSERT INTO torrents.torrent_objects (
    id, content_sha256, byte_length, parser_version, validation_profile,
    compatibility_flags, info_offset, info_length, created_at
) VALUES ($1, $2, 256, 'integration-v1', 'strict_upload', ARRAY[]::text[], 0, 128, $3)`, objectID, objectDigest[:], now); err != nil {
		t.Fatalf("insert integration torrent object: %v", err)
	}
	var torrentID int64
	if err := pool.QueryRow(ctx, `
INSERT INTO torrents.torrents (
    uploader_id, category_id, object_id, info_hash_v1,
    content_name, title, subtitle, total_size_bytes, payload_size_bytes,
    file_count, padding_file_count, piece_length_bytes, piece_count,
    state, version, submitted_at, state_changed_at, updated_at
) VALUES (
    $1, $2, $3, $4,
    'traffic-integration.bin', 'Core Traffic Integration', '', 4096, 4096,
    1, 0, 16384, 1,
    'pending_review', 1, $5, $5, $5
)
RETURNING id`, userID, categoryID, objectID, infoDigest[:20], now).Scan(&torrentID); err != nil {
		t.Fatalf("insert integration torrent: %v", err)
	}
	return userID, torrentID
}

func trafficIntegrationEvent(t *testing.T, userID uuid.UUID, torrentID int64) (settlementtrafficv1.Event, []byte) {
	t.Helper()
	eventID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Add(-time.Minute).Round(0)
	event := settlementtrafficv1.Event{
		SchemaVersion: settlementtrafficv1.SchemaVersion, EventID: eventID.String(), OccurredAt: start.Add(30 * time.Second),
		UserID: userID.String(), TorrentID: torrentID, IntervalStartsAt: start, IntervalEndsAt: start.Add(10 * time.Second),
		RawUploaded: 500, RawDownloaded: 300, CreditedUploaded: 1000, ChargedDownloaded: 0,
		SettlementSHA256: strings.Repeat("12", sha256.Size),
		Explanation: &settlementtrafficv1.Explanation{
			Status: settlementtrafficv1.ExplanationComplete, SegmentCount: 1,
			Segments: []settlementtrafficv1.Segment{{
				StartsAt: start, EndsAt: start.Add(10 * time.Second),
				RawUploaded: 500, RawDownloaded: 300, CreditedUploaded: 1000, ChargedDownloaded: 0,
			}},
		},
	}
	payload, err := settlementtrafficv1.Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	return event, payload
}

func waitForTrafficProjection(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var count int
		err := pool.QueryRow(ctx, `SELECT count(*) FROM traffic.settlement_inbox WHERE event_id = $1::uuid`, eventID).Scan(&count)
		if err == nil && count == 1 {
			return
		}
		if err != nil {
			t.Logf("wait for Core traffic projection: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(fmt.Sprintf("Core traffic projector did not persist event %s", eventID))
}
