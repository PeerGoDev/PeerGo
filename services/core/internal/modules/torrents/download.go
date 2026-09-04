package torrents

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/economy/torrentpurchase"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

const maxDownloadFilenameRunes = 160

var (
	ErrTorrentDownloadNotFound           = errors.New("published torrent was not found")
	ErrTorrentDownloadEmailUnverified    = errors.New("torrent download requires a verified email")
	ErrTorrentDownloadRestricted         = errors.New("torrent download is restricted by account policy")
	ErrTorrentDownloadStorageUnavailable = errors.New("torrent download storage is unavailable")
	ErrTorrentDownloadObjectConflict     = errors.New("torrent download object failed immutable verification")
)

type TorrentDownloadLocation struct {
	ID         uuid.UUID
	BackendID  StorageBackendID
	ObjectKey  ObjectKey
	State      StorageLocationState
	Preferred  bool
	VersionID  string
	Descriptor StoredObjectDescriptor
	VerifiedAt time.Time
}

type TorrentDownloadSource struct {
	TorrentID      TorrentID
	Title          string
	FilenamePrefix string
	ObjectID       uuid.UUID
	Descriptor     StoredObjectDescriptor
	InfoOffset     int64
	InfoLength     int64
	Locations      []TorrentDownloadLocation
}

type TorrentDownloadResult struct {
	Data     []byte
	Filename string
}

type TorrentDownloadRepository interface {
	DownloadRestricted(context.Context, uuid.UUID) (bool, error)
	PublishedDownloadSource(context.Context, TorrentID) (TorrentDownloadSource, error)
}

// PendingTorrentDownloadRepository is an uploader-private extension. Keeping
// it separate preserves the public/RSS repository contract while letting a
// newly submitted torrent be pre-seeded before review without making the
// object available to other members.
type PendingTorrentDownloadRepository interface {
	PendingReviewUploaderDownloadSource(context.Context, TorrentID, uuid.UUID) (TorrentDownloadSource, error)
}

type TorrentDownloadSessionAuthenticator interface {
	CurrentSession(context.Context, string) (identity.WebSession, error)
}

type TrackerCredentialProvider interface {
	ForUser(context.Context, identity.User) (identity.TrackerCredential, error)
}

type TorrentPurchaseService interface {
	RequireDownloadAccess(context.Context, uuid.UUID, int64) error
	MyStatus(context.Context, string, int64) (torrentpurchase.Status, error)
	MyHistory(context.Context, string, int, int) (torrentpurchase.HistoryPage, error)
	Purchase(context.Context, string, string, uuid.UUID, int64) (torrentpurchase.Receipt, error)
	PurchasePolicy(context.Context, authz.StaffActor) (torrentpurchase.PolicySettings, error)
	UpdatePurchasePolicy(context.Context, authz.StaffActor, torrentpurchase.UpdatePolicyCommand) (torrentpurchase.PolicySettings, error)
	UpdateTorrentPrice(context.Context, authz.StaffActor, torrentpurchase.UpdatePriceCommand) (torrentpurchase.PriceChange, error)
	AdminHistory(context.Context, authz.StaffActor, torrentpurchase.AdminPurchaseQuery) (torrentpurchase.AdminPurchasePage, error)
	RefundPurchase(context.Context, authz.StaffActor, torrentpurchase.RefundCommand) (torrentpurchase.RefundReceipt, error)
}

type TorrentDownloadServiceConfig struct {
	CanonicalTrackerOrigin string
	Now                    func() time.Time
}

type TorrentDownloadService struct {
	authenticator TorrentDownloadSessionAuthenticator
	authorizer    authz.Authorizer
	repository    TorrentDownloadRepository
	purchases     TorrentPurchaseService
	credentials   TrackerCredentialProvider
	stores        *StoreRegistry
	trackerOrigin url.URL
	now           func() time.Time
}

func NewTorrentDownloadService(
	authenticator TorrentDownloadSessionAuthenticator,
	authorizer authz.Authorizer,
	repository TorrentDownloadRepository,
	purchases TorrentPurchaseService,
	credentials TrackerCredentialProvider,
	stores *StoreRegistry,
	config TorrentDownloadServiceConfig,
) (*TorrentDownloadService, error) {
	if authenticator == nil || authorizer == nil || repository == nil || purchases == nil || credentials == nil || stores == nil {
		return nil, errors.New("torrent download service dependencies are required")
	}
	trackerOrigin, err := url.Parse(strings.TrimSpace(config.CanonicalTrackerOrigin))
	if err != nil || trackerOrigin.Scheme == "" || trackerOrigin.Host == "" || trackerOrigin.User != nil ||
		trackerOrigin.RawQuery != "" || trackerOrigin.Fragment != "" ||
		(trackerOrigin.Path != "" && trackerOrigin.Path != "/") ||
		(trackerOrigin.Scheme != "http" && trackerOrigin.Scheme != "https") {
		return nil, errors.New("canonical Tracker origin is invalid")
	}
	trackerOrigin.Path = ""
	if config.Now == nil {
		config.Now = time.Now
	}
	return &TorrentDownloadService{
		authenticator: authenticator,
		authorizer:    authorizer,
		repository:    repository,
		purchases:     purchases,
		credentials:   credentials,
		stores:        stores,
		trackerOrigin: *trackerOrigin,
		now:           config.Now,
	}, nil
}

