package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/peergo/peergo/contracts/go/trackerruntimepolicyv1"
	"github.com/peergo/peergo/services/core/internal/contracts/vaultoperations"
	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/economy/torrentpurchase"
	"github.com/peergo/peergo/services/core/internal/modules/operations"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	"github.com/peergo/peergo/services/core/internal/modules/trackercontrol"
)

func (h *Handler) GetEmailSettings(ctx context.Context, _ generated.GetEmailSettingsRequestObject) (generated.GetEmailSettingsResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.GetEmailSettings401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem)}, nil
		}
		return generated.GetEmailSettings403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	overview, err := h.operations.Email(ctx, staffActor(session))
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "operations_monitor_denied", "无法查看邮件设置", "当前后台身份没有 operations.monitor.read 权限。")
		return generated.GetEmailSettings403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	templates := make([]generated.EmailSettingsOverviewTemplates, 0, len(overview.Templates))
	for _, template := range overview.Templates {
		templates = append(templates, generated.EmailSettingsOverviewTemplates(template))
	}
	return generated.GetEmailSettings200JSONResponse{
		GeneratedAt:                  overview.GeneratedAt,
		DeliveryMode:                 generated.EmailSettingsOverviewDeliveryMode(overview.DeliveryMode),
		VerificationPublicOrigin:     overview.VerificationPublicOrigin,
		PasswordRecoveryPublicOrigin: overview.PasswordRecoveryPublicOrigin,
		VerificationTtlSeconds:       overview.VerificationTTLSeconds,
		PasswordRecoveryTtlSeconds:   overview.PasswordRecoveryTTLSeconds,
		CooldownSeconds:              overview.CooldownSeconds,
		Templates:                    templates,
		Stats: generated.EmailDeliveryStats{
			VerificationPending:  strconv.FormatInt(overview.Stats.VerificationPending, 10),
			VerificationSent:     strconv.FormatInt(overview.Stats.VerificationSent, 10),
			VerificationFailed:   strconv.FormatInt(overview.Stats.VerificationFailed, 10),
			VerificationVerified: strconv.FormatInt(overview.Stats.VerificationVerified, 10),
			RecoveryPending:      strconv.FormatInt(overview.Stats.RecoveryPending, 10),
			RecoverySent:         strconv.FormatInt(overview.Stats.RecoverySent, 10),
			RecoveryFailed:       strconv.FormatInt(overview.Stats.RecoveryFailed, 10),
			RecoveryCompleted:    strconv.FormatInt(overview.Stats.RecoveryCompleted, 10),
		},
	}, nil
}

func (h *Handler) TestEmailDelivery(ctx context.Context, request generated.TestEmailDeliveryRequestObject) (generated.TestEmailDeliveryResponseObject, error) {
	if request.Body == nil {
		return testEmailDeliveryBadRequest(ctx), nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.TestEmailDelivery401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.TestEmailDelivery403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	receipt, err := h.operations.TestEmail(ctx, staffActor(session), string(request.Body.Recipient))
	switch {
	case errors.Is(err, vaultoperations.ErrEmailTestInvalidRecipient):
		return testEmailDeliveryBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "email_delivery_test_denied", "没有权限发送测试邮件", "当前后台身份没有邮件测试权限。")
		return generated.TestEmailDelivery403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, vaultoperations.ErrEmailTestUnavailable):
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "email_delivery_unavailable", "邮件投递暂时不可用", "请检查 Relay、SMTP 和发件域配置后重试。")
		return generated.TestEmailDelivery503ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.TestEmailDelivery202JSONResponse{
		AcceptedAt: receipt.AcceptedAt,
		Template:   generated.PeergoDeliveryTestV1,
	}, nil
}

func testEmailDeliveryBadRequest(ctx context.Context) generated.TestEmailDelivery400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_email_recipient", "测试收件地址无效", "请输入一个完整的邮箱地址。")
	return generated.TestEmailDelivery400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func (h *Handler) GetTrackerOperations(ctx context.Context, _ generated.GetTrackerOperationsRequestObject) (generated.GetTrackerOperationsResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.GetTrackerOperations401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem)}, nil
		}
		return generated.GetTrackerOperations403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	overview, err := h.operations.Tracker(ctx, staffActor(session))
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "operations_monitor_denied", "无法查看 Tracker 运行状态", "当前后台身份没有 operations.monitor.read 权限。")
		return generated.GetTrackerOperations403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.GetTrackerOperations200JSONResponse(trackerOperationsDTO(overview)), nil
}

