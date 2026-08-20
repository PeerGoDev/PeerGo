package vaultoperations

import (
	"context"
	"errors"
	"time"
)

var (
	ErrEmailTestInvalidRecipient = errors.New("email delivery test recipient is invalid")
	ErrEmailTestUnavailable      = errors.New("email delivery test is unavailable")
)

type EmailStats struct {
	VerificationPending  int64 `json:"verification_pending"`
	VerificationSent     int64 `json:"verification_sent"`
	VerificationFailed   int64 `json:"verification_failed"`
	VerificationVerified int64 `json:"verification_verified"`
	RecoveryPending      int64 `json:"recovery_pending"`
	RecoverySent         int64 `json:"recovery_sent"`
	RecoveryFailed       int64 `json:"recovery_failed"`
	RecoveryCompleted    int64 `json:"recovery_completed"`
}

type EmailStatus struct {
	GeneratedAt                  time.Time  `json:"generated_at"`
	DeliveryMode                 string     `json:"delivery_mode"`
	VerificationPublicOrigin     string     `json:"verification_public_origin"`
	PasswordRecoveryPublicOrigin string     `json:"password_recovery_public_origin"`
	VerificationTTLSeconds       int64      `json:"verification_ttl_seconds"`
	PasswordRecoveryTTLSeconds   int64      `json:"password_recovery_ttl_seconds"`
	CooldownSeconds              int64      `json:"cooldown_seconds"`
	Templates                    []string   `json:"templates"`
	Stats                        EmailStats `json:"stats"`
}

type EmailTestResult struct {
	AcceptedAt time.Time `json:"accepted_at"`
	Template   string    `json:"template"`
}

type EmailOperationsClient interface {
	EmailOperations(context.Context) (EmailStatus, error)
	TestEmail(context.Context, string) (EmailTestResult, error)
}
