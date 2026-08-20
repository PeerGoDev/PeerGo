package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

func TestSessionSecurityServiceListsOnlyAuthenticatedUsersSessions(t *testing.T) {
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	userID := uuid.New()
	tokenHash := make([]byte, 32)
	tokenHash[0] = 7
	authenticator := &recordingSessionSecurityAuthenticator{session: WebSession{
		User: User{ID: userID}, TokenHash: tokenHash,
	}}
	authorizer := &recordingSessionSecurityAuthorizer{decision: allowedSelfSessionDecision(now)}
	want := []UserWebSession{{ID: uuid.New(), Current: true, CreatedAt: now.Add(-time.Hour), LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}}
	repository := &recordingSessionSecurityRepository{sessions: want}
	service, err := NewSessionSecurityService(authenticator, repository, &recordingTwoFactorStatusProvider{}, authorizer, SessionSecurityServiceConfig{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewSessionSecurityService() error = %v", err)
	}

	got, err := service.ListSessions(context.Background(), "cookie")
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("ListSessions() = %+v, want %+v", got, want)
	}
	if repository.listUserID != userID || string(repository.listTokenHash) != string(tokenHash) || !repository.listAsOf.Equal(now) {
		t.Fatalf("repository list input = user=%s token=%x as_of=%s", repository.listUserID, repository.listTokenHash, repository.listAsOf)
	}
	request := authorizer.request
	if request.Action != authz.ActionSessionReadSelf || request.CredentialAudience != authz.AudienceWebSession || request.Subject.ID != userID || request.Resource.OwnerID != userID {
		t.Fatalf("authorization request = %+v", request)
	}
}

func TestSessionSecurityServiceRevokesOthersWithCSRFAndAuthorizationEvidence(t *testing.T) {
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	userID := uuid.New()
	revocationID := uuid.New()
	tokenHash := make([]byte, 32)
	tokenHash[0] = 9
	authenticator := &recordingSessionSecurityAuthenticator{session: WebSession{User: User{ID: userID}, TokenHash: tokenHash}}
	decision := allowedSelfSessionDecision(now)
	authorizer := &recordingSessionSecurityAuthorizer{decision: decision}
	repository := &recordingSessionSecurityRepository{result: SessionRevocationResult{RevokedWebSessions: 2, RevokedStaffSessions: 1}}
	service, err := NewSessionSecurityService(authenticator, repository, &recordingTwoFactorStatusProvider{}, authorizer, SessionSecurityServiceConfig{
		Now: func() time.Time { return now }, NewRevocationID: func() uuid.UUID { return revocationID },
	})
	if err != nil {
		t.Fatalf("NewSessionSecurityService() error = %v", err)
	}

	result, err := service.RevokeOtherSessions(context.Background(), "cookie", "csrf")
	if err != nil {
		t.Fatalf("RevokeOtherSessions() error = %v", err)
	}
	if result.RevokedWebSessions != 2 || result.RevokedStaffSessions != 1 {
		t.Fatalf("RevokeOtherSessions() = %+v", result)
	}
	if authenticator.writeCookie != "cookie" || authenticator.writeCSRF != "csrf" {
		t.Fatalf("AuthenticateWrite() input = cookie %q csrf %q", authenticator.writeCookie, authenticator.writeCSRF)
	}
	command := repository.command
	if command.ID != revocationID || command.UserID != userID || command.Scope != SessionRevocationOthers || command.TargetSessionID != uuid.Nil || command.Authorization != decision || string(command.CurrentTokenHash) != string(tokenHash) {
		t.Fatalf("revocation command = %+v", command)
	}
	if authorizer.request.Action != authz.ActionSessionRevokeSelf {
		t.Fatalf("authorization action = %q", authorizer.request.Action)
	}
}

func TestSessionSecurityServiceStopsBeforeAuthorizationWhenCSRFIsInvalid(t *testing.T) {
	authenticator := &recordingSessionSecurityAuthenticator{writeErr: ErrInvalidCSRF}
	authorizer := &recordingSessionSecurityAuthorizer{}
	repository := &recordingSessionSecurityRepository{}
	service, err := NewSessionSecurityService(authenticator, repository, &recordingTwoFactorStatusProvider{}, authorizer, SessionSecurityServiceConfig{})
	if err != nil {
		t.Fatalf("NewSessionSecurityService() error = %v", err)
	}

	_, err = service.RevokeOtherSessions(context.Background(), "cookie", "bad-csrf")
	if !errors.Is(err, ErrInvalidCSRF) {
		t.Fatalf("RevokeOtherSessions() error = %v, want ErrInvalidCSRF", err)
	}
	if authorizer.calls != 0 || repository.applyCalls != 0 {
		t.Fatalf("downstream calls = authorizer %d repository %d", authorizer.calls, repository.applyCalls)
	}
}

type recordingSessionSecurityAuthenticator struct {
	session     WebSession
	currentErr  error
	writeErr    error
	writeCookie string
	writeCSRF   string
}

func (authenticator *recordingSessionSecurityAuthenticator) CurrentSession(context.Context, string) (WebSession, error) {
	return authenticator.session, authenticator.currentErr
}

func (authenticator *recordingSessionSecurityAuthenticator) AuthenticateWrite(_ context.Context, cookie, csrf string) (WebSession, error) {
	authenticator.writeCookie = cookie
	authenticator.writeCSRF = csrf
	return authenticator.session, authenticator.writeErr
}

type recordingSessionSecurityAuthorizer struct {
	decision authz.Decision
	err      error
	request  authz.Request
	calls    int
}

type recordingTwoFactorStatusProvider struct {
	credentialRef uuid.UUID
	status        TwoFactorStatus
	err           error
}

func (provider *recordingTwoFactorStatusProvider) TwoFactorStatus(_ context.Context, credentialRef uuid.UUID) (TwoFactorStatus, error) {
	provider.credentialRef = credentialRef
	return provider.status, provider.err
}

func (authorizer *recordingSessionSecurityAuthorizer) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	authorizer.calls++
	authorizer.request = request
	return authorizer.decision, authorizer.err
}

type recordingSessionSecurityRepository struct {
	overview      AccountSecurityOverview
	sessions      []UserWebSession
	result        SessionRevocationResult
	command       SessionRevocationCommand
	listUserID    uuid.UUID
	listTokenHash []byte
	listAsOf      time.Time
	applyCalls    int
}

func (repository *recordingSessionSecurityRepository) SecurityOverview(context.Context, uuid.UUID) (AccountSecurityOverview, error) {
	return repository.overview, nil
}

func (repository *recordingSessionSecurityRepository) ListActiveSessions(_ context.Context, userID uuid.UUID, tokenHash []byte, asOf time.Time) ([]UserWebSession, error) {
	repository.listUserID = userID
	repository.listTokenHash = append([]byte(nil), tokenHash...)
	repository.listAsOf = asOf
	return repository.sessions, nil
}

func (repository *recordingSessionSecurityRepository) ApplySessionRevocation(_ context.Context, command SessionRevocationCommand) (SessionRevocationResult, error) {
	repository.applyCalls++
	repository.command = command
	return repository.result, nil
}

func allowedSelfSessionDecision(now time.Time) authz.Decision {
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
		GrantID: uuid.New(), GrantVersion: 1, RoleID: "member", MandateID: uuid.New(),
		EffectiveUntil: now.Add(time.Hour),
	}
}