func (h *Handler) GetTrackerSettings(ctx context.Context, _ generated.GetTrackerSettingsRequestObject) (generated.GetTrackerSettingsResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.GetTrackerSettings401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem)}, nil
		}
		return generated.GetTrackerSettings403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	runtime, err := h.operations.TrackerRuntime(ctx, staffActor(session))
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "tracker_policy_read_denied", "无法查看 Tracker 设置", "当前后台身份没有 tracker.policy.read 权限。")
		return generated.GetTrackerSettings403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	configured, err := h.operations.TrackerPolicy(ctx, staffActor(session))
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "tracker_policy_read_denied", "无法查看 Tracker 设置", "当前后台身份没有 tracker.policy.read 权限。")
		return generated.GetTrackerSettings403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.GetTrackerSettings200JSONResponse{
		GeneratedAt: runtime.GeneratedAt, Configured: trackerPolicyRevisionDTO(configured),
		Effective: generated.TrackerEffectivePolicy{
			Sequence: strconv.FormatInt(runtime.PolicyControlSequence, 10), Revision: runtime.PolicyRevision,
			GeneratedAt: runtime.PolicyGeneratedAt, Settings: trackerRuntimeSettingsDTO(runtime),
		},
		ActivationPending: configured.Sequence != runtime.PolicyControlSequence || configured.Policy.Revision != runtime.PolicyRevision,
		Capacity: generated.TrackerCapacitySettings{
			PeerTtlSeconds: runtime.PeerTTLSeconds, MaxSwarms: strconv.FormatInt(runtime.MaxSwarms, 10),
			MaxPeers: strconv.FormatInt(runtime.MaxPeers, 10), MaxPeersPerSwarm: strconv.FormatInt(runtime.MaxPeersPerSwarm, 10),
		},
	}, nil
}

func (h *Handler) IssueTrackerPolicy(ctx context.Context, request generated.IssueTrackerPolicyRequestObject) (generated.IssueTrackerPolicyResponseObject, error) {
	if request.Body == nil {
		return trackerPolicyBadRequest(ctx), nil
	}
	expectedSequence, err := strconv.ParseInt(request.Body.ExpectedSequence, 10, 64)
	if err != nil {
		return trackerPolicyBadRequest(ctx), nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.IssueTrackerPolicy401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.IssueTrackerPolicy403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	issued, err := h.operations.IssueTrackerPolicy(ctx, staffActor(session), trackercontrol.IssueRuntimePolicyInput{
		RequestID: request.Params.IdempotencyKey, ExpectedSequence: expectedSequence,
		Policy: trackerPolicySettingsFromDTO(request.Body.Settings), Reason: request.Body.Reason,
	})
	switch {
	case errors.Is(err, trackercontrol.ErrRuntimePolicyInput):
		return trackerPolicyBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "tracker_policy_issue_denied", "没有权限修改 Tracker 设置", "当前后台身份没有 tracker.policy.issue 权限。")
		return generated.IssueTrackerPolicy403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, trackercontrol.ErrRuntimePolicyVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "tracker_policy_version_conflict", "Tracker 设置已经变化", "请重新载入页面并核对最新设置。")
		return generated.IssueTrackerPolicy409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, trackercontrol.ErrRuntimePolicyIdempotency):
		problem := newProblemFromContext(ctx, http.StatusConflict, "tracker_policy_idempotency_conflict", "请求标识已经使用", "请刷新设置后重新提交。")
		return generated.IssueTrackerPolicy409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.IssueTrackerPolicy201JSONResponse(trackerPolicyRevisionDTO(issued)), nil
}

func trackerPolicyBadRequest(ctx context.Context) generated.IssueTrackerPolicy400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_tracker_policy", "Tracker 设置无效", "请检查 announce、scrape、客户端、请求频率和至少 5 个字符的修改说明。")
	return generated.IssueTrackerPolicy400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func trackerPolicyRevisionDTO(revision trackercontrol.RuntimePolicyRevision) generated.TrackerPolicyRevision {
	result := generated.TrackerPolicyRevision{
		Sequence: strconv.FormatInt(revision.Sequence, 10), Revision: revision.Policy.Revision,
		Settings: trackerPolicySettingsDTO(revision.Policy), Reason: revision.Reason, CreatedAt: revision.CreatedAt,
	}
	if revision.IssuedBy != nil {
		value := openapi_types.UUID(*revision.IssuedBy)
		result.IssuedBy = &value
	}
	return result
}

func trackerPolicySettingsDTO(policy trackerruntimepolicyv1.Policy) generated.TrackerPolicySettings {
	clients := make([]generated.TrackerClientRule, 0, len(policy.AllowedClients))
	for _, rule := range policy.AllowedClients {
		clients = append(clients, generated.TrackerClientRule{Family: generated.TrackerClientRuleFamily(rule.Family), MinVersion: rule.MinVersion})
	}
	seedboxRules := make([]generated.TrackerSeedboxRule, 0, len(policy.Seedbox.Rules))
	for _, rule := range policy.Seedbox.Rules {
		dto := generated.TrackerSeedboxRule{Id: rule.ID, Cidr: rule.CIDR}
		if rule.UserNumericID > 0 {
			value := rule.UserNumericID
			dto.UserNumericId = &value
		}
		seedboxRules = append(seedboxRules, dto)
	}
	return generated.TrackerPolicySettings{
		AnnounceIntervalSeconds: int64(policy.AnnounceIntervalSeconds), MinAnnounceIntervalSeconds: int64(policy.MinAnnounceIntervalSeconds),
		DefaultNumwant: int64(policy.DefaultNumWant), MaxNumwant: int64(policy.MaxNumWant),
		ScrapeEnabled: policy.ScrapeEnabled, MaxScrapeHashes: int64(policy.MaxScrapeHashes),
		ClientMode: generated.TrackerPolicySettingsClientMode(policy.ClientMode), AllowedClients: clients,
		UserRequestsPerMinute: int64(policy.UserRequestsPerMinute), UserBurst: int64(policy.UserBurst),
		AddressRequestsPerMinute: int64(policy.AddressRequestsPerMinute), AddressBurst: int64(policy.AddressBurst),
		Seedbox: generated.TrackerSeedboxRuntimePolicy{
			Enabled: policy.Seedbox.Enabled, UploadFactorBasisPoints: policy.Seedbox.UploadFactorBasisPoints,
			DownloadFactorBasisPoints:        policy.Seedbox.DownloadFactorBasisPoints,
			SeedboxSpeedLimitBytesPerSecond:  policy.Seedbox.SeedboxSpeedLimitBytesPerSecond,
			StandardSpeedLimitBytesPerSecond: policy.Seedbox.StandardSpeedLimitBytesPerSecond,
			Rules:                            seedboxRules,
		},
	}
}

