package promotions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/generated/promotiondb"
	"github.com/peergo/peergo/services/core/internal/modules/economy"
)

type PostgresRepository struct {
	pool             *pgxpool.Pool
	eventBuilder     CampaignEventBuilder
	newAuditAppender func(pgx.Tx) auditevent.Appender
	economy          *economy.PostgresRepository
}

func NewPostgresRepository(pool *pgxpool.Pool, eventBuilder CampaignEventBuilder, newAuditAppender func(pgx.Tx) auditevent.Appender) (*PostgresRepository, error) {
	if pool == nil || eventBuilder == nil || newAuditAppender == nil {
		return nil, errors.New("promotion repository dependencies are required")
	}
	ledger, err := economy.NewPostgresRepository(pool)
	if err != nil {
		return nil, err
	}
	return &PostgresRepository{pool: pool, eventBuilder: eventBuilder, newAuditAppender: newAuditAppender, economy: ledger}, nil
}

func (repository *PostgresRepository) List(ctx context.Context, limit, offset int, now time.Time) (Page, error) {
	if limit < 1 || limit > MaxListLimit || offset < 0 || now.IsZero() {
		return Page{}, ErrInput
	}
	queries := promotiondb.New(repository.pool)
	rows, err := queries.ListPromotionCampaigns(ctx, promotiondb.ListPromotionCampaignsParams{ResultLimit: int32(limit), ResultOffset: int32(offset)})
	if err != nil {
		return Page{}, fmt.Errorf("list promotion campaigns: %w", err)
	}
	total, err := queries.CountPromotionCampaigns(ctx)
	if err != nil {
		return Page{}, fmt.Errorf("count promotion campaigns: %w", err)
	}
	page := Page{Items: make([]Campaign, 0, len(rows)), Total: total}
	for _, row := range rows {
		campaign, err := campaignFromRow(row, now)
		if err != nil {
			return Page{}, err
		}
		page.Items = append(page.Items, campaign)
	}
	return page, nil
}

