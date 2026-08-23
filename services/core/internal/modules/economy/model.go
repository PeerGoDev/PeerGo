// Package economy owns PeerGo's integer magic-point ledger. Business modules
// submit balanced commands here; they never update balance projections
// directly.
package economy

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInput               = errors.New("economy transaction input is invalid")
	ErrAccountNotFound     = errors.New("economy account was not found")
	ErrInsufficientBalance = errors.New("magic balance is insufficient")
	ErrIdempotencyConflict = errors.New("economy idempotency key was reused")
	ErrInvariant           = errors.New("economy ledger invariant failed")
)

type TransactionType string

const (
	TransactionSeedingReward        TransactionType = "seeding_reward"
	TransactionActivityReward       TransactionType = "activity_reward"
	TransactionTorrentBuy           TransactionType = "torrent_purchase"
	TransactionPromotionBuy         TransactionType = "promotion_product_purchase"
	TransactionMedalBuy             TransactionType = "medal_purchase"
	TransactionMemberGift           TransactionType = "member_gift"
	TransactionTip                  TransactionType = "tip"
	TransactionSocialRedPacketFund  TransactionType = "social_red_packet_fund"
	TransactionSocialRedPacketClaim TransactionType = "social_red_packet_claim"
	TransactionRefund               TransactionType = "refund"
	TransactionAdjustment           TransactionType = "adjustment"
)

var (
	rousiMigrationAccountID = uuid.MustParse("00000000-0000-7000-8000-000000000001")
	seedingMintAccountID    = uuid.MustParse("00000000-0000-7000-8000-000000000002")
	activityMintAccountID   = uuid.MustParse("00000000-0000-7000-8000-000000000003")
	torrentPurchaseSinkID   = uuid.MustParse("00000000-0000-7000-8000-000000000004")
	feeSinkAccountID        = uuid.MustParse("00000000-0000-7000-8000-000000000005")
	promotionProductSinkID  = uuid.MustParse("00000000-0000-7000-8000-000000000006")
	medalPurchaseSinkID     = uuid.MustParse("00000000-0000-7000-8000-000000000007")
)

// RousiMigrationAccountID returns the immutable opening-balance counterparty.
func RousiMigrationAccountID() uuid.UUID { return rousiMigrationAccountID }

// SeedingMintAccountID returns the system account that issues seeding rewards.
func SeedingMintAccountID() uuid.UUID { return seedingMintAccountID }

// ActivityMintAccountID returns the system account that issues activity rewards.
func ActivityMintAccountID() uuid.UUID { return activityMintAccountID }

// TorrentPurchaseSinkID returns the site-side torrent purchase account.
func TorrentPurchaseSinkID() uuid.UUID { return torrentPurchaseSinkID }

// FeeSinkAccountID returns the explicit system fee account.
func FeeSinkAccountID() uuid.UUID { return feeSinkAccountID }

// PromotionProductSinkID returns the site-side paid promotion and pin account.
func PromotionProductSinkID() uuid.UUID { return promotionProductSinkID }

// MedalPurchaseSinkID returns the site-side medal shop account.
func MedalPurchaseSinkID() uuid.UUID { return medalPurchaseSinkID }

type PostingInput struct {
	// Member account IDs equal their identity user UUID. System account IDs are
	// the explicit constants above; there is no caller-defined account code.
	AccountID uuid.UUID
	Amount    int64
}

type RecordCommand struct {
	TransactionID   uuid.UUID
	TransactionType TransactionType
	IdempotencyKey  string
	SourceReference string
	PolicyRevision  string
	PayloadSHA256   [32]byte
	OccurredAt      time.Time
	RecordedAt      time.Time
	Postings        []PostingInput
}

type Posting struct {
	AccountID    uuid.UUID
	Amount       int64
	BalanceAfter int64
}

type Transaction struct {
	ID              uuid.UUID
	LedgerSequence  int64
	TransactionType TransactionType
	IdempotencyKey  string
	SourceReference string
	PolicyRevision  string
	PayloadSHA256   [32]byte
	OccurredAt      time.Time
	RecordedAt      time.Time
	Postings        []Posting
	Replayed        bool
}
