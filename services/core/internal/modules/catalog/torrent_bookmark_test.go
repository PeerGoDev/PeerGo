package catalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type torrentBookmarkAuthenticatorFixture struct {
	session       identity.WebSession
	currentCalls  int
	writeCalls    int
	currentCookie string
	writeCookie   string
	writeCSRF     string
}

func (fixture *torrentBookmarkAuthenticatorFixture) CurrentSession(_ context.Context, cookie string) (identity.WebSession, error) {
	fixture.currentCalls++
	fixture.currentCookie = cookie
	return fixture.session, nil
}

func (fixture *torrentBookmarkAuthenticatorFixture) AuthenticateWrite(_ context.Context, cookie, csrf string) (identity.WebSession, error) {
	fixture.writeCalls++
	fixture.writeCookie, fixture.writeCSRF = cookie, csrf
	return fixture.session, nil
}

type torrentBookmarkAuthorizerFixture struct {
	requests []authz.Request
}

func (fixture *torrentBookmarkAuthorizerFixture) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	fixture.requests = append(fixture.requests, request)
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: "test",
		GrantID: uuid.New(), GrantVersion: 1, RoleID: "member", MandateID: uuid.New(),
		EffectiveUntil: request.Context.Now.Add(time.Hour),
	}, nil
}

type torrentBookmarkRepositoryFixture struct {
	records   []torrentBookmarkRecord
	total     int
	statuses  []int64
	createdAt time.Time
	putID     int64
	deleteID  int64
}

func (fixture *torrentBookmarkRepositoryFixture) List(context.Context, uuid.UUID, int, int) ([]torrentBookmarkRecord, int, error) {
	return fixture.records, fixture.total, nil
}

func (fixture *torrentBookmarkRepositoryFixture) Statuses(context.Context, uuid.UUID, []int64) ([]int64, error) {
	return fixture.statuses, nil
}

func (fixture *torrentBookmarkRepositoryFixture) Put(_ context.Context, _ uuid.UUID, torrentID int64, _ time.Time) (time.Time, error) {
	fixture.putID = torrentID
	return fixture.createdAt, nil
}

func (fixture *torrentBookmarkRepositoryFixture) Delete(_ context.Context, _ uuid.UUID, torrentID int64) error {
	fixture.deleteID = torrentID
	return nil
}

func TestTorrentBookmarkServiceSeparatesReadAndCSRFBoundWrite(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	authenticator := &torrentBookmarkAuthenticatorFixture{
		session: identity.WebSession{User: identity.User{ID: uuid.New()}},
	}
	authorizer := &torrentBookmarkAuthorizerFixture{}
	repository := &torrentBookmarkRepositoryFixture{
		total: 1,
		records: []torrentBookmarkRecord{{
			Torrent: Torrent{
				ID: 42, Name: "Paper Cranes", Category: Category{ID: "movie", Name: "电影"},
				UploadedAt: now.Add(-time.Hour), Swarm: SwarmStats{ObservedAt: now.Add(-time.Minute)},
			},
			BookmarkedAt: now.Add(-30 * time.Minute),
		}},
		createdAt: now.Add(-30 * time.Minute),
	}
	service, err := NewTorrentBookmarkService(authenticator, authorizer, repository, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewTorrentBookmarkService() error = %v", err)
	}

	page, err := service.List(context.Background(), "read-cookie", 20, 0)
	if err != nil || len(page.Items) != 1 || page.Items[0].Torrent.SwarmStale {
		t.Fatalf("List() page=%+v error=%v", page, err)
	}
	if authenticator.currentCalls != 1 || authenticator.writeCalls != 0 || authorizer.requests[0].Action != authz.ActionTorrentBookmarkReadSelf {
		t.Fatalf("read boundary current=%d write=%d requests=%+v", authenticator.currentCalls, authenticator.writeCalls, authorizer.requests)
	}

	state, err := service.Put(context.Background(), "write-cookie", "csrf-token", 42)
	if err != nil || state.TorrentID != 42 || !state.BookmarkedAt.Equal(repository.createdAt) {
		t.Fatalf("Put() state=%+v error=%v", state, err)
	}
	if authenticator.writeCalls != 1 || authenticator.writeCookie != "write-cookie" || authenticator.writeCSRF != "csrf-token" || authorizer.requests[1].Action != authz.ActionTorrentBookmarkWriteSelf {
		t.Fatalf("write boundary authenticator=%+v requests=%+v", authenticator, authorizer.requests)
	}
	if repository.putID != 42 {
		t.Fatalf("Put() repository torrent = %d", repository.putID)
	}
}

func TestTorrentBookmarkStatusesRejectDuplicatesBeforeAuthentication(t *testing.T) {
	t.Parallel()

	authenticator := &torrentBookmarkAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: uuid.New()}}}
	service, err := NewTorrentBookmarkService(
		authenticator,
		&torrentBookmarkAuthorizerFixture{},
		&torrentBookmarkRepositoryFixture{},
		func() time.Time { return time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC) },
	)
	if err != nil {
		t.Fatalf("NewTorrentBookmarkService() error = %v", err)
	}

	_, err = service.Statuses(context.Background(), "cookie", []int64{42, 42})
	if !errors.Is(err, ErrTorrentBookmarkInput) {
		t.Fatalf("Statuses() error = %v, want ErrTorrentBookmarkInput", err)
	}
	if authenticator.currentCalls != 0 || authenticator.writeCalls != 0 {
		t.Fatalf("invalid input reached authentication: %+v", authenticator)
	}
}
