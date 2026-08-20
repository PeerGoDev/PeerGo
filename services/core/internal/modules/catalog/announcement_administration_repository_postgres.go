package catalog

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/generated/catalogdb"
)

// PostgresAnnouncementAdministrationRepository keeps revision append, pointer
// change, optimistic version bump and audit outbox append in one transaction.
// Draft content can therefore never become public through a partial commit.
type PostgresAnnouncementAdministrationRepository struct {
	pool         *pgxpool.Pool
	queries      *catalogdb.Queries
	eventBuilder AnnouncementEventBuilder
	newAppender  func(pgx.Tx) auditevent.Appender
}

func NewPostgresAnnouncementAdministrationRepository(pool *pgxpool.Pool, eventBuilder AnnouncementEventBuilder, newAppender func(pgx.Tx) auditevent.Appender) (*PostgresAnnouncementAdministrationRepository, error) {
	if pool == nil || eventBuilder == nil || newAppender == nil {
		return nil, errors.New("announcement administration repository dependencies are required")
	}
	return &PostgresAnnouncementAdministrationRepository{
		pool: pool, queries: catalogdb.New(pool), eventBuilder: eventBuilder, newAppender: newAppender,
	}, nil
}

func (repository *PostgresAnnouncementAdministrationRepository) ListManagedAnnouncements(ctx context.Context, limit, offset int, observedAt time.Time) (ManagedAnnouncementPage, error) {
	total, err := repository.queries.CountManagedAnnouncements(ctx)
	if err != nil {
		return ManagedAnnouncementPage{}, fmt.Errorf("count managed announcements: %w", err)
	}
	if total > math.MaxInt {
		return ManagedAnnouncementPage{}, fmt.Errorf("%w: announcement count exceeds platform integer", errCatalogProjectionInvalid)
	}
	rows, err := repository.queries.ListManagedAnnouncements(ctx, catalogdb.ListManagedAnnouncementsParams{
		ResultLimit: int32(limit), ResultOffset: int32(offset),
	})
	if err != nil {
		return ManagedAnnouncementPage{}, fmt.Errorf("query managed announcements: %w", err)
	}
	items := make([]ManagedAnnouncement, 0, len(rows))
	for _, row := range rows {
		item, conversionErr := managedAnnouncementFromValues(
			row.ID, row.Title, row.Summary, row.Body, row.BodyFormat,
			row.Version, row.RevisionNumber, row.HasUnpublishedChanges,
			row.HasPublishedRevision, row.HasScheduledRevision,
			row.PublishedAt, row.ScheduledFor, row.WithdrawnAt,
			row.CreatedAt, row.UpdatedAt, observedAt,
		)
		if conversionErr != nil {
			return ManagedAnnouncementPage{}, conversionErr
		}
		items = append(items, item)
	}
	return ManagedAnnouncementPage{Items: items, Total: int(total), Limit: limit, Offset: offset}, nil
}

func (repository *PostgresAnnouncementAdministrationRepository) GetManagedAnnouncement(ctx context.Context, announcementID string, observedAt time.Time) (ManagedAnnouncement, error) {
	row, err := repository.queries.GetManagedAnnouncement(ctx, announcementID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedAnnouncement{}, ErrManagedAnnouncementNotFound
	}
	if err != nil {
		return ManagedAnnouncement{}, fmt.Errorf("query managed announcement: %w", err)
	}
	return managedAnnouncementFromProjection(row, observedAt)
}

