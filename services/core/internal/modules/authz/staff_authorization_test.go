package authz

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAuthorizeStaffActionAllowsRolePolicyToDecideWithoutMandatoryMFA(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 4, 0, 0, 0, time.UTC)
	recorder := &recordingStaffActionAuthorizer{decision: Decision{
		Allow: true, GrantID: uuid.New(), GrantVersion: 1,
		MandateID: uuid.New(), RoleID: "site_admin",
		EffectiveUntil: now.Add(time.Hour),
	}}
	actor := StaffActor{
		Subject: Subject{ID: uuid.New(), Status: SubjectActive},
	}
	decision, err := AuthorizeStaffAction(
		context.Background(), recorder, actor, ActionCategoryManageRead,
		SiteScope(), now, "",
	)
	if err != nil {
		t.Fatalf("AuthorizeStaffAction() error = %v", err)
	}
	if decision.RoleID != "site_admin" {
		t.Fatalf("AuthorizeStaffAction() decision = %+v", decision)
	}
	if !recorder.request.Context.MFAAuthenticatedAt.IsZero() {
		t.Fatalf("account-session administrator invented MFA time %s", recorder.request.Context.MFAAuthenticatedAt)
	}
	if recorder.request.CredentialAudience != AudienceStaffSession || recorder.request.Action != ActionCategoryManageRead {
		t.Fatalf("Authorize() request = %+v", recorder.request)
	}
}

type recordingStaffActionAuthorizer struct {
	request  Request
	decision Decision
}

func (authorizer *recordingStaffActionAuthorizer) Authorize(_ context.Context, request Request) (Decision, error) {
	authorizer.request = request
	return authorizer.decision, nil
}
