package credentials

import (
	"context"
	"testing"
	"time"
)

type emailOperationsRepositoryStub struct {
	at time.Time
}

type emailOperationsSenderStub struct {
	message TransactionalEmailMessage
	err     error
}

func (stub *emailOperationsSenderStub) SendTransactionalEmail(_ context.Context, message TransactionalEmailMessage) error {
	stub.message = message
	return stub.err
}

func (stub *emailOperationsRepositoryStub) EmailOperations(_ context.Context, at time.Time) (EmailOperationsStats, error) {
	stub.at = at
	return EmailOperationsStats{VerificationSent: 12, RecoveryCompleted: 3}, nil
}

func TestEmailOperationsStatusExcludesDeliverySecrets(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 16, 9, 0, 0, 0, time.FixedZone("test", 8*60*60))
	repository := &emailOperationsRepositoryStub{}
	service, err := NewEmailOperationsService(repository, &emailOperationsSenderStub{}, EmailOperationsRuntime{
		DeliveryMode:               "development_outbox",
		EmailVerificationPublicURL: "http://localhost:5174/verify-email",
		PasswordRecoveryPublicURL:  "http://localhost:5174/reset-password",
	}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.DeliveryMode != "development_outbox" || status.VerificationPublicOrigin != "http://localhost:5174" ||
		status.PasswordRecoveryPublicOrigin != "http://localhost:5174" || status.VerificationTTLSeconds != 1800 ||
		status.CooldownSeconds != 120 || status.Stats.VerificationSent != 12 || repository.at != now.UTC() {
		t.Fatalf("status = %+v, repository at = %s", status, repository.at)
	}
}

func TestEmailOperationsTestNormalizesAndDoesNotCreateBearerLink(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 19, 9, 0, 0, 0, time.FixedZone("test", 8*60*60))
	sender := &emailOperationsSenderStub{}
	service, err := NewEmailOperationsService(&emailOperationsRepositoryStub{}, sender, EmailOperationsRuntime{
		DeliveryMode:               "https_relay",
		EmailVerificationPublicURL: "https://peergo.example/verify-email",
		PasswordRecoveryPublicURL:  "https://peergo.example/reset-password",
	}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	acceptedAt, err := service.Test(context.Background(), "  ADMIN@Example.COM ")
	if err != nil {
		t.Fatal(err)
	}
	if acceptedAt != now.UTC() || sender.message.Template != EmailTemplateDeliveryTest ||
		sender.message.Recipient != "admin@example.com" || sender.message.ActionURL != "" || !sender.message.ExpiresAt.IsZero() {
		t.Fatalf("accepted at = %s, message = %+v", acceptedAt, sender.message)
	}
}
