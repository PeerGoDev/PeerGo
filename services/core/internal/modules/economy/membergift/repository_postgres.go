package membergift

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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
	giftNamespace        = uuid.MustParse("a62ccfe7-f7ca-5902-aa46-23de2fcd7774")
	transactionNamespace = uuid.MustParse("9f20ab05-3b19-5146-941d-7bdd3279b485")
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

func (repository *PostgresRepository) Overview(ctx context.Context, userID uuid.UUID, dayStart, dayEnd time.Time, limit int) (Overview, error) {
	if userID == uuid.Nil || dayStart.IsZero() || !dayEnd.After(dayStart) || limit < 1 || limit > MaximumHistoryLimit {
		return Overview{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Overview{}, fmt.Errorf("begin member gift overview: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	policy, found, err := resolvePolicy(ctx, tx)
	if err != nil {
		return Overview{}, err
	}
	if !found {
		return Overview{}, ErrPolicyNotFound
	}
	result := Overview{Policy: policy, History: make([]Gift, 0, limit)}
	if err := tx.QueryRow(ctx, `
SELECT users.numeric_id, COALESCE(sum(gift.gross_amount), 0)::bigint
FROM identity.users AS users
LEFT JOIN economy.member_gifts AS gift
  ON gift.sender_user_id = users.id
 AND gift.occurred_at >= $2
 AND gift.occurred_at < $3
WHERE users.id = $1
GROUP BY users.id, users.numeric_id`, userID, dayStart, dayEnd).Scan(&result.MyNumericID, &result.OutgoingToday); err != nil {
		return Overview{}, fmt.Errorf("sum member gifts today: %w", err)
	}
	result.RemainingToday = policy.Policy.DailyGrossLimit - result.OutgoingToday
	if result.RemainingToday < 0 {
		result.RemainingToday = 0
	}
	rows, err := tx.Query(ctx, `
SELECT
    gift.id, gift.request_id, gift.sender_user_id, gift.recipient_user_id,
    CASE WHEN gift.sender_user_id = $1 THEN 'sent' ELSE 'received' END,
    counterparty.numeric_id, counterparty.username, counterparty.display_name,
    gift.gross_amount, gift.fee_amount, gift.net_amount, gift.message,
    gift.policy_revision, gift.magic_transaction_id,
    gift.occurred_at, gift.recorded_at
FROM economy.member_gifts AS gift
JOIN identity.users AS counterparty
  ON counterparty.id = CASE
      WHEN gift.sender_user_id = $1 THEN gift.recipient_user_id
      ELSE gift.sender_user_id
  END
WHERE gift.sender_user_id = $1 OR gift.recipient_user_id = $1
ORDER BY gift.occurred_at DESC, gift.id DESC
LIMIT $2`, userID, limit)
	if err != nil {
		return Overview{}, fmt.Errorf("list member gifts: %w", err)
	}
	for rows.Next() {
		gift, err := scanGift(rows)
		if err != nil {
			rows.Close()
			return Overview{}, err
		}
		result.History = append(result.History, gift)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Overview{}, fmt.Errorf("finish member gift history: %w", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return Overview{}, fmt.Errorf("commit member gift overview: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) Create(ctx context.Context, command CreateCommand) (Gift, error) {
	command.Now = canonicalTime(command.Now)
	command.DayStartsAt = canonicalTime(command.DayStartsAt)
	command.DayEndsAt = canonicalTime(command.DayEndsAt)
	if command.RequestID == uuid.Nil || command.SenderUserID == uuid.Nil || command.RecipientNumericID < 1 ||
		command.Amount < 1 || command.Now.IsZero() || command.DayStartsAt.IsZero() ||
		!command.DayEndsAt.After(command.DayStartsAt) || command.Now.Before(command.DayStartsAt) || !command.Now.Before(command.DayEndsAt) {
		return Gift{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Gift{}, fmt.Errorf("begin member gift: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "peergo-member-gift:"+command.SenderUserID.String()); err != nil {
		return Gift{}, fmt.Errorf("lock member gift sender: %w", err)
	}
	if replay, found, err := readGiftByRequest(ctx, tx, command.RequestID); found || err != nil {
		if err != nil {
			return Gift{}, err
		}
		if replay.SenderUserID != command.SenderUserID || replay.Counterparty.NumericID != command.RecipientNumericID ||
			replay.GrossAmount != command.Amount || replay.Message != command.Message {
			return Gift{}, ErrIdempotencyConflict
		}
		replay.Replayed = true
		if err := tx.Commit(ctx); err != nil {
			return Gift{}, classifyDatabaseError("commit member gift replay", err)
		}
		return replay, nil
	}

	policy, found, err := resolvePolicy(ctx, tx)
	if err != nil {
		return Gift{}, err
	}
	if !found {
		return Gift{}, ErrPolicyNotFound
	}
	if !policy.Policy.Enabled {
		return Gift{}, ErrDisabled
	}
	if command.Amount < policy.Policy.MinimumAmount || command.Amount > policy.Policy.MaximumAmount {
		return Gift{}, ErrAmountOutOfRange
	}
	fee, err := FeeFor(command.Amount, policy.Policy.FeeBPS)
	if err != nil {
		return Gift{}, err
	}
	net := command.Amount - fee
	members, err := resolveMembers(ctx, tx, command.SenderUserID, command.RecipientNumericID, command.Now)
	if err != nil {
		return Gift{}, err
	}
	if members.sender.ID == members.recipient.ID {
		return Gift{}, ErrSelf
	}
	var outgoingToday int64
	if err := tx.QueryRow(ctx, `
SELECT COALESCE(sum(gross_amount), 0)::bigint
FROM economy.member_gifts
WHERE sender_user_id = $1 AND occurred_at >= $2 AND occurred_at < $3`, command.SenderUserID, command.DayStartsAt, command.DayEndsAt).Scan(&outgoingToday); err != nil {
		return Gift{}, fmt.Errorf("sum member gift daily spend: %w", err)
	}
	if outgoingToday > policy.Policy.DailyGrossLimit-command.Amount {
		return Gift{}, ErrDailyLimit
	}

	giftID := uuid.NewSHA1(giftNamespace, command.RequestID[:])
	transactionID := uuid.NewSHA1(transactionNamespace, command.RequestID[:])
	payload := giftPayload{
		GiftID: giftID.String(), RequestID: command.RequestID.String(),
		SenderUserID: command.SenderUserID.String(), RecipientUserID: members.recipient.ID.String(),
		RecipientNumericID: members.recipient.NumericID, GrossAmount: command.Amount,
		FeeAmount: fee, NetAmount: net, Message: command.Message,
		PolicyRevision: policy.Policy.Revision, OccurredAt: command.Now.Format(time.RFC3339Nano),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Gift{}, ErrInvariant
	}
	payloadSHA256 := sha256.Sum256(encoded)
	postings := []economy.PostingInput{
		{AccountID: command.SenderUserID, Amount: -command.Amount},
		{AccountID: members.recipient.ID, Amount: net},
	}
	if fee > 0 {
		postings = append(postings, economy.PostingInput{AccountID: economy.FeeSinkAccountID(), Amount: fee})
	}
	transaction, err := repository.economy.RecordInTransaction(ctx, tx, economy.RecordCommand{
		TransactionID: transactionID, TransactionType: economy.TransactionMemberGift,
		IdempotencyKey:  "member-gift:" + command.RequestID.String(),
		SourceReference: "member-gift:" + giftID.String(), PolicyRevision: policy.Policy.Revision,
		PayloadSHA256: payloadSHA256, OccurredAt: command.Now, RecordedAt: command.Now,
		Postings: postings,
	})
	if errors.Is(err, economy.ErrInsufficientBalance) {
		return Gift{}, ErrInsufficientBalance
	}
	if errors.Is(err, economy.ErrIdempotencyConflict) {
		return Gift{}, ErrIdempotencyConflict
	}
	if err != nil {
		return Gift{}, fmt.Errorf("record member gift ledger: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO economy.member_gifts (
    id, request_id, sender_user_id, recipient_user_id,
    gross_amount, fee_amount, net_amount, message, policy_revision,
    payload_sha256, magic_transaction_id, occurred_at, recorded_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12)`,
		giftID, command.RequestID, command.SenderUserID, members.recipient.ID,
		command.Amount, fee, net, command.Message, policy.Policy.Revision,
		payloadSHA256[:], transaction.ID, command.Now,
	); err != nil {
		return Gift{}, classifyDatabaseError("insert member gift receipt", err)
	}
	result := Gift{
		ID: giftID, RequestID: command.RequestID, SenderUserID: command.SenderUserID,
		RecipientUserID: members.recipient.ID, Direction: DirectionSent,
		Counterparty: Counterparty{NumericID: members.recipient.NumericID, Username: members.recipient.Username, DisplayName: members.recipient.DisplayName},
		GrossAmount:  command.Amount, FeeAmount: fee, NetAmount: net, Message: command.Message,
		PolicyRevision: policy.Policy.Revision, MagicTransactionID: transaction.ID,
		OccurredAt: command.Now, RecordedAt: command.Now,
	}
	if err := tx.Commit(ctx); err != nil {
		return Gift{}, classifyDatabaseError("commit member gift", err)
	}
	return result, nil
}

type memberRecord struct {
	ID          uuid.UUID
	NumericID   int64
	Username    string
	DisplayName string
}

type memberPair struct {
	sender    memberRecord
	recipient memberRecord
}

func resolveMembers(ctx context.Context, tx pgx.Tx, senderID uuid.UUID, recipientNumericID int64, now time.Time) (memberPair, error) {
	rows, err := tx.Query(ctx, `
SELECT users.id, users.numeric_id, users.username, users.display_name
FROM identity.users AS users
WHERE (users.id = $1 OR users.numeric_id = $2)
  AND users.status = 'active'
  AND users.email_verified_at IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM identity.account_restrictions AS restriction
      WHERE restriction.user_id = users.id
        AND restriction.kind = 'account_access'
        AND restriction.revoked_at IS NULL
        AND restriction.starts_at <= $3
        AND restriction.expires_at > $3
  )
ORDER BY users.id
FOR SHARE`, senderID, recipientNumericID, now)
	if err != nil {
		return memberPair{}, fmt.Errorf("resolve member gift participants: %w", err)
	}
	defer rows.Close()
	var result memberPair
	for rows.Next() {
		var member memberRecord
		if err := rows.Scan(&member.ID, &member.NumericID, &member.Username, &member.DisplayName); err != nil {
			return memberPair{}, fmt.Errorf("scan member gift participant: %w", err)
		}
		if member.ID == senderID {
			result.sender = member
		}
		if member.NumericID == recipientNumericID {
			result.recipient = member
		}
	}
	if err := rows.Err(); err != nil {
		return memberPair{}, fmt.Errorf("finish member gift participants: %w", err)
	}
	if result.sender.ID == uuid.Nil {
		return memberPair{}, ErrSenderIneligible
	}
	if result.recipient.ID == uuid.Nil {
		return memberPair{}, ErrRecipientUnavailable
	}
	return result, nil
}

type giftPayload struct {
	GiftID             string `json:"gift_id"`
	RequestID          string `json:"request_id"`
	SenderUserID       string `json:"sender_user_id"`
	RecipientUserID    string `json:"recipient_user_id"`
	RecipientNumericID int64  `json:"recipient_numeric_id"`
	GrossAmount        int64  `json:"gross_amount"`
	FeeAmount          int64  `json:"fee_amount"`
	NetAmount          int64  `json:"net_amount"`
	Message            string `json:"message"`
	PolicyRevision     string `json:"policy_revision"`
	OccurredAt         string `json:"occurred_at"`
}

func readGiftByRequest(ctx context.Context, tx pgx.Tx, requestID uuid.UUID) (Gift, bool, error) {
	row := tx.QueryRow(ctx, `
SELECT
    gift.id, gift.request_id, gift.sender_user_id, gift.recipient_user_id,
    'sent', recipient.numeric_id, recipient.username, recipient.display_name,
    gift.gross_amount, gift.fee_amount, gift.net_amount, gift.message,
    gift.policy_revision, gift.magic_transaction_id,
    gift.occurred_at, gift.recorded_at
FROM economy.member_gifts AS gift
JOIN identity.users AS recipient ON recipient.id = gift.recipient_user_id
WHERE gift.request_id = $1`, requestID)
	gift, err := scanGift(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Gift{}, false, nil
	}
	if err != nil {
		return Gift{}, true, err
	}
	return gift, true, nil
}

type giftScanner interface {
	Scan(...any) error
}

func scanGift(scanner giftScanner) (Gift, error) {
	var gift Gift
	var direction string
	if err := scanner.Scan(
		&gift.ID, &gift.RequestID, &gift.SenderUserID, &gift.RecipientUserID,
		&direction, &gift.Counterparty.NumericID, &gift.Counterparty.Username, &gift.Counterparty.DisplayName,
		&gift.GrossAmount, &gift.FeeAmount, &gift.NetAmount, &gift.Message,
		&gift.PolicyRevision, &gift.MagicTransactionID, &gift.OccurredAt, &gift.RecordedAt,
	); err != nil {
		return Gift{}, err
	}
	gift.Direction = Direction(direction)
	gift.OccurredAt = canonicalTime(gift.OccurredAt)
	gift.RecordedAt = canonicalTime(gift.RecordedAt)
	if gift.ID == uuid.Nil || gift.RequestID == uuid.Nil || gift.SenderUserID == uuid.Nil || gift.RecipientUserID == uuid.Nil ||
		gift.Counterparty.NumericID < 1 || (gift.Direction != DirectionSent && gift.Direction != DirectionReceived) ||
		gift.GrossAmount < 1 || gift.FeeAmount < 0 || gift.NetAmount != gift.GrossAmount-gift.FeeAmount || gift.NetAmount < 1 {
		return Gift{}, ErrInvariant
	}
	return gift, nil
}

func (repository *PostgresRepository) ListPolicies(ctx context.Context, limit, offset int) ([]PublishedPolicy, int64, error) {
	if limit < 1 || limit > MaximumPolicyLimit || offset < 0 || offset > 1_000_000 {
		return nil, 0, ErrInput
	}
	rows, err := repository.pool.Query(ctx, `
SELECT revision, enabled, minimum_amount, maximum_amount, daily_gross_limit,
       fee_bps, snapshot_sha256, issued_by, authorization_decision_id,
       reason, created_at, count(*) OVER ()::bigint
FROM economy.member_gift_policy_revisions
ORDER BY created_at DESC, revision DESC
LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list member gift policies: %w", err)
	}
	defer rows.Close()
	items := make([]PublishedPolicy, 0, limit)
	var total int64
	for rows.Next() {
		item, count, err := scanPolicyWithCount(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
		total = count
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("finish member gift policies: %w", err)
	}
	if len(items) == 0 && offset > 0 {
		if err := repository.pool.QueryRow(ctx, `SELECT count(*)::bigint FROM economy.member_gift_policy_revisions`).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("count member gift policies: %w", err)
		}
	}
	return items, total, nil
}

func (repository *PostgresRepository) LatestPolicy(ctx context.Context) (PublishedPolicy, error) {
	policy, found, err := resolvePolicy(ctx, repository.pool)
	if err != nil {
		return PublishedPolicy{}, err
	}
	if !found {
		return PublishedPolicy{}, ErrPolicyNotFound
	}
	return policy, nil
}

func (repository *PostgresRepository) PublishPolicy(ctx context.Context, command PublishCommand) (PublishedPolicy, error) {
	policy, snapshot, err := NormalizePolicy(command.Policy)
	if err != nil {
		return PublishedPolicy{}, err
	}
	if !bytes.Equal(snapshot, command.SnapshotJSON) || command.IssuedBy == uuid.Nil || command.AuthorizationDecisionID == uuid.Nil || strings.TrimSpace(command.Reason) != command.Reason {
		return PublishedPolicy{}, ErrInput
	}
	_, err = repository.pool.Exec(ctx, `
INSERT INTO economy.member_gift_policy_revisions (
    revision, enabled, minimum_amount, maximum_amount, daily_gross_limit,
    fee_bps, snapshot_json, snapshot_sha256, issued_by,
    authorization_decision_id, reason, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		policy.Revision, policy.Enabled, policy.MinimumAmount, policy.MaximumAmount,
		policy.DailyGrossLimit, policy.FeeBPS, string(snapshot), policy.SnapshotSHA256[:],
		command.IssuedBy, command.AuthorizationDecisionID, command.Reason, policy.CreatedAt,
	)
	if err == nil {
		issuedBy, decisionID := command.IssuedBy, command.AuthorizationDecisionID
		return PublishedPolicy{Policy: policy, IssuedBy: &issuedBy, AuthorizationDecisionID: &decisionID, Reason: command.Reason}, nil
	}
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "23505" {
		return PublishedPolicy{}, classifyDatabaseError("publish member gift policy", err)
	}
	existing, found, readErr := readPolicyByRevision(ctx, repository.pool, policy.Revision)
	if readErr != nil {
		return PublishedPolicy{}, readErr
	}
	if !found || existing.IssuedBy == nil || existing.AuthorizationDecisionID == nil ||
		existing.Policy != policy || *existing.IssuedBy != command.IssuedBy ||
		*existing.AuthorizationDecisionID != command.AuthorizationDecisionID || existing.Reason != command.Reason {
		return PublishedPolicy{}, ErrPolicyConflict
	}
	existing.Replayed = true
	return existing, nil
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func resolvePolicy(ctx context.Context, db queryRower) (PublishedPolicy, bool, error) {
	return scanPolicyRow(db.QueryRow(ctx, `
SELECT revision, enabled, minimum_amount, maximum_amount, daily_gross_limit,
       fee_bps, snapshot_sha256, issued_by, authorization_decision_id,
       reason, created_at
FROM economy.member_gift_policy_revisions
ORDER BY created_at DESC, revision DESC
LIMIT 1`))
}

func readPolicyByRevision(ctx context.Context, db queryRower, revision string) (PublishedPolicy, bool, error) {
	return scanPolicyRow(db.QueryRow(ctx, `
SELECT revision, enabled, minimum_amount, maximum_amount, daily_gross_limit,
       fee_bps, snapshot_sha256, issued_by, authorization_decision_id,
       reason, created_at
FROM economy.member_gift_policy_revisions
WHERE revision = $1`, revision))
}

func scanPolicyRow(row pgx.Row) (PublishedPolicy, bool, error) {
	var item PublishedPolicy
	var digest []byte
	var issuedBy, decisionID pgtype.UUID
	err := row.Scan(
		&item.Policy.Revision, &item.Policy.Enabled, &item.Policy.MinimumAmount,
		&item.Policy.MaximumAmount, &item.Policy.DailyGrossLimit, &item.Policy.FeeBPS,
		&digest, &issuedBy, &decisionID, &item.Reason, &item.Policy.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PublishedPolicy{}, false, nil
	}
	if err != nil {
		return PublishedPolicy{}, true, fmt.Errorf("scan member gift policy: %w", err)
	}
	if err := finishPolicy(&item, digest, issuedBy, decisionID); err != nil {
		return PublishedPolicy{}, true, err
	}
	return item, true, nil
}

type policyCountScanner interface {
	Scan(...any) error
}

func scanPolicyWithCount(scanner policyCountScanner) (PublishedPolicy, int64, error) {
	var item PublishedPolicy
	var digest []byte
	var issuedBy, decisionID pgtype.UUID
	var count int64
	if err := scanner.Scan(
		&item.Policy.Revision, &item.Policy.Enabled, &item.Policy.MinimumAmount,
		&item.Policy.MaximumAmount, &item.Policy.DailyGrossLimit, &item.Policy.FeeBPS,
		&digest, &issuedBy, &decisionID, &item.Reason, &item.Policy.CreatedAt, &count,
	); err != nil {
		return PublishedPolicy{}, 0, fmt.Errorf("scan member gift policy page: %w", err)
	}
	if err := finishPolicy(&item, digest, issuedBy, decisionID); err != nil {
		return PublishedPolicy{}, 0, err
	}
	if count < 0 {
		return PublishedPolicy{}, 0, ErrInvariant
	}
	return item, count, nil
}

func finishPolicy(item *PublishedPolicy, digest []byte, issuedBy, decisionID pgtype.UUID) error {
	if item == nil || len(digest) != sha256.Size {
		return ErrInvariant
	}
	copy(item.Policy.SnapshotSHA256[:], digest)
	item.Policy.CreatedAt = canonicalTime(item.Policy.CreatedAt)
	if issuedBy.Valid {
		value := uuid.UUID(issuedBy.Bytes)
		item.IssuedBy = &value
	}
	if decisionID.Valid {
		value := uuid.UUID(decisionID.Bytes)
		item.AuthorizationDecisionID = &value
	}
	if (item.IssuedBy == nil) != (item.AuthorizationDecisionID == nil) || !validPolicy(item.Policy) {
		return ErrInvariant
	}
	return nil
}

func classifyDatabaseError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return ErrIdempotencyConflict
		case "P0001", "23503", "23514":
			return fmt.Errorf("%w: %s: %v", ErrInvariant, operation, err)
		case "40001", "40P01":
			return fmt.Errorf("%s: retryable transaction conflict: %w", operation, err)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ Repository = (*PostgresRepository)(nil)
