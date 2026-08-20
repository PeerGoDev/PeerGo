package workgroups

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
	DefaultPageLimit        = 50
	MaximumPageLimit        = 100
	DefaultCycleLimit       = 6
	MaximumCycleLimit       = 24
	minimumStatementRunes   = 20
	minimumReasonRunes      = 10
	maximumTextRunes        = 1000
	maximumTaskTextRunes    = 2000
	maximumPolicyLeadMonths = 24
	maximumTaskDuration     = 366 * 24 * time.Hour
)

type SessionAuthenticator interface {
	CurrentSession(context.Context, string) (identity.WebSession, error)
	AuthenticateWrite(context.Context, string, string) (identity.WebSession, error)
}

// Service is the single application boundary for member and staff workgroup
// workflows. Authentication stays audience-specific while all persistence and
// history invariants remain in one repository transaction boundary.
type Service struct {
	authenticator SessionAuthenticator
	repository    Repository
	authorizer    authz.Authorizer
	now           func() time.Time
}

func NewService(authenticator SessionAuthenticator, repository Repository, authorizer authz.Authorizer, now func() time.Time) (*Service, error) {
	if authenticator == nil || repository == nil || authorizer == nil {
		return nil, errors.New("workgroup dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{authenticator: authenticator, repository: repository, authorizer: authorizer, now: now}, nil
}

func (service *Service) MyOverview(ctx context.Context, cookieToken string) (MyOverview, error) {
	now := canonicalTime(service.now())
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return MyOverview{}, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionWorkgroupReadSelf, now); err != nil {
		return MyOverview{}, err
	}
	return service.repository.MyOverview(ctx, session.User.ID, now)
}

func (service *Service) MyContributionCycles(ctx context.Context, cookieToken string, kind GroupKind, limit int) (ContributionCyclePage, error) {
	if !validGroupKind(kind) || !validCycleLimit(limit) {
		return ContributionCyclePage{}, ErrInput
	}
	now := canonicalTime(service.now())
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return ContributionCyclePage{}, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionWorkgroupReadSelf, now); err != nil {
		return ContributionCyclePage{}, err
	}
	return service.repository.ListMyContributionCycles(ctx, session.User.ID, kind, limit, now)
}

func (service *Service) MyTasks(ctx context.Context, cookieToken string, limit, offset int) (TaskAssignmentPage, error) {
	if !validPage(limit, offset) {
		return TaskAssignmentPage{}, ErrInput
	}
	now := canonicalTime(service.now())
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return TaskAssignmentPage{}, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionWorkgroupReadSelf, now); err != nil {
		return TaskAssignmentPage{}, err
	}
	return service.repository.ListMyTasks(ctx, session.User.ID, limit, offset, now)
}

func (service *Service) SubmitTask(ctx context.Context, cookieToken, csrfToken string, requestID, assignmentID uuid.UUID, statement string) (TaskAssignment, error) {
	statement = strings.TrimSpace(statement)
	if requestID == uuid.Nil || assignmentID == uuid.Nil || !validTaskStatement(statement) {
		return TaskAssignment{}, ErrInput
	}
	now := canonicalTime(service.now())
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return TaskAssignment{}, err
	}
	decision, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionWorkgroupTaskSubmitSelf, now)
	if err != nil {
		return TaskAssignment{}, err
	}
	return service.repository.SubmitTask(ctx, SubmitTaskCommand{
		SubmissionID: uuid.New(), RequestID: requestID, AssignmentID: assignmentID,
		UserID: session.User.ID, Statement: statement,
		AuthorizationDecisionID: decision.ID, OccurredAt: now,
	})
}

