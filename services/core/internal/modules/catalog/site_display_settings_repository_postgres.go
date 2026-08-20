package catalog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/generated/catalogdb"
)

// PostgresSiteDisplaySettingsRepository owns the singleton mutation
// transaction. The row remains locked from expected-version validation until
// the new value and its audit outbox evidence commit together. A stale editor
// therefore fails closed instead of silently replacing a newer version.
type PostgresSiteDisplaySettingsRepository struct {
	pool         *pgxpool.Pool
	queries      *catalogdb.Queries
	eventBuilder SiteDisplaySettingsEventBuilder
	newAppender  func(pgx.Tx) auditevent.Appender
}

func NewPostgresSiteDisplaySettingsRepository(pool *pgxpool.Pool, eventBuilder SiteDisplaySettingsEventBuilder, newAppender func(pgx.Tx) auditevent.Appender) (*PostgresSiteDisplaySettingsRepository, error) {
	if pool == nil || eventBuilder == nil || newAppender == nil {
		return nil, errors.New("site display settings repository dependencies are required")
	}
	return &PostgresSiteDisplaySettingsRepository{
		pool: pool, queries: catalogdb.New(pool), eventBuilder: eventBuilder, newAppender: newAppender,
	}, nil
}

func (repository *PostgresSiteDisplaySettingsRepository) GetSiteDisplaySettings(ctx context.Context) (SiteDisplaySettings, error) {
	row, err := repository.queries.GetSiteDisplaySettings(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return SiteDisplaySettings{}, ErrSiteDisplaySettingsNotFound
	}
	if err != nil {
		return SiteDisplaySettings{}, fmt.Errorf("query site display settings: %w", err)
	}
	return siteDisplaySettingsFromValues(
		row.Name, row.Description, row.DefaultTorrentView, row.ShowLatestAnnouncement,
		row.Version, row.EffectiveAt, row.UpdatedAt,
	)
}

func (repository *PostgresSiteDisplaySettingsRepository) UpdateSiteDisplaySettings(ctx context.Context, command UpdateSiteDisplaySettingsCommand) (SiteDisplaySettings, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return SiteDisplaySettings{}, fmt.Errorf("begin site display settings update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := catalogdb.New(tx)
	locked, err := queries.GetSiteDisplaySettingsForUpdate(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return SiteDisplaySettings{}, ErrSiteDisplaySettingsNotFound
	}
	if err != nil {
		return SiteDisplaySettings{}, fmt.Errorf("lock site display settings: %w", err)
	}
	before, err := siteDisplaySettingsFromValues(
		locked.Name, locked.Description, locked.DefaultTorrentView, locked.ShowLatestAnnouncement,
		locked.Version, locked.EffectiveAt, locked.UpdatedAt,
	)
	if err != nil {
		return SiteDisplaySettings{}, err
	}
	if before.Version != command.ExpectedVersion {
		return SiteDisplaySettings{}, ErrSiteDisplaySettingsVersionConflict
	}

	row, err := queries.UpdateSiteDisplaySettings(ctx, catalogdb.UpdateSiteDisplaySettingsParams{
		SiteName: command.Name, SiteDescription: command.Description,
		DefaultTorrentView:     string(command.DefaultTorrentView),
		ShowLatestAnnouncement: command.ShowLatestAnnouncement,
		OccurredAt:             siteDisplaySettingsTimestamp(command.OccurredAt),
		ExpectedVersion:        command.ExpectedVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return SiteDisplaySettings{}, ErrSiteDisplaySettingsVersionConflict
	}
	if err != nil {
		return SiteDisplaySettings{}, fmt.Errorf("update site display settings row: %w", err)
	}
	after, err := siteDisplaySettingsFromValues(
		row.Name, row.Description, row.DefaultTorrentView, row.ShowLatestAnnouncement,
		row.Version, row.EffectiveAt, row.UpdatedAt,
	)
	if err != nil {
		return SiteDisplaySettings{}, err
	}
	event, err := repository.eventBuilder.BuildSiteDisplaySettingsEvent(SiteDisplaySettingsAuditInput{
		OccurredAt: command.OccurredAt, ActorID: command.ActorID, Reason: command.Reason,
		ExpectedVersion: command.ExpectedVersion, Authorization: command.Authorization,
		Before: siteDisplaySettingsAuditState(before), After: siteDisplaySettingsAuditState(after),
	})
	if err != nil {
		return SiteDisplaySettings{}, fmt.Errorf("build site display settings audit event: %w", err)
	}
	if err := repository.newAppender(tx).Append(ctx, event); err != nil {
		return SiteDisplaySettings{}, fmt.Errorf("append site display settings audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return SiteDisplaySettings{}, fmt.Errorf("commit site display settings update: %w", err)
	}
	return after, nil
}

func siteDisplaySettingsFromValues(
	name string,
	description string,
	defaultView string,
	showLatestAnnouncement bool,
	version int64,
	effectiveAt pgtype.Timestamptz,
	updatedAt pgtype.Timestamptz,
) (SiteDisplaySettings, error) {
	view := TorrentView(defaultView)
	if (view != TorrentViewList && view != TorrentViewPoster) || version < 1 || !effectiveAt.Valid || !updatedAt.Valid {
		return SiteDisplaySettings{}, fmt.Errorf("%w: invalid site display settings projection", errCatalogProjectionInvalid)
	}
	return SiteDisplaySettings{
		Name: name, Description: description, DefaultTorrentView: view,
		ShowLatestAnnouncement: showLatestAnnouncement, Version: version,
		EffectiveAt: effectiveAt.Time, UpdatedAt: updatedAt.Time,
	}, nil
}

func siteDisplaySettingsAuditState(settings SiteDisplaySettings) SiteDisplaySettingsAuditState {
	return SiteDisplaySettingsAuditState{
		Name: settings.Name, Description: settings.Description,
		DefaultTorrentView:     settings.DefaultTorrentView,
		ShowLatestAnnouncement: settings.ShowLatestAnnouncement,
		Version:                settings.Version,
	}
}

func siteDisplaySettingsTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

var _ SiteDisplaySettingsRepository = (*PostgresSiteDisplaySettingsRepository)(nil)
