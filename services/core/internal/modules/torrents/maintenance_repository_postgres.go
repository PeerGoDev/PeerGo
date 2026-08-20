package torrents

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/contracts/trackerevent"
)

type PostgresTorrentMaintenanceRepository struct {
	pool               *pgxpool.Pool
	eventBuilder       TorrentLifecycleEventBuilder
	newAuditAppender   func(pgx.Tx) auditevent.Appender
	eligibilityBuilder TorrentLifecycleEligibilityEventBuilder
	newTrackerAppender func(pgx.Tx) trackerevent.Appender
}

func NewPostgresTorrentMaintenanceRepository(
	pool *pgxpool.Pool,
	eventBuilder TorrentLifecycleEventBuilder,
	newAuditAppender func(pgx.Tx) auditevent.Appender,
	eligibilityBuilder TorrentLifecycleEligibilityEventBuilder,
	newTrackerAppender func(pgx.Tx) trackerevent.Appender,
) (*PostgresTorrentMaintenanceRepository, error) {
	if pool == nil || eventBuilder == nil || newAuditAppender == nil || eligibilityBuilder == nil || newTrackerAppender == nil {
		return nil, errors.New("torrent maintenance dependencies are required")
	}
	return &PostgresTorrentMaintenanceRepository{
		pool: pool, eventBuilder: eventBuilder, newAuditAppender: newAuditAppender,
		eligibilityBuilder: eligibilityBuilder, newTrackerAppender: newTrackerAppender,
	}, nil
}

