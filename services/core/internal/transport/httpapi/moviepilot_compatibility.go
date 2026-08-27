package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
	"github.com/peergo/peergo/services/core/internal/modules/economy"
	"github.com/peergo/peergo/services/core/internal/modules/economy/attendance"
	"github.com/peergo/peergo/services/core/internal/modules/economy/torrentpurchase"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/moviepilot"
	"github.com/peergo/peergo/services/core/internal/modules/personalapikey"
	"github.com/peergo/peergo/services/core/internal/modules/social"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

const moviePilotAttendanceBodyLimit = 4 << 10

const (
	legacyAPIUploadBodyLimit = 48 << 20
	legacyMetainfoByteLimit  = 10 << 20
)

type moviePilotResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// legacyAPIService is kept separate from MoviePilotService so older narrow
// fixtures and adapters do not gain implicit write authority. Production's
// moviepilot.Service implements both surfaces.
type legacyAPIService interface {
	PublicProfile(context.Context, personalapikey.AuthenticatedCredential, string) (moviepilot.Profile, error)
	Categories(context.Context, personalapikey.AuthenticatedCredential) ([]moviepilot.LegacyCategory, error)
	Upload(context.Context, personalapikey.AuthenticatedCredential, moviepilot.LegacyUploadInput) (moviepilot.LegacyUploadResult, error)
	LegacyTorrent(context.Context, personalapikey.AuthenticatedCredential, string) (moviepilot.LegacyTorrentDetail, error)
	Comments(context.Context, personalapikey.AuthenticatedCredential, string, int, int) (moviepilot.LegacyCommentPage, error)
	Bookmarks(context.Context, personalapikey.AuthenticatedCredential, int, int) (moviepilot.LegacyBookmarkPage, error)
	PurchaseStatus(context.Context, personalapikey.AuthenticatedCredential, string) (torrentpurchase.Status, error)
	Purchase(context.Context, personalapikey.AuthenticatedCredential, string, uuid.UUID, *int64) (torrentpurchase.Receipt, error)
	Purchases(context.Context, personalapikey.AuthenticatedCredential, int, int) (torrentpurchase.HistoryPage, error)
}

// MoviePilotCompatibility intercepts the legacy Rousi API used by MoviePilot
// and PT-depiler, and only claims colliding torrent routes when an external
// Authorization header is present. Ordinary PeerGo browser calls continue
// through OpenAPI validation. It must be mounted after private response headers
// and before same-origin enforcement because API-key clients are not
// browser-cookie audiences.
func MoviePilotCompatibility(apiKeys PersonalAPIKeyService, service MoviePilotService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if apiKeys == nil || service == nil {
				next.ServeHTTP(w, r)
				return
			}
			legacy, legacyAvailable := service.(legacyAPIService)
			torrentRouteID, torrentAction, torrentRoute := legacyTorrentRoute(r.URL.Path)
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/v1/profile":
				handleMoviePilotProfile(apiKeys, service, w, r)
				return
			case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/profile/") && legacyAvailable && hasMoviePilotCredentialHeader(r):
				handleLegacyPublicProfile(apiKeys, legacy, w, r)
				return
			case r.Method == http.MethodGet && r.URL.Path == "/api/v1/search":
				handleMoviePilotTorrentList(apiKeys, service, w, r)
				return
			case r.Method == http.MethodGet && r.URL.Path == "/api/v1/seeding-reward":
				handlePTDepilerSeedingReward(apiKeys, service, w, r)
				return
			case r.Method == http.MethodGet && r.URL.Path == "/api/v1/torrents" && hasMoviePilotCredentialHeader(r):
				handleMoviePilotTorrentList(apiKeys, service, w, r)
				return
			case r.Method == http.MethodPost && r.URL.Path == "/api/v1/torrents" && legacyAvailable && hasMoviePilotCredentialHeader(r):
				handleLegacyTorrentUpload(apiKeys, legacy, w, r)
				return
			case r.Method == http.MethodGet && r.URL.Path == "/api/v1/categories" && legacyAvailable && hasMoviePilotCredentialHeader(r):
				handleLegacyCategories(apiKeys, legacy, w, r)
				return
			case r.Method == http.MethodGet && r.URL.Path == "/api/v1/bookmarks" && legacyAvailable && hasMoviePilotCredentialHeader(r):
				handleLegacyBookmarks(apiKeys, legacy, w, r)
				return
			case r.Method == http.MethodGet && r.URL.Path == "/api/v1/purchases" && legacyAvailable && hasMoviePilotCredentialHeader(r):
				handleLegacyPurchases(apiKeys, legacy, w, r)
				return
			case r.Method == http.MethodGet && torrentRoute && torrentAction == "comments" && legacyAvailable && hasMoviePilotCredentialHeader(r):
				handleLegacyTorrentComments(apiKeys, legacy, torrentRouteID, w, r)
				return
			case r.Method == http.MethodGet && torrentRoute && torrentAction == "purchase" && legacyAvailable && hasMoviePilotCredentialHeader(r):
				handleLegacyPurchaseStatus(apiKeys, legacy, torrentRouteID, w, r)
				return
			case r.Method == http.MethodPost && torrentRoute && torrentAction == "purchase" && legacyAvailable && hasMoviePilotCredentialHeader(r):
				handleLegacyPurchase(apiKeys, legacy, torrentRouteID, w, r)
				return
			case r.Method == http.MethodGet && torrentRoute && torrentAction == "" && legacyAvailable && hasMoviePilotCredentialHeader(r):
				handleLegacyTorrent(apiKeys, legacy, torrentRouteID, w, r)
				return
			case r.Method == http.MethodGet && moviePilotTorrentID(r.URL.Path) > 0 && hasMoviePilotCredentialHeader(r):
				handleMoviePilotTorrent(apiKeys, service, w, r)
				return
			case r.Method == http.MethodPost && r.URL.Path == "/api/points/attendance":
				handleMoviePilotAttendanceClaim(apiKeys, service, w, r)
				return
			case r.Method == http.MethodGet && r.URL.Path == "/api/points/attendance/stats":
				handleMoviePilotAttendanceStats(apiKeys, service, w, r)
				return
			case r.Method == http.MethodGet && moviePilotDownloadTorrentID(r.URL.Path) > 0:
				handleMoviePilotDownload(service, w, r)
				return
			default:
				if torrentID, rawCredential, ok := ptDepilerDownloadCredential(r); ok {
					handlePTDepilerDownload(apiKeys, service, torrentID, rawCredential, w, r)
					return
				}
				next.ServeHTTP(w, r)
			}
		})
	}
}

