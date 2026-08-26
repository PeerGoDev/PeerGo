package moviepilot

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
	"github.com/peergo/peergo/services/core/internal/modules/economy/attendance"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

const (
	rawAPIKeyBytes        = 32
	apiKeyPrefix          = "pgk_"
	downloadCapabilityTTL = 5 * time.Minute
)

type SessionAuthenticator interface {
	CurrentSession(context.Context, string) (identity.WebSession, error)
	AuthenticateWrite(context.Context, string, string) (identity.WebSession, error)
}

type CatalogService interface {
	ListTorrents(context.Context, catalog.TorrentListRequest) (catalog.TorrentPage, error)
	GetTorrentSwarm(context.Context, int64) (catalog.TorrentSwarmOverview, error)
}

type TorrentReadService interface {
	Detail(context.Context, torrents.TorrentID) (torrents.PublicDetail, error)
	UserTrackerActivityForIntegration(context.Context, identity.User) (torrents.UserTrackerActivity, error)
}

type TorrentDownloadService interface {
	DownloadForRSS(context.Context, identity.User, torrents.TorrentID) (torrents.TorrentDownloadResult, error)
}

type AttendanceService interface {
	OverviewForIntegration(context.Context, identity.User) (attendance.Overview, error)
	ClaimForIntegration(context.Context, identity.User, uuid.UUID, attendance.Mode) (attendance.Record, error)
}

type ServiceConfig struct {
	PublicOrigin string
	SigningKey   []byte
	Now          func() time.Time
	ReadRandom   func([]byte) (int, error)
}

type rateWindow struct {
	minute time.Time
	count  int
}

// Service is a deliberately narrow compatibility layer for MoviePilot. It
// projects canonical PeerGo reads directly and keeps only one hashed API-key
// row per user; searches, downloads and attendance calls create no API log or
// copied torrent/profile record in PostgreSQL.
type Service struct {
	repository    Repository
	authenticator SessionAuthenticator
	authorizer    authz.Authorizer
	catalog       CatalogService
	torrentRead   TorrentReadService
	downloader    TorrentDownloadService
	attendance    AttendanceService
	publicOrigin  url.URL
	signingKey    []byte
	now           func() time.Time
	readRandom    func([]byte) (int, error)
	rateMu        sync.Mutex
	rateWindows   map[string]rateWindow
}

func NewService(
	repository Repository,
	authenticator SessionAuthenticator,
	authorizer authz.Authorizer,
	catalogService CatalogService,
	torrentRead TorrentReadService,
	downloader TorrentDownloadService,
	attendanceService AttendanceService,
	config ServiceConfig,
) (*Service, error) {
	if repository == nil || authenticator == nil || authorizer == nil || catalogService == nil || torrentRead == nil || downloader == nil || attendanceService == nil {
		return nil, errors.New("MoviePilot service dependencies are required")
	}
	origin, err := url.Parse(strings.TrimSpace(config.PublicOrigin))
	if err != nil || origin.Scheme == "" || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" ||
		(origin.Path != "" && origin.Path != "/") || (origin.Scheme != "http" && origin.Scheme != "https") {
		return nil, errors.New("MoviePilot public origin is invalid")
	}
	if len(config.SigningKey) < 32 {
		return nil, errors.New("MoviePilot signing key must contain at least 32 bytes")
	}
	origin.Path = ""
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.ReadRandom == nil {
		config.ReadRandom = rand.Read
	}
	return &Service{
		repository: repository, authenticator: authenticator, authorizer: authorizer,
		catalog: catalogService, torrentRead: torrentRead, downloader: downloader,
		attendance: attendanceService, publicOrigin: *origin,
		signingKey: append([]byte(nil), config.SigningKey...), now: config.Now,
		readRandom: config.ReadRandom, rateWindows: make(map[string]rateWindow),
	}, nil
}

func (service *Service) CredentialStatus(ctx context.Context, cookieToken string) (CredentialStatus, error) {
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return CredentialStatus{}, err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionIntegrationMoviePilotReadSelf, now); err != nil {
		return CredentialStatus{}, err
	}
	credential, err := service.repository.Credential(ctx, session.User.ID)
	if errors.Is(err, ErrCredentialNotFound) {
		return CredentialStatus{Active: false}, nil
	}
	if err != nil {
		return CredentialStatus{}, err
	}
	return credentialStatus(credential), nil
}

