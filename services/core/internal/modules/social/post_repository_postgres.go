package social

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/generated/socialdb"
	"github.com/peergo/peergo/services/core/internal/modules/economy"
)

type PostgresPostRepository struct {
	pool    *pgxpool.Pool
	economy *economy.PostgresRepository
}

func NewPostgresPostRepository(pool *pgxpool.Pool) (*PostgresPostRepository, error) {
	if pool == nil {
		return nil, errors.New("social post database is required")
	}
	ledger, err := economy.NewPostgresRepository(pool)
	if err != nil {
		return nil, err
	}
	return &PostgresPostRepository{pool: pool, economy: ledger}, nil
}

// List keeps total and rows in one repeatable-read snapshot so offset bounds
// and displayed counts describe the same feed state.
func (repository *PostgresPostRepository) List(ctx context.Context, query PostListQuery) ([]Post, int64, error) {
	if !validPostSort(query.Sort) || query.Limit < 1 || query.Limit > MaxPostLimit || query.Offset < 0 || query.Offset > MaxPostOffset || utf8.RuneCountInString(query.AuthorUsername) > 64 {
		return nil, 0, ErrPostInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, fmt.Errorf("begin social post list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := socialdb.New(tx)
	total, err := queries.CountVisiblePosts(ctx, socialdb.CountVisiblePostsParams{
		AuthorUsername: query.AuthorUsername, BoardID: query.BoardID, FeaturedOnly: query.FeaturedOnly,
		Topic: query.Topic, FeedKind: string(query.Feed), ViewerID: query.ViewerID,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("count social posts: %w", err)
	}
	if total < 0 || total > math.MaxInt {
		return nil, 0, ErrPostInvariant
	}
	rows, err := queries.ListVisiblePosts(ctx, socialdb.ListVisiblePostsParams{
		AuthorUsername: query.AuthorUsername, BoardID: query.BoardID, FeaturedOnly: query.FeaturedOnly,
		Topic: query.Topic, FeedKind: string(query.Feed), ViewerID: query.ViewerID,
		SortOrder: string(query.Sort), ResultLimit: int32(query.Limit), ResultOffset: int32(query.Offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list social posts: %w", err)
	}
	items := make([]Post, 0, len(rows))
	for _, row := range rows {
		post, conversionErr := postFromFields(
			row.PostInternalID, row.PublicID, row.AuthorID, row.AuthorUsername, row.AuthorDisplayName,
			row.Body, row.TorrentID, row.State, row.Version, row.CommentCount, row.CreatedAt, row.UpdatedAt, row.EditedAt,
		)
		if conversionErr != nil {
			return nil, 0, conversionErr
		}
		items = append(items, post)
	}
	items, err = repository.enrichPosts(ctx, tx, query.ViewerID, items, time.Now().UTC())
	if err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, fmt.Errorf("commit social post list: %w", err)
	}
	return items, total, nil
}

func (repository *PostgresPostRepository) FindVisible(ctx context.Context, postID uuid.UUID) (Post, error) {
	if postID == uuid.Nil {
		return Post{}, ErrPostInput
	}
	row, err := socialdb.New(repository.pool).FindVisiblePost(ctx, postID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Post{}, ErrPostNotFound
	}
	if err != nil {
		return Post{}, fmt.Errorf("find visible social post: %w", err)
	}
	return postFromFields(
		row.PostInternalID, row.PublicID, row.AuthorID, row.AuthorUsername, row.AuthorDisplayName,
		row.Body, row.TorrentID, row.State, row.Version, row.CommentCount, row.CreatedAt, row.UpdatedAt, row.EditedAt,
	)
}

func (repository *PostgresPostRepository) ResolveBoardPostingPolicy(ctx context.Context, boardID string) (BoardPostingPolicy, error) {
	if !validBoardID(boardID) {
		return BoardPostingPolicy{}, ErrPostInput
	}
	var policy BoardPostingPolicy
	if err := repository.pool.QueryRow(ctx, `SELECT allow_member_posts FROM social.boards WHERE id = $1 AND enabled`, boardID).Scan(&policy.AllowMemberPosts); errors.Is(err, pgx.ErrNoRows) {
		return BoardPostingPolicy{}, ErrSocialBoardUnavailable
	} else if err != nil {
		return BoardPostingPolicy{}, fmt.Errorf("resolve social board posting policy: %w", err)
	}
	return policy, nil
}

func (repository *PostgresPostRepository) Create(ctx context.Context, command createPostCommand) (Post, error) {
	normalizedBody, bodyErr := normalizePostBody(command.Body, command.TorrentID != nil)
	if command.PublicID == uuid.Nil || command.RequestID == uuid.Nil || command.AuthorID == uuid.Nil || command.CreatedAt.IsZero() || !validBoardID(command.BoardID) ||
		bodyErr != nil || normalizedBody != command.Body || command.TorrentID != nil && *command.TorrentID < 1 || command.CreateBodySHA256 != createPostInputSHA256(command.Body, command.BoardID, command.MediaIDs, command.Poll, command.RedPacket, command.TorrentID) {
		return Post{}, ErrPostInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Post{}, fmt.Errorf("begin social post create: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := socialdb.New(tx)
	if existing, findErr := queries.FindPostByCreateRequest(ctx, socialdb.FindPostByCreateRequestParams{
		AuthorID: command.AuthorID, CreateRequestID: command.RequestID,
	}); findErr == nil {
		post, conversionErr := postFromCreateRequestRow(existing)
		if conversionErr != nil {
			return Post{}, conversionErr
		}
		if !createPostDigestMatches(existing.CreateBodySha256, command) {
			return Post{}, ErrPostIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return Post{}, fmt.Errorf("commit replayed social post create: %w", err)
		}
		return post, nil
	} else if !errors.Is(findErr, pgx.ErrNoRows) {
		return Post{}, fmt.Errorf("find social post create request: %w", findErr)
	}
	if command.TorrentID != nil {
		var published bool
		err := tx.QueryRow(ctx, `SELECT state = 'published' FROM torrents.torrents WHERE id = $1 FOR SHARE`, *command.TorrentID).Scan(&published)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && !published {
			return Post{}, ErrSocialTorrentUnavailable
		}
		if err != nil {
			return Post{}, fmt.Errorf("validate shared social torrent: %w", err)
		}
	}
	inserted, err := queries.InsertPost(ctx, socialdb.InsertPostParams{
		PublicID: command.PublicID, AuthorID: command.AuthorID, CreateRequestID: command.RequestID,
		CreateBodySha256: command.CreateBodySHA256[:], BoardID: command.BoardID, TorrentID: nullableInt64(command.TorrentID), Body: command.Body, CreatedAt: timestamp(command.CreatedAt),
	})
	if err != nil {
		return Post{}, fmt.Errorf("insert social post: %w", err)
	}
	row, err := queries.FindPostByCreateRequest(ctx, socialdb.FindPostByCreateRequestParams{
		AuthorID: command.AuthorID, CreateRequestID: command.RequestID,
	})
	if err != nil {
		return Post{}, fmt.Errorf("read created social post: %w", err)
	}
	post, err := postFromCreateRequestRow(row)
	if err != nil {
		return Post{}, err
	}
	if inserted == 0 && !createPostDigestMatches(row.CreateBodySha256, command) {
		return Post{}, ErrPostIdempotencyConflict
	}
	if inserted == 1 {
		if err := repository.attachPostFeatures(ctx, tx, row.PostInternalID, command); err != nil {
			return Post{}, err
		}
	} else if strings.TrimSpace(command.BoardID) != "" {
		var existingBoard string
		if err := tx.QueryRow(ctx, `SELECT board_id FROM social.posts WHERE id = $1`, row.PostInternalID).Scan(&existingBoard); err != nil {
			return Post{}, err
		}
		if existingBoard != command.BoardID {
			return Post{}, ErrPostIdempotencyConflict
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Post{}, fmt.Errorf("commit social post create: %w", err)
	}
	return post, nil
}

func (repository *PostgresPostRepository) Update(ctx context.Context, command updatePostCommand) (Post, error) {
	normalizedBody, bodyErr := normalizePostBody(command.Body, true)
	if command.PostID == uuid.Nil || command.AuthorID == uuid.Nil || command.ExpectedVersion < 1 || command.UpdatedAt.IsZero() ||
		bodyErr != nil || normalizedBody != command.Body {
		return Post{}, ErrPostInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Post{}, fmt.Errorf("begin social post update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := socialdb.New(tx)
	currentRow, err := queries.LockPostForAuthor(ctx, socialdb.LockPostForAuthorParams{PostPublicID: command.PostID, AuthorID: command.AuthorID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Post{}, ErrPostNotFound
	}
	if err != nil {
		return Post{}, fmt.Errorf("lock social post for update: %w", err)
	}
	current, err := postFromLockedRow(currentRow)
	if err != nil {
		return Post{}, err
	}
	if current.State != PostVisible {
		return Post{}, ErrPostNotFound
	}
	if command.Body == "" && current.Torrent == nil {
		return Post{}, ErrPostInput
	}
	if current.Body == command.Body && (current.Version == command.ExpectedVersion || current.Version == command.ExpectedVersion+1) {
		if err := tx.Commit(ctx); err != nil {
			return Post{}, fmt.Errorf("commit repeated social post update: %w", err)
		}
		return current, nil
	}
	if current.Version != command.ExpectedVersion {
		return Post{}, ErrPostVersionConflict
	}
	if err := queries.InsertPostRevision(ctx, socialdb.InsertPostRevisionParams{
		PostID: currentRow.PostInternalID, Version: current.Version, Body: current.Body,
		Reason: "author_edit", EditorID: command.AuthorID, CreatedAt: timestamp(command.UpdatedAt),
	}); err != nil {
		return Post{}, fmt.Errorf("append social post edit revision: %w", err)
	}
	affected, err := queries.UpdatePostBody(ctx, socialdb.UpdatePostBodyParams{
		Body: command.Body, UpdatedAt: timestamp(command.UpdatedAt), PostID: currentRow.PostInternalID,
		AuthorID: command.AuthorID, ExpectedVersion: command.ExpectedVersion,
	})
	if err != nil {
		return Post{}, fmt.Errorf("update social post body: %w", err)
	}
	if affected != 1 {
		return Post{}, ErrPostVersionConflict
	}
	if _, err := tx.Exec(ctx, `DELETE FROM social.post_topics WHERE post_id = $1`, currentRow.PostInternalID); err != nil {
		return Post{}, fmt.Errorf("replace social post topics: %w", err)
	}
	for _, topic := range extractTopics(command.Body) {
		if _, err := tx.Exec(ctx, `INSERT INTO social.post_topics (post_id, topic, display_topic) VALUES ($1, $2, $3)`, currentRow.PostInternalID, strings.ToLower(topic), topic); err != nil {
			return Post{}, fmt.Errorf("replace social post topics: %w", err)
		}
	}
	updatedRow, err := queries.FindVisiblePost(ctx, command.PostID)
	if err != nil {
		return Post{}, fmt.Errorf("read updated social post: %w", err)
	}
	updated, err := postFromVisibleRow(updatedRow)
	if err != nil {
		return Post{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Post{}, fmt.Errorf("commit social post update: %w", err)
	}
	return updated, nil
}

func (repository *PostgresPostRepository) Delete(ctx context.Context, command deletePostCommand) error {
	if command.PostID == uuid.Nil || command.AuthorID == uuid.Nil || command.ExpectedVersion < 1 || command.DeletedAt.IsZero() {
		return ErrPostInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin social post delete: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := socialdb.New(tx)
	currentRow, err := queries.LockPostForAuthor(ctx, socialdb.LockPostForAuthorParams{PostPublicID: command.PostID, AuthorID: command.AuthorID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPostNotFound
	}
	if err != nil {
		return fmt.Errorf("lock social post for delete: %w", err)
	}
	current, err := postFromLockedRow(currentRow)
	if err != nil {
		return err
	}
	if current.State == PostAuthorDeleted {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit repeated social post delete: %w", err)
		}
		return nil
	}
	if current.State != PostVisible {
		return ErrPostNotFound
	}
	if current.Version != command.ExpectedVersion {
		return ErrPostVersionConflict
	}
	if err := queries.InsertPostRevision(ctx, socialdb.InsertPostRevisionParams{
		PostID: currentRow.PostInternalID, Version: current.Version, Body: current.Body,
		Reason: "author_delete", EditorID: command.AuthorID, CreatedAt: timestamp(command.DeletedAt),
	}); err != nil {
		return fmt.Errorf("append social post delete revision: %w", err)
	}
	affected, err := queries.TombstonePostByAuthor(ctx, socialdb.TombstonePostByAuthorParams{
		UpdatedAt: timestamp(command.DeletedAt), PostID: currentRow.PostInternalID,
		AuthorID: command.AuthorID, ExpectedVersion: command.ExpectedVersion,
	})
	if err != nil {
		return fmt.Errorf("tombstone social post: %w", err)
	}
	if affected != 1 {
		return ErrPostVersionConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit social post delete: %w", err)
	}
	return nil
}

func postFromCreateRequestRow(row socialdb.FindPostByCreateRequestRow) (Post, error) {
	return postFromFields(row.PostInternalID, row.PublicID, row.AuthorID, row.AuthorUsername, row.AuthorDisplayName,
		row.Body, row.TorrentID, row.State, row.Version, row.CommentCount, row.CreatedAt, row.UpdatedAt, row.EditedAt)
}

func postFromLockedRow(row socialdb.LockPostForAuthorRow) (Post, error) {
	return postFromFields(row.PostInternalID, row.PublicID, row.AuthorID, row.AuthorUsername, row.AuthorDisplayName,
		row.Body, row.TorrentID, row.State, row.Version, row.CommentCount, row.CreatedAt, row.UpdatedAt, row.EditedAt)
}

func postFromVisibleRow(row socialdb.FindVisiblePostRow) (Post, error) {
	return postFromFields(row.PostInternalID, row.PublicID, row.AuthorID, row.AuthorUsername, row.AuthorDisplayName,
		row.Body, row.TorrentID, row.State, row.Version, row.CommentCount, row.CreatedAt, row.UpdatedAt, row.EditedAt)
}

func postFromFields(internalID int64, publicID, authorID uuid.UUID, username, displayName, body string, torrentID pgtype.Int8, state string, version, commentCount int64, createdAt, updatedAt pgtype.Timestamptz, editedAt pgtype.Timestamptz) (Post, error) {
	if internalID < 1 || !createdAt.Valid || !updatedAt.Valid {
		return Post{}, ErrPostInvariant
	}
	post := Post{
		ID: publicID, Author: PostAuthor{ID: authorID, Username: username, DisplayName: displayName},
		Body: body, State: PostState(state), Version: version, CommentCount: commentCount,
		CreatedAt: createdAt.Time.UTC(), UpdatedAt: updatedAt.Time.UTC(),
	}
	if torrentID.Valid {
		post.Torrent = &PostTorrent{ID: torrentID.Int64}
	}
	if editedAt.Valid {
		value := editedAt.Time.UTC()
		post.EditedAt = &value
	}
	if err := validatePersistedPost(post); err != nil {
		return Post{}, err
	}
	return post, nil
}

func nullableInt64(value *int64) pgtype.Int8 {
	if value == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *value, Valid: true}
}

var _ PostRepository = (*PostgresPostRepository)(nil)
