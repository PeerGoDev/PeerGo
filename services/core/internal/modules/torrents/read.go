package torrents

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/imaging"
)

const (
	DefaultTorrentFileLimit         = 50
	MaxTorrentFileLimit             = 100
	DefaultMyTorrentSubmissionLimit = 20
	MaxMyTorrentSubmissionLimit     = 50
	MaxRelatedTorrentVersions       = 10
)

var (
	ErrTorrentReadInput                 = errors.New("torrent read input is invalid")
	ErrTorrentReadNotFound              = errors.New("published torrent was not found")
	ErrTorrentReadInvariant             = errors.New("torrent read projection violates persisted invariants")
	ErrTorrentCoverNotFound             = errors.New("published torrent cover was not found")
	ErrTorrentCoverUnavailable          = errors.New("published torrent cover storage is unavailable")
	ErrTorrentCoverConflict             = errors.New("published torrent cover failed immutable verification")
	ErrTorrentScreenshotNotFound        = errors.New("published torrent screenshot was not found")
	ErrTorrentScreenshotUnavailable     = errors.New("published torrent screenshot storage is unavailable")
	ErrTorrentScreenshotConflict        = errors.New("published torrent screenshot failed immutable verification")
	ErrTorrentReviewEvidenceUnavailable = errors.New("pending torrent review evidence is unavailable")
)

// PublicDetail contains stable aggregate metadata. Comments and live swarm
// counters stay separate so a slow dependency cannot make the description
// fail. Promotion comes only from Core's local display projection; settlement
// never reads it.
type PublicDetail struct {
	ID                  TorrentID
	Category            catalog.Category
	Title               string
	Subtitle            string
	ContentName         string
	UploaderDisplayName string
	Anonymous           bool
	Promotion           catalog.Promotion
	PromotionEndsAt     *time.Time
	StickyUntil         *time.Time
	Facets              []PublicFacet
	ExternalIdentifiers []ExternalIdentifier
	InfoHashV1          InfoHashV1
	TotalSizeBytes      int64
	PayloadSizeBytes    int64
	FileCount           int
	PaddingFileCount    int
	ScreenshotCount     int
	PieceLengthBytes    int64
	PieceCount          int
	State               State
	SubmittedAt         time.Time
	PublishedAt         time.Time
}

type PublicFacet struct {
	FacetID     string
	FacetName   string
	OptionKey   string
	OptionLabel string
}

type PublicContent struct {
	TorrentID         TorrentID
	Description       string
	DescriptionFormat string
	MediaInfo         string
}

// PendingReviewEvidence is a private, reviewer-only view of the immutable
// upload. It intentionally has no promotion, swarm or download fields because
// a pending torrent is not public and is not yet eligible for Tracker traffic.
type PendingReviewEvidence struct {
	ID                  TorrentID
	Category            catalog.Category
	Title               string
	Subtitle            string
	ContentName         string
	UploaderDisplayName string
	Anonymous           bool
	Facets              []PublicFacet
	ExternalIdentifiers []ExternalIdentifier
	InfoHashV1          InfoHashV1
	TotalSizeBytes      int64
	PayloadSizeBytes    int64
	FileCount           int
	PaddingFileCount    int
	ScreenshotCount     int
	PieceLengthBytes    int64
	PieceCount          int
	State               State
	Version             int64
	SubmittedAt         time.Time
	ReviewRequestedAt   time.Time
	Description         string
	DescriptionFormat   string
	MediaInfo           string
}

type PendingReviewEvidenceRepository interface {
	PendingReviewEvidence(context.Context, TorrentID) (PendingReviewEvidence, error)
	PendingReviewCoverSource(context.Context, TorrentID) (PublicCoverSource, error)
	PendingReviewScreenshotSource(context.Context, TorrentID, int) (PublicScreenshotSource, error)
	PendingReviewFiles(context.Context, TorrentID, int, int) (PublicFilePage, error)
}

type PublicCoverSource struct {
	TorrentID   TorrentID
	ObjectID    uuid.UUID
	ContentType string
	Width       int
	Height      int
	Descriptor  StoredObjectDescriptor
	Locations   []ReadableObjectLocation
}

type PublicCover struct {
	Data        []byte
	ContentType string
	ETag        string
}

