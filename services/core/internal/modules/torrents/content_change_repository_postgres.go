package torrents

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type publishedContentChangeRecord struct {
	Change            PublishedContentChange
	UploaderID        uuid.UUID
	BaseContentSHA256 []byte
	AuthorizationID   uuid.UUID
}

func (repository *PostgresTorrentMaintenanceRepository) SubmitPublishedContentChange(ctx context.Context, command SubmitPublishedContentChangeCommand) (PublishedContentChange, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PublishedContentChange{}, fmt.Errorf("begin published content change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if replay, found, err := readPublishedContentChange(ctx, tx, command.RequestID); err != nil {
		return PublishedContentChange{}, err
	} else if found {
		if replay.UploaderID != command.UploaderID || replay.Change.TorrentID != command.TorrentID ||
			replay.Change.BaseTorrentVersion != command.ExpectedVersion || replay.Change.Reason != command.Reason ||
			!publishedContentSnapshotsEqual(replay.Change.Candidate, command.Candidate) {
			return PublishedContentChange{}, ErrPublishedContentChangeIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return PublishedContentChange{}, fmt.Errorf("commit replayed published content change: %w", err)
		}
		return replay.Change, nil
	}

	var state string
	var version int64
	var description, mediaInfo string
	err = tx.QueryRow(ctx, `
SELECT state, version, description, media_info
FROM torrents.torrents
WHERE id=$1 AND uploader_id=$2
FOR UPDATE`, command.TorrentID, command.UploaderID).Scan(&state, &version, &description, &mediaInfo)
	if errors.Is(err, pgx.ErrNoRows) {
		return PublishedContentChange{}, ErrPublishedContentChangeNotFound
	}
	if err != nil {
		return PublishedContentChange{}, fmt.Errorf("lock published torrent content: %w", err)
	}
	if State(state) != StatePublished {
		return PublishedContentChange{}, ErrPublishedContentChangeStateConflict
	}
	if version != command.ExpectedVersion {
		return PublishedContentChange{}, ErrPublishedContentChangeVersionConflict
	}
	baseIdentifiers, err := listTorrentExternalIdentifiers(ctx, tx, command.TorrentID)
	if err != nil {
		return PublishedContentChange{}, err
	}
	base := PublishedContentSnapshot{Description: description, MediaInfo: mediaInfo, ExternalIdentifiers: baseIdentifiers}
	if publishedContentSnapshotsEqual(base, command.Candidate) {
		return PublishedContentChange{}, ErrPublishedContentChangeUnchanged
	}
	baseDigest, err := publishedContentSnapshotDigest(base)
	if err != nil {
		return PublishedContentChange{}, fmt.Errorf("hash current published content: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO torrents.torrent_content_change_requests (
    id, torrent_id, uploader_id, base_torrent_version, base_content_sha256,
    base_description, base_media_info, candidate_description, candidate_media_info,
    reason, status, version, authorization_decision_id, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'pending',1,$11,$12)`,
		command.RequestID, command.TorrentID, command.UploaderID, command.ExpectedVersion, baseDigest[:],
		base.Description, base.MediaInfo, command.Candidate.Description, command.Candidate.MediaInfo,
		command.Reason, command.Authorization.ID, command.OccurredAt)
	if err != nil {
		return PublishedContentChange{}, mapPublishedContentChangeWriteError("insert published content change", err)
	}
	if err := insertContentChangeIdentifiers(ctx, tx, command.RequestID, "base", base.ExternalIdentifiers); err != nil {
		return PublishedContentChange{}, err
	}
	if err := insertContentChangeIdentifiers(ctx, tx, command.RequestID, "candidate", command.Candidate.ExternalIdentifiers); err != nil {
		return PublishedContentChange{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PublishedContentChange{}, fmt.Errorf("commit published content change: %w", err)
	}
	return PublishedContentChange{
		ID: command.RequestID, TorrentID: command.TorrentID, BaseTorrentVersion: command.ExpectedVersion,
		Base: base, Candidate: command.Candidate, Reason: command.Reason,
		Status: PublishedContentChangePending, Version: 1, CreatedAt: command.OccurredAt,
	}, nil
}

func (repository *PostgresTorrentMaintenanceRepository) ListPublishedContentChanges(ctx context.Context, query PublishedContentChangeQuery) (ManagedPublishedContentChangePage, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ManagedPublishedContentChangePage{}, fmt.Errorf("begin published content change list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
SELECT request.id, request.torrent_id, request.uploader_id, request.base_torrent_version,
       request.base_content_sha256, request.base_description, request.base_media_info,
       request.candidate_description, request.candidate_media_info, request.reason,
       request.status, request.version, request.authorization_decision_id,
       request.created_at, request.decided_at,
       uploader.numeric_id, uploader.username, uploader.display_name, torrent.title
FROM torrents.torrent_content_change_requests AS request
JOIN identity.users AS uploader ON uploader.id=request.uploader_id
JOIN torrents.torrents AS torrent ON torrent.id=request.torrent_id
WHERE ($1='' OR request.status=$1)
ORDER BY CASE WHEN request.status='pending' THEN 0 ELSE 1 END,
         request.created_at, request.id
LIMIT $2 OFFSET $3`, query.Status, query.Limit, query.Offset)
	if err != nil {
		return ManagedPublishedContentChangePage{}, fmt.Errorf("query published content changes: %w", err)
	}
	defer rows.Close()
	type managedRecord struct {
		record                       publishedContentChangeRecord
		numericID                    int64
		username, displayName, title string
	}
	records := make([]managedRecord, 0, query.Limit)
	for rows.Next() {
		var record publishedContentChangeRecord
		var decidedAt pgtype.Timestamptz
		var numericID int64
		var username, displayName, title string
		if err := rows.Scan(
			&record.Change.ID, &record.Change.TorrentID, &record.UploaderID, &record.Change.BaseTorrentVersion,
			&record.BaseContentSHA256, &record.Change.Base.Description, &record.Change.Base.MediaInfo,
			&record.Change.Candidate.Description, &record.Change.Candidate.MediaInfo, &record.Change.Reason,
			&record.Change.Status, &record.Change.Version, &record.AuthorizationID,
			&record.Change.CreatedAt, &decidedAt, &numericID, &username, &displayName, &title,
		); err != nil {
			return ManagedPublishedContentChangePage{}, fmt.Errorf("scan published content change: %w", err)
		}
		if decidedAt.Valid {
			value := decidedAt.Time.UTC()
			record.Change.DecidedAt = &value
		}
		records = append(records, managedRecord{record: record, numericID: numericID, username: username, displayName: displayName, title: title})
	}
	if err := rows.Err(); err != nil {
		return ManagedPublishedContentChangePage{}, fmt.Errorf("iterate published content changes: %w", err)
	}
	rows.Close()
	items := make([]ManagedPublishedContentChange, 0, len(records))
	for _, item := range records {
		if err := loadContentChangeIdentifiers(ctx, tx, &item.record.Change); err != nil {
			return ManagedPublishedContentChangePage{}, err
		}
		if err := validatePublishedContentChangeRecord(item.record); err != nil {
			return ManagedPublishedContentChangePage{}, err
		}
		if item.numericID < 1 || item.username == "" || item.displayName == "" || item.title == "" {
			return ManagedPublishedContentChangePage{}, ErrTorrentReadInvariant
		}
		items = append(items, ManagedPublishedContentChange{
			PublishedContentChange: item.record.Change, UploaderNumericID: item.numericID,
			UploaderUsername: item.username, UploaderDisplayName: item.displayName, TorrentTitle: item.title,
		})
	}
	var total int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM torrents.torrent_content_change_requests
WHERE ($1='' OR status=$1)`, query.Status).Scan(&total); err != nil {
		return ManagedPublishedContentChangePage{}, fmt.Errorf("count published content changes: %w", err)
	}
	if total < int64(len(items)) {
		return ManagedPublishedContentChangePage{}, ErrTorrentReadInvariant
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedPublishedContentChangePage{}, fmt.Errorf("commit published content change list: %w", err)
	}
	return ManagedPublishedContentChangePage{Items: items, Total: total, Limit: query.Limit, Offset: query.Offset}, nil
}

func (repository *PostgresTorrentMaintenanceRepository) DecidePublishedContentChange(ctx context.Context, command DecidePublishedContentChangeCommand) (PublishedContentChangeDecisionResult, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PublishedContentChangeDecisionResult{}, fmt.Errorf("begin published content change decision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if result, found, err := readPublishedContentChangeDecisionReplay(ctx, tx, command); found || err != nil {
		if err != nil {
			return PublishedContentChangeDecisionResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return PublishedContentChangeDecisionResult{}, fmt.Errorf("commit replayed content change decision: %w", err)
		}
		return result, nil
	}

	record, err := lockPublishedContentChange(ctx, tx, command.RequestID)
	if err != nil {
		return PublishedContentChangeDecisionResult{}, err
	}
	// A staff-capable uploader still needs a second person to approve the
	// candidate. Keeping this check beside the locked immutable request closes
	// the race where staff authority or account roles change after submission.
	if record.UploaderID == command.ReviewerID {
		return PublishedContentChangeDecisionResult{}, ErrPublishedContentChangeSelfReview
	}
	if record.Change.Status != PublishedContentChangePending || record.Change.Version != command.ExpectedRequestVersion {
		return PublishedContentChangeDecisionResult{}, ErrPublishedContentChangeVersionConflict
	}
	var torrentState string
	var torrentVersion int64
	var currentDescription, currentMediaInfo string
	err = tx.QueryRow(ctx, `
SELECT state, version, description, media_info
FROM torrents.torrents
WHERE id=$1 AND uploader_id=$2
FOR UPDATE`, record.Change.TorrentID, record.UploaderID).Scan(
		&torrentState, &torrentVersion, &currentDescription, &currentMediaInfo,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PublishedContentChangeDecisionResult{}, ErrPublishedContentChangeNotFound
	}
	if err != nil {
		return PublishedContentChangeDecisionResult{}, fmt.Errorf("lock torrent for content decision: %w", err)
	}
	if State(torrentState) != StatePublished {
		return PublishedContentChangeDecisionResult{}, ErrPublishedContentChangeStateConflict
	}
	currentIdentifiers, err := listTorrentExternalIdentifiers(ctx, tx, record.Change.TorrentID)
	if err != nil {
		return PublishedContentChangeDecisionResult{}, err
	}
	current := PublishedContentSnapshot{Description: currentDescription, MediaInfo: currentMediaInfo, ExternalIdentifiers: currentIdentifiers}
	currentDigest, err := publishedContentSnapshotDigest(current)
	if err != nil || len(record.BaseContentSHA256) != len(currentDigest) || !equalDigest(record.BaseContentSHA256, currentDigest[:]) ||
		!publishedContentSnapshotsEqual(current, record.Change.Base) {
		return PublishedContentChangeDecisionResult{}, ErrPublishedContentChangeVersionConflict
	}

	resultingTorrentVersion := torrentVersion
	resultingStatus := PublishedContentChangeRejected
	if command.Decision == PublishedContentChangeApprove {
		resultingStatus = PublishedContentChangeApproved
		resultingTorrentVersion++
		updated, err := tx.Exec(ctx, `
UPDATE torrents.torrents
SET description=$2, media_info=$3, version=$4, updated_at=$5
WHERE id=$1 AND state='published' AND version=$6`, record.Change.TorrentID,
			record.Change.Candidate.Description, record.Change.Candidate.MediaInfo,
			resultingTorrentVersion, command.OccurredAt, torrentVersion)
		if err != nil {
			return PublishedContentChangeDecisionResult{}, fmt.Errorf("apply published content change: %w", err)
		}
		if updated.RowsAffected() != 1 {
			return PublishedContentChangeDecisionResult{}, ErrPublishedContentChangeVersionConflict
		}
		if _, err := tx.Exec(ctx, `DELETE FROM torrents.torrent_external_identifiers WHERE torrent_id=$1`, record.Change.TorrentID); err != nil {
			return PublishedContentChangeDecisionResult{}, fmt.Errorf("replace published content identifiers: %w", err)
		}
		for _, identifier := range record.Change.Candidate.ExternalIdentifiers {
			if _, err := tx.Exec(ctx, `
INSERT INTO torrents.torrent_external_identifiers (
    torrent_id, provider, external_id, origin, created_at, updated_at
) VALUES ($1,$2,$3,'user',$4,$4)`, record.Change.TorrentID, identifier.Provider, identifier.ExternalID, command.OccurredAt); err != nil {
				return PublishedContentChangeDecisionResult{}, fmt.Errorf("insert published content identifier: %w", err)
			}
		}
	}
	updatedRequest, err := tx.Exec(ctx, `
UPDATE torrents.torrent_content_change_requests
SET status=$2, version=2, decided_at=$3
WHERE id=$1 AND status='pending' AND version=$4`, command.RequestID, resultingStatus, command.OccurredAt, command.ExpectedRequestVersion)
	if err != nil {
		return PublishedContentChangeDecisionResult{}, fmt.Errorf("resolve published content change: %w", err)
	}
	if updatedRequest.RowsAffected() != 1 {
		return PublishedContentChangeDecisionResult{}, ErrPublishedContentChangeVersionConflict
	}
	_, err = tx.Exec(ctx, `
INSERT INTO torrents.torrent_content_change_decisions (
    id, request_id, reviewer_id, decision, reason, expected_request_version,
    resulting_request_version, observed_torrent_version, resulting_torrent_version,
    authorization_decision_id, occurred_at
) VALUES ($1,$2,$3,$4,$5,$6,2,$7,$8,$9,$10)`, command.DecisionID, command.RequestID,
		command.ReviewerID, command.Decision, command.Reason, command.ExpectedRequestVersion,
		torrentVersion, resultingTorrentVersion, command.Authorization.ID, command.OccurredAt)
	if err != nil {
		return PublishedContentChangeDecisionResult{}, mapPublishedContentChangeWriteError("insert published content decision", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PublishedContentChangeDecisionResult{}, fmt.Errorf("commit published content decision: %w", err)
	}
	return PublishedContentChangeDecisionResult{
		DecisionID: command.DecisionID, RequestID: command.RequestID, TorrentID: record.Change.TorrentID,
		Decision: command.Decision, RequestStatus: resultingStatus, RequestVersion: 2,
		TorrentVersion: resultingTorrentVersion, DecidedAt: command.OccurredAt,
	}, nil
}

func readPublishedContentChange(ctx context.Context, tx pgx.Tx, requestID uuid.UUID) (publishedContentChangeRecord, bool, error) {
	var record publishedContentChangeRecord
	var decidedAt pgtype.Timestamptz
	err := tx.QueryRow(ctx, `
SELECT id, torrent_id, uploader_id, base_torrent_version, base_content_sha256,
       base_description, base_media_info, candidate_description, candidate_media_info,
       reason, status, version, authorization_decision_id, created_at, decided_at
FROM torrents.torrent_content_change_requests
WHERE id=$1`, requestID).Scan(
		&record.Change.ID, &record.Change.TorrentID, &record.UploaderID, &record.Change.BaseTorrentVersion,
		&record.BaseContentSHA256, &record.Change.Base.Description, &record.Change.Base.MediaInfo,
		&record.Change.Candidate.Description, &record.Change.Candidate.MediaInfo,
		&record.Change.Reason, &record.Change.Status, &record.Change.Version,
		&record.AuthorizationID, &record.Change.CreatedAt, &decidedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return publishedContentChangeRecord{}, false, nil
	}
	if err != nil {
		return publishedContentChangeRecord{}, false, fmt.Errorf("read published content change: %w", err)
	}
	if decidedAt.Valid {
		value := decidedAt.Time.UTC()
		record.Change.DecidedAt = &value
	}
	if err := loadContentChangeIdentifiers(ctx, tx, &record.Change); err != nil {
		return publishedContentChangeRecord{}, false, err
	}
	if err := validatePublishedContentChangeRecord(record); err != nil {
		return publishedContentChangeRecord{}, false, err
	}
	return record, true, nil
}

func lockPublishedContentChange(ctx context.Context, tx pgx.Tx, requestID uuid.UUID) (publishedContentChangeRecord, error) {
	var record publishedContentChangeRecord
	var decidedAt pgtype.Timestamptz
	err := tx.QueryRow(ctx, `
SELECT id, torrent_id, uploader_id, base_torrent_version, base_content_sha256,
       base_description, base_media_info, candidate_description, candidate_media_info,
       reason, status, version, authorization_decision_id, created_at, decided_at
FROM torrents.torrent_content_change_requests
WHERE id=$1
FOR UPDATE`, requestID).Scan(
		&record.Change.ID, &record.Change.TorrentID, &record.UploaderID, &record.Change.BaseTorrentVersion,
		&record.BaseContentSHA256, &record.Change.Base.Description, &record.Change.Base.MediaInfo,
		&record.Change.Candidate.Description, &record.Change.Candidate.MediaInfo,
		&record.Change.Reason, &record.Change.Status, &record.Change.Version,
		&record.AuthorizationID, &record.Change.CreatedAt, &decidedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return publishedContentChangeRecord{}, ErrPublishedContentChangeNotFound
	}
	if err != nil {
		return publishedContentChangeRecord{}, fmt.Errorf("lock published content change: %w", err)
	}
	if decidedAt.Valid {
		value := decidedAt.Time.UTC()
		record.Change.DecidedAt = &value
	}
	if err := loadContentChangeIdentifiers(ctx, tx, &record.Change); err != nil {
		return publishedContentChangeRecord{}, err
	}
	if err := validatePublishedContentChangeRecord(record); err != nil {
		return publishedContentChangeRecord{}, err
	}
	return record, nil
}

func listTorrentExternalIdentifiers(ctx context.Context, tx pgx.Tx, torrentID TorrentID) ([]ExternalIdentifier, error) {
	rows, err := tx.Query(ctx, `
SELECT provider, external_id
FROM torrents.torrent_external_identifiers
WHERE torrent_id=$1
ORDER BY provider`, torrentID)
	if err != nil {
		return nil, fmt.Errorf("list torrent external identifiers: %w", err)
	}
	defer rows.Close()
	identifiers := make([]ExternalIdentifier, 0, 5)
	for rows.Next() {
		var identifier ExternalIdentifier
		if err := rows.Scan(&identifier.Provider, &identifier.ExternalID); err != nil {
			return nil, fmt.Errorf("scan torrent external identifier: %w", err)
		}
		identifiers = append(identifiers, identifier)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate torrent external identifiers: %w", err)
	}
	return normalizeExternalIdentifiers(identifiers)
}

func insertContentChangeIdentifiers(ctx context.Context, tx pgx.Tx, requestID uuid.UUID, side string, identifiers []ExternalIdentifier) error {
	for _, identifier := range identifiers {
		if _, err := tx.Exec(ctx, `
INSERT INTO torrents.torrent_content_change_identifiers (
    request_id, revision_side, provider, external_id
) VALUES ($1,$2,$3,$4)`, requestID, side, identifier.Provider, identifier.ExternalID); err != nil {
			return fmt.Errorf("insert published content change identifier: %w", err)
		}
	}
	return nil
}

func loadContentChangeIdentifiers(ctx context.Context, tx pgx.Tx, change *PublishedContentChange) error {
	rows, err := tx.Query(ctx, `
SELECT revision_side, provider, external_id
FROM torrents.torrent_content_change_identifiers
WHERE request_id=$1
ORDER BY revision_side, provider`, change.ID)
	if err != nil {
		return fmt.Errorf("list published content change identifiers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var side string
		var identifier ExternalIdentifier
		if err := rows.Scan(&side, &identifier.Provider, &identifier.ExternalID); err != nil {
			return fmt.Errorf("scan published content change identifier: %w", err)
		}
		switch side {
		case "base":
			change.Base.ExternalIdentifiers = append(change.Base.ExternalIdentifiers, identifier)
		case "candidate":
			change.Candidate.ExternalIdentifiers = append(change.Candidate.ExternalIdentifiers, identifier)
		default:
			return ErrTorrentReadInvariant
		}
	}
	return rows.Err()
}

func validatePublishedContentChangeRecord(record publishedContentChangeRecord) error {
	if record.Change.ID == uuid.Nil || record.Change.TorrentID < 1 || record.UploaderID == uuid.Nil ||
		record.Change.BaseTorrentVersion < 1 || len(record.BaseContentSHA256) != sha256Size ||
		record.AuthorizationID == uuid.Nil || record.Change.CreatedAt.IsZero() ||
		record.Change.DescriptionInvalid() {
		return ErrTorrentReadInvariant
	}
	switch record.Change.Status {
	case PublishedContentChangePending:
		if record.Change.Version != 1 || record.Change.DecidedAt != nil {
			return ErrTorrentReadInvariant
		}
	case PublishedContentChangeApproved, PublishedContentChangeRejected:
		if record.Change.Version != 2 || record.Change.DecidedAt == nil {
			return ErrTorrentReadInvariant
		}
	default:
		return ErrTorrentReadInvariant
	}
	return nil
}

const sha256Size = 32

func (change PublishedContentChange) DescriptionInvalid() bool {
	baseIdentifiers, baseErr := normalizeExternalIdentifiers(change.Base.ExternalIdentifiers)
	candidateIdentifiers, candidateErr := normalizeExternalIdentifiers(change.Candidate.ExternalIdentifiers)
	return baseErr != nil || candidateErr != nil || change.Candidate.Description == "" ||
		!validTorrentContent(change.Base.Description, change.Base.MediaInfo) ||
		!validTorrentContent(change.Candidate.Description, change.Candidate.MediaInfo) ||
		len(baseIdentifiers) != len(change.Base.ExternalIdentifiers) || len(candidateIdentifiers) != len(change.Candidate.ExternalIdentifiers)
}

func publishedContentSnapshotsEqual(left, right PublishedContentSnapshot) bool {
	if left.Description != right.Description || left.MediaInfo != right.MediaInfo || len(left.ExternalIdentifiers) != len(right.ExternalIdentifiers) {
		return false
	}
	for index := range left.ExternalIdentifiers {
		if left.ExternalIdentifiers[index] != right.ExternalIdentifiers[index] {
			return false
		}
	}
	return true
}

func equalDigest(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var difference byte
	for index := range left {
		difference |= left[index] ^ right[index]
	}
	return difference == 0
}

func readPublishedContentChangeDecisionReplay(ctx context.Context, tx pgx.Tx, command DecidePublishedContentChangeCommand) (PublishedContentChangeDecisionResult, bool, error) {
	var result PublishedContentChangeDecisionResult
	var reviewerID uuid.UUID
	var reason string
	var expectedVersion int64
	err := tx.QueryRow(ctx, `
SELECT decision.id, decision.request_id, request.torrent_id, decision.reviewer_id,
       decision.decision, decision.reason, decision.expected_request_version,
       decision.resulting_request_version, decision.resulting_torrent_version,
       decision.occurred_at, request.status
FROM torrents.torrent_content_change_decisions AS decision
JOIN torrents.torrent_content_change_requests AS request ON request.id=decision.request_id
WHERE decision.id=$1`, command.DecisionID).Scan(
		&result.DecisionID, &result.RequestID, &result.TorrentID, &reviewerID,
		&result.Decision, &reason, &expectedVersion, &result.RequestVersion,
		&result.TorrentVersion, &result.DecidedAt, &result.RequestStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PublishedContentChangeDecisionResult{}, false, nil
	}
	if err != nil {
		return PublishedContentChangeDecisionResult{}, false, fmt.Errorf("read published content decision replay: %w", err)
	}
	if result.RequestID != command.RequestID || reviewerID != command.ReviewerID || result.Decision != command.Decision ||
		reason != command.Reason || expectedVersion != command.ExpectedRequestVersion {
		return PublishedContentChangeDecisionResult{}, true, ErrPublishedContentChangeIdempotencyConflict
	}
	return result, true, nil
}

func mapPublishedContentChangeWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		switch postgresError.ConstraintName {
		case "torrent_content_change_requests_pkey", "torrent_content_change_decisions_pkey":
			return ErrPublishedContentChangeIdempotencyConflict
		case "torrent_content_change_one_pending_idx":
			return ErrPublishedContentChangePending
		case "torrent_content_change_decisions_request_id_key":
			return ErrPublishedContentChangeVersionConflict
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ TorrentMaintenanceRepository = (*PostgresTorrentMaintenanceRepository)(nil)