func (repository *PostgresRepository) Schedule(ctx context.Context, command ScheduleCommand) (Campaign, error) {
	if _, err := normalizeScheduleInput(command.ScheduleInput, command.OccurredAt); err != nil ||
		command.ActorID == uuid.Nil || command.Authorization.ID == uuid.Nil || len(command.CommandJSON) < 2 {
		return Campaign{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Campaign{}, fmt.Errorf("begin promotion campaign: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := promotiondb.New(tx)
	if err := queries.LockPromotionScheduling(ctx); err != nil {
		return Campaign{}, fmt.Errorf("lock promotion scheduling: %w", err)
	}
	if existing, found, err := replayCampaign(ctx, queries, command); found || err != nil {
		if err != nil {
			return Campaign{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Campaign{}, fmt.Errorf("commit replayed promotion campaign: %w", err)
		}
		return existing, nil
	}

	torrentID := nullableInt64(command.TorrentID)
	torrentTitle := ""
	if command.Scope == ScopeTorrent {
		target, err := queries.GetPromotionTorrentTarget(ctx, *command.TorrentID)
		if errors.Is(err, pgx.ErrNoRows) {
			return Campaign{}, ErrNotFound
		}
		if err != nil {
			return Campaign{}, fmt.Errorf("read promotion torrent target: %w", err)
		}
		if target.State != "published" {
			return Campaign{}, ErrTorrentUnavailable
		}
		torrentTitle = target.Title
	}
	overlaps, err := queries.PromotionCampaignScopeOverlaps(ctx, promotiondb.PromotionCampaignScopeOverlapsParams{
		ScopeType: string(command.Scope), TorrentID: torrentID,
		StartsAt: timestamp(command.StartsAt), EndsAt: timestamp(command.EndsAt),
	})
	if err != nil {
		return Campaign{}, fmt.Errorf("check promotion campaign overlap: %w", err)
	}
	if overlaps {
		return Campaign{}, ErrScopeOverlap
	}
	// A global campaign would otherwise hide traffic benefits that members
	// already paid for. Refuse the conflicting schedule rather than silently
	// shortening an immutable paid order.
	if command.Scope == ScopeGlobal {
		overlapsPaid, err := queries.GlobalPromotionOverlapsMemberPurchase(ctx, promotiondb.GlobalPromotionOverlapsMemberPurchaseParams{
			StartsAt: timestamp(command.StartsAt), EndsAt: timestamp(command.EndsAt),
		})
		if err != nil {
			return Campaign{}, fmt.Errorf("check paid promotion overlap: %w", err)
		}
		if overlapsPaid {
			return Campaign{}, ErrScopeOverlap
		}
	}
	if err := queries.InsertPromotionCampaign(ctx, promotiondb.InsertPromotionCampaignParams{
		CampaignID: command.CampaignID, ScopeType: string(command.Scope), TorrentID: torrentID,
		Promotion: string(command.Promotion), StartsAt: timestamp(command.StartsAt), EndsAt: timestamp(command.EndsAt),
		OverrideLowerScopes: command.Scope == ScopeGlobal, Reason: command.Reason, ActorID: command.ActorID,
		AuthorizationDecisionID: command.Authorization.ID, CommandJson: string(command.CommandJSON),
		CommandSha256: command.CommandSHA256[:], CreatedAt: timestamp(command.OccurredAt),
	}); err != nil {
		return Campaign{}, fmt.Errorf("insert promotion campaign: %w", err)
	}
	if err := queries.EnqueuePromotionDelivery(ctx, promotiondb.EnqueuePromotionDeliveryParams{
		CampaignID: command.CampaignID, AvailableAt: timestamp(command.OccurredAt), CreatedAt: timestamp(command.OccurredAt),
	}); err != nil {
		return Campaign{}, fmt.Errorf("enqueue promotion command: %w", err)
	}
	campaign := Campaign{
		ID: command.CampaignID, Source: CampaignSourceStaffSchedule, Scope: command.Scope, TorrentID: command.TorrentID, TorrentTitle: torrentTitle,
		Promotion: command.Promotion, StartsAt: command.StartsAt, EndsAt: command.EndsAt,
		OverrideLowerScopes: command.Scope == ScopeGlobal, Reason: command.Reason, ActorID: command.ActorID,
		CreatedAt: command.OccurredAt, DeliveryState: DeliveryPending, TimelineState: timelineState(command.StartsAt, command.EndsAt, command.OccurredAt),
	}
	auditEvent, err := repository.eventBuilder.BuildPromotionCampaignEvent(CampaignAuditInput{Campaign: campaign, Authorization: command.Authorization})
	if err != nil {
		return Campaign{}, fmt.Errorf("build promotion campaign audit event: %w", err)
	}
	if err := repository.newAuditAppender(tx).Append(ctx, auditEvent); err != nil {
		return Campaign{}, fmt.Errorf("append promotion campaign audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Campaign{}, fmt.Errorf("commit promotion campaign: %w", err)
	}
	return campaign, nil
}

func replayCampaign(ctx context.Context, queries *promotiondb.Queries, command ScheduleCommand) (Campaign, bool, error) {
	row, err := queries.GetPromotionCampaign(ctx, command.CampaignID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Campaign{}, false, nil
	}
	if err != nil {
		return Campaign{}, false, fmt.Errorf("read promotion campaign replay: %w", err)
	}
	if row.ScopeType != string(command.Scope) || optionalInt64(row.TorrentID) != optionalInt64Value(command.TorrentID) ||
		row.Promotion != string(command.Promotion) || !row.StartsAt.Valid || !row.StartsAt.Time.Equal(command.StartsAt) ||
		!row.EndsAt.Valid || !row.EndsAt.Time.Equal(command.EndsAt) || row.Reason != command.Reason ||
		row.ActorID != command.ActorID || row.AuthorizationDecisionID != command.Authorization.ID ||
		row.CommandJson != string(command.CommandJSON) || !bytes.Equal(row.CommandSha256, command.CommandSHA256[:]) || !row.CreatedAt.Valid {
		return Campaign{}, true, ErrIdempotencyConflict
	}
	return Campaign{
		ID: row.ID, Source: CampaignSourceStaffSchedule, Scope: Scope(row.ScopeType), TorrentID: command.TorrentID, Promotion: Promotion(row.Promotion),
		StartsAt: row.StartsAt.Time.UTC(), EndsAt: row.EndsAt.Time.UTC(), OverrideLowerScopes: command.Scope == ScopeGlobal,
		Reason: row.Reason, ActorID: row.ActorID, CreatedAt: row.CreatedAt.Time.UTC(), DeliveryState: DeliveryPending,
		TimelineState: timelineState(row.StartsAt.Time, row.EndsAt.Time, command.OccurredAt),
	}, true, nil
}

func campaignFromRow(row promotiondb.ListPromotionCampaignsRow, now time.Time) (Campaign, error) {
	if row.ID == uuid.Nil || !row.StartsAt.Valid || !row.EndsAt.Valid || !row.EndsAt.Time.After(row.StartsAt.Time) ||
		!row.CreatedAt.Valid || row.ActorID == uuid.Nil || !validPromotion(Promotion(row.Promotion)) || row.Attempts < 0 {
		return Campaign{}, ErrInvariant
	}
	if row.SourceKind != string(CampaignSourceStaffSchedule) && row.SourceKind != string(CampaignSourceMemberPurchase) {
		return Campaign{}, ErrInvariant
	}
	scope := Scope(row.ScopeType)
	var torrentID *int64
	if row.TorrentID.Valid {
		value := row.TorrentID.Int64
		torrentID = &value
	}
	if (scope == ScopeGlobal && torrentID != nil) || (scope == ScopeTorrent && (torrentID == nil || row.TorrentTitle == "")) {
		return Campaign{}, ErrInvariant
	}
	deliveryState := DeliveryPending
	var deliveredAt *time.Time
	if row.DeliveredAt.Valid {
		value := row.DeliveredAt.Time.UTC()
		deliveredAt = &value
		deliveryState = DeliveryDelivered
	} else if row.Attempts > 0 && row.LastErrorCode != "" {
		deliveryState = DeliveryRetrying
	}
	return Campaign{
		ID: row.ID, Source: CampaignSource(row.SourceKind), Scope: scope, TorrentID: torrentID, TorrentTitle: row.TorrentTitle, Promotion: Promotion(row.Promotion),
		StartsAt: row.StartsAt.Time.UTC(), EndsAt: row.EndsAt.Time.UTC(), OverrideLowerScopes: row.OverrideLowerScopes,
		Reason: row.Reason, ActorID: row.ActorID, CreatedAt: row.CreatedAt.Time.UTC(), DeliveryState: deliveryState,
		DeliveryAttempts: row.Attempts, LastDeliveryError: row.LastErrorCode, DeliveredAt: deliveredAt,
		TimelineState: timelineState(row.StartsAt.Time, row.EndsAt.Time, now),
	}, nil
}

func timelineState(startsAt, endsAt, now time.Time) TimelineState {
	if now.Before(startsAt) {
		return TimelineScheduled
	}
	if now.Before(endsAt) {
		return TimelineActive
	}
	return TimelineExpired
}

func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC().Round(0), Valid: true}
}

func nullableInt64(value *int64) pgtype.Int8 {
	if value == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *value, Valid: true}
}

func optionalInt64(value pgtype.Int8) int64 {
	if !value.Valid {
		return 0
	}
	return value.Int64
}

func optionalInt64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
