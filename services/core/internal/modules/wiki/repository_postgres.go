package wiki

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, errors.New("wiki postgres pool is required")
	}
	return &PostgresRepository{pool: pool}, nil
}

func (repository *PostgresRepository) List(ctx context.Context, input ListInput, viewerID *uuid.UUID, member bool) (PageList, error) {
	viewer := nullableUUID(viewerID)
	rows, err := repository.pool.Query(ctx, `
SELECT
    page.id,
    page.slug,
    page.title,
    page.summary,
    page.visibility,
    page.sort_order,
    page.version,
    page.revision_number,
    $2::uuid IS NOT NULL AND (
        page.created_by = $2::uuid OR EXISTS (
            SELECT 1 FROM community.wiki_page_editors AS editor
            WHERE editor.page_id = page.id AND editor.user_id = $2::uuid
        )
    ) AS can_edit,
    page.archived_at IS NOT NULL AS archived,
    page.updated_at,
    count(*) OVER () AS total_count
FROM community.wiki_pages AS page
WHERE ($4::boolean OR page.archived_at IS NULL)
  AND ($3::boolean OR page.visibility = 'public')
  AND (
      $1::text = ''
      OR page.slug ILIKE '%' || $1::text || '%'
      OR page.title ILIKE '%' || $1::text || '%'
      OR page.summary ILIKE '%' || $1::text || '%'
      OR page.body ILIKE '%' || $1::text || '%'
  )
ORDER BY page.archived_at IS NOT NULL, page.sort_order DESC, page.updated_at DESC, page.id
LIMIT $5 OFFSET $6`, input.Query, viewer, member, input.IncludeArchived, input.Limit, input.Offset)
	if err != nil {
		return PageList{}, fmt.Errorf("query wiki pages: %w", err)
	}
	defer rows.Close()

	items := make([]PageSummary, 0, input.Limit)
	var total int64
	for rows.Next() {
		var item PageSummary
		var visibility string
		if err := rows.Scan(
			&item.ID, &item.Slug, &item.Title, &item.Summary, &visibility,
			&item.SortOrder, &item.Version, &item.RevisionNumber, &item.CanEdit,
			&item.Archived, &item.UpdatedAt, &total,
		); err != nil {
			return PageList{}, fmt.Errorf("scan wiki page summary: %w", err)
		}
		item.Visibility = Visibility(visibility)
		item.UpdatedAt = item.UpdatedAt.UTC()
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return PageList{}, fmt.Errorf("iterate wiki pages: %w", err)
	}
	if total > math.MaxInt {
		return PageList{}, ErrInvariant
	}
	return PageList{Items: items, Total: int(total), Limit: input.Limit, Offset: input.Offset}, nil
}

func (repository *PostgresRepository) GetBySlug(ctx context.Context, slug string, viewerID *uuid.UUID, member bool) (Page, error) {
	row := repository.pool.QueryRow(ctx, pageProjectionSQL+`
WHERE page.slug = $1
  AND page.archived_at IS NULL
  AND ($3::boolean OR page.visibility = 'public')`, slug, nullableUUID(viewerID), member)
	page, err := scanPage(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Page{}, ErrPageNotFound
	}
	if err != nil {
		return Page{}, fmt.Errorf("query wiki page by slug: %w", err)
	}
	page.Editors, err = repository.listEditors(ctx, page.ID)
	if err != nil {
		return Page{}, err
	}
	return page, nil
}

func (repository *PostgresRepository) GetManaged(ctx context.Context, pageID, viewerID uuid.UUID) (Page, error) {
	row := repository.pool.QueryRow(ctx, pageProjectionSQL+`
WHERE page.id = $1`, pageID, viewerID)
	page, err := scanPage(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Page{}, ErrPageNotFound
	}
	if err != nil {
		return Page{}, fmt.Errorf("query managed wiki page: %w", err)
	}
	page.Editors, err = repository.listEditors(ctx, page.ID)
	if err != nil {
		return Page{}, err
	}
	return page, nil
}

