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

type passwordRecoveryRepositoryRecorder struct {
	command       ReservePasswordRecoveryCommand
	reservation   PasswordRecoveryReservation
	reserveErr    error
	deliveredID   uuid.UUID
	failedID      uuid.UUID
	inspection    PasswordRecoveryInspection
	inspectDigest []byte
	inspectErr    error
	complete      CompletePasswordRecoveryCommand
	confirmation  PasswordRecoveryConfirmation
	completeCalls int
}

func (repository *passwordRecoveryRepositoryRecorder) ReservePasswordRecovery(_ context.Context, command ReservePasswordRecoveryCommand) (PasswordRecoveryReservation, error) {
	repository.command = command
	return repository.reservation, repository.reserveErr
}

func (repository *passwordRecoveryRepositoryRecorder) MarkPasswordRecoveryDelivered(_ context.Context, id uuid.UUID, _ time.Time) error {
	repository.deliveredID = id
	return nil
}

func (repository *passwordRecoveryRepositoryRecorder) MarkPasswordRecoveryDeliveryFailed(_ context.Context, id uuid.UUID) error {
	repository.failedID = id
	return nil
}

func (repository *passwordRecoveryRepositoryRecorder) InspectPasswordRecovery(_ context.Context, digest []byte, _ time.Time) (PasswordRecoveryInspection, error) {
	repository.inspectDigest = append([]byte(nil), digest...)
	return repository.inspection, repository.inspectErr
}

func (repository *passwordRecoveryRepositoryRecorder) CompletePasswordRecovery(_ context.Context, command CompletePasswordRecoveryCommand) (PasswordRecoveryConfirmation, error) {
	repository.completeCalls++
	repository.complete = command
	return repository.confirmation, nil
}

func TestPasswordRecoveryRequestStoresOnlyDigestsAndUsesSharedActionDelivery(t *testing.T) {
	now := time.Date(2026, time.August, 6, 18, 0, 0, 0, time.UTC)
	recoveryID := uuid.New()
	repository := &passwordRecoveryRepositoryRecorder{reservation: PasswordRecoveryReservation{
		RecoveryID: recoveryID, Issue: true, AcceptedAt: now, NextIssueAt: now.Add(passwordRecoveryCooldown),
	}}
	sender := &emailVerificationSenderRecorder{}
	randomBytes := bytes.Repeat([]byte{0x6b}, passwordRecoveryTokenBytes)
	service, err := NewPasswordRecoveryService(repository, []byte("0123456789abcdef0123456789abcdef"), sender, PasswordRecoveryServiceConfig{
		PublicURL: "https://peergo.test/reset-password", Now: func() time.Time { return now },
		Random: bytes.NewReader(randomBytes), NewID: func() uuid.UUID { return recoveryID },
	})
	if err != nil {
		t.Fatalf("NewPasswordRecoveryService() error = %v", err)
	}
	result, err := service.Request(context.Background(), " Member@Example.COM ")
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	if result.AcceptedAt != now || result.NextRequestAt != now.Add(passwordRecoveryCooldown) || sender.calls != 1 {
		t.Fatalf("result=%+v sender=%+v", result, sender)
	}
	if sender.message.Template != EmailTemplatePasswordRecovery || sender.message.Recipient != "member@example.com" {
		t.Fatalf("message=%+v", sender.message)
	}
	if bytes.Contains(repository.command.EmailLookup, []byte("member@example.com")) || bytes.Equal(repository.command.TokenDigest, randomBytes) {
		t.Fatal("repository command contained raw email or token material")
	}
	parsed, err := url.Parse(sender.message.ActionURL)
	if err != nil {
		t.Fatalf("Parse(action URL) error = %v", err)
	}
	wantToken := base64.RawURLEncoding.EncodeToString(randomBytes)
	if parsed.RawQuery != "" || parsed.Fragment != "token="+wantToken {
		t.Fatalf("action URL = %s", parsed.String())
	}
}

