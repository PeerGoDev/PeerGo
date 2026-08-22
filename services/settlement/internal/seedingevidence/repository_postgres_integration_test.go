package seedingevidence_test

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/settlementseedingv1"
	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
	"github.com/peergo/peergo/services/settlement/internal/ingest"
	platformpostgres "github.com/peergo/peergo/services/settlement/internal/platform/postgres"
	"github.com/peergo/peergo/services/settlement/internal/seedingevidence"
	"github.com/peergo/peergo/services/settlement/internal/seedingoutbox"
)

// The test is gated because immutable evidence intentionally cannot be cleaned
// out of a shared database. Point it at a freshly migrated disposable Tracker
// Ledger database.
func TestIntegrationClosesUnionedHourAndDetectsLateInterval(t *testing.T) {
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

	windowStart := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Hour)
	windowEnd := windowStart.Add(time.Hour)
	clock := windowEnd.Add(46 * time.Minute)
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	announceStream := "PEERGO_SEEDING_IT_" + strings.ToUpper(suffix)
	announceSubject := "peergo.seeding.it." + suffix
	snapshotStream := "PEERGO_SEEDING_SNAPSHOT_IT_" + strings.ToUpper(suffix)
	snapshotSubject := "peergo.seeding.snapshot.it." + suffix
	userID := uuid.New()
	torrentID := int64(42)
	infoHash := strings.Repeat("ab", 20)
	ingestRepository, err := ingest.NewPostgresRepository(pool, announceStream, announceSubject, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	sequence := uint64(1)
	sessionA := strings.Repeat("11", 32)
	sessionB := strings.Repeat("22", 32)
	staleSession := strings.Repeat("66", 32)
	ingestSeedingEvent(t, ctx, ingestRepository, announceStream, announceSubject, sequence, seedingEvent(t, userID, torrentID, infoHash, staleSession, windowStart.Add(2*time.Minute), 0, 0))
	sequence++
	ingestSeedingEvent(t, ctx, ingestRepository, announceStream, announceSubject, sequence, seedingEvent(t, userID, torrentID, infoHash, sessionA, windowStart.Add(5*time.Minute), 0, 0))
	sequence++
	ingestSeedingEvent(t, ctx, ingestRepository, announceStream, announceSubject, sequence, seedingEvent(t, userID, torrentID, infoHash, sessionB, windowStart.Add(10*time.Minute), 0, 0))
	sequence++
	ingestSeedingEvent(t, ctx, ingestRepository, announceStream, announceSubject, sequence, seedingEvent(t, userID, torrentID, infoHash, sessionA, windowStart.Add(20*time.Minute), 100, 0))
	sequence++
	ingestSeedingEvent(t, ctx, ingestRepository, announceStream, announceSubject, sequence, seedingEvent(t, userID, torrentID, infoHash, sessionB, windowStart.Add(30*time.Minute), 200, 0))
	sequence++
	// A 38-minute adjacent announce gap exceeds the 35-minute Tracker peer
	// lifetime. The entire interval is stale and must earn no seed time.
	ingestSeedingEvent(t, ctx, ingestRepository, announceStream, announceSubject, sequence, seedingEvent(t, userID, torrentID, infoHash, staleSession, windowStart.Add(40*time.Minute), 500, 0))
	sequence++
	ingestSeedingEvent(t, ctx, ingestRepository, announceStream, announceSubject, sequence, seedingEvent(t, uuid.New(), torrentID+1, strings.Repeat("cd", 20), strings.Repeat("33", 32), windowEnd.Add(46*time.Minute), 0, 0))

	repository, err := seedingevidence.NewPostgresRepository(pool, seedingevidence.PostgresRepositoryConfig{
		AnnounceStream: announceStream, SnapshotStream: snapshotStream, SnapshotSubject: snapshotSubject,
		MaxFutureSkew: 2 * time.Minute, MaximumSnapshotClosureDelay: 15 * time.Minute,
		ClosureDelay: 45 * time.Minute, MaxIntervalCredit: 35 * time.Minute,
	}, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	// Exercise replay of pre-fix Tracker messages whose timestamps carry more
	// precision than PostgreSQL timestamptz can store.
	selected := snapshotChunk(t, "tracker-primary", 1, windowStart.Add(50*time.Minute).Add(789*time.Nanosecond), infoHash, 3, 1)
	fence := snapshotChunk(t, "tracker-primary", 2, windowEnd.Add(time.Minute).Add(789*time.Nanosecond), infoHash, 4, 0)
	for index, chunk := range []trackerswarmv1.SnapshotChunk{selected, fence} {
		payload, encodeErr := trackerswarmv1.Encode(chunk)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		result, applyErr := repository.ApplySnapshot(ctx, seedingevidence.SnapshotDelivery{
			Stream: snapshotStream, Subject: snapshotSubject, Sequence: uint64(index + 1), DeliveryCount: 1, Payload: payload,
		})
		if applyErr != nil || !result.Complete {
			t.Fatalf("ApplySnapshot(%d) result=%+v error=%v", index, result, applyErr)
		}
	}

	result, err := repository.BuildHour(ctx, windowStart, clock)
	if err != nil || result.Duplicate || result.ItemCount != 1 || result.SelectedSnapshotSequence != 1 || result.SnapshotFenceSequence != 2 {
		t.Fatalf("BuildHour() result=%+v error=%v", result, err)
	}
	var activeSeconds, rawUploaded int64
	var seeders, leechers, sourceCount int
	if err := pool.QueryRow(ctx, `
SELECT active_seconds, raw_uploaded, snapshot_seeders, snapshot_leechers, source_interval_count
FROM ledger.seeding_evidence_items
WHERE window_start = $1 AND user_id = $2 AND torrent_id = $3`, windowStart, userID, torrentID).Scan(
		&activeSeconds, &rawUploaded, &seeders, &leechers, &sourceCount,
	); err != nil {
		t.Fatal(err)
	}
	if activeSeconds != 25*60 || rawUploaded != 300 || seeders != 3 || leechers != 1 || sourceCount != 2 {
		t.Fatalf("item active=%d uploaded=%d swarm=%d/%d sources=%d", activeSeconds, rawUploaded, seeders, leechers, sourceCount)
	}
	var schemaVersion string
	var closureSeconds, maxIntervalSeconds int
	if err := pool.QueryRow(ctx, `
SELECT schema_version, closure_delay_seconds, max_interval_credit_seconds
FROM ledger.seeding_evidence_windows
WHERE window_start = $1`, windowStart).Scan(&schemaVersion, &closureSeconds, &maxIntervalSeconds); err != nil {
		t.Fatal(err)
	}
	if schemaVersion != seedingevidence.SchemaVersion || closureSeconds != 2700 || maxIntervalSeconds != 2100 {
		t.Fatalf("window schema=%s closure=%d credit=%d", schemaVersion, closureSeconds, maxIntervalSeconds)
	}
	var outboxPayload string
	if err := pool.QueryRow(ctx, `
SELECT payload_json
FROM settlement.seeding_evidence_outbox
WHERE window_start = $1 AND chunk_index = 0`, windowStart).Scan(&outboxPayload); err != nil {
		t.Fatal(err)
	}
	projection, err := settlementseedingv1.Decode([]byte(outboxPayload))
	if err != nil || projection.ItemCount != 1 || len(projection.Items) != 1 ||
		projection.Items[0].UserID != userID.String() || projection.Items[0].TorrentID != torrentID ||
		projection.Items[0].ActiveSeconds != 25*60 || projection.Items[0].RawUploadedBytes != 300 ||
		projection.Items[0].SnapshotSeeders != 3 || projection.Items[0].SnapshotLeechers != 1 {
		t.Fatalf("outbox projection=%+v error=%v", projection, err)
	}
	outbox, err := seedingoutbox.NewPostgresRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	pending, found, err := outbox.ClaimNext(ctx, clock.Add(time.Second), 30*time.Second)
	if err != nil || !found || pending.Event.EventID != projection.EventID || pending.Attempts != 1 {
		t.Fatalf("ClaimNext() pending=%+v found=%t error=%v", pending, found, err)
	}
	if err := outbox.Release(ctx, pending, clock.Add(2*time.Second), "integration_retry"); err != nil {
		t.Fatal(err)
	}
	pending, found, err = outbox.ClaimNext(ctx, clock.Add(2*time.Second), 30*time.Second)
	if err != nil || !found || pending.Attempts != 2 {
		t.Fatalf("ClaimNext(retry) pending=%+v found=%t error=%v", pending, found, err)
	}
	if err := outbox.MarkPublished(ctx, pending, clock.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	duplicate, err := repository.BuildHour(ctx, windowStart, clock.Add(time.Second))
	if err != nil || !duplicate.Duplicate || duplicate.EvidenceSHA256 != result.EvidenceSHA256 {
		t.Fatalf("duplicate BuildHour() result=%+v error=%v", duplicate, err)
	}

	sequence++
	leechSession := strings.Repeat("55", 32)
	leechBaseline := seedingEvent(t, userID, torrentID, infoHash, leechSession, windowStart.Add(12*time.Minute), 0, 0)
	leechBaseline.Left = 1_000
	ingestSeedingEvent(t, ctx, ingestRepository, announceStream, announceSubject, sequence, leechBaseline)
	sequence++
	leechInterval := seedingEvent(t, userID, torrentID, infoHash, leechSession, windowStart.Add(18*time.Minute), 0, 100)
	leechInterval.Left = 900
	ingestSeedingEvent(t, ctx, ingestRepository, announceStream, announceSubject, sequence, leechInterval)
	var anomalyCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ledger.seeding_evidence_anomalies WHERE window_start = $1`, windowStart).Scan(&anomalyCount); err != nil || anomalyCount != 0 {
		t.Fatalf("late leeching interval anomalies=%d error=%v", anomalyCount, err)
	}

	sequence++
	lateSession := strings.Repeat("44", 32)
	ingestSeedingEvent(t, ctx, ingestRepository, announceStream, announceSubject, sequence, seedingEvent(t, userID, torrentID, infoHash, lateSession, windowStart.Add(15*time.Minute), 0, 0))
	sequence++
	ingestSeedingEvent(t, ctx, ingestRepository, announceStream, announceSubject, sequence, seedingEvent(t, userID, torrentID, infoHash, lateSession, windowStart.Add(40*time.Minute), 400, 0))
	drifted, err := repository.BuildHour(ctx, windowStart, clock.Add(2*time.Second))
	if !errors.Is(err, seedingevidence.ErrEvidenceDrift) || drifted.AnomalyCount != 1 {
		t.Fatalf("drift BuildHour() result=%+v error=%v", drifted, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE ledger.seeding_evidence_items SET active_seconds = active_seconds + 1 WHERE window_start = $1`, windowStart); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("immutable evidence update error=%v", err)
	}
}

