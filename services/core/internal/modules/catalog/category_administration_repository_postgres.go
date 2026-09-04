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
	facetRows, err := repository.queries.ListManagedCategoryFacets(ctx)
	if err != nil {
		return nil, fmt.Errorf("query managed category facets: %w", err)
	}
	categoryIndexes := make(map[string]int, len(result))
	for index := range result {
		categoryIndexes[result[index].ID] = index
	}
	facetIndexes := make(map[string]int, len(facetRows))
	for _, row := range facetRows {
		categoryIndex, exists := categoryIndexes[row.CategoryID]
		mode := FacetSelectionMode(row.SelectionMode)
		if !exists || (mode != FacetSelectionSingle && mode != FacetSelectionMulti) ||
			row.FacetName == "" || row.CanonicalName == "" || row.DisplayOrder < 0 || row.Version < 1 || row.TorrentCount < 0 ||
			!row.CreatedAt.Valid || !row.UpdatedAt.Valid {
			return nil, fmt.Errorf("%w: category facet %q/%q is invalid", errCatalogProjectionInvalid, row.CategoryID, row.FacetID)
		}
		category := &result[categoryIndex]
		facetIndexes[row.CategoryID+"\x00"+row.FacetID] = len(category.Facets)
		category.Facets = append(category.Facets, ManagedCategoryFacet{
			ID: row.FacetID, Name: row.FacetName, SelectionMode: mode,
			Required: row.Required, RequirementGroup: row.RequirementGroup,
			DisplayOrder: int(row.DisplayOrder), Enabled: row.Enabled,
			Version: row.Version, TorrentCount: row.TorrentCount,
			CreatedAt: row.CreatedAt.Time.UTC(), UpdatedAt: row.UpdatedAt.Time.UTC(),
			Options: make([]ManagedCategoryFacetOption, 0),
		})
	}
	optionRows, err := repository.queries.ListManagedCategoryFacetOptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("query managed category facet options: %w", err)
	}
	for _, row := range optionRows {
		categoryIndex, categoryExists := categoryIndexes[row.CategoryID]
		facetIndex, facetExists := facetIndexes[row.CategoryID+"\x00"+row.FacetID]
		if !categoryExists || !facetExists || row.OptionKey == "" || row.OptionLabel == "" || row.CanonicalLabel == "" ||
			row.OptionDisplayOrder < 0 || row.Version < 1 || row.TorrentCount < 0 || !row.CreatedAt.Valid || !row.UpdatedAt.Valid {
			return nil, fmt.Errorf("%w: category facet option %q/%q/%q is invalid", errCatalogProjectionInvalid, row.CategoryID, row.FacetID, row.OptionKey)
		}
		facet := &result[categoryIndex].Facets[facetIndex]
		facet.Options = append(facet.Options, ManagedCategoryFacetOption{
			Key: row.OptionKey, Label: row.OptionLabel, CanonicalLabel: row.CanonicalLabel,
			DisplayOrder: int(row.OptionDisplayOrder), Enabled: row.Enabled, Version: row.Version,
			TorrentCount: row.TorrentCount, CreatedAt: row.CreatedAt.Time.UTC(), UpdatedAt: row.UpdatedAt.Time.UTC(),
		})
	}
	return result, nil
}

