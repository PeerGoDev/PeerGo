package contenttip

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
	tipNamespace     = uuid.MustParse("436fda6b-4cb4-55d4-9409-c8ef796555ef")
	tipTransactionNS = uuid.MustParse("9d11d35f-8640-50f2-bf1e-89a64fd523ef")
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
		return Overview{}, fmt.Errorf("begin content tip overview: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	policy, found, err := resolvePolicy(ctx, tx)
	if err != nil {
		return Overview{}, err
	}
	if !found {
		return Overview{}, ErrPolicyNotFound
	}
	result := Overview{Policy: policy, History: make([]Tip, 0, limit)}
	if err := tx.QueryRow(ctx, `
SELECT COALESCE(sum(gross_amount), 0)::bigint
FROM economy.content_tips
WHERE tipper_user_id = $1 AND occurred_at >= $2 AND occurred_at < $3`, userID, dayStart, dayEnd).Scan(&result.OutgoingToday); err != nil {
		return Overview{}, fmt.Errorf("sum content tips today: %w", err)
	}
	result.RemainingToday = policy.Policy.DailyGrossLimit - result.OutgoingToday
	if result.RemainingToday < 0 {
		result.RemainingToday = 0
	}
	rows, err := tx.Query(ctx, tipSelectSQL+`
WHERE tip.tipper_user_id = $1 OR tip.recipient_user_id = $1
ORDER BY tip.occurred_at DESC, tip.id DESC
LIMIT $2`, userID, limit)
	if err != nil {
		return Overview{}, fmt.Errorf("list content tip history: %w", err)
	}
	for rows.Next() {
		tip, err := scanTip(rows)
		if err != nil {
			rows.Close()
			return Overview{}, err
		}
		result.History = append(result.History, tip)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Overview{}, fmt.Errorf("finish content tip history: %w", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return Overview{}, fmt.Errorf("commit content tip overview: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) Create(ctx context.Context, command CreateCommand) (Tip, error) {
	command.Now = canonicalTime(command.Now)
	command.DayStartsAt = canonicalTime(command.DayStartsAt)
	command.DayEndsAt = canonicalTime(command.DayEndsAt)
	if command.RequestID == uuid.Nil || command.TipperID == uuid.Nil || !command.Target.validReference() || command.Amount < 1 ||
		command.Now.IsZero() || command.DayStartsAt.IsZero() || !command.DayEndsAt.After(command.DayStartsAt) ||
		command.Now.Before(command.DayStartsAt) || !command.Now.Before(command.DayEndsAt) {
		return Tip{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Tip{}, fmt.Errorf("begin content tip: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "peergo-content-tip:"+command.TipperID.String()); err != nil {
		return Tip{}, fmt.Errorf("lock content tip sender: %w", err)
	}
	if replay, found, err := readTipByRequest(ctx, tx, command.TipperID, command.RequestID); found || err != nil {
		if err != nil {
			return Tip{}, err
		}
		if replay.TipperUserID != command.TipperID || !sameTarget(replay.Target, command.Target) || replay.GrossAmount != command.Amount {
			return Tip{}, ErrIdempotencyConflict
		}
		replay.Replayed = true
		if err := tx.Commit(ctx); err != nil {
			return Tip{}, classifyDatabaseError("commit content tip replay", err)
		}
		return replay, nil
	}
	policy, found, err := resolvePolicy(ctx, tx)
	if err != nil {
		return Tip{}, err
	}
	if !found {
		return Tip{}, ErrPolicyNotFound
	}
	if !policy.Policy.Enabled {
		return Tip{}, ErrDisabled
	}
	if command.Amount < policy.Policy.MinimumAmount || command.Amount > policy.Policy.MaximumAmount {
		return Tip{}, ErrAmountOutOfRange
	}
	fee, err := FeeFor(command.Amount, policy.Policy.FeeBPS)
	if err != nil {
		return Tip{}, err
	}
	target, recipientID, internalTargetID, err := resolveTarget(ctx, tx, command.Target)
	if err != nil {
		return Tip{}, err
	}
	members, err := resolveMembers(ctx, tx, command.TipperID, recipientID, command.Now)
	if err != nil {
		return Tip{}, err
	}
	if members.tipper.ID == members.recipient.ID {
		return Tip{}, ErrSelf
	}
	var outgoingToday int64
	if err := tx.QueryRow(ctx, `
SELECT COALESCE(sum(gross_amount), 0)::bigint
FROM economy.content_tips
WHERE tipper_user_id = $1 AND occurred_at >= $2 AND occurred_at < $3`, command.TipperID, command.DayStartsAt, command.DayEndsAt).Scan(&outgoingToday); err != nil {
		return Tip{}, fmt.Errorf("sum content tip daily spend: %w", err)
	}
	if outgoingToday > policy.Policy.DailyGrossLimit-command.Amount {
		return Tip{}, ErrDailyLimit
	}

	tipID := uuid.NewSHA1(tipNamespace, command.RequestID[:])
	transactionID := uuid.NewSHA1(tipTransactionNS, command.RequestID[:])
	net := command.Amount - fee
	payload := tipPayload{
		TipID: tipID.String(), RequestID: command.RequestID.String(),
		TipperUserID: command.TipperID.String(), RecipientUserID: recipientID.String(),
		TargetKind: string(target.Kind), TargetKey: targetKey(target), TargetTitle: target.Title,
		GrossAmount: command.Amount, FeeAmount: fee, NetAmount: net,
		PolicyRevision: policy.Policy.Revision, OccurredAt: command.Now.Format(time.RFC3339Nano),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Tip{}, ErrInvariant
	}
	payloadSHA256 := sha256.Sum256(encoded)
	postings := []economy.PostingInput{
		{AccountID: command.TipperID, Amount: -command.Amount},
		{AccountID: recipientID, Amount: net},
	}
	if fee > 0 {
		postings = append(postings, economy.PostingInput{AccountID: economy.FeeSinkAccountID(), Amount: fee})
	}
	transaction, err := repository.economy.RecordInTransaction(ctx, tx, economy.RecordCommand{
		TransactionID: transactionID, TransactionType: economy.TransactionTip,
		IdempotencyKey:  "content-tip:" + command.RequestID.String(),
		SourceReference: "content-tip:" + tipID.String(), PolicyRevision: policy.Policy.Revision,
		PayloadSHA256: payloadSHA256, OccurredAt: command.Now, RecordedAt: command.Now, Postings: postings,
	})
	if errors.Is(err, economy.ErrInsufficientBalance) {
		return Tip{}, ErrInsufficientBalance
	}
	if errors.Is(err, economy.ErrIdempotencyConflict) {
		return Tip{}, ErrIdempotencyConflict
	}
	if err != nil {
		return Tip{}, fmt.Errorf("record content tip ledger: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO economy.content_tips (
    id, request_id, tipper_user_id, recipient_user_id, target_kind, target_title,
    gross_amount, fee_amount, net_amount, policy_revision, payload_sha256,
    magic_transaction_id, occurred_at, recorded_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13)`,
		tipID, command.RequestID, command.TipperID, recipientID, target.Kind, target.Title,
		command.Amount, fee, net, policy.Policy.Revision, payloadSHA256[:], transaction.ID, command.Now,
	); err != nil {
		return Tip{}, classifyDatabaseError("insert content tip receipt", err)
	}
	if err := insertTargetBinding(ctx, tx, tipID, target.Kind, internalTargetID); err != nil {
		return Tip{}, err
	}
	result := Tip{
		ID: tipID, RequestID: command.RequestID, TipperUserID: command.TipperID,
		RecipientUserID: recipientID, Direction: DirectionSent,
		Counterparty: members.recipient.Counterparty, Target: target,
		GrossAmount: command.Amount, FeeAmount: fee, NetAmount: net,
		PolicyRevision: policy.Policy.Revision, MagicTransactionID: transaction.ID,
		OccurredAt: command.Now, RecordedAt: command.Now,
	}
	if err := tx.Commit(ctx); err != nil {
		return Tip{}, classifyDatabaseError("commit content tip", err)
	}
	return result, nil
}

type targetRecord struct {
	Target
	RecipientID uuid.UUID
	InternalID  int64
}

func resolveTarget(ctx context.Context, tx pgx.Tx, requested Target) (Target, uuid.UUID, int64, error) {
	var result targetRecord
	switch requested.Kind {
	case TargetTorrent:
		err := tx.QueryRow(ctx, `
SELECT id, uploader_id, title
FROM torrents.torrents
WHERE id = $1 AND state = 'published'
FOR SHARE`, requested.TorrentID).Scan(&result.InternalID, &result.RecipientID, &result.Title)
		if errors.Is(err, pgx.ErrNoRows) {
			return Target{}, uuid.Nil, 0, ErrTargetUnavailable
		}
		if err != nil {
			return Target{}, uuid.Nil, 0, fmt.Errorf("resolve torrent tip target: %w", err)
		}
		result.Kind, result.TorrentID = TargetTorrent, result.InternalID
	case TargetPost:
		err := tx.QueryRow(ctx, `
SELECT id, public_id, author_id, left(regexp_replace(body, '\s+', ' ', 'g'), 240)
FROM social.posts
WHERE public_id = $1 AND state = 'visible'
FOR SHARE`, requested.PostID).Scan(&result.InternalID, &result.PostID, &result.RecipientID, &result.Title)
		if errors.Is(err, pgx.ErrNoRows) {
			return Target{}, uuid.Nil, 0, ErrTargetUnavailable
		}
		if err != nil {
			return Target{}, uuid.Nil, 0, fmt.Errorf("resolve post tip target: %w", err)
		}
		result.Kind = TargetPost
	case TargetComment:
		err := tx.QueryRow(ctx, `
SELECT comment.id, comment.public_id, comment.author_id,
       left(regexp_replace(comment.body, '\s+', ' ', 'g'), 240)
FROM social.comments AS comment
JOIN social.comment_target_projection AS target
  ON target.thread_id = comment.thread_id AND target.target_is_public
WHERE comment.public_id = $1 AND comment.state = 'visible'
FOR SHARE OF comment`, requested.CommentID).Scan(&result.InternalID, &result.CommentID, &result.RecipientID, &result.Title)
		if errors.Is(err, pgx.ErrNoRows) {
			return Target{}, uuid.Nil, 0, ErrTargetUnavailable
		}
		if err != nil {
			return Target{}, uuid.Nil, 0, fmt.Errorf("resolve comment tip target: %w", err)
		}
		result.Kind = TargetComment
	default:
		return Target{}, uuid.Nil, 0, ErrInput
	}
	result.Title = strings.TrimSpace(result.Title)
	if result.RecipientID == uuid.Nil || result.InternalID < 1 || result.Title == "" {
		return Target{}, uuid.Nil, 0, ErrInvariant
	}
	return result.Target, result.RecipientID, result.InternalID, nil
}

type memberRecord struct {
	ID uuid.UUID
	Counterparty
}

type memberPair struct{ tipper, recipient memberRecord }

func resolveMembers(ctx context.Context, tx pgx.Tx, tipperID, recipientID uuid.UUID, now time.Time) (memberPair, error) {
	rows, err := tx.Query(ctx, `
SELECT users.id, users.numeric_id, users.username, users.display_name
FROM identity.users AS users
WHERE users.id = ANY($1::uuid[])
  AND users.status = 'active'
  AND users.email_verified_at IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM identity.account_restrictions AS restriction
      WHERE restriction.user_id = users.id
        AND restriction.kind = 'account_access'
        AND restriction.revoked_at IS NULL
        AND restriction.starts_at <= $2 AND restriction.expires_at > $2
  )
ORDER BY users.id FOR SHARE`, []uuid.UUID{tipperID, recipientID}, now)
	if err != nil {
		return memberPair{}, fmt.Errorf("resolve content tip participants: %w", err)
	}
	defer rows.Close()
	var result memberPair
	for rows.Next() {
		var member memberRecord
		if err := rows.Scan(&member.ID, &member.NumericID, &member.Username, &member.DisplayName); err != nil {
			return memberPair{}, fmt.Errorf("scan content tip participant: %w", err)
		}
		if member.ID == tipperID {
			result.tipper = member
		}
		if member.ID == recipientID {
			result.recipient = member
		}
	}
	if err := rows.Err(); err != nil {
		return memberPair{}, fmt.Errorf("finish content tip participants: %w", err)
	}
	if result.tipper.ID == uuid.Nil {
		return memberPair{}, ErrTipperIneligible
	}
	if result.recipient.ID == uuid.Nil {
		return memberPair{}, ErrRecipientUnavailable
	}
	return result, nil
}

func insertTargetBinding(ctx context.Context, tx pgx.Tx, tipID uuid.UUID, kind TargetKind, internalID int64) error {
	var statement string
	switch kind {
	case TargetTorrent:
		statement = `INSERT INTO economy.torrent_content_tips (content_tip_id, torrent_id) VALUES ($1,$2)`
	case TargetPost:
		statement = `INSERT INTO economy.post_content_tips (content_tip_id, post_id) VALUES ($1,$2)`
	case TargetComment:
		statement = `INSERT INTO economy.comment_content_tips (content_tip_id, comment_id) VALUES ($1,$2)`
	default:
		return ErrInput
	}
	if _, err := tx.Exec(ctx, statement, tipID, internalID); err != nil {
		return classifyDatabaseError("bind content tip target", err)
	}
	return nil
}

type tipPayload struct {
	TipID           string `json:"tip_id"`
	RequestID       string `json:"request_id"`
	TipperUserID    string `json:"tipper_user_id"`
	RecipientUserID string `json:"recipient_user_id"`
	TargetKind      string `json:"target_kind"`
	TargetKey       string `json:"target_key"`
	TargetTitle     string `json:"target_title"`
	GrossAmount     int64  `json:"gross_amount"`
	FeeAmount       int64  `json:"fee_amount"`
	NetAmount       int64  `json:"net_amount"`
	PolicyRevision  string `json:"policy_revision"`
	OccurredAt      string `json:"occurred_at"`
}

const tipSelectSQL = `
SELECT
    tip.id, tip.request_id, tip.tipper_user_id, tip.recipient_user_id,
    CASE WHEN tip.tipper_user_id = $1 THEN 'sent' ELSE 'received' END,
    counterparty.numeric_id, counterparty.username, counterparty.display_name,
    tip.target_kind, torrent_tip.torrent_id, post.public_id, comment.public_id,
    tip.target_title, tip.gross_amount, tip.fee_amount, tip.net_amount,
    tip.policy_revision, tip.magic_transaction_id, tip.occurred_at, tip.recorded_at
FROM economy.content_tips AS tip
JOIN identity.users AS counterparty
  ON counterparty.id = CASE WHEN tip.tipper_user_id = $1
      THEN tip.recipient_user_id ELSE tip.tipper_user_id END
LEFT JOIN economy.torrent_content_tips AS torrent_tip ON torrent_tip.content_tip_id = tip.id
LEFT JOIN economy.post_content_tips AS post_tip ON post_tip.content_tip_id = tip.id
LEFT JOIN social.posts AS post ON post.id = post_tip.post_id
LEFT JOIN economy.comment_content_tips AS comment_tip ON comment_tip.content_tip_id = tip.id
LEFT JOIN social.comments AS comment ON comment.id = comment_tip.comment_id
`

func readTipByRequest(ctx context.Context, tx pgx.Tx, tipperID, requestID uuid.UUID) (Tip, bool, error) {
	row := tx.QueryRow(ctx, tipSelectSQL+`WHERE tip.request_id = $2`, tipperID, requestID)
	tip, err := scanTip(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tip{}, false, nil
	}
	if err != nil {
		return Tip{}, true, err
	}
	return tip, true, nil
}

type tipScanner interface{ Scan(...any) error }

func scanTip(scanner tipScanner) (Tip, error) {
	var tip Tip
	var direction, kind string
	var torrentID pgtype.Int8
	var postID, commentID pgtype.UUID
	if err := scanner.Scan(
		&tip.ID, &tip.RequestID, &tip.TipperUserID, &tip.RecipientUserID, &direction,
		&tip.Counterparty.NumericID, &tip.Counterparty.Username, &tip.Counterparty.DisplayName,
		&kind, &torrentID, &postID, &commentID, &tip.Target.Title,
		&tip.GrossAmount, &tip.FeeAmount, &tip.NetAmount, &tip.PolicyRevision,
		&tip.MagicTransactionID, &tip.OccurredAt, &tip.RecordedAt,
	); err != nil {
		return Tip{}, err
	}
	tip.Direction, tip.Target.Kind = Direction(direction), TargetKind(kind)
	if torrentID.Valid {
		tip.Target.TorrentID = torrentID.Int64
	}
	if postID.Valid {
		tip.Target.PostID = uuid.UUID(postID.Bytes)
	}
	if commentID.Valid {
		tip.Target.CommentID = uuid.UUID(commentID.Bytes)
	}
	tip.OccurredAt, tip.RecordedAt = canonicalTime(tip.OccurredAt), canonicalTime(tip.RecordedAt)
	if tip.ID == uuid.Nil || tip.RequestID == uuid.Nil || tip.TipperUserID == uuid.Nil || tip.RecipientUserID == uuid.Nil ||
		tip.Counterparty.NumericID < 1 || (tip.Direction != DirectionSent && tip.Direction != DirectionReceived) ||
		!tip.Target.validReference() || strings.TrimSpace(tip.Target.Title) == "" || tip.GrossAmount < 1 || tip.FeeAmount < 0 ||
		tip.NetAmount != tip.GrossAmount-tip.FeeAmount || tip.NetAmount < 1 {
		return Tip{}, ErrInvariant
	}
	return tip, nil
}

func sameTarget(left, right Target) bool {
	return left.Kind == right.Kind && left.TorrentID == right.TorrentID && left.PostID == right.PostID && left.CommentID == right.CommentID
}

func targetKey(target Target) string {
	switch target.Kind {
	case TargetTorrent:
		return fmt.Sprintf("%d", target.TorrentID)
	case TargetPost:
		return target.PostID.String()
	case TargetComment:
		return target.CommentID.String()
	default:
		return ""
	}
}

func (repository *PostgresRepository) ListPolicies(ctx context.Context, limit, offset int) ([]PublishedPolicy, int64, error) {
	if limit < 1 || limit > MaximumPolicyLimit || offset < 0 || offset > 1_000_000 {
		return nil, 0, ErrInput
	}
	rows, err := repository.pool.Query(ctx, `
SELECT revision, enabled, minimum_amount, maximum_amount, daily_gross_limit,
       fee_bps, snapshot_sha256, issued_by, authorization_decision_id,
       reason, created_at, count(*) OVER ()::bigint
FROM economy.content_tip_policy_revisions
ORDER BY created_at DESC, revision DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list content tip policies: %w", err)
	}
	defer rows.Close()
	items := make([]PublishedPolicy, 0, limit)
	var total int64
	for rows.Next() {
		item, count, err := scanPolicyWithCount(rows)
		if err != nil {
			return nil, 0, err
		}
		items, total = append(items, item), count
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("finish content tip policies: %w", err)
	}
	if len(items) == 0 && offset > 0 {
		if err := repository.pool.QueryRow(ctx, `SELECT count(*)::bigint FROM economy.content_tip_policy_revisions`).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("count content tip policies: %w", err)
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
INSERT INTO economy.content_tip_policy_revisions (
    revision, enabled, minimum_amount, maximum_amount, daily_gross_limit,
    fee_bps, snapshot_json, snapshot_sha256, issued_by,
    authorization_decision_id, reason, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		policy.Revision, policy.Enabled, policy.MinimumAmount, policy.MaximumAmount,
		policy.DailyGrossLimit, policy.FeeBPS, string(snapshot), policy.SnapshotSHA256[:],
		command.IssuedBy, command.AuthorizationDecisionID, command.Reason, policy.CreatedAt)
	if err == nil {
		issuedBy, decisionID := command.IssuedBy, command.AuthorizationDecisionID
		return PublishedPolicy{Policy: policy, IssuedBy: &issuedBy, AuthorizationDecisionID: &decisionID, Reason: command.Reason}, nil
	}
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "23505" {
		return PublishedPolicy{}, classifyDatabaseError("publish content tip policy", err)
	}
	existing, found, readErr := readPolicyByRevision(ctx, repository.pool, policy.Revision)
	if readErr != nil {
		return PublishedPolicy{}, readErr
	}
	if !found || existing.IssuedBy == nil || existing.AuthorizationDecisionID == nil || existing.Policy != policy ||
		*existing.IssuedBy != command.IssuedBy || *existing.AuthorizationDecisionID != command.AuthorizationDecisionID || existing.Reason != command.Reason {
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
       fee_bps, snapshot_sha256, issued_by, authorization_decision_id, reason, created_at
FROM economy.content_tip_policy_revisions ORDER BY created_at DESC, revision DESC LIMIT 1`))
}

func readPolicyByRevision(ctx context.Context, db queryRower, revision string) (PublishedPolicy, bool, error) {
	return scanPolicyRow(db.QueryRow(ctx, `
SELECT revision, enabled, minimum_amount, maximum_amount, daily_gross_limit,
       fee_bps, snapshot_sha256, issued_by, authorization_decision_id, reason, created_at
FROM economy.content_tip_policy_revisions WHERE revision = $1`, revision))
}

func scanPolicyRow(row pgx.Row) (PublishedPolicy, bool, error) {
	var item PublishedPolicy
	var digest []byte
	var issuedBy, decisionID pgtype.UUID
	err := row.Scan(&item.Policy.Revision, &item.Policy.Enabled, &item.Policy.MinimumAmount,
		&item.Policy.MaximumAmount, &item.Policy.DailyGrossLimit, &item.Policy.FeeBPS,
		&digest, &issuedBy, &decisionID, &item.Reason, &item.Policy.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return PublishedPolicy{}, false, nil
	}
	if err != nil {
		return PublishedPolicy{}, true, fmt.Errorf("scan content tip policy: %w", err)
	}
	if err := finishPolicy(&item, digest, issuedBy, decisionID); err != nil {
		return PublishedPolicy{}, true, err
	}
	return item, true, nil
}

type policyCountScanner interface{ Scan(...any) error }

func scanPolicyWithCount(scanner policyCountScanner) (PublishedPolicy, int64, error) {
	var item PublishedPolicy
	var digest []byte
	var issuedBy, decisionID pgtype.UUID
	var count int64
	if err := scanner.Scan(&item.Policy.Revision, &item.Policy.Enabled, &item.Policy.MinimumAmount,
		&item.Policy.MaximumAmount, &item.Policy.DailyGrossLimit, &item.Policy.FeeBPS,
		&digest, &issuedBy, &decisionID, &item.Reason, &item.Policy.CreatedAt, &count); err != nil {
		return PublishedPolicy{}, 0, fmt.Errorf("scan content tip policy page: %w", err)
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
	limitsValid := revisionPattern.MatchString(item.Policy.Revision) && !item.Policy.CreatedAt.IsZero()
	if (item.IssuedBy == nil) != (item.AuthorizationDecisionID == nil) || !limitsValid {
		return ErrInvariant
	}
	if _, _, err := NormalizePolicy(item.Policy); err != nil {
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
