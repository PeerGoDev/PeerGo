package seedingreward

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type timelineRepositoryStub struct {
	published PublishCommand
	policies  map[string]PublishedPolicy
	resolved  PublishedPolicy
	err       error
}

func (repository *timelineRepositoryStub) Publish(_ context.Context, command PublishCommand) (PublishedPolicy, error) {
	repository.published = command
	if repository.err != nil {
		return PublishedPolicy{}, repository.err
	}
	return PublishedPolicy{
		Policy: command.Policy, IssuedBy: command.IssuedBy,
		AuthorizationDecisionID: command.AuthorizationDecisionID, Reason: command.Reason,
	}, nil
}

func (repository *timelineRepositoryStub) Get(_ context.Context, revision string) (PublishedPolicy, error) {
	if repository.err != nil {
		return PublishedPolicy{}, repository.err
	}
	policy, ok := repository.policies[revision]
	if !ok {
		return PublishedPolicy{}, ErrPolicyNotFound
	}
	return policy, nil
}

func (repository *timelineRepositoryStub) Resolve(_ context.Context, _ time.Time) (PublishedPolicy, error) {
	if repository.err != nil {
		return PublishedPolicy{}, repository.err
	}
	return repository.resolved, nil
}

func TestTimelineServicePublishesCanonicalFuturePolicy(t *testing.T) {
	repository := &timelineRepositoryStub{}
	policy := testPolicy()
	fixedNow := policy.EffectiveFrom.Add(-time.Hour)
	service, err := NewTimelineService(repository, func() time.Time { return fixedNow })
	if err != nil {
		t.Fatalf("NewTimelineService() error = %v", err)
	}
	issuer, decision := uuid.New(), uuid.New()
	result, err := service.Publish(context.Background(), policy, issuer, decision, "  首版做种奖励政策离线回放基线  ")
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if result.Policy.CreatedAt != fixedNow || result.Policy.SnapshotSHA256 == ([32]byte{}) || len(repository.published.SnapshotJSON) == 0 ||
		repository.published.Reason != "首版做种奖励政策离线回放基线" {
		t.Fatalf("published = %+v", repository.published)
	}
}

func TestTimelineServiceRejectsUnsignedOrBackdatedPolicy(t *testing.T) {
	policy := testPolicy()
	fixedNow := policy.EffectiveFrom.Add(-time.Hour)
	service, _ := NewTimelineService(&timelineRepositoryStub{}, func() time.Time { return fixedNow })
	tests := map[string]func() (PublishedPolicy, error){
		"missing issuer": func() (PublishedPolicy, error) {
			return service.Publish(context.Background(), policy, uuid.Nil, uuid.New(), "足够长度的签发原因文本")
		},
		"short reason": func() (PublishedPolicy, error) {
			return service.Publish(context.Background(), policy, uuid.New(), uuid.New(), "太短")
		},
		"backdated": func() (PublishedPolicy, error) {
			backdated := policy
			backdated.EffectiveFrom = fixedNow.Add(-time.Hour)
			return service.Publish(context.Background(), backdated, uuid.New(), uuid.New(), "足够长度的签发原因文本")
		},
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := run(); !errors.Is(err, ErrInput) {
				t.Fatalf("Publish() error = %v, want ErrInput", err)
			}
		})
	}
}

func TestTimelinePreviewUsesResolvedOrExplicitRevision(t *testing.T) {
	policy, _, err := NormalizePolicy(testPolicy())
	if err != nil {
		t.Fatalf("NormalizePolicy() error = %v", err)
	}
	repository := &timelineRepositoryStub{
		resolved: PublishedPolicy{Policy: policy},
		policies: map[string]PublishedPolicy{policy.Revision: {Policy: policy}},
	}
	service, _ := NewTimelineService(repository)
	byTime, err := service.PreviewAt(context.Background(), testCalculationInput())
	if err != nil {
		t.Fatalf("PreviewAt() error = %v", err)
	}
	byRevision, err := service.PreviewRevision(context.Background(), policy.Revision, testCalculationInput())
	if err != nil {
		t.Fatalf("PreviewRevision() error = %v", err)
	}
	if byTime.CalculationSHA256 != byRevision.CalculationSHA256 {
		t.Fatalf("preview paths diverged: %x != %x", byTime.CalculationSHA256, byRevision.CalculationSHA256)
	}
}
