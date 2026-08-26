// Package personalapikey owns the user-controlled credential shared by
// external PeerGo integrations. It persists one hashed, scope-bounded key per
// user and deliberately stores no per-request history.
package personalapikey

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

var (
	ErrNotFound    = errors.New("personal API key was not found")
	ErrConflict    = errors.New("personal API key version changed")
	ErrInvalid     = errors.New("personal API key is invalid")
	ErrInput       = errors.New("personal API key input is invalid")
	ErrScopeDenied = errors.New("personal API key scope is not granted")
)

type Scope string

const (
	ScopeProfileRead     Scope = "profile:read"
	ScopeTorrentRead     Scope = "torrent:read"
	ScopeTorrentDownload Scope = "torrent:download"
	ScopeAttendanceRead  Scope = "attendance:read"
	ScopeAttendanceClaim Scope = "attendance:claim"
)

var supportedScopes = []Scope{
	ScopeProfileRead,
	ScopeTorrentRead,
	ScopeTorrentDownload,
	ScopeAttendanceRead,
	ScopeAttendanceClaim,
}

type Credential struct {
	UserID     uuid.UUID
	KeyPrefix  string
	Version    int64
	Scopes     []Scope
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

type Status struct {
	Active     bool
	KeyPrefix  string
	Version    int64
	Scopes     []Scope
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

type IssuedCredential struct {
	Credential Status
	APIKey     string
}

type AuthenticatedCredential struct {
	Credential Credential
	User       identity.User
	NumericID  int64
}

func SupportedScopes() []Scope {
	return append([]Scope(nil), supportedScopes...)
}

// NormalizeScopes rejects unknown, empty and duplicate scope sets, then puts
// valid scopes into one canonical order for stable storage and responses.
func NormalizeScopes(input []Scope) ([]Scope, error) {
	if len(input) == 0 || len(input) > len(supportedScopes) {
		return nil, ErrInput
	}
	requested := make(map[Scope]struct{}, len(input))
	for _, scope := range input {
		if _, exists := requested[scope]; exists {
			return nil, ErrInput
		}
		requested[scope] = struct{}{}
	}
	result := make([]Scope, 0, len(input))
	for _, supported := range supportedScopes {
		if _, exists := requested[supported]; exists {
			result = append(result, supported)
			delete(requested, supported)
		}
	}
	if len(requested) != 0 {
		return nil, ErrInput
	}
	return result, nil
}

func RequireScope(credential AuthenticatedCredential, required Scope) error {
	for _, scope := range credential.Credential.Scopes {
		if scope == required {
			return nil
		}
	}
	return ErrScopeDenied
}
