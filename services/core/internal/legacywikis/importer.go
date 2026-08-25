// Package legacywikis migrates the small PtYes Wiki corpus into PeerGo's
// bounded editable knowledge base. It is intentionally independent from the
// bulk cutover so it can be rerun safely after Wiki support is deployed.
package legacywikis

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

const (
	legacySourceSystem = "ptyes"
	maximumEditors     = 20
)

var (
	wikiNamespace = uuid.MustParse("60b2c9dc-8d6c-5aae-ac37-5e096871d05d")
	slugPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,95}$`)
)

type Result struct {
	SourcePages   int
	ImportedPages int
	ExistingPages int
	VerifiedPages int
}

type sourcePage struct {
	LegacyID     int64
	Slug         string
	Title        string
	Body         string
	Public       bool
	Weight       int
	EditorIDs    []int64
	CreatedBy    int64
	UpdatedBy    int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
	SourceSHA256 [sha256.Size]byte
}

type mappedPage struct {
	sourcePage
	PageID        uuid.UUID
	CreatorID     uuid.UUID
	UpdaterID     uuid.UUID
	EditorUserIDs []uuid.UUID
	Summary       string
	Visibility    string
}

func Import(ctx context.Context, source, core *pgxpool.Pool) (Result, error) {
	if source == nil || core == nil || source == core {
		return Result{}, errors.New("distinct legacy source and Core pools are required")
	}
	if err := platformpostgres.RequireCurrentMigration(ctx, core); err != nil {
		return Result{}, err
	}
	pages, err := readSource(ctx, source)
	if err != nil {
		return Result{}, err
	}
	result := Result{SourcePages: len(pages)}
	for _, page := range pages {
		mapped, err := mapPage(ctx, core, page)
		if err != nil {
			return Result{}, err
		}
		inserted, err := insertPage(ctx, core, mapped)
		if err != nil {
			return Result{}, err
		}
		if inserted {
			result.ImportedPages++
		} else {
			result.ExistingPages++
		}
	}
	verified, err := verifyPages(ctx, core, pages)
	if err != nil {
		return Result{}, err
	}
	result.VerifiedPages = verified
	return result, nil
}

func Verify(ctx context.Context, source, core *pgxpool.Pool) (Result, error) {
	if source == nil || core == nil || source == core {
		return Result{}, errors.New("distinct legacy source and Core pools are required")
	}
	if err := platformpostgres.RequireCurrentMigration(ctx, core); err != nil {
		return Result{}, err
	}
	pages, err := readSource(ctx, source)
	if err != nil {
		return Result{}, err
	}
	verified, err := verifyPages(ctx, core, pages)
	if err != nil {
		return Result{}, err
	}
	return Result{SourcePages: len(pages), ExistingPages: verified, VerifiedPages: verified}, nil
}

func readSource(ctx context.Context, source *pgxpool.Pool) ([]sourcePage, error) {
	tx, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin legacy Wiki snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
SELECT id,
       slug,
       title,
       body,
       public,
       weight,
       COALESCE(editable_by_users::text, '[]'),
       created_by,
       COALESCE(updated_by, created_by),
       created_at,
       COALESCE(updated_at, created_at)
FROM public.wikis
WHERE deleted_at IS NULL
ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query legacy Wiki pages: %w", err)
	}
	defer rows.Close()
	pages := make([]sourcePage, 0)
	for rows.Next() {
		var page sourcePage
		var rawEditors string
		if err := rows.Scan(
			&page.LegacyID, &page.Slug, &page.Title, &page.Body, &page.Public,
			&page.Weight, &rawEditors, &page.CreatedBy, &page.UpdatedBy,
			&page.CreatedAt, &page.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan legacy Wiki page: %w", err)
		}
		page.EditorIDs, err = parseEditorIDs(rawEditors)
		if err != nil {
			return nil, fmt.Errorf("legacy Wiki %d editors: %w", page.LegacyID, err)
		}
		page.Slug = strings.ToLower(strings.TrimSpace(page.Slug))
		page.Title = strings.TrimSpace(page.Title)
		page.CreatedAt = page.CreatedAt.UTC()
		page.UpdatedAt = page.UpdatedAt.UTC()
		if page.UpdatedAt.Before(page.CreatedAt) {
			page.UpdatedAt = page.CreatedAt
		}
		if err := validateSourcePage(page); err != nil {
			return nil, err
		}
		page.SourceSHA256 = sourceFingerprint(page)
		pages = append(pages, page)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate legacy Wiki pages: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit legacy Wiki snapshot: %w", err)
	}
	return pages, nil
}

func mapPage(ctx context.Context, core *pgxpool.Pool, page sourcePage) (mappedPage, error) {
	legacyIDs := append([]int64{page.CreatedBy, page.UpdatedBy}, page.EditorIDs...)
	slices.Sort(legacyIDs)
	legacyIDs = slices.Compact(legacyIDs)
	rows, err := core.Query(ctx, `
