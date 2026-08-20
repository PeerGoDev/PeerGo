package torrents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type torrentWithdrawalRecord struct {
	Request         TorrentWithdrawalRequest
	UploaderID      uuid.UUID
	AuthorizationID uuid.UUID
}

func (repository *PostgresTorrentMaintenanceRepository) SubmitTorrentWithdrawal(ctx context.Context, command SubmitTorrentWithdrawalCommand) (TorrentWithdrawalRequest, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TorrentWithdrawalRequest{}, fmt.Errorf("begin torrent withdrawal: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if replay, found, err := readTorrentWithdrawal(ctx, tx, command.RequestID, false); err != nil {
		return TorrentWithdrawalRequest{}, err
	} else if found {
		if replay.UploaderID != command.UploaderID || replay.Request.TorrentID != command.TorrentID ||
			replay.Request.ExpectedTorrentVersion != command.ExpectedVersion || replay.Request.Reason != command.Reason {
			return TorrentWithdrawalRequest{}, ErrTorrentWithdrawalIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return TorrentWithdrawalRequest{}, fmt.Errorf("commit replayed torrent withdrawal: %w", err)
		}
		return replay.Request, nil
	}

	var state string
	var version int64
	var title string
	var infoHashBytes []byte
	var totalSizeBytes int64
	err = tx.QueryRow(ctx, `
SELECT state, version, title, info_hash_v1, total_size_bytes
FROM torrents.torrents
WHERE id=$1 AND uploader_id=$2
FOR UPDATE`, command.TorrentID, command.UploaderID).Scan(
		&state, &version, &title, &infoHashBytes, &totalSizeBytes,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return TorrentWithdrawalRequest{}, ErrTorrentWithdrawalNotFound
	}
	if err != nil {
		return TorrentWithdrawalRequest{}, fmt.Errorf("lock torrent for withdrawal: %w", err)
	}
	var pendingWithdrawal bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM torrents.torrent_withdrawal_requests
    WHERE torrent_id=$1 AND status='pending'
)`, command.TorrentID).Scan(&pendingWithdrawal); err != nil {
		return TorrentWithdrawalRequest{}, fmt.Errorf("check pending torrent withdrawal: %w", err)
	}
	if pendingWithdrawal {
		return TorrentWithdrawalRequest{}, ErrTorrentWithdrawalPending
	}
	if State(state) != StatePublished {
		return TorrentWithdrawalRequest{}, ErrTorrentWithdrawalStateConflict
	}
	if version != command.ExpectedVersion {
		return TorrentWithdrawalRequest{}, ErrTorrentWithdrawalVersionConflict
	}
	var pendingContentChange bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM torrents.torrent_content_change_requests
    WHERE torrent_id=$1 AND status='pending'
)`, command.TorrentID).Scan(&pendingContentChange); err != nil {
		return TorrentWithdrawalRequest{}, fmt.Errorf("check pending content change before withdrawal: %w", err)
	}
	if pendingContentChange {
		return TorrentWithdrawalRequest{}, ErrTorrentWithdrawalContentChangePending
	}
	infoHash, err := withdrawalInfoHash(infoHashBytes, totalSizeBytes)
	if err != nil {
		return TorrentWithdrawalRequest{}, err
	}

	disabledVersion := version + 1
	updated, err := tx.Exec(ctx, `
UPDATE torrents.torrents
SET state='disabled', version=$3, state_changed_at=$4, updated_at=$4
WHERE id=$1 AND uploader_id=$2 AND state='published' AND version=$5`,
		command.TorrentID, command.UploaderID, disabledVersion, command.OccurredAt, version)
	if err != nil {
		return TorrentWithdrawalRequest{}, fmt.Errorf("disable torrent for withdrawal: %w", err)
	}
	if updated.RowsAffected() != 1 {
		return TorrentWithdrawalRequest{}, ErrTorrentWithdrawalVersionConflict
	}
	_, err = tx.Exec(ctx, `
INSERT INTO torrents.torrent_withdrawal_requests (
    id, torrent_id, uploader_id, torrent_title, reason,
    expected_torrent_version, disabled_torrent_version, status, version,
    authorization_decision_id, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,'pending',1,$8,$9)`,
		command.RequestID, command.TorrentID, command.UploaderID, title, command.Reason,
		version, disabledVersion, command.Authorization.ID, command.OccurredAt)
	if err != nil {
		return TorrentWithdrawalRequest{}, mapTorrentWithdrawalWriteError("insert torrent withdrawal", err)
	}
	if err := repository.appendWithdrawalTransitionEvidence(ctx, tx, command.RequestID, command.UploaderID,
		TorrentAvailabilityWithdrawRequest, command.Reason, command.OccurredAt, command.Authorization,
		command.TorrentID, StatePublished, version, StateDisabled, disabledVersion, infoHash, totalSizeBytes); err != nil {
		return TorrentWithdrawalRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TorrentWithdrawalRequest{}, fmt.Errorf("commit torrent withdrawal: %w", err)
	}
	return TorrentWithdrawalRequest{
		ID: command.RequestID, TorrentID: command.TorrentID, TorrentTitle: title, Reason: command.Reason,
		ExpectedTorrentVersion: version, DisabledTorrentVersion: disabledVersion,
		Status: TorrentWithdrawalPending, Version: 1, CreatedAt: command.OccurredAt,
	}, nil
}

