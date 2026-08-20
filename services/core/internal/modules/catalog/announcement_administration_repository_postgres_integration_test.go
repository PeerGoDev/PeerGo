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

func TestPostgresAnnouncementAdministrationKeepsDraftScheduleAndAuditAtomic(t *testing.T) {
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

	var actorID uuid.UUID
	if err := pool.QueryRow(ctx, `
SELECT id
FROM identity.users
WHERE status = 'active'
ORDER BY created_at, id
LIMIT 1`).Scan(&actorID); errors.Is(err, pgx.ErrNoRows) {
		t.Skip("integration database has no active identity fixture")
	} else if err != nil {
		t.Fatalf("read actor fixture: %v", err)
	}

	eventBuilder, err := audit.NewAnnouncementChangeEventBuilder(audit.RecorderConfig{
		PseudonymKey: bytes.Repeat([]byte{0x61}, 32), PseudonymKeyEpoch: "integration-2026-08",
	})
	if err != nil {
		t.Fatalf("NewAnnouncementChangeEventBuilder() error = %v", err)
	}
	repository, err := catalog.NewPostgresAnnouncementAdministrationRepository(
		pool,
		eventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		t.Fatalf("NewPostgresAnnouncementAdministrationRepository() error = %v", err)
	}
	publicRepository := catalog.NewPostgresRepository(pool)

	now := time.Now().UTC().Truncate(time.Microsecond)
	announcementID := "integration-" + uuid.NewString()
	firstTitle := "公告集成测试初稿"
	secondTitle := "公告集成测试排期稿"
	createReason := "集成测试验证公告草稿与审计证据原子提交。"
	updateReason := "集成测试验证新草稿不会覆盖已有公开版本。"
	decision := announcementIntegrationDecision(now)

	created, err := repository.CreateAnnouncementDraft(ctx, catalog.CreateAnnouncementDraftCommand{
		CreateAnnouncementDraftInput: catalog.CreateAnnouncementDraftInput{
			ID: announcementID, Title: firstTitle, Summary: "第一版摘要", Body: "第一版正文。",
			BodyFormat: catalog.AnnouncementBodyPlainText, Reason: createReason,
		},
		ActorID: actorID, OccurredAt: now, Authorization: decision,
	})
	if err != nil {
		t.Fatalf("CreateAnnouncementDraft() error = %v", err)
	}
	if created.Status != catalog.ManagedAnnouncementDraft || created.Version != 1 || created.RevisionNumber != 1 {
		t.Fatalf("created announcement = %+v", created)
	}
	if _, err := publicRepository.Announcement(ctx, announcementID); !errors.Is(err, catalog.ErrAnnouncementNotFound) {
		t.Fatalf("draft public lookup error = %v, want not found", err)
	}
	if summary := publicAnnouncementSummary(t, ctx, publicRepository, announcementID); summary != nil {
		t.Fatalf("draft appeared in public list: %+v", summary)
	}

	published, err := repository.ChangeAnnouncementPublication(ctx, catalog.ChangeAnnouncementPublicationCommand{
		ChangeAnnouncementPublicationInput: catalog.ChangeAnnouncementPublicationInput{
			ID: announcementID, Action: catalog.AnnouncementPublishNow, ExpectedVersion: 1,
			Reason: "第一版已完成复核并立即发布。",
		},
		ActorID: actorID, OccurredAt: now.Add(time.Second), Authorization: decision,
	})
	if err != nil {
		t.Fatalf("publish ChangeAnnouncementPublication() error = %v", err)
	}
	if published.Status != catalog.ManagedAnnouncementPublished || published.Version != 2 || published.HasUnpublishedChanges {
		t.Fatalf("published announcement = %+v", published)
	}
	if summary := publicAnnouncementSummary(t, ctx, publicRepository, announcementID); summary == nil || summary.Title != firstTitle {
		t.Fatalf("published list summary = %+v", summary)
	}

	updated, err := repository.UpdateAnnouncementDraft(ctx, catalog.UpdateAnnouncementDraftCommand{
		UpdateAnnouncementDraftInput: catalog.UpdateAnnouncementDraftInput{
			ID: announcementID, Title: secondTitle, Summary: "第二版摘要", Body: "第二版正文。",
			BodyFormat: catalog.AnnouncementBodyPlainText, ExpectedVersion: 2, Reason: updateReason,
		},
		ActorID: actorID, OccurredAt: now.Add(2 * time.Second), Authorization: decision,
	})
	if err != nil {
		t.Fatalf("UpdateAnnouncementDraft() error = %v", err)
	}
	if updated.Version != 3 || updated.RevisionNumber != 2 || !updated.HasUnpublishedChanges {
		t.Fatalf("updated announcement = %+v", updated)
	}
	publicBeforeSchedule, err := publicRepository.Announcement(ctx, announcementID)
	if err != nil || publicBeforeSchedule.Title != firstTitle || publicBeforeSchedule.Version != 1 {
		t.Fatalf("public announcement before schedule = %+v, error = %v", publicBeforeSchedule, err)
	}

	scheduledFor := time.Now().UTC().Add(2 * time.Second).Truncate(time.Microsecond)
	scheduled, err := repository.ChangeAnnouncementPublication(ctx, catalog.ChangeAnnouncementPublicationCommand{
		ChangeAnnouncementPublicationInput: catalog.ChangeAnnouncementPublicationInput{
			ID: announcementID, Action: catalog.AnnouncementSchedule, ExpectedVersion: 3,
			ScheduledFor: &scheduledFor, Reason: "第二版已复核并按测试窗口预约发布。",
		},
		ActorID: actorID, OccurredAt: time.Now().UTC(), Authorization: decision,
	})
	if err != nil {
		t.Fatalf("schedule ChangeAnnouncementPublication() error = %v", err)
	}
	if scheduled.Status != catalog.ManagedAnnouncementScheduled || scheduled.Version != 4 || scheduled.HasUnpublishedChanges {
		t.Fatalf("scheduled announcement = %+v", scheduled)
	}
	publicBeforeEffective, err := publicRepository.Announcement(ctx, announcementID)
	if err != nil || publicBeforeEffective.Title != firstTitle {
		t.Fatalf("public announcement before effective time = %+v, error = %v", publicBeforeEffective, err)
	}
	if summary := publicAnnouncementSummary(t, ctx, publicRepository, announcementID); summary == nil || summary.Title != firstTitle {
		t.Fatalf("scheduled-before-effective list summary = %+v", summary)
	}

	publicAfterEffective := waitForAnnouncementTitle(t, ctx, publicRepository, announcementID, secondTitle, 5*time.Second)
	if publicAfterEffective.Version != 2 || !publicAfterEffective.PublishedAt.Equal(scheduledFor) || publicAfterEffective.UpdatedAt.Before(publicAfterEffective.PublishedAt) {
		t.Fatalf("public announcement after effective time = %+v", publicAfterEffective)
	}
	if summary := publicAnnouncementSummary(t, ctx, publicRepository, announcementID); summary == nil || summary.Title != secondTitle || !summary.PublishedAt.Equal(scheduledFor) {
		t.Fatalf("scheduled-after-effective list summary = %+v", summary)
	}

	withdrawn, err := repository.ChangeAnnouncementPublication(ctx, catalog.ChangeAnnouncementPublicationCommand{
		ChangeAnnouncementPublicationInput: catalog.ChangeAnnouncementPublicationInput{
			ID: announcementID, Action: catalog.AnnouncementWithdraw, ExpectedVersion: 4,
			Reason: "集成测试完成，撤回公开入口但保留版本证据。",
		},
		ActorID: actorID, OccurredAt: time.Now().UTC(), Authorization: decision,
	})
	if err != nil {
		t.Fatalf("withdraw ChangeAnnouncementPublication() error = %v", err)
	}
	if withdrawn.Status != catalog.ManagedAnnouncementWithdrawn || withdrawn.Version != 5 {
		t.Fatalf("withdrawn announcement = %+v", withdrawn)
	}
	if _, err := publicRepository.Announcement(ctx, announcementID); !errors.Is(err, catalog.ErrAnnouncementNotFound) {
		t.Fatalf("withdrawn public lookup error = %v, want not found", err)
	}
	if summary := publicAnnouncementSummary(t, ctx, publicRepository, announcementID); summary != nil {
		t.Fatalf("withdrawn announcement appeared in public list: %+v", summary)
	}

	revisions, err := repository.ListAnnouncementRevisions(ctx, announcementID, 50, 0)
	if err != nil {
		t.Fatalf("ListAnnouncementRevisions() error = %v", err)
	}
	if revisions.Total != 2 || len(revisions.Items) != 2 || revisions.Items[0].RevisionNumber != 2 {
		t.Fatalf("revision page = %+v", revisions)
	}

	var eventCount int
	var leakedEditableText bool
	if err := pool.QueryRow(ctx, `
SELECT count(*), bool_or(
    payload_json LIKE '%' || $3 || '%'
    OR payload_json LIKE '%' || $4 || '%'
    OR payload_json LIKE '%' || $5 || '%'
    OR payload_json LIKE '%' || $6 || '%'
)
FROM audit.outbox
WHERE event_type = $1
  AND payload_json::jsonb ->> 'announcement_id' = $2`,
		audit.AnnouncementChangeEventType,
		announcementID,
		firstTitle,
		secondTitle,
		createReason,
		updateReason,
	).Scan(&eventCount, &leakedEditableText); err != nil {
		t.Fatalf("read announcement audit events: %v", err)
	}
	if eventCount != 5 || leakedEditableText {
		t.Fatalf("announcement audit event count=%d leaked editable text=%t", eventCount, leakedEditableText)
	}
	// Announcement revisions deliberately have no cleanup hook: an integration
	// database must be disposable because production evidence is immutable.
}

func publicAnnouncementSummary(t *testing.T, ctx context.Context, repository *catalog.PostgresRepository, announcementID string) *catalog.AnnouncementSummary {
	t.Helper()
	items, _, err := repository.ListAnnouncements(ctx, 50, 0)
	if err != nil {
		t.Fatalf("list public announcements: %v", err)
	}
	for _, item := range items {
		if item.ID == announcementID {
			copy := item
			return &copy
		}
	}
	return nil
}

func waitForAnnouncementTitle(t *testing.T, ctx context.Context, repository *catalog.PostgresRepository, announcementID, title string, timeout time.Duration) catalog.Announcement {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		announcement, err := repository.Announcement(ctx, announcementID)
		if err == nil && announcement.Title == title {
			return announcement
		}
		if err != nil && !errors.Is(err, catalog.ErrAnnouncementNotFound) {
			t.Fatalf("poll public announcement: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("announcement %q did not expose title %q before timeout", announcementID, title)
	return catalog.Announcement{}
}

func announcementIntegrationDecision(now time.Time) authz.Decision {
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
		GrantID: uuid.New(), GrantVersion: 1, RoleID: "announcement_manager", MandateID: uuid.New(),
		EffectiveUntil: now.Add(time.Hour),
	}
}
