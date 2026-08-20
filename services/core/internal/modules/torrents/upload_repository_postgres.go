package torrents

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/generated/torrentdb"
)

type PostgresTorrentUploadRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresTorrentUploadRepository(pool *pgxpool.Pool) (*PostgresTorrentUploadRepository, error) {
	if pool == nil {
		return nil, errors.New("torrent upload repository requires a database pool")
	}
	return &PostgresTorrentUploadRepository{pool: pool}, nil
}

// Reserve claims the protocol and object identities before object storage is
// touched. ON CONFLICT DO NOTHING keeps both same-key retries and different-key
// duplicate races out of PostgreSQL's aborted-transaction state.
func (repository *PostgresTorrentUploadRepository) Reserve(ctx context.Context, command ReserveTorrentUploadCommand) (TorrentUploadReservation, error) {
	if err := validateReserveTorrentUploadCommand(command); err != nil {
		return TorrentUploadReservation{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TorrentUploadReservation{}, fmt.Errorf("begin torrent upload reservation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := torrentdb.New(tx)

	row, err := queries.GetTorrentUploadForUpdate(ctx, command.ID)
	if err == nil {
		reservation, conversionErr := repository.resumeReservation(ctx, queries, row, command)
		if conversionErr != nil {
			return TorrentUploadReservation{}, conversionErr
		}
		if err := tx.Commit(ctx); err != nil {
			return TorrentUploadReservation{}, fmt.Errorf("commit resumed torrent upload reservation: %w", err)
		}
		return reservation, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return TorrentUploadReservation{}, fmt.Errorf("read torrent upload reservation: %w", err)
	}

	eligible, err := queries.TorrentUploadUserEligible(ctx, command.UploaderID)
	if err != nil {
		return TorrentUploadReservation{}, fmt.Errorf("check torrent uploader eligibility: %w", err)
	}
	if !eligible {
		return TorrentUploadReservation{}, ErrTorrentUploadEmailUnverified
	}
	categoryEnabled, err := queries.TorrentUploadCategoryEnabled(ctx, command.CategoryID)
	if err != nil {
		return TorrentUploadReservation{}, fmt.Errorf("check torrent upload category: %w", err)
	}
	if !categoryEnabled {
		return TorrentUploadReservation{}, ErrTorrentUploadCategoryUnavailable
	}
	exists, err := queries.TorrentUploadIdentityExists(ctx, torrentdb.TorrentUploadIdentityExistsParams{
		InfoHashV1: command.InfoHashV1[:], ContentSha256: command.Descriptor.SHA256[:],
	})
	if err != nil {
		return TorrentUploadReservation{}, fmt.Errorf("check existing torrent identity: %w", err)
	}
	if exists {
		return TorrentUploadReservation{}, ErrTorrentUploadDuplicate
	}

	rows, err := queries.InsertTorrentUploadReservation(ctx, torrentdb.InsertTorrentUploadReservationParams{
		UploadID: command.ID, UploaderID: command.UploaderID,
		RequestFingerprint: command.RequestFingerprint[:],
		ObjectID:           command.ObjectID, CategoryID: command.CategoryID, InfoHashV1: command.InfoHashV1[:],
		ContentSha256: command.Descriptor.SHA256[:], ByteLength: command.Descriptor.ByteLength,
		BackendID: string(command.BackendID), ObjectKey: string(command.ObjectKey),
		UploadPolicyRevisionID: command.PolicyRevisionID,
		OccurredAt: storageTimestamp(command.OccurredAt),
	})
	if err != nil {
		return TorrentUploadReservation{}, fmt.Errorf("insert torrent upload reservation: %w", err)
	}
	row, err = queries.GetTorrentUploadForUpdate(ctx, command.ID)
	if errors.Is(err, pgx.ErrNoRows) && rows == 0 {
		return TorrentUploadReservation{}, ErrTorrentUploadDuplicate
	}
	if err != nil {
		return TorrentUploadReservation{}, fmt.Errorf("lock inserted torrent upload reservation: %w", err)
	}
	reservation, err := repository.resumeReservation(ctx, queries, row, command)
	if err != nil {
		return TorrentUploadReservation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TorrentUploadReservation{}, fmt.Errorf("commit torrent upload reservation: %w", err)
	}
	return reservation, nil
}

func (repository *PostgresTorrentUploadRepository) resumeReservation(
	ctx context.Context,
	queries *torrentdb.Queries,
	row torrentdb.GetTorrentUploadForUpdateRow,
	command ReserveTorrentUploadCommand,
) (TorrentUploadReservation, error) {
	reservation, err := torrentUploadReservationFromRow(row)
	if err != nil {
		return TorrentUploadReservation{}, err
	}
	if reservation.UploaderID != command.UploaderID || reservation.CategoryID != command.CategoryID ||
		reservation.RequestFingerprint != command.RequestFingerprint || reservation.InfoHashV1 != command.InfoHashV1 ||
		reservation.Descriptor != command.Descriptor {
		return TorrentUploadReservation{}, ErrTorrentUploadIdempotencyConflict
	}
	switch reservation.State {
	case TorrentUploadReserved, TorrentUploadObjectVerified:
		return reservation, nil
	case TorrentUploadCompleted:
		result, err := completedTorrentUploadResult(ctx, queries, reservation.ID)
		if err != nil {
			return TorrentUploadReservation{}, err
		}
		reservation.Result = &result
		return reservation, nil
	case TorrentUploadAbandoned:
		return TorrentUploadReservation{}, ErrTorrentUploadExpired
	default:
		return TorrentUploadReservation{}, ErrTorrentUploadStateConflict
	}
}

func (repository *PostgresTorrentUploadRepository) RecordObjectVerified(ctx context.Context, command RecordTorrentUploadObjectCommand) (TorrentUploadReservation, error) {
	if command.UploadID == uuid.Nil || command.UploaderID == uuid.Nil || command.RequestFingerprint == (ObjectSHA256{}) ||
		command.BackendID == "" || command.ObjectKey == "" || !command.Descriptor.Valid() || command.VerifiedAt.IsZero() {
		return TorrentUploadReservation{}, ErrTorrentInputInvalid
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TorrentUploadReservation{}, fmt.Errorf("begin torrent upload object verification: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := torrentdb.New(tx)
	row, err := queries.GetTorrentUploadForUpdate(ctx, command.UploadID)
	if errors.Is(err, pgx.ErrNoRows) {
		return TorrentUploadReservation{}, ErrTorrentUploadStateConflict
	}
	if err != nil {
		return TorrentUploadReservation{}, fmt.Errorf("lock torrent upload for object verification: %w", err)
	}
	reservation, err := torrentUploadReservationFromRow(row)
	if err != nil {
		return TorrentUploadReservation{}, err
	}
	if reservation.UploaderID != command.UploaderID || reservation.RequestFingerprint != command.RequestFingerprint ||
		reservation.BackendID != command.BackendID || reservation.ObjectKey != command.ObjectKey ||
		reservation.Descriptor != command.Descriptor {
		return TorrentUploadReservation{}, ErrTorrentUploadIdempotencyConflict
	}
	switch reservation.State {
	case TorrentUploadObjectVerified:
		if err := tx.Commit(ctx); err != nil {
			return TorrentUploadReservation{}, fmt.Errorf("commit resumed object verification: %w", err)
		}
		return reservation, nil
	case TorrentUploadCompleted:
		result, err := completedTorrentUploadResult(ctx, queries, command.UploadID)
		if err != nil {
			return TorrentUploadReservation{}, err
		}
		reservation.Result = &result
		if err := tx.Commit(ctx); err != nil {
			return TorrentUploadReservation{}, fmt.Errorf("commit completed object verification retry: %w", err)
		}
		return reservation, nil
	case TorrentUploadReserved:
		rows, err := queries.RecordTorrentUploadObjectVerified(ctx, torrentdb.RecordTorrentUploadObjectVerifiedParams{
			ObjectCreated: command.ObjectCreated, StorageVersionID: nullableStorageText(command.StorageVersionID),
			VerifiedAt: storageTimestamp(command.VerifiedAt), UploadID: command.UploadID,
		})
		if err != nil {
			return TorrentUploadReservation{}, fmt.Errorf("record verified torrent upload object: %w", err)
		}
		if rows != 1 {
			return TorrentUploadReservation{}, ErrTorrentUploadStateConflict
		}
	default:
		return TorrentUploadReservation{}, ErrTorrentUploadStateConflict
	}
	row, err = queries.GetTorrentUploadForUpdate(ctx, command.UploadID)
	if err != nil {
		return TorrentUploadReservation{}, fmt.Errorf("reload verified torrent upload: %w", err)
	}
	reservation, err = torrentUploadReservationFromRow(row)
	if err != nil {
		return TorrentUploadReservation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TorrentUploadReservation{}, fmt.Errorf("commit torrent upload object verification: %w", err)
	}
	return reservation, nil
}

func (repository *PostgresTorrentUploadRepository) Finalize(ctx context.Context, command FinalizeTorrentUploadCommand) (TorrentUploadResult, error) {
	if err := validateFinalizeTorrentUploadCommand(command); err != nil {
		return TorrentUploadResult{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TorrentUploadResult{}, fmt.Errorf("begin torrent upload finalization: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := torrentdb.New(tx)
	row, err := queries.GetTorrentUploadForUpdate(ctx, command.UploadID)
	if errors.Is(err, pgx.ErrNoRows) {
		return TorrentUploadResult{}, ErrTorrentUploadStateConflict
	}
	if err != nil {
		return TorrentUploadResult{}, fmt.Errorf("lock torrent upload for finalization: %w", err)
	}
	reservation, err := torrentUploadReservationFromRow(row)
	if err != nil {
		return TorrentUploadResult{}, err
	}
	if err := validateFinalizationAgainstReservation(command, reservation); err != nil {
		return TorrentUploadResult{}, err
	}
	if reservation.State == TorrentUploadCompleted {
		result, err := completedTorrentUploadResult(ctx, queries, command.UploadID)
		if err != nil {
			return TorrentUploadResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return TorrentUploadResult{}, fmt.Errorf("commit completed torrent upload retry: %w", err)
		}
		return result, nil
	}
	if reservation.State != TorrentUploadObjectVerified || reservation.ObjectVerifiedAt == nil {
		return TorrentUploadResult{}, ErrTorrentUploadStateConflict
	}
	eligible, err := queries.TorrentUploadUserEligible(ctx, command.Torrent.UploaderID)
	if err != nil {
		return TorrentUploadResult{}, fmt.Errorf("recheck torrent uploader eligibility: %w", err)
	}
	if !eligible {
		return TorrentUploadResult{}, ErrTorrentUploadEmailUnverified
	}
	categoryEnabled, err := queries.TorrentUploadCategoryEnabled(ctx, command.Torrent.CategoryID)
	if err != nil {
		return TorrentUploadResult{}, fmt.Errorf("recheck torrent upload category: %w", err)
	}
	if !categoryEnabled {
		return TorrentUploadResult{}, ErrTorrentUploadCategoryUnavailable
	}

	compatibilityFlags := make([]string, 0, len(command.Torrent.Object.CompatibilityFlags))
	for _, flag := range command.Torrent.Object.CompatibilityFlags {
		compatibilityFlags = append(compatibilityFlags, string(flag))
	}
	if err := queries.InsertTorrentObjectFromUpload(ctx, torrentdb.InsertTorrentObjectFromUploadParams{
		ObjectID: command.Torrent.Object.ID, ContentSha256: command.Torrent.Object.ContentSHA256[:],
		ByteLength: command.Torrent.Object.ByteLength, ParserVersion: command.Torrent.Object.ParserVersion,
		ValidationProfile: string(command.Torrent.Object.ValidationProfile), CompatibilityFlags: compatibilityFlags,
		InfoOffset: command.Torrent.Object.InfoOffset, InfoLength: command.Torrent.Object.InfoLength,
		CreatedAt: storageTimestamp(command.Torrent.SubmittedAt),
	}); err != nil {
		return TorrentUploadResult{}, torrentUploadWriteError("insert torrent object", err)
	}
	if err := queries.InsertInitialTorrentObjectLocation(ctx, torrentdb.InsertInitialTorrentObjectLocationParams{
		LocationID: uuid.New(), ObjectID: command.Torrent.Object.ID,
		BackendID: string(reservation.BackendID), ObjectKey: string(reservation.ObjectKey),
		VersionID:          nullableStorageText(reservation.StorageVersionID),
		ObservedByteLength: reservation.Descriptor.ByteLength,
		ObservedSha256:     reservation.Descriptor.SHA256[:],
		VerifiedAt:         storageTimestamp(*reservation.ObjectVerifiedAt),
	}); err != nil {
		return TorrentUploadResult{}, torrentUploadWriteError("insert initial torrent object location", err)
	}
	torrentID, err := queries.InsertPendingTorrentFromUpload(ctx, torrentdb.InsertPendingTorrentFromUploadParams{
		UploaderID: command.Torrent.UploaderID,
		CategoryID: command.Torrent.CategoryID, ObjectID: command.Torrent.Object.ID,
		InfoHashV1: command.Torrent.InfoHashV1[:], ContentName: command.Torrent.ContentName,
		Title: command.Torrent.Title, Subtitle: command.Torrent.Subtitle,
		Description: command.Torrent.Description, DescriptionFormat: command.Torrent.DescriptionFormat,
		MediaInfo: command.Torrent.MediaInfo, Anonymous: command.Torrent.Anonymous,
		TotalSizeBytes: command.Torrent.TotalSizeBytes, PayloadSizeBytes: command.Torrent.PayloadSizeBytes,
		FileCount: int32(command.Torrent.FileCount), PaddingFileCount: int32(command.Torrent.PaddingFileCount),
		PieceLengthBytes: command.Torrent.PieceLengthBytes, PieceCount: int32(command.Torrent.PieceCount),
		SubmittedAt: storageTimestamp(command.Torrent.SubmittedAt),
	})
	if err != nil {
		return TorrentUploadResult{}, torrentUploadWriteError("insert pending torrent", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO torrents.torrent_upload_policy_bindings (
		torrent_id, policy_revision_id, bound_at
	) VALUES ($1,$2,$3)`, torrentID, command.PolicyRevisionID, command.Torrent.SubmittedAt); err != nil {
		return TorrentUploadResult{}, torrentUploadWriteError("bind torrent upload policy", err)
	}
	// External identifiers are normalized and provider-deduplicated by the
	// aggregate before this transaction. Keeping their insert beside the parent
	// prevents a reviewable submission from observing partially written links.
	for _, identifier := range command.Torrent.ExternalIdentifiers {
		if err := queries.InsertTorrentExternalIdentifierFromUpload(ctx, torrentdb.InsertTorrentExternalIdentifierFromUploadParams{
			TorrentID: torrentID, Provider: identifier.Provider, ExternalID: identifier.ExternalID,
			CreatedAt: storageTimestamp(command.Torrent.SubmittedAt),
		}); err != nil {
			return TorrentUploadResult{}, torrentUploadWriteError("insert torrent external identifier", err)
		}
	}
	// Each row is inserted by selecting the canonical category binding, mode
	// and enabled option. Request data never supplies those invariants. The
	// transaction is rolled back if one option disappeared or a single-choice
	// facet was submitted with multiple values.
	for _, selection := range command.Torrent.FacetSelections {
		for position, optionKey := range selection.OptionKeys {
			rows, insertErr := queries.InsertTorrentFacetValueFromUpload(ctx, torrentdb.InsertTorrentFacetValueFromUploadParams{
				TorrentID: int64(torrentID), CategoryID: command.Torrent.CategoryID,
				FacetID: selection.FacetID, OptionKey: optionKey, Position: int32(position),
				CreatedAt: storageTimestamp(command.Torrent.SubmittedAt),
			})
			if insertErr != nil {
				var databaseError *pgconn.PgError
				if errors.As(insertErr, &databaseError) && (databaseError.Code == "23503" || databaseError.Code == "23505" || databaseError.Code == "23514") {
					return TorrentUploadResult{}, ErrTorrentInputInvalid
				}
				return TorrentUploadResult{}, torrentUploadWriteError("insert torrent facet value", insertErr)
			}
			if rows != 1 {
				return TorrentUploadResult{}, ErrTorrentInputInvalid
			}
		}
	}
	requiredFacetsSatisfied, err := queries.TorrentUploadRequiredFacetsSatisfied(ctx, torrentdb.TorrentUploadRequiredFacetsSatisfiedParams{
		CategoryID: command.Torrent.CategoryID, TorrentID: int64(torrentID),
	})
	if err != nil {
		return TorrentUploadResult{}, torrentUploadWriteError("check required torrent facets", err)
	}
	if !requiredFacetsSatisfied.Valid || !requiredFacetsSatisfied.Bool {
		return TorrentUploadResult{}, ErrTorrentInputInvalid
	}
	if err := copyTorrentFiles(ctx, tx, torrentID, command.Files, command.Torrent.SubmittedAt); err != nil {
		return TorrentUploadResult{}, torrentUploadWriteError("insert torrent files", err)
	}
	for _, screenshot := range command.Screenshots {
		objectID, resolveErr := queries.ResolveTorrentScreenshotObjectFromUpload(ctx, torrentdb.ResolveTorrentScreenshotObjectFromUploadParams{
			ObjectID: screenshot.ID, ContentSha256: screenshot.ContentSHA256[:], ByteLength: screenshot.ByteLength,
			ContentType: screenshot.ContentType, Width: int32(screenshot.Width), Height: int32(screenshot.Height),
			CreatedAt: storageTimestamp(command.Torrent.SubmittedAt),
		})
		if resolveErr != nil {
			return TorrentUploadResult{}, torrentUploadWriteError("resolve torrent screenshot object", resolveErr)
		}
		rows, locationErr := queries.InsertTorrentScreenshotLocationFromUpload(ctx, torrentdb.InsertTorrentScreenshotLocationFromUploadParams{
			LocationID: uuid.New(), ObjectID: objectID, BackendID: string(screenshot.BackendID),
			ObjectKey: string(screenshot.ObjectKey), VersionID: nullableStorageText(screenshot.StorageVersionID),
			ObservedByteLength: screenshot.ByteLength, ObservedSha256: screenshot.ContentSHA256[:],
			VerifiedAt: storageTimestamp(command.OccurredAt),
		})
		if locationErr != nil {
			return TorrentUploadResult{}, torrentUploadWriteError("insert torrent screenshot location", locationErr)
		}
		if rows == 0 {
			matches, matchErr := queries.TorrentScreenshotLocationMatches(ctx, torrentdb.TorrentScreenshotLocationMatchesParams{
				ObjectID: objectID, BackendID: string(screenshot.BackendID), ObjectKey: string(screenshot.ObjectKey),
				ObservedByteLength: screenshot.ByteLength, ObservedSha256: screenshot.ContentSHA256[:],
			})
			if matchErr != nil {
				return TorrentUploadResult{}, torrentUploadWriteError("check torrent screenshot location", matchErr)
			}
			if !matches {
				return TorrentUploadResult{}, ErrTorrentUploadStateConflict
			}
		}
		if err := queries.InsertTorrentScreenshotFromUpload(ctx, torrentdb.InsertTorrentScreenshotFromUploadParams{
			TorrentID: int64(torrentID), ObjectID: objectID, Position: int16(screenshot.Position),
			CreatedAt: storageTimestamp(command.Torrent.SubmittedAt),
		}); err != nil {
			return TorrentUploadResult{}, torrentUploadWriteError("insert torrent screenshot", err)
		}
	}
	rows, err := queries.CompleteTorrentUpload(ctx, torrentdb.CompleteTorrentUploadParams{
		TorrentID: torrentID, CompletedAt: storageTimestamp(command.OccurredAt), UploadID: command.UploadID,
	})
	if err != nil {
		return TorrentUploadResult{}, fmt.Errorf("complete torrent upload reservation: %w", err)
	}
	if rows != 1 {
		return TorrentUploadResult{}, ErrTorrentUploadStateConflict
	}
	result, err := completedTorrentUploadResult(ctx, queries, command.UploadID)
	if err != nil {
		return TorrentUploadResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TorrentUploadResult{}, torrentUploadWriteError("commit torrent upload", err)
	}
	return result, nil
}

func copyTorrentFiles(ctx context.Context, tx pgx.Tx, torrentID int64, files []File, createdAt time.Time) error {
	rows := make([][]any, 0, len(files))
	for _, file := range files {
		rows = append(rows, []any{
			torrentID, int32(file.Index), append([]string(nil), file.PathComponents...),
			file.DisplayPath, file.LengthBytes, file.Padding, createdAt.UTC(),
		})
	}
	copied, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"torrents", "torrent_files"},
		[]string{"torrent_id", "file_index", "path_components", "display_path", "size_bytes", "is_padding", "created_at"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return err
	}
	if copied != int64(len(files)) {
		return ErrTorrentUploadStateConflict
	}
	return nil
}

func (repository *PostgresTorrentUploadRepository) ClaimUploadCleanupTasks(ctx context.Context, backendID StorageBackendID, eligibleBefore, now time.Time, batchSize int32, leaseDuration time.Duration) ([]TorrentUploadCleanupTask, error) {
	if backendID == "" || eligibleBefore.IsZero() || now.IsZero() || eligibleBefore.After(now) || batchSize < 1 || batchSize > 100 || leaseDuration <= 0 || leaseDuration > 10*time.Minute {
		return nil, ErrStorageInputInvalid
	}
	rows, err := torrentdb.New(repository.pool).ClaimTorrentUploadCleanupTasks(ctx, torrentdb.ClaimTorrentUploadCleanupTasksParams{
		BackendID: string(backendID), EligibleBefore: storageTimestamp(eligibleBefore),
		ClaimedAt: storageTimestamp(now), BatchSize: batchSize, LeaseUntil: storageTimestamp(now.Add(leaseDuration)),
	})
	if err != nil {
		return nil, fmt.Errorf("claim torrent upload cleanup rows: %w", err)
	}
	tasks := make([]TorrentUploadCleanupTask, 0, len(rows))
	for _, row := range rows {
		parsedBackend, backendErr := ParseStorageBackendID(row.BackendID)
		key, keyErr := ParseObjectKey(row.ObjectKey)
		if backendErr != nil || keyErr != nil || !row.CleanupLeaseToken.Valid || parsedBackend != backendID {
			return nil, errors.New("claimed torrent upload cleanup has invalid metadata")
		}
		tasks = append(tasks, TorrentUploadCleanupTask{
			UploadID: row.ID, BackendID: parsedBackend, ObjectKey: key,
			StorageVersionID: nullableStorageString(row.StorageVersionID), DeleteObject: row.DeleteObject,
			LeaseToken: uuid.UUID(row.CleanupLeaseToken.Bytes), Attempts: row.CleanupAttempts,
		})
	}
	return tasks, nil
}

func (repository *PostgresTorrentUploadRepository) MarkUploadAbandoned(ctx context.Context, task TorrentUploadCleanupTask, abandonedAt time.Time) error {
	if task.UploadID == uuid.Nil || task.BackendID == "" || task.ObjectKey == "" || task.LeaseToken == uuid.Nil || abandonedAt.IsZero() {
		return ErrStorageInputInvalid
	}
	rows, err := torrentdb.New(repository.pool).MarkTorrentUploadAbandoned(ctx, torrentdb.MarkTorrentUploadAbandonedParams{
		AbandonedAt: storageTimestamp(abandonedAt), UploadID: task.UploadID, LeaseToken: task.LeaseToken,
	})
	if err != nil {
		return fmt.Errorf("mark torrent upload abandoned: %w", err)
	}
	if rows != 1 {
		return ErrTorrentUploadStateConflict
	}
	return nil
}

func (repository *PostgresTorrentUploadRepository) ReleaseUploadCleanupTask(ctx context.Context, task TorrentUploadCleanupTask, releasedAt, retryAt time.Time, errorCode string) error {
	if task.UploadID == uuid.Nil || task.LeaseToken == uuid.Nil || releasedAt.IsZero() || !retryAt.After(releasedAt) || !validStorageErrorCode(errorCode) {
		return ErrStorageInputInvalid
	}
	rows, err := torrentdb.New(repository.pool).ReleaseTorrentUploadCleanupTask(ctx, torrentdb.ReleaseTorrentUploadCleanupTaskParams{
		AvailableAt: storageTimestamp(retryAt), LastErrorCode: errorCode, ReleasedAt: storageTimestamp(releasedAt.UTC()),
		UploadID: task.UploadID, LeaseToken: task.LeaseToken,
	})
	if err != nil {
		return fmt.Errorf("release torrent upload cleanup task: %w", err)
	}
	if rows != 1 {
		return ErrTorrentUploadStateConflict
	}
	return nil
}

func validateReserveTorrentUploadCommand(command ReserveTorrentUploadCommand) error {
	if command.ID == uuid.Nil || command.UploaderID == uuid.Nil || command.ObjectID == uuid.Nil ||
		command.PolicyRevisionID == uuid.Nil ||
		command.RequestFingerprint == (ObjectSHA256{}) || command.CategoryID == "" || !command.Descriptor.Valid() ||
		command.BackendID == "" || command.ObjectKey != TorrentObjectKey(command.Descriptor.SHA256) || command.OccurredAt.IsZero() {
		return ErrTorrentInputInvalid
	}
	return nil
}

func validateFinalizeTorrentUploadCommand(command FinalizeTorrentUploadCommand) error {
	if command.UploadID == uuid.Nil || command.RequestFingerprint == (ObjectSHA256{}) || command.OccurredAt.IsZero() ||
		command.PolicyRevisionID == uuid.Nil ||
		command.Torrent.Object.ID == uuid.Nil ||
		command.Torrent.State != StatePendingReview || command.Torrent.ID != 0 ||
		len(command.Files) != command.Torrent.FileCount || command.OccurredAt.Before(command.Torrent.SubmittedAt) {
		return ErrTorrentInputInvalid
	}
	for index, file := range command.Files {
		if file.Index != index || len(file.PathComponents) == 0 || file.DisplayPath == "" || file.LengthBytes < 0 {
			return ErrTorrentInputInvalid
		}
	}
	if len(command.Screenshots) != len(command.Torrent.Screenshots) {
		return ErrTorrentInputInvalid
	}
	for index, screenshot := range command.Screenshots {
		metadata := command.Torrent.Screenshots[index]
		if screenshot.TorrentScreenshot != metadata || screenshot.BackendID == "" ||
			screenshot.ObjectKey != TorrentScreenshotObjectKey(screenshot.ContentSHA256, screenshot.Extension) {
			return ErrTorrentInputInvalid
		}
	}
	return nil
}

func validateFinalizationAgainstReservation(command FinalizeTorrentUploadCommand, reservation TorrentUploadReservation) error {
	torrent := command.Torrent
	if reservation.RequestFingerprint != command.RequestFingerprint || reservation.UploaderID != torrent.UploaderID ||
		reservation.ObjectID != torrent.Object.ID ||
		reservation.PolicyRevisionID != command.PolicyRevisionID ||
		reservation.CategoryID != torrent.CategoryID || reservation.InfoHashV1 != torrent.InfoHashV1 ||
		reservation.Descriptor.SHA256 != torrent.Object.ContentSHA256 ||
		reservation.Descriptor.ByteLength != torrent.Object.ByteLength {
		return ErrTorrentUploadIdempotencyConflict
	}
	return nil
}

func torrentUploadReservationFromRow(row torrentdb.GetTorrentUploadForUpdateRow) (TorrentUploadReservation, error) {
	backendID, backendErr := ParseStorageBackendID(row.BackendID)
	objectKey, keyErr := ParseObjectKey(row.ObjectKey)
	state := TorrentUploadState(row.State)
	validState := state == TorrentUploadReserved || state == TorrentUploadObjectVerified || state == TorrentUploadCompleted || state == TorrentUploadCleaning || state == TorrentUploadAbandoned
	if backendErr != nil || keyErr != nil || !validState || !row.CreatedAt.Valid || len(row.RequestFingerprint) != 32 ||
		len(row.InfoHashV1) != 20 || len(row.ContentSha256) != 32 || row.ByteLength <= 0 {
		return TorrentUploadReservation{}, ErrTorrentUploadStateConflict
	}
	var fingerprint ObjectSHA256
	var infoHash InfoHashV1
	var digest ObjectSHA256
	copy(fingerprint[:], row.RequestFingerprint)
	copy(infoHash[:], row.InfoHashV1)
	copy(digest[:], row.ContentSha256)
	reservation := TorrentUploadReservation{
		ID: row.ID, UploaderID: row.UploaderID, RequestFingerprint: fingerprint,
		ObjectID: row.ObjectID, CategoryID: row.CategoryID,
		InfoHashV1: infoHash, Descriptor: StoredObjectDescriptor{SHA256: digest, ByteLength: row.ByteLength},
		BackendID: backendID, ObjectKey: objectKey, PolicyRevisionID: row.UploadPolicyRevisionID, State: state,
		StorageVersionID: nullableStorageString(row.StorageVersionID), CreatedAt: row.CreatedAt.Time.UTC(),
	}
	if row.ObjectCreated.Valid {
		reservation.ObjectCreated = row.ObjectCreated.Bool
	}
	if row.ObjectVerifiedAt.Valid {
		verifiedAt := row.ObjectVerifiedAt.Time.UTC()
		reservation.ObjectVerifiedAt = &verifiedAt
	}
	if row.CompletedAt.Valid {
		completedAt := row.CompletedAt.Time.UTC()
		reservation.CompletedAt = &completedAt
	}
	return reservation, nil
}

func completedTorrentUploadResult(ctx context.Context, queries *torrentdb.Queries, uploadID uuid.UUID) (TorrentUploadResult, error) {
	row, err := queries.GetCompletedTorrentUploadResult(ctx, uploadID)
	if errors.Is(err, pgx.ErrNoRows) {
		return TorrentUploadResult{}, ErrTorrentUploadStateConflict
	}
	if err != nil {
		return TorrentUploadResult{}, fmt.Errorf("read completed torrent upload: %w", err)
	}
	if len(row.InfoHashV1) != 20 || !row.SubmittedAt.Valid || row.ID <= 0 || row.FileCount < 1 || row.State != string(StatePendingReview) {
		return TorrentUploadResult{}, ErrTorrentUploadStateConflict
	}
	var infoHash InfoHashV1
	copy(infoHash[:], row.InfoHashV1)
	return TorrentUploadResult{
		ID: TorrentID(row.ID), InfoHashV1: infoHash,
		State: StatePendingReview, ContentName: row.ContentName,
		TotalSizeBytes: row.TotalSizeBytes, FileCount: int(row.FileCount), SubmittedAt: row.SubmittedAt.Time.UTC(),
	}, nil
}

func torrentUploadWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return ErrTorrentUploadDuplicate
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ TorrentUploadRepository = (*PostgresTorrentUploadRepository)(nil)
var _ TorrentUploadOrphanRepository = (*PostgresTorrentUploadRepository)(nil)
