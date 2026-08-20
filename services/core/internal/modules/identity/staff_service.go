package identity

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	defaultStaffSessionDuration = 15 * time.Minute
	staffCSRFDomSeparation      = "peergo:staff-session:csrf:v1\x00"
	maxStaffAssertionBytes      = 32 * 1024
)

// StaffSessionCreation is the single persistence command that updates the
// WebAuthn credential record and inserts the staff token digest atomically.
// Losing the credential counter update while accepting a privileged session
// would weaken cloned-authenticator detection on the next request. Authority
// is the exact audited decision evidence that the new staff token derives from.
type StaffSessionCreation struct {
	TokenHash               []byte
	ParentTokenHash         []byte
	StaffCredentialID       []byte
	CredentialRecord        ProtectedRecord
	Authority               authz.AuthorityBinding
	UserID                  uuid.UUID
	CreatedAt               time.Time
	ExpiresAt               time.Time
	WebAuthnAuthenticatedAt time.Time
}

// StaffRepository is the Core persistence boundary for pre-provisioned staff
// credentials, one-time WebAuthn state and the independent session audience.
type StaffRepository interface {
	ListActiveStaffWebAuthnCredentials(context.Context, uuid.UUID) ([]StaffWebAuthnCredential, error)
	CreateStaffWebAuthnChallenge(context.Context, StaffWebAuthnChallenge) error
	ConsumeStaffWebAuthnChallenge(context.Context, uuid.UUID, uuid.UUID, []byte, time.Time) (StaffWebAuthnChallenge, error)
	CreateStaffSession(context.Context, StaffSessionCreation) (time.Time, error)
	ActiveStaffSession(context.Context, []byte, time.Time) (StaffSessionRecord, error)
	RevokeStaffSession(context.Context, []byte, time.Time) error
}

type staffAuthorizer = authz.Authorizer

// StaffServiceConfig contains independent staff-session cryptographic and
// timing inputs. Tests inject all entropy and clocks at this boundary.
type StaffServiceConfig struct {
	CSRFKey         []byte
	SessionDuration time.Duration
	Now             func() time.Time
	Random          io.Reader
	NewChallengeID  func() uuid.UUID
}

// StaffService coordinates the complete elevation workflow. An ordinary Web
// session is only a prerequisite: it is never accepted as staff authority and
// cannot be converted without an eligible grant plus a verified assertion.
type StaffService struct {
	webSessions     *Service
	repository      StaffRepository
	ceremony        StaffWebAuthnCeremony
	protector       *RecordProtector
	authorizer      staffAuthorizer
	csrfKey         []byte
	sessionDuration time.Duration
	now             func() time.Time
	random          io.Reader
	newChallengeID  func() uuid.UUID
}

