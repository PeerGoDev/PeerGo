package legacytorrents

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var torrentGroupNamespace = uuid.MustParse("1da717a3-28f2-52eb-896d-3c9fd7284382")

type preparedTaxonomyValue struct {
	CategoryID    string
	FacetID       string
	OptionKey     string
	Label         string
	SelectionMode string
	DisplayOrder  int
}

type sourceGroup struct {
	LegacyID   int64
	PublicID   uuid.UUID
	ExternalID map[string]string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type PreparationResult struct {
	FacetOptions              int64
	CategoryFacetOptions      int64
	ResourceGroups            int64
	ResourceGroupExternalIDs  int64
	RecoveredGroupExternalIDs int64
	SkippedGroupExternalIDs   int64
}

func prepareImportDependencies(
	ctx context.Context,
	source *pgxpool.Pool,
	core *pgxpool.Pool,
	runID uuid.UUID,
	occurredAt time.Time,
) (PreparationResult, error) {
	taxonomy, err := collectPreparedTaxonomy(ctx, source)
	if err != nil {
		return PreparationResult{}, err
	}
	groups, recovered, skipped, err := collectSourceGroups(ctx, source)
	if err != nil {
		return PreparationResult{}, err
	}
	if err := persistPreparedTaxonomy(ctx, core, taxonomy, occurredAt); err != nil {
		return PreparationResult{}, err
	}
	groupExternalIDs, err := persistSourceGroups(ctx, core, runID, groups, occurredAt)
	if err != nil {
		return PreparationResult{}, err
	}
	return PreparationResult{
		FacetOptions:              int64(uniqueFacetOptionCount(taxonomy)),
		CategoryFacetOptions:      int64(len(taxonomy)),
		ResourceGroups:            int64(len(groups)),
		ResourceGroupExternalIDs:  groupExternalIDs,
		RecoveredGroupExternalIDs: recovered,
		SkippedGroupExternalIDs:   skipped,
	}, nil
}

func collectPreparedTaxonomy(ctx context.Context, source *pgxpool.Pool) ([]preparedTaxonomyValue, error) {
	vocabulary, err := loadLegacyVocabulary(ctx, source)
	if err != nil {
		return nil, err
	}
	rows, err := source.Query(ctx, `
SELECT id::bigint, COALESCE(type, ''), COALESCE(attributes, '')
FROM torrents
ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query PtYes taxonomy source: %w", err)
	}
	defer rows.Close()
	values := make(map[string]preparedTaxonomyValue)
	optionLabels := make(map[string]string)
	for rows.Next() {
		var legacyID int64
		var category, attributes string
		if err := rows.Scan(&legacyID, &category, &attributes); err != nil {
			return nil, fmt.Errorf("scan PtYes taxonomy source: %w", err)
		}
		facets, _, err := mapLegacyAttributes(category, attributes, vocabulary)
		if err != nil {
			return nil, sourceTorrentError(legacyID, taxonomyErrorCode(err))
		}
		for _, facet := range facets {
			optionIdentity := facet.FacetID + "\x00" + facet.OptionKey + "\x00" + facet.SelectionMode
			if existing, seen := optionLabels[optionIdentity]; seen && existing != facet.Label {
				return nil, sourceTorrentError(legacyID, "conflicting_facet_label")
			}
			optionLabels[optionIdentity] = facet.Label
			identity := facet.CategoryID + "\x00" + optionIdentity
			values[identity] = preparedTaxonomyValue{
				CategoryID: facet.CategoryID, FacetID: facet.FacetID,
				OptionKey: facet.OptionKey, Label: facet.Label,
				SelectionMode: facet.SelectionMode,
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read PtYes taxonomy source: %w", err)
	}
	result := make([]preparedTaxonomyValue, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].FacetID != result[right].FacetID {
			return result[left].FacetID < result[right].FacetID
		}
		if result[left].OptionKey != result[right].OptionKey {
			return result[left].OptionKey < result[right].OptionKey
		}
		return result[left].CategoryID < result[right].CategoryID
	})
	orderByOption := make(map[string]int)
	nextOrderByFacet := make(map[string]int)
	for index := range result {
		identity := result[index].FacetID + "\x00" + result[index].OptionKey
		if order, exists := orderByOption[identity]; exists {
			result[index].DisplayOrder = order
			continue
		}
		nextOrderByFacet[result[index].FacetID]++
		order := 1000 + nextOrderByFacet[result[index].FacetID]
		orderByOption[identity] = order
		result[index].DisplayOrder = order
	}
	return result, nil
}

func uniqueFacetOptionCount(values []preparedTaxonomyValue) int {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		seen[value.FacetID+"\x00"+value.OptionKey] = struct{}{}
	}
	return len(seen)
}

func persistPreparedTaxonomy(
	ctx context.Context,
	core *pgxpool.Pool,
	values []preparedTaxonomyValue,
	occurredAt time.Time,
) error {
	tx, err := core.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin legacy taxonomy preparation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
CREATE TEMP TABLE legacy_torrent_taxonomy (
    category_id text NOT NULL,
    facet_id text NOT NULL,
    option_key text NOT NULL,
    label text NOT NULL,
    selection_mode text NOT NULL,
    display_order integer NOT NULL,
    PRIMARY KEY (category_id, facet_id, option_key)
) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("create legacy taxonomy staging table: %w", err)
	}
	if _, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"legacy_torrent_taxonomy"},
		[]string{"category_id", "facet_id", "option_key", "label", "selection_mode", "display_order"},
		pgx.CopyFromSlice(len(values), func(index int) ([]any, error) {
			value := values[index]
			return []any{
				value.CategoryID, value.FacetID, value.OptionKey,
				value.Label, value.SelectionMode, value.DisplayOrder,
			}, nil
		}),
	); err != nil {
		return fmt.Errorf("stage legacy taxonomy: %w", err)
	}
	var conflicts int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM (
    SELECT DISTINCT facet_id, option_key, label, selection_mode
    FROM legacy_torrent_taxonomy
) AS staged
JOIN catalog.facet_options AS existing
  ON existing.facet_id = staged.facet_id
 AND existing.option_key = staged.option_key
WHERE existing.label <> staged.label
   OR existing.selection_mode <> staged.selection_mode`).Scan(&conflicts); err != nil {
		return fmt.Errorf("check legacy facet option conflicts: %w", err)
	}
	if conflicts != 0 {
		return errors.New("legacy taxonomy conflicts with an existing controlled option")
	}
	var missingBindings int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM legacy_torrent_taxonomy AS staged
LEFT JOIN catalog.category_facets AS binding
  ON binding.category_id = staged.category_id
 AND binding.facet_id = staged.facet_id
 AND binding.selection_mode = staged.selection_mode
WHERE binding.category_id IS NULL`).Scan(&missingBindings); err != nil {
		return fmt.Errorf("check legacy category facet bindings: %w", err)
	}
	if missingBindings != 0 {
		return errors.New("legacy taxonomy references a category facet that is not explicitly bound")
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO catalog.facet_options (
    facet_id, option_key, selection_mode, label, display_order,
    enabled, created_at, updated_at
)
SELECT DISTINCT ON (facet_id, option_key)
    facet_id, option_key, selection_mode, label, display_order,
    true, $1, $1
FROM legacy_torrent_taxonomy
ORDER BY facet_id, option_key, category_id
ON CONFLICT (facet_id, option_key) DO NOTHING`, occurredAt); err != nil {
		return fmt.Errorf("insert controlled legacy facet options: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO catalog.category_facet_options (
    category_id, facet_id, option_key, selection_mode, display_order, created_at
)
SELECT category_id, facet_id, option_key, selection_mode, display_order, $1
FROM legacy_torrent_taxonomy
ON CONFLICT (category_id, facet_id, option_key) DO NOTHING`, occurredAt); err != nil {
		return fmt.Errorf("insert controlled legacy category facet options: %w", err)
	}
	var ready int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM legacy_torrent_taxonomy AS staged
JOIN catalog.facet_options AS option
  ON option.facet_id = staged.facet_id
 AND option.option_key = staged.option_key
 AND option.selection_mode = staged.selection_mode
 AND option.label = staged.label
JOIN catalog.category_facet_options AS category_option
  ON category_option.category_id = staged.category_id
 AND category_option.facet_id = staged.facet_id
 AND category_option.option_key = staged.option_key
 AND category_option.selection_mode = staged.selection_mode`).Scan(&ready); err != nil {
		return fmt.Errorf("verify prepared legacy taxonomy: %w", err)
	}
	if ready != int64(len(values)) {
		return errors.New("not every legacy taxonomy value has a controlled target option")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit legacy taxonomy preparation: %w", err)
	}
	return nil
}

