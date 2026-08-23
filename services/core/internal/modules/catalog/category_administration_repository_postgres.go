package catalog

import (
	"context"
	"encoding/json"
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
			Facets: make([]ManagedCategoryFacet, 0),
		})
	}
	facetRows, err := repository.queries.ListManagedCategoryFacetOptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("query managed category facets: %w", err)
	}
	categoryIndexes := make(map[string]int, len(result))
	for index := range result {
		categoryIndexes[result[index].ID] = index
	}
	for _, row := range facetRows {
		categoryIndex, exists := categoryIndexes[row.CategoryID]
		mode := FacetSelectionMode(row.SelectionMode)
		if !exists || (mode != FacetSelectionSingle && mode != FacetSelectionMulti) ||
			row.FacetName == "" || row.OptionKey == "" || row.OptionLabel == "" || row.CanonicalLabel == "" ||
			row.FacetDisplayOrder < 0 || row.OptionDisplayOrder < 0 || row.Version < 1 || row.TorrentCount < 0 ||
			!row.CreatedAt.Valid || !row.UpdatedAt.Valid {
			return nil, fmt.Errorf("%w: category facet option %q/%q/%q is invalid", errCatalogProjectionInvalid, row.CategoryID, row.FacetID, row.OptionKey)
		}
		category := &result[categoryIndex]
		if len(category.Facets) == 0 || category.Facets[len(category.Facets)-1].ID != row.FacetID {
			category.Facets = append(category.Facets, ManagedCategoryFacet{
				ID: row.FacetID, Name: row.FacetName, SelectionMode: mode,
				Required: row.Required, RequirementGroup: row.RequirementGroup,
				DisplayOrder: int(row.FacetDisplayOrder), Options: make([]ManagedCategoryFacetOption, 0),
			})
		}
		facet := &category.Facets[len(category.Facets)-1]
		if facet.Name != row.FacetName || facet.SelectionMode != mode || facet.Required != row.Required ||
			facet.RequirementGroup != row.RequirementGroup || facet.DisplayOrder != int(row.FacetDisplayOrder) {
			return nil, fmt.Errorf("%w: category facet %q/%q rows disagree", errCatalogProjectionInvalid, row.CategoryID, row.FacetID)
		}
		facet.Options = append(facet.Options, ManagedCategoryFacetOption{
			Key: row.OptionKey, Label: row.OptionLabel, CanonicalLabel: row.CanonicalLabel,
			DisplayOrder: int(row.OptionDisplayOrder), Enabled: row.Enabled, Version: row.Version,
			TorrentCount: row.TorrentCount, CreatedAt: row.CreatedAt.Time.UTC(), UpdatedAt: row.UpdatedAt.Time.UTC(),
		})
	}
	return result, nil
}

