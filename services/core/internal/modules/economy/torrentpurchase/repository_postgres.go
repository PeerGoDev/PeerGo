package torrentpurchase

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
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/economy"
)

var (
	entitlementNamespace       = uuid.MustParse("0c752db0-fef7-5d79-89bd-7dce45a6c3ec")
	transactionNamespace       = uuid.MustParse("84fae7dd-8955-5748-93ae-61b71c18ef14")
	refundTransactionNamespace = uuid.MustParse("ac891b2d-267c-59c2-a552-2fe225db29a1")
)

type PostgresRepository struct {
	pool    *pgxpool.Pool
	economy *economy.PostgresRepository
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, ErrInput
	}
	ledger, err := economy.NewPostgresRepository(pool)
	if err != nil {
		return nil, err
	}
	return &PostgresRepository{pool: pool, economy: ledger}, nil
}

func (repository *PostgresRepository) Status(ctx context.Context, userID uuid.UUID, torrentID int64, now time.Time) (Status, error) {
	if userID == uuid.Nil || torrentID < 1 || now.IsZero() {
		return Status{}, ErrInput
	}
	return readStatus(ctx, repository.pool, userID, torrentID, canonicalTime(now))
}

func (repository *PostgresRepository) Purchase(ctx context.Context, command PurchaseCommand) (Receipt, error) {
	command.Now = canonicalTime(command.Now)
	if command.RequestID == uuid.Nil || command.UserID == uuid.Nil || command.TorrentID < 1 || command.Now.IsZero() {
		return Receipt{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Receipt{}, fmt.Errorf("begin torrent purchase: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	lockKey := "torrent-purchase:" + command.UserID.String() + ":" + strconv.FormatInt(command.TorrentID, 10)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return Receipt{}, fmt.Errorf("lock torrent purchase: %w", err)
	}
	if receipt, found, err := readReceiptByRequest(ctx, tx, command); found || err != nil {
		if err != nil {
			return Receipt{}, err
		}
		receipt.Replayed = true
		return receipt, nil
	}

	status, err := readStatusForUpdate(ctx, tx, command.UserID, command.TorrentID, command.Now)
	if err != nil {
		return Receipt{}, err
	}
	if status.State == AccessPurchased {
		receipt, err := readReceiptByPair(ctx, tx, command.UserID, command.TorrentID)
		if err != nil {
			return Receipt{}, err
		}
		receipt.Replayed = true
		return receipt, nil
	}
	if status.State == AccessFree || status.State == AccessUploader {
		return Receipt{}, ErrPurchaseNotRequired
	}
	if status.State == AccessPurchaseDisabled {
		return Receipt{}, ErrPurchaseDisabled
	}
	if status.State != AccessPurchaseRequired || status.Price <= 0 || status.SellerID == command.UserID {
		return Receipt{}, ErrInvariant
	}
	var purchaseSequence int64
	if err := tx.QueryRow(ctx, `
SELECT COALESCE(max(purchase_sequence), 0)::bigint + 1
FROM economy.torrent_purchase_entitlements
WHERE user_id = $1 AND torrent_id = $2`, command.UserID, command.TorrentID).Scan(&purchaseSequence); err != nil {
		return Receipt{}, fmt.Errorf("read next torrent purchase sequence: %w", err)
	}

	payload := struct {
		RequestID      string `json:"request_id"`
		UserID         string `json:"user_id"`
		TorrentID      int64  `json:"torrent_id"`
		SellerID       string `json:"seller_id"`
		Price          int64  `json:"price"`
		Tax            int64  `json:"tax"`
		SellerIncome   int64  `json:"seller_income"`
		PolicyRevision string `json:"policy_revision"`
	}{
		RequestID: command.RequestID.String(), UserID: command.UserID.String(),
		TorrentID: command.TorrentID, SellerID: status.SellerID.String(),
		Price: status.Price, Tax: status.Tax, SellerIncome: status.SellerIncome,
		PolicyRevision: status.PolicyRevision,
	}
	canonicalPayload, err := json.Marshal(payload)
	if err != nil {
		return Receipt{}, fmt.Errorf("marshal torrent purchase payload: %w", err)
	}
	payloadSHA := sha256.Sum256(canonicalPayload)
	sourceReference := torrentTransactionSourceReference("purchase", command.TorrentID, command.RequestID)
	postings := []economy.PostingInput{{AccountID: command.UserID, Amount: -status.Price}}
	if status.SellerIncome > 0 {
		postings = append(postings, economy.PostingInput{AccountID: status.SellerID, Amount: status.SellerIncome})
	}
	if status.Tax > 0 {
		postings = append(postings, economy.PostingInput{AccountID: economy.FeeSinkAccountID(), Amount: status.Tax})
	}
	transactionID := uuid.NewSHA1(transactionNamespace, []byte(command.RequestID.String()))
	ledger, err := repository.economy.RecordInTransaction(ctx, tx, economy.RecordCommand{
		TransactionID: transactionID, TransactionType: economy.TransactionTorrentBuy,
		IdempotencyKey: "torrent-purchase:" + strings.ReplaceAll(command.RequestID.String(), "-", ""),
		// A torrent can be sold to many members. The source reference identifies
		// this sale, not only the shared torrent, so every seller-income statement
		// remains unique while the request UUID still provides replay safety.
		SourceReference: sourceReference,
		PolicyRevision:  status.PolicyRevision, PayloadSHA256: payloadSHA,
		OccurredAt: command.Now, RecordedAt: command.Now, Postings: postings,
	})
	if err != nil {
		return Receipt{}, classifyError("record torrent purchase ledger", err)
	}

	entitlementID := uuid.NewSHA1(entitlementNamespace, []byte(command.RequestID.String()))
	if _, err := tx.Exec(ctx, `
INSERT INTO economy.torrent_purchase_entitlements (
    id, request_id, user_id, torrent_id, seller_id,
    source_kind, source_reference, price, tax, seller_income,
    policy_revision, payload_sha256, magic_transaction_id,
    purchased_at, recorded_at, purchase_sequence
) VALUES ($1, $2, $3, $4, $5, 'live_purchase', $6, $7, $8, $9, $10, $11, $12, $13, $13, $14)`,
		entitlementID, command.RequestID, command.UserID, command.TorrentID, status.SellerID,
		sourceReference, status.Price, status.Tax,
		status.SellerIncome, status.PolicyRevision, payloadSHA[:], transactionID, command.Now, purchaseSequence,
	); err != nil {
		return Receipt{}, classifyError("insert torrent purchase entitlement", err)
	}
	balanceAfter, found := memberBalanceAfter(ledger, command.UserID)
	if !found {
		return Receipt{}, ErrInvariant
	}
	if err := tx.Commit(ctx); err != nil {
		return Receipt{}, classifyError("commit torrent purchase", err)
	}
	return Receipt{
		EntitlementID: entitlementID, RequestID: command.RequestID,
		UserID: command.UserID, TorrentID: command.TorrentID, SellerID: status.SellerID,
		Price: status.Price, Tax: status.Tax, SellerIncome: status.SellerIncome,
		BalanceAfter: balanceAfter, PolicyRevision: status.PolicyRevision,
		MagicTransactionID: transactionID, PurchasedAt: command.Now,
	}, nil
}

func (repository *PostgresRepository) ListHistory(ctx context.Context, query HistoryQuery) (HistoryPage, error) {
	if query.UserID == uuid.Nil || query.Limit < 1 || query.Limit > MaxHistoryLimit || query.Offset < 0 || query.Offset > MaxHistoryOffset {
		return HistoryPage{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return HistoryPage{}, fmt.Errorf("begin torrent purchase history read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var total int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM economy.torrent_purchase_entitlements AS entitlement
LEFT JOIN economy.torrent_purchase_refunds AS refund
  ON refund.entitlement_id = entitlement.id
WHERE entitlement.user_id = $1 AND refund.id IS NULL`, query.UserID).Scan(&total); err != nil {
		return HistoryPage{}, fmt.Errorf("count torrent purchase history: %w", err)
	}
	rows, err := tx.Query(ctx, `
SELECT
    entitlement.torrent_id, torrent.title, category.name, torrent.state,
    entitlement.price, entitlement.purchased_at,
    (entitlement.source_kind = 'legacy_import')::boolean
FROM economy.torrent_purchase_entitlements AS entitlement
JOIN torrents.torrents AS torrent ON torrent.id = entitlement.torrent_id
JOIN catalog.categories AS category ON category.id = torrent.category_id
LEFT JOIN economy.torrent_purchase_refunds AS refund
  ON refund.entitlement_id = entitlement.id
WHERE entitlement.user_id = $1 AND refund.id IS NULL
ORDER BY entitlement.purchased_at DESC, entitlement.torrent_id DESC
LIMIT $2 OFFSET $3`, query.UserID, query.Limit, query.Offset)
	if err != nil {
		return HistoryPage{}, fmt.Errorf("list torrent purchase history: %w", err)
	}
	defer rows.Close()
	items := make([]HistoryItem, 0, query.Limit)
	for rows.Next() {
		var item HistoryItem
		if err := rows.Scan(&item.TorrentID, &item.Title, &item.CategoryName, &item.TorrentState, &item.Price, &item.PurchasedAt, &item.LegacyImport); err != nil {
			return HistoryPage{}, fmt.Errorf("scan torrent purchase history: %w", err)
		}
		item.PurchasedAt = item.PurchasedAt.UTC()
		if item.TorrentID < 1 || strings.TrimSpace(item.Title) == "" || strings.TrimSpace(item.CategoryName) == "" || item.Price < 0 || item.PurchasedAt.IsZero() {
			return HistoryPage{}, ErrInvariant
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return HistoryPage{}, fmt.Errorf("iterate torrent purchase history: %w", err)
	}
	if total < int64(len(items)) {
		return HistoryPage{}, ErrInvariant
	}
	if err := tx.Commit(ctx); err != nil {
		return HistoryPage{}, fmt.Errorf("commit torrent purchase history read: %w", err)
	}
	return HistoryPage{Items: items, Total: total, Limit: query.Limit, Offset: query.Offset}, nil
}

func (repository *PostgresRepository) CurrentPolicy(ctx context.Context, now time.Time) (PolicySettings, error) {
	if now.IsZero() {
		return PolicySettings{}, ErrInput
	}
	policy, err := scanPolicy(repository.pool.QueryRow(ctx, `
SELECT enabled, tax_basis_points, revision, effective_from
FROM economy.torrent_purchase_policy_revisions
WHERE effective_from <= $1
ORDER BY effective_from DESC, revision DESC
LIMIT 1`, canonicalTime(now)))
	if errors.Is(err, pgx.ErrNoRows) {
		return PolicySettings{}, ErrInvariant
	}
	return policy, err
}

func (repository *PostgresRepository) UpdatePolicy(ctx context.Context, command UpdatePolicyCommand) (PolicySettings, error) {
	command.OccurredAt = canonicalTime(command.OccurredAt)
	if command.RequestID == uuid.Nil || command.ActorID == uuid.Nil || command.AuthorizationID == uuid.Nil ||
		command.TaxBasisPoints < 0 || command.TaxBasisPoints > 10000 || strings.TrimSpace(command.ExpectedRevision) == "" ||
		strings.TrimSpace(command.Reason) == "" || command.OccurredAt.IsZero() {
		return PolicySettings{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return PolicySettings{}, fmt.Errorf("begin torrent purchase policy update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('torrent-purchase-policy', 0))`); err != nil {
		return PolicySettings{}, fmt.Errorf("lock torrent purchase policy: %w", err)
	}
	if replay, found, replayErr := readPolicyByRequest(ctx, tx, command); found || replayErr != nil {
		if replayErr != nil {
			return PolicySettings{}, replayErr
		}
		if err := tx.Commit(ctx); err != nil {
			return PolicySettings{}, fmt.Errorf("commit replayed torrent purchase policy: %w", err)
		}
		return replay, nil
	}
	current, err := scanPolicy(tx.QueryRow(ctx, `
SELECT enabled, tax_basis_points, revision, effective_from
FROM economy.torrent_purchase_policy_revisions
WHERE effective_from <= $1
ORDER BY effective_from DESC, revision DESC
LIMIT 1`, command.OccurredAt))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PolicySettings{}, ErrInvariant
		}
		return PolicySettings{}, fmt.Errorf("read current torrent purchase policy: %w", err)
	}
	if current.Revision != command.ExpectedRevision {
		return PolicySettings{}, ErrVersionConflict
	}
	if current.Enabled == command.Enabled && current.TaxBasisPoints == command.TaxBasisPoints {
		return PolicySettings{}, ErrNoChange
	}
	effectiveFrom := command.OccurredAt
	var latestEffective time.Time
	if err := tx.QueryRow(ctx, `SELECT max(effective_from) FROM economy.torrent_purchase_policy_revisions`).Scan(&latestEffective); err != nil {
		return PolicySettings{}, fmt.Errorf("read latest torrent purchase policy time: %w", err)
	}
	if !latestEffective.Before(effectiveFrom) {
		effectiveFrom = latestEffective.Add(time.Microsecond)
	}
	revision := "torrent-purchase-" + strconv.FormatInt(effectiveFrom.UnixMicro(), 10) + "-" + strings.ReplaceAll(command.RequestID.String(), "-", "")[:8]
	snapshot := struct {
		Revision         string `json:"revision"`
		PreviousRevision string `json:"previous_revision"`
		EffectiveFrom    string `json:"effective_from"`
		Enabled          bool   `json:"enabled"`
		TaxBasisPoints   int64  `json:"tax_basis_points"`
		Currency         string `json:"currency"`
	}{revision, current.Revision, effectiveFrom.Format(time.RFC3339Nano), command.Enabled, command.TaxBasisPoints, "magic"}
	canonicalSnapshot, err := json.Marshal(snapshot)
	if err != nil {
		return PolicySettings{}, fmt.Errorf("marshal torrent purchase policy: %w", err)
	}
	snapshotSHA := sha256.Sum256(canonicalSnapshot)
	if _, err := tx.Exec(ctx, `
INSERT INTO economy.torrent_purchase_policy_revisions (
    revision, effective_from, enabled, tax_basis_points,
    snapshot_json, snapshot_sha256, issued_by,
    authorization_decision_id, reason, created_at, request_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		revision, effectiveFrom, command.Enabled, command.TaxBasisPoints,
		string(canonicalSnapshot), snapshotSHA[:], command.ActorID,
		command.AuthorizationID, strings.TrimSpace(command.Reason), command.OccurredAt, command.RequestID,
	); err != nil {
		return PolicySettings{}, classifyAdministrationError("insert torrent purchase policy", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PolicySettings{}, classifyAdministrationError("commit torrent purchase policy", err)
	}
	return PolicySettings{Enabled: command.Enabled, TaxBasisPoints: command.TaxBasisPoints, Revision: revision, EffectiveFrom: effectiveFrom}, nil
}

func (repository *PostgresRepository) UpdatePrice(ctx context.Context, command UpdatePriceCommand) (PriceChange, error) {
	command.OccurredAt = canonicalTime(command.OccurredAt)
	if command.RequestID == uuid.Nil || command.ActorID == uuid.Nil || command.AuthorizationID == uuid.Nil || command.TorrentID < 1 ||
		command.Price < 0 || command.Price > 1_000_000 || command.ExpectedVersion < 1 || strings.TrimSpace(command.Reason) == "" || command.OccurredAt.IsZero() {
		return PriceChange{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PriceChange{}, fmt.Errorf("begin torrent price update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if replay, found, replayErr := readPriceChangeByRequest(ctx, tx, command); found || replayErr != nil {
		if replayErr != nil {
			return PriceChange{}, replayErr
		}
		if err := tx.Commit(ctx); err != nil {
			return PriceChange{}, fmt.Errorf("commit replayed torrent price update: %w", err)
		}
		return replay, nil
	}
	var title, state string
	var previousPrice, version int64
	if err := tx.QueryRow(ctx, `
SELECT title, state, purchase_price, version
FROM torrents.torrents
WHERE id = $1
FOR UPDATE`, command.TorrentID).Scan(&title, &state, &previousPrice, &version); errors.Is(err, pgx.ErrNoRows) {
		return PriceChange{}, ErrNotFound
	} else if err != nil {
		return PriceChange{}, fmt.Errorf("lock torrent price: %w", err)
	}
	if state == "deleted" {
		return PriceChange{}, ErrNotFound
	}
	if version != command.ExpectedVersion {
		return PriceChange{}, ErrVersionConflict
	}
	if previousPrice == command.Price {
		return PriceChange{}, ErrNoChange
	}
	var resultingVersion int64
	var changedAt time.Time
	if err := tx.QueryRow(ctx, `
UPDATE torrents.torrents
SET purchase_price = $1, version = version + 1, updated_at = $2
WHERE id = $3 AND version = $4
RETURNING version, updated_at`, command.Price, command.OccurredAt, command.TorrentID, command.ExpectedVersion).Scan(&resultingVersion, &changedAt); errors.Is(err, pgx.ErrNoRows) {
		return PriceChange{}, ErrVersionConflict
	} else if err != nil {
		return PriceChange{}, fmt.Errorf("update torrent price: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO economy.torrent_purchase_price_changes (
    id, torrent_id, actor_id, previous_price, resulting_price,
    expected_torrent_version, resulting_torrent_version, reason,
    authorization_decision_id, occurred_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		command.RequestID, command.TorrentID, command.ActorID, previousPrice, command.Price,
		command.ExpectedVersion, resultingVersion, strings.TrimSpace(command.Reason),
		command.AuthorizationID, command.OccurredAt,
	); err != nil {
		return PriceChange{}, classifyAdministrationError("insert torrent price change", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PriceChange{}, classifyAdministrationError("commit torrent price update", err)
	}
	return PriceChange{RequestID: command.RequestID, TorrentID: command.TorrentID, Title: title, Price: command.Price, Version: resultingVersion, ChangedAt: changedAt.UTC()}, nil
}

func (repository *PostgresRepository) ListPurchases(ctx context.Context, query AdminPurchaseQuery) (AdminPurchasePage, error) {
	query.Query = strings.TrimSpace(query.Query)
	if query.Limit < 1 || query.Limit > MaxAdminLimit || query.Offset < 0 || query.Offset > MaxAdminOffset ||
		!validAdminPurchaseStatus(query.Status) || !validAdminPurchaseSource(query.Source) {
		return AdminPurchasePage{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return AdminPurchasePage{}, fmt.Errorf("begin managed torrent purchase read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const filterSQL = `
FROM economy.torrent_purchase_entitlements AS entitlement
JOIN identity.users AS buyer ON buyer.id = entitlement.user_id
JOIN identity.users AS seller ON seller.id = entitlement.seller_id
JOIN torrents.torrents AS torrent ON torrent.id = entitlement.torrent_id
JOIN catalog.categories AS category ON category.id = torrent.category_id
LEFT JOIN economy.torrent_purchase_refunds AS refund
  ON refund.entitlement_id = entitlement.id
LEFT JOIN identity.users AS refund_actor ON refund_actor.id = refund.refunded_by
WHERE (
    $1 = ''
    OR buyer.username ILIKE '%' || $1 || '%'
    OR buyer.display_name ILIKE '%' || $1 || '%'
    OR torrent.title ILIKE '%' || $1 || '%'
    OR buyer.numeric_id::text = $1
    OR torrent.id::text = $1
)
AND (
    $2 = 'all'
    OR ($2 = 'active' AND refund.id IS NULL)
    OR ($2 = 'refunded' AND refund.id IS NOT NULL)
)
AND ($3 = 'all' OR entitlement.source_kind = $3)`

	var total int64
	if err := tx.QueryRow(ctx, `SELECT count(*)::bigint `+filterSQL, query.Query, query.Status, query.Source).Scan(&total); err != nil {
		return AdminPurchasePage{}, fmt.Errorf("count managed torrent purchases: %w", err)
	}
	rows, err := tx.Query(ctx, `
SELECT
    buyer.numeric_id, buyer.username, buyer.display_name,
    seller.numeric_id, seller.username,
    entitlement.torrent_id, torrent.title, category.name,
    entitlement.source_kind,
    CASE WHEN refund.id IS NULL THEN 'active' ELSE 'refunded' END,
    entitlement.price, entitlement.tax, entitlement.seller_income,
    entitlement.purchased_at,
    refund.refunded_at, COALESCE(refund.reason, ''),
    refund_actor.numeric_id, COALESCE(refund_actor.username, ''),
    refund.buyer_balance_after
`+filterSQL+`
ORDER BY entitlement.purchased_at DESC, entitlement.torrent_id DESC, entitlement.purchase_sequence DESC
LIMIT $4 OFFSET $5`, query.Query, query.Status, query.Source, query.Limit, query.Offset)
	if err != nil {
		return AdminPurchasePage{}, fmt.Errorf("list managed torrent purchases: %w", err)
	}
	defer rows.Close()
	items := make([]AdminPurchaseItem, 0, query.Limit)
	for rows.Next() {
		var item AdminPurchaseItem
		var source, status string
		var refundedAt pgtype.Timestamptz
		var refundedBy pgtype.Int8
		var balanceAfter pgtype.Int8
		if err := rows.Scan(
			&item.BuyerNumericID, &item.BuyerUsername, &item.BuyerDisplayName,
			&item.SellerNumericID, &item.SellerUsername,
			&item.TorrentID, &item.TorrentTitle, &item.CategoryName,
			&source, &status, &item.Price, &item.Tax, &item.SellerIncome,
			&item.PurchasedAt, &refundedAt, &item.RefundReason,
			&refundedBy, &item.RefundedByUsername, &balanceAfter,
		); err != nil {
			return AdminPurchasePage{}, fmt.Errorf("scan managed torrent purchase: %w", err)
		}
		item.Source = AdminPurchaseSource(source)
		item.Status = AdminPurchaseStatus(status)
		item.PurchasedAt = item.PurchasedAt.UTC()
		if refundedAt.Valid {
			value := refundedAt.Time.UTC()
			item.RefundedAt = &value
		}
		if refundedBy.Valid {
			value := refundedBy.Int64
			item.RefundedByNumericID = &value
		}
		if balanceAfter.Valid {
			value := balanceAfter.Int64
			item.RefundedBalanceAfter = &value
		}
		if item.BuyerNumericID < 1 || item.SellerNumericID < 1 || item.TorrentID < 1 ||
			strings.TrimSpace(item.BuyerUsername) == "" || strings.TrimSpace(item.SellerUsername) == "" ||
			strings.TrimSpace(item.TorrentTitle) == "" || strings.TrimSpace(item.CategoryName) == "" ||
			item.Price < 0 || item.Tax < 0 || item.SellerIncome < 0 || item.PurchasedAt.IsZero() ||
			!validAdminPurchaseStatus(item.Status) || item.Status == AdminPurchaseStatusAll ||
			!validAdminPurchaseSource(item.Source) || item.Source == AdminPurchaseSourceAll {
			return AdminPurchasePage{}, ErrInvariant
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return AdminPurchasePage{}, fmt.Errorf("iterate managed torrent purchases: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminPurchasePage{}, fmt.Errorf("commit managed torrent purchase read: %w", err)
	}
	return AdminPurchasePage{Items: items, Total: total, Limit: query.Limit, Offset: query.Offset}, nil
}

func (repository *PostgresRepository) Refund(ctx context.Context, command RefundCommand) (RefundReceipt, error) {
	command.Reason = strings.TrimSpace(command.Reason)
	command.OccurredAt = canonicalTime(command.OccurredAt)
	if command.RequestID == uuid.Nil || command.ActorID == uuid.Nil || command.AuthorizationID == uuid.Nil ||
		command.BuyerNumericID < 1 || command.TorrentID < 1 || command.Reason == "" || command.OccurredAt.IsZero() {
		return RefundReceipt{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return RefundReceipt{}, fmt.Errorf("begin torrent purchase refund: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var buyerID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM identity.users WHERE numeric_id = $1`, command.BuyerNumericID).Scan(&buyerID); errors.Is(err, pgx.ErrNoRows) {
		return RefundReceipt{}, ErrNotFound
	} else if err != nil {
		return RefundReceipt{}, fmt.Errorf("resolve torrent refund buyer: %w", err)
	}
	lockKey := "torrent-purchase:" + buyerID.String() + ":" + strconv.FormatInt(command.TorrentID, 10)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return RefundReceipt{}, fmt.Errorf("lock torrent purchase refund: %w", err)
	}
	if replay, found, replayErr := readRefundByRequest(ctx, tx, command); found || replayErr != nil {
		if replayErr != nil {
			return RefundReceipt{}, replayErr
		}
		if err := tx.Commit(ctx); err != nil {
			return RefundReceipt{}, fmt.Errorf("commit replayed torrent purchase refund: %w", err)
		}
		return replay, nil
	}

	var entitlementID uuid.UUID
	var torrentTitle, policyRevision string
	var refundAmount int64
	err = tx.QueryRow(ctx, `
SELECT entitlement.id, torrent.title, entitlement.price, entitlement.policy_revision
FROM economy.torrent_purchase_entitlements AS entitlement
JOIN torrents.torrents AS torrent ON torrent.id = entitlement.torrent_id
LEFT JOIN economy.torrent_purchase_refunds AS refund ON refund.entitlement_id = entitlement.id
WHERE entitlement.user_id = $1
  AND entitlement.torrent_id = $2
  AND refund.id IS NULL
ORDER BY entitlement.purchase_sequence DESC
LIMIT 1
FOR UPDATE OF entitlement`, buyerID, command.TorrentID).Scan(
		&entitlementID, &torrentTitle, &refundAmount, &policyRevision,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		var everPurchased bool
		if existsErr := tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM economy.torrent_purchase_entitlements
    WHERE user_id = $1 AND torrent_id = $2
)`, buyerID, command.TorrentID).Scan(&everPurchased); existsErr != nil {
			return RefundReceipt{}, fmt.Errorf("check previous torrent purchase: %w", existsErr)
		}
		if everPurchased {
			return RefundReceipt{}, ErrAlreadyRefunded
		}
		return RefundReceipt{}, ErrNotFound
	}
	if err != nil {
		return RefundReceipt{}, fmt.Errorf("lock torrent purchase entitlement: %w", err)
	}

	payload := struct {
		RequestID      string `json:"request_id"`
		EntitlementID  string `json:"entitlement_id"`
		BuyerID        string `json:"buyer_id"`
		TorrentID      int64  `json:"torrent_id"`
		RefundAmount   int64  `json:"refund_amount"`
		PolicyRevision string `json:"purchase_policy_revision"`
		FundingAccount string `json:"funding_account"`
	}{
		RequestID: command.RequestID.String(), EntitlementID: entitlementID.String(),
		BuyerID: buyerID.String(), TorrentID: command.TorrentID, RefundAmount: refundAmount,
		PolicyRevision: policyRevision, FundingAccount: "system:sink:torrent_purchase",
	}
	canonicalPayload, err := json.Marshal(payload)
	if err != nil {
		return RefundReceipt{}, fmt.Errorf("marshal torrent purchase refund payload: %w", err)
	}
	payloadSHA := sha256.Sum256(canonicalPayload)
	var transactionID *uuid.UUID
	var balanceAfter int64
	if refundAmount > 0 {
		value := uuid.NewSHA1(refundTransactionNamespace, []byte(command.RequestID.String()))
		sourceReference := torrentTransactionSourceReference("refund", command.TorrentID, command.RequestID)
		ledger, ledgerErr := repository.economy.RecordInTransaction(ctx, tx, economy.RecordCommand{
			TransactionID: value, TransactionType: economy.TransactionRefund,
			IdempotencyKey:  "torrent-refund:" + strings.ReplaceAll(command.RequestID.String(), "-", ""),
			SourceReference: sourceReference,
			PolicyRevision:  policyRevision, PayloadSHA256: payloadSHA,
			OccurredAt: command.OccurredAt, RecordedAt: command.OccurredAt,
			Postings: []economy.PostingInput{
				{AccountID: buyerID, Amount: refundAmount},
				{AccountID: economy.TorrentPurchaseSinkID(), Amount: -refundAmount},
			},
		})
		if ledgerErr != nil {
			return RefundReceipt{}, classifyError("record torrent purchase refund ledger", ledgerErr)
		}
		var found bool
		balanceAfter, found = memberBalanceAfter(ledger, buyerID)
		if !found {
			return RefundReceipt{}, ErrInvariant
		}
		transactionID = &value
	} else if err := tx.QueryRow(ctx, `
SELECT COALESCE((SELECT balance FROM economy.magic_accounts WHERE user_id = $1), 0)::bigint`, buyerID).Scan(&balanceAfter); err != nil {
		return RefundReceipt{}, fmt.Errorf("read zero-value refund balance: %w", err)
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO economy.torrent_purchase_refunds (
    id, entitlement_id, source_kind, source_reference, reason,
    refunded_at, recorded_at, refunded_by, authorization_decision_id,
    refund_amount, buyer_balance_after, payload_sha256, magic_transaction_id
) VALUES ($1, $2, 'live_refund', $3, $4, $5, $5, $6, $7, $8, $9, $10, $11)`,
		command.RequestID, entitlementID, torrentTransactionSourceReference("refund", command.TorrentID, command.RequestID),
		command.Reason, command.OccurredAt, command.ActorID, command.AuthorizationID,
		refundAmount, balanceAfter, payloadSHA[:], transactionID,
	); err != nil {
		return RefundReceipt{}, classifyError("insert torrent purchase refund", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return RefundReceipt{}, classifyError("commit torrent purchase refund", err)
	}
	return RefundReceipt{
		RequestID: command.RequestID, BuyerNumericID: command.BuyerNumericID,
		TorrentID: command.TorrentID, TorrentTitle: torrentTitle,
		RefundAmount: refundAmount, BalanceAfter: balanceAfter,
		RefundedAt: command.OccurredAt,
	}, nil
}

func readRefundByRequest(ctx context.Context, tx pgx.Tx, command RefundCommand) (RefundReceipt, bool, error) {
	var result RefundReceipt
	var actorID uuid.UUID
	var reason string
	err := tx.QueryRow(ctx, `
SELECT
    refund.id, buyer.numeric_id, entitlement.torrent_id, torrent.title,
    refund.refund_amount, refund.buyer_balance_after, refund.refunded_at,
    refund.refunded_by, refund.reason
FROM economy.torrent_purchase_refunds AS refund
JOIN economy.torrent_purchase_entitlements AS entitlement ON entitlement.id = refund.entitlement_id
JOIN identity.users AS buyer ON buyer.id = entitlement.user_id
JOIN torrents.torrents AS torrent ON torrent.id = entitlement.torrent_id
WHERE refund.id = $1 AND refund.source_kind = 'live_refund'`, command.RequestID).Scan(
		&result.RequestID, &result.BuyerNumericID, &result.TorrentID, &result.TorrentTitle,
		&result.RefundAmount, &result.BalanceAfter, &result.RefundedAt,
		&actorID, &reason,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return RefundReceipt{}, false, nil
	}
	if err != nil {
		return RefundReceipt{}, true, fmt.Errorf("read torrent purchase refund replay: %w", err)
	}
	if result.BuyerNumericID != command.BuyerNumericID || result.TorrentID != command.TorrentID ||
		actorID != command.ActorID || reason != command.Reason {
		return RefundReceipt{}, true, ErrIdempotencyConflict
	}
	result.RefundedAt = result.RefundedAt.UTC()
	result.Replayed = true
	return result, true, nil
}

func scanPolicy(row pgx.Row) (PolicySettings, error) {
	var result PolicySettings
	if err := row.Scan(&result.Enabled, &result.TaxBasisPoints, &result.Revision, &result.EffectiveFrom); err != nil {
		return PolicySettings{}, err
	}
	result.EffectiveFrom = result.EffectiveFrom.UTC()
	if result.TaxBasisPoints < 0 || result.TaxBasisPoints > 10000 || strings.TrimSpace(result.Revision) == "" || result.EffectiveFrom.IsZero() {
		return PolicySettings{}, ErrInvariant
	}
	return result, nil
}

func readPolicyByRequest(ctx context.Context, tx pgx.Tx, command UpdatePolicyCommand) (PolicySettings, bool, error) {
	var previousRevision string
	result, err := scanPolicy(tx.QueryRow(ctx, `
SELECT enabled, tax_basis_points, revision, effective_from
FROM economy.torrent_purchase_policy_revisions
WHERE request_id = $1`, command.RequestID))
	if errors.Is(err, pgx.ErrNoRows) {
		return PolicySettings{}, false, nil
	}
	if err != nil {
		return PolicySettings{}, true, err
	}
	if err := tx.QueryRow(ctx, `
SELECT coalesce(snapshot_json::jsonb ->> 'previous_revision', '')
FROM economy.torrent_purchase_policy_revisions
WHERE request_id = $1`, command.RequestID).Scan(&previousRevision); err != nil {
		return PolicySettings{}, true, err
	}
	if result.Enabled != command.Enabled || result.TaxBasisPoints != command.TaxBasisPoints || previousRevision != command.ExpectedRevision {
		return PolicySettings{}, true, ErrIdempotencyConflict
	}
	return result, true, nil
}

func readPriceChangeByRequest(ctx context.Context, tx pgx.Tx, command UpdatePriceCommand) (PriceChange, bool, error) {
	var result PriceChange
	var expectedVersion int64
	err := tx.QueryRow(ctx, `
SELECT change.id, change.torrent_id, torrent.title, change.resulting_price,
       change.resulting_torrent_version, change.occurred_at,
       change.expected_torrent_version
FROM economy.torrent_purchase_price_changes AS change
JOIN torrents.torrents AS torrent ON torrent.id = change.torrent_id
WHERE change.id = $1`, command.RequestID).Scan(
		&result.RequestID, &result.TorrentID, &result.Title, &result.Price,
		&result.Version, &result.ChangedAt, &expectedVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PriceChange{}, false, nil
	}
	if err != nil {
		return PriceChange{}, true, err
	}
	if result.TorrentID != command.TorrentID || result.Price != command.Price || expectedVersion != command.ExpectedVersion {
		return PriceChange{}, true, ErrIdempotencyConflict
	}
	result.ChangedAt = result.ChangedAt.UTC()
	result.Replayed = true
	return result, true, nil
}

func classifyAdministrationError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return ErrIdempotencyConflict
		case "23503", "23514", "P0001":
			return fmt.Errorf("%w: %s", ErrInvariant, operation)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readStatus(ctx context.Context, db queryRower, userID uuid.UUID, torrentID int64, now time.Time) (Status, error) {
	return scanStatus(db.QueryRow(ctx, statusSQL(false), torrentID, userID, now))
}

func readStatusForUpdate(ctx context.Context, tx pgx.Tx, userID uuid.UUID, torrentID int64, now time.Time) (Status, error) {
	return scanStatus(tx.QueryRow(ctx, statusSQL(true), torrentID, userID, now))
}

func statusSQL(forUpdate bool) string {
	suffix := ""
	if forUpdate {
		suffix = " FOR UPDATE OF torrent"
	}
	return `
SELECT
    torrent.id, torrent.title, torrent.uploader_id, torrent.purchase_price,
    (torrent.uploader_id = $2)::boolean AS is_uploader,
    COALESCE(account.balance, 0)::bigint,
    policy.revision, policy.enabled, policy.tax_basis_points,
    entitlement.purchased_at, entitlement.source_kind
FROM torrents.torrents AS torrent
LEFT JOIN economy.magic_accounts AS account ON account.user_id = $2
LEFT JOIN LATERAL (
    SELECT revision, enabled, tax_basis_points
    FROM economy.torrent_purchase_policy_revisions
    WHERE effective_from <= $3
    ORDER BY effective_from DESC, revision DESC
    LIMIT 1
) AS policy ON true
LEFT JOIN LATERAL (
    SELECT purchase.purchased_at, purchase.source_kind
    FROM economy.torrent_purchase_entitlements AS purchase
    LEFT JOIN economy.torrent_purchase_refunds AS refund
      ON refund.entitlement_id = purchase.id
    WHERE purchase.user_id = $2
      AND purchase.torrent_id = torrent.id
      AND refund.id IS NULL
    ORDER BY purchase.purchase_sequence DESC
    LIMIT 1
) AS entitlement ON true
WHERE torrent.id = $1 AND torrent.state = 'published'` + suffix
}

func scanStatus(row pgx.Row) (Status, error) {
	var result Status
	var policyRevision pgtype.Text
	var policyEnabled pgtype.Bool
	var taxBasisPoints pgtype.Int4
	var purchasedAt pgtype.Timestamptz
	var sourceKind pgtype.Text
	var isUploader bool
	if err := row.Scan(
		&result.TorrentID, &result.Title, &result.SellerID, &result.Price,
		&isUploader, &result.MagicBalance, &policyRevision, &policyEnabled, &taxBasisPoints,
		&purchasedAt, &sourceKind,
	); errors.Is(err, pgx.ErrNoRows) {
		return Status{}, ErrNotFound
	} else if err != nil {
		return Status{}, fmt.Errorf("read torrent purchase status: %w", err)
	}
	if result.TorrentID < 1 || result.SellerID == uuid.Nil || result.Price < 0 {
		return Status{}, ErrInvariant
	}
	if policyRevision.Valid {
		result.PolicyRevision = policyRevision.String
	}
	if taxBasisPoints.Valid {
		result.Tax = roundedBasisPoints(result.Price, int64(taxBasisPoints.Int32))
		result.SellerIncome = result.Price - result.Tax
	}
	switch {
	case purchasedAt.Valid:
		value := purchasedAt.Time.UTC()
		result.PurchasedAt = &value
		result.LegacyImport = sourceKind.Valid && sourceKind.String == "legacy_import"
		result.State = AccessPurchased
	case result.Price == 0:
		result.State = AccessFree
	case isUploader:
		result.State = AccessUploader
	case !policyEnabled.Valid || !policyEnabled.Bool:
		result.State = AccessPurchaseDisabled
	default:
		result.State = AccessPurchaseRequired
	}
	return result, nil
}

func readReceiptByRequest(ctx context.Context, tx pgx.Tx, command PurchaseCommand) (Receipt, bool, error) {
	receipt, err := scanReceipt(tx.QueryRow(ctx, receiptSQL+` WHERE purchase.request_id = $1`, command.RequestID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Receipt{}, false, nil
	}
	if err != nil {
		return Receipt{}, true, err
	}
	if receipt.UserID != command.UserID || receipt.TorrentID != command.TorrentID {
		return Receipt{}, true, ErrIdempotencyConflict
	}
	return receipt, true, nil
}

func readReceiptByPair(ctx context.Context, tx pgx.Tx, userID uuid.UUID, torrentID int64) (Receipt, error) {
	return scanReceipt(tx.QueryRow(ctx, receiptSQL+`
LEFT JOIN economy.torrent_purchase_refunds AS refund ON refund.entitlement_id = purchase.id
WHERE purchase.user_id = $1 AND purchase.torrent_id = $2 AND refund.id IS NULL
ORDER BY purchase.purchase_sequence DESC
LIMIT 1`, userID, torrentID))
}

const receiptSQL = `
SELECT
    purchase.id, purchase.request_id, purchase.user_id, purchase.torrent_id,
    purchase.seller_id, purchase.price, purchase.tax, purchase.seller_income,
    COALESCE(member_posting.balance_after, account.balance, 0)::bigint,
    purchase.policy_revision, purchase.magic_transaction_id, purchase.purchased_at
FROM economy.torrent_purchase_entitlements AS purchase
LEFT JOIN economy.magic_postings AS member_posting
  ON member_posting.transaction_id = purchase.magic_transaction_id
 AND member_posting.account_id = purchase.user_id
LEFT JOIN economy.magic_accounts AS account ON account.user_id = purchase.user_id`

func scanReceipt(row pgx.Row) (Receipt, error) {
	var result Receipt
	var requestID pgtype.UUID
	var transactionID pgtype.UUID
	if err := row.Scan(
		&result.EntitlementID, &requestID, &result.UserID, &result.TorrentID,
		&result.SellerID, &result.Price, &result.Tax, &result.SellerIncome,
		&result.BalanceAfter, &result.PolicyRevision, &transactionID, &result.PurchasedAt,
	); err != nil {
		return Receipt{}, err
	}
	if requestID.Valid {
		result.RequestID = uuid.UUID(requestID.Bytes)
	}
	if transactionID.Valid {
		result.MagicTransactionID = uuid.UUID(transactionID.Bytes)
	}
	return result, nil
}

func roundedBasisPoints(amount, basisPoints int64) int64 {
	if amount <= 0 || basisPoints <= 0 {
		return 0
	}
	return (amount*basisPoints + 5000) / 10000
}

func memberBalanceAfter(transaction economy.Transaction, userID uuid.UUID) (int64, bool) {
	for _, posting := range transaction.Postings {
		if posting.AccountID == userID {
			return posting.BalanceAfter, true
		}
	}
	return 0, false
}

func torrentTransactionSourceReference(kind string, torrentID int64, requestID uuid.UUID) string {
	return "torrent:" + strconv.FormatInt(torrentID, 10) + ":" + kind + ":" + strings.ReplaceAll(requestID.String(), "-", "")
}

func isTorrentPurchaseIdempotencyConstraint(constraint string) bool {
	switch constraint {
	case "magic_transactions_pkey",
		"magic_transactions_idempotency_key_key",
		"torrent_purchase_entitlements_pkey",
		"torrent_purchase_entitlements_request_id_key",
		"torrent_purchase_entitlements_magic_transaction_id_key",
		"torrent_purchase_refunds_pkey",
		"torrent_purchase_refunds_magic_transaction_id_key":
		return true
	default:
		return false
	}
}

func classifyError(operation string, err error) error {
	if errors.Is(err, economy.ErrInsufficientBalance) || errors.Is(err, economy.ErrIdempotencyConflict) ||
		errors.Is(err, economy.ErrInvariant) || errors.Is(err, economy.ErrAccountNotFound) {
		return err
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			// A unique violation is not automatically an idempotency conflict.
			// Mapping unrelated statement constraints to a client retry error hid
			// the repeated-seller-income defect that this boundary now prevents.
			if isTorrentPurchaseIdempotencyConstraint(postgresError.ConstraintName) {
				return ErrIdempotencyConflict
			}
			return fmt.Errorf("%s: %w", operation, err)
		case "23503", "23514", "P0001":
			return fmt.Errorf("%w: %s", ErrInvariant, operation)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ Repository = (*PostgresRepository)(nil)
var _ HistoryRepository = (*PostgresRepository)(nil)
var _ AdministrationRepository = (*PostgresRepository)(nil)
