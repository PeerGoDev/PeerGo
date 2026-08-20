package seedingreward

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrWorkLease       = errors.New("seeding reward work lease is no longer owned")
	ErrBenefitNotFound = errors.New("seeding reward historical benefit was not found")
)

type PendingReward struct {
	WindowStart time.Time
	UserID      uuid.UUID
	LeaseToken  uuid.UUID
	Attempts    int32
}

type SettlementResult struct {
	WindowStart        time.Time
	UserID             uuid.UUID
	PolicyRevision     string
	Reward             int64
	ExperienceAmount   string
	MagicTransactionID uuid.UUID
	ExperienceEntryID  uuid.UUID
	CalculationSHA256  [32]byte
}

type SettlementRepository interface {
	Claim(context.Context, time.Time, int32, time.Duration) ([]PendingReward, error)
	Settle(context.Context, PendingReward, time.Time) (SettlementResult, error)
	Release(context.Context, PendingReward, time.Time, string, bool) error
}