func handleMoviePilotProfile(apiKeys PersonalAPIKeyService, service MoviePilotService, w http.ResponseWriter, r *http.Request) {
	credential, ok := authenticateMoviePilot(apiKeys, w, r)
	if !ok {
		return
	}
	profile, err := service.Profile(r.Context(), credential)
	if writeMoviePilotServiceError(w, err) {
		return
	}
	ratio := 0.0
	if profile.Downloaded > 0 {
		ratio = float64(profile.Uploaded) / float64(profile.Downloaded)
	} else if profile.Uploaded > 0 {
		ratio = 999999
	}
	writeMoviePilotJSON(w, http.StatusOK, moviePilotResponse{Code: 0, Message: "success", Data: map[string]any{
		"id": profile.NumericID, "username": profile.Username, "display_name": profile.DisplayName,
		"level": profile.Level, "level_text": fmt.Sprintf("Lv.%d", profile.Level), "registered_at": profile.RegisteredAt,
		"last_active_at": profile.LastActiveAt, "uploaded": profile.Uploaded, "downloaded": profile.Downloaded,
		"ratio": ratio, "karma": profile.Magic, "experience": profile.Experience,
		"email_verified": profile.EmailVerified, "vip": profile.VIP, "vip_until": profile.VIPUntil,
		"seeding_leeching_data": map[string]any{
			"seeding_count": profile.SeedingCount, "seeding_size": profile.SeedingSize,
			"leeching_count": profile.LeechingCount, "leeching_size": profile.LeechingSize,
		},
	}})
}

func handleLegacyPublicProfile(apiKeys PersonalAPIKeyService, service legacyAPIService, w http.ResponseWriter, r *http.Request) {
	credential, ok := authenticateMoviePilot(apiKeys, w, r)
	if !ok {
		return
	}
	username := strings.TrimPrefix(r.URL.Path, "/api/v1/profile/")
	if username == "" || strings.Contains(username, "/") {
		writeMoviePilotJSON(w, http.StatusBadRequest, moviePilotResponse{Code: 400, Message: "用户名无效"})
		return
	}
	profile, err := service.PublicProfile(r.Context(), credential, username)
	if writeMoviePilotServiceError(w, err) {
		return
	}
	ratio := legacyRatio(profile.Uploaded, profile.Downloaded)
	writeMoviePilotJSON(w, http.StatusOK, moviePilotResponse{Code: 0, Message: "success", Data: map[string]any{
		"id": profile.NumericID, "username": profile.Username, "nickname": profile.DisplayName,
		"avatar": "", "role": "user", "role_text": "用户", "level": profile.Level,
		"uploaded": profile.Uploaded, "downloaded": profile.Downloaded, "ratio": ratio,
		"registered_at": profile.RegisteredAt, "is_vip": profile.VIP,
	}})
}

func handleLegacyCategories(apiKeys PersonalAPIKeyService, service legacyAPIService, w http.ResponseWriter, r *http.Request) {
	credential, ok := authenticateMoviePilot(apiKeys, w, r)
	if !ok {
		return
	}
	categories, err := service.Categories(r.Context(), credential)
	if writeMoviePilotServiceError(w, err) {
		return
	}
	data := make([]map[string]any, 0, len(categories))
	for _, category := range categories {
		attributes := make([]map[string]any, 0, len(category.Attributes))
		for _, attribute := range category.Attributes {
			options := make([]map[string]any, 0, len(attribute.Options))
			for _, option := range attribute.Options {
				options = append(options, map[string]any{"value": option.Value, "label": option.Label})
			}
			attributes = append(attributes, map[string]any{
				"name": attribute.Name, "label": attribute.Label, "type": attribute.Type,
				"required": attribute.Required, "options": options,
			})
		}
		data = append(data, map[string]any{
			"id": category.ID, "name": category.Name, "label": category.Label,
			"icon": category.Icon, "attributes": attributes,
		})
	}
	writeMoviePilotJSON(w, http.StatusOK, moviePilotResponse{Code: 0, Message: "success", Data: data})
}

