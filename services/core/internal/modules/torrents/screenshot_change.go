package torrents

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	DefaultPublishedScreenshotChangeLimit = 20
	MaxPublishedScreenshotChangeLimit     = 50
)

var (
	ErrPublishedScreenshotChangeInput               = errors.New("published torrent screenshot change input is invalid")
	ErrPublishedScreenshotChangeNotFound            = errors.New("published torrent screenshot change was not found")
	ErrPublishedScreenshotChangeEmailUnverified     = errors.New("published torrent screenshot change requires a verified email")
	ErrPublishedScreenshotChangeSelfReview          = errors.New("published torrent screenshot change cannot be self-reviewed")
	ErrPublishedScreenshotChangeStateConflict       = errors.New("torrent cannot accept a published screenshot change")
	ErrPublishedScreenshotChangeVersionConflict     = errors.New("published torrent screenshot set changed")
	ErrPublishedScreenshotChangePending             = errors.New("torrent already has a pending screenshot change")
	ErrPublishedScreenshotChangeUnchanged           = errors.New("published torrent screenshots are unchanged")
	ErrPublishedScreenshotChangeIdempotencyConflict = errors.New("published torrent screenshot request was reused")
	ErrPublishedScreenshotChangeUnavailable         = errors.New("published torrent screenshot change storage is unavailable")
)

type PublishedScreenshotChangeStatus string

const (
	PublishedScreenshotChangePending  PublishedScreenshotChangeStatus = "pending"
	PublishedScreenshotChangeApproved PublishedScreenshotChangeStatus = "approved"
	PublishedScreenshotChangeRejected PublishedScreenshotChangeStatus = "rejected"
)

type PublishedScreenshotChangeDecision string

const (
	PublishedScreenshotChangeApprove PublishedScreenshotChangeDecision = "approve"
	PublishedScreenshotChangeReject  PublishedScreenshotChangeDecision = "reject"
)

type ScreenshotChangeSide string

const (
	ScreenshotChangeBase      ScreenshotChangeSide = "base"
	ScreenshotChangeCandidate ScreenshotChangeSide = "candidate"
)

type ScreenshotManifestSource string

const (
	ScreenshotManifestExisting ScreenshotManifestSource = "existing"
	ScreenshotManifestUpload   ScreenshotManifestSource = "upload"
)

// ScreenshotManifestItem composes one ordered candidate without exposing
// internal object UUIDs. Existing positions bind to the frozen current set;
// upload indexes bind to files in the same multipart command.
type ScreenshotManifestItem struct {
	Source ScreenshotManifestSource
	Index  int
}

type SubmitPublishedScreenshotChangeInput struct {
	RequestID       uuid.UUID
	TorrentID       TorrentID
	ExpectedVersion int64
	Manifest        []ScreenshotManifestItem
	Uploads         []TorrentScreenshotInput
	Reason          string
}

type SubmitPublishedScreenshotChangeCommand struct {
	RequestID          uuid.UUID
	TorrentID          TorrentID
	ExpectedVersion    int64
	UploaderID         uuid.UUID
	Manifest           []ScreenshotManifestItem
	Uploads            []storedTorrentScreenshot
	PolicyRevisionID   uuid.UUID
	RequestFingerprint ObjectSHA256
	Reason             string
	OccurredAt         time.Time
	Authorization      authz.Decision
}

type PublishedScreenshotChange struct {
	ID                 uuid.UUID
	TorrentID          TorrentID
	BaseTorrentVersion int64
	BaseSetVersion     int64
	BaseCount          int
	CandidateCount     int
	Reason             string
	Status             PublishedScreenshotChangeStatus
	Version            int64
	CreatedAt          time.Time
	DecidedAt          *time.Time
}

type PublishedScreenshotChangeQuery struct {
	Status PublishedScreenshotChangeStatus
	Limit  int
	Offset int
}

type ManagedPublishedScreenshotChange struct {
	PublishedScreenshotChange
	UploaderNumericID   int64
	UploaderUsername    string
	UploaderDisplayName string
	TorrentTitle        string
}

type ManagedPublishedScreenshotChangePage struct {
	Items  []ManagedPublishedScreenshotChange
	Total  int64
	Limit  int
	Offset int
}

type DecidePublishedScreenshotChangeInput struct {
	DecisionID             uuid.UUID
	RequestID              uuid.UUID
	ExpectedRequestVersion int64
	Decision               PublishedScreenshotChangeDecision
	Reason                 string
}