func (service *Service) Apply(ctx context.Context, cookieToken, csrfToken string, requestID uuid.UUID, kind GroupKind, statement string) (Application, error) {
	statement = strings.TrimSpace(statement)
	if requestID == uuid.Nil || !validGroupKind(kind) ||
		!utf8.ValidString(statement) || utf8.RuneCountInString(statement) < minimumStatementRunes ||
		utf8.RuneCountInString(statement) > maximumTextRunes {
		return Application{}, ErrInput
	}
	now := canonicalTime(service.now())
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return Application{}, err
	}
	decision, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionWorkgroupApplicationCreateSelf, now)
	if err != nil {
		return Application{}, err
	}
	return service.repository.SubmitApplication(ctx, SubmitApplicationCommand{
		ApplicationID: uuid.New(), RequestID: requestID, ApplicantID: session.User.ID,
		GroupKind: kind, Statement: statement,
		AuthorizationDecisionID: decision.ID, OccurredAt: now,
	})
}

func (service *Service) AdminOverview(ctx context.Context, actor authz.StaffActor) (AdminOverview, error) {
	now := canonicalTime(service.now())
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionWorkgroupManageRead, authz.SiteScope(), now, "workgroup-administration"); err != nil {
		return AdminOverview{}, err
	}
	return service.repository.AdminOverview(ctx, now)
}

func (service *Service) Tasks(ctx context.Context, actor authz.StaffActor, kind GroupKind, limit, offset int) (TaskPage, error) {
	if !validGroupKind(kind) || !validPage(limit, offset) {
		return TaskPage{}, ErrInput
	}
	now := canonicalTime(service.now())
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionWorkgroupManageRead, authz.SiteScope(), now, "workgroup-task-list"); err != nil {
		return TaskPage{}, err
	}
	return service.repository.ListTasks(ctx, kind, limit, offset, now)
}

func (service *Service) PublishTask(ctx context.Context, actor authz.StaffActor, requestID uuid.UUID, kind GroupKind, taskType TaskType, title, description string, startsAt, dueAt time.Time) (Task, error) {
	now := canonicalTime(service.now())
	startsAt = canonicalTime(startsAt)
	dueAt = canonicalTime(dueAt)
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	if requestID == uuid.Nil || !validGroupKind(kind) || !validTaskType(taskType) ||
		!validTaskTitle(title) || !validTaskDescription(description) || startsAt.Before(now) ||
		!dueAt.After(startsAt) || dueAt.Sub(startsAt) > maximumTaskDuration {
		return Task{}, ErrInput
	}
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionWorkgroupTaskPublish, authz.SiteScope(), now, "workgroup-task-publish")
	if err != nil {
		return Task{}, err
	}
	return service.repository.PublishTask(ctx, PublishTaskCommand{
		TaskID: uuid.New(), RequestID: requestID, GroupKind: kind, Type: taskType,
		Title: title, Description: description, StartsAt: startsAt, DueAt: dueAt,
		ActorID: actor.Subject.ID, AuthorizationDecisionID: decision.ID, OccurredAt: now,
	})
}

func (service *Service) TaskAssignments(ctx context.Context, actor authz.StaffActor, kind GroupKind, taskID uuid.UUID, limit, offset int) (TaskAssignmentPage, error) {
	if !validGroupKind(kind) || taskID == uuid.Nil || !validPage(limit, offset) {
		return TaskAssignmentPage{}, ErrInput
	}
	now := canonicalTime(service.now())
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionWorkgroupManageRead, authz.SiteScope(), now, "workgroup-task-assignment-list"); err != nil {
		return TaskAssignmentPage{}, err
	}
	return service.repository.ListTaskAssignments(ctx, kind, taskID, limit, offset, now)
}

func (service *Service) ReviewTaskSubmission(ctx context.Context, actor authz.StaffActor, requestID, submissionID uuid.UUID, decisionValue TaskReviewDecision, reason string) (TaskAssignment, error) {
	reason = strings.TrimSpace(reason)
	if requestID == uuid.Nil || submissionID == uuid.Nil || !validTaskReviewDecision(decisionValue) || !validReason(reason) {
		return TaskAssignment{}, ErrInput
	}
	now := canonicalTime(service.now())
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionWorkgroupTaskReview, authz.SiteScope(), now, "workgroup-task-review")
	if err != nil {
		return TaskAssignment{}, err
	}
	return service.repository.ReviewTaskSubmission(ctx, ReviewTaskSubmissionCommand{
		ReviewID: uuid.New(), RequestID: requestID, SubmissionID: submissionID,
		Decision: decisionValue, Reason: reason, ActorID: actor.Subject.ID,
		AuthorizationDecisionID: decision.ID, OccurredAt: now,
	})
}