func handleLegacyTorrentUpload(apiKeys PersonalAPIKeyService, service legacyAPIService, w http.ResponseWriter, r *http.Request) {
	credential, ok := authenticateMoviePilot(apiKeys, w, r)
	if !ok {
		return
	}
	var request struct {
		Torrent     string                     `json:"torrent"`
		Title       string                     `json:"title"`
		Subtitle    string                     `json:"subtitle"`
		Description string                     `json:"description"`
		Category    string                     `json:"category"`
		Attributes  map[string]json.RawMessage `json:"attributes"`
		Tags        string                     `json:"tags"`
		MediaInfo   string                     `json:"media_info"`
		Images      []string                   `json:"images"`
		Anonymous   bool                       `json:"anonymous"`
		Price       json.Number                `json:"price"`
		IMDB        string                     `json:"imdb"`
		IMDBID      string                     `json:"imdb_id"`
		TMDB        string                     `json:"tmdb"`
		TMDBID      string                     `json:"tmdb_id"`
		Douban      string                     `json:"douban"`
		DoubanID    string                     `json:"douban_id"`
	}
	if err := decodeLegacyJSON(w, r, legacyAPIUploadBodyLimit, &request, false); err != nil {
		writeMoviePilotJSON(w, http.StatusBadRequest, moviePilotResponse{Code: 4001, Message: "上传参数错误"})
		return
	}
	if len(request.Images) > torrents.MaxTorrentScreenshots {
		writeMoviePilotJSON(w, http.StatusBadRequest, moviePilotResponse{Code: 4002, Message: "图片数量超过限制"})
		return
	}
	metainfo, err := decodeLegacyBase64(request.Torrent, legacyMetainfoByteLimit, false)
	if err != nil {
		writeMoviePilotJSON(w, http.StatusBadRequest, moviePilotResponse{Code: 4001, Message: "种子文件编码无效"})
		return
	}
	screenshots := make([]torrents.TorrentScreenshotInput, 0, len(request.Images))
	for _, encoded := range request.Images {
		decoded, err := decodeLegacyBase64(encoded, torrents.MaxTorrentScreenshotBytes, true)
		if err != nil {
			writeMoviePilotJSON(w, http.StatusBadRequest, moviePilotResponse{Code: 4006, Message: "图片编码或大小不符合当前站点策略"})
			return
		}
		screenshots = append(screenshots, torrents.TorrentScreenshotInput{Raw: decoded})
	}
	attributes, err := decodeLegacyAttributes(request.Attributes)
	if err != nil {
		writeMoviePilotJSON(w, http.StatusBadRequest, moviePilotResponse{Code: 4008, Message: "分类属性无效"})
		return
	}
	price, err := legacyOptionalInteger(request.Price, 0, 1_000_000)
	if err != nil {
		writeMoviePilotJSON(w, http.StatusBadRequest, moviePilotResponse{Code: 4001, Message: "种子价格无效"})
		return
	}
	requestID := uuid.New()
	if rawID := strings.TrimSpace(r.Header.Get("Idempotency-Key")); rawID != "" {
		requestID, err = uuid.Parse(rawID)
		if err != nil || requestID == uuid.Nil {
			writeMoviePilotJSON(w, http.StatusBadRequest, moviePilotResponse{Code: 4001, Message: "Idempotency-Key 必须是 UUID"})
			return
		}
	}
	identifiers := legacyExternalIdentifiers(request.IMDB, request.IMDBID, request.TMDB, request.TMDBID, request.Douban, request.DoubanID)
	result, err := service.Upload(r.Context(), credential, moviepilot.LegacyUploadInput{
		RequestID: requestID, Category: request.Category, Title: request.Title,
		Subtitle: request.Subtitle, Description: request.Description, MediaInfo: request.MediaInfo,
		Anonymous: request.Anonymous, PurchasePrice: price, Attributes: attributes,
		ExternalIdentifiers: identifiers, Screenshots: screenshots, RawMetainfo: metainfo,
	})
	if writeLegacyUploadError(w, err) {
		return
	}
	writeMoviePilotJSON(w, http.StatusCreated, moviePilotResponse{Code: 0, Message: "success", Data: map[string]any{
		"id": result.ID, "uuid": result.RouteID, "info_hash": result.InfoHash, "status": result.Status,
	}})
}

func handleLegacyTorrent(apiKeys PersonalAPIKeyService, service legacyAPIService, routeID string, w http.ResponseWriter, r *http.Request) {
	credential, ok := authenticateMoviePilot(apiKeys, w, r)
	if !ok {
		return
	}
	result, err := service.LegacyTorrent(r.Context(), credential, routeID)
	if writeMoviePilotServiceError(w, err) {
		return
	}
	detail := result.Detail
	files := make([]map[string]any, 0, len(result.Files))
	if result.CanReadObject {
		for _, file := range result.Files {
			files = append(files, map[string]any{"id": file.Index + 1, "path": file.DisplayPath, "size": file.SizeBytes})
		}
	}
	images := make([]map[string]any, 0, len(result.ImageURLs))
	for position, imageURL := range result.ImageURLs {
		images = append(images, map[string]any{"url": imageURL, "is_cover": position == 0})
	}
	otherVersions := make([]map[string]any, 0, len(result.Related))
	for _, related := range result.Related {
		otherVersions = append(otherVersions, map[string]any{
			"id": related.ID, "uuid": strconv.FormatInt(related.ID, 10), "title": related.Name,
			"subtitle": related.Subtitle, "size": related.SizeBytes,
			"seeders": related.Swarm.Seeders, "leechers": related.Swarm.Leechers,
			"created_at": related.UploadedAt, "attributes": map[string]any{},
		})
	}
	infoHash := ""
	downloadURL := ""
	if result.CanReadObject {
		infoHash = detail.InfoHashV1.Hex()
		downloadURL = result.DownloadURL
	}
	writeMoviePilotJSON(w, http.StatusOK, moviePilotResponse{Code: 0, Message: "success", Data: map[string]any{
		"id": int64(detail.ID), "uuid": result.RouteID, "title": detail.Title, "subtitle": detail.Subtitle,
		"description": result.Content.Description, "category": moviePilotCategoryName(detail.Category.ID),
		"category_name": detail.Category.Name, "size": detail.TotalSizeBytes,
		"seeders": result.Swarm.Seeders, "leechers": result.Swarm.Leechers, "downloads": result.Swarm.Completed,
		"uploader": result.Metadata.Uploader, "uploader_id": result.Metadata.UploaderID,
		"anonymous": result.Metadata.Anonymous, "created_at": detail.PublishedAt,
		"info_hash": infoHash, "files": files, "images": images, "media_info": result.Content.MediaInfo,
		"attributes": result.Attributes, "download_url": downloadURL,
		"price": result.Purchase.Price, "is_purchased": result.CanReadObject,
		"other_versions": otherVersions, "promotion": moviePilotPromotionDTO(result.Promotion),
	}})
}

