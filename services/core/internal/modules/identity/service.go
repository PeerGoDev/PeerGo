package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	randomTokenBytes     = 32
	webCSRFDomSeparation = "peergo:web-session:csrf:v1\x00"
	maxIdentifierRunes   = 254
	maxPasswordBytes     = 1024
)

// Repository is the Core PostgreSQL boundary for public user projections and
// Web sessions. Implementations never receive an identifier or password.
type Repository interface {
	UserByCredentialRef(context.Context, uuid.UUID, time.Time) (User, error)
	WebSessionPolicy(context.Context) (WebSessionPolicy, error)
	CreateSession(context.Context, SessionRecord) error
	ActiveSession(context.Context, []byte, time.Time) (SessionRecord, error)
	RevokeSession(context.Context, []byte, time.Time) error
}

// CredentialVerifier is the narrow Privacy Vault port. A successful decision
// returns only an opaque reference, not hashes or direct identifiers.
type CredentialVerifier interface {
	Verify(context.Context, LoginInput) (uuid.UUID, error)
}

// ServiceConfig contains cryptographic and timing dependencies. Injecting the
// clock and random reader keeps security invariants deterministic in tests.
type ServiceConfig struct {
	CSRFKey []byte
	Now     func() time.Time
	Random  io.Reader
}

// Service creates and authenticates Core-owned Web sessions.
type Service struct {
	repository Repository
	verifier   CredentialVerifier
	csrfKey    []byte
	now        func() time.Time
	random     io.Reader
}

// NewService validates security-critical dependencies before serving traffic.
func NewService(repository Repository, verifier CredentialVerifier, config ServiceConfig) (*Service, error) {
	if repository == nil {
		return nil, errors.New("identity repository is required")
	}
	if verifier == nil {
		return nil, errors.New("credential verifier is required")
	}
	if len(config.CSRFKey) < sha256.Size {
		return nil, errors.New("identity csrf key must contain at least 32 bytes")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}

	return &Service{
		repository: repository,
		verifier:   verifier,
		csrfKey:    append([]byte(nil), config.CSRFKey...),
		now:        config.Now,
		random:     config.Random,
	}, nil
}

// Login verifies credentials through Vault, then persists a digest of a new
// random browser token. The same public error is used for every account miss.
func (s *Service) Login(ctx context.Context, input LoginInput) (WebSession, error) {
	input.Identifier = strings.TrimSpace(input.Identifier)
	if input.Identifier == "" || utf8.RuneCountInString(input.Identifier) > maxIdentifierRunes {
		return WebSession{}, ErrInvalidInput
	}
	if input.Password == "" || len(input.Password) > maxPasswordBytes {
		return WebSession{}, ErrInvalidInput
	}
	input.SecondFactorCode = strings.TrimSpace(input.SecondFactorCode)
	if len(input.SecondFactorCode) > 32 {
		return WebSession{}, ErrInvalidInput
	}

	credentialRef, err := s.verifier.Verify(ctx, input)
	if err != nil {
		return WebSession{}, err
	}

	now := s.now().UTC()
	user, err := s.repository.UserByCredentialRef(ctx, credentialRef, now)
	if errors.Is(err, ErrInvalidCredentials) {
		return WebSession{}, ErrInvalidCredentials
	}
	if err != nil {
		return WebSession{}, fmt.Errorf("load user for verified credential: %w", err)
	}
	policy, err := s.repository.WebSessionPolicy(ctx)
	if err != nil {
		return WebSession{}, fmt.Errorf("load web session policy: %w", err)
	}
	if !validWebSessionPolicy(policy) {
		return WebSession{}, errors.New("web session policy is invalid")
	}

	rawToken, tokenHash, cookieToken, err := newSessionToken(s.random)
	if err != nil {
		return WebSession{}, fmt.Errorf("generate web session token: %w", err)
	}

	duration := policy.SessionDuration
	if input.RememberMe {
		duration = policy.RememberSessionDuration
	}
	expiresAt := now.Add(duration)
	if err := s.repository.CreateSession(ctx, SessionRecord{
		TokenHash:     tokenHash,
		User:          user,
		ClientAddress: input.ClientAddress,
		CreatedAt:     now,
		ExpiresAt:     expiresAt,
	}); err != nil {
		return WebSession{}, fmt.Errorf("persist web session: %w", err)
	}

	return WebSession{
		User:        user,
		TokenHash:   append([]byte(nil), tokenHash...),
		CreatedAt:   now,
		ExpiresAt:   expiresAt,
		CSRFToken:   s.csrfToken(rawToken),
		CookieToken: cookieToken,
	}, nil
}

