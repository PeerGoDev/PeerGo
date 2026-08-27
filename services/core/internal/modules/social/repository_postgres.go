package social

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/contracts/go/authzcontractv1"
	"github.com/peergo/peergo/services/core/internal/generated/socialdb"
)

type PostgresCommentRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresCommentRepository(pool *pgxpool.Pool) (*PostgresCommentRepository, error) {
	if pool == nil {
		return nil, errors.New("comment database is required")
	}
	return &PostgresCommentRepository{pool: pool}, nil
}

// List keeps count and rows in one repeatable-read snapshot. The target check
// belongs to the count query so an unpublished UUID remains indistinguishable
// from an unknown one even when no discussion thread has been created yet.
func (repository *PostgresCommentRepository) List(ctx context.Context, target CommentTarget, limit, offset int) ([]Comment, int64, error) {
	targetKind, targetKey, targetErr := commentTargetParts(target)
	if targetErr != nil || limit < 1 || limit > MaxCommentLimit || offset < 0 || offset > MaxCommentOffset {
		return nil, 0, ErrCommentInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, fmt.Errorf("begin comment list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := socialdb.New(tx)
	total, err := queries.CountComments(ctx, socialdb.CountCommentsParams{
		TargetKind: targetKind, TargetKey: targetKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, ErrCommentTargetNotFound
	}
	if err != nil {
		return nil, 0, fmt.Errorf("count comments: %w", err)
	}
	if total < 0 || total > math.MaxInt {
		return nil, 0, ErrCommentInvariant
	}
	rows, err := queries.ListComments(ctx, socialdb.ListCommentsParams{
		TargetKind: targetKind, TargetKey: targetKey,
		ResultLimit: int32(limit), ResultOffset: int32(offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list comments: %w", err)
	}
	items := make([]Comment, 0, len(rows))
	for _, row := range rows {
		comment, conversionErr := commentFromFields(
			row.CommentInternalID,
			row.PublicID,
			row.TargetKind,
			row.TargetKey,
			row.ParentPublicID,
			row.AuthorID,
			row.AuthorDisplayName,
			row.Body,
			row.BodyFormat,
			row.State,
			row.Version,
			row.CreatedAt,
			row.UpdatedAt,
			row.EditedAt,
		)
		if conversionErr != nil {
			return nil, 0, conversionErr
		}
		items = append(items, comment)
	}
	if err := repository.enrichCommentAuthors(ctx, tx, items, time.Now().UTC()); err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, fmt.Errorf("commit comment list: %w", err)
	}
	return items, total, nil
}

// ListThreads pages top-level comments and returns each selected conversation
// with all of its replies. Direct parent links remain intact while the stored
// root identity guarantees that a reply can never be split onto another page.
func (repository *PostgresCommentRepository) ListThreads(ctx context.Context, target CommentTarget, sort CommentThreadSort, limit, offset int) ([]Comment, int64, int64, error) {
	targetKind, targetKey, targetErr := commentTargetParts(target)
	if targetErr != nil || !validCommentThreadSort(sort) || limit < 1 || limit > MaxCommentLimit || offset < 0 || offset > MaxCommentOffset {
		return nil, 0, 0, ErrCommentInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, 0, fmt.Errorf("begin comment thread list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := socialdb.New(tx)
	counts, err := queries.CountCommentThreads(ctx, socialdb.CountCommentThreadsParams{
		TargetKind: targetKind, TargetKey: targetKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, 0, ErrCommentTargetNotFound
	}
	if err != nil {
		return nil, 0, 0, fmt.Errorf("count comment threads: %w", err)
	}
	if counts.CommentTotal < 0 || counts.ThreadTotal < 0 || counts.CommentTotal < counts.ThreadTotal {
		return nil, 0, 0, ErrCommentInvariant
	}
	rows, err := queries.ListCommentThreads(ctx, socialdb.ListCommentThreadsParams{
		TargetKind: targetKind, TargetKey: targetKey, SortOrder: string(sort),
		ResultLimit: int32(limit), ResultOffset: int32(offset),
	})
	if err != nil {
		return nil, 0, 0, fmt.Errorf("list comment threads: %w", err)
	}
	items := make([]Comment, 0, len(rows))
	for _, row := range rows {
		comment, conversionErr := commentFromThreadRow(row)
		if conversionErr != nil {
			return nil, 0, 0, conversionErr
		}
		items = append(items, comment)
	}
	if err := repository.enrichCommentAuthors(ctx, tx, items, time.Now().UTC()); err != nil {
		return nil, 0, 0, err
	}
	byID := make(map[uuid.UUID]CommentAuthor, len(items))
	for _, item := range items {
		byID[item.ID] = item.Author
	}
	for index := range items {
		if items[index].ParentCommentID == nil {
			continue
		}
		parentAuthor, ok := byID[*items[index].ParentCommentID]
		if !ok {
			return nil, 0, 0, ErrCommentInvariant
		}
		items[index].ReplyTo = &parentAuthor
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, 0, fmt.Errorf("commit comment thread list: %w", err)
	}
	return items, counts.CommentTotal, counts.ThreadTotal, nil
}

// Create performs lazy thread creation and comment insertion in one
// transaction. The torrent row is locked only when the typed binding is still
// absent, preventing concurrent first comments from creating orphan threads.
func (repository *PostgresCommentRepository) Create(ctx context.Context, command createCommentCommand) (Comment, error) {
	normalizedBody, bodyErr := normalizeCommentBody(command.Body)
	if command.PublicID == uuid.Nil || command.RequestID == uuid.Nil || validateCommentTarget(command.Target) != nil ||
		command.AuthorID == uuid.Nil || command.CreatedAt.IsZero() || invalidOptionalUUID(command.ParentCommentID) ||
		bodyErr != nil || normalizedBody != command.Body || command.CreateBodySHA256 != sha256ForBody(command.Body) {
		return Comment{}, ErrCommentInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Comment{}, fmt.Errorf("begin comment create: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := socialdb.New(tx)

	if existing, findErr := queries.FindCommentByCreateRequest(ctx, socialdb.FindCommentByCreateRequestParams{
		AuthorID: command.AuthorID, CreateRequestID: command.RequestID,
	}); findErr == nil {
		comment, conversionErr := commentFromCreateRequestRow(existing)
		if conversionErr != nil {
			return Comment{}, conversionErr
		}
		if !sameCreateRequest(existing, comment, command) {
			return Comment{}, ErrCommentIdempotencyConflict
		}
		comments := []Comment{comment}
		if err := repository.enrichCommentAuthors(ctx, tx, comments, command.CreatedAt); err != nil {
			return Comment{}, err
		}
		comment = comments[0]
		if err := tx.Commit(ctx); err != nil {
			return Comment{}, fmt.Errorf("commit replayed comment create: %w", err)
		}
		return comment, nil
	} else if !errors.Is(findErr, pgx.ErrNoRows) {
		return Comment{}, fmt.Errorf("find comment create request: %w", findErr)
	}

	threadID, threadState, err := repository.ensureCommentThread(ctx, queries, command.Target, command.CreatedAt)
	if err != nil {
		return Comment{}, err
	}
	if threadState != "open" {
		return Comment{}, ErrCommentThreadLocked
	}

	parentID := pgtype.Int8{}
	if command.ParentCommentID != nil {
		value, parentErr := queries.FindVisibleCommentForReply(ctx, socialdb.FindVisibleCommentForReplyParams{
			ThreadID: threadID, ParentPublicID: *command.ParentCommentID,
		})
		if errors.Is(parentErr, pgx.ErrNoRows) {
			return Comment{}, ErrCommentParentNotFound
		}
		if parentErr != nil {
			return Comment{}, fmt.Errorf("find comment parent: %w", parentErr)
		}
		parentID = pgtype.Int8{Int64: value, Valid: true}
	}

	affected, err := queries.InsertComment(ctx, socialdb.InsertCommentParams{
		PublicID:         command.PublicID,
		ThreadID:         threadID,
		ParentCommentID:  parentID,
		AuthorID:         command.AuthorID,
		CreateRequestID:  command.RequestID,
		CreateBodySha256: command.CreateBodySHA256[:],
		Body:             command.Body,
		CreatedAt:        timestamp(command.CreatedAt),
	})
	if err != nil {
		return Comment{}, fmt.Errorf("insert comment: %w", err)
	}
	if affected == 0 {
		existing, findErr := queries.FindCommentByCreateRequest(ctx, socialdb.FindCommentByCreateRequestParams{
			AuthorID: command.AuthorID, CreateRequestID: command.RequestID,
		})
		if findErr != nil {
			return Comment{}, fmt.Errorf("read concurrent comment create: %w", findErr)
		}
		comment, conversionErr := commentFromCreateRequestRow(existing)
		if conversionErr != nil {
			return Comment{}, conversionErr
		}
		if !sameCreateRequest(existing, comment, command) {
			return Comment{}, ErrCommentIdempotencyConflict
		}
		comments := []Comment{comment}
		if err := repository.enrichCommentAuthors(ctx, tx, comments, command.CreatedAt); err != nil {
			return Comment{}, err
		}
		comment = comments[0]
		if err := tx.Commit(ctx); err != nil {
			return Comment{}, fmt.Errorf("commit concurrent comment replay: %w", err)
		}
		return comment, nil
	}

	row, err := queries.FindCommentByPublicID(ctx, command.PublicID)
	if err != nil {
		return Comment{}, fmt.Errorf("read created comment: %w", err)
	}
	comment, err := commentFromPublicIDRow(row)
	if err != nil {
		return Comment{}, err
	}
	if err := repository.projectCommentNotification(ctx, tx, command, row.CommentInternalID); err != nil {
		return Comment{}, err
	}
	comments := []Comment{comment}
	if err := repository.enrichCommentAuthors(ctx, tx, comments, command.CreatedAt); err != nil {
		return Comment{}, err
	}
	comment = comments[0]
	if err := tx.Commit(ctx); err != nil {
		return Comment{}, fmt.Errorf("commit comment create: %w", err)
	}
	return comment, nil
}

func (repository *PostgresCommentRepository) Update(ctx context.Context, command updateCommentCommand) (Comment, error) {
	normalizedBody, bodyErr := normalizeCommentBody(command.Body)
	if command.CommentID == uuid.Nil || command.AuthorID == uuid.Nil || command.ExpectedVersion < 1 || command.UpdatedAt.IsZero() ||
		bodyErr != nil || normalizedBody != command.Body {
		return Comment{}, ErrCommentInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Comment{}, fmt.Errorf("begin comment update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := socialdb.New(tx)
	currentRow, err := queries.LockCommentForAuthor(ctx, socialdb.LockCommentForAuthorParams{
		PublicID: command.CommentID, AuthorID: command.AuthorID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Comment{}, ErrCommentNotFound
	}
	if err != nil {
		return Comment{}, fmt.Errorf("lock comment for update: %w", err)
	}
	current, err := commentFromLockedRow(currentRow)
	if err != nil {
		return Comment{}, err
	}
	if current.State != CommentVisible {
		return Comment{}, ErrCommentNotFound
	}
	if !currentRow.TargetIsPublic {
		return Comment{}, ErrCommentTargetNotFound
	}
	if current.Body == command.Body && (current.Version == command.ExpectedVersion || current.Version == command.ExpectedVersion+1) {
		comments := []Comment{current}
		if err := repository.enrichCommentAuthors(ctx, tx, comments, command.UpdatedAt); err != nil {
			return Comment{}, err
		}
		current = comments[0]
		if err := tx.Commit(ctx); err != nil {
			return Comment{}, fmt.Errorf("commit repeated comment update: %w", err)
		}
		return current, nil
	}
	if current.Version != command.ExpectedVersion {
		return Comment{}, ErrCommentVersionConflict
	}
	if err := queries.InsertCommentRevision(ctx, socialdb.InsertCommentRevisionParams{
		CommentID:  currentRow.CommentInternalID,
		Version:    current.Version,
		Body:       current.Body,
		BodyFormat: string(current.BodyFormat),
		Reason:     "author_edit",
		EditorID:   command.AuthorID,
		CreatedAt:  timestamp(command.UpdatedAt),
	}); err != nil {
		return Comment{}, fmt.Errorf("append comment edit revision: %w", err)
	}
	affected, err := queries.UpdateCommentBody(ctx, socialdb.UpdateCommentBodyParams{
		Body:            command.Body,
		UpdatedAt:       timestamp(command.UpdatedAt),
		CommentID:       currentRow.CommentInternalID,
		AuthorID:        command.AuthorID,
		ExpectedVersion: command.ExpectedVersion,
	})
	if err != nil {
		return Comment{}, fmt.Errorf("update comment body: %w", err)
	}
	if affected != 1 {
		return Comment{}, ErrCommentVersionConflict
	}
	updatedRow, err := queries.FindCommentByPublicID(ctx, command.CommentID)
	if err != nil {
		return Comment{}, fmt.Errorf("read updated comment: %w", err)
	}
	updated, err := commentFromPublicIDRow(updatedRow)
	if err != nil {
		return Comment{}, err
	}
	comments := []Comment{updated}
	if err := repository.enrichCommentAuthors(ctx, tx, comments, command.UpdatedAt); err != nil {
		return Comment{}, err
	}
	updated = comments[0]
	if err := tx.Commit(ctx); err != nil {
		return Comment{}, fmt.Errorf("commit comment update: %w", err)
	}
	return updated, nil
}

func (repository *PostgresCommentRepository) Delete(ctx context.Context, command deleteCommentCommand) error {
	if command.CommentID == uuid.Nil || command.AuthorID == uuid.Nil || command.ExpectedVersion < 1 || command.DeletedAt.IsZero() {
		return ErrCommentInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin comment delete: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := socialdb.New(tx)
	currentRow, err := queries.LockCommentForAuthor(ctx, socialdb.LockCommentForAuthorParams{
		PublicID: command.CommentID, AuthorID: command.AuthorID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCommentNotFound
	}
	if err != nil {
		return fmt.Errorf("lock comment for delete: %w", err)
	}
	current, err := commentFromLockedRow(currentRow)
	if err != nil {
		return err
	}
	if current.State == CommentAuthorDeleted {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit repeated comment delete: %w", err)
		}
		return nil
	}
	if current.State != CommentVisible {
		return ErrCommentNotFound
	}
	if current.Version != command.ExpectedVersion {
		return ErrCommentVersionConflict
	}
	if err := queries.InsertCommentRevision(ctx, socialdb.InsertCommentRevisionParams{
		CommentID:  currentRow.CommentInternalID,
		Version:    current.Version,
		Body:       current.Body,
		BodyFormat: string(current.BodyFormat),
		Reason:     "author_delete",
		EditorID:   command.AuthorID,
		CreatedAt:  timestamp(command.DeletedAt),
	}); err != nil {
		return fmt.Errorf("append comment delete revision: %w", err)
	}
	affected, err := queries.TombstoneCommentByAuthor(ctx, socialdb.TombstoneCommentByAuthorParams{
		UpdatedAt:       timestamp(command.DeletedAt),
		CommentID:       currentRow.CommentInternalID,
		AuthorID:        command.AuthorID,
		ExpectedVersion: command.ExpectedVersion,
	})
	if err != nil {
		return fmt.Errorf("tombstone comment: %w", err)
	}
	if affected != 1 {
		return ErrCommentVersionConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit comment delete: %w", err)
	}
	return nil
}

func (repository *PostgresCommentRepository) ensureCommentThread(ctx context.Context, queries *socialdb.Queries, target CommentTarget, now time.Time) (int64, string, error) {
	switch target.Kind {
	case CommentTargetTorrent:
		return repository.ensureTorrentThread(ctx, queries, target.TorrentID, now)
	case CommentTargetAnnouncement:
		return repository.ensureAnnouncementThread(ctx, queries, target.AnnouncementID, now)
	case CommentTargetPost:
		return repository.ensurePostThread(ctx, queries, target.PostPublicID, now)
	default:
		return 0, "", ErrCommentInput
	}
}

func (repository *PostgresCommentRepository) ensurePostThread(ctx context.Context, queries *socialdb.Queries, postPublicID uuid.UUID, now time.Time) (int64, string, error) {
	thread, err := queries.FindVisiblePostCommentThread(ctx, postPublicID)
	if err == nil {
		return thread.ThreadID, thread.State, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, "", fmt.Errorf("find social post comment thread: %w", err)
	}
	postID, err := queries.LockVisiblePostForCommentThread(ctx, postPublicID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", ErrCommentTargetNotFound
	}
	if err != nil {
		return 0, "", fmt.Errorf("lock social post for comment thread: %w", err)
	}
	boundThread, err := queries.FindPostCommentThreadByPostID(ctx, postID)
	if err == nil {
		return boundThread.ThreadID, boundThread.State, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, "", fmt.Errorf("recheck social post comment thread: %w", err)
	}
	threadID, err := queries.CreateCommentThread(ctx, socialdb.CreateCommentThreadParams{
		TargetKind: string(CommentTargetPost), CreatedAt: timestamp(now),
	})
	if err != nil {
		return 0, "", fmt.Errorf("create social post comment thread: %w", err)
	}
	if err := queries.BindPostCommentThread(ctx, socialdb.BindPostCommentThreadParams{
		ThreadID: threadID, PostID: postID, CreatedAt: timestamp(now),
	}); err != nil {
		return 0, "", fmt.Errorf("bind social post comment thread: %w", err)
	}
	return threadID, "open", nil
}

func (repository *PostgresCommentRepository) ensureTorrentThread(ctx context.Context, queries *socialdb.Queries, torrentID int64, now time.Time) (int64, string, error) {
	thread, err := queries.FindPublishedTorrentCommentThread(ctx, torrentID)
	if err == nil {
		return thread.ThreadID, thread.State, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, "", fmt.Errorf("find torrent comment thread: %w", err)
	}
	lockedTorrentID, err := queries.LockPublishedTorrentForCommentThread(ctx, torrentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", ErrCommentTargetNotFound
	}
	if err != nil {
		return 0, "", fmt.Errorf("lock torrent for comment thread: %w", err)
	}
	boundThread, err := queries.FindTorrentCommentThreadByTorrentID(ctx, lockedTorrentID)
	if err == nil {
		return boundThread.ThreadID, boundThread.State, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, "", fmt.Errorf("recheck torrent comment thread: %w", err)
	}
	threadID, err := queries.CreateCommentThread(ctx, socialdb.CreateCommentThreadParams{
		TargetKind: string(CommentTargetTorrent), CreatedAt: timestamp(now),
	})
	if err != nil {
		return 0, "", fmt.Errorf("create comment thread: %w", err)
	}
	if err := queries.BindTorrentCommentThread(ctx, socialdb.BindTorrentCommentThreadParams{
		ThreadID: threadID, TorrentID: lockedTorrentID, CreatedAt: timestamp(now),
	}); err != nil {
		return 0, "", fmt.Errorf("bind torrent comment thread: %w", err)
	}
	return threadID, "open", nil
}

func (repository *PostgresCommentRepository) ensureAnnouncementThread(ctx context.Context, queries *socialdb.Queries, announcementID string, now time.Time) (int64, string, error) {
	thread, err := queries.FindPublishedAnnouncementCommentThread(ctx, announcementID)
	if err == nil {
		return thread.ThreadID, thread.State, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, "", fmt.Errorf("find announcement comment thread: %w", err)
	}
	lockedID, err := queries.LockPublishedAnnouncementForCommentThread(ctx, socialdb.LockPublishedAnnouncementForCommentThreadParams{
		AnnouncementID: announcementID, OccurredAt: timestamp(now),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", ErrCommentTargetNotFound
	}
	if err != nil {
		return 0, "", fmt.Errorf("lock announcement for comment thread: %w", err)
	}
	boundThread, err := queries.FindAnnouncementCommentThreadByAnnouncementID(ctx, lockedID)
	if err == nil {
		return boundThread.ThreadID, boundThread.State, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, "", fmt.Errorf("recheck announcement comment thread: %w", err)
	}
	threadID, err := queries.CreateCommentThread(ctx, socialdb.CreateCommentThreadParams{
		TargetKind: string(CommentTargetAnnouncement), CreatedAt: timestamp(now),
	})
	if err != nil {
		return 0, "", fmt.Errorf("create announcement comment thread: %w", err)
	}
	if err := queries.BindAnnouncementCommentThread(ctx, socialdb.BindAnnouncementCommentThreadParams{
		ThreadID: threadID, AnnouncementID: lockedID, CreatedAt: timestamp(now),
	}); err != nil {
		return 0, "", fmt.Errorf("bind announcement comment thread: %w", err)
	}
	return threadID, "open", nil
}

func commentFromCreateRequestRow(row socialdb.FindCommentByCreateRequestRow) (Comment, error) {
	return commentFromFields(
		row.CommentInternalID, row.PublicID, row.TargetKind, row.TargetKey, row.ParentPublicID,
		row.AuthorID, row.AuthorDisplayName, row.Body, row.BodyFormat, row.State,
		row.Version, row.CreatedAt, row.UpdatedAt, row.EditedAt,
	)
}

func commentFromPublicIDRow(row socialdb.FindCommentByPublicIDRow) (Comment, error) {
	return commentFromFields(
		row.CommentInternalID, row.PublicID, row.TargetKind, row.TargetKey, row.ParentPublicID,
		row.AuthorID, row.AuthorDisplayName, row.Body, row.BodyFormat, row.State,
		row.Version, row.CreatedAt, row.UpdatedAt, row.EditedAt,
	)
}

func commentFromLockedRow(row socialdb.LockCommentForAuthorRow) (Comment, error) {
	return commentFromFields(
		row.CommentInternalID, row.PublicID, row.TargetKind, row.TargetKey, row.ParentPublicID,
		row.AuthorID, row.AuthorDisplayName, row.Body, row.BodyFormat, row.State,
		row.Version, row.CreatedAt, row.UpdatedAt, row.EditedAt,
	)
}

func commentFromThreadRow(row socialdb.ListCommentThreadsRow) (Comment, error) {
	comment, err := commentFromFields(
		row.CommentInternalID, row.PublicID, row.TargetKind, row.TargetKey, row.ParentPublicID,
		row.AuthorID, row.AuthorDisplayName, row.Body, row.BodyFormat, row.State,
		row.Version, row.CreatedAt, row.UpdatedAt, row.EditedAt,
	)
	if err != nil {
		return Comment{}, err
	}
	if row.RootPublicID.Valid {
		root := uuid.UUID(row.RootPublicID.Bytes)
		comment.RootCommentID = &root
	}
	return comment, nil
}

func commentFromFields(
	internalID int64,
	publicID uuid.UUID,
	targetKind string,
	targetKey string,
	parentPublicID pgtype.UUID,
	authorID uuid.UUID,
	authorDisplayName string,
	body string,
	bodyFormat string,
	state string,
	version int64,
	createdAt pgtype.Timestamptz,
	updatedAt pgtype.Timestamptz,
	editedAt pgtype.Timestamptz,
) (Comment, error) {
	if internalID < 1 || !createdAt.Valid || !updatedAt.Valid {
		return Comment{}, ErrCommentInvariant
	}
	target, err := commentTargetFromParts(targetKind, targetKey)
	if err != nil {
		return Comment{}, err
	}
	comment := Comment{
		ID:         publicID,
		Target:     target,
		Author:     CommentAuthor{ID: authorID, DisplayName: authorDisplayName},
		Body:       body,
		BodyFormat: CommentBodyFormat(bodyFormat),
		State:      CommentState(state),
		Version:    version,
		CreatedAt:  createdAt.Time.UTC(),
		UpdatedAt:  updatedAt.Time.UTC(),
	}
	if parentPublicID.Valid {
		parent := uuid.UUID(parentPublicID.Bytes)
		comment.ParentCommentID = &parent
	}
	if editedAt.Valid {
		value := editedAt.Time.UTC()
		comment.EditedAt = &value
	}
	if err := validatePersistedComment(comment); err != nil {
		return Comment{}, err
	}
	return comment, nil
}

func (repository *PostgresCommentRepository) enrichCommentAuthors(ctx context.Context, db communityDB, comments []Comment, now time.Time) error {
	profiles := make(map[uuid.UUID]CommentAuthor, len(comments))
	for index := range comments {
		authorID := comments[index].Author.ID
		profile, ok := profiles[authorID]
		if !ok {
			profile = CommentAuthor{ID: authorID, Medals: []AuthorMedal{}}
			err := db.QueryRow(ctx, `
SELECT users.username, users.display_name,
       EXISTS (
           SELECT 1
           FROM identity.sessions AS session
           WHERE session.user_id = users.id
             AND session.audience = 'web'
             AND session.revoked_at IS NULL
             AND session.expires_at > $2
             AND session.last_seen_at >= $2 - interval '15 minutes'
             AND users.status = 'active'
             AND NOT EXISTS (
                 SELECT 1
                 FROM identity.account_restrictions AS restriction
                 WHERE restriction.user_id = users.id
                   AND restriction.kind = 'account_access'
                   AND restriction.revoked_at IS NULL
                   AND restriction.starts_at <= $2
                   AND restriction.expires_at > $2
             )
       ),
       COALESCE(access.vip_enabled AND (access.vip_until IS NULL OR access.vip_until > $2), false),
       EXISTS (
           SELECT 1
           FROM authz.grants AS grant_record
           JOIN governance.mandates AS mandate
             ON mandate.id = grant_record.mandate_id
            AND mandate.subject_id = grant_record.subject_id
           WHERE grant_record.subject_id = users.id
             AND grant_record.role_id = 'site_admin'
             AND grant_record.scope_type = $3
             AND grant_record.scope_id = $4
             AND grant_record.revoked_at IS NULL
             AND grant_record.valid_from <= $2
             AND $2 < grant_record.valid_until
             AND mandate.status = 'active'
             AND mandate.starts_at <= $2
             AND $2 < mandate.ends_at
       )
FROM identity.users AS users
LEFT JOIN identity.user_access_states AS access ON access.user_id = users.id
WHERE users.id = $1`, authorID, now, authzcontractv1.SiteScopeType, authzcontractv1.SiteScopeID).Scan(
				&profile.Username, &profile.DisplayName, &profile.Online, &profile.VIP, &profile.SiteAdministrator,
			)
			if err != nil {
				return fmt.Errorf("read comment author profile: %w", err)
			}

			rows, err := db.Query(ctx, `
SELECT definition.id, definition.name,
       COALESCE(definition.image_small_path, definition.image_large_path)
FROM economy.user_medals AS holding
JOIN economy.medal_definitions AS definition ON definition.id = holding.medal_id
WHERE holding.user_id = $1
  AND definition.display_on_page
  AND (holding.expires_at IS NULL OR holding.expires_at > $2)
  AND (definition.is_workgroup OR holding.state = 'wearing')
ORDER BY holding.priority DESC, definition.priority DESC, definition.id`, authorID, now)
			if err != nil {
				return fmt.Errorf("list comment author medals: %w", err)
			}
			for rows.Next() {
				var medal AuthorMedal
				if err := rows.Scan(&medal.ID, &medal.Name, &medal.ImagePath); err != nil {
					rows.Close()
					return fmt.Errorf("scan comment author medal: %w", err)
				}
				profile.Medals = append(profile.Medals, medal)
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return fmt.Errorf("list comment author medals: %w", err)
			}
			rows.Close()
			profiles[authorID] = profile
		}
		comments[index].Author = profile
	}
	return nil
}

func (repository *PostgresCommentRepository) projectCommentNotification(ctx context.Context, tx pgx.Tx, command createCommentCommand, commentInternalID int64) error {
	if command.Target.Kind != CommentTargetPost {
		return nil
	}
	var postInternalID int64
	var recipientID uuid.UUID
	kind := SocialNotificationPostComment
	if command.ParentCommentID == nil {
		err := tx.QueryRow(ctx, `SELECT post.id,post.author_id FROM social.posts AS post WHERE post.public_id=$1`, command.Target.PostPublicID).Scan(&postInternalID, &recipientID)
		if err != nil {
			return fmt.Errorf("read social post notification recipient: %w", err)
		}
	} else {
		kind = SocialNotificationCommentReply
		err := tx.QueryRow(ctx, `
SELECT post.id,parent.author_id
FROM social.posts AS post
JOIN social.post_comment_threads AS binding ON binding.post_id=post.id
JOIN social.comments AS parent ON parent.thread_id=binding.thread_id AND parent.public_id=$2
WHERE post.public_id=$1`, command.Target.PostPublicID, *command.ParentCommentID).Scan(&postInternalID, &recipientID)
		if err != nil {
			return fmt.Errorf("read social reply notification recipient: %w", err)
		}
	}
	if recipientID == command.AuthorID {
		return nil
	}
	return upsertSocialInteractionNotification(ctx, tx, uuid.New(), recipientID, command.AuthorID, kind, &postInternalID, &commentInternalID, command.CreatedAt)
}

func sameCreateRequest(row socialdb.FindCommentByCreateRequestRow, comment Comment, command createCommentCommand) bool {
	return comment.Target == command.Target &&
		sameOptionalUUID(comment.ParentCommentID, command.ParentCommentID) &&
		bytes.Equal(row.CreateBodySha256, command.CreateBodySHA256[:])
}

func commentTargetParts(target CommentTarget) (string, string, error) {
	if validateCommentTarget(target) != nil {
		return "", "", ErrCommentInput
	}
	switch target.Kind {
	case CommentTargetTorrent:
		return string(target.Kind), strconv.FormatInt(target.TorrentID, 10), nil
	case CommentTargetAnnouncement:
		return string(target.Kind), target.AnnouncementID, nil
	case CommentTargetPost:
		return string(target.Kind), target.PostPublicID.String(), nil
	default:
		return "", "", ErrCommentInput
	}
}

func commentTargetFromParts(kind, key string) (CommentTarget, error) {
	switch CommentTargetKind(kind) {
	case CommentTargetTorrent:
		value, err := strconv.ParseInt(key, 10, 64)
		if err != nil || value < 1 {
			return CommentTarget{}, ErrCommentInvariant
		}
		target := TorrentCommentTarget(value)
		if validateCommentTarget(target) != nil {
			return CommentTarget{}, ErrCommentInvariant
		}
		return target, nil
	case CommentTargetAnnouncement:
		target := AnnouncementCommentTarget(key)
		if validateCommentTarget(target) != nil {
			return CommentTarget{}, ErrCommentInvariant
		}
		return target, nil
	case CommentTargetPost:
		value, err := uuid.Parse(key)
		if err != nil {
			return CommentTarget{}, ErrCommentInvariant
		}
		target := PostCommentTarget(value)
		if validateCommentTarget(target) != nil {
			return CommentTarget{}, ErrCommentInvariant
		}
		return target, nil
	default:
		return CommentTarget{}, ErrCommentInvariant
	}
}

func sameOptionalUUID(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func sha256ForBody(body string) [32]byte {
	return sha256.Sum256([]byte(body))
}

var _ CommentRepository = (*PostgresCommentRepository)(nil)
