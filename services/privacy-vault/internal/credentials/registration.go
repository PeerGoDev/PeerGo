package credentials

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	registrationRequestDomain = "peergo:vault:registration-request:v1\x00"
	registrationProvisionTTL  = 24 * time.Hour
)

var registrationUsernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,31}$`)

var (
	ErrRegistrationInput               = errors.New("registration credential input is invalid")
	ErrIdentifierUnavailable           = errors.New("registration identifier is unavailable")
	ErrRegistrationIdempotencyConflict = errors.New("registration idempotency key was reused")
	ErrRegistrationProvisionNotFound   = errors.New("registration credential provision was not found")
)

// ProvisionRegistrationInput contains P0/P1 values only while the request is
// executing. Repository records receive keyed equality indexes, an Argon2id
// digest and masked display forms; they never receive the original password.
type ProvisionRegistrationInput struct {
	RegistrationID uuid.UUID
	Username       string
	Email          string
	Password       string
}

// RegistrationProvisionRecord is the bounded persistence command assembled
// by the Vault service after validation and one-way transformations.
type RegistrationProvisionRecord struct {
	RegistrationID uuid.UUID
	CredentialRef  uuid.UUID
	RequestHMAC    []byte
	PasswordHash   string
	UsernameLookup []byte
	UsernameMasked string
	EmailLookup    []byte
	EmailMasked    string
	EmailAddress   string
	CreatedAt      time.Time
	ExpiresAt      time.Time
}

// ProvisionRegistration creates or resumes an inert Vault credential. The
// same idempotency key may resume only the exact same sensitive request.
func (s *Service) ProvisionRegistration(ctx context.Context, input ProvisionRegistrationInput) (uuid.UUID, error) {
	username := strings.ToLower(strings.TrimSpace(input.Username))
	email, emailErr := normalizeEmailAddress(input.Email)
	if input.RegistrationID == uuid.Nil || !registrationUsernamePattern.MatchString(username) {
		return uuid.Nil, ErrRegistrationInput
	}
	if emailErr != nil {
		return uuid.Nil, ErrRegistrationInput
	}
	if len(input.Password) < 12 || len(input.Password) > maxPasswordBytes {
		return uuid.Nil, ErrRegistrationInput
	}

	usernameLookup, err := LookupHMAC(s.identifierKey, username)
	if err != nil {
		return uuid.Nil, ErrRegistrationInput
	}
	emailLookup, err := LookupHMAC(s.identifierKey, email)
	if err != nil {
		return uuid.Nil, ErrRegistrationInput
	}
	requestHMAC := registrationRequestHMAC(s.identifierKey, input.RegistrationID, username, email, input.Password)
	passwordHash, err := HashPassword(input.Password)
	if err != nil {
		return uuid.Nil, err
	}
	now := time.Now().UTC()
	return s.repository.ProvisionRegistration(ctx, RegistrationProvisionRecord{
		RegistrationID: input.RegistrationID,
		CredentialRef:  uuid.New(),
		RequestHMAC:    requestHMAC,
		PasswordHash:   passwordHash,
		UsernameLookup: usernameLookup,
		UsernameMasked: MaskUsername(username),
		EmailLookup:    emailLookup,
		EmailMasked:    MaskEmail(email),
		EmailAddress:   email,
		CreatedAt:      now,
		ExpiresAt:      now.Add(registrationProvisionTTL),
	})
}

func normalizeEmailAddress(value string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || utf8.RuneCountInString(email) > 254 {
		return "", ErrEmailVerificationInput
	}
	return email, nil
}

// ActivateRegistration makes an already-provisioned credential eligible for
// login. Core calls this only after its pending user projection is durable.
func (s *Service) ActivateRegistration(ctx context.Context, registrationID uuid.UUID) (uuid.UUID, error) {
	if registrationID == uuid.Nil {
		return uuid.Nil, ErrRegistrationInput
	}
	return s.repository.ActivateRegistration(ctx, registrationID, time.Now().UTC())
}

func registrationRequestHMAC(key []byte, registrationID uuid.UUID, username, email, password string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(registrationRequestDomain))
	_, _ = mac.Write(registrationID[:])
	for _, value := range []string{username, email, password} {
		_, _ = mac.Write([]byte{0})
		_, _ = mac.Write([]byte(value))
	}
	return mac.Sum(nil)
}