func trackerRuntimeSettingsDTO(runtime operations.TrackerRuntimeOverview) generated.TrackerPolicySettings {
	return trackerPolicySettingsDTO(trackerruntimepolicyv1.Policy{
		AnnounceIntervalSeconds: int(runtime.AnnounceIntervalSeconds), MinAnnounceIntervalSeconds: int(runtime.MinAnnounceIntervalSeconds),
		DefaultNumWant: int(runtime.DefaultNumWant), MaxNumWant: int(runtime.MaxNumWant), ScrapeEnabled: runtime.ScrapeEnabled,
		MaxScrapeHashes: int(runtime.MaxScrapeHashes), ClientMode: trackerruntimepolicyv1.ClientMode(runtime.ClientMode),
		AllowedClients: runtime.AllowedClients, UserRequestsPerMinute: int(runtime.UserRequestsPerMinute), UserBurst: int(runtime.UserBurst),
		AddressRequestsPerMinute: int(runtime.AddressRequestsPerMinute), AddressBurst: int(runtime.AddressBurst),
		Seedbox: runtime.Seedbox,
	})
}

func trackerPolicySettingsFromDTO(settings generated.TrackerPolicySettings) trackerruntimepolicyv1.Policy {
	clients := make([]trackerruntimepolicyv1.ClientRule, 0, len(settings.AllowedClients))
	for _, rule := range settings.AllowedClients {
		clients = append(clients, trackerruntimepolicyv1.ClientRule{Family: trackerruntimepolicyv1.ClientFamily(rule.Family), MinVersion: rule.MinVersion})
	}
	seedboxRules := make([]trackerruntimepolicyv1.SeedboxRule, 0, len(settings.Seedbox.Rules))
	for _, rule := range settings.Seedbox.Rules {
		value := trackerruntimepolicyv1.SeedboxRule{ID: rule.Id, CIDR: rule.Cidr}
		if rule.UserNumericId != nil {
			value.UserNumericID = *rule.UserNumericId
		}
		seedboxRules = append(seedboxRules, value)
	}
	return trackerruntimepolicyv1.Policy{
		AnnounceIntervalSeconds: int(settings.AnnounceIntervalSeconds), MinAnnounceIntervalSeconds: int(settings.MinAnnounceIntervalSeconds),
		DefaultNumWant: int(settings.DefaultNumwant), MaxNumWant: int(settings.MaxNumwant),
		ScrapeEnabled: settings.ScrapeEnabled, MaxScrapeHashes: int(settings.MaxScrapeHashes),
		ClientMode: trackerruntimepolicyv1.ClientMode(settings.ClientMode), AllowedClients: clients,
		UserRequestsPerMinute: int(settings.UserRequestsPerMinute), UserBurst: int(settings.UserBurst),
		AddressRequestsPerMinute: int(settings.AddressRequestsPerMinute), AddressBurst: int(settings.AddressBurst),
		Seedbox: trackerruntimepolicyv1.SeedboxPolicy{
			Enabled: settings.Seedbox.Enabled, UploadFactorBasisPoints: settings.Seedbox.UploadFactorBasisPoints,
			DownloadFactorBasisPoints:        settings.Seedbox.DownloadFactorBasisPoints,
			SeedboxSpeedLimitBytesPerSecond:  settings.Seedbox.SeedboxSpeedLimitBytesPerSecond,
			StandardSpeedLimitBytesPerSecond: settings.Seedbox.StandardSpeedLimitBytesPerSecond,
			Rules:                            seedboxRules,
		},
	}
}

