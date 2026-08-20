package seedingreward

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	minimumReasonRunes = 10
	maximumReasonRunes = 1000
)

type PublishCommand struct {
	Policy                  PolicyRevision
	IssuedBy                uuid.UUID
	AuthorizationDecisionID uuid.UUID
	Reason                  string
	SnapshotJSON            []byte
}

type PublishedPolicy struct {
	Policy                  PolicyRevision
	IssuedBy                uuid.UUID
	AuthorizationDecisionID uuid.UUID
	Reason                  string
	Replayed                bool
}

type TimelineRepository interface {
	Publish(context.Context, PublishCommand) (PublishedPolicy, error)
	Get(context.Context, string) (PublishedPolicy, error)
	Resolve(context.Context, time.Time) (PublishedPolicy, error)
}

type TimelineService struct {
	repository TimelineRepository
	now        func() time.Time
}

func NewTimelineService(repository TimelineRepository, clocks ...func() time.Time) (*TimelineService, error) {
	if repository == nil {
		return nil, ErrInput
	}
	if len(clocks) > 1 {
		return nil, ErrInput
	}
	now := time.Now
	if len(clocks) == 1 {
		if clocks[0] == nil {
			return nil, ErrInput
		}
		now = clocks[0]
	}
	return &TimelineService{repository: repository, now: now}, nil
}

func (service *TimelineService) Publish(ctx context.Context, policy PolicyRevision, issuedBy, authorizationDecisionID uuid.UUID, reason string) (PublishedPolicy, error) {
	// recorded time is owned by the service boundary; an operator cannot make a
	// newly signed formula appear to have existed before its real publication.
	policy.CreatedAt = canonicalTime(service.now())
	normalized, snapshot, err := NormalizePolicy(policy)
	if err != nil {
		return PublishedPolicy{}, err
	}
	reason = strings.TrimSpace(reason)
	reasonLength := utf8.RuneCountInString(reason)
	if issuedBy == uuid.Nil || authorizationDecisionID == uuid.Nil || !utf8.ValidString(reason) ||
		reasonLength < minimumReasonRunes || reasonLength > maximumReasonRunes {
		return PublishedPolicy{}, ErrInput
	}
	return service.repository.Publish(ctx, PublishCommand{
		Policy: normalized, IssuedBy: issuedBy, AuthorizationDecisionID: authorizationDecisionID,
		Reason: reason, SnapshotJSON: snapshot,
	})
}

func (service *TimelineService) Resolve(ctx context.Context, effectiveAt time.Time) (PublishedPolicy, error) {
	effectiveAt = canonicalTime(effectiveAt)
	if effectiveAt.IsZero() {
		return PublishedPolicy{}, ErrInput
	}
	return service.repository.Resolve(ctx, effectiveAt)
}

func (service *TimelineService) PreviewAt(ctx context.Context, input CalculationInput) (CalculationResult, error) {
	policy, err := service.Resolve(ctx, input.WindowStart)
	if err != nil {
		return CalculationResult{}, err
	}
	return Calculate(policy.Policy, input)
}

func (service *TimelineService) PreviewRevision(ctx context.Context, revision string, input CalculationInput) (CalculationResult, error) {
	revision = strings.TrimSpace(revision)
	if !revisionPattern.MatchString(revision) {
		return CalculationResult{}, ErrInput
	}
	policy, err := service.repository.Get(ctx, revision)
	if err != nil {
		return CalculationResult{}, err
	}
	return Calculate(policy.Policy, input)
}

func (service *TimelineService) BacktestRevision(ctx context.Context, revision string, inputs []CalculationInput) (BacktestReport, error) {
	revision = strings.TrimSpace(revision)
	if !revisionPattern.MatchString(revision) {
		return BacktestReport{}, ErrInput
	}
	policy, err := service.repository.Get(ctx, revision)
	if err != nil {
		return BacktestReport{}, err
	}
	return Backtest(policy.Policy, inputs)
}
