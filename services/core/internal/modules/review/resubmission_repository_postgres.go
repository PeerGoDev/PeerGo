package review

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/generated/reviewdb"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

type PostgresResubmissionRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresResubmissionRepository(pool *pgxpool.Pool) (*PostgresResubmissionRepository, error) {
	if pool == nil {
		return nil, errors.New("torrent resubmission database is required")
	}
	return &PostgresResubmissionRepository{pool: pool}, nil
}

// Resubmit locks the current aggregate and the selected category, verifies the
// latest immutable rejection, then changes metadata, reopens review and writes
// the immutable uploader response in one transaction. There is deliberately no
// object-store or Tracker call because swarm identity and eligibility do not
// change during this transition.
func (repository *PostgresResubmissionRepository) Resubmit(
	ctx context.Context,
	command ResubmitCommand,
) (ResubmissionResult, error) {
	command, err := normalizedResubmitCommand(command)
	if err != nil {
		return ResubmissionResult{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ResubmissionResult{}, fmt.Errorf("begin torrent resubmission: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := reviewdb.New(tx)

	if result, found, replayErr := resumeTorrentResubmission(ctx, queries, command); found || replayErr != nil {
		if replayErr != nil {
			return ResubmissionResult{}, replayErr
		}
		if err := tx.Commit(ctx); err != nil {
			return ResubmissionResult{}, fmt.Errorf("commit replayed torrent resubmission: %w", err)
		}
		return result, nil
	}

	locked, err := queries.GetRejectedTorrentForResubmissionForUpdate(ctx, int64(command.TorrentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ResubmissionResult{}, ErrTorrentResubmissionNotFound
	}
	if err != nil {
		return ResubmissionResult{}, fmt.Errorf("lock torrent resubmission target: %w", err)
	}
	// Treat another uploader's UUID as absent so this private write surface does
	// not become an oracle for rejected submissions owned by other accounts.
	if locked.UploaderID != command.UploaderID {
		return ResubmissionResult{}, ErrTorrentResubmissionNotFound
	}
	if locked.State != string(torrents.StateRejected) || locked.Version != command.ExpectedVersion {
		// An exact concurrent retry can wake after the first transaction commits.
		// Re-read its immutable request record before returning a stale-state error.
		if result, found, replayErr := resumeTorrentResubmission(ctx, queries, command); found || replayErr != nil {
			if replayErr != nil {
				return ResubmissionResult{}, replayErr
			}
			if err := tx.Commit(ctx); err != nil {
				return ResubmissionResult{}, fmt.Errorf("commit concurrently replayed torrent resubmission: %w", err)
			}
			return result, nil
		}
		if locked.Version != command.ExpectedVersion {
			return ResubmissionResult{}, ErrTorrentResubmissionVersionConflict
		}
		return ResubmissionResult{}, ErrTorrentResubmissionStateConflict
	}
	if !validResubmissionTarget(locked) {
		return ResubmissionResult{}, ErrTorrentResubmissionInvariant
	}
	if !MetadataResubmissionAllowed(torrents.State(locked.State), ReasonCode(locked.ReasonCode)) {
		return ResubmissionResult{}, ErrTorrentResubmissionNotAllowed
	}
	if _, err := queries.GetEnabledTorrentResubmissionCategory(ctx, command.Metadata.CategoryID); errors.Is(err, pgx.ErrNoRows) {
		return ResubmissionResult{}, ErrTorrentResubmissionCategoryUnavailable
	} else if err != nil {
		return ResubmissionResult{}, fmt.Errorf("lock torrent resubmission category: %w", err)
	}

	aggregate := torrents.Torrent{
		ID: torrents.TorrentID(locked.ID), UploaderID: locked.UploaderID,
		CategoryID: locked.CategoryID, Title: locked.Title, Subtitle: locked.Subtitle,
		State: torrents.State(locked.State), Version: locked.Version,
		SubmittedAt: locked.SubmittedAt.Time.UTC(), StateChangedAt: locked.StateChangedAt.Time.UTC(),
	}
	if err := aggregate.ResubmitWithMetadata(command.Metadata, command.OccurredAt); err != nil {
		switch {
		case errors.Is(err, torrents.ErrTorrentMetadataUnchanged):
			return ResubmissionResult{}, ErrTorrentResubmissionUnchanged
		case errors.Is(err, torrents.ErrTorrentStateConflict):
			return ResubmissionResult{}, ErrTorrentResubmissionStateConflict
		default:
			return ResubmissionResult{}, fmt.Errorf("apply torrent resubmission transition: %w", err)
		}
	}

	updated, err := queries.ResubmitRejectedTorrent(ctx, reviewdb.ResubmitRejectedTorrentParams{
		CategoryID: aggregate.CategoryID, Title: aggregate.Title, Subtitle: aggregate.Subtitle,
		OccurredAt: reviewTimestamp(command.OccurredAt), TorrentID: int64(aggregate.ID),
		UploaderID: command.UploaderID, ExpectedVersion: command.ExpectedVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ResubmissionResult{}, ErrTorrentResubmissionVersionConflict
	}
	if err != nil {
		return ResubmissionResult{}, fmt.Errorf("update rejected torrent for resubmission: %w", err)
	}
	if updated.ID != int64(aggregate.ID) || updated.State != string(aggregate.State) ||
		updated.Version != aggregate.Version || !updated.StateChangedAt.Valid ||
		!updated.StateChangedAt.Time.Equal(aggregate.StateChangedAt) {
		return ResubmissionResult{}, ErrTorrentResubmissionInvariant
	}

	if err := queries.InsertTorrentResubmission(ctx, reviewdb.InsertTorrentResubmissionParams{
		ResubmissionID: command.ID, TorrentID: int64(aggregate.ID), DecisionID: locked.DecisionID,
		ExpectedTorrentVersion: command.ExpectedVersion, ResultingTorrentVersion: aggregate.Version,
		CategoryID: aggregate.CategoryID, Title: aggregate.Title, Subtitle: aggregate.Subtitle,
		CorrectionNote: command.CorrectionNote, AuthorizationDecisionID: command.Authorization.ID,
		OccurredAt: reviewTimestamp(command.OccurredAt),
	}); err != nil {
		return ResubmissionResult{}, mapResubmissionWriteError("insert torrent resubmission", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ResubmissionResult{}, fmt.Errorf("commit torrent resubmission: %w", err)
	}
	return ResubmissionResult{
		ID: command.ID, TorrentID: aggregate.ID, State: aggregate.State,
		Version: aggregate.Version, Metadata: command.Metadata,
		ReviewRequestedAt: aggregate.StateChangedAt,
	}, nil
}

func normalizedResubmitCommand(command ResubmitCommand) (ResubmitCommand, error) {
	metadata, err := torrents.NewEditableMetadata(
		command.Metadata.CategoryID,
		command.Metadata.Title,
		command.Metadata.Subtitle,
	)
	if err != nil || metadata != command.Metadata {
		return ResubmitCommand{}, ErrTorrentResubmissionInput
	}
	note := strings.TrimSpace(command.CorrectionNote)
	normalizedMetadata, normalizedNote, err := normalizeResubmissionInput(ResubmitInput{
		ID: command.ID, TorrentID: command.TorrentID,
		ExpectedVersion: command.ExpectedVersion, CategoryID: metadata.CategoryID,
		Title: metadata.Title, Subtitle: metadata.Subtitle, CorrectionNote: note,
	})
	if err != nil || normalizedMetadata != command.Metadata || normalizedNote != command.CorrectionNote ||
		command.UploaderID == uuid.Nil || command.OccurredAt.IsZero() ||
		!command.Authorization.Allow || command.Authorization.ID == uuid.Nil {
		return ResubmitCommand{}, ErrTorrentResubmissionInput
	}
	return command, nil
}

func resumeTorrentResubmission(
	ctx context.Context,
	queries *reviewdb.Queries,
	command ResubmitCommand,
) (ResubmissionResult, bool, error) {
	row, err := queries.GetTorrentResubmissionByID(ctx, command.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ResubmissionResult{}, false, nil
	}
	if err != nil {
		return ResubmissionResult{}, false, fmt.Errorf("read torrent resubmission idempotency record: %w", err)
	}
	if torrents.TorrentID(row.TorrentID) != command.TorrentID || row.UploaderID != command.UploaderID ||
		row.ExpectedTorrentVersion != command.ExpectedVersion || row.CategoryID != command.Metadata.CategoryID ||
		row.Title != command.Metadata.Title || row.Subtitle != command.Metadata.Subtitle ||
		row.CorrectionNote != command.CorrectionNote {
		return ResubmissionResult{}, true, ErrTorrentResubmissionIdempotencyConflict
	}
	if row.ID == uuid.Nil || row.RespondsToDecisionID == uuid.Nil || row.ResultingTorrentVersion != row.ExpectedTorrentVersion+1 ||
		!row.OccurredAt.Valid {
		return ResubmissionResult{}, true, ErrTorrentResubmissionInvariant
	}
	return ResubmissionResult{
		ID: row.ID, TorrentID: torrents.TorrentID(row.TorrentID),
		State: torrents.StatePendingReview, Version: row.ResultingTorrentVersion,
		Metadata: torrents.EditableMetadata{
			CategoryID: row.CategoryID,
			Title:      row.Title,
			Subtitle:   row.Subtitle,
		},
		ReviewRequestedAt: row.OccurredAt.Time.UTC(),
	}, true, nil
}

func validResubmissionTarget(row reviewdb.GetRejectedTorrentForResubmissionForUpdateRow) bool {
	return row.ID > 0 && row.UploaderID != uuid.Nil &&
		row.CategoryID != "" && strings.TrimSpace(row.Title) != "" && row.Version > 1 &&
		row.SubmittedAt.Valid && row.PublishedAt.Valid == false && row.StateChangedAt.Valid &&
		row.DecisionID != uuid.Nil && row.Decision == string(DecisionReject) &&
		row.ResultingTorrentVersion == row.Version && row.DecisionOccurredAt.Valid &&
		row.DecisionOccurredAt.Time.Equal(row.StateChangedAt.Time) &&
		!row.StateChangedAt.Time.Before(row.SubmittedAt.Time)
}

func mapResubmissionWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		switch postgresError.ConstraintName {
		case "torrent_resubmissions_pkey":
			return ErrTorrentResubmissionIdempotencyConflict
		case "torrent_resubmissions_responds_to_decision_id_key":
			return ErrTorrentResubmissionStateConflict
		case "torrent_resubmissions_torrent_id_expected_torrent_version_key",
			"torrent_resubmissions_torrent_id_resulting_torrent_version_key":
			return ErrTorrentResubmissionVersionConflict
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ ResubmissionRepository = (*PostgresResubmissionRepository)(nil)
