package seedingreward_test

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/settlementseedingv1"
	"github.com/peergo/peergo/services/core/internal/generated/seedingrewarddb"
	"github.com/peergo/peergo/services/core/internal/modules/economy/seedingreward"
)

// Use a fresh disposable database: evidence and receipts are immutable.
func TestIntegrationPackedEvidenceReadsAndDuplicateReplay(t *testing.T) {
	url := os.Getenv("PEERGO_TEST_CORE_DATABASE_URL")
	if url == "" {
		t.Skip("PEERGO_TEST_CORE_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	hour := time.Now().UTC().Add(-101 * time.Hour).Truncate(time.Hour)
	built := hour.Add(2 * time.Hour)
	user, torrents := insertEvidenceFixture(t, ctx, pool, hour)
	items := []settlementseedingv1.Item{
		{UserID: user.String(), TorrentID: torrents[0], ActiveSeconds: 123, RawUploadedBytes: 123456789, SnapshotSeeders: 3, SnapshotLeechers: 2, EvidenceSHA256: digestText("pack-item-1")},
		{UserID: user.String(), TorrentID: torrents[1], ActiveSeconds: 2700, RawUploadedBytes: 987654321, SnapshotSeeders: 4, SnapshotLeechers: 1, EvidenceSHA256: digestText("pack-item-2")},
	}
	event := settlementseedingv1.Event{SchemaVersion: settlementseedingv1.SchemaVersion,
		EventID: mustV7(t).String(), WindowStart: hour, WindowEnd: hour.Add(time.Hour), BuiltAt: built,
		WindowEvidenceSHA256: digestText("pack-window"), SnapshotID: mustV7(t).String(), SnapshotSequence: 1,
		SnapshotObservedAt: hour.Add(55 * time.Minute), ItemCount: 2, ChunkCount: 1, ChunkIndex: 0, Items: items}
	digest, err := settlementseedingv1.ProjectionDigest(event, items)
	if err != nil {
		t.Fatal(err)
	}
	event.ProjectionSHA256 = settlementseedingv1.DigestHex(digest)
	payload := encodeEvidence(t, event)
	repo, err := seedingreward.NewPostgresEvidenceRepository(pool, func() time.Time { return built }, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := repo.ApplyEvidence(ctx, payload, built)
	if err != nil || !applied.Complete {
		t.Fatalf("apply: %+v %v", applied, err)
	}
	q := seedingrewarddb.New(pool)
	timestamp := pgtype.Timestamptz{Time: hour, Valid: true}
	before, err := q.ListSeedingRewardEvidenceItems(ctx, timestamp)
	if err != nil || len(before) != 2 {
		t.Fatalf("before: %+v %v", before, err)
	}
	if _, err = pool.Exec(ctx, "UPDATE economy.seeding_reward_evidence_packing_control SET enabled=true"); err != nil {
		t.Fatal(err)
	}
	var moved int64
	if err = pool.QueryRow(ctx, "SELECT economy.seeding_reward_evidence_pack_batch(1)").Scan(&moved); err != nil || moved != 2 {
		t.Fatalf("move=%d %v", moved, err)
	}
	after, err := q.ListSeedingRewardEvidenceItems(ctx, timestamp)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("archived evidence changed: before=%+v after=%+v %v", before, after, err)
	}
	replay, err := repo.ApplyEvidence(ctx, payload, built.Add(time.Second))
	if err != nil || !replay.Duplicate {
		t.Fatalf("archived duplicate replay: %+v %v", replay, err)
	}
	var active, receipts int64
	if err = pool.QueryRow(ctx, "SELECT active_seconds FROM economy.seeding_reward_evidence_user_hours WHERE window_start=$1 AND user_id=$2", hour, user).Scan(&active); err != nil || active != 2823 {
		t.Fatalf("summary=%d %v", active, err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM economy.seeding_reward_evidence_inbox WHERE window_start=$1", hour).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("receipts=%d %v", receipts, err)
	}
}
