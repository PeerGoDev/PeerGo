// Package wiki owns the editable site knowledge base. It stores one current
// projection plus a bounded recovery window; page views and search activity
// are deliberately outside this module.
package wiki

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultPageLimit    = 50
	MaximumPageLimit    = 100
	MaximumPageOffset   = 100_000
	MaximumEditors      = 20
	MaximumRevisions    = 50
	MaximumSlugRunes    = 96
	MaximumTitleRunes   = 160
	MaximumSummaryRunes = 500
	MaximumBodyRunes    = 100_000
	MaximumQueryRunes   = 100
	MaximumReasonRunes  = 500
)

var (
	ErrInput            = errors.New("wiki input is invalid")
	ErrPageNotFound     = errors.New("wiki page was not found")
	ErrRevisionNotFound = errors.New("wiki revision was not found")
	ErrPageExists       = errors.New("wiki page slug already exists")
	ErrVersionConflict  = errors.New("wiki page version changed")
	ErrEditDenied       = errors.New("wiki page edit is denied")
	ErrNoChanges        = errors.New("wiki page has no changes")
	ErrEditorNotFound   = errors.New("wiki editor was not found")
	ErrInvariant        = errors.New("wiki persisted state is invalid")
)

type Visibility string

const (
	VisibilityPublic  Visibility = "public"
	VisibilityMembers Visibility = "members"
)

type RevisionOrigin string

const (
	RevisionOriginMigration RevisionOrigin = "migration"
	RevisionOriginMember    RevisionOrigin = "member"
	RevisionOriginStaff     RevisionOrigin = "staff"
	RevisionOriginRestore   RevisionOrigin = "restore"
)

type UserReference struct {
	ID          uuid.UUID
	NumericID   int64
	Username    string
	DisplayName string
}

type Page struct {
	ID             uuid.UUID
	Slug           string
	Title          string
	Summary        string
	Body           string
	Visibility     Visibility
	SortOrder      int
	Version        int64
	RevisionNumber int64
	Creator        UserReference
	Updater        UserReference
	Editors        []UserReference
	CanEdit        bool
	Migrated       bool
	LegacyWikiID   *int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ArchivedAt     *time.Time
}

type PageSummary struct {
	ID             uuid.UUID
	Slug           string
	Title          string
	Summary        string
	Visibility     Visibility
	SortOrder      int
	Version        int64
	RevisionNumber int64
	CanEdit        bool
	Archived       bool
	UpdatedAt      time.Time
}

type PageList struct {
	Items  []PageSummary
	Total  int
	Limit  int
	Offset int
}

type ListInput struct {
	Query           string
	Limit           int
	Offset          int
	IncludeArchived bool
}

type UpdateAssignedInput struct {
	PageID          uuid.UUID
	Title           string
	Summary         string
	Body            string
	ExpectedVersion int64
	Reason          string
}

type CreateManagedInput struct {
	Slug             string
	Title            string
	Summary          string
	Body             string
	Visibility       Visibility
	SortOrder        int
	EditorNumericIDs []int64
	Reason           string
}

type UpdateManagedInput struct {
	PageID           uuid.UUID
	Slug             string
	Title            string
	Summary          string
	Body             string
	Visibility       Visibility
	SortOrder        int
	Archived         bool
	EditorNumericIDs []int64
	ExpectedVersion  int64
	Reason           string
}

type RestoreManagedInput struct {
	PageID          uuid.UUID
	RevisionNumber  int64
	ExpectedVersion int64
	Reason          string
}

type RevisionSummary struct {
	RevisionNumber int64
	Title          string
	Reason         string
	Origin         RevisionOrigin
	Editor         UserReference
	CreatedAt      time.Time
}

type RevisionPage struct {
	Items  []RevisionSummary
	Total  int
	Limit  int
	Offset int
}

type createCommand struct {
	CreateManagedInput
	PageID    uuid.UUID
	ActorID   uuid.UUID
	CreatedAt time.Time
}

type updateManagedCommand struct {
	UpdateManagedInput
	ActorID   uuid.UUID
	UpdatedAt time.Time
}

type updateAssignedCommand struct {
	UpdateAssignedInput
	ActorID   uuid.UUID
	UpdatedAt time.Time
}

type restoreCommand struct {
	RestoreManagedInput
	ActorID    uuid.UUID
	RestoredAt time.Time
}