// PublicScreenshotSource binds a stable display position to immutable object
// evidence. Physical locations remain server-side so a storage backend switch
// cannot change the public URL or leak object keys and credentials.
type PublicScreenshotSource struct {
	TorrentID   TorrentID
	Position    int
	ObjectID    uuid.UUID
	ContentType string
	Width       int
	Height      int
	Descriptor  StoredObjectDescriptor
	Locations   []ReadableObjectLocation
}

type PublicScreenshot struct {
	Data        []byte
	ContentType string
	ETag        string
}

type PublicFile struct {
	Index       int
	DisplayPath string
	SizeBytes   int64
	IsPadding   bool
}

type PublicFilePage struct {
	TorrentID TorrentID
	Items     []PublicFile
	Total     int
	Limit     int
	Offset    int
}

// ReviewFeedback is the uploader-safe result of the latest immutable review
// decision. Reviewer identity, authorization evidence and audit metadata never
// cross this read model.
type ReviewFeedback struct {
	Outcome    State
	ReasonCode string
	Reason     string
	DecidedAt  time.Time
}

// ContentChangeFeedback is the uploader-safe state of the latest published
// content candidate. Request/reviewer identifiers and authorization evidence
// remain internal; a decision reason is disclosed only after resolution.
type ContentChangeFeedback struct {
	Status         PublishedContentChangeStatus
	SubmittedAt    time.Time
	DecidedAt      *time.Time
	DecisionReason *string
}

// ScreenshotChangeFeedback is the uploader-safe state of the latest ordered
// attachment-set candidate. Object identifiers and storage locations stay
// private to the review workflow.
type ScreenshotChangeFeedback struct {
	Status         PublishedScreenshotChangeStatus
	SubmittedAt    time.Time
	DecidedAt      *time.Time
	DecisionReason *string
}

// WithdrawalFeedback exposes only the uploader-safe outcome of the latest
// withdrawal request. Request/reviewer UUIDs and authorization evidence stay
// inside the maintenance domain.
type WithdrawalFeedback struct {
	Status         TorrentWithdrawalStatus
	SubmittedAt    time.Time
	DecidedAt      *time.Time
	DecisionReason *string
}

type MySubmission struct {
	ID               TorrentID
	Category         catalog.Category
	Title            string
	Subtitle         string
	ContentName      string
	InfoHashV1       InfoHashV1
	TotalSizeBytes   int64
	FileCount        int
	State            State
	Version          int64
	SubmittedAt      time.Time
	PublishedAt      *time.Time
	StateChangedAt   time.Time
	LatestReview     *ReviewFeedback
	ContentChange    *ContentChangeFeedback
	ScreenshotChange *ScreenshotChangeFeedback
	Withdrawal       *WithdrawalFeedback
}

type MySubmissionPage struct {
	Items []MySubmission
	Total int64
	Limit int
}

type TorrentReadRepository interface {
	PublishedDetail(context.Context, TorrentID) (PublicDetail, error)
	PublishedCoverSource(context.Context, TorrentID) (PublicCoverSource, error)
	PublishedScreenshotSource(context.Context, TorrentID, int) (PublicScreenshotSource, error)
	PublishedContent(context.Context, TorrentID) (PublicContent, error)
	PublishedRelatedVersions(context.Context, TorrentID, int) ([]catalog.Torrent, error)
	PublishedFiles(context.Context, TorrentID, int, int) (PublicFilePage, error)
	UserSubmissions(context.Context, uuid.UUID, int) (MySubmissionPage, error)
}

type TorrentImageDerivativeReader interface {
	ReadyForTorrentScreenshot(context.Context, uuid.UUID, imaging.Variant) (imaging.ReadyDerivative, error)
}

type TorrentReadService struct {
	authenticator  TorrentDownloadSessionAuthenticator
	authorizer     authz.Authorizer
	repository     TorrentReadRepository
	stores         *StoreRegistry
	derivatives    TorrentImageDerivativeReader
	administration *TorrentAdministrationService
	now            func() time.Time
}

