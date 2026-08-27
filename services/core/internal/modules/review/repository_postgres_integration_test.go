package review_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/contracts/trackerevent"
	"github.com/peergo/peergo/services/core/internal/modules/audit"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/notifications"
	"github.com/peergo/peergo/services/core/internal/modules/review"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	"github.com/peergo/peergo/services/core/internal/modules/trackercontrol"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func TestPostgresTorrentReviewCommitsDecisionEvidenceAndTrackerProjection(t *testing.T) {
	databaseURL := os.Getenv("PEERGO_TEST_CORE_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PEERGO_TEST_CORE_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	if err := platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		t.Fatalf("RequireCurrentMigration() error = %v", err)
	}

	// Torrent, object, file, review and outbox evidence is intentionally
	// immutable. Run this test only against a disposable migrated database; it
	// does not weaken production invariants merely to clean up a shared fixture.
	now := time.Now().UTC().Truncate(time.Microsecond)
	uploaderID := insertReviewIntegrationUser(t, ctx, pool, "uploader")
	reviewerID := insertReviewIntegrationUser(t, ctx, pool, "reviewer")
	categoryID := "review-" + uuid.NewString()[:8]
	if _, err := pool.Exec(ctx, `
INSERT INTO catalog.categories (id, name, display_order, enabled, created_at, updated_at)
VALUES ($1, '审核集成测试', 100000 + abs(hashtext($1)) % 900000, true, $2, $2)`, categoryID, now); err != nil {
		t.Fatalf("insert integration category: %v", err)
	}

	auditBuilder, err := audit.NewTorrentReviewEventBuilder(audit.RecorderConfig{
		PseudonymKey: bytes.Repeat([]byte{0x76}, 32), PseudonymKeyEpoch: "integration-2026-08",
	})
	if err != nil {
		t.Fatalf("NewTorrentReviewEventBuilder() error = %v", err)
	}
	repository, err := review.NewPostgresRepository(
		pool,
		auditBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
		trackercontrol.NewEligibilityEventBuilder(nil),
		func(tx pgx.Tx) trackerevent.Appender { return trackercontrol.NewPostgresOutbox(tx) },
		func(tx pgx.Tx) review.NotificationAppender { return notifications.NewPostgresReviewAppender(tx) },
	)
	if err != nil {
		t.Fatalf("NewPostgresRepository() error = %v", err)
	}

	approved := insertPendingReviewTorrent(t, ctx, pool, uploaderID, categoryID, now.Add(-10*time.Minute))
	page, err := repository.ListPending(ctx, 50)
	if err != nil {
		t.Fatalf("ListPending() error = %v", err)
	}
	if !pendingPageContains(page, approved.torrentID) {
		t.Fatalf("pending page does not contain %d: %+v", approved.torrentID, page)
	}

	decisionID := uuid.New()
	approveCommand := review.DecideCommand{
		DecideInput: review.DecideInput{
			DecisionID: decisionID, TorrentID: torrents.TorrentID(approved.torrentID), ExpectedVersion: 1,
			Decision: review.DecisionApprove, ReasonCode: review.ReasonMeetsRequirements,
			Reason: "已核对元数据、文件清单与发布规则，允许正式发布。",
		},
		ReviewerID: reviewerID, OccurredAt: now, Authorization: reviewIntegrationDecision(now),
	}
	result, err := repository.Decide(ctx, approveCommand)
	if err != nil {
		t.Fatalf("Decide(approve) error = %v", err)
	}
	if result.TorrentID != torrents.TorrentID(approved.torrentID) ||
		result.State != torrents.StatePublished || result.Version != 2 || result.DecisionID != decisionID {
		t.Fatalf("Decide(approve) result = %+v", result)
	}

	var state string
	var version, decisionCount, notificationCount, auditCount, trackerCount, catalogCount int64
	var targetSequence int64
	if err := pool.QueryRow(ctx, `
SELECT
    torrent.state,
    torrent.version,
    (SELECT count(*) FROM review.torrent_decisions WHERE torrent_id = torrent.id),
    (SELECT count(*) FROM community.torrent_review_notifications
        WHERE torrent_id = torrent.id),
    (SELECT count(*) FROM audit.outbox
        WHERE event_type = $2
          AND (payload_json::jsonb ->> 'torrent_id')::bigint = torrent.id),
    (SELECT count(*) FROM tracker_control.outbox WHERE aggregate_id = torrent.id),
    (SELECT count(*) FROM catalog.torrents WHERE id = torrent.id),
    (SELECT sequence FROM tracker_control.outbox WHERE aggregate_id = torrent.id)
FROM torrents.torrents AS torrent
WHERE torrent.id = $1`, approved.torrentID, audit.TorrentReviewEventType).Scan(
		&state, &version, &decisionCount, &notificationCount, &auditCount, &trackerCount, &catalogCount, &targetSequence,
	); err != nil {
		t.Fatalf("read approved transaction graph: %v", err)
	}
	if state != string(torrents.StatePublished) || version != 2 || decisionCount != 1 || notificationCount != 1 || auditCount != 1 ||
		trackerCount != 1 || catalogCount != 1 || targetSequence < 1 {
		t.Fatalf("approved state=%s version=%d decisions=%d notifications=%d audit=%d tracker=%d catalog=%d sequence=%d",
			state, version, decisionCount, notificationCount, auditCount, trackerCount, catalogCount, targetSequence)
	}

	replayed, err := repository.Decide(ctx, approveCommand)
	if err != nil || replayed != result {
		t.Fatalf("Decide() replay = %+v, %v; want %+v", replayed, err, result)
	}
	changedCommand := approveCommand
	changedCommand.Reason = "复用同一幂等键却改变审核理由，必须拒绝这次请求。"
	if _, err := repository.Decide(ctx, changedCommand); !errors.Is(err, review.ErrTorrentReviewIdempotencyConflict) {
		t.Fatalf("changed idempotent Decide() error = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE review.torrent_decisions SET reason = reason || 'x' WHERE id = $1`, decisionID); err == nil {
		t.Fatal("immutable review decision unexpectedly accepted an update")
	}

	projectorRepository, err := trackercontrol.NewPostgresRepository(pool)
	if err != nil {
		t.Fatalf("NewPostgresRepository(projector) error = %v", err)
	}
	pendingSnapshot, err := projectorRepository.ReadSnapshot(ctx)
	if err != nil {
		t.Fatalf("ReadSnapshot(pending) error = %v", err)
	}
	if pendingSnapshot.PendingEvents < 1 || pendingSnapshot.ControlSequence >= targetSequence {
		t.Fatalf("pending snapshot = %+v", pendingSnapshot)
	}
	projectUntilSequence(t, ctx, projectorRepository, targetSequence, now.Add(time.Hour))
	entries, err := projectorRepository.ListEnabled(ctx)
	if err != nil {
		t.Fatalf("ListEnabled() error = %v", err)
	}
	var projected *trackercontrol.AllowlistEntry
	for index := range entries {
		if int64(entries[index].TorrentID) == approved.torrentID {
			projected = &entries[index]
			break
		}
	}
	if projected == nil || projected.TorrentID != torrents.TorrentID(approved.torrentID) ||
		projected.InfoHashV1 != approved.infoHash || projected.TorrentVersion != 2 ||
		projected.ControlSequence != targetSequence {
		t.Fatalf("projected allowlist entry = %+v", projected)
	}
	projectionSnapshot, err := projectorRepository.ReadSnapshot(ctx)
	if err != nil {
		t.Fatalf("ReadSnapshot(projected) error = %v", err)
	}
	if projectionSnapshot.PendingEvents != 0 || projectionSnapshot.ControlSequence < targetSequence ||
		!snapshotContains(projectionSnapshot, approved.torrentID) {
		t.Fatalf("projected snapshot = %+v", projectionSnapshot)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM tracker_control.torrent_allowlist_projection WHERE torrent_id = $1`, approved.torrentID); err == nil {
		t.Fatal("allowlist projection unexpectedly accepted a destructive delete")
	}

	rejected := insertPendingReviewTorrent(t, ctx, pool, uploaderID, categoryID, now.Add(-9*time.Minute))
	rejectResult, err := repository.Decide(ctx, review.DecideCommand{
		DecideInput: review.DecideInput{
			DecisionID: uuid.New(), TorrentID: torrents.TorrentID(rejected.torrentID), ExpectedVersion: 1,
			Decision: review.DecisionReject, ReasonCode: review.ReasonMetadataIncomplete,
			Reason: "缺少必要的发布说明与内容校验信息，请补充后重新提交。",
		},
		ReviewerID: reviewerID, OccurredAt: now.Add(time.Second), Authorization: reviewIntegrationDecision(now),
	})
	if err != nil || rejectResult.State != torrents.StateRejected || rejectResult.Version != 2 {
		t.Fatalf("Decide(reject) = %+v, %v", rejectResult, err)
	}
	assertReviewGraph(t, ctx, pool, rejected, string(torrents.StateRejected), 2, 1, 1, 1, 0)

	notificationRepository, err := notifications.NewPostgresRepository(pool)
	if err != nil {
		t.Fatalf("NewPostgresRepository(notifications) error = %v", err)
	}
	notificationPage, err := notificationRepository.List(ctx, uploaderID, notifications.ListQuery{Limit: 20})
	if err != nil || notificationPage.Total != 2 || notificationPage.UnreadCount != 2 || len(notificationPage.Items) != 2 {
		t.Fatalf("List(notifications) page=%+v error=%v", notificationPage, err)
	}
	if notificationPage.Items[0].TorrentReview == nil ||
		int64(notificationPage.Items[0].TorrentReview.TorrentID) != rejected.torrentID ||
		notificationPage.Items[0].TorrentReview.Outcome != torrents.StateRejected ||
		notificationPage.Items[1].TorrentReview == nil ||
		int64(notificationPage.Items[1].TorrentReview.TorrentID) != approved.torrentID ||
		notificationPage.Items[1].TorrentReview.Outcome != torrents.StatePublished {
		t.Fatalf("notification order/content = %+v", notificationPage.Items)
	}
	readReceipt, err := notificationRepository.MarkRead(ctx, uploaderID, notificationPage.Items[0].ID, now.Add(2*time.Second))
	if err != nil || readReceipt.AlreadyRead || !readReceipt.ReadAt.Equal(now.Add(2*time.Second)) {
		t.Fatalf("MarkRead() receipt=%+v error=%v", readReceipt, err)
	}
	replayedRead, err := notificationRepository.MarkRead(ctx, uploaderID, notificationPage.Items[0].ID, now.Add(3*time.Second))
	if err != nil || !replayedRead.AlreadyRead || !replayedRead.ReadAt.Equal(readReceipt.ReadAt) {
		t.Fatalf("MarkRead(replay) receipt=%+v error=%v", replayedRead, err)
	}
	if _, err := notificationRepository.MarkRead(ctx, reviewerID, notificationPage.Items[0].ID, now.Add(3*time.Second)); !errors.Is(err, notifications.ErrNotFound) {
		t.Fatalf("MarkRead(other user) error=%v, want ErrNotFound", err)
	}
	unreadPage, err := notificationRepository.List(ctx, uploaderID, notifications.ListQuery{Limit: 20, UnreadOnly: true})
	if err != nil || unreadPage.Total != 1 || unreadPage.UnreadCount != 1 || len(unreadPage.Items) != 1 || unreadPage.Items[0].ReadAt != nil {
		t.Fatalf("List(unread notifications) page=%+v error=%v", unreadPage, err)
	}
	readAllReceipt, err := notificationRepository.MarkAllRead(ctx, uploaderID, now.Add(4*time.Second))
	if err != nil || readAllReceipt.UpdatedCount != 1 {
		t.Fatalf("MarkAllRead() receipt=%+v error=%v", readAllReceipt, err)
	}
	summary, err := notificationRepository.Summary(ctx, uploaderID)
	if err != nil || summary.UnreadCount != 0 {
		t.Fatalf("Summary()=%+v error=%v", summary, err)
	}
	archiveReceipt, err := notificationRepository.ArchiveAll(ctx, uploaderID, now.Add(5*time.Second))
	if err != nil || archiveReceipt.UpdatedCount != 2 {
		t.Fatalf("ArchiveAll() receipt=%+v error=%v", archiveReceipt, err)
	}
	archivedPage, err := notificationRepository.List(ctx, uploaderID, notifications.ListQuery{Limit: 20})
	if err != nil || archivedPage.Total != 0 || archivedPage.UnreadCount != 0 || len(archivedPage.Items) != 0 {
		t.Fatalf("List(archived notifications) page=%+v error=%v", archivedPage, err)
	}
	var persistedNotifications int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM community.torrent_review_notifications WHERE recipient_user_id = $1`, uploaderID).Scan(&persistedNotifications); err != nil || persistedNotifications != 2 {
		t.Fatalf("persisted notification count=%d error=%v", persistedNotifications, err)
	}
	feedbackReceipt, err := notificationRepository.CreateFeedback(ctx, uploaderID, notifications.CreateFeedbackInput{
		Title:   "页面使用建议",
		Content: "消息页在窄屏下的操作区需要保持可读，请管理员后续核对。",
	}, now.Add(6*time.Second))
	if err != nil || feedbackReceipt.FeedbackID == uuid.Nil || !feedbackReceipt.CreatedAt.Equal(now.Add(6*time.Second)) {
		t.Fatalf("CreateFeedback() receipt=%+v error=%v", feedbackReceipt, err)
	}
	var persistedFeedbacks int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM community.support_feedbacks WHERE sender_user_id = $1 AND id = $2`, uploaderID, feedbackReceipt.FeedbackID).Scan(&persistedFeedbacks); err != nil || persistedFeedbacks != 1 {
		t.Fatalf("persisted feedback count=%d error=%v", persistedFeedbacks, err)
	}

	resubmissionRepository, err := review.NewPostgresResubmissionRepository(
		pool,
		func(tx pgx.Tx) trackerevent.Appender { return trackercontrol.NewPostgresOutbox(tx) },
	)
	if err != nil {
		t.Fatalf("NewPostgresResubmissionRepository() error = %v", err)
	}
	resubmissionID := uuid.New()
	resubmittedAt := now.Add(7 * time.Second)
	resubmissionCommand := review.ResubmitCommand{
		ID: resubmissionID, TorrentID: torrents.TorrentID(rejected.torrentID),
		ExpectedVersion: 2, UploaderID: uploaderID,
		Metadata: torrents.EditableMetadata{
			CategoryID: categoryID, Title: fmt.Sprintf("Review corrected %d", rejected.torrentID),
			Subtitle: "已补齐审核要求的发布说明",
		},
		CorrectionNote: "已根据审核反馈补齐发布标题和副标题，请重新核对。",
		OccurredAt:     resubmittedAt, Authorization: reviewIntegrationDecision(now),
	}
	resubmissionResult, err := resubmissionRepository.Resubmit(ctx, resubmissionCommand)
	if err != nil || resubmissionResult.ID != resubmissionID ||
		int64(resubmissionResult.TorrentID) != rejected.torrentID ||
		resubmissionResult.State != torrents.StatePendingReview || resubmissionResult.Version != 3 ||
		resubmissionResult.Metadata != resubmissionCommand.Metadata ||
		!resubmissionResult.ReviewRequestedAt.Equal(resubmittedAt) {
		t.Fatalf("Resubmit()=%+v error=%v", resubmissionResult, err)
	}
	resubmissionReplay, err := resubmissionRepository.Resubmit(ctx, resubmissionCommand)
	if err != nil || resubmissionReplay != resubmissionResult {
		t.Fatalf("Resubmit(replay)=%+v error=%v", resubmissionReplay, err)
	}
	changedResubmission := resubmissionCommand
	changedResubmission.CorrectionNote = "复用相同请求标识但替换整改说明，必须拒绝本次请求。"
	if _, err := resubmissionRepository.Resubmit(ctx, changedResubmission); !errors.Is(err, review.ErrTorrentResubmissionIdempotencyConflict) {
		t.Fatalf("Resubmit(changed replay) error=%v", err)
	}
	otherUploader := resubmissionCommand
	otherUploader.ID = uuid.New()
	otherUploader.UploaderID = reviewerID
	if _, err := resubmissionRepository.Resubmit(ctx, otherUploader); !errors.Is(err, review.ErrTorrentResubmissionNotFound) {
		t.Fatalf("Resubmit(other uploader) error=%v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE review.torrent_resubmissions SET correction_note = correction_note || 'x' WHERE id = $1`, resubmissionID); err == nil {
		t.Fatal("immutable torrent resubmission unexpectedly accepted an update")
	}
	assertReviewGraph(t, ctx, pool, rejected, string(torrents.StatePendingReview), 3, 1, 1, 2, 0)
	resubmittedPage, err := repository.ListPending(ctx, 50)
	if err != nil {
		t.Fatalf("ListPending(after resubmission) error=%v", err)
	}
	var queuedResubmission *review.PendingTorrent
	for index := range resubmittedPage.Items {
		if int64(resubmittedPage.Items[index].ID) == rejected.torrentID {
			queuedResubmission = &resubmittedPage.Items[index]
			break
		}
	}
	if queuedResubmission == nil || queuedResubmission.Version != 3 ||
		queuedResubmission.Title != resubmissionCommand.Metadata.Title ||
		!queuedResubmission.ReviewRequestedAt.Equal(resubmittedAt) ||
		!queuedResubmission.SubmittedAt.Before(queuedResubmission.ReviewRequestedAt) {
		t.Fatalf("resubmitted review queue item=%+v", queuedResubmission)
	}

	selfReviewed := insertPendingReviewTorrent(t, ctx, pool, reviewerID, categoryID, now.Add(-8*time.Minute))
	if _, err := repository.Decide(ctx, review.DecideCommand{
		DecideInput: review.DecideInput{
			DecisionID: uuid.New(), TorrentID: torrents.TorrentID(selfReviewed.torrentID), ExpectedVersion: 1,
			Decision: review.DecisionApprove, ReasonCode: review.ReasonMeetsRequirements,
			Reason: "即便内容符合要求，上传者也不能审核自己的种子。",
		},
		ReviewerID: reviewerID, OccurredAt: now.Add(2 * time.Second), Authorization: reviewIntegrationDecision(now),
	}); !errors.Is(err, review.ErrTorrentReviewSelf) {
		t.Fatalf("self review error = %v", err)
	}
	assertReviewGraph(t, ctx, pool, selfReviewed, string(torrents.StatePendingReview), 1, 0, 0, 0, 0)

	rollbackTarget := insertPendingReviewTorrent(t, ctx, pool, uploaderID, categoryID, now.Add(-7*time.Minute))
	failingRepository, err := review.NewPostgresRepository(
		pool,
		auditBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
		trackercontrol.NewEligibilityEventBuilder(nil),
		func(pgx.Tx) trackerevent.Appender { return failingTrackerAppender{} },
		func(tx pgx.Tx) review.NotificationAppender { return notifications.NewPostgresReviewAppender(tx) },
	)
	if err != nil {
		t.Fatalf("NewPostgresRepository(failing) error = %v", err)
	}
	if _, err := failingRepository.Decide(ctx, review.DecideCommand{
		DecideInput: review.DecideInput{
			DecisionID: uuid.New(), TorrentID: torrents.TorrentID(rollbackTarget.torrentID), ExpectedVersion: 1,
			Decision: review.DecisionApprove, ReasonCode: review.ReasonMeetsRequirements,
			Reason: "该决定会在 Tracker 事件追加失败后验证整笔事务回滚。",
		},
		ReviewerID: reviewerID, OccurredAt: now.Add(3 * time.Second), Authorization: reviewIntegrationDecision(now),
	}); !errors.Is(err, errIntegrationTrackerAppend) {
		t.Fatalf("failed Tracker append error = %v", err)
	}
	assertReviewGraph(t, ctx, pool, rollbackTarget, string(torrents.StatePendingReview), 1, 0, 0, 0, 0)
}

