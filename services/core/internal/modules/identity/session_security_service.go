package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type SessionSecurityServiceConfig struct {
	Now             func() time.Time
	NewRevocationID func() uuid.UUID
}

// SessionSecurityService owns the ordinary user's security projection and
// revocation commands. It reuses the canonical Web-session authenticator, so
// token parsing and CSRF verification never drift into a second implementation.
type SessionSecurityService struct {
	authenticator WebSessionAuthenticator
	repository    SessionSecurityRepository
	twoFactor     TwoFactorStatusProvider
	authorizer    authz.Authorizer
	now           func() time.Time
	newID         func() uuid.UUID
}

func NewSessionSecurityService(authenticator WebSessionAuthenticator, repository SessionSecurityRepository, twoFactor TwoFactorStatusProvider, authorizer authz.Authorizer, config SessionSecurityServiceConfig) (*SessionSecurityService, error) {
	if authenticator == nil || repository == nil || twoFactor == nil || authorizer == nil {
		return nil, errors.New("session security service dependencies are required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.NewRevocationID == nil {
		config.NewRevocationID = uuid.New
	}
	return &SessionSecurityService{
		authenticator: authenticator, repository: repository, twoFactor: twoFactor, authorizer: authorizer,
		now: config.Now, newID: config.NewRevocationID,
	}, nil
}

func (service *SessionSecurityService) Overview(ctx context.Context, cookieToken string) (AccountSecurityOverview, error) {
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return AccountSecurityOverview{}, err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionSessionReadSelf, now); err != nil {
		return AccountSecurityOverview{}, err
	}
	result, err := service.repository.SecurityOverview(ctx, session.User.ID)
	if err != nil {
		return AccountSecurityOverview{}, fmt.Errorf("read account security overview: %w", err)
	}
	result.TwoFactor, err = service.twoFactor.TwoFactorStatus(ctx, session.User.CredentialRef)
	if err != nil {
		return AccountSecurityOverview{}, fmt.Errorf("read redacted two-factor status: %w", err)
	}
	return result, nil
}

func (service *SessionSecurityService) ListSessions(ctx context.Context, cookieToken string) ([]UserWebSession, error) {
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return nil, err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionSessionReadSelf, now); err != nil {
		return nil, err
	}
	items, err := service.repository.ListActiveSessions(ctx, session.User.ID, session.TokenHash, now)
	if err != nil {
		return nil, fmt.Errorf("list active Web sessions: %w", err)
	}
	return items, nil
}

func (service *SessionSecurityService) RevokeSession(ctx context.Context, cookieToken, csrfToken string, sessionID uuid.UUID) (SessionRevocationResult, error) {
	if sessionID == uuid.Nil {
		return SessionRevocationResult{}, ErrInvalidInput
	}
	return service.revoke(ctx, cookieToken, csrfToken, SessionRevocationSingle, sessionID)
}

func (service *SessionSecurityService) RevokeOtherSessions(ctx context.Context, cookieToken, csrfToken string) (SessionRevocationResult, error) {
	return service.revoke(ctx, cookieToken, csrfToken, SessionRevocationOthers, uuid.Nil)
}

func (service *SessionSecurityService) revoke(ctx context.Context, cookieToken, csrfToken string, scope SessionRevocationScope, targetID uuid.UUID) (SessionRevocationResult, error) {
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return SessionRevocationResult{}, err
	}
	now := service.now().UTC()
	decision, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionSessionRevokeSelf, now)
	if err != nil {
		return SessionRevocationResult{}, err
	}
	commandID := service.newID()
	if commandID == uuid.Nil {
		return SessionRevocationResult{}, errors.New("session revocation id generator returned nil")
	}
	result, err := service.repository.ApplySessionRevocation(ctx, SessionRevocationCommand{
		ID: commandID, UserID: session.User.ID,
		CurrentTokenHash: append([]byte(nil), session.TokenHash...),
		TargetSessionID:  targetID, Scope: scope, OccurredAt: now,
		Authorization: decision,
	})
	if err != nil {
		return SessionRevocationResult{}, fmt.Errorf("apply session revocation: %w", err)
	}
	return result, nil
}
