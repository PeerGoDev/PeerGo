package identity

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

type trackerCredentialVaultFixture struct {
	credentialRef uuid.UUID
	credential    TrackerCredential
	err           error
}

func (fixture *trackerCredentialVaultFixture) GetOrCreateTrackerCredential(_ context.Context, credentialRef uuid.UUID) (TrackerCredential, error) {
	fixture.credentialRef = credentialRef
	return fixture.credential, fixture.err
}

type trackerCredentialProjectionFixture struct {
	userID        uuid.UUID
	credentialRef uuid.UUID
	credential    TrackerCredential
	boundAt       time.Time
	projection    TrackerCredentialProjection
	err           error
}

func (fixture *trackerCredentialProjectionFixture) BindTrackerCredential(_ context.Context, userID, credentialRef uuid.UUID, credential TrackerCredential, boundAt time.Time) (TrackerCredentialProjection, error) {
	fixture.userID = userID
	fixture.credentialRef = credentialRef
	fixture.credential = credential
	fixture.boundAt = boundAt
	return fixture.projection, fixture.err
}

func TestTrackerCredentialServiceReconcilesVaultProjection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 20, 0, 0, 0, time.UTC)
	user := User{ID: uuid.New(), CredentialRef: uuid.New()}
	credential := TrackerCredential{
		Passkey:   "0123456789abcdef0123456789abcdef",
		Version:   3,
		CreatedAt: now.Add(-time.Hour),
	}
	credential.LookupHMAC[0] = 0x7f
	vault := &trackerCredentialVaultFixture{credential: credential}
	repository := &trackerCredentialProjectionFixture{projection: TrackerCredentialProjection{
		UserID: user.ID, CredentialRef: user.CredentialRef,
		LookupHMAC: credential.LookupHMAC, VaultVersion: credential.Version,
		CreatedAt: credential.CreatedAt, UpdatedAt: now,
	}}
	service, err := NewTrackerCredentialService(vault, repository, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewTrackerCredentialService() error = %v", err)
	}

	result, err := service.ForUser(context.Background(), user)
	if err != nil {
		t.Fatalf("ForUser() error = %v", err)
	}
	if result.Passkey != credential.Passkey || vault.credentialRef != user.CredentialRef ||
		repository.userID != user.ID || repository.credentialRef != user.CredentialRef || repository.boundAt != now {
		t.Fatal("Tracker credential was not reconciled through the expected identity boundaries")
	}
}

func TestTrackerCredentialServiceRejectsSameVersionFork(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	user := User{ID: uuid.New(), CredentialRef: uuid.New()}
	credential := TrackerCredential{
		Passkey:   "0123456789abcdef0123456789abcdef",
		Version:   1,
		CreatedAt: now,
	}
	vault := &trackerCredentialVaultFixture{credential: credential}
	repository := &trackerCredentialProjectionFixture{projection: TrackerCredentialProjection{
		UserID: user.ID, CredentialRef: user.CredentialRef,
		VaultVersion: 1,
	}}
	repository.projection.LookupHMAC[0] = 1
	service, _ := NewTrackerCredentialService(vault, repository, func() time.Time { return now })
	if _, err := service.ForUser(context.Background(), user); err != ErrTrackerCredentialStateConflict {
		t.Fatalf("ForUser() error = %v, want state conflict", err)
	}
}
