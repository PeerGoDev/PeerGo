package torrents

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type publishedScreenshotChangeRecord struct {
	Change             PublishedScreenshotChange
	UploaderID         uuid.UUID
	BaseSetID          *uuid.UUID
	CandidateSetID     uuid.UUID
	BaseDigest         []byte
	CandidateDigest    []byte
	RequestFingerprint []byte
	PolicyRevisionID   uuid.UUID
	AuthorizationID    uuid.UUID
}

func (repository *PostgresTorrentMaintenanceRepository) SubmitPublishedScreenshotChange(ctx context.Context, command SubmitPublishedScreenshotChangeCommand) (PublishedScreenshotChange, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PublishedScreenshotChange{}, fmt.Errorf("begin published screenshot change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if replay, found, err := readPublishedScreenshotChange(ctx, tx, command.RequestID, false); err != nil {
		return PublishedScreenshotChange{}, err
	} else if found {
		if replay.UploaderID != command.UploaderID || replay.Change.TorrentID != command.TorrentID ||
			replay.Change.BaseTorrentVersion != command.ExpectedVersion || replay.Change.Reason != command.Reason ||
			len(replay.RequestFingerprint) != sha256.Size || !equalDigest(replay.RequestFingerprint, command.RequestFingerprint[:]) {
			return PublishedScreenshotChange{}, ErrPublishedScreenshotChangeIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return PublishedScreenshotChange{}, fmt.Errorf("commit replayed published screenshot change: %w", err)
		}
		return replay.Change, nil
	}

	var state string
	var torrentVersion int64
	err = tx.QueryRow(ctx, `SELECT state, version FROM torrents.torrents
		WHERE id=$1 AND uploader_id=$2 FOR UPDATE`, command.TorrentID, command.UploaderID).Scan(&state, &torrentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return PublishedScreenshotChange{}, ErrPublishedScreenshotChangeNotFound
	}
	if err != nil {
		return PublishedScreenshotChange{}, fmt.Errorf("lock torrent for screenshot change: %w", err)
	}
	if State(state) != StatePublished {
		return PublishedScreenshotChange{}, ErrPublishedScreenshotChangeStateConflict
	}
	if torrentVersion != command.ExpectedVersion {
		return PublishedScreenshotChange{}, ErrPublishedScreenshotChangeVersionConflict
	}

	baseSetID, baseSetVersion, baseObjectIDs, err := lockCurrentScreenshotSet(ctx, tx, command.TorrentID)
	if err != nil {
		return PublishedScreenshotChange{}, err
	}
	resolvedUploads := make([]uuid.UUID, 0, len(command.Uploads))
	for _, upload := range command.Uploads {
		objectID, err := resolveScreenshotChangeObject(ctx, tx, upload, command.OccurredAt)
		if err != nil {
			return PublishedScreenshotChange{}, err
		}
		resolvedUploads = append(resolvedUploads, objectID)
	}
	candidateObjectIDs := make([]uuid.UUID, 0, len(command.Manifest))
	seen := make(map[uuid.UUID]struct{}, len(command.Manifest))
	for _, item := range command.Manifest {
		var objectID uuid.UUID
		switch item.Source {
		case ScreenshotManifestExisting:
			if item.Index < 0 || item.Index >= len(baseObjectIDs) {
				return PublishedScreenshotChange{}, ErrPublishedScreenshotChangeInput
			}
			objectID = baseObjectIDs[item.Index]
		case ScreenshotManifestUpload:
			if item.Index < 0 || item.Index >= len(resolvedUploads) {
				return PublishedScreenshotChange{}, ErrPublishedScreenshotChangeInput
			}
			objectID = resolvedUploads[item.Index]
		default:
			return PublishedScreenshotChange{}, ErrPublishedScreenshotChangeInput
		}
		if _, duplicate := seen[objectID]; duplicate {
			return PublishedScreenshotChange{}, ErrPublishedScreenshotChangeInput
		}
		seen[objectID] = struct{}{}
		candidateObjectIDs = append(candidateObjectIDs, objectID)
	}
	baseDigest := screenshotSetDigest(baseObjectIDs)
	candidateDigest := screenshotSetDigest(candidateObjectIDs)
	if baseDigest == candidateDigest {
		return PublishedScreenshotChange{}, ErrPublishedScreenshotChangeUnchanged
	}

	candidateSetID := command.RequestID
	if _, err := tx.Exec(ctx, `INSERT INTO torrents.torrent_screenshot_sets (
		id, torrent_id, origin, created_at
	) VALUES ($1,$2,'change',$3)`, candidateSetID, command.TorrentID, command.OccurredAt); err != nil {
		return PublishedScreenshotChange{}, mapPublishedScreenshotChangeWriteError("insert screenshot candidate set", err)
	}
	for position, objectID := range candidateObjectIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO torrents.torrent_screenshot_set_items (
			set_id, object_id, position, created_at
		) VALUES ($1,$2,$3,$4)`, candidateSetID, objectID, position, command.OccurredAt); err != nil {
			return PublishedScreenshotChange{}, mapPublishedScreenshotChangeWriteError("insert screenshot candidate item", err)
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO torrents.torrent_screenshot_change_requests (
		id, torrent_id, uploader_id, base_torrent_version, base_set_id, base_set_version,
		candidate_set_id, base_set_sha256, candidate_set_sha256, request_fingerprint,
		upload_policy_revision_id, reason, status, version, authorization_decision_id, created_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'pending',1,$13,$14)`,
		command.RequestID, command.TorrentID, command.UploaderID, command.ExpectedVersion,
		baseSetID, baseSetVersion, candidateSetID, baseDigest[:], candidateDigest[:], command.RequestFingerprint[:],
		command.PolicyRevisionID, command.Reason, command.Authorization.ID, command.OccurredAt)
	if err != nil {
		return PublishedScreenshotChange{}, mapPublishedScreenshotChangeWriteError("insert published screenshot change", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PublishedScreenshotChange{}, fmt.Errorf("commit published screenshot change: %w", err)
	}
	return PublishedScreenshotChange{
		ID: command.RequestID, TorrentID: command.TorrentID, BaseTorrentVersion: command.ExpectedVersion,
		BaseSetVersion: baseSetVersion, BaseCount: len(baseObjectIDs), CandidateCount: len(candidateObjectIDs),
		Reason: command.Reason, Status: PublishedScreenshotChangePending, Version: 1, CreatedAt: command.OccurredAt,
	}, nil
}

