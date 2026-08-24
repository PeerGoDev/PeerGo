package catalog_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/audit"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
)

type failingSiteDisplaySettingsEventBuilder struct{}

func (failingSiteDisplaySettingsEventBuilder) BuildSiteDisplaySettingsEvent(catalog.SiteDisplaySettingsAuditInput) (auditevent.Event, error) {
	return auditevent.Event{}, errors.New("synthetic audit failure")
}

func TestPostgresSiteDisplaySettingsCommitsVersionedStateAndAuditTogether(t *testing.T) {
	databaseURL := os.Getenv("PEERGO_TEST_CORE_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PEERGO_TEST_CORE_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)

	original := struct {
		name                   string
		description            string
		torrentFilenamePrefix  string
		customNavigationItems  []byte
		defaultView            string
		showLatestAnnouncement bool
		version                int64
		effectiveAt            time.Time
		updatedAt              time.Time
	}{}
	if err := pool.QueryRow(ctx, `
SELECT name, description, torrent_filename_prefix, custom_navigation_items, default_torrent_view, show_latest_announcement,
       version, effective_at, updated_at
FROM catalog.site_profile
WHERE singleton = true`).Scan(
		&original.name, &original.description, &original.torrentFilenamePrefix, &original.customNavigationItems, &original.defaultView,
		&original.showLatestAnnouncement, &original.version,
		&original.effectiveAt, &original.updatedAt,
	); err != nil {
		t.Fatalf("read original site display settings: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		// This restores the shared integration fixture exactly. The test-only
		// direct write is outside application code; the emitted evidence remains
		// immutable so production behavior is still observable after the test.
		_, _ = pool.Exec(cleanupCtx, `
UPDATE catalog.site_profile
SET name = $1,
    description = $2,
    torrent_filename_prefix = $3,
    custom_navigation_items = $4::jsonb,
    default_torrent_view = $5,
    show_latest_announcement = $6,
    version = $7,
    effective_at = $8,
    updated_at = $9
WHERE singleton = true`,
			original.name, original.description, original.torrentFilenamePrefix, original.customNavigationItems, original.defaultView,
			original.showLatestAnnouncement, original.version,
			original.effectiveAt, original.updatedAt,
		)
	})

	now := time.Now().UTC().Truncate(time.Microsecond)
	newName := "PeerGo Integration " + uuid.NewString()[:8]
	newDescription := "集成测试验证站点与展示设置的原子版本写入。"
	newTorrentFilenamePrefix := "[INTEGRATION]"
	newCustomNavigationURL := "https://wiki.example.com/" + uuid.NewString()
	reason := "集成测试验证设置版本、事务回滚和审计内容脱敏。"
	newView := catalog.TorrentViewPoster
	if original.defaultView == string(catalog.TorrentViewPoster) {
		newView = catalog.TorrentViewList
	}
	command := catalog.UpdateSiteDisplaySettingsCommand{
		UpdateSiteDisplaySettingsInput: catalog.UpdateSiteDisplaySettingsInput{
			Name: newName, Description: newDescription, DefaultTorrentView: newView,
			TorrentFilenamePrefix:  newTorrentFilenamePrefix,
			CustomNavigationItems:  []catalog.CustomNavigationItem{{Label: "Wiki", URL: newCustomNavigationURL, OpenInNewTab: true, Enabled: true}},
			ShowLatestAnnouncement: !original.showLatestAnnouncement,
			ExpectedVersion:        original.version, Reason: reason,
		},
		ActorID: uuid.New(), OccurredAt: now, Authorization: siteDisplayIntegrationDecision(now),
	}

	failingRepository, err := catalog.NewPostgresSiteDisplaySettingsRepository(
		pool,
		failingSiteDisplaySettingsEventBuilder{},
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		t.Fatalf("NewPostgresSiteDisplaySettingsRepository(failing) error = %v", err)
	}
	if _, err := failingRepository.UpdateSiteDisplaySettings(ctx, command); err == nil {
		t.Fatal("UpdateSiteDisplaySettings() with failing audit builder error = nil")
	}
	var versionAfterRollback int64
	var nameAfterRollback string
	if err := pool.QueryRow(ctx, `SELECT name, version FROM catalog.site_profile WHERE singleton = true`).Scan(&nameAfterRollback, &versionAfterRollback); err != nil {
		t.Fatalf("read rolled back settings: %v", err)
	}
	if nameAfterRollback != original.name || versionAfterRollback != original.version {
		t.Fatalf("audit failure committed name=%q version=%d", nameAfterRollback, versionAfterRollback)
	}

	eventID := uuid.New()
	eventBuilder, err := audit.NewSiteDisplaySettingsChangeEventBuilder(audit.RecorderConfig{
		PseudonymKey: bytes.Repeat([]byte{0x73}, 32), PseudonymKeyEpoch: "integration-2026-08",
		NewEventID: func() uuid.UUID { return eventID },
	})
	if err != nil {
		t.Fatalf("NewSiteDisplaySettingsChangeEventBuilder() error = %v", err)
	}
	repository, err := catalog.NewPostgresSiteDisplaySettingsRepository(
		pool,
		eventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		t.Fatalf("NewPostgresSiteDisplaySettingsRepository() error = %v", err)
	}
	updated, err := repository.UpdateSiteDisplaySettings(ctx, command)
	if err != nil {
		t.Fatalf("UpdateSiteDisplaySettings() error = %v", err)
	}
	if updated.Name != newName || updated.TorrentFilenamePrefix != newTorrentFilenamePrefix || len(updated.CustomNavigationItems) != 1 || updated.CustomNavigationItems[0].URL != newCustomNavigationURL || updated.Version != original.version+1 || updated.DefaultTorrentView != newView || updated.ShowLatestAnnouncement == original.showLatestAnnouncement || !updated.EffectiveAt.Equal(now) {
		t.Fatalf("updated settings = %+v", updated)
	}

	_, err = repository.UpdateSiteDisplaySettings(ctx, command)
	if !errors.Is(err, catalog.ErrSiteDisplaySettingsVersionConflict) {
		t.Fatalf("stale UpdateSiteDisplaySettings() error = %v, want version conflict", err)
	}

	var eventCount int
	var leakedEditableText bool
	if err := pool.QueryRow(ctx, `
SELECT count(*), bool_or(
    payload_json LIKE '%' || $2 || '%'
    OR payload_json LIKE '%' || $3 || '%'
    OR payload_json LIKE '%' || $4 || '%'
    OR payload_json LIKE '%' || $5 || '%'
    OR payload_json LIKE '%' || $6 || '%'
)
FROM audit.outbox
WHERE event_id = $1`, eventID, newName, newDescription, newTorrentFilenamePrefix, reason, newCustomNavigationURL).Scan(&eventCount, &leakedEditableText); err != nil {
		t.Fatalf("read site display settings audit event: %v", err)
	}
	if eventCount != 1 || leakedEditableText {
		t.Fatalf("site display audit event count=%d leaked editable text=%t", eventCount, leakedEditableText)
	}
}

func siteDisplayIntegrationDecision(now time.Time) authz.Decision {
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
		GrantID: uuid.New(), GrantVersion: 1, RoleID: "site_display_manager", MandateID: uuid.New(),
		EffectiveUntil: now.Add(time.Hour),
	}
}
