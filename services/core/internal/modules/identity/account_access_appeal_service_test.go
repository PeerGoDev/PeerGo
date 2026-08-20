package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type accountAccessAppealVaultStub struct {
	credentialRef uuid.UUID
	verifyInput   LoginInput
	enabledRef    uuid.UUID
	order         *[]string
}

func (stub *accountAccessAppealVaultStub) VerifyForAccountAppeal(_ context.Context, input LoginInput) (uuid.UUID, error) {
	stub.verifyInput = input
	return stub.credentialRef, nil
}

func (stub *accountAccessAppealVaultStub) EnableAfterAccountAppeal(_ context.Context, credentialRef uuid.UUID) error {
	stub.enabledRef = credentialRef
	if stub.order != nil {
		*stub.order = append(*stub.order, "vault-enable")
	}
	return nil
}

type accountAccessAppealRepositoryStub struct {
	status        AccountAccessStatus
	userID        uuid.UUID
	submitCommand SubmitAccountAccessAppealCommand
	submitResult  AccountAccessAppeal
	preflight     AccountAccessAppealDecisionPreflight
	decideCommand DecideAccountAccessAppealCommand
	decideResult  AccountAccessAppeal
	order         *[]string
}

func (stub *accountAccessAppealRepositoryStub) StatusByCredentialRef(context.Context, uuid.UUID, time.Time) (AccountAccessStatus, uuid.UUID, error) {
	return stub.status, stub.userID, nil
}

func (stub *accountAccessAppealRepositoryStub) SubmitAccountAccessAppeal(_ context.Context, command SubmitAccountAccessAppealCommand) (AccountAccessAppeal, error) {
	stub.submitCommand = command
	return stub.submitResult, nil
}

func (stub *accountAccessAppealRepositoryStub) ListAccountAccessAppeals(context.Context, AccountAccessAppealQuery, time.Time) (AccountAccessAppealPage, error) {
	return AccountAccessAppealPage{}, nil
}

func (stub *accountAccessAppealRepositoryStub) AccountAccessAppealDecisionPreflight(context.Context, uuid.UUID, time.Time) (AccountAccessAppealDecisionPreflight, error) {
	return stub.preflight, nil
}

func (stub *accountAccessAppealRepositoryStub) DecideAccountAccessAppeal(_ context.Context, command DecideAccountAccessAppealCommand) (AccountAccessAppeal, error) {
	stub.decideCommand = command
	if stub.order != nil {
		*stub.order = append(*stub.order, "core-decision")
	}
	return stub.decideResult, nil
}

type accountAccessAppealAuthorizerStub struct{}

func (accountAccessAppealAuthorizerStub) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed,
		PolicyVersion: authz.PolicyVersion, GrantID: uuid.New(), GrantVersion: 1,
		RoleID: "user_access_operator", MandateID: uuid.New(),
		EffectiveUntil: request.Context.Now.Add(time.Hour),
	}, nil
}

