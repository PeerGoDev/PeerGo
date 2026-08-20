package identity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type memoryRepository struct {
	user    User
	profile PublicUserProfile
	session SessionRecord
	revoked bool
	policy  WebSessionPolicy
}

func (r *memoryRepository) WebSessionPolicy(context.Context) (WebSessionPolicy, error) {
	if r.policy.SessionDuration == 0 {
		return WebSessionPolicy{SessionDuration: 12 * time.Hour, RememberSessionDuration: 30 * 24 * time.Hour}, nil
	}
	return r.policy, nil
}

func (r *memoryRepository) PublicProfileByUsername(_ context.Context, username string, _ time.Time) (PublicUserProfile, error) {
	if !strings.EqualFold(username, r.profile.Username) {
		return PublicUserProfile{}, ErrPublicUserNotFound
	}
	return r.profile, nil
}

func (r *memoryRepository) UserByCredentialRef(_ context.Context, reference uuid.UUID, _ time.Time) (User, error) {
	if reference != r.user.CredentialRef {
		return User{}, ErrInvalidCredentials
	}
	return r.user, nil
}

func (r *memoryRepository) CreateSession(_ context.Context, session SessionRecord) error {
	r.session = session
	return nil
}

func (r *memoryRepository) ActiveSession(_ context.Context, tokenHash []byte, asOf time.Time) (SessionRecord, error) {
	if r.revoked || !bytes.Equal(tokenHash, r.session.TokenHash) || !asOf.Before(r.session.ExpiresAt) {
		return SessionRecord{}, ErrSessionNotFound
	}
	return r.session, nil
}

func (r *memoryRepository) RevokeSession(_ context.Context, tokenHash []byte, _ time.Time) error {
	if !bytes.Equal(tokenHash, r.session.TokenHash) || r.revoked {
		return ErrSessionNotFound
	}
	r.revoked = true
	return nil
}

type fixedVerifier struct {
	reference uuid.UUID
}

type allowingIdentityAuthorizer struct {
	now time.Time
}

func (authorizer allowingIdentityAuthorizer) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	return authz.Decision{
		ID:             uuid.New(),
		Allow:          true,
		Reason:         authz.ReasonAllowed,
		PolicyVersion:  authz.PolicyVersion,
		GrantID:        uuid.New(),
		GrantVersion:   1,
		RoleID:         "member",
		MandateID:      uuid.New(),
		EffectiveUntil: authorizer.now.Add(time.Hour),
	}, nil
}

func (v fixedVerifier) Verify(_ context.Context, input LoginInput) (uuid.UUID, error) {
	if input.Identifier != "demo" || input.Password != "password" {
		return uuid.Nil, ErrInvalidCredentials
	}
	return v.reference, nil
}

func TestServiceStoresOnlyTokenDigestAndRequiresCSRFForLogout(t *testing.T) {
	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	reference := uuid.MustParse("0198f20a-6da8-7e51-9c64-222222222222")
	repository := &memoryRepository{user: User{
		ID:            uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111"),
		CredentialRef: reference,
		Username:      "demo",
		DisplayName:   "Demo",
	}, profile: PublicUserProfile{
		Username: "member", DisplayName: "站内成员", JoinedAt: now.Add(-24 * time.Hour), PublishedTorrentCount: 4,
	}}
	randomToken := bytes.Repeat([]byte{0x42}, randomTokenBytes)
	service, err := NewService(repository, fixedVerifier{reference: reference}, ServiceConfig{
		CSRFKey: []byte("0123456789abcdef0123456789abcdef"),
		Now:     func() time.Time { return now },
		Random:  bytes.NewReader(randomToken),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	session, err := service.Login(context.Background(), LoginInput{Identifier: " demo ", Password: "password"})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	wantHash := sha256.Sum256(randomToken)
	if !bytes.Equal(repository.session.TokenHash, wantHash[:]) {
		t.Fatal("Login() did not persist SHA-256 token digest")
	}
	if bytes.Contains(repository.session.TokenHash, []byte(session.CookieToken)) {
		t.Fatal("Login() persisted the raw cookie token")
	}

	current, err := service.CurrentSession(context.Background(), session.CookieToken)
	if err != nil {
		t.Fatalf("CurrentSession() error = %v", err)
	}
	if current.User.ID != repository.user.ID || current.CSRFToken != session.CSRFToken {
		t.Fatalf("CurrentSession() = %+v", current)
	}
	if err := service.Logout(context.Background(), session.CookieToken, "wrong-token"); !errors.Is(err, ErrInvalidCSRF) {
		t.Fatalf("Logout(wrong csrf) error = %v, want ErrInvalidCSRF", err)
	}
	if repository.revoked {
		t.Fatal("Logout(wrong csrf) revoked the session")
	}
	if err := service.Logout(context.Background(), session.CookieToken, session.CSRFToken); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if _, err := service.CurrentSession(context.Background(), session.CookieToken); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("CurrentSession(after logout) error = %v, want ErrSessionNotFound", err)
	}
}

func TestServiceUsesCurrentWebSessionPolicyForNewLogin(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 19, 8, 0, 0, 0, time.UTC)
	reference := uuid.MustParse("0198f20a-6da8-7e51-9c64-222222222222")
	repository := &memoryRepository{
		user:   User{ID: uuid.New(), CredentialRef: reference, Username: "demo", DisplayName: "Demo"},
		policy: WebSessionPolicy{SessionDuration: 7 * 24 * time.Hour, RememberSessionDuration: 45 * 24 * time.Hour},
	}
	service, err := NewService(repository, fixedVerifier{reference: reference}, ServiceConfig{
		CSRFKey: []byte("0123456789abcdef0123456789abcdef"), Now: func() time.Time { return now },
		Random: bytes.NewReader(bytes.Repeat([]byte{0x43}, randomTokenBytes)),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	session, err := service.Login(context.Background(), LoginInput{Identifier: "demo", Password: "password", RememberMe: true})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if want := now.Add(45 * 24 * time.Hour); !session.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %s, want %s", session.ExpiresAt, want)
	}
}

func TestServicePublicProfileRequiresSessionAndNormalizesUsername(t *testing.T) {
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	reference := uuid.MustParse("0198f20a-6da8-7e51-9c64-222222222222")
	repository := &memoryRepository{
		user:    User{ID: uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111"), CredentialRef: reference, Username: "demo", DisplayName: "Demo"},
		profile: PublicUserProfile{Username: "Legacy-User", DisplayName: "迁移用户", JoinedAt: now.Add(-365 * 24 * time.Hour), PublishedTorrentCount: 9},
	}
	randomToken := bytes.Repeat([]byte{0x42}, randomTokenBytes)
	sessions, err := NewService(repository, fixedVerifier{reference: reference}, ServiceConfig{
		CSRFKey: []byte("0123456789abcdef0123456789abcdef"), Now: func() time.Time { return now }, Random: bytes.NewReader(randomToken),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service, err := NewPublicUserProfileService(sessions, repository, allowingIdentityAuthorizer{now: now}, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewPublicUserProfileService() error = %v", err)
	}
	if _, err := service.PublicProfile(context.Background(), "", "Legacy-User"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("PublicProfile(without session) error = %v", err)
	}
	session, err := sessions.Login(context.Background(), LoginInput{Identifier: "demo", Password: "password"})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	profile, err := service.PublicProfile(context.Background(), session.CookieToken, " legacy-user ")
	if err != nil {
		t.Fatalf("PublicProfile() error = %v", err)
	}
	if profile.Username != "Legacy-User" || profile.PublishedTorrentCount != 9 {
		t.Fatalf("PublicProfile() = %+v", profile)
	}
}
