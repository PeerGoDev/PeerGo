package hnradmin

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/contracts/go/hnrpolicyv1"
	"github.com/peergo/peergo/contracts/go/settlementoperationsv1"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type serviceRepositoryStub struct {
	page    Page
	command IssueCommand
}

func (stub *serviceRepositoryStub) List(context.Context, int, int, time.Time) (Page, error) {
	return stub.page, nil
}

func (stub *serviceRepositoryStub) Issue(_ context.Context, command IssueCommand) (Revision, error) {
	stub.command = command
	return Revision{
		ID: command.RevisionID,
		Policy: hnrpolicyv1.Policy{
			Rule:                     hnrpolicyv1.RuleRef{ID: DefaultRuleID, Version: command.BaseRuleVersion + 1},
			Mode:                     command.Policy.Mode,
			RequiredSeedSeconds:      command.Policy.RequiredSeedSeconds,
			RequiredRatioBasisPoints: command.Policy.RequiredRatioBasisPoints,
			AssessmentWindowSeconds:  command.Policy.AssessmentWindowSeconds,
			GracePeriodSeconds:       command.Policy.GracePeriodSeconds,
			MaxIntervalCreditSeconds: command.Policy.MaxIntervalCreditSeconds,
		},
		EffectiveAt: command.EffectiveAt,
	}, nil
}

type serviceAuthorizerStub struct {
	now      time.Time
	requests []authz.Request
}

func (stub *serviceAuthorizerStub) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	stub.requests = append(stub.requests, request)
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed,
		PolicyVersion: authz.PolicyVersion, RoleID: "site_admin",
		GrantID: uuid.New(), GrantVersion: 1, MandateID: uuid.New(),
		EffectiveUntil: stub.now.Add(24 * time.Hour),
	}, nil
}

type currentPolicyStub struct {
	settings settlementoperationsv1.Settings
}

func (stub currentPolicyStub) Settings(context.Context) (settlementoperationsv1.Settings, error) {
	return stub.settings, nil
}

func TestListReturnsSettlementCurrentPolicyAlongsideCoreHistory(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 16, 9, 0, 0, 0, time.UTC)
	repository := &serviceRepositoryStub{page: Page{Total: 2, Limit: 20}}
	authorizer := &serviceAuthorizerStub{now: now}
	service, err := NewService(repository, authorizer, currentPolicyStub{settings: settlementoperationsv1.Settings{
		GeneratedAt: now,
		HNR: settlementoperationsv1.HNRPolicy{
			Configured: true, RevisionID: uuid.NewString(), EffectiveAt: &now,
			RuleID: DefaultRuleID, RuleVersion: 4, Mode: string(hnrpolicyv1.ModeEnforced),
			RequiredSeedSeconds: 259_200, RequiredRatioBasisPoints: 10_000,
			AssessmentWindowSeconds: 604_800, GracePeriodSeconds: 86_400,
			MaxIntervalCreditSeconds: 3600,
		},
		GlobalRatioWatchConnected: false,
	}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}}
	page, err := service.List(context.Background(), actor, 20, 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if !page.Current.Configured || page.Current.RuleVersion != 4 || page.GlobalRatioConnected {
		t.Fatalf("page current = %+v, ratio connected = %v", page.Current, page.GlobalRatioConnected)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionHNRPolicyRead {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}

func TestPreviewUsesOneCompletionInstantWithoutWriting(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 16, 9, 0, 0, 0, time.UTC)
	repository := &serviceRepositoryStub{}
	authorizer := &serviceAuthorizerStub{now: now}
	service, err := NewService(repository, authorizer, currentPolicyStub{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}}
	preview, err := service.Preview(context.Background(), actor, PolicyInput{
		Mode: hnrpolicyv1.ModeEnforced, RequiredSeedSeconds: 72 * 3600,
		RequiredRatioBasisPoints: 10_000, AssessmentWindowSeconds: 7 * 24 * 3600,
		GracePeriodSeconds: 24 * 3600, MaxIntervalCreditSeconds: 3600,
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if !preview.CompletionAt.Equal(now) || !preview.AssessmentDueAt.Equal(now.Add(7*24*time.Hour)) ||
		!preview.GraceEndsAt.Equal(now.Add(8*24*time.Hour)) || preview.ContinuousSeedSatisfiedAt == nil ||
		!preview.ContinuousSeedSatisfiedAt.Equal(now.Add(72*time.Hour)) {
		t.Fatalf("preview = %+v", preview)
	}
	if repository.command.RevisionID != uuid.Nil {
		t.Fatal("preview unexpectedly wrote a policy revision")
	}
}

func TestIssueUsesSettlementVersionAndPersistsAuthorizationEvidence(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 16, 9, 0, 0, 0, time.UTC)
	repository := &serviceRepositoryStub{}
	authorizer := &serviceAuthorizerStub{now: now}
	service, err := NewService(repository, authorizer, currentPolicyStub{settings: settlementoperationsv1.Settings{
		GeneratedAt: now,
		HNR: settlementoperationsv1.HNRPolicy{
			Configured: true, RuleID: DefaultRuleID, RuleVersion: 7,
			Mode: string(hnrpolicyv1.ModeEnforced), RequiredSeedSeconds: 72 * 3600,
			RequiredRatioBasisPoints: 10_000, AssessmentWindowSeconds: 7 * 24 * 3600,
			GracePeriodSeconds: 24 * 3600, MaxIntervalCreditSeconds: 3600,
		},
	}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	revisionID := uuid.New()
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}}
	revision, err := service.Issue(context.Background(), actor, IssueInput{
		RevisionID: revisionID,
		Policy: PolicyInput{
			Mode: hnrpolicyv1.ModeEnforced, RequiredSeedSeconds: 96 * 3600,
			RequiredRatioBasisPoints: 10_000, AssessmentWindowSeconds: 7 * 24 * 3600,
			GracePeriodSeconds: 24 * 3600, MaxIntervalCreditSeconds: 3600,
		},
		EffectiveAt: now.Add(10 * time.Minute), Reason: "根据站点保种情况调整最低做种时间。",
	})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	command := repository.command
	if revision.ID != revisionID || command.BaseRuleVersion != 7 || command.CurrentPolicy == nil ||
		command.ActorID != actor.Subject.ID || command.Authorization.ID == uuid.Nil {
		t.Fatalf("issue command = %+v, revision = %+v", command, revision)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionHNRPolicyIssue {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}