func (service *Service) RotateCredential(ctx context.Context, cookieToken, csrfToken string, expectedVersion *int64) (IssuedCredential, error) {
	if expectedVersion != nil && *expectedVersion < 1 {
		return IssuedCredential{}, ErrInput
	}
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return IssuedCredential{}, err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionIntegrationMoviePilotManageSelf, now); err != nil {
		return IssuedCredential{}, err
	}
	raw, digest, prefix, err := service.newAPIKey()
	if err != nil {
		return IssuedCredential{}, err
	}
	credential, err := service.repository.RotateCredential(ctx, session.User.ID, expectedVersion, digest, prefix, now)
	if err != nil {
		return IssuedCredential{}, err
	}
	return IssuedCredential{Credential: credentialStatus(credential), APIKey: raw}, nil
}

func (service *Service) RevokeCredential(ctx context.Context, cookieToken, csrfToken string, expectedVersion int64) error {
	if expectedVersion < 1 {
		return ErrInput
	}
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionIntegrationMoviePilotManageSelf, now); err != nil {
		return err
	}
	return service.repository.RevokeCredential(ctx, session.User.ID, expectedVersion)
}

func (service *Service) Authenticate(ctx context.Context, raw string) (AuthenticatedCredential, error) {
	digest, err := apiKeyDigest(raw)
	if err != nil {
		return AuthenticatedCredential{}, ErrCredentialInvalid
	}
	return service.repository.Authenticate(ctx, digest, service.now().UTC())
}

func (service *Service) Profile(ctx context.Context, credential AuthenticatedCredential) (Profile, error) {
	if !service.allow(credential.User.ID, "profile", 60) {
		return Profile{}, ErrRateLimited
	}
	profile, err := service.repository.Profile(ctx, credential.User.ID, service.now().UTC())
	if err != nil {
		return Profile{}, err
	}
	activity, err := service.torrentRead.UserTrackerActivityForIntegration(ctx, credential.User)
	if err != nil && !errors.Is(err, torrents.ErrManagedTorrentPeersUnavailable) {
		return Profile{}, err
	}
	if err == nil {
		for _, task := range activity.Items {
			if task.SeedingConnections > 0 {
				profile.SeedingCount++
				profile.SeedingSize = saturatingAddInt64(profile.SeedingSize, task.TotalSizeBytes)
			}
			if task.LeechingConnections > 0 {
				profile.LeechingCount++
				profile.LeechingSize = saturatingAddInt64(profile.LeechingSize, task.TotalSizeBytes)
			}
		}
	}
	return profile, nil
}

func (service *Service) ListTorrents(ctx context.Context, credential AuthenticatedCredential, page, pageSize int, keyword, categoryID string) (TorrentPage, error) {
	if !service.allow(credential.User.ID, "torrent-list", 120) {
		return TorrentPage{}, ErrRateLimited
	}
	if page < 1 || pageSize < 1 || pageSize > 100 || page > 1_000_001 || (page-1) > 1_000_000/pageSize {
		return TorrentPage{}, ErrInput
	}
	categoryID, valid := peerGoCategory(categoryID)
	if !valid {
		return TorrentPage{}, ErrInput
	}
	offset := (page - 1) * pageSize
	result, err := service.catalog.ListTorrents(ctx, catalog.TorrentListRequest{
		Limit: &pageSize, Offset: &offset, Query: keyword, CategoryID: categoryID,
	})
	if err != nil {
		return TorrentPage{}, err
	}
	items := make([]TorrentSummary, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, TorrentSummary{
			ID: item.ID, LegacyRouteID: strconv.FormatInt(item.ID, 10), Title: item.Name,
			Subtitle: item.Subtitle, Category: moviePilotCategory(item.Category.ID), CategoryName: item.Category.Name,
			Size: item.SizeBytes, Seeders: item.Swarm.Seeders, Leechers: item.Swarm.Leechers,
			Downloads: item.Swarm.Completed, CreatedAt: item.UploadedAt.UTC(),
			Promotion: promotion(item.Promotion, nil),
		})
	}
	totalPages := 0
	if result.Total > 0 {
		totalPages = (result.Total + pageSize - 1) / pageSize
	}
	return TorrentPage{Items: items, Total: result.Total, Page: page, PageSize: pageSize, TotalPages: totalPages}, nil
}

