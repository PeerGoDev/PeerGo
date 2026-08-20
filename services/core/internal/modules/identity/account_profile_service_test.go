package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type recordingProfileSessions struct {
	session WebSession
	token   string
	csrf    string
}

func (sessions *recordingProfileSessions) CurrentSession(context.Context, string) (WebSession, error) {
	return sessions.session, nil
}

func (sessions *recordingProfileSessions) AuthenticateWrite(_ context.Context, token, csrf string) (WebSession, error) {
	sessions.token, sessions.csrf = token, csrf
	return sessions.session, nil
}

type recordingProfileRepository struct {
	userID      uuid.UUID
	displayName string
	updatedAt   time.Time
	result      User
}

func (repository *recordingProfileRepository) UpdateMyDisplayName(_ context.Context, userID uuid.UUID, displayName string, updatedAt time.Time) (User, error) {
	repository.userID, repository.displayName, repository.updatedAt = userID, displayName, updatedAt
	return repository.result, nil
}

func TestAccountProfileServiceNormalizesAuthorizesAndUpdatesNickname(t *testing.T) {
	now := time.Date(2026, time.August, 13, 9, 30, 0, 0, time.UTC)
	userID := uuid.New()
	sessions := &recordingProfileSessions{session: WebSession{User: User{ID: userID}}}
	repository := &recordingProfileRepository{result: User{ID: userID, Username: "member", DisplayName: "新昵称"}}
	authorizer := &recordingProfileAuthorizer{now: now}
	service, err := NewAccountProfileService(sessions, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewAccountProfileService() error = %v", err)
	}

	result, err := service.UpdateProfile(context.Background(), "cookie", "csrf", UpdateMyProfileInput{DisplayName: "  新昵称  "})
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if sessions.token != "cookie" || sessions.csrf != "csrf" || repository.userID != userID || repository.displayName != "新昵称" || !repository.updatedAt.Equal(now) {
		t.Fatalf("sessions=%+v repository=%+v", sessions, repository)
	}
	if result.DisplayName != "新昵称" || authorizer.request.Action != authz.ActionAccountProfileUpdateSelf || authorizer.request.Resource.OwnerID != userID {
		t.Fatalf("result=%+v authorization=%+v", result, authorizer.request)
	}
}

func TestAccountProfileServiceRejectsInvalidNicknameBeforeAuthentication(t *testing.T) {
	sessions := &recordingProfileSessions{}
	service, err := NewAccountProfileService(sessions, &recordingProfileRepository{}, &recordingProfileAuthorizer{now: time.Now()}, time.Now)
	if err != nil {
		t.Fatalf("NewAccountProfileService() error = %v", err)
	}
	for _, displayName := range []string{"   ", strings.Repeat("昵", 41)} {
		if _, err := service.UpdateProfile(context.Background(), "cookie", "csrf", UpdateMyProfileInput{DisplayName: displayName}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("UpdateProfile(%q) error = %v, want ErrInvalidInput", displayName, err)
		}
	}
	if sessions.token != "" {
		t.Fatal("invalid input reached session authentication")
	}
}

type recordingProfileAuthorizer struct {
	now     time.Time
	request authz.Request
}

func (authorizer *recordingProfileAuthorizer) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	authorizer.request = request
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
		GrantID: uuid.New(), GrantVersion: 1, RoleID: "member", MandateID: uuid.New(),
		EffectiveUntil: authorizer.now.Add(time.Hour),
	}, nil
}
