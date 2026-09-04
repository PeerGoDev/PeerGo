package social

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type commentAuthenticatorFixture struct {
	session identity.WebSession
	calls   int
	cookie  string
	csrf    string
}

func (fixture *commentAuthenticatorFixture) AuthenticateWrite(_ context.Context, cookie, csrf string) (identity.WebSession, error) {
	fixture.calls++
	fixture.cookie, fixture.csrf = cookie, csrf
	return fixture.session, nil
}

type commentAuthorizerFixture struct {
	requests []authz.Request
}

func (fixture *commentAuthorizerFixture) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	fixture.requests = append(fixture.requests, request)
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: "test",
		GrantID: uuid.New(), GrantVersion: 1, RoleID: "member", MandateID: uuid.New(),
		EffectiveUntil: request.Context.Now.Add(time.Hour),
	}, nil
}

type commentRepositoryFixture struct {
	items         []Comment
	total         int64
	threadTotal   int64
	threadSort    CommentThreadSort
	created       Comment
	updated       Comment
	listTarget    CommentTarget
	listLimit     int
	listOffset    int
	createCommand createCommentCommand
	updateCommand updateCommentCommand
	deleteCommand deleteCommentCommand
}

func (fixture *commentRepositoryFixture) List(_ context.Context, target CommentTarget, limit, offset int) ([]Comment, int64, error) {
	fixture.listTarget, fixture.listLimit, fixture.listOffset = target, limit, offset
	return fixture.items, fixture.total, nil
}

func (fixture *commentRepositoryFixture) ListThreads(_ context.Context, target CommentTarget, sort CommentThreadSort, limit, offset int) ([]Comment, int64, int64, error) {
	fixture.listTarget, fixture.threadSort, fixture.listLimit, fixture.listOffset = target, sort, limit, offset
	return fixture.items, fixture.total, fixture.threadTotal, nil
}

func (fixture *commentRepositoryFixture) Create(_ context.Context, command createCommentCommand) (Comment, error) {
	fixture.createCommand = command
	return fixture.created, nil
}

func (fixture *commentRepositoryFixture) Update(_ context.Context, command updateCommentCommand) (Comment, error) {
	fixture.updateCommand = command
	return fixture.updated, nil
}

func (fixture *commentRepositoryFixture) Delete(_ context.Context, command deleteCommentCommand) error {
	fixture.deleteCommand = command
	return nil
}

func TestCommentServiceKeepsPublicReadSeparateFromOwnedWrites(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 18, 0, 0, 0, time.UTC)
	const torrentID int64 = 42
	authorID, commentID, parentID, requestID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	comment := Comment{
		ID: commentID, Target: TorrentCommentTarget(torrentID), ParentCommentID: &parentID,
		Author: CommentAuthor{ID: authorID, DisplayName: "北岸"}, Body: "正文",
		BodyFormat: CommentBodyPlainText, State: CommentVisible, Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	authenticator := &commentAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: authorID}}}
	authorizer := &commentAuthorizerFixture{}
	repository := &commentRepositoryFixture{items: []Comment{comment}, total: 1, created: comment, updated: comment}
	service, err := NewCommentService(authenticator, authorizer, repository, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewCommentService() error = %v", err)
	}

	page, err := service.ListTorrentComments(context.Background(), torrentID, 20, 0)
	if err != nil || len(page.Items) != 1 || page.Total != 1 || authenticator.calls != 0 || len(authorizer.requests) != 0 {
		t.Fatalf("ListTorrentComments() page=%+v auth=%+v decisions=%+v error=%v", page, authenticator, authorizer.requests, err)
	}
	if repository.listTarget != TorrentCommentTarget(torrentID) {
		t.Fatalf("ListTorrentComments() target = %+v", repository.listTarget)
	}

	created, err := service.CreateTorrentComment(context.Background(), "web-cookie", "csrf-token", CreateTorrentCommentInput{
		RequestID: requestID, TorrentID: torrentID, ParentCommentID: &parentID, Body: "\r\n  正文  \r\n",
	})
	if err != nil || created.ID != commentID || repository.createCommand.Body != "正文" ||
		repository.createCommand.RequestID != requestID || repository.createCommand.AuthorID != authorID ||
		authenticator.cookie != "web-cookie" || authenticator.csrf != "csrf-token" || authorizer.requests[0].Action != authz.ActionTorrentCommentCreateSelf {
		t.Fatalf("CreateTorrentComment() comment=%+v command=%+v auth=%+v decisions=%+v error=%v", created, repository.createCommand, authenticator, authorizer.requests, err)
	}

	_, err = service.UpdateMyComment(context.Background(), "web-cookie", "csrf-token", UpdateCommentInput{CommentID: commentID, ExpectedVersion: 1, Body: "更新"})
	if err != nil || repository.updateCommand.Body != "更新" || repository.updateCommand.AuthorID != authorID || authorizer.requests[1].Action != authz.ActionCommentUpdateSelf {
		t.Fatalf("UpdateMyComment() command=%+v decisions=%+v error=%v", repository.updateCommand, authorizer.requests, err)
	}

	err = service.DeleteMyComment(context.Background(), "web-cookie", "csrf-token", DeleteCommentInput{CommentID: commentID, ExpectedVersion: 1})
	if err != nil || repository.deleteCommand.CommentID != commentID || repository.deleteCommand.AuthorID != authorID || authorizer.requests[2].Action != authz.ActionCommentDeleteSelf {
		t.Fatalf("DeleteMyComment() command=%+v decisions=%+v error=%v", repository.deleteCommand, authorizer.requests, err)
	}
}

