package legacytorrents

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestInspectCutoverInputsAndWriteManifest(t *testing.T) {
	publicID := uuid.MustParse("12345678-2222-4333-8444-555555555555")
	archivePath := writeSourceObjectArchive(t, []sourceArchiveEntry{{
		name: "12/" + publicID.String() + ".torrent", mode: 0o600,
		content: []byte("d4:infod6:lengthi1e4:name1:a12:piece lengthi1e6:pieces20:aaaaaaaaaaaaaaaaaaaa7:privatei1eee"),
	}})
	archiveSHA, archiveBytes, archiveObjects, err := InspectCutoverArchive(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if archiveSHA == ([sha256.Size]byte{}) || archiveBytes < 1 || archiveObjects != 1 {
		t.Fatalf("archive inspection = %x, %d, %d", archiveSHA, archiveBytes, archiveObjects)
	}

	dumpPath := filepath.Join(t.TempDir(), "rousi.sql.gz")
	dump, err := os.OpenFile(dumpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	compressed := gzip.NewWriter(dump)
	if _, err := compressed.Write([]byte("synthetic pg_dump")); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := dump.Close(); err != nil {
		t.Fatal(err)
	}
	dumpSHA, dumpBytes, err := InspectCutoverDatabaseDump(dumpPath)
	if err != nil || dumpSHA == ([sha256.Size]byte{}) || dumpBytes < 1 {
		t.Fatalf("dump inspection = %x, %d, %v", dumpSHA, dumpBytes, err)
	}

	now := time.Date(2026, 8, 11, 1, 2, 3, 0, time.UTC)
	report := CutoverPreflightReport{
		Schema: CutoverPreflightSchema, CheckedAt: now,
		RunID:   uuid.MustParse("cccccccc-2222-4333-8444-555555555555"),
		RunMode: "new", RunState: "not_started", SourceSystem: "ptyes",
		MappingVersion: "ptyes-v1", OccurredAt: now, Ready: true,
	}
	output := filepath.Join(t.TempDir(), "preflight.json")
	digest, err := WriteCutoverPreflightManifest(output, report)
	if err != nil || digest == ([sha256.Size]byte{}) {
		t.Fatalf("WriteCutoverPreflightManifest() = %x, %v", digest, err)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var decoded CutoverPreflightReport
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded.RunID != report.RunID || !decoded.Ready {
		t.Fatalf("decoded manifest = %+v, %v", decoded, err)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("manifest mode = %v", info.Mode().Perm())
	}
	if _, err := WriteCutoverPreflightManifest(output, report); err == nil {
		t.Fatal("preflight manifest overwrote an existing audit artifact")
	}
	if _, err := WriteCutoverPreflightManifest("relative.json", report); err == nil {
		t.Fatal("preflight manifest accepted a relative output path")
	}
}
