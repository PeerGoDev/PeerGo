package torrents

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	DefaultTorrentWithdrawalLimit = 20
	MaxTorrentWithdrawalLimit     = 50
)

var (
	ErrTorrentWithdrawalInput                = errors.New("torrent withdrawal input is invalid")
	ErrTorrentWithdrawalNotFound             = errors.New("torrent withdrawal request was not found")
	ErrTorrentWithdrawalEmailUnverified      = errors.New("torrent withdrawal requires a verified email")
	ErrTorrentWithdrawalSelfReview           = errors.New("torrent withdrawal cannot be self-reviewed")
	ErrTorrentWithdrawalStateConflict        = errors.New("torrent cannot perform the withdrawal transition")
	ErrTorrentWithdrawalVersionConflict      = errors.New("torrent withdrawal version changed")
	ErrTorrentWithdrawalPending              = errors.New("torrent already has a pending withdrawal")
	ErrTorrentWithdrawalContentChangePending = errors.New("torrent has a pending content change")
	ErrTorrentWithdrawalActivePurchases      = errors.New("torrent withdrawal has active purchase entitlements")
	ErrTorrentWithdrawalCategoryUnavailable  = errors.New("torrent withdrawal cannot restore to a disabled category")
	ErrTorrentWithdrawalObjectUnavailable    = errors.New("torrent withdrawal cannot restore without a verified object")
	ErrTorrentWithdrawalIdempotencyConflict  = errors.New("torrent withdrawal idempotency key was reused")
)

type TorrentWithdrawalStatus string

const (
	TorrentWithdrawalPending  TorrentWithdrawalStatus = "pending"
	TorrentWithdrawalApproved TorrentWithdrawalStatus = "approved"
	TorrentWithdrawalRejected TorrentWithdrawalStatus = "rejected"
)

type TorrentWithdrawalDecision string

const (
	TorrentWithdrawalApprove TorrentWithdrawalDecision = "approve"
	TorrentWithdrawalReject  TorrentWithdrawalDecision = "reject"
)

type SubmitTorrentWithdrawalInput struct {
	RequestID       uuid.UUID
	TorrentID       TorrentID
	ExpectedVersion int64
	Reason          string
}

type SubmitTorrentWithdrawalCommand struct {
	SubmitTorrentWithdrawalInput
	UploaderID    uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

// TorrentWithdrawalRequest is the immutable member-visible request evidence.
// It records the version that was disabled at submission; later decisions do
// not rewrite that historical transition.
type TorrentWithdrawalRequest struct {
	ID                     uuid.UUID
	TorrentID              TorrentID
	TorrentTitle           string
	Reason                 string
	ExpectedTorrentVersion int64
	DisabledTorrentVersion int64
	Status                 TorrentWithdrawalStatus
	Version                int64
	CreatedAt              time.Time
	DecidedAt              *time.Time
}

type TorrentWithdrawalQuery struct {
	Status TorrentWithdrawalStatus
	Limit  int
	Offset int
}

type ManagedTorrentWithdrawalRequest struct {
	TorrentWithdrawalRequest
	UploaderNumericID   int64
	UploaderUsername    string
	UploaderDisplayName string
	ActivePurchaseCount int64
}

type ManagedTorrentWithdrawalPage struct {
	Items  []ManagedTorrentWithdrawalRequest
	Total  int64
	Limit  int
	Offset int
}

type DecideTorrentWithdrawalInput struct {
	DecisionID             uuid.UUID
	RequestID              uuid.UUID
	ExpectedRequestVersion int64
	Decision               TorrentWithdrawalDecision
	Reason                 string
}

type DecideTorrentWithdrawalCommand struct {
	DecideTorrentWithdrawalInput
	ReviewerID    uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type TorrentWithdrawalDecisionResult struct {
	DecisionID     uuid.UUID
	RequestID      uuid.UUID
	TorrentID      TorrentID
	Decision       TorrentWithdrawalDecision
	RequestStatus  TorrentWithdrawalStatus
	RequestVersion int64
	TorrentState   State
	TorrentVersion int64
	DecidedAt      time.Time
}

func (service *TorrentMaintenanceService) SubmitTorrentWithdrawal(ctx context.Context, cookieToken, csrfToken string, input SubmitTorrentWithdrawalInput) (TorrentWithdrawalRequest, error) {
	normalized, err := normalizeSubmitTorrentWithdrawalInput(input)
	if err != nil {
		return TorrentWithdrawalRequest{}, err
	}
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return TorrentWithdrawalRequest{}, err
	}
	if session.User.EmailVerifiedAt == nil {
		return TorrentWithdrawalRequest{}, ErrTorrentWithdrawalEmailUnverified
	}
	now := service.now().UTC().Round(0)
	decision, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionTorrentWithdrawRequestSelf, now)
	if err != nil {
		return TorrentWithdrawalRequest{}, err
	}
	return service.repository.SubmitTorrentWithdrawal(ctx, SubmitTorrentWithdrawalCommand{
		SubmitTorrentWithdrawalInput: normalized,
		UploaderID:                   session.User.ID,
		OccurredAt:                   now,
		Authorization:                decision,
	})
}