func (service *Service) ContributionPolicies(ctx context.Context, actor authz.StaffActor, kind GroupKind, limit, offset int) (ContributionPolicyPage, error) {
	if !validGroupKind(kind) || !validPage(limit, offset) {
		return ContributionPolicyPage{}, ErrInput
	}
	now := canonicalTime(service.now())
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionWorkgroupManageRead, authz.SiteScope(), now, "workgroup-contribution-policy-read"); err != nil {
		return ContributionPolicyPage{}, err
	}
	return service.repository.ListContributionPolicies(ctx, kind, limit, offset, now)
}

func (service *Service) IssueContributionPolicy(ctx context.Context, actor authz.StaffActor, requestID uuid.UUID, kind GroupKind, targetValue int64, effectiveFrom time.Time, reason string) (ContributionPolicy, error) {
	now := canonicalTime(service.now())
	effectiveFrom = canonicalTime(effectiveFrom)
	reason = strings.TrimSpace(reason)
	minimumEffectiveFrom := calendarMonthStart(now).AddDate(0, 1, 0)
	maximumEffectiveFrom := calendarMonthStart(now).AddDate(0, maximumPolicyLeadMonths, 0)
	if requestID == uuid.Nil || !validGroupKind(kind) || !validContributionTarget(kind, targetValue) ||
		!effectiveFrom.Equal(calendarMonthStart(effectiveFrom)) || effectiveFrom.Before(minimumEffectiveFrom) ||
		effectiveFrom.After(maximumEffectiveFrom) || !validReason(reason) {
		return ContributionPolicy{}, ErrInput
	}
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionWorkgroupContributionPolicyIssue, authz.SiteScope(), now, "workgroup-contribution-policy-issue")
	if err != nil {
		return ContributionPolicy{}, err
	}
	return service.repository.IssueContributionPolicy(ctx, IssueContributionPolicyCommand{
		RequestID: requestID, GroupKind: kind, TargetValue: targetValue,
		EffectiveFrom: effectiveFrom, ActorID: actor.Subject.ID, Reason: reason,
		AuthorizationDecisionID: decision.ID, OccurredAt: now,
	})
}

func (service *Service) ListApplications(ctx context.Context, actor authz.StaffActor, status ApplicationStatus, limit, offset int) (ApplicationPage, error) {
	if !validApplicationStatusFilter(status) || !validPage(limit, offset) {
		return ApplicationPage{}, ErrInput
	}
	now := canonicalTime(service.now())
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionWorkgroupManageRead, authz.SiteScope(), now, "workgroup-application-list"); err != nil {
		return ApplicationPage{}, err
	}
	return service.repository.ListApplications(ctx, status, limit, offset)
}

func (service *Service) DecideApplication(ctx context.Context, actor authz.StaffActor, requestID, applicationID uuid.UUID, expectedVersion int64, approve bool, reason string) (Application, error) {
	reason = strings.TrimSpace(reason)
	if requestID == uuid.Nil || applicationID == uuid.Nil || expectedVersion < 1 || !validReason(reason) {
		return Application{}, ErrInput
	}
	now := canonicalTime(service.now())
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionWorkgroupApplicationDecide, authz.SiteScope(), now, "workgroup-application-decision")
	if err != nil {
		return Application{}, err
	}
	return service.repository.DecideApplication(ctx, DecideApplicationCommand{
		DecisionID: requestID, ApplicationID: applicationID,
		ExpectedVersion: expectedVersion, Approve: approve,
		ActorID: actor.Subject.ID, Reason: reason,
		AuthorizationDecisionID: decision.ID, OccurredAt: now,
	})
}

