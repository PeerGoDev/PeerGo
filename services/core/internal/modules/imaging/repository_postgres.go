package imaging

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
)

type Repository interface {
	Claim(context.Context, time.Time, time.Duration, uuid.UUID) (*Job, error)
	Source(context.Context, Job) (Source, error)
	Complete(context.Context, Job, Output, objectstorage.BackendID, objectstorage.Key, string, time.Time) error
	Fail(context.Context, Job, string, time.Time) error
	ReadyForTorrentScreenshot(context.Context, uuid.UUID, Variant) (ReadyDerivative, error)
	ReadyForAvatar(context.Context, uuid.UUID, Variant) (ReadyDerivative, error)
	Overview(context.Context) (QueueOverview, error)
	RetryDead(context.Context, time.Time) (int64, error)
}

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, errors.New("image derivative database is required")
	}
	return &PostgresRepository{pool: pool}, nil
}

func (repository *PostgresRepository) Claim(ctx context.Context, now time.Time, lease time.Duration, token uuid.UUID) (*Job, error) {
	if ctx == nil || now.IsZero() || lease < time.Second || lease > 10*time.Minute || token == uuid.Nil {
		return nil, ErrInput
	}
	now = now.UTC().Round(0)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin image derivative claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
UPDATE media.image_derivatives
SET state = 'dead', lease_token = NULL, lease_until = NULL,
    completed_at = $1, updated_at = $1,
    last_error_code = 'attempts_exhausted', last_error_at = $1
WHERE state = 'processing' AND lease_until <= $1 AND attempt_count >= $2`, now, MaxAttempts); err != nil {
		return nil, fmt.Errorf("reap exhausted image derivative leases: %w", err)
	}
	var job Job
	var screenshotObjectID, avatarObjectID *uuid.UUID
	err = tx.QueryRow(ctx, `
WITH candidate AS (
    SELECT id
    FROM media.image_derivatives
    WHERE attempt_count < $2
      AND (
        (state IN ('pending', 'retry_wait') AND available_at <= $1)
        OR (state = 'processing' AND lease_until <= $1)
      )
    ORDER BY available_at, created_at, id
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE media.image_derivatives AS job
SET state = 'processing', attempt_count = job.attempt_count + 1,
    lease_token = $3, lease_until = $4, updated_at = $1
FROM candidate
WHERE job.id = candidate.id
RETURNING job.id, job.torrent_screenshot_object_id, job.avatar_object_id,
          job.variant, job.attempt_count, job.lease_token, job.lease_until`,
		now, MaxAttempts, token, now.Add(lease),
	).Scan(&job.ID, &screenshotObjectID, &avatarObjectID, &job.Variant, &job.AttemptCount, &job.LeaseToken, &job.LeaseUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit empty image derivative claim: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim image derivative: %w", err)
	}
	switch {
	case screenshotObjectID != nil && avatarObjectID == nil:
		job.SourceKind, job.SourceObject = SourceTorrentScreenshot, *screenshotObjectID
	case avatarObjectID != nil && screenshotObjectID == nil:
		job.SourceKind, job.SourceObject = SourceAvatar, *avatarObjectID
	default:
		return nil, ErrSourceConflict
	}
	if !job.Variant.Valid() || job.ID == uuid.Nil || job.SourceObject == uuid.Nil || job.LeaseToken != token {
		return nil, ErrSourceConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit image derivative claim: %w", err)
	}
	return &job, nil
}

func (repository *PostgresRepository) Source(ctx context.Context, job Job) (Source, error) {
	if ctx == nil || job.ID == uuid.Nil || !job.SourceKind.Valid() || job.SourceObject == uuid.Nil {
		return Source{}, ErrInput
	}
	var source Source
	source.Kind, source.ObjectID = job.SourceKind, job.SourceObject
	var digest []byte
	var byteLength int64
	var rows pgx.Rows
	var err error
	switch job.SourceKind {
	case SourceTorrentScreenshot:
		err = repository.pool.QueryRow(ctx, `
SELECT content_sha256, byte_length, content_type,
       CASE content_type WHEN 'image/jpeg' THEN '.jpg' WHEN 'image/png' THEN '.png'
            WHEN 'image/webp' THEN '.webp' WHEN 'image/gif' THEN '.gif' END,
       width, height
FROM torrents.torrent_screenshot_objects
WHERE id = $1`, job.SourceObject).Scan(
			&digest, &byteLength, &source.ContentType, &source.Extension, &source.Width, &source.Height,
		)
		if err == nil {
			rows, err = repository.pool.Query(ctx, `
SELECT backend_id, object_key, COALESCE(version_id, ''), verified_at
FROM torrents.torrent_screenshot_object_locations
WHERE object_id = $1 AND state IN ('verified', 'retiring')
ORDER BY is_preferred DESC, (state = 'verified') DESC, verified_at DESC, id`, job.SourceObject)
		}
	case SourceAvatar:
		err = repository.pool.QueryRow(ctx, `
SELECT content_sha256, byte_length, content_type, extension, width, height
FROM identity.user_avatar_objects
WHERE id = $1`, job.SourceObject).Scan(
			&digest, &byteLength, &source.ContentType, &source.Extension, &source.Width, &source.Height,
		)
		if err == nil {
			rows, err = repository.pool.Query(ctx, `
SELECT backend_id, object_key, COALESCE(version_id, ''), verified_at
FROM identity.user_avatar_object_locations
WHERE object_id = $1 AND state IN ('verified', 'retiring')
ORDER BY is_preferred DESC, (state = 'verified') DESC, verified_at DESC, backend_id`, job.SourceObject)
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Source{}, ErrNotFound
	}
	if err != nil {
		return Source{}, fmt.Errorf("read image derivative source: %w", err)
	}
	defer rows.Close()
	if len(digest) != 32 || byteLength < 1 || source.Width < 1 || source.Height < 1 {
		return Source{}, ErrSourceConflict
	}
	copy(source.Descriptor.SHA256[:], digest)
	source.Descriptor.ByteLength = byteLength
	for rows.Next() {
		var backend, key, version string
		var verifiedAt time.Time
		if err := rows.Scan(&backend, &key, &version, &verifiedAt); err != nil {
			return Source{}, fmt.Errorf("scan image derivative source location: %w", err)
		}
		backendID, backendErr := objectstorage.ParseBackendID(backend)
		objectKey, keyErr := objectstorage.ParseKey(key)
		if backendErr != nil || keyErr != nil || verifiedAt.IsZero() {
			return Source{}, ErrSourceConflict
		}
		source.Locations = append(source.Locations, Location{
			BackendID: backendID, ObjectKey: objectKey, VersionID: version,
			VerifiedAt: verifiedAt.UTC(),
		})
	}
	if err := rows.Err(); err != nil {
		return Source{}, fmt.Errorf("read image derivative source locations: %w", err)
	}
	if len(source.Locations) == 0 {
		return Source{}, ErrSourceUnavailable
	}
	return source, nil
}

func (repository *PostgresRepository) Complete(ctx context.Context, job Job, output Output, backendID objectstorage.BackendID, key objectstorage.Key, versionID string, now time.Time) error {
	if ctx == nil || job.ID == uuid.Nil || job.LeaseToken == uuid.Nil || !output.Descriptor.Valid() ||
		output.Width < 1 || output.Height < 1 || backendID == "" || key == "" || now.IsZero() {
		return ErrInput
	}
	now = now.UTC().Round(0)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin image derivative completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var objectID uuid.UUID
	var storedDigest []byte
	var storedLength int64
	var storedWidth, storedHeight int
	err = tx.QueryRow(ctx, `
INSERT INTO media.image_derivative_objects (
    id, content_sha256, byte_length, content_type, extension, width, height, created_at
) VALUES ($1, $2, $3, 'image/webp', '.webp', $4, $5, $6)
ON CONFLICT (content_sha256) DO UPDATE
SET content_sha256 = EXCLUDED.content_sha256
RETURNING id, content_sha256, byte_length, width, height`,
		uuid.New(), output.Descriptor.SHA256[:], output.Descriptor.ByteLength,
		output.Width, output.Height, now,
	).Scan(&objectID, &storedDigest, &storedLength, &storedWidth, &storedHeight)
	if err != nil {
		return fmt.Errorf("resolve image derivative object: %w", err)
	}
	if !bytes.Equal(storedDigest, output.Descriptor.SHA256[:]) || storedLength != output.Descriptor.ByteLength ||
		storedWidth != output.Width || storedHeight != output.Height {
		return ErrOutputConflict
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO media.image_derivative_object_locations (
    object_id, backend_id, object_key, version_id,
    observed_byte_length, observed_sha256, verified_at
) VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7)
ON CONFLICT (backend_id, object_key) DO NOTHING`,
		objectID, string(backendID), string(key), versionID,
		output.Descriptor.ByteLength, output.Descriptor.SHA256[:], now,
	); err != nil {
		return fmt.Errorf("save image derivative location: %w", err)
	}
	var locationMatches bool
	if err := tx.QueryRow(ctx, `
SELECT object_key = $3
   AND COALESCE(version_id, '') = $4
   AND observed_byte_length = $5
   AND observed_sha256 = $6
FROM media.image_derivative_object_locations
WHERE object_id = $1 AND backend_id = $2 AND object_key = $3`,
		objectID, string(backendID), string(key), versionID,
		output.Descriptor.ByteLength, output.Descriptor.SHA256[:],
	).Scan(&locationMatches); err != nil || !locationMatches {
		return ErrOutputConflict
	}
	command, err := tx.Exec(ctx, `
UPDATE media.image_derivatives
SET state = 'ready', object_id = $3, lease_token = NULL, lease_until = NULL,
    last_error_code = NULL, last_error_at = NULL,
    updated_at = $4, completed_at = $4
WHERE id = $1 AND state = 'processing' AND lease_token = $2`,
		job.ID, job.LeaseToken, objectID, now,
	)
	if err != nil {
		return fmt.Errorf("complete image derivative: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrLeaseConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit image derivative completion: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) Fail(ctx context.Context, job Job, code string, now time.Time) error {
	if ctx == nil || job.ID == uuid.Nil || job.LeaseToken == uuid.Nil || code == "" || len(code) > 64 || now.IsZero() {
		return ErrInput
	}
	now = now.UTC().Round(0)
	state := "retry_wait"
	var completedAt any
	availableAt := now.Add(retryDelay(job.AttemptCount))
	if job.AttemptCount >= MaxAttempts {
		state, completedAt, availableAt = "dead", now, now
	}
	command, err := repository.pool.Exec(ctx, `
UPDATE media.image_derivatives
SET state = $3, available_at = $4, lease_token = NULL, lease_until = NULL,
    last_error_code = $5, last_error_at = $6, updated_at = $6, completed_at = $7
WHERE id = $1 AND state = 'processing' AND lease_token = $2`,
		job.ID, job.LeaseToken, state, availableAt, code, now, completedAt,
	)
	if err != nil {
		return fmt.Errorf("fail image derivative: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrLeaseConflict
	}
	return nil
}

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Duration(1<<(attempt-1)) * 15 * time.Second
	if delay > 15*time.Minute {
		return 15 * time.Minute
	}
	return delay
}

func (repository *PostgresRepository) ReadyForTorrentScreenshot(ctx context.Context, sourceID uuid.UUID, variant Variant) (ReadyDerivative, error) {
	return repository.ready(ctx, "torrent_screenshot_object_id", sourceID, variant)
}

func (repository *PostgresRepository) ReadyForAvatar(ctx context.Context, sourceID uuid.UUID, variant Variant) (ReadyDerivative, error) {
	return repository.ready(ctx, "avatar_object_id", sourceID, variant)
}

func (repository *PostgresRepository) ready(ctx context.Context, sourceColumn string, sourceID uuid.UUID, variant Variant) (ReadyDerivative, error) {
	if ctx == nil || sourceID == uuid.Nil || !variant.Valid() ||
		(sourceColumn != "torrent_screenshot_object_id" && sourceColumn != "avatar_object_id") {
		return ReadyDerivative{}, ErrInput
	}
	var result ReadyDerivative
	var digest []byte
	query := fmt.Sprintf(`
SELECT object.id, object.content_sha256, object.byte_length, object.width, object.height
FROM media.image_derivatives AS derivative
JOIN media.image_derivative_objects AS object ON object.id = derivative.object_id
WHERE derivative.%s = $1
  AND derivative.variant = $2 AND derivative.policy_version = $3
  AND derivative.state = 'ready'`, sourceColumn)
	err := repository.pool.QueryRow(ctx, query, sourceID, string(variant), PolicyVersion).Scan(
		&result.ObjectID, &digest, &result.Descriptor.ByteLength, &result.Width, &result.Height,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReadyDerivative{}, ErrNotFound
	}
	if err != nil {
		return ReadyDerivative{}, fmt.Errorf("read ready image derivative: %w", err)
	}
	if len(digest) != 32 || result.ObjectID == uuid.Nil || result.Descriptor.ByteLength < 1 {
		return ReadyDerivative{}, ErrOutputConflict
	}
	copy(result.Descriptor.SHA256[:], digest)
	rows, err := repository.pool.Query(ctx, `
SELECT backend_id, object_key, COALESCE(version_id, ''), verified_at
FROM media.image_derivative_object_locations
WHERE object_id = $1 AND state IN ('verified', 'retiring')
ORDER BY is_preferred DESC, (state = 'verified') DESC, verified_at DESC, backend_id`, result.ObjectID)
	if err != nil {
		return ReadyDerivative{}, fmt.Errorf("list ready image derivative locations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var backend, key, version string
		var verifiedAt time.Time
		if err := rows.Scan(&backend, &key, &version, &verifiedAt); err != nil {
			return ReadyDerivative{}, err
		}
		backendID, backendErr := objectstorage.ParseBackendID(backend)
		objectKey, keyErr := objectstorage.ParseKey(key)
		if backendErr != nil || keyErr != nil || verifiedAt.IsZero() {
			return ReadyDerivative{}, ErrOutputConflict
		}
		result.Locations = append(result.Locations, Location{
			BackendID: backendID, ObjectKey: objectKey, VersionID: version,
			VerifiedAt: verifiedAt.UTC(),
		})
	}
	if err := rows.Err(); err != nil {
		return ReadyDerivative{}, err
	}
	if len(result.Locations) == 0 {
		return ReadyDerivative{}, ErrNotFound
	}
	return result, nil
}

func (repository *PostgresRepository) Overview(ctx context.Context) (QueueOverview, error) {
	if ctx == nil {
		return QueueOverview{}, ErrInput
	}
	result := QueueOverview{PolicyVersion: PolicyVersion}
	err := repository.pool.QueryRow(ctx, `
SELECT
    count(*) FILTER (WHERE state = 'pending'),
    count(*) FILTER (WHERE state = 'processing'),
    count(*) FILTER (WHERE state = 'retry_wait'),
    count(*) FILTER (WHERE state = 'ready'),
    count(*) FILTER (WHERE state = 'dead'),
    count(DISTINCT torrent_screenshot_object_id)
      + count(DISTINCT avatar_object_id),
    (SELECT count(*) FROM media.image_derivative_objects),
    (SELECT COALESCE(sum(byte_length), 0) FROM media.image_derivative_objects),
    min(created_at) FILTER (WHERE state IN ('pending', 'retry_wait')),
    COALESCE((array_agg(last_error_code ORDER BY last_error_at DESC)
        FILTER (WHERE last_error_code IS NOT NULL))[1], ''),
    max(last_error_at)
FROM media.image_derivatives
WHERE policy_version = $1`, PolicyVersion).Scan(
		&result.Pending, &result.Processing, &result.Retrying, &result.Ready, &result.Dead,
		&result.SourceObjects, &result.OutputObjects, &result.OutputBytes,
		&result.OldestPendingAt, &result.LastErrorCode, &result.LastErrorAt,
	)
	if err != nil {
		return QueueOverview{}, fmt.Errorf("read image derivative overview: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) RetryDead(ctx context.Context, now time.Time) (int64, error) {
	if ctx == nil || now.IsZero() {
		return 0, ErrInput
	}
	now = now.UTC().Round(0)
	command, err := repository.pool.Exec(ctx, `
UPDATE media.image_derivatives
SET state = 'pending', attempt_count = 0, available_at = $1,
    last_error_code = NULL, last_error_at = NULL,
    updated_at = $1, completed_at = NULL
WHERE policy_version = $2 AND state = 'dead'`, now, PolicyVersion)
	if err != nil {
		return 0, fmt.Errorf("retry dead image derivatives: %w", err)
	}
	return command.RowsAffected(), nil
}
