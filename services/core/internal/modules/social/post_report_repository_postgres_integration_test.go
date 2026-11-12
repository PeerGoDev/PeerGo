package social

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func TestPostgresPostModerationRepositoryAggregatesReportsAndHidesWithStaffEvidence(t *testing.T) {
	pool, now := openPostModerationIntegrationDatabase(t)
	ctx := context.Background()
	authorID := insertCommentIntegrationUser(t, ctx, pool, "post-report-author", now)
	reporterOneID := insertCommentIntegrationUser(t, ctx, pool, "post-reporter-one", now)
	reporterTwoID := insertCommentIntegrationUser(t, ctx, pool, "post-reporter-two", now)
	lateReporterID := insertCommentIntegrationUser(t, ctx, pool, "post-reporter-late", now)
	moderatorID := insertCommentIntegrationUser(t, ctx, pool, "post-moderator", now)
	post := createPostModerationIntegrationPost(t, ctx, pool, authorID, "这条开发测试动态需要进入举报聚合案件。", now.Add(time.Second))

	repository, err := NewPostgresPostModerationRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	reportOne := postReportIntegrationCommand(post.ID, reporterOneID, PostReportSpam, "重复发布无关链接。", now.Add(2*time.Second))
	receipt, err := repository.CreateReport(ctx, reportOne)
	if err != nil || receipt.ID != reportOne.ReportPublicID || receipt.PostID != post.ID {
		t.Fatalf("CreateReport(first) receipt=%+v error=%v", receipt, err)
	}
	if replayed, err := repository.CreateReport(ctx, reportOne); err != nil || replayed != receipt {
		t.Fatalf("CreateReport(replay) receipt=%+v error=%v", replayed, err)
	}
	reused := reportOne
	reused.Details = "同一请求号换了内容。"
	reused.CreateInputHash = postReportInputHash(reused.PostID, reused.ReasonCode, reused.Details)
	if _, err := repository.CreateReport(ctx, reused); !errors.Is(err, ErrPostReportIdempotencyConflict) {
		t.Fatalf("CreateReport(reused request) error=%v", err)
	}
	if _, err := repository.CreateReport(ctx, postReportIntegrationCommand(post.ID, authorID, PostReportOther, "", now.Add(2*time.Second))); !errors.Is(err, ErrPostReportSelf) {
		t.Fatalf("CreateReport(self) error=%v", err)
	}
	if _, err := repository.CreateReport(ctx, postReportIntegrationCommand(post.ID, reporterOneID, PostReportHarassment, "", now.Add(2*time.Second))); !errors.Is(err, ErrPostAlreadyReported) {
		t.Fatalf("CreateReport(duplicate reporter) error=%v", err)
	}
	reportTwo := postReportIntegrationCommand(post.ID, reporterTwoID, PostReportHarassment, "", now.Add(3*time.Second))
	if _, err := repository.CreateReport(ctx, reportTwo); err != nil {
		t.Fatalf("CreateReport(second reporter) error=%v", err)
	}

	caseItem := findOpenPostModerationCase(t, ctx, repository, reportOne.CasePublicID)
	if caseItem.Post.ID != post.ID || caseItem.Post.Author.ID != authorID || caseItem.Post.BoardID != "general" ||
		caseItem.Post.State != PostVisible || caseItem.ReportCount != 2 || len(caseItem.Reports) != 2 ||
		!caseItem.LatestReportedAt.Equal(reportTwo.CreatedAt) {
		t.Fatalf("open post moderation case=%+v", caseItem)
	}

	command := decidePostModerationCaseCommand{
		DecidePostModerationCaseInput: DecidePostModerationCaseInput{
			DecisionID: uuid.New(), CaseID: caseItem.ID,
			ExpectedCaseVersion: caseItem.Version, ExpectedPostVersion: caseItem.Post.Version,
			Decision: PostModerationHidePost, ReasonCode: PostModerationSpam,
			Note: "已核对动态全文，确认为重复刷屏。",
		},
		ModeratorID: moderatorID, OccurredAt: now.Add(4 * time.Second), Authorization: allowedModerationDecision(now),
	}
	for name, conflicted := range map[string]uuid.UUID{"author": authorID, "reporter": reporterOneID} {
		attempt := command
		attempt.DecisionID, attempt.ModeratorID = uuid.New(), conflicted
		if _, err := repository.Decide(ctx, attempt); !errors.Is(err, ErrPostModerationConflictOfInterest) {
			t.Fatalf("Decide(%s conflict) error=%v", name, err)
		}
	}
	stale := command
	stale.DecisionID, stale.ExpectedPostVersion = uuid.New(), caseItem.Post.Version+1
	if _, err := repository.Decide(ctx, stale); !errors.Is(err, ErrPostModerationPostVersionConflict) {
		t.Fatalf("Decide(stale post version) error=%v", err)
	}
	assertPostModerationPersistedState(t, ctx, pool, caseItem.ID, "open", "visible", 0, 0)

	result, err := repository.Decide(ctx, command)
	if err != nil || result.CaseState != PostModerationCasePostHidden || result.PostState != PostModeratorHidden ||
		result.PostVersion != caseItem.Post.Version+1 || result.CaseVersion != caseItem.Version+1 {
		t.Fatalf("Decide(hide) result=%+v error=%v", result, err)
	}
	if replayed, err := repository.Decide(ctx, command); err != nil || replayed != result {
		t.Fatalf("Decide(replay) result=%+v error=%v", replayed, err)
	}
	reusedDecision := command
	reusedDecision.Note = "同一处置编号换了说明内容。"
	if _, err := repository.Decide(ctx, reusedDecision); !errors.Is(err, ErrPostModerationDecisionIdempotency) {
		t.Fatalf("Decide(reused decision) error=%v", err)
	}
	assertPostModerationPersistedState(t, ctx, pool, caseItem.ID, "post_hidden", "moderator_hidden", 1, 1)
	var managementReason string
	var moderatedBy uuid.UUID
	if err := pool.QueryRow(ctx, `
SELECT event.reason, post.moderated_by
FROM social.post_management_events AS event
JOIN social.posts AS post ON post.id = event.post_id
WHERE post.public_id = $1`, post.ID).Scan(&managementReason, &moderatedBy); err != nil {
		t.Fatalf("read hidden post management evidence: %v", err)
	}
	if managementReason != command.Note || moderatedBy != moderatorID {
		t.Fatalf("management reason=%q moderated_by=%s", managementReason, moderatedBy)
	}
	if _, err := repository.CreateReport(ctx, postReportIntegrationCommand(post.ID, lateReporterID, PostReportSpam, "", now.Add(5*time.Second))); !errors.Is(err, ErrPostReportTargetNotFound) {
		t.Fatalf("CreateReport(hidden post) error=%v", err)
	}
}

