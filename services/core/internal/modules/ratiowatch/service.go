package ratiowatch

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
	minimumWatch    = 24 * time.Hour
	maximumWatch    = 365 * 24 * time.Hour
)

type Service struct {
	repository    Repository
	authenticator WebSessionAuthenticator
	authorizer    authz.Authorizer
	now           func() time.Time
}

type WebSessionAuthenticator interface {
	CurrentSession(context.Context, string) (identity.WebSession, error)
	AuthenticateWrite(context.Context, string, string) (identity.WebSession, error)
}

func NewService(repository Repository, authenticator WebSessionAuthenticator, authorizer authz.Authorizer, now func() time.Time) (*Service, error) {
	if repository == nil || authenticator == nil || authorizer == nil {
		return nil, errors.New("ratio watch dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{repository: repository, authenticator: authenticator, authorizer: authorizer, now: now}, nil
}

// MyStatus authenticates the ordinary Web session and derives the target user
// exclusively from it. The HTTP request has no user ID parameter, preventing a
// caller from selecting another member's assessment.
func (service *Service) MyStatus(ctx context.Context, cookieToken string) (MyStatus, error) {
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return MyStatus{}, err
	}
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionRatioAssessmentReadSelf, now); err != nil {
		return MyStatus{}, err
	}
	return service.repository.MyStatus(ctx, session.User.ID, now)
}

// SubmitAppeal derives the target assessment from the authenticated user. The
// browser cannot select an assessment UUID, which prevents appealing another
// member's case or an old resolved assessment.
func (service *Service) SubmitAppeal(ctx context.Context, cookieToken, csrfToken string, input SubmitAppealInput) (Appeal, error) {
	input.Statement = strings.TrimSpace(input.Statement)
	if input.AppealID == uuid.Nil || !validAppealStatement(input.Statement) {
		return Appeal{}, ErrInput
	}
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return Appeal{}, err
	}
	now := service.now().UTC().Round(0)
	decision, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionRatioAppealCreateSelf, now)
	if err != nil {
		return Appeal{}, err
	}
	return service.repository.SubmitAppeal(ctx, SubmitAppealCommand{
		SubmitAppealInput: input, UserID: session.User.ID, OccurredAt: now, Authorization: decision,
	})
}

func (service *Service) Policies(ctx context.Context, actor authz.StaffActor, limit, offset int) (PolicyPage, error) {
	if !validPage(limit, offset) {
		return PolicyPage{}, ErrInput
	}
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionRatioPolicyRead, authz.SiteScope(), now, "ratio-policy-administration"); err != nil {
		return PolicyPage{}, err
	}
	return service.repository.Policies(ctx, limit, offset, now)
}

func (service *Service) Preview(ctx context.Context, actor authz.StaffActor, input PolicyInput) (ImpactPreview, error) {
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionRatioPolicyRead, authz.SiteScope(), now, "ratio-policy-preview"); err != nil {
		return ImpactPreview{}, err
	}
	policy, err := normalizePolicy(input)
	if err != nil {
		return ImpactPreview{}, err
	}
	return service.repository.Preview(ctx, policy, now)
}

func (service *Service) Issue(ctx context.Context, actor authz.StaffActor, input IssueInput) (PolicyRevision, error) {
	now := service.now().UTC().Round(0)
	input.EffectiveAt = input.EffectiveAt.UTC().Round(0)
	input.Reason = strings.TrimSpace(input.Reason)
	policy, err := normalizePolicy(input.Policy)
	if err != nil || input.RevisionID == uuid.Nil ||
		input.EffectiveAt.Before(now.Add(minimumLeadTime)) || input.EffectiveAt.After(now.Add(maximumLeadTime)) ||
		!validReason(input.Reason) {
		return PolicyRevision{}, ErrInput
	}
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionRatioPolicyIssue, authz.SiteScope(), now, "ratio-policy-administration")
	if err != nil {
		return PolicyRevision{}, err
	}
	input.Policy = policy
	return service.repository.Issue(ctx, IssueCommand{
		IssueInput: input, ActorID: actor.Subject.ID, OccurredAt: now, Authorization: decision,
	})
}

