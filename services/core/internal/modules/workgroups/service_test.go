package workgroups

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type authenticatorStub struct {
	session     identity.WebSession
	cookieToken string
	csrfToken   string
}

func (stub *authenticatorStub) CurrentSession(_ context.Context, cookieToken string) (identity.WebSession, error) {
	stub.cookieToken = cookieToken
	return stub.session, nil
}

func (stub *authenticatorStub) AuthenticateWrite(_ context.Context, cookieToken, csrfToken string) (identity.WebSession, error) {
	stub.cookieToken = cookieToken
	stub.csrfToken = csrfToken
	return stub.session, nil
}

type authorizerStub struct {
	now     time.Time
	request authz.Request
}

func (stub *authorizerStub) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	stub.request = request
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed,
		PolicyVersion: authz.PolicyVersion, RoleID: "site_admin",
		GrantID: uuid.New(), GrantVersion: 1, MandateID: uuid.New(),
		EffectiveUntil: stub.now.Add(time.Hour),
	}, nil
}

type repositoryStub struct {
	applicationCommand SubmitApplicationCommand
	grantCommand       GrantMembershipCommand
	policyCommand      IssueContributionPolicyCommand
	reminderCommand    IssueContributionReminderCommand
	publishTaskCommand PublishTaskCommand
	submitTaskCommand  SubmitTaskCommand
	reviewTaskCommand  ReviewTaskSubmissionCommand
	entitlementUser    uuid.UUID
	entitlement        Entitlement
	entitlementAt      time.Time
	myCycleUser        uuid.UUID
	cycleMembership    uuid.UUID
	cycleKind          GroupKind
	cycleLimit         int
	cycleAsOf          time.Time
}

func (stub *repositoryStub) MyOverview(context.Context, uuid.UUID, time.Time) (MyOverview, error) {
	return MyOverview{}, nil
}

func (stub *repositoryStub) SubmitApplication(_ context.Context, command SubmitApplicationCommand) (Application, error) {
	stub.applicationCommand = command
	return Application{ID: command.ApplicationID, Statement: command.Statement}, nil
}

func (stub *repositoryStub) AdminOverview(context.Context, time.Time) (AdminOverview, error) {
	return AdminOverview{}, nil
}

func (stub *repositoryStub) ListApplications(context.Context, ApplicationStatus, int, int) (ApplicationPage, error) {
	return ApplicationPage{}, nil
}

func (stub *repositoryStub) DecideApplication(context.Context, DecideApplicationCommand) (Application, error) {
	return Application{}, nil
}

func (stub *repositoryStub) ListMemberships(context.Context, GroupKind, MembershipStatus, int, int, time.Time) (MembershipPage, error) {
	return MembershipPage{}, nil
}

func (stub *repositoryStub) GrantMembership(_ context.Context, command GrantMembershipCommand) (Membership, error) {
	stub.grantCommand = command
	return Membership{GroupKind: command.GroupKind, UserNumericID: command.UserNumericID}, nil
}

func (stub *repositoryStub) ChangeMembership(context.Context, ChangeMembershipCommand) (Membership, error) {
	return Membership{}, nil
}

func (stub *repositoryStub) ListContributionPolicies(context.Context, GroupKind, int, int, time.Time) (ContributionPolicyPage, error) {
	return ContributionPolicyPage{}, nil
}

func (stub *repositoryStub) IssueContributionPolicy(_ context.Context, command IssueContributionPolicyCommand) (ContributionPolicy, error) {
	stub.policyCommand = command
	return ContributionPolicy{GroupKind: command.GroupKind, TargetValue: command.TargetValue}, nil
}

func (stub *repositoryStub) IssueContributionReminder(_ context.Context, command IssueContributionReminderCommand) (ContributionReminder, error) {
	stub.reminderCommand = command
	return ContributionReminder{ID: command.RequestID, GroupKind: command.GroupKind}, nil
}

func (stub *repositoryStub) ListMyContributionCycles(_ context.Context, userID uuid.UUID, kind GroupKind, limit int, asOf time.Time) (ContributionCyclePage, error) {
	stub.myCycleUser = userID
	stub.cycleKind = kind
	stub.cycleLimit = limit
	stub.cycleAsOf = asOf
	return ContributionCyclePage{Limit: limit}, nil
}

func (stub *repositoryStub) ListContributionCycles(_ context.Context, kind GroupKind, membershipID uuid.UUID, limit int, asOf time.Time) (ContributionCyclePage, error) {
	stub.cycleMembership = membershipID
	stub.cycleKind = kind
	stub.cycleLimit = limit
	stub.cycleAsOf = asOf
	return ContributionCyclePage{Limit: limit}, nil
}

