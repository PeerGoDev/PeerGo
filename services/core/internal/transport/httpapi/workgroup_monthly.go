package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/workgroups"
)

func (h *Handler) ListMyRetentionTargets(ctx context.Context, request generated.ListMyRetentionTargetsRequestObject) (generated.ListMyRetentionTargetsResponseObject, error) {

	items, err := h.workgroups.MyRetentionTargets(ctx, sessionTokenFromContext(ctx), workgroupMonthlyPageLimit(request.Params.Limit), workgroupMonthlyPageOffset(request.Params.Offset))
	if err != nil {
		problem := workgroupMonthlyProblem(ctx, err)
		if problem == nil {
			return nil, err
		}
		return generated.ListMyRetentionTargetsdefaultApplicationProblemPlusJSONResponse{Body: *problem, StatusCode: problem.Status}, nil
	}
	return generated.ListMyRetentionTargets200JSONResponse(retentionTargetListDTO(items)), nil
}

func (h *Handler) ClaimMyRetentionTarget(ctx context.Context, request generated.ClaimMyRetentionTargetRequestObject) (generated.ClaimMyRetentionTargetResponseObject, error) {

	var err error
	if request.Body == nil {
		return generated.ClaimMyRetentionTargetdefaultApplicationProblemPlusJSONResponse{Body: invalidWorkgroupMonthlyInput(ctx), StatusCode: http.StatusBadRequest}, nil
	}
	err = h.workgroups.ClaimRetentionTarget(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), request.Body.TorrentId)
	if err != nil {
		problem := workgroupMonthlyProblem(ctx, err)
		if problem == nil {
			return nil, err
		}
		return generated.ClaimMyRetentionTargetdefaultApplicationProblemPlusJSONResponse{Body: *problem, StatusCode: problem.Status}, nil
	}
	return generated.ClaimMyRetentionTarget204Response{}, nil
}

func (h *Handler) GetMyRetentionProgress(ctx context.Context, request generated.GetMyRetentionProgressRequestObject) (generated.GetMyRetentionProgressResponseObject, error) {

	progress, err := h.workgroups.MyRetentionProgress(ctx, sessionTokenFromContext(ctx), workgroupMonthText(request.Params.Month))
	if err != nil {
		problem := workgroupMonthlyProblem(ctx, err)
		if problem == nil {
			return nil, err
		}
		return generated.GetMyRetentionProgressdefaultApplicationProblemPlusJSONResponse{Body: *problem, StatusCode: problem.Status}, nil
	}
	return generated.GetMyRetentionProgress200JSONResponse(retentionProgressDTO(progress)), nil
}

func (h *Handler) ListStaffRetentionTargets(ctx context.Context, request generated.ListStaffRetentionTargetsRequestObject) (generated.ListStaffRetentionTargetsResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		return generated.ListStaffRetentionTargetsdefaultApplicationProblemPlusJSONResponse{Body: *problem, StatusCode: problem.Status}, nil
	}
	items, err := h.workgroups.StaffRetentionTargets(ctx, staffActor(session), workgroupMonthlyPageLimit(request.Params.Limit), workgroupMonthlyPageOffset(request.Params.Offset))
	if err != nil {
		problem := workgroupMonthlyProblem(ctx, err)
		if problem == nil {
			return nil, err
		}
		return generated.ListStaffRetentionTargetsdefaultApplicationProblemPlusJSONResponse{Body: *problem, StatusCode: problem.Status}, nil
	}
	return generated.ListStaffRetentionTargets200JSONResponse(retentionTargetListDTO(items)), nil
}

