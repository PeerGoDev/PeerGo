package torrents

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type torrentReportCaseRecord struct {
	DatabaseID int64
	UploaderID uuid.UUID
	Case       ManagedTorrentReportCase
}

func (repository *PostgresTorrentMaintenanceRepository) CreateTorrentReport(ctx context.Context, command CreateTorrentReportCommand) (TorrentReportReceipt, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TorrentReportReceipt{}, fmt.Errorf("begin torrent report: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if replay, inputHash, found, err := readTorrentReportReplay(ctx, tx, command.ReporterID, command.RequestID); err != nil {
		return TorrentReportReceipt{}, err
	} else if found {
		if replay.TorrentID != command.TorrentID || replay.ReasonCode != command.ReasonCode || !bytes.Equal(inputHash, command.InputSHA256[:]) {
			return TorrentReportReceipt{}, ErrTorrentReportIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return TorrentReportReceipt{}, fmt.Errorf("commit replayed torrent report: %w", err)
		}
		return replay, nil
	}

	var uploaderID uuid.UUID
	var title, state string
	// Serialize case creation with lifecycle changes. This prevents a report
	// from being accepted after the target has already left public visibility.
	err = tx.QueryRow(ctx, `
SELECT uploader_id, title, state
FROM torrents.torrents
WHERE id=$1
FOR UPDATE`, command.TorrentID).Scan(&uploaderID, &title, &state)
	if errors.Is(err, pgx.ErrNoRows) || State(state) != StatePublished {
		return TorrentReportReceipt{}, ErrTorrentReportTargetNotFound
	}
	if err != nil {
		return TorrentReportReceipt{}, fmt.Errorf("lock torrent report target: %w", err)
	}
	if uploaderID == command.ReporterID {
		return TorrentReportReceipt{}, ErrTorrentReportSelf
	}

	var caseDatabaseID int64
	var casePublicID uuid.UUID
	err = tx.QueryRow(ctx, `
SELECT id, public_id
FROM torrents.torrent_report_cases
WHERE torrent_id=$1 AND state='open'
FOR UPDATE`, command.TorrentID).Scan(&caseDatabaseID, &casePublicID)
	if errors.Is(err, pgx.ErrNoRows) {
		casePublicID = command.CaseID
		err = tx.QueryRow(ctx, `
INSERT INTO torrents.torrent_report_cases (
    public_id, torrent_id, uploader_id, torrent_title,
    state, version, opened_at, updated_at
) VALUES ($1,$2,$3,$4,'open',1,$5,$5)
RETURNING id`, casePublicID, command.TorrentID, uploaderID, title, command.CreatedAt).Scan(&caseDatabaseID)
	}
	if err != nil {
		return TorrentReportReceipt{}, mapTorrentReportWriteError("create torrent report case", err)
	}

	_, err = tx.Exec(ctx, `
INSERT INTO torrents.torrent_reports (
    public_id, case_id, reporter_id, create_request_id,
    create_input_sha256, reason_code, details, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		command.ReportID, caseDatabaseID, command.ReporterID, command.RequestID,
		command.InputSHA256[:], command.ReasonCode, command.Details, command.CreatedAt)
	if err != nil {
		return TorrentReportReceipt{}, mapTorrentReportWriteError("insert torrent report", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return TorrentReportReceipt{}, fmt.Errorf("commit torrent report: %w", err)
	}
	return TorrentReportReceipt{
		ID: command.ReportID, CaseID: casePublicID, TorrentID: command.TorrentID,
		ReasonCode: command.ReasonCode, CreatedAt: command.CreatedAt,
	}, nil
}

func readTorrentReportReplay(ctx context.Context, tx pgx.Tx, reporterID, requestID uuid.UUID) (TorrentReportReceipt, []byte, bool, error) {
	var receipt TorrentReportReceipt
	var inputHash []byte
	err := tx.QueryRow(ctx, `
SELECT report.public_id, case_row.public_id, case_row.torrent_id,
       report.reason_code, report.created_at, report.create_input_sha256
FROM torrents.torrent_reports AS report
JOIN torrents.torrent_report_cases AS case_row ON case_row.id=report.case_id
WHERE report.reporter_id=$1 AND report.create_request_id=$2`, reporterID, requestID).Scan(
		&receipt.ID, &receipt.CaseID, &receipt.TorrentID,
		&receipt.ReasonCode, &receipt.CreatedAt, &inputHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return TorrentReportReceipt{}, nil, false, nil
	}
	if err != nil {
		return TorrentReportReceipt{}, nil, false, fmt.Errorf("read torrent report replay: %w", err)
	}
	return receipt, inputHash, true, nil
}

func (repository *PostgresTorrentMaintenanceRepository) ListTorrentReportCases(ctx context.Context, query TorrentReportCaseQuery) (ManagedTorrentReportCasePage, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ManagedTorrentReportCasePage{}, fmt.Errorf("begin torrent report case list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
SELECT case_row.id, case_row.public_id, case_row.uploader_id,
       case_row.state, case_row.version, case_row.torrent_id, case_row.torrent_title,
       torrent.state, torrent.version,
       uploader.numeric_id, uploader.username, uploader.display_name,
       report_summary.report_count, report_summary.latest_reported_at,
       COALESCE(purchases.active_count, 0)::bigint,
       case_row.opened_at
FROM torrents.torrent_report_cases AS case_row
JOIN torrents.torrents AS torrent ON torrent.id=case_row.torrent_id
JOIN identity.users AS uploader ON uploader.id=case_row.uploader_id
JOIN LATERAL (
    SELECT count(*)::bigint AS report_count, max(created_at) AS latest_reported_at
    FROM torrents.torrent_reports AS report
    WHERE report.case_id=case_row.id
) AS report_summary ON true
LEFT JOIN LATERAL (
    SELECT count(*)::bigint AS active_count
    FROM economy.torrent_purchase_entitlements AS entitlement
    LEFT JOIN economy.torrent_purchase_refunds AS refund
      ON refund.entitlement_id=entitlement.id
    WHERE entitlement.torrent_id=case_row.torrent_id AND refund.id IS NULL
) AS purchases ON true
WHERE ($1='' OR case_row.state=$1)
ORDER BY CASE WHEN case_row.state='open' THEN 0 ELSE 1 END,
         case_row.opened_at, case_row.id
LIMIT $2 OFFSET $3`, query.State, query.Limit, query.Offset)
	if err != nil {
		return ManagedTorrentReportCasePage{}, fmt.Errorf("query torrent report cases: %w", err)
	}
	records := make([]torrentReportCaseRecord, 0, query.Limit)
	for rows.Next() {
		var record torrentReportCaseRecord
		if err := rows.Scan(
			&record.DatabaseID, &record.Case.ID, &record.UploaderID,
			&record.Case.State, &record.Case.Version, &record.Case.TorrentID, &record.Case.TorrentTitle,
			&record.Case.TorrentState, &record.Case.TorrentVersion,
			&record.Case.UploaderNumericID, &record.Case.UploaderUsername, &record.Case.UploaderDisplayName,
			&record.Case.ReportCount, &record.Case.LatestReportedAt,
			&record.Case.ActivePurchaseCount, &record.Case.OpenedAt,
		); err != nil {
			rows.Close()
			return ManagedTorrentReportCasePage{}, fmt.Errorf("scan torrent report case: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ManagedTorrentReportCasePage{}, fmt.Errorf("iterate torrent report cases: %w", err)
	}
	rows.Close()

	items := make([]ManagedTorrentReportCase, 0, len(records))
	for _, record := range records {
		allegations, err := readTorrentReportAllegations(ctx, tx, record.DatabaseID)
		if err != nil {
			return ManagedTorrentReportCasePage{}, err
		}
		record.Case.Reports = allegations
		if err := validateManagedTorrentReportCase(record); err != nil {
			return ManagedTorrentReportCasePage{}, err
		}
		items = append(items, record.Case)
	}

	var total int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM torrents.torrent_report_cases
WHERE ($1='' OR state=$1)`, query.State).Scan(&total); err != nil {
		return ManagedTorrentReportCasePage{}, fmt.Errorf("count torrent report cases: %w", err)
	}
	if total < int64(len(items)) {
		return ManagedTorrentReportCasePage{}, ErrTorrentReportInvariant
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedTorrentReportCasePage{}, fmt.Errorf("commit torrent report case list: %w", err)
	}
	return ManagedTorrentReportCasePage{Items: items, Total: total, Limit: query.Limit, Offset: query.Offset}, nil
}

func readTorrentReportAllegations(ctx context.Context, tx pgx.Tx, caseDatabaseID int64) ([]TorrentReportAllegation, error) {
	rows, err := tx.Query(ctx, `
SELECT reason_code, details, created_at
FROM torrents.torrent_reports
WHERE case_id=$1
ORDER BY created_at, id
LIMIT $2`, caseDatabaseID, MaxTorrentReportsPerCase)
	if err != nil {
		return nil, fmt.Errorf("query torrent report allegations: %w", err)
	}
	defer rows.Close()
	items := make([]TorrentReportAllegation, 0)
	for rows.Next() {
		var item TorrentReportAllegation
		if err := rows.Scan(&item.ReasonCode, &item.Details, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan torrent report allegation: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate torrent report allegations: %w", err)
	}
	return items, nil
}

func (repository *PostgresTorrentMaintenanceRepository) DecideTorrentReportCase(ctx context.Context, command DecideTorrentReportCaseCommand) (TorrentReportDecisionResult, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TorrentReportDecisionResult{}, fmt.Errorf("begin torrent report decision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if result, found, err := readTorrentReportDecisionReplay(ctx, tx, command); found || err != nil {
		if err != nil {
			return TorrentReportDecisionResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return TorrentReportDecisionResult{}, fmt.Errorf("commit replayed torrent report decision: %w", err)
		}
		return result, nil
	}

	var caseDatabaseID int64
	var uploaderID uuid.UUID
	var caseState string
	var caseVersion int64
	var torrentID TorrentID
	var torrentState string
	var torrentVersion int64
	var infoHashBytes []byte
	var totalSizeBytes int64
	err = tx.QueryRow(ctx, `
SELECT case_row.id, case_row.uploader_id, case_row.state, case_row.version,
       case_row.torrent_id, torrent.state, torrent.version,
       torrent.info_hash_v1, torrent.total_size_bytes
FROM torrents.torrent_report_cases AS case_row
JOIN torrents.torrents AS torrent ON torrent.id=case_row.torrent_id
WHERE case_row.public_id=$1
FOR UPDATE OF case_row, torrent`, command.CaseID).Scan(
		&caseDatabaseID, &uploaderID, &caseState, &caseVersion,
		&torrentID, &torrentState, &torrentVersion, &infoHashBytes, &totalSizeBytes,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return TorrentReportDecisionResult{}, ErrTorrentReportCaseNotFound
	}
	if err != nil {
		return TorrentReportDecisionResult{}, fmt.Errorf("lock torrent report case: %w", err)
	}
	if uploaderID == command.ReviewerID {
		return TorrentReportDecisionResult{}, ErrTorrentReportSelfReview
	}
	var reviewerReported bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM torrents.torrent_reports
    WHERE case_id=$1 AND reporter_id=$2
)`, caseDatabaseID, command.ReviewerID).Scan(&reviewerReported); err != nil {
		return TorrentReportDecisionResult{}, fmt.Errorf("check torrent report reviewer conflict: %w", err)
	}
	if reviewerReported {
		return TorrentReportDecisionResult{}, ErrTorrentReportSelfReview
	}
	if TorrentReportCaseState(caseState) != TorrentReportCaseOpen {
		return TorrentReportDecisionResult{}, ErrTorrentReportCaseStateConflict
	}
	if caseVersion != command.ExpectedCaseVersion {
		return TorrentReportDecisionResult{}, ErrTorrentReportCaseVersionConflict
	}
	if torrentVersion != command.ExpectedTorrentVersion {
		return TorrentReportDecisionResult{}, ErrTorrentReportVersionConflict
	}

	resultingCaseState := TorrentReportCaseDismissed
	resultingTorrentState := State(torrentState)
	resultingTorrentVersion := torrentVersion
	if command.Decision == TorrentReportDisableTorrent {
		if State(torrentState) != StatePublished {
			return TorrentReportDecisionResult{}, ErrTorrentReportStateConflict
		}
		resultingCaseState = TorrentReportCaseTorrentDisabled
		resultingTorrentState = StateDisabled
		resultingTorrentVersion++
		updated, err := tx.Exec(ctx, `
UPDATE torrents.torrents
SET state='disabled', version=$2, state_changed_at=$3, updated_at=$3
WHERE id=$1 AND state='published' AND version=$4`,
			torrentID, resultingTorrentVersion, command.DecidedAt, torrentVersion)
		if err != nil {
			return TorrentReportDecisionResult{}, fmt.Errorf("disable reported torrent: %w", err)
		}
		if updated.RowsAffected() != 1 {
			return TorrentReportDecisionResult{}, ErrTorrentReportVersionConflict
		}
	}

	resolved, err := tx.Exec(ctx, `
UPDATE torrents.torrent_report_cases
SET state=$2, version=$3, updated_at=$4, resolved_at=$4
WHERE id=$1 AND state='open' AND version=$5`, caseDatabaseID,
		resultingCaseState, caseVersion+1, command.DecidedAt, caseVersion)
	if err != nil {
		return TorrentReportDecisionResult{}, fmt.Errorf("resolve torrent report case: %w", err)
	}
	if resolved.RowsAffected() != 1 {
		return TorrentReportDecisionResult{}, ErrTorrentReportCaseVersionConflict
	}

	_, err = tx.Exec(ctx, `
INSERT INTO torrents.torrent_report_decisions (
    id, case_id, case_public_id, torrent_id, reviewer_id,
    decision, reason_code, note,
    expected_case_version, resulting_case_version,
    expected_torrent_state, resulting_torrent_state,
    expected_torrent_version, resulting_torrent_version,
    resulting_case_state, authorization_decision_id, decided_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		command.DecisionID, caseDatabaseID, command.CaseID, torrentID, command.ReviewerID,
		command.Decision, command.ReasonCode, command.Note,
		caseVersion, caseVersion+1, torrentState, resultingTorrentState,
		torrentVersion, resultingTorrentVersion, resultingCaseState,
		command.Authorization.ID, command.DecidedAt)
	if err != nil {
		return TorrentReportDecisionResult{}, mapTorrentReportWriteError("insert torrent report decision", err)
	}

	if command.Decision == TorrentReportDisableTorrent {
		infoHash, err := withdrawalInfoHash(infoHashBytes, totalSizeBytes)
		if err != nil {
			return TorrentReportDecisionResult{}, err
		}
		if err := repository.appendTorrentReportDisableEvidence(
			ctx, tx, command, torrentID, torrentVersion, resultingTorrentVersion,
			infoHash, totalSizeBytes,
		); err != nil {
			return TorrentReportDecisionResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return TorrentReportDecisionResult{}, fmt.Errorf("commit torrent report decision: %w", err)
	}
	return TorrentReportDecisionResult{
		DecisionID: command.DecisionID, CaseID: command.CaseID, TorrentID: torrentID,
		Decision: command.Decision, CaseState: resultingCaseState, CaseVersion: caseVersion + 1,
		TorrentState: resultingTorrentState, TorrentVersion: resultingTorrentVersion, DecidedAt: command.DecidedAt,
	}, nil
}

func readTorrentReportDecisionReplay(ctx context.Context, tx pgx.Tx, command DecideTorrentReportCaseCommand) (TorrentReportDecisionResult, bool, error) {
	var result TorrentReportDecisionResult
	var reviewerID uuid.UUID
	var reasonCode TorrentReportDecisionReasonCode
	var note string
	var expectedCaseVersion, expectedTorrentVersion int64
	err := tx.QueryRow(ctx, `
SELECT id, case_public_id, torrent_id, reviewer_id, decision, reason_code, note,
       expected_case_version, resulting_case_version,
       expected_torrent_version, resulting_torrent_version,
       resulting_case_state, resulting_torrent_state, decided_at
FROM torrents.torrent_report_decisions
WHERE id=$1`, command.DecisionID).Scan(
		&result.DecisionID, &result.CaseID, &result.TorrentID, &reviewerID,
		&result.Decision, &reasonCode, &note,
		&expectedCaseVersion, &result.CaseVersion,
		&expectedTorrentVersion, &result.TorrentVersion,
		&result.CaseState, &result.TorrentState, &result.DecidedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return TorrentReportDecisionResult{}, false, nil
	}
	if err != nil {
		return TorrentReportDecisionResult{}, false, fmt.Errorf("read torrent report decision replay: %w", err)
	}
	if result.CaseID != command.CaseID || reviewerID != command.ReviewerID || result.Decision != command.Decision ||
		reasonCode != command.ReasonCode || note != command.Note || expectedCaseVersion != command.ExpectedCaseVersion ||
		expectedTorrentVersion != command.ExpectedTorrentVersion {
		return TorrentReportDecisionResult{}, true, ErrTorrentReportDecisionConflict
	}
	return result, true, nil
}

func (repository *PostgresTorrentMaintenanceRepository) appendTorrentReportDisableEvidence(
	ctx context.Context,
	tx pgx.Tx,
	command DecideTorrentReportCaseCommand,
	torrentID TorrentID,
	expectedVersion, resultingVersion int64,
	infoHash InfoHashV1,
	totalSizeBytes int64,
) error {
	reason := string(command.ReasonCode) + ": " + command.Note
	result := TorrentAvailabilityResult{
		ChangeID: command.DecisionID, TorrentID: torrentID, Action: TorrentAvailabilityReportDisable,
		State: StateDisabled, Version: resultingVersion, ChangedAt: command.DecidedAt,
	}
	auditEvent, err := repository.eventBuilder.BuildTorrentLifecycleEvent(TorrentLifecycleAuditInput{
		ChangeID: command.DecisionID, ActorID: command.ReviewerID,
		Action: TorrentAvailabilityReportDisable, Reason: reason,
		OccurredAt: command.DecidedAt, Authorization: command.Authorization,
		Before: TorrentLifecycleAuditState{TorrentID: torrentID, State: StatePublished, Version: expectedVersion, TrackerEligible: true},
		After:  TorrentLifecycleAuditState{TorrentID: torrentID, State: StateDisabled, Version: resultingVersion, TrackerEligible: false},
	})
	if err != nil {
		return fmt.Errorf("build torrent report lifecycle audit event: %w", err)
	}
	if err := repository.newAuditAppender(tx).Append(ctx, auditEvent); err != nil {
		return fmt.Errorf("append torrent report lifecycle audit event: %w", err)
	}
	controlEvent, err := repository.eligibilityBuilder.BuildTorrentLifecycleEligibilityEvent(TorrentLifecycleEligibilityInput{
		Result: result, InfoHashV1: infoHash, TotalSizeBytes: totalSizeBytes,
	})
	if err != nil {
		return fmt.Errorf("build torrent report Tracker event: %w", err)
	}
	if err := repository.newTrackerAppender(tx).Append(ctx, controlEvent); err != nil {
		return fmt.Errorf("append torrent report Tracker event: %w", err)
	}
	return nil
}

func validateManagedTorrentReportCase(record torrentReportCaseRecord) error {
	item := record.Case
	if record.DatabaseID < 1 || record.UploaderID == uuid.Nil || item.ID == uuid.Nil || item.TorrentID < 1 ||
		strings.TrimSpace(item.TorrentTitle) == "" || item.Version < 1 || item.TorrentVersion < 1 ||
		item.UploaderNumericID < 1 || strings.TrimSpace(item.UploaderUsername) == "" || strings.TrimSpace(item.UploaderDisplayName) == "" ||
		item.ReportCount < 1 || item.ActivePurchaseCount < 0 || item.OpenedAt.IsZero() || item.LatestReportedAt.Before(item.OpenedAt) ||
		len(item.Reports) < 1 || int64(len(item.Reports)) > item.ReportCount || len(item.Reports) > MaxTorrentReportsPerCase {
		return ErrTorrentReportInvariant
	}
	switch item.State {
	case TorrentReportCaseOpen, TorrentReportCaseDismissed, TorrentReportCaseTorrentDisabled:
	default:
		return ErrTorrentReportInvariant
	}
	switch item.TorrentState {
	case StatePendingReview, StatePublished, StateRejected, StateDisabled, StateDeleted:
	default:
		return ErrTorrentReportInvariant
	}
	for _, allegation := range item.Reports {
		if !validTorrentReportReason(allegation.ReasonCode) || !utf8.ValidString(allegation.Details) ||
			utf8.RuneCountInString(allegation.Details) > MaxTorrentReportDetailsRunes || allegation.CreatedAt.Before(item.OpenedAt) {
			return ErrTorrentReportInvariant
		}
	}
	return nil
}

func mapTorrentReportWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		switch postgresError.ConstraintName {
		case "torrent_reports_reporter_id_create_request_id_key":
			return ErrTorrentReportIdempotencyConflict
		case "torrent_reports_case_id_reporter_id_key":
			return ErrTorrentAlreadyReported
		case "torrent_report_decisions_pkey":
			return ErrTorrentReportDecisionConflict
		case "torrent_report_decisions_case_id_key":
			return ErrTorrentReportCaseStateConflict
		case "torrent_report_cases_one_open_per_torrent_idx":
			return ErrTorrentReportCaseStateConflict
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
