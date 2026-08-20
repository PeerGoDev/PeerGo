// Package membergift owns member-to-member integer magic transfers. It keeps
// the product policy and immutable receipt next to the economy ledger workflow
// without exposing generic account-to-account transfers to other modules.
package membergift

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInput                = errors.New("member gift input is invalid")
	ErrPolicyNotFound       = errors.New("member gift policy was not found")
	ErrPolicyConflict       = errors.New("member gift policy conflicts with existing history")
	ErrDisabled             = errors.New("member gifts are disabled")
	ErrAmountOutOfRange     = errors.New("member gift amount is outside policy limits")
	ErrDailyLimit           = errors.New("member gift daily limit would be exceeded")
	ErrSelf                 = errors.New("member cannot gift to self")
	ErrSenderIneligible     = errors.New("member gift sender is not eligible")
	ErrRecipientUnavailable = errors.New("member gift recipient is unavailable")
	ErrInsufficientBalance  = errors.New("member gift balance is insufficient")
	ErrIdempotencyConflict  = errors.New("member gift idempotency key was reused")
	ErrInvariant            = errors.New("member gift invariant failed")
)

const (
	DefaultHistoryLimit = 30
	DefaultPolicyLimit  = 30
	MaximumHistoryLimit = 100
	MaximumPolicyLimit  = 100
)

type PolicyRevision struct {
	Revision        string
	Enabled         bool
	MinimumAmount   int64
	MaximumAmount   int64
	DailyGrossLimit int64
	FeeBPS          int32
	SnapshotSHA256  [32]byte
	CreatedAt       time.Time
}

type PublishedPolicy struct {
	Policy                  PolicyRevision
	IssuedBy                *uuid.UUID
	AuthorizationDecisionID *uuid.UUID
	Reason                  string
	Replayed                bool
}

type PolicyPage struct {
	Items  []PublishedPolicy
	Total  int64
	Limit  int
	Offset int
}

type Counterparty struct {
	NumericID   int64
	Username    string
	DisplayName string
}

type Direction string

const (
	DirectionSent     Direction = "sent"
	DirectionReceived Direction = "received"
)

type Gift struct {
	ID                 uuid.UUID
	RequestID          uuid.UUID
	SenderUserID       uuid.UUID
	RecipientUserID    uuid.UUID
	Direction          Direction
	Counterparty       Counterparty
	GrossAmount        int64
	FeeAmount          int64
	NetAmount          int64
	Message            string
	PolicyRevision     string
	MagicTransactionID uuid.UUID
	OccurredAt         time.Time
	RecordedAt         time.Time
	Replayed           bool
}

type Overview struct {
	Policy         PublishedPolicy
	MyNumericID    int64
	OutgoingToday  int64
	RemainingToday int64
	History        []Gift
}

type CreateCommand struct {
	RequestID          uuid.UUID
	SenderUserID       uuid.UUID
	RecipientNumericID int64
	Amount             int64
	Message            string
	Now                time.Time
	DayStartsAt        time.Time
	DayEndsAt          time.Time
}

type PublishCommand struct {
	Policy                  PolicyRevision
	IssuedBy                uuid.UUID
	AuthorizationDecisionID uuid.UUID
	Reason                  string
	SnapshotJSON            []byte
}

type Repository interface {
	Overview(context.Context, uuid.UUID, time.Time, time.Time, int) (Overview, error)
	Create(context.Context, CreateCommand) (Gift, error)
	ListPolicies(context.Context, int, int) ([]PublishedPolicy, int64, error)
	LatestPolicy(context.Context) (PublishedPolicy, error)
	PublishPolicy(context.Context, PublishCommand) (PublishedPolicy, error)
}
