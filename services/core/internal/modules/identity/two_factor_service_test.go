package identity

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type twoFactorVaultFixture struct {
	credentialRef uuid.UUID
	changeID      uuid.UUID
	password      string
	code          string
	rotation      TwoFactorVaultChange
	disabled      TwoFactorVaultChange
	rotateCalls   int
	disableCalls  int
}

func (*twoFactorVaultFixture) TwoFactorStatus(context.Context, uuid.UUID) (TwoFactorStatus, error) {
	return TwoFactorStatus{}, nil
}

func (*twoFactorVaultFixture) StartTOTPEnrollment(context.Context, uuid.UUID, string, string) (TOTPEnrollmentStart, error) {
	return TOTPEnrollmentStart{}, nil
}

func (*twoFactorVaultFixture) ConfirmTOTPEnrollment(context.Context, uuid.UUID, uuid.UUID, string) (TOTPEnrollmentConfirmation, error) {
	return TOTPEnrollmentConfirmation{}, nil
}

func (fixture *twoFactorVaultFixture) RotateTOTPRecoveryCodes(_ context.Context, credentialRef, changeID uuid.UUID, password, code string) (TwoFactorVaultChange, error) {
	fixture.rotateCalls++
	fixture.credentialRef, fixture.changeID = credentialRef, changeID
	fixture.password, fixture.code = password, code
	return fixture.rotation, nil
}

func (fixture *twoFactorVaultFixture) DisableTOTP(_ context.Context, credentialRef, changeID uuid.UUID, password, code string) (TwoFactorVaultChange, error) {
	fixture.disableCalls++
	fixture.credentialRef, fixture.changeID = credentialRef, changeID
	fixture.password, fixture.code = password, code
	return fixture.disabled, nil
}

type twoFactorChangeRepositoryFixture struct {
	command TwoFactorChangeCommand
	calls   int
}

func (fixture *twoFactorChangeRepositoryFixture) ApplyTwoFactorChange(_ context.Context, command TwoFactorChangeCommand) (TwoFactorChangeResult, error) {
	fixture.calls++
	fixture.command = command
	return TwoFactorChangeResult{}, nil
}

func TestTwoFactorRecoveryRotationPreservesIdempotencyAndAuthorizationEvidence(t *testing.T) {
	now := time.Date(2026, time.August, 7, 11, 0, 0, 0, time.UTC)
	userID, credentialRef, changeID := uuid.New(), uuid.New(), uuid.New()
	tokenHash := bytes.Repeat([]byte{0x31}, 32)
	session := WebSession{User: User{ID: userID, CredentialRef: credentialRef}, TokenHash: tokenHash}
	authenticator := &recordingSessionSecurityAuthenticator{session: session}
	decision := allowedSelfSessionDecision(now)
	authorizer := &recordingSessionSecurityAuthorizer{decision: decision}
	vault := &twoFactorVaultFixture{rotation: TwoFactorVaultChange{
		ChangeID: changeID, ChangedAt: now, RecoveryCodes: []string{"ABCD-EFGH-JKLM"},
	}}
	repository := &twoFactorChangeRepositoryFixture{}
	service, err := NewTwoFactorService(authenticator, vault, repository, authorizer, TwoFactorServiceConfig{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewTwoFactorService() error = %v", err)
	}

	result, err := service.RotateRecoveryCodes(context.Background(), "cookie", "csrf", TwoFactorReauthenticationCommand{
		ChangeID: changeID, Password: "current-password", SecondFactorCode: "123456",
	})
	if err != nil || result.ChangeID != changeID {
		t.Fatalf("RotateRecoveryCodes() result=%+v error=%v", result, err)
	}
	if vault.credentialRef != credentialRef || vault.changeID != changeID || vault.password != "current-password" || vault.code != "123456" {
		t.Fatalf("Vault input = %+v", vault)
	}
	command := repository.command
	if repository.calls != 1 || command.ID != changeID || command.UserID != userID || command.Kind != TwoFactorRecoveryCodesRotated || command.Authorization != decision || !bytes.Equal(command.CurrentTokenHash, tokenHash) {
		t.Fatalf("Core change command = %+v", command)
	}
	if authorizer.request.Action != authz.ActionAccountTotpManageSelf {
		t.Fatalf("authorization action = %q", authorizer.request.Action)
	}
}

func TestTwoFactorReauthenticationRequiresIdempotencyKeyBeforeAuthentication(t *testing.T) {
	authenticator := &recordingSessionSecurityAuthenticator{}
	vault := &twoFactorVaultFixture{}
	repository := &twoFactorChangeRepositoryFixture{}
	service, err := NewTwoFactorService(authenticator, vault, repository, &recordingSessionSecurityAuthorizer{}, TwoFactorServiceConfig{})
	if err != nil {
		t.Fatalf("NewTwoFactorService() error = %v", err)
	}

	_, err = service.Disable(context.Background(), "cookie", "csrf", TwoFactorReauthenticationCommand{
		Password: "current-password", SecondFactorCode: "123456",
	})
	if !errors.Is(err, ErrInvalidInput) || authenticator.writeCookie != "" || vault.disableCalls != 0 || repository.calls != 0 {
		t.Fatalf("Disable() error=%v authenticator=%+v vault=%+v repository=%+v", err, authenticator, vault, repository)
	}
}
