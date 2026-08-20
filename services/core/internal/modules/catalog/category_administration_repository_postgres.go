package catalog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/generated/catalogdb"
)

// PostgresCategoryAdministrationRepository owns the mutation transaction.
// Category state and its immutable audit outbox event either commit together
// or both roll back; optimistic concurrency is checked while the row is locked.
type PostgresCategoryAdministrationRepository struct {
	pool         *pgxpool.Pool
	queries      *catalogdb.Queries
	eventBuilder CategoryEventBuilder
	newAppender  func(pgx.Tx) auditevent.Appender
}

func NewPostgresCategoryAdministrationRepository(pool *pgxpool.Pool, eventBuilder CategoryEventBuilder, newAppender func(pgx.Tx) auditevent.Appender) (*PostgresCategoryAdministrationRepository, error) {
	if pool == nil || eventBuilder == nil || newAppender == nil {
		return nil, errors.New("category administration repository dependencies are required")
	}
	return &PostgresCategoryAdministrationRepository{
		pool: pool, queries: catalogdb.New(pool), eventBuilder: eventBuilder, newAppender: newAppender,
	}, nil
}

func (repository *PostgresCategoryAdministrationRepository) ListManagedCategories(ctx context.Context) ([]ManagedCategory, error) {
	rows, err := repository.queries.ListManagedCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("query managed categories: %w", err)
	}
	result := make([]ManagedCategory, 0, len(rows))
	for _, row := range rows {
		if !row.CreatedAt.Valid || !row.UpdatedAt.Valid {
			return nil, fmt.Errorf("%w: category %q has an invalid timestamp", errCatalogProjectionInvalid, row.ID)
		}
		result = append(result, ManagedCategory{
			ID: row.ID, Name: row.Name, DisplayOrder: int(row.DisplayOrder), Enabled: row.Enabled,
			Version: row.Version, TorrentCount: row.TorrentCount, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
		})
	}
	return result, nil
}

