package progression

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type contributionPolicyRepositoryStub struct {
	items         []ContributionExperiencePolicy
	issuedCommand contributionExperiencePolicyCommand
	issueResult   ContributionExperiencePolicy
}

func (stub *contributionPolicyRepositoryStub) ListContributionExperiencePolicies(context.Context, int, int) ([]ContributionExperiencePolicy, int64, error) {
	return append([]ContributionExperiencePolicy(nil), stub.items...), int64(len(stub.items)), nil
}

func (stub *contributionPolicyRepositoryStub) IssueContributionExperiencePolicy(_ context.Context, command contributionExperiencePolicyCommand) (ContributionExperiencePolicy, error) {
	stub.issuedCommand = command
	return stub.issueResult, nil
}

func TestContributionExperiencePolicyServiceIssuesCanonicalFutureRevision(t *testing.T) {
	now := time.Date(2026, time.August, 20, 8, 15, 0, 0, time.UTC)
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}}
	repository := &contributionPolicyRepositoryStub{
		issueResult: ContributionExperiencePolicy{Revision: "peergo-contribution-2026082011"},
	}
	authorizer := &levelPolicyAuthorizerStub{decision: allowedLevelPolicyDecision(now)}
	service, err := NewContributionExperiencePolicyService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewContributionExperiencePolicyService() error = %v", err)
	}

	result, err := service.Issue(context.Background(), actor, ContributionExperiencePolicyInput{
		Revision:                     " peergo-contribution-2026082011 ",
		EffectiveFrom:                time.Date(2026, time.August, 20, 11, 0, 0, 0, time.UTC),
		ExperiencePerUploadGiBMilli:  100,
		ExperiencePerTorrentMilli:    2_000,
		ExperiencePerAccountDayMilli: 1_000,
	}, "  调整三项基础经验获取参数并保留历史账本。  ")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if result.Revision != "peergo-contribution-2026082011" {
		t.Fatalf("Issue() result = %+v", result)
	}
	command := repository.issuedCommand
	if command.Policy.IssuedBy == nil || *command.Policy.IssuedBy != actor.Subject.ID || command.AuthorizationDecisionID == uuid.Nil {
		t.Fatalf("issued command evidence = %+v", command)
	}
	if command.Policy.Revision != "peergo-contribution-2026082011" || command.Policy.Reason != "调整三项基础经验获取参数并保留历史账本。" {
		t.Fatalf("canonical command = %+v", command.Policy)
	}
	if len(command.SnapshotJSON) == 0 || command.Policy.SnapshotSHA256 == [32]byte{} {
		t.Fatal("canonical snapshot evidence was not generated")
	}
	if authorizer.request.Action != authz.ActionProgressionContributionPolicyIssue || authorizer.request.Context.Purpose != "contribution-experience-policy-issue" {
		t.Fatalf("authorization request = %+v", authorizer.request)
	}
}

func TestNormalizeContributionExperiencePolicyRejectsInvalidValues(t *testing.T) {
	valid := ContributionExperiencePolicy{
		Revision:                     "contribution-v2",
		EffectiveFrom:                time.Date(2026, time.August, 20, 11, 0, 0, 0, time.UTC),
		ExperiencePerUploadGiBMilli:  100,
		ExperiencePerTorrentMilli:    2_000,
		ExperiencePerAccountDayMilli: 1_000,
		Reason:                       "调整三项基础经验获取参数并保留历史账本。",
		CreatedAt:                    time.Date(2026, time.August, 20, 8, 15, 0, 0, time.UTC),
	}
	tests := map[string]func(ContributionExperiencePolicy) ContributionExperiencePolicy{
		"revision too long for linked authorities": func(policy ContributionExperiencePolicy) ContributionExperiencePolicy {
			policy.Revision = "a" + strings.Repeat("b", maximumContributionPolicyRevision)
			return policy
		},
		"non-hour boundary": func(policy ContributionExperiencePolicy) ContributionExperiencePolicy {
			policy.EffectiveFrom = policy.EffectiveFrom.Add(time.Minute)
			return policy
		},
		"negative upload experience": func(policy ContributionExperiencePolicy) ContributionExperiencePolicy {
			policy.ExperiencePerUploadGiBMilli = -1
			return policy
		},
		"created after effective": func(policy ContributionExperiencePolicy) ContributionExperiencePolicy {
			policy.CreatedAt = policy.EffectiveFrom
			return policy
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, err := normalizeContributionExperiencePolicy(mutate(valid)); !errors.Is(err, ErrContributionPolicyInput) {
				t.Fatalf("normalizeContributionExperiencePolicy() error = %v, want ErrContributionPolicyInput", err)
			}
		})
	}
}
