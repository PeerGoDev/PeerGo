package social

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

type moderationEventBuilderFixture struct {
	inputs []CommentModerationAuditInput
}

func (fixture *moderationEventBuilderFixture) BuildCommentModerationDecisionEvent(input CommentModerationAuditInput) (auditevent.Event, error) {
	fixture.inputs = append(fixture.inputs, input)
	return auditevent.Event{}, nil
}

type moderationAppenderFixture struct {
	calls *int
	err   error
}

func (fixture moderationAppenderFixture) Append(context.Context, auditevent.Event) error {
	*fixture.calls++
	return fixture.err
}

func TestPostgresCommentModerationRepositoryAggregatesReportsAndFailsClosedWithoutAudit(t *testing.T) {
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

	// Moderation decisions and revisions are immutable by design. Like the
	// comment repository integration test, this uses a disposable migrated DB
	// and unique fixture IDs instead of weakening production triggers for cleanup.
	now := time.Now().UTC().Truncate(time.Microsecond)
	authorID := insertCommentIntegrationUser(t, ctx, pool, "moderation-author", now)
	reporterOneID := insertCommentIntegrationUser(t, ctx, pool, "moderation-reporter-one", now)
	reporterTwoID := insertCommentIntegrationUser(t, ctx, pool, "moderation-reporter-two", now)
	moderatorID := insertCommentIntegrationUser(t, ctx, pool, "moderator", now)
	torrentID := insertPublishedCommentTorrent(t, ctx, pool, authorID, now)
	commentRepository, err := NewPostgresCommentRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	body := "这条开发测试评论需要进入举报聚合案件。"
	comment, err := commentRepository.Create(ctx, createCommentCommand{
		PublicID: uuid.New(), RequestID: uuid.New(), Target: TorrentCommentTarget(torrentID),
		AuthorID: authorID, Body: body, CreateBodySHA256: sha256.Sum256([]byte(body)), CreatedAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("create moderated comment: %v", err)
	}

	eventBuilder := &moderationEventBuilderFixture{}
	appendCalls := 0
	appendErr := errors.New("synthetic audit outage")
	repository, err := NewPostgresCommentModerationRepository(
		pool,
		eventBuilder,
		func(pgx.Tx) auditevent.Appender {
			return moderationAppenderFixture{calls: &appendCalls, err: appendErr}
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	reportOne := createCommentReportCommand{
		ReportPublicID: uuid.New(), CasePublicID: uuid.New(), RequestID: uuid.New(),
		CommentID: comment.ID, ReporterID: reporterOneID, ReasonCode: CommentReportOffTopic,
		Details: "与资源校验讨论无关。", CreatedAt: now.Add(2 * time.Second),
	}
	reportOne.CreateInputHash = commentReportInputHash(reportOne.CommentID, reportOne.ReasonCode, reportOne.Details)
	receipt, err := repository.CreateReport(ctx, reportOne)
	if err != nil || receipt.ID != reportOne.ReportPublicID {
		t.Fatalf("CreateReport(first) receipt=%+v error=%v", receipt, err)
	}
	replayed, err := repository.CreateReport(ctx, reportOne)
	if err != nil || replayed != receipt {
		t.Fatalf("CreateReport(replay) receipt=%+v error=%v", replayed, err)
	}
	selfReport := reportOne
	selfReport.ReportPublicID, selfReport.CasePublicID, selfReport.RequestID = uuid.New(), uuid.New(), uuid.New()
	selfReport.ReporterID = authorID
	selfReport.CreateInputHash = commentReportInputHash(selfReport.CommentID, selfReport.ReasonCode, selfReport.Details)
	if _, err := repository.CreateReport(ctx, selfReport); !errors.Is(err, ErrCommentReportSelf) {
		t.Fatalf("CreateReport(self) error=%v", err)
	}
	reportTwo := reportOne
	reportTwo.ReportPublicID, reportTwo.CasePublicID, reportTwo.RequestID = uuid.New(), uuid.New(), uuid.New()
	reportTwo.ReporterID, reportTwo.ReasonCode = reporterTwoID, CommentReportSpam
	reportTwo.Details, reportTwo.CreatedAt = "重复发布无关链接。", now.Add(3*time.Second)
	reportTwo.CreateInputHash = commentReportInputHash(reportTwo.CommentID, reportTwo.ReasonCode, reportTwo.Details)
	if _, err := repository.CreateReport(ctx, reportTwo); err != nil {
		t.Fatalf("CreateReport(second reporter) error=%v", err)
	}

	page, err := repository.ListOpenCases(ctx, 20, 0)
	if err != nil || page.Total != 1 || len(page.Items) != 1 || len(page.Items[0].Reports) != 2 ||
		page.Items[0].ID != reportOne.CasePublicID || page.Items[0].LatestReportedAt != reportTwo.CreatedAt {
		t.Fatalf("ListOpenCases() page=%+v error=%v", page, err)
	}

	caseItem := page.Items[0]
	if caseItem.Target.CommentTarget != TorrentCommentTarget(torrentID) || caseItem.Comment.Target != caseItem.Target.CommentTarget {
		t.Fatalf("moderation target=%+v comment target=%+v", caseItem.Target, caseItem.Comment.Target)
	}
	decisionID := uuid.New()
	decision := allowedModerationDecision(now)
	command := decideCommentModerationCaseCommand{
		DecideCommentModerationCaseInput: DecideCommentModerationCaseInput{
			DecisionID: decisionID, CaseID: caseItem.ID,
			ExpectedCaseVersion: caseItem.Version, ExpectedCommentVersion: caseItem.Comment.Version,
			Decision: CommentModerationHideComment, ReasonCode: CommentModerationOffTopic,
			Note: "已核对完整上下文，确认隐藏这一条评论。",
		},
		ModeratorID: moderatorID, OccurredAt: now.Add(4 * time.Second), Authorization: decision,
	}
	conflicted := command
	conflicted.DecisionID, conflicted.ModeratorID = uuid.New(), authorID
	conflicted.Authorization = allowedModerationDecision(now)
	if _, err := repository.Decide(ctx, conflicted); !errors.Is(err, ErrModerationConflictOfInterest) {
		t.Fatalf("Decide(author conflict) error=%v", err)
	}
	if _, err := repository.Decide(ctx, command); err == nil || !errors.Is(err, appendErr) {
		t.Fatalf("Decide(audit outage) error=%v", err)
	}
	assertModerationPersistedState(t, ctx, pool, caseItem.ID, comment.ID, "open", "visible", 0)

	repository, err = NewPostgresCommentModerationRepository(
		pool,
		eventBuilder,
		func(pgx.Tx) auditevent.Appender {
			return moderationAppenderFixture{calls: &appendCalls}
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := repository.Decide(ctx, command)
	if err != nil || result.CaseState != CommentModerationCaseCommentHidden ||
		result.CommentState != CommentModeratorHidden || result.CommentVersion != comment.Version+1 {
		t.Fatalf("Decide(success) result=%+v error=%v", result, err)
	}
	replayedDecision, err := repository.Decide(ctx, command)
	if err != nil || replayedDecision != result {
		t.Fatalf("Decide(replay) result=%+v error=%v", replayedDecision, err)
	}
	assertModerationPersistedState(t, ctx, pool, caseItem.ID, comment.ID, "comment_hidden", "moderator_hidden", 1)
	if appendCalls != 2 || len(eventBuilder.inputs) != 2 || eventBuilder.inputs[1].ReportCount != 2 {
		t.Fatalf("audit calls=%d inputs=%+v", appendCalls, eventBuilder.inputs)
	}
}

func allowedModerationDecision(now time.Time) authz.Decision {
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
		GrantID: uuid.New(), GrantVersion: 1, RoleID: "community_moderator", MandateID: uuid.New(),
		EffectiveUntil: now.Add(time.Hour),
	}
}

func assertModerationPersistedState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, caseID, commentID uuid.UUID, wantCaseState, wantCommentState string, wantDecisions int64) {
	t.Helper()
	var caseState, commentState string
	var decisionCount, revisionCount int64
	if err := pool.QueryRow(ctx, `
SELECT moderation_case.state, comment.state,
       (SELECT count(*) FROM social.comment_moderation_decisions AS decision WHERE decision.case_id = moderation_case.id),
       (SELECT count(*) FROM social.comment_revisions AS revision WHERE revision.comment_id = comment.id)
FROM social.comment_moderation_cases AS moderation_case
JOIN social.comments AS comment ON comment.id = moderation_case.comment_id
WHERE moderation_case.public_id = $1 AND comment.public_id = $2`, caseID, commentID).Scan(
		&caseState, &commentState, &decisionCount, &revisionCount,
	); err != nil {
		t.Fatalf("read moderation persisted state: %v", err)
	}
	if caseState != wantCaseState || commentState != wantCommentState || decisionCount != wantDecisions || revisionCount != wantDecisions {
		t.Fatalf("moderation state case=%q comment=%q decisions=%d revisions=%d", caseState, commentState, decisionCount, revisionCount)
	}
}