func (h *Handler) GetTorrentSettings(ctx context.Context, _ generated.GetTorrentSettingsRequestObject) (generated.GetTorrentSettingsResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.GetTorrentSettings401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem)}, nil
		}
		return generated.GetTorrentSettings403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	rules, err := h.operations.TorrentRules(ctx, staffActor(session))
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_management_read_denied", "无法查看种子规则", "当前后台身份没有 torrent.manage.read 权限。")
		return generated.GetTorrentSettings403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	formats := make([]generated.TorrentScreenshotRulesFormats, 0, len(rules.Screenshots.Formats))
	for _, format := range rules.Screenshots.Formats {
		formats = append(formats, generated.TorrentScreenshotRulesFormats(format))
	}
	return generated.GetTorrentSettings200JSONResponse{
		GeneratedAt:             rules.GeneratedAt,
		ActiveUploadPolicy:      torrentUploadPolicyRevisionDTO(rules.ActiveUploadPolicy),
		ScheduledUploadPolicies: torrentUploadPolicyRevisionDTOs(rules.ScheduledUploadPolicies),
		Upload: generated.TorrentUploadRules{
			MetainfoMaxBytes: strconv.FormatInt(rules.Upload.MetainfoMaxBytes, 10),
			MaxFiles:         rules.Upload.MaxFiles, RequiredPrivate: rules.Upload.RequiredPrivate,
			SupportedProtocol:      generated.TorrentUploadRulesSupportedProtocol(rules.Upload.SupportedProtocol),
			DuplicateSwarmRejected: rules.Upload.DuplicateSwarmRejected,
			InitialState:           generated.TorrentUploadRulesInitialState(rules.Upload.InitialState),
		},
		Screenshots: generated.TorrentScreenshotRules{
			MaxCount:        rules.Screenshots.MaxCount,
			MaxBytesPerFile: strconv.FormatInt(rules.Screenshots.MaxBytesPerFile, 10),
			Formats:         formats, FirstIsCover: rules.Screenshots.FirstIsCover,
		},
		Object: generated.TorrentObjectRules{
			OriginalStoredImmutable:     rules.Object.OriginalStoredImmutable,
			AnnounceRewrittenOnDownload: rules.Object.AnnounceRewrittenOnDownload,
			LegacyImportProfile:         generated.TorrentObjectRulesLegacyImportProfile(rules.Object.LegacyImportProfile),
			NewUploadProfile:            generated.TorrentObjectRulesNewUploadProfile(rules.Object.NewUploadProfile),
		},
		Purchase: generated.TorrentPurchaseRules{
			Enabled:             rules.Purchase.Enabled,
			CurrencyName:        rules.Purchase.CurrencyName,
			WholeUnitsOnly:      rules.Purchase.WholeUnitsOnly,
			TaxBasisPoints:      rules.Purchase.TaxBasisPoints,
			PolicyRevision:      rules.Purchase.PolicyRevision,
			PolicyEffectiveFrom: rules.Purchase.PolicyEffectiveFrom,
			PricedTorrents:      strconv.FormatInt(rules.Purchase.PricedTorrents, 10),
			LegacyEntitlements:  strconv.FormatInt(rules.Purchase.LegacyEntitlements, 10),
			LiveEntitlements:    strconv.FormatInt(rules.Purchase.LiveEntitlements, 10),
			PermanentAccess:     rules.Purchase.PermanentAccess,
			AtomicSettlement:    rules.Purchase.AtomicSettlement,
			RefundConnected:     rules.Purchase.RefundConnected,
		},
	}, nil
}

