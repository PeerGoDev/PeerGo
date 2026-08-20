package torrents

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	DefaultPublishedContentChangeLimit = 20
	MaxPublishedContentChangeLimit     = 50
)

var (
	ErrPublishedContentChangeInput               = errors.New("published torrent content change input is invalid")
	ErrPublishedContentChangeNotFound            = errors.New("published torrent content change was not found")
	ErrPublishedContentChangeEmailUnverified     = errors.New("published torrent content change requires a verified email")
	ErrPublishedContentChangeSelfReview          = errors.New("published torrent content change cannot be self-reviewed")
	ErrPublishedContentChangeStateConflict       = errors.New("torrent cannot accept a published content change")
	ErrPublishedContentChangeVersionConflict     = errors.New("published torrent content version changed")
	ErrPublishedContentChangePending             = errors.New("torrent already has a pending content change")
	ErrPublishedContentChangeUnchanged           = errors.New("published torrent content is unchanged")
	ErrPublishedContentChangeIdempotencyConflict = errors.New("published torrent content change request was reused")
)

type PublishedContentChangeStatus string

const (
	PublishedContentChangePending  PublishedContentChangeStatus = "pending"
	PublishedContentChangeApproved PublishedContentChangeStatus = "approved"
	PublishedContentChangeRejected PublishedContentChangeStatus = "rejected"
)

type PublishedContentChangeDecision string

const (
	PublishedContentChangeApprove PublishedContentChangeDecision = "approve"
	PublishedContentChangeReject  PublishedContentChangeDecision = "reject"
)

// PublishedContentSnapshot contains only public editorial content. Screenshot
// objects use a separate immutable attachment-set workflow and are deliberately
// not represented as URLs or mutable identifiers here.
type PublishedContentSnapshot struct {
	Description         string
	MediaInfo           string
	ExternalIdentifiers []ExternalIdentifier
}

type SubmitPublishedContentChangeInput struct {
	RequestID           uuid.UUID
	TorrentID           TorrentID
	ExpectedVersion     int64
	Description         string
	MediaInfo           string
	ExternalIdentifiers []ExternalIdentifier
	Reason              string
}

type SubmitPublishedContentChangeCommand struct {
	SubmitPublishedContentChangeInput
	UploaderID    uuid.UUID
	Candidate     PublishedContentSnapshot
	OccurredAt    time.Time
	Authorization authz.Decision
}

type PublishedContentChange struct {
	ID                 uuid.UUID
	TorrentID          TorrentID
	BaseTorrentVersion int64
	Base               PublishedContentSnapshot
	Candidate          PublishedContentSnapshot
	Reason             string
	Status             PublishedContentChangeStatus
	Version            int64
	CreatedAt          time.Time
	DecidedAt          *time.Time
}

type PublishedContentChangeQuery struct {
	Status PublishedContentChangeStatus
	Limit  int
	Offset int
}

type ManagedPublishedContentChange struct {
	PublishedContentChange
	UploaderNumericID   int64
	UploaderUsername    string
	UploaderDisplayName string
	TorrentTitle        string
}

type ManagedPublishedContentChangePage struct {
	Items  []ManagedPublishedContentChange
	Total  int64
	Limit  int
	Offset int
}

type DecidePublishedContentChangeInput struct {
	DecisionID             uuid.UUID
	RequestID              uuid.UUID
	ExpectedRequestVersion int64
	Decision               PublishedContentChangeDecision
	Reason                 string
}

type DecidePublishedContentChangeCommand struct {
	DecidePublishedContentChangeInput
	ReviewerID    uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type PublishedContentChangeDecisionResult struct {
	DecisionID     uuid.UUID
	RequestID      uuid.UUID
	TorrentID      TorrentID
	Decision       PublishedContentChangeDecision
	RequestStatus  PublishedContentChangeStatus
	RequestVersion int64
	TorrentVersion int64
	DecidedAt      time.Time
}

func (service *TorrentMaintenanceService) SubmitPublishedContentChange(ctx context.Context, cookieToken, csrfToken string, input SubmitPublishedContentChangeInput) (PublishedContentChange, error) {
	normalized, err := normalizeSubmitPublishedContentChangeInput(input)
	if err != nil {
		return PublishedContentChange{}, err
	}
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return PublishedContentChange{}, err
	}
	if session.User.EmailVerifiedAt == nil {
		return PublishedContentChange{}, ErrPublishedContentChangeEmailUnverified
	}
	now := service.now().UTC().Round(0)
	decision, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionTorrentContentChangeSubmitSelf, now)
	if err != nil {
		return PublishedContentChange{}, err
	}
	return service.repository.SubmitPublishedContentChange(ctx, SubmitPublishedContentChangeCommand{
		SubmitPublishedContentChangeInput: normalized,
		UploaderID:                        session.User.ID,
		Candidate: PublishedContentSnapshot{
			Description: normalized.Description, MediaInfo: normalized.MediaInfo,
			ExternalIdentifiers: normalized.ExternalIdentifiers,
		},
		OccurredAt: now, Authorization: decision,
	})
}