func (service *TorrentDownloadService) MyPurchaseStatus(ctx context.Context, cookieToken string, torrentID TorrentID) (torrentpurchase.Status, error) {
	return service.purchases.MyStatus(ctx, cookieToken, int64(torrentID))
}

func (service *TorrentDownloadService) Purchase(ctx context.Context, cookieToken, csrfToken string, requestID uuid.UUID, torrentID TorrentID) (torrentpurchase.Receipt, error) {
	return service.purchases.Purchase(ctx, cookieToken, csrfToken, requestID, int64(torrentID))
}

func (service *TorrentDownloadService) MyPurchaseHistory(ctx context.Context, cookieToken string, limit, offset int) (torrentpurchase.HistoryPage, error) {
	return service.purchases.MyHistory(ctx, cookieToken, limit, offset)
}

func (service *TorrentDownloadService) PurchasePolicy(ctx context.Context, actor authz.StaffActor) (torrentpurchase.PolicySettings, error) {
	return service.purchases.PurchasePolicy(ctx, actor)
}

func (service *TorrentDownloadService) UpdatePurchasePolicy(ctx context.Context, actor authz.StaffActor, input torrentpurchase.UpdatePolicyCommand) (torrentpurchase.PolicySettings, error) {
	return service.purchases.UpdatePurchasePolicy(ctx, actor, input)
}

func (service *TorrentDownloadService) UpdateTorrentPrice(ctx context.Context, actor authz.StaffActor, input torrentpurchase.UpdatePriceCommand) (torrentpurchase.PriceChange, error) {
	return service.purchases.UpdateTorrentPrice(ctx, actor, input)
}

func (service *TorrentDownloadService) AdminPurchaseHistory(ctx context.Context, actor authz.StaffActor, query torrentpurchase.AdminPurchaseQuery) (torrentpurchase.AdminPurchasePage, error) {
	return service.purchases.AdminHistory(ctx, actor, query)
}

func (service *TorrentDownloadService) RefundPurchase(ctx context.Context, actor authz.StaffActor, input torrentpurchase.RefundCommand) (torrentpurchase.RefundReceipt, error) {
	return service.purchases.RefundPurchase(ctx, actor, input)
}

// Download verifies the ordinary Web audience and the typed self capability,
// then reads one immutable published object. The Tracker credential is fetched
// only after storage verification succeeds, so a missing/corrupt object cannot
// mint an otherwise unused passkey as a side effect.
func (service *TorrentDownloadService) Download(ctx context.Context, cookieToken string, torrentID TorrentID) (TorrentDownloadResult, error) {
	if torrentID < 1 {
		return TorrentDownloadResult{}, ErrTorrentInputInvalid
	}
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return TorrentDownloadResult{}, err
	}
	if session.User.EmailVerifiedAt == nil {
		return TorrentDownloadResult{}, ErrTorrentDownloadEmailUnverified
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionTorrentDownload, service.now().UTC()); err != nil {
		return TorrentDownloadResult{}, err
	}
	return service.downloadForAuthorizedUser(ctx, session.User, torrentID)
}

// DownloadForRSS reuses the complete purchase, restriction, immutable object
// and Tracker-announce pipeline after the RSS module has authenticated a
// separate subscription token and proved that the torrent belongs to its
// frozen feed projection.  Keeping this method here prevents RSS from growing
// a second, subtly weaker torrent download implementation.
func (service *TorrentDownloadService) DownloadForRSS(ctx context.Context, user identity.User, torrentID TorrentID) (TorrentDownloadResult, error) {
	if torrentID < 1 || user.ID == uuid.Nil || user.CredentialRef == uuid.Nil || user.EmailVerifiedAt == nil {
		return TorrentDownloadResult{}, ErrTorrentDownloadEmailUnverified
	}
	return service.downloadForAuthorizedUser(ctx, user, torrentID)
}

