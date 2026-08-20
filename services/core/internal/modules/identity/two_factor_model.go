package identity

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type TwoFactorStatus struct {
	Enabled                bool
	EnabledAt              *time.Time
	RecoveryCodesRemaining int64
}

type TOTPEnrollmentStart struct {
	EnrollmentID    uuid.UUID
	Secret          string
	ProvisioningURI string
	ExpiresAt       time.Time
}

type TOTPEnrollmentConfirmation struct {
	ChangeID      uuid.UUID
	EnabledAt     time.Time
	RecoveryCodes []string
}

type TwoFactorVaultChange struct {
	ChangeID      uuid.UUID
	ChangedAt     time.Time
	RecoveryCodes []string
}

// TwoFactorVault is deliberately action-oriented. It exposes no seed read,
// recovery-code list or credential-material export operation.
type TwoFactorVault interface {
	TwoFactorStatus(context.Context, uuid.UUID) (TwoFactorStatus, error)
	StartTOTPEnrollment(context.Context, uuid.UUID, string, string) (TOTPEnrollmentStart, error)
	ConfirmTOTPEnrollment(context.Context, uuid.UUID, uuid.UUID, string) (TOTPEnrollmentConfirmation, error)
	RotateTOTPRecoveryCodes(context.Context, uuid.UUID, uuid.UUID, string, string) (TwoFactorVaultChange, error)
	DisableTOTP(context.Context, uuid.UUID, uuid.UUID, string, string) (TwoFactorVaultChange, error)
}

type TwoFactorChangeKind string

const (
	TwoFactorEnabled              TwoFactorChangeKind = "enabled"
	TwoFactorRecoveryCodesRotated TwoFactorChangeKind = "recovery_codes_rotated"
	TwoFactorDisabled             TwoFactorChangeKind = "disabled"
)

type TwoFactorChangeCommand struct {
	ID               uuid.UUID
	UserID           uuid.UUID
	CurrentTokenHash []byte
	Kind             TwoFactorChangeKind
	OccurredAt       time.Time
	Authorization    authz.Decision
}

type TwoFactorChangeResult struct {
	RevokedWebSessions   int64
	RevokedStaffSessions int64
}

type TwoFactorChangeAuditInput struct {
	Command TwoFactorChangeCommand
	Result  TwoFactorChangeResult
}

type TwoFactorChangeRepository interface {
	ApplyTwoFactorChange(context.Context, TwoFactorChangeCommand) (TwoFactorChangeResult, error)
}

type TwoFactorChangeEventBuilder interface {
	BuildTwoFactorChangeEvent(TwoFactorChangeAuditInput) (auditevent.Event, error)
}

type TOTPEnrollmentCommand struct {
	Password string
}

type TOTPEnrollmentConfirmationCommand struct {
	EnrollmentID uuid.UUID
	Code         string
}

type TwoFactorReauthenticationCommand struct {
	ChangeID         uuid.UUID
	Password         string
	SecondFactorCode string
}