SELECT legacy_user_id, user_id
FROM migration.user_id_map
WHERE source_system = $1 AND legacy_user_id = ANY($2::bigint[])`, legacySourceSystem, legacyIDs)
	if err != nil {
		return mappedPage{}, fmt.Errorf("query legacy Wiki user mappings: %w", err)
	}
	defer rows.Close()
	mappings := make(map[int64]uuid.UUID, len(legacyIDs))
	for rows.Next() {
		var legacyID int64
		var userID uuid.UUID
		if err := rows.Scan(&legacyID, &userID); err != nil {
			return mappedPage{}, fmt.Errorf("scan legacy Wiki user mapping: %w", err)
		}
		mappings[legacyID] = userID
	}
	if err := rows.Err(); err != nil {
		return mappedPage{}, fmt.Errorf("iterate legacy Wiki user mappings: %w", err)
	}
	if len(mappings) != len(legacyIDs) {
		return mappedPage{}, fmt.Errorf("legacy Wiki %d has an unmapped creator, updater, or collaborator", page.LegacyID)
	}
	mapped := mappedPage{
		sourcePage: page,
		PageID:     uuid.NewSHA1(wikiNamespace, []byte(strconv.FormatInt(page.LegacyID, 10))),
		CreatorID:  mappings[page.CreatedBy],
		UpdaterID:  mappings[page.UpdatedBy],
		Summary:    markdownSummary(page.Body, page.Title),
		Visibility: "members",
	}
	if page.Public {
		mapped.Visibility = "public"
	}
	mapped.EditorUserIDs = make([]uuid.UUID, 0, len(page.EditorIDs))
	for _, legacyID := range page.EditorIDs {
		mapped.EditorUserIDs = append(mapped.EditorUserIDs, mappings[legacyID])
	}
	sortUUIDs(mapped.EditorUserIDs)
	return mapped, nil
}

func insertPage(ctx context.Context, core *pgxpool.Pool, page mappedPage) (bool, error) {
	tx, err := core.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin legacy Wiki insert: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var insertedID uuid.UUID
	err = tx.QueryRow(ctx, `
INSERT INTO community.wiki_pages (
    id, slug, title, summary, body, visibility, sort_order,
    version, revision_number, created_by, updated_by,
    legacy_source_system, legacy_wiki_id, legacy_source_sha256,
    created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    1, 1, $8, $9,
    $10, $11, $12,
    $13, $14
)
ON CONFLICT (legacy_source_system, legacy_wiki_id) DO NOTHING
RETURNING id`, page.PageID, page.Slug, page.Title, page.Summary, page.Body,
		page.Visibility, page.Weight, page.CreatorID, page.UpdaterID,
		legacySourceSystem, page.LegacyID, page.SourceSHA256[:], page.CreatedAt, page.UpdatedAt).Scan(&insertedID)
	if errors.Is(err, pgx.ErrNoRows) {
		var storedHash []byte
		if err := tx.QueryRow(ctx, `
SELECT legacy_source_sha256
FROM community.wiki_pages
WHERE legacy_source_system = $1 AND legacy_wiki_id = $2`, legacySourceSystem, page.LegacyID).Scan(&storedHash); err != nil {
			return false, fmt.Errorf("query existing legacy Wiki %d: %w", page.LegacyID, err)
		}
		if !bytes.Equal(storedHash, page.SourceSHA256[:]) {
			return false, fmt.Errorf("legacy Wiki %d source changed after its initial import", page.LegacyID)
		}
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit existing legacy Wiki check: %w", err)
		}
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("insert legacy Wiki %d: %w", page.LegacyID, err)
	}
	for _, editorID := range page.EditorUserIDs {
		if _, err := tx.Exec(ctx, `
INSERT INTO community.wiki_page_editors (page_id, user_id, assigned_by, assigned_at)
VALUES ($1, $2, $3, $4)`, page.PageID, editorID, page.CreatorID, page.CreatedAt); err != nil {
			return false, fmt.Errorf("insert legacy Wiki %d collaborator: %w", page.LegacyID, err)
		}
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO community.wiki_revisions (
    page_id, revision_number, slug, title, summary, body, visibility,
    sort_order, archived, editor_user_ids, reason, origin, editor_id, created_at
) VALUES ($1, 1, $2, $3, $4, $5, $6, $7, false, $8,
          '从 PtYes 旧站迁移的初始版本。', 'migration', $9, $10)`,
		page.PageID, page.Slug, page.Title, page.Summary, page.Body,
		page.Visibility, page.Weight, page.EditorUserIDs, page.UpdaterID, page.UpdatedAt); err != nil {
		return false, fmt.Errorf("insert legacy Wiki %d initial revision: %w", page.LegacyID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit legacy Wiki %d: %w", page.LegacyID, err)
	}
	return true, nil
}