func handleLegacyTorrentComments(apiKeys PersonalAPIKeyService, service legacyAPIService, routeID string, w http.ResponseWriter, r *http.Request) {
	credential, ok := authenticateMoviePilot(apiKeys, w, r)
	if !ok {
		return
	}
	page, validPage := positiveQueryInteger(r, "page", 1)
	pageSize, validPageSize := positiveQueryInteger(r, "page_size", 20)
	if !validPage || !validPageSize {
		writeMoviePilotJSON(w, http.StatusBadRequest, moviePilotResponse{Code: 400, Message: "分页参数无效"})
		return
	}
	result, err := service.Comments(r.Context(), credential, routeID, page, pageSize)
	if writeMoviePilotServiceError(w, err) {
		return
	}
	items := make([]map[string]any, 0, len(result.Items))
	for _, comment := range result.Items {
		items = append(items, map[string]any{
			"id": comment.ID, "content": comment.Body, "user_id": comment.UserID,
			"username": comment.Username, "avatar": "", "created_at": comment.CreatedAt,
		})
	}
	writeMoviePilotJSON(w, http.StatusOK, moviePilotResponse{Code: 0, Message: "success", Data: map[string]any{
		"comments": items, "total": result.Total, "page": result.Page,
		"page_size": result.PageSize, "total_pages": result.TotalPages,
	}})
}

func handleLegacyBookmarks(apiKeys PersonalAPIKeyService, service legacyAPIService, w http.ResponseWriter, r *http.Request) {
	credential, ok := authenticateMoviePilot(apiKeys, w, r)
	if !ok {
		return
	}
	page, validPage := positiveQueryInteger(r, "page", 1)
	pageSize, validPageSize := positiveQueryInteger(r, "page_size", 20)
	if !validPage || !validPageSize {
		writeMoviePilotJSON(w, http.StatusBadRequest, moviePilotResponse{Code: 400, Message: "分页参数无效"})
		return
	}
	result, err := service.Bookmarks(r.Context(), credential, page, pageSize)
	if writeMoviePilotServiceError(w, err) {
		return
	}
	items := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, legacyTorrentSummaryDTO(item))
	}
	writeMoviePilotJSON(w, http.StatusOK, moviePilotResponse{Code: 0, Message: "success", Data: map[string]any{
		"torrents": items, "total": result.Total, "page": result.Page,
		"page_size": result.PageSize, "total_pages": result.TotalPages,
	}})
}

func handleLegacyPurchaseStatus(apiKeys PersonalAPIKeyService, service legacyAPIService, routeID string, w http.ResponseWriter, r *http.Request) {
	credential, ok := authenticateMoviePilot(apiKeys, w, r)
	if !ok {
		return
	}
	status, err := service.PurchaseStatus(r.Context(), credential, routeID)
	if writeMoviePilotServiceError(w, err) {
		return
	}
	writeMoviePilotJSON(w, http.StatusOK, moviePilotResponse{Code: 0, Message: "success", Data: legacyPurchaseStatusDTO(status)})
}

func handleLegacyPurchase(apiKeys PersonalAPIKeyService, service legacyAPIService, routeID string, w http.ResponseWriter, r *http.Request) {
	credential, ok := authenticateMoviePilot(apiKeys, w, r)
	if !ok {
		return
	}
	requestID, err := uuid.Parse(strings.TrimSpace(r.Header.Get("Idempotency-Key")))
	if err != nil || requestID == uuid.Nil {
		writeMoviePilotJSON(w, http.StatusBadRequest, moviePilotResponse{Code: 400, Message: "购买必须提供 UUID 格式的 Idempotency-Key"})
		return
	}
	var request struct {
		ExpectedPrice json.Number `json:"expected_price"`
	}
	if err := decodeLegacyJSON(w, r, moviePilotAttendanceBodyLimit, &request, true); err != nil {
		writeMoviePilotJSON(w, http.StatusBadRequest, moviePilotResponse{Code: 400, Message: "购买参数无效"})
		return
	}
	var expectedPrice *int64
	if request.ExpectedPrice != "" {
		value, err := legacyOptionalInteger(request.ExpectedPrice, 0, 1_000_000)
		if err != nil {
			writeMoviePilotJSON(w, http.StatusBadRequest, moviePilotResponse{Code: 400, Message: "预期价格无效"})
			return
		}
		expectedPrice = &value
	}
	receipt, err := service.Purchase(r.Context(), credential, routeID, requestID, expectedPrice)
	if writeMoviePilotServiceError(w, err) {
		return
	}
	writeMoviePilotJSON(w, http.StatusOK, moviePilotResponse{Code: 0, Message: "success", Data: map[string]any{
		"request_id": receipt.RequestID, "torrent_id": receipt.TorrentID,
		"price": receipt.Price, "tax": receipt.Tax, "seller_income": receipt.SellerIncome,
		"balance_after": receipt.BalanceAfter, "purchased_at": receipt.PurchasedAt, "replayed": receipt.Replayed,
	}})
}

func handleLegacyPurchases(apiKeys PersonalAPIKeyService, service legacyAPIService, w http.ResponseWriter, r *http.Request) {
	credential, ok := authenticateMoviePilot(apiKeys, w, r)
	if !ok {
		return
	}
	page, validPage := positiveQueryInteger(r, "page", 1)
	pageSize, validPageSize := positiveQueryInteger(r, "page_size", torrentpurchase.DefaultHistoryLimit)
	if !validPage || !validPageSize {
		writeMoviePilotJSON(w, http.StatusBadRequest, moviePilotResponse{Code: 400, Message: "分页参数无效"})
		return
	}
	result, err := service.Purchases(r.Context(), credential, page, pageSize)
	if writeMoviePilotServiceError(w, err) {
		return
	}
	items := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, map[string]any{
			"torrent_id": item.TorrentID, "title": item.Title, "category_name": item.CategoryName,
			"torrent_state": item.TorrentState, "price": item.Price,
			"purchased_at": item.PurchasedAt, "legacy_import": item.LegacyImport,
		})
	}
	totalPages := int64(0)
	if result.Total > 0 {
		totalPages = (result.Total + int64(pageSize) - 1) / int64(pageSize)
	}
	writeMoviePilotJSON(w, http.StatusOK, moviePilotResponse{Code: 0, Message: "success", Data: map[string]any{
		"purchases": items, "total": result.Total, "page": page,
		"page_size": pageSize, "total_pages": totalPages,
	}})
}

