package authz

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type memoryRepository struct {
	catalog []PersistedPermission
	grants  []Grant
}

type recordedDecision struct {
	request  Request
	decision Decision
}

type memoryDecisionRecorder struct {
	records []recordedDecision
	err     error
}

func (recorder *memoryDecisionRecorder) RecordDecision(_ context.Context, request Request, decision Decision) error {
	if recorder.err != nil {
		return recorder.err
	}
	recorder.records = append(recorder.records, recordedDecision{request: request, decision: decision})
	return nil
}

func (repository memoryRepository) ListPermissionCatalog(context.Context) ([]PersistedPermission, error) {
	return append([]PersistedPermission(nil), repository.catalog...), nil
}

func (repository memoryRepository) ListSubjectGrants(context.Context, uuid.UUID) ([]Grant, error) {
	return append([]Grant(nil), repository.grants...), nil
}

func TestServiceValidatesCatalogAndDiscoversOnlyEffectiveCapabilities(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	subjectID := uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111")
	mandate := Mandate{
		ID:        uuid.MustParse("0198f20a-6da8-7e51-9c64-333333333333"),
		SubjectID: subjectID,
		Scope:     SiteScope(),
		StartsAt:  now.Add(-time.Hour),
		EndsAt:    now.Add(90 * time.Minute),
		Status:    MandateActive,
	}
	actions := []Action{ActionSessionRevokeSelf, ActionCapabilityReadSelf, ActionSessionReadSelf}
	grants := make([]Grant, 0, len(actions))
	for index, action := range actions {
		grants = append(grants, Grant{
			ID: uuid.MustParse([]string{
				"0198f20a-6da8-7e51-9c64-444444444441",
				"0198f20a-6da8-7e51-9c64-444444444442",
				"0198f20a-6da8-7e51-9c64-444444444443",
			}[index]),
			SubjectID:  subjectID,
			RoleID:     "member",
			Action:     action,
			Scope:      SiteScope(),
			ValidFrom:  now.Add(-30 * time.Minute),
			ValidUntil: mandate.EndsAt,
			Version:    1,
			Mandate:    mandate,
		})
	}

	repository := memoryRepository{catalog: persistedCatalog(), grants: grants}
	recorder := &memoryDecisionRecorder{}
	service, err := NewService(repository, recorder, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if err := service.ValidateCatalog(context.Background()); err != nil {
		t.Fatalf("ValidateCatalog() error = %v", err)
	}

	result, err := service.Capabilities(context.Background(), Subject{ID: subjectID, Status: SubjectActive})
	if err != nil {
		t.Fatalf("Capabilities() error = %v", err)
	}
	if result.PolicyVersion != PolicyVersion || len(result.Items) != 3 {
		t.Fatalf("Capabilities() = %+v", result)
	}
	wantActions := []Action{ActionCapabilityReadSelf, ActionSessionReadSelf, ActionSessionRevokeSelf}
	for index, wantAction := range wantActions {
		if result.Items[index].Action != wantAction || !result.Items[index].ExpiresAt.Equal(mandate.EndsAt) {
			t.Fatalf("capability[%d] = %+v, want action=%s expiry=%s", index, result.Items[index], wantAction, mandate.EndsAt)
		}
	}
	if len(recorder.records) != 1 {
		t.Fatalf("recorded decisions = %+v, want one capability gate", recorder.records)
	}
	recorded := recorder.records[0]
	if recorded.request.Action != ActionCapabilityReadSelf || !recorded.decision.Allow || recorded.decision.RoleID != "member" || recorded.decision.MandateID != mandate.ID {
		t.Fatalf("recorded decision = %+v, want allowed capability gate with authority", recorded)
	}
}

func TestServiceFailsClosedForCatalogDriftAndRevokedDiscoveryGrant(t *testing.T) {
	t.Parallel()

	catalog := persistedCatalog()
	catalog[0].Description = "drifted"
	service, err := NewService(memoryRepository{catalog: catalog}, &memoryDecisionRecorder{}, time.Now)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if err := service.ValidateCatalog(context.Background()); err == nil {
		t.Fatal("ValidateCatalog() error = nil, want drift failure")
	}

	now := time.Now().UTC()
	subjectID := uuid.New()
	revokedAt := now.Add(-time.Minute)
	grant := Grant{
		ID:         uuid.New(),
		SubjectID:  subjectID,
		RoleID:     "member",
		Action:     ActionCapabilityReadSelf,
		Scope:      SiteScope(),
		ValidFrom:  now.Add(-time.Hour),
		ValidUntil: now.Add(time.Hour),
		Version:    1,
		RevokedAt:  &revokedAt,
		Mandate: Mandate{
			ID: uuid.New(), SubjectID: subjectID, Scope: SiteScope(),
			StartsAt: now.Add(-2 * time.Hour), EndsAt: now.Add(2 * time.Hour), Status: MandateActive,
		},
	}
	recorder := &memoryDecisionRecorder{}
	service, err = NewService(memoryRepository{catalog: persistedCatalog(), grants: []Grant{grant}}, recorder, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	_, err = service.Capabilities(context.Background(), Subject{ID: subjectID, Status: SubjectActive})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Capabilities() error = %v, want ErrForbidden", err)
	}
	if len(recorder.records) != 1 || recorder.records[0].decision.Reason != ReasonGrantRevoked {
		t.Fatalf("recorded decisions = %+v, want revoked denial", recorder.records)
	}
}

func TestServiceFailsClosedWhenDecisionAuditCannotPersist(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	subjectID := uuid.New()
	mandate := Mandate{
		ID: uuid.New(), SubjectID: subjectID, Scope: SiteScope(),
		StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour), Status: MandateActive,
	}
	grant := Grant{
		ID: uuid.New(), SubjectID: subjectID, RoleID: "member",
		Action: ActionCapabilityReadSelf, Scope: SiteScope(), Version: 1,
		ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour), Mandate: mandate,
	}
	recorderFailure := errors.New("outbox unavailable")
	service, err := NewService(
		memoryRepository{catalog: persistedCatalog(), grants: []Grant{grant}},
		&memoryDecisionRecorder{err: recorderFailure},
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	_, err = service.Capabilities(context.Background(), Subject{ID: subjectID, Status: SubjectActive})
	if !errors.Is(err, recorderFailure) || errors.Is(err, ErrForbidden) {
		t.Fatalf("Capabilities() error = %v, want audit failure without policy denial", err)
	}
}