func (repository *PostgresTorrentMaintenanceRepository) ListTorrentWithdrawals(ctx context.Context, query TorrentWithdrawalQuery) (ManagedTorrentWithdrawalPage, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ManagedTorrentWithdrawalPage{}, fmt.Errorf("begin torrent withdrawal list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
SELECT request.id, request.torrent_id, request.uploader_id, request.torrent_title,
       request.reason, request.expected_torrent_version, request.disabled_torrent_version,
       request.status, request.version, request.authorization_decision_id,
       request.created_at, request.decided_at,
       uploader.numeric_id, uploader.username, uploader.display_name,
       COALESCE(purchases.active_count, 0)::bigint
FROM torrents.torrent_withdrawal_requests AS request
JOIN identity.users AS uploader ON uploader.id=request.uploader_id
LEFT JOIN LATERAL (
    SELECT count(*)::bigint AS active_count
    FROM economy.torrent_purchase_entitlements AS entitlement
    LEFT JOIN economy.torrent_purchase_refunds AS refund
      ON refund.entitlement_id=entitlement.id
    WHERE entitlement.torrent_id=request.torrent_id AND refund.id IS NULL
) AS purchases ON true
WHERE ($1='' OR request.status=$1)
ORDER BY CASE WHEN request.status='pending' THEN 0 ELSE 1 END,
         request.created_at, request.id
LIMIT $2 OFFSET $3`, query.Status, query.Limit, query.Offset)
	if err != nil {
		return ManagedTorrentWithdrawalPage{}, fmt.Errorf("query torrent withdrawals: %w", err)
	}
	defer rows.Close()
	items := make([]ManagedTorrentWithdrawalRequest, 0, query.Limit)
	for rows.Next() {
		var record torrentWithdrawalRecord
		var decidedAt pgtype.Timestamptz
		var item ManagedTorrentWithdrawalRequest
		if err := rows.Scan(
			&record.Request.ID, &record.Request.TorrentID, &record.UploaderID, &record.Request.TorrentTitle,
			&record.Request.Reason, &record.Request.ExpectedTorrentVersion, &record.Request.DisabledTorrentVersion,
			&record.Request.Status, &record.Request.Version, &record.AuthorizationID,
			&record.Request.CreatedAt, &decidedAt,
			&item.UploaderNumericID, &item.UploaderUsername, &item.UploaderDisplayName,
			&item.ActivePurchaseCount,
		); err != nil {
			return ManagedTorrentWithdrawalPage{}, fmt.Errorf("scan torrent withdrawal: %w", err)
		}
		if decidedAt.Valid {
			value := decidedAt.Time.UTC()
			record.Request.DecidedAt = &value
		}
		if err := validateTorrentWithdrawalRecord(record); err != nil || item.UploaderNumericID < 1 ||
			strings.TrimSpace(item.UploaderUsername) == "" || strings.TrimSpace(item.UploaderDisplayName) == "" || item.ActivePurchaseCount < 0 {
			return ManagedTorrentWithdrawalPage{}, ErrTorrentReadInvariant
		}
		item.TorrentWithdrawalRequest = record.Request
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ManagedTorrentWithdrawalPage{}, fmt.Errorf("iterate torrent withdrawals: %w", err)
	}
	var total int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM torrents.torrent_withdrawal_requests
WHERE ($1='' OR status=$1)`, query.Status).Scan(&total); err != nil {
		return ManagedTorrentWithdrawalPage{}, fmt.Errorf("count torrent withdrawals: %w", err)
	}
	if total < int64(len(items)) {
		return ManagedTorrentWithdrawalPage{}, ErrTorrentReadInvariant
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedTorrentWithdrawalPage{}, fmt.Errorf("commit torrent withdrawal list: %w", err)
	}
	return ManagedTorrentWithdrawalPage{Items: items, Total: total, Limit: query.Limit, Offset: query.Offset}, nil
}

