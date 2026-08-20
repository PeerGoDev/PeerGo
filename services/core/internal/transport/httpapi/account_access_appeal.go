package httpapi

import (
	"context"
	"errors"
	"net/http"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

func (handler *Handler) GetMyDownloadRestriction(ctx context.Context, _ generated.GetMyDownloadRestrictionRequestObject) (generated.GetMyDownloadRestrictionResponseObject, error) {
	cookieToken := sessionTokenFromContext(ctx)
	if cookieToken == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请登录后查看自己的下载限制。")
		return generated.GetMyDownloadRestriction401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	status, err := handler.identity.MyDownloadRestriction(ctx, cookieToken)
	switch {
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "会话已经失效", "请重新登录后查看自己的下载限制。")
		return generated.GetMyDownloadRestriction401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "download_restriction_read_denied", "无法查看下载限制", "当前账号不能查看这项记录。")
		return generated.GetMyDownloadRestriction403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.GetMyDownloadRestriction200JSONResponse{
		Body:    downloadRestrictionStatusDTO(status),
		Headers: generated.GetMyDownloadRestriction200ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func (handler *Handler) SubmitMyDownloadRestrictionAppeal(ctx context.Context, request generated.SubmitMyDownloadRestrictionAppealRequestObject) (generated.SubmitMyDownloadRestrictionAppealResponseObject, error) {
	if request.Body == nil {
		return submitDownloadRestrictionAppealBadRequest(ctx), nil
	}
	appeal, err := handler.identity.SubmitDownloadRestrictionAppeal(
		ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken),
		identity.SubmitDownloadRestrictionAppealInput{
			AppealID: request.Params.IdempotencyKey, Statement: request.Body.Statement,
		},
	)
	switch {
	case errors.Is(err, identity.ErrInvalidInput):
		return submitDownloadRestrictionAppealBadRequest(ctx), nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后提交下载限制申诉。")
		return generated.SubmitMyDownloadRestrictionAppeal401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.SubmitMyDownloadRestrictionAppeal403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "download_restriction_appeal_denied", "无法提交申诉", "当前账号暂时不能提交这项申诉。")
		return generated.SubmitMyDownloadRestrictionAppeal403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrAccountAccessNotRestricted):
		problem := newProblemFromContext(ctx, http.StatusConflict, "manual_download_restriction_not_active", "当前没有可申诉的人工下载限制", "长期分享率或 H&R 限制请进入各自页面处理。")
		return generated.SubmitMyDownloadRestrictionAppeal409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrAccountAccessAppealExists):
		problem := newProblemFromContext(ctx, http.StatusConflict, "download_restriction_appeal_exists", "该限制已经提交申诉", "同一限制版本只能提交一次，请等待管理员处理。")
		return generated.SubmitMyDownloadRestrictionAppeal409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrAccountAccessAppealConflict), errors.Is(err, identity.ErrAccountAccessAppealIdempotency):
		problem := newProblemFromContext(ctx, http.StatusConflict, "download_restriction_appeal_conflict", "申诉状态已经变化", "请刷新页面后重新确认当前限制。")
		return generated.SubmitMyDownloadRestrictionAppeal409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.SubmitMyDownloadRestrictionAppeal201JSONResponse{
		Body:    accountAccessAppealDTO(appeal),
		Headers: generated.SubmitMyDownloadRestrictionAppeal201ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

// InspectAccountAccess is deliberately anonymous-session compatible. The
// credential proof is sent only to the purpose-limited Vault verifier and is
// never copied into response DTOs, logs, query caches or Core persistence.
func (handler *Handler) InspectAccountAccess(ctx context.Context, request generated.InspectAccountAccessRequestObject) (generated.InspectAccountAccessResponseObject, error) {
	if request.Body == nil || request.Body.Credentials.Password == nil {
		return inspectAccountAccessBadRequest(ctx), nil
	}
	status, err := handler.identity.InspectAccountAccess(ctx, identity.InspectAccountAccessInput{
		Credentials: accountAccessCredentials(request.Body.Credentials),
	})
	switch {
	case errors.Is(err, identity.ErrInvalidInput):
		return inspectAccountAccessBadRequest(ctx), nil
	case errors.Is(err, identity.ErrInvalidCredentials):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误", "请检查登录信息后重试。")
		return generated.InspectAccountAccess401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrSecondFactorRequired):
		problem := newProblemFromContext(ctx, http.StatusPreconditionRequired, "second_factor_required", "需要两步验证码", "密码已验证，请继续输入六位验证码或一次性恢复码。")
		return generated.InspectAccountAccess428ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrLoginThrottled):
		problem := newProblemFromContext(ctx, http.StatusTooManyRequests, "login_throttled", "验证尝试过于频繁", "请短暂等待后再试。")
		return generated.InspectAccountAccess429ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrCredentialVerifierUnavailable):
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "identity_unavailable", "身份服务暂时不可用", "请稍后重试。")
		return generated.InspectAccountAccessdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	case err != nil:
		return nil, err
	}
	return generated.InspectAccountAccess200JSONResponse{
		Body:    accountAccessStatusDTO(status),
		Headers: generated.InspectAccountAccess200ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func (handler *Handler) SubmitAccountAccessAppeal(ctx context.Context, request generated.SubmitAccountAccessAppealRequestObject) (generated.SubmitAccountAccessAppealResponseObject, error) {
	if request.Body == nil || request.Body.Credentials.Password == nil {
		return submitAccountAccessAppealBadRequest(ctx), nil
	}
	appeal, err := handler.identity.SubmitAccountAccessAppeal(ctx, identity.SubmitAccountAccessAppealInput{
		AppealID:    request.Params.IdempotencyKey,
		Credentials: accountAccessCredentials(request.Body.Credentials),
		Statement:   request.Body.Statement,
	})
	switch {
	case errors.Is(err, identity.ErrInvalidInput):
		return submitAccountAccessAppealBadRequest(ctx), nil
	case errors.Is(err, identity.ErrInvalidCredentials):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误", "请检查登录信息后重试。")
		return generated.SubmitAccountAccessAppeal401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrSecondFactorRequired):
		problem := newProblemFromContext(ctx, http.StatusPreconditionRequired, "second_factor_required", "需要两步验证码", "密码已验证，请继续输入六位验证码或一次性恢复码。")
		return generated.SubmitAccountAccessAppeal428ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrLoginThrottled):
		problem := newProblemFromContext(ctx, http.StatusTooManyRequests, "login_throttled", "验证尝试过于频繁", "请短暂等待后再试。")
		return generated.SubmitAccountAccessAppeal429ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrAccountAccessNotRestricted):
		problem := newProblemFromContext(ctx, http.StatusConflict, "account_access_not_restricted", "当前账户没有可申诉的访问限制", "请先查询最新账户状态。")
		return generated.SubmitAccountAccessAppeal409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrAccountAccessAppealExists):
		problem := newProblemFromContext(ctx, http.StatusConflict, "account_access_appeal_exists", "该限制已经提交过申诉", "请查询申诉处理状态，无需重复提交。")
		return generated.SubmitAccountAccessAppeal409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrAccountAccessAppealIdempotency), errors.Is(err, identity.ErrAccountAccessAppealConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "account_access_appeal_conflict", "申诉状态已变化", "请重新查询账户状态后再试。")
		return generated.SubmitAccountAccessAppeal409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrCredentialVerifierUnavailable):
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "identity_unavailable", "身份服务暂时不可用", "请稍后重试。")
		return generated.SubmitAccountAccessAppealdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	case err != nil:
		return nil, err
	}
	return generated.SubmitAccountAccessAppeal201JSONResponse{
		Body:    accountAccessAppealDTO(appeal),
		Headers: generated.SubmitAccountAccessAppeal201ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func (handler *Handler) ListManagedAccountAccessAppeals(ctx context.Context, request generated.ListManagedAccountAccessAppealsRequestObject) (generated.ListManagedAccountAccessAppealsResponseObject, error) {
	session, problem, err := handler.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListManagedAccountAccessAppeals401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListManagedAccountAccessAppeals403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	query := identity.AccountAccessAppealQuery{Filter: identity.AccountAccessAppealFilterPending, Limit: request.Params.Limit, Offset: request.Params.Offset}
	if request.Params.Query != nil {
		query.Query = *request.Params.Query
	}
	if request.Params.Status != nil {
		query.Filter = identity.AccountAccessAppealFilter(*request.Params.Status)
	}
	page, err := handler.identity.AccountAccessAppeals(ctx, staffActor(session), query)
	switch {
	case errors.Is(err, identity.ErrInvalidInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_account_access_appeal_query", "申诉查询无效", "请检查筛选和分页参数。")
		return generated.ListManagedAccountAccessAppeals400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "account_access_appeal_read_denied", "无法查看账户申诉", "当前后台身份没有账户申诉查看权限。")
		return generated.ListManagedAccountAccessAppeals403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	items := make([]generated.AccountAccessAppeal, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, accountAccessAppealDTO(item))
	}
	return generated.ListManagedAccountAccessAppeals200JSONResponse{
		Body: generated.AccountAccessAppealPage{
			Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset,
		},
		Headers: generated.ListManagedAccountAccessAppeals200ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func (handler *Handler) DecideManagedAccountAccessAppeal(ctx context.Context, request generated.DecideManagedAccountAccessAppealRequestObject) (generated.DecideManagedAccountAccessAppealResponseObject, error) {
	if request.Body == nil {
		return decideAccountAccessAppealBadRequest(ctx), nil
	}
	session, problem, err := handler.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.DecideManagedAccountAccessAppeal401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.DecideManagedAccountAccessAppeal403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	appeal, err := handler.identity.DecideAccountAccessAppeal(ctx, staffActor(session), identity.DecideAccountAccessAppealInput{
		AppealID: request.AppealId, Decision: identity.AccountAccessAppealDecision(request.Body.Decision),
		ExpectedSourceVersion: request.Body.ExpectedSourceVersion, Response: request.Body.Response,
	})
	switch {
	case errors.Is(err, identity.ErrInvalidInput):
		return decideAccountAccessAppealBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden), errors.Is(err, identity.ErrAccountAccessAppealSelfTarget):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "account_access_appeal_decision_denied", "无法处理账户申诉", "当前后台身份无权处置该申诉。")
		return generated.DecideManagedAccountAccessAppeal403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrAccountAccessAppealMissing):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "account_access_appeal_not_found", "申诉不存在", "该账户访问限制申诉不存在。")
		return generated.DecideManagedAccountAccessAppeal404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrAccountAccessAppealConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "account_access_appeal_decision_conflict", "申诉或账户限制状态已变化", "请刷新列表并重新确认。")
		return generated.DecideManagedAccountAccessAppeal409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrCredentialVerifierUnavailable):
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "identity_unavailable", "身份服务暂时不可用", "账户凭据尚未恢复，Core 仍保持禁用；请稍后重试同一审批。")
		return generated.DecideManagedAccountAccessAppealdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	case err != nil:
		return nil, err
	}
	return generated.DecideManagedAccountAccessAppeal200JSONResponse{
		Body:    accountAccessAppealDTO(appeal),
		Headers: generated.DecideManagedAccountAccessAppeal200ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func accountAccessCredentials(input generated.AccountAccessCredentialProof) identity.AccountAccessCredentials {
	password := ""
	if input.Password != nil {
		password = *input.Password
	}
	secondFactorCode := ""
	if input.SecondFactorCode != nil {
		secondFactorCode = *input.SecondFactorCode
	}
	return identity.AccountAccessCredentials{Identifier: input.Identifier, Password: password, SecondFactorCode: secondFactorCode}
}

func accountAccessStatusDTO(input identity.AccountAccessStatus) generated.AccountAccessStatus {
	result := generated.AccountAccessStatus{Restricted: input.Restricted, CanAppeal: input.CanAppeal}
	if input.Restriction != nil {
		restriction := accountAccessRestrictionDTO(*input.Restriction)
		result.Restriction = &restriction
	}
	if input.Appeal != nil {
		appeal := accountAccessAppealDTO(*input.Appeal)
		result.Appeal = &appeal
	}
	return result
}

func downloadRestrictionStatusDTO(input identity.DownloadRestrictionStatus) generated.DownloadRestrictionStatus {
	result := generated.DownloadRestrictionStatus{
		Restricted: input.Restricted,
		Sources: generated.DownloadRestrictionSources{
			ManualOrLegacy: input.Sources.ManualOrLegacy,
			RatioWatch:     input.Sources.RatioWatch,
			HitAndRun:      input.Sources.HitAndRun,
		},
		CanAppeal: input.CanAppeal,
	}
	if input.Restriction != nil {
		restriction := accountAccessRestrictionDTO(*input.Restriction)
		result.Restriction = &restriction
	}
	if input.Appeal != nil {
		appeal := accountAccessAppealDTO(*input.Appeal)
		result.Appeal = &appeal
	}
	return result
}

func accountAccessAppealDTO(input identity.AccountAccessAppeal) generated.AccountAccessAppeal {
	return generated.AccountAccessAppeal{
		Id: input.ID, UserId: input.UserID, UserNumericId: input.UserNumericID, Username: input.Username,
		Restriction: accountAccessRestrictionDTO(input.Restriction), Statement: input.Statement,
		Status: generated.AccountAccessAppealStatus(input.Status), Response: input.Response,
		CreatedAt: input.CreatedAt, ResolvedAt: input.ResolvedAt, SourceActive: input.SourceActive, Replayed: input.Replayed,
	}
}

func accountAccessRestrictionDTO(input identity.AccountAccessRestriction) generated.AccountAccessRestriction {
	return generated.AccountAccessRestriction{
		SourceKind: generated.AccountAccessSourceKind(input.SourceKind), ReasonCode: input.ReasonCode,
		ReasonSummary: input.ReasonSummary, StartsAt: input.StartsAt, ExpiresAt: input.ExpiresAt,
		SourceVersion: input.SourceVersion,
	}
}

func inspectAccountAccessBadRequest(ctx context.Context) generated.InspectAccountAccess400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_account_access_inspection", "查询信息无效", "请填写用户名、密码，以及账户启用时所需的两步验证码。")
	return generated.InspectAccountAccess400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func submitAccountAccessAppealBadRequest(ctx context.Context) generated.SubmitAccountAccessAppeal400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_account_access_appeal", "申诉内容无效", "请填写本人凭据，并用 20 到 1000 个字说明情况。")
	return generated.SubmitAccountAccessAppeal400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func submitDownloadRestrictionAppealBadRequest(ctx context.Context) generated.SubmitMyDownloadRestrictionAppeal400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_download_restriction_appeal", "申诉内容无效", "请用 20 到 1000 个字说明需要复核的情况。")
	return generated.SubmitMyDownloadRestrictionAppeal400ApplicationProblemPlusJSONResponse{
		ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
	}
}

func decideAccountAccessAppealBadRequest(ctx context.Context) generated.DecideManagedAccountAccessAppeal400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_account_access_appeal_decision", "处理内容无效", "请选择批准或驳回，并填写 10 到 1000 个字的处理意见。")
	return generated.DecideManagedAccountAccessAppeal400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}
