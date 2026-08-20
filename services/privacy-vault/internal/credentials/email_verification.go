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
	emailVerificationTokenBytes = 32
	emailVerificationTTL        = 30 * time.Minute
	emailVerificationCooldown   = 2 * time.Minute
)

var (
	ErrEmailVerificationInput              = errors.New("email verification input is invalid")
	ErrEmailVerificationIdentifierMismatch = errors.New("email verification identifier does not match")
	ErrEmailVerificationCooldown           = errors.New("email verification request is cooling down")
	ErrEmailVerificationTokenInvalid       = errors.New("email verification token is invalid or expired")
	ErrEmailVerificationDelivery           = errors.New("email verification delivery is unavailable")
	ErrEmailVerificationStateConflict      = errors.New("email verification state is inconsistent")
)

// EmailVerificationCooldownError carries the next allowed issue time without
// leaking the email address or revealing whether an anonymous account exists.
type EmailVerificationCooldownError struct {
	NextRequestAt time.Time
}

func (err *EmailVerificationCooldownError) Error() string {
	return ErrEmailVerificationCooldown.Error()
}
func (err *EmailVerificationCooldownError) Unwrap() error { return ErrEmailVerificationCooldown }

type EmailVerificationReservation struct {
	VerificationID  uuid.UUID
	CredentialRef   uuid.UUID
	AlreadyVerified bool
	VerifiedAt      *time.Time
	NextRequestAt   time.Time
}

type ReserveEmailVerificationCommand struct {
	VerificationID uuid.UUID
	CredentialRef  uuid.UUID
	EmailLookup    []byte
	TokenDigest    []byte
	IssuedAt       time.Time
	ExpiresAt      time.Time
	NextRequestAt  time.Time
}

type EmailVerificationConfirmation struct {
	VerificationID uuid.UUID
	CredentialRef  uuid.UUID
	VerifiedAt     time.Time
}

// EmailVerificationRepository persists only keyed lookups and token digests.
// Raw email addresses and bearer tokens never cross this boundary.
type EmailVerificationRepository interface {
	ReserveEmailVerification(context.Context, ReserveEmailVerificationCommand) (EmailVerificationReservation, error)
	MarkEmailVerificationDelivered(context.Context, uuid.UUID, time.Time) error
	MarkEmailVerificationDeliveryFailed(context.Context, uuid.UUID) error
	ConfirmEmailVerification(context.Context, []byte, time.Time) (EmailVerificationConfirmation, error)
}

type EmailVerificationServiceConfig struct {
	PublicURL string
	Now       func() time.Time
	Random    io.Reader
	NewID     func() uuid.UUID
}

type EmailVerificationService struct {
	repository    EmailVerificationRepository
	sender        TransactionalEmailSender
	identifierKey []byte
	publicURL     url.URL
	now           func() time.Time
	random        io.Reader
	newID         func() uuid.UUID
}

type EmailVerificationRequestResult struct {
	VerificationID  uuid.UUID
	AcceptedAt      time.Time
	NextRequestAt   time.Time
	AlreadyVerified bool
	VerifiedAt      *time.Time
}

