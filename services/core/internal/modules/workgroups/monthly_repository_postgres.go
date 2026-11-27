package workgroups

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/economy"
)

func (repository *PostgresRepository) RetentionTargets(ctx context.Context, userID uuid.UUID, limit, offset int) ([]RetentionTarget, error) {
	if userID != uuid.Nil {
		var active bool
		if err := repository.pool.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM workgroups.memberships
    WHERE user_id = $1 AND group_kind = 'retention' AND status = 'active')`, userID).Scan(&active); err != nil {
			return nil, err
		}
		if !active {
			return nil, authz.ErrForbidden
		}
	}
	rows, err := repository.pool.Query(ctx, `
SELECT torrent.id, torrent.title, torrent.total_size_bytes, target.required_hours, claim.claimed_at
FROM workgroups.retention_targets target
JOIN torrents.torrents torrent ON torrent.id = target.torrent_id
LEFT JOIN workgroups.memberships member ON member.user_id = $1 AND member.group_kind = 'retention'
LEFT JOIN workgroups.retention_claims claim ON claim.membership_id = member.id AND claim.torrent_id = target.torrent_id
WHERE target.retired_at IS NULL AND ($1 = '00000000-0000-0000-0000-000000000000' OR torrent.state = 'published')
ORDER BY target.listed_at DESC, torrent.id DESC LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[RetentionTarget])
}

