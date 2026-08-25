// Package httpserver composes the Core HTTP transport and its cross-cutting
// middleware. Domain modules remain unaware of Chi and OpenAPI validation.
package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	oapimiddleware "github.com/oapi-codegen/nethttp-middleware"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	"github.com/peergo/peergo/services/core/internal/transport/httpapi"
)

// Dependencies makes every runtime boundary explicit at composition time.
type Dependencies struct {
	// Readiness is intentionally separate from liveness. The process may remain
	// alive while PostgreSQL is temporarily unavailable, but an ingress must not
	// route user traffic to it until this check succeeds.
	Readiness                   func(context.Context) error
	Catalog                     *catalog.Service
	Identity                    httpapi.IdentityService
	Registration                httpapi.RegistrationService
	HumanVerification           identity.HumanVerificationVerifier
	Invitations                 httpapi.InvitationService
	EmailVerification           httpapi.EmailVerificationService
	PasswordRecovery            httpapi.PasswordRecoveryService
	SessionSecurity             httpapi.SessionSecurityService
	TwoFactor                   httpapi.TwoFactorService
	StaffIdentity               httpapi.StaffIdentityService
	StaffEnrollment             httpapi.StaffEnrollmentService
	Authorization               httpapi.AuthorizationService
	GrantAdministration         httpapi.GrantAdministrationService
	CategoryAdministration      httpapi.CategoryAdministrationService
	AnnouncementAdministration  httpapi.AnnouncementAdministrationService
	Wiki                        httpapi.WikiService
	SiteDisplaySettings         httpapi.SiteDisplaySettingsService
	UserAdministration          httpapi.UserAdministrationService
	Notifications               httpapi.NotificationService
	TrafficOverview             httpapi.TrafficOverviewService
	EconomyOverview             httpapi.EconomyOverviewService
	Attendance                  httpapi.AttendanceService
	MemberGifts                 httpapi.MemberGiftService
	ContentTips                 httpapi.ContentTipService
	Workgroups                  httpapi.WorkgroupService
	SeedingRewardAdministration httpapi.SeedingRewardAdministrationService
	LevelPolicyAdministration   httpapi.LevelPolicyAdministrationService
	ContributionExperience      httpapi.ContributionExperiencePolicyService
	MedalAdministration         httpapi.MedalAdministrationService
	MemberMedals                httpapi.MemberMedalService
	HNRPolicyAdministration     httpapi.HNRPolicyAdministrationService
	RatioWatchAdministration    httpapi.RatioWatchAdministrationService
	NewcomerAdministration      httpapi.NewcomerAdministrationService
	Operations                  httpapi.OperationsService
	TorrentBookmarks            httpapi.TorrentBookmarkService
	Comments                    httpapi.CommentService
	SocialPosts                 httpapi.SocialPostService
	CommentModeration           httpapi.CommentModerationService
	TorrentRead                 httpapi.TorrentReadService
	TorrentUpload               httpapi.TorrentUploadService
	TorrentDownload             httpapi.TorrentDownloadService
	TorrentReview               httpapi.TorrentReviewService
	TorrentResubmission         httpapi.TorrentResubmissionService
	TorrentMaintenance          httpapi.TorrentMaintenanceService
	PromotionAdministration     httpapi.PromotionAdministrationService
	RSS                         httpapi.RSSService
	TorrentUploadMaxBytes       int
	SessionCookie               httpapi.SessionCookieConfig
	StaffSessionCookie          httpapi.SessionCookieConfig
	AllowedOrigins              []string
}

