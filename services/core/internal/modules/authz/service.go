package authz

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

type Repository interface {
	ListPermissionCatalog(context.Context) ([]PersistedPermission, error)
	ListSubjectGrants(context.Context, uuid.UUID) ([]Grant, error)
}

// DecisionRecorder is the mandatory audit boundary for enforcement decisions.
// Implementations must durably append before an allowed request may continue.
type DecisionRecorder interface {
	RecordDecision(context.Context, Request, Decision) error
}

type Service struct {
	repository Repository
	recorder   DecisionRecorder
	policy     Policy
	now        func() time.Time
}

func NewService(repository Repository, recorder DecisionRecorder, now func() time.Time) (*Service, error) {
	if repository == nil {
		return nil, errors.New("authz repository is required")
	}
	if recorder == nil {
		return nil, errors.New("authz decision recorder is required")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{repository: repository, recorder: recorder, policy: NewPolicy(nil), now: now}, nil
}

// ValidateCatalog makes database/code drift a startup failure. A deployment
// cannot introduce an unreviewed action by inserting a string into PostgreSQL,
// nor can it run code whose migration omitted a typed action.
func (service *Service) ValidateCatalog(ctx context.Context) error {
	persisted, err := service.repository.ListPermissionCatalog(ctx)
	if err != nil {
		return fmt.Errorf("load permission catalog: %w", err)
	}
	expected := Catalog()
	if len(persisted) != len(expected) {
		return fmt.Errorf("permission catalog has %d entries, want %d", len(persisted), len(expected))
	}
	for index := range expected {
		want := expected[index]
		got := persisted[index]
		if got.Action != want.Action ||
			got.Description != want.Description ||
			got.Risk != want.Risk ||
			got.Relationship != want.Relationship ||
			got.CredentialAudience != want.CredentialAudience ||
			got.Grantable != want.Grantable ||
			got.Discoverable != want.Discoverable {
			return fmt.Errorf("permission catalog drift at %q", want.Action)
		}
	}
	return nil
}

func (service *Service) Authorize(ctx context.Context, request Request) (Decision, error) {
	grants, err := service.repository.ListSubjectGrants(ctx, request.Subject.ID)
	if err != nil {
		return Decision{}, fmt.Errorf("load subject grants: %w", err)
	}
	if request.Context.Now.IsZero() {
		request.Context.Now = service.now().UTC()
	}
	decision, err := service.evaluateAndRecord(ctx, request, grants)
	if err != nil {
		return decision, err
	}
	if !decision.Allow {
		return decision, DeniedError{Decision: decision}
	}
	return decision, nil
}

// Capabilities returns only currently effective, explicitly discoverable
// actions for the caller. Grant IDs, mandate IDs and other users' authority are
// intentionally absent from this projection.
func (service *Service) Capabilities(ctx context.Context, subject Subject) (CapabilitySet, error) {
	return service.capabilities(ctx, subject, ActionCapabilityReadSelf, AudienceWebSession, time.Time{}, AuthorityBinding{})
}

// StaffCapabilities projects only discoverable staff-audience actions after
// revalidating the exact eligibility authority bound to the short-lived staff
// session. Business actions may still derive from separate scoped grants and
// are independently authorized when invoked.
func (service *Service) StaffCapabilities(ctx context.Context, subject Subject, mfaAt time.Time, requiredAuthority AuthorityBinding) (CapabilitySet, error) {
	return service.capabilities(ctx, subject, ActionStaffCapabilityReadSelf, AudienceStaffSession, mfaAt, requiredAuthority)
}

func (service *Service) capabilities(ctx context.Context, subject Subject, gateAction Action, audience CredentialAudience, mfaAt time.Time, requiredAuthority AuthorityBinding) (CapabilitySet, error) {
	grants, err := service.repository.ListSubjectGrants(ctx, subject.ID)
	if err != nil {
		return CapabilitySet{}, fmt.Errorf("load subject grants: %w", err)
	}
	now := service.now().UTC()
	gateRequest := Request{
		Subject:            subject,
		Action:             gateAction,
		CredentialAudience: audience,
		Resource:           Resource{OwnerID: subject.ID, Scope: SiteScope()},
		Context: EvaluationContext{
			Now:                now,
			MFAAuthenticatedAt: mfaAt,
			RequiredAuthority:  requiredAuthority,
		},
	}
	gate, err := service.evaluateAndRecord(ctx, gateRequest, grants)
	if err != nil {
		return CapabilitySet{}, err
	}
	if !gate.Allow {
		return CapabilitySet{}, DeniedError{Decision: gate}
	}

	capabilities := make(map[string]Capability)
	for _, grant := range grants {
		definition, known := Lookup(grant.Action)
		if !known || !definition.Discoverable || definition.CredentialAudience != audience {
			continue
		}
		// These evaluations derive fields inside the already-authorized response;
		// they are not separate enforcement decisions. The capability gate above
		// is the single audited decision for this use case.
		decision := service.policy.Evaluate(Request{
			Subject:            subject,
			Action:             grant.Action,
			CredentialAudience: definition.CredentialAudience,
			Resource:           Resource{OwnerID: subject.ID, Scope: grant.Scope},
			Context: EvaluationContext{
				Now:                now,
				MFAAuthenticatedAt: mfaAt,
			},
		}, grants)
		if !decision.Allow {
			continue
		}
		key := string(grant.Action) + "\x00" + string(grant.Scope.Type) + "\x00" + grant.Scope.ID
		candidate := Capability{
			Action:      grant.Action,
			Description: definition.Description,
			Scope:       grant.Scope,
			ExpiresAt:   decision.EffectiveUntil,
		}
		current, exists := capabilities[key]
		if !exists || current.ExpiresAt.Before(candidate.ExpiresAt) {
			capabilities[key] = candidate
		}
	}

	items := make([]Capability, 0, len(capabilities))
	for _, capability := range capabilities {
		items = append(items, capability)
	}
	sort.Slice(items, func(left, right int) bool {
		if items[left].Action != items[right].Action {
			return items[left].Action < items[right].Action
		}
		if items[left].Scope.Type != items[right].Scope.Type {
			return items[left].Scope.Type < items[right].Scope.Type
		}
		return items[left].Scope.ID < items[right].Scope.ID
	})
	return CapabilitySet{PolicyVersion: PolicyVersion, Items: items}, nil
}

// evaluateAndRecord is the policy enforcement point. Recording failures are
// returned before allow/deny handling so an allowed operation can never proceed
// without durable audit evidence; callers also do not misreport an unaudited
// denial as an ordinary policy denial.
func (service *Service) evaluateAndRecord(ctx context.Context, request Request, grants []Grant) (Decision, error) {
	decision := service.policy.Evaluate(request, grants)
	if err := service.recorder.RecordDecision(ctx, request, decision); err != nil {
		return decision, fmt.Errorf("record authorization decision: %w", err)
	}
	return decision, nil
}
