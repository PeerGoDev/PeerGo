package rss

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/generated/rssdb"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type PostgresRepository struct {
	pool    *pgxpool.Pool
	queries *rssdb.Queries
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, errors.New("rss repository pool is required")
	}
	return &PostgresRepository{pool: pool, queries: rssdb.New(pool)}, nil
}

func (repository *PostgresRepository) Settings(ctx context.Context) (Settings, error) {
	row, err := repository.queries.GetRSSSettings(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return Settings{}, ErrSettingsNotFound
	}
	if err != nil {
		return Settings{}, fmt.Errorf("get rss settings: %w", err)
	}
	return settingsFromValues(row.Enabled, row.CacheTtlSeconds, row.MaxItemsPerFeed, row.MaxSubscriptionsPerUser, row.RequestsPerMinute, row.Version, row.EffectiveAt, row.UpdatedAt)
}

func (repository *PostgresRepository) UpdateSettings(ctx context.Context, command SettingsChangeCommand) (Settings, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Settings{}, fmt.Errorf("begin rss settings update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := rssdb.New(tx)
	locked, err := queries.GetRSSSettingsForUpdate(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return Settings{}, ErrSettingsNotFound
	}
	if err != nil {
		return Settings{}, fmt.Errorf("lock rss settings: %w", err)
	}
	before, err := settingsFromValues(locked.Enabled, locked.CacheTtlSeconds, locked.MaxItemsPerFeed, locked.MaxSubscriptionsPerUser, locked.RequestsPerMinute, locked.Version, locked.EffectiveAt, locked.UpdatedAt)
	if err != nil {
		return Settings{}, err
	}
	if before.Version != command.ExpectedVersion {
		return Settings{}, ErrSettingsConflict
	}
	row, err := queries.UpdateRSSSettings(ctx, rssdb.UpdateRSSSettingsParams{
		Enabled: command.Enabled, CacheTtlSeconds: int32(command.CacheTTLSeconds),
		MaxItemsPerFeed: int32(command.MaxItemsPerFeed), MaxSubscriptionsPerUser: int32(command.MaxSubscriptionsPerUser),
		RequestsPerMinute: int32(command.RequestsPerMinute), OccurredAt: timestamp(command.OccurredAt),
		ExpectedVersion: command.ExpectedVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Settings{}, ErrSettingsConflict
	}
	if err != nil {
		return Settings{}, fmt.Errorf("update rss settings: %w", err)
	}
	after, err := settingsFromValues(row.Enabled, row.CacheTtlSeconds, row.MaxItemsPerFeed, row.MaxSubscriptionsPerUser, row.RequestsPerMinute, row.Version, row.EffectiveAt, row.UpdatedAt)
	if err != nil {
		return Settings{}, err
	}
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		return Settings{}, fmt.Errorf("encode rss settings before snapshot: %w", err)
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		return Settings{}, fmt.Errorf("encode rss settings after snapshot: %w", err)
	}
	if err := queries.InsertRSSSettingsChange(ctx, rssdb.InsertRSSSettingsChangeParams{
		ID: command.ID, ActorID: command.ActorID, AuthorizationDecisionID: command.Authorization.ID,
		ExpectedVersion: command.ExpectedVersion, ResultingVersion: after.Version,
		BeforeJson: beforeJSON, AfterJson: afterJSON, Reason: command.Reason, OccurredAt: timestamp(command.OccurredAt),
	}); err != nil {
		return Settings{}, fmt.Errorf("append rss settings change: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Settings{}, fmt.Errorf("commit rss settings update: %w", err)
	}
	return after, nil
}

func (repository *PostgresRepository) ListSubscriptions(ctx context.Context, userID uuid.UUID) ([]Subscription, error) {
	rows, err := repository.queries.ListRSSSubscriptions(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list rss subscriptions: %w", err)
	}
	result := make([]Subscription, 0, len(rows))
	for _, row := range rows {
		subscription, conversionErr := subscriptionFromValues(row.ID, row.Name, row.Enabled, row.TokenVersion, row.CategoryIds, row.PromotionFilters, row.PriceFilter, row.BookmarkedOnly, row.ItemLimit, row.IncludeCategory, row.IncludeSubtitle, row.IncludeSize, row.IncludePromotion, row.Version, row.CreatedAt, row.UpdatedAt)
		if conversionErr != nil {
			return nil, conversionErr
		}
		result = append(result, subscription)
	}
	return result, nil
}

func (repository *PostgresRepository) CreateSubscription(ctx context.Context, userID uuid.UUID, input SubscriptionInput, id uuid.UUID, tokenDigest []byte, now time.Time) (Subscription, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Subscription{}, fmt.Errorf("begin rss subscription create: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// The per-user advisory lock makes the configured subscription cap exact
	// under concurrent browser requests without locking the identity row.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, userID.String()); err != nil {
		return Subscription{}, fmt.Errorf("lock rss subscription owner: %w", err)
	}
	queries := rssdb.New(tx)
	settings, err := queries.GetRSSSettings(ctx)
	if err != nil {
		return Subscription{}, fmt.Errorf("read rss subscription cap: %w", err)
	}
	count, err := queries.CountRSSSubscriptions(ctx, userID)
	if err != nil {
		return Subscription{}, fmt.Errorf("count rss subscriptions: %w", err)
	}
	if count >= int64(settings.MaxSubscriptionsPerUser) {
		return Subscription{}, ErrSubscriptionLimit
	}
	row, err := queries.CreateRSSSubscription(ctx, rssdb.CreateRSSSubscriptionParams{
		ID: id, UserID: userID, Name: input.Name, Enabled: input.Enabled, TokenSha256: tokenDigest,
		CategoryIds: input.CategoryIDs, PromotionFilters: input.PromotionFilters, PriceFilter: string(input.PriceFilter),
		BookmarkedOnly: input.BookmarkedOnly, ItemLimit: int32(input.ItemLimit), IncludeCategory: input.IncludeCategory,
		IncludeSubtitle: input.IncludeSubtitle, IncludeSize: input.IncludeSize, IncludePromotion: input.IncludePromotion,
		CreatedAt: timestamp(now),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Subscription{}, ErrInvalidInput
	}
	if err != nil {
		return Subscription{}, fmt.Errorf("create rss subscription: %w", err)
	}
	result, err := subscriptionFromValues(row.ID, row.Name, row.Enabled, row.TokenVersion, row.CategoryIds, row.PromotionFilters, row.PriceFilter, row.BookmarkedOnly, row.ItemLimit, row.IncludeCategory, row.IncludeSubtitle, row.IncludeSize, row.IncludePromotion, row.Version, row.CreatedAt, row.UpdatedAt)
	if err != nil {
		return Subscription{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Subscription{}, fmt.Errorf("commit rss subscription create: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) UpdateSubscription(ctx context.Context, userID uuid.UUID, input UpdateSubscriptionInput, now time.Time) (Subscription, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Subscription{}, fmt.Errorf("begin rss subscription update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := rssdb.New(tx)
	if _, err := queries.GetRSSSubscriptionForUpdate(ctx, rssdb.GetRSSSubscriptionForUpdateParams{ID: input.ID, UserID: userID}); errors.Is(err, pgx.ErrNoRows) {
		return Subscription{}, ErrSubscriptionNotFound
	} else if err != nil {
		return Subscription{}, fmt.Errorf("lock rss subscription: %w", err)
	}
	row, err := queries.UpdateRSSSubscription(ctx, rssdb.UpdateRSSSubscriptionParams{
		Name: input.Name, Enabled: input.Enabled, CategoryIds: input.CategoryIDs,
		PromotionFilters: input.PromotionFilters, PriceFilter: string(input.PriceFilter), BookmarkedOnly: input.BookmarkedOnly,
		ItemLimit: int32(input.ItemLimit), IncludeCategory: input.IncludeCategory, IncludeSubtitle: input.IncludeSubtitle,
		IncludeSize: input.IncludeSize, IncludePromotion: input.IncludePromotion, UpdatedAt: timestamp(now),
		ID: input.ID, UserID: userID, ExpectedVersion: input.ExpectedVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Subscription{}, ErrSubscriptionConflict
	}
	if err != nil {
		return Subscription{}, fmt.Errorf("update rss subscription: %w", err)
	}
	if err := queries.DeleteRSSFeedCache(ctx, input.ID); err != nil {
		return Subscription{}, fmt.Errorf("invalidate rss subscription cache: %w", err)
	}
	result, err := subscriptionFromValues(row.ID, row.Name, row.Enabled, row.TokenVersion, row.CategoryIds, row.PromotionFilters, row.PriceFilter, row.BookmarkedOnly, row.ItemLimit, row.IncludeCategory, row.IncludeSubtitle, row.IncludeSize, row.IncludePromotion, row.Version, row.CreatedAt, row.UpdatedAt)
	if err != nil {
		return Subscription{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Subscription{}, fmt.Errorf("commit rss subscription update: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) RotateToken(ctx context.Context, userID uuid.UUID, input SubscriptionVersionInput, digest []byte, now time.Time) (Subscription, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Subscription{}, fmt.Errorf("begin rss token rotation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := rssdb.New(tx)
	row, err := queries.RotateRSSSubscriptionToken(ctx, rssdb.RotateRSSSubscriptionTokenParams{
		TokenSha256: digest, UpdatedAt: timestamp(now), ID: input.ID, UserID: userID, ExpectedVersion: input.ExpectedVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Subscription{}, ErrSubscriptionConflict
	}
	if err != nil {
		return Subscription{}, fmt.Errorf("rotate rss token: %w", err)
	}
	if err := queries.DeleteRSSFeedCache(ctx, input.ID); err != nil {
		return Subscription{}, fmt.Errorf("invalidate rotated rss cache: %w", err)
	}
	result, err := subscriptionFromValues(row.ID, row.Name, row.Enabled, row.TokenVersion, row.CategoryIds, row.PromotionFilters, row.PriceFilter, row.BookmarkedOnly, row.ItemLimit, row.IncludeCategory, row.IncludeSubtitle, row.IncludeSize, row.IncludePromotion, row.Version, row.CreatedAt, row.UpdatedAt)
	if err != nil {
		return Subscription{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Subscription{}, fmt.Errorf("commit rss token rotation: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) RevokeSubscription(ctx context.Context, userID uuid.UUID, input SubscriptionVersionInput, now time.Time) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin rss subscription revocation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := rssdb.New(tx)
	affected, err := queries.RevokeRSSSubscription(ctx, rssdb.RevokeRSSSubscriptionParams{RevokedAt: timestamp(now), ID: input.ID, UserID: userID, ExpectedVersion: input.ExpectedVersion})
	if err != nil {
		return fmt.Errorf("revoke rss subscription: %w", err)
	}
	if affected == 0 {
		return ErrSubscriptionConflict
	}
	if err := queries.DeleteRSSFeedCache(ctx, input.ID); err != nil {
		return fmt.Errorf("delete revoked rss cache: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit rss subscription revocation: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) ResolveByToken(ctx context.Context, digest []byte, now time.Time) (ResolvedSubscription, error) {
	row, err := repository.queries.ResolveRSSSubscriptionByToken(ctx, rssdb.ResolveRSSSubscriptionByTokenParams{TokenSha256: digest, AsOf: timestamp(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return ResolvedSubscription{}, ErrTokenInvalid
	}
	if err != nil {
		return ResolvedSubscription{}, fmt.Errorf("resolve rss subscription token: %w", err)
	}
	subscription, err := subscriptionFromValues(row.ID, row.Name, true, row.TokenVersion, row.CategoryIds, row.PromotionFilters, row.PriceFilter, row.BookmarkedOnly, row.ItemLimit, row.IncludeCategory, row.IncludeSubtitle, row.IncludeSize, row.IncludePromotion, row.Version, row.CreatedAt, row.UpdatedAt)
	if err != nil {
		return ResolvedSubscription{}, err
	}
	verifiedAt := row.EmailVerifiedAt.Time
	return ResolvedSubscription{
		Subscription:    subscription,
		User:            identity.User{ID: row.UserID, CredentialRef: row.CredentialRef, Username: row.Username, DisplayName: row.DisplayName, EmailVerifiedAt: &verifiedAt},
		CacheTTLSeconds: int(row.CacheTtlSeconds), MaxItemsPerFeed: int(row.MaxItemsPerFeed), RequestsPerMinute: int(row.RequestsPerMinute),
	}, nil
}

func (repository *PostgresRepository) ConsumeAllowance(ctx context.Context, userID uuid.UUID, now time.Time, limit int) error {
	row, err := repository.queries.ConsumeRSSRequestAllowance(ctx, rssdb.ConsumeRSSRequestAllowanceParams{UserID: userID, RequestedAt: timestamp(now)})
	if err != nil {
		return fmt.Errorf("consume rss request allowance: %w", err)
	}
	if row.RequestCount > int32(limit) {
		return &RateLimitError{RetryAt: row.WindowStartedAt.Time.Add(time.Minute)}
	}
	return nil
}

func (repository *PostgresRepository) FeedProjection(ctx context.Context, subscription ResolvedSubscription, now time.Time) (FeedProjection, error) {
	revision, err := repository.queries.GetRSSContentRevision(ctx)
	if err != nil {
		return FeedProjection{}, fmt.Errorf("read rss content revision: %w", err)
	}
	cache, err := repository.queries.GetValidRSSFeedCache(ctx, rssdb.GetValidRSSFeedCacheParams{
		SubscriptionID: subscription.ID, SubscriptionVersion: subscription.Version, ContentRevision: revision, AsOf: timestamp(now),
	})
	if err == nil {
		var items []FeedItem
		if decodeErr := json.Unmarshal(cache.ItemProjection, &items); decodeErr == nil {
			return FeedProjection{ObservedAt: cache.ObservedAt.Time, ExpiresAt: cache.ExpiresAt.Time, Items: items}, nil
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return FeedProjection{}, fmt.Errorf("read rss feed cache: %w", err)
	}
	observedAt := now.UTC()
	limit := subscription.ItemLimit
	if limit > subscription.MaxItemsPerFeed {
		limit = subscription.MaxItemsPerFeed
	}
	rows, err := repository.queries.ListRSSFeedItems(ctx, rssdb.ListRSSFeedItemsParams{
		ObservedAt: timestamp(observedAt), CategoryIds: subscription.CategoryIDs, PromotionFilters: subscription.PromotionFilters,
		PriceFilter: string(subscription.PriceFilter), BookmarkedOnly: subscription.BookmarkedOnly, UserID: subscription.User.ID,
		ResultLimit: int32(limit),
	})
	if err != nil {
		return FeedProjection{}, fmt.Errorf("list rss feed items: %w", err)
	}
	items := make([]FeedItem, 0, len(rows))
	for _, row := range rows {
		if !row.PublishedAt.Valid {
			return FeedProjection{}, errors.New("rss feed item has invalid publication timestamp")
		}
		items = append(items, FeedItem{
			TorrentID: row.ID, Title: row.Name, Subtitle: row.Subtitle, SizeBytes: row.SizeBytes,
			Promotion: row.Promotion, PromotionEndsAt: optionalTime(row.PromotionEndsAt), StickyUntil: optionalTime(row.StickyUntil),
			PublishedAt: row.PublishedAt.Time, CategoryID: row.CategoryID, CategoryName: row.CategoryName,
			Seeders: int(row.Seeders), Leechers: int(row.Leechers), Completed: int(row.Completed), PurchasePrice: row.PurchasePrice,
		})
	}
	expiresAt := observedAt.Add(time.Duration(subscription.CacheTTLSeconds) * time.Second)
	boundary, err := repository.queries.GetNextRSSContentBoundary(ctx, timestamp(observedAt))
	if err != nil {
		return FeedProjection{}, fmt.Errorf("read next rss content boundary: %w", err)
	}
	if boundary.Valid && boundary.Time.Before(expiresAt) {
		expiresAt = boundary.Time
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return FeedProjection{}, fmt.Errorf("encode rss feed cache: %w", err)
	}
	if err := repository.queries.UpsertRSSFeedCache(ctx, rssdb.UpsertRSSFeedCacheParams{
		SubscriptionID: subscription.ID, SubscriptionVersion: subscription.Version, ContentRevision: revision,
		ObservedAt: timestamp(observedAt), ExpiresAt: timestamp(expiresAt), NextBoundaryAt: boundary,
		ItemProjection: encoded, UpdatedAt: timestamp(now),
	}); err != nil {
		return FeedProjection{}, fmt.Errorf("store rss feed cache: %w", err)
	}
	return FeedProjection{ObservedAt: observedAt, ExpiresAt: expiresAt, Items: items}, nil
}

func settingsFromValues(enabled bool, ttl, maxItems, maxSubscriptions, rate int32, version int64, effectiveAt, updatedAt pgtype.Timestamptz) (Settings, error) {
	if version < 1 || !effectiveAt.Valid || !updatedAt.Valid {
		return Settings{}, errors.New("invalid rss settings projection")
	}
	return Settings{Enabled: enabled, CacheTTLSeconds: int(ttl), MaxItemsPerFeed: int(maxItems), MaxSubscriptionsPerUser: int(maxSubscriptions), RequestsPerMinute: int(rate), Version: version, EffectiveAt: effectiveAt.Time, UpdatedAt: updatedAt.Time}, nil
}

func subscriptionFromValues(id uuid.UUID, name string, enabled bool, tokenVersion int64, categoryIDs, promotionFilters []string, priceFilter string, bookmarkedOnly bool, itemLimit int32, includeCategory, includeSubtitle, includeSize, includePromotion bool, version int64, createdAt, updatedAt pgtype.Timestamptz) (Subscription, error) {
	if id == uuid.Nil || tokenVersion < 1 || version < 1 || !createdAt.Valid || !updatedAt.Valid {
		return Subscription{}, errors.New("invalid rss subscription projection")
	}
	return Subscription{ID: id, Name: name, Enabled: enabled, TokenVersion: tokenVersion, CategoryIDs: categoryIDs, PromotionFilters: promotionFilters, PriceFilter: PriceFilter(priceFilter), BookmarkedOnly: bookmarkedOnly, ItemLimit: int(itemLimit), IncludeCategory: includeCategory, IncludeSubtitle: includeSubtitle, IncludeSize: includeSize, IncludePromotion: includePromotion, Version: version, CreatedAt: createdAt.Time, UpdatedAt: updatedAt.Time}, nil
}

func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func optionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}