func (repository *PostgresAnnouncementAdministrationRepository) ListAnnouncementRevisions(ctx context.Context, announcementID string, limit, offset int) (AnnouncementRevisionPage, error) {
	if _, err := repository.queries.GetManagedAnnouncement(ctx, announcementID); errors.Is(err, pgx.ErrNoRows) {
		return AnnouncementRevisionPage{}, ErrManagedAnnouncementNotFound
	} else if err != nil {
		return AnnouncementRevisionPage{}, fmt.Errorf("verify managed announcement: %w", err)
	}
	total, err := repository.queries.CountAnnouncementRevisions(ctx, announcementID)
	if err != nil {
		return AnnouncementRevisionPage{}, fmt.Errorf("count announcement revisions: %w", err)
	}
	if total > math.MaxInt {
		return AnnouncementRevisionPage{}, fmt.Errorf("%w: revision count exceeds platform integer", errCatalogProjectionInvalid)
	}
	rows, err := repository.queries.ListAnnouncementRevisions(ctx, catalogdb.ListAnnouncementRevisionsParams{
		AnnouncementID: announcementID, ResultLimit: int32(limit), ResultOffset: int32(offset),
	})
	if err != nil {
		return AnnouncementRevisionPage{}, fmt.Errorf("query announcement revisions: %w", err)
	}
	items := make([]AnnouncementRevisionSummary, 0, len(rows))
	for _, row := range rows {
		if !row.CreatedAt.Valid || row.RevisionNumber < 1 || !ValidAnnouncementID(announcementID) {
			return AnnouncementRevisionPage{}, fmt.Errorf("%w: invalid announcement revision", errCatalogProjectionInvalid)
		}
		format := AnnouncementBodyFormat(row.BodyFormat)
		origin := AnnouncementRevisionOrigin(row.Origin)
		if (format != AnnouncementBodyPlainText && format != AnnouncementBodyLegacyBBCode) ||
			(origin != AnnouncementRevisionMigration && origin != AnnouncementRevisionDevelopmentSeed && origin != AnnouncementRevisionStaff) {
			return AnnouncementRevisionPage{}, fmt.Errorf("%w: invalid announcement revision enum", errCatalogProjectionInvalid)
		}
		items = append(items, AnnouncementRevisionSummary{
			RevisionNumber: row.RevisionNumber, Title: row.Title, Summary: row.Summary,
			BodyFormat: format, Origin: origin, EditorDisplayName: row.EditorDisplayName.String,
			IsDraft: row.IsDraft, IsPublished: row.IsPublished, IsScheduled: row.IsScheduled,
			CreatedAt: row.CreatedAt.Time.UTC(),
		})
	}
	return AnnouncementRevisionPage{Items: items, Total: int(total), Limit: limit, Offset: offset}, nil
}

