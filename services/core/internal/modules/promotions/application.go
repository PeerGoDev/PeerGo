package promotions

import (
	"context"

	"github.com/google/uuid"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

// Application composes the staff campaign control plane and the member-paid
// product use cases without making either service depend on HTTP concerns.
type Application struct {
	campaigns *Service
	products  *ProductService
}

func NewApplication(campaigns *Service, products *ProductService) (*Application, error) {
	if campaigns == nil || products == nil {
		return nil, ErrInput
	}
	return &Application{campaigns: campaigns, products: products}, nil
}

func (application *Application) List(ctx context.Context, actor authz.StaffActor, limit, offset int) (Page, error) {
	return application.campaigns.List(ctx, actor, limit, offset)
}

func (application *Application) Schedule(ctx context.Context, actor authz.StaffActor, input ScheduleInput) (Campaign, error) {
	return application.campaigns.Schedule(ctx, actor, input)
}

func (application *Application) Offer(ctx context.Context, cookieToken string, torrentID int64) (ProductOffer, error) {
	return application.products.Offer(ctx, cookieToken, torrentID)
}

func (application *Application) Purchase(ctx context.Context, cookieToken, csrfToken string, orderID uuid.UUID, torrentID int64, selection ProductSelection) (ProductOrder, error) {
	return application.products.Purchase(ctx, cookieToken, csrfToken, orderID, torrentID, selection)
}

func (application *Application) MyOrders(ctx context.Context, cookieToken string, limit, offset int) (ProductOrderPage, error) {
	return application.products.MyOrders(ctx, cookieToken, limit, offset)
}

func (application *Application) ProductPolicy(ctx context.Context, actor authz.StaffActor) (ProductPolicy, error) {
	return application.products.ProductPolicy(ctx, actor)
}

func (application *Application) UpdateProductPolicy(ctx context.Context, actor authz.StaffActor, input UpdateProductPolicyCommand) (ProductPolicy, error) {
	return application.products.UpdateProductPolicy(ctx, actor, input)
}

func (application *Application) AdminOrders(ctx context.Context, actor authz.StaffActor, query ProductOrderQuery) (ProductOrderPage, error) {
	return application.products.AdminOrders(ctx, actor, query)
}