type DecidePublishedScreenshotChangeCommand struct {
	DecidePublishedScreenshotChangeInput
	ReviewerID    uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type PublishedScreenshotChangeDecisionResult struct {
	DecisionID        uuid.UUID
	RequestID         uuid.UUID
	TorrentID         TorrentID
	Decision          PublishedScreenshotChangeDecision
	RequestStatus     PublishedScreenshotChangeStatus
	RequestVersion    int64
	AttachmentVersion int64
	DecidedAt         time.Time
}

func (service *TorrentMaintenanceService) SubmitPublishedScreenshotChange(ctx context.Context, cookieToken, csrfToken string, input SubmitPublishedScreenshotChangeInput) (PublishedScreenshotChange, error) {
	if service.stores == nil || service.uploadPolicies == nil {
		return PublishedScreenshotChange{}, ErrPublishedScreenshotChangeUnavailable
	}
	normalized, err := normalizeSubmitPublishedScreenshotChangeInput(input)
	if err != nil {
		return PublishedScreenshotChange{}, err
	}
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return PublishedScreenshotChange{}, err
	}
	if session.User.EmailVerifiedAt == nil {
		return PublishedScreenshotChange{}, ErrPublishedScreenshotChangeEmailUnverified
	}
	now := service.now().UTC().Round(0)
	decision, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionTorrentScreenshotChangeSubmitSelf, now)
	if err != nil {
		return PublishedScreenshotChange{}, err
	}
	prepared, err := prepareTorrentScreenshots(normalized.Uploads, service.newUUID)
	if err != nil {
		return PublishedScreenshotChange{}, err
	}
	policy, err := service.uploadPolicies.Effective(ctx, now)
	if err != nil {
		return PublishedScreenshotChange{}, fmt.Errorf("resolve screenshot change upload policy: %w", err)
	}
	if len(normalized.Manifest) > policy.Settings.ScreenshotMaxCount {
		return PublishedScreenshotChange{}, ErrPublishedScreenshotChangeInput
	}
	if err := validateScreenshotsAgainstPolicy(policy, prepared); err != nil {
		return PublishedScreenshotChange{}, err
	}
	stored, err := storeAndVerifyTorrentScreenshots(ctx, service.stores, service.activeBackendID, prepared)
	if err != nil {
		return PublishedScreenshotChange{}, ErrPublishedScreenshotChangeUnavailable
	}
	return service.repository.SubmitPublishedScreenshotChange(ctx, SubmitPublishedScreenshotChangeCommand{
		RequestID: normalized.RequestID, TorrentID: normalized.TorrentID,
		ExpectedVersion: normalized.ExpectedVersion, UploaderID: session.User.ID,
		Manifest: normalized.Manifest, Uploads: stored, PolicyRevisionID: policy.ID,
		RequestFingerprint: screenshotChangeRequestFingerprint(normalized, prepared),
		Reason:             normalized.Reason, OccurredAt: now, Authorization: decision,
	})
}

func screenshotChangeRequestFingerprint(input SubmitPublishedScreenshotChangeInput, uploads []preparedTorrentScreenshot) ObjectSHA256 {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte("peergo:torrent-screenshot-change:v1\x00"))
	var buffer [8]byte
	binary.BigEndian.PutUint64(buffer[:], uint64(input.TorrentID))
	_, _ = hasher.Write(buffer[:])
	binary.BigEndian.PutUint64(buffer[:], uint64(input.ExpectedVersion))
	_, _ = hasher.Write(buffer[:])
	writeFingerprintString(hasher, input.Reason)
	for _, item := range input.Manifest {
		writeFingerprintString(hasher, string(item.Source))
		binary.BigEndian.PutUint64(buffer[:], uint64(item.Index))
		_, _ = hasher.Write(buffer[:])
	}
	for _, upload := range uploads {
		_, _ = hasher.Write(upload.ContentSHA256[:])
	}
	var result ObjectSHA256
	copy(result[:], hasher.Sum(nil))
	return result
}

func (service *TorrentMaintenanceService) ListPublishedScreenshotChanges(ctx context.Context, actor authz.StaffActor, query PublishedScreenshotChangeQuery) (ManagedPublishedScreenshotChangePage, error) {
	normalized, err := normalizePublishedScreenshotChangeQuery(query)
	if err != nil {
		return ManagedPublishedScreenshotChangePage{}, err
	}
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTorrentScreenshotChangeReview, authz.SiteScope(), service.now().UTC(), "torrent-screenshot-change"); err != nil {
		return ManagedPublishedScreenshotChangePage{}, err
	}
	return service.repository.ListPublishedScreenshotChanges(ctx, normalized)
}

func (service *TorrentMaintenanceService) DecidePublishedScreenshotChange(ctx context.Context, actor authz.StaffActor, input DecidePublishedScreenshotChangeInput) (PublishedScreenshotChangeDecisionResult, error) {
	normalized, err := normalizeDecidePublishedScreenshotChangeInput(input)
	if err != nil {
		return PublishedScreenshotChangeDecisionResult{}, err
	}
	now := service.now().UTC().Round(0)
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTorrentScreenshotChangeReview, authz.SiteScope(), now, "torrent-screenshot-change")
	if err != nil {
		return PublishedScreenshotChangeDecisionResult{}, err
	}
	return service.repository.DecidePublishedScreenshotChange(ctx, DecidePublishedScreenshotChangeCommand{
		DecidePublishedScreenshotChangeInput: normalized,
		ReviewerID:                           actor.Subject.ID, OccurredAt: now, Authorization: decision,
	})
}

