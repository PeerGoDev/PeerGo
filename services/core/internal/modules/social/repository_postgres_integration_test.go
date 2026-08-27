package social

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/economy"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func TestPostgresCommentRepositoryPreservesThreadHistoryAndOwnedWrites(t *testing.T) {
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

	// Torrent evidence and comment revisions are intentionally immutable. This
	// integration case therefore targets a disposable migrated database and
	// leaves uniquely named fixtures instead of weakening production triggers.
	now := time.Now().UTC().Truncate(time.Microsecond)
	authorID := insertCommentIntegrationUser(t, ctx, pool, "author", now)
	otherID := insertCommentIntegrationUser(t, ctx, pool, "other", now)
	torrentID := insertPublishedCommentTorrent(t, ctx, pool, authorID, now)
	torrentTarget := TorrentCommentTarget(torrentID)
	announcementID := insertPublishedCommentAnnouncement(t, ctx, pool, now)
	announcementTarget := AnnouncementCommentTarget(announcementID)
	repository, err := NewPostgresCommentRepository(pool)
	if err != nil {
		t.Fatalf("NewPostgresCommentRepository() error = %v", err)
	}

	rootRequestID, rootPublicID := uuid.New(), uuid.New()
	rootCommand := createCommentCommand{
		PublicID: rootPublicID, RequestID: rootRequestID, Target: torrentTarget,
		AuthorID: authorID, Body: "已完成文件与音轨校验。",
		CreateBodySHA256: sha256.Sum256([]byte("已完成文件与音轨校验。")), CreatedAt: now.Add(time.Second),
	}
	root, err := repository.Create(ctx, rootCommand)
	if err != nil || root.ID != rootPublicID || root.Version != 1 || root.State != CommentVisible {
		t.Fatalf("Create(root) comment=%+v error=%v", root, err)
	}
	replayed, err := repository.Create(ctx, rootCommand)
	if err != nil || !reflect.DeepEqual(replayed, root) {
		t.Fatalf("Create(root replay) comment=%+v error=%v", replayed, err)
	}
	changedReplay := rootCommand
	changedReplay.Body = "复用请求标识但改变正文。"
	changedReplay.CreateBodySHA256 = sha256.Sum256([]byte(changedReplay.Body))
	if _, err := repository.Create(ctx, changedReplay); !errors.Is(err, ErrCommentIdempotencyConflict) {
		t.Fatalf("Create(changed replay) error=%v", err)
	}

	replyPublicID := uuid.New()
	replyBody := "收到，感谢核验。"
	reply, err := repository.Create(ctx, createCommentCommand{
		PublicID: replyPublicID, RequestID: uuid.New(), Target: torrentTarget,
		ParentCommentID: &rootPublicID, AuthorID: otherID, Body: replyBody,
		CreateBodySHA256: sha256.Sum256([]byte(replyBody)), CreatedAt: now.Add(2 * time.Second),
	})
	if err != nil || reply.ParentCommentID == nil || *reply.ParentCommentID != rootPublicID {
		t.Fatalf("Create(reply) comment=%+v error=%v", reply, err)
	}
	nestedReplyPublicID := uuid.New()
	nestedReplyBody := "直接回复关系需要保留，但展示仍压平一层。"
	nestedReply, err := repository.Create(ctx, createCommentCommand{
		PublicID: nestedReplyPublicID, RequestID: uuid.New(), Target: torrentTarget,
		ParentCommentID: &replyPublicID, AuthorID: authorID, Body: nestedReplyBody,
		CreateBodySHA256: sha256.Sum256([]byte(nestedReplyBody)), CreatedAt: now.Add(3 * time.Second),
	})
	if err != nil || nestedReply.ParentCommentID == nil || *nestedReply.ParentCommentID != replyPublicID {
		t.Fatalf("Create(reply to reply) comment=%+v error=%v", nestedReply, err)
	}

	announcementBody := "公告评论与种子评论共用服务，但绑定仍由公告外键约束。"
	if _, err := repository.Create(ctx, createCommentCommand{
		PublicID: uuid.New(), RequestID: uuid.New(), Target: announcementTarget,
		ParentCommentID: &rootPublicID, AuthorID: otherID, Body: announcementBody,
		CreateBodySHA256: sha256.Sum256([]byte(announcementBody)), CreatedAt: now.Add(3500 * time.Millisecond),
	}); !errors.Is(err, ErrCommentParentNotFound) {
		t.Fatalf("Create(cross-target announcement reply) error=%v", err)
	}
	announcementPublicID := uuid.New()
	announcementComment, err := repository.Create(ctx, createCommentCommand{
		PublicID: announcementPublicID, RequestID: uuid.New(), Target: announcementTarget,
		AuthorID: otherID, Body: announcementBody,
		CreateBodySHA256: sha256.Sum256([]byte(announcementBody)), CreatedAt: now.Add(3500 * time.Millisecond),
	})
	if err != nil || announcementComment.Target != announcementTarget || announcementComment.ID != announcementPublicID {
		t.Fatalf("Create(announcement) comment=%+v error=%v", announcementComment, err)
	}
	announcementItems, announcementTotal, err := repository.List(ctx, announcementTarget, 20, 0)
	if err != nil || announcementTotal != 1 || len(announcementItems) != 1 || announcementItems[0].ID != announcementPublicID {
		t.Fatalf("List(announcement) items=%+v total=%d error=%v", announcementItems, announcementTotal, err)
	}

	items, total, err := repository.List(ctx, torrentTarget, 20, 0)
	if err != nil || total != 3 || len(items) != 3 || items[0].ID != rootPublicID || items[1].ID != replyPublicID || items[2].ID != nestedReplyPublicID {
		t.Fatalf("List() items=%+v total=%d error=%v", items, total, err)
	}
	threadItems, commentTotal, threadTotal, err := repository.ListThreads(ctx, torrentTarget, CommentThreadNewest, 1, 0)
	if err != nil || commentTotal != 3 || threadTotal != 1 || len(threadItems) != 3 ||
		threadItems[0].ID != rootPublicID || threadItems[1].ID != replyPublicID || threadItems[2].ID != nestedReplyPublicID {
		t.Fatalf("ListThreads() items=%+v comments=%d threads=%d error=%v", threadItems, commentTotal, threadTotal, err)
	}
	if threadItems[1].RootCommentID == nil || *threadItems[1].RootCommentID != rootPublicID ||
		threadItems[2].RootCommentID == nil || *threadItems[2].RootCommentID != rootPublicID ||
		threadItems[1].ReplyTo == nil || threadItems[1].ReplyTo.ID != authorID ||
		threadItems[2].ReplyTo == nil || threadItems[2].ReplyTo.ID != otherID {
		t.Fatalf("ListThreads() reply context = %+v", threadItems)
	}

	updated, err := repository.Update(ctx, updateCommentCommand{
		CommentID: rootPublicID, AuthorID: authorID, ExpectedVersion: 1,
		Body: "已完成文件、字幕与音轨校验。", UpdatedAt: now.Add(4 * time.Second),
	})
	if err != nil || updated.Version != 2 || updated.EditedAt == nil || updated.Body != "已完成文件、字幕与音轨校验。" {
		t.Fatalf("Update() comment=%+v error=%v", updated, err)
	}
	if _, err := repository.Update(ctx, updateCommentCommand{
		CommentID: rootPublicID, AuthorID: otherID, ExpectedVersion: 2,
		Body: "不能编辑他人的评论。", UpdatedAt: now.Add(5 * time.Second),
	}); !errors.Is(err, ErrCommentNotFound) {
		t.Fatalf("Update(other author) error=%v", err)
	}

	if err := repository.Delete(ctx, deleteCommentCommand{
		CommentID: replyPublicID, AuthorID: otherID, ExpectedVersion: 1, DeletedAt: now.Add(6 * time.Second),
	}); err != nil {
		t.Fatalf("Delete(reply) error=%v", err)
	}
	if err := repository.Delete(ctx, deleteCommentCommand{
		CommentID: replyPublicID, AuthorID: otherID, ExpectedVersion: 1, DeletedAt: now.Add(7 * time.Second),
	}); err != nil {
		t.Fatalf("Delete(reply replay) error=%v", err)
	}

	disabledAt := now.Add(8 * time.Second)
	if _, err := pool.Exec(ctx, `
UPDATE torrents.torrents
SET state = 'disabled', version = version + 1, state_changed_at = $2, updated_at = $2
WHERE id = $1`, torrentID, disabledAt); err != nil {
		t.Fatalf("disable integration torrent: %v", err)
	}
	if _, _, err := repository.List(ctx, torrentTarget, 20, 0); !errors.Is(err, ErrCommentTargetNotFound) {
		t.Fatalf("List(disabled torrent) error=%v", err)
	}
	if _, err := repository.Create(ctx, createCommentCommand{
		PublicID: uuid.New(), RequestID: uuid.New(), Target: torrentTarget,
		AuthorID: authorID, Body: "停用种子不能新增评论。",
		CreateBodySHA256: sha256.Sum256([]byte("停用种子不能新增评论。")), CreatedAt: now.Add(9 * time.Second),
	}); !errors.Is(err, ErrCommentTargetNotFound) {
		t.Fatalf("Create(disabled torrent) error=%v", err)
	}
	if err := repository.Delete(ctx, deleteCommentCommand{
		CommentID: rootPublicID, AuthorID: authorID, ExpectedVersion: 2, DeletedAt: now.Add(10 * time.Second),
	}); err != nil {
		t.Fatalf("Delete(own comment after target disabled) error=%v", err)
	}
	postDeleteReplay, err := repository.Create(ctx, rootCommand)
	if err != nil || postDeleteReplay.State != CommentAuthorDeleted || postDeleteReplay.Body != "" {
		t.Fatalf("Create(original request after edit/delete) comment=%+v error=%v", postDeleteReplay, err)
	}

	var threadCount, commentCount, revisionCount int64
	if err := pool.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM social.torrent_comment_threads WHERE torrent_id = $1),
    (SELECT count(*) FROM social.comments AS comment
        JOIN social.torrent_comment_threads AS binding ON binding.thread_id = comment.thread_id
        WHERE binding.torrent_id = $1),
    (SELECT count(*) FROM social.comment_revisions AS revision
        JOIN social.comments AS comment ON comment.id = revision.comment_id
        JOIN social.torrent_comment_threads AS binding ON binding.thread_id = comment.thread_id
        WHERE binding.torrent_id = $1)`, torrentID).Scan(&threadCount, &commentCount, &revisionCount); err != nil {
		t.Fatalf("read persisted comment graph: %v", err)
	}
	if threadCount != 1 || commentCount != 3 || revisionCount != 3 {
		t.Fatalf("comment graph threads=%d comments=%d revisions=%d", threadCount, commentCount, revisionCount)
	}
	var announcementThreadCount, announcementCommentCount int64
	if err := pool.QueryRow(ctx, `