func (repository *PostgresTorrentMaintenanceRepository) UpdatePublishedMetadata(ctx context.Context, command UpdatePublishedMetadataCommand) (PublishedMetadataRevision, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PublishedMetadataRevision{}, wrapTorrentMetadataUpdate("begin", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Request UUID is the immutable command identity. Check it before locking the
	// aggregate so a response-loss replay returns the original version, while a
	// changed payload with the same key fails closed.
	if replay, replayUploaderID, found, err := readPublishedMetadataReplay(ctx, tx, command.RequestID); err != nil {
		return PublishedMetadataRevision{}, err
	} else if found {
		if replayUploaderID != command.UploaderID || replay.TorrentID != command.TorrentID || replay.Version != command.ExpectedVersion+1 ||
			replay.Metadata != command.Metadata || replay.Reason != command.Reason {
			return PublishedMetadataRevision{}, ErrTorrentMetadataUpdateIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return PublishedMetadataRevision{}, wrapTorrentMetadataUpdate("commit replay", err)
		}
		return replay, nil
	}

	var state string
	var version int64
	var categoryID, title, subtitle string
	err = tx.QueryRow(ctx, `
SELECT state, version, category_id, title, subtitle
FROM torrents.torrents
WHERE id=$1 AND uploader_id=$2
FOR UPDATE`, command.TorrentID, command.UploaderID).Scan(&state, &version, &categoryID, &title, &subtitle)
	if errors.Is(err, pgx.ErrNoRows) {
		return PublishedMetadataRevision{}, ErrTorrentMetadataUpdateNotFound
	}
	if err != nil {
		return PublishedMetadataRevision{}, wrapTorrentMetadataUpdate("lock aggregate", err)
	}
	if State(state) != StatePublished {
		return PublishedMetadataRevision{}, ErrTorrentMetadataUpdateStateConflict
	}
	if version != command.ExpectedVersion {
		return PublishedMetadataRevision{}, ErrTorrentMetadataUpdateVersionConflict
	}
	if categoryID == command.Metadata.CategoryID && title == command.Metadata.Title && subtitle == command.Metadata.Subtitle {
		return PublishedMetadataRevision{}, ErrTorrentMetadataUpdateUnchanged
	}
	var categoryEnabled bool
	if err := tx.QueryRow(ctx, `SELECT enabled FROM catalog.categories WHERE id=$1 FOR SHARE`, command.Metadata.CategoryID).Scan(&categoryEnabled); errors.Is(err, pgx.ErrNoRows) || !categoryEnabled {
		return PublishedMetadataRevision{}, ErrTorrentMetadataUpdateCategoryUnavailable
	} else if err != nil {
		return PublishedMetadataRevision{}, wrapTorrentMetadataUpdate("read category", err)
	}

	resultingVersion := version + 1
	result, err := tx.Exec(ctx, `
UPDATE torrents.torrents
SET category_id=$2, title=$3, subtitle=$4, version=$5, updated_at=$6
WHERE id=$1 AND uploader_id=$7 AND state='published' AND version=$8`,
		command.TorrentID, command.Metadata.CategoryID, command.Metadata.Title, command.Metadata.Subtitle,
		resultingVersion, command.OccurredAt, command.UploaderID, version)
	if err != nil {
		return PublishedMetadataRevision{}, wrapTorrentMetadataUpdate("update aggregate", err)
	}
	if result.RowsAffected() != 1 {
		return PublishedMetadataRevision{}, ErrTorrentMetadataUpdateVersionConflict
	}
	// The public catalog is a separate read projection used by the home page,
	// search and bookmarks. Keep it transactionally aligned with the aggregate
	// so a successful self-edit cannot show old titles in list views.
	projectionResult, err := tx.Exec(ctx, `
UPDATE catalog.torrents
SET category_id=$2, name=$3, subtitle=$4
WHERE id=$1`, command.TorrentID, command.Metadata.CategoryID, command.Metadata.Title, command.Metadata.Subtitle)
	if err != nil {
		return PublishedMetadataRevision{}, wrapTorrentMetadataUpdate("update public catalog projection", err)
	}
	if projectionResult.RowsAffected() != 1 {
		return PublishedMetadataRevision{}, ErrTorrentMetadataUpdateStateConflict
	}
	_, err = tx.Exec(ctx, `
INSERT INTO torrents.torrent_metadata_revisions (
    id, torrent_id, uploader_id, expected_torrent_version,
    resulting_torrent_version, category_id, title, subtitle, reason,
    authorization_decision_id, occurred_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, command.RequestID, command.TorrentID,
		command.UploaderID, version, resultingVersion, command.Metadata.CategoryID, command.Metadata.Title,
		command.Metadata.Subtitle, command.Reason, command.Authorization.ID, command.OccurredAt)
	if err != nil {
		return PublishedMetadataRevision{}, wrapTorrentMetadataUpdate("insert revision", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PublishedMetadataRevision{}, wrapTorrentMetadataUpdate("commit", err)
	}
	return PublishedMetadataRevision{ID: command.RequestID, TorrentID: command.TorrentID, Version: resultingVersion, Metadata: command.Metadata, Reason: command.Reason, UpdatedAt: command.OccurredAt}, nil
}

func readPublishedMetadataReplay(ctx context.Context, tx pgx.Tx, requestID uuid.UUID) (PublishedMetadataRevision, uuid.UUID, bool, error) {
	var result PublishedMetadataRevision
	var uploaderID uuid.UUID
	err := tx.QueryRow(ctx, `
SELECT id, torrent_id, uploader_id, resulting_torrent_version, category_id, title, subtitle,
       reason, occurred_at
FROM torrents.torrent_metadata_revisions
WHERE id=$1`, requestID).Scan(&result.ID, &result.TorrentID, &uploaderID, &result.Version,
		&result.Metadata.CategoryID, &result.Metadata.Title, &result.Metadata.Subtitle,
		&result.Reason, &result.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return PublishedMetadataRevision{}, uuid.Nil, false, nil
	}
	if err != nil {
		return PublishedMetadataRevision{}, uuid.Nil, false, fmt.Errorf("read torrent metadata replay: %w", err)
	}
	return result, uploaderID, true, nil
}

var _ TorrentMaintenanceRepository = (*PostgresTorrentMaintenanceRepository)(nil)