func (repository *PostgresTorrentMaintenanceRepository) DecideTorrentWithdrawal(ctx context.Context, command DecideTorrentWithdrawalCommand) (TorrentWithdrawalDecisionResult, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TorrentWithdrawalDecisionResult{}, fmt.Errorf("begin torrent withdrawal decision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if result, found, err := readTorrentWithdrawalDecisionReplay(ctx, tx, command); found || err != nil {
		if err != nil {
			return TorrentWithdrawalDecisionResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return TorrentWithdrawalDecisionResult{}, fmt.Errorf("commit replayed torrent withdrawal decision: %w", err)
		}
		return result, nil
	}
	record, found, err := readTorrentWithdrawal(ctx, tx, command.RequestID, true)
	if err != nil {
		return TorrentWithdrawalDecisionResult{}, err
	}
	if !found {
		return TorrentWithdrawalDecisionResult{}, ErrTorrentWithdrawalNotFound
	}
	// A site administrator who uploaded the torrent still needs another
	// administrator to approve its tombstone. This prevents a privileged owner
	// from using the review path as an immediate delete command.
	if record.UploaderID == command.ReviewerID {
		return TorrentWithdrawalDecisionResult{}, ErrTorrentWithdrawalSelfReview
	}
	if record.Request.Status != TorrentWithdrawalPending || record.Request.Version != command.ExpectedRequestVersion {
		return TorrentWithdrawalDecisionResult{}, ErrTorrentWithdrawalVersionConflict
	}

	var state string
	var version int64
	var categoryEnabled, hasVerifiedLocation bool
	var infoHashBytes []byte
	var totalSizeBytes int64
	err = tx.QueryRow(ctx, `
SELECT torrent.state, torrent.version, category.enabled, torrent.info_hash_v1,
       torrent.total_size_bytes,
       EXISTS (
           SELECT 1 FROM torrents.torrent_object_locations AS location
           WHERE location.object_id=torrent.object_id
             AND location.state='verified' AND location.verified_at IS NOT NULL
       )
FROM torrents.torrents AS torrent
JOIN catalog.categories AS category ON category.id=torrent.category_id
WHERE torrent.id=$1 AND torrent.uploader_id=$2
FOR UPDATE OF torrent`, record.Request.TorrentID, record.UploaderID).Scan(
		&state, &version, &categoryEnabled, &infoHashBytes, &totalSizeBytes, &hasVerifiedLocation,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return TorrentWithdrawalDecisionResult{}, ErrTorrentWithdrawalNotFound
	}
	if err != nil {
		return TorrentWithdrawalDecisionResult{}, fmt.Errorf("lock torrent for withdrawal decision: %w", err)
	}
	if State(state) != StateDisabled {
		return TorrentWithdrawalDecisionResult{}, ErrTorrentWithdrawalStateConflict
	}
	if version != record.Request.DisabledTorrentVersion {
		return TorrentWithdrawalDecisionResult{}, ErrTorrentWithdrawalVersionConflict
	}
	infoHash, err := withdrawalInfoHash(infoHashBytes, totalSizeBytes)
	if err != nil {
		return TorrentWithdrawalDecisionResult{}, err
	}

	resultingStatus := TorrentWithdrawalApproved
	resultingState := StateDeleted
	action := TorrentAvailabilityWithdrawApprove
	if command.Decision == TorrentWithdrawalApprove {
		activePurchases, err := countActiveTorrentPurchases(ctx, tx, record.Request.TorrentID)
		if err != nil {
			return TorrentWithdrawalDecisionResult{}, err
		}
		if activePurchases > 0 {
			return TorrentWithdrawalDecisionResult{}, ErrTorrentWithdrawalActivePurchases
		}
	} else {
		resultingStatus = TorrentWithdrawalRejected
		resultingState = StatePublished
		action = TorrentAvailabilityWithdrawReject
		if !categoryEnabled {
			return TorrentWithdrawalDecisionResult{}, ErrTorrentWithdrawalCategoryUnavailable
		}
		if !hasVerifiedLocation {
			return TorrentWithdrawalDecisionResult{}, ErrTorrentWithdrawalObjectUnavailable
		}
	}
	resultingTorrentVersion := version + 1
	updated, err := tx.Exec(ctx, `
UPDATE torrents.torrents
SET state=$2, version=$3, state_changed_at=$4, updated_at=$4
WHERE id=$1 AND state='disabled' AND version=$5`, record.Request.TorrentID,
		resultingState, resultingTorrentVersion, command.OccurredAt, version)
	if err != nil {
		return TorrentWithdrawalDecisionResult{}, fmt.Errorf("apply torrent withdrawal decision: %w", err)
	}
	if updated.RowsAffected() != 1 {
		return TorrentWithdrawalDecisionResult{}, ErrTorrentWithdrawalVersionConflict
	}
	resolved, err := tx.Exec(ctx, `
UPDATE torrents.torrent_withdrawal_requests
SET status=$2, version=2, decided_at=$3
WHERE id=$1 AND status='pending' AND version=$4`, command.RequestID,
		resultingStatus, command.OccurredAt, command.ExpectedRequestVersion)
	if err != nil {
		return TorrentWithdrawalDecisionResult{}, fmt.Errorf("resolve torrent withdrawal request: %w", err)
	}
	if resolved.RowsAffected() != 1 {
		return TorrentWithdrawalDecisionResult{}, ErrTorrentWithdrawalVersionConflict
	}
	_, err = tx.Exec(ctx, `
INSERT INTO torrents.torrent_withdrawal_decisions (
    id, request_id, torrent_id, reviewer_id, decision, reason,
    expected_request_version, resulting_request_version,
    expected_torrent_version, resulting_torrent_version,
    authorization_decision_id, occurred_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,2,$8,$9,$10,$11)`,
		command.DecisionID, command.RequestID, record.Request.TorrentID, command.ReviewerID,
		command.Decision, command.Reason, command.ExpectedRequestVersion,
		version, resultingTorrentVersion, command.Authorization.ID, command.OccurredAt)
	if err != nil {
		return TorrentWithdrawalDecisionResult{}, mapTorrentWithdrawalWriteError("insert torrent withdrawal decision", err)
	}
	if err := repository.appendWithdrawalTransitionEvidence(ctx, tx, command.DecisionID, command.ReviewerID,
		action, command.Reason, command.OccurredAt, command.Authorization,
		record.Request.TorrentID, StateDisabled, version, resultingState, resultingTorrentVersion,
		infoHash, totalSizeBytes); err != nil {
		return TorrentWithdrawalDecisionResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TorrentWithdrawalDecisionResult{}, fmt.Errorf("commit torrent withdrawal decision: %w", err)
	}
	return TorrentWithdrawalDecisionResult{
		DecisionID: command.DecisionID, RequestID: command.RequestID, TorrentID: record.Request.TorrentID,
		Decision: command.Decision, RequestStatus: resultingStatus, RequestVersion: 2,
		TorrentState: resultingState, TorrentVersion: resultingTorrentVersion, DecidedAt: command.OccurredAt,
	}, nil
}

