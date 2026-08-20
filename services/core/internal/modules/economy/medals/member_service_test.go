package medals

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type memberMedalAuthenticatorStub struct {
	session    identity.WebSession
	readToken  string
	writeCalls [][2]string
}

func (stub *memberMedalAuthenticatorStub) CurrentSession(_ context.Context, token string) (identity.WebSession, error) {
	stub.readToken = token
	return stub.session, nil
}

func (stub *memberMedalAuthenticatorStub) AuthenticateWrite(_ context.Context, token, csrf string) (identity.WebSession, error) {
	stub.writeCalls = append(stub.writeCalls, [2]string{token, csrf})
	return stub.session, nil
}

type memberMedalRepositoryStub struct {
	overviewUserID uuid.UUID
	purchase       PurchaseCommand
	wear           WearCommand
	priority       PriorityCommand
}

func (stub *memberMedalRepositoryStub) MemberOverview(_ context.Context, userID uuid.UUID, _ time.Time) (MemberOverview, error) {
	stub.overviewUserID = userID
	return MemberOverview{OwnedCount: 2}, nil
}

func (stub *memberMedalRepositoryStub) Purchase(_ context.Context, command PurchaseCommand) (PurchaseReceipt, error) {
	stub.purchase = command
	return PurchaseReceipt{ID: uuid.New(), RequestID: command.RequestID, MedalID: command.MedalID}, nil
}

func (stub *memberMedalRepositoryStub) SetWearing(_ context.Context, command WearCommand) (Holding, error) {
	stub.wear = command
	return Holding{ID: 21, State: "wearing", Version: command.ExpectedVersion + 1}, nil
}

func (stub *memberMedalRepositoryStub) MovePriority(_ context.Context, command PriorityCommand) (Holding, error) {
	stub.priority = command
	return Holding{ID: 21, State: "wearing", Version: command.ExpectedVersion + 1}, nil
}

func TestMemberServiceAuthenticatesAndUsesTypedMedalActions(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 19, 1, 30, 0, 987654321, time.UTC)
	userID := uuid.New()
	sessions := &memberMedalAuthenticatorStub{session: identity.WebSession{User: identity.User{ID: userID}}}
	repository := &memberMedalRepositoryStub{}
	authorizer := &medalAuthorizerStub{}
	service, err := NewMemberService(sessions, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.MyOverview(context.Background(), "read-cookie"); err != nil {
		t.Fatalf("MyOverview() error = %v", err)
	}
	requestID := uuid.New()
	if _, err := service.Purchase(context.Background(), "write-cookie", "csrf-token", requestID, 7); err != nil {
		t.Fatalf("Purchase() error = %v", err)
	}
	if _, err := service.SetWearing(context.Background(), "write-cookie", "csrf-token", 8, 3, true); err != nil {
		t.Fatalf("SetWearing() error = %v", err)
	}
	if _, err := service.MovePriority(context.Background(), "write-cookie", "csrf-token", 8, 4, PriorityUp); err != nil {
		t.Fatalf("MovePriority() error = %v", err)
	}

	if sessions.readToken != "read-cookie" || len(sessions.writeCalls) != 3 {
		t.Fatalf("session calls = %+v", sessions)
	}
	if repository.overviewUserID != userID || repository.purchase.UserID != userID ||
		repository.purchase.RequestID != requestID || repository.purchase.MedalID != 7 {
		t.Fatalf("overview user = %s, purchase = %+v", repository.overviewUserID, repository.purchase)
	}
	if repository.wear.UserID != userID || repository.wear.MedalID != 8 ||
		repository.wear.ExpectedVersion != 3 || !repository.wear.Wearing {
		t.Fatalf("wear command = %+v", repository.wear)
	}
	if repository.priority.UserID != userID || repository.priority.MedalID != 8 ||
		repository.priority.ExpectedVersion != 4 || repository.priority.Direction != PriorityUp {
		t.Fatalf("priority command = %+v", repository.priority)
	}
	wantActions := []authz.Action{
		authz.ActionEconomyMedalReadSelf,
		authz.ActionEconomyMedalPurchaseSelf,
		authz.ActionEconomyMedalWearSelf,
		authz.ActionEconomyMedalWearSelf,
	}
	if len(authorizer.requests) != len(wantActions) {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
	for index, want := range wantActions {
		if authorizer.requests[index].Action != want {
			t.Fatalf("authorization action[%d] = %s, want %s", index, authorizer.requests[index].Action, want)
		}
	}
}