func (h *Handler) SaveStaffRetentionTarget(ctx context.Context, request generated.SaveStaffRetentionTargetRequestObject) (generated.SaveStaffRetentionTargetResponseObject, error) {
	session, problem, err := h.authenticateStaffWrite(ctx, string(request.Params.XCSRFToken))
	if err != nil {
		return nil, err
	}
	if problem != nil {
		return generated.SaveStaffRetentionTargetdefaultApplicationProblemPlusJSONResponse{Body: *problem, StatusCode: problem.Status}, nil
	}
	if request.Body == nil {
		return generated.SaveStaffRetentionTargetdefaultApplicationProblemPlusJSONResponse{Body: invalidWorkgroupMonthlyInput(ctx), StatusCode: http.StatusBadRequest}, nil
	}
	var hours int
	if request.Body.RequiredHours != nil {
		hours = *request.Body.RequiredHours
	}
	err = h.workgroups.SaveRetentionTarget(ctx, staffActor(session), request.Body.TorrentId, hours, request.Body.Retire)
	if err != nil {
		problem := workgroupMonthlyProblem(ctx, err)
		if problem == nil {
			return nil, err
		}
		return generated.SaveStaffRetentionTargetdefaultApplicationProblemPlusJSONResponse{Body: *problem, StatusCode: problem.Status}, nil
	}
	return generated.SaveStaffRetentionTarget204Response{}, nil
}

func (h *Handler) GetStaffRetentionProgress(ctx context.Context, request generated.GetStaffRetentionProgressRequestObject) (generated.GetStaffRetentionProgressResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		return generated.GetStaffRetentionProgressdefaultApplicationProblemPlusJSONResponse{Body: *problem, StatusCode: problem.Status}, nil
	}
	progress, err := h.workgroups.StaffRetentionProgress(ctx, staffActor(session), request.MembershipId, workgroupMonthText(request.Params.Month))
	if err != nil {
		problem := workgroupMonthlyProblem(ctx, err)
		if problem == nil {
			return nil, err
		}
		return generated.GetStaffRetentionProgressdefaultApplicationProblemPlusJSONResponse{Body: *problem, StatusCode: problem.Status}, nil
	}
	return generated.GetStaffRetentionProgress200JSONResponse(retentionProgressDTO(progress)), nil
}

func (h *Handler) ListStaffReseedPublications(ctx context.Context, request generated.ListStaffReseedPublicationsRequestObject) (generated.ListStaffReseedPublicationsResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		return generated.ListStaffReseedPublicationsdefaultApplicationProblemPlusJSONResponse{Body: *problem, StatusCode: problem.Status}, nil
	}
	page, err := h.workgroups.StaffReseedPublications(ctx, staffActor(session), request.MembershipId, workgroupMonthText(request.Params.Month), workgroupMonthlyPageLimit(request.Params.Limit), workgroupMonthlyPageOffset(request.Params.Offset))
	if err != nil {
		problem := workgroupMonthlyProblem(ctx, err)
		if problem == nil {
			return nil, err
		}
		return generated.ListStaffReseedPublicationsdefaultApplicationProblemPlusJSONResponse{Body: *problem, StatusCode: problem.Status}, nil
	}
	return generated.ListStaffReseedPublications200JSONResponse(reseedPublicationPageDTO(page)), nil
}

func (h *Handler) PreviewStaffWorkgroupPayroll(ctx context.Context, request generated.PreviewStaffWorkgroupPayrollRequestObject) (generated.PreviewStaffWorkgroupPayrollResponseObject, error) {
	session, problem, err := h.authenticateStaffWrite(ctx, string(request.Params.XCSRFToken))
	if err != nil {
		return nil, err
	}
	if problem != nil {
		return generated.PreviewStaffWorkgroupPayrolldefaultApplicationProblemPlusJSONResponse{Body: *problem, StatusCode: problem.Status}, nil
	}
	if request.Body == nil {
		return generated.PreviewStaffWorkgroupPayrolldefaultApplicationProblemPlusJSONResponse{Body: invalidWorkgroupMonthlyInput(ctx), StatusCode: http.StatusBadRequest}, nil
	}
	preview, err := h.readPayrollPreview(ctx, staffActor(session), request.MembershipId, workgroups.GroupKind(request.GroupKind), string(request.Body.Month), request.Body.Amount, request.Body.MinimumCount, request.Body.MinimumBytes, "")
	if err != nil {
		problem := workgroupMonthlyProblem(ctx, err)
		if problem == nil {
			return nil, err
		}
		return generated.PreviewStaffWorkgroupPayrolldefaultApplicationProblemPlusJSONResponse{Body: *problem, StatusCode: problem.Status}, nil
	}
	return generated.PreviewStaffWorkgroupPayroll200JSONResponse(workgroupPayrollDTO(preview)), nil
}