func NewEmailVerificationService(repository EmailVerificationRepository, identifierKey []byte, sender TransactionalEmailSender, config EmailVerificationServiceConfig) (*EmailVerificationService, error) {
	if repository == nil || sender == nil {
		return nil, errors.New("email verification repository and sender are required")
	}
	if len(identifierKey) < sha256.Size {
		return nil, errors.New("email verification identifier key must contain at least 32 bytes")
	}
	parsed, err := url.Parse(config.PublicURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("email verification public URL must be absolute without user info, query or fragment")
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
	return &EmailVerificationService{
		repository:    repository,
		sender:        sender,
		identifierKey: append([]byte(nil), identifierKey...),
		publicURL:     *parsed,
		now:           config.Now,
		random:        config.Random,
		newID:         config.NewID,
	}, nil
}

// Request creates at most one live link per credential and starts the cooldown
// before delivery. A mismatched re-entered email gets the same accepted result
// but no message, preventing this endpoint from becoming an identifier oracle.
func (service *EmailVerificationService) Request(ctx context.Context, credentialRef uuid.UUID, rawEmail string) (EmailVerificationRequestResult, error) {
	email, err := normalizeEmailAddress(rawEmail)
	if err != nil || credentialRef == uuid.Nil {
		return EmailVerificationRequestResult{}, ErrEmailVerificationInput
	}
	emailLookup, err := LookupHMAC(service.identifierKey, email)
	if err != nil {
		return EmailVerificationRequestResult{}, ErrEmailVerificationInput
	}
	rawToken := make([]byte, emailVerificationTokenBytes)
	if _, err := io.ReadFull(service.random, rawToken); err != nil {
		return EmailVerificationRequestResult{}, fmt.Errorf("generate email verification token: %w", err)
	}
	tokenDigest := sha256.Sum256(rawToken)
	token := base64.RawURLEncoding.EncodeToString(rawToken)
	now := service.now().UTC()
	verificationID := service.newID()
	if verificationID == uuid.Nil {
		return EmailVerificationRequestResult{}, errors.New("email verification id generator returned nil")
	}
	reservation, err := service.repository.ReserveEmailVerification(ctx, ReserveEmailVerificationCommand{
		VerificationID: verificationID,
		CredentialRef:  credentialRef,
		EmailLookup:    emailLookup,
		TokenDigest:    tokenDigest[:],
		IssuedAt:       now,
		ExpiresAt:      now.Add(emailVerificationTTL),
		NextRequestAt:  now.Add(emailVerificationCooldown),
	})
	if errors.Is(err, ErrEmailVerificationIdentifierMismatch) {
		return EmailVerificationRequestResult{AcceptedAt: now, NextRequestAt: now.Add(emailVerificationCooldown)}, nil
	}
	if err != nil {
		return EmailVerificationRequestResult{}, err
	}
	result := EmailVerificationRequestResult{
		VerificationID:  reservation.VerificationID,
		AcceptedAt:      now,
		NextRequestAt:   reservation.NextRequestAt,
		AlreadyVerified: reservation.AlreadyVerified,
		VerifiedAt:      reservation.VerifiedAt,
	}
	if reservation.AlreadyVerified {
		return result, nil
	}

	verificationURL := service.publicURL
	verificationURL.Fragment = url.Values{"token": {token}}.Encode()
	if err := service.sender.SendTransactionalEmail(ctx, TransactionalEmailMessage{
		Template:  EmailTemplateVerification,
		Recipient: email,
		ActionURL: verificationURL.String(),
		ExpiresAt: now.Add(emailVerificationTTL),
	}); err != nil {
		_ = service.repository.MarkEmailVerificationDeliveryFailed(ctx, verificationID)
		return EmailVerificationRequestResult{}, fmt.Errorf("%w: %v", ErrEmailVerificationDelivery, err)
	}
	if err := service.repository.MarkEmailVerificationDelivered(ctx, verificationID, service.now().UTC()); err != nil {
		return EmailVerificationRequestResult{}, fmt.Errorf("%w: record delivery: %v", ErrEmailVerificationDelivery, err)
	}
	return result, nil
}

// Confirm hashes the bearer token before persistence lookup. A previously
// consumed token remains idempotent so Core can recover if its own transaction
// failed after Vault had already verified the identifier.
func (service *EmailVerificationService) Confirm(ctx context.Context, token string) (EmailVerificationConfirmation, error) {
	rawToken, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(rawToken) != emailVerificationTokenBytes {
		return EmailVerificationConfirmation{}, ErrEmailVerificationInput
	}
	digest := sha256.Sum256(rawToken)
	return service.repository.ConfirmEmailVerification(ctx, digest[:], service.now().UTC())
}
