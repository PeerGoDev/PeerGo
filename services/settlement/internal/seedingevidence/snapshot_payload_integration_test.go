package seedingevidence

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
	"github.com/peergo/peergo/services/settlement/internal/generated/ledgerdb"
)

func TestIntegrationSnapshotMixedChunksRejectDuplicatesAndMissingPayload(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("PEERGO_TEST_TRACKER_DATABASE_URL"))
	if url == "" {
		t.Skip("PEERGO_TEST_TRACKER_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	stream := "PAYLOAD_" + strings.ToUpper(suffix)
	now := time.Now().UTC().Truncate(time.Microsecond)
	config := PostgresRepositoryConfig{AnnounceStream: stream, SnapshotStream: stream, SnapshotSubject: "peergo.payload." + suffix,
		MaxFutureSkew: time.Minute, MaximumSnapshotClosureDelay: 15 * time.Minute}
	legacy, err := NewPostgresRepository(pool, config, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	config.CompactSnapshots = true
	compact, err := NewPostgresRepository(pool, config, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	source := "payload-" + suffix
	makeChunk := func(id uuid.UUID, sequence int64, index int32, hash string) trackerswarmv1.SnapshotChunk {
		return trackerswarmv1.SnapshotChunk{SchemaVersion: trackerswarmv1.SchemaVersion, EventID: uuid.Must(uuid.NewV7()).String(),
			SnapshotID: id.String(), SourceID: source, RoutingEpoch: 1, SnapshotSequence: sequence, ObservedAt: now.Add(time.Duration(sequence-2) * time.Minute),
			Scope: trackerswarmv1.ScopeAll, ChunkCount: 2, ChunkIndex: index, Entries: []trackerswarmv1.Entry{{InfoHashV1: hash, Seeders: 3, Leechers: 1}}}
	}
	apply := func(repo *PostgresRepository, seq uint64, chunk trackerswarmv1.SnapshotChunk) (SnapshotApplyResult, error) {
		payload, e := trackerswarmv1.Encode(chunk)
		if e != nil {
			t.Fatal(e)
		}
		return repo.ApplySnapshot(ctx, SnapshotDelivery{Stream: stream, Subject: config.SnapshotSubject, Sequence: seq, DeliveryCount: 1, Payload: payload})
	}
	id := uuid.Must(uuid.NewV7())
	if result, e := apply(legacy, 1, makeChunk(id, 1, 0, strings.Repeat("11", 20))); e != nil || result.Complete {
		t.Fatalf("legacy first chunk %+v %v", result, e)
	}
	if result, e := apply(compact, 2, makeChunk(id, 1, 1, strings.Repeat("22", 20))); e != nil || !result.Complete {
		t.Fatalf("mixed completion %+v %v", result, e)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	counts, err := readSnapshotCounts(ctx, tx, ledgerdb.New(tx), id)
	_ = tx.Rollback(ctx)
	if err != nil || len(counts) != 2 {
		t.Fatalf("mixed counts %d %v", len(counts), err)
	}
	// Recent compressed data is protected even from a manual delete.
	if _, err = pool.Exec(ctx, "DELETE FROM ledger.seeding_snapshot_payloads WHERE snapshot_id=$1", id); err == nil {
		t.Fatal("recent payload deletion accepted")
	}
	// Simulate a missing disk representation only inside a rolled-back disposable
	// transaction. The reader must fail, never substitute a zero count.
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, "SET LOCAL session_replication_role=replica"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "DELETE FROM ledger.seeding_snapshot_payloads WHERE snapshot_id=$1", id); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "SET LOCAL session_replication_role=origin"); err != nil {
		t.Fatal(err)
	}
	if _, err = readSnapshotCounts(ctx, tx, ledgerdb.New(tx), id); !errors.Is(err, ErrInvariant) {
		t.Fatalf("missing chunk accepted: %v", err)
	}
	_ = tx.Rollback(ctx)
	second := uuid.Must(uuid.NewV7())
	if result, e := apply(compact, 3, makeChunk(second, 2, 0, strings.Repeat("33", 20))); e != nil || result.Complete {
		t.Fatalf("first compressed %+v %v", result, e)
	}
	bad := makeChunk(second, 2, 1, strings.Repeat("33", 20))
	if _, e := apply(compact, 4, bad); !errors.Is(e, ErrConflict) {
		t.Fatalf("duplicate torrent across chunks accepted: %v", e)
	}
	var status string
	var received int
	if err = pool.QueryRow(ctx, "SELECT status,received_chunk_count FROM ledger.seeding_swarm_snapshots WHERE snapshot_id=$1", second).Scan(&status, &received); err != nil {
		t.Fatal(err)
	}
	if status != "collecting" || received != 1 {
		t.Fatalf("bad chunk partially committed: %s %d", status, received)
	}
	bad.Entries[0].InfoHashV1 = strings.Repeat("44", 20)
	if result, e := apply(compact, 4, bad); e != nil || !result.Complete {
		t.Fatalf("corrected chunk %+v %v", result, e)
	}
}