func (stub *repositoryStub) ListMyTasks(context.Context, uuid.UUID, int, int, time.Time) (TaskAssignmentPage, error) {
	return TaskAssignmentPage{}, nil
}

func (stub *repositoryStub) SubmitTask(_ context.Context, command SubmitTaskCommand) (TaskAssignment, error) {
	stub.submitTaskCommand = command
	return TaskAssignment{ID: command.AssignmentID}, nil
}

func (stub *repositoryStub) ListTasks(context.Context, GroupKind, int, int, time.Time) (TaskPage, error) {
	return TaskPage{}, nil
}

func (stub *repositoryStub) PublishTask(_ context.Context, command PublishTaskCommand) (Task, error) {
	stub.publishTaskCommand = command
	return Task{ID: command.TaskID, GroupKind: command.GroupKind}, nil
}

func (stub *repositoryStub) ListTaskAssignments(context.Context, GroupKind, uuid.UUID, int, int, time.Time) (TaskAssignmentPage, error) {
	return TaskAssignmentPage{}, nil
}

func (stub *repositoryStub) ReviewTaskSubmission(_ context.Context, command ReviewTaskSubmissionCommand) (TaskAssignment, error) {
	stub.reviewTaskCommand = command
	return TaskAssignment{}, nil
}

func (stub *repositoryStub) HasEntitlementAt(_ context.Context, userID uuid.UUID, entitlement Entitlement, at time.Time) (bool, error) {
	stub.entitlementUser = userID
	stub.entitlement = entitlement
	stub.entitlementAt = at
	return true, nil
}

func TestApplyUsesTypedSelfAuthorizationAndImmutableSnapshotTime(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 17, 13, 0, 0, 123456000, time.UTC)
	userID := uuid.New()
	authenticator := &authenticatorStub{session: identity.WebSession{User: identity.User{ID: userID}}}
	authorizer := &authorizerStub{now: now}
	repository := &repositoryStub{}
	service, err := NewService(authenticator, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	requestID := uuid.New()
	application, err := service.Apply(context.Background(), "cookie", "csrf", requestID, GroupReview, "  我理解站点规则并愿意认真参加每一轮种子审核工作。  ")
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	command := repository.applicationCommand
	if application.ID == uuid.Nil || command.RequestID != requestID || command.ApplicantID != userID || command.GroupKind != GroupReview ||
		command.Statement != "我理解站点规则并愿意认真参加每一轮种子审核工作。" || command.OccurredAt != now || command.AuthorizationDecisionID == uuid.Nil {
		t.Fatalf("application=%+v command=%+v", application, command)
	}
	if authenticator.cookieToken != "cookie" || authenticator.csrfToken != "csrf" ||
		authorizer.request.Action != authz.ActionWorkgroupApplicationCreateSelf || authorizer.request.CredentialAudience != authz.AudienceWebSession {
		t.Fatalf("authenticator=%+v authorization=%+v", authenticator, authorizer.request)
	}
}

func TestGrantMembershipUsesStaffMembershipCapability(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 17, 13, 5, 0, 0, time.UTC)
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}}
	authorizer := &authorizerStub{now: now}
	repository := &repositoryStub{}
	service, err := NewService(&authenticatorStub{}, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	requestID := uuid.New()
	_, err = service.GrantMembership(context.Background(), actor, requestID, GroupRetention, 42, "保种任务达标，由管理员批准加入保种工作组。")
	if err != nil {
		t.Fatalf("GrantMembership() error = %v", err)
	}
	if authorizer.request.Action != authz.ActionWorkgroupMembershipManage || authorizer.request.Context.Purpose != "workgroup-membership-grant" ||
		repository.grantCommand.TransitionID != requestID || repository.grantCommand.ActorID != actor.Subject.ID || repository.grantCommand.UserNumericID != 42 {
		t.Fatalf("authorization=%+v command=%+v", authorizer.request, repository.grantCommand)
	}
}

