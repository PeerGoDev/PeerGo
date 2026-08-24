package catalog

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

var (
	ErrSiteDisplaySettingsInput           = errors.New("site display settings input is invalid")
	ErrSiteDisplaySettingsNotFound        = errors.New("site display settings were not found")
	ErrSiteDisplaySettingsVersionConflict = errors.New("site display settings version changed")
)

const SiteDisplaySettingsSection = "site-display"

// SiteDisplaySettings is the complete, non-secret value object for one
// independently versioned settings section. Registration policy is excluded
// because identity/access owns that higher-risk decision.
type SiteDisplaySettings struct {
	Name                   string
	Description            string
	TorrentFilenamePrefix  string
	DefaultTorrentView     TorrentView
	ShowLatestAnnouncement bool
	CustomNavigationItems  []CustomNavigationItem
	Version                int64
	EffectiveAt            time.Time
	UpdatedAt              time.Time
}

type UpdateSiteDisplaySettingsInput struct {
	Name                   string
	Description            string
	TorrentFilenamePrefix  string
	DefaultTorrentView     TorrentView
	ShowLatestAnnouncement bool
	CustomNavigationItems  []CustomNavigationItem
	ExpectedVersion        int64
	Reason                 string
}

type UpdateSiteDisplaySettingsCommand struct {
	UpdateSiteDisplaySettingsInput
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

// SiteDisplaySettingsAuditState is hashed before leaving Core. Editable brand
// copy stays in the owning table and never becomes searchable audit payload.
type SiteDisplaySettingsAuditState struct {
	Name                   string                 `json:"name"`
	Description            string                 `json:"description"`
	TorrentFilenamePrefix  string                 `json:"torrent_filename_prefix"`
	DefaultTorrentView     TorrentView            `json:"default_torrent_view"`
	ShowLatestAnnouncement bool                   `json:"show_latest_announcement"`
	CustomNavigationItems  []CustomNavigationItem `json:"custom_navigation_items"`
	Version                int64                  `json:"version"`
}

type SiteDisplaySettingsAuditInput struct {
	OccurredAt      time.Time
	ActorID         uuid.UUID
	Reason          string
	ExpectedVersion int64
	Authorization   authz.Decision
	Before          SiteDisplaySettingsAuditState
	After           SiteDisplaySettingsAuditState
}

type SiteDisplaySettingsEventBuilder interface {
	BuildSiteDisplaySettingsEvent(SiteDisplaySettingsAuditInput) (auditevent.Event, error)
}