func handlePTDepilerSeedingReward(apiKeys PersonalAPIKeyService, service MoviePilotService, w http.ResponseWriter, r *http.Request) {
	credential, ok := authenticateMoviePilot(apiKeys, w, r)
	if !ok {
		return
	}
	reward, err := service.SeedingReward(r.Context(), credential)
	if writeMoviePilotServiceError(w, err) {
		return
	}
	writeMoviePilotJSON(w, http.StatusOK, moviePilotResponse{Code: 0, Message: "success", Data: map[string]any{
		"total_reward": reward,
	}})
}

func handleMoviePilotTorrentList(apiKeys PersonalAPIKeyService, service MoviePilotService, w http.ResponseWriter, r *http.Request) {
	credential, ok := authenticateMoviePilot(apiKeys, w, r)
	if !ok {
		return
	}
	page, validPage := positiveQueryInteger(r, "page", 1)
	pageSize, validPageSize := positiveQueryInteger(r, "page_size", 100)
	if !validPage || !validPageSize {
		writeMoviePilotJSON(w, http.StatusBadRequest, moviePilotResponse{Code: 400, Message: "分页参数无效"})
		return
	}
	result, err := service.ListTorrents(r.Context(), credential, page, pageSize, r.URL.Query().Get("keyword"), r.URL.Query().Get("category"))
	if writeMoviePilotServiceError(w, err) {
		return
	}
	items := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, map[string]any{
			"id": item.ID, "uuid": item.LegacyRouteID, "title": item.Title, "subtitle": item.Subtitle,
			"category": item.Category, "category_name": item.CategoryName, "size": item.Size,
			"seeders": item.Seeders, "leechers": item.Leechers, "downloads": item.Downloads,
			"uploader": item.Uploader, "uploader_id": item.UploaderID, "anonymous": item.Anonymous,
			"created_at": item.CreatedAt, "promotion": moviePilotPromotionDTO(item.Promotion),
		})
	}
	writeMoviePilotJSON(w, http.StatusOK, moviePilotResponse{Code: 0, Message: "success", Data: map[string]any{
		"torrents": items, "total": result.Total, "page": result.Page,
		"page_size": result.PageSize, "total_pages": result.TotalPages,
	}})
}

func handleMoviePilotTorrent(apiKeys PersonalAPIKeyService, service MoviePilotService, w http.ResponseWriter, r *http.Request) {
	credential, ok := authenticateMoviePilot(apiKeys, w, r)
	if !ok {
		return
	}
	torrentID := moviePilotTorrentID(r.URL.Path)
	result, err := service.Torrent(r.Context(), credential, torrentID)
	if writeMoviePilotServiceError(w, err) {
		return
	}
	detail := result.Detail
	writeMoviePilotJSON(w, http.StatusOK, moviePilotResponse{Code: 0, Message: "success", Data: map[string]any{
		"id": int64(detail.ID), "uuid": strconv.FormatInt(int64(detail.ID), 10), "title": detail.Title,
		"subtitle": detail.Subtitle, "category": moviePilotCategoryName(detail.Category.ID),
		"category_name": detail.Category.Name, "size": detail.TotalSizeBytes,
		"seeders": result.Swarm.Seeders, "leechers": result.Swarm.Leechers, "downloads": result.Swarm.Completed,
		"created_at": detail.PublishedAt, "info_hash": detail.InfoHashV1.Hex(),
		"download_url": result.DownloadURL, "promotion": moviePilotPromotionDTO(result.Promotion),
	}})
}