// New creates a contract-validating Core HTTP handler.
func New(dependencies Dependencies, logger *slog.Logger) (http.Handler, error) {
	// Older transport tests use one unavailable economy stub for adjacent
	// settings surfaces. Runtime composition always supplies the dedicated
	// progression service, while this narrow assertion avoids duplicating that
	// stub across every unrelated middleware test fixture.
	if dependencies.LevelPolicyAdministration == nil {
		dependencies.LevelPolicyAdministration, _ = dependencies.SeedingRewardAdministration.(httpapi.LevelPolicyAdministrationService)
	}
	if dependencies.HumanVerification == nil {
		dependencies.HumanVerification = identity.NewUnavailableHumanVerificationVerifier()
	}
	if dependencies.Catalog == nil || dependencies.Identity == nil || dependencies.Registration == nil || dependencies.Invitations == nil || dependencies.EmailVerification == nil || dependencies.PasswordRecovery == nil || dependencies.SessionSecurity == nil || dependencies.TwoFactor == nil || dependencies.StaffIdentity == nil || dependencies.StaffEnrollment == nil || dependencies.Authorization == nil || dependencies.GrantAdministration == nil || dependencies.CategoryAdministration == nil || dependencies.AnnouncementAdministration == nil || dependencies.Wiki == nil || dependencies.SiteDisplaySettings == nil || dependencies.UserAdministration == nil || dependencies.Notifications == nil || dependencies.TrafficOverview == nil || dependencies.EconomyOverview == nil || dependencies.Attendance == nil || dependencies.MemberGifts == nil || dependencies.ContentTips == nil || dependencies.Workgroups == nil || dependencies.SeedingRewardAdministration == nil || dependencies.LevelPolicyAdministration == nil || dependencies.HNRPolicyAdministration == nil || dependencies.RatioWatchAdministration == nil || dependencies.NewcomerAdministration == nil || dependencies.Operations == nil || dependencies.TorrentBookmarks == nil || dependencies.Comments == nil || dependencies.SocialPosts == nil || dependencies.CommentModeration == nil || dependencies.TorrentRead == nil || dependencies.TorrentUpload == nil || dependencies.TorrentDownload == nil || dependencies.TorrentReview == nil || dependencies.TorrentResubmission == nil || dependencies.TorrentMaintenance == nil || dependencies.PromotionAdministration == nil || dependencies.RSS == nil {
		return nil, errors.New("catalog, identity, registration, email verification, password recovery, session security, two-factor, staff identity, staff enrollment, authorization, grant administration, category administration, announcement administration, Wiki, site display settings, user administration, notifications, traffic overview, torrent bookmarks, comments, comment moderation, torrent read, torrent upload, torrent download, torrent review and torrent resubmission services are required")
	}
	if dependencies.TorrentUploadMaxBytes < 1 {
		return nil, errors.New("torrent upload byte limit is required")
	}
	if dependencies.SessionCookie.Name == "" {
		return nil, errors.New("session cookie name is required")
	}
	if dependencies.StaffSessionCookie.Name == "" {
		return nil, errors.New("staff session cookie name is required")
	}
	if dependencies.SessionCookie.Name == dependencies.StaffSessionCookie.Name {
		return nil, errors.New("Web and staff session cookie names must differ")
	}
	if dependencies.SessionCookie.Path != "/" {
		return nil, errors.New("Web session cookie path must be /")
	}
	if dependencies.StaffSessionCookie.Path != "/api/v1/admin" {
		return nil, errors.New("staff session cookie path must be /api/v1/admin")
	}
	if len(dependencies.AllowedOrigins) == 0 {
		return nil, errors.New("at least one Web origin is required")
	}
	spec, err := generated.GetSwagger()
	if err != nil {
		return nil, err
	}

	router := chi.NewRouter()
	router.Use(opaqueRequestID)
	router.Use(middleware.Recoverer)
	router.Use(middleware.Timeout(15 * time.Second))
	router.Use(httpapi.PrivateResponseHeaders)
	router.Use(httpapi.CaptureSessionCookies(dependencies.SessionCookie.Name, dependencies.StaffSessionCookie.Name))
	router.Use(httpapi.EnforceSameOrigin(dependencies.AllowedOrigins))

	// Liveness is intentionally outside the public API contract and never reads
	// PostgreSQL or another service; readiness checks will be added with adapters.
	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	router.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if dependencies.Readiness != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := dependencies.Readiness(ctx); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "unavailable"})
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	})

	strictHandler := generated.NewStrictHandlerWithOptions(
		httpapi.NewHandler(dependencies.Catalog, dependencies.Identity, dependencies.Registration, dependencies.HumanVerification, dependencies.Invitations, dependencies.EmailVerification, dependencies.PasswordRecovery, dependencies.SessionSecurity, dependencies.TwoFactor, dependencies.StaffIdentity, dependencies.StaffEnrollment, dependencies.Authorization, dependencies.GrantAdministration, dependencies.CategoryAdministration, dependencies.AnnouncementAdministration, dependencies.Wiki, dependencies.SiteDisplaySettings, dependencies.UserAdministration, dependencies.Notifications, dependencies.TrafficOverview, dependencies.EconomyOverview, dependencies.Attendance, dependencies.MemberGifts, dependencies.ContentTips, dependencies.Workgroups, dependencies.SeedingRewardAdministration, dependencies.LevelPolicyAdministration, dependencies.ContributionExperience, dependencies.MedalAdministration, dependencies.MemberMedals, dependencies.HNRPolicyAdministration, dependencies.RatioWatchAdministration, dependencies.NewcomerAdministration, dependencies.Operations, dependencies.TorrentBookmarks, dependencies.Comments, dependencies.SocialPosts, dependencies.CommentModeration, dependencies.TorrentRead, dependencies.TorrentUpload, dependencies.TorrentDownload, dependencies.TorrentReview, dependencies.TorrentResubmission, dependencies.TorrentMaintenance, dependencies.PromotionAdministration, dependencies.RSS, dependencies.SessionCookie, dependencies.StaffSessionCookie),
		nil,
		generated.StrictHTTPServerOptions{
			RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
				if requestBodyTooLarge(err) {
					writeBodyTooLargeProblem(w, r)
					return
				}
				httpapi.WriteProblem(w, r, http.StatusBadRequest, "invalid_request", "请求参数无效", "请求无法按 API 契约解析。")
			},
			ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
				logger.ErrorContext(r.Context(), "core request failed", "request_id", middleware.GetReqID(r.Context()), "error", err)
				httpapi.WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "服务暂时不可用", "请稍后重试，并在反馈时附上 request_id。")
			},
		},
	)

	router.Group(func(apiRouter chi.Router) {
		apiRouter.Use(limitUploadBody(dependencies.TorrentUploadMaxBytes))
		apiRouter.Use(fillAutomaticCommandText)
		// Both generated binding and full OpenAPI validation remain enabled: the
		// first provides typed handlers, while this middleware enforces schema
		// bounds before any use case is called.
		apiRouter.Use(oapimiddleware.OapiRequestValidatorWithOptions(spec, &oapimiddleware.Options{
			DoNotValidateServers: true,
			ErrorHandlerWithOpts: func(_ context.Context, err error, w http.ResponseWriter, r *http.Request, opts oapimiddleware.ErrorHandlerOpts) {
				if requestBodyTooLarge(err) {
					writeBodyTooLargeProblem(w, r)
					return
				}
				status := opts.StatusCode
				if status == 0 {
					status = http.StatusBadRequest
				}
				httpapi.WriteProblem(w, r, status, "contract_validation_failed", "请求不符合 API 契约", "请检查请求路径、参数和数据格式。")
			},
		}))
		generated.HandlerFromMux(strictHandler, apiRouter)
	})

	return router, nil
}