func TestHasEntitlementAtPreservesCallerEventTime(t *testing.T) {
	t.Parallel()
	repository := &repositoryStub{}
	service, err := NewService(&authenticatorStub{}, repository, &authorizerStub{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	userID := uuid.New()
	eventTime := time.Date(2026, time.August, 17, 13, 10, 0, 987654321, time.FixedZone("CST", 8*60*60))
	allowed, err := service.HasEntitlementAt(context.Background(), userID, EntitlementDownloadChargeExempt, eventTime)
	if err != nil || !allowed {
		t.Fatalf("HasEntitlementAt() = %v, %v", allowed, err)
	}
	want := eventTime.UTC().Truncate(time.Microsecond)
	if repository.entitlementUser != userID || repository.entitlement != EntitlementDownloadChargeExempt || repository.entitlementAt != want {
		t.Fatalf("entitlement user=%s entitlement=%s at=%s, want at=%s", repository.entitlementUser, repository.entitlement, repository.entitlementAt, want)
	}
}

func TestIssueContributionPolicyRequiresNextUTCMonthAndTypedCapability(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 17, 13, 10, 0, 0, time.UTC)
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}}
	authorizer := &authorizerStub{now: now}
	repository := &repositoryStub{}
	service, err := NewService(&authenticatorStub{}, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	requestID := uuid.New()
	effectiveFrom := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	_, err = service.IssueContributionPolicy(context.Background(), actor, requestID, GroupReview, 24, effectiveFrom, "九月起提高种审组月度观察目标。")
	if err != nil {
		t.Fatalf("IssueContributionPolicy() error = %v", err)
	}
	if authorizer.request.Action != authz.ActionWorkgroupContributionPolicyIssue ||
		authorizer.request.Context.Purpose != "workgroup-contribution-policy-issue" ||
		repository.policyCommand.RequestID != requestID || repository.policyCommand.TargetValue != 24 ||
		repository.policyCommand.EffectiveFrom != effectiveFrom || repository.policyCommand.ActorID != actor.Subject.ID ||
		repository.policyCommand.AuthorizationDecisionID == uuid.Nil {
		t.Fatalf("authorization=%+v command=%+v", authorizer.request, repository.policyCommand)
	}
}

func TestIssueContributionPolicyRejectsCurrentMonthAndInvalidTargets(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 17, 13, 10, 0, 0, time.UTC)
	service, err := NewService(&authenticatorStub{}, &repositoryStub{}, &authorizerStub{now: now}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}}
	for _, testCase := range []struct {
		kind      GroupKind
		target    int64
		effective time.Time
	}{
		{kind: GroupReview, target: 20, effective: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)},
		{kind: GroupReseed, target: 0, effective: time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)},
		{kind: GroupRetention, target: 1, effective: time.Date(2026, time.September, 1, 9, 0, 0, 0, time.FixedZone("CST", 8*60*60))},
	} {
		_, err := service.IssueContributionPolicy(context.Background(), actor, uuid.New(), testCase.kind, testCase.target, testCase.effective, "这是满足长度要求的签发原因。")
		if !errors.Is(err, ErrInput) {
			t.Fatalf("IssueContributionPolicy(%s, %d, %s) error = %v, want ErrInput", testCase.kind, testCase.target, testCase.effective, err)
		}
	}
}

func TestIssueContributionReminderUsesFrozenCycleCapability(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 17, 13, 10, 0, 0, time.UTC)
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}}
	authorizer := &authorizerStub{now: now}
	repository := &repositoryStub{}
	service, err := NewService(&authenticatorStub{}, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	requestID := uuid.New()
	membershipID := uuid.New()
	periodStart := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	_, err = service.IssueContributionReminder(
		context.Background(), actor, requestID, GroupRetention, membershipID,
		periodStart, "请关注本月保种贡献进度并及时补充做种。",
	)
	if err != nil {
		t.Fatalf("IssueContributionReminder() error = %v", err)
	}
	if authorizer.request.Action != authz.ActionWorkgroupContributionReminderIssue ||
		authorizer.request.Context.Purpose != "workgroup-contribution-reminder-issue" ||
		repository.reminderCommand.RequestID != requestID ||
		repository.reminderCommand.MembershipID != membershipID ||
		repository.reminderCommand.PeriodStartsAt != periodStart ||
		repository.reminderCommand.ActorID != actor.Subject.ID ||
		repository.reminderCommand.AuthorizationDecisionID == uuid.Nil {
		t.Fatalf("authorization=%+v command=%+v", authorizer.request, repository.reminderCommand)
	}
}

func TestContributionCyclesUseAudienceSpecificReadAuthorization(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 17, 13, 30, 0, 123456000, time.UTC)
	userID := uuid.New()
	membershipID := uuid.New()
	authenticator := &authenticatorStub{session: identity.WebSession{User: identity.User{ID: userID}}}
	authorizer := &authorizerStub{now: now}
	repository := &repositoryStub{}
	service, err := NewService(authenticator, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.MyContributionCycles(context.Background(), "cookie", GroupReview, 6); err != nil {
		t.Fatalf("MyContributionCycles() error = %v", err)
	}
	if repository.myCycleUser != userID || repository.cycleKind != GroupReview ||
		repository.cycleLimit != 6 || repository.cycleAsOf != now ||
		authorizer.request.Action != authz.ActionWorkgroupReadSelf ||
		authorizer.request.CredentialAudience != authz.AudienceWebSession {
		t.Fatalf("repository=%+v authorization=%+v", repository, authorizer.request)
	}

	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}}
	if _, err := service.ContributionCycles(context.Background(), actor, GroupRetention, membershipID, 12); err != nil {
		t.Fatalf("ContributionCycles() error = %v", err)
	}
	if repository.cycleMembership != membershipID || repository.cycleKind != GroupRetention ||
		repository.cycleLimit != 12 || repository.cycleAsOf != now ||
		authorizer.request.Action != authz.ActionWorkgroupManageRead ||
		authorizer.request.CredentialAudience != authz.AudienceStaffSession {
		t.Fatalf("repository=%+v authorization=%+v", repository, authorizer.request)
	}
}

