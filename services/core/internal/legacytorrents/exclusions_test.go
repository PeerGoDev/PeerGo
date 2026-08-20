package legacytorrents

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

func TestTorrentExclusionManifestIsStrictAndSnapshotBound(t *testing.T) {
	t.Parallel()

	snapshot := sha256.Sum256([]byte("snapshot"))
	content := fmt.Sprintf(
		"%s\nsnapshot_sha256\t%x\n%s\n7\t11111111-2222-4333-8444-555555555555\t1111111111111111111111111111111111111111\t42\t%s\n",
		torrentExclusionVersion, snapshot, torrentExclusionColumns, torrentExclusionReason,
	)
	path := filepath.Join(t.TempDir(), "exclusions.tsv")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := LoadTorrentExclusionManifest(path, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Len() != 1 || manifest.ContentSHA256() != sha256.Sum256([]byte(content)) {
		t.Fatalf("manifest = %+v", manifest)
	}
	source := sourceTorrent{
		LegacyID: 7, LegacyUUID: "11111111-2222-4333-8444-555555555555",
		InfoHash: "1111111111111111111111111111111111111111", SizeBytes: 42,
	}
	if !manifest.match(source) {
		t.Fatal("manifest did not match its exact source identity")
	}
	first, err := manifest.objectFingerprint(source)
	if err != nil || first == ([sha256.Size]byte{}) {
		t.Fatalf("object fingerprint = %x, %v", first, err)
	}
	source.SizeBytes++
	if manifest.match(source) {
		t.Fatal("manifest matched a changed source identity")
	}

	differentSnapshot := sha256.Sum256([]byte("different"))
	if _, err := LoadTorrentExclusionManifest(path, differentSnapshot); err == nil {
		t.Fatal("manifest for a different snapshot was accepted")
	}
}

func TestTorrentExclusionManifestRejectsUnsortedOrBroadRows(t *testing.T) {
	t.Parallel()

	snapshot := sha256.Sum256([]byte("snapshot"))
	tests := []string{
		fmt.Sprintf(
			"%s\nsnapshot_sha256\t%x\n%s\n7\t11111111-2222-4333-8444-555555555555\t1111111111111111111111111111111111111111\t42\tany_error\n",
			torrentExclusionVersion, snapshot, torrentExclusionColumns,
		),
		fmt.Sprintf(
			"%s\nsnapshot_sha256\t%x\n%s\n8\t11111111-2222-4333-8444-555555555555\t1111111111111111111111111111111111111111\t42\t%s\n7\t22222222-2222-4333-8444-555555555555\t2222222222222222222222222222222222222222\t43\t%s\n",
			torrentExclusionVersion, snapshot, torrentExclusionColumns, torrentExclusionReason, torrentExclusionReason,
		),
	}
	for index, content := range tests {
		path := filepath.Join(t.TempDir(), fmt.Sprintf("exclusions-%d.tsv", index))
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadTorrentExclusionManifest(path, snapshot); err == nil {
			t.Fatalf("invalid manifest %d was accepted", index)
		}
	}
}

func TestRenderTorrentExclusionManifestRoundTripsAndPrivateWriterDoesNotOverwrite(t *testing.T) {
	t.Parallel()

	snapshot := sha256.Sum256([]byte("snapshot"))
	infoHash, err := torrents.ParseInfoHashV1Hex("1111111111111111111111111111111111111111")
	if err != nil {
		t.Fatal(err)
	}
	raw := renderTorrentExclusionManifest(snapshot, []torrentExclusion{{
		legacyID: 7, publicID: uuid.MustParse("11111111-2222-4333-8444-555555555555"),
		infoHash: infoHash, size: 42,
	}})
	parsed, err := parseTorrentExclusionManifest(raw, snapshot)
	if err != nil || parsed.Len() != 1 {
		t.Fatalf("parsed rendered manifest = %+v, %v", parsed, err)
	}
	path := filepath.Join(t.TempDir(), "candidate.tsv")
	if err := writeNewPrivateFile(path, raw); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("candidate mode = %v, %v", info, err)
	}
	if err := writeNewPrivateFile(path, []byte("replacement")); err == nil {
		t.Fatal("candidate writer overwrote an existing file")
	}
	stored, err := os.ReadFile(path)
	if err != nil || string(stored) != string(raw) {
		t.Fatalf("stored candidate changed = %q, %v", stored, err)
	}
}
