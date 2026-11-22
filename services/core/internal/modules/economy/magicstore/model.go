// Package magicstore owns the member magic shop (invitations, VIP time,
// colored usernames and attendance make-up cards) and the magic games
// (scratch cards, the daily lottery and PvP challenges). Every magic movement
// is a balanced economy ledger transaction recorded in the same database
// transaction as the receipt that explains it.
package magicstore

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInput               = errors.New("magic store input is invalid")
	ErrShopDisabled        = errors.New("magic shop item is disabled")
	ErrShopPriceChanged    = errors.New("magic shop price changed")
	ErrGameDisabled        = errors.New("magic game is disabled")
	ErrInsufficientMagic   = errors.New("magic balance is insufficient")
	ErrIdempotencyConflict = errors.New("magic store request key was reused")
	ErrSettingsConflict    = errors.New("magic store settings version changed")
	ErrMonthlyLimit        = errors.New("monthly purchase limit reached")
	ErrHoldingLimit        = errors.New("item holding limit reached")
	ErrInviteIneligible    = errors.New("member cannot use purchased invitations")
	ErrVIPPermanent        = errors.New("member already has permanent VIP")
	ErrAccountUnavailable  = errors.New("member account is not active")
	ErrNameStyleTier       = errors.New("name style is not allowed by the purchased tier")
	ErrNameStyleMissing    = errors.New("member has no active name style")
	ErrDailyLimit          = errors.New("daily game limit reached")
	ErrLotteryClosed       = errors.New("lottery round is closed for sales")
	ErrLotteryTicketLimit  = errors.New("lottery ticket limit reached")
	ErrChallengeNotFound   = errors.New("pvp challenge was not found")
	ErrChallengeClosed     = errors.New("pvp challenge is no longer open")
	ErrChallengeOwn        = errors.New("cannot accept your own pvp challenge")
	ErrChallengeOpenLimit  = errors.New("too many open pvp challenges")
	ErrChallengeForbidden  = errors.New("only the creator can cancel a pvp challenge")
)

type ItemKey string

const (
	ItemInvite            ItemKey = "invite"
	ItemTemporaryInvite   ItemKey = "temporary_invite"
	ItemVIP               ItemKey = "vip"
	ItemNameColorSolid    ItemKey = "name_color_solid"
	ItemNameColorGradient ItemKey = "name_color_gradient"
	ItemMakeupCard        ItemKey = "makeup_card"
)

func validItem(item ItemKey) bool {
	switch item {
	case ItemInvite, ItemTemporaryInvite, ItemVIP, ItemNameColorSolid, ItemNameColorGradient, ItemMakeupCard:
		return true
	default:
		return false
	}
}

type Order struct {
	ID          uuid.UUID
	ItemKey     ItemKey
	Quantity    int
	UnitPrice   int64
	TotalPrice  int64
	StyleKey    *string
	EffectUntil *time.Time
	PurchasedAt time.Time
}

type PurchaseCommand struct {
	RequestID         uuid.UUID
	UserID            uuid.UUID
	ItemKey           ItemKey
	Quantity          int
	StyleKey          string
	ExpectedUnitPrice *int64
	Now               time.Time
}

type PurchaseReceipt struct {
	Order        Order
	BalanceAfter int64
	Replayed     bool
}

type NameStyle struct {
	UserID    uuid.UUID
	Username  string
	Tier      NameStyleTier
	StyleKey  string
	ExpiresAt time.Time
}

type VIPStatus struct {
	Active    bool
	Permanent bool
	Until     *time.Time
}

type ShopOverview struct {
	Settings                  Settings
	SettingsVersion           int64
	MagicBalance              int64
	InvitesPurchasedThisMonth int
	TemporaryPurchasedMonth   int
	RemainingInvites          int
	TemporaryInvites          int
	TemporaryInviteNextExpiry *time.Time
	InviteBlocker             string
	VIP                       VIPStatus
	NameStyle                 *NameStyle
	MakeupCards               int
	RecentOrders              []Order
}

type ScratchCard struct {
	ID          uuid.UUID
	Price       int64
	Prize       int64
	PrizeIndex  int
	PrizeLabel  string
	PurchasedAt time.Time
}