func TestPublishTaskFreezesTypedGroupAudience(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 17, 14, 0, 0, 123456000, time.UTC)
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}}
	authorizer := &authorizerStub{now: now}
	repository := &repositoryStub{}
	service, err := NewService(&authenticatorStub{}, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	requestID := uuid.New()
	startsAt := now.Add(time.Hour)
	dueAt := startsAt.Add(7 * 24 * time.Hour)
	_, err = service.PublishTask(
		context.Background(), actor, requestID, GroupReseed, TaskTypeActivity,
		"九月转种协作活动", "请认领缺源资源，并在截止前说明已经完成的转种成果。", startsAt, dueAt,
	)
	if err != nil {
		t.Fatalf("PublishTask() error = %v", err)
	}
	command := repository.publishTaskCommand
	if authorizer.request.Action != authz.ActionWorkgroupTaskPublish ||
		authorizer.request.Context.Purpose != "workgroup-task-publish" ||
		command.TaskID == uuid.Nil || command.RequestID != requestID ||
		command.GroupKind != GroupReseed || command.Type != TaskTypeActivity ||
		command.ActorID != actor.Subject.ID || command.AuthorizationDecisionID == uuid.Nil ||
		command.StartsAt != canonicalTime(startsAt) || command.DueAt != canonicalTime(dueAt) {
		t.Fatalf("authorization=%+v command=%+v", authorizer.request, command)
	}
}

func TestSubmitTaskUsesSelfWriteAuthorization(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 17, 14, 30, 0, 0, time.UTC)
	userID := uuid.New()
	assignmentID := uuid.New()
	authenticator := &authenticatorStub{session: identity.WebSession{User: identity.User{ID: userID}}}
	authorizer := &authorizerStub{now: now}
	repository := &repositoryStub{}
	service, err := NewService(authenticator, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	requestID := uuid.New()
	_, err = service.SubmitTask(
		context.Background(), "cookie", "csrf", requestID, assignmentID,
		"已完成三项资源核对，并附上可供工作人员复核的文字说明。",
	)
	if err != nil {
		t.Fatalf("SubmitTask() error = %v", err)
	}
	command := repository.submitTaskCommand
	if authenticator.cookieToken != "cookie" || authenticator.csrfToken != "csrf" ||
		authorizer.request.Action != authz.ActionWorkgroupTaskSubmitSelf ||
		command.SubmissionID == uuid.Nil || command.RequestID != requestID ||
		command.AssignmentID != assignmentID || command.UserID != userID ||
		command.AuthorizationDecisionID == uuid.Nil || command.OccurredAt != now {
		t.Fatalf("authenticator=%+v authorization=%+v command=%+v", authenticator, authorizer.request, command)
	}
}

func TestReviewTaskSubmissionUsesDedicatedCapability(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 17, 15, 0, 0, 0, time.UTC)
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}}
	authorizer := &authorizerStub{now: now}
	repository := &repositoryStub{}
	service, err := NewService(&authenticatorStub{}, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	requestID := uuid.New()
	submissionID := uuid.New()
	_, err = service.ReviewTaskSubmission(
		context.Background(), actor, requestID, submissionID, TaskReviewAccepted,
		"成果说明完整，已经人工核对并确认通过。",
	)
	if err != nil {
		t.Fatalf("ReviewTaskSubmission() error = %v", err)
	}
	command := repository.reviewTaskCommand
	if authorizer.request.Action != authz.ActionWorkgroupTaskReview ||
		authorizer.request.Context.Purpose != "workgroup-task-review" ||
		command.ReviewID == uuid.Nil || command.RequestID != requestID ||
		command.SubmissionID != submissionID || command.Decision != TaskReviewAccepted ||
		command.ActorID != actor.Subject.ID || command.AuthorizationDecisionID == uuid.Nil {
		t.Fatalf("authorization=%+v command=%+v", authorizer.request, command)
	}
}
