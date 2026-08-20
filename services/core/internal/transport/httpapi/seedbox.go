package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	openapi_types "github.com/oapi-codegen/runtime/types"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/trackercontrol"
)

func (h *Handler) ListMySeedboxReports(ctx context.Context, request generated.ListMySeedboxReportsRequestObject) (generated.ListMySeedboxReportsResponseObject, error) {
	limit, offset := trackercontrol.DefaultSeedboxReportLimit, 0
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	session, err := h.identity.CurrentSession(ctx, sessionTokenFromContext(ctx))
	if errors.Is(err, identity.ErrSessionNotFound) {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后查看盒子申报。")
		return generated.ListMySeedboxReports401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	page, err := h.operations.MySeedboxReports(ctx, session.User.ID, limit, offset)
	switch {
	case errors.Is(err, trackercontrol.ErrSeedboxReportInput):
		return listMySeedboxReportsBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "seedbox_read_denied", "无法查看盒子申报", "当前账号没有 tracker.seedbox.read.self 权限。")
		return generated.ListMySeedboxReports403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListMySeedboxReports200JSONResponse(seedboxReportPageDTO(page)), nil
}

func (h *Handler) CreateMySeedboxReport(ctx context.Context, request generated.CreateMySeedboxReportRequestObject) (generated.CreateMySeedboxReportResponseObject, error) {
	if request.Body == nil {
		return createMySeedboxReportBadRequest(ctx), nil
	}
	session, err := h.identity.AuthenticateWrite(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken))
	switch {
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后提交盒子申报。")
		return generated.CreateMySeedboxReport401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.CreateMySeedboxReport403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	report, err := h.operations.SubmitSeedboxReport(ctx, session.User.ID, trackercontrol.SubmitSeedboxReportInput{
		RequestID: request.Params.IdempotencyKey, Address: request.Body.Address,
		Provider: request.Body.Provider, BandwidthMbps: request.Body.BandwidthMbps, Statement: request.Body.Statement,
	})
	switch {
	case errors.Is(err, trackercontrol.ErrSeedboxReportInput):
		return createMySeedboxReportBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "seedbox_report_denied", "暂时不能申报盒子", "当前账号没有盒子申报权限。")
		return generated.CreateMySeedboxReport403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, trackercontrol.ErrSeedboxReportPending):
		problem := newProblemFromContext(ctx, http.StatusConflict, "seedbox_report_pending", "已有待处理申报", "请等待当前申报完成审核后再提交。")
		return generated.CreateMySeedboxReport409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, trackercontrol.ErrSeedboxDecisionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "seedbox_report_request_conflict", "请求标识已经使用", "请刷新页面后重新提交。")
		return generated.CreateMySeedboxReport409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.CreateMySeedboxReport201JSONResponse(seedboxReportDTO(report)), nil
}

func (h *Handler) ListAdminSeedboxReports(ctx context.Context, request generated.ListAdminSeedboxReportsRequestObject) (generated.ListAdminSeedboxReportsResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListAdminSeedboxReports401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListAdminSeedboxReports403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	status := trackercontrol.SeedboxReportStatus("")
	limit, offset := trackercontrol.DefaultSeedboxReportLimit, 0
	if request.Params.Status != nil {
		status = trackercontrol.SeedboxReportStatus(*request.Params.Status)
	}
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := h.operations.SeedboxReports(ctx, staffActor(session), status, limit, offset)
	switch {
	case errors.Is(err, trackercontrol.ErrSeedboxReportInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_seedbox_report_query", "盒子申报查询无效", "请检查状态和分页参数。")
		return generated.ListAdminSeedboxReports400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "seedbox_registry_read_denied", "无法查看盒子申报", "当前后台身份没有盒子审核读取权限。")
		return generated.ListAdminSeedboxReports403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListAdminSeedboxReports200JSONResponse(seedboxReportPageDTO(page)), nil
}

func (h *Handler) DecideAdminSeedboxReport(ctx context.Context, request generated.DecideAdminSeedboxReportRequestObject) (generated.DecideAdminSeedboxReportResponseObject, error) {
	if request.Body == nil {
		return decideAdminSeedboxReportBadRequest(ctx), nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, string(request.Params.XCSRFToken))
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.DecideAdminSeedboxReport401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.DecideAdminSeedboxReport403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	report, err := h.operations.DecideSeedboxReport(ctx, staffActor(session), trackercontrol.DecideSeedboxReportInput{
		RequestID: request.Params.IdempotencyKey, ReportID: request.ReportId,
		ExpectedVersion: request.Body.ExpectedVersion, Decision: trackercontrol.SeedboxDecision(request.Body.Decision), Reason: request.Body.Reason,
	})
	switch {
	case errors.Is(err, trackercontrol.ErrSeedboxReportInput):
		return decideAdminSeedboxReportBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "seedbox_report_decision_denied", "无法处理盒子申报", "当前后台身份没有盒子审核权限。")
		return generated.DecideAdminSeedboxReport403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, trackercontrol.ErrSeedboxReportNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "seedbox_report_not_found", "盒子申报不存在", "记录不存在或已经不可访问。")
		return generated.DecideAdminSeedboxReport404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, trackercontrol.ErrSeedboxReportConflict), errors.Is(err, trackercontrol.ErrSeedboxDecisionConflict), errors.Is(err, trackercontrol.ErrSeedboxReportApproved):
		problem := newProblemFromContext(ctx, http.StatusConflict, "seedbox_report_conflict", "盒子申报状态已变化", "请刷新审核队列后重新处理。")
		return generated.DecideAdminSeedboxReport409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.DecideAdminSeedboxReport200JSONResponse(seedboxReportDTO(report)), nil
}

func seedboxReportDTO(report trackercontrol.SeedboxReport) generated.SeedboxReport {
	result := generated.SeedboxReport{
		Id: openapi_types.UUID(report.ID), UserNumericId: report.UserNumericID, Username: report.Username,
		Address: report.Address, Provider: report.Provider, BandwidthMbps: report.BandwidthMbps,
		Statement: report.Statement, Status: generated.SeedboxReportStatus(report.Status), Version: report.Version,
		SubmittedAt: report.SubmittedAt, DecidedAt: report.DecidedAt, DecisionReason: report.DecisionReason,
	}
	if report.PolicySequence != nil {
		value := strconv.FormatInt(*report.PolicySequence, 10)
		result.PolicySequence = &value
	}
	return result
}

func seedboxReportPageDTO(page trackercontrol.SeedboxReportPage) generated.SeedboxReportPage {
	items := make([]generated.SeedboxReport, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, seedboxReportDTO(item))
	}
	return generated.SeedboxReportPage{Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset}
}

func listMySeedboxReportsBadRequest(ctx context.Context) generated.ListMySeedboxReports400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_seedbox_report_query", "盒子申报查询无效", "请检查分页参数。")
	return generated.ListMySeedboxReports400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func createMySeedboxReportBadRequest(ctx context.Context) generated.CreateMySeedboxReport400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_seedbox_report", "盒子申报无效", "请填写单个 IP、服务商、带宽和至少 10 个字符的说明。")
	return generated.CreateMySeedboxReport400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func decideAdminSeedboxReportBadRequest(ctx context.Context) generated.DecideAdminSeedboxReport400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_seedbox_decision", "盒子审核决定无效", "请刷新版本并填写至少 10 个字符的审核说明。")
	return generated.DecideAdminSeedboxReport400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}