func TestPasswordRecoveryRequestKeepsUnknownAddressEnumerationSafe(t *testing.T) {
	now := time.Date(2026, time.August, 6, 18, 0, 0, 0, time.UTC)
	repository := &passwordRecoveryRepositoryRecorder{reservation: PasswordRecoveryReservation{Issue: false}}
	sender := &emailVerificationSenderRecorder{}
	service, err := NewPasswordRecoveryService(repository, []byte("0123456789abcdef0123456789abcdef"), sender, PasswordRecoveryServiceConfig{
		PublicURL: "https://peergo.test/reset-password", Now: func() time.Time { return now },
		Random: bytes.NewReader(bytes.Repeat([]byte{1}, passwordRecoveryTokenBytes)),
	})
	if err != nil {
		t.Fatalf("NewPasswordRecoveryService() error = %v", err)
	}
	result, err := service.Request(context.Background(), "unknown@example.com")
	if err != nil || sender.calls != 0 || result.AcceptedAt != now || result.NextRequestAt != now.Add(passwordRecoveryCooldown) {
		t.Fatalf("result=%+v error=%v sender calls=%d", result, err, sender.calls)
	}
}

func TestPasswordRecoveryConfirmInspectsBeforeHashAndKeepsConsumedTokenIdempotent(t *testing.T) {
	rawToken := bytes.Repeat([]byte{0x31}, passwordRecoveryTokenBytes)
	token := base64.RawURLEncoding.EncodeToString(rawToken)
	wantDigest := sha256.Sum256(rawToken)
	completedAt := time.Date(2026, time.August, 6, 19, 0, 0, 0, time.UTC)
	repository := &passwordRecoveryRepositoryRecorder{inspection: PasswordRecoveryInspection{
		RecoveryID: uuid.New(), CredentialRef: uuid.New(), AlreadyCompleted: true, PasswordChangedAt: completedAt,
	}}
	service, err := NewPasswordRecoveryService(repository, []byte("0123456789abcdef0123456789abcdef"), &emailVerificationSenderRecorder{}, PasswordRecoveryServiceConfig{
		PublicURL: "https://peergo.test/reset-password",
	})
	if err != nil {
		t.Fatalf("NewPasswordRecoveryService() error = %v", err)
	}
	result, err := service.Confirm(context.Background(), token, "PeerGo-new-password-2026!")
	if err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if !bytes.Equal(repository.inspectDigest, wantDigest[:]) || repository.completeCalls != 0 || result.PasswordChangedAt != completedAt {
		t.Fatalf("result=%+v inspect=%x complete calls=%d", result, repository.inspectDigest, repository.completeCalls)
	}

	repository.inspectErr = ErrPasswordRecoveryTokenInvalid
	_, err = service.Confirm(context.Background(), base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, passwordRecoveryTokenBytes)), "PeerGo-new-password-2026!")
	if !errors.Is(err, ErrPasswordRecoveryTokenInvalid) || repository.completeCalls != 0 {
		t.Fatalf("invalid Confirm() error=%v complete calls=%d", err, repository.completeCalls)
	}
}

func TestPasswordRecoveryConfirmPassesOnlyArgon2HashToRepository(t *testing.T) {
	rawToken := bytes.Repeat([]byte{0x44}, passwordRecoveryTokenBytes)
	repository := &passwordRecoveryRepositoryRecorder{
		inspection:   PasswordRecoveryInspection{RecoveryID: uuid.New(), CredentialRef: uuid.New()},
		confirmation: PasswordRecoveryConfirmation{RecoveryID: uuid.New(), CredentialRef: uuid.New(), PasswordChangedAt: time.Now().UTC()},
	}
	service, err := NewPasswordRecoveryService(repository, []byte("0123456789abcdef0123456789abcdef"), &emailVerificationSenderRecorder{}, PasswordRecoveryServiceConfig{
		PublicURL: "https://peergo.test/reset-password",
	})
	if err != nil {
		t.Fatalf("NewPasswordRecoveryService() error = %v", err)
	}
	password := "PeerGo-new-password-2026!"
	if _, err := service.Confirm(context.Background(), base64.RawURLEncoding.EncodeToString(rawToken), password); err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if repository.completeCalls != 1 || repository.complete.PasswordHash == password {
		t.Fatalf("complete command leaked raw password: %+v", repository.complete)
	}
	match, _, err := VerifyPassword(repository.complete.PasswordHash, password)
	if err != nil || !match {
		t.Fatalf("stored hash did not verify: match=%v error=%v", match, err)
	}
}
