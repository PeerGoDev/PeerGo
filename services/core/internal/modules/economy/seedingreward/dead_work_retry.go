package seedingreward

import (
	"errors"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
)

var (
	ErrDeadWorkNotFound = errors.New("seeding reward dead work was not found")
	ErrDeadWorkConflict = errors.New("seeding reward dead work changed")
)

type DeadWorkRetryCommand struct {
	ID                uuid.UUID
	WindowStart       time.Time
	UserID            uuid.UUID
	ExpectedAttempts  int32
	ExpectedErrorCode string
	OperatorReference string
	Reason            string
	OccurredAt        time.Time
}

type DeadWorkRetryResult struct {
	RetryID           uuid.UUID
	WindowStart       time.Time
	UserID            uuid.UUID
	PreviousAttempts  int32
	PreviousErrorCode string
	RequeuedAt        time.Time
}

type DeadWorkRetryAuditInput struct {
	Command DeadWorkRetryCommand
	Result  DeadWorkRetryResult
}

type DeadWorkRetryEventBuilder interface {
	BuildSeedingRewardRetryEvent(DeadWorkRetryAuditInput) (auditevent.Event, error)
}

type TransactionEventAppenderFactory func(pgx.Tx) auditevent.Appender

func validDeadWorkRetryCommand(command DeadWorkRetryCommand) bool {
	command.WindowStart = canonicalTime(command.WindowStart)
	command.OccurredAt = canonicalTime(command.OccurredAt)
	return command.ID != uuid.Nil && command.UserID != uuid.Nil &&
		!command.WindowStart.IsZero() && command.WindowStart.Minute() == 0 &&
		command.WindowStart.Second() == 0 && command.WindowStart.Nanosecond() == 0 &&
		command.ExpectedAttempts > 0 && command.ExpectedAttempts <= 1_000_000 &&
		validDeadWorkErrorCode(command.ExpectedErrorCode) &&
		validOperatorText(command.OperatorReference, 200) &&
		validOperatorText(command.Reason, 1_000) && !command.OccurredAt.IsZero()
}

func validDeadWorkErrorCode(value string) bool {
	if len(value) < 1 || len(value) > 64 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, character := range value[1:] {
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '.' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func validOperatorText(value string, maximumBytes int) bool {
	if value == "" || len(value) > maximumBytes || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
