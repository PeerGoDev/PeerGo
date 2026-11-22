package magicstore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/economy"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

var (
	shopOrderNamespace       = uuid.MustParse("6b0f8f51-3c1e-4a8a-9a52-6f1d2a4b9c01")
	shopTransactionNamespace = uuid.MustParse("6b0f8f51-3c1e-4a8a-9a52-6f1d2a4b9c02")
	itemEventNamespace       = uuid.MustParse("6b0f8f51-3c1e-4a8a-9a52-6f1d2a4b9c03")
)

type querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type PostgresRepository struct {
	pool    *pgxpool.Pool
	economy *economy.PostgresRepository
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, errors.New("magic store database is required")
	}
	ledger, err := economy.NewPostgresRepository(pool)
	if err != nil {
		return nil, err
	}
	return &PostgresRepository{pool: pool, economy: ledger}, nil
}

func readSettings(ctx context.Context, db querier, forUpdate bool) (Settings, int64, time.Time, error) {
	query := `SELECT document, version, updated_at FROM economy.magic_store_settings WHERE singleton`
	if forUpdate {
		query += " FOR UPDATE"
	}
	var document []byte
	var version int64
	var updatedAt time.Time
	if err := db.QueryRow(ctx, query).Scan(&document, &version, &updatedAt); err != nil {
		return Settings{}, 0, time.Time{}, fmt.Errorf("read magic store settings: %w", err)
	}
	settings, err := DecodeSettings(document)
	if err != nil {
		return Settings{}, 0, time.Time{}, fmt.Errorf("decode magic store settings v%d: %w", version, err)
	}
	return settings, version, updatedAt.UTC(), nil
}

func memberBalance(ctx context.Context, db querier, userID uuid.UUID) (int64, error) {
	var balance int64
	if err := db.QueryRow(ctx, `
SELECT COALESCE((SELECT balance FROM economy.magic_accounts WHERE user_id = $1), 0)::bigint`, userID).Scan(&balance); err != nil {
		return 0, fmt.Errorf("read magic balance: %w", err)
	}
	return balance, nil
}

func lockMember(ctx context.Context, tx pgx.Tx, scope string, userID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, scope+":"+userID.String()); err != nil {
		return fmt.Errorf("lock member %s: %w", scope, err)
	}
	return nil
}

func payloadDigest(value any) ([32]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return [32]byte{}, fmt.Errorf("marshal ledger payload: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

func compactUUID(value uuid.UUID) string { return strings.ReplaceAll(value.String(), "-", "") }

func balanceOf(transaction economy.Transaction, userID uuid.UUID) int64 {
	for _, posting := range transaction.Postings {
		if posting.AccountID == userID {
			return posting.BalanceAfter
		}
	}
	return 0
}

func classifyLedgerError(err error) error {
	switch {
	case errors.Is(err, economy.ErrInsufficientBalance):
		return ErrInsufficientMagic
	case errors.Is(err, economy.ErrIdempotencyConflict):
		return ErrIdempotencyConflict
	default:
		return err
	}
}

func monthStart(now time.Time, location *time.Location) time.Time {
	local := now.In(location)
	return time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location).UTC()
}

func dayStart(now time.Time, location *time.Location) time.Time {
	local := now.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location).UTC()
}

func monthlyPurchased(ctx context.Context, db querier, userID uuid.UUID, item ItemKey, since time.Time) (int, error) {
	var count int64
	if err := db.QueryRow(ctx, `
SELECT COALESCE(sum(quantity), 0)::bigint
FROM economy.magic_shop_orders
WHERE user_id = $1 AND item_key = $2 AND purchased_at >= $3`, userID, string(item), since).Scan(&count); err != nil {
		return 0, fmt.Errorf("count monthly shop purchases: %w", err)
	}
	return int(count), nil
}

func itemQuantity(ctx context.Context, db querier, userID uuid.UUID, item ItemKey) (int, error) {
	var quantity int
	err := db.QueryRow(ctx, `
SELECT quantity FROM economy.member_item_balances WHERE user_id = $1 AND item_key = $2`,
		userID, string(item)).Scan(&quantity)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read member item balance: %w", err)
	}
	return quantity, nil
}

func readNameStyle(ctx context.Context, db querier, userID uuid.UUID, now time.Time) (*NameStyle, error) {
	var style NameStyle
	var tier string
	err := db.QueryRow(ctx, `
SELECT style.user_id, account.username, style.tier, style.style_key, style.expires_at
FROM economy.member_name_styles AS style
JOIN identity.users AS account ON account.id = style.user_id
WHERE style.user_id = $1 AND style.expires_at > $2`, userID, now).Scan(
		&style.UserID, &style.Username, &tier, &style.StyleKey, &style.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read member name style: %w", err)
	}
	style.Tier = NameStyleTier(tier)
	style.ExpiresAt = style.ExpiresAt.UTC()
	return &style, nil
}