func ingestSeedingEvent(t *testing.T, ctx context.Context, repository *ingest.PostgresRepository, stream, subject string, sequence uint64, event trackerannouncev1.Event) {
	t.Helper()
	payload, err := trackerannouncev1.Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Process(ctx, ingest.Delivery{
		Stream: stream, Subject: subject, Sequence: sequence, DeliveryCount: 1, Payload: payload,
	}); err != nil {
		t.Fatalf("ingest sequence %d: %v", sequence, err)
	}
}

func seedingEvent(t *testing.T, userID uuid.UUID, torrentID int64, infoHash, sessionToken string, receivedAt time.Time, uploaded, downloaded int64) trackerannouncev1.Event {
	t.Helper()
	eventID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return trackerannouncev1.Event{
		SchemaVersion: trackerannouncev1.SchemaVersion, EventID: eventID.String(), ReceivedAt: receivedAt,
		UserID: userID.String(), TorrentID: torrentID, InfoHashV1: infoHash, SessionToken: sessionToken,
		AddressFamily: 4, Uploaded: uploaded, Downloaded: downloaded, Left: 0,
		CredentialVersion: 1, TorrentControlSequence: 1, SubjectControlSequence: 1,
	}
}

func snapshotChunk(t *testing.T, source string, sequence int64, observedAt time.Time, infoHash string, seeders, leechers int32) trackerswarmv1.SnapshotChunk {
	t.Helper()
	eventID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	snapshotID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hex.DecodeString(infoHash); err != nil {
		t.Fatal(err)
	}
	return trackerswarmv1.SnapshotChunk{
		SchemaVersion: trackerswarmv1.SchemaVersion, EventID: eventID.String(), SnapshotID: snapshotID.String(),
		SourceID: source, RoutingEpoch: 1, SnapshotSequence: sequence, ObservedAt: observedAt,
		Scope: trackerswarmv1.ScopeAll, ChunkIndex: 0, ChunkCount: 1,
		Entries: []trackerswarmv1.Entry{{InfoHashV1: infoHash, Seeders: seeders, Leechers: leechers}},
	}
}
