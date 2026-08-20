package traffic

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const hnrAppealSelect = `
SELECT
    appeal.id,
    appeal.obligation_id,
    appeal.user_id,
    users.numeric_id,
    users.username,
    torrent.id,
    torrent.title,
    appeal.statement,
    appeal.created_at,
    COALESCE(resolution.outcome, 'pending'),
    COALESCE(resolution.response, ''),
    resolution.created_at,
    CASE
        WHEN exemption.obligation_id IS NOT NULL THEN 'exempt'
        WHEN obligation.state = 'satisfied' THEN 'satisfied'
        WHEN CURRENT_TIMESTAMP < obligation.assessment_due_at THEN 'tracking'
        WHEN CURRENT_TIMESTAMP < obligation.grace_ends_at THEN 'grace'
        ELSE 'overdue'
    END,
    obligation.version,
    obligation.seeded_seconds,
    obligation.required_seed_seconds,
    obligation.raw_ratio_basis_points,
    obligation.required_ratio_basis_points,
    obligation.grace_ends_at
FROM traffic.hnr_appeals AS appeal
JOIN traffic.user_hnr_obligations AS obligation
  ON obligation.obligation_id = appeal.obligation_id
JOIN identity.users AS users ON users.id = appeal.user_id
JOIN torrents.torrents AS torrent ON torrent.id = obligation.torrent_id
LEFT JOIN traffic.hnr_appeal_resolutions AS resolution
  ON resolution.appeal_id = appeal.id
LEFT JOIN traffic.hnr_appeal_exemptions AS exemption
  ON exemption.obligation_id = obligation.obligation_id
`

