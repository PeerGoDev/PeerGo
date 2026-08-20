package hnradmin

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/peergo/peergo/contracts/go/hnrpolicyv1"
	"github.com/peergo/peergo/contracts/go/settlementoperationsv1"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	minimumLeadTime = 5 * time.Minute
	maximumLeadTime = 365 * 24 * time.Hour
)

type CurrentPolicyReader interface {
	Settings(context.Context) (settlementoperationsv1.Settings, error)
}

type Service struct {
	repository Repository
	authorizer authz.Authorizer
	current    CurrentPolicyReader
	now        func() time.Time
}

func NewService(repository Repository, authorizer authz.Authorizer, current CurrentPolicyReader, now func() time.Time) (*Service, error) {
	if repository == nil || authorizer == nil || current == nil {
		return nil, errors.New("H&R administration service dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{repository: repository, authorizer: authorizer, current: current, now: now}, nil
}

func (service *Service) List(ctx context.Context, actor authz.StaffActor, limit, offset int) (Page, error) {
	if limit < 1 || limit > MaxListLimit || offset < 0 || offset > 1_000_000 {
		return Page{}, ErrInput
	}
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionHNRPolicyRead, authz.SiteScope(), now, "hnr-policy-administration"); err != nil {
		return Page{}, err
	}
	page, err := service.repository.List(ctx, limit, offset, now)
	if err != nil {
		return Page{}, err
	}
	// Settlement is the accounting authority, so the administration page shows
	// its currently effective revision instead of guessing from Core delivery
	// state or from a future scheduled revision.
	settings, err := service.current.Settings(ctx)
	if err != nil {
		return Page{}, err
	}
	page.Current = settings.HNR
	page.GlobalRatioConnected = settings.GlobalRatioWatchConnected
	return page, nil
}

func (service *Service) Preview(ctx context.Context, actor authz.StaffActor, input PolicyInput) (Preview, error) {
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionHNRPolicyRead, authz.SiteScope(), now, "hnr-policy-preview"); err != nil {
		return Preview{}, err
	}
	input, err := normalizePolicyInput(input)
	if err != nil {
		return Preview{}, err
	}
	preview := Preview{Policy: input, CompletionAt: now, AssessmentDueAt: now, GraceEndsAt: now}
	if input.Mode == hnrpolicyv1.ModeEnforced {
		preview.AssessmentDueAt = now.Add(time.Duration(input.AssessmentWindowSeconds) * time.Second)
		preview.GraceEndsAt = preview.AssessmentDueAt.Add(time.Duration(input.GracePeriodSeconds) * time.Second)
		if input.RequiredSeedSeconds > 0 {
			at := now.Add(time.Duration(input.RequiredSeedSeconds) * time.Second)
			preview.ContinuousSeedSatisfiedAt = &at
		}
	}
	return preview, nil
}

func (service *Service) Issue(ctx context.Context, actor authz.StaffActor, input IssueInput) (Revision, error) {
	now := service.now().UTC().Round(0)
	input.EffectiveAt = input.EffectiveAt.UTC().Round(0)
	input.Reason = strings.TrimSpace(input.Reason)
	policy, err := normalizePolicyInput(input.Policy)
	if err != nil || input.RevisionID == uuid.Nil || input.EffectiveAt.Before(now.Add(minimumLeadTime)) ||
		input.EffectiveAt.After(now.Add(maximumLeadTime)) || !utf8.ValidString(input.Reason) ||
		utf8.RuneCountInString(input.Reason) < 10 || utf8.RuneCountInString(input.Reason) > 1000 {
		return Revision{}, ErrInput
	}
	input.Policy = policy
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionHNRPolicyIssue, authz.SiteScope(), now, "hnr-policy-administration")
	if err != nil {
		return Revision{}, err
	}
	settings, err := service.current.Settings(ctx)
	if err != nil {
		return Revision{}, err
	}
	baseVersion := settings.HNR.RuleVersion
	var currentPolicy *PolicyInput
	if settings.HNR.Configured {
		value := PolicyInput{
			Mode: hnrpolicyv1.Mode(settings.HNR.Mode), RequiredSeedSeconds: settings.HNR.RequiredSeedSeconds,
			RequiredRatioBasisPoints: settings.HNR.RequiredRatioBasisPoints,
			AssessmentWindowSeconds:  settings.HNR.AssessmentWindowSeconds,
			GracePeriodSeconds:       settings.HNR.GracePeriodSeconds,
			MaxIntervalCreditSeconds: settings.HNR.MaxIntervalCreditSeconds,
		}
		currentPolicy = &value
	}
	return service.repository.Issue(ctx, IssueCommand{
		IssueInput: input, BaseRuleVersion: baseVersion, CurrentPolicy: currentPolicy, ActorID: actor.Subject.ID,
		OccurredAt: now, Authorization: decision,
	})
}

func normalizePolicyInput(input PolicyInput) (PolicyInput, error) {
	if input.Mode == hnrpolicyv1.ModeDisabled || input.Mode == hnrpolicyv1.ModeExempt {
		input.RequiredSeedSeconds = 0
		input.RequiredRatioBasisPoints = 0
		input.AssessmentWindowSeconds = 0
		input.GracePeriodSeconds = 0
		input.MaxIntervalCreditSeconds = 0
	}
	policy := hnrpolicyv1.Policy{
		Rule: hnrpolicyv1.RuleRef{ID: DefaultRuleID, Version: 1}, Mode: input.Mode,
		RequiredSeedSeconds:      input.RequiredSeedSeconds,
		RequiredRatioBasisPoints: input.RequiredRatioBasisPoints,
		AssessmentWindowSeconds:  input.AssessmentWindowSeconds,
		GracePeriodSeconds:       input.GracePeriodSeconds,
		MaxIntervalCreditSeconds: input.MaxIntervalCreditSeconds,
	}
	if hnrpolicyv1.Validate(policy) != nil {
		return PolicyInput{}, ErrInput
	}
	return input, nil
}
