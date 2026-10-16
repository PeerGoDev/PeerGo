package seedingevidence_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
	"github.com/peergo/peergo/services/settlement/internal/ingest"
	platformpostgres "github.com/peergo/peergo/services/settlement/internal/platform/postgres"
	"github.com/peergo/peergo/services/settlement/internal/seedingevidence"
)

func compactDatabase(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("PEERGO_TEST_TRACKER_DATABASE_URL"))
	if url == "" {
		t.Skip("PEERGO_TEST_TRACKER_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err = platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return ctx, pool
}

func TestIntegrationCompactActivityMatchesV2AndRollsBackWithIngest(t *testing.T) {
	ctx, pool := compactDatabase(t)
	hour := time.Now().UTC().Add(-96 * time.Hour).Truncate(time.Hour)
	clock := hour.Add(time.Hour + 46*time.Minute)
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	stream := "COMPACT_ACTIVITY_" + strings.ToUpper(suffix)
	subject := "peergo.compact.activity." + suffix
	snapshots := "COMPACT_FRAMES_" + strings.ToUpper(suffix)
	snapshotSubject := "peergo.compact.frames." + suffix
	if _, err := pool.Exec(ctx, `INSERT INTO settlement.activity_projection_profiles
  (source_stream,activates_at,max_interval_credit_seconds) VALUES($1,$2,2100)`, stream, hour); err != nil {
		t.Fatal(err)
	}
	writer, err := ingest.NewPostgresRepository(pool, stream, subject, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	user := uuid.New()
	hash := strings.Repeat("a7", 20)
	a := strings.Repeat("31", 32)
	b := strings.Repeat("32", 32)
	stale := strings.Repeat("33", 32)
	events := []trackerannouncev1.Event{
		seedingEvent(t, user, 8123, hash, a, hour.Add(-5*time.Minute), 0, 0),
		seedingEvent(t, user, 8123, hash, b, hour.Add(2*time.Minute), 0, 0),
		seedingEvent(t, user, 8123, hash, a, hour.Add(5*time.Minute), 10, 0),
		seedingEvent(t, user, 8123, hash, b, hour.Add(22*time.Minute), 0, 0),
		seedingEvent(t, user, 8123, hash, a, hour.Add(25*time.Minute), 20, 0),
		seedingEvent(t, user, 8123, hash, a, hour.Add(65*time.Minute), 30, 0),
		seedingEvent(t, user, 8123, hash, stale, hour.Add(time.Minute), 0, 0),
		seedingEvent(t, user, 8123, hash, stale, hour.Add(50*time.Minute), 99, 0),
		seedingEvent(t, uuid.New(), 8124, strings.Repeat("b7", 20), strings.Repeat("34", 32), clock, 0, 0),
	}
	for i, e := range events {
		ingestSeedingEvent(t, ctx, writer, stream, subject, uint64(i+1), e)
	}
	repo, err := seedingevidence.NewPostgresRepository(pool, seedingevidence.PostgresRepositoryConfig{
		AnnounceStream: stream, SnapshotStream: snapshots, SnapshotSubject: snapshotSubject, CompactSnapshots: true,
		MaxFutureSkew: 2 * time.Minute, MaximumSnapshotClosureDelay: 15 * time.Minute,
		ClosureDelay: 45 * time.Minute, MaxIntervalCredit: 35 * time.Minute,
	}, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	selected := snapshotChunk(t, "compact-"+suffix, 1, hour.Add(59*time.Minute), hash, 3, 1)
	fence := snapshotChunk(t, "compact-"+suffix, 2, hour.Add(61*time.Minute), hash, 4, 0)
	for i, c := range []trackerswarmv1.SnapshotChunk{selected, fence} {
		payload, err := trackerswarmv1.Encode(c)
		if err != nil {
			t.Fatal(err)
		}
		delivery := seedingevidence.SnapshotDelivery{Stream: snapshots, Subject: snapshotSubject, Sequence: uint64(i + 1), DeliveryCount: 1, Payload: payload}
		result, err := repo.ApplySnapshot(ctx, delivery)
		if err != nil || !result.Complete {
			t.Fatalf("snapshot %+v %v", result, err)
		}
		result, err = repo.ApplySnapshot(ctx, delivery)
		if err != nil || !result.Duplicate {
			t.Fatalf("snapshot replay %+v %v", result, err)
		}
	}
	var payloads, entries int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM ledger.seeding_snapshot_payloads WHERE snapshot_id=ANY($1::uuid[])", []uuid.UUID{uuid.MustParse(selected.SnapshotID), uuid.MustParse(fence.SnapshotID)}).Scan(&payloads); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM ledger.seeding_swarm_snapshot_entries WHERE snapshot_id=ANY($1::uuid[])", []uuid.UUID{uuid.MustParse(selected.SnapshotID), uuid.MustParse(fence.SnapshotID)}).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if payloads != 2 || entries != 0 {
		t.Fatalf("expected two compressed chunks and no per-entry copies: %d %d", payloads, entries)
	}
	result, err := repo.BuildHour(ctx, hour, clock)
	if err != nil || result.SchemaVersion != seedingevidence.SchemaVersionV3 || result.ItemCount != 1 || result.Reconciliation {
		t.Fatalf("v3 hour %+v %v", result, err)
	}
	var seconds, uploaded, count int64
	var proof bool
	var verified int
	if err = pool.QueryRow(ctx, `SELECT active_seconds,raw_uploaded,source_interval_count,
  active_ranges IS NOT NULL AND octet_length(source_commitment)=32
  FROM ledger.seeding_evidence_items WHERE window_start=$1 AND user_id=$2`, hour, user).Scan(&seconds, &uploaded, &count, &proof); err != nil {
		t.Fatal(err)
	}
	if seconds != 1500 || uploaded != 20 || count != 3 || !proof {
		t.Fatalf("compact values %d %d %d %t", seconds, uploaded, count, proof)
	}
	if err = pool.QueryRow(ctx, "SELECT shadow_verified_windows FROM settlement.activity_projection_profiles WHERE source_stream=$1", stream).Scan(&verified); err != nil || verified != 1 {
		t.Fatalf("shadow checks=%d %v", verified, err)
	}
	duplicate, err := repo.BuildHour(ctx, hour, clock)
	if err != nil || !duplicate.Duplicate || duplicate.EvidenceSHA256 != result.EvidenceSHA256 {
		t.Fatalf("duplicate %+v %v", duplicate, err)
	}
	// This fixture consumes its own transport record; package integration
	// tests share a disposable database and the original dispatcher test claims
	// the oldest available message globally.
	if _, err = pool.Exec(ctx, "UPDATE settlement.seeding_evidence_outbox SET published_at=$2 WHERE window_start=$1", hour, clock.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	// A committed late source must preserve the sealed item and create drift
	// evidence. Its mutable activity state must not rewrite a past reward.
	late := seedingEvent(t, user, 8123, hash, b, hour.Add(29*time.Minute), 0, 0)
	ingestSeedingEvent(t, ctx, writer, stream, subject, 10, late)
	if _, err = repo.BuildHour(ctx, hour, clock); !errors.Is(err, seedingevidence.ErrEvidenceDrift) {
		t.Fatalf("late input not detected: %v", err)
	}
	valid := seedingEvent(t, user, 8123, hash, b, hour.Add(30*time.Minute), 0, 0)
	payload, err := trackerannouncev1.Encode(valid)
	if err != nil {
		t.Fatal(err)
	}
	_, err = writer.ProcessBatch(ctx, []ingest.Delivery{
		{Stream: stream, Subject: subject, Sequence: 11, DeliveryCount: 1, Payload: payload},
		{Stream: stream, Subject: subject, Sequence: 12, DeliveryCount: 1, Payload: []byte("{")},
	})
	if err == nil {
		t.Fatal("invalid batch committed")
	}
	var sequence, sourceCount int64
	if err = pool.QueryRow(ctx, "SELECT last_source_sequence FROM settlement.ingest_stream_cursors WHERE source_stream=$1", stream).Scan(&sequence); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT source_count FROM settlement.seeding_activity_hours WHERE source_stream=$1 AND window_start=$2 AND user_id=$3", stream, hour, user).Scan(&sourceCount); err != nil {
		t.Fatal(err)
	}
	if sequence != 10 || sourceCount != 4 {
		t.Fatalf("rollback advanced cursor/activity: %d %d", sequence, sourceCount)
	}
}

func TestIntegrationCoverageGapDoesNotBlockLaterCompleteHour(t *testing.T) {
	ctx, pool := compactDatabase(t)
	hour := time.Now().UTC().Add(-120 * time.Hour).Truncate(time.Hour)
	clock := hour.Add(2*time.Hour + 46*time.Minute)
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	stream := "GAP_ACTIVITY_" + strings.ToUpper(suffix)
	subject := "peergo.gap.activity." + suffix
	snapshots := "GAP_FRAMES_" + strings.ToUpper(suffix)
	snapshotSubject := "peergo.gap.frames." + suffix
	repo, err := seedingevidence.NewPostgresRepository(pool, seedingevidence.PostgresRepositoryConfig{
		AnnounceStream: stream, SnapshotStream: snapshots, SnapshotSubject: snapshotSubject, CompactSnapshots: true,
		MaxFutureSkew: 2 * time.Minute, MaximumSnapshotClosureDelay: 15 * time.Minute,
		ClosureDelay: 45 * time.Minute, MaxIntervalCredit: 35 * time.Minute,
	}, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO settlement.seeding_window_scan(source_stream,next_window) VALUES($1,$2)", stream, hour); err != nil {
		t.Fatal(err)
	}
	writer, err := ingest.NewPostgresRepository(pool, stream, subject, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	user := uuid.New()
	hash := strings.Repeat("c7", 20)
	session := strings.Repeat("35", 32)
	events := []trackerannouncev1.Event{
		seedingEvent(t, user, 8125, hash, session, hour.Add(5*time.Minute), 0, 0),
		seedingEvent(t, user, 8125, hash, session, hour.Add(15*time.Minute), 0, 0),
		seedingEvent(t, user, 8125, hash, session, hour.Add(70*time.Minute), 0, 0),
		seedingEvent(t, user, 8125, hash, session, hour.Add(80*time.Minute), 0, 0),
		seedingEvent(t, uuid.New(), 8126, strings.Repeat("d7", 20), strings.Repeat("36", 32), clock, 0, 0),
	}
	for i, e := range events {
		ingestSeedingEvent(t, ctx, writer, stream, subject, uint64(i+1), e)
	}
	chunks := []trackerswarmv1.SnapshotChunk{
		snapshotChunk(t, "gap-"+suffix, 1, hour.Add(93*time.Minute), hash, 3, 0),
		snapshotChunk(t, "gap-"+suffix, 2, hour.Add(121*time.Minute), hash, 3, 0),
	}
	for i, c := range chunks {
		payload, _ := trackerswarmv1.Encode(c)
		if _, err = repo.ApplySnapshot(ctx, seedingevidence.SnapshotDelivery{Stream: snapshots, Subject: snapshotSubject, Sequence: uint64(i + 1), DeliveryCount: 1, Payload: payload}); err != nil {
			t.Fatal(err)
		}
	}
	worker, err := seedingevidence.NewWorker(repo, seedingevidence.WorkerConfig{InitialWindowStart: hour, ClosureDelay: 45 * time.Minute, IdleInterval: time.Second}, func() time.Time { return clock }, nil)
	if err != nil {
		t.Fatal(err)
	}
	done, err := worker.RunOnce(ctx)
	if err != nil || done {
		t.Fatalf("missing hour should defer: %t %v", done, err)
	}
	done, err = worker.RunOnce(ctx)
	if err != nil || !done {
		t.Fatalf("later complete hour was blocked: %t %v", done, err)
	}
	var missing, complete, retry int
	if err = pool.QueryRow(ctx, "SELECT count(*) FILTER(WHERE window_start=$1),count(*) FILTER(WHERE window_start=$2) FROM ledger.seeding_evidence_windows WHERE window_start=ANY($3::timestamptz[])", hour, hour.Add(time.Hour), []time.Time{hour, hour.Add(time.Hour)}).Scan(&missing, &complete); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM settlement.seeding_window_retries WHERE source_stream=$1 AND window_start=$2", stream, hour).Scan(&retry); err != nil {
		t.Fatal(err)
	}
	if missing != 0 || complete != 1 || retry != 1 {
		t.Fatalf("gap represented as completed or later hour lost: %d %d %d", missing, complete, retry)
	}
	var oldClosed, newClosed bool
	if err = pool.QueryRow(ctx, "SELECT ledger.seeding_interval_is_closed($1,$2),ledger.seeding_interval_is_closed($3,$4)", hour.Add(5*time.Minute), hour.Add(15*time.Minute), hour.Add(70*time.Minute), hour.Add(80*time.Minute)).Scan(&oldClosed, &newClosed); err != nil {
		t.Fatal(err)
	}
	if oldClosed || !newClosed {
		t.Fatalf("unsafe coverage across gap: %t %t", oldClosed, newClosed)
	}
	var contiguous time.Time
	if err = pool.QueryRow(ctx, "SELECT ledger.seeding_closed_through()").Scan(&contiguous); err != nil {
		t.Fatal(err)
	}
	if contiguous.After(hour) {
		t.Fatalf("contiguous watermark skipped earliest pending hour: %s > %s", contiguous, hour)
	}

	if _, err = pool.Exec(ctx, "UPDATE settlement.seeding_evidence_outbox SET published_at=$2 WHERE window_start=$1", hour.Add(time.Hour), clock.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	// Real delayed frames from another route can repair coverage, but a recovered
	// historical hour must go to reconciliation instead of blind later credit.
	prior := snapshotChunk(t, "gap-"+suffix, 1, hour.Add(59*time.Minute), hash, 3, 0)
	prior.RoutingEpoch = 2
	fence := snapshotChunk(t, "gap-"+suffix, 2, hour.Add(61*time.Minute), hash, 3, 0)
	fence.RoutingEpoch = 2
	for i, c := range []trackerswarmv1.SnapshotChunk{prior, fence} {
		payload, _ := trackerswarmv1.Encode(c)
		if _, err = repo.ApplySnapshot(ctx, seedingevidence.SnapshotDelivery{Stream: snapshots, Subject: snapshotSubject, Sequence: uint64(i + 3), DeliveryCount: 1, Payload: payload}); err != nil {
			t.Fatal(err)
		}
	}
	repaired, err := repo.BuildHour(ctx, hour, clock.Add(10*time.Minute))
	if err != nil || !repaired.Reconciliation {
		t.Fatalf("historical repair %+v %v", repaired, err)
	}
	var recon, outbox int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM settlement.seeding_reconciliation_work WHERE window_start=$1 AND resolved_at IS NULL", hour).Scan(&recon); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM settlement.seeding_evidence_outbox WHERE window_start=$1", hour).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if recon != 1 || outbox != 0 {
		t.Fatalf("recovered gap auto-published rewards: recon=%d outbox=%d", recon, outbox)
	}
	replayed, err := repo.BuildHour(ctx, hour, clock.Add(11*time.Minute))
	if err != nil || !replayed.Duplicate || !replayed.Reconciliation {
		t.Fatalf("historical duplicate %+v %v", replayed, err)
	}
}
