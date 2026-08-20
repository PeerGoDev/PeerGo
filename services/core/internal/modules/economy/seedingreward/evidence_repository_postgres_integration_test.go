package seedingreward_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/settlementseedingv1"
	"github.com/peergo/peergo/services/core/internal/modules/economy/seedingreward"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

// This test requires a freshly migrated disposable Core database because the
// evidence projection is intentionally immutable and cannot clean itself up.
func TestIntegrationAssemblesChunksAndVerifiesProjectionDigest(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("PEERGO_TEST_CORE_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("PEERGO_TEST_CORE_DATABASE_URL is required")
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
	now := windowStart.Add(time.Hour + 2*time.Minute)
	userID, torrentIDs := insertEvidenceFixture(t, ctx, pool, now)
	snapshotID := mustV7(t)
	items := []settlementseedingv1.Item{
		{
			UserID: userID.String(), TorrentID: torrentIDs[0], ActiveSeconds: 1200,
			RawUploadedBytes: 1024, SnapshotSeeders: 2, SnapshotLeechers: 1,
			EvidenceSHA256: digestText("tracker-item-1"),
		},
		{
			UserID: userID.String(), TorrentID: torrentIDs[1], ActiveSeconds: 2400,
			RawUploadedBytes: 2048, SnapshotSeeders: 5, SnapshotLeechers: 0,
			EvidenceSHA256: digestText("tracker-item-2"),
		},
	}
	header := settlementseedingv1.Event{
		SchemaVersion: settlementseedingv1.SchemaVersion,
		WindowStart:   windowStart, WindowEnd: windowStart.Add(time.Hour), BuiltAt: now,
		WindowEvidenceSHA256: digestText("tracker-window"), SnapshotID: snapshotID.String(), SnapshotSequence: 7,
		SnapshotObservedAt: windowStart.Add(55 * time.Minute), ItemCount: 2, ChunkCount: 2,
	}
	projectionDigest, err := settlementseedingv1.ProjectionDigest(header, items)
	if err != nil {
		t.Fatal(err)
	}
	header.ProjectionSHA256 = settlementseedingv1.DigestHex(projectionDigest)
	first := header
	first.EventID, first.ChunkIndex, first.Items = mustV7(t).String(), 0, items[:1]
	second := header
	second.EventID, second.ChunkIndex, second.Items = mustV7(t).String(), 1, items[1:]

	repository, err := seedingreward.NewPostgresEvidenceRepository(pool, func() time.Time { return now.Add(time.Second) }, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	firstPayload := encodeEvidence(t, first)
	result, err := repository.ApplyEvidence(ctx, firstPayload, now)
	if err != nil || result.Complete || result.Duplicate {
		t.Fatalf("ApplyEvidence(first) result=%+v error=%v", result, err)
	}
	duplicate, err := repository.ApplyEvidence(ctx, firstPayload, now.Add(time.Second))
	if err != nil || !duplicate.Duplicate {
		t.Fatalf("ApplyEvidence(duplicate) result=%+v error=%v", duplicate, err)
	}

	conflict := first
	conflict.EventID = mustV7(t).String()
	conflict.Items[0].RawUploadedBytes++
	if _, err := repository.ApplyEvidence(ctx, encodeEvidence(t, conflict), now); !errors.Is(err, seedingreward.ErrEvidenceConflict) {
		t.Fatalf("ApplyEvidence(conflicting chunk) error=%v", err)
	}

	complete, err := repository.ApplyEvidence(ctx, encodeEvidence(t, second), now)
	if err != nil || !complete.Complete {
		t.Fatalf("ApplyEvidence(second) result=%+v error=%v", complete, err)
	}
	var status string
	var receivedChunks, itemCount int
	var storedProjection []byte
	if err := pool.QueryRow(ctx, `
SELECT status, received_chunk_count, item_count, projection_sha256
FROM economy.seeding_reward_evidence_windows
WHERE window_start = $1`, windowStart).Scan(&status, &receivedChunks, &itemCount, &storedProjection); err != nil {
		t.Fatal(err)
	}
	if status != "complete" || receivedChunks != 2 || itemCount != 2 || !strings.EqualFold(hex.EncodeToString(storedProjection), header.ProjectionSHA256) {
		t.Fatalf("window status=%s chunks=%d items=%d digest=%x", status, receivedChunks, itemCount, storedProjection)
	}
	if _, err := pool.Exec(ctx, `
UPDATE economy.seeding_reward_evidence_items
SET active_seconds = active_seconds + 1
WHERE window_start = $1`, windowStart); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("immutable evidence update error=%v", err)
	}
}

func insertEvidenceFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) (uuid.UUID, []int64) {
	t.Helper()
	userID, credentialRef := uuid.New(), uuid.New()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.users (
    id, credential_ref, username, display_name, status, email_verified_at,
    created_at, updated_at, password_changed_at
) VALUES ($1, $2, $3, $4, 'active', $5, $5, $5, $5)`,
		userID, credentialRef, "seed-evidence-"+suffix, "Seeding evidence "+suffix, now); err != nil {
		t.Fatal(err)
	}
	categoryID := "seed-evidence-" + suffix
	if _, err := pool.Exec(ctx, `
INSERT INTO catalog.categories (id, name, display_order, enabled, created_at, updated_at)
VALUES ($1, $2, 100000 + abs(hashtext($1)) % 900000, true, $3, $3)`, categoryID, "Evidence "+suffix, now); err != nil {
		t.Fatal(err)
	}
	torrentIDs := make([]int64, 2)
	for index := range torrentIDs {
		objectID := uuid.New()
		objectDigest := sha256.Sum256([]byte(fmt.Sprintf("evidence-object-%s-%d", suffix, index)))
		infoDigest := sha256.Sum256([]byte(fmt.Sprintf("evidence-info-%s-%d", suffix, index)))
		if _, err := pool.Exec(ctx, `
INSERT INTO torrents.torrent_objects (
    id, content_sha256, byte_length, parser_version, validation_profile,
    compatibility_flags, info_offset, info_length, created_at
) VALUES ($1, $2, 256, 'integration-v1', 'strict_upload', ARRAY[]::text[], 0, 128, $3)`,
			objectID, objectDigest[:], now); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `
INSERT INTO torrents.torrents (
    uploader_id, category_id, object_id, info_hash_v1,
    content_name, title, subtitle, total_size_bytes, payload_size_bytes,
    file_count, padding_file_count, piece_length_bytes, piece_count,
    state, version, submitted_at, published_at, state_changed_at, updated_at
) VALUES (
	$1, $2, $3, $4, $5, $6, '', 4096, 4096,
    1, 0, 16384, 1, 'published', 2,
	$7::timestamptz - interval '1 hour', $7, $7, $7
) RETURNING id`, userID, categoryID, objectID, infoDigest[:20],
			fmt.Sprintf("evidence-%d.bin", index), fmt.Sprintf("Evidence %s %d", suffix, index), now).Scan(&torrentIDs[index]); err != nil {
			t.Fatal(err)
		}
	}
	return userID, torrentIDs
}

func encodeEvidence(t *testing.T, event settlementseedingv1.Event) []byte {
	t.Helper()
	payload, err := settlementseedingv1.Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func digestText(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func mustV7(t *testing.T) uuid.UUID {
	t.Helper()
	value, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return value
}
