package legacytorrents

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCutoverAcceptanceManifestRoundTripAndNoOverwrite(t *testing.T) {
	now := time.Date(2026, 8, 11, 2, 3, 4, 0, time.UTC)
	preflight := CutoverPreflightReport{
		Schema: CutoverPreflightSchema, CheckedAt: now.Add(-time.Hour),
		RunID:   uuid.MustParse("cccccccc-2222-4333-8444-555555555555"),
		RunMode: "new", RunState: "not_started", SourceSystem: "ptyes",
		MappingVersion: "ptyes-v1", OccurredAt: now.Add(-2 * time.Hour), Ready: true,
	}
	directory := t.TempDir()
	preflightPath := filepath.Join(directory, "preflight.json")
	preflightDigest, err := WriteCutoverPreflightManifest(preflightPath, preflight)
	if err != nil {
		t.Fatal(err)
	}
	loaded, loadedDigest, err := LoadCutoverPreflightManifest(preflightPath)
	if err != nil || loaded.RunID != preflight.RunID || loadedDigest != preflightDigest {
		t.Fatalf("LoadCutoverPreflightManifest() = %+v, %x, %v", loaded, loadedDigest, err)
	}
	if err := os.Chmod(preflightPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadCutoverPreflightManifest(preflightPath); err == nil {
		t.Fatal("preflight loader accepted an operator artifact readable by other users")
	}

	report := CutoverAcceptanceReport{
		Schema: CutoverAcceptanceSchema, CheckedAt: now, RunID: preflight.RunID,
		RunState: "reconciled", CoreRuntimeDefaultsReady: true,
		LegacyMemberAuthorizationReady: true, ReadyToActivate: true,
		Users: 1, AttendanceOpenings: 1,
		SeedboxPolicySequence:            1,
		SeedboxUploadFactorBasisPoints:   5_000,
		SeedboxDownloadFactorBasisPoints: 20_000,
		StandardSpeedLimitBytesPerSecond: 25 * 1024 * 1024,
	}
	acceptancePath := filepath.Join(directory, "acceptance.json")
	digest, err := WriteCutoverAcceptanceManifest(acceptancePath, report)
	if err != nil || digest == ([sha256.Size]byte{}) {
		t.Fatalf("WriteCutoverAcceptanceManifest() = %x, %v", digest, err)
	}
	raw, err := os.ReadFile(acceptancePath)
	observedDigest := sha256.Sum256(raw)
	if err != nil || hex.EncodeToString(digest[:]) != hex.EncodeToString(observedDigest[:]) {
		t.Fatalf("acceptance digest mismatch: %x, %v", digest, err)
	}
	info, err := os.Stat(acceptancePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("acceptance mode = %v", info.Mode().Perm())
	}
	if _, err := WriteCutoverAcceptanceManifest(acceptancePath, report); err == nil {
		t.Fatal("acceptance manifest overwrote existing evidence")
	}
	report.AttendanceOpenings = 0
	if _, err := WriteCutoverAcceptanceManifest(filepath.Join(directory, "missing-attendance.json"), report); err == nil {
		t.Fatal("acceptance writer accepted missing attendance openings")
	}
	report.AttendanceOpenings = 1
	report.ReadyToActivate = false
	if _, err := WriteCutoverAcceptanceManifest(filepath.Join(directory, "not-ready.json"), report); err == nil {
		t.Fatal("acceptance writer persisted a report that was not ready")
	}
}

func TestRequireAcceptedSnapshotTime(t *testing.T) {
	checkedAt := time.Date(2026, 8, 11, 2, 0, 0, 0, time.UTC)
	reconciledAt := checkedAt.Add(-time.Minute)
	if err := requireAcceptedSnapshotTime(
		checkedAt.Add(-30*time.Second), reconciledAt, checkedAt, time.Minute, time.Second,
	); err != nil {
		t.Fatal(err)
	}
	for _, generatedAt := range []time.Time{
		reconciledAt.Add(-time.Nanosecond),
		checkedAt.Add(2 * time.Second),
		checkedAt.Add(-2 * time.Minute),
	} {
		if err := requireAcceptedSnapshotTime(
			generatedAt, reconciledAt, checkedAt, time.Minute, time.Second,
		); err == nil {
			t.Fatalf("accepted invalid snapshot time %s", generatedAt)
		}
	}
}

func TestCompareAcceptedCountsRequiresDrainedExactProjection(t *testing.T) {
	source := acceptedSourceCounts{
		Torrents: 2, Files: 3, FacetValues: 4, ExternalIdentifiers: 1,
		Groups: 1, GroupExternalIdentifiers: 2, Published: 1,
	}
	target := acceptedTargetCounts{
		Torrents: 2, Objects: 2, Locations: 2, Files: 3, FacetValues: 4,
		ExternalIdentifiers: 1, Groups: 1, GroupMappings: 1,
		GroupExternalIdentifiers: 2, Published: 1, CatalogRows: 1,
		OutboxRows: 1, Allowlisted: 1, ProjectionSequence: 1, OutboxSequence: 1,
		SiteProfiles: 1, RegistrationPolicies: 1,
	}
	if err := compareAcceptedCounts(source, target); err != nil {
		t.Fatal(err)
	}
	target.PendingOutbox = 1
	if err := compareAcceptedCounts(source, target); err == nil {
		t.Fatal("acceptance allowed a pending Tracker control event")
	}
	target.PendingOutbox = 0
	target.SiteProfiles = 0
	if err := compareAcceptedCounts(source, target); err == nil {
		t.Fatal("acceptance allowed missing Core runtime defaults")
	}
}

func TestReportCutoverAcceptanceProgressIsBoundedAndKeepsFinalUpdate(t *testing.T) {
	var observed []CutoverAcceptanceProgress
	for processed := int64(1); processed <= 501; processed++ {
		reportCutoverAcceptanceProgress(
			func(progress CutoverAcceptanceProgress) {
				observed = append(observed, progress)
			},
			"image_objects",
			processed,
			501,
			250,
		)
	}
	want := []int64{250, 500, 501}
	if len(observed) != len(want) {
		t.Fatalf("progress updates = %+v, want processed=%v", observed, want)
	}
	for index, processed := range want {
		if observed[index].Phase != "image_objects" ||
			observed[index].Processed != processed ||
			observed[index].Expected != 501 {
			t.Fatalf("progress[%d] = %+v", index, observed[index])
		}
	}
}

func TestBindAcceptanceToPreflightRejectsChangedImageArchive(t *testing.T) {
	now := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	runID := uuid.MustParse("cccccccc-2222-4333-8444-555555555555")
	databaseSHA := sha256.Sum256([]byte("database"))
	torrentSHA := sha256.Sum256([]byte("torrents"))
	imageSHA := sha256.Sum256([]byte("images"))
	storageSHA := sha256.Sum256([]byte("storage"))
	sourceIdentity := cutoverDatabaseIdentity{fingerprint: sha256.Sum256([]byte("source"))}
	coreIdentity := cutoverDatabaseIdentity{fingerprint: sha256.Sum256([]byte("core"))}
	vaultIdentity := cutoverDatabaseIdentity{fingerprint: sha256.Sum256([]byte("vault"))}
	config := CutoverAcceptanceConfig{
		Inventory:  InventoryConfig{RunID: runID, SnapshotSHA256: databaseSHA, MappingVersion: "ptyes-v1"},
		OccurredAt: now.Add(-time.Hour), DatabaseDumpBytes: 100,
		TorrentArchiveSHA256: torrentSHA, TorrentArchiveBytes: 200, TorrentArchiveObjects: 2,
		ImageArchiveSHA256: imageSHA, ImageArchiveBytes: 300, ImageArchiveObjects: 3,
		StorageBackendID: "content-primary", StorageDriver: "filesystem", StorageConfigSHA256: storageSHA,
	}
	config.Preflight = CutoverPreflightReport{
		Schema: CutoverPreflightSchema, CheckedAt: now.Add(-time.Minute), Ready: true,
		RunID: runID, MappingVersion: "ptyes-v1", OccurredAt: config.OccurredAt,
		DatabaseDumpSHA256: hex.EncodeToString(databaseSHA[:]), DatabaseDumpBytes: 100,
		TorrentArchiveSHA256: hex.EncodeToString(torrentSHA[:]), TorrentArchiveBytes: 200,
		TorrentArchiveObjects: 2, ImageArchiveSHA256: hex.EncodeToString(imageSHA[:]),
		ImageArchiveBytes: 300, ImageArchiveObjects: 3,
		StorageBackendID: "content-primary", StorageDriver: "filesystem",
		StorageConfigSHA256: hex.EncodeToString(storageSHA[:]), ExpectedUsers: 4, ExpectedTorrents: 5,
		SourceDatabase: CutoverDatabaseReport{IdentitySHA256: hex.EncodeToString(sourceIdentity.fingerprint[:])},
		CoreDatabase:   CutoverDatabaseReport{IdentitySHA256: hex.EncodeToString(coreIdentity.fingerprint[:])},
		VaultDatabase:  CutoverDatabaseReport{IdentitySHA256: hex.EncodeToString(vaultIdentity.fingerprint[:])},
	}
	if err := bindAcceptanceToPreflight(
		config, now, sourceIdentity, coreIdentity, vaultIdentity, 4, 5,
	); err != nil {
		t.Fatal(err)
	}
	config.ImageArchiveSHA256 = sha256.Sum256([]byte("changed-images"))
	if err := bindAcceptanceToPreflight(
		config, now, sourceIdentity, coreIdentity, vaultIdentity, 4, 5,
	); err == nil {
		t.Fatal("acceptance accepted an image archive that differed from preflight")
	}
}