func NewStaffService(webSessions *Service, repository StaffRepository, ceremony StaffWebAuthnCeremony, protector *RecordProtector, authorizer staffAuthorizer, config StaffServiceConfig) (*StaffService, error) {
	if webSessions == nil || repository == nil || ceremony == nil || protector == nil || authorizer == nil {
		return nil, errors.New("staff identity dependencies are required")
	}
	if len(config.CSRFKey) < sha256.Size {
		return nil, errors.New("staff session csrf key must contain at least 32 bytes")
	}
	if config.SessionDuration == 0 {
		config.SessionDuration = defaultStaffSessionDuration
	}
	if config.SessionDuration < time.Minute || config.SessionDuration > 30*time.Minute {
		return nil, errors.New("staff session duration must be between one and thirty minutes")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.NewChallengeID == nil {
		config.NewChallengeID = uuid.New
	}
	return &StaffService{
		webSessions:     webSessions,
		repository:      repository,
		ceremony:        ceremony,
		protector:       protector,
		authorizer:      authorizer,
		csrfKey:         append([]byte(nil), config.CSRFKey...),
		sessionDuration: config.SessionDuration,
		now:             config.Now,
		random:          config.Random,
		newChallengeID:  config.NewChallengeID,
	}, nil
}

// BeginElevation creates one server-side challenge after checking the current
// staff eligibility grant. Any prior unconsumed challenge for this Web session
// is invalidated by the repository, keeping one active ceremony per session.
func (service *StaffService) BeginElevation(ctx context.Context, webCookie, csrfToken string) (StaffElevationOptions, error) {
	webSession, err := service.webSessions.AuthenticateWrite(ctx, webCookie, csrfToken)
	if err != nil {
		return StaffElevationOptions{}, err
	}
	now := service.now().UTC()
	if _, err := service.authorize(ctx, webSession.User, authz.ActionStaffSessionCreateSelf, authz.AudienceWebSession, time.Time{}, now, authz.AuthorityBinding{}); err != nil {
		return StaffElevationOptions{}, err
	}

	materials, err := service.activeCredentialMaterials(ctx, webSession.User)
	if err != nil {
		return StaffElevationOptions{}, err
	}
	publicKey, sessionData, expiresAt, err := service.ceremony.Begin(webSession.User, materials)
	if err != nil {
		return StaffElevationOptions{}, err
	}
	// The database also enforces a ten-minute ceiling. Checking here catches a
	// misconfigured or replaced provider before storing unusable ceremony data.
	if !expiresAt.After(now) || expiresAt.After(now.Add(10*time.Minute)) {
		return StaffElevationOptions{}, errors.New("staff WebAuthn provider returned an invalid expiry")
	}
	challengeID := service.newChallengeID()
	if challengeID == uuid.Nil {
		return StaffElevationOptions{}, errors.New("staff WebAuthn challenge ID generator returned nil")
	}
	protected, err := service.protector.Seal(staffChallengeRecordKind, webSession.User.ID, challengeID[:], sessionData)
	if err != nil {
		return StaffElevationOptions{}, fmt.Errorf("protect staff WebAuthn session: %w", err)
	}
	challenge := StaffWebAuthnChallenge{
		ID:              challengeID,
		UserID:          webSession.User.ID,
		ParentTokenHash: append([]byte(nil), webSession.TokenHash...),
		Protected:       protected,
		CreatedAt:       now,
		ExpiresAt:       expiresAt.UTC(),
	}
	if err := service.repository.CreateStaffWebAuthnChallenge(ctx, challenge); err != nil {
		return StaffElevationOptions{}, fmt.Errorf("persist staff WebAuthn challenge: %w", err)
	}
	return StaffElevationOptions{ChallengeID: challengeID, ExpiresAt: challenge.ExpiresAt, PublicKey: publicKey}, nil
}

// CompleteElevation consumes the challenge before parsing or verifying the
// assertion. A failed signature therefore cannot reuse the same challenge;
// the caller must begin a fresh, independently audited attempt.
func (service *StaffService) CompleteElevation(ctx context.Context, webCookie, csrfToken string, input CompleteStaffElevationInput) (StaffSession, error) {
	if input.ChallengeID == uuid.Nil || len(input.Assertion) == 0 || len(input.Assertion) > maxStaffAssertionBytes || !json.Valid(input.Assertion) {
		return StaffSession{}, ErrInvalidInput
	}
	webSession, err := service.webSessions.AuthenticateWrite(ctx, webCookie, csrfToken)
	if err != nil {
		return StaffSession{}, err
	}
	now := service.now().UTC()
	decision, err := service.authorize(ctx, webSession.User, authz.ActionStaffSessionCreateSelf, authz.AudienceWebSession, time.Time{}, now, authz.AuthorityBinding{})
	if err != nil {
		return StaffSession{}, err
	}
	challenge, err := service.repository.ConsumeStaffWebAuthnChallenge(ctx, input.ChallengeID, webSession.User.ID, webSession.TokenHash, now)
	if err != nil {
		return StaffSession{}, err
	}
	sessionData, err := service.protector.Open(staffChallengeRecordKind, webSession.User.ID, input.ChallengeID[:], challenge.Protected)
	if err != nil {
		return StaffSession{}, fmt.Errorf("open staff WebAuthn session: %w", err)
	}
	materials, err := service.activeCredentialMaterials(ctx, webSession.User)
	if err != nil {
		return StaffSession{}, err
	}
	result, err := service.ceremony.Finish(webSession.User, materials, sessionData, input.Assertion)
	if err != nil {
		if errors.Is(err, ErrStaffWebAuthnVerification) {
			return StaffSession{}, ErrStaffWebAuthnVerification
		}
		return StaffSession{}, err
	}
	if result.CloneWarning {
		return StaffSession{}, ErrStaffAuthenticatorCloneDetected
	}
	if !materialContainsCredential(materials, result.CredentialID) {
		return StaffSession{}, ErrStaffWebAuthnVerification
	}
	protectedCredential, err := service.protector.Seal(staffCredentialRecordKind, webSession.User.ID, result.CredentialID, result.Record)
	if err != nil {
		return StaffSession{}, fmt.Errorf("protect updated staff WebAuthn credential: %w", err)
	}
	rawToken, tokenHash, cookieToken, err := newSessionToken(service.random)
	if err != nil {
		return StaffSession{}, fmt.Errorf("generate staff session token: %w", err)
	}
	expiresAt := earliestTime(now.Add(service.sessionDuration), webSession.ExpiresAt, decision.EffectiveUntil)
	if !expiresAt.After(now) {
		return StaffSession{}, authz.ErrForbidden
	}
	created := StaffSessionCreation{
		TokenHash:               tokenHash,
		ParentTokenHash:         append([]byte(nil), webSession.TokenHash...),
		StaffCredentialID:       append([]byte(nil), result.CredentialID...),
		CredentialRecord:        protectedCredential,
		Authority:               decision.AuthorityBinding(),
		UserID:                  webSession.User.ID,
		CreatedAt:               now,
		ExpiresAt:               expiresAt,
		WebAuthnAuthenticatedAt: now,
	}
	persistedExpiry, err := service.repository.CreateStaffSession(ctx, created)
	if err != nil {
		return StaffSession{}, fmt.Errorf("persist staff session: %w", err)
	}
	if !persistedExpiry.After(now) || persistedExpiry.After(expiresAt) {
		return StaffSession{}, errors.New("staff session repository expanded or invalidated the authorized expiry")
	}
	return StaffSession{
		User:                    webSession.User,
		Authority:               created.Authority,
		CreatedAt:               now,
		ExpiresAt:               persistedExpiry.UTC(),
		AuthenticationMethod:    StaffAuthenticationPasskey,
		AuthenticatedAt:         now,
		WebAuthnAuthenticatedAt: now,
		CSRFToken:               service.csrfToken(rawToken),
		CookieToken:             cookieToken,
	}, nil
}

// CurrentSession authenticates only the staff cookie and then re-evaluates the
// current grant. The parent Web session and credential status are checked by
// the repository, so revoking either invalidates this audience immediately.
func (service *StaffService) CurrentSession(ctx context.Context, cookieToken string) (StaffSession, error) {
	rawToken, err := decodeCookieToken(cookieToken)
	if err != nil {
		return StaffSession{}, ErrStaffSessionNotFound
	}
	digest := sha256.Sum256(rawToken)
	now := service.now().UTC()
	record, err := service.repository.ActiveStaffSession(ctx, digest[:], now)
	if err != nil {
		return StaffSession{}, err
	}
	if _, err := service.authorize(ctx, record.User, authz.ActionStaffSessionReadSelf, authz.AudienceStaffSession, record.WebAuthnAuthenticatedAt, now, record.Authority); err != nil {
		return StaffSession{}, err
	}
	return StaffSession{
		User:                    record.User,
		Authority:               record.Authority,
		CreatedAt:               record.CreatedAt,
		ExpiresAt:               record.ExpiresAt,
		AuthenticationMethod:    StaffAuthenticationPasskey,
		AuthenticatedAt:         record.WebAuthnAuthenticatedAt,
		WebAuthnAuthenticatedAt: record.WebAuthnAuthenticatedAt,
		CSRFToken:               service.csrfToken(rawToken),
		CookieToken:             cookieToken,
	}, nil
}

// AuthenticateWrite validates the exact active staff authority first and then
// verifies the CSRF token bound to this independent credential audience. HTTP
// handlers call it before any staff business write so CSRF logic is not copied
// into each owning module.
func (service *StaffService) AuthenticateWrite(ctx context.Context, cookieToken, csrfToken string) (StaffSession, error) {
	session, err := service.CurrentSession(ctx, cookieToken)
	if err != nil {
		return StaffSession{}, err
	}
	rawToken, err := decodeCookieToken(cookieToken)
	if err != nil {
		return StaffSession{}, ErrStaffSessionNotFound
	}
	if !validCSRF(csrfToken, service.csrfToken(rawToken)) {
		return StaffSession{}, ErrInvalidCSRF
	}
	return session, nil
}

// Logout is intentionally authorized by possession of the active staff token
// plus its CSRF value. It remains possible after a grant is revoked; requiring
// a now-invalid grant to reduce privilege would strand a server-side session.
func (service *StaffService) Logout(ctx context.Context, cookieToken, csrfToken string) error {
	rawToken, err := decodeCookieToken(cookieToken)
	if err != nil {
		return ErrStaffSessionNotFound
	}
	digest := sha256.Sum256(rawToken)
	if _, err := service.repository.ActiveStaffSession(ctx, digest[:], service.now().UTC()); err != nil {
		return err
	}
	if !validCSRF(csrfToken, service.csrfToken(rawToken)) {
		return ErrInvalidCSRF
	}
	if err := service.repository.RevokeStaffSession(ctx, digest[:], service.now().UTC()); err != nil {
		return fmt.Errorf("revoke staff session: %w", err)
	}
	return nil
}

func (service *StaffService) activeCredentialMaterials(ctx context.Context, user User) ([]StaffCredentialMaterial, error) {
	return loadStaffCredentialMaterials(ctx, service.repository, service.protector, user, true)
}

func (service *StaffService) authorize(ctx context.Context, user User, action authz.Action, audience authz.CredentialAudience, mfaAt, now time.Time, requiredAuthority authz.AuthorityBinding) (authz.Decision, error) {
	return authorizeStaffIdentity(ctx, service.authorizer, user, action, audience, mfaAt, now, requiredAuthority)
}

func (service *StaffService) csrfToken(rawToken []byte) string {
	mac := hmac.New(sha256.New, service.csrfKey)
	_, _ = mac.Write([]byte(staffCSRFDomSeparation))
	_, _ = mac.Write(rawToken)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func materialContainsCredential(materials []StaffCredentialMaterial, credentialID []byte) bool {
	for _, material := range materials {
		if bytes.Equal(material.ID, credentialID) {
			return true
		}
	}
	return false
}

func earliestTime(values ...time.Time) time.Time {
	if len(values) == 0 {
		return time.Time{}
	}
	result := values[0]
	for _, value := range values[1:] {
		if value.Before(result) {
			result = value
		}
	}
	return result
}