func (h *Handler) IssueTorrentUploadPolicyRevision(ctx context.Context, request generated.IssueTorrentUploadPolicyRevisionRequestObject) (generated.IssueTorrentUploadPolicyRevisionResponseObject, error) {
	if request.Body == nil {
		return torrentUploadPolicyBadRequest(ctx), nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.IssueTorrentUploadPolicyRevision401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.IssueTorrentUploadPolicyRevision403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	formats := make([]string, 0, len(request.Body.Settings.ScreenshotFormats))
	for _, format := range request.Body.Settings.ScreenshotFormats {
		formats = append(formats, string(format))
	}
	issued, err := h.operations.IssueTorrentUploadPolicy(ctx, staffActor(session), torrents.IssueUploadPolicyInput{
		RequestID: request.Params.IdempotencyKey, ExpectedSequence: request.Body.ExpectedSequence,
		EffectiveAt: request.Body.EffectiveAt, Reason: request.Body.Reason,
		Settings: torrents.UploadPolicySettings{
			MetainfoMaxBytes: request.Body.Settings.MetainfoMaxBytes, MaxFiles: request.Body.Settings.MaxFiles,
			ScreenshotMaxCount: request.Body.Settings.ScreenshotMaxCount, ScreenshotMaxBytes: request.Body.Settings.ScreenshotMaxBytes,
			ScreenshotMaxPixels: request.Body.Settings.ScreenshotMaxPixels, ScreenshotFormats: formats,
		},
	})
	switch {
	case errors.Is(err, torrents.ErrUploadPolicyInput):
		return torrentUploadPolicyBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_upload_policy_issue_denied", "没有权限修改上传规则", "当前后台身份没有 torrent.upload.policy.issue 权限。")
		return generated.IssueTorrentUploadPolicyRevision403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, torrents.ErrUploadPolicyVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_upload_policy_version_conflict", "上传规则时间线已经变化", "请重新载入页面并核对最新规则版本。")
		return generated.IssueTorrentUploadPolicyRevision409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, torrents.ErrUploadPolicyIdempotency):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_upload_policy_idempotency_conflict", "请求标识已经使用", "请刷新设置后重新提交。")
		return generated.IssueTorrentUploadPolicyRevision409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.IssueTorrentUploadPolicyRevision201JSONResponse{
		Body:    torrentUploadPolicyRevisionDTO(issued),
		Headers: generated.IssueTorrentUploadPolicyRevision201ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func torrentUploadPolicyBadRequest(ctx context.Context) generated.IssueTorrentUploadPolicyRevision400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_upload_policy", "上传规则无效", "请检查生效时间、安全上限和至少 10 个字符的修改说明。")
	return generated.IssueTorrentUploadPolicyRevision400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func torrentUploadPolicyRevisionDTOs(policies []torrents.UploadPolicyRevision) []generated.TorrentUploadPolicyRevision {
	result := make([]generated.TorrentUploadPolicyRevision, 0, len(policies))
	for _, policy := range policies {
		result = append(result, torrentUploadPolicyRevisionDTO(policy))
	}
	return result
}

func torrentUploadPolicyRevisionDTO(policy torrents.UploadPolicyRevision) generated.TorrentUploadPolicyRevision {
	formats := make([]generated.TorrentUploadPolicySettingsScreenshotFormats, 0, len(policy.Settings.ScreenshotFormats))
	for _, format := range policy.Settings.ScreenshotFormats {
		formats = append(formats, generated.TorrentUploadPolicySettingsScreenshotFormats(format))
	}
	return generated.TorrentUploadPolicyRevision{
		Id: policy.ID, Sequence: policy.Sequence, EffectiveAt: policy.EffectiveAt, CreatedAt: policy.CreatedAt, Reason: policy.Reason,
		Settings: generated.TorrentUploadPolicySettings{
			MetainfoMaxBytes: policy.Settings.MetainfoMaxBytes, MaxFiles: policy.Settings.MaxFiles,
			ScreenshotMaxCount: policy.Settings.ScreenshotMaxCount, ScreenshotMaxBytes: policy.Settings.ScreenshotMaxBytes,
			ScreenshotMaxPixels: policy.Settings.ScreenshotMaxPixels, ScreenshotFormats: formats,
		},
	}
}

func (h *Handler) UpdateTorrentPurchasePolicySettings(ctx context.Context, request generated.UpdateTorrentPurchasePolicySettingsRequestObject) (generated.UpdateTorrentPurchasePolicySettingsResponseObject, error) {
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_purchase_policy", "购买设置无效", "请检查购买开关、手续费、当前版本和至少 10 个字符的修改说明。")
		return generated.UpdateTorrentPurchasePolicySettings400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.UpdateTorrentPurchasePolicySettings401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.UpdateTorrentPurchasePolicySettings403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	policy, err := h.torrentDownload.UpdatePurchasePolicy(ctx, staffActor(session), torrentpurchase.UpdatePolicyCommand{
		RequestID: request.Params.IdempotencyKey, Enabled: request.Body.Enabled,
		TaxBasisPoints: request.Body.TaxBasisPoints, ExpectedRevision: request.Body.ExpectedRevision,
		Reason: request.Body.Reason,
	})
	switch {
	case errors.Is(err, torrentpurchase.ErrInput), errors.Is(err, torrentpurchase.ErrNoChange):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_purchase_policy", "购买设置无效", "设置没有变化，或修改说明不足 10 个字符。")
		return generated.UpdateTorrentPurchasePolicySettings400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_purchase_manage_denied", "没有权限修改购买设置", "当前后台身份没有 torrent.purchase.manage.update 权限。")
		return generated.UpdateTorrentPurchasePolicySettings403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, torrentpurchase.ErrVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_purchase_policy_version_conflict", "购买设置已经变化", "请重新载入页面并核对最新设置。")
		return generated.UpdateTorrentPurchasePolicySettings409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, torrentpurchase.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_purchase_policy_idempotency_conflict", "请求标识已经使用", "请刷新设置后重新提交。")
		return generated.UpdateTorrentPurchasePolicySettings409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.UpdateTorrentPurchasePolicySettings200JSONResponse(torrentPurchasePolicySettingsDTO(policy)), nil
}

func torrentPurchasePolicySettingsDTO(policy torrentpurchase.PolicySettings) generated.TorrentPurchasePolicySettings {
	return generated.TorrentPurchasePolicySettings{
		Enabled: policy.Enabled, TaxBasisPoints: policy.TaxBasisPoints,
		Revision: policy.Revision, EffectiveFrom: policy.EffectiveFrom,
	}
}

func (h *Handler) GetSettlementSettings(ctx context.Context, _ generated.GetSettlementSettingsRequestObject) (generated.GetSettlementSettingsResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.GetSettlementSettings401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem)}, nil
		}
		return generated.GetSettlementSettings403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	settings, err := h.operations.SettlementSettings(ctx, staffActor(session))
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "operations_monitor_denied", "无法查看流量规则", "当前后台身份没有 operations.monitor.read 权限。")
		return generated.GetSettlementSettings403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.GetSettlementSettings200JSONResponse{
		GeneratedAt: settings.GeneratedAt,
		Hnr:         hnrPolicySettingsDTO(settings.HNR),
		Seedbox: generated.SeedboxPolicySettings{
			SettlementPrimitiveSupported: settings.Seedbox.SettlementPrimitiveSupported,
			GlobalPolicyConfigured:       settings.Seedbox.GlobalPolicyConfigured,
			UploadFactorBasisPoints:      settings.Seedbox.UploadFactorBasisPoints,
			DownloadFactorBasisPoints:    settings.Seedbox.DownloadFactorBasisPoints,
			ClassificationConnected:      settings.Seedbox.ClassificationConnected,
			RegistryConnected:            settings.Seedbox.RegistryConnected,
			SpeedObservationConnected:    settings.Seedbox.SpeedObservationConnected,
		},
		GlobalRatioWatchConnected: settings.GlobalRatioWatchConnected,
	}, nil
}