SELECT
    count(DISTINCT binding.thread_id),
    count(comment.id)
FROM social.announcement_comment_threads AS binding
JOIN social.comments AS comment ON comment.thread_id = binding.thread_id
WHERE binding.announcement_id = $1`, announcementID).Scan(&announcementThreadCount, &announcementCommentCount); err != nil {
		t.Fatalf("read persisted announcement comment graph: %v", err)
	}
	if announcementThreadCount != 1 || announcementCommentCount != 1 {
		t.Fatalf("announcement graph threads=%d comments=%d", announcementThreadCount, announcementCommentCount)
	}
	if _, err := pool.Exec(ctx, `
UPDATE social.comment_revisions SET body = body || 'x'
WHERE comment_id = (SELECT id FROM social.comments WHERE public_id = $1)
  AND version = 1`, rootPublicID); err == nil {
		t.Fatal("immutable comment revision unexpectedly accepted an update")
	}
}

func TestPostgresSocialRedPacketLetsSenderClaimOneShare(t *testing.T) {
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

	now := time.Now().UTC().Truncate(time.Microsecond)
	senderID := insertCommentIntegrationUser(t, ctx, pool, "red-packet-sender", now)
	ledger, err := economy.NewPostgresRepository(pool)
	if err != nil {
		t.Fatalf("NewPostgresRepository(economy) error = %v", err)
	}
	openingReference := "social-red-packet-integration:" + uuid.NewString()
	openingDigest := sha256.Sum256([]byte(openingReference))
	if _, err := ledger.Record(ctx, economy.RecordCommand{
		TransactionID:   uuid.New(),
		TransactionType: economy.TransactionActivityReward,
		IdempotencyKey:  openingReference,
		SourceReference: openingReference,
		PolicyRevision:  "social-red-packet-integration-v1",
		PayloadSHA256:   openingDigest,
		OccurredAt:      now,
		RecordedAt:      now,
		Postings: []economy.PostingInput{
			{AccountID: economy.ActivityMintAccountID(), Amount: -100},
			{AccountID: senderID, Amount: 100},
		},
	}); err != nil {
		t.Fatalf("fund sender account: %v", err)
	}

	repository, err := NewPostgresPostRepository(pool)
	if err != nil {
		t.Fatalf("NewPostgresPostRepository() error = %v", err)
	}
	postID := uuid.New()
	requestID := uuid.New()
	body := "发送者也可以领取一份红包。"
	redPacket := &CreateRedPacketInput{TotalAmount: 50, ClaimCount: 2}
	created, err := repository.Create(ctx, createPostCommand{
		PublicID:         postID,
		RequestID:        requestID,
		AuthorID:         senderID,
		Body:             body,
		BoardID:          "general",
		RedPacket:        redPacket,
		CreateBodySHA256: createPostInputSHA256(body, "general", nil, nil, redPacket, nil),
		CreatedAt:        now.Add(time.Second),
	})
	if err != nil || created.ID != postID {
		t.Fatalf("Create(red packet) post=%+v error=%v", created, err)
	}

	claim, err := repository.ClaimRedPacket(ctx, senderID, postID, uuid.New(), now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("ClaimRedPacket(sender) error = %v", err)
	}
	if claim.Amount != 25 || claim.RemainingAmount != 25 || claim.RemainingClaims != 1 || claim.Replayed {
		t.Fatalf("ClaimRedPacket(sender) = %+v", claim)
	}
	replayed, err := repository.ClaimRedPacket(ctx, senderID, postID, uuid.New(), now.Add(3*time.Second))
	if err != nil || !replayed.Replayed || replayed.Amount != claim.Amount {
		t.Fatalf("ClaimRedPacket(sender replay) = %+v, %v", replayed, err)
	}
}

func insertPublishedCommentAnnouncement(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) string {
	t.Helper()
	announcementID := "comment-" + uuid.NewString()[:8]
	if _, err := pool.Exec(ctx, `
