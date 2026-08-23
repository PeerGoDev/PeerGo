package catalog

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

var (
	ErrCategoryAdministrationInput   = errors.New("category administration input is invalid")
	ErrCategoryAlreadyExists         = errors.New("category already exists")
	ErrCategoryNotFound              = errors.New("category was not found")
	ErrCategoryVersionConflict       = errors.New("category version changed")
	ErrCategoryFacetNotFound         = errors.New("category facet was not found")
	ErrCategoryOptionAlreadyExists   = errors.New("category facet option already exists")
	ErrCategoryOptionNotFound        = errors.New("category facet option was not found")
	ErrCategoryOptionUnavailable     = errors.New("canonical category facet option is unavailable")
	ErrCategoryOptionVersionConflict = errors.New("category facet option version changed")
)

type CategoryTransition string

const (
	CategoryTransitionCreated CategoryTransition = "created"
	CategoryTransitionUpdated CategoryTransition = "updated"
)

// ManagedCategory is the bounded staff projection. TorrentCount communicates
// the impact of disabling a category without exposing torrent metadata.
type ManagedCategory struct {
	ID           string
	Name         string
	DisplayOrder int
	Enabled      bool
	Version      int64
	TorrentCount int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Facets       []ManagedCategoryFacet
}

// ManagedCategoryFacet and ManagedCategoryFacetOption expose the complete
// category-scoped upload vocabulary to staff, including options hidden from
// new uploads but still referenced by historical torrents.
type ManagedCategoryFacet struct {
	ID               string
	Name             string
	SelectionMode    FacetSelectionMode
	Required         bool
	RequirementGroup string
	DisplayOrder     int
	Options          []ManagedCategoryFacetOption
}

type ManagedCategoryFacetOption struct {
	Key            string
	Label          string
	CanonicalLabel string
	DisplayOrder   int
	Enabled        bool
	Version        int64
	TorrentCount   int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type UpsertCategoryFacetOptionInput struct {
	CategoryID      string
	FacetID         string
	OptionKey       string
	Label           string
	DisplayOrder    int
	Enabled         bool
	ExpectedVersion int64
	Reason          string
}

type UpsertCategoryFacetOptionCommand struct {
	UpsertCategoryFacetOptionInput
	ChangeID      uuid.UUID
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type CategoryFacetOptionAuditState struct {
	CategoryID   string `json:"category_id"`
	FacetID      string `json:"facet_id"`
	OptionKey    string `json:"option_key"`
	Label        string `json:"label"`
	DisplayOrder int    `json:"display_order"`
	Enabled      bool   `json:"enabled"`
	Version      int64  `json:"version"`
}

type CreateCategoryInput struct {
	ID           string
	Name         string
	DisplayOrder int
	Enabled      bool
	Reason       string
}

type UpdateCategoryInput struct {
	ID              string
	Name            string
	DisplayOrder    int
	Enabled         bool
	ExpectedVersion int64
	Reason          string
}

type CreateCategoryCommand struct {
	ID            string
	Name          string
	DisplayOrder  int
	Enabled       bool
	Reason        string
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type UpdateCategoryCommand struct {
	ID              string
	Name            string
	DisplayOrder    int
	Enabled         bool
	ExpectedVersion int64
	Reason          string
	ActorID         uuid.UUID
	OccurredAt      time.Time
	Authorization   authz.Decision
}

// CategoryAuditState is the canonical security-relevant projection hashed by
// the audit builder. It deliberately excludes timestamps and torrent counts so
// retry timing and unrelated content writes cannot change the evidence hash.
type CategoryAuditState struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	DisplayOrder int    `json:"display_order"`
	Enabled      bool   `json:"enabled"`
	Version      int64  `json:"version"`
}

type CategoryAuditInput struct {
	Transition      CategoryTransition
	OccurredAt      time.Time
	ActorID         uuid.UUID
	CategoryID      string
	Reason          string
	ExpectedVersion int64
	Authorization   authz.Decision
	Before          *CategoryAuditState
	After           CategoryAuditState
}

// CategoryEventBuilder is implemented by audit. The catalog transaction owns
// ordering, while pseudonym keys and the reviewed JSON contract stay outside
// the catalog module.
type CategoryEventBuilder interface {
	BuildCategoryEvent(CategoryAuditInput) (auditevent.Event, error)
}
