package storagecleanup

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
	"github.com/peergo/peergo/services/settlement/internal/generated/ledgerdb"
	"github.com/peergo/peergo/services/settlement/internal/ingest"
	platformpostgres "github.com/peergo/peergo/services/settlement/internal/platform/postgres"
	"github.com/peergo/peergo/services/settlement/internal/policy"
	"github.com/peergo/peergo/services/settlement/internal/seedingevidence"
	"github.com/peergo/peergo/services/settlement/internal/settler"
	"github.com/peergo/peergo/services/settlement/internal/timeline"
	"github.com/peergo/peergo/services/settlement/internal/trafficoutbox"
)

// Immutable evidence requires a freshly migrated, disposable Tracker database.
func compactTestDatabase(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("PEERGO_TEST_TRACKER_DATABASE_URL"))
	if url == "" {
		t.Skip("PEERGO_TEST_TRACKER_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return ctx, pool
}

func TestIntegrationSnapshotThinningPreservesUnclosedBoundaries(t *testing.T) {
	ctx, pool := compactTestDatabase(t)
	q := ledgerdb.New(pool)
	hour := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Hour)
	source := "compact-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	// Direct inserts create canonical complete frames without a closed reward
	// window. Thinning must work even when an older coverage gap blocks closure.
	insert := func(route int64, seq int64, at time.Time, count int, stream string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO ledger.seeding_swarm_snapshots
 (snapshot_id,source_id,routing_epoch,snapshot_sequence,observed_at,chunk_count,received_chunk_count,status,created_at,completed_at)
 VALUES ($1,$2,$3,$4,$5,1,1,'complete',$5,$5)`, id, source, route, seq, at); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO settlement.seeding_swarm_snapshot_inbox
 (event_id,snapshot_id,payload_sha256,source_stream,source_subject,source_sequence,delivery_count,observed_at,received_at)
 VALUES ($1,$2,$3,$4,'peergo.compact.snapshot',$5,1,$6,$6)`, uuid.New(), id, make([]byte, 32), stream, seq, at); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < count; i++ {
			if _, err := pool.Exec(ctx, `INSERT INTO ledger.seeding_swarm_snapshot_entries(snapshot_id,info_hash_v1,seeders,leechers)
 VALUES ($1,decode($2,'hex'),3,1)`, id, fmt.Sprintf("%040x", i)); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	stream := "COMPACT_" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))
	boundary := insert(1, 1, hour, 1, stream)
	interior := insert(1, 2, hour.Add(time.Minute), 205, stream)
	last := insert(1, 3, hour.Add(59*time.Minute), 1, stream)
	fence := insert(1, 4, hour.Add(time.Hour+time.Minute), 1, stream)
	otherRoute := insert(2, 1, hour.Add(20*time.Minute), 1, stream+"_B")
	youngHour := time.Now().UTC().Add(-time.Hour).Truncate(time.Hour)
	insert(3, 1, youngHour.Add(time.Minute), 1, stream+"_C")
	young := insert(3, 2, youngHour.Add(2*time.Minute), 1, stream+"_C")
	insert(3, 3, youngHour.Add(3*time.Minute), 1, stream+"_C")
	// A frame can be interior for its route while still being the first
	// after-hour fence delivered to a newly configured consumer stream.
	insert(4, 1, hour.Add(2*time.Minute), 1, stream+"_D")
	firstInStream := insert(4, 2, hour.Add(3*time.Minute), 1, stream+"_E")
	insert(4, 3, hour.Add(4*time.Minute), 1, stream+"_D")
	for _, id := range []uuid.UUID{boundary, last, fence, otherRoute, firstInStream} {
		var redundant bool
		if err := pool.QueryRow(ctx, `SELECT ledger.is_redundant_hourly_snapshot($1)`, id).Scan(&redundant); err != nil || redundant {
			t.Fatalf("boundary %s considered redundant=%t: %v", id, redundant, err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM ledger.seeding_swarm_snapshot_entries WHERE snapshot_id=$1`, id); err == nil {
			t.Fatalf("unclosed boundary %s could be deleted", id)
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM ledger.seeding_swarm_snapshot_entries WHERE snapshot_id=$1`, young); err == nil {
		t.Fatal("young interior frame could be deleted")
	}
	var selectedBefore, fenceBefore uuid.UUID
	selectBoundary := func() (uuid.UUID, uuid.UUID) {
		t.Helper()
		var selected, after uuid.UUID
		if err := pool.QueryRow(ctx, `SELECT snapshot_id FROM ledger.seeding_swarm_snapshots
 WHERE source_id=$1 AND routing_epoch=1 AND status='complete' AND observed_at<=$2
 ORDER BY snapshot_sequence DESC LIMIT 1`, source, hour.Add(time.Hour)).Scan(&selected); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT snapshot_id FROM ledger.seeding_swarm_snapshots
 WHERE source_id=$1 AND routing_epoch=1 AND status='complete' AND observed_at>=$2
 ORDER BY observed_at,snapshot_sequence LIMIT 1`, source, hour.Add(time.Hour)).Scan(&after); err != nil {
			t.Fatal(err)
		}
		return selected, after
	}
	selectedBefore, fenceBefore = selectBoundary()
	for pass, want := range []int64{100, 100, 5, 0} {
		rows, err := q.StorageCleanupDeleteSnapshotEntries(ctx, ledgerdb.StorageCleanupDeleteSnapshotEntriesParams{DetailBefore: cleanupTimestamp(time.Now().Add(-12 * time.Hour)), BatchSize: 100})
		if err != nil || rows != want {
			t.Fatalf("snapshot pass %d rows=%d want=%d: %v", pass, rows, want, err)
		}
		var finished bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM settlement.snapshot_entry_gc_progress WHERE snapshot_id=$1)`, interior).Scan(&finished); err != nil {
			t.Fatal(err)
		}
		if finished != (pass >= 2) {
			t.Fatalf("partial frame marked finished on pass %d: %t", pass, finished)
		}
	}
	if selected, after := selectBoundary(); selected != selectedBefore || after != fenceBefore || selected != last || after != fence {
		t.Fatalf("thinning changed selected/fence: %s/%s -> %s/%s", selectedBefore, fenceBefore, selected, after)
	}
	var retained int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ledger.seeding_swarm_snapshot_entries WHERE snapshot_id=ANY($1::uuid[])`, []uuid.UUID{boundary, last, fence, otherRoute, young, firstInStream}).Scan(&retained); err != nil || retained != 6 {
		t.Fatalf("retained boundary entries=%d: %v", retained, err)
	}
}

func TestIntegrationRawCleanupRetriesDurablyAndRollsBack(t *testing.T) {
	ctx, pool := compactTestDatabase(t)
	q := ledgerdb.New(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	old := now.Add(-72 * time.Hour)
	stream := "COMPACT_RAW_" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))
	subject := "peergo.compact.raw"
	repo, err := ingest.NewPostgresRepository(pool, stream, subject, func() time.Time { return old.Add(time.Hour) })
	if err != nil {
		t.Fatal(err)
	}
	var ids []uuid.UUID
	for i := 0; i < 3; i++ {
		event := trackerannouncev1.Event{SchemaVersion: trackerannouncev1.SchemaVersion, EventID: uuid.Must(uuid.NewV7()).String(), ReceivedAt: old,
			UserID: uuid.NewString(), TorrentID: 42, InfoHashV1: strings.Repeat("ab", 20), SessionToken: strings.Repeat("cd", 32),
			AddressFamily: 4, Event: "started", Left: 1000, CredentialVersion: 1, TorrentControlSequence: 1, SubjectControlSequence: 1}
		for j := 0; j < 2; j++ {
			if j == 1 {
				event.EventID = uuid.Must(uuid.NewV7()).String()
				event.Event = ""
				event.ReceivedAt = old.Add(10 * time.Second)
			}
			payload, err := trackerannouncev1.Encode(event)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repo.Process(ctx, ingest.Delivery{Stream: stream, Subject: subject, Sequence: uint64(i*2 + j + 1), DeliveryCount: 1, Payload: payload}); err != nil {
				t.Fatal(err)
			}
		}
		ids = append(ids, uuid.MustParse(event.EventID))
	}
	if _, err := pool.Exec(ctx, `UPDATE settlement.policy_work SET settled_at=$2::timestamptz WHERE interval_event_id=ANY($1::uuid[])`, ids, old.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// Enqueue and dependency deletion must commit atomically. A failed cleanup
	// transaction must retain both the dependency and its original raw interval.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM settlement.policy_work WHERE interval_event_id=ANY($1::uuid[])`, ids); err != nil {
		t.Fatal(err)
	}
	var queued int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM settlement.raw_interval_gc WHERE event_id=ANY($1::uuid[])`, ids).Scan(&queued); err != nil || queued != 3 {
		t.Fatalf("transactional jobs=%d: %v", queued, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM settlement.raw_interval_gc WHERE event_id=ANY($1::uuid[])`, ids).Scan(&queued); err != nil || queued != 0 {
		t.Fatalf("rolled back jobs=%d: %v", queued, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM settlement.policy_work WHERE interval_event_id=ANY($1::uuid[])`, ids); err != nil {
		t.Fatal(err)
	}
	cleanup, err := NewPostgresRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	// An intentionally stricter cutoff defers, rather than loses, the jobs.
	if n, err := cleanup.cleanupQueuedRawIntervals(ctx, cleanupTimestamp(old.Add(-time.Hour)), 2); err != nil || n != 0 {
		t.Fatalf("deferred rows=%d: %v", n, err)
	}
	var deferred int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM settlement.raw_interval_gc WHERE event_id=ANY($1::uuid[]) AND retry_after>statement_timestamp()`, ids).Scan(&deferred); err != nil || deferred != 2 {
		t.Fatalf("deferred jobs=%d: %v", deferred, err)
	}
	// A fresh repository simulates a process restart. Ready jobs beyond the
	// previous batch and deferred jobs both remain reachable.
	cleanup, err = NewPostgresRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := cleanup.cleanupQueuedRawIntervals(ctx, cleanupTimestamp(now.Add(-12*time.Hour)), 2); err != nil || n != 1 {
		t.Fatalf("remaining ready rows=%d: %v", n, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE settlement.raw_interval_gc SET retry_after=statement_timestamp() WHERE event_id=ANY($1::uuid[])`, ids); err != nil {
		t.Fatal(err)
	}
	if n, err := drainDetailBacklog(ctx, func(ctx context.Context) (int64, error) {
		return cleanup.cleanupQueuedRawIntervals(ctx, cleanupTimestamp(now.Add(-12*time.Hour)), 1)
	}); err != nil || n != 2 {
		t.Fatalf("retried rows=%d: %v", n, err)
	}
	if due, err := q.StorageCleanupListDueRawIntervals(ctx, 100); err != nil || len(due) != 0 {
		t.Fatalf("finished jobs=%v: %v", due, err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ledger.raw_session_intervals WHERE event_id=ANY($1::uuid[])`, ids).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("remaining raw=%d: %v", remaining, err)
	}
}