func NewTorrentReadService(
	authenticator TorrentDownloadSessionAuthenticator,
	authorizer authz.Authorizer,
	repository TorrentReadRepository,
	stores *StoreRegistry,
	derivatives TorrentImageDerivativeReader,
	now func() time.Time,
	administration ...*TorrentAdministrationService,
) (*TorrentReadService, error) {
	if authenticator == nil || authorizer == nil || repository == nil || stores == nil {
		return nil, errors.New("torrent read service dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	var administrationService *TorrentAdministrationService
	if len(administration) > 1 {
		return nil, errors.New("at most one torrent administration service may be provided")
	}
	if len(administration) == 1 {
		administrationService = administration[0]
	}
	return &TorrentReadService{
		authenticator:  authenticator,
		authorizer:     authorizer,
		repository:     repository,
		stores:         stores,
		derivatives:    derivatives,
		administration: administrationService,
		now:            now,
	}, nil
}

// ListManaged and ChangeAvailability deliberately delegate to the dedicated
// staff use case. They share the transport dependency with public reads only to
// avoid expanding the already broad HTTP composition surface; authorization
// and mutation ownership remain separate.
func (service *TorrentReadService) ListManaged(ctx context.Context, actor authz.StaffActor, query ManagedTorrentQuery) (ManagedTorrentPage, error) {
	if service.administration == nil {
		return ManagedTorrentPage{}, errors.New("torrent administration service is unavailable")
	}
	return service.administration.ListManaged(ctx, actor, query)
}

func (service *TorrentReadService) ChangeAvailability(ctx context.Context, actor authz.StaffActor, input ChangeTorrentAvailabilityInput) (TorrentAvailabilityResult, error) {
	if service.administration == nil {
		return TorrentAvailabilityResult{}, errors.New("torrent administration service is unavailable")
	}
	return service.administration.ChangeAvailability(ctx, actor, input)
}

func (service *TorrentReadService) ManagedActivePeers(ctx context.Context, actor authz.StaffActor, torrentID TorrentID) (ManagedTorrentPeerList, error) {
	if service.administration == nil {
		return ManagedTorrentPeerList{}, ErrManagedTorrentPeersUnavailable
	}
	return service.administration.ActivePeers(ctx, actor, torrentID)
}

func (service *TorrentReadService) ManagedUserTrackerActivity(ctx context.Context, actor authz.StaffActor, userID uuid.UUID) (UserTrackerActivity, error) {
	if service.administration == nil {
		return UserTrackerActivity{}, ErrManagedTorrentPeersUnavailable
	}
	return service.administration.UserActivePeers(ctx, actor, userID)
}

// ActivePeers restores the member-facing PtYes user list while retaining the
// privacy-minimized Tracker projection. A valid Web session is required, and
// network endpoints and protocol/session identifiers never leave Tracker.
func (service *TorrentReadService) ActivePeers(ctx context.Context, cookieToken string, torrentID TorrentID) (ManagedTorrentPeerList, error) {
	if torrentID < 1 {
		return ManagedTorrentPeerList{}, ErrTorrentReadInput
	}
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return ManagedTorrentPeerList{}, err
	}
	if _, err := authz.AuthorizeWebMemberAction(
		ctx,
		service.authorizer,
		session.User.ID,
		authz.ActionTorrentPeerReadMember,
		service.now().UTC(),
	); err != nil {
		return ManagedTorrentPeerList{}, err
	}
	if service.administration == nil {
		return ManagedTorrentPeerList{}, ErrManagedTorrentPeersUnavailable
	}
	return service.administration.activePeers(ctx, torrentID)
}

func (service *TorrentReadService) MyTrackerActivity(ctx context.Context, cookieToken string) (UserTrackerActivity, error) {
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return UserTrackerActivity{}, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionTrafficReadSelf, service.now().UTC()); err != nil {
		return UserTrackerActivity{}, err
	}
	if service.administration == nil {
		return UserTrackerActivity{}, ErrManagedTorrentPeersUnavailable
	}
	return service.administration.userActivePeers(ctx, session.User.ID)
}

// UserTrackerActivityForIntegration exposes the same privacy-minimized,
// in-memory Tracker aggregate after a dedicated integration credential has
// authenticated the account and its fixed traffic-read scope. No endpoint,
// peer ID, passkey, Web-audience authorization decision or durable activity
// record crosses this boundary.
func (service *TorrentReadService) UserTrackerActivityForIntegration(ctx context.Context, user identity.User) (UserTrackerActivity, error) {
	if user.ID == uuid.Nil {
		return UserTrackerActivity{}, ErrTorrentReadInput
	}
	if service.administration == nil {
		return UserTrackerActivity{}, ErrManagedTorrentPeersUnavailable
	}
	return service.administration.userActivePeers(ctx, user.ID)
}

