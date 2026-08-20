package catalog

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

var (
	ErrAnnouncementAdministrationInput = errors.New("announcement administration input is invalid")
	ErrAnnouncementAlreadyExists       = errors.New("announcement already exists")
	ErrManagedAnnouncementNotFound     = errors.New("managed announcement was not found")
	ErrAnnouncementVersionConflict     = errors.New("announcement version changed")
	ErrAnnouncementPublicationConflict = errors.New("announcement publication state conflicts with the command")
	ErrAnnouncementNoChanges           = errors.New("announcement draft has no content changes")
)

type ManagedAnnouncementStatus string

const (
	ManagedAnnouncementDraft     ManagedAnnouncementStatus = "draft"
	ManagedAnnouncementScheduled ManagedAnnouncementStatus = "scheduled"
	ManagedAnnouncementPublished ManagedAnnouncementStatus = "published"
	ManagedAnnouncementWithdrawn ManagedAnnouncementStatus = "withdrawn"
)

type AnnouncementRevisionOrigin string

const (
	AnnouncementRevisionMigration       AnnouncementRevisionOrigin = "migration"
	AnnouncementRevisionDevelopmentSeed AnnouncementRevisionOrigin = "development_seed"
	AnnouncementRevisionStaff           AnnouncementRevisionOrigin = "staff"
)

// ManagedAnnouncement is the staff editing projection. Content always comes
// from one immutable revision: draft first, then scheduled, then published.
// Aggregate Version is deliberately separate from RevisionNumber because a
// publication command changes state without rewriting reviewed content.
type ManagedAnnouncement struct {
	ID                    string
	Title                 string
	Summary               string
	Body                  string
	BodyFormat            AnnouncementBodyFormat
	Status                ManagedAnnouncementStatus
	Version               int64
	RevisionNumber        int64
	HasUnpublishedChanges bool
	PublishedAt           *time.Time
	ScheduledFor          *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type ManagedAnnouncementPage struct {
	Items  []ManagedAnnouncement
	Total  int
	Limit  int
	Offset int
}

type AnnouncementRevisionSummary struct {
	RevisionNumber    int64
	Title             string
	Summary           string
	BodyFormat        AnnouncementBodyFormat
	Origin            AnnouncementRevisionOrigin
	EditorDisplayName string
	IsDraft           bool
	IsPublished       bool
	IsScheduled       bool
	CreatedAt         time.Time
}

type AnnouncementRevisionPage struct {
	Items  []AnnouncementRevisionSummary
	Total  int
	Limit  int
	Offset int
}

type CreateAnnouncementDraftInput struct {
	ID         string
	Title      string
	Summary    string
	Body       string
	BodyFormat AnnouncementBodyFormat
	Reason     string
}

type UpdateAnnouncementDraftInput struct {
	ID              string
	Title           string
	Summary         string
	Body            string
	BodyFormat      AnnouncementBodyFormat
	ExpectedVersion int64
	Reason          string
}

type AnnouncementPublicationAction string

const (
	AnnouncementPublishNow     AnnouncementPublicationAction = "publish_now"
	AnnouncementSchedule       AnnouncementPublicationAction = "schedule"
	AnnouncementCancelSchedule AnnouncementPublicationAction = "cancel_schedule"
	AnnouncementWithdraw       AnnouncementPublicationAction = "withdraw"
)

type ChangeAnnouncementPublicationInput struct {
	ID              string
	Action          AnnouncementPublicationAction
	ExpectedVersion int64
	ScheduledFor    *time.Time
	Reason          string
}

type CreateAnnouncementDraftCommand struct {
	CreateAnnouncementDraftInput
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type UpdateAnnouncementDraftCommand struct {
	UpdateAnnouncementDraftInput
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type ChangeAnnouncementPublicationCommand struct {
	ChangeAnnouncementPublicationInput
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type AnnouncementTransition string

const (
	AnnouncementTransitionDraftCreated     AnnouncementTransition = "draft_created"
	AnnouncementTransitionDraftUpdated     AnnouncementTransition = "draft_updated"
	AnnouncementTransitionPublished        AnnouncementTransition = "published"
	AnnouncementTransitionScheduled        AnnouncementTransition = "scheduled"
	AnnouncementTransitionScheduleCanceled AnnouncementTransition = "schedule_canceled"
	AnnouncementTransitionWithdrawn        AnnouncementTransition = "withdrawn"
)

// AnnouncementAuditState is hashed before leaving Core. The public audit event
// contains no editable title, summary, body, staff display name, or raw reason.
type AnnouncementAuditState struct {
	ID                    string                    `json:"id"`
	Title                 string                    `json:"title"`
	Summary               string                    `json:"summary"`
	Body                  string                    `json:"body"`
	BodyFormat            AnnouncementBodyFormat    `json:"body_format"`
	Status                ManagedAnnouncementStatus `json:"status"`
	Version               int64                     `json:"version"`
	RevisionNumber        int64                     `json:"revision_number"`
	HasUnpublishedChanges bool                      `json:"has_unpublished_changes"`
	PublishedAt           *time.Time                `json:"published_at,omitempty"`
	ScheduledFor          *time.Time                `json:"scheduled_for,omitempty"`
}

type AnnouncementAuditInput struct {
	Transition      AnnouncementTransition
	OccurredAt      time.Time
	ActorID         uuid.UUID
	AnnouncementID  string
	Reason          string
	ExpectedVersion int64
	Authorization   authz.Decision
	Before          *AnnouncementAuditState
	After           AnnouncementAuditState
}

type AnnouncementEventBuilder interface {
	BuildAnnouncementEvent(AnnouncementAuditInput) (auditevent.Event, error)
}
