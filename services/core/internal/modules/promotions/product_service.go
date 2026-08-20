package promotions

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type ProductSessionAuthenticator interface {
	CurrentSession(context.Context, string) (identity.WebSession, error)
	AuthenticateWrite(context.Context, string, string) (identity.WebSession, error)
}

type ProductService struct {
	authenticator ProductSessionAuthenticator
	repository    ProductRepository
	authorizer    authz.Authorizer
	now           func() time.Time
}

func NewProductService(authenticator ProductSessionAuthenticator, repository ProductRepository, authorizer authz.Authorizer, now func() time.Time) (*ProductService, error) {
	if authenticator == nil || repository == nil || authorizer == nil {
		return nil, errors.New("promotion product dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &ProductService{authenticator: authenticator, repository: repository, authorizer: authorizer, now: now}, nil
}

func (service *ProductService) Offer(ctx context.Context, cookieToken string, torrentID int64) (ProductOffer, error) {
	if torrentID < 1 {
		return ProductOffer{}, ErrInput
	}
	now := canonicalProductTime(service.now())
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return ProductOffer{}, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionTorrentPromotionPurchaseSelf, now); err != nil {
		return ProductOffer{}, err
	}
	return service.repository.ProductOffer(ctx, session.User.ID, torrentID, now)
}

func (service *ProductService) Purchase(ctx context.Context, cookieToken, csrfToken string, orderID uuid.UUID, torrentID int64, selection ProductSelection) (ProductOrder, error) {
	if orderID == uuid.Nil || torrentID < 1 || !validProductSelection(selection) {
		return ProductOrder{}, ErrInput
	}
	now := canonicalProductTime(service.now())
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return ProductOrder{}, err
	}
	if session.User.EmailVerifiedAt == nil {
		return ProductOrder{}, ErrEmailUnverified
	}
	decision, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionTorrentPromotionPurchaseSelf, now)
	if err != nil {
		return ProductOrder{}, err
	}
	return service.repository.PurchaseProduct(ctx, ProductPurchaseCommand{
		OrderID: orderID, BuyerID: session.User.ID, TorrentID: torrentID,
		Selection: selection, Now: now, AuthorizationID: decision.ID, Authorization: decision,
	})
}

func (service *ProductService) MyOrders(ctx context.Context, cookieToken string, limit, offset int) (ProductOrderPage, error) {
	if !validProductPage(limit, offset) {
		return ProductOrderPage{}, ErrInput
	}
	now := canonicalProductTime(service.now())
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return ProductOrderPage{}, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionTorrentPromotionPurchaseSelf, now); err != nil {
		return ProductOrderPage{}, err
	}
	return service.repository.ListProductOrders(ctx, ProductOrderQuery{BuyerID: session.User.ID, Limit: limit, Offset: offset})
}

func (service *ProductService) ProductPolicy(ctx context.Context, actor authz.StaffActor) (ProductPolicy, error) {
	now := canonicalProductTime(service.now())
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionPromotionManageRead, authz.SiteScope(), now, "promotion-product-administration"); err != nil {
		return ProductPolicy{}, err
	}
	return service.repository.CurrentProductPolicy(ctx, now)
}

func (service *ProductService) UpdateProductPolicy(ctx context.Context, actor authz.StaffActor, input UpdateProductPolicyCommand) (ProductPolicy, error) {
	input.ExpectedRevision = strings.TrimSpace(input.ExpectedRevision)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.RequestID == uuid.Nil || input.ExpectedRevision == "" || !validProductPolicyInput(input) ||
		!utf8.ValidString(input.Reason) || utf8.RuneCountInString(input.Reason) < minReasonRunes || utf8.RuneCountInString(input.Reason) > maxReasonRunes {
		return ProductPolicy{}, ErrInput
	}
	now := canonicalProductTime(service.now())
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionPromotionSchedule, authz.SiteScope(), now, "promotion-product-administration")
	if err != nil {
		return ProductPolicy{}, err
	}
	input.ActorID = actor.Subject.ID
	input.AuthorizationID = decision.ID
	input.OccurredAt = now
	return service.repository.UpdateProductPolicy(ctx, input)
}

func (service *ProductService) AdminOrders(ctx context.Context, actor authz.StaffActor, query ProductOrderQuery) (ProductOrderPage, error) {
	query.Query = strings.TrimSpace(query.Query)
	if !utf8.ValidString(query.Query) || utf8.RuneCountInString(query.Query) > 100 || !validProductPage(query.Limit, query.Offset) {
		return ProductOrderPage{}, ErrInput
	}
	now := canonicalProductTime(service.now())
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionPromotionManageRead, authz.SiteScope(), now, "promotion-product-administration"); err != nil {
		return ProductOrderPage{}, err
	}
	query.BuyerID = uuid.Nil
	return service.repository.ListProductOrders(ctx, query)
}

func validProductSelection(selection ProductSelection) bool {
	if selection.Promotion == nil && selection.StickyDays == 0 {
		return false
	}
	if selection.Promotion == nil {
		return selection.PromotionDays == 0 && selection.StickyDays > 0
	}
	return validPromotion(*selection.Promotion) && selection.PromotionDays > 0 && selection.StickyDays >= 0
}

func validProductPolicyInput(input UpdateProductPolicyCommand) bool {
	prices := []int64{
		input.FreePricePerDay, input.DoubleUploadPricePerDay, input.DoubleUploadFreePricePerDay,
		input.HalfDownloadPricePerDay, input.DoubleUploadHalfDownloadPricePerDay,
		input.ThirtyPercentDownloadPricePerDay, input.StickyPricePerDay,
	}
	for _, price := range prices {
		if price < 0 || price > 1_000_000 {
			return false
		}
	}
	return input.MaxPromotionDays >= 1 && input.MaxPromotionDays <= 30 && input.MaxStickyDays >= 1 && input.MaxStickyDays <= 30
}

func validProductPage(limit, offset int) bool {
	return limit >= 1 && limit <= MaxProductOrderLimit && offset >= 0 && offset <= MaxProductOrderOffset
}

func canonicalProductTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}