func TestPostgresPostModerationRepositoryOnlyDismissesCasesForDeletedPosts(t *testing.T) {
	pool, now := openPostModerationIntegrationDatabase(t)
	ctx := context.Background()
	authorID := insertCommentIntegrationUser(t, ctx, pool, "deleted-post-author", now)
	reporterID := insertCommentIntegrationUser(t, ctx, pool, "deleted-post-reporter", now)
	moderatorID := insertCommentIntegrationUser(t, ctx, pool, "deleted-post-moderator", now)
	post := createPostModerationIntegrationPost(t, ctx, pool, authorID, "作者稍后会删除这条被举报的动态。", now.Add(time.Second))

	repository, err := NewPostgresPostModerationRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	report := postReportIntegrationCommand(post.ID, reporterID, PostReportOffTopic, "与板块主题无关。", now.Add(2*time.Second))
	if _, err := repository.CreateReport(ctx, report); err != nil {
		t.Fatalf("CreateReport() error=%v", err)
	}
	posts, err := NewPostgresPostRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := posts.Delete(ctx, deletePostCommand{PostID: post.ID, AuthorID: authorID, ExpectedVersion: post.Version, DeletedAt: now.Add(3 * time.Second)}); err != nil {
		t.Fatalf("Delete(post) error=%v", err)
	}

	caseItem := findOpenPostModerationCase(t, ctx, repository, report.CasePublicID)
	if caseItem.Post.State != PostAuthorDeleted || caseItem.Post.Body != "" || caseItem.Post.Version != post.Version+1 {
		t.Fatalf("deleted post moderation case=%+v", caseItem)
	}
	hide := decidePostModerationCaseCommand{
		DecidePostModerationCaseInput: DecidePostModerationCaseInput{
			DecisionID: uuid.New(), CaseID: caseItem.ID,
			ExpectedCaseVersion: caseItem.Version, ExpectedPostVersion: caseItem.Post.Version,
			Decision: PostModerationHidePost, ReasonCode: PostModerationOffTopic,
			Note: "动态已经删除，尝试隐藏应当被拒绝。",
		},
		ModeratorID: moderatorID, OccurredAt: now.Add(4 * time.Second), Authorization: allowedModerationDecision(now),
	}
	if _, err := repository.Decide(ctx, hide); !errors.Is(err, ErrPostModerationCaseStateConflict) {
		t.Fatalf("Decide(hide deleted post) error=%v", err)
	}
	dismiss := hide
	dismiss.DecisionID, dismiss.Decision, dismiss.ReasonCode = uuid.New(), PostModerationDismiss, PostModerationNoViolation
	dismiss.Note = "作者已自行删除动态，关闭案件。"
	result, err := repository.Decide(ctx, dismiss)
	if err != nil || result.CaseState != PostModerationCaseDismissed || result.PostState != PostAuthorDeleted ||
		result.PostVersion != caseItem.Post.Version {
		t.Fatalf("Decide(dismiss) result=%+v error=%v", result, err)
	}
	assertPostModerationPersistedState(t, ctx, pool, caseItem.ID, "dismissed", "author_deleted", 1, 0)
}

