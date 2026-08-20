package credentials

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
)

type emailVerificationRepositoryRecorder struct {
	command      ReserveEmailVerificationCommand
	reservation  EmailVerificationReservation
	reserveErr   error
	deliveredID  uuid.UUID
	deliveryTime time.Time
	failedID     uuid.UUID
	confirmHash  []byte
	confirmation EmailVerificationConfirmation
}

func (repository *emailVerificationRepositoryRecorder) ReserveEmailVerification(_ context.Context, command ReserveEmailVerificationCommand) (EmailVerificationReservation, error) {
	repository.command = command
	return repository.reservation, repository.reserveErr
}

func (repository *emailVerificationRepositoryRecorder) MarkEmailVerificationDelivered(_ context.Context, id uuid.UUID, deliveredAt time.Time) error {
	repository.deliveredID = id
	repository.deliveryTime = deliveredAt
	return nil
}

func (repository *emailVerificationRepositoryRecorder) MarkEmailVerificationDeliveryFailed(_ context.Context, id uuid.UUID) error {
	repository.failedID = id
	return nil
}

func (repository *emailVerificationRepositoryRecorder) ConfirmEmailVerification(_ context.Context, digest []byte, _ time.Time) (EmailVerificationConfirmation, error) {
	repository.confirmHash = append([]byte(nil), digest...)
	return repository.confirmation, nil
}

type emailVerificationSenderRecorder struct {
	message TransactionalEmailMessage
	calls   int
	err     error
}

func (sender *emailVerificationSenderRecorder) SendTransactionalEmail(_ context.Context, message TransactionalEmailMessage) error {
	sender.calls++
	sender.message = message
	return sender.err
}

func TestEmailVerificationRequestPersistsOnlyDigestsAndDeliversFragmentLink(t *testing.T) {
	now := time.Date(2026, time.August, 6, 13, 0, 0, 0, time.UTC)
	verificationID := uuid.MustParse("0198f20a-6da8-7e51-9c64-616161616161")
	credentialRef := uuid.MustParse("0198f20a-6da8-7e51-9c64-626262626262")
	repository := &emailVerificationRepositoryRecorder{reservation: EmailVerificationReservation{
		VerificationID: verificationID,
		CredentialRef:  credentialRef,
		NextRequestAt:  now.Add(emailVerificationCooldown),
	}}
	sender := &emailVerificationSenderRecorder{}
	randomBytes := bytes.Repeat([]byte{0x5a}, emailVerificationTokenBytes)
	service, err := NewEmailVerificationService(
		repository,
		[]byte("0123456789abcdef0123456789abcdef"),
		sender,
		EmailVerificationServiceConfig{
			PublicURL: "https://peergo.test/verify-email",
			Now:       func() time.Time { return now },
			Random:    bytes.NewReader(randomBytes),
			NewID:     func() uuid.UUID { return verificationID },
		},
	)
	if err != nil {
		t.Fatalf("NewEmailVerificationService() error = %v", err)
	}
	result, err := service.Request(context.Background(), credentialRef, " Member@Example.COM ")
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	if result.VerificationID != verificationID || sender.calls != 1 {
		t.Fatalf("result=%+v sender=%+v", result, sender)
	}
	if repository.deliveredID != verificationID || repository.deliveryTime.IsZero() {
		t.Fatalf("delivery marker = %s at %s", repository.deliveredID, repository.deliveryTime)
	}
	if sender.message.Recipient != "member@example.com" {
		t.Fatalf("recipient = %q", sender.message.Recipient)
	}
	if len(repository.command.EmailLookup) != sha256.Size || len(repository.command.TokenDigest) != sha256.Size {
		t.Fatalf("stored lookup sizes = email %d token %d", len(repository.command.EmailLookup), len(repository.command.TokenDigest))
	}
	if bytes.Contains(repository.command.EmailLookup, []byte("member@example.com")) || bytes.Contains(repository.command.TokenDigest, randomBytes) {
		t.Fatal("repository command contained a raw address or token")
	}
	if sender.message.Template != EmailTemplateVerification {
		t.Fatalf("message template = %q", sender.message.Template)
	}
	verificationURL, err := url.Parse(sender.message.ActionURL)
	if err != nil {
		t.Fatalf("Parse(verification URL) error = %v", err)
	}
	wantToken := base64.RawURLEncoding.EncodeToString(randomBytes)
	if verificationURL.RawQuery != "" || verificationURL.Fragment != "token="+wantToken {
		t.Fatalf("verification URL = %s", verificationURL.String())
	}
}