func TestStaffCapabilitiesRevalidatesBoundAuthorityAndFiltersWebActions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	mfaAt := now.Add(-2 * time.Minute)
	subjectID := uuid.New()
	mandate := Mandate{
		ID: uuid.New(), SubjectID: subjectID, Scope: SiteScope(),
		StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour), Status: MandateActive,
	}
	staffAccessGrantID := uuid.New()
	businessGrantID := uuid.New()
	grant := func(id uuid.UUID, role string, action Action, constraints Constraints) Grant {
		return Grant{
			ID: id, SubjectID: subjectID, RoleID: role, Action: action,
			Scope: SiteScope(), Version: 3, ValidFrom: now.Add(-time.Hour),
			ValidUntil: now.Add(time.Hour), Constraints: constraints, Mandate: mandate,
		}
	}
	grants := []Grant{
		grant(staffAccessGrantID, "staff_access", ActionStaffCapabilityReadSelf, Constraints{}),
		grant(staffAccessGrantID, "staff_access", ActionStaffSessionReadSelf, Constraints{}),
		grant(staffAccessGrantID, "staff_access", ActionStaffCredentialEnrollSelf, Constraints{}),
		grant(businessGrantID, "grant_proposer", ActionGrantRead, Constraints{MFAMaxAgeSeconds: 900}),
		grant(businessGrantID, "grant_proposer", ActionGrantRevokePropose, Constraints{MFAMaxAgeSeconds: 900}),
	}
	recorder := &memoryDecisionRecorder{}
	service, err := NewService(memoryRepository{catalog: persistedCatalog(), grants: grants}, recorder, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	binding := AuthorityBinding{GrantID: staffAccessGrantID, GrantVersion: 3, MandateID: mandate.ID}
	result, err := service.StaffCapabilities(context.Background(), Subject{ID: subjectID, Status: SubjectActive}, mfaAt, binding)
	if err != nil {
		t.Fatalf("StaffCapabilities() error = %v", err)
	}
	wantActions := []Action{ActionGrantRead, ActionGrantRevokePropose}
	if len(result.Items) != len(wantActions) {
		t.Fatalf("StaffCapabilities() items = %+v", result.Items)
	}
	for index, want := range wantActions {
		if result.Items[index].Action != want {
			t.Fatalf("StaffCapabilities()[%d] = %q, want %q", index, result.Items[index].Action, want)
		}
	}
	if len(recorder.records) != 1 || recorder.records[0].request.Action != ActionStaffCapabilityReadSelf || recorder.records[0].request.Context.RequiredAuthority != binding || recorder.records[0].request.CredentialAudience != AudienceStaffSession {
		t.Fatalf("staff capability gate record = %+v", recorder.records)
	}

	_, err = service.StaffCapabilities(
		context.Background(),
		Subject{ID: subjectID, Status: SubjectActive},
		mfaAt,
		AuthorityBinding{GrantID: staffAccessGrantID, GrantVersion: 2, MandateID: mandate.ID},
	)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("StaffCapabilities(stale authority) error = %v, want ErrForbidden", err)
	}
}