func readTorrentWithdrawal(ctx context.Context, tx pgx.Tx, requestID uuid.UUID, forUpdate bool) (torrentWithdrawalRecord, bool, error) {
	query := `
SELECT id, torrent_id, uploader_id, torrent_title, reason,
       expected_torrent_version, disabled_torrent_version, status, version,
       authorization_decision_id, created_at, decided_at
FROM torrents.torrent_withdrawal_requests
WHERE id=$1`
	if forUpdate {
		query += " FOR UPDATE"
	}
	var record torrentWithdrawalRecord
	var decidedAt pgtype.Timestamptz
	err := tx.QueryRow(ctx, query, requestID).Scan(
		&record.Request.ID, &record.Request.TorrentID, &record.UploaderID, &record.Request.TorrentTitle,
		&record.Request.Reason, &record.Request.ExpectedTorrentVersion, &record.Request.DisabledTorrentVersion,
		&record.Request.Status, &record.Request.Version, &record.AuthorizationID,
		&record.Request.CreatedAt, &decidedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return torrentWithdrawalRecord{}, false, nil
	}
	if err != nil {
		return torrentWithdrawalRecord{}, false, fmt.Errorf("read torrent withdrawal: %w", err)
	}
	if decidedAt.Valid {
		value := decidedAt.Time.UTC()
		record.Request.DecidedAt = &value
	}
	if err := validateTorrentWithdrawalRecord(record); err != nil {
		return torrentWithdrawalRecord{}, false, err
	}
	return record, true, nil
}