func handleMoviePilotAttendanceClaim(apiKeys PersonalAPIKeyService, service MoviePilotService, w http.ResponseWriter, r *http.Request) {
	credential, ok := authenticateMoviePilot(apiKeys, w, r)
	if !ok {
		return
	}
	var body struct {
		Mode attendance.Mode `json:"mode"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, moviePilotAttendanceBodyLimit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeMoviePilotJSON(w, http.StatusBadRequest, moviePilotResponse{Code: 400, Message: "签到参数无效"})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeMoviePilotJSON(w, http.StatusBadRequest, moviePilotResponse{Code: 400, Message: "签到参数无效"})
		return
	}
	if body.Mode == "" {
		body.Mode = attendance.ModeFixed
	}
	record, err := service.ClaimAttendance(r.Context(), credential, body.Mode)
	if errors.Is(err, attendance.ErrAlreadyClaimed) {
		writeMoviePilotJSON(w, http.StatusBadRequest, moviePilotResponse{Code: 1, Message: "今日已签到"})
		return
	}
	if writeMoviePilotServiceError(w, err) {
		return
	}
	writeMoviePilotJSON(w, http.StatusOK, moviePilotResponse{Code: 0, Message: "success", Data: map[string]any{
		"attendance_date": record.AttendanceDate, "mode": record.Mode, "reward": record.TotalReward,
		"experience_reward": record.ExperienceReward, "current_streak": record.CurrentStreak,
		"total_days": record.TotalDays, "longest_streak": record.LongestStreak,
	}})
}

func handleMoviePilotAttendanceStats(apiKeys PersonalAPIKeyService, service MoviePilotService, w http.ResponseWriter, r *http.Request) {
	credential, ok := authenticateMoviePilot(apiKeys, w, r)
	if !ok {
		return
	}
	overview, err := service.AttendanceOverview(r.Context(), credential)
	if writeMoviePilotServiceError(w, err) {
		return
	}
	writeMoviePilotJSON(w, http.StatusOK, moviePilotResponse{Code: 0, Message: "success", Data: map[string]any{
		"claimed_today": overview.ClaimedToday, "today": overview.Today,
		"current_streak": overview.CurrentStreak, "total_days": overview.TotalDays,
		"longest_streak": overview.LongestStreak,
	}})
}

func handleMoviePilotDownload(service MoviePilotService, w http.ResponseWriter, r *http.Request) {
	torrentID := moviePilotDownloadTorrentID(r.URL.Path)
	result, err := service.Download(r.Context(), torrentID, r.URL.Query().Get("capability"))
	if writeIntegrationDownloadError(w, err) {
		return
	}
	writeIntegrationTorrent(w, result)
}

func handlePTDepilerDownload(apiKeys PersonalAPIKeyService, service MoviePilotService, torrentID int64, rawCredential string, w http.ResponseWriter, r *http.Request) {
	if headerCredential, present, valid := optionalMoviePilotCredentialFromRequest(r); present && (!valid || headerCredential != rawCredential) {
		writeInvalidIntegrationCredential(w)
		return
	}
	credential, ok := authenticateIntegrationCredential(apiKeys, rawCredential, w, r)
	if !ok {
		return
	}
	result, err := service.DownloadWithCredential(r.Context(), credential, torrentID)
	if writeIntegrationDownloadError(w, err) {
		return
	}
	writeIntegrationTorrent(w, result)
}

func writeIntegrationTorrent(w http.ResponseWriter, result torrents.TorrentDownloadResult) {
	w.Header().Set("Content-Type", "application/x-bittorrent")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": result.Filename}))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.Data)
}

func writeIntegrationDownloadError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, moviepilot.ErrCapabilityInvalid), errors.Is(err, personalapikey.ErrInvalid):
		status = http.StatusUnauthorized
	case errors.Is(err, moviepilot.ErrRateLimited):
		status = http.StatusTooManyRequests
	case errors.Is(err, torrents.ErrTorrentDownloadNotFound), errors.Is(err, torrents.ErrTorrentReadNotFound), errors.Is(err, catalog.ErrTorrentNotFound):
		status = http.StatusNotFound
	case errors.Is(err, torrentpurchase.ErrPurchaseRequired):
		status = http.StatusPaymentRequired
	case errors.Is(err, torrents.ErrTorrentDownloadEmailUnverified), errors.Is(err, torrents.ErrTorrentDownloadRestricted), errors.Is(err, torrentpurchase.ErrPurchaseDisabled), errors.Is(err, authz.ErrForbidden), errors.Is(err, personalapikey.ErrScopeDenied):
		status = http.StatusForbidden
	case errors.Is(err, identity.ErrTrackerCredentialUnavailable), errors.Is(err, identity.ErrTrackerCredentialStateConflict), errors.Is(err, torrents.ErrTorrentDownloadStorageUnavailable):
		status = http.StatusServiceUnavailable
	case errors.Is(err, torrents.ErrTorrentDownloadObjectConflict):
		status = http.StatusConflict
	}
	writeMoviePilotJSON(w, status, moviePilotResponse{Code: status, Message: "种子下载失败"})
	return true
}

func authenticateMoviePilot(service PersonalAPIKeyService, w http.ResponseWriter, r *http.Request) (personalapikey.AuthenticatedCredential, bool) {
	raw, valid := moviePilotCredentialFromRequest(r)
	if !valid {
		writeInvalidIntegrationCredential(w)
		return personalapikey.AuthenticatedCredential{}, false
	}
	return authenticateIntegrationCredential(service, raw, w, r)
}

func authenticateIntegrationCredential(service PersonalAPIKeyService, raw string, w http.ResponseWriter, r *http.Request) (personalapikey.AuthenticatedCredential, bool) {
	credential, err := service.Authenticate(r.Context(), raw)
	if errors.Is(err, personalapikey.ErrInvalid) {
		writeInvalidIntegrationCredential(w)
		return personalapikey.AuthenticatedCredential{}, false
	}
	if err != nil {
		writeMoviePilotJSON(w, http.StatusInternalServerError, moviePilotResponse{Code: 500, Message: "服务暂时不可用"})
		return personalapikey.AuthenticatedCredential{}, false
	}
	return credential, true
}

func writeInvalidIntegrationCredential(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="PeerGo integrations"`)
	writeMoviePilotJSON(w, http.StatusUnauthorized, moviePilotResponse{Code: 401, Message: "API Key 无效或已撤销"})
}

func moviePilotCredentialFromRequest(r *http.Request) (string, bool) {
	apiToken := strings.TrimSpace(r.Header.Get("api-token"))
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	bearer := ""
	if authorization != "" {
		parts := strings.Fields(authorization)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return "", false
		}
		bearer = parts[1]
	}
	if apiToken != "" && bearer != "" && apiToken != bearer {
		return "", false
	}
	if apiToken != "" {
		return apiToken, true
	}
	return bearer, bearer != ""
}

func hasMoviePilotCredentialHeader(r *http.Request) bool {
	return strings.TrimSpace(r.Header.Get("Authorization")) != "" || strings.TrimSpace(r.Header.Get("api-token")) != ""
}

func optionalMoviePilotCredentialFromRequest(r *http.Request) (string, bool, bool) {
	if !hasMoviePilotCredentialHeader(r) {
		return "", false, true
	}
	raw, valid := moviePilotCredentialFromRequest(r)
	return raw, true, valid
}

func ptDepilerDownloadCredential(r *http.Request) (int64, string, bool) {
	if r.Method != http.MethodGet {
		return 0, "", false
	}
	const prefix = "/api/torrent/"
	const marker = "/download/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		return 0, "", false
	}
	remainder := strings.TrimPrefix(r.URL.Path, prefix)
	rawTorrentID, rawCredential, found := strings.Cut(remainder, marker)
	if !found || rawTorrentID == "" || rawCredential == "" || strings.Contains(rawCredential, "/") {
		return 0, "", false
	}
	torrentID, err := strconv.ParseInt(rawTorrentID, 10, 64)
	if err != nil || torrentID < 1 {
		return 0, "", false
	}
	return torrentID, rawCredential, true
}