func (repository *PostgresTorrentMaintenanceRepository) ListPublishedScreenshotChanges(ctx context.Context, query PublishedScreenshotChangeQuery) (ManagedPublishedScreenshotChangePage, error) {
	rows, err := repository.pool.Query(ctx, `SELECT request.id, request.torrent_id, request.uploader_id,
		request.base_torrent_version, request.base_set_id, request.base_set_version, request.candidate_set_id,
		request.base_set_sha256, request.candidate_set_sha256, request.request_fingerprint,
		request.upload_policy_revision_id, request.reason, request.status, request.version,
		request.authorization_decision_id, request.created_at, request.decided_at,
		(SELECT count(*) FROM torrents.torrent_screenshot_set_items WHERE set_id=request.base_set_id)::integer,
		(SELECT count(*) FROM torrents.torrent_screenshot_set_items WHERE set_id=request.candidate_set_id)::integer,
		uploader.numeric_id, uploader.username, uploader.display_name, torrent.title
	FROM torrents.torrent_screenshot_change_requests AS request
	JOIN identity.users AS uploader ON uploader.id=request.uploader_id
	JOIN torrents.torrents AS torrent ON torrent.id=request.torrent_id
	WHERE ($1='' OR request.status=$1)
	ORDER BY CASE WHEN request.status='pending' THEN 0 ELSE 1 END, request.created_at, request.id
	LIMIT $2 OFFSET $3`, query.Status, query.Limit, query.Offset)
	if err != nil {
		return ManagedPublishedScreenshotChangePage{}, fmt.Errorf("query published screenshot changes: %w", err)
	}
	defer rows.Close()
	items := make([]ManagedPublishedScreenshotChange, 0, query.Limit)
	for rows.Next() {
		var record publishedScreenshotChangeRecord
		var baseSetID pgtype.UUID
		var decidedAt pgtype.Timestamptz
		var numericID int64
		var username, displayName, title string
		if err := rows.Scan(
			&record.Change.ID, &record.Change.TorrentID, &record.UploaderID,
			&record.Change.BaseTorrentVersion, &baseSetID, &record.Change.BaseSetVersion, &record.CandidateSetID,
			&record.BaseDigest, &record.CandidateDigest, &record.RequestFingerprint,
			&record.PolicyRevisionID, &record.Change.Reason, &record.Change.Status, &record.Change.Version,
			&record.AuthorizationID, &record.Change.CreatedAt, &decidedAt,
			&record.Change.BaseCount, &record.Change.CandidateCount,
			&numericID, &username, &displayName, &title,
		); err != nil {
			return ManagedPublishedScreenshotChangePage{}, fmt.Errorf("scan published screenshot change: %w", err)
		}
		if baseSetID.Valid {
			value := uuid.UUID(baseSetID.Bytes)
			record.BaseSetID = &value
		}
		if decidedAt.Valid {
			value := decidedAt.Time.UTC()
			record.Change.DecidedAt = &value
		}
		if err := validatePublishedScreenshotChangeRecord(record); err != nil || numericID < 1 || username == "" || displayName == "" || title == "" {
			return ManagedPublishedScreenshotChangePage{}, ErrTorrentReadInvariant
		}
		items = append(items, ManagedPublishedScreenshotChange{
			PublishedScreenshotChange: record.Change, UploaderNumericID: numericID,
			UploaderUsername: username, UploaderDisplayName: displayName, TorrentTitle: title,
		})
	}
	if err := rows.Err(); err != nil {
		return ManagedPublishedScreenshotChangePage{}, fmt.Errorf("iterate published screenshot changes: %w", err)
	}
	var total int64
	if err := repository.pool.QueryRow(ctx, `SELECT count(*) FROM torrents.torrent_screenshot_change_requests WHERE ($1='' OR status=$1)`, query.Status).Scan(&total); err != nil {
		return ManagedPublishedScreenshotChangePage{}, fmt.Errorf("count published screenshot changes: %w", err)
	}
	return ManagedPublishedScreenshotChangePage{Items: items, Total: total, Limit: query.Limit, Offset: query.Offset}, nil
}

