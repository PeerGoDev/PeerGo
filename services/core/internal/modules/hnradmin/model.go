// Package hnradmin owns Core's staff-facing H&R control plane. It stores
// administrator intent and delivery evidence; Settlement remains the only
// service allowed to apply an H&R policy to Tracker completion facts.
package hnradmin

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/contracts/go/hnrpolicyv1"
	"github.com/peergo/peergo/contracts/go/settlementoperationsv1"
	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	DefaultRuleID    = "global-default"
	DefaultListLimit = 20
	MaxListLimit     = 100
)

var (
	ErrInput               = errors.New("H&R policy administration input is invalid")
	ErrConflict            = errors.New("H&R policy timeline conflicts with existing history")
	ErrIdempotencyConflict = errors.New("H&R policy revision id was reused")
	ErrNoChange            = errors.New("H&R policy did not change")
	ErrInvariant           = errors.New("H&R policy administration invariant failed")
)

type DeliveryState string

const (
	DeliveryPending   DeliveryState = "pending"
	DeliveryRetrying  DeliveryState = "retrying"
	DeliveryDelivered DeliveryState = "delivered"
)

type TimelineState string

const (
	TimelineScheduled TimelineState = "scheduled"
	TimelineActive    TimelineState = "active"
)

type Revision struct {
	ID                uuid.UUID
	Policy            hnrpolicyv1.Policy
	EffectiveAt       time.Time
	Reason            string
	ActorID           uuid.UUID
	CreatedAt         time.Time
	DeliveryState     DeliveryState
	DeliveryAttempts  int32
	LastDeliveryError string
	DeliveredAt       *time.Time
	TimelineState     TimelineState
	CommandSHA256     [32]byte
	Authorization     authz.Decision
	AuthorizationID   uuid.UUID
	Replayed          bool
}

type PolicyInput struct {
	Mode                     hnrpolicyv1.Mode
	RequiredSeedSeconds      int64
	RequiredRatioBasisPoints int64
	AssessmentWindowSeconds  int64
	GracePeriodSeconds       int64
	MaxIntervalCreditSeconds int64
}

type IssueInput struct {
	RevisionID  uuid.UUID
	Policy      PolicyInput
	EffectiveAt time.Time
	Reason      string
}

type IssueCommand struct {
	IssueInput
	BaseRuleVersion int64
	CurrentPolicy   *PolicyInput
	ActorID         uuid.UUID
	OccurredAt      time.Time
	Authorization   authz.Decision
}

type Page struct {
	Items                []Revision
	Total                int64
	Limit                int
	Offset               int
	MinimumEffectiveFrom time.Time
	Current              settlementoperationsv1.HNRPolicy
	GlobalRatioConnected bool
}

type Preview struct {
	Policy                    PolicyInput
	CompletionAt              time.Time
	AssessmentDueAt           time.Time
	GraceEndsAt               time.Time
	ContinuousSeedSatisfiedAt *time.Time
}

type RevisionAuditInput struct {
	Revision      Revision
	Authorization authz.Decision
}

type RevisionEventBuilder interface {
	BuildHNRPolicyRevisionEvent(RevisionAuditInput) (auditevent.Event, error)
}

type Repository interface {
	List(context.Context, int, int, time.Time) (Page, error)
	Issue(context.Context, IssueCommand) (Revision, error)
}
