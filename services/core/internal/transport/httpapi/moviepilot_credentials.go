package httpapi

import (
	"context"
	"errors"
	"net/http"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/moviepilot"
)

func (h *Handler) GetMyMoviePilotCredential(ctx context.Context, _ generated.GetMyMoviePilotCredentialRequestObject) (generated.GetMyMoviePilotCredentialResponseObject, error) {
	if h.moviePilot == nil {
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "moviepilot_unavailable", "MoviePilot 集成暂不可用", "请稍后重试。")
		return generated.GetMyMoviePilotCredentialdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	}
	status, err := h.moviePilot.CredentialStatus(ctx, sessionTokenFromContext(ctx))
	switch {
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后管理 MoviePilot API Key。")
		return generated.GetMyMoviePilotCredential401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "moviepilot_credential_read_denied", "无法查看 API Key", "当前账号没有 MoviePilot 集成查看权限。")
		return generated.GetMyMoviePilotCredential403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.GetMyMoviePilotCredential200JSONResponse(moviePilotCredentialStatusDTO(status)), nil
}

func (h *Handler) RotateMyMoviePilotCredential(ctx context.Context, request generated.RotateMyMoviePilotCredentialRequestObject) (generated.RotateMyMoviePilotCredentialResponseObject, error) {
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "moviepilot_credential_invalid", "API Key 请求无效", "请刷新页面后重试。")
		return generated.RotateMyMoviePilotCredential400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	if h.moviePilot == nil {
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "moviepilot_unavailable", "MoviePilot 集成暂不可用", "请稍后重试。")
		return generated.RotateMyMoviePilotCredentialdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	}
	issued, err := h.moviePilot.RotateCredential(
		ctx,
		sessionTokenFromContext(ctx),
		string(request.Params.XCSRFToken),
		request.Body.ExpectedVersion,
	)
	switch {
	case errors.Is(err, moviepilot.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "moviepilot_credential_invalid", "API Key 请求无效", "请刷新页面后重试。")
		return generated.RotateMyMoviePilotCredential400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后管理 MoviePilot API Key。")
		return generated.RotateMyMoviePilotCredential401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.RotateMyMoviePilotCredential403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "moviepilot_credential_manage_denied", "无法管理 API Key", "当前账号没有 MoviePilot 集成管理权限。")
		return generated.RotateMyMoviePilotCredential403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, moviepilot.ErrCredentialConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "moviepilot_credential_conflict", "API Key 状态已变化", "请刷新页面后重试。")
		return generated.RotateMyMoviePilotCredential409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	apiKey := issued.APIKey
	return generated.RotateMyMoviePilotCredential201JSONResponse{
		Credential: moviePilotCredentialStatusDTO(issued.Credential), ApiKey: &apiKey,
	}, nil
}

func (h *Handler) RevokeMyMoviePilotCredential(ctx context.Context, request generated.RevokeMyMoviePilotCredentialRequestObject) (generated.RevokeMyMoviePilotCredentialResponseObject, error) {
	if h.moviePilot == nil {
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "moviepilot_unavailable", "MoviePilot 集成暂不可用", "请稍后重试。")
		return generated.RevokeMyMoviePilotCredentialdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	}
	err := h.moviePilot.RevokeCredential(
		ctx,
		sessionTokenFromContext(ctx),
		string(request.Params.XCSRFToken),
		request.Params.ExpectedVersion,
	)
	switch {
	case errors.Is(err, moviepilot.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "moviepilot_credential_invalid", "API Key 请求无效", "请刷新页面后重试。")
		return generated.RevokeMyMoviePilotCredential400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后管理 MoviePilot API Key。")
		return generated.RevokeMyMoviePilotCredential401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.RevokeMyMoviePilotCredential403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "moviepilot_credential_manage_denied", "无法管理 API Key", "当前账号没有 MoviePilot 集成管理权限。")
		return generated.RevokeMyMoviePilotCredential403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, moviepilot.ErrCredentialNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "moviepilot_credential_not_found", "API Key 不存在", "该密钥可能已经被撤销。")
		return generated.RevokeMyMoviePilotCredential404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, moviepilot.ErrCredentialConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "moviepilot_credential_conflict", "API Key 状态已变化", "请刷新页面后重试。")
		return generated.RevokeMyMoviePilotCredential409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.RevokeMyMoviePilotCredential204Response{}, nil
}

func moviePilotCredentialStatusDTO(status moviepilot.CredentialStatus) generated.MoviePilotCredentialStatus {
	result := generated.MoviePilotCredentialStatus{
		Active: status.Active,
		Scopes: []generated.MoviePilotCredentialScope{
			generated.MoviePilotCredentialScopeProfileRead,
			generated.MoviePilotCredentialScopeTorrentRead,
			generated.MoviePilotCredentialScopeTorrentDownload,
			generated.MoviePilotCredentialScopeAttendanceRead,
			generated.MoviePilotCredentialScopeAttendanceClaim,
		},
	}
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
