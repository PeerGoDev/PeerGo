package promotions

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/peergo/peergo/contracts/go/promotioncontrolv1"
	"github.com/peergo/peergo/services/core/internal/modules/economy"
)

var (
	productCampaignNamespace    = uuid.MustParse("a72f8ec1-bc50-53f5-94d5-8ee040804320")
	productTransactionNamespace = uuid.MustParse("f89e948d-898f-5df1-9a28-f14021db8e63")
)

func (repository *PostgresRepository) ProductOffer(ctx context.Context, buyerID uuid.UUID, torrentID int64, now time.Time) (ProductOffer, error) {
	if buyerID == uuid.Nil || torrentID < 1 || now.IsZero() {
		return ProductOffer{}, ErrInput
	}
	now = canonicalProductTime(now)
	var offer ProductOffer
	err := repository.pool.QueryRow(ctx, `
SELECT torrent.id, torrent.title,
       COALESCE(account.balance, 0)::bigint
FROM torrents.torrents AS torrent
LEFT JOIN economy.magic_accounts AS account ON account.user_id = $1
WHERE torrent.id = $2 AND torrent.state = 'published'`, buyerID, torrentID).Scan(
		&offer.TorrentID, &offer.TorrentTitle, &offer.MagicBalance,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProductOffer{}, ErrNotFound
	}
	if err != nil {
		return ProductOffer{}, fmt.Errorf("read promotion product target: %w", err)
	}
	policy, err := repository.CurrentProductPolicy(ctx, now)
	if err != nil {
		return ProductOffer{}, err
	}
	offer.Policy = policy

	var promotion string
	var promotionStarts, promotionEnds pgtype.Timestamptz
	err = repository.pool.QueryRow(ctx, `
SELECT campaign.promotion, campaign.starts_at, campaign.ends_at
FROM promotion.campaigns AS campaign
WHERE campaign.source_kind = 'member_purchase'
  AND campaign.torrent_id = $1
  AND campaign.ends_at > $2
ORDER BY campaign.starts_at, campaign.id
LIMIT 1`, torrentID, now).Scan(&promotion, &promotionStarts, &promotionEnds)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ProductOffer{}, fmt.Errorf("read paid promotion window: %w", err)
	}
	if err == nil {
		value := Promotion(promotion)
		if !validPromotion(value) || !promotionStarts.Valid || !promotionEnds.Valid {
			return ProductOffer{}, ErrInvariant
		}
		offer.ActivePromotion = &value
		offer.PromotionWindow = &ProductWindow{StartsAt: promotionStarts.Time.UTC(), EndsAt: promotionEnds.Time.UTC()}
	}

	var stickyStarts, stickyEnds pgtype.Timestamptz
	err = repository.pool.QueryRow(ctx, `
SELECT sticky_starts_at, sticky_ends_at
FROM promotion.product_orders
WHERE torrent_id = $1 AND sticky_ends_at > $2
ORDER BY sticky_starts_at, id
LIMIT 1`, torrentID, now).Scan(&stickyStarts, &stickyEnds)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ProductOffer{}, fmt.Errorf("read paid sticky window: %w", err)
	}
	if err == nil {
		if !stickyStarts.Valid || !stickyEnds.Valid {
			return ProductOffer{}, ErrInvariant
		}
		offer.StickyWindow = &ProductWindow{StartsAt: stickyStarts.Time.UTC(), EndsAt: stickyEnds.Time.UTC()}
	}
	return offer, nil
}

