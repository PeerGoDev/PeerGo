package progression

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type levelPolicyRepositoryStub struct {
	overview        []LevelPolicyRevision
	issuedCommand   issueLevelPolicyCommand
	issueResult     LevelPolicyRevision
	issueErr        error
	activations     []LevelPolicyActivation
	activationCalls int
}

func (stub *levelPolicyRepositoryStub) LevelPolicyOverview(context.Context, time.Time, int) ([]LevelPolicyRevision, error) {
	return append([]LevelPolicyRevision(nil), stub.overview...), nil
}

func (stub *levelPolicyRepositoryStub) IssueLevelPolicy(_ context.Context, command issueLevelPolicyCommand) (LevelPolicyRevision, error) {
	stub.issuedCommand = command
	return stub.issueResult, stub.issueErr
}

func (stub *levelPolicyRepositoryStub) ActivateDueLevelPolicy(context.Context, time.Time) (LevelPolicyActivation, bool, error) {
	call := stub.activationCalls
	stub.activationCalls++
	if call >= len(stub.activations) {
		return LevelPolicyActivation{}, false, nil
	}
	return stub.activations[call], true, nil
}

type levelPolicyAuthorizerStub struct {
	request  authz.Request
	decision authz.Decision
}

func (stub *levelPolicyAuthorizerStub) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	stub.request = request
	return stub.decision, nil
}

func TestLevelPolicyServiceIssuesCanonicalFutureRevision(t *testing.T) {
	now := time.Date(2026, time.August, 18, 8, 15, 0, 0, time.UTC)
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}}
	repository := &levelPolicyRepositoryStub{issueResult: LevelPolicyRevision{PolicyVersion: "issued"}}
	authorizer := &levelPolicyAuthorizerStub{decision: allowedLevelPolicyDecision(now)}
	service, err := NewLevelPolicyService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewLevelPolicyService() error = %v", err)
	}

	requestID := uuid.New()
	result, err := service.IssueLevelPolicy(context.Background(), actor, IssueLevelPolicyInput{
		RequestID: requestID, ExpectedSequence: 1,
		EffectiveAt: time.Date(2026, time.August, 18, 11, 0, 0, 0, time.UTC),
		Reason:      "调整站点等级门槛并保留历史版本。",
		Levels: []LevelRule{
			{Level: 1, MinimumExperience: 0, KarmaBonusBPS: 0, SeedingCountBonus: 0, CurrentUserCount: 99},
			{Level: 2, MinimumExperience: 1_000, KarmaBonusBPS: 200, SeedingCountBonus: 5, CurrentUserCount: 12},
		},
	})
	if err != nil {
		t.Fatalf("IssueLevelPolicy() error = %v", err)
	}
	if result.PolicyVersion != "issued" {
		t.Fatalf("IssueLevelPolicy() result = %+v", result)
	}
	command := repository.issuedCommand
	if command.ActorID != actor.Subject.ID || command.RequestID != requestID || command.Authorization.ID == uuid.Nil {
		t.Fatalf("issued command evidence = %+v", command)
	}
	if command.Levels[0].CurrentUserCount != 0 || command.Levels[1].CurrentUserCount != 0 {
		t.Fatalf("issued rules retained projection counts: %+v", command.Levels)
	}
	if len(command.SnapshotJSON) == 0 || command.SnapshotSHA256 == [32]byte{} {
		t.Fatal("canonical snapshot evidence was not generated")
	}
	if authorizer.request.Action != authz.ActionProgressionLevelPolicyIssue || authorizer.request.Context.Purpose != "level-policy-issue" {
		t.Fatalf("authorization request = %+v", authorizer.request)
	}
}

func TestNormalizeLevelPolicyRejectsBrokenLadders(t *testing.T) {
	now := time.Date(2026, time.August, 18, 8, 15, 0, 0, time.UTC)
	valid := IssueLevelPolicyInput{
		RequestID: uuid.New(), ExpectedSequence: 1,
		EffectiveAt: time.Date(2026, time.August, 18, 11, 0, 0, 0, time.UTC),
		Reason:      "调整站点等级门槛并保留历史版本。",
		Levels: []LevelRule{
			{Level: 1, MinimumExperience: 0, KarmaBonusBPS: 0, SeedingCountBonus: 0},
			{Level: 2, MinimumExperience: 1_000, KarmaBonusBPS: 200, SeedingCountBonus: 5},
		},
	}
	tests := map[string]func(IssueLevelPolicyInput) IssueLevelPolicyInput{
		"non-contiguous level": func(input IssueLevelPolicyInput) IssueLevelPolicyInput {
			input.Levels[1].Level = 3
			return input
		},
		"decreasing experience": func(input IssueLevelPolicyInput) IssueLevelPolicyInput {
			input.Levels[1].MinimumExperience = 0
			return input
		},
		"decreasing benefit": func(input IssueLevelPolicyInput) IssueLevelPolicyInput {
			input.Levels[0].KarmaBonusBPS = 300
			return input
		},
		"not whole hour": func(input IssueLevelPolicyInput) IssueLevelPolicyInput {
			input.EffectiveAt = input.EffectiveAt.Add(time.Minute)
			return input
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			input := valid
			input.Levels = append([]LevelRule(nil), valid.Levels...)
			if _, _, _, err := normalizeIssueLevelPolicyInput(mutate(input), now); !errors.Is(err, ErrLevelPolicyInput) {
				t.Fatalf("normalizeIssueLevelPolicyInput() error = %v, want ErrLevelPolicyInput", err)
			}
		})
	}
}

func TestLevelPolicyWorkerDrainsBoundedDueRevisions(t *testing.T) {
	repository := &levelPolicyRepositoryStub{activations: []LevelPolicyActivation{
		{PolicyVersion: "level-2"},
		{PolicyVersion: "level-3"},
	}}
	worker, err := NewLevelPolicyWorker(repository, LevelPolicyWorkerConfig{MaximumBatch: 4}, nil, nil)
	if err != nil {
		t.Fatalf("NewLevelPolicyWorker() error = %v", err)
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if repository.activationCalls != 3 {
		t.Fatalf("activation calls = %d, want 3", repository.activationCalls)
	}
}

func allowedLevelPolicyDecision(now time.Time) authz.Decision {
	return authz.Decision{
		ID: uuid.New(), Allow: true, GrantID: uuid.New(), GrantVersion: 1,
		MandateID: uuid.New(), RoleID: "site_admin", EffectiveUntil: now.Add(time.Hour),
	}
}