func TestEmailVerificationRequestKeepsIdentifierMismatchEnumerationSafe(t *testing.T) {
	now := time.Date(2026, time.August, 6, 13, 0, 0, 0, time.UTC)
	repository := &emailVerificationRepositoryRecorder{reserveErr: ErrEmailVerificationIdentifierMismatch}
	sender := &emailVerificationSenderRecorder{}
	service, err := NewEmailVerificationService(
		repository,
		[]byte("0123456789abcdef0123456789abcdef"),
		sender,
		EmailVerificationServiceConfig{
			PublicURL: "https://peergo.test/verify-email",
			Now:       func() time.Time { return now },
			Random:    bytes.NewReader(bytes.Repeat([]byte{1}, emailVerificationTokenBytes)),
		},
	)
	if err != nil {
		t.Fatalf("NewEmailVerificationService() error = %v", err)
	}
	result, err := service.Request(context.Background(), uuid.New(), "other@example.com")
	if err != nil || result.AcceptedAt.IsZero() || result.NextRequestAt.IsZero() || sender.calls != 0 {
		t.Fatalf("Request() result=%+v error=%v sender calls=%d", result, err, sender.calls)
	}
}

func TestEmailVerificationConfirmHashesTokenBeforeRepository(t *testing.T) {
	raw := bytes.Repeat([]byte{0x33}, emailVerificationTokenBytes)
	token := base64.RawURLEncoding.EncodeToString(raw)
	wantDigest := sha256.Sum256(raw)
	repository := &emailVerificationRepositoryRecorder{confirmation: EmailVerificationConfirmation{
		VerificationID: uuid.New(), CredentialRef: uuid.New(), VerifiedAt: time.Now().UTC(),
	}}
	service, err := NewEmailVerificationService(
		repository,
		[]byte("0123456789abcdef0123456789abcdef"),
		&emailVerificationSenderRecorder{},
		EmailVerificationServiceConfig{PublicURL: "https://peergo.test/verify-email"},
	)
	if err != nil {
		t.Fatalf("NewEmailVerificationService() error = %v", err)
	}
	if _, err := service.Confirm(context.Background(), token); err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if !bytes.Equal(repository.confirmHash, wantDigest[:]) || bytes.Equal(repository.confirmHash, raw) {
		t.Fatal("repository did not receive only the token digest")
	}
}

func TestEmailVerificationRequestMarksDeliveryFailure(t *testing.T) {
	verificationID := uuid.New()
	repository := &emailVerificationRepositoryRecorder{reservation: EmailVerificationReservation{
		VerificationID: verificationID, CredentialRef: uuid.New(), NextRequestAt: time.Now().Add(time.Minute),
	}}
	sender := &emailVerificationSenderRecorder{err: errors.New("relay unavailable")}
	service, err := NewEmailVerificationService(
		repository,
		[]byte("0123456789abcdef0123456789abcdef"),
		sender,
		EmailVerificationServiceConfig{
			PublicURL: "https://peergo.test/verify-email",
			Random:    bytes.NewReader(bytes.Repeat([]byte{2}, emailVerificationTokenBytes)),
			NewID:     func() uuid.UUID { return verificationID },
		},
	)
	if err != nil {
		t.Fatalf("NewEmailVerificationService() error = %v", err)
	}
	_, err = service.Request(context.Background(), repository.reservation.CredentialRef, "member@example.com")
	if !errors.Is(err, ErrEmailVerificationDelivery) || repository.failedID != verificationID {
		t.Fatalf("Request() error=%v failedID=%s", err, repository.failedID)
	}
}
