package promotions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type productAuthenticatorStub struct {
	session      identity.WebSession
	currentCalls int
	writeCalls   int
}

func (stub *productAuthenticatorStub) CurrentSession(context.Context, string) (identity.WebSession, error) {
	stub.currentCalls++
	return stub.session, nil
}

func (stub *productAuthenticatorStub) AuthenticateWrite(context.Context, string, string) (identity.WebSession, error) {
	stub.writeCalls++
	return stub.session, nil
}

type productRepositoryStub struct {
	offer         ProductOffer
	order         ProductOrder
	page          ProductOrderPage
	policy        ProductPolicy
	offerCalls    int
	purchaseCalls int
	listCalls     int
	updateCalls   int
	command       ProductPurchaseCommand
	update        UpdateProductPolicyCommand
}

func (stub *productRepositoryStub) ProductOffer(_ context.Context, _ uuid.UUID, _ int64, _ time.Time) (ProductOffer, error) {
	stub.offerCalls++
	return stub.offer, nil
}

func (stub *productRepositoryStub) PurchaseProduct(_ context.Context, command ProductPurchaseCommand) (ProductOrder, error) {
	stub.purchaseCalls++
	stub.command = command
	return stub.order, nil
}

func (stub *productRepositoryStub) ListProductOrders(context.Context, ProductOrderQuery) (ProductOrderPage, error) {
	stub.listCalls++
	return stub.page, nil
}

func (stub *productRepositoryStub) CurrentProductPolicy(context.Context, time.Time) (ProductPolicy, error) {
	return stub.policy, nil
}

func (stub *productRepositoryStub) UpdateProductPolicy(_ context.Context, command UpdateProductPolicyCommand) (ProductPolicy, error) {
	stub.updateCalls++
	stub.update = command
	return stub.policy, nil
}

func TestProductPurchaseUsesVerifiedMemberCapabilityAndTypedSelection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 16, 9, 0, 0, 123456789, time.UTC)
	verifiedAt := now.Add(-time.Hour)
	buyerID := uuid.New()
	orderID := uuid.New()
	promotion := PromotionDoubleUploadFree
	authenticator := &productAuthenticatorStub{session: identity.WebSession{User: identity.User{
		ID: buyerID, EmailVerifiedAt: &verifiedAt,
	}}}
	repository := &productRepositoryStub{order: ProductOrder{ID: orderID}}
	authorizer := &promotionAuthorizerStub{decision: promotionAllowedDecision(now)}
	service, err := NewProductService(authenticator, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	order, err := service.Purchase(context.Background(), "cookie", "csrf", orderID, 42, ProductSelection{
		Promotion: &promotion, PromotionDays: 3, StickyDays: 2,
	})
	if err != nil || order.ID != orderID {
		t.Fatalf("Purchase() = %+v, %v", order, err)
	}
	if authenticator.writeCalls != 1 || repository.purchaseCalls != 1 || len(authorizer.requests) != 1 {
		t.Fatalf("calls = authn %d repository %d authz %d", authenticator.writeCalls, repository.purchaseCalls, len(authorizer.requests))
	}
	if authorizer.requests[0].Action != authz.ActionTorrentPromotionPurchaseSelf ||
		repository.command.OrderID != orderID || repository.command.BuyerID != buyerID ||
		repository.command.TorrentID != 42 || repository.command.Selection.Promotion != &promotion ||
		repository.command.Selection.PromotionDays != 3 || repository.command.Selection.StickyDays != 2 ||
		!repository.command.Now.Equal(now.Truncate(time.Microsecond)) ||
		repository.command.AuthorizationID != authorizer.decision.ID {
		t.Fatalf("purchase command = %+v, authorization = %+v", repository.command, authorizer.requests[0])
	}
}

func TestProductPurchaseRejectsUnverifiedEmailBeforeAuthorization(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 16, 9, 0, 0, 0, time.UTC)
	authenticator := &productAuthenticatorStub{session: identity.WebSession{User: identity.User{ID: uuid.New()}}}
	repository := &productRepositoryStub{}
	authorizer := &promotionAuthorizerStub{decision: promotionAllowedDecision(now)}
	service, err := NewProductService(authenticator, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	err = func() error {
		_, purchaseErr := service.Purchase(context.Background(), "cookie", "csrf", uuid.New(), 42, ProductSelection{StickyDays: 1})
		return purchaseErr
	}()
	if !errors.Is(err, ErrEmailUnverified) {
		t.Fatalf("Purchase() error = %v, want ErrEmailUnverified", err)
	}
	if len(authorizer.requests) != 0 || repository.purchaseCalls != 0 {
		t.Fatalf("unverified purchase reached authorization=%d repository=%d", len(authorizer.requests), repository.purchaseCalls)
	}
}

func TestProductPurchaseRejectsEmptyOrInconsistentSelectionBeforeSession(t *testing.T) {
	t.Parallel()

	service, err := NewProductService(
		&productAuthenticatorStub{}, &productRepositoryStub{}, &promotionAuthorizerStub{}, time.Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	promotion := PromotionFree
	for _, selection := range []ProductSelection{
		{},
		{Promotion: &promotion},
		{PromotionDays: 1, StickyDays: 1},
	} {
		if _, err := service.Purchase(context.Background(), "", "", uuid.New(), 1, selection); !errors.Is(err, ErrInput) {
			t.Fatalf("Purchase(%+v) error = %v, want ErrInput", selection, err)
		}
	}
}

func TestProductPolicyUpdateUsesAppendOnlyRevisionCommand(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 16, 9, 0, 0, 0, time.UTC)
	repository := &productRepositoryStub{policy: ProductPolicy{Revision: "promotion-products-v2"}}
	authorizer := &promotionAuthorizerStub{decision: promotionAllowedDecision(now)}
	service, err := NewProductService(&productAuthenticatorStub{}, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	requestID := uuid.New()
	actor := promotionTestActor()
	policy, err := service.UpdateProductPolicy(context.Background(), actor, UpdateProductPolicyCommand{
		RequestID: requestID, ExpectedRevision: " promotion-products-v1 ", PromotionEnabled: true, StickyEnabled: true,
		FreePricePerDay: 50, DoubleUploadPricePerDay: 30, DoubleUploadFreePricePerDay: 80,
		HalfDownloadPricePerDay: 25, DoubleUploadHalfDownloadPricePerDay: 55,
		ThirtyPercentDownloadPricePerDay: 35, StickyPricePerDay: 200,
		MaxPromotionDays: 30, MaxStickyDays: 30, Reason: "  根据站点运营情况调整成员购买定价。  ",
	})
	if err != nil || policy.Revision != "promotion-products-v2" {
		t.Fatalf("UpdateProductPolicy() = %+v, %v", policy, err)
	}
	if repository.updateCalls != 1 || len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionPromotionSchedule {
		t.Fatalf("calls = repository %d authorization %+v", repository.updateCalls, authorizer.requests)
	}
	if repository.update.RequestID != requestID || repository.update.ActorID != actor.Subject.ID ||
		repository.update.ExpectedRevision != "promotion-products-v1" || repository.update.Reason != "根据站点运营情况调整成员购买定价。" ||
		repository.update.AuthorizationID != authorizer.decision.ID || !repository.update.OccurredAt.Equal(now) {
		t.Fatalf("update command = %+v", repository.update)
	}
}