func (repository *PostgresAnnouncementAdministrationRepository) CreateAnnouncementDraft(ctx context.Context, command CreateAnnouncementDraftCommand) (ManagedAnnouncement, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ManagedAnnouncement{}, fmt.Errorf("begin announcement create: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := catalogdb.New(tx)
	_, err = queries.CreateAnnouncementAggregate(ctx, catalogdb.CreateAnnouncementAggregateParams{
		AnnouncementID: command.ID, OccurredAt: announcementTimestamp(command.OccurredAt),
	})
	if isUniqueViolation(err) {
		return ManagedAnnouncement{}, ErrAnnouncementAlreadyExists
	}
	if err != nil {
		return ManagedAnnouncement{}, fmt.Errorf("insert announcement aggregate: %w", err)
	}
	revisionID, err := repository.insertRevision(ctx, queries, command.ID, 1, command.Title, command.Summary, command.Body, command.BodyFormat, command.ActorID, command.OccurredAt)
	if err != nil {
		return ManagedAnnouncement{}, err
	}
	rows, err := queries.AttachInitialAnnouncementDraft(ctx, catalogdb.AttachInitialAnnouncementDraftParams{
		RevisionID: revisionID, OccurredAt: announcementTimestamp(command.OccurredAt), AnnouncementID: command.ID,
	})
	if err != nil {
		return ManagedAnnouncement{}, fmt.Errorf("attach initial announcement draft: %w", err)
	}
	if rows != 1 {
		return ManagedAnnouncement{}, fmt.Errorf("attach initial announcement draft: expected one aggregate, got %d", rows)
	}
	after, err := repository.getManagedWithQueries(ctx, queries, command.ID, command.OccurredAt)
	if err != nil {
		return ManagedAnnouncement{}, err
	}
	if err := repository.appendAnnouncementEvent(ctx, tx, AnnouncementAuditInput{
		Transition: AnnouncementTransitionDraftCreated, OccurredAt: command.OccurredAt,
		ActorID: command.ActorID, AnnouncementID: command.ID, Reason: command.Reason,
		Authorization: command.Authorization, After: announcementAuditState(after),
	}); err != nil {
		return ManagedAnnouncement{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedAnnouncement{}, fmt.Errorf("commit announcement create: %w", err)
	}
	return after, nil
}

func (repository *PostgresAnnouncementAdministrationRepository) UpdateAnnouncementDraft(ctx context.Context, command UpdateAnnouncementDraftCommand) (ManagedAnnouncement, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ManagedAnnouncement{}, fmt.Errorf("begin announcement draft update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := catalogdb.New(tx)
	locked, err := queries.GetAnnouncementAggregateForUpdate(ctx, command.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedAnnouncement{}, ErrManagedAnnouncementNotFound
	}
	if err != nil {
		return ManagedAnnouncement{}, fmt.Errorf("lock announcement aggregate: %w", err)
	}
	if locked.Version != command.ExpectedVersion {
		return ManagedAnnouncement{}, ErrAnnouncementVersionConflict
	}
	if locked.ScheduledFor.Valid && locked.ScheduledFor.Time.After(command.OccurredAt) {
		return ManagedAnnouncement{}, ErrAnnouncementPublicationConflict
	}
	before, err := repository.getManagedWithQueries(ctx, queries, command.ID, command.OccurredAt)
	if err != nil {
		return ManagedAnnouncement{}, err
	}
	if before.Title == command.Title && before.Summary == command.Summary && before.Body == command.Body && before.BodyFormat == command.BodyFormat {
		return ManagedAnnouncement{}, ErrAnnouncementNoChanges
	}
	revisionNumber := locked.LatestRevisionNumber + 1
	revisionID, err := repository.insertRevision(ctx, queries, command.ID, revisionNumber, command.Title, command.Summary, command.Body, command.BodyFormat, command.ActorID, command.OccurredAt)
	if err != nil {
		return ManagedAnnouncement{}, err
	}
	rows, err := queries.UpdateAnnouncementDraftPointer(ctx, catalogdb.UpdateAnnouncementDraftPointerParams{
		RevisionID: revisionID, RevisionNumber: revisionNumber, OccurredAt: announcementTimestamp(command.OccurredAt),
		AnnouncementID: command.ID, ExpectedVersion: command.ExpectedVersion,
	})
	if err != nil {
		return ManagedAnnouncement{}, fmt.Errorf("attach announcement draft revision: %w", err)
	}
	if rows != 1 {
		return ManagedAnnouncement{}, ErrAnnouncementVersionConflict
	}
	after, err := repository.getManagedWithQueries(ctx, queries, command.ID, command.OccurredAt)
	if err != nil {
		return ManagedAnnouncement{}, err
	}
	beforeState := announcementAuditState(before)
	if err := repository.appendAnnouncementEvent(ctx, tx, AnnouncementAuditInput{
		Transition: AnnouncementTransitionDraftUpdated, OccurredAt: command.OccurredAt,
		ActorID: command.ActorID, AnnouncementID: command.ID, Reason: command.Reason,
		ExpectedVersion: command.ExpectedVersion, Authorization: command.Authorization,
		Before: &beforeState, After: announcementAuditState(after),
	}); err != nil {
		return ManagedAnnouncement{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedAnnouncement{}, fmt.Errorf("commit announcement draft update: %w", err)
	}
	return after, nil
}

func (repository *PostgresAnnouncementAdministrationRepository) ChangeAnnouncementPublication(ctx context.Context, command ChangeAnnouncementPublicationCommand) (ManagedAnnouncement, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ManagedAnnouncement{}, fmt.Errorf("begin announcement publication change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := catalogdb.New(tx)
	locked, err := queries.GetAnnouncementAggregateForUpdate(ctx, command.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedAnnouncement{}, ErrManagedAnnouncementNotFound
	}
	if err != nil {
		return ManagedAnnouncement{}, fmt.Errorf("lock announcement aggregate: %w", err)
	}
	if locked.Version != command.ExpectedVersion {
		return ManagedAnnouncement{}, ErrAnnouncementVersionConflict
	}
	before, err := repository.getManagedWithQueries(ctx, queries, command.ID, command.OccurredAt)
	if err != nil {
		return ManagedAnnouncement{}, err
	}
	var transition AnnouncementTransition
	var affected int64
	switch command.Action {
	case AnnouncementPublishNow:
		transition = AnnouncementTransitionPublished
		affected, err = queries.PublishAnnouncementDraftNow(ctx, catalogdb.PublishAnnouncementDraftNowParams{
			OccurredAt: announcementTimestamp(command.OccurredAt), AnnouncementID: command.ID, ExpectedVersion: command.ExpectedVersion,
		})
	case AnnouncementSchedule:
		transition = AnnouncementTransitionScheduled
		affected, err = queries.ScheduleAnnouncementDraft(ctx, catalogdb.ScheduleAnnouncementDraftParams{
			ScheduledFor: announcementTimestamp(*command.ScheduledFor), OccurredAt: announcementTimestamp(command.OccurredAt),
			AnnouncementID: command.ID, ExpectedVersion: command.ExpectedVersion,
		})
	case AnnouncementCancelSchedule:
		transition = AnnouncementTransitionScheduleCanceled
		affected, err = queries.CancelAnnouncementSchedule(ctx, catalogdb.CancelAnnouncementScheduleParams{
			OccurredAt: announcementTimestamp(command.OccurredAt), AnnouncementID: command.ID, ExpectedVersion: command.ExpectedVersion,
		})
	case AnnouncementWithdraw:
		if locked.WithdrawnAt.Valid {
			return ManagedAnnouncement{}, ErrAnnouncementPublicationConflict
		}
		transition = AnnouncementTransitionWithdrawn
		affected, err = queries.WithdrawAnnouncement(ctx, catalogdb.WithdrawAnnouncementParams{
			OccurredAt: announcementTimestamp(command.OccurredAt), AnnouncementID: command.ID, ExpectedVersion: command.ExpectedVersion,
		})
	default:
		return ManagedAnnouncement{}, ErrAnnouncementAdministrationInput
	}
	if err != nil {
		return ManagedAnnouncement{}, fmt.Errorf("update announcement publication pointers: %w", err)
	}
	if affected != 1 {
		return ManagedAnnouncement{}, ErrAnnouncementPublicationConflict
	}
	after, err := repository.getManagedWithQueries(ctx, queries, command.ID, command.OccurredAt)
	if err != nil {
		return ManagedAnnouncement{}, err
	}
	beforeState := announcementAuditState(before)
	if err := repository.appendAnnouncementEvent(ctx, tx, AnnouncementAuditInput{
		Transition: transition, OccurredAt: command.OccurredAt, ActorID: command.ActorID,
		AnnouncementID: command.ID, Reason: command.Reason, ExpectedVersion: command.ExpectedVersion,
		Authorization: command.Authorization, Before: &beforeState, After: announcementAuditState(after),
	}); err != nil {
		return ManagedAnnouncement{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedAnnouncement{}, fmt.Errorf("commit announcement publication change: %w", err)
	}
	return after, nil
}

func (repository *PostgresAnnouncementAdministrationRepository) insertRevision(ctx context.Context, queries *catalogdb.Queries, announcementID string, revisionNumber int64, title, summary, body string, format AnnouncementBodyFormat, actorID uuid.UUID, occurredAt time.Time) (int64, error) {
	revisionID, err := queries.InsertAnnouncementRevision(ctx, catalogdb.InsertAnnouncementRevisionParams{
		AnnouncementID: announcementID, RevisionNumber: revisionNumber,
		Title: title, Summary: summary, Body: body, BodyFormat: string(format),
		CreatedByUserID: pgtype.UUID{Bytes: actorID, Valid: true}, OccurredAt: announcementTimestamp(occurredAt),
	})
	if err != nil {
		return 0, fmt.Errorf("insert announcement revision: %w", err)
	}
	return revisionID, nil
}

func (repository *PostgresAnnouncementAdministrationRepository) getManagedWithQueries(ctx context.Context, queries *catalogdb.Queries, announcementID string, observedAt time.Time) (ManagedAnnouncement, error) {
	row, err := queries.GetManagedAnnouncement(ctx, announcementID)
	if err != nil {
		return ManagedAnnouncement{}, fmt.Errorf("read announcement transaction projection: %w", err)
	}
	return managedAnnouncementFromProjection(row, observedAt)
}

func (repository *PostgresAnnouncementAdministrationRepository) appendAnnouncementEvent(ctx context.Context, tx pgx.Tx, input AnnouncementAuditInput) error {
	event, err := repository.eventBuilder.BuildAnnouncementEvent(input)
	if err != nil {
		return fmt.Errorf("build announcement audit event: %w", err)
	}
	if err := repository.newAppender(tx).Append(ctx, event); err != nil {
		return fmt.Errorf("append announcement audit event: %w", err)
	}
	return nil
}

func managedAnnouncementFromProjection(row catalogdb.CatalogManagedAnnouncementProjection, observedAt time.Time) (ManagedAnnouncement, error) {
	return managedAnnouncementFromValues(
		row.ID, row.Title, row.Summary, row.Body, row.BodyFormat,
		row.Version, row.RevisionNumber, row.HasUnpublishedChanges,
		row.HasPublishedRevision, row.HasScheduledRevision,
		row.PublishedAt, row.ScheduledFor, row.WithdrawnAt,
		row.CreatedAt, row.UpdatedAt, observedAt,
	)
}

func managedAnnouncementFromValues(id, title, summary, body, bodyFormat string, version, revisionNumber int64, hasDraft, hasPublished, hasScheduled bool, publishedAt, scheduledFor, withdrawnAt, createdAt, updatedAt pgtype.Timestamptz, observedAt time.Time) (ManagedAnnouncement, error) {
	format := AnnouncementBodyFormat(bodyFormat)
	if !ValidAnnouncementID(id) || version < 1 || revisionNumber < 1 || !createdAt.Valid || !updatedAt.Valid ||
		(format != AnnouncementBodyPlainText && format != AnnouncementBodyLegacyBBCode) ||
		(hasScheduled != scheduledFor.Valid) {
		return ManagedAnnouncement{}, fmt.Errorf("%w: announcement %q", errCatalogProjectionInvalid, id)
	}
	status := ManagedAnnouncementDraft
	effectivePublishedAt := optionalAnnouncementTime(publishedAt)
	if withdrawnAt.Valid {
		status = ManagedAnnouncementWithdrawn
	} else if hasScheduled && scheduledFor.Time.After(observedAt) {
		status = ManagedAnnouncementScheduled
	} else if hasScheduled {
		status = ManagedAnnouncementPublished
		effectivePublishedAt = optionalAnnouncementTime(scheduledFor)
	} else if hasPublished {
		status = ManagedAnnouncementPublished
	}
	return ManagedAnnouncement{
		ID: id, Title: title, Summary: summary, Body: body, BodyFormat: format,
		Status: status, Version: version, RevisionNumber: revisionNumber,
		HasUnpublishedChanges: hasDraft, PublishedAt: effectivePublishedAt,
		ScheduledFor: optionalAnnouncementTime(scheduledFor),
		CreatedAt:    createdAt.Time.UTC(), UpdatedAt: updatedAt.Time.UTC(),
	}, nil
}

func optionalAnnouncementTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}

func announcementAuditState(value ManagedAnnouncement) AnnouncementAuditState {
	return AnnouncementAuditState{
		ID: value.ID, Title: value.Title, Summary: value.Summary, Body: value.Body,
		BodyFormat: value.BodyFormat, Status: value.Status, Version: value.Version,
		RevisionNumber: value.RevisionNumber, HasUnpublishedChanges: value.HasUnpublishedChanges,
		PublishedAt: value.PublishedAt, ScheduledFor: value.ScheduledFor,
	}
}

func announcementTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

var _ AnnouncementAdministrationRepository = (*PostgresAnnouncementAdministrationRepository)(nil)