func snapshotContains(snapshot trackercontrol.ProjectionSnapshot, torrentID int64) bool {
	for _, entry := range snapshot.Torrents {
		if int64(entry.TorrentID) == torrentID {
			return true
		}
	}
	return false
}

type reviewTorrentFixture struct {
	torrentID int64
	infoHash  torrents.InfoHashV1
}

func insertReviewIntegrationUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label string) uuid.UUID {
	t.Helper()
	userID := uuid.New()
	credentialRef := uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)
	username := fmt.Sprintf("review-%s-%s", label, uuid.NewString()[:8])
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.users (
    id, credential_ref, username, display_name, status,
    email_verified_at, created_at, updated_at, password_changed_at
) VALUES ($1, $2, $3, $4, 'active', $5, $5, $5, $5)`,
		userID, credentialRef, username, "审核集成测试 "+label, now,
	); err != nil {
		t.Fatalf("insert %s integration user: %v", label, err)
	}
	return userID
}

func insertPendingReviewTorrent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, uploaderID uuid.UUID, categoryID string, submittedAt time.Time) reviewTorrentFixture {
	t.Helper()
	objectID := uuid.New()
	objectDigest := sha256.Sum256([]byte("review-object-" + objectID.String()))
	infoDigest := sha256.Sum256([]byte("review-info-" + objectID.String()))
	var infoHash torrents.InfoHashV1
	copy(infoHash[:], infoDigest[:len(infoHash)])
	submittedAt = submittedAt.UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `
