package magicstore

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/economy"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func TestPostgresMagicShopAndGames(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("PEERGO_TEST_CORE_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("PEERGO_TEST_CORE_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	if err := platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		t.Fatalf("RequireCurrentMigration() error = %v", err)
	}
	economyRepository, err := economy.NewPostgresRepository(pool)
	if err != nil {
		t.Fatalf("economy repository: %v", err)
	}
	repository, err := NewPostgresRepository(pool)
	if err != nil {
		t.Fatalf("NewPostgresRepository() error = %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	alice := insertMagicIntegrationUser(t, ctx, pool, "alice", now)
	bob := insertMagicIntegrationUser(t, ctx, pool, "bob", now)
	creditMagicIntegrationUser(t, ctx, economyRepository, alice, 200000, now)
	creditMagicIntegrationUser(t, ctx, economyRepository, bob, 200000, now)

	t.Run("shop", func(t *testing.T) {
		quotedPrice := int64(799)
		makeup := PurchaseCommand{RequestID: uuid.New(), UserID: alice, ItemKey: ItemMakeupCard, Quantity: 2, ExpectedUnitPrice: &quotedPrice, Now: now}
		if _, err := repository.Purchase(ctx, makeup); !errors.Is(err, ErrShopPriceChanged) {
			t.Fatalf("Purchase(makeup with stale price) error = %v, want ErrShopPriceChanged", err)
		}
		quotedPrice = 800
		receipt, err := repository.Purchase(ctx, makeup)
		if err != nil || receipt.Order.TotalPrice != 1600 || receipt.BalanceAfter != 198400 {
			t.Fatalf("Purchase(makeup) = %+v, %v", receipt, err)
		}
		if replay, err := repository.Purchase(ctx, makeup); err != nil || !replay.Replayed || replay.Order.ID != receipt.Order.ID {
			t.Fatalf("Purchase(makeup replay) = %+v, %v", replay, err)
		}
		if _, err := repository.Purchase(ctx, PurchaseCommand{RequestID: uuid.New(), UserID: alice, ItemKey: ItemMakeupCard, Quantity: 9, Now: now}); !errors.Is(err, ErrHoldingLimit) {
			t.Fatalf("Purchase(makeup over holding) error = %v, want ErrHoldingLimit", err)
		}
		gradientCommand := PurchaseCommand{RequestID: uuid.New(), UserID: alice, ItemKey: ItemNameColorGradient, Quantity: 1, StyleKey: "rainbow", Now: now}
		gradient, err := repository.Purchase(ctx, gradientCommand)
		if err != nil || gradient.Order.EffectUntil == nil {
			t.Fatalf("Purchase(gradient) = %+v, %v", gradient, err)
		}
		gradientCommand.StyleKey = "ocean"
		if _, err := repository.Purchase(ctx, gradientCommand); !errors.Is(err, ErrIdempotencyConflict) {
			t.Fatalf("Purchase(replay with different style) error = %v, want ErrIdempotencyConflict", err)
		}
		if _, err := repository.Purchase(ctx, PurchaseCommand{RequestID: uuid.New(), UserID: alice, ItemKey: ItemNameColorSolid, Quantity: 1, StyleKey: "teal", Now: now}); !errors.Is(err, ErrNameStyleTier) {
			t.Fatalf("Purchase(solid while gradient active) error = %v, want ErrNameStyleTier", err)
		}
		if style, err := repository.SetNameStyle(ctx, alice, "crimson", now); err != nil || style.StyleKey != "crimson" || style.Tier != NameStyleGradient {
			t.Fatalf("SetNameStyle(crimson) = %+v, %v", style, err)
		}
		styles, err := repository.ActiveNameStyles(ctx, now)
		if err != nil || !containsStyle(styles, alice, "crimson") {
			t.Fatalf("ActiveNameStyles() = %+v, %v", styles, err)
		}
		vip, err := repository.Purchase(ctx, PurchaseCommand{RequestID: uuid.New(), UserID: alice, ItemKey: ItemVIP, Quantity: 1, Now: now})
		if err != nil || vip.Order.EffectUntil == nil || vip.Order.EffectUntil.Sub(now) < 29*24*time.Hour {
			t.Fatalf("Purchase(vip) = %+v, %v", vip, err)
		}
		temporary, err := repository.Purchase(ctx, PurchaseCommand{RequestID: uuid.New(), UserID: alice, ItemKey: ItemTemporaryInvite, Quantity: 1, Now: now})
		if err != nil && !errors.Is(err, ErrInviteIneligible) {
			t.Fatalf("Purchase(temporary invite) = %+v, %v", temporary, err)
		}
		overview, err := repository.ShopOverview(ctx, alice, now)
		if err != nil || overview.MakeupCards != 2 || overview.NameStyle == nil || !overview.VIP.Active || len(overview.RecentOrders) < 3 {
			t.Fatalf("ShopOverview() = %+v, %v", overview, err)
		}
	})

	t.Run("scratch", func(t *testing.T) {
		request := uuid.New()
		card, err := repository.PlayScratch(ctx, request, bob, now)
		if err != nil || card.Card.Price != 100 {
			t.Fatalf("PlayScratch() = %+v, %v", card, err)
		}
		if replay, err := repository.PlayScratch(ctx, request, bob, now); err != nil || !replay.Replayed || replay.Card.ID != card.Card.ID {
			t.Fatalf("PlayScratch(replay) = %+v, %v", replay, err)
		}
		if card.BalanceAfter != 200000-100+card.Card.Prize {
			t.Fatalf("scratch balance %d with prize %d", card.BalanceAfter, card.Card.Prize)
		}
	})

	t.Run("lottery", func(t *testing.T) {
		games, err := repository.GamesOverview(ctx, alice, now)
		if err != nil || games.CurrentRound == nil || games.CurrentRound.Seed != nil {
			t.Fatalf("GamesOverview() = %+v, %v", games.CurrentRound, err)
		}
		round := *games.CurrentRound
		alicePurchase := BuyLotteryTicketsCommand{RequestID: uuid.New(), UserID: alice, RoundID: &round.ID, Quantity: 5, Now: now}
		if _, err := repository.BuyLotteryTickets(ctx, alicePurchase); err != nil {
			t.Fatalf("BuyLotteryTickets(alice) error = %v", err)
		}
		bobTickets, err := repository.BuyLotteryTickets(ctx, BuyLotteryTicketsCommand{
			RequestID: uuid.New(), UserID: bob, RoundID: &round.ID, Quantity: 3, Now: now,
		})
		if err != nil || bobTickets.FirstTicket != round.TicketsSold+6 || bobTickets.LastTicket != round.TicketsSold+8 {
			t.Fatalf("BuyLotteryTickets(bob) = %+v, %v", bobTickets, err)
		}
		if _, err := repository.BuyLotteryTickets(ctx, BuyLotteryTicketsCommand{
			RequestID: uuid.New(), UserID: bob, RoundID: &round.ID, Quantity: 1000, Now: now,
		}); !errors.Is(err, ErrLotteryTicketLimit) {
			t.Fatalf("BuyLotteryTickets(over limit) error = %v, want ErrLotteryTicketLimit", err)
		}
		if drawn, err := repository.DrawDueRound(ctx, round.DrawAt.Add(-time.Minute)); err != nil || drawn {
			t.Fatalf("DrawDueRound(before draw) = %v, %v", drawn, err)
		}
		if drawn, err := repository.DrawDueRound(ctx, round.DrawAt.Add(time.Minute)); err != nil || !drawn {
			t.Fatalf("DrawDueRound(after draw) = %v, %v", drawn, err)
		}
		after, err := repository.GamesOverview(ctx, alice, round.DrawAt.Add(2*time.Minute))
		if err != nil || after.PreviousRound == nil || after.PreviousRound.ID != round.ID || after.PreviousRound.Seed == nil {
			t.Fatalf("GamesOverview(after draw) previous = %+v, %v", after.PreviousRound, err)
		}
		previous := after.PreviousRound
		if sha256.Sum256(previous.Seed) != previous.SeedSHA256 {
			t.Fatal("revealed seed does not match the published digest")
		}
		if *previous.Burned+*previous.PaidOut+*previous.CarriedOut != previous.CarriedIn+previous.SalesTotal {
			t.Fatalf("drawn round does not conserve its pool: %+v", previous)
		}
		if after.CurrentRound == nil || after.CurrentRound.CarriedIn != *previous.CarriedOut {
			t.Fatalf("next round carried_in = %+v, want %d", after.CurrentRound, *previous.CarriedOut)
		}
		alicePurchase.Now = round.DrawAt.Add(2 * time.Minute)
		replayed, err := repository.BuyLotteryTickets(ctx, alicePurchase)
		if err != nil || !replayed.Replayed || replayed.RoundID != round.ID {
			t.Fatalf("BuyLotteryTickets(replay after draw) = %+v, %v", replayed, err)
		}
		wrongRound := alicePurchase
		wrongRound.RoundID = &after.CurrentRound.ID
		if _, err := repository.BuyLotteryTickets(ctx, wrongRound); !errors.Is(err, ErrIdempotencyConflict) {
			t.Fatalf("BuyLotteryTickets(same key, different round) error = %v, want ErrIdempotencyConflict", err)
		}
		alicePurchase.RequestID = uuid.New()
		if _, err := repository.BuyLotteryTickets(ctx, alicePurchase); !errors.Is(err, ErrLotteryClosed) {
			t.Fatalf("BuyLotteryTickets(stale round) error = %v, want ErrLotteryClosed", err)
		}
		var escrow int64
		if err := pool.QueryRow(ctx, `SELECT balance FROM economy.magic_accounts WHERE id = $1`, economy.LotteryPoolEscrowID()).Scan(&escrow); err != nil {
			t.Fatalf("read lottery escrow: %v", err)
		}
		if escrow < after.CurrentRound.CarriedIn {
			t.Fatalf("lottery escrow %d cannot cover carried pool %d", escrow, after.CurrentRound.CarriedIn)
		}
	})

	t.Run("pvp", func(t *testing.T) {
		dice, err := repository.CreateChallenge(ctx, CreateChallengeCommand{RequestID: uuid.New(), UserID: alice, Mode: PvPDice, Stake: 1000, Now: now})
		if err != nil || dice.Status != PvPOpen {
			t.Fatalf("CreateChallenge(dice) = %+v, %v", dice, err)
		}
		if _, err := repository.AcceptChallenge(ctx, dice.ID, alice, "", now); !errors.Is(err, ErrChallengeOwn) {
			t.Fatalf("AcceptChallenge(own) error = %v, want ErrChallengeOwn", err)
		}
		settled, err := repository.AcceptChallenge(ctx, dice.ID, bob, "", now)
		if err != nil || settled.Status != PvPSettled || settled.WinnerID == nil || settled.Fee == nil || *settled.Fee != 100 {
			t.Fatalf("AcceptChallenge(dice) = %+v, %v", settled, err)
		}
		if retried, err := repository.AcceptChallenge(ctx, dice.ID, bob, "", now); err != nil || *retried.WinnerID != *settled.WinnerID {
			t.Fatalf("AcceptChallenge(opponent retry) = %+v, %v; want the settled result", retried, err)
		}

		rpsCommand := CreateChallengeCommand{RequestID: uuid.New(), UserID: alice, Mode: PvPRPS, Stake: 500, Choice: RPSRock, Message: "来战", Now: now}
		rps, err := repository.CreateChallenge(ctx, rpsCommand)
		if err != nil {
			t.Fatalf("CreateChallenge(rps) error = %v", err)
		}
		if retry, err := repository.CreateChallenge(ctx, rpsCommand); err != nil || retry.ID != rps.ID {
			t.Fatalf("CreateChallenge(replay) = %+v, %v", retry, err)
		}
		changedChoice := rpsCommand
		changedChoice.Choice = RPSPaper
		if _, err := repository.CreateChallenge(ctx, changedChoice); !errors.Is(err, ErrIdempotencyConflict) {
			t.Fatalf("CreateChallenge(replay with different choice) error = %v, want ErrIdempotencyConflict", err)
		}
		rpsCommand.Message = "再来一局"
		if _, err := repository.CreateChallenge(ctx, rpsCommand); !errors.Is(err, ErrIdempotencyConflict) {
			t.Fatalf("CreateChallenge(replay with different message) error = %v, want ErrIdempotencyConflict", err)
		}
		if _, err := repository.CancelChallenge(ctx, rps.ID, bob, now); !errors.Is(err, ErrChallengeForbidden) {
			t.Fatalf("CancelChallenge(other) error = %v, want ErrChallengeForbidden", err)
		}
		if cancelled, err := repository.CancelChallenge(ctx, rps.ID, alice, now); err != nil || cancelled.Status != PvPCancelled {
			t.Fatalf("CancelChallenge(own) = %+v, %v", cancelled, err)
		}
		if replay, err := repository.CancelChallenge(ctx, rps.ID, alice, now); err != nil || replay.Status != PvPCancelled {
			t.Fatalf("CancelChallenge(replay) = %+v, %v", replay, err)
		}
		if _, err := repository.AcceptChallenge(ctx, rps.ID, bob, RPSPaper, now); !errors.Is(err, ErrChallengeClosed) {
			t.Fatalf("AcceptChallenge(cancelled) error = %v, want ErrChallengeClosed", err)
		}

		tie, err := repository.CreateChallenge(ctx, CreateChallengeCommand{RequestID: uuid.New(), UserID: alice, Mode: PvPRPS, Stake: 300, Choice: RPSPaper, Now: now})
		if err != nil {
			t.Fatalf("CreateChallenge(tie) error = %v", err)
		}
		if result, err := repository.AcceptChallenge(ctx, tie.ID, bob, RPSPaper, now); err != nil || result.WinnerID != nil || result.Status != PvPSettled {
			t.Fatalf("AcceptChallenge(tie) = %+v, %v", result, err)
		}
		if _, err := repository.AcceptChallenge(ctx, tie.ID, bob, RPSScissors, now); !errors.Is(err, ErrIdempotencyConflict) {
			t.Fatalf("AcceptChallenge(replay with different choice) error = %v, want ErrIdempotencyConflict", err)
		}

		stale, err := repository.CreateChallenge(ctx, CreateChallengeCommand{RequestID: uuid.New(), UserID: bob, Mode: PvPDice, Stake: 200, Now: now})
		if err != nil {
			t.Fatalf("CreateChallenge(stale) error = %v", err)
		}
		expired, err := repository.ExpireChallenges(ctx, now.Add(25*time.Hour), 100)
		if err != nil || expired < 1 {
			t.Fatalf("ExpireChallenges() = %d, %v", expired, err)
		}
		var status string
		if err := pool.QueryRow(ctx, `SELECT status FROM economy.pvp_challenges WHERE id = $1`, stale.ID).Scan(&status); err != nil || status != "expired" {
			t.Fatalf("stale challenge status = %q, %v", status, err)
		}
		var escrow int64
		if err := pool.QueryRow(ctx, `SELECT balance FROM economy.magic_accounts WHERE id = $1`, economy.PvPChallengeEscrowID()).Scan(&escrow); err != nil || escrow != 0 {
			t.Fatalf("pvp escrow = %d, %v; every closed challenge must release its stakes", escrow, err)
		}
	})

	t.Run("admin", func(t *testing.T) {
		overview, err := repository.AdminOverview(ctx)
		if err != nil || overview.Version < 1 {
			t.Fatalf("AdminOverview() = %+v, %v", overview, err)
		}
		settings := overview.Settings
		settings.Scratch.DailyLimit = 30
		if _, err := repository.UpdateSettings(ctx, UpdateSettingsCommand{
			Settings: settings, ExpectedVersion: overview.Version + 7, Reason: "集成测试并发版本校验", OccurredAt: now,
		}); !errors.Is(err, ErrSettingsConflict) {
			t.Fatalf("UpdateSettings(stale version) error = %v, want ErrSettingsConflict", err)
		}
	})
}

func containsStyle(styles []NameStyle, userID uuid.UUID, key string) bool {
	for _, style := range styles {
		if style.UserID == userID && style.StyleKey == key {
			return true
		}
	}
	return false
}

func insertMagicIntegrationUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, role string, now time.Time) uuid.UUID {
	t.Helper()
	userID := uuid.New()
	username := fmt.Sprintf("magic-it-%s-%s", role, userID.String()[:8])
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.users (
    id, credential_ref, username, display_name, status, created_at, updated_at
) VALUES ($1, $2, $3, $3, 'active', $4, $4)`, userID, uuid.New(), username, now); err != nil {
		t.Fatalf("insert %s integration user: %v", role, err)
	}
	return userID
}

func creditMagicIntegrationUser(t *testing.T, ctx context.Context, repository *economy.PostgresRepository, userID uuid.UUID, amount int64, occurredAt time.Time) {
	t.Helper()
	transactionID := uuid.New()
	reference := "magic-it-credit:" + strings.ReplaceAll(transactionID.String(), "-", "")
	digest := sha256.Sum256([]byte(reference))
	if _, err := repository.Record(ctx, economy.RecordCommand{
		TransactionID: transactionID, TransactionType: economy.TransactionActivityReward,
		IdempotencyKey: reference, SourceReference: reference,
		PolicyRevision: "magic-integration-v1", PayloadSHA256: digest,
		OccurredAt: occurredAt, RecordedAt: occurredAt,
		Postings: []economy.PostingInput{
			{AccountID: economy.ActivityMintAccountID(), Amount: -amount},
			{AccountID: userID, Amount: amount},
		},
	}); err != nil {
		t.Fatalf("credit integration user %s: %v", userID, err)
	}
}
