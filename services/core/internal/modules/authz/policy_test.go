package authz

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPolicyMatrixDefaultsToDeny(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	subjectID := uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111")
	baseRequest := Request{
		Subject:            Subject{ID: subjectID, Status: SubjectActive},
		Action:             ActionCapabilityReadSelf,
		CredentialAudience: AudienceWebSession,
		Resource:           Resource{OwnerID: subjectID, Scope: SiteScope()},
		Context:            EvaluationContext{Now: now},
	}
	baseGrant := Grant{
		ID:         uuid.MustParse("0198f20a-6da8-7e51-9c64-444444444444"),
		SubjectID:  subjectID,
		RoleID:     "member",
		Action:     ActionCapabilityReadSelf,
		Scope:      SiteScope(),
		ValidFrom:  now.Add(-time.Hour),
		ValidUntil: now.Add(2 * time.Hour),
		Version:    1,
		Mandate: Mandate{
			ID:        uuid.MustParse("0198f20a-6da8-7e51-9c64-333333333333"),
			SubjectID: subjectID,
			Scope:     SiteScope(),
			StartsAt:  now.Add(-2 * time.Hour),
			EndsAt:    now.Add(3 * time.Hour),
			Status:    MandateActive,
		},
	}

	tests := []struct {
		name       string
		wantAllow  bool
		wantReason ReasonCode
		mutate     func(*Request, *Grant)
	}{
		{name: "valid grant", wantAllow: true, wantReason: ReasonAllowed},
		{
			name: "unknown action", wantReason: ReasonActionUnknown,
			mutate: func(request *Request, _ *Grant) { request.Action = "unreviewed.superpower" },
		},
		{
			name: "inactive subject wins", wantReason: ReasonSubjectInactive,
			mutate: func(request *Request, _ *Grant) { request.Subject.Status = SubjectFrozen },
		},
		{
			name: "credential audience mismatch", wantReason: ReasonCredentialAudienceMismatch,
			mutate: func(request *Request, _ *Grant) { request.CredentialAudience = AudienceAnonymous },
		},
		{
			name: "self relationship mismatch", wantReason: ReasonRelationshipMismatch,
			mutate: func(request *Request, _ *Grant) { request.Resource.OwnerID = uuid.New() },
		},
		{
			name: "grant missing", wantReason: ReasonGrantMissing,
			mutate: func(_ *Request, grant *Grant) { grant.Action = ActionSessionReadSelf },
		},
		{
			name: "grant revoked", wantReason: ReasonGrantRevoked,
			mutate: func(_ *Request, grant *Grant) { revokedAt := now; grant.RevokedAt = &revokedAt },
		},
		{
			name: "grant expired", wantReason: ReasonGrantExpired,
			mutate: func(_ *Request, grant *Grant) { grant.ValidUntil = now },
		},
		{
			name: "mandate suspended", wantReason: ReasonMandateInactive,
			mutate: func(_ *Request, grant *Grant) { grant.Mandate.Status = MandateSuspended },
		},
		{
			name: "grant exceeds mandate", wantReason: ReasonGrantInvariant,
			mutate: func(_ *Request, grant *Grant) { grant.ValidUntil = grant.Mandate.EndsAt.Add(time.Second) },
		},
		{
			name: "grant authority evidence invalid", wantReason: ReasonGrantInvariant,
			mutate: func(_ *Request, grant *Grant) { grant.Version = 0 },
		},
		{
			name: "scope mismatch", wantReason: ReasonScopeMismatch,
			mutate: func(request *Request, _ *Grant) {
				request.Resource.Scope = Scope{Type: ScopeCategory, ID: "movies"}
			},
		},
		{
			name: "purpose missing", wantReason: ReasonContextMissing,
			mutate: func(_ *Request, grant *Grant) { grant.Constraints.PurposeRequired = true },
		},
		{
			name: "recent mfa missing", wantReason: ReasonContextMissing,
			mutate: func(_ *Request, grant *Grant) { grant.Constraints.MFAMaxAgeSeconds = 300 },
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := baseRequest
			grant := baseGrant
			if test.mutate != nil {
				test.mutate(&request, &grant)
			}
			policy := NewPolicy(func() uuid.UUID {
				return uuid.MustParse("0198f20a-6da8-7e51-9c64-555555555555")
			})
			decision := policy.Evaluate(request, []Grant{grant})
			if decision.Allow != test.wantAllow || decision.Reason != test.wantReason {
				t.Fatalf("decision = %+v, want allow=%t reason=%s", decision, test.wantAllow, test.wantReason)
			}
			if decision.ID == uuid.Nil || decision.PolicyVersion != PolicyVersion {
				t.Fatalf("decision metadata = %+v", decision)
			}
		})
	}
}

