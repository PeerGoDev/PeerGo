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
	requests      []authz.Request
	deniedActions map[authz.Action]bool
}

func (fixture *postAuthorizerFixture) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	fixture.requests = append(fixture.requests, request)
	if fixture.deniedActions[request.Action] {
		return authz.Decision{}, authz.ErrForbidden
	}
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
	postingPolicy *BoardPostingPolicy
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

func (fixture *postRepositoryFixture) ResolveBoardPostingPolicy(_ context.Context, _ string) (BoardPostingPolicy, error) {
	if fixture.postingPolicy == nil {
		return BoardPostingPolicy{AllowMemberPosts: true}, nil
	}
	return *fixture.postingPolicy, nil
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

func TestPostServiceAllowsCardOnlyTorrentShare(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 24, 8, 0, 0, 0, time.UTC)
	userID, postID, requestID := uuid.New(), uuid.New(), uuid.New()
	torrentID := int64(42)
	post := Post{
		ID: postID, Author: PostAuthor{ID: userID, Username: "demo", DisplayName: "演示用户"},
		Body: "", Torrent: &PostTorrent{ID: torrentID}, State: PostVisible, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	authenticator := &postAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: userID}}}
	repository := &postRepositoryFixture{created: post}
	service, err := NewPostService(authenticator, &postAuthorizerFixture{}, repository, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	created, err := service.Create(context.Background(), "cookie", "csrf", CreatePostInput{
		RequestID: requestID, Body: "  \r\n", BoardID: "resources", TorrentID: &torrentID,
	})
	if err != nil || created.ID != postID || repository.createCommand.Body != "" || repository.createCommand.TorrentID == nil || *repository.createCommand.TorrentID != torrentID {
		t.Fatalf("Create(torrent share) post=%+v command=%+v error=%v", created, repository.createCommand, err)
	}
}

func TestPostServiceRequiresRestrictedBoardCapability(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 24, 3, 0, 0, 0, time.UTC)
	userID, postID := uuid.New(), uuid.New()
	post := Post{
		ID: postID, Author: PostAuthor{ID: userID, Username: "admin", DisplayName: "管理员"},
		Body: "站务通知", State: PostVisible, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	authenticator := &postAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: userID}}}
	policy := BoardPostingPolicy{AllowMemberPosts: false}
	deniedAuthorizer := &postAuthorizerFixture{deniedActions: map[authz.Action]bool{
		authz.ActionSocialPostCreateRestrictedSelf: true,
	}}
	deniedRepository := &postRepositoryFixture{created: post, postingPolicy: &policy}
	deniedService, err := NewPostService(authenticator, deniedAuthorizer, deniedRepository, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	_, err = deniedService.Create(context.Background(), "cookie", "csrf", CreatePostInput{
		RequestID: uuid.New(), Body: "站务通知", BoardID: "staff",
	})
	if !errors.Is(err, authz.ErrForbidden) || deniedRepository.createCommand.PublicID != uuid.Nil {
		t.Fatalf("Create(restricted member) command=%+v error=%v", deniedRepository.createCommand, err)
	}
	if len(deniedAuthorizer.requests) != 2 || deniedAuthorizer.requests[1].Action != authz.ActionSocialPostCreateRestrictedSelf {
		t.Fatalf("Create(restricted member) decisions=%+v", deniedAuthorizer.requests)
	}

	privilegedAuthorizer := &postAuthorizerFixture{}
	privilegedRepository := &postRepositoryFixture{created: post, postingPolicy: &policy}
	privilegedService, err := NewPostService(authenticator, privilegedAuthorizer, privilegedRepository, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	created, err := privilegedService.Create(context.Background(), "cookie", "csrf", CreatePostInput{
		RequestID: uuid.New(), Body: "站务通知", BoardID: "staff",
	})
	if err != nil || created.ID != postID || !privilegedRepository.createCommand.CanPostRestrictedBoard {
		t.Fatalf("Create(restricted administrator) post=%+v command=%+v error=%v", created, privilegedRepository.createCommand, err)
	}
}
