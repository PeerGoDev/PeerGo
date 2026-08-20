package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type TwoFactorServiceConfig struct {
	Now func() time.Time
}

// TwoFactorService authenticates the current Web session and records the
// authorization decision before sending transient reauthentication material to
// Vault. It never persists or logs a password, seed or recovery code.
type TwoFactorService struct {
	authenticator WebSessionAuthenticator
	vault         TwoFactorVault
	repository    TwoFactorChangeRepository
	authorizer    authz.Authorizer
	now           func() time.Time
}

func NewTwoFactorService(authenticator WebSessionAuthenticator, vault TwoFactorVault, repository TwoFactorChangeRepository, authorizer authz.Authorizer, config TwoFactorServiceConfig) (*TwoFactorService, error) {
	if authenticator == nil || vault == nil || repository == nil || authorizer == nil {
		return nil, errors.New("two-factor service dependencies are required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &TwoFactorService{
		authenticator: authenticator, vault: vault, repository: repository,
		authorizer: authorizer, now: config.Now,
	}, nil
}

func (service *TwoFactorService) StartEnrollment(ctx context.Context, cookieToken, csrfToken string, command TOTPEnrollmentCommand) (TOTPEnrollmentStart, error) {
	if command.Password == "" || len(command.Password) > maxPasswordBytes {
		return TOTPEnrollmentStart{}, ErrInvalidInput
	}
	session, _, err := service.authenticateManagement(ctx, cookieToken, csrfToken)
	if err != nil {
		return TOTPEnrollmentStart{}, err
	}
	result, err := service.vault.StartTOTPEnrollment(ctx, session.User.CredentialRef, command.Password, session.User.Username)
	if err != nil {
		return TOTPEnrollmentStart{}, err
	}
	return result, nil
}

func (service *TwoFactorService) ConfirmEnrollment(ctx context.Context, cookieToken, csrfToken string, command TOTPEnrollmentConfirmationCommand) (TOTPEnrollmentConfirmation, error) {
	command.Code = strings.TrimSpace(command.Code)
	if command.EnrollmentID == uuid.Nil || !isSixDigits(command.Code) {
		return TOTPEnrollmentConfirmation{}, ErrInvalidInput
	}
	session, decision, err := service.authenticateManagement(ctx, cookieToken, csrfToken)
	if err != nil {
		return TOTPEnrollmentConfirmation{}, err
	}
	result, err := service.vault.ConfirmTOTPEnrollment(ctx, session.User.CredentialRef, command.EnrollmentID, command.Code)
	if err != nil {
		return TOTPEnrollmentConfirmation{}, err
	}
	if _, err := service.repository.ApplyTwoFactorChange(ctx, TwoFactorChangeCommand{
		ID: result.ChangeID, UserID: session.User.ID,
		CurrentTokenHash: append([]byte(nil), session.TokenHash...),
		Kind:             TwoFactorEnabled, OccurredAt: result.EnabledAt, Authorization: decision,
	}); err != nil {
		return TOTPEnrollmentConfirmation{}, fmt.Errorf("record enabled TOTP factor: %w", err)
	}
	return result, nil
}

func (service *TwoFactorService) RotateRecoveryCodes(ctx context.Context, cookieToken, csrfToken string, command TwoFactorReauthenticationCommand) (TwoFactorVaultChange, error) {
	if err := validateTwoFactorReauthentication(command); err != nil {
		return TwoFactorVaultChange{}, err
	}
	session, decision, err := service.authenticateManagement(ctx, cookieToken, csrfToken)
	if err != nil {
		return TwoFactorVaultChange{}, err
	}
	result, err := service.vault.RotateTOTPRecoveryCodes(ctx, session.User.CredentialRef, command.ChangeID, command.Password, command.SecondFactorCode)
	if err != nil {
		return TwoFactorVaultChange{}, err
	}
	if _, err := service.repository.ApplyTwoFactorChange(ctx, TwoFactorChangeCommand{
		ID: result.ChangeID, UserID: session.User.ID,
		CurrentTokenHash: append([]byte(nil), session.TokenHash...),
		Kind:             TwoFactorRecoveryCodesRotated, OccurredAt: result.ChangedAt, Authorization: decision,
	}); err != nil {
		return TwoFactorVaultChange{}, fmt.Errorf("record recovery code rotation: %w", err)
	}
	return result, nil
}

func (service *TwoFactorService) Disable(ctx context.Context, cookieToken, csrfToken string, command TwoFactorReauthenticationCommand) (TwoFactorVaultChange, error) {
	if err := validateTwoFactorReauthentication(command); err != nil {
		return TwoFactorVaultChange{}, err
	}
	session, decision, err := service.authenticateManagement(ctx, cookieToken, csrfToken)
	if err != nil {
		return TwoFactorVaultChange{}, err
	}
	result, err := service.vault.DisableTOTP(ctx, session.User.CredentialRef, command.ChangeID, command.Password, command.SecondFactorCode)
	if err != nil {
		return TwoFactorVaultChange{}, err
	}
	if _, err := service.repository.ApplyTwoFactorChange(ctx, TwoFactorChangeCommand{
		ID: result.ChangeID, UserID: session.User.ID,
		CurrentTokenHash: append([]byte(nil), session.TokenHash...),
		Kind:             TwoFactorDisabled, OccurredAt: result.ChangedAt, Authorization: decision,
	}); err != nil {
		return TwoFactorVaultChange{}, fmt.Errorf("record disabled TOTP factor: %w", err)
	}
	return result, nil
}

func (service *TwoFactorService) authenticateManagement(ctx context.Context, cookieToken, csrfToken string) (WebSession, authz.Decision, error) {
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return WebSession{}, authz.Decision{}, err
	}
	decision, err := authz.AuthorizeWebSelfAction(
		ctx, service.authorizer, session.User.ID, authz.ActionAccountTotpManageSelf, service.now().UTC(),
	)
	if err != nil {
		return WebSession{}, decision, err
	}
	return session, decision, nil
}

func validateTwoFactorReauthentication(command TwoFactorReauthenticationCommand) error {
	command.SecondFactorCode = strings.TrimSpace(command.SecondFactorCode)
	if command.ChangeID == uuid.Nil || command.Password == "" || len(command.Password) > maxPasswordBytes ||
		command.SecondFactorCode == "" || len(command.SecondFactorCode) > 32 {
		return ErrInvalidInput
	}
	return nil
}