func TestStaffSessionAudienceAndRecentWebAuthnRemainNonInterchangeable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	subjectID := uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111")
	mandate := Mandate{
		ID:        uuid.MustParse("0198f20a-6da8-7e51-9c64-333333333333"),
		SubjectID: subjectID,
		Scope:     SiteScope(),
		StartsAt:  now.Add(-time.Hour),
		EndsAt:    now.Add(time.Hour),
		Status:    MandateActive,
	}
	readGrant := Grant{
		ID:          uuid.MustParse("0198f20a-6da8-7e51-9c64-444444444444"),
		SubjectID:   subjectID,
		RoleID:      "staff_access",
		Action:      ActionStaffSessionReadSelf,
		Scope:       SiteScope(),
		ValidFrom:   now.Add(-time.Hour),
		ValidUntil:  now.Add(time.Hour),
		Constraints: Constraints{MFAMaxAgeSeconds: 300},
		Version:     1,
		Mandate:     mandate,
	}
	request := Request{
		Subject:            Subject{ID: subjectID, Status: SubjectActive},
		Action:             ActionStaffSessionReadSelf,
		CredentialAudience: AudienceStaffSession,
		Resource:           Resource{OwnerID: subjectID, Scope: SiteScope()},
		Context: EvaluationContext{
			Now:                now,
			MFAAuthenticatedAt: now.Add(-time.Minute),
		},
	}
	policy := NewPolicy(nil)
	if decision := policy.Evaluate(request, []Grant{readGrant}); !decision.Allow || decision.Reason != ReasonAllowed {
		t.Fatalf("valid staff decision = %+v", decision)
	}

	webAudience := request
	webAudience.CredentialAudience = AudienceWebSession
	if decision := policy.Evaluate(webAudience, []Grant{readGrant}); decision.Allow || decision.Reason != ReasonCredentialAudienceMismatch {
		t.Fatalf("Web cookie reading staff session decision = %+v", decision)
	}

	missingMFA := request
	missingMFA.Context.MFAAuthenticatedAt = time.Time{}
	if decision := policy.Evaluate(missingMFA, []Grant{readGrant}); decision.Allow || decision.Reason != ReasonContextMissing {
		t.Fatalf("staff session without recent WebAuthn decision = %+v", decision)
	}

	createGrant := readGrant
	createGrant.Action = ActionStaffSessionCreateSelf
	createGrant.Constraints = Constraints{}
	staffAudienceCreate := request
	staffAudienceCreate.Action = ActionStaffSessionCreateSelf
	if decision := policy.Evaluate(staffAudienceCreate, []Grant{createGrant}); decision.Allow || decision.Reason != ReasonCredentialAudienceMismatch {
		t.Fatalf("staff cookie self-elevation decision = %+v", decision)
	}
}

func TestPolicyRequiresTheExactAuthorityThatIssuedAStaffSession(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	subjectID := uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111")
	grantID := uuid.MustParse("0198f20a-6da8-7e51-9c64-444444444444")
	mandateID := uuid.MustParse("0198f20a-6da8-7e51-9c64-333333333333")
	request := Request{
		Subject:            Subject{ID: subjectID, Status: SubjectActive},
		Action:             ActionStaffSessionReadSelf,
		CredentialAudience: AudienceStaffSession,
		Resource:           Resource{OwnerID: subjectID, Scope: SiteScope()},
		Context: EvaluationContext{
			Now:                now,
			MFAAuthenticatedAt: now.Add(-time.Minute),
			RequiredAuthority: AuthorityBinding{
				GrantID:      grantID,
				GrantVersion: 7,
				MandateID:    mandateID,
			},
		},
	}
	grant := Grant{
		ID:         grantID,
		SubjectID:  subjectID,
		RoleID:     "staff_access",
		Action:     ActionStaffSessionReadSelf,
		Scope:      SiteScope(),
		ValidFrom:  now.Add(-time.Hour),
		ValidUntil: now.Add(time.Hour),
		Version:    7,
		Mandate: Mandate{
			ID:        mandateID,
			SubjectID: subjectID,
			Scope:     SiteScope(),
			StartsAt:  now.Add(-2 * time.Hour),
			EndsAt:    now.Add(2 * time.Hour),
			Status:    MandateActive,
		},
	}
	policy := NewPolicy(nil)

	if decision := policy.Evaluate(request, []Grant{grant}); !decision.Allow || decision.Reason != ReasonAllowed {
		t.Fatalf("exact authority decision = %+v", decision)
	}

	versionChanged := grant
	versionChanged.Version++
	if decision := policy.Evaluate(request, []Grant{versionChanged}); decision.Allow || decision.Reason != ReasonAuthorityBindingMismatch {
		t.Fatalf("changed grant version decision = %+v", decision)
	}

	replacement := grant
	replacement.ID = uuid.MustParse("0198f20a-6da8-7e51-9c64-555555555555")
	if decision := policy.Evaluate(request, []Grant{replacement}); decision.Allow || decision.Reason != ReasonAuthorityBindingMismatch {
		t.Fatalf("replacement grant decision = %+v", decision)
	}

	revoked := grant
	revokedAt := now
	revoked.RevokedAt = &revokedAt
	if decision := policy.Evaluate(request, []Grant{replacement, revoked}); decision.Allow || decision.Reason != ReasonGrantRevoked {
		t.Fatalf("revoked exact grant plus replacement decision = %+v", decision)
	}

	missingPermission := grant
	missingPermission.Action = ActionSessionReadSelf
	if decision := policy.Evaluate(request, []Grant{missingPermission}); decision.Allow || decision.Reason != ReasonAuthorityBindingMismatch {
		t.Fatalf("bound grant without action decision = %+v", decision)
	}

	partialBinding := request
	partialBinding.Context.RequiredAuthority.MandateID = uuid.Nil
	if decision := policy.Evaluate(partialBinding, []Grant{grant}); decision.Allow || decision.Reason != ReasonContextMissing {
		t.Fatalf("partial authority binding decision = %+v", decision)
	}
}