func (service *Service) Torrent(ctx context.Context, credential AuthenticatedCredential, torrentID int64) (TorrentDownloadDescriptor, error) {
	if torrentID < 1 {
		return TorrentDownloadDescriptor{}, ErrInput
	}
	if !service.allow(credential.User.ID, "torrent-detail", 60) {
		return TorrentDownloadDescriptor{}, ErrRateLimited
	}
	now := service.now().UTC()
	detail, err := service.torrentRead.Detail(ctx, torrents.TorrentID(torrentID))
	if err != nil {
		return TorrentDownloadDescriptor{}, err
	}
	swarm, err := service.catalog.GetTorrentSwarm(ctx, torrentID)
	if err != nil {
		return TorrentDownloadDescriptor{}, err
	}
	capability, err := service.issueDownloadCapability(credential.User.ID, torrentID, credential.Credential.Version, now)
	if err != nil {
		return TorrentDownloadDescriptor{}, err
	}
	downloadURL := service.publicOrigin
	downloadURL.Path = "/api/compat/moviepilot/v1/torrents/" + strconv.FormatInt(torrentID, 10) + "/download"
	query := downloadURL.Query()
	query.Set("capability", capability)
	downloadURL.RawQuery = query.Encode()
	return TorrentDownloadDescriptor{
		Detail: detail, Swarm: swarm, DownloadURL: downloadURL.String(),
		Promotion: promotion(detail.Promotion, detail.PromotionEndsAt),
	}, nil
}

func (service *Service) Download(ctx context.Context, torrentID int64, capability string) (torrents.TorrentDownloadResult, error) {
	userID, version, err := service.validateDownloadCapability(capability, torrentID)
	if err != nil {
		return torrents.TorrentDownloadResult{}, err
	}
	if !service.allow(userID, "torrent-download", 20) {
		return torrents.TorrentDownloadResult{}, ErrRateLimited
	}
	now := service.now().UTC()
	user, err := service.repository.ResolveCapabilityUser(ctx, userID, version, now)
	if err != nil {
		return torrents.TorrentDownloadResult{}, err
	}
	return service.downloader.DownloadForRSS(ctx, user, torrents.TorrentID(torrentID))
}

func (service *Service) AttendanceOverview(ctx context.Context, credential AuthenticatedCredential) (attendance.Overview, error) {
	if !service.allow(credential.User.ID, "attendance-read", 30) {
		return attendance.Overview{}, ErrRateLimited
	}
	return service.attendance.OverviewForIntegration(ctx, credential.User)
}

func (service *Service) ClaimAttendance(ctx context.Context, credential AuthenticatedCredential, mode attendance.Mode) (attendance.Record, error) {
	if !service.allow(credential.User.ID, "attendance-claim", 10) {
		return attendance.Record{}, ErrRateLimited
	}
	return service.attendance.ClaimForIntegration(ctx, credential.User, uuid.New(), mode)
}

func (service *Service) newAPIKey() (string, []byte, string, error) {
	randomBytes := make([]byte, rawAPIKeyBytes)
	n, err := service.readRandom(randomBytes)
	if err != nil {
		return "", nil, "", fmt.Errorf("generate MoviePilot API key: %w", err)
	}
	if n != len(randomBytes) {
		return "", nil, "", fmt.Errorf("generate MoviePilot API key: %w", io.ErrUnexpectedEOF)
	}
	raw := apiKeyPrefix + base64.RawURLEncoding.EncodeToString(randomBytes)
	digest := sha256.Sum256([]byte(raw))
	return raw, digest[:], raw[:12], nil
}

func apiKeyDigest(raw string) ([]byte, error) {
	if len(raw) != 47 || !strings.HasPrefix(raw, apiKeyPrefix) || strings.TrimSpace(raw) != raw {
		return nil, ErrCredentialInvalid
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(raw, apiKeyPrefix))
	if err != nil || len(decoded) != rawAPIKeyBytes {
		return nil, ErrCredentialInvalid
	}
	digest := sha256.Sum256([]byte(raw))
	return digest[:], nil
}

func credentialStatus(credential Credential) CredentialStatus {
	return CredentialStatus{
		Active: true, KeyPrefix: credential.KeyPrefix, Version: credential.Version,
		CreatedAt: credential.CreatedAt.UTC(), LastUsedAt: credential.LastUsedAt,
	}
}

func (service *Service) allow(userID uuid.UUID, operation string, maximum int) bool {
	now := service.now().UTC()
	minute := now.Truncate(time.Minute)
	key := userID.String() + ":" + operation
	service.rateMu.Lock()
	defer service.rateMu.Unlock()
	window := service.rateWindows[key]
	if !window.minute.Equal(minute) {
		window = rateWindow{minute: minute}
	}
	if window.count >= maximum {
		service.rateWindows[key] = window
		return false
	}
	window.count++
	service.rateWindows[key] = window
	if len(service.rateWindows) > 10_000 {
		cutoff := minute.Add(-time.Minute)
		for existingKey, existing := range service.rateWindows {
			if existing.minute.Before(cutoff) {
				delete(service.rateWindows, existingKey)
			}
		}
	}
	return true
}