func collectSourceGroups(
	ctx context.Context,
	source *pgxpool.Pool,
) ([]sourceGroup, int64, int64, error) {
	rows, err := source.Query(ctx, `
SELECT id::bigint, COALESCE(external_ids, '{}'::jsonb)::text, created_at, updated_at
FROM torrent_groups
ORDER BY id`)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("query PtYes resource groups: %w", err)
	}
	defer rows.Close()
	result := make([]sourceGroup, 0)
	var recovered, skipped int64
	var previousID int64
	for rows.Next() {
		var legacyID int64
		var rawExternalIDs string
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&legacyID, &rawExternalIDs, &createdAt, &updatedAt); err != nil {
			return nil, 0, 0, fmt.Errorf("scan PtYes resource group: %w", err)
		}
		if legacyID <= previousID || createdAt.IsZero() || updatedAt.Before(createdAt) {
			return nil, 0, 0, errors.New("PtYes resource group metadata is invalid")
		}
		previousID = legacyID
		externalIDs, warnings, err := extractGroupExternalIDs(rawExternalIDs)
		if err != nil {
			return nil, 0, 0, sourceTorrentError(legacyID, taxonomyErrorCode(err))
		}
		for _, warning := range warnings {
			switch warning.Code {
			case warningExternalIDRecovered:
				recovered++
			case warningExternalIDSkipped:
				skipped++
			}
		}
		result = append(result, sourceGroup{
			LegacyID:   legacyID,
			PublicID:   uuid.NewSHA1(torrentGroupNamespace, []byte(strconv.FormatInt(legacyID, 10))),
			ExternalID: externalIDs,
			CreatedAt:  createdAt.UTC().Truncate(time.Microsecond),
			UpdatedAt:  updatedAt.UTC().Truncate(time.Microsecond),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, 0, fmt.Errorf("read PtYes resource groups: %w", err)
	}
	return result, recovered, skipped, nil
}

