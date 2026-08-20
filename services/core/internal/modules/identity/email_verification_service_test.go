package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type emailVerificationSessionFixture struct {
	session WebSession
	err     error
	cookie  string
	csrf    string
}

func (fixture *emailVerificationSessionFixture) AuthenticateWrite(_ context.Context, cookie, csrf string) (WebSession, error) {
	fixture.cookie = cookie
	fixture.csrf = csrf
	return fixture.session, fixture.err
}

type coreEmailVerificationVaultFixture struct {
	credentialRef uuid.UUID
	email         string
	dispatch      VaultEmailVerificationDispatch
	confirmation  VaultEmailVerificationConfirmation
	requestCalls  int
}

func (fixture *coreEmailVerificationVaultFixture) RequestEmailVerification(_ context.Context, credentialRef uuid.UUID, email string) (VaultEmailVerificationDispatch, error) {
	fixture.requestCalls++
	fixture.credentialRef = credentialRef
	fixture.email = email
	return fixture.dispatch, nil
}

func (fixture *coreEmailVerificationVaultFixture) ConfirmEmailVerification(context.Context, string) (VaultEmailVerificationConfirmation, error) {
	return fixture.confirmation, nil
}

type coreEmailVerificationRepositoryFixture struct {
	confirmation VaultEmailVerificationConfirmation
	completion   EmailVerificationCompletion
	calls        int
}

func (fixture *coreEmailVerificationRepositoryFixture) CompleteEmailVerification(_ context.Context, confirmation VaultEmailVerificationConfirmation) (EmailVerificationCompletion, error) {
	fixture.calls++
	fixture.confirmation = confirmation
	return fixture.completion, nil
}

func TestEmailVerificationRequestAuthenticatesSessionAndKeepsEmailOutOfCoreRepository(t *testing.T) {
	now := time.Date(2026, time.August, 6, 14, 0, 0, 0, time.UTC)
	credentialRef := uuid.New()
	sessions := &emailVerificationSessionFixture{session: WebSession{User: User{
		ID: uuid.New(), CredentialRef: credentialRef, Username: "member", DisplayName: "成员",
	}}}
	vault := &coreEmailVerificationVaultFixture{dispatch: VaultEmailVerificationDispatch{
		VerificationID: uuid.New(), AcceptedAt: now, NextRequestAt: now.Add(2 * time.Minute),
	}}
	repository := &coreEmailVerificationRepositoryFixture{}
	service, err := NewEmailVerificationService(sessions, vault, repository)
	if err != nil {
		t.Fatalf("NewEmailVerificationService() error = %v", err)
	}
	result, err := service.Request(context.Background(), "cookie", "csrf", " Member@example.com ")
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	if sessions.cookie != "cookie" || sessions.csrf != "csrf" || vault.credentialRef != credentialRef || vault.email != "member@example.com" {
		t.Fatalf("session=%+v vault=%+v", sessions, vault)
	}
	if repository.calls != 0 || result.AlreadyVerified {
		t.Fatalf("repository calls=%d result=%+v", repository.calls, result)
	}
}

func TestEmailVerificationRequestStopsBeforeVaultWhenSessionFails(t *testing.T) {
	sessions := &emailVerificationSessionFixture{err: ErrInvalidCSRF}
	vault := &coreEmailVerificationVaultFixture{}
	service, err := NewEmailVerificationService(sessions, vault, &coreEmailVerificationRepositoryFixture{})
	if err != nil {
		t.Fatalf("NewEmailVerificationService() error = %v", err)
	}
	_, err = service.Request(context.Background(), "cookie", "bad", "member@example.com")
	if !errors.Is(err, ErrInvalidCSRF) || vault.requestCalls != 0 {
		t.Fatalf("Request() error=%v vault calls=%d", err, vault.requestCalls)
	}
}

func TestEmailVerificationConfirmCompletesCoreProjection(t *testing.T) {
	confirmation := VaultEmailVerificationConfirmation{
		VerificationID: uuid.New(), CredentialRef: uuid.New(), VerifiedAt: time.Now().UTC(),
	}
	completion := EmailVerificationCompletion{VerificationID: confirmation.VerificationID, Changed: true, VerifiedAt: confirmation.VerifiedAt}
	repository := &coreEmailVerificationRepositoryFixture{completion: completion}
	service, err := NewEmailVerificationService(
		&emailVerificationSessionFixture{},
		&coreEmailVerificationVaultFixture{confirmation: confirmation},
		repository,
	)
	if err != nil {
		t.Fatalf("NewEmailVerificationService() error = %v", err)
	}
	result, err := service.Confirm(context.Background(), "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	if err != nil || repository.confirmation != confirmation || result.VerificationID != confirmation.VerificationID {
		t.Fatalf("Confirm() result=%+v error=%v repository=%+v", result, err, repository.confirmation)
	}
}