func (h *Handler) IssueStaffWorkgroupPayroll(ctx context.Context, request generated.IssueStaffWorkgroupPayrollRequestObject) (generated.IssueStaffWorkgroupPayrollResponseObject, error) {
	session, problem, err := h.authenticateStaffWrite(ctx, string(request.Params.XCSRFToken))
	if err != nil {
		return nil, err
	}
	if problem != nil {
		return generated.IssueStaffWorkgroupPayrolldefaultApplicationProblemPlusJSONResponse{Body: *problem, StatusCode: problem.Status}, nil
	}
	if request.Body == nil || request.Body.PreviewDigest == "" {
		return generated.IssueStaffWorkgroupPayrolldefaultApplicationProblemPlusJSONResponse{Body: invalidWorkgroupMonthlyInput(ctx), StatusCode: http.StatusBadRequest}, nil
	}
	preview, err := h.readPayrollPreview(ctx, staffActor(session), request.MembershipId, workgroups.GroupKind(request.GroupKind), string(request.Body.Month), request.Body.Amount, request.Body.MinimumCount, request.Body.MinimumBytes, request.Body.PreviewDigest)
	if err != nil {
		problem := workgroupMonthlyProblem(ctx, err)
		if problem == nil {
			return nil, err
		}
		return generated.IssueStaffWorkgroupPayrolldefaultApplicationProblemPlusJSONResponse{Body: *problem, StatusCode: problem.Status}, nil
	}
	return generated.IssueStaffWorkgroupPayroll200JSONResponse(workgroupPayrollDTO(preview)), nil
}

func workgroupMonthText(value *generated.WorkgroupMonth) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func workgroupMonthlyPageLimit(limit *int) int {
	if limit == nil {
		return workgroups.DefaultPageLimit
	}
	return *limit
}

func workgroupMonthlyPageOffset(offset *int) int {
	if offset == nil {
		return 0
	}
	return *offset
}

func invalidWorkgroupMonthlyInput(ctx context.Context) generated.Problem {
	return newProblemFromContext(ctx, http.StatusBadRequest, "invalid_workgroup_monthly_input", "请求参数无效", "请核对月份、种子或成员，以及结算门槛。")
}