INSERT INTO catalog.announcements (
    id, title, summary, body, body_format, version,
    published_at, created_at, updated_at
) VALUES ($1, $2, $3, $4, 'plain_text', 1, $5, $5, $5)`,
		announcementID, "公告评论集成测试", "验证公告评论的强类型绑定。", "这是一篇已经公开的集成测试公告。", now); err != nil {
		t.Fatalf("insert published integration announcement: %v", err)
	}
	return announcementID
}

func insertCommentIntegrationUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label string, now time.Time) uuid.UUID {
	t.Helper()
	userID := uuid.New()
	username := fmt.Sprintf("comment-%s-%s", label, uuid.NewString()[:8])
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.users (
    id, credential_ref, username, display_name, status,
    email_verified_at, created_at, updated_at, password_changed_at
) VALUES ($1, $2, $3, $4, 'active', $5, $5, $5, $5)`,
		userID, uuid.New(), username, "评论集成测试 "+label, now); err != nil {
		t.Fatalf("insert %s integration user: %v", label, err)
	}
	return userID
}

func insertPublishedCommentTorrent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, uploaderID uuid.UUID, now time.Time) int64 {
	t.Helper()
	categoryID := "comment-" + uuid.NewString()[:8]
	if _, err := pool.Exec(ctx, `
INSERT INTO catalog.categories (id, name, display_order, enabled, created_at, updated_at)
VALUES ($1, '评论集成测试', 100000 + abs(hashtext($1)) % 900000, true, $2, $2)`, categoryID, now); err != nil {
		t.Fatalf("insert integration category: %v", err)
	}
	objectID := uuid.New()
	objectDigest := sha256.Sum256([]byte("comment-object-" + objectID.String()))
	infoDigest := sha256.Sum256([]byte("comment-info-" + objectID.String()))
	if _, err := pool.Exec(ctx, `
INSERT INTO torrents.torrent_objects (
    id, content_sha256, byte_length, parser_version, validation_profile,
    compatibility_flags, info_offset, info_length, created_at
) VALUES ($1, $2, 256, 'integration-v1', 'strict_upload', ARRAY[]::text[], 0, 128, $3)`,
		objectID, objectDigest[:], now); err != nil {
		t.Fatalf("insert integration torrent object: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO torrents.torrent_object_locations (
    object_id, backend_id, object_key, state, is_preferred,
    observed_byte_length, observed_sha256, verified_at, created_at, updated_at
) VALUES ($1, 'integration-local', $2, 'verified', true, 256, $3, $4, $4, $4)`,
		objectID, "comments/"+objectID.String()+".torrent", objectDigest[:], now); err != nil {
		t.Fatalf("insert integration object location: %v", err)
	}
	var torrentID int64
	if err := pool.QueryRow(ctx, `
INSERT INTO torrents.torrents (
    uploader_id, category_id, object_id, info_hash_v1,
    content_name, title, subtitle, total_size_bytes, payload_size_bytes,
    file_count, padding_file_count, piece_length_bytes, piece_count,
    state, version, submitted_at, published_at, state_changed_at, updated_at
) VALUES (
    $1, $2, $3, $4,
    'comment-integration.bin', $5, '评论线程与历史保留', 4096, 4096,
    1, 0, 16384, 1,
    'published', 1, $6, $6, $6, $6
)
RETURNING id`, uploaderID, categoryID, objectID, infoDigest[:20], "Comment "+objectID.String()[:8], now).Scan(&torrentID); err != nil {
		t.Fatalf("insert published integration torrent: %v", err)
	}
	return torrentID
}
