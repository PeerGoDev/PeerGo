package review

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

const (
	minCorrectionNoteRunes = 10
	maxCorrectionNoteRunes = 1000
)

var (
	ErrTorrentResubmissionInput               = errors.New("torrent resubmission input is invalid")
	ErrTorrentResubmissionNotFound            = errors.New("torrent resubmission target was not found")
	ErrTorrentResubmissionVersionConflict     = errors.New("torrent resubmission target version changed")
	ErrTorrentResubmissionStateConflict       = errors.New("torrent is not rejected")
	ErrTorrentResubmissionNotAllowed          = errors.New("torrent rejection does not allow metadata resubmission")
	ErrTorrentResubmissionUnchanged           = errors.New("torrent resubmission metadata is unchanged")
	ErrTorrentResubmissionCategoryUnavailable = errors.New("torrent resubmission category is unavailable")
	ErrTorrentResubmissionIdempotencyConflict = errors.New("torrent resubmission idempotency key was reused")
	ErrTorrentResubmissionEmailUnverified     = errors.New("torrent resubmission requires a verified email")
	ErrTorrentResubmissionInvariant           = errors.New("torrent resubmission projection violates persisted invariants")
)

// ResubmitInput contains only uploader-editable metadata. A replacement
// .torrent object is intentionally impossible to express through this command.
type ResubmitInput struct {
	ID              uuid.UUID
	TorrentID       torrents.TorrentID
	ExpectedVersion int64
	CategoryID      string
	Title           string
	Subtitle        string
	CorrectionNote  string
}

type ResubmitCommand struct {
	ID              uuid.UUID
	TorrentID       torrents.TorrentID
	ExpectedVersion int64
	UploaderID      uuid.UUID
	Metadata        torrents.EditableMetadata
	CorrectionNote  string
	OccurredAt      time.Time
	Authorization   authz.Decision
}

// ResubmissionResult is returned for both the initial transaction and an exact
// replay after response loss. ReviewRequestedAt is the queue time, while the
// original torrent SubmittedAt remains unchanged.
type ResubmissionResult struct {
	ID                uuid.UUID
	TorrentID         torrents.TorrentID
	State             torrents.State
	Version           int64
	Metadata          torrents.EditableMetadata
	ReviewRequestedAt time.Time
}

type ResubmissionRepository interface {
	Resubmit(context.Context, ResubmitCommand) (ResubmissionResult, error)
}

type ResubmissionSessionAuthenticator interface {
	AuthenticateWrite(context.Context, string, string) (identity.WebSession, error)
}

type ResubmissionService struct {
	authenticator ResubmissionSessionAuthenticator
	authorizer    authz.Authorizer
	repository    ResubmissionRepository
	now           func() time.Time
}

func NewResubmissionService(
	authenticator ResubmissionSessionAuthenticator,
	authorizer authz.Authorizer,
	repository ResubmissionRepository,
	now func() time.Time,
) (*ResubmissionService, error) {
	if authenticator == nil || authorizer == nil || repository == nil {
		return nil, errors.New("torrent resubmission dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &ResubmissionService{
		authenticator: authenticator,
		authorizer:    authorizer,
		repository:    repository,
		now:           now,
	}, nil
}

func (service *ResubmissionService) Resubmit(
	ctx context.Context,
	cookieToken string,
	csrfToken string,
	input ResubmitInput,
) (ResubmissionResult, error) {
	metadata, note, err := normalizeResubmissionInput(input)
	if err != nil {
		return ResubmissionResult{}, err
	}
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return ResubmissionResult{}, err
	}
	if session.User.EmailVerifiedAt == nil {
		return ResubmissionResult{}, ErrTorrentResubmissionEmailUnverified
	}
	now := service.now().UTC()
	decision, err := authz.AuthorizeWebSelfAction(
		ctx,
		service.authorizer,
		session.User.ID,
		authz.ActionTorrentSubmissionResubmitSelf,
		now,
	)
	if err != nil {
		return ResubmissionResult{}, err
	}
	result, err := service.repository.Resubmit(ctx, ResubmitCommand{
		ID: input.ID, TorrentID: input.TorrentID,
		ExpectedVersion: input.ExpectedVersion, UploaderID: session.User.ID,
		Metadata: metadata, CorrectionNote: note, OccurredAt: now,
		Authorization: decision,
	})
	if err != nil {
		return ResubmissionResult{}, fmt.Errorf("resubmit rejected torrent: %w", err)
	}
	return result, nil
}

// MetadataResubmissionAllowed is the one product rule shared by repository and
// HTTP projection. Other rejection reasons may require a new immutable torrent
// or a future appeal flow and therefore must not expose the correction form.
func MetadataResubmissionAllowed(state torrents.State, reasonCode ReasonCode) bool {
	return state == torrents.StateRejected && reasonCode == ReasonMetadataIncomplete
}

func normalizeResubmissionInput(input ResubmitInput) (torrents.EditableMetadata, string, error) {
	if input.ID == uuid.Nil || input.TorrentID < 1 || input.ExpectedVersion < 1 {
		return torrents.EditableMetadata{}, "", ErrTorrentResubmissionInput
	}
	metadata, err := torrents.NewEditableMetadata(input.CategoryID, input.Title, input.Subtitle)
	if err != nil {
		return torrents.EditableMetadata{}, "", ErrTorrentResubmissionInput
	}
	note := strings.TrimSpace(input.CorrectionNote)
	noteRunes := utf8.RuneCountInString(note)
	if !utf8.ValidString(note) || noteRunes < minCorrectionNoteRunes || noteRunes > maxCorrectionNoteRunes {
		return torrents.EditableMetadata{}, "", ErrTorrentResubmissionInput
	}
	return metadata, note, nil
}