func (service *Service) ListMemberships(ctx context.Context, actor authz.StaffActor, kind GroupKind, status MembershipStatus, limit, offset int) (MembershipPage, error) {
	if !validGroupKind(kind) || !validMembershipStatusFilter(status) || !validPage(limit, offset) {
		return MembershipPage{}, ErrInput
	}
	now := canonicalTime(service.now())
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionWorkgroupManageRead, authz.SiteScope(), now, "workgroup-membership-list"); err != nil {
		return MembershipPage{}, err
	}
	return service.repository.ListMemberships(ctx, kind, status, limit, offset, now)
}

func (service *Service) ContributionCycles(ctx context.Context, actor authz.StaffActor, kind GroupKind, membershipID uuid.UUID, limit int) (ContributionCyclePage, error) {
	if !validGroupKind(kind) || membershipID == uuid.Nil || !validCycleLimit(limit) {
		return ContributionCyclePage{}, ErrInput
	}
	now := canonicalTime(service.now())
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionWorkgroupManageRead, authz.SiteScope(), now, "workgroup-contribution-cycle-read"); err != nil {
		return ContributionCyclePage{}, err
	}
	return service.repository.ListContributionCycles(ctx, kind, membershipID, limit, now)
}

func (service *Service) IssueContributionReminder(ctx context.Context, actor authz.StaffActor, requestID uuid.UUID, kind GroupKind, membershipID uuid.UUID, periodStartsAt time.Time, reason string) (ContributionReminder, error) {
	now := canonicalTime(service.now())
	periodStartsAt = canonicalTime(periodStartsAt)
	reason = strings.TrimSpace(reason)
	currentPeriod := calendarMonthStart(now)
	oldestPeriod := currentPeriod.AddDate(0, -(MaximumCycleLimit - 1), 0)
	if requestID == uuid.Nil || membershipID == uuid.Nil || !validGroupKind(kind) ||
		!periodStartsAt.Equal(calendarMonthStart(periodStartsAt)) ||
		periodStartsAt.Before(oldestPeriod) || periodStartsAt.After(currentPeriod) ||
		!validReason(reason) {
		return ContributionReminder{}, ErrInput
	}
	decision, err := authz.AuthorizeStaffAction(
		ctx,
		service.authorizer,
		actor,
		authz.ActionWorkgroupContributionReminderIssue,
		authz.SiteScope(),
		now,
		"workgroup-contribution-reminder-issue",
	)
	if err != nil {
		return ContributionReminder{}, err
	}
	return service.repository.IssueContributionReminder(ctx, IssueContributionReminderCommand{
		RequestID: requestID, MembershipID: membershipID, GroupKind: kind,
		PeriodStartsAt: periodStartsAt, ActorID: actor.Subject.ID, Reason: reason,
		AuthorizationDecisionID: decision.ID, OccurredAt: now,
	})
}

func (service *Service) GrantMembership(ctx context.Context, actor authz.StaffActor, requestID uuid.UUID, kind GroupKind, userNumericID int64, reason string) (Membership, error) {
	reason = strings.TrimSpace(reason)
	if requestID == uuid.Nil || !validGroupKind(kind) || userNumericID < 1 || !validReason(reason) {
		return Membership{}, ErrInput
	}
	now := canonicalTime(service.now())
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionWorkgroupMembershipManage, authz.SiteScope(), now, "workgroup-membership-grant")
	if err != nil {
		return Membership{}, err
	}
	return service.repository.GrantMembership(ctx, GrantMembershipCommand{
		TransitionID: requestID, GroupKind: kind, UserNumericID: userNumericID,
		ActorID: actor.Subject.ID, Reason: reason,
		AuthorizationDecisionID: decision.ID, OccurredAt: now,
	})
}

