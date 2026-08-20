package newcomer

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
	minimumLeadTime = 5 * time.Minute
	maximumLeadTime = 365 * 24 * time.Hour
	minimumDuration = 7 * 24 * time.Hour
	maximumDuration = 90 * 24 * time.Hour
)

type WebSessionAuthenticator interface {
	CurrentSession(context.Context, string) (identity.WebSession, error)
}

type Service struct {
	repository    Repository
	authenticator WebSessionAuthenticator
	authorizer    authz.Authorizer
	now           func() time.Time
}

func NewService(repository Repository, authenticator WebSessionAuthenticator, authorizer authz.Authorizer, now func() time.Time) (*Service, error) {
	if repository == nil || authenticator == nil || authorizer == nil {
		return nil, errors.New("newcomer assessment dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{repository: repository, authenticator: authenticator, authorizer: authorizer, now: now}, nil
}

func (service *Service) MyStatus(ctx context.Context, cookieToken string) (MyStatus, error) {
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return MyStatus{}, err
	}
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionNewcomerAssessmentReadSelf, now); err != nil {
		return MyStatus{}, err
	}
	return service.repository.MyStatus(ctx, session.User.ID, now)
}

func (service *Service) Policies(ctx context.Context, actor authz.StaffActor, limit, offset int) (PolicyPage, error) {
	if !validPage(limit, offset) {
		return PolicyPage{}, ErrInput
	}
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionNewcomerPolicyRead, authz.SiteScope(), now, "newcomer-policy-read"); err != nil {
		return PolicyPage{}, err
	}
	return service.repository.Policies(ctx, limit, offset, now)
}

func (service *Service) Issue(ctx context.Context, actor authz.StaffActor, input IssueInput) (PolicyRevision, error) {
	now := service.now().UTC().Round(0)
	input.EffectiveAt = input.EffectiveAt.UTC().Round(0)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.RequestID == uuid.Nil || !validPolicy(input.Policy) || !validReason(input.Reason) ||
		input.EffectiveAt.Before(now.Add(minimumLeadTime)) || input.EffectiveAt.After(now.Add(maximumLeadTime)) {
		return PolicyRevision{}, ErrInput
	}
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionNewcomerPolicyIssue, authz.SiteScope(), now, "newcomer-policy-issue")
	if err != nil {
		return PolicyRevision{}, err
	}
	return service.repository.Issue(ctx, IssueCommand{
		IssueInput: input, ActorID: actor.Subject.ID, OccurredAt: now, Authorization: decision,
	})
}

func (service *Service) Assessments(ctx context.Context, actor authz.StaffActor, query AssessmentQuery) (AssessmentPage, error) {
	query.Query = strings.TrimSpace(query.Query)
	if !validPage(query.Limit, query.Offset) || utf8.RuneCountInString(query.Query) > 120 || !validFilter(query.Filter) {
		return AssessmentPage{}, ErrInput
	}
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionNewcomerAssessmentRead, authz.SiteScope(), now, "newcomer-assessment-read"); err != nil {
		return AssessmentPage{}, err
	}
	return service.repository.Assessments(ctx, query)
}

func (service *Service) Exempt(ctx context.Context, actor authz.StaffActor, input ExemptInput) (Assessment, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if input.ExemptionID == uuid.Nil || input.AssessmentID == uuid.Nil || input.ExpectedVersion < 1 || !validReason(input.Reason) {
		return Assessment{}, ErrInput
	}
	now := service.now().UTC().Round(0)
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionNewcomerAssessmentExempt, authz.SiteScope(), now, "newcomer-assessment-exempt")
	if err != nil {
		return Assessment{}, err
	}
	return service.repository.Exempt(ctx, ExemptCommand{
		ExemptInput: input, ActorID: actor.Subject.ID, OccurredAt: now, Authorization: decision,
	})
}

func validPolicy(policy PolicyInput) bool {
	duration := time.Duration(policy.DurationSeconds) * time.Second
	if duration < minimumDuration || duration > maximumDuration ||
		policy.MinimumCreditedUploadBytes < 0 || policy.MinimumCreditedUploadBytes > 9_000_000_000_000_000_000 ||
		policy.MinimumSeedingActiveSeconds < 0 || policy.MinimumSeedingActiveSeconds > 315_360_000 {
		return false
	}
	if policy.Enabled {
		return policy.MinimumCreditedUploadBytes > 0 || policy.MinimumSeedingActiveSeconds > 0
	}
	return policy.MinimumCreditedUploadBytes == 0 && policy.MinimumSeedingActiveSeconds == 0
}

func validPage(limit, offset int) bool {
	return limit >= 1 && limit <= MaximumListLimit && offset >= 0 && offset <= 1_000_000
}

func validReason(reason string) bool {
	return utf8.ValidString(reason) && utf8.RuneCountInString(reason) >= 10 && utf8.RuneCountInString(reason) <= 1000
}

func validFilter(filter AssessmentFilter) bool {
	return filter == AssessmentFilterAll || filter == AssessmentFilterActive ||
		filter == AssessmentFilterRestricted || filter == AssessmentFilterResolved
}