func moviePilotTorrentID(path string) int64 {
	const prefix = "/api/v1/torrents/"
	if !strings.HasPrefix(path, prefix) {
		return 0
	}
	value := strings.TrimPrefix(path, prefix)
	if value == "" || strings.Contains(value, "/") {
		return 0
	}
	result, err := strconv.ParseInt(value, 10, 64)
	if err != nil || result < 1 {
		return 0
	}
	return result
}

func legacyTorrentRoute(path string) (routeID, action string, ok bool) {
	const prefix = "/api/v1/torrents/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) < 1 || len(parts) > 2 || strings.TrimSpace(parts[0]) == "" {
		return "", "", false
	}
	if numericID, err := strconv.ParseInt(parts[0], 10, 64); err != nil || numericID < 1 {
		return "", "", false
	}
	if len(parts) == 2 {
		if parts[1] != "comments" && parts[1] != "purchase" {
			return "", "", false
		}
		action = parts[1]
	}
	return parts[0], action, true
}

func decodeLegacyJSON(w http.ResponseWriter, r *http.Request, limit int64, destination any, allowEmpty bool) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	err := decoder.Decode(destination)
	if allowEmpty && errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("legacy JSON body contains trailing data")
	}
	return nil
}

func decodeLegacyBase64(raw string, maximumBytes int, allowImageDataURL bool) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if allowImageDataURL && strings.HasPrefix(strings.ToLower(raw), "data:") {
		header, payload, found := strings.Cut(raw, ",")
		if !found || !strings.HasPrefix(strings.ToLower(header), "data:image/") || !strings.HasSuffix(strings.ToLower(header), ";base64") {
			return nil, errors.New("invalid image data URL")
		}
		raw = payload
	}
	if raw == "" || strings.IndexFunc(raw, func(character rune) bool {
		return character == ' ' || character == '\n' || character == '\r' || character == '\t'
	}) >= 0 {
		return nil, errors.New("invalid base64 whitespace")
	}
	maximumEncoded := base64.StdEncoding.EncodedLen(maximumBytes)
	if len(raw) > maximumEncoded+3 {
		return nil, errors.New("base64 object exceeds limit")
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(raw)
	if err != nil {
		decoded, err = base64.RawStdEncoding.Strict().DecodeString(raw)
	}
	if err != nil || len(decoded) < 1 || len(decoded) > maximumBytes {
		return nil, errors.New("invalid base64 object")
	}
	return decoded, nil
}

func decodeLegacyAttributes(raw map[string]json.RawMessage) (map[string][]string, error) {
	if len(raw) > 20 {
		return nil, moviepilot.ErrInput
	}
	result := make(map[string][]string, len(raw))
	for name, value := range raw {
		name = strings.TrimSpace(name)
		if name == "" || len(name) > 80 {
			return nil, moviepilot.ErrInput
		}
		var single string
		if err := json.Unmarshal(value, &single); err == nil {
			if strings.TrimSpace(single) == "" {
				return nil, moviepilot.ErrInput
			}
			result[name] = []string{single}
			continue
		}
		var multiple []string
		if err := json.Unmarshal(value, &multiple); err != nil || len(multiple) < 1 || len(multiple) > 32 {
			return nil, moviepilot.ErrInput
		}
		for _, option := range multiple {
			if strings.TrimSpace(option) == "" || len(option) > 160 {
				return nil, moviepilot.ErrInput
			}
		}
		result[name] = multiple
	}
	return result, nil
}

func legacyOptionalInteger(number json.Number, minimum, maximum int64) (int64, error) {
	if number == "" {
		return minimum, nil
	}
	value, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil || value < minimum || value > maximum {
		return 0, moviepilot.ErrInput
	}
	return value, nil
}

func legacyExternalIdentifiers(imdb, imdbID, tmdb, tmdbID, douban, doubanID string) []torrents.ExternalIdentifier {
	values := []struct {
		provider string
		first    string
		second   string
	}{
		{provider: "imdb", first: imdbID, second: imdb},
		{provider: "tmdb", first: tmdbID, second: tmdb},
		{provider: "douban", first: doubanID, second: douban},
	}
	result := make([]torrents.ExternalIdentifier, 0, len(values))
	for _, value := range values {
		externalID := strings.TrimSpace(value.first)
		if externalID == "" {
			externalID = strings.TrimSpace(value.second)
		}
		if externalID != "" {
			result = append(result, torrents.ExternalIdentifier{Provider: value.provider, ExternalID: externalID})
		}
	}
	return result
}

func legacyTorrentSummaryDTO(item moviepilot.TorrentSummary) map[string]any {
	return map[string]any{
		"id": item.ID, "uuid": item.LegacyRouteID, "title": item.Title, "subtitle": item.Subtitle,
		"category": item.Category, "category_name": item.CategoryName, "size": item.Size,
		"seeders": item.Seeders, "leechers": item.Leechers, "downloads": item.Downloads,
		"uploader": item.Uploader, "uploader_id": item.UploaderID, "anonymous": item.Anonymous,
		"created_at": item.CreatedAt, "cover_image": "", "promotion": moviePilotPromotionDTO(item.Promotion),
	}
}

func legacyPurchaseStatusDTO(status torrentpurchase.Status) map[string]any {
	accessible := status.State == torrentpurchase.AccessFree || status.State == torrentpurchase.AccessUploader || status.State == torrentpurchase.AccessPurchased
	return map[string]any{
		"torrent_id": status.TorrentID, "title": status.Title, "price": status.Price,
		"tax": status.Tax, "seller_income": status.SellerIncome,
		"magic_balance": status.MagicBalance, "state": status.State,
		"is_purchased": accessible, "purchased_at": status.PurchasedAt,
		"legacy_import": status.LegacyImport,
	}
}

func legacyRatio(uploaded, downloaded int64) float64 {
	if downloaded > 0 {
		return float64(uploaded) / float64(downloaded)
	}
	if uploaded > 0 {
		return 999999
	}
	return 0
}