func (repository *PostgresRepository) SubmitHNRAppeal(ctx context.Context, command SubmitHNRAppealCommand) (HNRAppeal, error) {
	if command.AppealID == uuid.Nil || command.ObligationID == uuid.Nil || command.UserID == uuid.Nil ||
		command.OccurredAt.IsZero() || command.Authorization.ID == uuid.Nil || !command.Authorization.Allow {
		return HNRAppeal{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return HNRAppeal{}, fmt.Errorf("begin H&R appeal submission: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Replay lookup precedes current-state checks so a lost successful response
	// remains recoverable even when the obligation advances immediately after it.
	existing, err := scanHNRAppeal(tx.QueryRow(ctx, hnrAppealSelect+`
WHERE appeal.id = $1`, command.AppealID))
	if err == nil {
		if existing.UserID != command.UserID || existing.ObligationID != command.ObligationID || existing.Statement != command.Statement {
			return HNRAppeal{}, ErrIdempotency
		}
		existing.Replayed = true
		if err := tx.Commit(ctx); err != nil {
			return HNRAppeal{}, classifyDatabaseError("commit H&R appeal replay", err)
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return HNRAppeal{}, err
	}

	var state string
	var graceEndsAt time.Time
	if err := tx.QueryRow(ctx, `
SELECT obligation.state, obligation.grace_ends_at
FROM traffic.user_hnr_obligations AS obligation
WHERE obligation.obligation_id = $1
  AND obligation.user_id = $2
FOR UPDATE`, command.ObligationID, command.UserID).Scan(&state, &graceEndsAt); errors.Is(err, pgx.ErrNoRows) {
		return HNRAppeal{}, ErrHNRNotAppealable
	} else if err != nil {
		return HNRAppeal{}, fmt.Errorf("lock H&R obligation for appeal: %w", err)
	}
	if state != "tracking" || command.OccurredAt.Before(graceEndsAt) {
		return HNRAppeal{}, ErrHNRNotAppealable
	}
	var appealExists bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM traffic.hnr_appeals WHERE obligation_id = $1
)`, command.ObligationID).Scan(&appealExists); err != nil {
		return HNRAppeal{}, fmt.Errorf("check existing H&R appeal: %w", err)
	}
	if appealExists {
		return HNRAppeal{}, ErrHNRAppealExists
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO traffic.hnr_appeals (
    id, obligation_id, user_id, statement,
    authorization_decision_id, created_at
) VALUES ($1, $2, $3, $4, $5, $6)`, command.AppealID, command.ObligationID,
		command.UserID, command.Statement, command.Authorization.ID, command.OccurredAt); err != nil {
		return HNRAppeal{}, classifyDatabaseError("insert H&R appeal", err)
	}
	result, err := scanHNRAppeal(tx.QueryRow(ctx, hnrAppealSelect+`
WHERE appeal.id = $1`, command.AppealID))
	if err != nil {
		return HNRAppeal{}, fmt.Errorf("read submitted H&R appeal: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return HNRAppeal{}, classifyDatabaseError("commit H&R appeal submission", err)
	}
	return result, nil
}

func (repository *PostgresRepository) HNRAppeals(ctx context.Context, query HNRAppealQuery) (HNRAppealPage, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return HNRAppealPage{}, fmt.Errorf("begin H&R appeal read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	filterSQL := hnrAppealFilterSQL(query.Filter)
	search := "%" + strings.ToLower(query.Query) + "%"
	var total int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)
FROM traffic.hnr_appeals AS appeal
JOIN identity.users AS users ON users.id = appeal.user_id
JOIN traffic.user_hnr_obligations AS obligation
  ON obligation.obligation_id = appeal.obligation_id
JOIN torrents.torrents AS torrent ON torrent.id = obligation.torrent_id
LEFT JOIN traffic.hnr_appeal_resolutions AS resolution
  ON resolution.appeal_id = appeal.id
WHERE ($1 = '%%' OR lower(users.username) LIKE $1
       OR lower(torrent.title) LIKE $1
       OR users.numeric_id::text = btrim($2, '%')
       OR torrent.id::text = btrim($2, '%'))
  AND `+filterSQL, search, search).Scan(&total); err != nil {
		return HNRAppealPage{}, fmt.Errorf("count H&R appeals: %w", err)
	}
	rows, err := tx.Query(ctx, hnrAppealSelect+`
WHERE ($1 = '%%' OR lower(users.username) LIKE $1
       OR lower(torrent.title) LIKE $1
       OR users.numeric_id::text = btrim($1, '%')
       OR torrent.id::text = btrim($1, '%'))
  AND `+filterSQL+`
ORDER BY (resolution.id IS NULL) DESC, appeal.created_at ASC, appeal.id ASC
LIMIT $2 OFFSET $3`, search, query.Limit, query.Offset)
	if err != nil {
		return HNRAppealPage{}, fmt.Errorf("list H&R appeals: %w", err)
	}
	items := make([]HNRAppeal, 0, query.Limit)
	for rows.Next() {
		item, scanErr := scanHNRAppeal(rows)
		if scanErr != nil {
			rows.Close()
			return HNRAppealPage{}, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return HNRAppealPage{}, fmt.Errorf("iterate H&R appeals: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return HNRAppealPage{}, fmt.Errorf("commit H&R appeal read: %w", err)
	}
	return HNRAppealPage{Items: items, Total: total, Limit: query.Limit, Offset: query.Offset}, nil
}

func (repository *PostgresRepository) DecideHNRAppeal(ctx context.Context, command DecideHNRAppealCommand) (HNRAppeal, error) {
	if command.AppealID == uuid.Nil || command.ActorID == uuid.Nil || command.OccurredAt.IsZero() ||
		command.Authorization.ID == uuid.Nil || !command.Authorization.Allow {
		return HNRAppeal{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return HNRAppeal{}, fmt.Errorf("begin H&R appeal decision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := scanHNRAppeal(tx.QueryRow(ctx, hnrAppealSelect+`
WHERE appeal.id = $1
FOR UPDATE OF appeal, obligation`, command.AppealID))
	if errors.Is(err, pgx.ErrNoRows) {
		return HNRAppeal{}, ErrNotFound
	}
	if err != nil {
		return HNRAppeal{}, err
	}
	if result.UserID == command.ActorID {
		return HNRAppeal{}, ErrSelfTarget
	}
	if result.Status != HNRAppealPending {
		return HNRAppeal{}, ErrHNRAppealResolved
	}
	if result.ObligationStatus != HNRStatusOverdue || result.ObligationVersion != command.ExpectedObligationVersion {
		return HNRAppeal{}, ErrConflict
	}
	resolutionID := uuid.New()
	if _, err := tx.Exec(ctx, `
INSERT INTO traffic.hnr_appeal_resolutions (
    id, appeal_id, outcome, response, actor_id,
    authorization_decision_id, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)`, resolutionID, result.ID,
		command.Decision, command.Response, command.ActorID,
		command.Authorization.ID, command.OccurredAt); err != nil {
		return HNRAppeal{}, classifyDatabaseError("insert H&R appeal resolution", err)
	}
	result.Status = HNRAppealStatus(command.Decision)
	result.Response = command.Response
	resolvedAt := command.OccurredAt
	result.ResolvedAt = &resolvedAt
	if command.Decision == HNRAppealDecisionApprove {
		result.ObligationStatus = HNRStatusExempt
	}
	if err := tx.Commit(ctx); err != nil {
		return HNRAppeal{}, classifyDatabaseError("commit H&R appeal decision", err)
	}
	return result, nil
}

func scanHNRAppeal(scanner interface{ Scan(...any) error }) (HNRAppeal, error) {
	var result HNRAppeal
	var status string
	var obligationStatus string
	var resolvedAt pgtype.Timestamptz
	var graceEndsAt pgtype.Timestamptz
	if err := scanner.Scan(
		&result.ID, &result.ObligationID, &result.UserID,
		&result.UserNumericID, &result.Username,
		&result.TorrentID, &result.TorrentTitle,
		&result.Statement, &result.CreatedAt,
		&status, &result.Response, &resolvedAt,
		&obligationStatus, &result.ObligationVersion,
		&result.SeededSeconds, &result.RequiredSeedSeconds,
		&result.RawRatioBasisPoints, &result.RequiredRatioBasisPoints,
		&graceEndsAt,
	); err != nil {
		return HNRAppeal{}, err
	}
	result.Status = HNRAppealStatus(status)
	result.ObligationStatus = HNRStatus(obligationStatus)
	if !validHNRAppealStatus(result.Status) || !validHNRStatus(result.ObligationStatus) ||
		result.ID == uuid.Nil || result.ObligationID == uuid.Nil || result.UserID == uuid.Nil ||
		result.UserNumericID < 1 || strings.TrimSpace(result.Username) == "" || result.TorrentID < 1 ||
		strings.TrimSpace(result.TorrentTitle) == "" || result.ObligationVersion < 1 || !graceEndsAt.Valid {
		return HNRAppeal{}, ErrInvariant
	}
	result.CreatedAt = result.CreatedAt.UTC().Round(0)
	result.GraceEndsAt = graceEndsAt.Time.UTC().Round(0)
	if resolvedAt.Valid {
		value := resolvedAt.Time.UTC().Round(0)
		result.ResolvedAt = &value
	}
	return result, nil
}

func validHNRAppealStatus(value HNRAppealStatus) bool {
	switch value {
	case HNRAppealPending, HNRAppealApproved, HNRAppealRejected, HNRAppealObligationResolved:
		return true
	default:
		return false
	}
}

func hnrAppealFilterSQL(filter HNRAppealFilter) string {
	switch filter {
	case HNRAppealFilterPending:
		return "resolution.id IS NULL"
	case HNRAppealFilterResolved:
		return "resolution.id IS NOT NULL"
	default:
		return "TRUE"
	}
}

var _ HNRAppealRepository = (*PostgresRepository)(nil)