func TestAccountAccessAppealSubmitDelegatesIdempotentReplayToRepository(t *testing.T) {
	now := time.Date(2026, time.August, 17, 3, 0, 0, 0, time.UTC)
	credentialRef, userID, appealID := uuid.New(), uuid.New(), uuid.New()
	vault := &accountAccessAppealVaultStub{credentialRef: credentialRef}
	repository := &accountAccessAppealRepositoryStub{
		userID: userID,
		status: AccountAccessStatus{
			Restricted: true,
			CanAppeal:  false,
			Appeal:     &AccountAccessAppeal{ID: appealID},
		},
		submitResult: AccountAccessAppeal{ID: appealID, Replayed: true},
	}
	service, err := NewAccountAccessAppealService(vault, repository, accountAccessAppealAuthorizerStub{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.SubmitAccountAccessAppeal(context.Background(), SubmitAccountAccessAppealInput{
		AppealID: appealID,
		Credentials: AccountAccessCredentials{
			Identifier: " member ", Password: "secret", SecondFactorCode: " 123456 ",
		},
		Statement: "  这是同一个请求在网络中断后的安全重试说明。  ",
	})
	if err != nil || result.ID != appealID || !result.Replayed {
		t.Fatalf("SubmitAccountAccessAppeal() = %+v, %v", result, err)
	}
	if repository.submitCommand.AppealID != appealID || repository.submitCommand.UserID != userID ||
		repository.submitCommand.Statement != "这是同一个请求在网络中断后的安全重试说明。" ||
		vault.verifyInput.Identifier != "member" || vault.verifyInput.SecondFactorCode != "123456" {
		t.Fatalf("submit=%+v verify=%+v", repository.submitCommand, vault.verifyInput)
	}
}

func TestAccountAccessAppealDisabledApprovalEnablesVaultBeforeCoreDecision(t *testing.T) {
	now := time.Date(2026, time.August, 17, 3, 10, 0, 0, time.UTC)
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}}
	credentialRef, appealID, targetID := uuid.New(), uuid.New(), uuid.New()
	order := []string{}
	vault := &accountAccessAppealVaultStub{credentialRef: credentialRef, order: &order}
	repository := &accountAccessAppealRepositoryStub{
		preflight: AccountAccessAppealDecisionPreflight{
			UserID: targetID, CredentialRef: credentialRef,
			SourceKind: AccountAccessSourceDisabledAccount, SourceVersion: 7,
		},
		decideResult: AccountAccessAppeal{ID: appealID, Status: AccountAccessAppealApproved},
		order:        &order,
	}
	service, _ := NewAccountAccessAppealService(vault, repository, accountAccessAppealAuthorizerStub{}, func() time.Time { return now })
	result, err := service.DecideAccountAccessAppeal(context.Background(), actor, DecideAccountAccessAppealInput{
		AppealID: appealID, Decision: AccountAccessAppealDecisionApprove,
		ExpectedSourceVersion: 7, Response: "复核材料一致，同意恢复该账户的登录访问。",
	})
	if err != nil || result.Status != AccountAccessAppealApproved {
		t.Fatalf("DecideAccountAccessAppeal() = %+v, %v", result, err)
	}
	if len(order) != 2 || order[0] != "vault-enable" || order[1] != "core-decision" || vault.enabledRef != credentialRef {
		t.Fatalf("decision order=%v enabled=%s", order, vault.enabledRef)
	}
	if repository.decideCommand.Authorization.ID == uuid.Nil || !repository.decideCommand.Authorization.Allow {
		t.Fatalf("decision evidence=%+v", repository.decideCommand.Authorization)
	}
}

func TestAccountAccessAppealRejectDoesNotEnableCredentialAndSelfDecisionIsDenied(t *testing.T) {
	now := time.Date(2026, time.August, 17, 3, 20, 0, 0, time.UTC)
	actorID, appealID := uuid.New(), uuid.New()
	vault := &accountAccessAppealVaultStub{credentialRef: uuid.New()}
	repository := &accountAccessAppealRepositoryStub{preflight: AccountAccessAppealDecisionPreflight{
		UserID: actorID, CredentialRef: vault.credentialRef,
		SourceKind: AccountAccessSourceDisabledAccount, SourceVersion: 2,
	}}
	service, _ := NewAccountAccessAppealService(vault, repository, accountAccessAppealAuthorizerStub{}, func() time.Time { return now })
	_, err := service.DecideAccountAccessAppeal(context.Background(), authz.StaffActor{
		Subject: authz.Subject{ID: actorID, Status: authz.SubjectActive},
	}, DecideAccountAccessAppealInput{
		AppealID: appealID, Decision: AccountAccessAppealDecisionReject,
		ExpectedSourceVersion: 2, Response: "不能由处理人员审批自己的账户访问申诉。",
	})
	if !errors.Is(err, ErrAccountAccessAppealSelfTarget) || vault.enabledRef != uuid.Nil {
		t.Fatalf("self decision error=%v enabled=%s", err, vault.enabledRef)
	}
}
