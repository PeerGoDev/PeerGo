// Package review owns staff review queues and immutable decisions. It may ask
// the torrents aggregate to transition, but it never edits metainfo evidence or
// reaches into the Tracker data plane.
package review

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/contracts/trackerevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

var (
	ErrTorrentReviewInput               = errors.New("torrent review input is invalid")
	ErrTorrentReviewNotFound            = errors.New("torrent review target was not found")
	ErrTorrentReviewVersionConflict     = errors.New("torrent review target version changed")
	ErrTorrentReviewStateConflict       = errors.New("torrent is not pending review")
	ErrTorrentReviewIdempotencyConflict = errors.New("torrent review idempotency key was reused")
	ErrTorrentReviewSelf                = errors.New("reviewers cannot decide their own torrent")
	ErrTorrentReviewMembership          = errors.New("an active review workgroup membership is required")
	ErrTorrentReviewAlreadyVoted        = errors.New("reviewer already voted in this round")
	ErrTorrentReviewRoundEscalated      = errors.New("torrent review round requires staff resolution")
	ErrTorrentReviewCategoryUnavailable = errors.New("torrent category is unavailable")
	ErrTorrentReviewObjectUnavailable   = errors.New("torrent object has no verified location")
)

type Decision string

const (
	DecisionApprove Decision = "approve"
	DecisionReject  Decision = "reject"
)

type ReasonCode string

const (
	ReasonMeetsRequirements         ReasonCode = "meets_requirements"
	ReasonMetadataIncomplete        ReasonCode = "metadata_incomplete"
	ReasonDuplicateOrSuperseded     ReasonCode = "duplicate_or_superseded"
	ReasonContentPolicyViolation    ReasonCode = "content_policy_violation"
	ReasonQualityRequirementsNotMet ReasonCode = "quality_requirements_not_met"
	ReasonUploaderActionRequired    ReasonCode = "uploader_action_required"
	ReasonOther                     ReasonCode = "other"
)

// PendingTorrent is a bounded staff projection. It contains the uploader name
// needed for review context, but no email, passkey, IP, traffic or credentials.
type PendingTorrent struct {
	ID                  torrents.TorrentID
	UploaderID          uuid.UUID
	UploaderDisplayName string
	CategoryID          string
	CategoryName        string
	Title               string
	Subtitle            string
	ContentName         string
	InfoHashV1          torrents.InfoHashV1
	TotalSizeBytes      int64
	FileCount           int
	Version             int64
	SubmittedAt         time.Time
	ReviewRequestedAt   time.Time
}

type PendingTorrentPage struct {
	Items []PendingTorrent
	Total int64
}

const (
	RequiredReviewVotes = 3
	MaximumReviewVotes  = 4
)

type RoundOutcome string

const (
	RoundWaiting   RoundOutcome = "waiting"
	RoundPublished RoundOutcome = "published"
	RoundRejected  RoundOutcome = "rejected"
	RoundEscalated RoundOutcome = "escalated"
)

// ReviewAssignment deliberately exposes only aggregate progress before the
// member votes. Approve/reject counts remain hidden to prevent follow-the-crowd
// voting; staff can inspect the immutable votes when resolving an escalation.
type ReviewAssignment struct {
	PendingTorrent
	VotesCast     int
	RequiredVotes int
	MaximumVotes  int
}

type ReviewAssignmentPage struct {
	Items []ReviewAssignment
	Total int64
}

type DecideInput struct {
	DecisionID      uuid.UUID
	TorrentID       torrents.TorrentID
	ExpectedVersion int64
	Decision        Decision
	ReasonCode      ReasonCode
	Reason          string
}

type DecideCommand struct {
	DecideInput
	ReviewerID             uuid.UUID
	OccurredAt             time.Time
	Authorization          authz.Decision
	Resolution             string
	ReviewRoundID          *uuid.UUID
	MembershipTransitionID *uuid.UUID
}

type VoteInput struct {
	VoteID          uuid.UUID
	TorrentID       torrents.TorrentID
	ExpectedVersion int64
	Decision        Decision
	ReasonCode      ReasonCode
	Reason          string
}

type VoteCommand struct {
	VoteInput
	VoterID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

// VoteResult is stable across exact Idempotency-Key replays. The counters and
// outcome are the state immediately after this vote, not today's round state.
type VoteResult struct {
	VoteID        uuid.UUID
	RoundID       uuid.UUID
	TorrentID     torrents.TorrentID
	Decision      Decision
	VotesCast     int
	RequiredVotes int
	MaximumVotes  int
	Outcome       RoundOutcome
	FinalDecision *DecisionResult
	VotedAt       time.Time
}

// DecisionResult is the durable outcome returned on both first execution and
// an exact idempotent replay. Published means the Core state is committed; the
// Tracker consumes the corresponding control event asynchronously.
type DecisionResult struct {
	DecisionID uuid.UUID
	TorrentID  torrents.TorrentID
	Decision   Decision
	ReasonCode ReasonCode
	State      torrents.State
	Version    int64
	OccurredAt time.Time
}

type TorrentReviewAuditState struct {
	TorrentID torrents.TorrentID `json:"torrent_id"`
	State     torrents.State     `json:"state"`
	Version   int64              `json:"version"`
}

type TorrentReviewAuditInput struct {
	DecisionID    uuid.UUID
	ReviewerID    uuid.UUID
	UploaderID    uuid.UUID
	Decision      Decision
	ReasonCode    ReasonCode
	Reason        string
	OccurredAt    time.Time
	Authorization authz.Decision
	Before        TorrentReviewAuditState
	After         TorrentReviewAuditState
}

// Event builders are injected so the review transaction can atomically append
// audit and Tracker-control evidence without depending on either adapter.
type AuditEventBuilder interface {
	BuildTorrentReviewEvent(TorrentReviewAuditInput) (auditevent.Event, error)
}

type EligibilityEventBuilder interface {
	BuildTorrentEligibilityEvent(DecisionResult, PendingTorrent) (trackerevent.Event, error)
}

// NotificationAppender is the transaction-local outbound port for the one
// user-visible consequence of a review decision. The notification adapter
// derives recipient and target from the immutable decision instead of trusting
// duplicate caller-supplied identity fields.
type NotificationAppender interface {
	AppendTorrentReviewNotification(context.Context, uuid.UUID) error
}