func (service *TorrentMaintenanceService) ListPublishedContentChanges(ctx context.Context, actor authz.StaffActor, query PublishedContentChangeQuery) (ManagedPublishedContentChangePage, error) {
	normalized, err := normalizePublishedContentChangeQuery(query)
	if err != nil {
		return ManagedPublishedContentChangePage{}, err
	}
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTorrentContentChangeReview, authz.SiteScope(), service.now().UTC(), "torrent-content-change"); err != nil {
		return ManagedPublishedContentChangePage{}, err
	}
	return service.repository.ListPublishedContentChanges(ctx, normalized)
}

func (service *TorrentMaintenanceService) DecidePublishedContentChange(ctx context.Context, actor authz.StaffActor, input DecidePublishedContentChangeInput) (PublishedContentChangeDecisionResult, error) {
	normalized, err := normalizeDecidePublishedContentChangeInput(input)
	if err != nil {
		return PublishedContentChangeDecisionResult{}, err
	}
	now := service.now().UTC().Round(0)
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTorrentContentChangeReview, authz.SiteScope(), now, "torrent-content-change")
	if err != nil {
		return PublishedContentChangeDecisionResult{}, err
	}
	return service.repository.DecidePublishedContentChange(ctx, DecidePublishedContentChangeCommand{
		DecidePublishedContentChangeInput: normalized,
		ReviewerID:                        actor.Subject.ID, OccurredAt: now, Authorization: decision,
	})
}

func normalizeSubmitPublishedContentChangeInput(input SubmitPublishedContentChangeInput) (SubmitPublishedContentChangeInput, error) {
	input.Description = strings.TrimSpace(input.Description)
	input.MediaInfo = strings.TrimSpace(input.MediaInfo)
	input.Reason = strings.TrimSpace(input.Reason)
	identifiers, err := normalizeExternalIdentifiers(input.ExternalIdentifiers)
	reasonRunes := utf8.RuneCountInString(input.Reason)
	if err != nil || input.RequestID == uuid.Nil || input.TorrentID < 1 || input.ExpectedVersion < 1 ||
		input.Description == "" || !validTorrentContent(input.Description, input.MediaInfo) ||
		!utf8.ValidString(input.Reason) || reasonRunes < 10 || reasonRunes > 1000 {
		return SubmitPublishedContentChangeInput{}, ErrPublishedContentChangeInput
	}
	input.ExternalIdentifiers = identifiers
	return input, nil
}

func normalizePublishedContentChangeQuery(query PublishedContentChangeQuery) (PublishedContentChangeQuery, error) {
	if query.Limit < 1 || query.Limit > MaxPublishedContentChangeLimit || query.Offset < 0 || query.Offset > MaxManagedTorrentOffset {
		return PublishedContentChangeQuery{}, ErrPublishedContentChangeInput
	}
	switch query.Status {
	case "", PublishedContentChangePending, PublishedContentChangeApproved, PublishedContentChangeRejected:
		return query, nil
	default:
		return PublishedContentChangeQuery{}, ErrPublishedContentChangeInput
	}
}

func normalizeDecidePublishedContentChangeInput(input DecidePublishedContentChangeInput) (DecidePublishedContentChangeInput, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	reasonRunes := utf8.RuneCountInString(input.Reason)
	if input.DecisionID == uuid.Nil || input.RequestID == uuid.Nil || input.ExpectedRequestVersion != 1 ||
		!utf8.ValidString(input.Reason) || reasonRunes < 10 || reasonRunes > 1000 ||
		(input.Decision != PublishedContentChangeApprove && input.Decision != PublishedContentChangeReject) {
		return DecidePublishedContentChangeInput{}, ErrPublishedContentChangeInput
	}
	return input, nil
}

func publishedContentSnapshotDigest(snapshot PublishedContentSnapshot) ([32]byte, error) {
	identifiers, err := normalizeExternalIdentifiers(snapshot.ExternalIdentifiers)
	if err != nil || !validTorrentContent(snapshot.Description, snapshot.MediaInfo) {
		return [32]byte{}, ErrPublishedContentChangeInput
	}
	payload := struct {
		Description         string               `json:"description"`
		MediaInfo           string               `json:"media_info"`
		ExternalIdentifiers []ExternalIdentifier `json:"external_identifiers"`
	}{snapshot.Description, snapshot.MediaInfo, identifiers}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return [32]byte{}, fmt.Errorf("encode published content snapshot: %w", err)
	}
	return sha256.Sum256(encoded), nil
}
