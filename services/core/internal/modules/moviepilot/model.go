// Package moviepilot owns the deliberately narrow compatibility projection for
// external Rousi clients. Personal API-key lifecycle and authentication live
// in personalapikey so MoviePilot, PT-depiler and future adapters share one
// secure credential instead of introducing tool-specific secrets.
package moviepilot

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/catalog"
	"github.com/peergo/peergo/services/core/internal/modules/economy/torrentpurchase"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

var (
	ErrCapabilityInvalid = errors.New("MoviePilot download capability is invalid")
	ErrInput             = errors.New("MoviePilot compatibility input is invalid")
	ErrNotFound          = errors.New("legacy API resource was not found")
	ErrRateLimited       = errors.New("MoviePilot compatibility request rate exceeded")
	ErrUnavailable       = errors.New("legacy API compatibility dependency is unavailable")
)

type Profile struct {
	NumericID     int64
	Username      string
	DisplayName   string
	Level         int
	RegisteredAt  time.Time
	LastActiveAt  *time.Time
	Uploaded      int64
	Downloaded    int64
	Magic         int64
	Experience    float64
	EmailVerified bool
	VIP           bool
	VIPUntil      *time.Time
	SeedingCount  int
	SeedingSize   int64
	LeechingCount int
	LeechingSize  int64
}

type TorrentPromotion struct {
	Type           int
	TimeType       int
	Active         bool
	Until          *time.Time
	UploadFactor   float64
	DownloadFactor float64
}

type TorrentSummary struct {
	ID            int64
	LegacyRouteID string
	Title         string
	Subtitle      string
	Category      string
	CategoryName  string
	Size          int64
	Seeders       int
	Leechers      int
	Downloads     int
	Uploader      string
	UploaderID    int64
	Anonymous     bool
	CreatedAt     time.Time
	Promotion     TorrentPromotion
}

type TorrentMetadata struct {
	TorrentID     int64
	LegacyRouteID string
	Uploader      string
	UploaderID    int64
	Anonymous     bool
	PurchasePrice int64
}

type LegacyCategoryAttribute struct {
	Name     string
	Label    string
	Type     string
	Required bool
	Options  []LegacyCategoryOption
}

type LegacyCategoryOption struct {
	Value string
	Label string
}

type LegacyCategory struct {
	ID         int64
	Name       string
	Label      string
	Icon       string
	Attributes []LegacyCategoryAttribute
}

type LegacyUploadInput struct {
	RequestID           uuid.UUID
	Category            string
	Title               string
	Subtitle            string
	Description         string
	MediaInfo           string
	Anonymous           bool
	PurchasePrice       int64
	Attributes          map[string][]string
	ExternalIdentifiers []torrents.ExternalIdentifier
	Screenshots         []torrents.TorrentScreenshotInput
	RawMetainfo         []byte
}

type LegacyUploadResult struct {
	RouteID  string
	InfoHash string
	Status   string
	ID       int64
}

type LegacyTorrentDetail struct {
	RouteID       string
	Detail        torrents.PublicDetail
	Content       torrents.PublicContent
	Swarm         catalog.TorrentSwarmOverview
	Metadata      TorrentMetadata
	Files         []torrents.PublicFile
	Related       []catalog.TorrentSummary
	Attributes    map[string]any
	ImageURLs     []string
	DownloadURL   string
	Purchase      torrentpurchase.Status
	CanReadObject bool
	Promotion     TorrentPromotion
}

type LegacyComment struct {
	ID        uuid.UUID
	Body      string
	UserID    int64
	Username  string
	CreatedAt time.Time
}

type LegacyCommentPage struct {
	Items      []LegacyComment
	Total      int64
	Page       int
	PageSize   int
	TotalPages int
}

type LegacyBookmarkPage struct {
	Items      []TorrentSummary
	Total      int
	Page       int
	PageSize   int
	TotalPages int
}

type TorrentPage struct {
	Items      []TorrentSummary
	Total      int
	Page       int
	PageSize   int
	TotalPages int
}

type TorrentDownloadDescriptor struct {
	Detail      torrents.PublicDetail
	Swarm       catalog.TorrentSwarmOverview
	DownloadURL string
	Promotion   TorrentPromotion
}