func verifyPages(ctx context.Context, core *pgxpool.Pool, pages []sourcePage) (int, error) {
	verified := 0
	for _, page := range pages {
		var storedHash []byte
		var revisions int
		err := core.QueryRow(ctx, `
SELECT page.legacy_source_sha256,
       (SELECT count(*) FROM community.wiki_revisions AS revision WHERE revision.page_id = page.id)
FROM community.wiki_pages AS page
WHERE page.legacy_source_system = $1 AND page.legacy_wiki_id = $2`, legacySourceSystem, page.LegacyID).Scan(&storedHash, &revisions)
		if errors.Is(err, pgx.ErrNoRows) {
			return verified, fmt.Errorf("legacy Wiki %d is missing from PeerGo", page.LegacyID)
		}
		if err != nil {
			return verified, fmt.Errorf("verify legacy Wiki %d: %w", page.LegacyID, err)
		}
		if !bytes.Equal(storedHash, page.SourceSHA256[:]) || revisions < 1 || revisions > 50 {
			return verified, fmt.Errorf("legacy Wiki %d import evidence is inconsistent", page.LegacyID)
		}
		verified++
	}
	return verified, nil
}

func parseEditorIDs(raw string) ([]int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return []int64{}, nil
	}
	var numeric []int64
	if err := json.Unmarshal([]byte(raw), &numeric); err != nil {
		// encoding/json may leave partially decoded zero values in the target
		// slice before reporting a type mismatch. Discard that partial state
		// before accepting PtYes rows that used quoted numeric IDs.
		numeric = nil
		var textual []string
		if fallbackErr := json.Unmarshal([]byte(raw), &textual); fallbackErr != nil {
			return nil, errors.New("editable_by_users is not a numeric JSON array")
		}
		for _, value := range textual {
			parsed, parseErr := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if parseErr != nil {
				return nil, errors.New("editable_by_users contains a non-numeric value")
			}
			numeric = append(numeric, parsed)
		}
	}
	slices.Sort(numeric)
	numeric = slices.Compact(numeric)
	if len(numeric) > maximumEditors {
		return nil, errors.New("editable_by_users exceeds the collaborator limit")
	}
	for _, value := range numeric {
		if value < 1 {
			return nil, errors.New("editable_by_users contains a non-positive user id")
		}
	}
	return numeric, nil
}

func validateSourcePage(page sourcePage) error {
	if page.LegacyID < 1 || !slugPattern.MatchString(page.Slug) ||
		!utf8.ValidString(page.Title) || utf8.RuneCountInString(page.Title) < 1 || utf8.RuneCountInString(page.Title) > 160 ||
		!utf8.ValidString(page.Body) || strings.TrimSpace(page.Body) == "" || utf8.RuneCountInString(page.Body) > 100_000 ||
		page.Weight < -100_000 || page.Weight > 100_000 || page.CreatedBy < 1 || page.UpdatedBy < 1 ||
		page.CreatedAt.IsZero() || page.UpdatedAt.IsZero() {
		return fmt.Errorf("legacy Wiki %d is outside the PeerGo Wiki contract", page.LegacyID)
	}
	return nil
}

func sourceFingerprint(page sourcePage) [sha256.Size]byte {
	payload := struct {
		LegacyID  int64   `json:"legacy_id"`
		Slug      string  `json:"slug"`
		Title     string  `json:"title"`
		Body      string  `json:"body"`
		Public    bool    `json:"public"`
		Weight    int     `json:"weight"`
		Editors   []int64 `json:"editors"`
		CreatedBy int64   `json:"created_by"`
		UpdatedBy int64   `json:"updated_by"`
		CreatedAt string  `json:"created_at"`
		UpdatedAt string  `json:"updated_at"`
	}{
		LegacyID: page.LegacyID, Slug: page.Slug, Title: page.Title, Body: page.Body,
		Public: page.Public, Weight: page.Weight, Editors: page.EditorIDs,
		CreatedBy: page.CreatedBy, UpdatedBy: page.UpdatedBy,
		CreatedAt: page.CreatedAt.Format(time.RFC3339Nano), UpdatedAt: page.UpdatedAt.Format(time.RFC3339Nano),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		panic("legacy Wiki source fingerprint cannot fail to encode: " + err.Error())
	}
	return sha256.Sum256(encoded)
}

func markdownSummary(body, fallback string) string {
	inFence := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || trimmed == "" || strings.HasPrefix(trimmed, "#") ||
			strings.HasPrefix(trimmed, "![") || strings.HasPrefix(trimmed, "---") {
			continue
		}
		trimmed = strings.TrimLeft(trimmed, ">*-+0123456789. ")
		if trimmed == "" {
			continue
		}
		return truncateRunes(trimmed, 220)
	}
	return truncateRunes(fallback, 220)
}

func truncateRunes(value string, maximum int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maximum {
		return string(runes)
	}
	return string(runes[:maximum-1]) + "…"
}

func sortUUIDs(values []uuid.UUID) {
	slices.SortFunc(values, func(left, right uuid.UUID) int { return slices.Compare(left[:], right[:]) })
}