func (h *Handler) GetEconomySettings(ctx context.Context, _ generated.GetEconomySettingsRequestObject) (generated.GetEconomySettingsResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.GetEconomySettings401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem)}, nil
		}
		return generated.GetEconomySettings403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	settings, err := h.operations.EconomySettings(ctx, staffActor(session))
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "economy_settings_denied", "无法查看等级与魔力设置", "当前后台身份没有 economy.seedingreward.policy.read 权限。")
		return generated.GetEconomySettings403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.GetEconomySettings200JSONResponse{
		GeneratedAt: settings.GeneratedAt,
		Activity: generated.ActivityRewardReadiness{
			LedgerSupported:           settings.Activity.LedgerSupported,
			DailyAttendanceConnected:  settings.Activity.DailyAttendanceConnected,
			RandomAttendanceConnected: settings.Activity.RandomAttendanceConnected,
			StreakRewardConnected:     settings.Activity.StreakRewardConnected,
			RetroactiveConnected:      settings.Activity.RetroactiveConnected,
			TorrentPublishConnected:   settings.Activity.TorrentPublishConnected,
			InviteRewardConnected:     settings.Activity.InviteRewardConnected,
		},
		Usage: generated.MagicUsageRules{
			CurrencyName:             settings.Usage.CurrencyName,
			WholeUnitsOnly:           settings.Usage.WholeUnitsOnly,
			PtCoinEnabled:            settings.Usage.PTCoinEnabled,
			MemberOverdraftAllowed:   settings.Usage.MemberOverdraftAllowed,
			AppendOnlyLedger:         settings.Usage.AppendOnlyLedger,
			TorrentPurchaseSupported: settings.Usage.TorrentPurchaseSupported,
			TorrentPurchaseConnected: settings.Usage.TorrentPurchaseConnected,
			MemberGiftConnected:      settings.Usage.MemberGiftConnected,
			ContentTipConnected:      settings.Usage.ContentTipConnected,
			RefundSupported:          settings.Usage.RefundSupported,
		},
		Transactions: generated.EconomyTransactionCounts{
			LegacyOpening:   strconv.FormatInt(settings.Transactions.LegacyOpening, 10),
			SeedingReward:   strconv.FormatInt(settings.Transactions.SeedingReward, 10),
			ActivityReward:  strconv.FormatInt(settings.Transactions.ActivityReward, 10),
			TorrentPurchase: strconv.FormatInt(settings.Transactions.TorrentPurchase, 10),
			MemberGift:      strconv.FormatInt(settings.Transactions.MemberGift, 10),
			Tip:             strconv.FormatInt(settings.Transactions.Tip, 10),
			Refund:          strconv.FormatInt(settings.Transactions.Refund, 10),
			Adjustment:      strconv.FormatInt(settings.Transactions.Adjustment, 10),
		},
	}, nil
}

func (h *Handler) GetWorkerOperations(ctx context.Context, _ generated.GetWorkerOperationsRequestObject) (generated.GetWorkerOperationsResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.GetWorkerOperations401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem)}, nil
		}
		return generated.GetWorkerOperations403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	overview, err := h.operations.Workers(ctx, staffActor(session))
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "operations_monitor_denied", "无法查看 Worker 运行状态", "当前后台身份没有 operations.monitor.read 权限。")
		return generated.GetWorkerOperations403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	queues := make([]generated.WorkerQueueStatus, 0, len(overview.Queues))
	for _, queue := range overview.Queues {
		queues = append(queues, generated.WorkerQueueStatus{
			Id: generated.WorkerQueueStatusId(queue.ID), Label: queue.Label,
			Pending: strconv.FormatInt(queue.Pending, 10), Processing: strconv.FormatInt(queue.Processing, 10),
			Retrying: strconv.FormatInt(queue.Retrying, 10), Dead: strconv.FormatInt(queue.Dead, 10),
			Completed: strconv.FormatInt(queue.Completed, 10), OldestPendingAt: queue.OldestPendingAt,
			LastErrorCode: queue.LastErrorCode, LastErrorAt: queue.LastErrorAt,
		})
	}
	return generated.GetWorkerOperations200JSONResponse{GeneratedAt: overview.GeneratedAt, Queues: queues}, nil
}

