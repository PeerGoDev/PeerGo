package magicstore

import (
	"bytes"
	"encoding/json"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maximumPrice        = int64(100_000_000)
	maximumPrizes       = 12
	maximumLotteryTiers = 10
	maximumWinners      = 200
)

// Settings is the complete, versioned configuration of the magic shop and the
// magic games. Every field is an integer or a bounded string; prices are whole
// magic points.
type Settings struct {
	Timezone        string              `json:"timezone"`
	ShopEnabled     bool                `json:"shop_enabled"`
	Invite          InviteItem          `json:"invite"`
	TemporaryInvite TemporaryInviteItem `json:"temporary_invite"`
	VIP             VIPItem             `json:"vip"`
	NameColor       NameColorItem       `json:"name_color"`
	MakeupCard      MakeupCardItem      `json:"makeup_card"`
	GamesEnabled    bool                `json:"games_enabled"`
	Scratch         ScratchGame         `json:"scratch"`
	Lottery         LotteryGame         `json:"lottery"`
	PvP             PvPGame             `json:"pvp"`
}

type InviteItem struct {
	Enabled bool  `json:"enabled"`
	Price   int64 `json:"price"`
	// MonthlyLimit counts purchased slots per calendar month; 0 is unlimited.
	MonthlyLimit int `json:"monthly_limit"`
}

type TemporaryInviteItem struct {
	Enabled      bool  `json:"enabled"`
	Price        int64 `json:"price"`
	ValidDays    int   `json:"valid_days"`
	MonthlyLimit int   `json:"monthly_limit"`
}

type VIPItem struct {
	Enabled      bool  `json:"enabled"`
	Price        int64 `json:"price"`
	DurationDays int   `json:"duration_days"`
}

type NameColorItem struct {
	Enabled       bool  `json:"enabled"`
	SolidPrice    int64 `json:"solid_price"`
	GradientPrice int64 `json:"gradient_price"`
	DurationDays  int   `json:"duration_days"`
}

type MakeupCardItem struct {
	Enabled        bool  `json:"enabled"`
	Price          int64 `json:"price"`
	WindowDays     int   `json:"window_days"`
	MaximumHolding int   `json:"maximum_holding"`
}

type ScratchPrize struct {
	Label  string `json:"label"`
	Payout int64  `json:"payout"`
	Weight int64  `json:"weight"`
}

type ScratchGame struct {
	Enabled    bool           `json:"enabled"`
	Price      int64          `json:"price"`
	DailyLimit int            `json:"daily_limit"`
	Prizes     []ScratchPrize `json:"prizes"`
}

type LotteryTier struct {
	Label    string `json:"label"`
	Winners  int    `json:"winners"`
	ShareBPS int64  `json:"share_bps"`
}

type LotteryGame struct {
	Enabled                bool          `json:"enabled"`
	TicketPrice            int64         `json:"ticket_price"`
	MaximumTicketsPerRound int           `json:"maximum_tickets_per_round"`
	DrawHour               int           `json:"draw_hour"`
	BurnBPS                int64         `json:"burn_bps"`
	Tiers                  []LotteryTier `json:"tiers"`
}

type PvPGame struct {
	Enabled              bool  `json:"enabled"`
	MinimumStake         int64 `json:"minimum_stake"`
	MaximumStake         int64 `json:"maximum_stake"`
	FeeBPS               int64 `json:"fee_bps"`
	MaximumOpenPerMember int   `json:"maximum_open_per_member"`
	ExpiryHours          int   `json:"expiry_hours"`
}

// NameStyleTier separates plain colors from the more expensive gradients.
// A gradient purchase may also select any solid style.
type NameStyleTier string

const (
	NameStyleSolid    NameStyleTier = "solid"
	NameStyleGradient NameStyleTier = "gradient"
)

// The Web client owns the actual colors; Core only validates the key so a
// stored style always has a rendering.
var nameStyles = map[string]NameStyleTier{
	"crimson": NameStyleSolid, "orange": NameStyleSolid, "amber": NameStyleSolid,
	"emerald": NameStyleSolid, "teal": NameStyleSolid, "sky": NameStyleSolid,
	"indigo": NameStyleSolid, "violet": NameStyleSolid, "pink": NameStyleSolid,
	"sunset": NameStyleGradient, "ocean": NameStyleGradient, "aurora": NameStyleGradient,
	"flame": NameStyleGradient, "sakura": NameStyleGradient, "gold": NameStyleGradient,
	"rainbow": NameStyleGradient,
}

// NameStyleAllowed reports whether a member holding tier may display style.
func NameStyleAllowed(tier NameStyleTier, style string) bool {
	styleTier, known := nameStyles[style]
	if !known {
		return false
	}
	return styleTier == NameStyleSolid || tier == NameStyleGradient
}

type NameStyleOption struct {
	Key  string
	Tier NameStyleTier
}

var nameStyleOrder = []string{
	"crimson", "orange", "amber", "emerald", "teal", "sky", "indigo", "violet", "pink",
	"sunset", "ocean", "aurora", "flame", "sakura", "gold", "rainbow",
}

// NameStyleOptions lists every style in palette order, solid ones first.
func NameStyleOptions() []NameStyleOption {
	result := make([]NameStyleOption, 0, len(nameStyleOrder))
	for _, key := range nameStyleOrder {
		result = append(result, NameStyleOption{Key: key, Tier: nameStyles[key]})
	}
	return result
}

// NameStyleTierOf returns the minimum tier that unlocks style.
func NameStyleTierOf(style string) (NameStyleTier, bool) {
	tier, known := nameStyles[style]
	return tier, known
}

