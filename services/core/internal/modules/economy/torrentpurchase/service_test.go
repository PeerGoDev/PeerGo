package torrentpurchase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type purchaseSessionStub struct {
	session identity.WebSession
}

func (stub purchaseSessionStub) CurrentSession(context.Context, string) (identity.WebSession, error) {
	return stub.session, nil
}

func (stub purchaseSessionStub) AuthenticateWrite(context.Context, string, string) (identity.WebSession, error) {
	return stub.session, nil
}

type purchaseAuthorizerStub struct {
	now      time.Time
	requests []authz.Request
}

func (stub *purchaseAuthorizerStub) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	stub.requests = append(stub.requests, request)
	return authz.Decision{
		ID: uuid.New(), Allow: true, GrantID: uuid.New(), GrantVersion: 1,
		MandateID: uuid.New(), RoleID: "member", EffectiveUntil: stub.now.Add(time.Hour),
	}, nil
}

type purchaseRepositoryStub struct {
	status        Status
	err           error
	userID        uuid.UUID
	torrent       int64
	command       PurchaseCommand
	adminQuery    AdminPurchaseQuery
	refundCommand RefundCommand
}

func (stub *purchaseRepositoryStub) Status(_ context.Context, userID uuid.UUID, torrentID int64, _ time.Time) (Status, error) {
	stub.userID, stub.torrent = userID, torrentID
	return stub.status, stub.err
}

func (stub *purchaseRepositoryStub) Purchase(_ context.Context, command PurchaseCommand) (Receipt, error) {
	stub.command = command
	return Receipt{TorrentID: command.TorrentID}, stub.err
}

func (stub *purchaseRepositoryStub) ListHistory(context.Context, HistoryQuery) (HistoryPage, error) {
	return HistoryPage{}, stub.err
}

func (stub *purchaseRepositoryStub) CurrentPolicy(context.Context, time.Time) (PolicySettings, error) {
	return PolicySettings{}, stub.err
}

func (stub *purchaseRepositoryStub) UpdatePolicy(context.Context, UpdatePolicyCommand) (PolicySettings, error) {
	return PolicySettings{}, stub.err
}

func (stub *purchaseRepositoryStub) UpdatePrice(context.Context, UpdatePriceCommand) (PriceChange, error) {
	return PriceChange{}, stub.err
}

func (stub *purchaseRepositoryStub) ListPurchases(_ context.Context, query AdminPurchaseQuery) (AdminPurchasePage, error) {
	stub.adminQuery = query
	return AdminPurchasePage{}, stub.err
}

func (stub *purchaseRepositoryStub) Refund(_ context.Context, command RefundCommand) (RefundReceipt, error) {
	stub.refundCommand = command
	return RefundReceipt{BuyerNumericID: command.BuyerNumericID, TorrentID: command.TorrentID}, stub.err
}

func TestServiceUsesTypedPurchaseCapabilities(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 16, 5, 0, 0, 0, time.UTC)
	userID := uuid.New()
	repository := &purchaseRepositoryStub{status: Status{State: AccessPurchased}}
	authorizer := &purchaseAuthorizerStub{now: now}
	service, err := NewService(
		purchaseSessionStub{session: identity.WebSession{User: identity.User{ID: userID}}},
		repository,
		authorizer,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.MyStatus(context.Background(), "cookie", 42); err != nil {
		t.Fatalf("MyStatus() error = %v", err)
	}
	requestID := uuid.New()
	if _, err := service.Purchase(context.Background(), "cookie", "csrf", requestID, 42); err != nil {
		t.Fatalf("Purchase() error = %v", err)
	}
	if repository.userID != userID || repository.torrent != 42 || repository.command.UserID != userID || repository.command.RequestID != requestID {
		t.Fatalf("repository calls status=%s/%d purchase=%+v", repository.userID, repository.torrent, repository.command)
	}
	if len(authorizer.requests) != 2 || authorizer.requests[0].Action != authz.ActionTorrentPurchaseReadSelf || authorizer.requests[1].Action != authz.ActionTorrentPurchaseCreateSelf {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}

func TestRequireDownloadAccessFailsClosedForPricedTorrent(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	for _, test := range []struct {
		state AccessState
		want  error
	}{
		{state: AccessFree},
		{state: AccessUploader},
		{state: AccessPurchased},
		{state: AccessPurchaseRequired, want: ErrPurchaseRequired},
		{state: AccessPurchaseDisabled, want: ErrPurchaseDisabled},
	} {
		t.Run(string(test.state), func(t *testing.T) {
			repository := &purchaseRepositoryStub{status: Status{State: test.state}}
			service, _ := NewService(
				purchaseSessionStub{}, repository, &purchaseAuthorizerStub{}, time.Now,
			)
			err := service.RequireDownloadAccess(context.Background(), userID, 7)
			if !errors.Is(err, test.want) {
				t.Fatalf("RequireDownloadAccess() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestServiceUsesSeparateStaffPurchaseReadAndRefundCapabilities(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 16, 6, 0, 0, 0, time.UTC)
	actorID := uuid.New()
	repository := &purchaseRepositoryStub{}
	authorizer := &purchaseAuthorizerStub{now: now}
	service, err := NewService(purchaseSessionStub{}, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	actor := authz.StaffActor{Subject: authz.Subject{ID: actorID, Status: authz.SubjectActive}}
	query := AdminPurchaseQuery{Status: AdminPurchaseStatusAll, Source: AdminPurchaseSourceAll, Limit: 20}
	if _, err := service.AdminHistory(context.Background(), actor, query); err != nil {
		t.Fatalf("AdminHistory() error = %v", err)
	}
	requestID := uuid.New()
	if _, err := service.RefundPurchase(context.Background(), actor, RefundCommand{
		RequestID: requestID, BuyerNumericID: 327, TorrentID: 1234,
		Reason: "管理员核实重复购买后执行全额退款",
	}); err != nil {
		t.Fatalf("RefundPurchase() error = %v", err)
	}
	if repository.adminQuery != query {
		t.Fatalf("admin query = %+v", repository.adminQuery)
	}
	if repository.refundCommand.RequestID != requestID || repository.refundCommand.ActorID != actorID ||
		repository.refundCommand.BuyerNumericID != 327 || repository.refundCommand.TorrentID != 1234 ||
		!repository.refundCommand.OccurredAt.Equal(now) || repository.refundCommand.AuthorizationID == uuid.Nil {
		t.Fatalf("refund command = %+v", repository.refundCommand)
	}
	if len(authorizer.requests) != 2 || authorizer.requests[0].Action != authz.ActionTorrentPurchaseManageRead ||
		authorizer.requests[1].Action != authz.ActionTorrentPurchaseManageRefund {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}

func TestRoundedBasisPointsUsesIntegerHalfUp(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		amount, basisPoints, want int64
	}{
		{amount: 100, basisPoints: 1000, want: 10},
		{amount: 5, basisPoints: 1000, want: 1},
		{amount: 4, basisPoints: 1000, want: 0},
		{amount: 100, basisPoints: 0, want: 0},
	} {
		if got := roundedBasisPoints(test.amount, test.basisPoints); got != test.want {
			t.Fatalf("roundedBasisPoints(%d, %d) = %d, want %d", test.amount, test.basisPoints, got, test.want)
		}
	}
}
