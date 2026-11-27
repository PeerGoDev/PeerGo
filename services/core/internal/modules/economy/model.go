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
	TransactionReseedReward         TransactionType = "reseed_reward"
	TransactionWorkgroupPayroll     TransactionType = "workgroup_payroll"
	TransactionHaremReward          TransactionType = "harem_reward"
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
	TransactionMagicShopPurchase    TransactionType = "magic_shop_purchase"
	TransactionScratchCardPurchase  TransactionType = "scratch_card_purchase"
	TransactionScratchCardPayout    TransactionType = "scratch_card_payout"
	TransactionLotteryTicket        TransactionType = "lottery_ticket_purchase"
	TransactionLotteryPayout        TransactionType = "lottery_payout"
	TransactionLotteryBurn          TransactionType = "lottery_burn"
	TransactionPvPStake             TransactionType = "pvp_stake"
	TransactionPvPPayout            TransactionType = "pvp_payout"
	TransactionPvPRefund            TransactionType = "pvp_refund"
)

var (
	rousiMigrationAccountID          = uuid.MustParse("00000000-0000-7000-8000-000000000001")
	seedingMintAccountID             = uuid.MustParse("00000000-0000-7000-8000-000000000002")
	reseedMintAccountID              = uuid.MustParse("00000000-0000-7000-8000-000000000015")
	workgroupPayrollMintAccountID    = uuid.MustParse("00000000-0000-7000-8000-000000000016")
	activityMintAccountID            = uuid.MustParse("00000000-0000-7000-8000-000000000003")
	torrentPurchaseSinkID            = uuid.MustParse("00000000-0000-7000-8000-000000000004")
	feeSinkAccountID                 = uuid.MustParse("00000000-0000-7000-8000-000000000005")
	promotionProductSinkID           = uuid.MustParse("00000000-0000-7000-8000-000000000006")
	medalPurchaseSinkID              = uuid.MustParse("00000000-0000-7000-8000-000000000007")
	administratorAdjustmentAccountID = uuid.MustParse("00000000-0000-7000-8000-000000000009")
	haremMintAccountID               = uuid.MustParse("00000000-0000-7000-8000-000000000010")
	magicShopSinkID                  = uuid.MustParse("00000000-0000-7000-8000-000000000011")
	magicGameHouseID                 = uuid.MustParse("00000000-0000-7000-8000-000000000012")
	lotteryPoolEscrowID              = uuid.MustParse("00000000-0000-7000-8000-000000000013")
	pvpChallengeEscrowID             = uuid.MustParse("00000000-0000-7000-8000-000000000014")
)

// MagicShopSinkID returns the site-side account that receives shop purchases.
func MagicShopSinkID() uuid.UUID { return magicShopSinkID }

// MagicGameHouseID returns the house account behind scratch cards, lottery
// burns and PvP fees. A positive balance is magic removed from circulation.
func MagicGameHouseID() uuid.UUID { return magicGameHouseID }

// LotteryPoolEscrowID holds ticket sales and rollover until the next draw.
func LotteryPoolEscrowID() uuid.UUID { return lotteryPoolEscrowID }

// PvPChallengeEscrowID holds both stakes of an open or accepted challenge.
func PvPChallengeEscrowID() uuid.UUID { return pvpChallengeEscrowID }

// RousiMigrationAccountID returns the immutable opening-balance counterparty.
func RousiMigrationAccountID() uuid.UUID { return rousiMigrationAccountID }

// SeedingMintAccountID returns the system account that issues seeding rewards.
func SeedingMintAccountID() uuid.UUID { return seedingMintAccountID }

// ReseedMintAccountID returns the issuer for one-time swarm recovery rewards.
func ReseedMintAccountID() uuid.UUID { return reseedMintAccountID }

func WorkgroupPayrollMintAccountID() uuid.UUID { return workgroupPayrollMintAccountID }

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

// AdministratorAdjustmentAccountID returns the explicit counterparty for
// audited staff-issued magic adjustments.
func AdministratorAdjustmentAccountID() uuid.UUID { return administratorAdjustmentAccountID }

// HaremMintAccountID returns the explicit issuer for invitation-tree rewards.
func HaremMintAccountID() uuid.UUID { return haremMintAccountID }

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
