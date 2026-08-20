package torrents

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func TestPostgresTorrentDownloadLoadsOnlyPublishedVerifiedEvidence(t *testing.T) {
	databaseURL := os.Getenv("PEERGO_TEST_CORE_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PEERGO_TEST_CORE_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	if err := platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		t.Fatalf("RequireCurrentMigration() error = %v", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin rollback-only integration transaction: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	now := time.Now().UTC().Truncate(time.Microsecond)
	userID, credentialRef := uuid.New(), uuid.New()
	categoryID := "download-it-" + uuid.NewString()[:8]
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.users (
    id, credential_ref, username, display_name, status, created_at, updated_at
) VALUES ($1, $2, $3, '下载仓储集成测试', 'active', $4, $4)`,
		userID, credentialRef, "download-it-"+userID.String()[:8], now,
	); err != nil {
		t.Fatalf("insert integration user: %v", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO catalog.categories (id, name, display_order, enabled, created_at, updated_at)
VALUES ($1, '下载仓储集成测试', 100000 + abs(hashtext($1)) % 900000, true, $2, $2)`, categoryID, now); err != nil {
		t.Fatalf("insert integration category: %v", err)
	}

	rawMetainfo := validSingleFixture("download-integration.bin", 42, 16*1024)
	parsed := mustParseV1(t, rawMetainfo, ValidationProfileStrictUpload)
	digest := sha256.Sum256(rawMetainfo)
	objectID := uuid.New()
	if _, err := tx.Exec(ctx, `
INSERT INTO torrents.torrent_objects (
    id, content_sha256, byte_length, parser_version, validation_profile,
    compatibility_flags, info_offset, info_length, created_at
) VALUES ($1, $2, $3, 'integration-v1', 'strict_upload', ARRAY[]::text[], $4, $5, $6)`,
		objectID, digest[:], len(rawMetainfo), parsed.InfoOffset, parsed.InfoLength, now,
	); err != nil {
		t.Fatalf("insert integration torrent object: %v", err)
	}
	objectKey := TorrentObjectKey(ObjectSHA256(digest))
	preferredID, retiringID := uuid.New(), uuid.New()
	if _, err := tx.Exec(ctx, `
INSERT INTO torrents.torrent_object_locations (
    id, object_id, backend_id, object_key, state, is_preferred,
    observed_byte_length, observed_sha256, verified_at, retiring_at,
    created_at, updated_at
) VALUES
    ($1, $3, 'local-primary', $4, 'verified', true, $5, $6, $7, NULL, $7, $7),
    ($2, $3, 's3-fallback', $4, 'retiring', false, $5, $6, $7, $7, $7, $7)`,
		preferredID, retiringID, objectID, objectKey, len(rawMetainfo), digest[:], now,
	); err != nil {
		t.Fatalf("insert integration object locations: %v", err)
	}
	var torrentID TorrentID
	if err := tx.QueryRow(ctx, `
INSERT INTO torrents.torrents (
    uploader_id, category_id, object_id, info_hash_v1,
    content_name, title, subtitle, total_size_bytes, payload_size_bytes,
    file_count, padding_file_count, piece_length_bytes, piece_count,
    state, version, submitted_at, published_at, state_changed_at, updated_at
) VALUES (
    $1, $2, $3, $4,
    'download-integration.bin', 'Integration Download', '', 42, 42,
    1, 0, 16384, 1,
    'published', 2, $5, $6, $6, $6
)
RETURNING id`, userID, categoryID, objectID, parsed.InfoHashV1[:], now.Add(-time.Hour), now).Scan(&torrentID); err != nil {
		t.Fatalf("insert published integration torrent: %v", err)
	}

	repository, err := NewPostgresTorrentDownloadRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	// Use the transaction-bound generated queries so the repository can see the
	// rollback-only fixture before it is committed.
	repository.queries = repository.queries.WithTx(tx)
	source, err := repository.PublishedDownloadSource(ctx, torrentID)
	if err != nil {
		t.Fatalf("PublishedDownloadSource() error = %v", err)
	}
	if source.TorrentID != torrentID || source.ObjectID != objectID || source.Title != "Integration Download" ||
		source.Descriptor.SHA256 != ObjectSHA256(digest) || source.InfoOffset != parsed.InfoOffset || source.InfoLength != parsed.InfoLength ||
		len(source.Locations) != 2 || source.Locations[0].ID != preferredID || !source.Locations[0].Preferred || source.Locations[1].ID != retiringID {
		t.Fatalf("published download source = %+v", source)
	}
	if _, err := repository.PublishedDownloadSource(ctx, torrentID+1_000_000); !errors.Is(err, ErrTorrentDownloadNotFound) {
		t.Fatalf("missing download source error = %v", err)
	}
}