func (repository *PostgresRepository) SaveRetentionTarget(ctx context.Context, torrentID int64, hours int, retire bool, actorID, decisionID uuid.UUID, now time.Time) error {
	if retire {
		_, err := repository.pool.Exec(ctx, `
UPDATE workgroups.retention_targets
SET retired_at = $2, retired_by = $3, retirement_authorization_decision_id = $4
WHERE torrent_id = $1 AND retired_at IS NULL`, torrentID, now, actorID, decisionID)
		return err
	}
	result, err := repository.pool.Exec(ctx, `
INSERT INTO workgroups.retention_targets (torrent_id, required_hours, listed_at, listed_by, authorization_decision_id)
SELECT id, $2, $3, $4, $5 FROM torrents.torrents WHERE id = $1 AND state = 'published'
ON CONFLICT (torrent_id) DO NOTHING`, torrentID, hours, now, actorID, decisionID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 0 {
		return nil
	}
	var existingHours int
	var retiredAt *time.Time
	err = repository.pool.QueryRow(ctx, `
SELECT required_hours, retired_at FROM workgroups.retention_targets WHERE torrent_id = $1`, torrentID).Scan(&existingHours, &retiredAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrRetentionTargetNotFound
	case err != nil:
		return err
	case retiredAt != nil || existingHours != hours:
		return ErrRetentionTargetClosed
	default:
		return nil
	}
}

func (repository *PostgresRepository) ClaimRetentionTarget(ctx context.Context, torrentID int64, userID, decisionID uuid.UUID, now time.Time) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var membershipID uuid.UUID
	if err := tx.QueryRow(ctx, `
SELECT id FROM workgroups.memberships WHERE group_kind = 'retention' AND user_id = $1 AND status = 'active'
FOR SHARE`, userID).Scan(&membershipID); errors.Is(err, pgx.ErrNoRows) {
		return authz.ErrForbidden
	} else if err != nil {
		return err
	}
	var available int64
	if err := tx.QueryRow(ctx, `
SELECT target.torrent_id FROM workgroups.retention_targets target
JOIN torrents.torrents torrent ON torrent.id = target.torrent_id AND torrent.state = 'published'
WHERE target.torrent_id = $1 AND target.retired_at IS NULL AND target.listed_at <= $2
FOR SHARE OF target, torrent`, torrentID, now).Scan(&available); errors.Is(err, pgx.ErrNoRows) {
		return ErrRetentionTargetClosed
	} else if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO workgroups.retention_claims (membership_id,torrent_id,claimed_at,authorization_decision_id)
VALUES ($1,$2,$3,$4) ON CONFLICT (membership_id,torrent_id) DO NOTHING`, membershipID, torrentID, now, decisionID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (repository *PostgresRepository) RetentionProgress(ctx context.Context, userID, membershipID uuid.UUID, month, now time.Time) (RetentionProgress, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return RetentionProgress{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = tx.QueryRow(ctx, `
SELECT id,user_id FROM workgroups.memberships WHERE group_kind = 'retention'
AND CASE WHEN $1::uuid = '00000000-0000-0000-0000-000000000000' THEN user_id = $2 ELSE id = $1 END`,
		membershipID, userID).Scan(&membershipID, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return RetentionProgress{}, ErrMembershipNotFound
	}
	if err != nil {
		return RetentionProgress{}, err
	}
	return readRetentionProgress(ctx, tx, membershipID, userID, month, now)
}

// Only complete evidence contributes. On a partial boundary hour, subtract the
// maximum time that could fall outside the claim or active membership.
const retentionProgressSQL = `
WITH claims AS MATERIALIZED (
    SELECT claim.torrent_id, claim.claimed_at, target.required_hours, target.retired_at
    FROM workgroups.retention_claims claim
    JOIN workgroups.retention_targets target USING (torrent_id)
    WHERE claim.membership_id = $1 AND claim.claimed_at < $4
      AND (target.retired_at IS NULL OR target.retired_at > $3)
), transitions AS MATERIALIZED (
    SELECT occurred_at AS starts_at, to_status,
           lead(occurred_at) OVER (ORDER BY occurred_at,state_version,id) AS ends_at
    FROM workgroups.membership_transitions WHERE membership_id = $1
), evidence AS (
    SELECT item.torrent_id, item.window_start, item.active_seconds
    FROM workgroups.retention_evidence_items item JOIN claims USING (torrent_id)
    WHERE item.user_id = $2 AND item.window_start >= $3 AND item.window_start < $5
), qualified_seconds AS (
    SELECT e.torrent_id,
           sum(greatest(0,e.active_seconds - 3600 + floor(covered.seconds)::bigint))::bigint AS seconds
    FROM evidence e JOIN claims USING (torrent_id)
    JOIN workgroups.retention_evidence_windows w USING (window_start)
    JOIN LATERAL (
        SELECT COALESCE(sum(greatest(0,extract(epoch FROM
            least(w.window_end, transitions.ends_at, claims.retired_at)
            - greatest(w.window_start, transitions.starts_at, claims.claimed_at)))),0) AS seconds
        FROM transitions WHERE to_status = 'active'
          AND starts_at < w.window_end AND (ends_at IS NULL OR ends_at > w.window_start)
    ) covered ON covered.seconds > 0
    WHERE w.status = 'complete'
    GROUP BY e.torrent_id
)
SELECT torrent.id,torrent.title,torrent.total_size_bytes,claims.required_hours,claims.claimed_at,
       COALESCE(qualified_seconds.seconds,0),
       torrent.published_at IS NOT NULL AND COALESCE(qualified_seconds.seconds,0) >= claims.required_hours::bigint * 3600
FROM claims
JOIN torrents.torrents torrent ON torrent.id = claims.torrent_id
LEFT JOIN qualified_seconds USING (torrent_id)
ORDER BY torrent.id`

func readRetentionProgress(ctx context.Context, querier workgroupQuerier, membershipID, userID uuid.UUID, month, now time.Time) (RetentionProgress, error) {
	end := minTime(month.AddDate(0, 1, 0), now)
	hourEnd := end.Truncate(time.Hour)
	progress := RetentionProgress{Month: month.Format("2006-01"), ExpectedHours: int(hourEnd.Sub(month) / time.Hour)}
	if err := querier.QueryRow(ctx, `
SELECT count(*) FROM workgroups.retention_evidence_windows
WHERE window_start >= $1 AND window_start < $2 AND status = 'complete'`, month, hourEnd).Scan(&progress.CompleteHours); err != nil {
		return RetentionProgress{}, err
	}
	rows, err := querier.Query(ctx, retentionProgressSQL, membershipID, userID, month, end, hourEnd)
	if err != nil {
		return RetentionProgress{}, fmt.Errorf("read retention progress: %w", err)
	}
	defer rows.Close()
	progress.Items = make([]RetentionItem, 0)
	for rows.Next() {
		var item RetentionItem
		if err := rows.Scan(&item.TorrentID, &item.Title, &item.SizeBytes, &item.RequiredHours, &item.ClaimedAt, &item.ActiveSeconds, &item.Qualified); err != nil {
			return RetentionProgress{}, err
		}
		progress.Items = append(progress.Items, item)
		if item.Qualified {
			progress.QualifiedCount++
			progress.QualifiedBytes += min(item.SizeBytes, math.MaxInt64-progress.QualifiedBytes)
		}
	}
	return progress, rows.Err()
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func (repository *PostgresRepository) ReseedPublications(ctx context.Context, membershipID uuid.UUID, month, now time.Time, limit, offset int) (ReseedPublications, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ReseedPublications{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	end := month.AddDate(0, 1, 0)
	var page ReseedPublications
	if err := scanReseedTotals(tx.QueryRow(ctx, reseedTotalsSQL, membershipID, month, end, now), &page); err != nil {
		return page, err
	}
	rows, err := tx.Query(ctx, `
SELECT * FROM workgroups.reseed_publications($1,$2,$3,$4)
ORDER BY submitted_at DESC, torrent_id DESC LIMIT $5 OFFSET $6`, membershipID, month, end, now, limit, offset)
	if err != nil {
		return page, err
	}
	page.Items, err = pgx.CollectRows(rows, pgx.RowToStructByPos[ReseedPublication])
	return page, err
}

const reseedTotalsSQL = `
SELECT count(*) FILTER (WHERE contribution_status = 'counted'),
       count(*) FILTER (WHERE contribution_status = 'pending'), count(*)
FROM workgroups.reseed_publications($1,$2,$3,$4)`

func scanReseedTotals(row pgx.Row, page *ReseedPublications) error {
	return row.Scan(&page.Counted, &page.Pending, &page.Total)
}

func (repository *PostgresRepository) PreviewPayroll(ctx context.Context, input PayrollInput, now time.Time) (PayrollPreview, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return PayrollPreview{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	preview, _, err := repository.payrollPreview(ctx, tx, input, now)
	return preview, err
}

type payrollEvidence struct {
	Preview      PayrollPreview
	Retention    []RetentionItem
	Publications []int64
}

func (repository *PostgresRepository) payrollPreview(ctx context.Context, tx pgx.Tx, input PayrollInput, now time.Time) (PayrollPreview, []byte, error) {
	preview := PayrollPreview{Input: input, State: "below_target"}
	if err := tx.QueryRow(ctx, `
SELECT member.user_id, account.username FROM workgroups.memberships member
JOIN identity.users account ON account.id=member.user_id WHERE member.id=$1 AND member.group_kind=$2`,
		input.MembershipID, input.Group).Scan(&preview.UserID, &preview.Username); errors.Is(err, pgx.ErrNoRows) {
		return preview, nil, ErrMembershipNotFound
	} else if err != nil {
		return preview, nil, err
	}
	var originalDigest, originalSnapshot []byte
	var paidAt time.Time
	var paidTransactionID uuid.UUID
	err := tx.QueryRow(ctx, `
SELECT paid_at,magic_transaction_id,preview_digest,preview_snapshot FROM workgroups.payroll_payments
WHERE group_kind=$1 AND user_id=$2 AND period_starts_at=$3`, input.Group, preview.UserID, input.Month).
		Scan(&paidAt, &paidTransactionID, &originalDigest, &originalSnapshot)
	if err == nil {
		var stored payrollEvidence
		if err := json.Unmarshal(originalSnapshot, &stored); err != nil {
			return preview, nil, err
		}
		preview = stored.Preview
		preview.State, preview.Digest = "paid", hex.EncodeToString(originalDigest)
		preview.PaidAt, preview.TransactionID = &paidAt, &paidTransactionID
		return preview, nil, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return preview, nil, err
	}
	end := input.Month.AddDate(0, 1, 0)
	var items []RetentionItem
	var countedIDs []int64
	switch input.Group {
	case GroupRetention:
		progress, err := readRetentionProgress(ctx, tx, input.MembershipID, preview.UserID, input.Month, now)
		if err != nil {
			return preview, nil, err
		}
		items = progress.Items
		preview.Contribution, preview.Target = int64(progress.QualifiedCount), input.MinimumCount
		preview.QualifiedBytes = progress.QualifiedBytes
		preview.MissingHours = progress.ExpectedHours - progress.CompleteHours
	case GroupReseed, GroupReview:
		policy, err := repository.contributionPolicyWith(ctx, tx, input.Group, input.Month)
		if err != nil {
			return preview, nil, err
		}
		preview.Target = policy.TargetValue
		if input.Group == GroupReseed {
			var publications ReseedPublications
			if err := scanReseedTotals(tx.QueryRow(ctx, reseedTotalsSQL, input.MembershipID, input.Month, end, now), &publications); err != nil {
				return preview, nil, err
			}
			preview.Contribution, preview.PendingCount = int64(publications.Counted), publications.Pending
			rows, err := tx.Query(ctx, `
SELECT torrent_id FROM workgroups.reseed_publications($1,$2,$3,$4)
WHERE contribution_status='counted' ORDER BY torrent_id`, input.MembershipID, input.Month, end, now)
			if err != nil {
				return preview, nil, err
			}
			countedIDs, err = pgx.CollectRows(rows, pgx.RowTo[int64])
			if err != nil {
				return preview, nil, err
			}
		} else {
			values, _, err := repository.contributionValuesWith(ctx, tx, policy.Metric, []uuid.UUID{input.MembershipID}, input.Month, minTime(end, now), now)
			if err != nil {
				return preview, nil, err
			}
			preview.Contribution = values[input.MembershipID]
		}
	}
	switch {
	case now.Before(end):
		preview.State = "in_progress"
	case preview.Contribution >= preview.Target && preview.QualifiedBytes >= input.MinimumBytes:
		preview.State = "eligible"
	case preview.PendingCount > 0 || preview.MissingHours > 0:
		preview.State = "pending_evidence"
	}
	payload, err := json.Marshal(payrollEvidence{Preview: preview, Retention: items, Publications: countedIDs})
	if err != nil {
		return preview, nil, err
	}
	digest := sha256.Sum256(payload)
	preview.Digest = hex.EncodeToString(digest[:])
	return preview, payload, nil
}

func (repository *PostgresRepository) PayPayroll(ctx context.Context, input PayrollInput, expected []byte, actorID, decisionID uuid.UUID, now time.Time) (PayrollPreview, error) {
	ledger, err := economy.NewPostgresRepository(repository.pool)
	if err != nil {
		return PayrollPreview{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return PayrollPreview{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	key := fmt.Sprintf("workgroup_payroll:%s:%s:%s", input.Group, input.MembershipID, input.Month.Format("2006-01"))
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", key); err != nil {
		return PayrollPreview{}, err
	}
	preview, payload, err := repository.payrollPreview(ctx, tx, input, now)
	if err != nil {
		return preview, err
	}
	if preview.State == "paid" {
		if preview.Digest != hex.EncodeToString(expected) {
			return preview, ErrPayrollPreviewChanged
		}
		return preview, nil
	}
	digest := sha256.Sum256(payload)
	if !bytes.Equal(digest[:], expected) {
		return preview, ErrPayrollPreviewChanged
	}
	if preview.State != "eligible" {
		return preview, ErrPayrollNotEligible
	}
	transactionID := uuid.New()
	if _, err := ledger.RecordInTransaction(ctx, tx, economy.RecordCommand{
		TransactionID: transactionID, TransactionType: economy.TransactionWorkgroupPayroll,
		IdempotencyKey: key, SourceReference: key, PayloadSHA256: digest, OccurredAt: now, RecordedAt: now,
		Postings: []economy.PostingInput{
			{AccountID: preview.UserID, Amount: input.Amount},
			{AccountID: economy.WorkgroupPayrollMintAccountID(), Amount: -input.Amount},
		},
	}); err != nil {
		return preview, fmt.Errorf("issue workgroup payroll: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO workgroups.payroll_payments (
    group_kind,user_id,membership_id,period_starts_at,amount,preview_digest,preview_snapshot,
    issued_by,authorization_decision_id,paid_at,magic_transaction_id
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		input.Group, preview.UserID, input.MembershipID, input.Month, input.Amount, digest[:], payload,
		actorID, decisionID, now, transactionID); err != nil {
		return preview, err
	}
	if err := tx.Commit(ctx); err != nil {
		return preview, err
	}
	preview.State, preview.PaidAt, preview.TransactionID = "paid", &now, &transactionID
	return preview, nil
}