func writeLegacyUploadError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	status, code, message := http.StatusInternalServerError, 4003, "上传失败"
	switch {
	case errors.Is(err, torrents.ErrTorrentUploadDuplicate):
		status, code, message = http.StatusConflict, 4004, "种子已存在"
	case errors.Is(err, torrents.ErrTorrentUploadEmailUnverified), errors.Is(err, personalapikey.ErrScopeDenied), errors.Is(err, authz.ErrForbidden):
		status, code, message = http.StatusForbidden, 4005, "当前账号没有发种权限"
	case errors.Is(err, torrents.ErrTorrentUploadCategoryUnavailable), errors.Is(err, catalog.ErrCategoryNotFound):
		status, code, message = http.StatusBadRequest, 4007, "分类无效或已停用"
	case errors.Is(err, moviepilot.ErrInput), errors.Is(err, torrents.ErrTorrentInputInvalid):
		status, code, message = http.StatusBadRequest, 4008, "分类属性或上传参数无效"
	case errors.Is(err, moviepilot.ErrRateLimited):
		status, code, message = http.StatusTooManyRequests, 429, "请求过于频繁，请稍后重试"
	case errors.Is(err, moviepilot.ErrUnavailable), errors.Is(err, torrents.ErrTorrentUploadStorageUnavailable):
		status, code, message = http.StatusServiceUnavailable, 503, "上传服务暂不可用"
	default:
		if validationCode, ok := torrents.ValidationCodeOf(err); ok {
			status, code, message = http.StatusBadRequest, 4001, "种子文件无效"
			if validationCode == torrents.CodeObjectTooLarge {
				code, message = 4006, "文件大小超过当前站点策略"
			}
		}
	}
	writeMoviePilotJSON(w, status, moviePilotResponse{Code: code, Message: message})
	return true
}

func moviePilotDownloadTorrentID(path string) int64 {
	const prefix = "/api/compat/moviepilot/v1/torrents/"
	const suffix = "/download"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return 0
	}
	value := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	if value == "" || strings.Contains(value, "/") {
		return 0
	}
	result, err := strconv.ParseInt(value, 10, 64)
	if err != nil || result < 1 {
		return 0
	}
	return result
}

func positiveQueryInteger(r *http.Request, name string, fallback int) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return fallback, true
	}
	value, err := strconv.Atoi(raw)
	return value, err == nil && value > 0
}

func moviePilotPromotionDTO(value moviepilot.TorrentPromotion) map[string]any {
	return map[string]any{
		"type": value.Type, "time_type": value.TimeType, "is_active": value.Active,
		"is_global": false, "until": value.Until,
		"up_multiplier": value.UploadFactor, "down_multiplier": value.DownloadFactor,
	}
}

func moviePilotCategoryName(value string) string {
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

func writeMoviePilotServiceError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	status, code, message := http.StatusInternalServerError, 500, "服务暂时不可用"
	switch {
	case errors.Is(err, personalapikey.ErrInvalid), errors.Is(err, identity.ErrSessionNotFound):
		status, code, message = http.StatusUnauthorized, 401, "API Key 无效或已撤销"
	case errors.Is(err, personalapikey.ErrScopeDenied), errors.Is(err, authz.ErrForbidden):
		status, code, message = http.StatusForbidden, 403, "当前账号没有执行该操作的权限"
	case errors.Is(err, moviepilot.ErrInput), errors.Is(err, attendance.ErrInput), errors.Is(err, torrentpurchase.ErrInput), errors.Is(err, catalog.ErrTorrentBookmarkInput), errors.Is(err, social.ErrCommentInput), errors.Is(err, catalog.ErrInvalidLimit), errors.Is(err, catalog.ErrInvalidQuery), errors.Is(err, catalog.ErrInvalidTorrentPage), errors.Is(err, catalog.ErrInvalidTorrentFilter):
		status, code, message = http.StatusBadRequest, 400, "请求参数无效"
	case errors.Is(err, moviepilot.ErrRateLimited):
		status, code, message = http.StatusTooManyRequests, 429, "请求过于频繁，请稍后重试"
	case errors.Is(err, moviepilot.ErrNotFound), errors.Is(err, torrents.ErrTorrentReadNotFound), errors.Is(err, catalog.ErrTorrentNotFound), errors.Is(err, catalog.ErrCategoryNotFound), errors.Is(err, social.ErrCommentTargetNotFound), errors.Is(err, torrentpurchase.ErrNotFound), errors.Is(err, torrents.ErrTorrentDownloadNotFound):
		status, code, message = http.StatusNotFound, 404, "种子不存在"
	case errors.Is(err, attendance.ErrPolicyNotFound), errors.Is(err, attendance.ErrDisabled), errors.Is(err, attendance.ErrModeDisabled):
		status, code, message = http.StatusConflict, 409, "签到暂未开放"
	case errors.Is(err, torrentpurchase.ErrPriceChanged):
		status, code, message = http.StatusConflict, 409, "种子价格已变化，请重新确认"
	case errors.Is(err, torrentpurchase.ErrIdempotencyConflict):
		status, code, message = http.StatusConflict, 409, "购买请求编号已被其他请求使用"
	case errors.Is(err, torrentpurchase.ErrPurchaseNotRequired):
		status, code, message = http.StatusConflict, 409, "该种子无需购买"
	case errors.Is(err, torrentpurchase.ErrPurchaseDisabled):
		status, code, message = http.StatusForbidden, 403, "站点当前未开放种子购买"
	case errors.Is(err, economy.ErrInsufficientBalance):
		status, code, message = http.StatusPaymentRequired, 402, "魔力值余额不足"
	case errors.Is(err, moviepilot.ErrUnavailable):
		status, code, message = http.StatusServiceUnavailable, 503, "兼容 API 暂不可用"
	}
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Bearer realm="PeerGo integrations"`)
	}
	writeMoviePilotJSON(w, status, moviePilotResponse{Code: code, Message: message})
	return true
}

func writeMoviePilotJSON(w http.ResponseWriter, status int, response moviePilotResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}