func (service *TorrentReadService) Cover(ctx context.Context, torrentID TorrentID) (PublicCover, error) {
	if torrentID < 1 {
		return PublicCover{}, ErrTorrentReadInput
	}
	source, err := service.repository.PublishedCoverSource(ctx, torrentID)
	if err != nil {
		return PublicCover{}, err
	}
	return service.readCoverSource(ctx, source)
}

func (service *TorrentReadService) readCoverSource(ctx context.Context, source PublicCoverSource) (PublicCover, error) {
	if service.derivatives != nil {
		derivative, derivativeErr := service.derivatives.ReadyForTorrentScreenshot(ctx, source.ObjectID, imaging.VariantThumbnail)
		if derivativeErr == nil {
			if data, readErr := imaging.ReadReady(ctx, service.stores, derivative); readErr == nil {
				return PublicCover{
					Data: data, ContentType: "image/webp",
					ETag: `"sha256-` + derivative.Descriptor.SHA256.Hex() + `"`,
				}, nil
			}
		}
	}
	data, err := readVerifiedStoredObject(
		ctx,
		service.stores,
		source.Descriptor,
		source.Locations,
		MaxStoredTorrentScreenshotBytes,
	)
	if errors.Is(err, ErrReadableObjectUnavailable) {
		return PublicCover{}, ErrTorrentCoverUnavailable
	}
	if err != nil {
		return PublicCover{}, ErrTorrentCoverConflict
	}
	return PublicCover{
		Data:        data,
		ContentType: source.ContentType,
		ETag:        `"sha256-` + source.Descriptor.SHA256.Hex() + `"`,
	}, nil
}

// Screenshot reads one of the at-most-six immutable screenshot objects by its
// upload position. It deliberately repeats the cover integrity boundary: every
// response is revalidated against the database descriptor after resolving the
// current local/S3 read priority.
func (service *TorrentReadService) Screenshot(ctx context.Context, torrentID TorrentID, position int) (PublicScreenshot, error) {
	if torrentID < 1 || position < 0 || position >= MaxTorrentScreenshots {
		return PublicScreenshot{}, ErrTorrentReadInput
	}
	source, err := service.repository.PublishedScreenshotSource(ctx, torrentID, position)
	if err != nil {
		return PublicScreenshot{}, err
	}
	return service.readScreenshotSource(ctx, source)
}

func (service *TorrentReadService) readScreenshotSource(ctx context.Context, source PublicScreenshotSource) (PublicScreenshot, error) {
	if service.derivatives != nil {
		derivative, derivativeErr := service.derivatives.ReadyForTorrentScreenshot(ctx, source.ObjectID, imaging.VariantDisplay)
		if derivativeErr == nil {
			if data, readErr := imaging.ReadReady(ctx, service.stores, derivative); readErr == nil {
				return PublicScreenshot{
					Data: data, ContentType: "image/webp",
					ETag: `"sha256-` + derivative.Descriptor.SHA256.Hex() + `"`,
				}, nil
			}
		}
	}
	data, err := readVerifiedStoredObject(
		ctx,
		service.stores,
		source.Descriptor,
		source.Locations,
		MaxStoredTorrentScreenshotBytes,
	)
	if errors.Is(err, ErrReadableObjectUnavailable) {
		return PublicScreenshot{}, ErrTorrentScreenshotUnavailable
	}
	if err != nil {
		return PublicScreenshot{}, ErrTorrentScreenshotConflict
	}
	return PublicScreenshot{
		Data:        data,
		ContentType: source.ContentType,
		ETag:        `"sha256-` + source.Descriptor.SHA256.Hex() + `"`,
	}, nil
}

func (service *TorrentReadService) PendingReviewEvidence(ctx context.Context, torrentID TorrentID) (PendingReviewEvidence, error) {
	if torrentID < 1 {
		return PendingReviewEvidence{}, ErrTorrentReadInput
	}
	repository, ok := service.repository.(PendingReviewEvidenceRepository)
	if !ok {
		return PendingReviewEvidence{}, ErrTorrentReviewEvidenceUnavailable
	}
	return repository.PendingReviewEvidence(ctx, torrentID)
}

