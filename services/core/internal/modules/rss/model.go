// Package rss provides saved, member-owned RSS feeds for published torrents.
// A feed token is a narrow delegated credential: it can read one configured
// feed and download only torrents already permitted to its owning account.
package rss

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

var (
	ErrInvalidInput         = errors.New("rss input is invalid")
	ErrSubscriptionNotFound = errors.New("rss subscription was not found")
	ErrSubscriptionConflict = errors.New("rss subscription version changed")
	ErrSubscriptionLimit    = errors.New("rss subscription limit reached")
	ErrTokenInvalid         = errors.New("rss token is invalid")
	ErrRateLimited          = errors.New("rss request rate limit exceeded")
	ErrSettingsNotFound     = errors.New("rss settings were not found")
	ErrSettingsConflict     = errors.New("rss settings version changed")
)

type PriceFilter string

const (
	PriceFilterAll  PriceFilter = "all"
	PriceFilterFree PriceFilter = "free"
	PriceFilterPaid PriceFilter = "paid"
)

type Subscription struct {
	ID               uuid.UUID
	Name             string
	Enabled          bool
	TokenVersion     int64
	CategoryIDs      []string
	PromotionFilters []string
	PriceFilter      PriceFilter
	BookmarkedOnly   bool
	ItemLimit        int
	IncludeCategory  bool
	IncludeSubtitle  bool
	IncludeSize      bool
	IncludePromotion bool
	Version          int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// IssuedSubscription is the only value that may contain a raw token.  List
// and update projections intentionally cannot reconstruct a feed URL.
type IssuedSubscription struct {
	Subscription Subscription
	Token        string
	FeedURL      string
}

type SubscriptionInput struct {
	Name             string
	Enabled          bool
	CategoryIDs      []string
	PromotionFilters []string
	PriceFilter      PriceFilter
	BookmarkedOnly   bool
	ItemLimit        int
	IncludeCategory  bool
	IncludeSubtitle  bool
	IncludeSize      bool
	IncludePromotion bool
}

type UpdateSubscriptionInput struct {
	SubscriptionInput
	ID              uuid.UUID
	ExpectedVersion int64
}

type SubscriptionVersionInput struct {
	ID              uuid.UUID
	ExpectedVersion int64
}

type Settings struct {
	Enabled                 bool      `json:"enabled"`
	CacheTTLSeconds         int       `json:"cache_ttl_seconds"`
	MaxItemsPerFeed         int       `json:"max_items_per_feed"`
	MaxSubscriptionsPerUser int       `json:"max_subscriptions_per_user"`
	RequestsPerMinute       int       `json:"requests_per_minute"`
	Version                 int64     `json:"version"`
	EffectiveAt             time.Time `json:"effective_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type UpdateSettingsInput struct {
	Enabled                 bool
	CacheTTLSeconds         int
	MaxItemsPerFeed         int
	MaxSubscriptionsPerUser int
	RequestsPerMinute       int
	ExpectedVersion         int64
	Reason                  string
}

type FeedItem struct {
	TorrentID       int64      `json:"torrent_id"`
	Title           string     `json:"title"`
	Subtitle        string     `json:"subtitle"`
	SizeBytes       int64      `json:"size_bytes"`
	Promotion       string     `json:"promotion"`
	PromotionEndsAt *time.Time `json:"promotion_ends_at,omitempty"`
	StickyUntil     *time.Time `json:"sticky_until,omitempty"`
	PublishedAt     time.Time  `json:"published_at"`
	CategoryID      string     `json:"category_id"`
	CategoryName    string     `json:"category_name"`
	Seeders         int        `json:"seeders"`
	Leechers        int        `json:"leechers"`
	Completed       int        `json:"completed"`
	PurchasePrice   int64      `json:"purchase_price"`
}

type ResolvedSubscription struct {
	Subscription
	User              identity.User
	CacheTTLSeconds   int
	MaxItemsPerFeed   int
	RequestsPerMinute int
}

type FeedProjection struct {
	ObservedAt time.Time
	ExpiresAt  time.Time
	Items      []FeedItem
}

type FeedDocument struct {
	Data         []byte
	ETag         string
	LastModified time.Time
	ExpiresAt    time.Time
	User         identity.User
}

type RateLimitError struct {
	RetryAt time.Time
}

func (err *RateLimitError) Error() string { return ErrRateLimited.Error() }
func (err *RateLimitError) Unwrap() error { return ErrRateLimited }

type SettingsChangeCommand struct {
	UpdateSettingsInput
	ID            uuid.UUID
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}