func (repository *PostgresCategoryAdministrationRepository) UpsertCategoryFacet(ctx context.Context, command UpsertCategoryFacetCommand) (ManagedCategoryFacet, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ManagedCategoryFacet{}, fmt.Errorf("begin category facet change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := catalogdb.New(tx)

	if _, err := queries.GetManagedCategoryForUpdate(ctx, command.CategoryID); errors.Is(err, pgx.ErrNoRows) {
		return ManagedCategoryFacet{}, ErrCategoryNotFound
	} else if err != nil {
		return ManagedCategoryFacet{}, fmt.Errorf("lock category for facet change: %w", err)
	}

	existing, existingErr := queries.GetManagedCategoryFacetForUpdate(ctx, catalogdb.GetManagedCategoryFacetForUpdateParams{
		CategoryID: command.CategoryID, FacetID: command.FacetID,
	})
	if existingErr != nil && !errors.Is(existingErr, pgx.ErrNoRows) {
		return ManagedCategoryFacet{}, fmt.Errorf("lock managed category facet: %w", existingErr)
	}
	if command.ExpectedVersion == 0 && existingErr == nil {
		return ManagedCategoryFacet{}, ErrCategoryFacetAlreadyExists
	}
	if command.ExpectedVersion == 0 {
		count, countErr := queries.CountManagedCategoryFacets(ctx, command.CategoryID)
		if countErr != nil {
			return ManagedCategoryFacet{}, fmt.Errorf("count managed category facets: %w", countErr)
		}
		if count >= maxCategoryFacets {
			return ManagedCategoryFacet{}, ErrCategoryFacetLimitReached
		}
	}
	if command.ExpectedVersion > 0 && errors.Is(existingErr, pgx.ErrNoRows) {
		return ManagedCategoryFacet{}, ErrCategoryFacetNotFound
	}
	if command.ExpectedVersion > 0 && existing.Version != command.ExpectedVersion {
		return ManagedCategoryFacet{}, ErrCategoryFacetVersionConflict
	}

	canonical, canonicalErr := queries.GetFacetDefinitionForCategoryAdministration(ctx, command.FacetID)
	if errors.Is(canonicalErr, pgx.ErrNoRows) && command.ExpectedVersion == 0 {
		_, insertErr := queries.InsertFacetDefinitionForCategoryAdministration(ctx, catalogdb.InsertFacetDefinitionForCategoryAdministrationParams{
			FacetID: command.FacetID, FacetName: command.Name, SelectionMode: string(command.SelectionMode),
			DisplayOrder: int32(command.DisplayOrder), OccurredAt: categoryTimestamp(command.OccurredAt),
		})
		if insertErr != nil && !errors.Is(insertErr, pgx.ErrNoRows) {
			return ManagedCategoryFacet{}, fmt.Errorf("insert canonical category facet: %w", insertErr)
		}
		canonical, canonicalErr = queries.GetFacetDefinitionForCategoryAdministration(ctx, command.FacetID)
	}
	if canonicalErr != nil {
		return ManagedCategoryFacet{}, fmt.Errorf("resolve canonical category facet: %w", canonicalErr)
	}
	if !canonical.Enabled || canonical.SelectionMode != string(command.SelectionMode) {
		return ManagedCategoryFacet{}, ErrCategoryFacetUnavailable
	}
	if command.ExpectedVersion > 0 && existing.SelectionMode != string(command.SelectionMode) {
		return ManagedCategoryFacet{}, ErrCategoryFacetUnavailable
	}

	nameOverride := nullableCategoryText(command.Name, canonical.Name)
	requirementGroup := nullableCategoryText(command.RequirementGroup, "")
	transition := "created"
	var before *CategoryFacetAuditState
	var result ManagedCategoryFacet
	if command.ExpectedVersion == 0 {
		row, insertErr := queries.InsertManagedCategoryFacet(ctx, catalogdb.InsertManagedCategoryFacetParams{
			CategoryID: command.CategoryID, FacetID: command.FacetID,
			SelectionMode: string(command.SelectionMode), Required: command.Required,
			RequirementGroup: requirementGroup, DisplayOrder: int32(command.DisplayOrder),
			NameOverride: nameOverride, Enabled: command.Enabled, OccurredAt: categoryTimestamp(command.OccurredAt),
		})
		if isUniqueViolation(insertErr) {
			return ManagedCategoryFacet{}, ErrCategoryFacetAlreadyExists
		}
		if insertErr != nil {
			return ManagedCategoryFacet{}, fmt.Errorf("insert managed category facet: %w", insertErr)
		}
		result, err = managedCategoryFacetFromMutation(
			command.FacetID, command.Name, command.SelectionMode, row.Required,
			command.RequirementGroup, row.DisplayOrder, row.Enabled, row.Version, 0,
			row.CreatedAt, row.UpdatedAt,
		)
	} else {
		beforeState := categoryFacetAuditState(
			command.CategoryID, existing.FacetID, existing.FacetName,
			FacetSelectionMode(existing.SelectionMode), existing.Required,
			existing.RequirementGroup, int(existing.DisplayOrder), existing.Enabled, existing.Version,
		)
		before = &beforeState
		transition = "updated"
		row, updateErr := queries.UpdateManagedCategoryFacet(ctx, catalogdb.UpdateManagedCategoryFacetParams{
			Required: command.Required, RequirementGroup: requirementGroup,
			DisplayOrder: int32(command.DisplayOrder), NameOverride: nameOverride,
			Enabled: command.Enabled, OccurredAt: categoryTimestamp(command.OccurredAt),
			CategoryID: command.CategoryID, FacetID: command.FacetID,
			ExpectedVersion: command.ExpectedVersion,
		})
		if errors.Is(updateErr, pgx.ErrNoRows) {
			return ManagedCategoryFacet{}, ErrCategoryFacetVersionConflict
		}
		if updateErr != nil {
			return ManagedCategoryFacet{}, fmt.Errorf("update managed category facet: %w", updateErr)
		}
		result, err = managedCategoryFacetFromMutation(
			command.FacetID, command.Name, command.SelectionMode, row.Required,
			command.RequirementGroup, row.DisplayOrder, row.Enabled, row.Version,
			existing.TorrentCount, row.CreatedAt, row.UpdatedAt,
		)
	}
	if err != nil {
		return ManagedCategoryFacet{}, err
	}
	after := categoryFacetAuditState(
		command.CategoryID, result.ID, result.Name, result.SelectionMode,
		result.Required, result.RequirementGroup, result.DisplayOrder, result.Enabled, result.Version,
	)
	beforeJSON, afterJSON, err := categoryFacetAuditJSON(before, after)
	if err != nil {
		return ManagedCategoryFacet{}, err
	}
	if err := queries.InsertCategoryFacetChange(ctx, catalogdb.InsertCategoryFacetChangeParams{
		ChangeID:   pgtype.UUID{Bytes: command.ChangeID, Valid: true},
		CategoryID: command.CategoryID, FacetID: command.FacetID, Transition: transition,
		ActorID: pgtype.UUID{Bytes: command.ActorID, Valid: true}, Reason: command.Reason,
		ExpectedVersion: command.ExpectedVersion, ResultingVersion: result.Version,
		BeforeState: beforeJSON, AfterState: afterJSON,
		AuthorizationDecisionID: pgtype.UUID{Bytes: command.Authorization.ID, Valid: true},
		OccurredAt:              categoryTimestamp(command.OccurredAt),
	}); err != nil {
		return ManagedCategoryFacet{}, fmt.Errorf("insert category facet audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedCategoryFacet{}, fmt.Errorf("commit category facet change: %w", err)
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
	if command.ExpectedVersion == 0 {
		count, countErr := queries.CountManagedCategoryFacetOptions(ctx, catalogdb.CountManagedCategoryFacetOptionsParams{
			CategoryID: command.CategoryID, FacetID: command.FacetID,
		})
		if countErr != nil {
			return ManagedCategoryFacetOption{}, fmt.Errorf("count managed category facet options: %w", countErr)
		}
		if count >= maxCategoryFacetOptions {
			return ManagedCategoryFacetOption{}, ErrCategoryOptionLimitReached
		}
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

func managedCategoryFacetFromMutation(
	facetID, name string,
	selectionMode FacetSelectionMode,
	required bool,
	requirementGroup string,
	displayOrder int32,
	enabled bool,
	version, torrentCount int64,
	createdAt, updatedAt pgtype.Timestamptz,
) (ManagedCategoryFacet, error) {
	if facetID == "" || name == "" ||
		(selectionMode != FacetSelectionSingle && selectionMode != FacetSelectionMulti) ||
		displayOrder < 0 || version < 1 || torrentCount < 0 || !createdAt.Valid || !updatedAt.Valid {
		return ManagedCategoryFacet{}, errCatalogProjectionInvalid
	}
	return ManagedCategoryFacet{
		ID: facetID, Name: name, SelectionMode: selectionMode,
		Required: required, RequirementGroup: requirementGroup,
		DisplayOrder: int(displayOrder), Enabled: enabled, Version: version,
		TorrentCount: torrentCount, CreatedAt: createdAt.Time.UTC(), UpdatedAt: updatedAt.Time.UTC(),
		Options: make([]ManagedCategoryFacetOption, 0),
	}, nil
}

func categoryFacetAuditState(
	categoryID, facetID, name string,
	selectionMode FacetSelectionMode,
	required bool,
	requirementGroup string,
	displayOrder int,
	enabled bool,
	version int64,
) CategoryFacetAuditState {
	return CategoryFacetAuditState{
		CategoryID: categoryID, FacetID: facetID, Name: name,
		SelectionMode: selectionMode, Required: required,
		RequirementGroup: requirementGroup, DisplayOrder: displayOrder,
		Enabled: enabled, Version: version,
	}
}

func categoryFacetAuditJSON(before *CategoryFacetAuditState, after CategoryFacetAuditState) ([]byte, []byte, error) {
	var beforeJSON []byte
	var err error
	if before != nil {
		beforeJSON, err = json.Marshal(before)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal category facet before state: %w", err)
		}
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal category facet after state: %w", err)
	}
	return beforeJSON, afterJSON, nil
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

func nullableCategoryText(value, canonical string) pgtype.Text {
	if value == "" || value == canonical {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

var _ CategoryAdministrationRepository = (*PostgresCategoryAdministrationRepository)(nil)
