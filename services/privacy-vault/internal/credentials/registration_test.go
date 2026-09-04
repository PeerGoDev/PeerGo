package credentials

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

type registrationRepositoryRecorder struct {
	record        RegistrationProvisionRecord
	credentialRef uuid.UUID
	activatedID   uuid.UUID
}

func (repository *registrationRepositoryRecorder) CredentialByLookupHMAC(context.Context, []byte) (Credential, error) {
	return Credential{}, ErrInvalidCredentials
}

func (repository *registrationRepositoryRecorder) CredentialByLookupHMACForAccountAppeal(context.Context, []byte) (Credential, error) {
	return Credential{}, ErrInvalidCredentials
}

func (repository *registrationRepositoryRecorder) EnableCredentialAfterAccountAppeal(context.Context, uuid.UUID, time.Time) error {
	return nil
}

func (repository *registrationRepositoryRecorder) LoginBlockedUntil(context.Context, []byte, time.Time) (time.Time, error) {
	return time.Time{}, nil
}

func (repository *registrationRepositoryRecorder) RecordLoginFailure(context.Context, []byte, time.Time) error {
	return nil
}

func (repository *registrationRepositoryRecorder) ClearLoginFailures(context.Context, []byte) error {
	return nil
}

func (repository *registrationRepositoryRecorder) RehashPasswordIfCurrent(context.Context, uuid.UUID, string, string, time.Time) (bool, error) {
	return true, nil
}

func (repository *registrationRepositoryRecorder) ProvisionRegistration(_ context.Context, record RegistrationProvisionRecord) (uuid.UUID, error) {
	repository.record = record
	return repository.credentialRef, nil
}

func (repository *registrationRepositoryRecorder) ActivateRegistration(_ context.Context, registrationID uuid.UUID, _ time.Time) (uuid.UUID, error) {
	repository.activatedID = registrationID
	return repository.credentialRef, nil
}

func (repository *registrationRepositoryRecorder) IdentifierExists(context.Context, []byte) (bool, error) {
	return false, nil
}

func TestProvisionRegistrationTransformsSecretsBeforePersistence(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	registrationID := uuid.MustParse("0198f20a-6da8-7e51-9c64-303030303030")
	credentialRef := uuid.MustParse("0198f20a-6da8-7e51-9c64-404040404040")
	repository := &registrationRepositoryRecorder{credentialRef: credentialRef}
	service, err := NewService(repository, noSecondFactorVerifier{}, key)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	result, err := service.ProvisionRegistration(context.Background(), ProvisionRegistrationInput{
		RegistrationID: registrationID,
		Username:       "New_Member",
		Email:          "Member@Example.COM",
		Password:       "PeerGo-member-2026!",
	})
	if err != nil {
		t.Fatalf("ProvisionRegistration() error = %v", err)
	}
	if result != credentialRef || repository.record.RegistrationID != registrationID {
		t.Fatalf("provision result=%s record=%+v", result, repository.record)
	}
	if repository.record.PasswordHash == "PeerGo-member-2026!" || repository.record.PasswordHash == "" {
		t.Fatal("repository received an empty or plaintext password hash")
	}
	if len(repository.record.UsernameLookup) != 32 || len(repository.record.EmailLookup) != 32 || len(repository.record.RequestHMAC) != 32 {
		t.Fatalf("lookup sizes = username %d email %d request %d", len(repository.record.UsernameLookup), len(repository.record.EmailLookup), len(repository.record.RequestHMAC))
	}
	if repository.record.UsernameMasked != "n***" || repository.record.EmailMasked != "m***@example.com" {
		t.Fatalf("masked identifiers = %q %q", repository.record.UsernameMasked, repository.record.EmailMasked)
	}
	if repository.record.EmailAddress != "member@example.com" {
		t.Fatalf("email address = %q", repository.record.EmailAddress)
	}
	if repository.record.CreatedAt.Nanosecond()%int(time.Microsecond) != 0 {
		t.Fatalf("created at precision = %s, want microseconds", repository.record.CreatedAt.Format(time.RFC3339Nano))
	}

	activated, err := service.ActivateRegistration(context.Background(), registrationID)
	if err != nil || activated != credentialRef || repository.activatedID != registrationID {
		t.Fatalf("ActivateRegistration() = %s, %v", activated, err)
	}
}

func TestRegistrationRequestHMACBindsPasswordAndIdentifiers(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	id := uuid.New()
	first := registrationRequestHMAC(key, id, "member", "member@example.com", "first-password")
	second := registrationRequestHMAC(key, id, "member", "member@example.com", "second-password")
	if string(first) == string(second) {
		t.Fatal("request HMAC did not bind the password")
	}
}