func readTorrentWithdrawalDecisionReplay(ctx context.Context, tx pgx.Tx, command DecideTorrentWithdrawalCommand) (TorrentWithdrawalDecisionResult, bool, error) {
	var result TorrentWithdrawalDecisionResult
	var reviewerID uuid.UUID
	var reason string
	var expectedRequestVersion int64
	err := tx.QueryRow(ctx, `
SELECT decision.id, decision.request_id, decision.torrent_id, decision.reviewer_id,
       decision.decision, decision.reason, decision.expected_request_version,
       request.status, request.version,
       decision.resulting_torrent_version, decision.occurred_at
FROM torrents.torrent_withdrawal_decisions AS decision
JOIN torrents.torrent_withdrawal_requests AS request ON request.id=decision.request_id
WHERE decision.id=$1`, command.DecisionID).Scan(
		&result.DecisionID, &result.RequestID, &result.TorrentID, &reviewerID,
		&result.Decision, &reason, &expectedRequestVersion,
		&result.RequestStatus, &result.RequestVersion,
		&result.TorrentVersion, &result.DecidedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return TorrentWithdrawalDecisionResult{}, false, nil
	}
	if err != nil {
		return TorrentWithdrawalDecisionResult{}, false, fmt.Errorf("read torrent withdrawal decision replay: %w", err)
	}
	if result.RequestID != command.RequestID || reviewerID != command.ReviewerID || result.Decision != command.Decision ||
		reason != command.Reason || expectedRequestVersion != command.ExpectedRequestVersion {
		return TorrentWithdrawalDecisionResult{}, true, ErrTorrentWithdrawalIdempotencyConflict
	}
	if result.Decision == TorrentWithdrawalApprove {
		result.TorrentState = StateDeleted
	} else {
		result.TorrentState = StatePublished
	}
	return result, true, nil
}