func (h *Handler) GetStorageOperations(ctx context.Context, _ generated.GetStorageOperationsRequestObject) (generated.GetStorageOperationsResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.GetStorageOperations401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem)}, nil
		}
		return generated.GetStorageOperations403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	overview, err := h.operations.Storage(ctx, staffActor(session))
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "operations_monitor_denied", "无法查看图片与存储状态", "当前后台身份没有 operations.monitor.read 权限。")
		return generated.GetStorageOperations403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	migrations := make([]generated.StorageMigrationOverview, 0, len(overview.Migrations))
	for _, migration := range overview.Migrations {
		kinds := make([]generated.StorageMigrationOverviewObjectKinds, 0, len(migration.ObjectKinds))
		for _, kind := range migration.ObjectKinds {
			kinds = append(kinds, generated.StorageMigrationOverviewObjectKinds(kind))
		}
		migrations = append(migrations, generated.StorageMigrationOverview{
			Id: migration.ID, Mode: generated.StorageMigrationOverviewMode(migration.Mode),
			Status: generated.StorageMigrationOverviewStatus(migration.Status), ObjectKinds: kinds,
			SourceBackendId: migration.SourceBackendID, DestinationBackendId: migration.DestinationBackendID,
			TotalItems:    strconv.FormatInt(migration.TotalItems, 10),
			PendingItems:  strconv.FormatInt(migration.PendingItems, 10),
			VerifiedItems: strconv.FormatInt(migration.VerifiedItems, 10),
			FailedItems:   strconv.FormatInt(migration.FailedItems, 10),
			DeletedItems:  strconv.FormatInt(migration.DeletedItems, 10),
			LastErrorCode: migration.LastErrorCode, CreatedAt: migration.CreatedAt,
			CutoverAt: migration.CutoverAt, RetentionUntil: migration.RetentionUntil,
			CleanupApprovedAt: migration.CleanupApprovedAt, CompletedAt: migration.CompletedAt,
		})
	}
	return generated.GetStorageOperations200JSONResponse{
		GeneratedAt: overview.GeneratedAt,
		Runtime: generated.StorageRuntime{
			BackendId:             overview.Runtime.BackendID,
			Driver:                generated.StorageRuntimeDriver(overview.Runtime.Driver),
			TorrentUploadMaxBytes: strconv.FormatInt(overview.Runtime.TorrentUploadMaxBytes, 10),
			ScreenshotMaxBytes:    strconv.FormatInt(overview.Runtime.ScreenshotMaxBytes, 10),
			AvatarMaxBytes:        strconv.FormatInt(overview.Runtime.AvatarMaxBytes, 10),
		},
		Inventory: generated.StorageInventory{
			TorrentObjects:           strconv.FormatInt(overview.Inventory.TorrentObjects, 10),
			TorrentBytes:             strconv.FormatInt(overview.Inventory.TorrentBytes, 10),
			ScreenshotObjects:        strconv.FormatInt(overview.Inventory.ScreenshotObjects, 10),
			ScreenshotBytes:          strconv.FormatInt(overview.Inventory.ScreenshotBytes, 10),
			AvatarObjects:            strconv.FormatInt(overview.Inventory.AvatarObjects, 10),
			AvatarBytes:              strconv.FormatInt(overview.Inventory.AvatarBytes, 10),
			PreferredOnActiveBackend: strconv.FormatInt(overview.Inventory.PreferredOnActiveBackend, 10),
			VerifiedOnOtherBackends:  strconv.FormatInt(overview.Inventory.VerifiedOnOtherBackends, 10),
			ActiveMigrations:         strconv.FormatInt(overview.Inventory.ActiveMigrations, 10),
			FailedMigrationItems:     strconv.FormatInt(overview.Inventory.FailedMigrationItems, 10),
		},
		ImageDerivatives: generated.ImageDerivativeOverview{
			PolicyVersion:   overview.ImageDerivatives.PolicyVersion,
			Pending:         strconv.FormatInt(overview.ImageDerivatives.Pending, 10),
			Processing:      strconv.FormatInt(overview.ImageDerivatives.Processing, 10),
			Retrying:        strconv.FormatInt(overview.ImageDerivatives.Retrying, 10),
			Ready:           strconv.FormatInt(overview.ImageDerivatives.Ready, 10),
			Dead:            strconv.FormatInt(overview.ImageDerivatives.Dead, 10),
			SourceObjects:   strconv.FormatInt(overview.ImageDerivatives.SourceObjects, 10),
			OutputObjects:   strconv.FormatInt(overview.ImageDerivatives.OutputObjects, 10),
			OutputBytes:     strconv.FormatInt(overview.ImageDerivatives.OutputBytes, 10),
			OldestPendingAt: overview.ImageDerivatives.OldestPendingAt,
			LastErrorCode:   overview.ImageDerivatives.LastErrorCode,
			LastErrorAt:     overview.ImageDerivatives.LastErrorAt,
		},
		Migrations: migrations,
	}, nil
}