func (repository *PostgresCategoryAdministrationRepository) CreateCategory(ctx context.Context, command CreateCategoryCommand) (ManagedCategory, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ManagedCategory{}, fmt.Errorf("begin category create: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := catalogdb.New(tx)
	row, err := queries.CreateManagedCategory(ctx, catalogdb.CreateManagedCategoryParams{
		CategoryID: command.ID, CategoryName: command.Name, DisplayOrder: int32(command.DisplayOrder),
		Enabled: command.Enabled, OccurredAt: categoryTimestamp(command.OccurredAt),
	})
	if isUniqueViolation(err) {
		return ManagedCategory{}, ErrCategoryAlreadyExists
	}
	if err != nil {
		return ManagedCategory{}, fmt.Errorf("insert managed category: %w", err)
	}
	result, err := managedCategoryFromCreatedRow(row)
	if err != nil {
		return ManagedCategory{}, err
	}
	if err := repository.appendCategoryEvent(ctx, tx, CategoryAuditInput{
		Transition: CategoryTransitionCreated, OccurredAt: command.OccurredAt,
		ActorID: command.ActorID, CategoryID: command.ID, Reason: command.Reason,
		Authorization: command.Authorization, After: categoryAuditState(result),
	}); err != nil {
		return ManagedCategory{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedCategory{}, fmt.Errorf("commit category create: %w", err)
	}
	return result, nil
}

func (repository *PostgresCategoryAdministrationRepository) UpdateCategory(ctx context.Context, command UpdateCategoryCommand) (ManagedCategory, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ManagedCategory{}, fmt.Errorf("begin category update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := catalogdb.New(tx)
	locked, err := queries.GetManagedCategoryForUpdate(ctx, command.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedCategory{}, ErrCategoryNotFound
	}
	if err != nil {
		return ManagedCategory{}, fmt.Errorf("lock managed category: %w", err)
	}
	if locked.Version != command.ExpectedVersion {
		return ManagedCategory{}, ErrCategoryVersionConflict
	}
	if !locked.CreatedAt.Valid || !locked.UpdatedAt.Valid {
		return ManagedCategory{}, fmt.Errorf("%w: category %q has an invalid timestamp", errCatalogProjectionInvalid, locked.ID)
	}
	before := ManagedCategory{
		ID: locked.ID, Name: locked.Name, DisplayOrder: int(locked.DisplayOrder), Enabled: locked.Enabled,
		Version: locked.Version, CreatedAt: locked.CreatedAt.Time, UpdatedAt: locked.UpdatedAt.Time,
	}
	row, err := queries.UpdateManagedCategory(ctx, catalogdb.UpdateManagedCategoryParams{
		CategoryName: command.Name, DisplayOrder: int32(command.DisplayOrder), Enabled: command.Enabled,
		OccurredAt: categoryTimestamp(command.OccurredAt), CategoryID: command.ID, ExpectedVersion: command.ExpectedVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedCategory{}, ErrCategoryVersionConflict
	}
	if err != nil {
		return ManagedCategory{}, fmt.Errorf("update managed category: %w", err)
	}
	torrentCount, err := queries.CountCategoryTorrents(ctx, command.ID)
	if err != nil {
		return ManagedCategory{}, fmt.Errorf("count category torrents: %w", err)
	}
	result, err := managedCategoryFromUpdatedRow(row, torrentCount)
	if err != nil {
		return ManagedCategory{}, err
	}
	before.TorrentCount = torrentCount
	beforeState := categoryAuditState(before)
	if err := repository.appendCategoryEvent(ctx, tx, CategoryAuditInput{
		Transition: CategoryTransitionUpdated, OccurredAt: command.OccurredAt,
		ActorID: command.ActorID, CategoryID: command.ID, Reason: command.Reason,
		ExpectedVersion: command.ExpectedVersion, Authorization: command.Authorization,
		Before: &beforeState, After: categoryAuditState(result),
	}); err != nil {
		return ManagedCategory{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedCategory{}, fmt.Errorf("commit category update: %w", err)
	}
	return result, nil
}

func (repository *PostgresCategoryAdministrationRepository) appendCategoryEvent(ctx context.Context, tx pgx.Tx, input CategoryAuditInput) error {
	event, err := repository.eventBuilder.BuildCategoryEvent(input)
	if err != nil {
		return fmt.Errorf("build category audit event: %w", err)
	}
	if err := repository.newAppender(tx).Append(ctx, event); err != nil {
		return fmt.Errorf("append category audit event: %w", err)
	}
	return nil
}

func managedCategoryFromCreatedRow(row catalogdb.CreateManagedCategoryRow) (ManagedCategory, error) {
	if !row.CreatedAt.Valid || !row.UpdatedAt.Valid {
		return ManagedCategory{}, fmt.Errorf("%w: category %q has an invalid timestamp", errCatalogProjectionInvalid, row.ID)
	}
	return ManagedCategory{
		ID: row.ID, Name: row.Name, DisplayOrder: int(row.DisplayOrder), Enabled: row.Enabled,
		Version: row.Version, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}, nil
}

func managedCategoryFromUpdatedRow(row catalogdb.UpdateManagedCategoryRow, torrentCount int64) (ManagedCategory, error) {
	if !row.CreatedAt.Valid || !row.UpdatedAt.Valid {
		return ManagedCategory{}, fmt.Errorf("%w: category %q has an invalid timestamp", errCatalogProjectionInvalid, row.ID)
	}
	return ManagedCategory{
		ID: row.ID, Name: row.Name, DisplayOrder: int(row.DisplayOrder), Enabled: row.Enabled,
		Version: row.Version, TorrentCount: torrentCount, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}, nil
}

func categoryAuditState(category ManagedCategory) CategoryAuditState {
	return CategoryAuditState{
		ID: category.ID, Name: category.Name, DisplayOrder: category.DisplayOrder,
		Enabled: category.Enabled, Version: category.Version,
	}
}

func isUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}

func categoryTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

var _ CategoryAdministrationRepository = (*PostgresCategoryAdministrationRepository)(nil)