func (service *TorrentMaintenanceService) PublishedScreenshotChangeImage(ctx context.Context, actor authz.StaffActor, requestID uuid.UUID, side ScreenshotChangeSide, position int) (PublicScreenshot, error) {
	if service.stores == nil || requestID == uuid.Nil || (side != ScreenshotChangeBase && side != ScreenshotChangeCandidate) || position < 0 || position >= MaxTorrentScreenshots {
		return PublicScreenshot{}, ErrPublishedScreenshotChangeInput
	}
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTorrentScreenshotChangeReview, authz.SiteScope(), service.now().UTC(), "torrent-screenshot-change-image"); err != nil {
		return PublicScreenshot{}, err
	}
	source, err := service.repository.PublishedScreenshotChangeSource(ctx, requestID, side, position)
	if err != nil {
		return PublicScreenshot{}, err
	}
	data, err := readVerifiedStoredObject(ctx, service.stores, source.Descriptor, source.Locations, MaxStoredTorrentScreenshotBytes)
	if err != nil {
		return PublicScreenshot{}, ErrPublishedScreenshotChangeUnavailable
	}
	return PublicScreenshot{Data: data, ContentType: source.ContentType, ETag: `"sha256-` + source.Descriptor.SHA256.Hex() + `"`}, nil
}

func normalizeSubmitPublishedScreenshotChangeInput(input SubmitPublishedScreenshotChangeInput) (SubmitPublishedScreenshotChangeInput, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if input.RequestID == uuid.Nil || input.TorrentID < 1 || input.ExpectedVersion < 1 ||
		len(input.Manifest) > MaxTorrentScreenshots || len(input.Uploads) > MaxTorrentScreenshots ||
		!utf8.ValidString(input.Reason) || utf8.RuneCountInString(input.Reason) < 10 || utf8.RuneCountInString(input.Reason) > 1000 {
		return SubmitPublishedScreenshotChangeInput{}, ErrPublishedScreenshotChangeInput
	}
	usedUploads := make(map[int]struct{}, len(input.Uploads))
	for _, item := range input.Manifest {
		if item.Index < 0 || item.Index >= MaxTorrentScreenshots ||
			(item.Source != ScreenshotManifestExisting && item.Source != ScreenshotManifestUpload) {
			return SubmitPublishedScreenshotChangeInput{}, ErrPublishedScreenshotChangeInput
		}
		if item.Source == ScreenshotManifestUpload {
			if item.Index >= len(input.Uploads) {
				return SubmitPublishedScreenshotChangeInput{}, ErrPublishedScreenshotChangeInput
			}
			if _, duplicate := usedUploads[item.Index]; duplicate {
				return SubmitPublishedScreenshotChangeInput{}, ErrPublishedScreenshotChangeInput
			}
			usedUploads[item.Index] = struct{}{}
		}
	}
	if len(usedUploads) != len(input.Uploads) {
		return SubmitPublishedScreenshotChangeInput{}, ErrPublishedScreenshotChangeInput
	}
	return input, nil
}

func normalizePublishedScreenshotChangeQuery(query PublishedScreenshotChangeQuery) (PublishedScreenshotChangeQuery, error) {
	if query.Limit < 1 || query.Limit > MaxPublishedScreenshotChangeLimit || query.Offset < 0 || query.Offset > MaxManagedTorrentOffset {
		return PublishedScreenshotChangeQuery{}, ErrPublishedScreenshotChangeInput
	}
	switch query.Status {
	case "", PublishedScreenshotChangePending, PublishedScreenshotChangeApproved, PublishedScreenshotChangeRejected:
		return query, nil
	default:
		return PublishedScreenshotChangeQuery{}, ErrPublishedScreenshotChangeInput
	}
}

func normalizeDecidePublishedScreenshotChangeInput(input DecidePublishedScreenshotChangeInput) (DecidePublishedScreenshotChangeInput, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	length := utf8.RuneCountInString(input.Reason)
	if input.DecisionID == uuid.Nil || input.RequestID == uuid.Nil || input.ExpectedRequestVersion != 1 ||
		!utf8.ValidString(input.Reason) || length < 10 || length > 1000 ||
		(input.Decision != PublishedScreenshotChangeApprove && input.Decision != PublishedScreenshotChangeReject) {
		return DecidePublishedScreenshotChangeInput{}, ErrPublishedScreenshotChangeInput
	}
	return input, nil
}