func (service *Service) Assessments(ctx context.Context, actor authz.StaffActor, query AssessmentQuery) (AssessmentPage, error) {
	query.Query = strings.TrimSpace(query.Query)
	if !validPage(query.Limit, query.Offset) || utf8.RuneCountInString(query.Query) > 120 || !validAssessmentFilter(query.Filter) {
		return AssessmentPage{}, ErrInput
	}
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionRatioPolicyRead, authz.SiteScope(), now, "ratio-assessment-read"); err != nil {
		return AssessmentPage{}, err
	}
	return service.repository.Assessments(ctx, query)
}

func (service *Service) Appeals(ctx context.Context, actor authz.StaffActor, query AppealQuery) (AppealPage, error) {
	query.Query = strings.TrimSpace(query.Query)
	if !validPage(query.Limit, query.Offset) || utf8.RuneCountInString(query.Query) > 120 || !validAppealFilter(query.Filter) {
		return AppealPage{}, ErrInput
	}
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionRatioPolicyRead, authz.SiteScope(), now, "ratio-appeal-read"); err != nil {
		return AppealPage{}, err
	}
	return service.repository.Appeals(ctx, query)
}

func (service *Service) DecideAppeal(ctx context.Context, actor authz.StaffActor, input DecideAppealInput) (Appeal, error) {
	input.Response = strings.TrimSpace(input.Response)
	if input.AppealID == uuid.Nil || input.ExpectedAssessmentVersion < 1 ||
		(input.Decision != AppealDecisionApprove && input.Decision != AppealDecisionReject) || !validReason(input.Response) {
		return Appeal{}, ErrInput
	}
	now := service.now().UTC().Round(0)
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionRatioAssessmentManage, authz.SiteScope(), now, "ratio-appeal-administration")
	if err != nil {
		return Appeal{}, err
	}
	return service.repository.DecideAppeal(ctx, DecideAppealCommand{
		DecideAppealInput: input, ActorID: actor.Subject.ID, OccurredAt: now, Authorization: decision,
	})
}

func (service *Service) Clear(ctx context.Context, actor authz.StaffActor, input ClearInput) (Assessment, error) {
	now := service.now().UTC().Round(0)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.AssessmentID == uuid.Nil || input.ExpectedVersion < 1 || !validReason(input.Reason) {
		return Assessment{}, ErrInput
	}
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionRatioAssessmentManage, authz.SiteScope(), now, "ratio-assessment-administration")
	if err != nil {
		return Assessment{}, err
	}
	return service.repository.Clear(ctx, ClearCommand{
		ClearInput: input, ActorID: actor.Subject.ID, OccurredAt: now, Authorization: decision,
	})
}

func normalizePolicy(input PolicyInput) (PolicyInput, error) {
	if !input.Enabled {
		return PolicyInput{}, nil
	}
	watch := time.Duration(input.WatchPeriodSeconds) * time.Second
	if input.DownloadThresholdBytes < MinimumDownloadGate || input.DownloadThresholdBytes > 9_000_000_000_000_000_000 ||
		input.MinimumRatioBasisPoints < 1 || input.MinimumRatioBasisPoints > MaximumRatioBPS ||
		input.RestrictionRatioBasisPoints < 1 || input.RestrictionRatioBasisPoints > input.MinimumRatioBasisPoints ||
		watch < minimumWatch || watch > maximumWatch {
		return PolicyInput{}, ErrInput
	}
	return input, nil
}

func validPage(limit, offset int) bool {
	return limit >= 1 && limit <= MaximumListLimit && offset >= 0 && offset <= 1_000_000
}

func validReason(reason string) bool {
	return utf8.ValidString(reason) && utf8.RuneCountInString(reason) >= 10 && utf8.RuneCountInString(reason) <= 1000
}

func validAppealStatement(statement string) bool {
	return utf8.ValidString(statement) && utf8.RuneCountInString(statement) >= 20 && utf8.RuneCountInString(statement) <= 1000
}

func validAssessmentFilter(filter AssessmentFilter) bool {
	switch filter {
	case AssessmentFilterAll, AssessmentFilterActive, AssessmentFilterWatching,
		AssessmentFilterWarning, AssessmentFilterRestricted, AssessmentFilterResolved:
		return true
	default:
		return false
	}
}

func validAppealFilter(filter AppealFilter) bool {
	switch filter {
	case AppealFilterAll, AppealFilterPending, AppealFilterResolved:
		return true
	default:
		return false
	}
}
