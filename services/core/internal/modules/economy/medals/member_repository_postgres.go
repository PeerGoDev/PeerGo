package medals

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
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/peergo/peergo/services/core/internal/modules/economy"
)

var (
	medalPurchaseNamespace            = uuid.MustParse("41a79cb4-b081-5ac3-9809-5b1d97d502e0")
	medalPurchaseTransactionNamespace = uuid.MustParse("f27f146f-7103-557d-a816-b903605beab2")
)

func (repository *PostgresRepository) MemberOverview(ctx context.Context, userID uuid.UUID, now time.Time) (MemberOverview, error) {
	if userID == uuid.Nil || now.IsZero() {
		return MemberOverview{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return MemberOverview{}, fmt.Errorf("begin member medal overview: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	settings, err := readSettings(ctx, tx)
	if err != nil {
		return MemberOverview{}, err
	}
	result := MemberOverview{Settings: settings, Items: []MemberMedal{}}
	if err := tx.QueryRow(ctx, `
SELECT COALESCE((SELECT balance FROM economy.magic_accounts WHERE user_id = $1), 0)::bigint`, userID).Scan(&result.MagicBalance); err != nil {
		return MemberOverview{}, fmt.Errorf("read member medal balance: %w", err)
	}
	rows, err := tx.Query(ctx, `
SELECT
    definition.id, definition.name, definition.description,
    definition.image_large_path, definition.image_small_path,
    definition.acquisition_method, definition.price, definition.duration_days,
    definition.upload_bonus_bps, definition.download_discount_bps,
    definition.magic_bonus_bps, definition.invite_bonus,
    definition.is_workgroup, definition.sale_begin_at, definition.sale_end_at,
    definition.inventory,
    holding.id, holding.state, holding.priority, holding.expires_at,
    holding.acquired_at, holding.version
FROM economy.medal_definitions AS definition
LEFT JOIN economy.user_medals AS holding
  ON holding.medal_id = definition.id AND holding.user_id = $1
WHERE definition.display_on_page OR holding.id IS NOT NULL
ORDER BY definition.priority DESC, definition.id`, userID)
	if err != nil {
		return MemberOverview{}, fmt.Errorf("query member medals: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanMemberMedal(rows)
		if err != nil {
			return MemberOverview{}, err
		}
		activeHolding := item.Holding != nil && (item.Holding.ExpiresAt == nil || item.Holding.ExpiresAt.After(now))
		if activeHolding {
			result.OwnedCount++
			activeBenefit := item.IsWorkgroup || item.Holding.State == "wearing"
			if !item.IsWorkgroup && item.Holding.State == "wearing" {
				result.WearingCount++
			}
			if activeBenefit {
				result.Benefits.UploadBonusBPS += item.UploadBonusBPS
				result.Benefits.DownloadDiscountBPS += item.DownloadDiscountBPS
				result.Benefits.MagicBonusBPS += item.MagicBonusBPS
				result.Benefits.InviteBonus += item.InviteBonus
			}
		}
		item.Purchasable, item.PurchaseUnavailableReason = purchaseAvailability(item, settings, activeHolding, now)
		if item.AcquisitionMethod == AcquisitionPurchase {
			result.ShopCount++
		}
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return MemberOverview{}, fmt.Errorf("finish member medals: %w", err)
	}
	result.Benefits.UploadBonusBPS = minInt64(result.Benefits.UploadBonusBPS, settings.MaximumUploadBonusBPS)
	result.Benefits.DownloadDiscountBPS = minInt64(result.Benefits.DownloadDiscountBPS, settings.MaximumDownloadDiscountBPS)
	result.Benefits.MagicBonusBPS = minInt64(result.Benefits.MagicBonusBPS, settings.MaximumMagicBonusBPS)
	result.Benefits.InviteBonus = minInt64(result.Benefits.InviteBonus, settings.MaximumInviteBonus)
	if err := tx.Commit(ctx); err != nil {
		return MemberOverview{}, fmt.Errorf("commit member medal overview: %w", err)
	}
	return result, nil
}

func scanMemberMedal(row rowScanner) (MemberMedal, error) {
	var result MemberMedal
	var description, largeImage, smallImage, acquisition, state pgtype.Text
	var saleBegin, saleEnd, expiresAt, acquiredAt pgtype.Timestamptz
	var inventory, holdingID, holdingPriority, holdingVersion pgtype.Int8
	if err := row.Scan(
		&result.ID, &result.Name, &description, &largeImage, &smallImage,
		&acquisition, &result.Price, &result.DurationDays, &result.UploadBonusBPS,
		&result.DownloadDiscountBPS, &result.MagicBonusBPS, &result.InviteBonus,
		&result.IsWorkgroup, &saleBegin, &saleEnd, &inventory,
		&holdingID, &state, &holdingPriority, &expiresAt, &acquiredAt, &holdingVersion,
	); err != nil {
		return MemberMedal{}, fmt.Errorf("scan member medal: %w", err)
	}
	result.Description = textPointer(description)
	result.ImageLargePath = textPointer(largeImage)
	result.ImageSmallPath = textPointer(smallImage)
	result.AcquisitionMethod = AcquisitionMethod(acquisition.String)
	result.SaleBeginAt = timePointer(saleBegin)
	result.SaleEndAt = timePointer(saleEnd)
	if inventory.Valid {
		value := inventory.Int64
		result.Inventory = &value
	}
	if holdingID.Valid {
		holding := Holding{
			ID: holdingID.Int64, State: state.String, Priority: holdingPriority.Int64,
			ExpiresAt: timePointer(expiresAt), AcquiredAt: acquiredAt.Time.UTC(), Version: holdingVersion.Int64,
		}
		result.Holding = &holding
	}
	return result, nil
}

func purchaseAvailability(item MemberMedal, settings Settings, activeHolding bool, now time.Time) (bool, *string) {
	reason := ""
	switch {
	case !settings.Enabled:
		reason = "勋章系统暂未开放"
	case item.AcquisitionMethod != AcquisitionPurchase:
		reason = "该勋章仅可由站点授予"
	case activeHolding:
		reason = "已经拥有"
	case item.SaleBeginAt != nil && item.SaleBeginAt.After(now):
		reason = "尚未开售"
	case item.SaleEndAt != nil && !item.SaleEndAt.After(now):
		reason = "已经停售"
	case item.Inventory != nil && *item.Inventory < 1:
		reason = "库存不足"
	}
	if reason == "" {
		return true, nil
	}
	return false, &reason
}

func (repository *PostgresRepository) Purchase(ctx context.Context, command PurchaseCommand) (PurchaseReceipt, error) {
	if command.RequestID == uuid.Nil || command.UserID == uuid.Nil || command.MedalID < 1 || command.Now.IsZero() {
		return PurchaseReceipt{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return PurchaseReceipt{}, fmt.Errorf("begin medal purchase: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	lockKey := "member-medal:" + command.UserID.String()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return PurchaseReceipt{}, fmt.Errorf("lock member medals: %w", err)
	}
	if replay, found, err := readPurchaseReceipt(ctx, tx, command.RequestID, command.UserID, command.MedalID); found || err != nil {
		if err != nil {
			return PurchaseReceipt{}, err
		}
		replay.Replayed = true
		if err := tx.Commit(ctx); err != nil {
			return PurchaseReceipt{}, fmt.Errorf("commit replayed medal purchase: %w", err)
		}
		return replay, nil
	}
	settings, err := readSettings(ctx, tx)
	if err != nil {
		return PurchaseReceipt{}, err
	}
	if !settings.Enabled {
		return PurchaseReceipt{}, ErrDisabled
	}
	var price, durationDays, definitionPriority, definitionVersion int64
	var method string
	var display bool
	var saleBegin, saleEnd pgtype.Timestamptz
	var inventory pgtype.Int8
	err = tx.QueryRow(ctx, `
SELECT acquisition_method, price, duration_days, display_on_page, priority,
       sale_begin_at, sale_end_at, inventory, version
FROM economy.medal_definitions
WHERE id = $1
FOR UPDATE`, command.MedalID).Scan(
		&method, &price, &durationDays, &display, &definitionPriority,
		&saleBegin, &saleEnd, &inventory, &definitionVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PurchaseReceipt{}, ErrNotFound
	}
	if err != nil {
		return PurchaseReceipt{}, fmt.Errorf("lock medal for purchase: %w", err)
	}
	if method != string(AcquisitionPurchase) || !display ||
		(saleBegin.Valid && saleBegin.Time.After(command.Now)) ||
		(saleEnd.Valid && !saleEnd.Time.After(command.Now)) ||
		(inventory.Valid && inventory.Int64 < 1) {
		return PurchaseReceipt{}, ErrNotPurchasable
	}

	var userMedalID, holdingVersion int64
	var expiresAt pgtype.Timestamptz
	err = tx.QueryRow(ctx, `
SELECT id, expires_at, version
FROM economy.user_medals
WHERE user_id = $1 AND medal_id = $2
FOR UPDATE`, command.UserID, command.MedalID).Scan(&userMedalID, &expiresAt, &holdingVersion)
	switch {
	case err == nil && (!expiresAt.Valid || expiresAt.Time.After(command.Now)):
		return PurchaseReceipt{}, ErrAlreadyOwned
	case err == nil:
		newExpiry := purchaseExpiry(command.Now, durationDays)
		if err := tx.QueryRow(ctx, `
UPDATE economy.user_medals
SET state = 'owned', priority = $3, expires_at = $4, granted_by = NULL,
    note = NULL, acquired_at = $5, updated_at = $5, last_reward_at = NULL,
    version = version + 1
WHERE id = $1 AND version = $2
RETURNING version`, userMedalID, holdingVersion, definitionPriority, newExpiry, command.Now).Scan(&holdingVersion); err != nil {
			return PurchaseReceipt{}, fmt.Errorf("renew medal holding: %w", err)
		}
	case errors.Is(err, pgx.ErrNoRows):
		if err := tx.QueryRow(ctx, `
INSERT INTO economy.user_medals (
    user_id, medal_id, state, priority, expires_at,
    acquired_at, updated_at
) VALUES ($1, $2, 'owned', $3, $4, $5, $5)
RETURNING id, version`, command.UserID, command.MedalID, definitionPriority,
			purchaseExpiry(command.Now, durationDays), command.Now).Scan(&userMedalID, &holdingVersion); err != nil {
			return PurchaseReceipt{}, fmt.Errorf("create medal holding: %w", err)
		}
	default:
		return PurchaseReceipt{}, fmt.Errorf("read medal holding: %w", err)
	}

	var magicTransactionID *uuid.UUID
	balanceAfter := int64(0)
	if price > 0 {
		payload, err := json.Marshal(struct {
			RequestID string `json:"request_id"`
			UserID    string `json:"user_id"`
			MedalID   int64  `json:"medal_id"`
			Price     int64  `json:"price"`
			Version   int64  `json:"definition_version"`
		}{command.RequestID.String(), command.UserID.String(), command.MedalID, price, definitionVersion})
		if err != nil {
			return PurchaseReceipt{}, fmt.Errorf("marshal medal purchase: %w", err)
		}
		payloadSHA := sha256.Sum256(payload)
		transactionID := uuid.NewSHA1(medalPurchaseTransactionNamespace, command.RequestID[:])
		transaction, err := repository.economy.RecordInTransaction(ctx, tx, economy.RecordCommand{
			TransactionID: transactionID, TransactionType: economy.TransactionMedalBuy,
			IdempotencyKey:  "medal-purchase:" + strings.ReplaceAll(command.RequestID.String(), "-", ""),
			SourceReference: "medal:" + strconv.FormatInt(command.MedalID, 10),
			PolicyRevision:  "medal-definition-v" + strconv.FormatInt(definitionVersion, 10),
			PayloadSHA256:   payloadSHA, OccurredAt: command.Now, RecordedAt: command.Now,
			Postings: []economy.PostingInput{
				{AccountID: command.UserID, Amount: -price},
				{AccountID: economy.MedalPurchaseSinkID(), Amount: price},
			},
		})
		if errors.Is(err, economy.ErrInsufficientBalance) {
			return PurchaseReceipt{}, ErrInsufficientMagic
		}
		if errors.Is(err, economy.ErrIdempotencyConflict) {
			return PurchaseReceipt{}, ErrIdempotencyConflict
		}
		if err != nil {
			return PurchaseReceipt{}, fmt.Errorf("record medal purchase: %w", err)
		}
		for _, posting := range transaction.Postings {
			if posting.AccountID == command.UserID {
				balanceAfter = posting.BalanceAfter
			}
		}
		magicTransactionID = &transactionID
	} else if err := tx.QueryRow(ctx, `
SELECT COALESCE((SELECT balance FROM economy.magic_accounts WHERE user_id = $1), 0)::bigint`, command.UserID).Scan(&balanceAfter); err != nil {
		return PurchaseReceipt{}, fmt.Errorf("read medal purchase balance: %w", err)
	}
	if inventory.Valid {
		// 库存消耗属于运行时状态，不推进勋章定义版本；定义版本只由后台配置变更推进，
		// 这样定义版本与不可变修订记录始终保持一一对应，便于后续审计和回放。
		if _, err := tx.Exec(ctx, `UPDATE economy.medal_definitions SET inventory = inventory - 1, updated_at = $2 WHERE id = $1`, command.MedalID, command.Now); err != nil {
			return PurchaseReceipt{}, fmt.Errorf("decrement medal inventory: %w", err)
		}
	}
	receipt := PurchaseReceipt{
		ID: uuid.NewSHA1(medalPurchaseNamespace, command.RequestID[:]), RequestID: command.RequestID,
		MedalID: command.MedalID, UserMedalID: userMedalID, Price: price,
		BalanceAfter: balanceAfter, MagicTransactionID: magicTransactionID, PurchasedAt: command.Now,
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO economy.medal_purchases (
    id, request_id, user_id, medal_id, user_medal_id, price,
    magic_transaction_id, purchased_at, recorded_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)`,
		receipt.ID, receipt.RequestID, command.UserID, receipt.MedalID, receipt.UserMedalID,
		receipt.Price, receipt.MagicTransactionID, receipt.PurchasedAt); err != nil {
		return PurchaseReceipt{}, fmt.Errorf("insert medal purchase receipt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PurchaseReceipt{}, fmt.Errorf("commit medal purchase: %w", err)
	}
	return receipt, nil
}

func (repository *PostgresRepository) SetWearing(ctx context.Context, command WearCommand) (Holding, error) {
	if command.UserID == uuid.Nil || command.MedalID < 1 || command.ExpectedVersion < 1 || command.Now.IsZero() {
		return Holding{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Holding{}, fmt.Errorf("begin medal wear change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "member-medal:"+command.UserID.String()); err != nil {
		return Holding{}, fmt.Errorf("lock member medals: %w", err)
	}
	settings, err := readSettings(ctx, tx)
	if err != nil {
		return Holding{}, err
	}
	if !settings.Enabled {
		return Holding{}, ErrDisabled
	}
	var holding Holding
	var isWorkgroup bool
	var expires pgtype.Timestamptz
	err = tx.QueryRow(ctx, `
SELECT holding.id, holding.state, holding.priority, holding.expires_at,
       holding.acquired_at, holding.version, definition.is_workgroup
FROM economy.user_medals AS holding
JOIN economy.medal_definitions AS definition ON definition.id = holding.medal_id
WHERE holding.user_id = $1 AND holding.medal_id = $2
FOR UPDATE OF holding`, command.UserID, command.MedalID).Scan(
		&holding.ID, &holding.State, &holding.Priority, &expires,
		&holding.AcquiredAt, &holding.Version, &isWorkgroup,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Holding{}, ErrNotFound
	}
	if err != nil {
		return Holding{}, fmt.Errorf("lock medal holding: %w", err)
	}
	holding.ExpiresAt = timePointer(expires)
	if holding.Version != command.ExpectedVersion {
		return Holding{}, ErrVersionConflict
	}
	if isWorkgroup {
		return Holding{}, ErrWorkgroupManaged
	}
	if holding.ExpiresAt != nil && !holding.ExpiresAt.After(command.Now) {
		return Holding{}, ErrExpired
	}
	nextState := "owned"
	if command.Wearing {
		nextState = "wearing"
	}
	if holding.State == nextState {
		return Holding{}, ErrNoChange
	}
	if command.Wearing {
		var wearing int64
		if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM economy.user_medals AS other
JOIN economy.medal_definitions AS definition ON definition.id = other.medal_id
WHERE other.user_id = $1 AND other.state = 'wearing' AND NOT definition.is_workgroup
  AND (other.expires_at IS NULL OR other.expires_at > $2)`, command.UserID, command.Now).Scan(&wearing); err != nil {
			return Holding{}, fmt.Errorf("count wearing medals: %w", err)
		}
		if wearing >= settings.MaximumWearCount {
			return Holding{}, ErrWearLimit
		}
		if err := tx.QueryRow(ctx, `
SELECT COALESCE(max(priority), 0)::bigint + 1 FROM economy.user_medals WHERE user_id = $1`, command.UserID).Scan(&holding.Priority); err != nil {
			return Holding{}, fmt.Errorf("choose medal priority: %w", err)
		}
	}
	if err := tx.QueryRow(ctx, `
UPDATE economy.user_medals
SET state = $3, priority = $4, version = version + 1, updated_at = $5
WHERE id = $1 AND version = $2
RETURNING version`, holding.ID, command.ExpectedVersion, nextState, holding.Priority, command.Now).Scan(&holding.Version); err != nil {
		return Holding{}, ErrVersionConflict
	}
	holding.State = nextState
	if err := appendUserBenefitRevision(ctx, tx, command.UserID, command.MedalID, holding.Version, command.Now); err != nil {
		return Holding{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Holding{}, fmt.Errorf("commit medal wear change: %w", err)
	}
	return holding, nil
}

func (repository *PostgresRepository) MovePriority(ctx context.Context, command PriorityCommand) (Holding, error) {
	if command.UserID == uuid.Nil || command.MedalID < 1 || command.ExpectedVersion < 1 ||
		(command.Direction != PriorityUp && command.Direction != PriorityDown) || command.Now.IsZero() {
		return Holding{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Holding{}, fmt.Errorf("begin medal priority change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "member-medal:"+command.UserID.String()); err != nil {
		return Holding{}, fmt.Errorf("lock member medals: %w", err)
	}
	rows, err := tx.Query(ctx, `
SELECT holding.id, holding.medal_id, holding.state, holding.priority,
       holding.expires_at, holding.acquired_at, holding.version
FROM economy.user_medals AS holding
JOIN economy.medal_definitions AS definition ON definition.id = holding.medal_id
WHERE holding.user_id = $1 AND holding.state = 'wearing' AND NOT definition.is_workgroup
  AND (holding.expires_at IS NULL OR holding.expires_at > $2)
ORDER BY holding.priority DESC, holding.id
FOR UPDATE OF holding`, command.UserID, command.Now)
	if err != nil {
		return Holding{}, fmt.Errorf("lock wearing medal order: %w", err)
	}
	type orderedHolding struct {
		Holding
		MedalID int64
	}
	ordered := []orderedHolding{}
	for rows.Next() {
		var item orderedHolding
		var expiry pgtype.Timestamptz
		if err := rows.Scan(&item.ID, &item.MedalID, &item.State, &item.Priority, &expiry, &item.AcquiredAt, &item.Version); err != nil {
			rows.Close()
			return Holding{}, fmt.Errorf("scan wearing medal order: %w", err)
		}
		item.ExpiresAt = timePointer(expiry)
		ordered = append(ordered, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Holding{}, fmt.Errorf("finish wearing medal order: %w", err)
	}
	rows.Close()
	index := -1
	for candidate := range ordered {
		if ordered[candidate].MedalID == command.MedalID {
			index = candidate
			break
		}
	}
	if index < 0 {
		return Holding{}, ErrNotFound
	}
	if ordered[index].Version != command.ExpectedVersion {
		return Holding{}, ErrVersionConflict
	}
	neighbor := index - 1
	if command.Direction == PriorityDown {
		neighbor = index + 1
	}
	if neighbor < 0 || neighbor >= len(ordered) {
		return Holding{}, ErrNoChange
	}
	targetPriority, neighborPriority := ordered[index].Priority, ordered[neighbor].Priority
	if targetPriority == neighborPriority {
		if command.Direction == PriorityUp {
			neighborPriority = targetPriority + 1
		} else {
			neighborPriority = targetPriority - 1
		}
	}
	if _, err := tx.Exec(ctx, `
UPDATE economy.user_medals
SET priority = CASE id WHEN $1 THEN $4 ELSE $3 END,
    version = version + 1, updated_at = $5
WHERE id IN ($1, $2)`, ordered[index].ID, ordered[neighbor].ID,
		targetPriority, neighborPriority, command.Now); err != nil {
		return Holding{}, fmt.Errorf("swap medal priorities: %w", err)
	}
	ordered[index].Priority = neighborPriority
	ordered[index].Version++
	if err := tx.Commit(ctx); err != nil {
		return Holding{}, fmt.Errorf("commit medal priority change: %w", err)
	}
	return ordered[index].Holding, nil
}

func appendUserBenefitRevision(ctx context.Context, tx pgx.Tx, userID uuid.UUID, medalID, holdingVersion int64, occurredAt time.Time) error {
	// Keep the audit reference a Go string. PostgreSQL otherwise infers a text
	// parameter through concatenation, which pgx cannot encode from an int64.
	sourceReference := fmt.Sprintf("medal-wear:%d:v%d", medalID, holdingVersion)
	_, err := tx.Exec(ctx, `
WITH latest AS MATERIALIZED (
    SELECT revision, effective_from, vip_enabled, vip_until, medal_bonus_bps
    FROM identity.user_reward_benefit_revisions
    WHERE user_id = $1
    ORDER BY revision DESC
    LIMIT 1
), recalculated AS MATERIALIZED (
    SELECT LEAST(
        settings.maximum_magic_bonus_bps,
        COALESCE(sum(definition.magic_bonus_bps) FILTER (
            WHERE holding.id IS NOT NULL
              AND (holding.expires_at IS NULL OR holding.expires_at > $3)
              AND (definition.is_workgroup OR holding.state = 'wearing')
        ), 0)
    )::bigint AS next_bonus
    FROM economy.medal_settings AS settings
    LEFT JOIN economy.user_medals AS holding ON holding.user_id = $1
    LEFT JOIN economy.medal_definitions AS definition ON definition.id = holding.medal_id
    WHERE settings.singleton
    GROUP BY settings.maximum_magic_bonus_bps
)
INSERT INTO identity.user_reward_benefit_revisions (
    user_id, revision, effective_from, vip_enabled, vip_until,
    medal_bonus_bps, source_kind, source_reference, created_at
)
SELECT $1, latest.revision + 1,
       GREATEST($3::timestamptz, latest.effective_from + interval '1 microsecond'),
       latest.vip_enabled, latest.vip_until, recalculated.next_bonus,
       'runtime', $2, $3
FROM latest CROSS JOIN recalculated
WHERE recalculated.next_bonus <> latest.medal_bonus_bps`, userID, sourceReference, occurredAt)
	if err != nil {
		return fmt.Errorf("append member medal benefit revision: %w", err)
	}
	return nil
}

func readPurchaseReceipt(ctx context.Context, tx pgx.Tx, requestID, userID uuid.UUID, medalID int64) (PurchaseReceipt, bool, error) {
	var result PurchaseReceipt
	var magicTransaction pgtype.UUID
	var receiptUserID uuid.UUID
	err := tx.QueryRow(ctx, `
SELECT purchase.id, purchase.request_id, purchase.medal_id,
       purchase.user_medal_id, purchase.price, purchase.magic_transaction_id,
       purchase.purchased_at,
       COALESCE(account.balance, 0)::bigint,
       purchase.user_id
FROM economy.medal_purchases AS purchase
LEFT JOIN economy.magic_accounts AS account ON account.user_id = purchase.user_id
WHERE purchase.request_id = $1`, requestID).Scan(
		&result.ID, &result.RequestID, &result.MedalID, &result.UserMedalID,
		&result.Price, &magicTransaction, &result.PurchasedAt,
		&result.BalanceAfter, &receiptUserID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PurchaseReceipt{}, false, nil
	}
	if err != nil {
		return PurchaseReceipt{}, true, fmt.Errorf("read medal purchase replay: %w", err)
	}
	if result.MedalID != medalID || receiptUserID != userID {
		return PurchaseReceipt{}, true, ErrIdempotencyConflict
	}
	if magicTransaction.Valid {
		value := uuid.UUID(magicTransaction.Bytes)
		result.MagicTransactionID = &value
	}
	return result, true, nil
}

func purchaseExpiry(now time.Time, durationDays int64) *time.Time {
	if durationDays == 0 {
		return nil
	}
	value := now.Add(time.Duration(durationDays) * 24 * time.Hour)
	return &value
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

var _ MemberRepository = (*PostgresRepository)(nil)
