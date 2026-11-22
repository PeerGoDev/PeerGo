package magicstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/peergo/peergo/services/core/internal/modules/economy"
)

var (
	scratchNamespace     = uuid.MustParse("6b0f8f51-3c1e-4a8a-9a52-6f1d2a4b9c11")
	lotteryNamespace     = uuid.MustParse("6b0f8f51-3c1e-4a8a-9a52-6f1d2a4b9c12")
	pvpNamespace         = uuid.MustParse("6b0f8f51-3c1e-4a8a-9a52-6f1d2a4b9c13")
	lotteryRoundLockName = "magic-lottery-round"
)

func lockLottery(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lotteryRoundLockName); err != nil {
		return fmt.Errorf("lock lottery: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------- scratch

const scratchColumns = `id, price, prize, prize_index, prize_label, purchased_at`

func scanScratch(row pgx.Row) (ScratchCard, error) {
	var card ScratchCard
	if err := row.Scan(&card.ID, &card.Price, &card.Prize, &card.PrizeIndex, &card.PrizeLabel, &card.PurchasedAt); err != nil {
		return ScratchCard{}, err
	}
	card.PurchasedAt = card.PurchasedAt.UTC()
	return card, nil
}

func scratchUsedSince(ctx context.Context, db querier, userID uuid.UUID, since time.Time) (int, error) {
	var count int
	if err := db.QueryRow(ctx, `
SELECT count(*)::int FROM economy.scratch_cards WHERE user_id = $1 AND purchased_at >= $2`,
		userID, since).Scan(&count); err != nil {
		return 0, fmt.Errorf("count scratch cards: %w", err)
	}
	return count, nil
}

func (repository *PostgresRepository) PlayScratch(ctx context.Context, requestID, userID uuid.UUID, now time.Time) (ScratchReceipt, error) {
	if requestID == uuid.Nil || userID == uuid.Nil || now.IsZero() {
		return ScratchReceipt{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ScratchReceipt{}, fmt.Errorf("begin scratch card: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockMember(ctx, tx, "magic-game", userID); err != nil {
		return ScratchReceipt{}, err
	}
	replay, err := scanScratch(tx.QueryRow(ctx, `SELECT `+scratchColumns+`
FROM economy.scratch_cards WHERE user_id = $1 AND request_id = $2`, userID, requestID))
	if err == nil {
		balance, err := memberBalance(ctx, tx, userID)
		if err != nil {
			return ScratchReceipt{}, err
		}
		return ScratchReceipt{Card: replay, BalanceAfter: balance, Replayed: true}, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ScratchReceipt{}, fmt.Errorf("read scratch replay: %w", err)
	}
	settings, version, _, err := readSettings(ctx, tx, false)
	if err != nil {
		return ScratchReceipt{}, err
	}
	game := settings.Scratch
	if !settings.GamesEnabled || !game.Enabled {
		return ScratchReceipt{}, ErrGameDisabled
	}
	used, err := scratchUsedSince(ctx, tx, userID, dayStart(now, settings.location()))
	if err != nil {
		return ScratchReceipt{}, err
	}
	if used >= game.DailyLimit {
		return ScratchReceipt{}, ErrDailyLimit
	}
	index, err := drawScratchPrize(game.Prizes)
	if err != nil {
		return ScratchReceipt{}, fmt.Errorf("draw scratch prize: %w", err)
	}
	cardID := uuid.NewSHA1(scratchNamespace, []byte(userID.String()+":"+requestID.String()))
	card := ScratchCard{
		ID: cardID, Price: game.Price, Prize: game.Prizes[index].Payout,
		PrizeIndex: index, PrizeLabel: game.Prizes[index].Label, PurchasedAt: now,
	}
	payload, err := payloadDigest(struct {
		CardID  string `json:"card_id"`
		UserID  string `json:"user_id"`
		Price   int64  `json:"price"`
		Prize   int64  `json:"prize"`
		Index   int    `json:"prize_index"`
		Version int64  `json:"settings_version"`
	}{cardID.String(), userID.String(), card.Price, card.Prize, index, version})
	if err != nil {
		return ScratchReceipt{}, err
	}
	policy := fmt.Sprintf("magic-store-v%d", version)
	source := "scratch-card:" + cardID.String()
	purchaseID := uuid.NewSHA1(scratchNamespace, append(cardID[:], 'p'))
	transaction, err := repository.economy.RecordInTransaction(ctx, tx, economy.RecordCommand{
		TransactionID: purchaseID, TransactionType: economy.TransactionScratchCardPurchase,
		IdempotencyKey: "scratch:" + compactUUID(cardID), SourceReference: source,
		PolicyRevision: policy, PayloadSHA256: payload, OccurredAt: now, RecordedAt: now,
		Postings: []economy.PostingInput{
			{AccountID: userID, Amount: -card.Price},
			{AccountID: economy.MagicGameHouseID(), Amount: card.Price},
		},
	})
	if err != nil {
		return ScratchReceipt{}, classifyLedgerError(err)
	}
	balance := balanceOf(transaction, userID)
	var payoutID *uuid.UUID
	if card.Prize > 0 {
		id := uuid.NewSHA1(scratchNamespace, append(cardID[:], 'w'))
		payout, err := repository.economy.RecordInTransaction(ctx, tx, economy.RecordCommand{
			TransactionID: id, TransactionType: economy.TransactionScratchCardPayout,
			IdempotencyKey: "scratch-payout:" + compactUUID(cardID), SourceReference: source,
			PolicyRevision: policy, PayloadSHA256: payload, OccurredAt: now, RecordedAt: now,
			Postings: []economy.PostingInput{
				{AccountID: economy.MagicGameHouseID(), Amount: -card.Prize},
				{AccountID: userID, Amount: card.Prize},
			},
		})
		if err != nil {
			return ScratchReceipt{}, classifyLedgerError(err)
		}
		balance = balanceOf(payout, userID)
		payoutID = &id
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO economy.scratch_cards (
    id, request_id, user_id, price, prize, prize_index, prize_label,
    settings_version, purchase_transaction_id, payout_transaction_id, purchased_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		cardID, requestID, userID, card.Price, card.Prize, index, card.PrizeLabel,
		version, purchaseID, payoutID, now); err != nil {
		return ScratchReceipt{}, fmt.Errorf("insert scratch card: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ScratchReceipt{}, fmt.Errorf("commit scratch card: %w", err)
	}
	return ScratchReceipt{Card: card, BalanceAfter: balance}, nil
}

// ---------------------------------------------------------------- lottery

const roundColumns = `id, round_number, status, ticket_price, maximum_tickets_per_member,
burn_bps, tiers, opens_at, draw_at, seed_sha256, seed, carried_in, tickets_sold,
sales_total, burned, paid_out, carried_out, drawn_at`

func scanRound(row pgx.Row) (LotteryRound, error) {
	var round LotteryRound
	var tiers []byte
	var digest []byte
	var burned, paid, carried pgtype.Int8
	var drawnAt pgtype.Timestamptz
	if err := row.Scan(&round.ID, &round.RoundNumber, &round.Status, &round.TicketPrice,
		&round.MaximumTicketsPerMember, &round.BurnBPS, &tiers, &round.OpensAt, &round.DrawAt,
		&digest, &round.Seed, &round.CarriedIn, &round.TicketsSold, &round.SalesTotal,
		&burned, &paid, &carried, &drawnAt); err != nil {
		return LotteryRound{}, err
	}
	if err := json.Unmarshal(tiers, &round.Tiers); err != nil {
		return LotteryRound{}, fmt.Errorf("decode lottery tiers: %w", err)
	}
	copy(round.SeedSHA256[:], digest)
	round.OpensAt, round.DrawAt = round.OpensAt.UTC(), round.DrawAt.UTC()
	if burned.Valid {
		round.Burned, round.PaidOut, round.CarriedOut = &burned.Int64, &paid.Int64, &carried.Int64
	}
	if drawnAt.Valid {
		value := drawnAt.Time.UTC()
		round.DrawnAt = &value
	}
	return round, nil
}

// ensureOpenRound returns the open round, creating one when the lottery is
// enabled and none exists. Callers must hold the lottery advisory lock.
func ensureOpenRound(ctx context.Context, tx pgx.Tx, settings Settings, now time.Time) (*LotteryRound, error) {
	round, err := scanRound(tx.QueryRow(ctx, `SELECT `+roundColumns+`
FROM economy.lottery_rounds WHERE status = 'open' FOR UPDATE`))
	if err == nil {
		return &round, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("read open lottery round: %w", err)
	}
	if !settings.GamesEnabled || !settings.Lottery.Enabled {
		return nil, nil
	}
	var carriedIn int64
	if err := tx.QueryRow(ctx, `
SELECT COALESCE((SELECT carried_out FROM economy.lottery_rounds
                 WHERE status = 'drawn' ORDER BY round_number DESC LIMIT 1), 0)::bigint`).Scan(&carriedIn); err != nil {
		return nil, fmt.Errorf("read lottery rollover: %w", err)
	}
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return nil, fmt.Errorf("generate lottery seed: %w", err)
	}
	digest := sha256.Sum256(seed)
	tiers, err := json.Marshal(settings.Lottery.Tiers)
	if err != nil {
		return nil, fmt.Errorf("encode lottery tiers: %w", err)
	}
	id := uuid.New()
	drawAt := NextDrawAt(now, settings.location(), settings.Lottery.DrawHour)
	created, err := scanRound(tx.QueryRow(ctx, `
INSERT INTO economy.lottery_rounds (
    id, status, ticket_price, maximum_tickets_per_member, burn_bps, tiers,
    opens_at, draw_at, seed_sha256, seed, carried_in, created_at, updated_at
) VALUES ($1, 'open', $2, $3, $4, $5, $6, $7, $8, $9, $10, $6, $6)
RETURNING `+roundColumns,
		id, settings.Lottery.TicketPrice, settings.Lottery.MaximumTicketsPerRound,
		settings.Lottery.BurnBPS, tiers, now, drawAt, digest[:], seed, carriedIn))
	if err != nil {
		return nil, fmt.Errorf("create lottery round: %w", err)
	}
	return &created, nil
}

func memberTickets(ctx context.Context, db querier, roundID, userID uuid.UUID) (int, [][2]int, error) {
	rows, err := db.Query(ctx, `
SELECT first_ticket, last_ticket FROM economy.lottery_purchases
WHERE round_id = $1 AND user_id = $2 ORDER BY first_ticket`, roundID, userID)
	if err != nil {
		return 0, nil, fmt.Errorf("list member lottery tickets: %w", err)
	}
	defer rows.Close()
	total, ranges := 0, make([][2]int, 0, 4)
	for rows.Next() {
		var first, last int
		if err := rows.Scan(&first, &last); err != nil {
			return 0, nil, fmt.Errorf("scan member lottery tickets: %w", err)
		}
		total += last - first + 1
		ranges = append(ranges, [2]int{first, last})
	}
	return total, ranges, rows.Err()
}

func (repository *PostgresRepository) BuyLotteryTickets(ctx context.Context, command BuyLotteryTicketsCommand) (LotteryReceipt, error) {
	requestID, userID, quantity, now := command.RequestID, command.UserID, command.Quantity, command.Now
	if requestID == uuid.Nil || userID == uuid.Nil || quantity < 1 || quantity > 1000 || now.IsZero() ||
		(command.RoundID != nil && *command.RoundID == uuid.Nil) {
		return LotteryReceipt{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return LotteryReceipt{}, fmt.Errorf("begin lottery purchase: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockLottery(ctx, tx); err != nil {
		return LotteryReceipt{}, err
	}
	var replay LotteryReceipt
	err = tx.QueryRow(ctx, `
SELECT purchase.round_id, round.round_number, purchase.quantity, purchase.first_ticket,
       purchase.last_ticket, purchase.total_price
FROM economy.lottery_purchases AS purchase
JOIN economy.lottery_rounds AS round ON round.id = purchase.round_id
WHERE purchase.user_id = $1 AND purchase.request_id = $2`, userID, requestID).Scan(
		&replay.RoundID, &replay.RoundNumber, &replay.Quantity, &replay.FirstTicket,
		&replay.LastTicket, &replay.TotalPrice)
	if err == nil {
		if replay.Quantity != quantity || (command.RoundID != nil && replay.RoundID != *command.RoundID) {
			return LotteryReceipt{}, ErrIdempotencyConflict
		}
		if replay.BalanceAfter, err = memberBalance(ctx, tx, userID); err != nil {
			return LotteryReceipt{}, err
		}
		replay.Replayed = true
		return replay, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return LotteryReceipt{}, fmt.Errorf("read lottery replay: %w", err)
	}
	settings, _, _, err := readSettings(ctx, tx, false)
	if err != nil {
		return LotteryReceipt{}, err
	}
	if !settings.GamesEnabled || !settings.Lottery.Enabled {
		return LotteryReceipt{}, ErrGameDisabled
	}
	round, err := ensureOpenRound(ctx, tx, settings, now)
	if err != nil {
		return LotteryReceipt{}, err
	}
	if round == nil {
		return LotteryReceipt{}, ErrGameDisabled
	}
	if !round.DrawAt.After(now) || (command.RoundID != nil && round.ID != *command.RoundID) {
		return LotteryReceipt{}, ErrLotteryClosed
	}
	held, _, err := memberTickets(ctx, tx, round.ID, userID)
	if err != nil {
		return LotteryReceipt{}, err
	}
	if held+quantity > round.MaximumTicketsPerMember {
		return LotteryReceipt{}, ErrLotteryTicketLimit
	}
	purchaseID := uuid.NewSHA1(lotteryNamespace, []byte(userID.String()+":"+requestID.String()))
	total := round.TicketPrice * int64(quantity)
	first := round.TicketsSold + 1
	payload, err := payloadDigest(struct {
		PurchaseID string `json:"purchase_id"`
		RoundID    string `json:"round_id"`
		UserID     string `json:"user_id"`
		Quantity   int    `json:"quantity"`
		First      int    `json:"first_ticket"`
		Total      int64  `json:"total_price"`
	}{purchaseID.String(), round.ID.String(), userID.String(), quantity, first, total})
	if err != nil {
		return LotteryReceipt{}, err
	}
	transactionID := uuid.NewSHA1(lotteryNamespace, append(purchaseID[:], 't'))
	transaction, err := repository.economy.RecordInTransaction(ctx, tx, economy.RecordCommand{
		TransactionID: transactionID, TransactionType: economy.TransactionLotteryTicket,
		IdempotencyKey:  "lottery-ticket:" + compactUUID(purchaseID),
		SourceReference: "lottery-purchase:" + purchaseID.String(),
		PolicyRevision:  "lottery-round-" + strconv.FormatInt(round.RoundNumber, 10),
		PayloadSHA256:   payload, OccurredAt: now, RecordedAt: now,
		Postings: []economy.PostingInput{
			{AccountID: userID, Amount: -total},
			{AccountID: economy.LotteryPoolEscrowID(), Amount: total},
		},
	})
	if err != nil {
		return LotteryReceipt{}, classifyLedgerError(err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO economy.lottery_purchases (
    id, request_id, round_id, user_id, quantity, first_ticket, last_ticket,
    total_price, magic_transaction_id, purchased_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		purchaseID, requestID, round.ID, userID, quantity, first, first+quantity-1,
		total, transactionID, now); err != nil {
		return LotteryReceipt{}, fmt.Errorf("insert lottery purchase: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE economy.lottery_rounds
SET tickets_sold = tickets_sold + $2, sales_total = sales_total + $3,
    version = version + 1, updated_at = $4
WHERE id = $1`, round.ID, quantity, total, now); err != nil {
		return LotteryReceipt{}, fmt.Errorf("update lottery round sales: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return LotteryReceipt{}, fmt.Errorf("commit lottery purchase: %w", err)
	}
	return LotteryReceipt{
		RoundID: round.ID, RoundNumber: round.RoundNumber, Quantity: quantity,
		FirstTicket: first, LastTicket: first + quantity - 1, TotalPrice: total,
		BalanceAfter: balanceOf(transaction, userID),
	}, nil
}

type ticketOwner struct {
	first, last int
	userID      uuid.UUID
}

// DrawDueRound draws the open round once its draw time has passed and opens
// the next one. It reports whether a round was drawn.
func (repository *PostgresRepository) DrawDueRound(ctx context.Context, now time.Time) (bool, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin lottery draw: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockLottery(ctx, tx); err != nil {
		return false, err
	}
	settings, _, _, err := readSettings(ctx, tx, false)
	if err != nil {
		return false, err
	}
	round, err := scanRound(tx.QueryRow(ctx, `SELECT `+roundColumns+`
FROM economy.lottery_rounds WHERE status = 'open' FOR UPDATE`))
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := ensureOpenRound(ctx, tx, settings, now); err != nil {
			return false, err
		}
		return false, tx.Commit(ctx)
	}
	if err != nil {
		return false, fmt.Errorf("read open lottery round: %w", err)
	}
	if round.DrawAt.After(now) {
		return false, tx.Commit(ctx)
	}

	rows, err := tx.Query(ctx, `
SELECT first_ticket, last_ticket, user_id FROM economy.lottery_purchases
WHERE round_id = $1 ORDER BY first_ticket`, round.ID)
	if err != nil {
		return false, fmt.Errorf("list lottery purchases: %w", err)
	}
	owners := make([]ticketOwner, 0, 64)
	for rows.Next() {
		var owner ticketOwner
		if err := rows.Scan(&owner.first, &owner.last, &owner.userID); err != nil {
			rows.Close()
			return false, fmt.Errorf("scan lottery purchase: %w", err)
		}
		owners = append(owners, owner)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("finish lottery purchases: %w", err)
	}

	allocation := AllocatePrizes(round.CarriedIn, round.SalesTotal, round.BurnBPS, round.Tiers, round.TicketsSold)
	totalWinners := 0
	for _, tier := range allocation.Tiers {
		totalWinners += tier.FilledCount
	}
	tickets := DrawTickets(round.Seed, round.ID, round.TicketsSold, totalWinners)
	policy := "lottery-round-" + strconv.FormatInt(round.RoundNumber, 10)
	payload, err := payloadDigest(struct {
		RoundID string `json:"round_id"`
		Seed    []byte `json:"seed"`
		Tickets []int  `json:"tickets"`
	}{round.ID.String(), round.Seed, tickets})
	if err != nil {
		return false, err
	}
	rank := 0
	for tierIndex, tier := range allocation.Tiers {
		for range tier.FilledCount {
			ticket := tickets[rank]
			rank++
			ownerIndex := sort.Search(len(owners), func(index int) bool { return owners[index].last >= ticket })
			if ownerIndex >= len(owners) || owners[ownerIndex].first > ticket {
				return false, errors.New("lottery winning ticket has no owner")
			}
			winner := owners[ownerIndex].userID
			var transactionID *uuid.UUID
			if tier.PrizeEach > 0 {
				id := uuid.NewSHA1(lotteryNamespace, []byte(fmt.Sprintf("%s:payout:%d", round.ID, rank)))
				if _, err := repository.economy.RecordInTransaction(ctx, tx, economy.RecordCommand{
					TransactionID: id, TransactionType: economy.TransactionLotteryPayout,
					IdempotencyKey:  fmt.Sprintf("lottery-payout:%s:%d", compactUUID(round.ID), rank),
					SourceReference: fmt.Sprintf("lottery-round:%s:%d", round.ID, rank),
					PolicyRevision:  policy, PayloadSHA256: payload, OccurredAt: now, RecordedAt: now,
					Postings: []economy.PostingInput{
						{AccountID: economy.LotteryPoolEscrowID(), Amount: -tier.PrizeEach},
						{AccountID: winner, Amount: tier.PrizeEach},
					},
				}); err != nil {
					return false, fmt.Errorf("pay lottery winner: %w", err)
				}
				transactionID = &id
			}
			if _, err := tx.Exec(ctx, `
INSERT INTO economy.lottery_winners (
    round_id, draw_rank, tier_index, tier_label, ticket_number, user_id, prize, magic_transaction_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
				round.ID, rank, tierIndex, tier.Label, ticket, winner, tier.PrizeEach, transactionID); err != nil {
				return false, fmt.Errorf("insert lottery winner: %w", err)
			}
		}
	}
	if allocation.Burned > 0 {
		if _, err := repository.economy.RecordInTransaction(ctx, tx, economy.RecordCommand{
			TransactionID:   uuid.NewSHA1(lotteryNamespace, []byte(round.ID.String()+":burn")),
			TransactionType: economy.TransactionLotteryBurn,
			IdempotencyKey:  "lottery-burn:" + compactUUID(round.ID),
			SourceReference: "lottery-round:" + round.ID.String(),
			PolicyRevision:  policy, PayloadSHA256: payload, OccurredAt: now, RecordedAt: now,
			Postings: []economy.PostingInput{
				{AccountID: economy.LotteryPoolEscrowID(), Amount: -allocation.Burned},
				{AccountID: economy.MagicGameHouseID(), Amount: allocation.Burned},
			},
		}); err != nil {
			return false, fmt.Errorf("burn lottery share: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
UPDATE economy.lottery_rounds
SET status = 'drawn', burned = $2, paid_out = $3, carried_out = $4,
    drawn_at = $5, version = version + 1, updated_at = $5
WHERE id = $1`, round.ID, allocation.Burned, allocation.PaidOut, allocation.CarriedOut, now); err != nil {
		return false, fmt.Errorf("close lottery round: %w", err)
	}
	if _, err := ensureOpenRound(ctx, tx, settings, now); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit lottery draw: %w", err)
	}
	return true, nil
}

func readWinners(ctx context.Context, db querier, roundID uuid.UUID) ([]LotteryWinner, error) {
	rows, err := db.Query(ctx, `
SELECT winner.draw_rank, winner.tier_index, winner.tier_label, winner.ticket_number,
       winner.user_id, account.username, winner.prize
FROM economy.lottery_winners AS winner
JOIN identity.users AS account ON account.id = winner.user_id
WHERE winner.round_id = $1 ORDER BY winner.draw_rank`, roundID)
	if err != nil {
		return nil, fmt.Errorf("list lottery winners: %w", err)
	}
	defer rows.Close()
	result := make([]LotteryWinner, 0, 16)
	for rows.Next() {
		var winner LotteryWinner
		if err := rows.Scan(&winner.DrawRank, &winner.TierIndex, &winner.TierLabel, &winner.TicketNumber,
			&winner.UserID, &winner.Username, &winner.Prize); err != nil {
			return nil, fmt.Errorf("scan lottery winner: %w", err)
		}
		result = append(result, winner)
	}
	return result, rows.Err()
}

// ---------------------------------------------------------------- pvp

const challengeSelect = `
SELECT challenge.id, challenge.mode, challenge.status, challenge.creator_id, creator.username,
       challenge.creator_choice, challenge.stake, challenge.fee_bps, challenge.message,
       challenge.opponent_id, opponent.username, challenge.opponent_choice,
       challenge.creator_roll, challenge.opponent_roll, challenge.winner_id, challenge.fee,
       challenge.expires_at, challenge.created_at, challenge.closed_at, challenge.version
FROM economy.pvp_challenges AS challenge
JOIN identity.users AS creator ON creator.id = challenge.creator_id
LEFT JOIN identity.users AS opponent ON opponent.id = challenge.opponent_id`

func scanChallenge(row pgx.Row) (PvPChallenge, error) {
	var challenge PvPChallenge
	var mode, status string
	var creatorChoice, opponentChoice, message, opponentName pgtype.Text
	var opponentID, winnerID pgtype.UUID
	var creatorRoll, opponentRoll pgtype.Int4
	var fee pgtype.Int8
	var closedAt pgtype.Timestamptz
	if err := row.Scan(&challenge.ID, &mode, &status, &challenge.CreatorID, &challenge.CreatorUsername,
		&creatorChoice, &challenge.Stake, &challenge.FeeBPS, &message,
		&opponentID, &opponentName, &opponentChoice, &creatorRoll, &opponentRoll, &winnerID, &fee,
		&challenge.ExpiresAt, &challenge.CreatedAt, &closedAt, &challenge.Version); err != nil {
		return PvPChallenge{}, err
	}
	challenge.Mode, challenge.Status = PvPMode(mode), PvPStatus(status)
	if creatorChoice.Valid {
		value := RPSChoice(creatorChoice.String)
		challenge.CreatorChoice = &value
	}
	if opponentChoice.Valid {
		value := RPSChoice(opponentChoice.String)
		challenge.OpponentChoice = &value
	}
	if message.Valid {
		challenge.Message = &message.String
	}
	if opponentID.Valid {
		value := uuid.UUID(opponentID.Bytes)
		challenge.OpponentID = &value
		challenge.OpponentUsername = &opponentName.String
	}
	if winnerID.Valid {
		value := uuid.UUID(winnerID.Bytes)
		challenge.WinnerID = &value
	}
	if creatorRoll.Valid {
		value := int(creatorRoll.Int32)
		challenge.CreatorRoll = &value
	}
	if opponentRoll.Valid {
		value := int(opponentRoll.Int32)
		challenge.OpponentRoll = &value
	}
	if fee.Valid {
		challenge.Fee = &fee.Int64
	}
	if closedAt.Valid {
		value := closedAt.Time.UTC()
		challenge.ClosedAt = &value
	}
	challenge.ExpiresAt, challenge.CreatedAt = challenge.ExpiresAt.UTC(), challenge.CreatedAt.UTC()
	return challenge, nil
}

func listChallenges(ctx context.Context, db querier, suffix string, arguments ...any) ([]PvPChallenge, error) {
	rows, err := db.Query(ctx, challengeSelect+" "+suffix, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list pvp challenges: %w", err)
	}
	defer rows.Close()
	result := make([]PvPChallenge, 0, 16)
	for rows.Next() {
		challenge, err := scanChallenge(rows)
		if err != nil {
			return nil, fmt.Errorf("scan pvp challenge: %w", err)
		}
		result = append(result, challenge)
	}
	return result, rows.Err()
}

func (repository *PostgresRepository) CreateChallenge(ctx context.Context, command CreateChallengeCommand) (PvPChallenge, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PvPChallenge{}, fmt.Errorf("begin pvp challenge: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockMember(ctx, tx, "magic-game", command.UserID); err != nil {
		return PvPChallenge{}, err
	}
	replay, err := scanChallenge(tx.QueryRow(ctx, challengeSelect+`
WHERE challenge.creator_id = $1 AND challenge.request_id = $2`, command.UserID, command.RequestID))
	if err == nil {
		var choice RPSChoice
		if replay.CreatorChoice != nil {
			choice = *replay.CreatorChoice
		}
		var message string
		if replay.Message != nil {
			message = *replay.Message
		}
		if replay.Stake != command.Stake || replay.Mode != command.Mode || choice != command.Choice || message != command.Message {
			return PvPChallenge{}, ErrIdempotencyConflict
		}
		return replay, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return PvPChallenge{}, fmt.Errorf("read pvp replay: %w", err)
	}
	settings, _, _, err := readSettings(ctx, tx, false)
	if err != nil {
		return PvPChallenge{}, err
	}
	game := settings.PvP
	if !settings.GamesEnabled || !game.Enabled {
		return PvPChallenge{}, ErrGameDisabled
	}
	if command.Stake < game.MinimumStake || command.Stake > game.MaximumStake {
		return PvPChallenge{}, ErrInput
	}
	var open int
	if err := tx.QueryRow(ctx, `
SELECT count(*)::int FROM economy.pvp_challenges
WHERE creator_id = $1 AND status = 'open' AND expires_at > $2`, command.UserID, command.Now).Scan(&open); err != nil {
		return PvPChallenge{}, fmt.Errorf("count open pvp challenges: %w", err)
	}
	if open >= game.MaximumOpenPerMember {
		return PvPChallenge{}, ErrChallengeOpenLimit
	}
	challengeID := uuid.NewSHA1(pvpNamespace, []byte(command.UserID.String()+":"+command.RequestID.String()))
	payload, err := payloadDigest(struct {
		ChallengeID string `json:"challenge_id"`
		UserID      string `json:"user_id"`
		Stake       int64  `json:"stake"`
		Mode        string `json:"mode"`
	}{challengeID.String(), command.UserID.String(), command.Stake, string(command.Mode)})
	if err != nil {
		return PvPChallenge{}, err
	}
	stakeID := uuid.NewSHA1(pvpNamespace, []byte(challengeID.String()+":creator"))
	if _, err := repository.economy.RecordInTransaction(ctx, tx, economy.RecordCommand{
		TransactionID: stakeID, TransactionType: economy.TransactionPvPStake,
		IdempotencyKey:  "pvp-stake:" + compactUUID(challengeID) + ":creator",
		SourceReference: "pvp-challenge:" + challengeID.String(),
		PolicyRevision:  "pvp-v1", PayloadSHA256: payload, OccurredAt: command.Now, RecordedAt: command.Now,
		Postings: []economy.PostingInput{
			{AccountID: command.UserID, Amount: -command.Stake},
			{AccountID: economy.PvPChallengeEscrowID(), Amount: command.Stake},
		},
	}); err != nil {
		return PvPChallenge{}, classifyLedgerError(err)
	}
	var choice, message *string
	if command.Mode == PvPRPS {
		value := string(command.Choice)
		choice = &value
	}
	if command.Message != "" {
		message = &command.Message
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO economy.pvp_challenges (
    id, request_id, mode, status, creator_id, creator_choice, stake, fee_bps, message,
    stake_transaction_id, expires_at, created_at, updated_at
) VALUES ($1, $2, $3, 'open', $4, $5, $6, $7, $8, $9, $10, $11, $11)`,
		challengeID, command.RequestID, string(command.Mode), command.UserID, choice, command.Stake,
		game.FeeBPS, message, stakeID, command.Now.Add(time.Duration(game.ExpiryHours)*time.Hour),
		command.Now); err != nil {
		return PvPChallenge{}, fmt.Errorf("insert pvp challenge: %w", err)
	}
	created, err := scanChallenge(tx.QueryRow(ctx, challengeSelect+` WHERE challenge.id = $1`, challengeID))
	if err != nil {
		return PvPChallenge{}, fmt.Errorf("read created pvp challenge: %w", err)
	}
	return created, tx.Commit(ctx)
}

func (repository *PostgresRepository) AcceptChallenge(ctx context.Context, challengeID, userID uuid.UUID, choice RPSChoice, now time.Time) (PvPChallenge, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PvPChallenge{}, fmt.Errorf("begin pvp accept: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockMember(ctx, tx, "magic-game", userID); err != nil {
		return PvPChallenge{}, err
	}
	challenge, err := scanChallenge(tx.QueryRow(ctx, challengeSelect+`
WHERE challenge.id = $1 FOR UPDATE OF challenge`, challengeID))
	if errors.Is(err, pgx.ErrNoRows) {
		return PvPChallenge{}, ErrChallengeNotFound
	}
	if err != nil {
		return PvPChallenge{}, fmt.Errorf("lock pvp challenge: %w", err)
	}
	if challenge.Status == PvPSettled && challenge.OpponentID != nil && *challenge.OpponentID == userID {
		var previousChoice RPSChoice
		if challenge.OpponentChoice != nil {
			previousChoice = *challenge.OpponentChoice
		}
		if choice != previousChoice {
			return PvPChallenge{}, ErrIdempotencyConflict
		}
		return challenge, tx.Commit(ctx)
	}
	if challenge.CreatorID == userID {
		return PvPChallenge{}, ErrChallengeOwn
	}
	if challenge.Status != PvPOpen || !challenge.ExpiresAt.After(now) {
		return PvPChallenge{}, ErrChallengeClosed
	}
	if (challenge.Mode == PvPRPS && !validChoice(choice)) || (challenge.Mode == PvPDice && choice != "") {
		return PvPChallenge{}, ErrInput
	}
	settings, _, _, err := readSettings(ctx, tx, false)
	if err != nil {
		return PvPChallenge{}, err
	}
	if !settings.GamesEnabled || !settings.PvP.Enabled {
		return PvPChallenge{}, ErrGameDisabled
	}
	source := "pvp-challenge:" + challengeID.String()
	payload, err := payloadDigest(struct {
		ChallengeID string `json:"challenge_id"`
		Opponent    string `json:"opponent_id"`
		Stake       int64  `json:"stake"`
	}{challengeID.String(), userID.String(), challenge.Stake})
	if err != nil {
		return PvPChallenge{}, err
	}
	acceptID := uuid.NewSHA1(pvpNamespace, []byte(challengeID.String()+":opponent"))
	if _, err := repository.economy.RecordInTransaction(ctx, tx, economy.RecordCommand{
		TransactionID: acceptID, TransactionType: economy.TransactionPvPStake,
		IdempotencyKey:  "pvp-stake:" + compactUUID(challengeID) + ":opponent",
		SourceReference: source, PolicyRevision: "pvp-v1", PayloadSHA256: payload,
		OccurredAt: now, RecordedAt: now,
		Postings: []economy.PostingInput{
			{AccountID: userID, Amount: -challenge.Stake},
			{AccountID: economy.PvPChallengeEscrowID(), Amount: challenge.Stake},
		},
	}); err != nil {
		return PvPChallenge{}, classifyLedgerError(err)
	}

	var outcome PvPOutcome
	if challenge.Mode == PvPRPS {
		outcome = RPSOutcome(*challenge.CreatorChoice, choice)
	} else if outcome, err = DiceOutcome(secureRoll); err != nil {
		return PvPChallenge{}, fmt.Errorf("roll pvp dice: %w", err)
	}
	pot := challenge.Stake * 2
	closingID := uuid.NewSHA1(pvpNamespace, []byte(challengeID.String()+":close"))
	closing := economy.RecordCommand{
		TransactionID: closingID, IdempotencyKey: "pvp-close:" + compactUUID(challengeID),
		SourceReference: source, PolicyRevision: "pvp-v1", PayloadSHA256: payload,
		OccurredAt: now, RecordedAt: now,
	}
	fee := int64(0)
	var winner *uuid.UUID
	if outcome.Tie {
		closing.TransactionType = economy.TransactionPvPRefund
		closing.Postings = []economy.PostingInput{
			{AccountID: economy.PvPChallengeEscrowID(), Amount: -pot},
			{AccountID: challenge.CreatorID, Amount: challenge.Stake},
			{AccountID: userID, Amount: challenge.Stake},
		}
	} else {
		fee = PvPFee(challenge.Stake, challenge.FeeBPS)
		winnerID := userID
		if outcome.CreatorWins {
			winnerID = challenge.CreatorID
		}
		winner = &winnerID
		closing.TransactionType = economy.TransactionPvPPayout
		closing.Postings = []economy.PostingInput{
			{AccountID: economy.PvPChallengeEscrowID(), Amount: -pot},
			{AccountID: winnerID, Amount: pot - fee},
		}
		if fee > 0 {
			closing.Postings = append(closing.Postings, economy.PostingInput{AccountID: economy.MagicGameHouseID(), Amount: fee})
		}
	}
	if _, err := repository.economy.RecordInTransaction(ctx, tx, closing); err != nil {
		return PvPChallenge{}, fmt.Errorf("settle pvp challenge: %w", err)
	}
	var opponentChoice *string
	if challenge.Mode == PvPRPS {
		value := string(choice)
		opponentChoice = &value
	}
	var creatorRoll, opponentRoll *int
	if challenge.Mode == PvPDice {
		creatorRoll, opponentRoll = &outcome.CreatorRoll, &outcome.OpponentRoll
	}
	if _, err := tx.Exec(ctx, `
UPDATE economy.pvp_challenges
SET status = 'settled', opponent_id = $2, opponent_choice = $3, creator_roll = $4,
    opponent_roll = $5, winner_id = $6, fee = $7, accept_transaction_id = $8,
    closing_transaction_id = $9, closed_at = $10, version = version + 1, updated_at = $10
WHERE id = $1`, challengeID, userID, opponentChoice, creatorRoll, opponentRoll, winner, fee,
		acceptID, closingID, now); err != nil {
		return PvPChallenge{}, fmt.Errorf("record pvp settlement: %w", err)
	}
	settled, err := scanChallenge(tx.QueryRow(ctx, challengeSelect+` WHERE challenge.id = $1`, challengeID))
	if err != nil {
		return PvPChallenge{}, fmt.Errorf("read settled pvp challenge: %w", err)
	}
	return settled, tx.Commit(ctx)
}

// closeChallenge refunds an open challenge's creator stake. Status is either
// cancelled (by the creator) or expired (by the runner).
func (repository *PostgresRepository) closeChallenge(ctx context.Context, tx pgx.Tx, challenge PvPChallenge, status PvPStatus, now time.Time) error {
	payload, err := payloadDigest(struct {
		ChallengeID string `json:"challenge_id"`
		Status      string `json:"status"`
	}{challenge.ID.String(), string(status)})
	if err != nil {
		return err
	}
	closingID := uuid.NewSHA1(pvpNamespace, []byte(challenge.ID.String()+":close"))
	if _, err := repository.economy.RecordInTransaction(ctx, tx, economy.RecordCommand{
		TransactionID: closingID, TransactionType: economy.TransactionPvPRefund,
		IdempotencyKey:  "pvp-close:" + compactUUID(challenge.ID),
		SourceReference: "pvp-challenge:" + challenge.ID.String(),
		PolicyRevision:  "pvp-v1", PayloadSHA256: payload, OccurredAt: now, RecordedAt: now,
		Postings: []economy.PostingInput{
			{AccountID: economy.PvPChallengeEscrowID(), Amount: -challenge.Stake},
			{AccountID: challenge.CreatorID, Amount: challenge.Stake},
		},
	}); err != nil {
		return fmt.Errorf("refund pvp challenge: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE economy.pvp_challenges
SET status = $2, closing_transaction_id = $3, closed_at = $4, version = version + 1, updated_at = $4
WHERE id = $1 AND status = 'open'`, challenge.ID, string(status), closingID, now); err != nil {
		return fmt.Errorf("close pvp challenge: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) CancelChallenge(ctx context.Context, challengeID, userID uuid.UUID, now time.Time) (PvPChallenge, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PvPChallenge{}, fmt.Errorf("begin pvp cancel: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	challenge, err := scanChallenge(tx.QueryRow(ctx, challengeSelect+`
WHERE challenge.id = $1 FOR UPDATE OF challenge`, challengeID))
	if errors.Is(err, pgx.ErrNoRows) {
		return PvPChallenge{}, ErrChallengeNotFound
	}
	if err != nil {
		return PvPChallenge{}, fmt.Errorf("lock pvp challenge: %w", err)
	}
	if challenge.CreatorID != userID {
		return PvPChallenge{}, ErrChallengeForbidden
	}
	if challenge.Status == PvPCancelled {
		return challenge, tx.Commit(ctx)
	}
	if challenge.Status != PvPOpen {
		return PvPChallenge{}, ErrChallengeClosed
	}
	if err := repository.closeChallenge(ctx, tx, challenge, PvPCancelled, now); err != nil {
		return PvPChallenge{}, err
	}
	closed, err := scanChallenge(tx.QueryRow(ctx, challengeSelect+` WHERE challenge.id = $1`, challengeID))
	if err != nil {
		return PvPChallenge{}, fmt.Errorf("read cancelled pvp challenge: %w", err)
	}
	return closed, tx.Commit(ctx)
}

// ExpireChallenges refunds up to limit open challenges whose deadline passed.
func (repository *PostgresRepository) ExpireChallenges(ctx context.Context, now time.Time, limit int) (int, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin pvp expiry: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	challenges, err := listChallenges(ctx, tx, `
WHERE challenge.status = 'open' AND challenge.expires_at <= $1
ORDER BY challenge.expires_at LIMIT $2 FOR UPDATE OF challenge SKIP LOCKED`, now, limit)
	if err != nil {
		return 0, err
	}
	for _, challenge := range challenges {
		if err := repository.closeChallenge(ctx, tx, challenge, PvPExpired, now); err != nil {
			return 0, err
		}
	}
	return len(challenges), tx.Commit(ctx)
}

// ---------------------------------------------------------------- overview

func (repository *PostgresRepository) GamesOverview(ctx context.Context, userID uuid.UUID, now time.Time) (GamesOverview, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return GamesOverview{}, fmt.Errorf("begin magic games overview: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	settings, version, _, err := readSettings(ctx, tx, false)
	if err != nil {
		return GamesOverview{}, err
	}
	result := GamesOverview{Settings: settings, SettingsVersion: version}
	if result.MagicBalance, err = memberBalance(ctx, tx, userID); err != nil {
		return GamesOverview{}, err
	}
	if result.ScratchUsedToday, err = scratchUsedSince(ctx, tx, userID, dayStart(now, settings.location())); err != nil {
		return GamesOverview{}, err
	}
	rows, err := tx.Query(ctx, `SELECT `+scratchColumns+`
FROM economy.scratch_cards WHERE user_id = $1 ORDER BY purchased_at DESC, id DESC LIMIT 10`, userID)
	if err != nil {
		return GamesOverview{}, fmt.Errorf("list scratch cards: %w", err)
	}
	result.RecentScratchCards = make([]ScratchCard, 0, 10)
	for rows.Next() {
		card, err := scanScratch(rows)
		if err != nil {
			rows.Close()
			return GamesOverview{}, fmt.Errorf("scan scratch card: %w", err)
		}
		result.RecentScratchCards = append(result.RecentScratchCards, card)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return GamesOverview{}, fmt.Errorf("finish scratch cards: %w", err)
	}

	var current *LotteryRound
	open, err := scanRound(tx.QueryRow(ctx, `SELECT `+roundColumns+`
FROM economy.lottery_rounds WHERE status = 'open'`))
	switch {
	case err == nil:
		current = &open
	case errors.Is(err, pgx.ErrNoRows):
		// The first visitor after a deploy or re-enable opens the round so the
		// page never waits a worker tick to show a pool.
		if err := lockLottery(ctx, tx); err != nil {
			return GamesOverview{}, err
		}
		if current, err = ensureOpenRound(ctx, tx, settings, now); err != nil {
			return GamesOverview{}, err
		}
	default:
		return GamesOverview{}, fmt.Errorf("read open lottery round: %w", err)
	}
	if current != nil {
		if current.MyTickets, current.MyTicketRanges, err = memberTickets(ctx, tx, current.ID, userID); err != nil {
			return GamesOverview{}, err
		}
		current.Seed = nil
		result.CurrentRound = current
	}
	previous, err := scanRound(tx.QueryRow(ctx, `SELECT `+roundColumns+`
FROM economy.lottery_rounds WHERE status = 'drawn' ORDER BY round_number DESC LIMIT 1`))
	if err == nil {
		if previous.Winners, err = readWinners(ctx, tx, previous.ID); err != nil {
			return GamesOverview{}, err
		}
		if previous.MyTickets, previous.MyTicketRanges, err = memberTickets(ctx, tx, previous.ID, userID); err != nil {
			return GamesOverview{}, err
		}
		for _, winner := range previous.Winners {
			if winner.UserID == userID {
				previous.MyPrize += winner.Prize
			}
		}
		result.PreviousRound = &previous
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return GamesOverview{}, fmt.Errorf("read previous lottery round: %w", err)
	}

	if result.OpenChallenges, err = listChallenges(ctx, tx, `
WHERE challenge.status = 'open' AND challenge.expires_at > $1
ORDER BY challenge.created_at DESC LIMIT 50`, now); err != nil {
		return GamesOverview{}, err
	}
	if result.MyChallenges, err = listChallenges(ctx, tx, `
WHERE (challenge.creator_id = $1 OR challenge.opponent_id = $1)
ORDER BY challenge.created_at DESC LIMIT 20`, userID); err != nil {
		return GamesOverview{}, err
	}
	if result.RecentSettled, err = listChallenges(ctx, tx, `
WHERE challenge.status = 'settled'
ORDER BY challenge.closed_at DESC LIMIT 10`); err != nil {
		return GamesOverview{}, err
	}
	return result, tx.Commit(ctx)
}

// ---------------------------------------------------------------- admin

func (repository *PostgresRepository) AdminOverview(ctx context.Context) (AdminOverview, error) {
	settings, version, updatedAt, err := readSettings(ctx, repository.pool, false)
	if err != nil {
		return AdminOverview{}, err
	}
	result := AdminOverview{Settings: settings, Version: version, UpdatedAt: updatedAt}
	if err := repository.pool.QueryRow(ctx, `
SELECT
    COALESCE(max(balance) FILTER (WHERE id = $1), 0)::bigint,
    COALESCE(max(balance) FILTER (WHERE id = $2), 0)::bigint,
    COALESCE(max(balance) FILTER (WHERE id = $3), 0)::bigint,
    COALESCE(max(balance) FILTER (WHERE id = $4), 0)::bigint
FROM economy.magic_accounts WHERE id = ANY(ARRAY[$1, $2, $3, $4]::uuid[])`,
		economy.MagicShopSinkID(), economy.MagicGameHouseID(),
		economy.LotteryPoolEscrowID(), economy.PvPChallengeEscrowID()).Scan(
		&result.Accounts.ShopSink, &result.Accounts.GameHouse,
		&result.Accounts.LotteryPool, &result.Accounts.PvPEscrow); err != nil {
		return AdminOverview{}, fmt.Errorf("read magic store accounts: %w", err)
	}
	rows, err := repository.pool.Query(ctx, `
SELECT revision.version, revision.reason, account.username, revision.occurred_at
FROM economy.magic_store_settings_revisions AS revision
LEFT JOIN identity.users AS account ON account.id = revision.actor_id
ORDER BY revision.version DESC LIMIT 10`)
	if err != nil {
		return AdminOverview{}, fmt.Errorf("list magic store revisions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var revision SettingsRevision
		var actor pgtype.Text
		if err := rows.Scan(&revision.Version, &revision.Reason, &actor, &revision.OccurredAt); err != nil {
			return AdminOverview{}, fmt.Errorf("scan magic store revision: %w", err)
		}
		if actor.Valid {
			revision.ActorUsername = &actor.String
		}
		revision.OccurredAt = revision.OccurredAt.UTC()
		result.Revisions = append(result.Revisions, revision)
	}
	return result, rows.Err()
}

func (repository *PostgresRepository) UpdateSettings(ctx context.Context, command UpdateSettingsCommand) (AdminOverview, error) {
	document, err := json.Marshal(command.Settings)
	if err != nil {
		return AdminOverview{}, fmt.Errorf("encode magic store settings: %w", err)
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return AdminOverview{}, fmt.Errorf("begin magic store settings update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, version, _, err := readSettings(ctx, tx, true)
	if err != nil {
		return AdminOverview{}, err
	}
	if version != command.ExpectedVersion {
		return AdminOverview{}, ErrSettingsConflict
	}
	if _, err := tx.Exec(ctx, `
UPDATE economy.magic_store_settings
SET document = $1, version = version + 1, updated_at = $2
WHERE singleton`, document, command.OccurredAt); err != nil {
		return AdminOverview{}, fmt.Errorf("update magic store settings: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO economy.magic_store_settings_revisions (
    version, document, reason, actor_id, authorization_decision_id, occurred_at
) VALUES ($1, $2, $3, $4, $5, $6)`,
		version+1, document, command.Reason, command.ActorID, command.DecisionID, command.OccurredAt); err != nil {
		return AdminOverview{}, fmt.Errorf("insert magic store settings revision: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminOverview{}, fmt.Errorf("commit magic store settings update: %w", err)
	}
	return repository.AdminOverview(ctx)
}
