package legacytorrents

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	platformobjectstore "github.com/peergo/peergo/services/core/internal/platform/objectstore"
)

func TestBuildAndStoreTorrentImportRecordUsesOriginalBytes(t *testing.T) {
	t.Parallel()

	raw := []byte("d4:infod6:lengthi3e4:name5:a.bin12:piece lengthi16384e6:pieces20:aaaaaaaaaaaaaaaaaaaa7:privatei1eee")
	parsed, err := torrents.InspectLegacyV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	publicID := uuid.MustParse("12345678-2222-4333-8444-555555555555")
	rootPath := t.TempDir()
	objectDirectory := filepath.Join(rootPath, "12")
	if err := os.Mkdir(objectDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(objectDirectory, publicID.String()+".torrent"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := openSourceObjectRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC)
	uploaderID := uuid.MustParse("aaaaaaaa-2222-4333-8444-555555555555")
	source := sourceTorrent{
		LegacyID: 1, LegacyUUID: publicID.String(), InfoHash: parsed.InfoHashV1.Hex(),
		Title: "Release", SourceCategory: "movie", Attributes: "{}",
		SizeBytes: 3, UploaderLegacyID: 7, Status: "approved",
		PromotionType: 1, PromotionTimeType: 0,
		GroupExternalIDs: "{}", CreatedAt: now, UpdatedAt: now,
	}
	manifest, err := newSourceFileManifest(1, []sourceFile{{LegacyID: 1, Path: "a.bin", Size: 3}})
	if err != nil {
		t.Fatal(err)
	}
	record, err := buildTorrentImportRecord(
		root, source, manifest, map[int64]uuid.UUID{7: uploaderID}, legacyVocabulary{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if record.sourceObjectID != publicID || record.uploaderID != uploaderID ||
		record.parsed.ObjectSHA256 != parsed.ObjectSHA256 || string(record.raw) != string(raw) {
		t.Fatalf("import record does not retain the audited immutable identity")
	}

	backendID, err := torrents.ParseStorageBackendID("legacy-test")
	if err != nil {
		t.Fatal(err)
	}
	store, err := platformobjectstore.NewFilesystem(backendID, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key, versionID, err := storeAndVerifyLegacyObject(
		context.Background(), store, record.raw, record.parsed, record.source.LegacyID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if key != torrents.TorrentObjectKey(parsed.ObjectSHA256) || versionID != "" {
		t.Fatalf("stored object = key %q version %q", key, versionID)
	}
	if _, _, err := storeAndVerifyLegacyObject(
		context.Background(), store, record.raw, record.parsed, record.source.LegacyID,
	); err != nil {
		t.Fatalf("idempotent object store retry: %v", err)
	}
}
