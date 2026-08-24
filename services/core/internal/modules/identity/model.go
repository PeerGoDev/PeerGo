// Package identity owns Core-side account references plus Web and staff
// sessions. Ordinary P0/P1 login material is verified by Privacy Vault and is
// never persisted here; staff public-key records are encrypted before storage.
package identity

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

var (
	// ErrInvalidInput means the command is outside the identity contract bounds.
	ErrInvalidInput = errors.New("identity input is invalid")
	// ErrPublicUserNotFound covers unknown, inactive and access-restricted
	// members so the public profile cannot reveal account-control state.
	ErrPublicUserNotFound = errors.New("public user profile was not found")
	// ErrInvalidCredentials deliberately covers unknown, disabled and mismatched
	// credentials so the public response cannot enumerate accounts.
	ErrInvalidCredentials = errors.New("identifier or password is invalid")
	// ErrCredentialVerifierUnavailable means Privacy Vault could not safely make
	// a verification decision; callers must fail closed.
	ErrCredentialVerifierUnavailable = errors.New("credential verifier is unavailable")
	// ErrLoginThrottled is returned for both known and unknown identifier HMAC
	// buckets; callers must not alter the message based on account existence.
	ErrLoginThrottled = errors.New("login attempts are temporarily throttled")
	// ErrSecondFactorRequired is returned only after Vault has verified the
	// password for a credential with an enabled factor. No Web session exists yet.
	ErrSecondFactorRequired = errors.New("a second factor is required")
	// ErrTwoFactorVerification deliberately combines a wrong password, invalid
	// TOTP, replayed time step and unavailable recovery code.
	ErrTwoFactorVerification         = errors.New("two-factor verification failed")
	ErrTwoFactorAlreadyEnabled       = errors.New("two-factor authentication is already enabled")
	ErrTwoFactorNotEnabled           = errors.New("two-factor authentication is not enabled")
	ErrTwoFactorEnrollmentNotFound   = errors.New("two-factor enrollment was not found")
	ErrRecoveryCodeBundleUnavailable = errors.New("recovery code bundle is unavailable")
	ErrTwoFactorIdempotencyConflict  = errors.New("two-factor idempotency key conflicts with an existing change")
	ErrTwoFactorServiceUnavailable   = errors.New("two-factor service is unavailable")
	// ErrSessionNotFound covers missing, malformed, expired and revoked sessions.
	ErrSessionNotFound = errors.New("web session was not found")
	// ErrInvalidCSRF means a write was not authorized by the session-bound token.
	ErrInvalidCSRF = errors.New("csrf token is invalid")
	// ErrStaffCredentialRequired means the subject is eligible for staff access
	// but has no active credential provisioned by the controlled bootstrap flow.
	ErrStaffCredentialRequired = errors.New("an active staff WebAuthn credential is required")
	// ErrStaffChallengeNotFound deliberately combines missing, expired, replayed
	// and differently-bound challenges into one result.
	ErrStaffChallengeNotFound = errors.New("staff WebAuthn challenge was not found")
	// ErrStaffWebAuthnVerification covers malformed or invalid assertions without
	// exposing which verification step failed.
	ErrStaffWebAuthnVerification = errors.New("staff WebAuthn verification failed")
	// ErrStaffAuthenticatorCloneDetected is fail-closed: a valid signature with a
	// non-monotonic counter cannot create a privileged session.
	ErrStaffAuthenticatorCloneDetected = errors.New("staff authenticator clone signal detected")
	// ErrStaffSessionNotFound covers malformed, expired, revoked, parent-revoked,
	// credential-revoked and inactive-subject staff sessions.
	ErrStaffSessionNotFound = errors.New("staff session was not found")
)

type LoginThrottleError struct {
	RetryAt time.Time
}

func (err *LoginThrottleError) Error() string { return ErrLoginThrottled.Error() }
func (err *LoginThrottleError) Unwrap() error { return ErrLoginThrottled }

// User is the non-sensitive Core identity projection returned to the Web.
type User struct {
	ID              uuid.UUID
	CredentialRef   uuid.UUID
	Username        string
	DisplayName     string
	EmailVerifiedAt *time.Time
}