func workgroupMonthlyProblem(ctx context.Context, err error) *generated.Problem {
	switch {
	case errors.Is(err, workgroups.ErrInput):
		return valuePointer(invalidWorkgroupMonthlyInput(ctx))
	case errors.Is(err, identity.ErrSessionNotFound):
		return valuePointer(newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后操作。"))
	case errors.Is(err, authz.ErrForbidden), errors.Is(err, identity.ErrInvalidCSRF):
		return valuePointer(newProblemFromContext(ctx, http.StatusForbidden, "workgroup_monthly_denied", "当前无法执行此操作", "请核对成员资格、后台权限或会话状态。"))
	case errors.Is(err, workgroups.ErrMembershipNotFound), errors.Is(err, workgroups.ErrRetentionTargetNotFound):
		return valuePointer(newProblemFromContext(ctx, http.StatusNotFound, "workgroup_monthly_not_found", "未找到成员或种子", "请检查工作组成员与种子状态。"))
	case errors.Is(err, workgroups.ErrRetentionTargetClosed):
		return valuePointer(newProblemFromContext(ctx, http.StatusConflict, "retention_target_closed", "当前无法认领或添加", "目标已移出、已经使用不同的时长门槛，或种子已下架。"))
	case errors.Is(err, workgroups.ErrPayrollNotEligible), errors.Is(err, workgroups.ErrPayrollPreviewChanged):
		return valuePointer(newProblemFromContext(ctx, http.StatusConflict, "workgroup_payroll_preview_changed", "当前无法发放工资", "请重新预览，核对贡献、应发金额和证据状态。"))
	default:
		return nil
	}
}

func (h *Handler) readPayrollPreview(ctx context.Context, actor authz.StaffActor, membershipID uuid.UUID, group workgroups.GroupKind, month string, amount int64, minimumCount int, minimumBytes, digest string) (workgroups.PayrollPreview, error) {
	size, err := strconv.ParseInt(minimumBytes, 10, 64)
	if err != nil {
		return workgroups.PayrollPreview{}, workgroups.ErrInput
	}
	return h.workgroups.MonthlyPayroll(ctx, actor, workgroups.PayrollInput{
		MembershipID: membershipID, Group: group, Amount: amount,
		MinimumCount: int64(minimumCount), MinimumBytes: size,
	}, month, digest)
}

func retentionTargetListDTO(items []workgroups.RetentionTarget) generated.RetentionTargetList {
	result := generated.RetentionTargetList{Items: make([]generated.RetentionTarget, 0, len(items))}
	for _, item := range items {
		result.Items = append(result.Items, generated.RetentionTarget{
			TorrentId: item.TorrentID, Title: item.Title, SizeBytes: strconv.FormatInt(item.SizeBytes, 10),
			RequiredHours: item.RequiredHours, ClaimedAt: item.ClaimedAt,
		})
	}
	return result
}

func retentionProgressDTO(progress workgroups.RetentionProgress) generated.RetentionProgress {
	result := generated.RetentionProgress{
		Month: progress.Month, QualifiedCount: progress.QualifiedCount, QualifiedBytes: strconv.FormatInt(progress.QualifiedBytes, 10),
		ExpectedHours: progress.ExpectedHours, CompleteHours: progress.CompleteHours, Items: make([]generated.RetentionItem, 0, len(progress.Items)),
	}
	for _, item := range progress.Items {
		result.Items = append(result.Items, generated.RetentionItem{
			TorrentId: item.TorrentID, Title: item.Title, SizeBytes: strconv.FormatInt(item.SizeBytes, 10),
			RequiredHours: item.RequiredHours, ClaimedAt: item.ClaimedAt, ActiveSeconds: item.ActiveSeconds, Qualified: item.Qualified,
		})
	}
	return result
}

func reseedPublicationPageDTO(page workgroups.ReseedPublications) generated.ReseedPublicationPage {
	result := generated.ReseedPublicationPage{Items: make([]generated.ReseedPublication, 0, len(page.Items)), Counted: page.Counted, Pending: page.Pending, Total: page.Total}
	for _, item := range page.Items {
		result.Items = append(result.Items, generated.ReseedPublication{
			TorrentId: item.TorrentID, Title: item.Title, Category: item.Category, SubmittedAt: item.SubmittedAt, PublishedAt: item.PublishedAt,
			ResolutionSource: item.ResolutionSource, Status: generated.ReseedPublicationStatus(item.Status), Reason: item.Reason,
		})
	}
	return result
}

func workgroupPayrollDTO(item workgroups.PayrollPreview) generated.WorkgroupPayrollPreview {
	return generated.WorkgroupPayrollPreview{
		Month: item.Input.Month.Format("2006-01"), Username: item.Username, GroupKind: generated.WorkgroupKind(item.Input.Group),
		Amount: item.Input.Amount, State: generated.WorkgroupPayrollPreviewState(item.State), Contribution: item.Contribution,
		Target: item.Target, QualifiedBytes: strconv.FormatInt(item.QualifiedBytes, 10), PendingCount: item.PendingCount,
		MissingHours: item.MissingHours, PaidAt: item.PaidAt, TransactionId: item.TransactionID, PreviewDigest: item.Digest,
	}
}