func (service *Service) ChangeMembership(ctx context.Context, actor authz.StaffActor, requestID, membershipID uuid.UUID, kind GroupKind, expectedVersion int64, transition MembershipTransition, reason string) (Membership, error) {
	reason = strings.TrimSpace(reason)
	if requestID == uuid.Nil || membershipID == uuid.Nil || !validGroupKind(kind) || expectedVersion < 1 || !validMembershipTransition(transition) || !validReason(reason) {
		return Membership{}, ErrInput
	}
	now := canonicalTime(service.now())
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionWorkgroupMembershipManage, authz.SiteScope(), now, "workgroup-membership-transition")
	if err != nil {
		return Membership{}, err
	}
	return service.repository.ChangeMembership(ctx, ChangeMembershipCommand{
		TransitionID: requestID, MembershipID: membershipID,
		GroupKind:       kind,
		ExpectedVersion: expectedVersion, Transition: transition,
		ActorID: actor.Subject.ID, Reason: reason,
		AuthorizationDecisionID: decision.ID, OccurredAt: now,
	})
}

// HasEntitlementAt is the narrow port consumed by torrent and settlement use
// cases. Callers must pass the event time; using time.Now here would corrupt
// historical accounting after a membership is suspended or restored.
func (service *Service) HasEntitlementAt(ctx context.Context, userID uuid.UUID, entitlement Entitlement, at time.Time) (bool, error) {
	if userID == uuid.Nil || at.IsZero() {
		return false, ErrInput
	}
	return service.repository.HasEntitlementAt(ctx, userID, entitlement, canonicalTime(at))
}

func validGroupKind(kind GroupKind) bool {
	return kind == GroupReseed || kind == GroupReview || kind == GroupRetention
}

func validApplicationStatusFilter(status ApplicationStatus) bool {
	return status == "" || status == ApplicationPending || status == ApplicationApproved || status == ApplicationRejected
}

func validMembershipStatusFilter(status MembershipStatus) bool {
	return status == "" || status == MembershipActive || status == MembershipSuspended || status == MembershipEnded
}

func validMembershipTransition(transition MembershipTransition) bool {
	return transition == TransitionSuspend || transition == TransitionReactivate || transition == TransitionEnd
}

func validPage(limit, offset int) bool {
	return limit >= 1 && limit <= MaximumPageLimit && offset >= 0 && offset <= 1_000_000
}

func validCycleLimit(limit int) bool {
	return limit >= 1 && limit <= MaximumCycleLimit
}

func validReason(reason string) bool {
	return utf8.ValidString(reason) && utf8.RuneCountInString(reason) >= minimumReasonRunes && utf8.RuneCountInString(reason) <= maximumTextRunes
}

func validTaskType(taskType TaskType) bool {
	return taskType == TaskTypeTask || taskType == TaskTypeActivity
}

func validTaskReviewDecision(decision TaskReviewDecision) bool {
	return decision == TaskReviewAccepted || decision == TaskReviewRejected
}

func validTaskTitle(title string) bool {
	return utf8.ValidString(title) && utf8.RuneCountInString(title) >= 2 && utf8.RuneCountInString(title) <= 100
}

func validTaskDescription(description string) bool {
	return utf8.ValidString(description) && utf8.RuneCountInString(description) >= minimumReasonRunes && utf8.RuneCountInString(description) <= maximumTaskTextRunes
}

func validTaskStatement(statement string) bool {
	return utf8.ValidString(statement) && utf8.RuneCountInString(statement) >= minimumReasonRunes && utf8.RuneCountInString(statement) <= maximumTaskTextRunes
}

func validContributionTarget(kind GroupKind, target int64) bool {
	switch kind {
	case GroupReseed:
		return target >= 1 && target <= 100_000
	case GroupReview:
		return target >= 1 && target <= 1_000_000
	case GroupRetention:
		return target >= 1 && target <= 3_153_600_000
	default:
		return false
	}
}

func calendarMonthStart(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func canonicalTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}