INSERT INTO torrents.torrent_objects (
    id, content_sha256, byte_length, parser_version, validation_profile,
    compatibility_flags, info_offset, info_length, created_at
) VALUES ($1, $2, 256, 'integration-v1', 'strict_upload', ARRAY[]::text[], 0, 128, $3)`,
		objectID, objectDigest[:], submittedAt,
	); err != nil {
		t.Fatalf("insert integration torrent object: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO torrents.torrent_object_locations (
    object_id, backend_id, object_key, state, is_preferred,
    observed_byte_length, observed_sha256, verified_at, created_at, updated_at
) VALUES ($1, 'integration-local', $2, 'verified', true, 256, $3, $4, $4, $4)`,
		objectID, "review/"+objectID.String()+".torrent", objectDigest[:], submittedAt,
	); err != nil {
		t.Fatalf("insert verified integration location: %v", err)
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
    'integration.bin', $5, '审核事务与 Tracker 投影', 4096, 4096,
    1, 0, 16384, 1,
    'pending_review', 1, $6, $6, $6
)
RETURNING id`, uploaderID, categoryID, objectID, infoHash[:], "Review "+objectID.String()[:8], submittedAt).Scan(&torrentID); err != nil {
		t.Fatalf("insert pending integration torrent: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO torrents.torrent_files (
    torrent_id, file_index, path_components, display_path, size_bytes, is_padding, created_at
) VALUES ($1, 0, ARRAY['integration.bin'], 'integration.bin', 4096, false, $2)`, torrentID, submittedAt); err != nil {
		t.Fatalf("insert integration torrent file: %v", err)
	}
	return reviewTorrentFixture{torrentID: torrentID, infoHash: infoHash}
}

