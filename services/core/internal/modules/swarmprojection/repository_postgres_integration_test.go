package swarmprojection_test

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
	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
	"github.com/peergo/peergo/services/core/internal/modules/swarmprojection"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

// This test intentionally exercises a full projection, which updates every
// catalog row backed by a published aggregate. Run it only against a disposable migrated database and,
// when running all integration packages, give this package its own database.
//
//	PEERGO_TEST_CORE_DATABASE_URL=postgres://.../peergo_core_swarm_test?sslmode=disable \
//	go test ./internal/modules/swarmprojection -run Integration
func TestIntegrationAppliesOnlyCompleteSnapshotsAndDeduplicatesCompletions(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("PEERGO_TEST_CORE_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("PEERGO_TEST_CORE_DATABASE_URL is required")
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

	now := time.Now().UTC().Truncate(time.Microsecond)
	first := insertPublishedSwarmFixture(t, ctx, pool, "included", now, 9, 8, 4)
	second := insertPublishedSwarmFixture(t, ctx, pool, "absent", now, 6, 5, 2)
	repository, err := swarmprojection.NewPostgresRepository(pool, func() time.Time { return now }, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	// A fresh epoch makes the gated test repeatable on the same disposable DB.
	// Epoch changes are an operator-controlled source handoff in production.
	routingEpoch := time.Now().UTC().UnixNano()
	sourceID := "integration-" + strings.ToLower(strings.ReplaceAll(uuid.NewString()[:8], "-", ""))
	snapshotID := mustUUIDV7(t)
	// Production Tracker events created before timestamp canonicalization can
	// contain nanoseconds that PostgreSQL cannot retain. The consumer must still
	// apply and replay them without reporting a false conflict.
	observedAt := now.Add(-30 * time.Second).Add(789 * time.Nanosecond)
	unknownHash := sha256.Sum256([]byte("unknown-swarm-entry-" + snapshotID.String()))
	chunks := []trackerswarmv1.SnapshotChunk{
		newSnapshotChunk(t, snapshotID, mustUUIDV7(t), sourceID, routingEpoch, 2, observedAt, 0, 2, []trackerswarmv1.Entry{{
			InfoHashV1: hex.EncodeToString(first.infoHash), Seeders: 21, Leechers: 3,
		}}),
		newSnapshotChunk(t, snapshotID, mustUUIDV7(t), sourceID, routingEpoch, 2, observedAt, 1, 2, []trackerswarmv1.Entry{{
			InfoHashV1: hex.EncodeToString(unknownHash[:20]), Seeders: 1, Leechers: 0,
		}}),
	}

	// Receive the final chunk first. Until both chunks are present, Core must
	// preserve the last known-good active counts.
	partialPayload := encodeSnapshot(t, chunks[1])
	partial, err := repository.ApplySnapshot(ctx, partialPayload, now)
	if err != nil || partial.Applied || partial.Duplicate || partial.Obsolete {
		t.Fatalf("ApplySnapshot(partial) = %+v, %v", partial, err)
	}
	assertSwarmStats(t, ctx, pool, first.publicID, 9, 8, 4)

	completePayload := encodeSnapshot(t, chunks[0])
	complete, err := repository.ApplySnapshot(ctx, completePayload, now)
	if err != nil || !complete.Applied || complete.SnapshotID != snapshotID {
		t.Fatalf("ApplySnapshot(complete) = %+v, %v", complete, err)
	}
	assertSwarmStats(t, ctx, pool, first.publicID, 21, 3, 4)
	// A full scope snapshot means an absent published info hash has no active
	// peers, but its independent lifetime completion count is preserved.
	assertSwarmStats(t, ctx, pool, second.publicID, 0, 0, 2)

	duplicate, err := repository.ApplySnapshot(ctx, completePayload, now)
	if err != nil || !duplicate.Duplicate || duplicate.Applied {
		t.Fatalf("ApplySnapshot(duplicate) = %+v, %v", duplicate, err)
	}
	olderID := mustUUIDV7(t)
	older := newSnapshotChunk(t, olderID, mustUUIDV7(t), sourceID, routingEpoch, 1, observedAt.Add(-time.Minute), 0, 1, []trackerswarmv1.Entry{})
	obsolete, err := repository.ApplySnapshot(ctx, encodeSnapshot(t, older), now)
	if err != nil || !obsolete.Obsolete || obsolete.Applied {
		t.Fatalf("ApplySnapshot(obsolete) = %+v, %v", obsolete, err)
	}

	completion, completionPayload := completionEvent(t, first, mustUUIDV7(t), now.Add(-20*time.Second), "")
	result, err := repository.ApplyCompletion(ctx, completionPayload, now)
	if err != nil || !result.Applied || result.Duplicate {
		t.Fatalf("ApplyCompletion() = %+v, %v", result, err)
	}
	assertSwarmStats(t, ctx, pool, first.publicID, 21, 3, 5)

	retryEventID := mustUUIDV7(t)
	retry := completion
	retry.EventID = retryEventID.String()
	retryPayload, err := trackerannouncev1.Encode(retry)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := repository.ApplyCompletion(ctx, retryPayload, now)
	if err != nil || !retried.Duplicate || retried.Applied {
		t.Fatalf("ApplyCompletion(retry) = %+v, %v", retried, err)
	}
	assertSwarmStats(t, ctx, pool, first.publicID, 21, 3, 5)

	// A later active-count snapshot must not overwrite the independently
	// settled lifetime completion count.
	laterID := mustUUIDV7(t)
	later := newSnapshotChunk(t, laterID, mustUUIDV7(t), sourceID, routingEpoch, 3, observedAt.Add(time.Minute), 0, 1, []trackerswarmv1.Entry{{
		InfoHashV1: hex.EncodeToString(first.infoHash), Seeders: 18, Leechers: 4,
	}})
	if laterResult, err := repository.ApplySnapshot(ctx, encodeSnapshot(t, later), now.Add(time.Minute)); err != nil || !laterResult.Applied {
		t.Fatalf("ApplySnapshot(later) = %+v, %v", laterResult, err)
	}
	assertSwarmStats(t, ctx, pool, first.publicID, 18, 4, 5)

	conflict := completion
	conflict.EventID = mustUUIDV7(t).String()
	conflict.CompletionID = strings.Repeat("ab", sha256.Size)
	conflict.InfoHashV1 = hex.EncodeToString(second.infoHash)
	// The numeric torrent ID and info hash now disagree, so published identity
	// resolution fails closed instead of crediting the wrong catalog row.
	conflictPayload, err := trackerannouncev1.Encode(conflict)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ApplyCompletion(ctx, conflictPayload, now); !errors.Is(err, swarmprojection.ErrInvariant) {
		t.Fatalf("ApplyCompletion(mismatched identity) error = %v", err)
	}
}

type swarmFixture struct {
	userID    uuid.UUID
	torrentID int64
	publicID  uuid.UUID
	infoHash  []byte
}

func insertPublishedSwarmFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label string, now time.Time, seeders, leechers, completed int) swarmFixture {
	t.Helper()
	userID, credentialRef := uuid.New(), uuid.New()
	username := "swarm-it-" + label + "-" + uuid.NewString()[:8]
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.users (
    id, credential_ref, username, display_name, status, email_verified_at,
    created_at, updated_at, password_changed_at
) VALUES ($1, $2, $3, $4, 'active', $5, $5, $5, $5)`,
		userID, credentialRef, username, "Swarm integration "+label, now); err != nil {
		t.Fatalf("insert integration user: %v", err)
	}
	categoryID := "swarm-it-" + label + "-" + uuid.NewString()[:8]
	if _, err := pool.Exec(ctx, `
INSERT INTO catalog.categories (id, name, display_order, enabled, created_at, updated_at)
VALUES ($1, $2, 100000 + abs(hashtext($1)) % 900000, true, $3, $3)`,
		categoryID, "Swarm integration "+label, now); err != nil {
		t.Fatalf("insert integration category: %v", err)
	}
	objectID, publicID := uuid.New(), uuid.New()
	objectDigest := sha256.Sum256([]byte("swarm-object-" + objectID.String()))
	infoDigest := sha256.Sum256([]byte("swarm-info-" + publicID.String()))
	infoHash := append([]byte(nil), infoDigest[:20]...)
	if _, err := pool.Exec(ctx, `
INSERT INTO torrents.torrent_objects (
    id, content_sha256, byte_length, parser_version, validation_profile,
    compatibility_flags, info_offset, info_length, created_at
) VALUES ($1, $2, 256, 'integration-v1', 'strict_upload', ARRAY[]::text[], 0, 128, $3)`,
		objectID, objectDigest[:], now); err != nil {
		t.Fatalf("insert integration object: %v", err)
	}
	var torrentID int64
	if err := pool.QueryRow(ctx, `
INSERT INTO torrents.torrents (
    public_id, uploader_id, category_id, object_id, info_hash_v1,
    content_name, title, subtitle, total_size_bytes, payload_size_bytes,
    file_count, padding_file_count, piece_length_bytes, piece_count,
    state, version, submitted_at, published_at, state_changed_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5,
    'integration.bin', $6, '', 4096, 4096,
    1, 0, 16384, 1,
    'published', 2, $7::timestamptz - interval '1 hour', $7::timestamptz, $7::timestamptz, $7::timestamptz
)
RETURNING id`, publicID, userID, categoryID, objectID, infoHash, "Swarm "+label+" "+publicID.String()[:8], now).Scan(&torrentID); err != nil {
		t.Fatalf("insert integration torrent: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO catalog.torrents (
    id, category_id, name, subtitle, size_bytes, promotion, published_at, created_at
) VALUES ($1, $2, $3, '', 4096, 'none', $4, $4)`, publicID.String(), categoryID, "Swarm "+label, now); err != nil {
		t.Fatalf("insert integration catalog row: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO catalog.torrent_swarm_stats (torrent_id, seeders, leechers, completed, observed_at)
VALUES ($1, $2, $3, $4, $5)`, publicID.String(), seeders, leechers, completed, now.Add(-time.Minute)); err != nil {
		t.Fatalf("insert integration swarm stats: %v", err)
	}
	// Deliberately omit catalog.torrent_completion_stats. This models a legacy
	// catalog import that occurs after the migration backfill and proves that
	// the first new completion continues from, rather than overwrites, history.
	return swarmFixture{userID: userID, torrentID: torrentID, publicID: publicID, infoHash: infoHash}
}

func newSnapshotChunk(t *testing.T, snapshotID, eventID uuid.UUID, sourceID string, routingEpoch, sequence int64, observedAt time.Time, index, count int32, entries []trackerswarmv1.Entry) trackerswarmv1.SnapshotChunk {
	t.Helper()
	return trackerswarmv1.SnapshotChunk{
		SchemaVersion: trackerswarmv1.SchemaVersion, EventID: eventID.String(), SnapshotID: snapshotID.String(),
		SourceID: sourceID, RoutingEpoch: routingEpoch, SnapshotSequence: sequence,
		ObservedAt: observedAt.UTC().Round(0), Scope: trackerswarmv1.ScopeAll,
		ChunkIndex: index, ChunkCount: count, Entries: entries,
	}
}

func encodeSnapshot(t *testing.T, chunk trackerswarmv1.SnapshotChunk) []byte {
	t.Helper()
	payload, err := trackerswarmv1.Encode(chunk)
	if err != nil {
		t.Fatalf("Encode(snapshot) error = %v", err)
	}
	return payload
}

func completionEvent(t *testing.T, fixture swarmFixture, eventID uuid.UUID, receivedAt time.Time, completionID string) (trackerannouncev1.Event, []byte) {
	t.Helper()
	if completionID == "" {
		digest := sha256.Sum256([]byte("completion-" + fixture.publicID.String()))
		completionID = hex.EncodeToString(digest[:])
	}
	event := trackerannouncev1.Event{
		SchemaVersion: trackerannouncev1.SchemaVersion, EventID: eventID.String(), ReceivedAt: receivedAt.UTC().Round(0),
		UserID: fixture.userID.String(), TorrentID: fixture.torrentID, InfoHashV1: hex.EncodeToString(fixture.infoHash),
		SessionToken: strings.Repeat("12", sha256.Size), CompletionID: completionID,
		AddressFamily: 4, Event: "completed", Uploaded: 1024, Downloaded: 4096, Left: 0,
		CredentialVersion: 1, TorrentControlSequence: 1, SubjectControlSequence: 1,
	}
	payload, err := trackerannouncev1.Encode(event)
	if err != nil {
		t.Fatalf("Encode(completion) error = %v", err)
	}
	return event, payload
}

func assertSwarmStats(t *testing.T, ctx context.Context, pool *pgxpool.Pool, publicID uuid.UUID, wantSeeders, wantLeechers, wantCompleted int) {
	t.Helper()
	var seeders, leechers, completed int
	if err := pool.QueryRow(ctx, `
SELECT seeders, leechers, completed
FROM catalog.torrent_swarm_stats
WHERE torrent_id = $1`, publicID.String()).Scan(&seeders, &leechers, &completed); err != nil {
		t.Fatalf("read swarm stats for %s: %v", publicID, err)
	}
	if seeders != wantSeeders || leechers != wantLeechers || completed != wantCompleted {
		t.Fatalf("swarm stats for %s = %d/%d/%d; want %d/%d/%d", publicID, seeders, leechers, completed, wantSeeders, wantLeechers, wantCompleted)
	}
}

func mustUUIDV7(t *testing.T) uuid.UUID {
	t.Helper()
	value, err := uuid.NewV7()
	if err != nil {
		t.Fatal(fmt.Errorf("generate UUIDv7: %w", err))
	}
	return value
}
