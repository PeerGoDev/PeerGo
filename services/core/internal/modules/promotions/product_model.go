package promotions

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	DefaultProductOrderLimit = 20
	MaxProductOrderLimit     = 50
	MaxProductOrderOffset    = 1_000_000
)

// ProductPolicy is the effective immutable pricing revision. Prices are whole
// magic points per day; orders copy them so a later edit cannot alter history.
type ProductPolicy struct {
	Revision                            string
	EffectiveFrom                       time.Time
	PromotionEnabled                    bool
	StickyEnabled                       bool
	FreePricePerDay                     int64
	DoubleUploadPricePerDay             int64
	DoubleUploadFreePricePerDay         int64
	HalfDownloadPricePerDay             int64
	DoubleUploadHalfDownloadPricePerDay int64
	ThirtyPercentDownloadPricePerDay    int64
	StickyPricePerDay                   int64
	MaxPromotionDays                    int
	MaxStickyDays                       int
}

func (policy ProductPolicy) PromotionPrice(value Promotion) (int64, bool) {
	switch value {
	case PromotionFree:
		return policy.FreePricePerDay, true
	case PromotionDoubleUpload:
		return policy.DoubleUploadPricePerDay, true
	case PromotionDoubleUploadFree:
		return policy.DoubleUploadFreePricePerDay, true
	case PromotionHalfDownload:
		return policy.HalfDownloadPricePerDay, true
	case PromotionDoubleUploadHalfDownload:
		return policy.DoubleUploadHalfDownloadPricePerDay, true
	case PromotionThirtyPercentDownload:
		return policy.ThirtyPercentDownloadPricePerDay, true
	default:
		return 0, false
	}
}

type ProductWindow struct {
	StartsAt time.Time
	EndsAt   time.Time
}

// ProductOffer contains only current, decision-relevant state. The repository
// recalculates all values under a serializable transaction during purchase.
type ProductOffer struct {
	TorrentID             int64
	TorrentTitle          string
	MagicBalance          int64
	Policy                ProductPolicy
	ActivePromotion       *Promotion
	PromotionWindow       *ProductWindow
	StickyWindow          *ProductWindow
	ConflictingCampaignAt *time.Time
}

type ProductSelection struct {
	Promotion     *Promotion
	PromotionDays int
	StickyDays    int
}

// ProductPurchaseCommand is the repository command with the full typed
// authorization decision needed by the campaign audit event.
type ProductPurchaseCommand struct {
	OrderID         uuid.UUID
	BuyerID         uuid.UUID
	TorrentID       int64
	Selection       ProductSelection
	Now             time.Time
	AuthorizationID uuid.UUID
	Authorization   authz.Decision
}

type ProductOrder struct {
	ID                 uuid.UUID
	BuyerID            uuid.UUID
	BuyerNumericID     int64
	BuyerUsername      string
	TorrentID          int64
	TorrentTitle       string
	CampaignID         *uuid.UUID
	Promotion          *Promotion
	PromotionDays      int
	PromotionUnitPrice int64
	PromotionWindow    *ProductWindow
	StickyDays         int
	StickyUnitPrice    int64
	StickyWindow       *ProductWindow
	TotalPrice         int64
	PolicyRevision     string
	BalanceAfter       int64
	PurchasedAt        time.Time
	Replayed           bool
}

type ProductOrderPage struct {
	Items  []ProductOrder
	Total  int64
	Limit  int
	Offset int
}

type ProductOrderQuery struct {
	BuyerID uuid.UUID
	Query   string
	Limit   int
	Offset  int
}

type UpdateProductPolicyCommand struct {
	RequestID                           uuid.UUID
	ActorID                             uuid.UUID
	ExpectedRevision                    string
	PromotionEnabled                    bool
	StickyEnabled                       bool
	FreePricePerDay                     int64
	DoubleUploadPricePerDay             int64
	DoubleUploadFreePricePerDay         int64
	HalfDownloadPricePerDay             int64
	DoubleUploadHalfDownloadPricePerDay int64
	ThirtyPercentDownloadPricePerDay    int64
	StickyPricePerDay                   int64
	MaxPromotionDays                    int
	MaxStickyDays                       int
	Reason                              string
	OccurredAt                          time.Time
	AuthorizationID                     uuid.UUID
}

type ProductRepository interface {
	ProductOffer(context.Context, uuid.UUID, int64, time.Time) (ProductOffer, error)
	PurchaseProduct(context.Context, ProductPurchaseCommand) (ProductOrder, error)
	ListProductOrders(context.Context, ProductOrderQuery) (ProductOrderPage, error)
	CurrentProductPolicy(context.Context, time.Time) (ProductPolicy, error)
	UpdateProductPolicy(context.Context, UpdateProductPolicyCommand) (ProductPolicy, error)
}
