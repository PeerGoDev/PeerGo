package identity

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

type EmailVerificationService struct {
	sessions   EmailVerificationSessionAuthenticator
	vault      EmailVerificationVault
	repository EmailVerificationRepository
}

func NewEmailVerificationService(sessions EmailVerificationSessionAuthenticator, vault EmailVerificationVault, repository EmailVerificationRepository) (*EmailVerificationService, error) {
	if sessions == nil || vault == nil || repository == nil {
		return nil, errors.New("email verification service dependencies are required")
	}
	return &EmailVerificationService{sessions: sessions, vault: vault, repository: repository}, nil
}

// Request requires the current Web session and its CSRF value, but Core never
// stores the re-entered email. Vault returns an accepted response for a lookup
// mismatch so this self-service route cannot be repurposed for enumeration.
func (service *EmailVerificationService) Request(ctx context.Context, cookieToken, csrfToken, email string) (EmailVerificationDispatch, error) {
	email, err := normalizeEmailAddress(email)
	if err != nil {
		return EmailVerificationDispatch{}, ErrInvalidInput
	}
	session, err := service.sessions.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return EmailVerificationDispatch{}, err
	}
	if session.User.EmailVerifiedAt != nil {
		return EmailVerificationDispatch{
			AcceptedAt:      *session.User.EmailVerifiedAt,
			NextRequestAt:   *session.User.EmailVerifiedAt,
			AlreadyVerified: true,
		}, nil
	}
	dispatch, err := service.vault.RequestEmailVerification(ctx, session.User.CredentialRef, email)
	if err != nil {
		return EmailVerificationDispatch{}, err
	}
	if dispatch.AlreadyVerified {
		if dispatch.VerifiedAt == nil {
			return EmailVerificationDispatch{}, ErrEmailVerificationStateConflict
		}
		_, err := service.repository.CompleteEmailVerification(ctx, VaultEmailVerificationConfirmation{
			VerificationID: dispatch.VerificationID,
			CredentialRef:  session.User.CredentialRef,
			VerifiedAt:     *dispatch.VerifiedAt,
		})
		if err != nil {
			return EmailVerificationDispatch{}, err
		}
	}
	return EmailVerificationDispatch{
		AcceptedAt:      dispatch.AcceptedAt,
		NextRequestAt:   dispatch.NextRequestAt,
		AlreadyVerified: dispatch.AlreadyVerified,
	}, nil
}

// Confirm is anonymous because possession of the high-entropy single-purpose
// token is the proof. Vault commits first; the idempotent Core transaction can
// then be retried without issuing another token or duplicating audit evidence.
func (service *EmailVerificationService) Confirm(ctx context.Context, token string) (EmailVerificationCompletion, error) {
	if len(token) != 43 {
		return EmailVerificationCompletion{}, ErrInvalidInput
	}
	confirmation, err := service.vault.ConfirmEmailVerification(ctx, token)
	if err != nil {
		return EmailVerificationCompletion{}, err
	}
	if confirmation.VerificationID == uuid.Nil || confirmation.CredentialRef == uuid.Nil || confirmation.VerifiedAt.IsZero() {
		return EmailVerificationCompletion{}, ErrEmailVerificationStateConflict
	}
	return service.repository.CompleteEmailVerification(ctx, confirmation)
}
