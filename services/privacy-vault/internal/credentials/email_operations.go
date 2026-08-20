package credentials

import (
	"context"
	"errors"
	"net/url"
	"time"
)

var (
	ErrEmailTestInput    = errors.New("email delivery test input is invalid")
	ErrEmailTestDelivery = errors.New("email delivery test failed")
)

type EmailOperationsRuntime struct {
	DeliveryMode               string
	EmailVerificationPublicURL string
	PasswordRecoveryPublicURL  string
}

type EmailOperationsStats struct {
	VerificationPending  int64 `json:"verification_pending"`
	VerificationSent     int64 `json:"verification_sent"`
	VerificationFailed   int64 `json:"verification_failed"`
	VerificationVerified int64 `json:"verification_verified"`
	RecoveryPending      int64 `json:"recovery_pending"`
	RecoverySent         int64 `json:"recovery_sent"`
	RecoveryFailed       int64 `json:"recovery_failed"`
	RecoveryCompleted    int64 `json:"recovery_completed"`
}

type EmailOperationsStatus struct {
	GeneratedAt                  time.Time
	DeliveryMode                 string
	VerificationPublicOrigin     string
	PasswordRecoveryPublicOrigin string
	VerificationTTLSeconds       int64
	PasswordRecoveryTTLSeconds   int64
	CooldownSeconds              int64
	Templates                    []string
	Stats                        EmailOperationsStats
}

type EmailOperationsRepository interface {
	EmailOperations(context.Context, time.Time) (EmailOperationsStats, error)
}

type EmailOperationsService struct {
	repository EmailOperationsRepository
	sender     TransactionalEmailSender
	runtime    EmailOperationsRuntime
	now        func() time.Time
}

func NewEmailOperationsService(repository EmailOperationsRepository, sender TransactionalEmailSender, runtime EmailOperationsRuntime, now func() time.Time) (*EmailOperationsService, error) {
	verificationURL, verificationErr := url.Parse(runtime.EmailVerificationPublicURL)
	recoveryURL, recoveryErr := url.Parse(runtime.PasswordRecoveryPublicURL)
	if repository == nil || sender == nil || (runtime.DeliveryMode != "development_outbox" && runtime.DeliveryMode != "https_relay") ||
		verificationErr != nil || recoveryErr != nil || verificationURL.Scheme == "" || verificationURL.Host == "" || recoveryURL.Scheme == "" || recoveryURL.Host == "" {
		return nil, errors.New("email operations dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &EmailOperationsService{repository: repository, sender: sender, runtime: runtime, now: now}, nil
}

// Status exposes only deployment-safe metadata and aggregate delivery state.
// Raw recipients, bearer links, local paths, relay URLs and tokens stay inside
// Privacy Vault and never cross into the Core administration API.
func (service *EmailOperationsService) Status(ctx context.Context) (EmailOperationsStatus, error) {
	now := service.now().UTC().Round(0)
	stats, err := service.repository.EmailOperations(ctx, now)
	if err != nil {
		return EmailOperationsStatus{}, err
	}
	verificationURL, _ := url.Parse(service.runtime.EmailVerificationPublicURL)
	recoveryURL, _ := url.Parse(service.runtime.PasswordRecoveryPublicURL)
	return EmailOperationsStatus{
		GeneratedAt:                  now,
		DeliveryMode:                 service.runtime.DeliveryMode,
		VerificationPublicOrigin:     verificationURL.Scheme + "://" + verificationURL.Host,
		PasswordRecoveryPublicOrigin: recoveryURL.Scheme + "://" + recoveryURL.Host,
		VerificationTTLSeconds:       int64(emailVerificationTTL / time.Second),
		PasswordRecoveryTTLSeconds:   int64(passwordRecoveryTTL / time.Second),
		CooldownSeconds:              int64(emailVerificationCooldown / time.Second),
		Templates:                    []string{string(EmailTemplateVerification), string(EmailTemplatePasswordRecovery)},
		Stats:                        stats,
	}, nil
}

// Test sends the dedicated no-link template through the exact delivery adapter
// used by verification and password recovery. The normalized recipient is
// intentionally neither stored nor returned to Core.
func (service *EmailOperationsService) Test(ctx context.Context, rawRecipient string) (time.Time, error) {
	recipient, err := normalizeEmailAddress(rawRecipient)
	if err != nil {
		return time.Time{}, ErrEmailTestInput
	}
	acceptedAt := service.now().UTC().Round(0)
	if err := service.sender.SendTransactionalEmail(ctx, TransactionalEmailMessage{
		Template:  EmailTemplateDeliveryTest,
		Recipient: recipient,
	}); err != nil {
		return time.Time{}, errors.Join(ErrEmailTestDelivery, err)
	}
	return acceptedAt, nil
}
