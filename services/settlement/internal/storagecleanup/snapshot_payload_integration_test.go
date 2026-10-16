package storagecleanup

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
	"github.com/peergo/peergo/services/settlement/internal/generated/ledgerdb"
	"github.com/peergo/peergo/services/settlement/internal/seedingevidence"
	"strings"
	"testing"
	"time"
)

func TestIntegrationCompressedSnapshotCleanupPreservesUnclosedBoundaries(t *testing.T) {
	ctx, pool := compactTestDatabase(t)
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	stream := "COMPRESSED_GC_" + strings.ToUpper(suffix)
	now := time.Now().UTC().Truncate(time.Microsecond)
	hour := now.Add(-168 * time.Hour).Truncate(time.Hour)
	config := seedingevidence.PostgresRepositoryConfig{CompactSnapshots: true, AnnounceStream: stream, SnapshotStream: stream, SnapshotSubject: "peergo.compact.gc." + suffix,
		MaxFutureSkew: time.Minute, MaximumSnapshotClosureDelay: 15 * time.Minute}
	repo, err := seedingevidence.NewPostgresRepository(pool, config, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	var ids []uuid.UUID
	for i, minute := range []int{0, 1, 59, 61} {
		id := uuid.Must(uuid.NewV7())
		ids = append(ids, id)
		frame := trackerswarmv1.SnapshotChunk{SchemaVersion: trackerswarmv1.SchemaVersion, EventID: uuid.Must(uuid.NewV7()).String(), SnapshotID: id.String(),
			SourceID: "compressed-" + suffix, RoutingEpoch: 1, SnapshotSequence: int64(i + 1), ObservedAt: hour.Add(time.Duration(minute) * time.Minute),
			Scope: trackerswarmv1.ScopeAll, ChunkCount: 1, Entries: []trackerswarmv1.Entry{{InfoHashV1: fmt.Sprintf("%040x", i), Seeders: 3, Leechers: 1}}}
		payload, e := trackerswarmv1.Encode(frame)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = repo.ApplySnapshot(ctx, seedingevidence.SnapshotDelivery{Stream: stream, Subject: config.SnapshotSubject, Sequence: uint64(i + 1), DeliveryCount: 1, Payload: payload}); e != nil {
			t.Fatal(e)
		}
	}
	q := ledgerdb.New(pool)
	if deleted, e := q.StorageCleanupDeleteSnapshotPayloads(ctx); e != nil || deleted != 1 {
		t.Fatalf("compressed interior deleted=%d %v", deleted, e)
	}
	if deleted, e := q.StorageCleanupDeleteSnapshotPayloads(ctx); e != nil || deleted != 0 {
		t.Fatalf("repeat compressed cleanup deleted=%d %v", deleted, e)
	}
	for _, id := range []uuid.UUID{ids[0], ids[2], ids[3]} {
		if _, e := pool.Exec(ctx, "DELETE FROM ledger.seeding_snapshot_payloads WHERE snapshot_id=$1", id); e == nil {
			t.Fatalf("unclosed compressed boundary %s lost", id)
		}
	}
	var retained int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM ledger.seeding_snapshot_payloads WHERE snapshot_id=ANY($1::uuid[])", ids).Scan(&retained); err != nil || retained != 3 {
		t.Fatalf("retained compressed frames=%d %v", retained, err)
	}
}