func TestIntegrationTrafficCleanupBackfillAndDelayedPublication(t *testing.T) {
	ctx, pool := compactTestDatabase(t)
	q := ledgerdb.New(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	old := now.Add(-72 * time.Hour)
	stream := "COMPACT_TRAFFIC_" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))
	subject := "peergo.compact.traffic"
	userID := uuid.New()
	ingester, err := ingest.NewPostgresRepository(pool, stream, subject, func() time.Time { return old.Add(time.Hour) })
	if err != nil {
		t.Fatal(err)
	}
	accounting, err := settler.NewPostgresRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	promotion, err := policy.ResolvePromotion(policy.ProfilePeerGoV1, old, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := policy.Snapshot{Revision: policy.RuleRef{Source: policy.SourcePolicyRevision, ID: "compact-integration", Version: 1}, Profile: policy.ProfilePeerGoV1, Promotion: promotion}
	if _, err := accounting.AppendRevision(ctx, timeline.Revision{ID: uuid.New(), Scope: timeline.Scope{UserID: &userID}, EffectiveAt: old.Add(-time.Hour), Snapshot: snapshot}, old); err != nil {
		t.Fatal(err)
	}
	event := trackerannouncev1.Event{SchemaVersion: trackerannouncev1.SchemaVersion, UserID: userID.String(), TorrentID: 42,
		InfoHashV1: strings.Repeat("ab", 20), SessionToken: strings.Repeat("cd", 32), AddressFamily: 4, Left: 1000,
		CredentialVersion: 1, TorrentControlSequence: 1, SubjectControlSequence: 1}
	var ids []uuid.UUID
	for i := 0; i < 4; i++ {
		event.EventID = uuid.Must(uuid.NewV7()).String()
		event.ReceivedAt = old.Add(time.Duration(i) * 10 * time.Second)
		event.Uploaded, event.Downloaded = int64(i), int64(i*2)
		payload, err := trackerannouncev1.Encode(event)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ingester.Process(ctx, ingest.Delivery{Stream: stream, Subject: subject, Sequence: uint64(i + 1), DeliveryCount: 1, Payload: payload}); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			continue
		}
		id := uuid.MustParse(event.EventID)
		pending := settler.PendingWork{IntervalEventID: id, LeaseToken: uuid.New()}
		if err := pool.QueryRow(ctx, `UPDATE settlement.policy_work SET lease_token=$2,lease_until=$3,attempts=attempts+1
 WHERE interval_event_id=$1 RETURNING attempts`, id, pending.LeaseToken, old.Add(2*time.Hour)).Scan(&pending.Attempts); err != nil {
			t.Fatal(err)
		}
		if err := accounting.Settle(ctx, pending, old.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	transport, err := trafficoutbox.NewPostgresRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	publish := func() {
		t.Helper()
		pending, found, err := transport.ClaimNext(ctx, old.Add(2*time.Hour), time.Minute)
		if err != nil || !found {
			t.Fatalf("claim publication found=%t: %v", found, err)
		}
		if err := transport.MarkPublished(ctx, pending, old.Add(2*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	publish()
	if n, err := q.StorageCleanupDeleteTrafficOutbox(ctx, ledgerdb.StorageCleanupDeleteTrafficOutboxParams{TerminalBefore: cleanupTimestamp(now.Add(-3 * time.Hour)), BatchSize: 100}); err != nil || n != 1 {
		t.Fatalf("published transport rows=%d: %v", n, err)
	}
	// Simulate a detail whose outbox was removed before this migration. The
	// bounded keyset scan must recover it once, while skipping live transport.
	if _, err := pool.Exec(ctx, `DELETE FROM settlement.traffic_detail_gc WHERE settlement_id=ANY($1::uuid[])`, ids); err != nil {
		t.Fatal(err)
	}
	var queued int64
	for i := 0; i < 4; i++ {
		n, err := q.StorageCleanupBackfillTrafficDetailJobs(ctx, ledgerdb.StorageCleanupBackfillTrafficDetailJobsParams{DetailBefore: cleanupTimestamp(now.Add(-12 * time.Hour)), BatchSize: 1})
		if err != nil {
			t.Fatal(err)
		}
		queued += n
	}
	if queued != 1 {
		t.Fatalf("historical recovered jobs=%d", queued)
	}
	cleanup, err := NewPostgresRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	if got, _, err := cleanup.cleanupTrafficDetails(ctx, cleanupTimestamp(now.Add(-12*time.Hour)), 2); err != nil || len(got) != 1 {
		t.Fatalf("historical detail rows=%v: %v", got, err)
	}
	// The cursor has already passed these settlements. Their later outbox
	// deletion must enqueue them independently of the backfill cursor.
	publish()
	publish()
	if n, err := q.StorageCleanupDeleteTrafficOutbox(ctx, ledgerdb.StorageCleanupDeleteTrafficOutboxParams{TerminalBefore: cleanupTimestamp(now.Add(-3 * time.Hour)), BatchSize: 100}); err != nil || n != 2 {
		t.Fatalf("delayed transport rows=%d: %v", n, err)
	}
	if got, _, err := cleanup.cleanupTrafficDetails(ctx, cleanupTimestamp(old.Add(-time.Hour)), 1); err != nil || len(got) != 0 {
		t.Fatalf("strict cutoff deleted rows=%v: %v", got, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE settlement.traffic_detail_gc SET retry_after=statement_timestamp()`); err != nil {
		t.Fatal(err)
	}
	if got, segments, err := cleanup.cleanupTrafficDetails(ctx, cleanupTimestamp(now.Add(-12*time.Hour)), 1); err != nil || len(got) != 1 || segments != 1 {
		t.Fatalf("bounded detail rows=%v segments=%d: %v", got, segments, err)
	}
	// Exercise the complete worker pass, including raw dependency queues.
	result, err := cleanup.Cleanup(ctx, Cutoffs{TerminalBefore: now.Add(-3 * time.Hour), SessionBefore: now.Add(-12 * time.Hour), DetailBefore: now.Add(-12 * time.Hour), AnomalyBefore: now.Add(-30 * 24 * time.Hour)}, 100)
	if err != nil || result.TrafficSettlements != 1 || result.RawIntervals != 3 {
		t.Fatalf("worker result=%+v: %v", result, err)
	}
	var uploaded, downloaded int64
	if err := pool.QueryRow(ctx, `SELECT sum(raw_uploaded),sum(raw_downloaded) FROM ledger.traffic_daily_rollups WHERE user_id=$1`, userID).Scan(&uploaded, &downloaded); err != nil || uploaded != 3 || downloaded != 6 {
		t.Fatalf("compact totals changed: %d/%d: %v", uploaded, downloaded, err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM settlement.traffic_detail_gc)+(SELECT count(*) FROM settlement.raw_interval_gc)`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("finished cleanup jobs=%d: %v", remaining, err)
	}
}

func TestIntegrationUnclosedActivityWaitsForCoverage(t *testing.T) {
	ctx, pool := compactTestDatabase(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	hour := now.Add(-84 * time.Hour).Truncate(time.Hour)
	suffix := strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))
	stream, subject := "COMPACT_ACTIVITY_"+suffix, "peergo.compact.activity"
	snapshotStream, snapshotSubject := "COMPACT_SWARM_"+suffix, "peergo.compact.swarm"
	ingester, err := ingest.NewPostgresRepository(pool, stream, subject, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	event := trackerannouncev1.Event{SchemaVersion: trackerannouncev1.SchemaVersion, UserID: uuid.NewString(), TorrentID: 42,
		InfoHashV1: strings.Repeat("ab", 20), SessionToken: strings.Repeat("cd", 32), AddressFamily: 4, Left: 0,
		CredentialVersion: 1, TorrentControlSequence: 1, SubjectControlSequence: 1}
	for i := 0; i < 2; i++ {
		event.EventID = uuid.Must(uuid.NewV7()).String()
		event.ReceivedAt = hour.Add(time.Duration(10+i*10) * time.Minute)
		payload, err := trackerannouncev1.Encode(event)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ingester.Process(ctx, ingest.Delivery{Stream: stream, Subject: subject, Sequence: uint64(i + 1), DeliveryCount: 1, Payload: payload}); err != nil {
			t.Fatal(err)
		}
	}
	id := uuid.MustParse(event.EventID)
	if _, err := pool.Exec(ctx, `UPDATE settlement.policy_work SET settled_at=$2 WHERE interval_event_id=$1`, id, hour.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM settlement.policy_work WHERE interval_event_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	var jobs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM settlement.raw_interval_gc WHERE event_id=$1`, id).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("unclosed heartbeat created duplicate pending job=%d: %v", jobs, err)
	}
	cleanup, err := NewPostgresRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := cleanup.cleanupQueuedRawIntervals(ctx, cleanupTimestamp(now.Add(-12*time.Hour)), 100); err != nil || n != 0 {
		t.Fatalf("unclosed activity removed=%d: %v", n, err)
	}
	// Advance the announce fence with an unrelated baseline, leaving the seed
	// interval's counters and raw evidence unchanged.
	fence := event
	fence.UserID = uuid.NewString()
	fence.EventID = uuid.Must(uuid.NewV7()).String()
	fence.ReceivedAt = hour.Add(time.Hour + 46*time.Minute)
	payload, err := trackerannouncev1.Encode(fence)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ingester.Process(ctx, ingest.Delivery{Stream: stream, Subject: subject, Sequence: 3, DeliveryCount: 1, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	evidence, err := seedingevidence.NewPostgresRepository(pool, seedingevidence.PostgresRepositoryConfig{
		AnnounceStream: stream, SnapshotStream: snapshotStream, SnapshotSubject: snapshotSubject, MaxFutureSkew: time.Minute,
		MaximumSnapshotClosureDelay: 15 * time.Minute, ClosureDelay: 45 * time.Minute, MaxIntervalCredit: 35 * time.Minute}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	for i, at := range []time.Time{hour.Add(59 * time.Minute), hour.Add(time.Hour + time.Minute)} {
		chunk := trackerswarmv1.SnapshotChunk{SchemaVersion: trackerswarmv1.SchemaVersion, EventID: uuid.Must(uuid.NewV7()).String(), SnapshotID: uuid.Must(uuid.NewV7()).String(),
			SourceID: "compact-" + strings.ToLower(suffix[:12]), RoutingEpoch: 1, SnapshotSequence: int64(i + 1), ObservedAt: at, Scope: trackerswarmv1.ScopeAll, ChunkIndex: 0, ChunkCount: 1,
			Entries: []trackerswarmv1.Entry{{InfoHashV1: event.InfoHashV1, Seeders: 3, Leechers: 1}}}
		payload, err := trackerswarmv1.Encode(chunk)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := evidence.ApplySnapshot(ctx, seedingevidence.SnapshotDelivery{Stream: snapshotStream, Subject: snapshotSubject, Sequence: uint64(i + 1), DeliveryCount: 1, Payload: payload}); err != nil {
			t.Fatal(err)
		}
	}
	if built, err := evidence.BuildHour(ctx, hour, now); err != nil || built.ItemCount != 1 {
		t.Fatalf("closed evidence=%+v: %v", built, err)
	}
	var seconds int64
	if err := pool.QueryRow(ctx, `SELECT active_seconds FROM ledger.seeding_evidence_items WHERE window_start=$1 AND user_id=$2::uuid`, hour, event.UserID).Scan(&seconds); err != nil || seconds != 600 {
		t.Fatalf("zero traffic seed seconds=%d: %v", seconds, err)
	}
	q := ledgerdb.New(pool)
	// A later completed hour must not cause cleanup to select legacy detail
	// that the database still protects behind an earlier unresolved hole.
	if _, err := pool.Exec(ctx, `INSERT INTO settlement.seeding_window_retries
      (source_stream,window_start,retry_after,first_seen_at,last_seen_at,reason_code)
      VALUES($1,$2,$3,$3,$3,'source_coverage_pending')`, stream, hour.Add(-time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if n, err := q.StorageCleanupDeleteSnapshotEntries(ctx, ledgerdb.StorageCleanupDeleteSnapshotEntriesParams{
		DetailBefore: cleanupTimestamp(now.Add(-12 * time.Hour)), BatchSize: 100}); err != nil || n != 0 {
		t.Fatalf("protected selected legacy frame interrupted cleanup: %d %v", n, err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM settlement.seeding_window_retries WHERE source_stream=$1 AND window_start=$2", stream, hour.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	// The historical cursor can already be past this older fixture. Closing a
	// window must register an independent range so its heartbeat remains reachable.
	if n, err := q.StorageCleanupBackfillRawRangeJobs(ctx, ledgerdb.StorageCleanupBackfillRawRangeJobsParams{DetailBefore: cleanupTimestamp(now.Add(-12 * time.Hour)), BatchSize: 100}); err != nil || n != 1 {
		t.Fatalf("closed range jobs=%d: %v", n, err)
	}
	if n, err := cleanup.cleanupQueuedRawIntervals(ctx, cleanupTimestamp(now.Add(-12*time.Hour)), 100); err != nil || n != 0 {
		t.Fatalf("referenced evidence raw removed=%d: %v", n, err)
	}
	if ids, err := q.StorageCleanupDeleteSeedingSources(ctx, ledgerdb.StorageCleanupDeleteSeedingSourcesParams{DetailBefore: cleanupTimestamp(now.Add(-12 * time.Hour)), BatchSize: 100}); err != nil || len(ids) != 1 {
		t.Fatalf("consumed source links=%v: %v", ids, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE settlement.raw_interval_gc SET retry_after=statement_timestamp() WHERE event_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if n, err := cleanup.cleanupQueuedRawIntervals(ctx, cleanupTimestamp(now.Add(-12*time.Hour)), 100); err != nil || n != 1 {
		t.Fatalf("consumed raw removed=%d: %v", n, err)
	}
}

// An almost-empty payload ledger must not evaluate expiry across the much
// larger legacy header ledger. Roll back the synthetic headers after the test.
func TestIntegrationSnapshotPayloadCleanupWithSparseLegacyHeaders(t *testing.T) {
	ctx, pool := compactTestDatabase(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	source := "payload-sparse-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	_, err = tx.Exec(ctx, `INSERT INTO ledger.seeding_swarm_snapshots
 (snapshot_id,source_id,routing_epoch,snapshot_sequence,observed_at,chunk_count,received_chunk_count,status,created_at,completed_at)
 SELECT md5($1||n::text)::uuid,$1,1,n,
 date_trunc('hour',now()-interval '72 hours')+n*interval '1 millisecond',
 1,1,'complete',now()-interval '72 hours',now()-interval '72 hours'
 FROM generate_series(1,60000) n`, source)
	if err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := ledgerdb.New(tx).StorageCleanupDeleteSnapshotPayloads(bounded); err != nil {
		t.Fatalf("sparse-payload cleanup exceeded bounded execution: %v", err)
	}
}