func persistSourceGroups(
	ctx context.Context,
	core *pgxpool.Pool,
	runID uuid.UUID,
	groups []sourceGroup,
	occurredAt time.Time,
) (int64, error) {
	tx, err := core.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin legacy resource group preparation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
CREATE TEMP TABLE legacy_torrent_groups (
    legacy_id bigint PRIMARY KEY,
    public_id uuid NOT NULL UNIQUE,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
) ON COMMIT DROP`); err != nil {
		return 0, fmt.Errorf("create legacy resource group staging table: %w", err)
	}
	if _, err := tx.Exec(ctx, `
CREATE TEMP TABLE legacy_torrent_group_external_ids (
    legacy_id bigint NOT NULL,
    provider text NOT NULL,
    external_id text NOT NULL,
    PRIMARY KEY (legacy_id, provider)
) ON COMMIT DROP`); err != nil {
		return 0, fmt.Errorf("create legacy resource group external ID staging table: %w", err)
	}
	if _, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"legacy_torrent_groups"},
		[]string{"legacy_id", "public_id", "created_at", "updated_at"},
		pgx.CopyFromSlice(len(groups), func(index int) ([]any, error) {
			group := groups[index]
			return []any{group.LegacyID, group.PublicID, group.CreatedAt, group.UpdatedAt}, nil
		}),
	); err != nil {
		return 0, fmt.Errorf("stage legacy resource groups: %w", err)
	}
	externalRows := make([][]any, 0)
	for _, group := range groups {
		providers := make([]string, 0, len(group.ExternalID))
		for provider := range group.ExternalID {
			providers = append(providers, provider)
		}
		sort.Strings(providers)
		for _, provider := range providers {
			externalRows = append(externalRows, []any{group.LegacyID, provider, group.ExternalID[provider]})
		}
	}
	if _, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"legacy_torrent_group_external_ids"},
		[]string{"legacy_id", "provider", "external_id"},
		pgx.CopyFromRows(externalRows),
	); err != nil {
		return 0, fmt.Errorf("stage legacy resource group external IDs: %w", err)
	}
	var conflicts int64
	if err := tx.QueryRow(ctx, `
SELECT (
    SELECT count(*)
    FROM migration.torrent_group_id_map AS mapping
    JOIN legacy_torrent_groups AS staged ON staged.legacy_id = mapping.legacy_group_id
    WHERE mapping.source_system = 'ptyes'
      AND (mapping.resource_group_id <> staged.legacy_id OR mapping.public_id <> staged.public_id)
) + (
    SELECT count(*)
    FROM torrents.resource_groups AS existing
    JOIN legacy_torrent_groups AS staged ON staged.legacy_id = existing.id
    WHERE existing.public_id <> staged.public_id
       OR existing.created_at <> staged.created_at
       OR existing.updated_at <> staged.updated_at
) + (
    SELECT count(*)
    FROM torrents.resource_group_external_identifiers AS existing
    JOIN legacy_torrent_group_external_ids AS staged
      ON staged.legacy_id = existing.resource_group_id
     AND staged.provider = existing.provider
    WHERE existing.external_id <> staged.external_id
)`).Scan(&conflicts); err != nil {
		return 0, fmt.Errorf("check legacy resource group conflicts: %w", err)
	}
	if conflicts != 0 {
		return 0, errors.New("legacy resource group target data conflicts with stable mappings")
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO migration.torrent_group_id_map (
    source_system, legacy_group_id, resource_group_id,
    public_id, first_run_id, created_at
)
SELECT 'ptyes', legacy_id, legacy_id, public_id, $1, $2
FROM legacy_torrent_groups
ON CONFLICT (source_system, legacy_group_id) DO NOTHING`, runID, occurredAt); err != nil {
		return 0, fmt.Errorf("insert legacy resource group mappings: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO torrents.resource_groups (id, public_id, created_at, updated_at)
SELECT legacy_id, public_id, created_at, updated_at
FROM legacy_torrent_groups
ON CONFLICT (id) DO NOTHING`); err != nil {
		return 0, fmt.Errorf("insert legacy resource groups: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO torrents.resource_group_external_identifiers (
    resource_group_id, provider, external_id, origin, created_at
)
SELECT legacy_id, provider, external_id, 'legacy_import', $1
FROM legacy_torrent_group_external_ids
ON CONFLICT (resource_group_id, provider) DO NOTHING`, occurredAt); err != nil {
		return 0, fmt.Errorf("insert legacy resource group external IDs: %w", err)
	}
	var readyGroups, readyMappings, readyExternalIDs int64
	if err := tx.QueryRow(ctx, `
SELECT
    (SELECT count(*)
     FROM legacy_torrent_groups AS staged
     JOIN torrents.resource_groups AS target
       ON target.id = staged.legacy_id
      AND target.public_id = staged.public_id
      AND target.created_at = staged.created_at
      AND target.updated_at = staged.updated_at),
    (SELECT count(*)
     FROM legacy_torrent_groups AS staged
     JOIN migration.torrent_group_id_map AS mapping
       ON mapping.source_system = 'ptyes'
      AND mapping.legacy_group_id = staged.legacy_id
      AND mapping.resource_group_id = staged.legacy_id
      AND mapping.public_id = staged.public_id),
    (SELECT count(*)
     FROM legacy_torrent_group_external_ids AS staged
     JOIN torrents.resource_group_external_identifiers AS target
       ON target.resource_group_id = staged.legacy_id
      AND target.provider = staged.provider
      AND target.external_id = staged.external_id
      AND target.origin = 'legacy_import')`).Scan(&readyGroups, &readyMappings, &readyExternalIDs); err != nil {
		return 0, fmt.Errorf("verify prepared legacy resource groups: %w", err)
	}
	if readyGroups != int64(len(groups)) || readyMappings != int64(len(groups)) || readyExternalIDs != int64(len(externalRows)) {
		return 0, errors.New("not every PtYes resource group has a stable target mapping")
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit legacy resource group preparation: %w", err)
	}
	return readyExternalIDs, nil
}
