package traffic

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/settlementhnrv1"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

// This gated test intentionally requires a disposable, currently migrated
// Core database: H&R inbox and projection rows are immutable by design and
// must not gain a test-only deletion escape hatch.
func TestIntegrationAppliesAndListsHNRProjection(t *testing.T) {
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
	userID, torrentID := insertHNRIntegrationFixture(t, ctx, pool)
	repository, err := NewPostgresRepository(pool, time.Now)
	if err != nil {
		t.Fatal(err)
	}

	completedAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	tracking, trackingPayload := hnrIntegrationEvent(t, userID, torrentID, completedAt, settlementhnrv1.StateTracking)
	result, err := repository.ApplyHNR(ctx, trackingPayload, time.Now())
	if err != nil || result.Duplicate || result.EventID.String() != tracking.EventID {
		t.Fatalf("ApplyHNR(tracking) = %+v, error %v", result, err)
	}
	page, err := repository.ListHNR(ctx, userID, HNRQuery{Filter: HNRFilterOpen, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Summary.Total != 1 || page.Summary.Tracking != 1 || len(page.Items) != 1 ||
		page.Items[0].TorrentID != torrentID || page.Items[0].TorrentTitle != "Core H&R Integration" ||
		page.Items[0].Status != HNRStatusTracking {
		t.Fatalf("ListHNR(tracking) = %+v", page)
	}
	if duplicate, err := repository.ApplyHNR(ctx, trackingPayload, time.Now()); err != nil || !duplicate.Duplicate {
		t.Fatalf("ApplyHNR(duplicate) = %+v, error %v", duplicate, err)
	}

	gap := tracking
	gap.EventID = mustHNRV7(t).String()
	gap.ObligationVersion = 3
	gap.OccurredAt = tracking.OccurredAt.Add(time.Minute)
	gapPayload := encodeHNRIntegrationEvent(t, gap)
	if _, err := repository.ApplyHNR(ctx, gapPayload, time.Now()); !errors.Is(err, ErrInvariant) {
		t.Fatalf("ApplyHNR(version gap) error = %v", err)
	}

	satisfied := tracking
	satisfied.EventID = mustHNRV7(t).String()
	satisfied.ObligationVersion = 2
	satisfied.OccurredAt = completedAt.Add(2 * time.Hour)
	satisfied.State = settlementhnrv1.StateSatisfied
	satisfied.SeededSeconds = satisfied.RequiredSeedSeconds
	satisfied.RawUploaded = 2048
	satisfied.RawRatioBasisPoints = 20000
	bySeedTime := settlementhnrv1.SatisfiedBySeedTime
	satisfied.SatisfiedBy = &bySeedTime
	satisfiedAt := satisfied.OccurredAt
	satisfied.SatisfiedAt = &satisfiedAt
	if _, err := repository.ApplyHNR(ctx, encodeHNRIntegrationEvent(t, satisfied), time.Now()); err != nil {
		t.Fatalf("ApplyHNR(satisfied) error = %v", err)
	}

	exemptCompletedAt := completedAt.Add(time.Minute)
	_, exemptPayload := hnrIntegrationEvent(t, userID, torrentID, exemptCompletedAt, settlementhnrv1.StateExempt)
	if _, err := repository.ApplyHNR(ctx, exemptPayload, time.Now()); err != nil {
		t.Fatalf("ApplyHNR(exempt) error = %v", err)
	}
	first, err := repository.ListHNR(ctx, userID, HNRQuery{Filter: HNRFilterAll, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if first.Summary.Total != 2 || first.Summary.Satisfied != 1 || first.Summary.Exempt != 1 ||
		len(first.Items) != 1 || first.Items[0].Status != HNRStatusExempt || first.NextCursor == nil {
		t.Fatalf("ListHNR(first page) = %+v", first)
	}
	second, err := repository.ListHNR(ctx, userID, HNRQuery{Filter: HNRFilterAll, Limit: 1, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].Status != HNRStatusSatisfied || second.NextCursor != nil {
		t.Fatalf("ListHNR(second page) = %+v", second)
	}
	open, err := repository.ListHNR(ctx, userID, HNRQuery{Filter: HNRFilterOpen, Limit: 10})
	if err != nil || len(open.Items) != 0 {
		t.Fatalf("ListHNR(open after terminal states) = %+v, error %v", open, err)
	}

	conflicting := tracking
	conflicting.RawUploaded++
	if _, err := repository.ApplyHNR(ctx, encodeHNRIntegrationEvent(t, conflicting), time.Now()); !errors.Is(err, ErrConflict) {
		t.Fatalf("ApplyHNR(conflicting event ID) error = %v", err)
	}
	terminalAdvance := satisfied
	terminalAdvance.EventID = mustHNRV7(t).String()
	terminalAdvance.ObligationVersion = 3
	terminalAdvance.OccurredAt = satisfied.OccurredAt.Add(time.Minute)
	if _, err := repository.ApplyHNR(ctx, encodeHNRIntegrationEvent(t, terminalAdvance), time.Now()); !errors.Is(err, ErrInvariant) {
		t.Fatalf("ApplyHNR(terminal advance) error = %v", err)
	}
}

func insertHNRIntegrationFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (uuid.UUID, int64) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	userID, credentialRef := uuid.New(), uuid.New()
	username := "hnr-it-" + userID.String()[:8]
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.users (
    id, credential_ref, username, display_name, status, email_verified_at, created_at, updated_at, password_changed_at
) VALUES ($1, $2, $3, 'Core H&R 投影集成测试', 'active', $4, $4, $4, $4)`, userID, credentialRef, username, now); err != nil {
		t.Fatalf("insert H&R integration user: %v", err)
	}
	categoryID := "hnr-it-" + uuid.NewString()[:8]
	if _, err := pool.Exec(ctx, `
INSERT INTO catalog.categories (id, name, display_order, enabled, created_at, updated_at)
VALUES ($1, 'Core H&R 投影集成测试', 100000 + abs(hashtext($1)) % 900000, true, $2, $2)`, categoryID, now); err != nil {
		t.Fatalf("insert H&R integration category: %v", err)
	}
	objectID := uuid.New()
	objectDigest := sha256.Sum256([]byte("hnr-object-" + objectID.String()))
	infoDigest := sha256.Sum256([]byte("hnr-info-" + objectID.String()))
	if _, err := pool.Exec(ctx, `
INSERT INTO torrents.torrent_objects (
    id, content_sha256, byte_length, parser_version, validation_profile,
    compatibility_flags, info_offset, info_length, created_at
) VALUES ($1, $2, 256, 'integration-v1', 'strict_upload', ARRAY[]::text[], 0, 128, $3)`, objectID, objectDigest[:], now); err != nil {
		t.Fatalf("insert H&R integration torrent object: %v", err)
	}
	var torrentID int64
	if err := pool.QueryRow(ctx, `
INSERT INTO torrents.torrents (
    uploader_id, category_id, object_id, info_hash_v1,
    content_name, title, subtitle, total_size_bytes, payload_size_bytes,
    file_count, padding_file_count, piece_length_bytes, piece_count,
    state, version, submitted_at, state_changed_at, updated_at
) VALUES (
    $1, $2, $3, $4,
    'hnr-integration.bin', 'Core H&R Integration', '', 4096, 4096,
    1, 0, 16384, 1,
    'pending_review', 1, $5, $5, $5
)
RETURNING id`, userID, categoryID, objectID, infoDigest[:20], now).Scan(&torrentID); err != nil {
		t.Fatalf("insert H&R integration torrent: %v", err)
	}
	return userID, torrentID
}

func hnrIntegrationEvent(t *testing.T, userID uuid.UUID, torrentID int64, completedAt time.Time, state settlementhnrv1.State) (settlementhnrv1.Event, []byte) {
	t.Helper()
	event := settlementhnrv1.Event{
		SchemaVersion:       settlementhnrv1.SchemaVersion,
		EventID:             mustHNRV7(t).String(),
		OccurredAt:          completedAt.Add(time.Second),
		ObligationID:        uuid.NewString(),
		ObligationVersion:   1,
		UserID:              userID.String(),
		TorrentID:           torrentID,
		CompletedAt:         completedAt,
		State:               state,
		RequiredSeedSeconds: 7200,
		RawUploaded:         256,
		RawDownloaded:       1024,
		RawRatioBasisPoints: 2500,
		RequiredRatioBPS:    10000,
		AssessmentDueAt:     completedAt.Add(7 * 24 * time.Hour),
		GraceEndsAt:         completedAt.Add(10 * 24 * time.Hour),
	}
	if state == settlementhnrv1.StateExempt {
		byExempt := settlementhnrv1.SatisfiedByExempt
		event.RequiredSeedSeconds = 0
		event.RequiredRatioBPS = 0
		event.AssessmentDueAt = completedAt
		event.GraceEndsAt = completedAt
		event.SatisfiedBy = &byExempt
		event.SatisfiedAt = &completedAt
	}
	return event, encodeHNRIntegrationEvent(t, event)
}

func encodeHNRIntegrationEvent(t *testing.T, event settlementhnrv1.Event) []byte {
	t.Helper()
	payload, err := settlementhnrv1.Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func mustHNRV7(t *testing.T) uuid.UUID {
	t.Helper()
	value, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return value
}