func pendingPageContains(page review.PendingTorrentPage, torrentID int64) bool {
	for _, item := range page.Items {
		if int64(item.ID) == torrentID {
			return true
		}
	}
	return false
}

func reviewIntegrationDecision(now time.Time) authz.Decision {
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
		GrantID: uuid.New(), GrantVersion: 1, RoleID: "torrent_reviewer", MandateID: uuid.New(),
		EffectiveUntil: now.Add(time.Hour),
	}
}

func projectUntilSequence(t *testing.T, ctx context.Context, repository *trackercontrol.PostgresRepository, targetSequence int64, now time.Time) {
	t.Helper()
	for attempts := 0; attempts < 1000; attempts++ {
		status, err := repository.Status(ctx)
		if err != nil {
			t.Fatalf("Status() error = %v", err)
		}
		if status.LastSequence >= targetSequence {
			return
		}
		pending, found, err := repository.ClaimNext(ctx, now, time.Minute)
		if err != nil {
			t.Fatalf("ClaimNext() error = %v", err)
		}
		if !found {
			t.Fatalf("Tracker control sequence %d is not claimable; status = %+v", targetSequence, status)
		}
		if err := repository.Apply(ctx, pending, now.Add(time.Duration(attempts+1)*time.Microsecond)); err != nil {
			t.Fatalf("Apply(sequence=%d) error = %v", pending.Sequence, err)
		}
	}
	t.Fatalf("Tracker control sequence %d was not projected within the safety limit", targetSequence)
}