func (service *TorrentReadService) PendingReviewFiles(ctx context.Context, torrentID TorrentID, limit, offset int) (PublicFilePage, error) {
	if torrentID < 1 || limit < 1 || limit > MaxTorrentFileLimit || offset < 0 || offset > 99999 {
		return PublicFilePage{}, ErrTorrentReadInput
	}
	repository, ok := service.repository.(PendingReviewEvidenceRepository)
	if !ok {
		return PublicFilePage{}, ErrTorrentReviewEvidenceUnavailable
	}
	return repository.PendingReviewFiles(ctx, torrentID, limit, offset)
}

func (service *TorrentReadService) PendingReviewCover(ctx context.Context, torrentID TorrentID) (PublicCover, error) {
	if torrentID < 1 {
		return PublicCover{}, ErrTorrentReadInput
	}
	repository, ok := service.repository.(PendingReviewEvidenceRepository)
	if !ok {
		return PublicCover{}, ErrTorrentReviewEvidenceUnavailable
	}
	source, err := repository.PendingReviewCoverSource(ctx, torrentID)
	if err != nil {
		return PublicCover{}, err
	}
	return service.readCoverSource(ctx, source)
}

func (service *TorrentReadService) PendingReviewScreenshot(ctx context.Context, torrentID TorrentID, position int) (PublicScreenshot, error) {
	if torrentID < 1 || position < 0 || position >= MaxTorrentScreenshots {
		return PublicScreenshot{}, ErrTorrentReadInput
	}
	repository, ok := service.repository.(PendingReviewEvidenceRepository)
	if !ok {
		return PublicScreenshot{}, ErrTorrentReviewEvidenceUnavailable
	}
	source, err := repository.PendingReviewScreenshotSource(ctx, torrentID, position)
	if err != nil {
		return PublicScreenshot{}, err
	}
	return service.readScreenshotSource(ctx, source)
}

func (service *TorrentReadService) Detail(ctx context.Context, torrentID TorrentID) (PublicDetail, error) {
	if torrentID < 1 {
		return PublicDetail{}, ErrTorrentReadInput
	}
	return service.repository.PublishedDetail(ctx, torrentID)
}

func (service *TorrentReadService) Content(ctx context.Context, torrentID TorrentID) (PublicContent, error) {
	if torrentID < 1 {
		return PublicContent{}, ErrTorrentReadInput
	}
	return service.repository.PublishedContent(ctx, torrentID)
}

func (service *TorrentReadService) RelatedVersions(ctx context.Context, torrentID TorrentID) ([]catalog.TorrentSummary, error) {
	if torrentID < 1 {
		return nil, ErrTorrentReadInput
	}
	items, err := service.repository.PublishedRelatedVersions(ctx, torrentID, MaxRelatedTorrentVersions)
	if err != nil {
		return nil, err
	}
	summaries := make([]catalog.TorrentSummary, 0, len(items))
	for _, item := range items {
		summaries = append(summaries, catalog.SummarizeTorrent(item, service.now()))
	}
	return summaries, nil
}

func (service *TorrentReadService) Files(ctx context.Context, torrentID TorrentID, limit, offset int) (PublicFilePage, error) {
	if torrentID < 1 || limit < 1 || limit > MaxTorrentFileLimit || offset < 0 || offset > 99999 {
		return PublicFilePage{}, ErrTorrentReadInput
	}
	return service.repository.PublishedFiles(ctx, torrentID, limit, offset)
}

// MySubmissions authenticates and authorizes before reading by uploader ID.
// Keeping read permission separate from torrent.submit lets a user retain
// access to past rejection feedback even if future upload authority is removed.
func (service *TorrentReadService) MySubmissions(ctx context.Context, cookieToken string, limit int) (MySubmissionPage, error) {
	if limit < 1 || limit > MaxMyTorrentSubmissionLimit {
		return MySubmissionPage{}, ErrTorrentReadInput
	}
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return MySubmissionPage{}, err
	}
	if _, err := authz.AuthorizeWebSelfAction(
		ctx,
		service.authorizer,
		session.User.ID,
		authz.ActionTorrentSubmissionReadSelf,
		service.now().UTC(),
	); err != nil {
		return MySubmissionPage{}, err
	}
	return service.repository.UserSubmissions(ctx, session.User.ID, limit)
}