type ScratchReceipt struct {
	Card         ScratchCard
	BalanceAfter int64
	Replayed     bool
}

type LotteryWinner struct {
	DrawRank     int
	TierIndex    int
	TierLabel    string
	TicketNumber int
	UserID       uuid.UUID
	Username     string
	Prize        int64
}

type LotteryRound struct {
	ID                      uuid.UUID
	RoundNumber             int64
	Status                  string
	TicketPrice             int64
	MaximumTicketsPerMember int
	BurnBPS                 int64
	Tiers                   []LotteryTier
	OpensAt                 time.Time
	DrawAt                  time.Time
	SeedSHA256              [32]byte
	Seed                    []byte
	CarriedIn               int64
	TicketsSold             int
	SalesTotal              int64
	Burned                  *int64
	PaidOut                 *int64
	CarriedOut              *int64
	DrawnAt                 *time.Time
	MyTickets               int
	MyTicketRanges          [][2]int
	MyPrize                 int64
	Winners                 []LotteryWinner
}

type LotteryReceipt struct {
	RoundID      uuid.UUID
	RoundNumber  int64
	Quantity     int
	FirstTicket  int
	LastTicket   int
	TotalPrice   int64
	BalanceAfter int64
	Replayed     bool
}

type BuyLotteryTicketsCommand struct {
	RequestID uuid.UUID
	UserID    uuid.UUID
	RoundID   *uuid.UUID
	Quantity  int
	Now       time.Time
}

type PvPMode string

const (
	PvPDice PvPMode = "dice"
	PvPRPS  PvPMode = "rps"
)

type RPSChoice string

const (
	RPSRock     RPSChoice = "rock"
	RPSPaper    RPSChoice = "paper"
	RPSScissors RPSChoice = "scissors"
)

func validChoice(choice RPSChoice) bool {
	return choice == RPSRock || choice == RPSPaper || choice == RPSScissors
}

type PvPStatus string

const (
	PvPOpen      PvPStatus = "open"
	PvPSettled   PvPStatus = "settled"
	PvPCancelled PvPStatus = "cancelled"
	PvPExpired   PvPStatus = "expired"
)

type PvPChallenge struct {
	ID               uuid.UUID
	Mode             PvPMode
	Status           PvPStatus
	CreatorID        uuid.UUID
	CreatorUsername  string
	CreatorChoice    *RPSChoice
	Stake            int64
	FeeBPS           int64
	Message          *string
	OpponentID       *uuid.UUID
	OpponentUsername *string
	OpponentChoice   *RPSChoice
	CreatorRoll      *int
	OpponentRoll     *int
	WinnerID         *uuid.UUID
	Fee              *int64
	ExpiresAt        time.Time
	CreatedAt        time.Time
	ClosedAt         *time.Time
	Version          int64
}

type CreateChallengeCommand struct {
	RequestID uuid.UUID
	UserID    uuid.UUID
	Mode      PvPMode
	Stake     int64
	Choice    RPSChoice
	Message   string
	Now       time.Time
}

type GamesOverview struct {
	Settings           Settings
	SettingsVersion    int64
	MagicBalance       int64
	ScratchUsedToday   int
	RecentScratchCards []ScratchCard
	CurrentRound       *LotteryRound
	PreviousRound      *LotteryRound
	OpenChallenges     []PvPChallenge
	MyChallenges       []PvPChallenge
	RecentSettled      []PvPChallenge
}

type SettingsRevision struct {
	Version       int64
	Reason        string
	ActorUsername *string
	OccurredAt    time.Time
}

type AccountBalances struct {
	ShopSink    int64
	GameHouse   int64
	LotteryPool int64
	PvPEscrow   int64
}

type AdminOverview struct {
	Settings  Settings
	Version   int64
	UpdatedAt time.Time
	Accounts  AccountBalances
	Revisions []SettingsRevision
}

type UpdateSettingsCommand struct {
	Settings        Settings
	ExpectedVersion int64
	Reason          string
	ActorID         uuid.UUID
	DecisionID      uuid.UUID
	OccurredAt      time.Time
}