func assertReviewGraph(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture reviewTorrentFixture, wantState string, wantVersion, wantDecisions, wantAudit, wantTracker, wantCatalog int64) {
	t.Helper()
	var state string
	var version, decisions, notificationEvents, auditEvents, trackerEvents, catalogRows int64
	if err := pool.QueryRow(ctx, `
SELECT
    torrent.state,
    torrent.version,
    (SELECT count(*) FROM review.torrent_decisions WHERE torrent_id = torrent.id),
    (SELECT count(*) FROM community.torrent_review_notifications
        WHERE torrent_id = torrent.id),
    (SELECT count(*) FROM audit.outbox
        WHERE event_type = $2
          AND (payload_json::jsonb ->> 'torrent_id')::bigint = torrent.id),
    (SELECT count(*) FROM tracker_control.outbox WHERE aggregate_id = torrent.id),
    (SELECT count(*) FROM catalog.torrents WHERE id = torrent.id)
FROM torrents.torrents AS torrent
WHERE torrent.id = $1`, fixture.torrentID, audit.TorrentReviewEventType).Scan(
		&state, &version, &decisions, &notificationEvents, &auditEvents, &trackerEvents, &catalogRows,
	); err != nil {
		t.Fatalf("read review transaction graph: %v", err)
	}
	if state != wantState || version != wantVersion || decisions != wantDecisions || notificationEvents != wantDecisions || auditEvents != wantAudit ||
		trackerEvents != wantTracker || catalogRows != wantCatalog {
		t.Fatalf("review graph state=%s version=%d decisions=%d notifications=%d audit=%d tracker=%d catalog=%d",
			state, version, decisions, notificationEvents, auditEvents, trackerEvents, catalogRows)
	}
}

var errIntegrationTrackerAppend = errors.New("integration Tracker append failed")

type failingTrackerAppender struct{}

func (failingTrackerAppender) Append(context.Context, trackerevent.Event) error {
	return errIntegrationTrackerAppend
}
