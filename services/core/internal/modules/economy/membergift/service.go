package membergift

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

const (
	minimumReasonRunes  = 10
	maximumReasonRunes  = 1000
	maximumMessageRunes = 200
	policyTimezone      = "Asia/Shanghai"
)

type SessionAuthenticator interface {
	CurrentSession(context.Context, string) (identity.WebSession, error)
	AuthenticateWrite(context.Context, string, string) (identity.WebSession, error)
}

type Service struct {
	authenticator SessionAuthenticator
	repository    Repository
	authorizer    authz.Authorizer
	now           func() time.Time
}

func NewService(authenticator SessionAuthenticator, repository Repository, authorizer authz.Authorizer, now func() time.Time) (*Service, error) {
	if authenticator == nil || repository == nil || authorizer == nil {
		return nil, errors.New("member gift dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{authenticator: authenticator, repository: repository, authorizer: authorizer, now: now}, nil
}

func (service *Service) MyOverview(ctx context.Context, cookieToken string, limit int) (Overview, error) {
	if limit < 1 || limit > MaximumHistoryLimit {
		return Overview{}, ErrInput
	}
	now := canonicalTime(service.now())
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return Overview{}, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionEconomyMemberGiftReadSelf, now); err != nil {
		return Overview{}, err
	}
	dayStart, dayEnd, err := giftDay(now)
	if err != nil {
		return Overview{}, err
	}
	return service.repository.Overview(ctx, session.User.ID, dayStart, dayEnd, limit)
}

func (service *Service) Create(ctx context.Context, cookieToken, csrfToken string, requestID uuid.UUID, recipientNumericID, amount int64, message string) (Gift, error) {
	message = strings.TrimSpace(message)
	if requestID == uuid.Nil || recipientNumericID < 1 || amount < 1 ||
		!utf8.ValidString(message) || utf8.RuneCountInString(message) > maximumMessageRunes {
		return Gift{}, ErrInput
	}
	now := canonicalTime(service.now())
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return Gift{}, err
	}
	if session.User.EmailVerifiedAt == nil {
		return Gift{}, ErrSenderIneligible
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionEconomyMemberGiftCreateSelf, now); err != nil {
		return Gift{}, err
	}
	dayStart, dayEnd, err := giftDay(now)
	if err != nil {
		return Gift{}, err
	}
	return service.repository.Create(ctx, CreateCommand{
		RequestID: requestID, SenderUserID: session.User.ID,
		RecipientNumericID: recipientNumericID, Amount: amount, Message: message,
		Now: now, DayStartsAt: dayStart, DayEndsAt: dayEnd,
	})
}

func (service *Service) ListPolicies(ctx context.Context, actor authz.StaffActor, limit, offset int) (PolicyPage, error) {
	if limit < 1 || limit > MaximumPolicyLimit || offset < 0 || offset > 1_000_000 {
		return PolicyPage{}, ErrInput
	}
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionEconomyMemberGiftPolicyRead, authz.SiteScope(), canonicalTime(service.now()), "member-gift-policy-read"); err != nil {
		return PolicyPage{}, err
	}
	items, total, err := service.repository.ListPolicies(ctx, limit, offset)
	if err != nil {
		return PolicyPage{}, err
	}
	return PolicyPage{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

func (service *Service) IssuePolicy(ctx context.Context, actor authz.StaffActor, requestID uuid.UUID, settings PolicyRevision, reason string) (PublishedPolicy, error) {
	reason = strings.TrimSpace(reason)
	now := canonicalTime(service.now())
	if requestID == uuid.Nil || !utf8.ValidString(reason) ||
		utf8.RuneCountInString(reason) < minimumReasonRunes || utf8.RuneCountInString(reason) > maximumReasonRunes {
		return PublishedPolicy{}, ErrInput
	}
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionEconomyMemberGiftPolicyIssue, authz.SiteScope(), now, "member-gift-policy-issue")
	if err != nil {
		return PublishedPolicy{}, err
	}
	settings.Revision = "member-gift-" + strings.ReplaceAll(requestID.String(), "-", "")
	settings.CreatedAt = now
	normalized, snapshot, err := NormalizePolicy(settings)
	if err != nil {
		return PublishedPolicy{}, err
	}
	return service.repository.PublishPolicy(ctx, PublishCommand{
		Policy: normalized, IssuedBy: actor.Subject.ID,
		AuthorizationDecisionID: decision.ID, Reason: reason, SnapshotJSON: snapshot,
	})
}

func giftDay(now time.Time) (time.Time, time.Time, error) {
	location, err := time.LoadLocation(policyTimezone)
	if err != nil {
		return time.Time{}, time.Time{}, ErrInvariant
	}
	local := now.In(location)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	return canonicalTime(start), canonicalTime(start.AddDate(0, 0, 1)), nil
}