const orderColumns = `id, item_key, quantity, unit_price, total_price, style_key, effect_until, purchased_at`

func scanOrder(row pgx.Row) (Order, error) {
	var order Order
	var item string
	var style pgtype.Text
	var until pgtype.Timestamptz
	if err := row.Scan(&order.ID, &item, &order.Quantity, &order.UnitPrice, &order.TotalPrice,
		&style, &until, &order.PurchasedAt); err != nil {
		return Order{}, err
	}
	order.ItemKey = ItemKey(item)
	if style.Valid {
		order.StyleKey = &style.String
	}
	if until.Valid {
		value := until.Time.UTC()
		order.EffectUntil = &value
	}
	order.PurchasedAt = order.PurchasedAt.UTC()
	return order, nil
}

func (repository *PostgresRepository) ShopOverview(ctx context.Context, userID uuid.UUID, now time.Time) (ShopOverview, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ShopOverview{}, fmt.Errorf("begin magic shop overview: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	settings, version, _, err := readSettings(ctx, tx, false)
	if err != nil {
		return ShopOverview{}, err
	}
	result := ShopOverview{Settings: settings, SettingsVersion: version}
	if result.MagicBalance, err = memberBalance(ctx, tx, userID); err != nil {
		return ShopOverview{}, err
	}
	since := monthStart(now, settings.location())
	if result.InvitesPurchasedThisMonth, err = monthlyPurchased(ctx, tx, userID, ItemInvite, since); err != nil {
		return ShopOverview{}, err
	}
	if result.TemporaryPurchasedMonth, err = monthlyPurchased(ctx, tx, userID, ItemTemporaryInvite, since); err != nil {
		return ShopOverview{}, err
	}
	if err := tx.QueryRow(ctx, `
SELECT COALESCE((SELECT remaining_invites FROM identity.invitation_accounts WHERE user_id = $1), 0)`,
		userID).Scan(&result.RemainingInvites); err != nil {
		return ShopOverview{}, fmt.Errorf("read remaining invitations: %w", err)
	}
	if result.TemporaryInvites, result.TemporaryInviteNextExpiry, err = identity.TemporaryInvitationSummary(ctx, tx, userID, now); err != nil {
		return ShopOverview{}, err
	}
	blocker, err := identity.InvitationPurchaseBlocker(ctx, tx, userID, now)
	if err != nil {
		return ShopOverview{}, err
	}
	if blocker != identity.InvitationBlockerNone {
		result.InviteBlocker = string(blocker)
	}
	vip, err := identity.CurrentVIPState(ctx, tx, userID, now)
	if err != nil {
		return ShopOverview{}, err
	}
	result.VIP = VIPStatus{Active: vip.Active, Permanent: vip.Active && vip.Until == nil}
	if vip.Active {
		result.VIP.Until = vip.Until
	}
	if result.NameStyle, err = readNameStyle(ctx, tx, userID, now); err != nil {
		return ShopOverview{}, err
	}
	if result.MakeupCards, err = itemQuantity(ctx, tx, userID, ItemMakeupCard); err != nil {
		return ShopOverview{}, err
	}
	rows, err := tx.Query(ctx, `SELECT `+orderColumns+`
FROM economy.magic_shop_orders WHERE user_id = $1
ORDER BY purchased_at DESC, id DESC LIMIT 20`, userID)
	if err != nil {
		return ShopOverview{}, fmt.Errorf("list magic shop orders: %w", err)
	}
	defer rows.Close()
	result.RecentOrders = make([]Order, 0, 20)
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			return ShopOverview{}, fmt.Errorf("scan magic shop order: %w", err)
		}
		result.RecentOrders = append(result.RecentOrders, order)
	}
	if err := rows.Err(); err != nil {
		return ShopOverview{}, fmt.Errorf("finish magic shop orders: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) Purchase(ctx context.Context, command PurchaseCommand) (PurchaseReceipt, error) {
	if command.RequestID == uuid.Nil || command.UserID == uuid.Nil || !validItem(command.ItemKey) ||
		command.Quantity < 1 || command.Quantity > 100 || command.Now.IsZero() ||
		(command.ExpectedUnitPrice != nil && *command.ExpectedUnitPrice < 1) {
		return PurchaseReceipt{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PurchaseReceipt{}, fmt.Errorf("begin magic shop purchase: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockMember(ctx, tx, "magic-shop", command.UserID); err != nil {
		return PurchaseReceipt{}, err
	}
	replay, err := scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+`
FROM economy.magic_shop_orders WHERE user_id = $1 AND request_id = $2`, command.UserID, command.RequestID))
	if err == nil {
		if replay.ItemKey != command.ItemKey || replay.Quantity != command.Quantity ||
			(replay.StyleKey != nil && *replay.StyleKey != command.StyleKey) ||
			(command.ExpectedUnitPrice != nil && replay.UnitPrice != *command.ExpectedUnitPrice) {
			return PurchaseReceipt{}, ErrIdempotencyConflict
		}
		balance, err := memberBalance(ctx, tx, command.UserID)
		if err != nil {
			return PurchaseReceipt{}, err
		}
		return PurchaseReceipt{Order: replay, BalanceAfter: balance, Replayed: true}, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return PurchaseReceipt{}, fmt.Errorf("read magic shop replay: %w", err)
	}

	settings, version, _, err := readSettings(ctx, tx, false)
	if err != nil {
		return PurchaseReceipt{}, err
	}
	if !settings.ShopEnabled {
		return PurchaseReceipt{}, ErrShopDisabled
	}
	location := settings.location()
	orderID := uuid.NewSHA1(shopOrderNamespace, []byte(command.UserID.String()+":"+command.RequestID.String()))
	order := Order{ID: orderID, ItemKey: command.ItemKey, Quantity: command.Quantity, PurchasedAt: command.Now}

	switch command.ItemKey {
	case ItemInvite, ItemTemporaryInvite:
		enabled, price, limit := settings.Invite.Enabled, settings.Invite.Price, settings.Invite.MonthlyLimit
		if command.ItemKey == ItemTemporaryInvite {
			enabled, price, limit = settings.TemporaryInvite.Enabled, settings.TemporaryInvite.Price, settings.TemporaryInvite.MonthlyLimit
		}
		if !enabled {
			return PurchaseReceipt{}, ErrShopDisabled
		}
		if limit > 0 {
			purchased, err := monthlyPurchased(ctx, tx, command.UserID, command.ItemKey, monthStart(command.Now, location))
			if err != nil {
				return PurchaseReceipt{}, err
			}
			if purchased+command.Quantity > limit {
				return PurchaseReceipt{}, ErrMonthlyLimit
			}
		}
		blocker, err := identity.InvitationPurchaseBlocker(ctx, tx, command.UserID, command.Now)
		if err != nil {
			return PurchaseReceipt{}, err
		}
		if blocker != identity.InvitationBlockerNone {
			return PurchaseReceipt{}, fmt.Errorf("%w: %s", ErrInviteIneligible, blocker)
		}
		order.UnitPrice = price
		if command.ItemKey == ItemTemporaryInvite {
			until := command.Now.AddDate(0, 0, settings.TemporaryInvite.ValidDays)
			order.EffectUntil = &until
		}
	case ItemVIP:
		if !settings.VIP.Enabled {
			return PurchaseReceipt{}, ErrShopDisabled
		}
		if command.Quantity > 12 || settings.VIP.DurationDays*command.Quantity > identity.MaximumVIPShopDurationDays {
			return PurchaseReceipt{}, ErrInput
		}
		order.UnitPrice = settings.VIP.Price
	case ItemNameColorSolid, ItemNameColorGradient:
		if !settings.NameColor.Enabled {
			return PurchaseReceipt{}, ErrShopDisabled
		}
		tier := NameStyleSolid
		order.UnitPrice = settings.NameColor.SolidPrice
		if command.ItemKey == ItemNameColorGradient {
			tier = NameStyleGradient
			order.UnitPrice = settings.NameColor.GradientPrice
		}
		if command.Quantity > 12 || !NameStyleAllowed(tier, command.StyleKey) {
			return PurchaseReceipt{}, ErrNameStyleTier
		}
		style := command.StyleKey
		order.StyleKey = &style
	case ItemMakeupCard:
		if !settings.MakeupCard.Enabled {
			return PurchaseReceipt{}, ErrShopDisabled
		}
		held, err := itemQuantity(ctx, tx, command.UserID, ItemMakeupCard)
		if err != nil {
			return PurchaseReceipt{}, err
		}
		if held+command.Quantity > settings.MakeupCard.MaximumHolding {
			return PurchaseReceipt{}, ErrHoldingLimit
		}
		order.UnitPrice = settings.MakeupCard.Price
	}
	if command.ExpectedUnitPrice != nil && order.UnitPrice != *command.ExpectedUnitPrice {
		return PurchaseReceipt{}, ErrShopPriceChanged
	}
	order.TotalPrice = order.UnitPrice * int64(command.Quantity)

	payload, err := payloadDigest(struct {
		OrderID  string `json:"order_id"`
		UserID   string `json:"user_id"`
		Item     string `json:"item"`
		Quantity int    `json:"quantity"`
		Price    int64  `json:"unit_price"`
		Style    string `json:"style"`
		Version  int64  `json:"settings_version"`
	}{orderID.String(), command.UserID.String(), string(command.ItemKey), command.Quantity, order.UnitPrice, command.StyleKey, version})
	if err != nil {
		return PurchaseReceipt{}, err
	}
	transactionID := uuid.NewSHA1(shopTransactionNamespace, orderID[:])
	transaction, err := repository.economy.RecordInTransaction(ctx, tx, economy.RecordCommand{
		TransactionID: transactionID, TransactionType: economy.TransactionMagicShopPurchase,
		IdempotencyKey:  "magic-shop:" + compactUUID(orderID),
		SourceReference: "magic-shop-order:" + orderID.String(),
		PolicyRevision:  fmt.Sprintf("magic-store-v%d", version),
		PayloadSHA256:   payload, OccurredAt: command.Now, RecordedAt: command.Now,
		Postings: []economy.PostingInput{
			{AccountID: command.UserID, Amount: -order.TotalPrice},
			{AccountID: economy.MagicShopSinkID(), Amount: order.TotalPrice},
		},
	})
	if err != nil {
		return PurchaseReceipt{}, classifyLedgerError(err)
	}

	switch command.ItemKey {
	case ItemInvite:
		if _, err := identity.GrantPurchasedInvitations(ctx, tx, command.UserID, command.Quantity, command.Now); err != nil {
			return PurchaseReceipt{}, err
		}
	case ItemVIP:
		until, err := identity.ExtendVIPFromShop(ctx, tx, command.UserID, orderID, settings.VIP.DurationDays*command.Quantity, command.Now)
		switch {
		case errors.Is(err, identity.ErrVIPPermanent):
			return PurchaseReceipt{}, ErrVIPPermanent
		case errors.Is(err, identity.ErrManagedUserNotActive), errors.Is(err, identity.ErrManagedUserNotFound):
			return PurchaseReceipt{}, ErrAccountUnavailable
		case err != nil:
			return PurchaseReceipt{}, err
		}
		order.EffectUntil = &until
	case ItemNameColorSolid, ItemNameColorGradient:
		until, err := extendNameStyle(ctx, tx, command, settings.NameColor.DurationDays*command.Quantity)
		if err != nil {
			return PurchaseReceipt{}, err
		}
		order.EffectUntil = &until
	case ItemMakeupCard:
		if _, err := adjustItem(ctx, tx, command.UserID, ItemMakeupCard, command.Quantity,
			"magic-shop-order:"+orderID.String(), command.Now); err != nil {
			return PurchaseReceipt{}, err
		}
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO economy.magic_shop_orders (
    id, request_id, user_id, item_key, quantity, unit_price, total_price,
    style_key, effect_until, settings_version, magic_transaction_id, purchased_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		orderID, command.RequestID, command.UserID, string(command.ItemKey), command.Quantity,
		order.UnitPrice, order.TotalPrice, order.StyleKey, order.EffectUntil, version,
		transactionID, command.Now); err != nil {
		return PurchaseReceipt{}, fmt.Errorf("insert magic shop order: %w", err)
	}
	if command.ItemKey == ItemTemporaryInvite {
		if err := identity.GrantTemporaryInvitationCredits(ctx, tx, command.UserID, orderID,
			command.Quantity, *order.EffectUntil, command.Now); err != nil {
			return PurchaseReceipt{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return PurchaseReceipt{}, fmt.Errorf("commit magic shop purchase: %w", err)
	}
	return PurchaseReceipt{Order: order, BalanceAfter: balanceOf(transaction, command.UserID)}, nil
}

// extendNameStyle adds days on top of an active style of the same tier. A tier
// change restarts the clock so cheap solid days cannot be upgraded later.
func extendNameStyle(ctx context.Context, tx pgx.Tx, command PurchaseCommand, days int) (time.Time, error) {
	tier := NameStyleSolid
	if command.ItemKey == ItemNameColorGradient {
		tier = NameStyleGradient
	}
	var currentTier string
	var expiresAt time.Time
	err := tx.QueryRow(ctx, `
SELECT tier, expires_at FROM economy.member_name_styles WHERE user_id = $1 FOR UPDATE`,
		command.UserID).Scan(&currentTier, &expiresAt)
	base := command.Now
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return time.Time{}, fmt.Errorf("lock member name style: %w", err)
	case expiresAt.After(command.Now) && NameStyleTier(currentTier) == tier:
		base = expiresAt
	case expiresAt.After(command.Now) && NameStyleTier(currentTier) == NameStyleGradient && tier == NameStyleSolid:
		return time.Time{}, ErrNameStyleTier
	}
	until := base.AddDate(0, 0, days).UTC()
	if _, err := tx.Exec(ctx, `
INSERT INTO economy.member_name_styles (user_id, tier, style_key, expires_at, version, updated_at)
VALUES ($1, $2, $3, $4, 1, $5)
ON CONFLICT (user_id) DO UPDATE
SET tier = EXCLUDED.tier, style_key = EXCLUDED.style_key, expires_at = EXCLUDED.expires_at,
    version = economy.member_name_styles.version + 1, updated_at = EXCLUDED.updated_at`,
		command.UserID, string(tier), command.StyleKey, until, command.Now); err != nil {
		return time.Time{}, fmt.Errorf("write member name style: %w", err)
	}
	return until, nil
}

// adjustItem changes a consumable balance and appends the matching event.
func adjustItem(ctx context.Context, tx pgx.Tx, userID uuid.UUID, item ItemKey, delta int, source string, now time.Time) (int, error) {
	var balance int
	err := tx.QueryRow(ctx, `
INSERT INTO economy.member_item_balances (user_id, item_key, quantity, version, updated_at)
VALUES ($1, $2, $3, 1, $4)
ON CONFLICT (user_id, item_key) DO UPDATE
SET quantity = economy.member_item_balances.quantity + EXCLUDED.quantity,
    version = economy.member_item_balances.version + 1,
    updated_at = EXCLUDED.updated_at
RETURNING quantity`, userID, string(item), delta, now).Scan(&balance)
	if err != nil {
		return 0, fmt.Errorf("adjust member item balance: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO economy.member_item_events (id, user_id, item_key, delta, balance_after, source_reference, occurred_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.NewSHA1(itemEventNamespace, []byte(source)), userID, string(item), delta, balance, source, now); err != nil {
		return 0, fmt.Errorf("record member item event: %w", err)
	}
	return balance, nil
}

func (repository *PostgresRepository) SetNameStyle(ctx context.Context, userID uuid.UUID, style string, now time.Time) (NameStyle, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return NameStyle{}, fmt.Errorf("begin name style change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockMember(ctx, tx, "magic-shop", userID); err != nil {
		return NameStyle{}, err
	}
	current, err := readNameStyle(ctx, tx, userID, now)
	if err != nil {
		return NameStyle{}, err
	}
	if current == nil {
		return NameStyle{}, ErrNameStyleMissing
	}
	if !NameStyleAllowed(current.Tier, style) {
		return NameStyle{}, ErrNameStyleTier
	}
	if _, err := tx.Exec(ctx, `
UPDATE economy.member_name_styles
SET style_key = $2, version = version + 1, updated_at = $3
WHERE user_id = $1`, userID, style, now); err != nil {
		return NameStyle{}, fmt.Errorf("update member name style: %w", err)
	}
	current.StyleKey = style
	return *current, tx.Commit(ctx)
}

func (repository *PostgresRepository) ActiveNameStyles(ctx context.Context, now time.Time) ([]NameStyle, error) {
	rows, err := repository.pool.Query(ctx, `
SELECT style.user_id, account.username, style.tier, style.style_key, style.expires_at
FROM economy.member_name_styles AS style
JOIN identity.users AS account ON account.id = style.user_id
WHERE style.expires_at > $1 AND account.status = 'active'
ORDER BY account.username`, now)
	if err != nil {
		return nil, fmt.Errorf("list active name styles: %w", err)
	}
	defer rows.Close()
	result := make([]NameStyle, 0, 64)
	for rows.Next() {
		var style NameStyle
		var tier string
		if err := rows.Scan(&style.UserID, &style.Username, &tier, &style.StyleKey, &style.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan active name style: %w", err)
		}
		style.Tier = NameStyleTier(tier)
		style.ExpiresAt = style.ExpiresAt.UTC()
		result = append(result, style)
	}
	return result, rows.Err()
}