func countActiveTorrentPurchases(ctx context.Context, tx pgx.Tx, torrentID TorrentID) (int64, error) {
	var count int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM economy.torrent_purchase_entitlements AS entitlement
LEFT JOIN economy.torrent_purchase_refunds AS refund ON refund.entitlement_id=entitlement.id
WHERE entitlement.torrent_id=$1 AND refund.id IS NULL`, torrentID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active purchases before torrent withdrawal: %w", err)
	}
	return count, nil
}

func withdrawalInfoHash(value []byte, totalSizeBytes int64) (InfoHashV1, error) {
	if len(value) != 20 || totalSizeBytes < 1 {
		return InfoHashV1{}, ErrTorrentReadInvariant
	}
	var infoHash InfoHashV1
	copy(infoHash[:], value)
	return infoHash, nil
}

func validateTorrentWithdrawalRecord(record torrentWithdrawalRecord) error {
	request := record.Request
	if request.ID == uuid.Nil || request.TorrentID < 1 || record.UploaderID == uuid.Nil || record.AuthorizationID == uuid.Nil ||
		strings.TrimSpace(request.TorrentTitle) == "" || !validWithdrawalReason(request.Reason) ||
		request.ExpectedTorrentVersion < 1 || request.DisabledTorrentVersion != request.ExpectedTorrentVersion+1 || request.CreatedAt.IsZero() {
		return ErrTorrentReadInvariant
	}
	switch request.Status {
	case TorrentWithdrawalPending:
		if request.Version != 1 || request.DecidedAt != nil {
			return ErrTorrentReadInvariant
		}
	case TorrentWithdrawalApproved, TorrentWithdrawalRejected:
		if request.Version != 2 || request.DecidedAt == nil || request.DecidedAt.Before(request.CreatedAt) {
			return ErrTorrentReadInvariant
		}
	default:
		return ErrTorrentReadInvariant
	}
	return nil
}

func (repository *PostgresTorrentMaintenanceRepository) appendWithdrawalTransitionEvidence(
	ctx context.Context,
	tx pgx.Tx,
	changeID, actorID uuid.UUID,
	action TorrentAvailabilityAction,
	reason string,
	occurredAt time.Time,
	authorization authz.Decision,
	torrentID TorrentID,
	beforeState State,
	beforeVersion int64,
	afterState State,
	afterVersion int64,
	infoHash InfoHashV1,
	totalSizeBytes int64,
) error {
	result := TorrentAvailabilityResult{
		ChangeID: changeID, TorrentID: torrentID, Action: action,
		State: afterState, Version: afterVersion, ChangedAt: occurredAt,
	}
	auditEvent, err := repository.eventBuilder.BuildTorrentLifecycleEvent(TorrentLifecycleAuditInput{
		ChangeID: changeID, ActorID: actorID, Action: action, Reason: reason,
		OccurredAt: occurredAt, Authorization: authorization,
		Before: TorrentLifecycleAuditState{TorrentID: torrentID, State: beforeState, Version: beforeVersion, TrackerEligible: beforeState == StatePublished},
		After:  TorrentLifecycleAuditState{TorrentID: torrentID, State: afterState, Version: afterVersion, TrackerEligible: afterState == StatePublished},
	})
	if err != nil {
		return fmt.Errorf("build torrent withdrawal audit event: %w", err)
	}
	if err := repository.newAuditAppender(tx).Append(ctx, auditEvent); err != nil {
		return fmt.Errorf("append torrent withdrawal audit event: %w", err)
	}
	controlEvent, err := repository.eligibilityBuilder.BuildTorrentLifecycleEligibilityEvent(TorrentLifecycleEligibilityInput{
		Result: result, InfoHashV1: infoHash, TotalSizeBytes: totalSizeBytes,
	})
	if err != nil {
		return fmt.Errorf("build torrent withdrawal Tracker event: %w", err)
	}
	if err := repository.newTrackerAppender(tx).Append(ctx, controlEvent); err != nil {
		return fmt.Errorf("append torrent withdrawal Tracker event: %w", err)
	}
	return nil
}

func mapTorrentWithdrawalWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		switch postgresError.ConstraintName {
		case "torrent_withdrawal_requests_pkey", "torrent_withdrawal_decisions_pkey":
			return ErrTorrentWithdrawalIdempotencyConflict
		case "torrent_withdrawal_one_pending_idx":
			return ErrTorrentWithdrawalPending
		default:
			return ErrTorrentWithdrawalVersionConflict
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