const (
	torrentMultipartEnvelopeAllowance  int64 = 64 << 10
	torrentScreenshotEnvelopeAllowance int64 = 6 * torrents.MaxTorrentScreenshotBytes
)

// limitTorrentUploadBody runs before OpenAPI validation because kin-openapi
// buffers request bodies in order to restore them for generated handlers. The
// small fixed allowance covers multipart boundaries and scalar fields; the use
// case independently enforces the exact metainfo byte limit.
func limitUploadBody(maxMetainfoBytes int) func(http.Handler) http.Handler {
	limit := int64(maxMetainfoBytes) + torrentScreenshotEnvelopeAllowance + torrentMultipartEnvelopeAllowance
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPut && r.URL.Path == "/api/v1/me/avatar" {
				avatarLimit := int64(identity.MaxAvatarBytes)
				if r.ContentLength > avatarLimit {
					writeBodyTooLargeProblem(w, r)
					return
				}
				r.Body = http.MaxBytesReader(w, r.Body, avatarLimit)
				next.ServeHTTP(w, r)
				return
			}
			isTorrentUpload := r.Method == http.MethodPost && r.URL.Path == "/api/v1/torrents"
			isScreenshotChange := r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/me/torrent-submissions/") && strings.HasSuffix(r.URL.Path, "/screenshot-change")
			if !isTorrentUpload && !isScreenshotChange {
				next.ServeHTTP(w, r)
				return
			}
			requestLimit := limit
			if isScreenshotChange {
				requestLimit = torrentScreenshotEnvelopeAllowance + torrentMultipartEnvelopeAllowance
			}
			if r.ContentLength > requestLimit {
				writeBodyTooLargeProblem(w, r)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, requestLimit)
			next.ServeHTTP(w, r)
		})
	}
}

// Kept as a narrow compatibility helper for focused middleware tests.
func limitTorrentUploadBody(maxMetainfoBytes int) func(http.Handler) http.Handler {
	return limitUploadBody(maxMetainfoBytes)
}

func writeBodyTooLargeProblem(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/v1/me/avatar" {
		httpapi.WriteProblem(w, r, http.StatusRequestEntityTooLarge, "avatar_too_large", "头像文件过大", "处理后的头像不能超过 1MB。")
		return
	}
	if strings.HasSuffix(r.URL.Path, "/screenshot-change") {
		httpapi.WriteProblem(w, r, http.StatusRequestEntityTooLarge, "torrent_screenshot_change_too_large", "截图修改过大", "上传内容超过当前站点允许的大小。")
		return
	}
	httpapi.WriteProblem(w, r, http.StatusRequestEntityTooLarge, "torrent_upload_too_large", "种子文件过大", "上传内容超过当前站点允许的大小。")
}

func requestBodyTooLarge(err error) bool {
	var maxBytesError *http.MaxBytesError
	return errors.As(err, &maxBytesError)
}
