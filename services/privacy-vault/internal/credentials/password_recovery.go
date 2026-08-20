package credentials

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/google/uuid"
)

const (
	passwordRecoveryTokenBytes = 32
	passwordRecoveryTTL        = 30 * time.Minute
	passwordRecoveryCooldown   = 2 * time.Minute
)

var (
	ErrPasswordRecoveryInput         = errors.New("password recovery input is invalid")
	ErrPasswordRecoveryTokenInvalid  = errors.New("password recovery token is invalid or expired")
	ErrPasswordRecoveryDelivery      = errors.New("password recovery delivery is unavailable")
	ErrPasswordRecoveryStateConflict = errors.New("password recovery state is inconsistent")
)

type PasswordRecoveryReservation struct {
	RecoveryID  uuid.UUID
	Issue       bool
	AcceptedAt  time.Time
	NextIssueAt time.Time
}

type ReservePasswordRecoveryCommand struct {
	RecoveryID  uuid.UUID
	EmailLookup []byte
	TokenDigest []byte
	IssuedAt    time.Time
	ExpiresAt   time.Time
	NextIssueAt time.Time
}

type PasswordRecoveryDispatch struct {
	AcceptedAt    time.Time
	NextRequestAt time.Time
}

type PasswordRecoveryInspection struct {
	RecoveryID        uuid.UUID
	CredentialRef     uuid.UUID
	AlreadyCompleted  bool
	PasswordChangedAt time.Time
}

type CompletePasswordRecoveryCommand struct {
	TokenDigest       []byte
	PasswordHash      string
	PasswordChangedAt time.Time
}

type PasswordRecoveryConfirmation struct {
	RecoveryID        uuid.UUID
	CredentialRef     uuid.UUID
	PasswordChangedAt time.Time
}

type PasswordRecoveryRepository interface {
	ReservePasswordRecovery(context.Context, ReservePasswordRecoveryCommand) (PasswordRecoveryReservation, error)
	MarkPasswordRecoveryDelivered(context.Context, uuid.UUID, time.Time) error
	MarkPasswordRecoveryDeliveryFailed(context.Context, uuid.UUID) error
	InspectPasswordRecovery(context.Context, []byte, time.Time) (PasswordRecoveryInspection, error)
	CompletePasswordRecovery(context.Context, CompletePasswordRecoveryCommand) (PasswordRecoveryConfirmation, error)
}

// PasswordRecoveryDeliveryError carries the enumeration-safe result. The
// internal HTTP adapter logs the relay failure but still returns the same
// accepted response that an unknown email receives.
type PasswordRecoveryDeliveryError struct {
	Dispatch PasswordRecoveryDispatch
	Cause    error
}

func (err *PasswordRecoveryDeliveryError) Error() string {
	return fmt.Sprintf("%s: %v", ErrPasswordRecoveryDelivery, err.Cause)
}

func (err *PasswordRecoveryDeliveryError) Unwrap() error { return ErrPasswordRecoveryDelivery }

type PasswordRecoveryServiceConfig struct {
	PublicURL string
	Now       func() time.Time
	Random    io.Reader
	NewID     func() uuid.UUID
}

type PasswordRecoveryService struct {
	repository    PasswordRecoveryRepository
	sender        TransactionalEmailSender
	identifierKey []byte
	publicURL     url.URL
	now           func() time.Time
	random        io.Reader
	newID         func() uuid.UUID
}

