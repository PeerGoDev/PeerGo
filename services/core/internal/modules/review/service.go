package review

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/workgroups"
)

const (
	maxPendingReviewLimit = 50
	minReviewReasonRunes  = 10
	maxReviewReasonRunes  = 1000
)

type Repository interface {
	ListPending(context.Context, int32) (PendingTorrentPage, error)
	ListAssignments(context.Context, uuid.UUID, int32) (ReviewAssignmentPage, error)
	Decide(context.Context, DecideCommand) (DecisionResult, error)
	Vote(context.Context, VoteCommand) (VoteResult, error)
}

type SessionAuthenticator interface {
	CurrentSession(context.Context, string) (identity.WebSession, error)
	AuthenticateWrite(context.Context, string, string) (identity.WebSession, error)
}

type EntitlementChecker interface {
	HasEntitlementAt(context.Context, uuid.UUID, workgroups.Entitlement, time.Time) (bool, error)
}

type Service struct {
	authenticator SessionAuthenticator
	repository    Repository
	authorizer    authz.Authorizer
	entitlements  EntitlementChecker
	now           func() time.Time
}

func NewService(authenticator SessionAuthenticator, repository Repository, authorizer authz.Authorizer, entitlements EntitlementChecker, now func() time.Time) (*Service, error) {
	if authenticator == nil || repository == nil || authorizer == nil || entitlements == nil {
		return nil, errors.New("torrent review dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{authenticator: authenticator, repository: repository, authorizer: authorizer, entitlements: entitlements, now: now}, nil
}

// NewStaffService exists only for controlled offline tooling that can execute
// the established staff finalization path but never serves member HTTP APIs.
// The runtime API must use NewService so member voting dependencies are explicit.
func NewStaffService(repository Repository, authorizer authz.Authorizer, now func() time.Time) (*Service, error) {
	if repository == nil || authorizer == nil {
		return nil, errors.New("torrent review dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{repository: repository, authorizer: authorizer, now: now}, nil
}

func (service *Service) ListAssignments(ctx context.Context, cookieToken string, limit int) (ReviewAssignmentPage, error) {
	if limit < 1 || limit > maxPendingReviewLimit {
		return ReviewAssignmentPage{}, ErrTorrentReviewInput
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return ReviewAssignmentPage{}, err
	}
	if _, err := authz.AuthorizeWebMemberAction(ctx, service.authorizer, session.User.ID, authz.ActionTorrentReviewVote, now); err != nil {
		return ReviewAssignmentPage{}, err
	}
	active, err := service.entitlements.HasEntitlementAt(ctx, session.User.ID, workgroups.EntitlementTorrentReviewVote, now)
	if err != nil {
		return ReviewAssignmentPage{}, err
	}
	if !active {
		return ReviewAssignmentPage{}, ErrTorrentReviewMembership
	}
	page, err := service.repository.ListAssignments(ctx, session.User.ID, int32(limit))
	if err != nil {
		return ReviewAssignmentPage{}, fmt.Errorf("list torrent review assignments: %w", err)
	}
	return page, nil
}

func (service *Service) Vote(ctx context.Context, cookieToken, csrfToken string, input VoteInput) (VoteResult, error) {
	normalized, err := normalizeVoteInput(input)
	if err != nil {
		return VoteResult{}, err
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return VoteResult{}, err
	}
	decision, err := authz.AuthorizeWebMemberAction(ctx, service.authorizer, session.User.ID, authz.ActionTorrentReviewVote, now)
	if err != nil {
		return VoteResult{}, err
	}
	active, err := service.entitlements.HasEntitlementAt(ctx, session.User.ID, workgroups.EntitlementTorrentReviewVote, now)
	if err != nil {
		return VoteResult{}, err
	}
	if !active {
		return VoteResult{}, ErrTorrentReviewMembership
	}
	result, err := service.repository.Vote(ctx, VoteCommand{
		VoteInput: normalized, VoterID: session.User.ID, OccurredAt: now, Authorization: decision,
	})
	if err != nil {
		return VoteResult{}, fmt.Errorf("submit torrent review vote: %w", err)
	}
	return result, nil
}

func (service *Service) ListPending(ctx context.Context, actor authz.StaffActor, limit int) (PendingTorrentPage, error) {
	if limit < 1 || limit > maxPendingReviewLimit {
		return PendingTorrentPage{}, ErrTorrentReviewInput
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTorrentReview, authz.SiteScope(), now, "torrent-review"); err != nil {
		return PendingTorrentPage{}, err
	}
	page, err := service.repository.ListPending(ctx, int32(limit))
	if err != nil {
		return PendingTorrentPage{}, fmt.Errorf("list pending torrent reviews: %w", err)
	}
	return page, nil
}

func (service *Service) Decide(ctx context.Context, actor authz.StaffActor, input DecideInput) (DecisionResult, error) {
	normalized, err := normalizeDecideInput(input)
	if err != nil {
		return DecisionResult{}, err
	}
	now := service.now().UTC()
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTorrentReview, authz.SiteScope(), now, "torrent-review")
	if err != nil {
		return DecisionResult{}, err
	}
	result, err := service.repository.Decide(ctx, DecideCommand{
		DecideInput: normalized, ReviewerID: actor.Subject.ID,
		OccurredAt: now, Authorization: decision, Resolution: "staff",
	})
	if err != nil {
		return DecisionResult{}, fmt.Errorf("decide torrent review: %w", err)
	}
	return result, nil
}

func normalizeVoteInput(input VoteInput) (VoteInput, error) {
	normalized, err := normalizeDecideInput(DecideInput{
		DecisionID: input.VoteID, TorrentID: input.TorrentID,
		ExpectedVersion: input.ExpectedVersion, Decision: input.Decision,
		ReasonCode: input.ReasonCode, Reason: input.Reason,
	})
	if err != nil {
		return VoteInput{}, err
	}
	input.VoteID = normalized.DecisionID
	input.TorrentID = normalized.TorrentID
	input.ExpectedVersion = normalized.ExpectedVersion
	input.Decision = normalized.Decision
	input.ReasonCode = normalized.ReasonCode
	input.Reason = normalized.Reason
	return input, nil
}

func normalizeDecideInput(input DecideInput) (DecideInput, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	reasonRunes := utf8.RuneCountInString(input.Reason)
	if input.DecisionID == uuid.Nil || input.TorrentID < 1 || input.ExpectedVersion < 1 ||
		!utf8.ValidString(input.Reason) || reasonRunes < minReviewReasonRunes || reasonRunes > maxReviewReasonRunes {
		return DecideInput{}, ErrTorrentReviewInput
	}
	switch input.Decision {
	case DecisionApprove:
		if input.ReasonCode != ReasonMeetsRequirements {
			return DecideInput{}, ErrTorrentReviewInput
		}
	case DecisionReject:
		if !validRejectionReasonCode(input.ReasonCode) {
			return DecideInput{}, ErrTorrentReviewInput
		}
	default:
		return DecideInput{}, ErrTorrentReviewInput
	}
	return input, nil
}

func validRejectionReasonCode(code ReasonCode) bool {
	switch code {
	case ReasonMetadataIncomplete, ReasonDuplicateOrSuperseded, ReasonContentPolicyViolation,
		ReasonQualityRequirementsNotMet, ReasonUploaderActionRequired, ReasonOther:
		return true
	default:
		return false
	}
}