func (h *Handler) GetVIPProfileSettings(ctx context.Context, _ generated.GetVIPProfileSettingsRequestObject) (generated.GetVIPProfileSettingsResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.GetVIPProfileSettings401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem)}, nil
		}
		return generated.GetVIPProfileSettings403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	overview, err := h.operations.VIPProfile(ctx, staffActor(session))
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "operations_monitor_denied", "无法查看 VIP 与用户资料设置", "当前后台身份没有 operations.monitor.read 权限。")
		return generated.GetVIPProfileSettings403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.GetVIPProfileSettings200JSONResponse{
		GeneratedAt: overview.GeneratedAt,
		Stats: generated.VIPProfileStats{
			TotalUsers:   strconv.FormatInt(overview.Stats.TotalUsers, 10),
			ActiveVip:    strconv.FormatInt(overview.Stats.ActiveVIP, 10),
			PermanentVip: strconv.FormatInt(overview.Stats.PermanentVIP, 10),
			ExpiringVip:  strconv.FormatInt(overview.Stats.ExpiringVIP, 10),
			ExpiredVip:   strconv.FormatInt(overview.Stats.ExpiredVIP, 10),
		},
		Profile: generated.ProfileRules{
			DisplayNameMinCharacters: overview.Profile.DisplayNameMinCharacters,
			DisplayNameMaxCharacters: overview.Profile.DisplayNameMaxCharacters,
			AvatarMinPixels:          overview.Profile.AvatarMinPixels,
			AvatarMaxPixels:          overview.Profile.AvatarMaxPixels,
			AvatarMaxBytes:           strconv.FormatInt(overview.Profile.AvatarMaxBytes, 10),
			AvatarFormat:             generated.ProfileRulesAvatarFormat(overview.Profile.AvatarFormat),
		},
		Benefits: generated.VIPBenefits{
			SeedingRewardPolicyRevision: overview.Benefits.SeedingRewardPolicyRevision,
			SeedingRewardBonusBps:       int(overview.Benefits.SeedingRewardBonusBPS),
			FreeDownloadEnabled:         overview.Benefits.FreeDownloadEnabled,
			ShareRatioExempt:            overview.Benefits.ShareRatioExempt,
			NewcomerAssessmentExempt:    overview.Benefits.NewcomerAssessmentExempt,
			SpeedLimitExempt:            overview.Benefits.SpeedLimitExempt,
			SeedboxNoDiscount:           overview.Benefits.SeedboxNoDiscount,
		},
	}, nil
}

func trackerOperationsDTO(overview operations.TrackerOverview) generated.TrackerOperationsOverview {
	return generated.TrackerOperationsOverview{
		GeneratedAt: overview.GeneratedAt,
		Control: generated.TrackerControlStatus{
			LastSequence: strconv.FormatInt(overview.Control.LastSequence, 10), PendingEvents: strconv.FormatInt(overview.Control.PendingEvents, 10),
			RetryingEvents: strconv.FormatInt(overview.Control.RetryingEvents, 10), EnabledTorrents: strconv.FormatInt(overview.Control.EnabledTorrents, 10),
			DisabledTorrents: strconv.FormatInt(overview.Control.DisabledTorrents, 10), OldestPendingAt: overview.Control.OldestPendingAt,
			UpdatedAt: overview.Control.UpdatedAt,
		},
		Swarm: generated.SwarmProjectionStatus{
			SourceId: overview.Swarm.SourceID, RoutingEpoch: strconv.FormatInt(overview.Swarm.RoutingEpoch, 10),
			SnapshotSequence: strconv.FormatInt(overview.Swarm.SnapshotSequence, 10), ObservedAt: overview.Swarm.ObservedAt,
			AppliedAt: overview.Swarm.AppliedAt, CollectingRuns: strconv.FormatInt(overview.Swarm.CollectingRuns, 10),
			LatestRunProgress: overview.Swarm.LatestRunProgress,
		},
		Evidence: generated.EvidenceWindowStatus{
			CollectingWindows: strconv.FormatInt(overview.Evidence.CollectingWindows, 10), CompleteWindows: strconv.FormatInt(overview.Evidence.CompleteWindows, 10),
			LatestWindowStart: overview.Evidence.LatestWindowStart, LatestWindowEnd: overview.Evidence.LatestWindowEnd,
			LatestStatus:    generated.EvidenceWindowStatusLatestStatus(overview.Evidence.LatestStatus),
			LatestItemCount: strconv.FormatInt(overview.Evidence.LatestItemCount, 10), LatestChunks: int(overview.Evidence.LatestChunks),
			LatestReceived:   int(overview.Evidence.LatestReceived),
			MonthStartsAt:    overview.Evidence.MonthStartsAt,
			CoverageStartsAt: overview.Evidence.CoverageStartsAt,
			ExpectedThrough:  overview.Evidence.ExpectedThrough,
			ExpectedWindows:  strconv.FormatInt(overview.Evidence.ExpectedWindows, 10),
			MissingWindows:   strconv.FormatInt(overview.Evidence.MissingWindows, 10),
			OldestIncomplete: overview.Evidence.OldestIncomplete,
			Health:           generated.EvidenceWindowStatusHealth(overview.Evidence.Health),
		},
		Consumers: generated.ConsumerProjectionStatus{
			TrafficEntries: strconv.FormatInt(overview.Consumers.TrafficEntries, 10), TrafficAppliedAt: overview.Consumers.TrafficAppliedAt,
			HnrEvents: strconv.FormatInt(overview.Consumers.HNREvents, 10), HnrAppliedAt: overview.Consumers.HNRAppliedAt,
		},
	}
}