func (repository *PostgresRepository) Create(ctx context.Context, command createCommand) (Page, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Page{}, fmt.Errorf("begin wiki create: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	editorIDs, err := resolveEditorIDs(ctx, tx, command.EditorNumericIDs)
	if err != nil {
		return Page{}, err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO community.wiki_pages (
    id, slug, title, summary, body, visibility, sort_order,
    version, revision_number, created_by, updated_by, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, 1, 1, $8, $8, $9, $9)`,
		command.PageID, command.Slug, command.Title, command.Summary, command.Body,
		command.Visibility, command.SortOrder, command.ActorID, command.CreatedAt)
	if err != nil {
		return Page{}, mapWriteError("insert wiki page", err)
	}
	if err := replaceEditors(ctx, tx, command.PageID, editorIDs, command.ActorID, command.CreatedAt); err != nil {
		return Page{}, err
	}
	if err := insertRevision(ctx, tx, revisionSnapshot{
		PageID: command.PageID, RevisionNumber: 1, Slug: command.Slug, Title: command.Title,
		Summary: command.Summary, Body: command.Body, Visibility: command.Visibility,
		SortOrder: command.SortOrder, Archived: false, EditorIDs: editorIDs,
		Reason: command.Reason, Origin: RevisionOriginStaff, EditorID: command.ActorID,
		CreatedAt: command.CreatedAt,
	}); err != nil {
		return Page{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Page{}, fmt.Errorf("commit wiki create: %w", err)
	}
	return repository.GetManaged(ctx, command.PageID, command.ActorID)
}

func (repository *PostgresRepository) UpdateManaged(ctx context.Context, command updateManagedCommand) (Page, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Page{}, fmt.Errorf("begin managed wiki update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := lockPage(ctx, tx, command.PageID)
	if err != nil {
		return Page{}, err
	}
	if current.Version != command.ExpectedVersion {
		return Page{}, ErrVersionConflict
	}
	editorIDs, err := resolveEditorIDs(ctx, tx, command.EditorNumericIDs)
	if err != nil {
		return Page{}, err
	}
	currentEditorIDs, err := pageEditorIDs(ctx, tx, command.PageID)
	if err != nil {
		return Page{}, err
	}
	archived := current.ArchivedAt.Valid
	if current.Slug == command.Slug && current.Title == command.Title && current.Summary == command.Summary &&
		current.Body == command.Body && current.Visibility == command.Visibility && current.SortOrder == command.SortOrder &&
		archived == command.Archived && slices.Equal(currentEditorIDs, editorIDs) {
		return Page{}, ErrNoChanges
	}

	nextRevision := current.RevisionNumber + 1
	_, err = tx.Exec(ctx, `
UPDATE community.wiki_pages
SET slug = $2,
    title = $3,
    summary = $4,
    body = $5,
    visibility = $6,
    sort_order = $7,
    version = version + 1,
    revision_number = $8,
    updated_by = $9,
    updated_at = $10,
    archived_at = CASE WHEN $11::boolean THEN COALESCE(archived_at, $10) ELSE NULL END
WHERE id = $1`, command.PageID, command.Slug, command.Title, command.Summary, command.Body,
		command.Visibility, command.SortOrder, nextRevision, command.ActorID, command.UpdatedAt, command.Archived)
	if err != nil {
		return Page{}, mapWriteError("update wiki page", err)
	}
	if err := replaceEditors(ctx, tx, command.PageID, editorIDs, command.ActorID, command.UpdatedAt); err != nil {
		return Page{}, err
	}
	if err := insertRevision(ctx, tx, revisionSnapshot{
		PageID: command.PageID, RevisionNumber: nextRevision, Slug: command.Slug, Title: command.Title,
		Summary: command.Summary, Body: command.Body, Visibility: command.Visibility,
		SortOrder: command.SortOrder, Archived: command.Archived, EditorIDs: editorIDs,
		Reason: command.Reason, Origin: RevisionOriginStaff, EditorID: command.ActorID,
		CreatedAt: command.UpdatedAt,
	}); err != nil {
		return Page{}, err
	}
	if err := pruneRevisions(ctx, tx, command.PageID); err != nil {
		return Page{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Page{}, fmt.Errorf("commit managed wiki update: %w", err)
	}
	return repository.GetManaged(ctx, command.PageID, command.ActorID)
}

func (repository *PostgresRepository) UpdateAssigned(ctx context.Context, command updateAssignedCommand) (Page, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Page{}, fmt.Errorf("begin assigned wiki update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := lockPage(ctx, tx, command.PageID)
	if err != nil {
		return Page{}, err
	}
	if current.ArchivedAt.Valid {
		return Page{}, ErrPageNotFound
	}
	if current.Version != command.ExpectedVersion {
		return Page{}, ErrVersionConflict
	}
	editorIDs, err := pageEditorIDs(ctx, tx, command.PageID)
	if err != nil {
		return Page{}, err
	}
	if current.CreatedBy != command.ActorID && !slices.Contains(editorIDs, command.ActorID) {
		return Page{}, ErrEditDenied
	}
	if current.Title == command.Title && current.Summary == command.Summary && current.Body == command.Body {
		return Page{}, ErrNoChanges
	}

	nextRevision := current.RevisionNumber + 1
	_, err = tx.Exec(ctx, `
UPDATE community.wiki_pages
SET title = $2,
    summary = $3,
    body = $4,
    version = version + 1,
    revision_number = $5,
    updated_by = $6,
    updated_at = $7
WHERE id = $1`, command.PageID, command.Title, command.Summary, command.Body,
		nextRevision, command.ActorID, command.UpdatedAt)
	if err != nil {
		return Page{}, fmt.Errorf("update assigned wiki page: %w", err)
	}
	if err := insertRevision(ctx, tx, revisionSnapshot{
		PageID: command.PageID, RevisionNumber: nextRevision, Slug: current.Slug,
		Title: command.Title, Summary: command.Summary, Body: command.Body,
		Visibility: current.Visibility, SortOrder: current.SortOrder, Archived: false,
		EditorIDs: editorIDs, Reason: command.Reason, Origin: RevisionOriginMember,
		EditorID: command.ActorID, CreatedAt: command.UpdatedAt,
	}); err != nil {
		return Page{}, err
	}
	if err := pruneRevisions(ctx, tx, command.PageID); err != nil {
		return Page{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Page{}, fmt.Errorf("commit assigned wiki update: %w", err)
	}
	return repository.GetManaged(ctx, command.PageID, command.ActorID)
}

func (repository *PostgresRepository) ListRevisions(ctx context.Context, pageID uuid.UUID, limit, offset int) (RevisionPage, error) {
	var total int64
	if err := repository.pool.QueryRow(ctx, `
SELECT count(*) FROM community.wiki_revisions WHERE page_id = $1`, pageID).Scan(&total); err != nil {
		return RevisionPage{}, fmt.Errorf("count wiki revisions: %w", err)
	}
	if total == 0 {
		var exists bool
		if err := repository.pool.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM community.wiki_pages WHERE id = $1)`, pageID).Scan(&exists); err != nil {
			return RevisionPage{}, fmt.Errorf("check wiki page for revisions: %w", err)
		}
		if !exists {
			return RevisionPage{}, ErrPageNotFound
		}
	}
	if total > math.MaxInt {
		return RevisionPage{}, ErrInvariant
	}
	rows, err := repository.pool.Query(ctx, `
SELECT revision.revision_number,
       revision.title,
       revision.reason,
       revision.origin,
       editor.id,
       editor.numeric_id,
       editor.username,
       editor.display_name,
       revision.created_at
FROM community.wiki_revisions AS revision
JOIN identity.users AS editor ON editor.id = revision.editor_id
WHERE revision.page_id = $1
ORDER BY revision.revision_number DESC
LIMIT $2 OFFSET $3`, pageID, limit, offset)
	if err != nil {
		return RevisionPage{}, fmt.Errorf("query wiki revisions: %w", err)
	}
	defer rows.Close()
	items := make([]RevisionSummary, 0, limit)
	for rows.Next() {
		var item RevisionSummary
		var origin string
		if err := rows.Scan(
			&item.RevisionNumber, &item.Title, &item.Reason, &origin,
			&item.Editor.ID, &item.Editor.NumericID, &item.Editor.Username,
			&item.Editor.DisplayName, &item.CreatedAt,
		); err != nil {
			return RevisionPage{}, fmt.Errorf("scan wiki revision: %w", err)
		}
		item.Origin = RevisionOrigin(origin)
		item.CreatedAt = item.CreatedAt.UTC()
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return RevisionPage{}, fmt.Errorf("iterate wiki revisions: %w", err)
	}
	return RevisionPage{Items: items, Total: int(total), Limit: limit, Offset: offset}, nil
}

