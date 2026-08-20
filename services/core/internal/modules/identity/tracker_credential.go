package identity

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/contracts/go/trackerpasskeyv1"
)

var (
	ErrTrackerCredentialUnavailable   = errors.New("Tracker credential service is unavailable")
	ErrTrackerCredentialStateConflict = errors.New("Tracker credential state conflicts with Core")
)

// TrackerCredential is intentionally short lived. Core uses Passkey only to
// construct one authenticated announce URL; the database projection contains
// LookupHMAC and VaultVersion but never this plaintext value.
type TrackerCredential struct {
	Passkey    string
	LookupHMAC [sha256.Size]byte
	Version    int64
	CreatedAt  time.Time
}

type TrackerCredentialVault interface {
	GetOrCreateTrackerCredential(context.Context, uuid.UUID) (TrackerCredential, error)
}

type TrackerCredentialProjection struct {
	UserID        uuid.UUID
	CredentialRef uuid.UUID
	LookupHMAC    [sha256.Size]byte
	VaultVersion  int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type TrackerCredentialProjectionRepository interface {
	BindTrackerCredential(context.Context, uuid.UUID, uuid.UUID, TrackerCredential, time.Time) (TrackerCredentialProjection, error)
}

type TrackerCredentialService struct {
	vault      TrackerCredentialVault
	repository TrackerCredentialProjectionRepository
	now        func() time.Time
}

func NewTrackerCredentialService(vault TrackerCredentialVault, repository TrackerCredentialProjectionRepository, now func() time.Time) (*TrackerCredentialService, error) {
	if vault == nil || repository == nil {
		return nil, errors.New("Tracker credential service dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &TrackerCredentialService{vault: vault, repository: repository, now: now}, nil
}

// ForUser reconciles Vault's stable credential into the non-reversible Core
// projection before returning it. If Vault committed but Core timed out, the
// next call safely repairs the same version instead of minting another passkey.
func (service *TrackerCredentialService) ForUser(ctx context.Context, user User) (TrackerCredential, error) {
	if user.ID == uuid.Nil || user.CredentialRef == uuid.Nil {
		return TrackerCredential{}, ErrTrackerCredentialStateConflict
	}
	credential, err := service.vault.GetOrCreateTrackerCredential(ctx, user.CredentialRef)
	if err != nil {
		return TrackerCredential{}, err
	}
	if !validCoreTrackerPasskey(credential.Passkey) || credential.Version < 1 || credential.CreatedAt.IsZero() {
		return TrackerCredential{}, ErrTrackerCredentialStateConflict
	}
	projection, err := service.repository.BindTrackerCredential(
		ctx,
		user.ID,
		user.CredentialRef,
		credential,
		service.now().UTC(),
	)
	if err != nil {
		return TrackerCredential{}, err
	}
	if projection.UserID != user.ID || projection.CredentialRef != user.CredentialRef ||
		projection.LookupHMAC != credential.LookupHMAC || projection.VaultVersion != credential.Version {
		return TrackerCredential{}, ErrTrackerCredentialStateConflict
	}
	return credential, nil
}

func validCoreTrackerPasskey(passkey string) bool {
	_, err := trackerpasskeyv1.DetectProfile(passkey)
	return err == nil
}
