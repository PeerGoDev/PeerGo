package httpapi

import (
	"context"
	"errors"
	"net/http"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/personalapikey"
)

func (h *Handler) GetMyPersonalAPIKey(ctx context.Context, _ generated.GetMyPersonalAPIKeyRequestObject) (generated.GetMyPersonalAPIKeyResponseObject, error) {
	if h.personalAPIKeys == nil {
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "personal_api_key_unavailable", "API Key 服务暂不可用", "请稍后重试。")
		return generated.GetMyPersonalAPIKeydefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	}
	status, err := h.personalAPIKeys.Status(ctx, sessionTokenFromContext(ctx))
	switch {
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后管理个人 API Key。")
		return generated.GetMyPersonalAPIKey401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "personal_api_key_read_denied", "无法查看 API Key", "当前账号没有个人 API Key 查看权限。")
		return generated.GetMyPersonalAPIKey403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.GetMyPersonalAPIKey200JSONResponse(personalAPIKeyStatusDTO(status)), nil
}

func (h *Handler) RotateMyPersonalAPIKey(ctx context.Context, request generated.RotateMyPersonalAPIKeyRequestObject) (generated.RotateMyPersonalAPIKeyResponseObject, error) {
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "personal_api_key_invalid", "API Key 请求无效", "请至少选择一项权限后重试。")
		return generated.RotateMyPersonalAPIKey400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	if h.personalAPIKeys == nil {
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "personal_api_key_unavailable", "API Key 服务暂不可用", "请稍后重试。")
		return generated.RotateMyPersonalAPIKeydefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	}
	scopes := make([]personalapikey.Scope, 0, len(request.Body.Scopes))
	for _, scope := range request.Body.Scopes {
		scopes = append(scopes, personalapikey.Scope(scope))
	}
	issued, err := h.personalAPIKeys.Rotate(
		ctx,
		sessionTokenFromContext(ctx),
		string(request.Params.XCSRFToken),
		request.Body.ExpectedVersion,
		scopes,
	)
	switch {
	case errors.Is(err, personalapikey.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "personal_api_key_invalid", "API Key 请求无效", "请至少选择一项有效权限后重试。")
		return generated.RotateMyPersonalAPIKey400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后管理个人 API Key。")
		return generated.RotateMyPersonalAPIKey401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.RotateMyPersonalAPIKey403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "personal_api_key_manage_denied", "无法管理 API Key", "当前账号没有个人 API Key 管理权限。")
		return generated.RotateMyPersonalAPIKey403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, personalapikey.ErrConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "personal_api_key_conflict", "API Key 状态已变化", "请刷新页面后重试。")
		return generated.RotateMyPersonalAPIKey409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	apiKey := issued.APIKey
	return generated.RotateMyPersonalAPIKey201JSONResponse{
		Credential: personalAPIKeyStatusDTO(issued.Credential), ApiKey: &apiKey,
	}, nil
}

func (h *Handler) RevokeMyPersonalAPIKey(ctx context.Context, request generated.RevokeMyPersonalAPIKeyRequestObject) (generated.RevokeMyPersonalAPIKeyResponseObject, error) {
	if h.personalAPIKeys == nil {
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "personal_api_key_unavailable", "API Key 服务暂不可用", "请稍后重试。")
		return generated.RevokeMyPersonalAPIKeydefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	}
	err := h.personalAPIKeys.Revoke(
		ctx,
		sessionTokenFromContext(ctx),
		string(request.Params.XCSRFToken),
		request.Params.ExpectedVersion,
	)
	switch {
	case errors.Is(err, personalapikey.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "personal_api_key_invalid", "API Key 请求无效", "请刷新页面后重试。")
		return generated.RevokeMyPersonalAPIKey400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后管理个人 API Key。")
		return generated.RevokeMyPersonalAPIKey401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.RevokeMyPersonalAPIKey403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "personal_api_key_manage_denied", "无法管理 API Key", "当前账号没有个人 API Key 管理权限。")
		return generated.RevokeMyPersonalAPIKey403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, personalapikey.ErrNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "personal_api_key_not_found", "API Key 不存在", "该密钥可能已经被撤销。")
		return generated.RevokeMyPersonalAPIKey404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, personalapikey.ErrConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "personal_api_key_conflict", "API Key 状态已变化", "请刷新页面后重试。")
		return generated.RevokeMyPersonalAPIKey409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.RevokeMyPersonalAPIKey204Response{}, nil
}

func personalAPIKeyStatusDTO(status personalapikey.Status) generated.PersonalAPIKeyStatus {
	scopes := make([]generated.PersonalAPIKeyScope, 0, len(status.Scopes))
	for _, scope := range status.Scopes {
		scopes = append(scopes, generated.PersonalAPIKeyScope(scope))
	}
	result := generated.PersonalAPIKeyStatus{Active: status.Active, Scopes: scopes}
	if !status.Active {
		return result
	}
	result.KeyPrefix = &status.KeyPrefix
	result.Version = &status.Version
	createdAt := status.CreatedAt.UTC()
	result.CreatedAt = &createdAt
	result.LastUsedAt = status.LastUsedAt
	return result
}