// DecodeSettings strictly decodes a stored or submitted settings document.
func DecodeSettings(document []byte) (Settings, error) {
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()
	var settings Settings
	if err := decoder.Decode(&settings); err != nil {
		return Settings{}, ErrInput
	}
	if decoder.More() {
		return Settings{}, ErrInput
	}
	return NormalizeSettings(settings)
}

// NormalizeSettings trims labels and rejects any configuration that would be
// unsafe to run, including scratch tables that pay back more than they take.
func NormalizeSettings(settings Settings) (Settings, error) {
	settings.Timezone = strings.TrimSpace(settings.Timezone)
	if settings.Timezone == "" || len(settings.Timezone) > 64 {
		return Settings{}, ErrInput
	}
	if _, err := time.LoadLocation(settings.Timezone); err != nil {
		return Settings{}, ErrInput
	}
	if !validPrice(settings.Invite.Price) || !between(settings.Invite.MonthlyLimit, 0, 100) ||
		!validPrice(settings.TemporaryInvite.Price) || !between(settings.TemporaryInvite.ValidDays, 1, 90) ||
		!between(settings.TemporaryInvite.MonthlyLimit, 0, 100) ||
		!validPrice(settings.VIP.Price) || !between(settings.VIP.DurationDays, 1, 365) ||
		!validPrice(settings.NameColor.SolidPrice) || !validPrice(settings.NameColor.GradientPrice) ||
		!between(settings.NameColor.DurationDays, 1, 365) ||
		!validPrice(settings.MakeupCard.Price) || !between(settings.MakeupCard.WindowDays, 1, 30) ||
		!between(settings.MakeupCard.MaximumHolding, 1, 100) {
		return Settings{}, ErrInput
	}

	scratch := settings.Scratch
	if !validPrice(scratch.Price) || scratch.Price > 1_000_000 || !between(scratch.DailyLimit, 1, 1000) ||
		len(scratch.Prizes) < 2 || len(scratch.Prizes) > maximumPrizes {
		return Settings{}, ErrInput
	}
	totalWeight := new(big.Int)
	expectedPayout := new(big.Int)
	for index := range scratch.Prizes {
		prize := &scratch.Prizes[index]
		prize.Label = strings.TrimSpace(prize.Label)
		if !validLabel(prize.Label) || prize.Payout < 0 || prize.Payout > maximumPrice ||
			prize.Weight < 0 || prize.Weight > 1_000_000 {
			return Settings{}, ErrInput
		}
		totalWeight.Add(totalWeight, big.NewInt(prize.Weight))
		expectedPayout.Add(expectedPayout, new(big.Int).Mul(big.NewInt(prize.Payout), big.NewInt(prize.Weight)))
	}
	maximumPayout := new(big.Int).Mul(big.NewInt(scratch.Price), totalWeight)
	if totalWeight.Sign() == 0 || expectedPayout.Cmp(maximumPayout) > 0 {
		return Settings{}, ErrInput
	}

	lottery := settings.Lottery
	if !validPrice(lottery.TicketPrice) || lottery.TicketPrice > 1_000_000 ||
		!between(lottery.MaximumTicketsPerRound, 1, 1000) || !between(lottery.DrawHour, 0, 23) ||
		lottery.BurnBPS < 0 || lottery.BurnBPS > 10000 ||
		len(lottery.Tiers) < 1 || len(lottery.Tiers) > maximumLotteryTiers {
		return Settings{}, ErrInput
	}
	totalShare, totalWinners := int64(0), 0
	for index := range lottery.Tiers {
		tier := &lottery.Tiers[index]
		tier.Label = strings.TrimSpace(tier.Label)
		if !validLabel(tier.Label) || !between(tier.Winners, 1, 100) || tier.ShareBPS < 1 || tier.ShareBPS > 10000 {
			return Settings{}, ErrInput
		}
		totalShare += tier.ShareBPS
		totalWinners += tier.Winners
	}
	if totalShare > 10000 || totalWinners > maximumWinners {
		return Settings{}, ErrInput
	}

	pvp := settings.PvP
	if !validPrice(pvp.MinimumStake) || pvp.MaximumStake < pvp.MinimumStake || pvp.MaximumStake > maximumPrice ||
		pvp.FeeBPS < 0 || pvp.FeeBPS > 5000 || !between(pvp.MaximumOpenPerMember, 1, 20) ||
		!between(pvp.ExpiryHours, 1, 168) {
		return Settings{}, ErrInput
	}
	return settings, nil
}

// ScratchExpectedReturnBPS is the long-run share of the card price paid back.
func ScratchExpectedReturnBPS(game ScratchGame) int64 {
	totalWeight, expected := int64(0), new(big.Int)
	for _, prize := range game.Prizes {
		totalWeight += prize.Weight
		expected.Add(expected, new(big.Int).Mul(big.NewInt(prize.Payout), big.NewInt(prize.Weight)))
	}
	if totalWeight == 0 || game.Price == 0 {
		return 0
	}
	expected.Mul(expected, big.NewInt(10000))
	expected.Quo(expected, new(big.Int).Mul(big.NewInt(game.Price), big.NewInt(totalWeight)))
	return expected.Int64()
}

func (settings Settings) location() *time.Location {
	location, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		return time.UTC
	}
	return location
}

func validPrice(value int64) bool { return value >= 1 && value <= maximumPrice }

func between(value, minimum, maximum int) bool { return value >= minimum && value <= maximum }

func validLabel(value string) bool {
	count := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && count >= 1 && count <= 20
}
