package personalapikey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

const (
	rawAPIKeyBytes = 32
	apiKeyPrefix   = "pgk_"
)

type SessionAuthenticator interface {
	CurrentSession(context.Context, string) (identity.WebSession, error)
	AuthenticateWrite(context.Context, string, string) (identity.WebSession, error)
}

type ServiceConfig struct {
	Now        func() time.Time
	ReadRandom func([]byte) (int, error)
}

type Service struct {
	repository    Repository
	authenticator SessionAuthenticator
	authorizer    authz.Authorizer
	now           func() time.Time
	readRandom    func([]byte) (int, error)
}

func NewService(repository Repository, authenticator SessionAuthenticator, authorizer authz.Authorizer, config ServiceConfig) (*Service, error) {
	if repository == nil || authenticator == nil || authorizer == nil {
		return nil, errors.New("personal API key service dependencies are required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.ReadRandom == nil {
		config.ReadRandom = rand.Read
	}
	return &Service{
		repository: repository, authenticator: authenticator, authorizer: authorizer,
		now: config.Now, readRandom: config.ReadRandom,
	}, nil
}

func (service *Service) Status(ctx context.Context, cookieToken string) (Status, error) {
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return Status{}, err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionIntegrationAPIKeyReadSelf, now); err != nil {
		return Status{}, err
	}
	credential, err := service.repository.Credential(ctx, session.User.ID)
	if errors.Is(err, ErrNotFound) {
		return Status{Active: false, Scopes: []Scope{}}, nil
	}
	if err != nil {
		return Status{}, err
	}
	return credentialStatus(credential), nil
}

func (service *Service) Rotate(ctx context.Context, cookieToken, csrfToken string, expectedVersion *int64, requestedScopes []Scope) (IssuedCredential, error) {
	if expectedVersion != nil && *expectedVersion < 1 {
		return IssuedCredential{}, ErrInput
	}
	scopes, err := NormalizeScopes(requestedScopes)
	if err != nil {
		return IssuedCredential{}, err
	}
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return IssuedCredential{}, err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionIntegrationAPIKeyManageSelf, now); err != nil {
		return IssuedCredential{}, err
	}
	raw, digest, prefix, err := service.newAPIKey()
	if err != nil {
		return IssuedCredential{}, err
	}
	credential, err := service.repository.RotateCredential(ctx, session.User.ID, expectedVersion, digest, prefix, scopes, now)
	if err != nil {
		return IssuedCredential{}, err
	}
	return IssuedCredential{Credential: credentialStatus(credential), APIKey: raw}, nil
}

func (service *Service) Revoke(ctx context.Context, cookieToken, csrfToken string, expectedVersion int64) error {
	if expectedVersion < 1 {
		return ErrInput
	}
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionIntegrationAPIKeyManageSelf, now); err != nil {
		return err
	}
	return service.repository.RevokeCredential(ctx, session.User.ID, expectedVersion)
}

func (service *Service) Authenticate(ctx context.Context, raw string) (AuthenticatedCredential, error) {
	digest, err := apiKeyDigest(raw)
	if err != nil {
		return AuthenticatedCredential{}, ErrInvalid
	}
	return service.repository.Authenticate(ctx, digest, service.now().UTC())
}

func (service *Service) ResolveActiveUser(ctx context.Context, userID uuid.UUID, version int64, requiredScope Scope) (identity.User, error) {
	if userID == uuid.Nil || version < 1 || !isSupportedScope(requiredScope) {
		return identity.User{}, ErrInvalid
	}
	return service.repository.ResolveActiveUser(ctx, userID, version, requiredScope, service.now().UTC())
}

func (service *Service) newAPIKey() (string, []byte, string, error) {
	randomBytes := make([]byte, rawAPIKeyBytes)
	n, err := service.readRandom(randomBytes)
	if err != nil {
		return "", nil, "", fmt.Errorf("generate personal API key: %w", err)
	}
	if n != len(randomBytes) {
		return "", nil, "", fmt.Errorf("generate personal API key: %w", io.ErrUnexpectedEOF)
	}
	raw := apiKeyPrefix + base64.RawURLEncoding.EncodeToString(randomBytes)
	digest := sha256.Sum256([]byte(raw))
	return raw, digest[:], raw[:12], nil
}

func apiKeyDigest(raw string) ([]byte, error) {
	if len(raw) != 47 || !strings.HasPrefix(raw, apiKeyPrefix) || strings.TrimSpace(raw) != raw {
		return nil, ErrInvalid
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(raw, apiKeyPrefix))
	if err != nil || len(decoded) != rawAPIKeyBytes {
		return nil, ErrInvalid
	}
	digest := sha256.Sum256([]byte(raw))
	return digest[:], nil
}

func credentialStatus(credential Credential) Status {
	return Status{
		Active: true, KeyPrefix: credential.KeyPrefix, Version: credential.Version,
		Scopes: append([]Scope(nil), credential.Scopes...), CreatedAt: credential.CreatedAt.UTC(),
		LastUsedAt: credential.LastUsedAt,
	}
}

func isSupportedScope(scope Scope) bool {
	for _, supported := range supportedScopes {
		if scope == supported {
			return true
		}
	}
	return false
}
