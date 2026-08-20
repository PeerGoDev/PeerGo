package identity

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type SessionRevocationScope string

const (
	SessionRevocationSingle SessionRevocationScope = "single"
	SessionRevocationOthers SessionRevocationScope = "others"
)

// AccountSecurityOverview combines Core-owned timestamps with Vault's redacted
// factor status. No seed, recovery code or credential reference enters the DTO.
type AccountSecurityOverview struct {
	EmailVerified     bool
	PasswordChangedAt time.Time
	TwoFactor         TwoFactorStatus
}

type TwoFactorStatusProvider interface {
	TwoFactorStatus(context.Context, uuid.UUID) (TwoFactorStatus, error)
}

// UserWebSession is the browser-safe projection of one active session. ID is
// independent from TokenHash; IP addresses, raw user agents and fingerprints
// are intentionally not collected or returned.
type UserWebSession struct {
	ID         uuid.UUID
	Current    bool
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

type SessionRevocationCommand struct {
	ID               uuid.UUID
	UserID           uuid.UUID
	CurrentTokenHash []byte
	TargetSessionID  uuid.UUID
	Scope            SessionRevocationScope
	OccurredAt       time.Time
	Authorization    authz.Decision
}

type SessionRevocationResult struct {
	RevokedWebSessions    int64
	RevokedStaffSessions  int64
	CurrentSessionRevoked bool
}

type SessionRevocationAuditInput struct {
	Command SessionRevocationCommand
	Result  SessionRevocationResult
}

type SessionSecurityRepository interface {
	SecurityOverview(context.Context, uuid.UUID) (AccountSecurityOverview, error)
	ListActiveSessions(context.Context, uuid.UUID, []byte, time.Time) ([]UserWebSession, error)
	ApplySessionRevocation(context.Context, SessionRevocationCommand) (SessionRevocationResult, error)
}

type SessionRevocationEventBuilder interface {
	BuildSessionRevocationEvent(SessionRevocationAuditInput) (auditevent.Event, error)
}

type WebSessionAuthenticator interface {
	CurrentSession(context.Context, string) (WebSession, error)
	AuthenticateWrite(context.Context, string, string) (WebSession, error)
}