func (repository *PostgresTorrentMaintenanceRepository) DecidePublishedScreenshotChange(ctx context.Context, command DecidePublishedScreenshotChangeCommand) (PublishedScreenshotChangeDecisionResult, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PublishedScreenshotChangeDecisionResult{}, fmt.Errorf("begin published screenshot decision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if replay, found, err := readPublishedScreenshotDecisionReplay(ctx, tx, command); found || err != nil {
		if err != nil {
			return PublishedScreenshotChangeDecisionResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return PublishedScreenshotChangeDecisionResult{}, fmt.Errorf("commit replayed screenshot decision: %w", err)
		}
		return replay, nil
	}
	record, found, err := readPublishedScreenshotChange(ctx, tx, command.RequestID, true)
	if err != nil {
		return PublishedScreenshotChangeDecisionResult{}, err
	}
	if !found {
		return PublishedScreenshotChangeDecisionResult{}, ErrPublishedScreenshotChangeNotFound
	}
	if record.UploaderID == command.ReviewerID {
		return PublishedScreenshotChangeDecisionResult{}, ErrPublishedScreenshotChangeSelfReview
	}
	if record.Change.Status != PublishedScreenshotChangePending || record.Change.Version != command.ExpectedRequestVersion {
		return PublishedScreenshotChangeDecisionResult{}, ErrPublishedScreenshotChangeVersionConflict
	}
	var torrentState string
	if err := tx.QueryRow(ctx, `SELECT state FROM torrents.torrents WHERE id=$1 FOR UPDATE`, record.Change.TorrentID).Scan(&torrentState); errors.Is(err, pgx.ErrNoRows) {
		return PublishedScreenshotChangeDecisionResult{}, ErrPublishedScreenshotChangeNotFound
	} else if err != nil {
		return PublishedScreenshotChangeDecisionResult{}, fmt.Errorf("lock torrent for screenshot decision: %w", err)
	}
	if State(torrentState) != StatePublished {
		return PublishedScreenshotChangeDecisionResult{}, ErrPublishedScreenshotChangeStateConflict
	}
	currentSetID, currentVersion, currentObjects, err := lockCurrentScreenshotSet(ctx, tx, record.Change.TorrentID)
	if err != nil {
		return PublishedScreenshotChangeDecisionResult{}, err
	}
	currentDigest := screenshotSetDigest(currentObjects)
	if currentVersion != record.Change.BaseSetVersion || !optionalUUIDEqual(currentSetID, record.BaseSetID) ||
		len(record.BaseDigest) != sha256.Size || !equalDigest(record.BaseDigest, currentDigest[:]) {
		return PublishedScreenshotChangeDecisionResult{}, ErrPublishedScreenshotChangeVersionConflict
	}

	resultingStatus := PublishedScreenshotChangeRejected
	resultingSetVersion := currentVersion
	if command.Decision == PublishedScreenshotChangeApprove {
		resultingStatus = PublishedScreenshotChangeApproved
		resultingSetVersion++
		if currentVersion == 0 {
			if _, err := tx.Exec(ctx, `INSERT INTO torrents.torrent_screenshot_set_heads (
				torrent_id, active_set_id, version, updated_at
			) VALUES ($1,$2,1,$3)`, record.Change.TorrentID, record.CandidateSetID, command.OccurredAt); err != nil {
				return PublishedScreenshotChangeDecisionResult{}, fmt.Errorf("activate first screenshot set: %w", err)
			}
		} else {
			updated, err := tx.Exec(ctx, `UPDATE torrents.torrent_screenshot_set_heads
				SET active_set_id=$2, version=version+1, updated_at=$3
				WHERE torrent_id=$1 AND active_set_id=$4 AND version=$5`, record.Change.TorrentID,
				record.CandidateSetID, command.OccurredAt, *currentSetID, currentVersion)
			if err != nil {
				return PublishedScreenshotChangeDecisionResult{}, fmt.Errorf("switch screenshot set: %w", err)
			}
			if updated.RowsAffected() != 1 {
				return PublishedScreenshotChangeDecisionResult{}, ErrPublishedScreenshotChangeVersionConflict
			}
		}
	}
	updated, err := tx.Exec(ctx, `UPDATE torrents.torrent_screenshot_change_requests
		SET status=$2, version=2, decided_at=$3
		WHERE id=$1 AND status='pending' AND version=$4`, command.RequestID, resultingStatus, command.OccurredAt, command.ExpectedRequestVersion)
	if err != nil {
		return PublishedScreenshotChangeDecisionResult{}, fmt.Errorf("resolve published screenshot change: %w", err)
	}
	if updated.RowsAffected() != 1 {
		return PublishedScreenshotChangeDecisionResult{}, ErrPublishedScreenshotChangeVersionConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO torrents.torrent_screenshot_change_decisions (
		id, request_id, reviewer_id, decision, reason, expected_request_version,
		resulting_request_version, observed_set_version, resulting_set_version,
		authorization_decision_id, occurred_at
	) VALUES ($1,$2,$3,$4,$5,$6,2,$7,$8,$9,$10)`, command.DecisionID, command.RequestID,
		command.ReviewerID, command.Decision, command.Reason, command.ExpectedRequestVersion,
		currentVersion, resultingSetVersion, command.Authorization.ID, command.OccurredAt)
	if err != nil {
		return PublishedScreenshotChangeDecisionResult{}, mapPublishedScreenshotChangeWriteError("insert screenshot decision", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PublishedScreenshotChangeDecisionResult{}, fmt.Errorf("commit published screenshot decision: %w", err)
	}
	return PublishedScreenshotChangeDecisionResult{
		DecisionID: command.DecisionID, RequestID: command.RequestID, TorrentID: record.Change.TorrentID,
		Decision: command.Decision, RequestStatus: resultingStatus, RequestVersion: 2,
		AttachmentVersion: resultingSetVersion, DecidedAt: command.OccurredAt,
	}, nil
}

func (repository *PostgresTorrentMaintenanceRepository) PublishedScreenshotChangeSource(ctx context.Context, requestID uuid.UUID, side ScreenshotChangeSide, position int) (PublicScreenshotSource, error) {
	var torrentID int64
	var objectID uuid.UUID
	var digest []byte
	var byteLength int64
	var contentType string
	var width, height int32
	err := repository.pool.QueryRow(ctx, `SELECT request.torrent_id, object.id, object.content_sha256,
		object.byte_length, object.content_type, object.width, object.height
	FROM torrents.torrent_screenshot_change_requests AS request
	JOIN torrents.torrent_screenshot_set_items AS item
	  ON item.set_id = CASE WHEN $2='base' THEN request.base_set_id ELSE request.candidate_set_id END
	 AND item.position=$3
	JOIN torrents.torrent_screenshot_objects AS object ON object.id=item.object_id
	WHERE request.id=$1 AND $2 IN ('base','candidate')`, requestID, side, position).Scan(
		&torrentID, &objectID, &digest, &byteLength, &contentType, &width, &height,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PublicScreenshotSource{}, ErrPublishedScreenshotChangeNotFound
	}
	if err != nil {
		return PublicScreenshotSource{}, fmt.Errorf("read screenshot change object: %w", err)
	}
	if len(digest) != sha256.Size || objectID == uuid.Nil || byteLength < 1 || byteLength > MaxStoredTorrentScreenshotBytes || !supportedStoredScreenshotType(contentType) {
		return PublicScreenshotSource{}, ErrTorrentReadInvariant
	}
	locations, err := readScreenshotChangeLocations(ctx, repository.pool, objectID)
	if err != nil {
		return PublicScreenshotSource{}, err
	}
	var contentDigest ObjectSHA256
	copy(contentDigest[:], digest)
	return PublicScreenshotSource{
		TorrentID: TorrentID(torrentID), Position: position, ObjectID: objectID,
		ContentType: contentType, Width: int(width), Height: int(height),
		Descriptor: StoredObjectDescriptor{SHA256: contentDigest, ByteLength: byteLength}, Locations: locations,
	}, nil
}

func lockCurrentScreenshotSet(ctx context.Context, tx pgx.Tx, torrentID TorrentID) (*uuid.UUID, int64, []uuid.UUID, error) {
	var setID uuid.UUID
	var version int64
	err := tx.QueryRow(ctx, `SELECT active_set_id, version FROM torrents.torrent_screenshot_set_heads
		WHERE torrent_id=$1 FOR UPDATE`, torrentID).Scan(&setID, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, nil, nil
	}
	if err != nil {
		return nil, 0, nil, fmt.Errorf("lock current torrent screenshot set: %w", err)
	}
	objects, err := listScreenshotSetObjectIDs(ctx, tx, setID)
	if err != nil {
		return nil, 0, nil, err
	}
	return &setID, version, objects, nil
}

func listScreenshotSetObjectIDs(ctx context.Context, tx pgx.Tx, setID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT object_id FROM torrents.torrent_screenshot_set_items WHERE set_id=$1 ORDER BY position`, setID)
	if err != nil {
		return nil, fmt.Errorf("list torrent screenshot set: %w", err)
	}
	defer rows.Close()
	objects := make([]uuid.UUID, 0, MaxTorrentScreenshots)
	for rows.Next() {
		var objectID uuid.UUID
		if err := rows.Scan(&objectID); err != nil {
			return nil, fmt.Errorf("scan torrent screenshot set: %w", err)
		}
		objects = append(objects, objectID)
	}
	if err := rows.Err(); err != nil || len(objects) > MaxTorrentScreenshots {
		return nil, ErrTorrentReadInvariant
	}
	return objects, nil
}