func (repository *PostgresCategoryAdministrationRepository) UpsertCategoryFacetOption(ctx context.Context, command UpsertCategoryFacetOptionCommand) (ManagedCategoryFacetOption, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ManagedCategoryFacetOption{}, fmt.Errorf("begin category facet option change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := catalogdb.New(tx)

	facet, err := queries.GetCategoryFacetForOptionAdministration(ctx, catalogdb.GetCategoryFacetForOptionAdministrationParams{
		CategoryID: command.CategoryID, FacetID: command.FacetID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedCategoryFacetOption{}, ErrCategoryFacetNotFound
	}
	if err != nil {
		return ManagedCategoryFacetOption{}, fmt.Errorf("lock category facet: %w", err)
	}

	existing, existingErr := queries.GetManagedCategoryFacetOptionForUpdate(ctx, catalogdb.GetManagedCategoryFacetOptionForUpdateParams{
		CategoryID: command.CategoryID, FacetID: command.FacetID, OptionKey: command.OptionKey,
	})
	if existingErr != nil && !errors.Is(existingErr, pgx.ErrNoRows) {
		return ManagedCategoryFacetOption{}, fmt.Errorf("lock category facet option: %w", existingErr)
	}
	if command.ExpectedVersion == 0 && existingErr == nil {
		return ManagedCategoryFacetOption{}, ErrCategoryOptionAlreadyExists
	}
	if command.ExpectedVersion > 0 && errors.Is(existingErr, pgx.ErrNoRows) {
		return ManagedCategoryFacetOption{}, ErrCategoryOptionNotFound
	}
	if command.ExpectedVersion > 0 && existing.Version != command.ExpectedVersion {
		return ManagedCategoryFacetOption{}, ErrCategoryOptionVersionConflict
	}

	canonical, canonicalErr := queries.GetCanonicalFacetOption(ctx, catalogdb.GetCanonicalFacetOptionParams{
		FacetID: command.FacetID, OptionKey: command.OptionKey,
	})
	if errors.Is(canonicalErr, pgx.ErrNoRows) && command.ExpectedVersion == 0 {
		rows, insertErr := queries.InsertCanonicalFacetOption(ctx, catalogdb.InsertCanonicalFacetOptionParams{
			FacetID: command.FacetID, OptionKey: command.OptionKey, SelectionMode: facet.SelectionMode,
			OptionLabel: command.Label, DisplayOrder: int32(command.DisplayOrder), OccurredAt: categoryTimestamp(command.OccurredAt),
		})
		if insertErr != nil {
			return ManagedCategoryFacetOption{}, fmt.Errorf("insert canonical facet option: %w", insertErr)
		}
		if rows == 1 {
			canonical = catalogdb.GetCanonicalFacetOptionRow{SelectionMode: facet.SelectionMode, Label: command.Label, Enabled: true}
		} else {
			canonical, canonicalErr = queries.GetCanonicalFacetOption(ctx, catalogdb.GetCanonicalFacetOptionParams{
				FacetID: command.FacetID, OptionKey: command.OptionKey,
			})
		}
	}
	if canonicalErr != nil && !(errors.Is(canonicalErr, pgx.ErrNoRows) && command.ExpectedVersion == 0) {
		return ManagedCategoryFacetOption{}, fmt.Errorf("read canonical facet option: %w", canonicalErr)
	}
	if canonical.SelectionMode != facet.SelectionMode || !canonical.Enabled {
		return ManagedCategoryFacetOption{}, ErrCategoryOptionUnavailable
	}

	labelOverride := pgtype.Text{}
	if command.Label != canonical.Label {
		labelOverride = pgtype.Text{String: command.Label, Valid: true}
	}
	var result ManagedCategoryFacetOption
	var before *CategoryFacetOptionAuditState
	transition := "created"
	if command.ExpectedVersion == 0 {
		row, insertErr := queries.InsertManagedCategoryFacetOption(ctx, catalogdb.InsertManagedCategoryFacetOptionParams{
			CategoryID: command.CategoryID, FacetID: command.FacetID, OptionKey: command.OptionKey,
			SelectionMode: facet.SelectionMode, LabelOverride: labelOverride,
			DisplayOrder: int32(command.DisplayOrder), Enabled: command.Enabled, OccurredAt: categoryTimestamp(command.OccurredAt),
		})
		if isUniqueViolation(insertErr) {
			return ManagedCategoryFacetOption{}, ErrCategoryOptionAlreadyExists
		}
		if insertErr != nil {
			return ManagedCategoryFacetOption{}, fmt.Errorf("insert managed category facet option: %w", insertErr)
		}
		result, err = managedCategoryFacetOptionFromMutation(row.OptionKey, command.Label, canonical.Label, row.DisplayOrder, row.Enabled, row.Version, 0, row.CreatedAt, row.UpdatedAt)
	} else {
		beforeState := categoryFacetOptionAuditState(command.CategoryID, command.FacetID, existing.OptionKey, existing.OptionLabel, int(existing.DisplayOrder), existing.Enabled, existing.Version)
		before = &beforeState
		transition = "updated"
		row, updateErr := queries.UpdateManagedCategoryFacetOption(ctx, catalogdb.UpdateManagedCategoryFacetOptionParams{
			LabelOverride: labelOverride, DisplayOrder: int32(command.DisplayOrder), Enabled: command.Enabled,
			OccurredAt: categoryTimestamp(command.OccurredAt), CategoryID: command.CategoryID, FacetID: command.FacetID,
			OptionKey: command.OptionKey, ExpectedVersion: command.ExpectedVersion,
		})
		if errors.Is(updateErr, pgx.ErrNoRows) {
			return ManagedCategoryFacetOption{}, ErrCategoryOptionVersionConflict
		}
		if updateErr != nil {
			return ManagedCategoryFacetOption{}, fmt.Errorf("update managed category facet option: %w", updateErr)
		}
		result, err = managedCategoryFacetOptionFromMutation(row.OptionKey, command.Label, canonical.Label, row.DisplayOrder, row.Enabled, row.Version, existing.TorrentCount, row.CreatedAt, row.UpdatedAt)
	}
	if err != nil {
		return ManagedCategoryFacetOption{}, err
	}
	after := categoryFacetOptionAuditState(command.CategoryID, command.FacetID, result.Key, result.Label, result.DisplayOrder, result.Enabled, result.Version)
	beforeJSON, afterJSON, err := categoryFacetOptionAuditJSON(before, after)
	if err != nil {
		return ManagedCategoryFacetOption{}, err
	}
	if err := queries.InsertCategoryFacetOptionChange(ctx, catalogdb.InsertCategoryFacetOptionChangeParams{
		ChangeID: pgtype.UUID{Bytes: command.ChangeID, Valid: true}, CategoryID: command.CategoryID,
		FacetID: command.FacetID, OptionKey: command.OptionKey, Transition: transition,
		ActorID: pgtype.UUID{Bytes: command.ActorID, Valid: true}, Reason: command.Reason,
		ExpectedVersion: command.ExpectedVersion, ResultingVersion: result.Version,
		BeforeState: beforeJSON, AfterState: afterJSON,
		AuthorizationDecisionID: pgtype.UUID{Bytes: command.Authorization.ID, Valid: true},
		OccurredAt:              categoryTimestamp(command.OccurredAt),
	}); err != nil {
		return ManagedCategoryFacetOption{}, fmt.Errorf("insert category facet option audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedCategoryFacetOption{}, fmt.Errorf("commit category facet option change: %w", err)
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

func managedCategoryFacetOptionFromMutation(
	optionKey, label, canonicalLabel string,
	displayOrder int32,
	enabled bool,
	version, torrentCount int64,
	createdAt, updatedAt pgtype.Timestamptz,
) (ManagedCategoryFacetOption, error) {
	if optionKey == "" || label == "" || canonicalLabel == "" || displayOrder < 0 || version < 1 || torrentCount < 0 ||
		!createdAt.Valid || !updatedAt.Valid {
		return ManagedCategoryFacetOption{}, errCatalogProjectionInvalid
	}
	return ManagedCategoryFacetOption{
		Key: optionKey, Label: label, CanonicalLabel: canonicalLabel,
		DisplayOrder: int(displayOrder), Enabled: enabled, Version: version, TorrentCount: torrentCount,
		CreatedAt: createdAt.Time.UTC(), UpdatedAt: updatedAt.Time.UTC(),
	}, nil
}

func categoryFacetOptionAuditState(categoryID, facetID, optionKey, label string, displayOrder int, enabled bool, version int64) CategoryFacetOptionAuditState {
	return CategoryFacetOptionAuditState{
		CategoryID: categoryID, FacetID: facetID, OptionKey: optionKey,
		Label: label, DisplayOrder: displayOrder, Enabled: enabled, Version: version,
	}
}

func categoryFacetOptionAuditJSON(before *CategoryFacetOptionAuditState, after CategoryFacetOptionAuditState) ([]byte, []byte, error) {
	var beforeJSON []byte
	var err error
	if before != nil {
		beforeJSON, err = json.Marshal(before)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal category facet option before state: %w", err)
		}
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal category facet option after state: %w", err)
	}
	return beforeJSON, afterJSON, nil
}

func isUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}

func categoryTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

var _ CategoryAdministrationRepository = (*PostgresCategoryAdministrationRepository)(nil)
