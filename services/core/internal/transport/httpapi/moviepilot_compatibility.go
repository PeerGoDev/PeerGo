package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
	"github.com/peergo/peergo/services/core/internal/modules/economy/attendance"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/moviepilot"
	"github.com/peergo/peergo/services/core/internal/modules/personalapikey"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

const moviePilotAttendanceBodyLimit = 4 << 10

type moviePilotResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// MoviePilotCompatibility intercepts only the legacy endpoints and only
// claims the colliding torrent routes when an external Authorization header is
// present. Ordinary PeerGo browser calls continue through OpenAPI validation.
// It must be mounted after private response headers and before same-origin
// enforcement because API-key clients are not browser-cookie audiences.
func MoviePilotCompatibility(apiKeys PersonalAPIKeyService, service MoviePilotService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if apiKeys == nil || service == nil {
				next.ServeHTTP(w, r)
				return
			}
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/v1/profile":
				handleMoviePilotProfile(apiKeys, service, w, r)
				return
			case r.Method == http.MethodGet && r.URL.Path == "/api/v1/torrents" && hasMoviePilotCredentialHeader(r):
				handleMoviePilotTorrentList(apiKeys, service, w, r)
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
		"level_text": fmt.Sprintf("Lv.%d", profile.Level), "registered_at": profile.RegisteredAt,
		"last_active_at": profile.LastActiveAt, "uploaded": profile.Uploaded, "downloaded": profile.Downloaded,
		"ratio": ratio, "karma": profile.Magic, "experience": profile.Experience,
		"email_verified": profile.EmailVerified, "vip": profile.VIP, "vip_until": profile.VIPUntil,
		"seeding_leeching_data": map[string]any{
			"seeding_count": profile.SeedingCount, "seeding_size": profile.SeedingSize,
			"leeching_count": profile.LeechingCount, "leeching_size": profile.LeechingSize,
		},
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
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, moviepilot.ErrCapabilityInvalid), errors.Is(err, personalapikey.ErrInvalid):
			status = http.StatusUnauthorized
		case errors.Is(err, moviepilot.ErrRateLimited):
			status = http.StatusTooManyRequests
		case errors.Is(err, torrents.ErrTorrentDownloadNotFound), errors.Is(err, torrents.ErrTorrentReadNotFound), errors.Is(err, catalog.ErrTorrentNotFound):
			status = http.StatusNotFound
		case errors.Is(err, torrents.ErrTorrentDownloadEmailUnverified), errors.Is(err, torrents.ErrTorrentDownloadRestricted), errors.Is(err, authz.ErrForbidden):
			status = http.StatusForbidden
		case errors.Is(err, torrents.ErrTorrentDownloadStorageUnavailable):
			status = http.StatusServiceUnavailable
		case errors.Is(err, torrents.ErrTorrentDownloadObjectConflict):
			status = http.StatusConflict
		}
		writeMoviePilotJSON(w, status, moviePilotResponse{Code: status, Message: "种子下载失败"})
		return
	}
	w.Header().Set("Content-Type", "application/x-bittorrent")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": result.Filename}))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.Data)
}

func authenticateMoviePilot(service PersonalAPIKeyService, w http.ResponseWriter, r *http.Request) (personalapikey.AuthenticatedCredential, bool) {
	raw, valid := moviePilotCredentialFromRequest(r)
	if !valid {
		w.Header().Set("WWW-Authenticate", `Bearer realm="PeerGo MoviePilot"`)
		writeMoviePilotJSON(w, http.StatusUnauthorized, moviePilotResponse{Code: 401, Message: "API Key 无效或已撤销"})
		return personalapikey.AuthenticatedCredential{}, false
	}
	credential, err := service.Authenticate(r.Context(), raw)
	if errors.Is(err, personalapikey.ErrInvalid) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="PeerGo MoviePilot"`)
		writeMoviePilotJSON(w, http.StatusUnauthorized, moviePilotResponse{Code: 401, Message: "API Key 无效或已撤销"})
		return personalapikey.AuthenticatedCredential{}, false
	}
	if err != nil {
		writeMoviePilotJSON(w, http.StatusInternalServerError, moviePilotResponse{Code: 500, Message: "服务暂时不可用"})
		return personalapikey.AuthenticatedCredential{}, false
	}
	return credential, true
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
	case errors.Is(err, moviepilot.ErrInput), errors.Is(err, attendance.ErrInput), errors.Is(err, catalog.ErrInvalidLimit), errors.Is(err, catalog.ErrInvalidQuery), errors.Is(err, catalog.ErrInvalidTorrentPage), errors.Is(err, catalog.ErrInvalidTorrentFilter):
		status, code, message = http.StatusBadRequest, 400, "请求参数无效"
	case errors.Is(err, moviepilot.ErrRateLimited):
		status, code, message = http.StatusTooManyRequests, 429, "请求过于频繁，请稍后重试"
	case errors.Is(err, torrents.ErrTorrentReadNotFound), errors.Is(err, catalog.ErrTorrentNotFound), errors.Is(err, torrents.ErrTorrentDownloadNotFound):
		status, code, message = http.StatusNotFound, 404, "种子不存在"
	case errors.Is(err, attendance.ErrPolicyNotFound), errors.Is(err, attendance.ErrDisabled), errors.Is(err, attendance.ErrModeDisabled):
		status, code, message = http.StatusConflict, 409, "签到暂未开放"
	}
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Bearer realm="PeerGo MoviePilot"`)
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
