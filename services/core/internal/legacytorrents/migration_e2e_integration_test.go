package legacytorrents

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
	"github.com/peergo/peergo/services/core/internal/legacymedia"
	"github.com/peergo/peergo/services/core/internal/legacypersonalstate"
	"github.com/peergo/peergo/services/core/internal/legacyseedboxes"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	"github.com/peergo/peergo/services/core/internal/modules/trackercontrol"
	platformobjectstore "github.com/peergo/peergo/services/core/internal/platform/objectstore"
	"github.com/peergo/peergo/services/core/internal/platform/trackersnapshot"
)

func TestLegacyTorrentMigrationEndToEnd(t *testing.T) {
	sourceURL := os.Getenv("PEERGO_TEST_LEGACY_SOURCE_DATABASE_URL")
	coreURL := os.Getenv("PEERGO_TEST_LEGACY_CORE_DATABASE_URL")
	vaultURL := os.Getenv("PEERGO_TEST_LEGACY_VAULT_DATABASE_URL")
	if sourceURL == "" || coreURL == "" || vaultURL == "" {
		t.Skip("legacy torrent migration integration database URLs are not configured")
	}
	ctx := context.Background()
	source, err := pgxpool.New(ctx, sourceURL)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	core, err := pgxpool.New(ctx, coreURL)
	if err != nil {
		t.Fatal(err)
	}
	defer core.Close()
	vault, err := pgxpool.New(ctx, vaultURL)
	if err != nil {
		t.Fatal(err)
	}
	defer vault.Close()

	raw := []byte("d4:infod6:lengthi3e4:name5:a.bin12:piece lengthi16384e6:pieces20:aaaaaaaaaaaaaaaaaaaa7:privatei1eee")
	parsed, err := torrents.InspectLegacyV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	publicID := uuid.MustParse("12345678-2222-4333-8444-555555555555")
	imageID := uuid.MustParse("aaaaaaaa-1111-4111-8111-111111111111")
	userID := uuid.MustParse("aaaaaaaa-2222-4333-8444-555555555555")
	credentialRef := uuid.MustParse("bbbbbbbb-2222-4333-8444-555555555555")
	runID := uuid.MustParse("cccccccc-2222-4333-8444-555555555555")
	now := time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC)
	snapshot := sha256.Sum256([]byte("synthetic-ptyes-snapshot"))

	createSyntheticPtYesSource(t, ctx, source, publicID, imageID, parsed, now)
	seedSyntheticMigrationRun(t, ctx, core, runID, userID, credentialRef, snapshot, now)
	seedSyntheticVaultCredential(t, ctx, vault, credentialRef, now)

	readOnlySource := openReadOnlyMigrationPool(t, ctx, sourceURL)
	defer readOnlySource.Close()
	readOnlyCore := openReadOnlyMigrationPool(t, ctx, coreURL)
	defer readOnlyCore.Close()
	readOnlyVault := openReadOnlyMigrationPool(t, ctx, vaultURL)
	defer readOnlyVault.Close()
	storageConfigSHA256 := sha256.Sum256([]byte("synthetic-legacy-e2e-storage"))
	archiveSHA256 := sha256.Sum256(raw)
	imageBytes, err := base64.StdEncoding.DecodeString("R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==")
	if err != nil {
		t.Fatal(err)
	}
	imageArchivePath := writeSourceObjectArchive(t, []sourceArchiveEntry{{
		name: "uploads/images/aa/" + imageID.String() + ".gif", mode: 0o600, content: imageBytes,
	}})
	imageArchive, err := legacymedia.InspectSourceArchive(imageArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	preflight, err := InspectCutoverPreflight(
		ctx,
		readOnlySource,
		readOnlyCore,
		readOnlyVault,
		CutoverPreflightConfig{
			Inventory: InventoryConfig{
				RunID: runID, SnapshotSHA256: snapshot, MappingVersion: "ptyes-v1",
			},
			OccurredAt: now, CheckedAt: now.Add(time.Minute), DatabaseDumpBytes: 1024,
			TorrentArchiveSHA256: archiveSHA256, TorrentArchiveBytes: int64(len(raw)),
			TorrentArchiveObjects: 1, StorageBackendID: "legacy-e2e",
			ImageArchiveSHA256: imageArchive.SHA256, ImageArchiveBytes: imageArchive.ByteLength,
			ImageArchiveObjects: imageArchive.ImageCount,
			StorageDriver:       "filesystem", StorageConfigSHA256: storageConfigSHA256,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !preflight.Ready || preflight.RunMode != "resume" || preflight.RunState != "importing" ||
		preflight.ExpectedUsers != 1 || preflight.ExpectedTorrents != 1 {
		t.Fatalf("cutover preflight = %+v", preflight)
	}

	torrentRoot := t.TempDir()
	directory := filepath.Join(torrentRoot, publicID.String()[:2])
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, publicID.String()+".torrent"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	inventory := InventoryConfig{RunID: runID, SnapshotSHA256: snapshot, MappingVersion: "ptyes-v1"}
	validated, err := ValidateObjects(ctx, source, core, ObjectValidationConfig{
		Inventory: inventory, TorrentRoot: torrentRoot, OccurredAt: now,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if validated.ValidatedTorrents != 1 || validated.ValidatedObjects != 1 {
		t.Fatalf("validation result = %+v", validated)
	}

	backendID, err := torrents.ParseStorageBackendID("legacy-e2e")
	if err != nil {
		t.Fatal(err)
	}
	store, err := platformobjectstore.NewFilesystem(backendID, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	imported, err := Import(ctx, source, core, ImportConfig{
		Inventory: inventory, TorrentRoot: torrentRoot, OccurredAt: now, Store: store,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if imported.ImportedTorrents != 1 || imported.SkippedTorrents != 0 || imported.PublishedTorrents != 1 {
		t.Fatalf("initial import result = %+v", imported)
	}
	retried, err := Import(ctx, source, core, ImportConfig{
		Inventory: inventory, TorrentRoot: torrentRoot, OccurredAt: now, Store: store,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if retried.ImportedTorrents != 0 || retried.SkippedTorrents != 1 {
		t.Fatalf("retry import result = %+v", retried)
	}
	purchases, err := ImportPurchases(ctx, source, core, PurchaseImportConfig{
		Inventory: inventory, ImportedAt: now.Add(10 * time.Minute),
	}, nil)
	if err != nil || purchases.SourceRows != 1 || purchases.PriceOpenings != 1 || purchases.Entitlements != 1 {
		t.Fatalf("import legacy purchases = %+v, %v", purchases, err)
	}
	purchaseRetry, err := ImportPurchases(ctx, source, core, PurchaseImportConfig{
		Inventory: inventory, ImportedAt: now.Add(10 * time.Minute),
	}, nil)
	if err != nil || purchaseRetry.SourceRows != 1 || purchaseRetry.PriceOpenings != 0 || purchaseRetry.Entitlements != 0 {
		t.Fatalf("retry legacy purchases = %+v, %v", purchaseRetry, err)
	}
	mediaInventory := legacymedia.InventoryConfig{
		RunID: runID, SnapshotSHA256: snapshot, MappingVersion: "ptyes-v1",
	}
	mediaValidated, err := legacymedia.Validate(ctx, source, core, legacymedia.ValidationConfig{
		Inventory: mediaInventory, ImageArchive: imageArchivePath, ArchiveSHA256: imageArchive.SHA256,
		OccurredAt: now,
	}, nil)
	if err != nil || mediaValidated.ImportableImages != 1 {
		t.Fatalf("validate legacy images = %+v, %v", mediaValidated, err)
	}
	registry, err := objectstorage.NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	mediaImported, err := legacymedia.Import(ctx, source, core, registry, legacymedia.ImportConfig{
		Inventory: mediaInventory, ImageArchive: imageArchivePath, ArchiveSHA256: imageArchive.SHA256,
		OccurredAt: now, BackendID: store.BackendID(),
	}, nil)
	if err != nil || mediaImported.ImportedImages != 1 {
		t.Fatalf("import legacy images = %+v, %v", mediaImported, err)
	}
	mediaVerified, err := legacymedia.Import(ctx, source, core, registry, legacymedia.ImportConfig{
		Inventory: mediaInventory, ImageArchive: imageArchivePath, ArchiveSHA256: imageArchive.SHA256,
		OccurredAt: now, BackendID: store.BackendID(), VerifyOnly: true,
	}, nil)
	if err != nil || mediaVerified.VerifiedImages != 1 {
		t.Fatalf("verify legacy images = %+v, %v", mediaVerified, err)
	}
	if _, err := legacymedia.Reconcile(ctx, core, mediaVerified); err != nil {
		t.Fatal(err)
	}
	reconciled, err := ReconcileMigration(
		ctx,
		source,
		core,
		vault,
		ReconciliationConfig{
			Inventory: inventory, ReconciledAt: now.Add(time.Hour),
			BackendID: string(store.BackendID()),
		},
		retried,
	)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.State != "reconciled" || reconciled.Users != 1 || reconciled.VaultCredentials != 1 ||
		reconciled.Torrents != 1 || reconciled.TorrentObjects != 1 || reconciled.TorrentFiles != 1 ||
		reconciled.PurchaseRows != 1 || reconciled.PurchaseRights != 1 || reconciled.PurchaseEvidence != 0 {
		t.Fatalf("reconciliation result = %+v", reconciled)
	}
	seedboxResult, err := legacyseedboxes.Import(ctx, source, core, legacyseedboxes.Config{
		RunID: runID, SnapshotSHA256: snapshot, MappingVersion: "ptyes-v1",
		ImportedAt: now.Add(65 * time.Minute),
	})
	if err != nil || seedboxResult.SourceRows != 1 || seedboxResult.EnabledRows != 1 ||
		seedboxResult.BindingRows != 1 || seedboxResult.PolicySequence < 1 {
		t.Fatalf("import legacy seedboxes = %+v, %v", seedboxResult, err)
	}
	seedboxRetry, err := legacyseedboxes.Import(ctx, source, core, legacyseedboxes.Config{
		RunID: runID, SnapshotSHA256: snapshot, MappingVersion: "ptyes-v1",
		ImportedAt: now.Add(65 * time.Minute),
	})
	if err != nil || !seedboxRetry.Duplicate || seedboxRetry.PolicySequence != seedboxResult.PolicySequence {
		t.Fatalf("retry legacy seedboxes = %+v, %v", seedboxRetry, err)
	}
	personalState, err := legacypersonalstate.Import(ctx, source, core, legacypersonalstate.Config{
		RunID: runID, SnapshotSHA256: snapshot, MappingVersion: "ptyes-v1",
		ImportedAt: now.Add(66 * time.Minute),
	}, nil)
	if err != nil || personalState.BookmarkSourceRows != 1 ||
		personalState.BookmarkAppliedRows != 1 || personalState.InvitationSourceRows != 0 {
		t.Fatalf("import legacy personal state = %+v, %v", personalState, err)
	}
	personalStateRetry, err := legacypersonalstate.Import(ctx, source, core, legacypersonalstate.Config{
		RunID: runID, SnapshotSHA256: snapshot, MappingVersion: "ptyes-v1",
		ImportedAt: now.Add(66 * time.Minute),
	}, nil)
	if err != nil || !personalStateRetry.Duplicate || personalStateRetry.BookmarkAppliedRows != 1 {
		t.Fatalf("retry legacy personal state = %+v, %v", personalStateRetry, err)
	}
	status, err := InspectMigrationStatus(ctx, core, inventory)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != "reconciled" || !status.CheckpointsComplete() ||
		status.ImportedUsers != 1 || status.ImportedTorrents != 1 ||
		status.ImportedTorrentObjects != 1 || status.VerifiedPreferredObjects != 1 {
		t.Fatalf("migration status = %+v", status)
	}

	// The final gate deliberately runs after the asynchronous control plane has
	// caught up. Reconciliation alone is insufficient because Tracker must
	// receive the exact imported published set before any user traffic moves.
	repository, err := trackercontrol.NewPostgresRepository(core)
	if err != nil {
		t.Fatal(err)
	}
	projectedAt := now.Add(70 * time.Minute)
	projector, err := trackercontrol.NewProjector(
		repository, trackercontrol.ProjectorConfig{}, func() time.Time { return projectedAt }, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	processed, err := projector.RunOnce(ctx)
	if err != nil || !processed {
		t.Fatalf("project Tracker control event = %v, %v", processed, err)
	}

	snapshotAt := now.Add(80 * time.Minute)
	snapshotDirectory := t.TempDir()
	if err := os.Chmod(snapshotDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	controlPath := filepath.Join(snapshotDirectory, "control.snapshot")
	subjectPath := filepath.Join(snapshotDirectory, "subjects.snapshot")
	runtimePolicyPath := filepath.Join(snapshotDirectory, "runtime-policy.snapshot")
	privateKey := ed25519.NewKeyFromSeed(bytesOf(0x61, ed25519.SeedSize))
	controlPublisher, err := trackersnapshot.NewFilesystemPublisher(controlPath)
	if err != nil {
		t.Fatal(err)
	}
	controlBuilder, err := trackercontrol.NewSnapshotBuilder(
		repository, controlPublisher, "legacy-e2e", privateKey, func() time.Time { return snapshotAt },
	)
	if err != nil {
		t.Fatal(err)
	}
	controlResult, err := controlBuilder.BuildAndPublish(ctx)
	if err != nil || controlResult.TorrentCount != 1 {
		t.Fatalf("build Tracker control snapshot = %+v, %v", controlResult, err)
	}
	subjectPublisher, err := trackersnapshot.NewSubjectFilesystemPublisher(subjectPath)
	if err != nil {
		t.Fatal(err)
	}
	subjectBuilder, err := trackercontrol.NewSubjectSnapshotBuilder(
		repository, subjectPublisher, "legacy-e2e", privateKey, func() time.Time { return snapshotAt },
	)
	if err != nil {
		t.Fatal(err)
	}
	subjectResult, err := subjectBuilder.BuildAndPublish(ctx)
	if err != nil || subjectResult.SubjectCount != 1 {
		t.Fatalf("build Tracker subject snapshot = %+v, %v", subjectResult, err)
	}
	runtimePolicyRepository, err := trackercontrol.NewPostgresRuntimePolicyRepository(core)
	if err != nil {
		t.Fatal(err)
	}
	runtimePolicyPublisher, err := trackersnapshot.NewRuntimePolicyFilesystemPublisher(runtimePolicyPath)
	if err != nil {
		t.Fatal(err)
	}
	runtimePolicyBuilder, err := trackercontrol.NewRuntimePolicySnapshotBuilder(
		runtimePolicyRepository, runtimePolicyPublisher, "legacy-e2e", privateKey,
		func() time.Time { return snapshotAt },
	)
	if err != nil {
		t.Fatal(err)
	}
	runtimePolicyResult, err := runtimePolicyBuilder.BuildAndPublish(ctx)
	if err != nil || runtimePolicyResult.ControlSequence != seedboxResult.PolicySequence {
		t.Fatalf("build Tracker runtime policy snapshot = %+v, %v", runtimePolicyResult, err)
	}
	preflightPath := filepath.Join(t.TempDir(), "preflight.json")
	preflightSHA256, err := WriteCutoverPreflightManifest(preflightPath, preflight)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := InspectCutoverAcceptance(
		ctx,
		readOnlySource,
		readOnlyCore,
		readOnlyVault,
		store,
		CutoverAcceptanceConfig{
			Inventory: inventory, OccurredAt: now,
			Now:               func() time.Time { return snapshotAt.Add(time.Minute) },
			DatabaseDumpBytes: 1024, TorrentArchiveSHA256: archiveSHA256,
			TorrentArchiveBytes: int64(len(raw)), TorrentArchiveObjects: 1,
			ImageArchiveSHA256: imageArchive.SHA256, ImageArchiveBytes: imageArchive.ByteLength,
			ImageArchiveObjects: imageArchive.ImageCount,
			StorageBackendID:    string(store.BackendID()), StorageDriver: "filesystem",
			StorageConfigSHA256: storageConfigSHA256, Preflight: preflight,
			PreflightManifestSHA256: preflightSHA256, TrackerSnapshotPath: controlPath,
			TrackerSubjectSnapshotPath: subjectPath,
			TrackerRuntimePolicyPath:   runtimePolicyPath,
			TrackerTrustedKeys: map[string]ed25519.PublicKey{
				"legacy-e2e": privateKey.Public().(ed25519.PublicKey),
			},
			TrackerSnapshotMaxAge: 5 * time.Minute, TrackerSubjectMaxAge: 5 * time.Minute,
			TrackerRuntimePolicyMaxAge: 5 * time.Minute,
			TrackerMaxFutureSkew:       time.Second,
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !accepted.CoreRuntimeDefaultsReady || !accepted.LegacyMemberAuthorizationReady ||
		!accepted.ReadyToActivate || accepted.VerifiedStoredObjects != 1 ||
		accepted.TorrentImages != 1 || accepted.VerifiedImageObjects != 1 ||
		accepted.TorrentPurchaseRows != 1 || accepted.TorrentPurchaseRights != 1 ||
		accepted.TorrentPurchaseEvidenceOnly != 0 || accepted.TrackerTorrentCount != 1 ||
		accepted.TrackerSubjectCount != 1 || accepted.SeedboxBindingRows != 1 ||
		accepted.SeedboxUploadFactorBasisPoints != 5_000 ||
		accepted.SeedboxDownloadFactorBasisPoints != 20_000 ||
		accepted.SeedboxSpeedLimitBytesPerSecond != 0 ||
		accepted.StandardSpeedLimitBytesPerSecond != 25*1024*1024 {
		t.Fatalf("cutover acceptance = %+v", accepted)
	}

	var runState string
	var torrentRows, objectRows, fileRows, catalogRows, outboxRows int64
	if err := core.QueryRow(ctx, `
SELECT
    (SELECT state FROM migration.runs WHERE id = $1),
    (SELECT count(*)::bigint FROM torrents.torrents),
    (SELECT count(*)::bigint FROM torrents.torrent_objects),
    (SELECT count(*)::bigint FROM torrents.torrent_files),
    (SELECT count(*)::bigint FROM catalog.torrents),
	(SELECT count(*)::bigint FROM tracker_control.outbox)`, runID).Scan(
		&runState, &torrentRows, &objectRows, &fileRows, &catalogRows, &outboxRows,
	); err != nil {
		t.Fatal(err)
	}
	if runState != "reconciled" || torrentRows != 1 || objectRows != 1 || fileRows != 1 || catalogRows != 1 || outboxRows != 1 {
		t.Fatalf(
			"target counts = state %s torrents %d objects %d files %d catalog %d outbox %d",
			runState, torrentRows, objectRows, fileRows, catalogRows, outboxRows,
		)
	}
}

func bytesOf(value byte, size int) []byte {
	result := make([]byte, size)
	for index := range result {
		result[index] = value
	}
	return result
}

func TestPrepareLegacyTorrentDependenciesAgainstSnapshot(t *testing.T) {
	sourceURL := os.Getenv("PEERGO_TEST_LEGACY_SOURCE_DATABASE_URL")
	coreURL := os.Getenv("PEERGO_TEST_LEGACY_CORE_DATABASE_URL")
	runIDValue := os.Getenv("PEERGO_TEST_LEGACY_RUN_ID")
	if sourceURL == "" || coreURL == "" || runIDValue == "" {
		t.Skip("legacy torrent dependency integration settings are not configured")
	}
	runID, err := uuid.Parse(runIDValue)
	if err != nil || runID == uuid.Nil {
		t.Fatal("PEERGO_TEST_LEGACY_RUN_ID is invalid")
	}
	ctx := context.Background()
	source, err := pgxpool.New(ctx, sourceURL)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	core, err := pgxpool.New(ctx, coreURL)
	if err != nil {
		t.Fatal(err)
	}
	defer core.Close()
	var expectedGroups int64
	if err := source.QueryRow(ctx, `SELECT count(*)::bigint FROM torrent_groups`).Scan(&expectedGroups); err != nil {
		t.Fatal(err)
	}
	result, err := prepareImportDependencies(
		ctx,
		source,
		core,
		runID,
		time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.ResourceGroups != expectedGroups || result.CategoryFacetOptions < 1 || result.FacetOptions < 1 {
		t.Fatalf("prepared dependencies = %+v, expected groups %d", result, expectedGroups)
	}
}

func createSyntheticPtYesSource(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	publicID uuid.UUID,
	imageID uuid.UUID,
	parsed torrents.ParsedMetainfo,
	now time.Time,
) {
	t.Helper()
	statements := []string{
		`CREATE TABLE users (id bigint PRIMARY KEY)`,
		`CREATE TABLE user_attendance_stats (
            user_id bigint PRIMARY KEY,
            total_days bigint NOT NULL,
            retroactive_cards bigint NOT NULL
        )`,
		`CREATE TABLE attendance_records (
            id bigint PRIMARY KEY,
            user_id bigint NOT NULL
        )`,
		`CREATE TABLE categories (id bigint PRIMARY KEY, name text NOT NULL)`,
		`CREATE TABLE category_attributes (id bigint PRIMARY KEY, category_id bigint NOT NULL, name text NOT NULL)`,
		`CREATE TABLE category_attribute_options (id bigint PRIMARY KEY, attribute_id bigint NOT NULL, value text NOT NULL, label text)`,
		`CREATE TABLE torrent_groups (id bigint PRIMARY KEY, external_ids jsonb, created_at timestamptz, updated_at timestamptz)`,
		`CREATE TABLE torrents (
            id bigint PRIMARY KEY,
            uuid text NOT NULL,
            info_hash text NOT NULL,
            title text NOT NULL,
            subtitle text NOT NULL,
            description text,
            type text,
            attributes text,
            size bigint NOT NULL,
            uploaded_by bigint NOT NULL,
            anonymous boolean,
            status text,
			promotion_type integer,
			promotion_time_type integer,
			promotion_until timestamptz,
            group_id bigint,
            media_info text,
            poster text,
            created_at timestamptz NOT NULL,
            updated_at timestamptz NOT NULL,
			deleted_at timestamptz
		)`,
		`ALTER TABLE torrents ADD COLUMN price numeric NOT NULL DEFAULT 0`,
		`CREATE TABLE torrent_files (id bigint PRIMARY KEY, torrent_id bigint NOT NULL, path text NOT NULL, size bigint NOT NULL)`,
		`CREATE TABLE torrent_purchases (
            id bigint PRIMARY KEY,
            torrent_id bigint,
            torrent_uuid text,
            buyer_id bigint NOT NULL,
            seller_id bigint NOT NULL,
            price numeric NOT NULL,
            tax_amount numeric NOT NULL,
            seller_income numeric NOT NULL,
            status text NOT NULL,
            created_at timestamptz NOT NULL
        )`,
		`CREATE TABLE torrent_images (
            id bigint PRIMARY KEY,
            torrent_id bigint NOT NULL,
            url text NOT NULL,
            is_cover boolean,
            sort_order integer
        )`,
		`CREATE TABLE seed_boxes (
			id bigint PRIMARY KEY,
			user_id bigint,
			ip_start text NOT NULL,
			ip_end text NOT NULL,
			ip text,
			c_id_r text,
			operator text,
			bandwidth text,
			comment text,
			type integer NOT NULL,
			status integer NOT NULL,
			created_at timestamptz NOT NULL,
			updated_at timestamptz NOT NULL
		)`,
		`CREATE TABLE site_settings (key text PRIMARY KEY, value text NOT NULL)`,
		`CREATE TABLE bookmarks (
			id bigint PRIMARY KEY,
			user_id bigint NOT NULL,
			torrent_id bigint NOT NULL,
			created_at timestamptz NOT NULL
		)`,
		`CREATE TABLE invites (
			id bigint PRIMARY KEY,
			inviting_user bigint NOT NULL,
			claimed boolean NOT NULL,
			claimed_by_uid bigint,
			claimed_at timestamptz,
			created_at timestamptz NOT NULL
		)`,
		`CREATE TABLE points_logs (
			id bigint PRIMARY KEY,
			user_id bigint NOT NULL,
			point_type text NOT NULL,
			action text NOT NULL,
			amount numeric NOT NULL,
			created_at timestamptz NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(ctx, `INSERT INTO users (id) VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO bookmarks (id, user_id, torrent_id, created_at) VALUES (1, 1, 1, $1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
INSERT INTO seed_boxes (
    id, user_id, ip_start, ip_end, ip, c_id_r, operator, bandwidth,
    comment, type, status, created_at, updated_at
) VALUES (1, 1, '192.0.2.10', '192.0.2.10', '192.0.2.10', '',
          'synthetic', '1Gbps', '', 2, 1, $1, $1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
INSERT INTO site_settings (key, value) VALUES
    ('seedbox.enabled', 'true'),
    ('seedbox.upload_ratio', '0.5'),
    ('seedbox.max_speed', '400'),
    ('seedbox.non_seedbox_max_speed', '200'),
    ('seedbox.uploader_max_speed', '600'),
    ('seedbox.uploader_upload_ratio', '0.5'),
    ('seedbox.warning_limit', '3'),
    ('vip.no_speed_limit', 'true'),
    ('vip.seedbox_no_discount', 'false')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO categories (id, name) VALUES (1, 'movie')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
INSERT INTO torrent_groups (id, external_ids, created_at, updated_at)
VALUES (1, '{"imdb":"tt1234567"}'::jsonb, $1, $1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
INSERT INTO torrents (
    id, uuid, info_hash, title, subtitle, description, type, attributes,
    size, uploaded_by, anonymous, status, group_id, media_info, poster,
    created_at, updated_at
) VALUES (1, $1, $2, 'Release', '', 'Description', 'movie', '{}',
		  3, 1, false, 'approved', 1, '', '', $3, $3)`,
		publicID.String(), parsed.InfoHashV1.Hex(), now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE torrents SET price = 10 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
INSERT INTO torrent_purchases (
    id, torrent_id, torrent_uuid, buyer_id, seller_id,
    price, tax_amount, seller_income, status, created_at
) VALUES (1, 1, $1, 1, 1, 10, 1, 9, 'completed', $2)`, publicID.String(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO torrent_files (id, torrent_id, path, size) VALUES (1, 1, 'a.bin', 3)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
INSERT INTO torrent_images (id, torrent_id, url, is_cover, sort_order)
VALUES (1, 1, $1, true, 0)`, "/uploads/images/aa/"+imageID.String()+".gif"); err != nil {
		t.Fatal(err)
	}
}

func seedSyntheticMigrationRun(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	runID, userID, credentialRef uuid.UUID,
	snapshot [sha256.Size]byte,
	now time.Time,
) {
	t.Helper()
	if _, err := db.Exec(ctx, `
INSERT INTO identity.users (
    id, credential_ref, username, display_name, status, created_at, updated_at,
    email_verified_at, password_changed_at
) VALUES ($1, $2, 'legacy-user', 'Legacy User', 'active', $3, $3, $3, $3)`,
		userID, credentialRef, now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
INSERT INTO migration.runs (
    id, source_system, source_snapshot_sha256, mapping_version, state,
    expected_user_rows, expected_torrent_rows, created_at, state_changed_at
) VALUES ($1, 'ptyes', $2, 'ptyes-v1', 'importing', 1, 1, $3, $3)`,
		runID, snapshot[:], now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
INSERT INTO migration.user_id_map (
    source_system, legacy_user_id, user_id, credential_ref, first_run_id, created_at
) VALUES ('ptyes', 1, $1, $2, $3, $4)`, userID, credentialRef, runID, now); err != nil {
		t.Fatal(err)
	}
	attendanceFingerprint := sha256.Sum256([]byte("synthetic-attendance-fingerprint"))
	if _, err := db.Exec(ctx, `
INSERT INTO migration.user_attendance_openings (
    source_system, legacy_user_id, user_id, source_stats_present,
    source_current_streak, source_longest_streak, source_total_days,
    source_retroactive_cards, source_last_attendance_date,
    source_stats_last_attendance_at, source_record_days, source_fingerprint,
    first_run_id, imported_at
) VALUES (
    'ptyes', 1, $1, false, 0, 0, 0, 0, NULL, NULL, 0, $2, $3, $4
)`, userID, attendanceFingerprint[:], runID, now); err != nil {
		t.Fatal(err)
	}
	userFingerprint := sha256.Sum256([]byte("synthetic-user-fingerprint"))
	if _, err := db.Exec(ctx, `
INSERT INTO migration.source_rows (
    run_id, entity_kind, legacy_id, source_fingerprint, fingerprint_scheme,
    state, attempt_count, version, created_at, updated_at
) VALUES ($1, 'user', 1, $2, 'hmac-sha256-v1', 'imported', 1, 1, $3, $3)`,
		runID, userFingerprint[:], now,
	); err != nil {
		t.Fatal(err)
	}
	mandateID := uuid.MustParse("dddddddd-2222-4333-8444-555555555555")
	grantID := uuid.MustParse("eeeeeeee-2222-4333-8444-555555555555")
	if _, err := db.Exec(ctx, `
INSERT INTO governance.mandates (
    id, subject_id, source_type, source_reference, scope_type, scope_id,
    starts_at, ends_at, status, created_at, updated_at
) VALUES ($1, $2, 'legacy_import', 'ptyes-user-migration-v1', 'site', 'peergo',
          $3, $4, 'active', $3, $3)`, mandateID, userID, now, now.AddDate(100, 0, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
INSERT INTO authz.grants (
    id, subject_id, role_id, mandate_id, scope_type, scope_id,
    valid_from, valid_until, constraints, created_at, updated_at
) VALUES ($1, $2, 'member', $3, 'site', 'peergo', $4, $5, '{}'::jsonb, $4, $4)`,
		grantID, userID, mandateID, now, now.AddDate(100, 0, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
INSERT INTO identity.tracker_passkey_hmac (
    user_id, credential_ref, lookup_hmac, vault_version, created_at, updated_at
) VALUES ($1, $2, decode(repeat('11', 32), 'hex'), 1, $3, $3)`, userID, credentialRef, now); err != nil {
		t.Fatal(err)
	}
}

func seedSyntheticVaultCredential(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	credentialRef uuid.UUID,
	now time.Time,
) {
	t.Helper()
	if _, err := db.Exec(ctx, `
INSERT INTO vault.credentials (
    credential_ref, password_hash, password_updated_at, created_at, updated_at,
    password_algorithm
) VALUES ($1, '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy',
          $2, $2, $2, 'bcrypt_ptyes_cost10')`, credentialRef, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
INSERT INTO vault.direct_identifiers (
    credential_ref, kind, lookup_hmac, masked_value, verified_at, created_at
) VALUES
    ($1, 'username', decode(repeat('21', 32), 'hex'), 'l***y', $2, $2),
    ($1, 'email', decode(repeat('22', 32), 'hex'), 'l***y@example.test', $2, $2)`, credentialRef, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
INSERT INTO vault.tracker_passkeys (
    credential_ref, ciphertext, nonce, encryption_key_epoch, lookup_hmac,
    version, created_at, updated_at, format_profile
) VALUES (
    $1, decode(repeat('31', 17), 'hex'), decode(repeat('32', 12), 'hex'),
    'legacy-e2e', decode(repeat('33', 32), 'hex'), 1, $2, $2, 'canonical_hex_v1'
)`, credentialRef, now); err != nil {
		t.Fatal(err)
	}
}

func openReadOnlyMigrationPool(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	return pool
}

type syntheticImageTransformer struct{}

func (syntheticImageTransformer) Probe(context.Context) error { return nil }

func (syntheticImageTransformer) TransformToWebP(context.Context, []byte, string) ([]byte, error) {
	return nil, errors.New("synthetic GIF fixture must not require transformation")
}
