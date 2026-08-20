package identity

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

type PasswordRecoveryService struct {
	vault      PasswordRecoveryVault
	repository PasswordRecoveryRepository
}

func NewPasswordRecoveryService(vault PasswordRecoveryVault, repository PasswordRecoveryRepository) (*PasswordRecoveryService, error) {
	if vault == nil || repository == nil {
		return nil, errors.New("password recovery service dependencies are required")
	}
	return &PasswordRecoveryService{vault: vault, repository: repository}, nil
}

// Request validates only syntax before forwarding the transient address. Vault
// owns lookup, cooldown and delivery, while Core receives no match indicator.
func (service *PasswordRecoveryService) Request(ctx context.Context, email string) (PasswordRecoveryDispatch, error) {
	email, err := normalizeEmailAddress(email)
	if err != nil {
		return PasswordRecoveryDispatch{}, ErrInvalidInput
	}
	return service.vault.RequestPasswordRecovery(ctx, email)
}

// Confirm lets Vault commit the password first. The Core half is idempotent and
// can be retried with the consumed token to finish session revocation plus the
// audit outbox transaction after a cross-service timeout.
func (service *PasswordRecoveryService) Confirm(ctx context.Context, token, newPassword string) (PasswordRecoveryCompletion, error) {
	if len(token) != 43 || len(newPassword) < 12 || len(newPassword) > maxPasswordBytes {
		return PasswordRecoveryCompletion{}, ErrInvalidInput
	}
	confirmation, err := service.vault.ConfirmPasswordRecovery(ctx, token, newPassword)
	if err != nil {
		return PasswordRecoveryCompletion{}, err
	}
	if confirmation.RecoveryID == uuid.Nil || confirmation.CredentialRef == uuid.Nil || confirmation.PasswordChangedAt.IsZero() {
		return PasswordRecoveryCompletion{}, ErrPasswordRecoveryStateConflict
	}
	return service.repository.CompletePasswordRecovery(ctx, confirmation)
}

var _ interface {
	Request(context.Context, string) (PasswordRecoveryDispatch, error)
	Confirm(context.Context, string, string) (PasswordRecoveryCompletion, error)
} = (*PasswordRecoveryService)(nil)
