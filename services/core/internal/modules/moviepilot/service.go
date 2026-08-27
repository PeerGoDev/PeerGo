package moviepilot

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"math"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/catalog"
	"github.com/peergo/peergo/services/core/internal/modules/economy/attendance"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/personalapikey"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

const (
	downloadCapabilityTTL = 5 * time.Minute
)

type APIKeyService interface {
	ResolveActiveUser(context.Context, uuid.UUID, int64, personalapikey.Scope) (identity.User, error)
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
}

type rateWindow struct {
	minute time.Time
	count  int
}

// Service is a deliberately narrow compatibility adapter for external Rousi
// clients. It projects canonical PeerGo reads directly; shared API-key
// lifecycle and authentication are owned by personalapikey. Searches,
// downloads and attendance calls create no API log or copied record in
// PostgreSQL.
type Service struct {
	repository   Repository
	apiKeys      APIKeyService
	catalog      CatalogService
	torrentRead  TorrentReadService
	downloader   TorrentDownloadService
	attendance   AttendanceService
	legacy       LegacyServices
	publicOrigin url.URL
	signingKey   []byte
	now          func() time.Time
	rateMu       sync.Mutex
	rateWindows  map[string]rateWindow
}

func NewService(
	repository Repository,
	apiKeys APIKeyService,
	catalogService CatalogService,
	torrentRead TorrentReadService,
	downloader TorrentDownloadService,
	attendanceService AttendanceService,
	config ServiceConfig,
	legacy ...LegacyServices,
) (*Service, error) {
	if repository == nil || apiKeys == nil || catalogService == nil || torrentRead == nil || downloader == nil || attendanceService == nil {
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
	legacyServices := LegacyServices{}
	if len(legacy) > 1 {
		return nil, errors.New("only one legacy API dependency set may be configured")
	}
	if len(legacy) == 1 {
		legacyServices = legacy[0]
	}
	return &Service{
		repository: repository, apiKeys: apiKeys,
		catalog: catalogService, torrentRead: torrentRead, downloader: downloader,
		attendance: attendanceService, legacy: legacyServices, publicOrigin: *origin,
		signingKey: append([]byte(nil), config.SigningKey...), now: config.Now,
		rateWindows: make(map[string]rateWindow),
	}, nil
}

func (service *Service) Profile(ctx context.Context, credential personalapikey.AuthenticatedCredential) (Profile, error) {
	if err := personalapikey.RequireScope(credential, personalapikey.ScopeProfileRead); err != nil {
		return Profile{}, err
	}
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

// SeedingReward returns the latest completed hourly settlement used by
// PT-depiler's bonusPerHour field. It never recomputes or stores a polling-time
// estimate, keeping the integration read-only and storage-bounded.
func (service *Service) SeedingReward(ctx context.Context, credential personalapikey.AuthenticatedCredential) (int64, error) {
	if err := personalapikey.RequireScope(credential, personalapikey.ScopeProfileRead); err != nil {
		return 0, err
	}
	if !service.allow(credential.User.ID, "seeding-reward", 30) {
		return 0, ErrRateLimited
	}
	return service.repository.LatestSeedingReward(ctx, credential.User.ID)
}

func (service *Service) ListTorrents(ctx context.Context, credential personalapikey.AuthenticatedCredential, page, pageSize int, keyword, categoryID string) (TorrentPage, error) {
	if err := personalapikey.RequireScope(credential, personalapikey.ScopeTorrentRead); err != nil {
		return TorrentPage{}, err
	}
	if !service.allow(credential.User.ID, "torrent-list", 120) {
		return TorrentPage{}, ErrRateLimited
	}
	if page < 1 || pageSize < 1 || pageSize > 100 || page > 1_000_001 || (page-1) > 1_000_000/pageSize {
		return TorrentPage{}, ErrInput
	}
	rawCategoryID := categoryID
	categoryID, valid := peerGoCategory(rawCategoryID)
	if !valid && service.legacy.Catalog != nil {
		candidate := strings.ToLower(strings.TrimSpace(rawCategoryID))
		if legacyCategoryPattern.MatchString(candidate) {
			categories, err := service.legacy.Catalog.ListCategories(ctx)
			if err != nil {
				return TorrentPage{}, err
			}
			for _, category := range categories {
				if candidate == category.ID {
					categoryID, valid = category.ID, true
					break
				}
			}
		}
	}
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
	metadata := make(map[int64]TorrentMetadata)
	if repository, ok := service.repository.(LegacyRepository); ok {
		ids := make([]int64, 0, len(result.Items))
		for _, item := range result.Items {
			ids = append(ids, item.ID)
		}
		metadata, err = repository.TorrentMetadataBatch(ctx, ids)
		if err != nil {
			return TorrentPage{}, err
		}
	}
	items := make([]TorrentSummary, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, legacyTorrentSummary(item, metadata[item.ID]))
	}
	totalPages := 0
	if result.Total > 0 {
		totalPages = (result.Total + pageSize - 1) / pageSize
	}
	return TorrentPage{Items: items, Total: result.Total, Page: page, PageSize: pageSize, TotalPages: totalPages}, nil
}

func (service *Service) Torrent(ctx context.Context, credential personalapikey.AuthenticatedCredential, torrentID int64) (TorrentDownloadDescriptor, error) {
	if err := personalapikey.RequireScope(credential, personalapikey.ScopeTorrentRead); err != nil {
		return TorrentDownloadDescriptor{}, err
	}
	if err := personalapikey.RequireScope(credential, personalapikey.ScopeTorrentDownload); err != nil {
		return TorrentDownloadDescriptor{}, err
	}
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
	user, err := service.apiKeys.ResolveActiveUser(ctx, userID, version, personalapikey.ScopeTorrentDownload)
	if err != nil {
		return torrents.TorrentDownloadResult{}, err
	}
	return service.downloader.DownloadForRSS(ctx, user, torrents.TorrentID(torrentID))
}

// DownloadWithCredential supports PT-depiler's fixed legacy download route.
// Authentication has already resolved the shared personal API key; this method
// applies the same scope, rate, purchase, restriction and metainfo pipeline as
// every other external download.
func (service *Service) DownloadWithCredential(ctx context.Context, credential personalapikey.AuthenticatedCredential, torrentID int64) (torrents.TorrentDownloadResult, error) {
	if err := personalapikey.RequireScope(credential, personalapikey.ScopeTorrentDownload); err != nil {
		return torrents.TorrentDownloadResult{}, err
	}
	if credential.User.ID == uuid.Nil || torrentID < 1 {
		return torrents.TorrentDownloadResult{}, ErrInput
	}
	if !service.allow(credential.User.ID, "torrent-download", 20) {
		return torrents.TorrentDownloadResult{}, ErrRateLimited
	}
	return service.downloader.DownloadForRSS(ctx, credential.User, torrents.TorrentID(torrentID))
}

func (service *Service) AttendanceOverview(ctx context.Context, credential personalapikey.AuthenticatedCredential) (attendance.Overview, error) {
	if err := personalapikey.RequireScope(credential, personalapikey.ScopeAttendanceRead); err != nil {
		return attendance.Overview{}, err
	}
	if !service.allow(credential.User.ID, "attendance-read", 30) {
		return attendance.Overview{}, ErrRateLimited
	}
	return service.attendance.OverviewForIntegration(ctx, credential.User)
}

func (service *Service) ClaimAttendance(ctx context.Context, credential personalapikey.AuthenticatedCredential, mode attendance.Mode) (attendance.Record, error) {
	if err := personalapikey.RequireScope(credential, personalapikey.ScopeAttendanceClaim); err != nil {
		return attendance.Record{}, err
	}
	if !service.allow(credential.User.ID, "attendance-claim", 10) {
		return attendance.Record{}, ErrRateLimited
	}
	return service.attendance.ClaimForIntegration(ctx, credential.User, uuid.New(), mode)
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