// PublicUserProfile is the deliberately small member-directory projection.
// Private account, credential, traffic and moderation fields must never be
// added here merely because they exist in another identity read model.
type PublicUserProfile struct {
	NumericID             int64
	Username              string
	DisplayName           string
	JoinedAt              time.Time
	PublishedTorrentCount int64
	PublishedTorrents     []PublicUserPublishedTorrent
}

// PublicUserPublishedTorrent is the intentionally small, non-anonymous
// publication list shown to other members. Submission/review state and
// uploader-private evidence remain in the self-service torrent projection.
type PublicUserPublishedTorrent struct {
	ID             int64
	Title          string
	Subtitle       string
	CategoryID     string
	CategoryName   string
	TotalSizeBytes int64
	PublishedAt    time.Time
}

// SessionRecord contains only the digest of the browser token. Raw tokens must
// exist only in request memory and the HttpOnly cookie response.
type SessionRecord struct {
	TokenHash []byte
	User      User
	CreatedAt time.Time
	ExpiresAt time.Time
}

// WebSessionPolicy is read from the identity-owned singleton immediately
// before a new browser session is issued. Existing sessions keep their
// committed expiry; policy edits apply only to later logins.
type WebSessionPolicy struct {
	SessionDuration         time.Duration
	RememberSessionDuration time.Duration
}

// LoginInput is the transport-independent create-session command.
type LoginInput struct {
	Identifier       string
	Password         string
	SecondFactorCode string
	RememberMe       bool
}

// WebSession is returned by identity use cases. CookieToken is transport-only
// material and must never be serialized into the JSON DTO.
type WebSession struct {
	User        User
	TokenHash   []byte
	CreatedAt   time.Time
	ExpiresAt   time.Time
	CSRFToken   string
	CookieToken string
}

// ProtectedRecord is an AES-GCM envelope. KeyEpoch is explicit so a process
// never attempts to decrypt a record with the wrong key silently.
type ProtectedRecord struct {
	Ciphertext []byte
	Nonce      []byte
	KeyEpoch   string
}

// StaffWebAuthnCredential stores the plaintext lookup ID separately from the
// encrypted complete credential record required by the WebAuthn library.
type StaffWebAuthnCredential struct {
	ID        []byte
	UserID    uuid.UUID
	Protected ProtectedRecord
}

// StaffWebAuthnChallenge is one-time, server-side ceremony state bound to the
// exact ordinary Web session which initiated elevation.
type StaffWebAuthnChallenge struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	ParentTokenHash []byte
	Protected       ProtectedRecord
	CreatedAt       time.Time
	ExpiresAt       time.Time
}

// StaffElevationOptions contains only client-safe assertion options. The
// WebAuthn SessionData needed for verification never leaves the server.
type StaffElevationOptions struct {
	ChallengeID uuid.UUID
	ExpiresAt   time.Time
	PublicKey   json.RawMessage
}

// CompleteStaffElevationInput is the browser's response to an existing,
// session-bound challenge.
type CompleteStaffElevationInput struct {
	ChallengeID uuid.UUID
	Assertion   json.RawMessage
}

// StaffSessionRecord is the persistence representation of a staff session.
// It contains no raw cookie token.
type StaffSessionRecord struct {
	TokenHash               []byte
	ParentTokenHash         []byte
	StaffCredentialID       []byte
	Authority               authz.AuthorityBinding
	User                    User
	CreatedAt               time.Time
	ExpiresAt               time.Time
	WebAuthnAuthenticatedAt time.Time
}

// StaffSession is a distinct, short-lived credential audience. CookieToken and
// Authority are server-side enforcement material: transport DTOs map fields
// explicitly and must never serialize either value into JSON or logs.
type StaffSession struct {
	User                    User
	Authority               authz.AuthorityBinding
	CreatedAt               time.Time
	ExpiresAt               time.Time
	AuthenticationMethod    StaffAuthenticationMethod
	AuthenticatedAt         time.Time
	WebAuthnAuthenticatedAt time.Time
	CSRFToken               string
	CookieToken             string
}

// StaffAuthenticationMethod tells the first-party client whether admin access
// comes directly from the current account login or from the optional passkey
// elevation flow retained for installations that want it.
type StaffAuthenticationMethod string

const (
	StaffAuthenticationAccountSession StaffAuthenticationMethod = "account_session"
	StaffAuthenticationPasskey        StaffAuthenticationMethod = "passkey"
)