func (repository *PostgresRepository) Restore(ctx context.Context, command restoreCommand) (Page, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Page{}, fmt.Errorf("begin wiki restore: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := lockPage(ctx, tx, command.PageID)
	if err != nil {
		return Page{}, err
	}
	if current.Version != command.ExpectedVersion {
		return Page{}, ErrVersionConflict
	}
	target, err := loadRevision(ctx, tx, command.PageID, command.RevisionNumber)
	if err != nil {
		return Page{}, err
	}
	currentEditors, err := pageEditorIDs(ctx, tx, command.PageID)
	if err != nil {
		return Page{}, err
	}
	sortUUIDs(target.EditorIDs)
	if current.Slug == target.Slug && current.Title == target.Title && current.Summary == target.Summary &&
		current.Body == target.Body && current.Visibility == target.Visibility && current.SortOrder == target.SortOrder &&
		current.ArchivedAt.Valid == target.Archived && slices.Equal(currentEditors, target.EditorIDs) {
		return Page{}, ErrNoChanges
	}

	nextRevision := current.RevisionNumber + 1
	_, err = tx.Exec(ctx, `
UPDATE community.wiki_pages
SET slug = $2,
    title = $3,
    summary = $4,
    body = $5,
    visibility = $6,
    sort_order = $7,
    version = version + 1,
    revision_number = $8,
    updated_by = $9,
    updated_at = $10,
    archived_at = CASE WHEN $11::boolean THEN $10 ELSE NULL END
WHERE id = $1`, command.PageID, target.Slug, target.Title, target.Summary, target.Body,
		target.Visibility, target.SortOrder, nextRevision, command.ActorID, command.RestoredAt, target.Archived)
	if err != nil {
		return Page{}, mapWriteError("restore wiki page", err)
	}
	if err := replaceEditors(ctx, tx, command.PageID, target.EditorIDs, command.ActorID, command.RestoredAt); err != nil {
		return Page{}, err
	}
	if err := insertRevision(ctx, tx, revisionSnapshot{
		PageID: command.PageID, RevisionNumber: nextRevision, Slug: target.Slug,
		Title: target.Title, Summary: target.Summary, Body: target.Body,
		Visibility: target.Visibility, SortOrder: target.SortOrder, Archived: target.Archived,
		EditorIDs: target.EditorIDs, Reason: command.Reason, Origin: RevisionOriginRestore,
		EditorID: command.ActorID, CreatedAt: command.RestoredAt,
	}); err != nil {
		return Page{}, err
	}
	if err := pruneRevisions(ctx, tx, command.PageID); err != nil {
		return Page{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Page{}, fmt.Errorf("commit wiki restore: %w", err)
	}
	return repository.GetManaged(ctx, command.PageID, command.ActorID)
}

const pageProjectionSQL = `
SELECT
    page.id,
    page.slug,
    page.title,
    page.summary,
    page.body,
    page.visibility,
    page.sort_order,
    page.version,
    page.revision_number,
    creator.id,
    creator.numeric_id,
    creator.username,
    creator.display_name,
    updater.id,
    updater.numeric_id,
    updater.username,
    updater.display_name,
    $2::uuid IS NOT NULL AND (
        page.created_by = $2::uuid OR EXISTS (
            SELECT 1 FROM community.wiki_page_editors AS editor
            WHERE editor.page_id = page.id AND editor.user_id = $2::uuid
        )
    ) AS can_edit,
    page.legacy_wiki_id IS NOT NULL AS migrated,
    page.legacy_wiki_id,
    page.created_at,
    page.updated_at,
    page.archived_at
FROM community.wiki_pages AS page
JOIN identity.users AS creator ON creator.id = page.created_by
JOIN identity.users AS updater ON updater.id = page.updated_by
`

type rowScanner interface {
	Scan(...any) error
}

func scanPage(row rowScanner) (Page, error) {
	var page Page
	var visibility string
	var legacyWikiID pgtype.Int8
	var archivedAt pgtype.Timestamptz
	if err := row.Scan(
		&page.ID, &page.Slug, &page.Title, &page.Summary, &page.Body,
		&visibility, &page.SortOrder, &page.Version, &page.RevisionNumber,
		&page.Creator.ID, &page.Creator.NumericID, &page.Creator.Username, &page.Creator.DisplayName,
		&page.Updater.ID, &page.Updater.NumericID, &page.Updater.Username, &page.Updater.DisplayName,
		&page.CanEdit, &page.Migrated, &legacyWikiID, &page.CreatedAt, &page.UpdatedAt, &archivedAt,
	); err != nil {
		return Page{}, err
	}
	page.Visibility = Visibility(visibility)
	if legacyWikiID.Valid {
		value := legacyWikiID.Int64
		page.LegacyWikiID = &value
	}
	if archivedAt.Valid {
		value := archivedAt.Time.UTC()
		page.ArchivedAt = &value
	}
	page.CreatedAt = page.CreatedAt.UTC()
	page.UpdatedAt = page.UpdatedAt.UTC()
	return page, nil
}

func (repository *PostgresRepository) listEditors(ctx context.Context, pageID uuid.UUID) ([]UserReference, error) {
	rows, err := repository.pool.Query(ctx, `
SELECT user_account.id, user_account.numeric_id, user_account.username, user_account.display_name
FROM community.wiki_page_editors AS editor
JOIN identity.users AS user_account ON user_account.id = editor.user_id
WHERE editor.page_id = $1
ORDER BY user_account.numeric_id`, pageID)
	if err != nil {
		return nil, fmt.Errorf("query wiki editors: %w", err)
	}
	defer rows.Close()
	items := make([]UserReference, 0)
	for rows.Next() {
		var item UserReference
		if err := rows.Scan(&item.ID, &item.NumericID, &item.Username, &item.DisplayName); err != nil {
			return nil, fmt.Errorf("scan wiki editor: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate wiki editors: %w", err)
	}
	return items, nil
}

type lockedPage struct {
	Slug           string
	Title          string
	Summary        string
	Body           string
	Visibility     Visibility
	SortOrder      int
	Version        int64
	RevisionNumber int64
	CreatedBy      uuid.UUID
	ArchivedAt     pgtype.Timestamptz
}

func lockPage(ctx context.Context, tx pgx.Tx, pageID uuid.UUID) (lockedPage, error) {
	var page lockedPage
	var visibility string
	err := tx.QueryRow(ctx, `
SELECT slug, title, summary, body, visibility, sort_order, version,
       revision_number, created_by, archived_at
FROM community.wiki_pages
WHERE id = $1
FOR UPDATE`, pageID).Scan(
		&page.Slug, &page.Title, &page.Summary, &page.Body, &visibility,
		&page.SortOrder, &page.Version, &page.RevisionNumber, &page.CreatedBy, &page.ArchivedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedPage{}, ErrPageNotFound
	}
	if err != nil {
		return lockedPage{}, fmt.Errorf("lock wiki page: %w", err)
	}
	page.Visibility = Visibility(visibility)
	return page, nil
}

func resolveEditorIDs(ctx context.Context, tx pgx.Tx, numericIDs []int64) ([]uuid.UUID, error) {
	if len(numericIDs) == 0 {
		return []uuid.UUID{}, nil
	}
	rows, err := tx.Query(ctx, `
SELECT id
FROM identity.users
WHERE numeric_id = ANY($1::bigint[]) AND status = 'active'
ORDER BY numeric_id`, numericIDs)
	if err != nil {
		return nil, fmt.Errorf("resolve wiki editors: %w", err)
	}
	defer rows.Close()
	result := make([]uuid.UUID, 0, len(numericIDs))
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan resolved wiki editor: %w", err)
		}
		result = append(result, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate resolved wiki editors: %w", err)
	}
	if len(result) != len(numericIDs) {
		return nil, ErrEditorNotFound
	}
	sortUUIDs(result)
	return result, nil
}

func sortUUIDs(values []uuid.UUID) {
	slices.SortFunc(values, func(left, right uuid.UUID) int { return slices.Compare(left[:], right[:]) })
}

func pageEditorIDs(ctx context.Context, tx pgx.Tx, pageID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
SELECT user_id FROM community.wiki_page_editors WHERE page_id = $1 ORDER BY user_id`, pageID)
	if err != nil {
		return nil, fmt.Errorf("query wiki editor ids: %w", err)
	}
	defer rows.Close()
	result := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan wiki editor id: %w", err)
		}
		result = append(result, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate wiki editor ids: %w", err)
	}
	return result, nil
}

func replaceEditors(ctx context.Context, tx pgx.Tx, pageID uuid.UUID, editorIDs []uuid.UUID, actorID uuid.UUID, now time.Time) error {
	if _, err := tx.Exec(ctx, `DELETE FROM community.wiki_page_editors WHERE page_id = $1`, pageID); err != nil {
		return fmt.Errorf("clear wiki editors: %w", err)
	}
	for _, editorID := range editorIDs {
		if _, err := tx.Exec(ctx, `
INSERT INTO community.wiki_page_editors (page_id, user_id, assigned_by, assigned_at)
VALUES ($1, $2, $3, $4)`, pageID, editorID, actorID, now); err != nil {
			return fmt.Errorf("assign wiki editor: %w", err)
		}
	}
	return nil
}

type revisionSnapshot struct {
	PageID         uuid.UUID
	RevisionNumber int64
	Slug           string
	Title          string
	Summary        string
	Body           string
	Visibility     Visibility
	SortOrder      int
	Archived       bool
	EditorIDs      []uuid.UUID
	Reason         string
	Origin         RevisionOrigin
	EditorID       uuid.UUID
	CreatedAt      time.Time
}

func insertRevision(ctx context.Context, tx pgx.Tx, revision revisionSnapshot) error {
	_, err := tx.Exec(ctx, `
INSERT INTO community.wiki_revisions (
    page_id, revision_number, slug, title, summary, body, visibility,
    sort_order, archived, editor_user_ids, reason, origin, editor_id, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		revision.PageID, revision.RevisionNumber, revision.Slug, revision.Title,
		revision.Summary, revision.Body, revision.Visibility, revision.SortOrder,
		revision.Archived, revision.EditorIDs, revision.Reason, revision.Origin,
		revision.EditorID, revision.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert wiki revision: %w", err)
	}
	return nil
}

func loadRevision(ctx context.Context, tx pgx.Tx, pageID uuid.UUID, revisionNumber int64) (revisionSnapshot, error) {
	var revision revisionSnapshot
	var visibility, origin string
	err := tx.QueryRow(ctx, `
SELECT slug, title, summary, body, visibility, sort_order, archived,
       editor_user_ids, reason, origin, editor_id, created_at
FROM community.wiki_revisions
WHERE page_id = $1 AND revision_number = $2`, pageID, revisionNumber).Scan(
		&revision.Slug, &revision.Title, &revision.Summary, &revision.Body,
		&visibility, &revision.SortOrder, &revision.Archived, &revision.EditorIDs,
		&revision.Reason, &origin, &revision.EditorID, &revision.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return revisionSnapshot{}, ErrRevisionNotFound
	}
	if err != nil {
		return revisionSnapshot{}, fmt.Errorf("query wiki revision: %w", err)
	}
	revision.PageID = pageID
	revision.RevisionNumber = revisionNumber
	revision.Visibility = Visibility(visibility)
	revision.Origin = RevisionOrigin(origin)
	return revision, nil
}

func pruneRevisions(ctx context.Context, tx pgx.Tx, pageID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
DELETE FROM community.wiki_revisions AS revision
WHERE revision.page_id = $1
  AND revision.revision_number IN (
      SELECT old_revision.revision_number
      FROM community.wiki_revisions AS old_revision
      WHERE old_revision.page_id = $1
      ORDER BY old_revision.revision_number DESC
      OFFSET $2
  )`, pageID, MaximumRevisions)
	if err != nil {
		return fmt.Errorf("prune wiki revisions: %w", err)
	}
	return nil
}

func nullableUUID(value *uuid.UUID) any {
	if value == nil || *value == uuid.Nil {
		return nil
	}
	return *value
}

func mapWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return ErrPageExists
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ Repository = (*PostgresRepository)(nil)