func NewPasswordRecoveryService(repository PasswordRecoveryRepository, identifierKey []byte, sender TransactionalEmailSender, config PasswordRecoveryServiceConfig) (*PasswordRecoveryService, error) {
	if repository == nil || sender == nil {
		return nil, errors.New("password recovery repository and sender are required")
	}
	if len(identifierKey) < sha256.Size {
		return nil, errors.New("password recovery identifier key must contain at least 32 bytes")
	}
	parsed, err := url.Parse(config.PublicURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("password recovery public URL must be absolute without user info, query or fragment")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.NewID == nil {
		config.NewID = uuid.New
	}
	return &PasswordRecoveryService{
		repository: repository, sender: sender,
		identifierKey: append([]byte(nil), identifierKey...), publicURL: *parsed,
		now: config.Now, random: config.Random, newID: config.NewID,
	}, nil
}

// Request never exposes whether the normalized email resolved to a verified
// identifier. Vault applies the per-credential cooldown and performs delivery;
// Core receives only the fixed accepted/next-request projection.
func (service *PasswordRecoveryService) Request(ctx context.Context, rawEmail string) (PasswordRecoveryDispatch, error) {
	email, err := normalizeEmailAddress(rawEmail)
	if err != nil {
		return PasswordRecoveryDispatch{}, ErrPasswordRecoveryInput
	}
	emailLookup, err := LookupHMAC(service.identifierKey, email)
	if err != nil {
		return PasswordRecoveryDispatch{}, ErrPasswordRecoveryInput
	}
	rawToken := make([]byte, passwordRecoveryTokenBytes)
	if _, err := io.ReadFull(service.random, rawToken); err != nil {
		return PasswordRecoveryDispatch{}, fmt.Errorf("generate password recovery token: %w", err)
	}
	digest := sha256.Sum256(rawToken)
	now := service.now().UTC()
	recoveryID := service.newID()
	if recoveryID == uuid.Nil {
		return PasswordRecoveryDispatch{}, errors.New("password recovery id generator returned nil")
	}
	reservation, err := service.repository.ReservePasswordRecovery(ctx, ReservePasswordRecoveryCommand{
		RecoveryID: recoveryID, EmailLookup: emailLookup, TokenDigest: digest[:],
		IssuedAt: now, ExpiresAt: now.Add(passwordRecoveryTTL), NextIssueAt: now.Add(passwordRecoveryCooldown),
	})
	if err != nil {
		return PasswordRecoveryDispatch{}, err
	}
	// The public timing hint is intentionally fixed even when an existing
	// credential is still cooling down; otherwise repeated probes could turn a
	// longer internal state into an email-enumeration oracle.
	dispatch := PasswordRecoveryDispatch{AcceptedAt: now, NextRequestAt: now.Add(passwordRecoveryCooldown)}
	if !reservation.Issue {
		return dispatch, nil
	}
	actionURL := service.publicURL
	actionURL.Fragment = url.Values{"token": {base64.RawURLEncoding.EncodeToString(rawToken)}}.Encode()
	if err := service.sender.SendTransactionalEmail(ctx, TransactionalEmailMessage{
		Template: EmailTemplatePasswordRecovery, Recipient: email,
		ActionURL: actionURL.String(), ExpiresAt: now.Add(passwordRecoveryTTL),
	}); err != nil {
		_ = service.repository.MarkPasswordRecoveryDeliveryFailed(ctx, recoveryID)
		return dispatch, &PasswordRecoveryDeliveryError{Dispatch: dispatch, Cause: err}
	}
	if err := service.repository.MarkPasswordRecoveryDelivered(ctx, recoveryID, service.now().UTC()); err != nil {
		return PasswordRecoveryDispatch{}, fmt.Errorf("%w: record delivery: %v", ErrPasswordRecoveryDelivery, err)
	}
	return dispatch, nil
}

// Confirm performs a cheap digest lookup before Argon2id work. The repository
// validates the token again under lock, making supersession/consumption races
// fail closed while random 43-character input cannot force unbounded hashing.
func (service *PasswordRecoveryService) Confirm(ctx context.Context, token, newPassword string) (PasswordRecoveryConfirmation, error) {
	rawToken, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(rawToken) != passwordRecoveryTokenBytes || len(newPassword) < 12 || len(newPassword) > maxPasswordBytes {
		return PasswordRecoveryConfirmation{}, ErrPasswordRecoveryInput
	}
	digest := sha256.Sum256(rawToken)
	now := service.now().UTC()
	inspection, err := service.repository.InspectPasswordRecovery(ctx, digest[:], now)
	if err != nil {
		return PasswordRecoveryConfirmation{}, err
	}
	if inspection.AlreadyCompleted {
		return PasswordRecoveryConfirmation{
			RecoveryID: inspection.RecoveryID, CredentialRef: inspection.CredentialRef,
			PasswordChangedAt: inspection.PasswordChangedAt,
		}, nil
	}
	passwordHash, err := HashPassword(newPassword)
	if err != nil {
		return PasswordRecoveryConfirmation{}, fmt.Errorf("hash recovered password: %w", err)
	}
	return service.repository.CompletePasswordRecovery(ctx, CompletePasswordRecoveryCommand{
		TokenDigest: digest[:], PasswordHash: passwordHash, PasswordChangedAt: now,
	})
}
