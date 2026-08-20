package social

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type postAuthenticatorFixture struct {
	session     identity.WebSession
	readCookie  string
	writeCookie string
	writeCSRF   string
	readCalls   int
	writeCalls  int
}

func (fixture *postAuthenticatorFixture) CurrentSession(_ context.Context, cookie string) (identity.WebSession, error) {
	fixture.readCalls++
	fixture.readCookie = cookie
	return fixture.session, nil
}

func (fixture *postAuthenticatorFixture) AuthenticateWrite(_ context.Context, cookie, csrf string) (identity.WebSession, error) {
	fixture.writeCalls++
	fixture.writeCookie, fixture.writeCSRF = cookie, csrf
	return fixture.session, nil
}

type postAuthorizerFixture struct {
	requests []authz.Request
}

func (fixture *postAuthorizerFixture) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	fixture.requests = append(fixture.requests, request)
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: "test",
		GrantID: uuid.New(), GrantVersion: 1, RoleID: "member", MandateID: uuid.New(),
		EffectiveUntil: request.Context.Now.Add(time.Hour),
	}, nil
}

type postRepositoryFixture struct {
	items         []Post
	total         int64
	listQuery     PostListQuery
	created       Post
	createCommand createPostCommand
}

func (fixture *postRepositoryFixture) List(_ context.Context, query PostListQuery) ([]Post, int64, error) {
	fixture.listQuery = query
	return fixture.items, fixture.total, nil
}

func (fixture *postRepositoryFixture) FindVisible(_ context.Context, id uuid.UUID) (Post, error) {
	for _, post := range fixture.items {
		if post.ID == id {
			return post, nil
		}
	}
	return Post{}, ErrPostNotFound
}

func (fixture *postRepositoryFixture) Create(_ context.Context, command createPostCommand) (Post, error) {
	fixture.createCommand = command
	return fixture.created, nil
}

func (*postRepositoryFixture) Update(context.Context, updatePostCommand) (Post, error) {
	return Post{}, nil
}

func (*postRepositoryFixture) Delete(context.Context, deletePostCommand) error { return nil }

func TestPostServiceSeparatesMemberReadFromCSRFWrites(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 13, 6, 0, 0, 0, time.UTC)
	userID, postID, requestID := uuid.New(), uuid.New(), uuid.New()
	post := Post{
		ID: postID, Author: PostAuthor{ID: userID, Username: "demo", DisplayName: "演示用户"},
		Body: "首条动态", State: PostVisible, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	authenticator := &postAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: userID}}}
	authorizer := &postAuthorizerFixture{}
	repository := &postRepositoryFixture{items: []Post{post}, total: 1, created: post}
	service, err := NewPostService(authenticator, authorizer, repository, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	page, err := service.List(context.Background(), "cookie", PostListQuery{
		Sort: PostNewest, Limit: 20, AuthorUsername: " demo ",
	})
	if err != nil || len(page.Items) != 1 || authenticator.readCalls != 1 || authenticator.writeCalls != 0 ||
		len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionSocialPostRead || repository.listQuery.AuthorUsername != "demo" {
		t.Fatalf("List() page=%+v auth=%+v decisions=%+v error=%v", page, authenticator, authorizer.requests, err)
	}

	created, err := service.Create(context.Background(), "cookie", "csrf", CreatePostInput{RequestID: requestID, Body: "  首条动态\r\n  "})
	if err != nil || created.ID != postID || authenticator.writeCalls != 1 || repository.createCommand.Body != "首条动态" ||
		authorizer.requests[1].Action != authz.ActionSocialPostCreateSelf {
		t.Fatalf("Create() post=%+v auth=%+v command=%+v decisions=%+v error=%v", created, authenticator, repository.createCommand, authorizer.requests, err)
	}
}

func TestPostServiceRejectsInvalidInputBeforeAuthentication(t *testing.T) {
	t.Parallel()
	authenticator := &postAuthenticatorFixture{}
	service, err := NewPostService(authenticator, &postAuthorizerFixture{}, &postRepositoryFixture{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(context.Background(), "cookie", "csrf", CreatePostInput{RequestID: uuid.New(), Body: ""}); err != ErrPostInput {
		t.Fatalf("Create() error = %v", err)
	}
	if authenticator.readCalls != 0 || authenticator.writeCalls != 0 {
		t.Fatalf("invalid input authenticated: %+v", authenticator)
	}
}