func TestServiceAuditsAuthorityBindingInvalidationBeforeDenying(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	subjectID := uuid.New()
	mandateID := uuid.New()
	grantID := uuid.New()
	grant := Grant{
		ID:         grantID,
		SubjectID:  subjectID,
		RoleID:     "staff_access",
		Action:     ActionStaffSessionReadSelf,
		Scope:      SiteScope(),
		Version:    8,
		ValidFrom:  now.Add(-time.Hour),
		ValidUntil: now.Add(time.Hour),
		Mandate: Mandate{
			ID: mandateID, SubjectID: subjectID, Scope: SiteScope(),
			StartsAt: now.Add(-2 * time.Hour), EndsAt: now.Add(2 * time.Hour), Status: MandateActive,
		},
	}
	recorder := &memoryDecisionRecorder{}
	service, err := NewService(memoryRepository{grants: []Grant{grant}}, recorder, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	decision, err := service.Authorize(context.Background(), Request{
		Subject:            Subject{ID: subjectID, Status: SubjectActive},
		Action:             ActionStaffSessionReadSelf,
		CredentialAudience: AudienceStaffSession,
		Resource:           Resource{OwnerID: subjectID, Scope: SiteScope()},
		Context: EvaluationContext{
			Now:                now,
			MFAAuthenticatedAt: now,
			RequiredAuthority: AuthorityBinding{
				GrantID: grantID, GrantVersion: 7, MandateID: mandateID,
			},
		},
	})
	if !errors.Is(err, ErrForbidden) || decision.Reason != ReasonAuthorityBindingMismatch {
		t.Fatalf("Authorize() decision=%+v error=%v", decision, err)
	}
	if len(recorder.records) != 1 || recorder.records[0].decision.Reason != ReasonAuthorityBindingMismatch {
		t.Fatalf("recorded decisions = %+v, want authority binding denial", recorder.records)
	}
}

func persistedCatalog() []PersistedPermission {
	definitions := Catalog()
	result := make([]PersistedPermission, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, PersistedPermission{
			Action:             definition.Action,
			Description:        definition.Description,
			Risk:               definition.Risk,
			Relationship:       definition.Relationship,
			CredentialAudience: definition.CredentialAudience,
			Grantable:          definition.Grantable,
			Discoverable:       definition.Discoverable,
		})
	}
	return result
}