func (service *TorrentMaintenanceService) ListTorrentWithdrawals(ctx context.Context, actor authz.StaffActor, query TorrentWithdrawalQuery) (ManagedTorrentWithdrawalPage, error) {
	normalized, err := normalizeTorrentWithdrawalQuery(query)
	if err != nil {
		return ManagedTorrentWithdrawalPage{}, err
	}
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTorrentWithdrawReview, authz.SiteScope(), service.now().UTC(), "torrent-withdrawal"); err != nil {
		return ManagedTorrentWithdrawalPage{}, err
	}
	return service.repository.ListTorrentWithdrawals(ctx, normalized)
}

func (service *TorrentMaintenanceService) DecideTorrentWithdrawal(ctx context.Context, actor authz.StaffActor, input DecideTorrentWithdrawalInput) (TorrentWithdrawalDecisionResult, error) {
	normalized, err := normalizeDecideTorrentWithdrawalInput(input)
	if err != nil {
		return TorrentWithdrawalDecisionResult{}, err
	}
	now := service.now().UTC().Round(0)
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTorrentWithdrawReview, authz.SiteScope(), now, "torrent-withdrawal")
	if err != nil {
		return TorrentWithdrawalDecisionResult{}, err
	}
	return service.repository.DecideTorrentWithdrawal(ctx, DecideTorrentWithdrawalCommand{
		DecideTorrentWithdrawalInput: normalized,
		ReviewerID:                   actor.Subject.ID,
		OccurredAt:                   now,
		Authorization:                decision,
	})
}

func normalizeSubmitTorrentWithdrawalInput(input SubmitTorrentWithdrawalInput) (SubmitTorrentWithdrawalInput, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if input.RequestID == uuid.Nil || input.TorrentID < 1 || input.ExpectedVersion < 1 ||
		!validWithdrawalReason(input.Reason) {
		return SubmitTorrentWithdrawalInput{}, ErrTorrentWithdrawalInput
	}
	return input, nil
}

func normalizeTorrentWithdrawalQuery(query TorrentWithdrawalQuery) (TorrentWithdrawalQuery, error) {
	if query.Limit < 1 || query.Limit > MaxTorrentWithdrawalLimit || query.Offset < 0 || query.Offset > MaxManagedTorrentOffset {
		return TorrentWithdrawalQuery{}, ErrTorrentWithdrawalInput
	}
	switch query.Status {
	case "", TorrentWithdrawalPending, TorrentWithdrawalApproved, TorrentWithdrawalRejected:
		return query, nil
	default:
		return TorrentWithdrawalQuery{}, ErrTorrentWithdrawalInput
	}
}

func normalizeDecideTorrentWithdrawalInput(input DecideTorrentWithdrawalInput) (DecideTorrentWithdrawalInput, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if input.DecisionID == uuid.Nil || input.RequestID == uuid.Nil || input.ExpectedRequestVersion != 1 ||
		!validWithdrawalReason(input.Reason) ||
		(input.Decision != TorrentWithdrawalApprove && input.Decision != TorrentWithdrawalReject) {
		return DecideTorrentWithdrawalInput{}, ErrTorrentWithdrawalInput
	}
	return input, nil
}

func validWithdrawalReason(reason string) bool {
	count := utf8.RuneCountInString(reason)
	return utf8.ValidString(reason) && count >= 10 && count <= 1000
}
