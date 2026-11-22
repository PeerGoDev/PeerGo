package magicstore

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
)

func seededSettings(t *testing.T) Settings {
	t.Helper()
	migration, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "..", "db", "core", "migrations", "202611130001_core_magic_shop_and_games.sql"))
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	match := regexp.MustCompile(`(?s)INSERT INTO economy\.magic_store_settings .*?VALUES \(true, '(\{.*?\})'::jsonb`).FindSubmatch(migration)
	if match == nil {
		t.Fatal("seed document not found in migration")
	}
	settings, err := DecodeSettings(match[1])
	if err != nil {
		t.Fatalf("seeded settings must be valid: %v", err)
	}
	return settings
}

func TestSeededSettingsAreValidAndHouseFavored(t *testing.T) {
	settings := seededSettings(t)
	if bps := ScratchExpectedReturnBPS(settings.Scratch); bps <= 0 || bps >= 10000 {
		t.Fatalf("scratch return = %d bps, want inside (0, 10000)", bps)
	}
}

func TestDecodeSettingsRejectsUnknownFields(t *testing.T) {
	if _, err := DecodeSettings([]byte(`{"timezone":"Asia/Shanghai","surprise":1}`)); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestNormalizeSettingsRejectsUnsafeConfiguration(t *testing.T) {
	cases := map[string]func(*Settings){
		"scratch pays more than price": func(s *Settings) {
			s.Scratch.Prizes = []ScratchPrize{{Label: "a", Payout: 0, Weight: 1}, {Label: "b", Payout: s.Scratch.Price * 3, Weight: 1}}
		},
		"lottery shares exceed pool": func(s *Settings) { s.Lottery.Tiers[0].ShareBPS = 9000 },
		"pvp fee too high":           func(s *Settings) { s.PvP.FeeBPS = 6000 },
		"stake range inverted":       func(s *Settings) { s.PvP.MaximumStake = s.PvP.MinimumStake - 1 },
		"unknown timezone":           func(s *Settings) { s.Timezone = "Mars/Olympus" },
		"empty prize label":          func(s *Settings) { s.Scratch.Prizes[0].Label = " " },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			settings := seededSettings(t)
			mutate(&settings)
			if _, err := NormalizeSettings(settings); err == nil {
				t.Fatal("unsafe settings accepted")
			}
		})
	}
}

func TestNameStyleTiers(t *testing.T) {
	if !NameStyleAllowed(NameStyleGradient, "crimson") {
		t.Fatal("gradient tier must include solid styles")
	}
	if NameStyleAllowed(NameStyleSolid, "rainbow") {
		t.Fatal("solid tier must not unlock gradients")
	}
	if _, known := NameStyleTierOf("not-a-style"); known {
		t.Fatal("unknown style accepted")
	}
}

func TestDrawTicketsIsDeterministicAndDistinct(t *testing.T) {
	seed := []byte("0123456789abcdef0123456789abcdef")
	round := uuid.MustParse("6b2f3d55-8c34-4a59-9a0b-6f3f3c1a2b10")
	first := DrawTickets(seed, round, 50, 14)
	second := DrawTickets(seed, round, 50, 14)
	if len(first) != 14 {
		t.Fatalf("drew %d tickets, want 14", len(first))
	}
	seen := map[int]bool{}
	for index, ticket := range first {
		if ticket != second[index] {
			t.Fatal("draw is not reproducible from the revealed seed")
		}
		if ticket < 1 || ticket > 50 || seen[ticket] {
			t.Fatalf("ticket %d is out of range or repeated", ticket)
		}
		seen[ticket] = true
	}
	if got := DrawTickets(seed, round, 3, 14); len(got) != 3 {
		t.Fatalf("drew %d tickets from 3 sold, want 3", len(got))
	}
	if other := DrawTickets([]byte("another seed value for the round"), round, 50, 14); equalInts(other, first) {
		t.Fatal("different seeds produced the same draw")
	}
}

func TestAllocatePrizesConservesPool(t *testing.T) {
	tiers := []LotteryTier{{Label: "一", Winners: 1, ShareBPS: 5000}, {Label: "二", Winners: 3, ShareBPS: 3000}, {Label: "三", Winners: 10, ShareBPS: 2000}}
	for _, tc := range []struct {
		carried, sales int64
		sold           int
	}{{0, 0, 0}, {5000, 0, 0}, {0, 1000, 10}, {12345, 9900, 99}, {0, 300, 3}} {
		allocation := AllocatePrizes(tc.carried, tc.sales, 1000, tiers, tc.sold)
		if allocation.Burned+allocation.PaidOut+allocation.CarriedOut != tc.carried+tc.sales {
			t.Fatalf("%+v does not conserve the pool %+v", allocation, tc)
		}
		if tc.sold == 0 && (allocation.Burned != 0 || allocation.PaidOut != 0) {
			t.Fatalf("empty round must roll everything over: %+v", allocation)
		}
	}
	partial := AllocatePrizes(0, 300, 0, tiers, 3)
	if partial.Tiers[0].FilledCount != 1 || partial.Tiers[1].FilledCount != 2 || partial.Tiers[2].FilledCount != 0 {
		t.Fatalf("tiers must fill in order: %+v", partial.Tiers)
	}
	if partial.CarriedOut <= 0 {
		t.Fatal("unfilled prizes must roll over")
	}
}

func TestNextDrawAtLeavesSalesWindow(t *testing.T) {
	location, _ := time.LoadLocation("Asia/Shanghai")
	before := time.Date(2026, 11, 13, 20, 0, 0, 0, location)
	if got := NextDrawAt(before, location, 21); !got.Equal(time.Date(2026, 11, 13, 21, 0, 0, 0, location)) {
		t.Fatalf("NextDrawAt(20:00) = %v", got.In(location))
	}
	late := time.Date(2026, 11, 13, 20, 55, 0, 0, location)
	if got := NextDrawAt(late, location, 21); !got.Equal(time.Date(2026, 11, 14, 21, 0, 0, 0, location)) {
		t.Fatalf("NextDrawAt(20:55) = %v", got.In(location))
	}
}

func TestPvPOutcomes(t *testing.T) {
	if outcome := RPSOutcome(RPSRock, RPSScissors); outcome.Tie || !outcome.CreatorWins {
		t.Fatal("rock must beat scissors")
	}
	if outcome := RPSOutcome(RPSRock, RPSPaper); outcome.Tie || outcome.CreatorWins {
		t.Fatal("paper must beat rock")
	}
	if !RPSOutcome(RPSPaper, RPSPaper).Tie {
		t.Fatal("same choice must tie")
	}
	rolls := []int{50, 50, 30, 80}
	outcome, err := DiceOutcome(func() (int, error) {
		value := rolls[0]
		rolls = rolls[1:]
		return value, nil
	})
	if err != nil || outcome.CreatorWins || outcome.CreatorRoll != 30 || outcome.OpponentRoll != 80 {
		t.Fatalf("dice must re-roll ties: %+v %v", outcome, err)
	}
	if fee := PvPFee(1000, 500); fee != 100 {
		t.Fatalf("PvPFee = %d, want 100", fee)
	}
}

func TestDrawScratchPrizeHonorsZeroWeights(t *testing.T) {
	prizes := []ScratchPrize{{Label: "a", Weight: 0}, {Label: "b", Weight: 5}, {Label: "c", Weight: 0}}
	for range 200 {
		index, err := drawScratchPrize(prizes)
		if err != nil || index != 1 {
			t.Fatalf("drawScratchPrize = %d, %v", index, err)
		}
	}
}

func equalInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
