package torrents

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/identity"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func TestPostgresTorrentUploadCommitsOneRecoverablePendingAggregate(t *testing.T) {
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

	now := time.Now().UTC().Truncate(time.Microsecond)
	userID := uuid.New()
	credentialRef := uuid.New()
	categoryID := "integration-" + uuid.NewString()[:8]
	facetID := "integration-" + uuid.NewString()[:8]
	username := "integration-" + uuid.NewString()[:8]
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.users (
    id, credential_ref, username, display_name, status,
    email_verified_at, created_at, updated_at, password_changed_at
) VALUES ($1, $2, $3, '上传集成测试', 'active', $4, $4, $4, $4)`,
		userID, credentialRef, username, now,
	); err != nil {
		t.Fatalf("insert integration user: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO catalog.categories (id, name, display_order, enabled, created_at, updated_at)
VALUES ($1, '上传集成测试', 100000 + abs(hashtext($1)) % 900000, true, $2, $2)`,
		categoryID, now,
	); err != nil {
		t.Fatalf("insert integration category: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, `
UPDATE catalog.categories
SET enabled = false, updated_at = now(), version = version + 1
WHERE id = $1 AND enabled = true`, categoryID)
	})
	if _, err := pool.Exec(ctx, `
INSERT INTO catalog.facet_definitions (
    id, name, selection_mode, display_order, enabled, created_at, updated_at
) VALUES ($1, '集成属性', 'single_option', 900000, true, $2, $2)`, facetID, now); err != nil {
		t.Fatalf("insert integration facet definition: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO catalog.facet_options (
    facet_id, option_key, selection_mode, label, display_order, enabled, created_at, updated_at
) VALUES ($1, 'selected', 'single_option', '已选择', 10, true, $2, $2)`, facetID, now); err != nil {
		t.Fatalf("insert integration facet option: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO catalog.category_facets (
    category_id, facet_id, selection_mode, required, display_order, created_at
) VALUES ($1, $2, 'single_option', true, 10, $3)`, categoryID, facetID, now); err != nil {
		t.Fatalf("insert integration category facet: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO catalog.category_facet_options (
    category_id, facet_id, option_key, selection_mode, created_at
) VALUES ($1, $2, 'selected', 'single_option', $3)`, categoryID, facetID, now); err != nil {
		t.Fatalf("insert integration category facet option: %v", err)
	}
	// Upload, object and file rows are deliberately immutable. This integration
	// test therefore targets a disposable migrated database and does not pretend
	// that cleanup can safely erase the evidence it just verified.

	repository, err := NewPostgresTorrentUploadRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	store := newMemoryObjectStore("integration-primary")
	registry, err := NewStoreRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	verifiedAt := now.Add(-time.Hour)
	service, err := NewTorrentUploadService(
		staticTorrentUploadAuthenticator{session: identity.WebSession{User: identity.User{
			ID: userID, EmailVerifiedAt: &verifiedAt,
		}}},
		&recordingTorrentUploadAuthorizer{now: now},
		repository,
		registry,
		TorrentUploadServiceConfig{ActiveBackendID: store.BackendID(), Now: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatal(err)
	}
	uploadID := uuid.New()
	rawMetainfo := validSingleFixture(fmt.Sprintf("integration-%s.bin", uploadID.String()[:8]), 42, 16*1024)
	input := TorrentUploadInput{
		ID: uploadID, CategoryID: categoryID, Title: "Integration release",
		Subtitle:    "Repository transaction",
		Description: "Integration **description**",
		MediaInfo:   "General\nComplete name: integration.bin",
		Anonymous:   true,
		ExternalIdentifiers: []ExternalIdentifier{
			{Provider: "imdb", ExternalID: "tt1234567"},
			{Provider: "tmdb", ExternalID: "12345"},
		},
		FacetSelections: []FacetSelection{{FacetID: facetID, OptionKeys: []string{"selected"}}},
		Screenshots: []TorrentScreenshotInput{{
			Raw: screenshotPNG(t, 4, 3, color.RGBA{R: 32, G: 64, B: 96, A: 255}),
		}},
		RawMetainfo: rawMetainfo,
	}

	result, err := service.Submit(ctx, "cookie", "csrf", input)
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if result.State != StatePendingReview || result.ID < 1 || result.FileCount != 1 || result.TotalSizeBytes != 42 {
		t.Fatalf("Submit() result = %+v", result)
	}
	replayed, err := service.Submit(ctx, "cookie", "csrf", input)
	if err != nil || replayed != result {
		t.Fatalf("Submit() replay = %+v, %v; want %+v", replayed, err, result)
	}
	changed := input
	changed.Title = "Changed request"
	if _, err := service.Submit(ctx, "cookie", "csrf", changed); !errors.Is(err, ErrTorrentUploadIdempotencyConflict) {
		t.Fatalf("changed idempotent Submit() error = %v", err)
	}

	var uploadState string
	var aggregateCount, objectCount, locationCount, fileCount int
	var screenshotCount, screenshotObjectCount, screenshotLocationCount int
	if err := pool.QueryRow(ctx, `
SELECT
    upload.state,
    (SELECT count(*) FROM torrents.torrents WHERE id = upload.torrent_id),
    (SELECT count(*) FROM torrents.torrent_objects WHERE id = upload.object_id),
    (SELECT count(*) FROM torrents.torrent_object_locations WHERE object_id = upload.object_id),
    (SELECT count(*) FROM torrents.torrent_files WHERE torrent_id = upload.torrent_id),
    (SELECT count(*) FROM torrents.torrent_screenshots WHERE torrent_id = upload.torrent_id),
    (SELECT count(*) FROM torrents.torrent_screenshot_objects AS object
     JOIN torrents.torrent_screenshots AS screenshot ON screenshot.object_id = object.id
     WHERE screenshot.torrent_id = upload.torrent_id),
    (SELECT count(*) FROM torrents.torrent_screenshot_object_locations AS location
     JOIN torrents.torrent_screenshots AS screenshot ON screenshot.object_id = location.object_id
     WHERE screenshot.torrent_id = upload.torrent_id)
FROM torrents.torrent_uploads AS upload
WHERE upload.id = $1`, uploadID).Scan(
		&uploadState, &aggregateCount, &objectCount, &locationCount, &fileCount,
		&screenshotCount, &screenshotObjectCount, &screenshotLocationCount,
	); err != nil {
		t.Fatalf("read committed upload graph: %v", err)
	}
	if uploadState != string(TorrentUploadCompleted) || aggregateCount != 1 || objectCount != 1 || locationCount != 1 || fileCount != 1 ||
		screenshotCount != 1 || screenshotObjectCount != 1 || screenshotLocationCount != 1 || len(store.objects) != 2 {
		t.Fatalf("graph state=%s aggregate=%d object=%d location=%d files=%d screenshots=%d screenshot_objects=%d screenshot_locations=%d stored=%d",
			uploadState, aggregateCount, objectCount, locationCount, fileCount, screenshotCount, screenshotObjectCount, screenshotLocationCount, len(store.objects))
	}
	var description, descriptionFormat, mediaInfo string
	var anonymous bool
	var externalIdentifierCount, facetValueCount int
	if err := pool.QueryRow(ctx, `
SELECT
    torrent.description,
    torrent.description_format,
    torrent.media_info,
    torrent.anonymous,
    (SELECT count(*) FROM torrents.torrent_external_identifiers AS identifier
     WHERE identifier.torrent_id = torrent.id),
    (SELECT count(*) FROM torrents.torrent_facet_values AS value
     WHERE value.torrent_id = torrent.id)
FROM torrents.torrents AS torrent
WHERE torrent.id = $1`, result.ID).Scan(
		&description, &descriptionFormat, &mediaInfo, &anonymous, &externalIdentifierCount, &facetValueCount,
	); err != nil {
		t.Fatalf("read committed upload metadata: %v", err)
	}
	if description != input.Description || descriptionFormat != DescriptionFormatMarkdown ||
		mediaInfo != input.MediaInfo || !anonymous || externalIdentifierCount != 2 || facetValueCount != 1 {
		t.Fatalf("metadata description=%q format=%q media_info=%q anonymous=%t identifiers=%d facets=%d",
			description, descriptionFormat, mediaInfo, anonymous, externalIdentifierCount, facetValueCount)
	}

	failedAt := now.Add(time.Minute)
	failedUploadID := uuid.New()
	failingRepository := finalizationFailingUploadRepository{TorrentUploadRepository: repository}
	failingService, err := NewTorrentUploadService(
		staticTorrentUploadAuthenticator{session: identity.WebSession{User: identity.User{
			ID: userID, EmailVerifiedAt: &verifiedAt,
		}}},
		&recordingTorrentUploadAuthorizer{now: failedAt},
		failingRepository,
		registry,
		TorrentUploadServiceConfig{ActiveBackendID: store.BackendID(), Now: func() time.Time { return failedAt }},
	)
	if err != nil {
		t.Fatal(err)
	}
	failedInput := TorrentUploadInput{
		ID: failedUploadID, CategoryID: categoryID, Title: "Interrupted integration release",
		RawMetainfo: validSingleFixture(fmt.Sprintf("orphan-%s.bin", failedUploadID.String()[:8]), 7, 16*1024),
	}
	if _, err := failingService.Submit(ctx, "cookie", "csrf", failedInput); !errors.Is(err, errIntegrationFinalization) {
		t.Fatalf("interrupted Submit() error = %v", err)
	}
	if len(store.objects) != 3 {
		t.Fatalf("stored objects before reconciliation = %d", len(store.objects))
	}
	recoveryService, err := NewTorrentUploadService(
		staticTorrentUploadAuthenticator{session: identity.WebSession{User: identity.User{
			ID: userID, EmailVerifiedAt: &verifiedAt,
		}}},
		&recordingTorrentUploadAuthorizer{now: failedAt},
		repository,
		registry,
		TorrentUploadServiceConfig{ActiveBackendID: store.BackendID(), Now: func() time.Time { return failedAt }},
	)
	if err != nil {
		t.Fatal(err)
	}
	recoveredInput := failedInput
	recoveredInput.ID = uuid.New()
	recovered, err := recoveryService.Submit(ctx, "cookie", "csrf", recoveredInput)
	if err != nil || recovered.State != StatePendingReview {
		t.Fatalf("Submit() with a fresh browser key = %+v, %v", recovered, err)
	}
	var recoveredUploadState string
	if err := pool.QueryRow(ctx, `SELECT state FROM torrents.torrent_uploads WHERE id = $1`, failedUploadID).Scan(&recoveredUploadState); err != nil {
		t.Fatalf("read recovered upload: %v", err)
	}
	if recoveredUploadState != string(TorrentUploadCompleted) || len(store.objects) != 3 {
		t.Fatalf("recovered state=%s stored=%d", recoveredUploadState, len(store.objects))
	}

	orphanUploadID := uuid.New()
	orphanInput := TorrentUploadInput{
		ID: orphanUploadID, CategoryID: categoryID, Title: "Orphaned integration release",
		RawMetainfo: validSingleFixture(fmt.Sprintf("cleanup-%s.bin", orphanUploadID.String()[:8]), 9, 16*1024),
	}
	if _, err := failingService.Submit(ctx, "cookie", "csrf", orphanInput); !errors.Is(err, errIntegrationFinalization) {
		t.Fatalf("orphan Submit() error = %v", err)
	}
	if len(store.objects) != 4 {
		t.Fatalf("stored objects before orphan cleanup = %d", len(store.objects))
	}
	reconcileAt := failedAt.Add(25 * time.Hour)
	orphanService, err := NewTorrentUploadOrphanService(repository, registry, TorrentUploadOrphanServiceConfig{
		Retention: 24 * time.Hour, Now: func() time.Time { return reconcileAt },
	})
	if err != nil {
		t.Fatal(err)
	}
	processed, err := orphanService.RunBatch(ctx, store.BackendID())
	// A shared local integration database can contain an older interrupted
	// reservation from an aborted prior run. The batch is intentionally allowed
	// to reconcile more than this test's own upload; the assertion below proves
	// that this run's evidence reached the expected terminal state.
	if err != nil || processed < 1 {
		t.Fatalf("RunBatch() processed=%d error=%v", processed, err)
	}
	var failedState string
	if err := pool.QueryRow(ctx, `SELECT state FROM torrents.torrent_uploads WHERE id = $1`, orphanUploadID).Scan(&failedState); err != nil {
		t.Fatalf("read reconciled upload: %v", err)
	}
	if failedState != string(TorrentUploadAbandoned) || len(store.objects) != 3 {
		t.Fatalf("reconciled state=%s stored=%d", failedState, len(store.objects))
	}
	if _, err := failingService.Submit(ctx, "cookie", "csrf", orphanInput); !errors.Is(err, ErrTorrentUploadExpired) {
		t.Fatalf("abandoned Submit() error = %v", err)
	}
}

var errIntegrationFinalization = errors.New("integration finalization interrupted")

type finalizationFailingUploadRepository struct {
	TorrentUploadRepository
}

func (finalizationFailingUploadRepository) Finalize(context.Context, FinalizeTorrentUploadCommand) (TorrentUploadResult, error) {
	return TorrentUploadResult{}, errIntegrationFinalization
}