func openPostModerationIntegrationDatabase(t *testing.T) (*pgxpool.Pool, time.Time) {
	t.Helper()
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
	// Moderation evidence is immutable by design, so fixtures use unique IDs in
	// a disposable migrated database instead of cleaning up through triggers.
	return pool, time.Now().UTC().Truncate(time.Second)
}

func createPostModerationIntegrationPost(t *testing.T, ctx context.Context, pool *pgxpool.Pool, authorID uuid.UUID, body string, createdAt time.Time) Post {
	t.Helper()
	posts, err := NewPostgresPostRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	post, err := posts.Create(ctx, createPostCommand{
		PublicID: uuid.New(), RequestID: uuid.New(), AuthorID: authorID, Body: body, BoardID: "general",
		CreateBodySHA256: createPostInputSHA256(body, "general", nil, nil, nil, nil), CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("create reported post: %v", err)
	}
	return post
}

func postReportIntegrationCommand(postID, reporterID uuid.UUID, reason PostReportReasonCode, details string, createdAt time.Time) createPostReportCommand {
	return createPostReportCommand{
		ReportPublicID: uuid.New(), CasePublicID: uuid.New(), RequestID: uuid.New(),
		PostID: postID, ReporterID: reporterID, ReasonCode: reason, Details: details,
		CreateInputHash: postReportInputHash(postID, reason, details), CreatedAt: createdAt,
	}
}

func findOpenPostModerationCase(t *testing.T, ctx context.Context, repository *PostgresPostModerationRepository, caseID uuid.UUID) PostModerationCase {
	t.Helper()
	for offset := 0; offset <= MaxModerationCaseOffset; offset += MaxModerationCaseLimit {
		page, err := repository.ListOpenCases(ctx, MaxModerationCaseLimit, offset)
		if err != nil {
			t.Fatalf("ListOpenCases(offset=%d) error = %v", offset, err)
		}
		for _, item := range page.Items {
			if item.ID == caseID {
				return item
			}
		}
		if int64(offset+len(page.Items)) >= page.Total {
			break
		}
	}
	t.Fatalf("open post moderation case %s was not listed", caseID)
	return PostModerationCase{}
}

func assertPostModerationPersistedState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, caseID uuid.UUID, wantCaseState, wantPostState string, wantDecisions, wantManagementEvents int64) {
	t.Helper()
	var caseState, postState string
	var decisionCount, eventCount int64
	if err := pool.QueryRow(ctx, `
SELECT moderation_case.state, post.state,
       (SELECT count(*) FROM social.post_moderation_decisions AS decision WHERE decision.case_id = moderation_case.id),
       (SELECT count(*) FROM social.post_management_events AS event WHERE event.post_id = post.id)
FROM social.post_moderation_cases AS moderation_case
JOIN social.posts AS post ON post.id = moderation_case.post_id
WHERE moderation_case.public_id = $1`, caseID).Scan(&caseState, &postState, &decisionCount, &eventCount); err != nil {
		t.Fatalf("read post moderation persisted state: %v", err)
	}
	if caseState != wantCaseState || postState != wantPostState || decisionCount != wantDecisions || eventCount != wantManagementEvents {
		t.Fatalf("post moderation state case=%q post=%q decisions=%d management_events=%d", caseState, postState, decisionCount, eventCount)
	}
}
