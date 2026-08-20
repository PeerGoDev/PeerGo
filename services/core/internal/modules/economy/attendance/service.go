package attendance

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
)

const (
	DefaultHistoryLimit = 35
	DefaultPolicyLimit  = 30
	MaximumPolicyLimit  = 100
	minimumReasonRunes  = 10
	maximumReasonRunes  = 1000
)

type MemberService struct {
	authenticator SessionAuthenticator
	repository    Repository
	authorizer    authz.Authorizer
	now           func() time.Time
}

type SessionAuthenticator interface {
	CurrentSession(context.Context, string) (identity.WebSession, error)
	AuthenticateWrite(context.Context, string, string) (identity.WebSession, error)
}

func NewMemberService(authenticator SessionAuthenticator, repository Repository, authorizer authz.Authorizer, now func() time.Time) (*MemberService, error) {
	if authenticator == nil || repository == nil || authorizer == nil {
		return nil, errors.New("attendance member dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &MemberService{authenticator: authenticator, repository: repository, authorizer: authorizer, now: now}, nil
}

func (service *MemberService) MyOverview(ctx context.Context, cookieToken string) (Overview, error) {
	now := canonicalTime(service.now())
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return Overview{}, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionEconomyAttendanceReadSelf, now); err != nil {
		return Overview{}, err
	}
	return service.repository.Overview(ctx, session.User.ID, now, DefaultHistoryLimit)
}

func (service *MemberService) Claim(ctx context.Context, cookieToken, csrfToken string, requestID uuid.UUID, mode Mode) (Record, error) {
	now := canonicalTime(service.now())
	if requestID == uuid.Nil || (mode != ModeFixed && mode != ModeRandom) {
		return Record{}, ErrInput
	}
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return Record{}, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionEconomyAttendanceClaimSelf, now); err != nil {
		return Record{}, err
	}
	return service.repository.Claim(ctx, ClaimCommand{RequestID: requestID, UserID: session.User.ID, Mode: mode, Now: now})
}

type AdministrationService struct {
	repository Repository
	authorizer authz.Authorizer
	now        func() time.Time
}

func NewAdministrationService(repository Repository, authorizer authz.Authorizer, now func() time.Time) (*AdministrationService, error) {
	if repository == nil || authorizer == nil {
		return nil, errors.New("attendance administration dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &AdministrationService{repository: repository, authorizer: authorizer, now: now}, nil
}

func (service *AdministrationService) List(ctx context.Context, actor authz.StaffActor, limit, offset int) (PolicyPage, error) {
	if limit < 1 || limit > MaximumPolicyLimit || offset < 0 || offset > 1_000_000 {
		return PolicyPage{}, ErrInput
	}
	now := canonicalTime(service.now())
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionEconomyAttendancePolicyRead, authz.SiteScope(), now, "attendance-policy-administration"); err != nil {
		return PolicyPage{}, err
	}
	items, total, err := service.repository.ListPolicies(ctx, limit, offset)
	if err != nil {
		return PolicyPage{}, err
	}
	minimum, err := minimumEffectiveFrom(items, now)
	if err != nil {
		return PolicyPage{}, err
	}
	return PolicyPage{Items: items, Total: total, Limit: limit, Offset: offset, MinimumEffectiveFrom: minimum}, nil
}

func (service *AdministrationService) Issue(ctx context.Context, actor authz.StaffActor, requestID uuid.UUID, settings PolicyRevision, reason string) (PublishedPolicy, error) {
	now := canonicalTime(service.now())
	reason = strings.TrimSpace(reason)
	if requestID == uuid.Nil || !utf8.ValidString(reason) || utf8.RuneCountInString(reason) < minimumReasonRunes || utf8.RuneCountInString(reason) > maximumReasonRunes {
		return PublishedPolicy{}, ErrInput
	}
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionEconomyAttendancePolicyIssue, authz.SiteScope(), now, "attendance-policy-issue")
	if err != nil {
		return PublishedPolicy{}, err
	}
	latest, err := service.repository.LatestPolicy(ctx)
	if err != nil && !errors.Is(err, ErrPolicyNotFound) {
		return PublishedPolicy{}, err
	}
	effectiveFrom, err := nextLocalMidnight(now, settings.DayBoundaryTimezone)
	if err != nil {
		return PublishedPolicy{}, err
	}
	if err == nil && !latest.Policy.EffectiveFrom.IsZero() && !effectiveFrom.After(latest.Policy.EffectiveFrom) {
		effectiveFrom, err = nextLocalMidnight(latest.Policy.EffectiveFrom, settings.DayBoundaryTimezone)
		if err != nil {
			return PublishedPolicy{}, err
		}
	}
	settings.Revision = "attendance-" + strings.ReplaceAll(requestID.String(), "-", "")
	settings.EffectiveFrom = effectiveFrom
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

func minimumEffectiveFrom(items []PublishedPolicy, now time.Time) (time.Time, error) {
	timezone := "Asia/Shanghai"
	if len(items) > 0 {
		timezone = items[0].Policy.DayBoundaryTimezone
	}
	minimum, err := nextLocalMidnight(now, timezone)
	if err != nil {
		return time.Time{}, err
	}
	if len(items) > 0 && !minimum.After(items[0].Policy.EffectiveFrom) {
		minimum, err = nextLocalMidnight(items[0].Policy.EffectiveFrom, timezone)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("calculate attendance policy boundary: %w", err)
	}
	return minimum, nil
}

// Service is the single HTTP-facing attendance surface. It keeps Web-session
// authentication and staff authorization in their specialized services while
// avoiding parallel transport dependencies for one bounded feature.
type Service struct {
	member         *MemberService
	administration *AdministrationService
}

func NewService(authenticator SessionAuthenticator, repository Repository, authorizer authz.Authorizer, now func() time.Time) (*Service, error) {
	member, err := NewMemberService(authenticator, repository, authorizer, now)
	if err != nil {
		return nil, err
	}
	administration, err := NewAdministrationService(repository, authorizer, now)
	if err != nil {
		return nil, err
	}
	return &Service{member: member, administration: administration}, nil
}

func (service *Service) MyOverview(ctx context.Context, cookieToken string) (Overview, error) {
	return service.member.MyOverview(ctx, cookieToken)
}

func (service *Service) Claim(ctx context.Context, cookieToken, csrfToken string, requestID uuid.UUID, mode Mode) (Record, error) {
	return service.member.Claim(ctx, cookieToken, csrfToken, requestID, mode)
}

func (service *Service) ListPolicies(ctx context.Context, actor authz.StaffActor, limit, offset int) (PolicyPage, error) {
	return service.administration.List(ctx, actor, limit, offset)
}

func (service *Service) IssuePolicy(ctx context.Context, actor authz.StaffActor, requestID uuid.UUID, policy PolicyRevision, reason string) (PublishedPolicy, error) {
	return service.administration.Issue(ctx, actor, requestID, policy, reason)
}