func (service *TorrentDownloadService) downloadForAuthorizedUser(ctx context.Context, user identity.User, torrentID TorrentID) (TorrentDownloadResult, error) {
	// Manual restrictions and automated ratio assessments share one database
	// predicate. Keep this gate ahead of purchases and storage reads so a
	// restricted account cannot spend magic or trigger object-store traffic.
	restricted, err := service.repository.DownloadRestricted(ctx, user.ID)
	if err != nil {
		return TorrentDownloadResult{}, err
	}
	if restricted {
		return TorrentDownloadResult{}, ErrTorrentDownloadRestricted
	}
	purchaseErr := service.purchases.RequireDownloadAccess(ctx, user.ID, int64(torrentID))
	var source TorrentDownloadSource
	switch {
	case purchaseErr == nil:
		source, err = service.repository.PublishedDownloadSource(ctx, torrentID)
	case errors.Is(purchaseErr, torrentpurchase.ErrNotFound):
		pendingRepository, ok := service.repository.(PendingTorrentDownloadRepository)
		if !ok {
			return TorrentDownloadResult{}, ErrTorrentDownloadNotFound
		}
		source, err = pendingRepository.PendingReviewUploaderDownloadSource(ctx, torrentID, user.ID)
	default:
		return TorrentDownloadResult{}, purchaseErr
	}
	if err != nil {
		return TorrentDownloadResult{}, err
	}
	original, err := service.readVerifiedOriginal(ctx, source)
	if err != nil {
		return TorrentDownloadResult{}, err
	}
	credential, err := service.credentials.ForUser(ctx, user)
	if err != nil {
		return TorrentDownloadResult{}, err
	}
	announceURL := service.trackerOrigin
	announceURL.Path = "/tracker/" + credential.Passkey + "/announce"
	copyBytes, err := RewriteTrackerAnnounce(
		original,
		source.InfoOffset,
		source.InfoLength,
		[][]string{{announceURL.String()}},
	)
	if err != nil {
		return TorrentDownloadResult{}, fmt.Errorf("%w: rewrite Tracker announce fields: %v", ErrTorrentDownloadObjectConflict, err)
	}
	return TorrentDownloadResult{
		Data:     copyBytes,
		Filename: torrentDownloadFilename(source.FilenamePrefix, source.Title, source.TorrentID),
	}, nil
}

// readVerifiedOriginal follows the storage migration read contract: preferred
// comes first and an unavailable backend may fall back to another
// verified/retiring copy. Once a backend returns bytes or metadata that
// conflicts with immutable PostgreSQL evidence, the request fails closed
// instead of masking corruption with a fallback.
func (service *TorrentDownloadService) readVerifiedOriginal(ctx context.Context, source TorrentDownloadSource) ([]byte, error) {
	if source.TorrentID < 1 || source.ObjectID == uuid.Nil || !source.Descriptor.Valid() ||
		source.Descriptor.ByteLength > MaxMetainfoBytes || source.InfoOffset < 0 || source.InfoLength <= 0 ||
		source.InfoOffset > source.Descriptor.ByteLength-source.InfoLength {
		return nil, ErrTorrentDownloadObjectConflict
	}
	locations := make([]ReadableObjectLocation, 0, len(source.Locations))
	for _, location := range source.Locations {
		if location.Descriptor != source.Descriptor || location.ID == uuid.Nil || location.VerifiedAt.IsZero() ||
			(location.State != StorageLocationVerified && location.State != StorageLocationRetiring) {
			return nil, ErrTorrentDownloadObjectConflict
		}
		locations = append(locations, ReadableObjectLocation{
			ID: location.ID, BackendID: location.BackendID,
			ObjectKey: location.ObjectKey, Preferred: location.Preferred,
			VersionID: location.VersionID, Descriptor: location.Descriptor,
			VerifiedAt: location.VerifiedAt,
		})
	}
	data, err := readVerifiedStoredObject(ctx, service.stores, source.Descriptor, locations, MaxMetainfoBytes)
	if errors.Is(err, ErrReadableObjectUnavailable) {
		return nil, ErrTorrentDownloadStorageUnavailable
	}
	if err != nil {
		return nil, ErrTorrentDownloadObjectConflict
	}
	return data, nil
}

func torrentDownloadFilename(prefix, title string, torrentID TorrentID) string {
	title = torrentDownloadFilenamePart(title)
	if title == "" {
		title = fmt.Sprintf("PeerGo-%d", torrentID)
	}
	prefix = torrentDownloadFilenamePart(prefix)
	name := title
	if prefix != "" {
		name = prefix + "." + title
	}
	if utf8.RuneCountInString(name) > maxDownloadFilenameRunes {
		name = string([]rune(name)[:maxDownloadFilenameRunes])
	}
	name = strings.Trim(strings.TrimSpace(name), ".")
	if name == "" {
		name = fmt.Sprintf("PeerGo-%d", torrentID)
	}
	return name + ".torrent"
}

func torrentDownloadFilenamePart(value string) string {
	value = strings.TrimSpace(value)
	var builder strings.Builder
	for _, character := range value {
		if builder.Len() >= maxDownloadFilenameRunes*utf8.UTFMax {
			break
		}
		switch {
		case unicode.IsControl(character), strings.ContainsRune(`/\\:*?"<>|`, character):
			builder.WriteRune('_')
		default:
			builder.WriteRune(character)
		}
	}
	return strings.Trim(strings.TrimSpace(builder.String()), ".")
}
