package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type passwordRecoveryVaultFixture struct {
	email        string
	token        string
	newPassword  string
	dispatch     PasswordRecoveryDispatch
	confirmation VaultPasswordRecoveryConfirmation
	err          error
}

func (fixture *passwordRecoveryVaultFixture) RequestPasswordRecovery(_ context.Context, email string) (PasswordRecoveryDispatch, error) {
	fixture.email = email
	return fixture.dispatch, fixture.err
}

func (fixture *passwordRecoveryVaultFixture) ConfirmPasswordRecovery(_ context.Context, token, newPassword string) (VaultPasswordRecoveryConfirmation, error) {
	fixture.token = token
	fixture.newPassword = newPassword
	return fixture.confirmation, fixture.err
}

type passwordRecoveryRepositoryFixture struct {
	confirmation VaultPasswordRecoveryConfirmation
	completion   PasswordRecoveryCompletion
	calls        int
}

func (fixture *passwordRecoveryRepositoryFixture) CompletePasswordRecovery(_ context.Context, confirmation VaultPasswordRecoveryConfirmation) (PasswordRecoveryCompletion, error) {
	fixture.calls++
	fixture.confirmation = confirmation
	return fixture.completion, nil
}

func TestPasswordRecoveryRequestNormalizesTransientEmail(t *testing.T) {
	now := time.Date(2026, time.August, 6, 21, 0, 0, 0, time.UTC)
	vault := &passwordRecoveryVaultFixture{dispatch: PasswordRecoveryDispatch{
		AcceptedAt: now, NextRequestAt: now.Add(2 * time.Minute),
	}}
	service, err := NewPasswordRecoveryService(vault, &passwordRecoveryRepositoryFixture{})
	if err != nil {
		t.Fatalf("NewPasswordRecoveryService() error = %v", err)
	}
	result, err := service.Request(context.Background(), " Member@Example.COM ")
	if err != nil || vault.email != "member@example.com" || result.AcceptedAt != now {
		t.Fatalf("Request() result=%+v error=%v vault=%+v", result, err, vault)
	}
}

func TestPasswordRecoveryConfirmFinishesCoreAfterVault(t *testing.T) {
	confirmation := VaultPasswordRecoveryConfirmation{
		RecoveryID: uuid.New(), CredentialRef: uuid.New(), PasswordChangedAt: time.Now().UTC(),
	}
	vault := &passwordRecoveryVaultFixture{confirmation: confirmation}
	repository := &passwordRecoveryRepositoryFixture{completion: PasswordRecoveryCompletion{
		RecoveryID: confirmation.RecoveryID, PasswordChangedAt: confirmation.PasswordChangedAt, Changed: true,
	}}
	service, err := NewPasswordRecoveryService(vault, repository)
	if err != nil {
		t.Fatalf("NewPasswordRecoveryService() error = %v", err)
	}
	token := "0123456789abcdef0123456789abcdef0123456789A"
	password := "PeerGo-new-password-2026!"
	result, err := service.Confirm(context.Background(), token, password)
	if err != nil || repository.calls != 1 || repository.confirmation != confirmation || vault.newPassword != password || !result.Changed {
		t.Fatalf("Confirm() result=%+v error=%v vault=%+v repository=%+v", result, err, vault, repository)
	}
}

func TestPasswordRecoveryConfirmRejectsInvalidInputBeforeVault(t *testing.T) {
	vault := &passwordRecoveryVaultFixture{}
	service, err := NewPasswordRecoveryService(vault, &passwordRecoveryRepositoryFixture{})
	if err != nil {
		t.Fatalf("NewPasswordRecoveryService() error = %v", err)
	}
	_, err = service.Confirm(context.Background(), "short", "too-short")
	if !errors.Is(err, ErrInvalidInput) || vault.token != "" {
		t.Fatalf("Confirm() error=%v vault=%+v", err, vault)
	}
}