func validWebSessionPolicy(policy WebSessionPolicy) bool {
	return policy.SessionDuration >= time.Hour && policy.SessionDuration <= 30*24*time.Hour &&
		policy.RememberSessionDuration >= 24*time.Hour && policy.RememberSessionDuration <= 90*24*time.Hour &&
		policy.SessionDuration <= policy.RememberSessionDuration
}

// CurrentSession authenticates the raw cookie without ever querying by it.
func (s *Service) CurrentSession(ctx context.Context, cookieToken string) (WebSession, error) {
	rawToken, err := decodeCookieToken(cookieToken)
	if err != nil {
		return WebSession{}, ErrSessionNotFound
	}
	tokenHash := sha256.Sum256(rawToken)
	record, err := s.repository.ActiveSession(ctx, tokenHash[:], s.now().UTC())
	if err != nil {
		return WebSession{}, err
	}

	return WebSession{
		User:        record.User,
		TokenHash:   append([]byte(nil), record.TokenHash...),
		CreatedAt:   record.CreatedAt,
		ExpiresAt:   record.ExpiresAt,
		CSRFToken:   s.csrfToken(rawToken),
		CookieToken: cookieToken,
	}, nil
}

// AuthenticateWrite validates both the ordinary Web session and its bound
// CSRF value. Higher-risk workflows reuse this method instead of duplicating
// token parsing or constant-time comparison in HTTP handlers.
func (s *Service) AuthenticateWrite(ctx context.Context, cookieToken, csrfToken string) (WebSession, error) {
	session, err := s.CurrentSession(ctx, cookieToken)
	if err != nil {
		return WebSession{}, err
	}
	if !validCSRF(csrfToken, session.CSRFToken) {
		return WebSession{}, ErrInvalidCSRF
	}
	return session, nil
}

// Logout requires both the HttpOnly cookie and its session-bound CSRF token.
func (s *Service) Logout(ctx context.Context, cookieToken, csrfToken string) error {
	rawToken, err := decodeCookieToken(cookieToken)
	if err != nil {
		return ErrSessionNotFound
	}
	tokenHash := sha256.Sum256(rawToken)
	if _, err := s.repository.ActiveSession(ctx, tokenHash[:], s.now().UTC()); err != nil {
		return err
	}

	expected := s.csrfToken(rawToken)
	if !validCSRF(csrfToken, expected) {
		return ErrInvalidCSRF
	}

	if err := s.repository.RevokeSession(ctx, tokenHash[:], s.now().UTC()); err != nil {
		return fmt.Errorf("revoke web session: %w", err)
	}
	return nil
}

func (s *Service) csrfToken(rawToken []byte) string {
	mac := hmac.New(sha256.New, s.csrfKey)
	_, _ = mac.Write([]byte(webCSRFDomSeparation))
	_, _ = mac.Write(rawToken)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func validCSRF(provided, expected string) bool {
	return len(provided) == len(expected) && subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func decodeCookieToken(encoded string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(decoded) != randomTokenBytes {
		return nil, ErrSessionNotFound
	}
	return decoded, nil
}

func newSessionToken(random io.Reader) (rawToken, tokenHash []byte, cookieToken string, err error) {
	rawToken = make([]byte, randomTokenBytes)
	if _, err = io.ReadFull(random, rawToken); err != nil {
		return nil, nil, "", err
	}
	digest := sha256.Sum256(rawToken)
	return rawToken, append([]byte(nil), digest[:]...), base64.RawURLEncoding.EncodeToString(rawToken), nil
}

func isSixDigits(value string) bool {
	if len(value) != 6 {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
