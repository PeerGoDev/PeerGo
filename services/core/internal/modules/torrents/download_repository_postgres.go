package torrents

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/generated/torrentdb"
)

type PostgresTorrentDownloadRepository struct {
	pool    *pgxpool.Pool
	queries *torrentdb.Queries
}

func NewPostgresTorrentDownloadRepository(pool *pgxpool.Pool) (*PostgresTorrentDownloadRepository, error) {
	if pool == nil {
		return nil, errors.New("torrent download database is required")
	}
	return &PostgresTorrentDownloadRepository{pool: pool, queries: torrentdb.New(pool)}, nil
}

func (repository *PostgresTorrentDownloadRepository) DownloadRestricted(ctx context.Context, userID uuid.UUID) (bool, error) {
	if userID == uuid.Nil {
		return false, ErrTorrentInputInvalid
	}
	var restricted bool
	if err := repository.pool.QueryRow(ctx, `SELECT identity.is_download_restricted($1)`, userID).Scan(&restricted); err != nil {
		return false, fmt.Errorf("read torrent download restriction: %w", err)
	}
	return restricted, nil
}

func (repository *PostgresTorrentDownloadRepository) PublishedDownloadSource(ctx context.Context, torrentID TorrentID) (TorrentDownloadSource, error) {
	if torrentID < 1 {
		return TorrentDownloadSource{}, ErrTorrentInputInvalid
	}
	object, err := repository.queries.GetPublishedTorrentDownloadObject(ctx, int64(torrentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return TorrentDownloadSource{}, ErrTorrentDownloadNotFound
	}
	if err != nil {
		return TorrentDownloadSource{}, fmt.Errorf("get published torrent download object: %w", err)
	}
	if object.TorrentID != int64(torrentID) || object.ObjectID == uuid.Nil || len(object.ContentSha256) != 32 ||
		object.ByteLength <= 0 || object.InfoOffset < 0 || object.InfoLength <= 0 ||
		object.InfoOffset > object.ByteLength-object.InfoLength {
		return TorrentDownloadSource{}, ErrTorrentDownloadObjectConflict
	}
	var contentDigest ObjectSHA256
	copy(contentDigest[:], object.ContentSha256)
	rows, err := repository.queries.ListReadableTorrentObjectLocations(ctx, object.ObjectID)
	if err != nil {
		return TorrentDownloadSource{}, fmt.Errorf("list readable torrent object locations: %w", err)
	}
	locations := make([]TorrentDownloadLocation, 0, len(rows))
	for _, row := range rows {
		backendID, err := ParseStorageBackendID(row.BackendID)
		if err != nil {
			return TorrentDownloadSource{}, ErrTorrentDownloadObjectConflict
		}
		objectKey, err := ParseObjectKey(row.ObjectKey)
		if err != nil || row.ID == uuid.Nil || row.ObjectID != object.ObjectID ||
			!row.ObservedByteLength.Valid || row.ObservedByteLength.Int64 <= 0 ||
			len(row.ObservedSha256) != 32 || !row.VerifiedAt.Valid {
			return TorrentDownloadSource{}, ErrTorrentDownloadObjectConflict
		}
		state := StorageLocationState(row.State)
		if state != StorageLocationVerified && state != StorageLocationRetiring {
			return TorrentDownloadSource{}, ErrTorrentDownloadObjectConflict
		}
		var observedDigest ObjectSHA256
		copy(observedDigest[:], row.ObservedSha256)
		locations = append(locations, TorrentDownloadLocation{
			ID: row.ID, BackendID: backendID, ObjectKey: objectKey,
			State: state, Preferred: row.IsPreferred,
			VersionID: nullableStorageString(row.VersionID),
			Descriptor: StoredObjectDescriptor{
				SHA256: observedDigest, ByteLength: row.ObservedByteLength.Int64,
			},
			VerifiedAt: row.VerifiedAt.Time.UTC(),
		})
	}
	return TorrentDownloadSource{
		TorrentID: torrentID, Title: object.Title, ObjectID: object.ObjectID,
		Descriptor: StoredObjectDescriptor{SHA256: contentDigest, ByteLength: object.ByteLength},
		InfoOffset: object.InfoOffset, InfoLength: object.InfoLength,
		Locations: locations,
	}, nil
}

var _ TorrentDownloadRepository = (*PostgresTorrentDownloadRepository)(nil)