func (service *Service) issueDownloadCapability(userID uuid.UUID, torrentID, version int64, now time.Time) (string, error) {
	if userID == uuid.Nil || torrentID < 1 || version < 1 {
		return "", ErrInput
	}
	payload := strings.Join([]string{
		"1", userID.String(), strconv.FormatInt(torrentID, 10),
		strconv.FormatInt(now.Add(downloadCapabilityTTL).Unix(), 10), strconv.FormatInt(version, 10),
	}, ":")
	signature := service.signCapability(payload)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (service *Service) validateDownloadCapability(raw string, requestedTorrentID int64) (uuid.UUID, int64, error) {
	if requestedTorrentID < 1 || len(raw) < 32 || len(raw) > 512 || strings.TrimSpace(raw) != raw {
		return uuid.Nil, 0, ErrCapabilityInvalid
	}
	encodedPayload, encodedSignature, found := strings.Cut(raw, ".")
	if !found || strings.Contains(encodedSignature, ".") {
		return uuid.Nil, 0, ErrCapabilityInvalid
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return uuid.Nil, 0, ErrCapabilityInvalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(encodedSignature)
	if err != nil || !hmac.Equal(signature, service.signCapability(string(payloadBytes))) {
		return uuid.Nil, 0, ErrCapabilityInvalid
	}
	parts := strings.Split(string(payloadBytes), ":")
	if len(parts) != 5 || parts[0] != "1" {
		return uuid.Nil, 0, ErrCapabilityInvalid
	}
	userID, err := uuid.Parse(parts[1])
	if err != nil || userID == uuid.Nil {
		return uuid.Nil, 0, ErrCapabilityInvalid
	}
	torrentID, torrentErr := strconv.ParseInt(parts[2], 10, 64)
	expiresAt, expiryErr := strconv.ParseInt(parts[3], 10, 64)
	version, versionErr := strconv.ParseInt(parts[4], 10, 64)
	now := service.now().UTC()
	if torrentErr != nil || expiryErr != nil || versionErr != nil || torrentID != requestedTorrentID || version < 1 ||
		expiresAt <= now.Unix() || expiresAt > now.Add(downloadCapabilityTTL+time.Minute).Unix() {
		return uuid.Nil, 0, ErrCapabilityInvalid
	}
	return userID, version, nil
}

func (service *Service) signCapability(payload string) []byte {
	mac := hmac.New(sha256.New, service.signingKey)
	_, _ = mac.Write([]byte("peergo:moviepilot:download-capability:v1\x00"))
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}

func peerGoCategory(value string) (string, bool) {
	value = strings.TrimSpace(strings.ToLower(value))
	switch value {
	case "":
		return "", true
	case "movie":
		return "movies", true
	case "animation":
		return "anime", true
	case "game":
		return "games", true
	case "ebook":
		return "ebooks", true
	case "movies", "tv", "documentary", "anime", "variety", "sports", "music", "games", "software", "ebooks", "9kg", "other":
		return value, true
	default:
		return "", false
	}
}

func moviePilotCategory(value string) string {
	switch value {
	case "movies":
		return "movie"
	case "anime":
		return "animation"
	case "games":
		return "game"
	case "ebooks":
		return "ebook"
	default:
		return value
	}
}

func promotion(value catalog.Promotion, until *time.Time) TorrentPromotion {
	result := TorrentPromotion{Type: 1, TimeType: 1, Active: value != catalog.PromotionNone, UploadFactor: 1, DownloadFactor: 1}
	if until != nil {
		resolved := until.UTC()
		result.Until = &resolved
		result.TimeType = 2
	}
	switch value {
	case catalog.PromotionFree:
		result.Type, result.DownloadFactor = 2, 0
	case catalog.PromotionDoubleUpload:
		result.Type, result.UploadFactor = 3, 2
	case catalog.PromotionDoubleUploadFree:
		result.Type, result.UploadFactor, result.DownloadFactor = 4, 2, 0
	case catalog.PromotionHalfDownload:
		result.Type, result.DownloadFactor = 5, 0.5
	case catalog.PromotionDoubleUploadHalfDownload:
		result.Type, result.UploadFactor, result.DownloadFactor = 6, 2, 0.5
	case catalog.PromotionThirtyPercentDownload:
		result.Type, result.DownloadFactor = 7, 0.3
	}
	return result
}

func saturatingAddInt64(current, next int64) int64 {
	if next <= 0 {
		return current
	}
	if current > math.MaxInt64-next {
		return math.MaxInt64
	}
	return current + next
}