func (repository *PostgresRepository) PurchaseProduct(ctx context.Context, command ProductPurchaseCommand) (ProductOrder, error) {
	command.Now = canonicalProductTime(command.Now)
	if command.OrderID == uuid.Nil || command.BuyerID == uuid.Nil || command.TorrentID < 1 ||
		command.AuthorizationID == uuid.Nil || command.Authorization.ID != command.AuthorizationID ||
		command.Now.IsZero() || !validProductSelection(command.Selection) {
		return ProductOrder{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ProductOrder{}, fmt.Errorf("begin promotion product purchase: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "promotion-product:"+strconv.FormatInt(command.TorrentID, 10)); err != nil {
		return ProductOrder{}, fmt.Errorf("lock promotion product purchase: %w", err)
	}
	if command.Selection.Promotion != nil {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('peergo-core-promotion-scheduling-v1', 0))`); err != nil {
			return ProductOrder{}, fmt.Errorf("lock promotion scheduling: %w", err)
		}
	}
	if replay, found, err := readProductOrderByID(ctx, tx, command.OrderID); found || err != nil {
		if err != nil {
			return ProductOrder{}, err
		}
		if !productReplayMatches(replay, command) {
			return ProductOrder{}, ErrIdempotencyConflict
		}
		replay.Replayed = true
		return replay, nil
	}

	var torrentTitle, torrentState string
	if err := tx.QueryRow(ctx, `SELECT title, state FROM torrents.torrents WHERE id = $1 FOR UPDATE`, command.TorrentID).Scan(&torrentTitle, &torrentState); errors.Is(err, pgx.ErrNoRows) {
		return ProductOrder{}, ErrNotFound
	} else if err != nil {
		return ProductOrder{}, fmt.Errorf("lock promotion product target: %w", err)
	}
	if torrentState != "published" {
		return ProductOrder{}, ErrTorrentUnavailable
	}
	var buyerNumericID int64
	var buyerUsername string
	if err := tx.QueryRow(ctx, `SELECT numeric_id, username FROM identity.users WHERE id = $1`, command.BuyerID).Scan(&buyerNumericID, &buyerUsername); errors.Is(err, pgx.ErrNoRows) {
		return ProductOrder{}, ErrNotFound
	} else if err != nil {
		return ProductOrder{}, fmt.Errorf("read promotion product buyer: %w", err)
	}
	policy, err := currentProductPolicy(ctx, tx, command.Now)
	if err != nil {
		return ProductOrder{}, err
	}
	if (command.Selection.Promotion != nil && !policy.PromotionEnabled) || (command.Selection.StickyDays > 0 && !policy.StickyEnabled) {
		return ProductOrder{}, ErrProductDisabled
	}
	if command.Selection.PromotionDays > policy.MaxPromotionDays || command.Selection.StickyDays > policy.MaxStickyDays {
		return ProductOrder{}, ErrInput
	}

	order := ProductOrder{
		ID: command.OrderID, BuyerID: command.BuyerID, BuyerNumericID: buyerNumericID, BuyerUsername: buyerUsername,
		TorrentID: command.TorrentID, TorrentTitle: torrentTitle,
		PromotionDays: command.Selection.PromotionDays, StickyDays: command.Selection.StickyDays,
		PolicyRevision: policy.Revision, PurchasedAt: command.Now,
	}
	var campaignJSON []byte
	var campaignSHA [32]byte
	if command.Selection.Promotion != nil {
		price, ok := policy.PromotionPrice(*command.Selection.Promotion)
		if !ok {
			return ProductOrder{}, ErrInput
		}
		startsAt, err := nextPromotionWindowStart(ctx, tx, command.TorrentID, command.Now, time.Duration(command.Selection.PromotionDays)*24*time.Hour)
		if err != nil {
			return ProductOrder{}, err
		}
		endsAt := startsAt.Add(time.Duration(command.Selection.PromotionDays) * 24 * time.Hour)
		campaignID := uuid.NewSHA1(productCampaignNamespace, []byte(command.OrderID.String()))
		promotion := *command.Selection.Promotion
		order.CampaignID = &campaignID
		order.Promotion = &promotion
		order.PromotionUnitPrice = price
		order.PromotionWindow = &ProductWindow{StartsAt: startsAt, EndsAt: endsAt}

		control := promotioncontrolv1.Command{
			SchemaVersion: promotioncontrolv1.SchemaVersion, CampaignID: campaignID.String(),
			Scope: promotioncontrolv1.ScopeTorrent, TorrentID: &command.TorrentID,
			Promotion: promotioncontrolv1.Promotion(promotion), StartsAt: startsAt, EndsAt: endsAt,
			OverrideLowerScopes: false, ReasonCode: "member_purchase",
		}
		campaignJSON, err = promotioncontrolv1.Encode(control)
		if err != nil {
			return ProductOrder{}, ErrInvariant
		}
		campaignSHA, err = promotioncontrolv1.SHA256(campaignJSON)
		if err != nil {
			return ProductOrder{}, ErrInvariant
		}
	}
	if command.Selection.StickyDays > 0 {
		startsAt := command.Now
		if err := tx.QueryRow(ctx, `
SELECT COALESCE(max(sticky_ends_at), $2)::timestamptz
FROM promotion.product_orders
WHERE torrent_id = $1 AND sticky_ends_at > $2`, command.TorrentID, command.Now).Scan(&startsAt); err != nil {
			return ProductOrder{}, fmt.Errorf("read next sticky window: %w", err)
		}
		startsAt = canonicalProductTime(startsAt)
		order.StickyUnitPrice = policy.StickyPricePerDay
		order.StickyWindow = &ProductWindow{StartsAt: startsAt, EndsAt: startsAt.Add(time.Duration(command.Selection.StickyDays) * 24 * time.Hour)}
	}
	order.TotalPrice = order.PromotionUnitPrice*int64(order.PromotionDays) + order.StickyUnitPrice*int64(order.StickyDays)

	payloadValue := productOrderPayload(command, order)
	payload, err := json.Marshal(payloadValue)
	if err != nil {
		return ProductOrder{}, fmt.Errorf("marshal promotion product purchase: %w", err)
	}
	payloadSHA := sha256.Sum256(payload)
	var transactionID *uuid.UUID
	if order.TotalPrice > 0 {
		value := uuid.NewSHA1(productTransactionNamespace, []byte(command.OrderID.String()))
		ledger, err := repository.economy.RecordInTransaction(ctx, tx, economy.RecordCommand{
			TransactionID: value, TransactionType: economy.TransactionPromotionBuy,
			IdempotencyKey:  "promotion-product:" + strings.ReplaceAll(command.OrderID.String(), "-", ""),
			SourceReference: "torrent:" + strconv.FormatInt(command.TorrentID, 10),
			PolicyRevision:  policy.Revision, PayloadSHA256: payloadSHA,
			OccurredAt: command.Now, RecordedAt: command.Now,
			Postings: []economy.PostingInput{
				{AccountID: command.BuyerID, Amount: -order.TotalPrice},
				{AccountID: economy.PromotionProductSinkID(), Amount: order.TotalPrice},
			},
		})
		if err != nil {
			if errors.Is(err, economy.ErrInsufficientBalance) {
				return ProductOrder{}, ErrInsufficientBalance
			}
			return ProductOrder{}, fmt.Errorf("record promotion product ledger: %w", err)
		}
		balanceAfter, found := productMemberBalanceAfter(ledger, command.BuyerID)
		if !found {
			return ProductOrder{}, ErrInvariant
		}
		order.BalanceAfter = balanceAfter
		transactionID = &value
	} else if err := tx.QueryRow(ctx, `
SELECT COALESCE((SELECT balance FROM economy.magic_accounts WHERE user_id = $1), 0)::bigint`, command.BuyerID).Scan(&order.BalanceAfter); err != nil {
		return ProductOrder{}, fmt.Errorf("read promotion product balance: %w", err)
	}

	if order.CampaignID != nil && order.PromotionWindow != nil && order.Promotion != nil {
		reason := "用户付费促销订单：" + command.OrderID.String()
		if _, err := tx.Exec(ctx, `
INSERT INTO promotion.campaigns (
    id, scope_type, torrent_id, promotion, starts_at, ends_at,
    override_lower_scopes, reason, actor_id, authorization_decision_id,
    command_json, command_sha256, created_at, source_kind
) VALUES ($1, 'torrent', $2, $3, $4, $5, false, $6, $7, $8, $9, $10, $11, 'member_purchase')`,
			*order.CampaignID, command.TorrentID, string(*order.Promotion),
			order.PromotionWindow.StartsAt, order.PromotionWindow.EndsAt, reason,
			command.BuyerID, command.AuthorizationID, string(campaignJSON), campaignSHA[:], command.Now,
		); err != nil {
			return ProductOrder{}, classifyProductDatabaseError("insert paid promotion campaign", err)
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO promotion.delivery_outbox (campaign_id, available_at, created_at)
VALUES ($1, $2, $2)`, *order.CampaignID, command.Now); err != nil {
			return ProductOrder{}, classifyProductDatabaseError("enqueue paid promotion campaign", err)
		}
		campaign := Campaign{
			ID: *order.CampaignID, Source: CampaignSourceMemberPurchase, Scope: ScopeTorrent,
			TorrentID: &command.TorrentID, TorrentTitle: torrentTitle, Promotion: *order.Promotion,
			StartsAt: order.PromotionWindow.StartsAt, EndsAt: order.PromotionWindow.EndsAt,
			Reason: reason, ActorID: command.BuyerID, CreatedAt: command.Now,
			DeliveryState: DeliveryPending, TimelineState: timelineState(order.PromotionWindow.StartsAt, order.PromotionWindow.EndsAt, command.Now),
		}
		event, err := repository.eventBuilder.BuildPromotionCampaignEvent(CampaignAuditInput{Campaign: campaign, Authorization: command.Authorization})
		if err != nil {
			return ProductOrder{}, fmt.Errorf("build paid promotion audit event: %w", err)
		}
		if err := repository.newAuditAppender(tx).Append(ctx, event); err != nil {
			return ProductOrder{}, fmt.Errorf("append paid promotion audit event: %w", err)
		}
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO promotion.product_orders (
    id, buyer_id, torrent_id, campaign_id,
    promotion, promotion_days, promotion_unit_price, promotion_starts_at, promotion_ends_at,
    sticky_days, sticky_unit_price, sticky_starts_at, sticky_ends_at,
    total_price, policy_revision, authorization_decision_id,
    payload_sha256, magic_transaction_id, balance_after, purchased_at
) VALUES (
    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20
)`, command.OrderID, command.BuyerID, command.TorrentID, nullableUUID(order.CampaignID),
		nullablePromotion(order.Promotion), nullablePositiveInt(order.PromotionDays), nullableStickyPrice(order.PromotionDays, order.PromotionUnitPrice),
		productWindowStart(order.PromotionWindow), productWindowEnd(order.PromotionWindow),
		nullablePositiveInt(order.StickyDays), nullableStickyPrice(order.StickyDays, order.StickyUnitPrice),
		productWindowStart(order.StickyWindow), productWindowEnd(order.StickyWindow),
		order.TotalPrice, order.PolicyRevision, command.AuthorizationID, payloadSHA[:], nullableUUID(transactionID),
		order.BalanceAfter, command.Now,
	); err != nil {
		return ProductOrder{}, classifyProductDatabaseError("insert promotion product order", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ProductOrder{}, classifyProductDatabaseError("commit promotion product purchase", err)
	}
	return order, nil
}

func (repository *PostgresRepository) ListProductOrders(ctx context.Context, query ProductOrderQuery) (ProductOrderPage, error) {
	query.Query = strings.TrimSpace(query.Query)
	if !validProductPage(query.Limit, query.Offset) {
		return ProductOrderPage{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ProductOrderPage{}, fmt.Errorf("begin promotion product order read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	filter := `
FROM promotion.product_orders AS product_order
JOIN identity.users AS buyer ON buyer.id = product_order.buyer_id
JOIN torrents.torrents AS torrent ON torrent.id = product_order.torrent_id
WHERE ($1::uuid IS NULL OR product_order.buyer_id = $1)
  AND (
      $2 = '' OR buyer.username ILIKE '%' || $2 || '%'
      OR buyer.display_name ILIKE '%' || $2 || '%'
      OR buyer.numeric_id::text = $2 OR torrent.id::text = $2
      OR torrent.title ILIKE '%' || $2 || '%'
  )`
	var buyerID any
	if query.BuyerID != uuid.Nil {
		buyerID = query.BuyerID
	}
	var total int64
	if err := tx.QueryRow(ctx, `SELECT count(*)::bigint `+filter, buyerID, query.Query).Scan(&total); err != nil {
		return ProductOrderPage{}, fmt.Errorf("count promotion product orders: %w", err)
	}
	rows, err := tx.Query(ctx, `
SELECT
    product_order.id, buyer.id, buyer.numeric_id, buyer.username,
    product_order.torrent_id, torrent.title, product_order.campaign_id,
    product_order.promotion, product_order.promotion_days, product_order.promotion_unit_price,
    product_order.promotion_starts_at, product_order.promotion_ends_at,
    product_order.sticky_days, product_order.sticky_unit_price,
    product_order.sticky_starts_at, product_order.sticky_ends_at,
    product_order.total_price, product_order.policy_revision,
    product_order.balance_after, product_order.purchased_at
`+filter+`
ORDER BY product_order.purchased_at DESC, product_order.id DESC
LIMIT $3 OFFSET $4`, buyerID, query.Query, query.Limit, query.Offset)
	if err != nil {
		return ProductOrderPage{}, fmt.Errorf("list promotion product orders: %w", err)
	}
	defer rows.Close()
	items := make([]ProductOrder, 0, query.Limit)
	for rows.Next() {
		item, err := scanProductOrder(rows)
		if err != nil {
			return ProductOrderPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ProductOrderPage{}, fmt.Errorf("iterate promotion product orders: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ProductOrderPage{}, fmt.Errorf("commit promotion product order read: %w", err)
	}
	return ProductOrderPage{Items: items, Total: total, Limit: query.Limit, Offset: query.Offset}, nil
}

func (repository *PostgresRepository) CurrentProductPolicy(ctx context.Context, now time.Time) (ProductPolicy, error) {
	if now.IsZero() {
		return ProductPolicy{}, ErrInput
	}
	return currentProductPolicy(ctx, repository.pool, canonicalProductTime(now))
}

func currentProductPolicy(ctx context.Context, querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, now time.Time) (ProductPolicy, error) {
	var policy ProductPolicy
	err := querier.QueryRow(ctx, `
SELECT revision, effective_from, promotion_enabled, sticky_enabled,
       free_price_per_day, double_upload_price_per_day,
       double_upload_free_price_per_day, half_download_price_per_day,
       double_upload_half_download_price_per_day,
       thirty_percent_download_price_per_day, sticky_price_per_day,
       max_promotion_days, max_sticky_days
FROM promotion.product_policy_revisions
WHERE effective_from <= $1
ORDER BY effective_from DESC, revision DESC
LIMIT 1`, now).Scan(
		&policy.Revision, &policy.EffectiveFrom, &policy.PromotionEnabled, &policy.StickyEnabled,
		&policy.FreePricePerDay, &policy.DoubleUploadPricePerDay,
		&policy.DoubleUploadFreePricePerDay, &policy.HalfDownloadPricePerDay,
		&policy.DoubleUploadHalfDownloadPricePerDay,
		&policy.ThirtyPercentDownloadPricePerDay, &policy.StickyPricePerDay,
		&policy.MaxPromotionDays, &policy.MaxStickyDays,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProductPolicy{}, ErrInvariant
	}
	if err != nil {
		return ProductPolicy{}, fmt.Errorf("read promotion product policy: %w", err)
	}
	policy.EffectiveFrom = policy.EffectiveFrom.UTC()
	if policy.Revision == "" || policy.MaxPromotionDays < 1 || policy.MaxStickyDays < 1 {
		return ProductPolicy{}, ErrInvariant
	}
	return policy, nil
}

func (repository *PostgresRepository) UpdateProductPolicy(ctx context.Context, command UpdateProductPolicyCommand) (ProductPolicy, error) {
	command.OccurredAt = canonicalProductTime(command.OccurredAt)
	command.ExpectedRevision = strings.TrimSpace(command.ExpectedRevision)
	command.Reason = strings.TrimSpace(command.Reason)
	if command.RequestID == uuid.Nil || command.ActorID == uuid.Nil || command.AuthorizationID == uuid.Nil ||
		command.ExpectedRevision == "" || command.Reason == "" || command.OccurredAt.IsZero() || !validProductPolicyInput(command) {
		return ProductPolicy{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ProductPolicy{}, fmt.Errorf("begin promotion product policy update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('promotion-product-policy', 0))`); err != nil {
		return ProductPolicy{}, fmt.Errorf("lock promotion product policy: %w", err)
	}
	if replay, found, err := readProductPolicyByRequest(ctx, tx, command.RequestID); found || err != nil {
		if err != nil {
			return ProductPolicy{}, err
		}
		if !productPolicyValuesEqual(replay, productPolicyFromUpdate(command)) {
			return ProductPolicy{}, ErrIdempotencyConflict
		}
		return replay, nil
	}
	current, err := currentProductPolicy(ctx, tx, command.OccurredAt)
	if err != nil {
		return ProductPolicy{}, err
	}
	if current.Revision != command.ExpectedRevision {
		return ProductPolicy{}, ErrVersionConflict
	}
	next := productPolicyFromUpdate(command)
	if productPolicyValuesEqual(current, next) {
		return ProductPolicy{}, ErrNoChange
	}
	var latest time.Time
	if err := tx.QueryRow(ctx, `SELECT max(effective_from) FROM promotion.product_policy_revisions`).Scan(&latest); err != nil {
		return ProductPolicy{}, fmt.Errorf("read latest promotion product policy time: %w", err)
	}
	effectiveFrom := command.OccurredAt
	if !latest.Before(effectiveFrom) {
		effectiveFrom = latest.Add(time.Microsecond)
	}
	revision := "promotion-products-" + strconv.FormatInt(effectiveFrom.UnixMicro(), 10) + "-" + strings.ReplaceAll(command.RequestID.String(), "-", "")[:8]
	next.Revision = revision
	next.EffectiveFrom = effectiveFrom
	snapshotValue := productPolicySnapshot(next, current.Revision)
	snapshot, err := json.Marshal(snapshotValue)
	if err != nil {
		return ProductPolicy{}, fmt.Errorf("marshal promotion product policy: %w", err)
	}
	snapshotSHA := sha256.Sum256(snapshot)
	if _, err := tx.Exec(ctx, `
INSERT INTO promotion.product_policy_revisions (
    revision, effective_from, promotion_enabled, sticky_enabled,
    free_price_per_day, double_upload_price_per_day,
    double_upload_free_price_per_day, half_download_price_per_day,
    double_upload_half_download_price_per_day,
    thirty_percent_download_price_per_day, sticky_price_per_day,
    max_promotion_days, max_sticky_days,
    snapshot_json, snapshot_sha256, issued_by,
    authorization_decision_id, reason, created_at, request_id
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
		next.Revision, next.EffectiveFrom, next.PromotionEnabled, next.StickyEnabled,
		next.FreePricePerDay, next.DoubleUploadPricePerDay, next.DoubleUploadFreePricePerDay,
		next.HalfDownloadPricePerDay, next.DoubleUploadHalfDownloadPricePerDay,
		next.ThirtyPercentDownloadPricePerDay, next.StickyPricePerDay,
		next.MaxPromotionDays, next.MaxStickyDays, string(snapshot), snapshotSHA[:],
		command.ActorID, command.AuthorizationID, command.Reason, command.OccurredAt, command.RequestID,
	); err != nil {
		return ProductPolicy{}, classifyProductDatabaseError("insert promotion product policy", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ProductPolicy{}, classifyProductDatabaseError("commit promotion product policy", err)
	}
	return next, nil
}

func nextPromotionWindowStart(ctx context.Context, tx pgx.Tx, torrentID int64, now time.Time, duration time.Duration) (time.Time, error) {
	candidate := now
	for attempt := 0; attempt < 256; attempt++ {
		var conflictingEnd time.Time
		err := tx.QueryRow(ctx, `
SELECT ends_at
FROM promotion.campaigns
WHERE (scope_type = 'global' OR torrent_id = $1)
  AND starts_at < $2
	  AND ends_at > $3
ORDER BY ends_at DESC, id DESC
LIMIT 1`, torrentID, candidate.Add(duration), candidate).Scan(&conflictingEnd)
		if errors.Is(err, pgx.ErrNoRows) {
			return candidate, nil
		}
		if err != nil {
			return time.Time{}, fmt.Errorf("read next paid promotion window: %w", err)
		}
		if !conflictingEnd.After(candidate) {
			return time.Time{}, ErrInvariant
		}
		candidate = canonicalProductTime(conflictingEnd)
	}
	return time.Time{}, ErrInvariant
}

type productOrderPayloadValue struct {
	OrderID            string     `json:"order_id"`
	BuyerID            string     `json:"buyer_id"`
	TorrentID          int64      `json:"torrent_id"`
	Promotion          *Promotion `json:"promotion,omitempty"`
	PromotionDays      int        `json:"promotion_days,omitempty"`
	PromotionUnitPrice int64      `json:"promotion_unit_price,omitempty"`
	PromotionStartsAt  *time.Time `json:"promotion_starts_at,omitempty"`
	PromotionEndsAt    *time.Time `json:"promotion_ends_at,omitempty"`
	StickyDays         int        `json:"sticky_days,omitempty"`
	StickyUnitPrice    int64      `json:"sticky_unit_price,omitempty"`
	StickyStartsAt     *time.Time `json:"sticky_starts_at,omitempty"`
	StickyEndsAt       *time.Time `json:"sticky_ends_at,omitempty"`
	TotalPrice         int64      `json:"total_price"`
	PolicyRevision     string     `json:"policy_revision"`
}

func productOrderPayload(command ProductPurchaseCommand, order ProductOrder) productOrderPayloadValue {
	value := productOrderPayloadValue{
		OrderID: command.OrderID.String(), BuyerID: command.BuyerID.String(), TorrentID: command.TorrentID,
		Promotion: order.Promotion, PromotionDays: order.PromotionDays, PromotionUnitPrice: order.PromotionUnitPrice,
		StickyDays: order.StickyDays, StickyUnitPrice: order.StickyUnitPrice,
		TotalPrice: order.TotalPrice, PolicyRevision: order.PolicyRevision,
	}
	if order.PromotionWindow != nil {
		value.PromotionStartsAt = &order.PromotionWindow.StartsAt
		value.PromotionEndsAt = &order.PromotionWindow.EndsAt
	}
	if order.StickyWindow != nil {
		value.StickyStartsAt = &order.StickyWindow.StartsAt
		value.StickyEndsAt = &order.StickyWindow.EndsAt
	}
	return value
}

func productReplayMatches(order ProductOrder, command ProductPurchaseCommand) bool {
	if order.ID != command.OrderID || order.BuyerID != command.BuyerID || order.TorrentID != command.TorrentID || order.StickyDays != command.Selection.StickyDays ||
		order.PromotionDays != command.Selection.PromotionDays {
		return false
	}
	if (order.Promotion == nil) != (command.Selection.Promotion == nil) {
		return false
	}
	return order.Promotion == nil || *order.Promotion == *command.Selection.Promotion
}

func readProductOrderByID(ctx context.Context, querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, orderID uuid.UUID) (ProductOrder, bool, error) {
	row := querier.QueryRow(ctx, `
SELECT
    product_order.id, buyer.id, buyer.numeric_id, buyer.username,
    product_order.torrent_id, torrent.title, product_order.campaign_id,
    product_order.promotion, product_order.promotion_days, product_order.promotion_unit_price,
    product_order.promotion_starts_at, product_order.promotion_ends_at,
    product_order.sticky_days, product_order.sticky_unit_price,
    product_order.sticky_starts_at, product_order.sticky_ends_at,
    product_order.total_price, product_order.policy_revision,
    product_order.balance_after, product_order.purchased_at
FROM promotion.product_orders AS product_order
JOIN identity.users AS buyer ON buyer.id = product_order.buyer_id
JOIN torrents.torrents AS torrent ON torrent.id = product_order.torrent_id
WHERE product_order.id = $1`, orderID)
	order, err := scanProductOrder(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProductOrder{}, false, nil
	}
	if err != nil {
		return ProductOrder{}, true, err
	}
	return order, true, nil
}

type rowScanner interface{ Scan(...any) error }

func scanProductOrder(row rowScanner) (ProductOrder, error) {
	var order ProductOrder
	var campaignID pgtype.UUID
	var promotion pgtype.Text
	var promotionDays, stickyDays pgtype.Int4
	var promotionPrice, stickyPrice pgtype.Int8
	var promotionStarts, promotionEnds, stickyStarts, stickyEnds pgtype.Timestamptz
	if err := row.Scan(
		&order.ID, &order.BuyerID, &order.BuyerNumericID, &order.BuyerUsername,
		&order.TorrentID, &order.TorrentTitle, &campaignID,
		&promotion, &promotionDays, &promotionPrice, &promotionStarts, &promotionEnds,
		&stickyDays, &stickyPrice, &stickyStarts, &stickyEnds,
		&order.TotalPrice, &order.PolicyRevision, &order.BalanceAfter, &order.PurchasedAt,
	); err != nil {
		return ProductOrder{}, err
	}
	if campaignID.Valid {
		value := uuid.UUID(campaignID.Bytes)
		order.CampaignID = &value
	}
	if promotion.Valid {
		value := Promotion(promotion.String)
		order.Promotion = &value
		order.PromotionDays = int(promotionDays.Int32)
		order.PromotionUnitPrice = promotionPrice.Int64
		order.PromotionWindow = &ProductWindow{StartsAt: promotionStarts.Time.UTC(), EndsAt: promotionEnds.Time.UTC()}
	}
	if stickyDays.Valid {
		order.StickyDays = int(stickyDays.Int32)
		order.StickyUnitPrice = stickyPrice.Int64
		order.StickyWindow = &ProductWindow{StartsAt: stickyStarts.Time.UTC(), EndsAt: stickyEnds.Time.UTC()}
	}
	order.PurchasedAt = order.PurchasedAt.UTC()
	if order.ID == uuid.Nil || order.BuyerID == uuid.Nil || order.BuyerNumericID < 1 || order.TorrentID < 1 || order.TorrentTitle == "" || order.PolicyRevision == "" ||
		order.TotalPrice < 0 || order.PurchasedAt.IsZero() || (order.Promotion == nil && order.StickyDays == 0) {
		return ProductOrder{}, ErrInvariant
	}
	return order, nil
}

func readProductPolicyByRequest(ctx context.Context, tx pgx.Tx, requestID uuid.UUID) (ProductPolicy, bool, error) {
	var policy ProductPolicy
	err := tx.QueryRow(ctx, `
SELECT revision, effective_from, promotion_enabled, sticky_enabled,
       free_price_per_day, double_upload_price_per_day,
       double_upload_free_price_per_day, half_download_price_per_day,
       double_upload_half_download_price_per_day,
       thirty_percent_download_price_per_day, sticky_price_per_day,
       max_promotion_days, max_sticky_days
FROM promotion.product_policy_revisions
WHERE request_id = $1`, requestID).Scan(
		&policy.Revision, &policy.EffectiveFrom, &policy.PromotionEnabled, &policy.StickyEnabled,
		&policy.FreePricePerDay, &policy.DoubleUploadPricePerDay,
		&policy.DoubleUploadFreePricePerDay, &policy.HalfDownloadPricePerDay,
		&policy.DoubleUploadHalfDownloadPricePerDay,
		&policy.ThirtyPercentDownloadPricePerDay, &policy.StickyPricePerDay,
		&policy.MaxPromotionDays, &policy.MaxStickyDays,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProductPolicy{}, false, nil
	}
	if err != nil {
		return ProductPolicy{}, true, fmt.Errorf("read promotion product policy replay: %w", err)
	}
	policy.EffectiveFrom = policy.EffectiveFrom.UTC()
	return policy, true, nil
}

func productPolicyFromUpdate(command UpdateProductPolicyCommand) ProductPolicy {
	return ProductPolicy{
		PromotionEnabled: command.PromotionEnabled, StickyEnabled: command.StickyEnabled,
		FreePricePerDay: command.FreePricePerDay, DoubleUploadPricePerDay: command.DoubleUploadPricePerDay,
		DoubleUploadFreePricePerDay:         command.DoubleUploadFreePricePerDay,
		HalfDownloadPricePerDay:             command.HalfDownloadPricePerDay,
		DoubleUploadHalfDownloadPricePerDay: command.DoubleUploadHalfDownloadPricePerDay,
		ThirtyPercentDownloadPricePerDay:    command.ThirtyPercentDownloadPricePerDay,
		StickyPricePerDay:                   command.StickyPricePerDay,
		MaxPromotionDays:                    command.MaxPromotionDays, MaxStickyDays: command.MaxStickyDays,
	}
}

func productPolicyValuesEqual(left, right ProductPolicy) bool {
	return left.PromotionEnabled == right.PromotionEnabled && left.StickyEnabled == right.StickyEnabled &&
		left.FreePricePerDay == right.FreePricePerDay && left.DoubleUploadPricePerDay == right.DoubleUploadPricePerDay &&
		left.DoubleUploadFreePricePerDay == right.DoubleUploadFreePricePerDay &&
		left.HalfDownloadPricePerDay == right.HalfDownloadPricePerDay &&
		left.DoubleUploadHalfDownloadPricePerDay == right.DoubleUploadHalfDownloadPricePerDay &&
		left.ThirtyPercentDownloadPricePerDay == right.ThirtyPercentDownloadPricePerDay &&
		left.StickyPricePerDay == right.StickyPricePerDay && left.MaxPromotionDays == right.MaxPromotionDays && left.MaxStickyDays == right.MaxStickyDays
}

func productPolicySnapshot(policy ProductPolicy, previousRevision string) any {
	return struct {
		Revision                            string `json:"revision"`
		PreviousRevision                    string `json:"previous_revision"`
		EffectiveFrom                       string `json:"effective_from"`
		PromotionEnabled                    bool   `json:"promotion_enabled"`
		StickyEnabled                       bool   `json:"sticky_enabled"`
		FreePricePerDay                     int64  `json:"free_price_per_day"`
		DoubleUploadPricePerDay             int64  `json:"double_upload_price_per_day"`
		DoubleUploadFreePricePerDay         int64  `json:"double_upload_free_price_per_day"`
		HalfDownloadPricePerDay             int64  `json:"half_download_price_per_day"`
		DoubleUploadHalfDownloadPricePerDay int64  `json:"double_upload_half_download_price_per_day"`
		ThirtyPercentDownloadPricePerDay    int64  `json:"thirty_percent_download_price_per_day"`
		StickyPricePerDay                   int64  `json:"sticky_price_per_day"`
		MaxPromotionDays                    int    `json:"max_promotion_days"`
		MaxStickyDays                       int    `json:"max_sticky_days"`
		Currency                            string `json:"currency"`
	}{
		policy.Revision, previousRevision, policy.EffectiveFrom.Format(time.RFC3339Nano),
		policy.PromotionEnabled, policy.StickyEnabled,
		policy.FreePricePerDay, policy.DoubleUploadPricePerDay, policy.DoubleUploadFreePricePerDay,
		policy.HalfDownloadPricePerDay, policy.DoubleUploadHalfDownloadPricePerDay,
		policy.ThirtyPercentDownloadPricePerDay, policy.StickyPricePerDay,
		policy.MaxPromotionDays, policy.MaxStickyDays, "magic",
	}
}

func productMemberBalanceAfter(transaction economy.Transaction, buyerID uuid.UUID) (int64, bool) {
	for _, posting := range transaction.Postings {
		if posting.AccountID == buyerID {
			return posting.BalanceAfter, true
		}
	}
	return 0, false
}

func nullableUUID(value *uuid.UUID) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullablePromotion(value *Promotion) any {
	if value == nil {
		return nil
	}
	return string(*value)
}

func nullablePositiveInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func nullableStickyPrice(days int, price int64) any {
	if days == 0 {
		return nil
	}
	return price
}

func productWindowStart(window *ProductWindow) any {
	if window == nil {
		return nil
	}
	return window.StartsAt
}

func productWindowEnd(window *ProductWindow) any {
	if window == nil {
		return nil
	}
	return window.EndsAt
}

func classifyProductDatabaseError(operation string, err error) error {
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) {
		switch databaseError.Code {
		case "23505":
			return fmt.Errorf("%s: %w", operation, ErrIdempotencyConflict)
		case "40001", "40P01":
			return fmt.Errorf("%s: retryable database conflict: %w", operation, err)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
