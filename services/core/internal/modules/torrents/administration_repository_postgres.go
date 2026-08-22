package torrents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/contracts/trackerevent"
	"github.com/peergo/peergo/services/core/internal/generated/torrentdb"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
)

// PostgresTorrentAdministrationRepository keeps a lifecycle transition, its
// idempotency record, audit evidence and Tracker control event in one database
// transaction. The Tracker is never called while the aggregate row is locked.
type PostgresTorrentAdministrationRepository struct {
	pool               *pgxpool.Pool
	eventBuilder       TorrentLifecycleEventBuilder
	newAuditAppender   func(pgx.Tx) auditevent.Appender
	eligibilityBuilder TorrentLifecycleEligibilityEventBuilder
	newTrackerAppender func(pgx.Tx) trackerevent.Appender
}

func NewPostgresTorrentAdministrationRepository(
	pool *pgxpool.Pool,
	eventBuilder TorrentLifecycleEventBuilder,
	newAuditAppender func(pgx.Tx) auditevent.Appender,
	eligibilityBuilder TorrentLifecycleEligibilityEventBuilder,
	newTrackerAppender func(pgx.Tx) trackerevent.Appender,
) (*PostgresTorrentAdministrationRepository, error) {
	if pool == nil || eventBuilder == nil || newAuditAppender == nil || eligibilityBuilder == nil || newTrackerAppender == nil {
		return nil, errors.New("torrent administration repository dependencies are required")
	}
	return &PostgresTorrentAdministrationRepository{
		pool: pool, eventBuilder: eventBuilder, newAuditAppender: newAuditAppender,
		eligibilityBuilder: eligibilityBuilder, newTrackerAppender: newTrackerAppender,
	}, nil
}