func resolveScreenshotChangeObject(ctx context.Context, tx pgx.Tx, screenshot storedTorrentScreenshot, occurredAt time.Time) (uuid.UUID, error) {
	var objectID uuid.UUID
	err := tx.QueryRow(ctx, `WITH inserted AS (
		INSERT INTO torrents.torrent_screenshot_objects (
			id, content_sha256, byte_length, content_type, width, height, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (content_sha256) DO NOTHING RETURNING id
	) SELECT id FROM inserted UNION ALL
	SELECT id FROM torrents.torrent_screenshot_objects WHERE content_sha256=$2 LIMIT 1`,
		screenshot.ID, screenshot.ContentSHA256[:], screenshot.ByteLength, screenshot.ContentType,
		screenshot.Width, screenshot.Height, occurredAt).Scan(&objectID)
	if err != nil {
		return uuid.Nil, mapPublishedScreenshotChangeWriteError("resolve screenshot change object", err)
	}
	result, err := tx.Exec(ctx, `INSERT INTO torrents.torrent_screenshot_object_locations (
		id, object_id, backend_id, object_key, version_id, observed_byte_length,
		observed_sha256, verified_at, created_at, updated_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8,$8)
	ON CONFLICT (object_id, backend_id) DO NOTHING`, uuid.New(), objectID, screenshot.BackendID,
		screenshot.ObjectKey, nullableStorageText(screenshot.StorageVersionID), screenshot.ByteLength,
		screenshot.ContentSHA256[:], occurredAt)
	if err != nil {
		return uuid.Nil, mapPublishedScreenshotChangeWriteError("insert screenshot change location", err)
	}
	if result.RowsAffected() == 0 {
		var matches bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM torrents.torrent_screenshot_object_locations
			WHERE object_id=$1 AND backend_id=$2 AND object_key=$3 AND observed_byte_length=$4 AND observed_sha256=$5)`,
			objectID, screenshot.BackendID, screenshot.ObjectKey, screenshot.ByteLength, screenshot.ContentSHA256[:]).Scan(&matches); err != nil || !matches {
			return uuid.Nil, ErrPublishedScreenshotChangeVersionConflict
		}
	}
	return objectID, nil
}

func screenshotSetDigest(objects []uuid.UUID) [sha256.Size]byte {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte("peergo:torrent-screenshot-set:v1\x00"))
	for _, objectID := range objects {
		_, _ = hasher.Write(objectID[:])
	}
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest
}

func optionalUUIDEqual(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func readPublishedScreenshotChange(ctx context.Context, tx pgx.Tx, requestID uuid.UUID, lock bool) (publishedScreenshotChangeRecord, bool, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	var record publishedScreenshotChangeRecord
	var baseSetID pgtype.UUID
	var decidedAt pgtype.Timestamptz
	err := tx.QueryRow(ctx, `SELECT request.id, request.torrent_id, request.uploader_id,
		request.base_torrent_version, request.base_set_id, request.base_set_version, request.candidate_set_id,
		request.base_set_sha256, request.candidate_set_sha256, request.request_fingerprint,
		request.upload_policy_revision_id, request.reason, request.status, request.version,
		request.authorization_decision_id, request.created_at, request.decided_at,
		(SELECT count(*) FROM torrents.torrent_screenshot_set_items WHERE set_id=request.base_set_id)::integer,
		(SELECT count(*) FROM torrents.torrent_screenshot_set_items WHERE set_id=request.candidate_set_id)::integer
	FROM torrents.torrent_screenshot_change_requests AS request WHERE request.id=$1`+suffix, requestID).Scan(
		&record.Change.ID, &record.Change.TorrentID, &record.UploaderID,
		&record.Change.BaseTorrentVersion, &baseSetID, &record.Change.BaseSetVersion, &record.CandidateSetID,
		&record.BaseDigest, &record.CandidateDigest, &record.RequestFingerprint,
		&record.PolicyRevisionID, &record.Change.Reason, &record.Change.Status, &record.Change.Version,
		&record.AuthorizationID, &record.Change.CreatedAt, &decidedAt,
		&record.Change.BaseCount, &record.Change.CandidateCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return publishedScreenshotChangeRecord{}, false, nil
	}
	if err != nil {
		return publishedScreenshotChangeRecord{}, false, fmt.Errorf("read published screenshot change: %w", err)
	}
	if baseSetID.Valid {
		value := uuid.UUID(baseSetID.Bytes)
		record.BaseSetID = &value
	}
	if decidedAt.Valid {
		value := decidedAt.Time.UTC()
		record.Change.DecidedAt = &value
	}
	if err := validatePublishedScreenshotChangeRecord(record); err != nil {
		return publishedScreenshotChangeRecord{}, false, err
	}
	return record, true, nil
}

func validatePublishedScreenshotChangeRecord(record publishedScreenshotChangeRecord) error {
	if record.Change.ID == uuid.Nil || record.Change.TorrentID < 1 || record.UploaderID == uuid.Nil ||
		record.Change.BaseTorrentVersion < 1 || record.CandidateSetID == uuid.Nil || record.PolicyRevisionID == uuid.Nil ||
		record.AuthorizationID == uuid.Nil || len(record.BaseDigest) != sha256.Size || len(record.CandidateDigest) != sha256.Size ||
		len(record.RequestFingerprint) != sha256.Size || record.Change.BaseCount < 0 || record.Change.BaseCount > MaxTorrentScreenshots ||
		record.Change.CandidateCount < 0 || record.Change.CandidateCount > MaxTorrentScreenshots || record.Change.CreatedAt.IsZero() {
		return ErrTorrentReadInvariant
	}
	if (record.BaseSetID == nil && record.Change.BaseSetVersion != 0) || (record.BaseSetID != nil && record.Change.BaseSetVersion < 1) {
		return ErrTorrentReadInvariant
	}
	switch record.Change.Status {
	case PublishedScreenshotChangePending:
		if record.Change.Version != 1 || record.Change.DecidedAt != nil {
			return ErrTorrentReadInvariant
		}
	case PublishedScreenshotChangeApproved, PublishedScreenshotChangeRejected:
		if record.Change.Version != 2 || record.Change.DecidedAt == nil {
			return ErrTorrentReadInvariant
		}
	default:
		return ErrTorrentReadInvariant
	}
	return nil
}

func readPublishedScreenshotDecisionReplay(ctx context.Context, tx pgx.Tx, command DecidePublishedScreenshotChangeCommand) (PublishedScreenshotChangeDecisionResult, bool, error) {
	var result PublishedScreenshotChangeDecisionResult
	var reviewerID uuid.UUID
	var reason string
	var expected int64
	err := tx.QueryRow(ctx, `SELECT decision.id, decision.request_id, request.torrent_id,
		decision.reviewer_id, decision.decision, decision.reason, decision.expected_request_version,
		decision.resulting_request_version, decision.resulting_set_version, decision.occurred_at, request.status
	FROM torrents.torrent_screenshot_change_decisions AS decision
	JOIN torrents.torrent_screenshot_change_requests AS request ON request.id=decision.request_id
	WHERE decision.id=$1`, command.DecisionID).Scan(
		&result.DecisionID, &result.RequestID, &result.TorrentID, &reviewerID, &result.Decision, &reason,
		&expected, &result.RequestVersion, &result.AttachmentVersion, &result.DecidedAt, &result.RequestStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PublishedScreenshotChangeDecisionResult{}, false, nil
	}
	if err != nil {
		return PublishedScreenshotChangeDecisionResult{}, false, fmt.Errorf("read screenshot decision replay: %w", err)
	}
	if result.RequestID != command.RequestID || reviewerID != command.ReviewerID || result.Decision != command.Decision ||
		reason != command.Reason || expected != command.ExpectedRequestVersion {
		return PublishedScreenshotChangeDecisionResult{}, false, ErrPublishedScreenshotChangeIdempotencyConflict
	}
	return result, true, nil
}

func readScreenshotChangeLocations(ctx context.Context, querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, objectID uuid.UUID) ([]ReadableObjectLocation, error) {
	rows, err := querier.Query(ctx, `SELECT id, backend_id, object_key, state, is_preferred,
		version_id, observed_byte_length, observed_sha256, verified_at
	FROM torrents.torrent_screenshot_object_locations
	WHERE object_id=$1 AND state IN ('verified','retiring')
	ORDER BY is_preferred DESC, (state='verified') DESC, verified_at DESC, id`, objectID)
	if err != nil {
		return nil, fmt.Errorf("list screenshot change locations: %w", err)
	}
	defer rows.Close()
	locations := make([]ReadableObjectLocation, 0)
	for rows.Next() {
		var location ReadableObjectLocation
		var backend, key, state string
		var version pgtype.Text
		var digest []byte
		if err := rows.Scan(&location.ID, &backend, &key, &state, &location.Preferred,
			&version, &location.Descriptor.ByteLength, &digest, &location.VerifiedAt); err != nil {
			return nil, fmt.Errorf("scan screenshot change location: %w", err)
		}
		backendID, backendErr := ParseStorageBackendID(backend)
		objectKey, keyErr := ParseObjectKey(key)
		if backendErr != nil || keyErr != nil || len(digest) != sha256.Size || location.ID == uuid.Nil {
			return nil, ErrTorrentReadInvariant
		}
		location.BackendID, location.ObjectKey = backendID, objectKey
		location.VersionID = nullableStorageString(version)
		copy(location.Descriptor.SHA256[:], digest)
		location.VerifiedAt = location.VerifiedAt.UTC()
		locations = append(locations, location)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate screenshot change locations: %w", err)
	}
	return locations, nil
}

func mapPublishedScreenshotChangeWriteError(operation string, err error) error {
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) {
		switch databaseError.Code {
		case "23505":
			if databaseError.ConstraintName == "torrent_screenshot_change_one_pending_idx" {
				return ErrPublishedScreenshotChangePending
			}
			return ErrPublishedScreenshotChangeIdempotencyConflict
		case "23503", "23514":
			return ErrPublishedScreenshotChangeInput
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
