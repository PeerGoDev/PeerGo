// Package torrentpurchase owns priced-torrent access and its atomic integer
// magic-point settlement.  Download code consumes only the resulting durable
// entitlement; it never recalculates an old purchase from today's price.
package torrentpurchase

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInput               = errors.New("torrent purchase input is invalid")
	ErrNotFound            = errors.New("published torrent was not found")
	ErrPurchaseRequired    = errors.New("torrent purchase is required")
	ErrPurchaseDisabled    = errors.New("torrent purchasing is disabled")
	ErrPurchaseNotRequired = errors.New("torrent purchase is not required")
	ErrIdempotencyConflict = errors.New("torrent purchase idempotency key was reused")
	ErrVersionConflict     = errors.New("torrent purchase settings version changed")
	ErrNoChange            = errors.New("torrent purchase settings did not change")
	ErrAlreadyRefunded     = errors.New("torrent purchase was already refunded")
	ErrInvariant           = errors.New("torrent purchase invariant failed")
)

const (
	DefaultHistoryLimit = 20
	MaxHistoryLimit     = 50
	MaxHistoryOffset    = 1_000_000
	DefaultAdminLimit   = 20
	MaxAdminLimit       = 50
	MaxAdminOffset      = 1_000_000
)

type AccessState string

const (
	AccessFree             AccessState = "free"
	AccessUploader         AccessState = "uploader"
	AccessPurchased        AccessState = "purchased"
	AccessPurchaseRequired AccessState = "purchase_required"
	AccessPurchaseDisabled AccessState = "purchase_disabled"
)

type Status struct {
	TorrentID      int64
	Title          string
	SellerID       uuid.UUID
	Price          int64
	Tax            int64
	SellerIncome   int64
	MagicBalance   int64
	State          AccessState
	PolicyRevision string
	PurchasedAt    *time.Time
	LegacyImport   bool
}

type Receipt struct {
	EntitlementID      uuid.UUID
	RequestID          uuid.UUID
	UserID             uuid.UUID
	TorrentID          int64
	SellerID           uuid.UUID
	Price              int64
	Tax                int64
	SellerIncome       int64
	BalanceAfter       int64
	PolicyRevision     string
	MagicTransactionID uuid.UUID
	PurchasedAt        time.Time
	Replayed           bool
}

type PurchaseCommand struct {
	RequestID uuid.UUID
	UserID    uuid.UUID
	TorrentID int64
	Now       time.Time
}

// HistoryItem is the durable purchase right shown to its owner.  Paid values
// come from the immutable entitlement rather than today's torrent price.
type HistoryItem struct {
	TorrentID    int64
	Title        string
	CategoryName string
	TorrentState string
	Price        int64
	PurchasedAt  time.Time
	LegacyImport bool
}

type HistoryPage struct {
	Items  []HistoryItem
	Total  int64
	Limit  int
	Offset int
}

type HistoryQuery struct {
	UserID uuid.UUID
	Limit  int
	Offset int
}

// PolicySettings is the small operator-facing view.  The repository still
// stores the full immutable snapshot and digest for settlement evidence.
type PolicySettings struct {
	Enabled        bool
	TaxBasisPoints int64
	Revision       string
	EffectiveFrom  time.Time
}

type UpdatePolicyCommand struct {
	RequestID        uuid.UUID
	ActorID          uuid.UUID
	Enabled          bool
	TaxBasisPoints   int64
	ExpectedRevision string
	Reason           string
	OccurredAt       time.Time
	AuthorizationID  uuid.UUID
}

type PriceChange struct {
	RequestID uuid.UUID
	TorrentID int64
	Title     string
	Price     int64
	Version   int64
	ChangedAt time.Time
	Replayed  bool
}

type UpdatePriceCommand struct {
	RequestID       uuid.UUID
	ActorID         uuid.UUID
	TorrentID       int64
	Price           int64
	ExpectedVersion int64
	Reason          string
	OccurredAt      time.Time
	AuthorizationID uuid.UUID
}

type AdminPurchaseStatus string

const (
	AdminPurchaseStatusAll      AdminPurchaseStatus = "all"
	AdminPurchaseStatusActive   AdminPurchaseStatus = "active"
	AdminPurchaseStatusRefunded AdminPurchaseStatus = "refunded"
)

type AdminPurchaseSource string

const (
	AdminPurchaseSourceAll    AdminPurchaseSource = "all"
	AdminPurchaseSourceLive   AdminPurchaseSource = "live_purchase"
	AdminPurchaseSourceLegacy AdminPurchaseSource = "legacy_import"
)

// AdminPurchaseItem is an operations projection. Public numeric user and
// torrent IDs are used for staff actions; entitlement UUIDs stay internal.
type AdminPurchaseItem struct {
	BuyerNumericID       int64
	BuyerUsername        string
	BuyerDisplayName     string
	SellerNumericID      int64
	SellerUsername       string
	TorrentID            int64
	TorrentTitle         string
	CategoryName         string
	Source               AdminPurchaseSource
	Status               AdminPurchaseStatus
	Price                int64
	Tax                  int64
	SellerIncome         int64
	PurchasedAt          time.Time
	RefundedAt           *time.Time
	RefundReason         string
	RefundedByNumericID  *int64
	RefundedByUsername   string
	RefundedBalanceAfter *int64
}

type AdminPurchasePage struct {
	Items  []AdminPurchaseItem
	Total  int64
	Limit  int
	Offset int
}

type AdminPurchaseQuery struct {
	Query  string
	Status AdminPurchaseStatus
	Source AdminPurchaseSource
	Limit  int
	Offset int
}

type RefundCommand struct {
	RequestID       uuid.UUID
	ActorID         uuid.UUID
	BuyerNumericID  int64
	TorrentID       int64
	Reason          string
	OccurredAt      time.Time
	AuthorizationID uuid.UUID
}

type RefundReceipt struct {
	RequestID      uuid.UUID
	BuyerNumericID int64
	TorrentID      int64
	TorrentTitle   string
	RefundAmount   int64
	BalanceAfter   int64
	RefundedAt     time.Time
	Replayed       bool
}

type Repository interface {
	Status(context.Context, uuid.UUID, int64, time.Time) (Status, error)
	Purchase(context.Context, PurchaseCommand) (Receipt, error)
}

// HistoryRepository and AdministrationRepository are intentionally separate
// capability ports.  Download-only tests and adapters do not need staff write
// methods, while the production PostgreSQL repository implements all three.
type HistoryRepository interface {
	ListHistory(context.Context, HistoryQuery) (HistoryPage, error)
}

type AdministrationRepository interface {
	CurrentPolicy(context.Context, time.Time) (PolicySettings, error)
	UpdatePolicy(context.Context, UpdatePolicyCommand) (PolicySettings, error)
	UpdatePrice(context.Context, UpdatePriceCommand) (PriceChange, error)
	ListPurchases(context.Context, AdminPurchaseQuery) (AdminPurchasePage, error)
	Refund(context.Context, RefundCommand) (RefundReceipt, error)
}