func (repository *PostgresTorrentAdministrationRepository) ManagedPeerTarget(ctx context.Context, torrentID TorrentID) (ManagedTorrentPeerTarget, error) {
	row, err := torrentdb.New(repository.pool).GetManagedTorrentPeerTarget(ctx, int64(torrentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedTorrentPeerTarget{}, ErrManagedTorrentNotFound
	}
	if err != nil {
		return ManagedTorrentPeerTarget{}, fmt.Errorf("get managed torrent peer target: %w", err)
	}
	if row.ID != int64(torrentID) || len(row.InfoHashV1) != 20 || row.TotalSizeBytes < 1 || row.UploaderID == uuid.Nil {
		return ManagedTorrentPeerTarget{}, ErrTorrentReadInvariant
	}
	var infoHash InfoHashV1
	copy(infoHash[:], row.InfoHashV1)
	return ManagedTorrentPeerTarget{
		TorrentID: torrentID, InfoHashV1: infoHash, TotalSizeBytes: row.TotalSizeBytes, UploaderID: row.UploaderID,
	}, nil
}

func (repository *PostgresTorrentAdministrationRepository) ManagedPeerIdentities(ctx context.Context, userIDs []uuid.UUID) ([]ManagedTorrentPeerIdentity, error) {
	if len(userIDs) == 0 {
		return []ManagedTorrentPeerIdentity{}, nil
	}
	rows, err := torrentdb.New(repository.pool).ListManagedTorrentPeerIdentities(ctx, userIDs)
	if err != nil {
		return nil, fmt.Errorf("list managed torrent peer identities: %w", err)
	}
	result := make([]ManagedTorrentPeerIdentity, 0, len(rows))
	seen := make(map[uuid.UUID]struct{}, len(rows))
	for _, row := range rows {
		if row.ID == uuid.Nil || row.NumericID < 1 || strings.TrimSpace(row.Username) == "" || strings.TrimSpace(row.DisplayName) == "" {
			return nil, ErrTorrentReadInvariant
		}
		if _, exists := seen[row.ID]; exists {
			return nil, ErrTorrentReadInvariant
		}
		seen[row.ID] = struct{}{}
		result = append(result, ManagedTorrentPeerIdentity{
			UserID: row.ID, NumericID: row.NumericID, Username: row.Username, DisplayName: row.DisplayName,
		})
	}
	return result, nil
}

func (repository *PostgresTorrentAdministrationRepository) ListManaged(ctx context.Context, query ManagedTorrentQuery) (ManagedTorrentPage, error) {
	query, err := normalizeManagedTorrentQuery(query)
	if err != nil {
		return ManagedTorrentPage{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ManagedTorrentPage{}, fmt.Errorf("begin managed torrent read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := torrentdb.New(tx)
	params := torrentdb.ListManagedTorrentsParams{
		SearchText: query.Query, TorrentState: string(query.State), CategoryID: query.CategoryID,
		ResultLimit: int32(query.Limit), ResultOffset: int32(query.Offset),
	}
	rows, err := queries.ListManagedTorrents(ctx, params)
	if err != nil {
		return ManagedTorrentPage{}, fmt.Errorf("query managed torrents: %w", err)
	}
	total, err := queries.CountManagedTorrents(ctx, torrentdb.CountManagedTorrentsParams{
		SearchText: query.Query, TorrentState: string(query.State), CategoryID: query.CategoryID,
	})
	if err != nil {
		return ManagedTorrentPage{}, fmt.Errorf("count managed torrents: %w", err)
	}
	categoryRows, err := queries.ListManagedTorrentFilterCategories(ctx)
	if err != nil {
		return ManagedTorrentPage{}, fmt.Errorf("list managed torrent categories: %w", err)
	}
	stateRows, err := queries.CountManagedTorrentsByState(ctx)
	if err != nil {
		return ManagedTorrentPage{}, fmt.Errorf("count managed torrent states: %w", err)
	}
	page := ManagedTorrentPage{
		Items: make([]ManagedTorrent, 0, len(rows)), Categories: make([]ManagedTorrentCategory, 0, len(categoryRows)),
		Total: total, Limit: query.Limit, Offset: query.Offset,
	}
	for _, row := range rows {
		item, conversionErr := managedTorrentFromRow(row)
		if conversionErr != nil {
			return ManagedTorrentPage{}, conversionErr
		}
		page.Items = append(page.Items, item)
	}
	for _, row := range categoryRows {
		if row.ID == "" || strings.TrimSpace(row.Name) == "" {
			return ManagedTorrentPage{}, ErrTorrentReadInvariant
		}
		page.Categories = append(page.Categories, ManagedTorrentCategory{ID: row.ID, Name: row.Name, Enabled: row.Enabled})
	}
	for _, row := range stateRows {
		switch State(row.State) {
		case StatePendingReview:
			page.StateCounts.PendingReview = row.TorrentCount
		case StatePublished:
			page.StateCounts.Published = row.TorrentCount
		case StateRejected:
			page.StateCounts.Rejected = row.TorrentCount
		case StateDisabled:
			page.StateCounts.Disabled = row.TorrentCount
		case StateDeleted:
			page.StateCounts.Deleted = row.TorrentCount
		default:
			return ManagedTorrentPage{}, ErrTorrentReadInvariant
		}
	}
	if page.Total < int64(len(page.Items)) || page.StateCounts.Total() < page.Total {
		return ManagedTorrentPage{}, ErrTorrentReadInvariant
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedTorrentPage{}, fmt.Errorf("commit managed torrent read: %w", err)
	}
	return page, nil
}

func managedTorrentFromRow(row torrentdb.ListManagedTorrentsRow) (ManagedTorrent, error) {
	state := State(row.State)
	promotion := catalog.Promotion(row.Promotion)
	if row.ID < 1 || row.UploaderNumericID < 1 || strings.TrimSpace(row.UploaderUsername) == "" ||
		strings.TrimSpace(row.UploaderDisplayName) == "" || row.CategoryID == "" || strings.TrimSpace(row.CategoryName) == "" ||
		strings.TrimSpace(row.Title) == "" || row.TotalSizeBytes < 1 || row.PurchasePrice < 0 || !validReadState(state) || row.Version < 1 ||
		!promotion.Valid() || row.Seeders < 0 || row.Leechers < 0 || row.Completed < 0 ||
		!row.SubmittedAt.Valid || !row.StateChangedAt.Valid || !row.UpdatedAt.Valid {
		return ManagedTorrent{}, ErrTorrentReadInvariant
	}
	var publishedAt *time.Time
	if row.PublishedAt.Valid {
		value := row.PublishedAt.Time.UTC()
		publishedAt = &value
	}
	if (state == StatePublished || state == StateDisabled) && publishedAt == nil {
		return ManagedTorrent{}, ErrTorrentReadInvariant
	}
	var promotionEndsAt *time.Time
	if row.PromotionEndsAt.Valid {
		value := row.PromotionEndsAt.Time.UTC()
		promotionEndsAt = &value
	}
	return ManagedTorrent{
		ID: TorrentID(row.ID), UploaderNumericID: row.UploaderNumericID, UploaderUsername: row.UploaderUsername,
		UploaderDisplayName: row.UploaderDisplayName, CategoryID: row.CategoryID, CategoryName: row.CategoryName,
		Title: row.Title, Subtitle: row.Subtitle, TotalSizeBytes: row.TotalSizeBytes, PurchasePrice: row.PurchasePrice, State: state, Version: row.Version,
		Promotion: promotion, PromotionEndsAt: promotionEndsAt, Seeders: int(row.Seeders), Leechers: int(row.Leechers), Completed: int(row.Completed),
		SubmittedAt: row.SubmittedAt.Time.UTC(), PublishedAt: publishedAt, StateChangedAt: row.StateChangedAt.Time.UTC(), UpdatedAt: row.UpdatedAt.Time.UTC(),
	}, nil
}

func (repository *PostgresTorrentAdministrationRepository) ChangeAvailability(ctx context.Context, command ChangeTorrentAvailabilityCommand) (TorrentAvailabilityResult, error) {
	command, err := normalizeTorrentAvailabilityCommand(command)
	if err != nil {
		return TorrentAvailabilityResult{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TorrentAvailabilityResult{}, fmt.Errorf("begin torrent availability change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := torrentdb.New(tx)
	if result, found, replayErr := resumeTorrentAvailabilityChange(ctx, queries, command); found || replayErr != nil {
		if replayErr != nil {
			return TorrentAvailabilityResult{}, replayErr
		}
		if err := tx.Commit(ctx); err != nil {
			return TorrentAvailabilityResult{}, fmt.Errorf("commit replayed torrent availability change: %w", err)
		}
		return result, nil
	}

	locked, err := queries.GetManagedTorrentForAvailabilityUpdate(ctx, int64(command.TorrentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return TorrentAvailabilityResult{}, ErrManagedTorrentNotFound
	}
	if err != nil {
		return TorrentAvailabilityResult{}, fmt.Errorf("lock managed torrent: %w", err)
	}
	if locked.Version != command.ExpectedVersion {
		if result, found, replayErr := resumeTorrentAvailabilityChange(ctx, queries, command); found || replayErr != nil {
			return result, replayErr
		}
		return TorrentAvailabilityResult{}, ErrManagedTorrentVersionConflict
	}
	expectedState, resultingState := availabilityStates(command.Action)
	if State(locked.State) != expectedState {
		return TorrentAvailabilityResult{}, ErrManagedTorrentStateConflict
	}
	if command.Action == TorrentAvailabilityRestore && !locked.CategoryEnabled {
		return TorrentAvailabilityResult{}, ErrManagedTorrentCategoryUnavailable
	}
	if command.Action == TorrentAvailabilityRestore && !locked.HasVerifiedLocation {
		return TorrentAvailabilityResult{}, ErrManagedTorrentObjectUnavailable
	}
	if len(locked.InfoHashV1) != 20 || locked.TotalSizeBytes < 1 || !locked.SubmittedAt.Valid || !locked.StateChangedAt.Valid || !locked.UpdatedAt.Valid {
		return TorrentAvailabilityResult{}, ErrTorrentReadInvariant
	}
	var infoHash InfoHashV1
	copy(infoHash[:], locked.InfoHashV1)
	var publishedAt *time.Time
	if locked.PublishedAt.Valid {
		value := locked.PublishedAt.Time.UTC()
		publishedAt = &value
	}
	aggregate := Torrent{
		ID: TorrentID(locked.ID), UploaderID: locked.UploaderID, CategoryID: locked.CategoryID,
		InfoHashV1: infoHash, TotalSizeBytes: locked.TotalSizeBytes, State: State(locked.State), Version: locked.Version,
		SubmittedAt: locked.SubmittedAt.Time.UTC(), PublishedAt: publishedAt, StateChangedAt: locked.StateChangedAt.Time.UTC(), UpdatedAt: locked.UpdatedAt.Time.UTC(),
	}
	if command.Action == TorrentAvailabilityDisable {
		err = aggregate.Disable(command.OccurredAt)
	} else {
		err = aggregate.Restore(command.OccurredAt)
	}
	if err != nil {
		return TorrentAvailabilityResult{}, ErrManagedTorrentStateConflict
	}
	updated, err := queries.ChangeManagedTorrentAvailability(ctx, torrentdb.ChangeManagedTorrentAvailabilityParams{
		ResultingState: string(resultingState), OccurredAt: torrentAdministrationTimestamp(command.OccurredAt),
		TorrentID: int64(command.TorrentID), ExpectedState: string(expectedState), ExpectedVersion: command.ExpectedVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return TorrentAvailabilityResult{}, ErrManagedTorrentVersionConflict
	}
	if err != nil {
		return TorrentAvailabilityResult{}, fmt.Errorf("update managed torrent availability: %w", err)
	}
	result := TorrentAvailabilityResult{
		ChangeID: command.ChangeID, TorrentID: command.TorrentID, Action: command.Action,
		State: State(updated.State), Version: updated.Version, ChangedAt: updated.StateChangedAt.Time.UTC(),
	}
	if result.State != aggregate.State || result.Version != aggregate.Version || !updated.StateChangedAt.Valid {
		return TorrentAvailabilityResult{}, ErrManagedTorrentStateConflict
	}
	if err := queries.InsertManagedTorrentLifecycleChange(ctx, torrentdb.InsertManagedTorrentLifecycleChangeParams{
		ChangeID: command.ChangeID, TorrentID: int64(command.TorrentID), ActorID: command.ActorID,
		LifecycleAction: string(command.Action), Reason: command.Reason, ExpectedVersion: command.ExpectedVersion,
		ResultingVersion: result.Version, BeforeState: string(expectedState), AfterState: string(resultingState),
		AuthorizationDecisionID: command.Authorization.ID, OccurredAt: torrentAdministrationTimestamp(command.OccurredAt),
	}); err != nil {
		return TorrentAvailabilityResult{}, mapTorrentAdministrationWriteError("insert torrent lifecycle change", err)
	}
	before := TorrentLifecycleAuditState{TorrentID: command.TorrentID, State: expectedState, Version: command.ExpectedVersion, TrackerEligible: expectedState == StatePublished}
	auditEvent, err := repository.eventBuilder.BuildTorrentLifecycleEvent(TorrentLifecycleAuditInput{
		ChangeID: command.ChangeID, ActorID: command.ActorID, Action: command.Action, Reason: command.Reason,
		OccurredAt: command.OccurredAt, Authorization: command.Authorization, Before: before,
		After: TorrentLifecycleAuditState{TorrentID: command.TorrentID, State: resultingState, Version: result.Version, TrackerEligible: resultingState == StatePublished},
	})
	if err != nil {
		return TorrentAvailabilityResult{}, fmt.Errorf("build torrent lifecycle audit event: %w", err)
	}
	if err := repository.newAuditAppender(tx).Append(ctx, auditEvent); err != nil {
		return TorrentAvailabilityResult{}, fmt.Errorf("append torrent lifecycle audit event: %w", err)
	}
	controlEvent, err := repository.eligibilityBuilder.BuildTorrentLifecycleEligibilityEvent(TorrentLifecycleEligibilityInput{
		Result: result, InfoHashV1: infoHash, TotalSizeBytes: locked.TotalSizeBytes,
	})
	if err != nil {
		return TorrentAvailabilityResult{}, fmt.Errorf("build Tracker lifecycle eligibility event: %w", err)
	}
	if err := repository.newTrackerAppender(tx).Append(ctx, controlEvent); err != nil {
		return TorrentAvailabilityResult{}, fmt.Errorf("append Tracker lifecycle eligibility event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return TorrentAvailabilityResult{}, fmt.Errorf("commit torrent availability change: %w", err)
	}
	return result, nil
}

func availabilityStates(action TorrentAvailabilityAction) (State, State) {
	if action == TorrentAvailabilityDisable {
		return StatePublished, StateDisabled
	}
	return StateDisabled, StatePublished
}

func normalizeTorrentAvailabilityCommand(command ChangeTorrentAvailabilityCommand) (ChangeTorrentAvailabilityCommand, error) {
	normalized, err := normalizeChangeTorrentAvailabilityInput(command.ChangeTorrentAvailabilityInput)
	if err != nil || command.ActorID == uuid.Nil || command.OccurredAt.IsZero() || !command.Authorization.Allow || command.Authorization.ID == uuid.Nil {
		return ChangeTorrentAvailabilityCommand{}, ErrTorrentAdministrationInput
	}
	command.ChangeTorrentAvailabilityInput = normalized
	return command, nil
}

func resumeTorrentAvailabilityChange(ctx context.Context, queries *torrentdb.Queries, command ChangeTorrentAvailabilityCommand) (TorrentAvailabilityResult, bool, error) {
	row, err := queries.GetManagedTorrentLifecycleChange(ctx, command.ChangeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return TorrentAvailabilityResult{}, false, nil
	}
	if err != nil {
		return TorrentAvailabilityResult{}, false, fmt.Errorf("read torrent lifecycle change: %w", err)
	}
	if TorrentID(row.TorrentID) != command.TorrentID || row.ActorID != command.ActorID || row.Action != string(command.Action) ||
		row.Reason != command.Reason || row.ExpectedTorrentVersion != command.ExpectedVersion {
		return TorrentAvailabilityResult{}, true, ErrManagedTorrentIdempotencyConflict
	}
	if !row.OccurredAt.Valid || row.ResultingTorrentVersion != command.ExpectedVersion+1 {
		return TorrentAvailabilityResult{}, true, ErrManagedTorrentStateConflict
	}
	_, expectedAfter := availabilityStates(command.Action)
	if State(row.AfterState) != expectedAfter {
		return TorrentAvailabilityResult{}, true, ErrManagedTorrentStateConflict
	}
	return TorrentAvailabilityResult{
		ChangeID: row.ID, TorrentID: TorrentID(row.TorrentID), Action: TorrentAvailabilityAction(row.Action),
		State: State(row.AfterState), Version: row.ResultingTorrentVersion, ChangedAt: row.OccurredAt.Time.UTC(),
	}, true, nil
}

func mapTorrentAdministrationWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		switch postgresError.ConstraintName {
		case "torrent_lifecycle_changes_pkey":
			return ErrManagedTorrentIdempotencyConflict
		case "torrent_lifecycle_changes_torrent_id_expected_torrent_version_key":
			return ErrManagedTorrentVersionConflict
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func torrentAdministrationTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

var _ TorrentAdministrationRepository = (*PostgresTorrentAdministrationRepository)(nil)