func TestCommentServiceReusesTheThreadBoundaryForAnnouncements(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	authorID, commentID := uuid.New(), uuid.New()
	const announcementID = "welcome-to-peergo"
	target := AnnouncementCommentTarget(announcementID)
	comment := Comment{
		ID: commentID, Target: target,
		Author: CommentAuthor{ID: authorID, DisplayName: "北岸"}, Body: "公告回复",
		BodyFormat: CommentBodyPlainText, State: CommentVisible, Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	authenticator := &commentAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: authorID}}}
	authorizer := &commentAuthorizerFixture{}
	repository := &commentRepositoryFixture{items: []Comment{comment}, total: 1, created: comment}
	service, err := NewCommentService(authenticator, authorizer, repository, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewCommentService() error = %v", err)
	}

	page, err := service.ListAnnouncementComments(context.Background(), announcementID, 20, 0)
	if err != nil || page.Target != target || repository.listTarget != target {
		t.Fatalf("ListAnnouncementComments() page=%+v target=%+v error=%v", page, repository.listTarget, err)
	}

	created, err := service.CreateAnnouncementComment(context.Background(), "web-cookie", "csrf-token", CreateAnnouncementCommentInput{
		RequestID: uuid.New(), AnnouncementID: announcementID, Body: "  公告回复  ",
	})
	if err != nil || created.ID != commentID || repository.createCommand.Target != target ||
		repository.createCommand.Body != "公告回复" || authorizer.requests[0].Action != authz.ActionAnnouncementCommentCreateSelf {
		t.Fatalf("CreateAnnouncementComment() comment=%+v command=%+v auth=%+v error=%v", created, repository.createCommand, authorizer.requests, err)
	}
}

func TestCommentServicePagesSocialPostThreadsInsteadOfIndividualReplies(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 27, 10, 0, 0, 0, time.UTC)
	postID, rootID, replyID := uuid.New(), uuid.New(), uuid.New()
	rootAuthor := CommentAuthor{ID: uuid.New(), DisplayName: "北岸"}
	replyAuthor := CommentAuthor{ID: uuid.New(), DisplayName: "南岸"}
	target := PostCommentTarget(postID)
	root := Comment{
		ID: rootID, Target: target, Author: rootAuthor, Body: "一级评论",
		BodyFormat: CommentBodyPlainText, State: CommentVisible, Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	reply := Comment{
		ID: replyID, Target: target, ParentCommentID: &rootID, RootCommentID: &rootID,
		Author: replyAuthor, ReplyTo: &rootAuthor, Body: "楼中楼回复",
		BodyFormat: CommentBodyPlainText, State: CommentVisible, Version: 1,
		CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute),
	}
	repository := &commentRepositoryFixture{
		items: []Comment{root, reply}, total: 2, threadTotal: 1,
	}
	service, err := NewCommentService(
		&commentAuthenticatorFixture{}, &commentAuthorizerFixture{}, repository, func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewCommentService() error = %v", err)
	}

	page, err := service.ListPostComments(context.Background(), postID, CommentThreadHot, 20, 0)
	if err != nil || page.Total != 2 || page.ThreadTotal != 1 || len(page.Items) != 2 ||
		repository.threadSort != CommentThreadHot || repository.listTarget != target {
		t.Fatalf("ListPostComments() page=%+v repository=%+v error=%v", page, repository, err)
	}
}

func TestCommentServiceRejectsInvalidInputBeforeAuthentication(t *testing.T) {
	t.Parallel()

	authenticator := &commentAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: uuid.New()}}}
	service, err := NewCommentService(authenticator, &commentAuthorizerFixture{}, &commentRepositoryFixture{}, time.Now)
	if err != nil {
		t.Fatalf("NewCommentService() error = %v", err)
	}

	_, err = service.CreateTorrentComment(context.Background(), "cookie", "csrf", CreateTorrentCommentInput{
		RequestID: uuid.New(), TorrentID: 42, Body: "\x00",
	})
	if !errors.Is(err, ErrCommentInput) || authenticator.calls != 0 {
		t.Fatalf("CreateTorrentComment() error=%v authentication calls=%d", err, authenticator.calls)
	}
}
