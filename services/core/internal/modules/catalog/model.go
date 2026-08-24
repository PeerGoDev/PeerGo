// Package catalog owns the first read-only Core vertical slice: site metadata,
// announcements, torrent categories, and torrent list summaries.
package catalog

import "time"

// Promotion describes the effective download promotion shown in a list row.
type Promotion string

const (
	PromotionNone                     Promotion = "none"
	PromotionFree                     Promotion = "free"
	PromotionDoubleUpload             Promotion = "double_upload"
	PromotionDoubleUploadFree         Promotion = "double_upload_free"
	PromotionHalfDownload             Promotion = "half_download"
	PromotionDoubleUploadHalfDownload Promotion = "double_upload_half_download"
	PromotionThirtyPercentDownload    Promotion = "thirty_percent_download"
)

// Valid reports whether promotion belongs to the public catalog vocabulary.
// Settlement owns the economic factors; Core only carries the effective label
// required by list and detail projections.
func (promotion Promotion) Valid() bool {
	switch promotion {
	case PromotionNone,
		PromotionFree,
		PromotionDoubleUpload,
		PromotionDoubleUploadFree,
		PromotionHalfDownload,
		PromotionDoubleUploadHalfDownload,
		PromotionThirtyPercentDownload:
		return true
	default:
		return false
	}
}

// TorrentSearchScope limits where a catalog keyword is matched. Keeping this
// vocabulary in Core prevents the Web client from approximating a title-only
// search against a single paginated result page.
type TorrentSearchScope string

const (
	TorrentSearchTitleSubtitle TorrentSearchScope = "title_subtitle"
	TorrentSearchTitle         TorrentSearchScope = "title"
	TorrentSearchSubtitle      TorrentSearchScope = "subtitle"
)

// TorrentSort is the bounded public ordering vocabulary. Repositories receive
// only these normalized values, so SQL never accepts a caller-provided ORDER BY.
type TorrentSort string

const (
	TorrentSortPublishedDesc TorrentSort = "published_desc"
	TorrentSortPublishedAsc  TorrentSort = "published_asc"
	TorrentSortSizeDesc      TorrentSort = "size_desc"
	TorrentSortSizeAsc       TorrentSort = "size_asc"
	TorrentSortCompletedDesc TorrentSort = "completed_desc"
)

// TorrentView is the public catalog layout selected before a visitor makes a
// local choice. It is presentation metadata, never a Tracker or content rule.
type TorrentView string

const (
	TorrentViewList   TorrentView = "list"
	TorrentViewPoster TorrentView = "poster"
)

// SiteInfo contains non-sensitive information needed to render the app shell.
type SiteInfo struct {
	Name                   string
	Description            string
	OnlineUsers            int
	DefaultTorrentView     TorrentView
	ShowLatestAnnouncement bool
	CustomNavigationItems  []CustomNavigationItem
}

// CustomNavigationItem is one bounded operator-configured sidebar link. The
// ordered slice is stored with the site singleton; Core never records clicks or
// visits for these links.
type CustomNavigationItem struct {
	Label        string `json:"label"`
	URL          string `json:"url"`
	OpenInNewTab bool   `json:"open_in_new_tab"`
	Enabled      bool   `json:"enabled"`
}

type AnnouncementBodyFormat string

const (
	AnnouncementBodyPlainText    AnnouncementBodyFormat = "plain_text"
	AnnouncementBodyLegacyBBCode AnnouncementBodyFormat = "legacy_bbcode"
)

// AnnouncementSummary is the only announcement shape used by list and home
// reads. Keeping the body out of this value prevents accidental N-body list
// queries and mirrors the public contract without creating a second DTO model.
type AnnouncementSummary struct {
	ID          string
	Title       string
	Summary     string
	PublishedAt time.Time
}

// Announcement is one published immutable revision with its bounded body.
type Announcement struct {
	AnnouncementSummary
	Body       string
	BodyFormat AnnouncementBodyFormat
	Version    int64
	UpdatedAt  time.Time
}

type AnnouncementPage struct {
	Items  []AnnouncementSummary
	Total  int
	Limit  int
	Offset int
}

// Category is the stable classification attached to a torrent.
type Category struct {
	ID   string
	Name string
}

// CategorySummary adds the number of currently public torrents to the stable
// category identity used by the catalog filter bar.
type CategorySummary struct {
	Category
	TorrentCount int
}

// FacetSelectionMode is owned by the catalog vocabulary. Upload clients can
// render the right control, while write-side repositories still resolve the
// canonical mode from PostgreSQL instead of trusting request data.
type FacetSelectionMode string

const (
	FacetSelectionSingle FacetSelectionMode = "single_option"
	FacetSelectionMulti  FacetSelectionMode = "multi_option"
)

type CategoryFacetOption struct {
	Key   string
	Label string
}

// CategoryFacet is the enabled, category-scoped upload vocabulary. It never
// exposes disabled global options that the selected category cannot accept.
type CategoryFacet struct {
	ID               string
	Name             string
	SelectionMode    FacetSelectionMode
	Required         bool
	RequirementGroup string
	Options          []CategoryFacetOption
}

// SwarmStats is an eventually consistent projection from the Tracker fault
// domain. It never represents the live peer set and must not be used for billing.
type SwarmStats struct {
	Seeders    int
	Leechers   int
	Completed  int
	ObservedAt time.Time
}

// SwarmConfidence describes only the availability and age of a complete
// asynchronous snapshot. It is intentionally not a quality score and must not
// be inferred from partial Tracker responses.
type SwarmConfidence string

const (
	SwarmConfidenceFresh       SwarmConfidence = "fresh"
	SwarmConfidenceStale       SwarmConfidence = "stale"
	SwarmConfidenceUnavailable SwarmConfidence = "unavailable"
)

// TorrentSwarmOverview is the public-safe activity read model for one
// published torrent. It carries aggregate counters only, never peer identity.
type TorrentSwarmOverview struct {
	TorrentID int64
	SwarmStats
	Stale      bool
	Confidence SwarmConfidence
}

// Torrent stores the content fields and latest swarm projection needed by the
// list use case. SizeBytes is always an exact byte count, never a formatted unit.
type Torrent struct {
	ID          int64
	Name        string
	Subtitle    string
	Category    Category
	SizeBytes   int64
	Promotion   Promotion
	StickyUntil *time.Time
	UploadedAt  time.Time
	Swarm       SwarmStats
}

// TorrentSummary is the use-case output. SwarmStale is derived at read time so
// clients can keep useful content visible without mistaking old counters as live.
type TorrentSummary struct {
	Torrent
	SwarmStale bool
}

// TorrentPage is a bounded list result and its pre-limit match count.
type TorrentPage struct {
	Items  []TorrentSummary
	Total  int
	Limit  int
	Offset int
}

// TorrentListRequest is the transport-independent input to the public catalog.
// Pointer pagination fields preserve the difference between omitted defaults
// and explicit invalid values at every adapter boundary.
type TorrentListRequest struct {
	Limit       *int
	Offset      *int
	Query       string
	SearchScope *TorrentSearchScope
	CategoryID  string
	Promotion   *Promotion
	Sort        *TorrentSort
}

// TorrentFilter contains only normalized values accepted by repositories.
type TorrentFilter struct {
	Limit       int
	Offset      int
	Query       string
	SearchScope TorrentSearchScope
	CategoryID  string
	Promotion   Promotion
	Sort        TorrentSort
}
