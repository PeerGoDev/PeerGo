// Package moviepilot owns the deliberately narrow compatibility boundary for
// the official MoviePilot Rousi adapter. Personal API-key lifecycle and
// authentication live in personalapikey so future adapters share one secure
// credential instead of introducing tool-specific secrets.
package moviepilot

import (
	"errors"
	"time"

	"github.com/peergo/peergo/services/core/internal/modules/catalog"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

var (
	ErrCapabilityInvalid = errors.New("MoviePilot download capability is invalid")
	ErrInput             = errors.New("MoviePilot compatibility input is invalid")
	ErrRateLimited       = errors.New("MoviePilot compatibility request rate exceeded")
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
	CreatedAt     time.Time
	Promotion     TorrentPromotion
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
