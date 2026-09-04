package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/workgroups"
)

func (h *Handler) GetMyWorkgroups(ctx context.Context, _ generated.GetMyWorkgroupsRequestObject) (generated.GetMyWorkgroupsResponseObject, error) {
	overview, err := h.workgroups.MyOverview(ctx, sessionTokenFromContext(ctx))
	switch {
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后查看自己的工作组。")
		return generated.GetMyWorkgroups401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "workgroup_read_denied", "无法查看工作组", "当前账号没有工作组读取权限。")
		return generated.GetMyWorkgroups403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.GetMyWorkgroups200JSONResponse(myWorkgroupOverviewDTO(overview)), nil
}

func (h *Handler) ListMyWorkgroupContributionCycles(ctx context.Context, request generated.ListMyWorkgroupContributionCyclesRequestObject) (generated.ListMyWorkgroupContributionCyclesResponseObject, error) {
	limit := workgroups.DefaultCycleLimit
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	page, err := h.workgroups.MyContributionCycles(
		ctx, sessionTokenFromContext(ctx), workgroups.GroupKind(request.GroupKind), limit,
	)
	switch {
	case errors.Is(err, workgroups.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_workgroup_contribution_cycle_query", "贡献历史查询无效", "请检查工作组和月份数量。")
		return generated.ListMyWorkgroupContributionCycles400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后查看贡献历史。")
		return generated.ListMyWorkgroupContributionCycles401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "workgroup_read_denied", "无法查看贡献历史", "当前账号没有工作组读取权限。")
		return generated.ListMyWorkgroupContributionCycles403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrMembershipNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "workgroup_membership_not_found", "尚无成员历史", "当前账号还没有这个工作组的成员资格记录。")
		return generated.ListMyWorkgroupContributionCycles404ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListMyWorkgroupContributionCycles200JSONResponse(workgroupContributionCyclePageDTO(page)), nil
}

func (h *Handler) ListMyWorkgroupTasks(ctx context.Context, request generated.ListMyWorkgroupTasksRequestObject) (generated.ListMyWorkgroupTasksResponseObject, error) {
	limit, offset := workgroups.DefaultPageLimit, 0
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := h.workgroups.MyTasks(ctx, sessionTokenFromContext(ctx), limit, offset)
	switch {
	case errors.Is(err, workgroups.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_workgroup_task_query", "任务查询无效", "请检查分页参数。")
		return generated.ListMyWorkgroupTasks400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后查看工作组任务。")
		return generated.ListMyWorkgroupTasks401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "workgroup_task_read_denied", "无法查看工作组任务", "当前账号没有工作组读取权限。")
		return generated.ListMyWorkgroupTasks403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListMyWorkgroupTasks200JSONResponse(workgroupTaskAssignmentPageDTO(page)), nil
}

func (h *Handler) SubmitMyWorkgroupTask(ctx context.Context, request generated.SubmitMyWorkgroupTaskRequestObject) (generated.SubmitMyWorkgroupTaskResponseObject, error) {
	if request.Body == nil {
		return submitWorkgroupTaskBadRequest(ctx), nil
	}
	assignment, err := h.workgroups.SubmitTask(
		ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken),
		request.Params.IdempotencyKey, request.AssignmentId, request.Body.Statement,
	)
	switch {
	case errors.Is(err, workgroups.ErrInput):
		return submitWorkgroupTaskBadRequest(ctx), nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后提交工作组任务。")
		return generated.SubmitMyWorkgroupTask401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.SubmitMyWorkgroupTask403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "workgroup_task_submit_denied", "无法提交工作组任务", "当前账号没有任务提交权限。")
		return generated.SubmitMyWorkgroupTask403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrTaskAssignmentNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "workgroup_task_assignment_not_found", "任务分配不存在", "该任务未分配给当前账号，或记录已经不可用。")
		return generated.SubmitMyWorkgroupTask404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrTaskSubmissionNotAllowed):
		problem := newProblemFromContext(ctx, http.StatusConflict, "workgroup_task_submission_not_allowed", "当前不能提交", "任务尚未开始、已经截止、成员资格已暂停，或成果正在验收/已经通过。")
		return generated.SubmitMyWorkgroupTask409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "workgroup_task_submission_request_conflict", "提交请求已经用于其他操作", "请刷新任务状态后重新提交。")
		return generated.SubmitMyWorkgroupTask409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.SubmitMyWorkgroupTask201JSONResponse(workgroupTaskAssignmentDTO(assignment)), nil
}

func (h *Handler) CreateMyWorkgroupApplication(ctx context.Context, request generated.CreateMyWorkgroupApplicationRequestObject) (generated.CreateMyWorkgroupApplicationResponseObject, error) {
	if request.Body == nil {
		return createWorkgroupApplicationBadRequest(ctx), nil
	}
	application, err := h.workgroups.Apply(
		ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken),
		request.Params.IdempotencyKey, workgroups.GroupKind(request.GroupKind),
		request.Body.Statement,
	)
	switch {
	case errors.Is(err, workgroups.ErrInput):
		return createWorkgroupApplicationBadRequest(ctx), nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后提交工作组申请。")
		return generated.CreateMyWorkgroupApplication401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.CreateMyWorkgroupApplication403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "workgroup_application_denied", "暂时不能申请", "当前账号没有工作组申请权限。")
		return generated.CreateMyWorkgroupApplication403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrApplicationNotAllowed):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "workgroup_application_not_allowed", "该工作组不开放申请", "转种组和保种组由管理员按实际工作授予。")
		return generated.CreateMyWorkgroupApplication403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrApplicationNotEligible):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "workgroup_application_not_eligible", "暂未达到申请条件", "请核对等级、累计上传、账号年龄、邮箱验证和下载限制状态。")
		return generated.CreateMyWorkgroupApplication403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrApplicationPending), errors.Is(err, workgroups.ErrMembershipAlreadyActive):
		problem := newProblemFromContext(ctx, http.StatusConflict, "workgroup_application_conflict", "无需重复申请", "已经存在待处理申请或有效成员资格，请刷新页面查看。")
		return generated.CreateMyWorkgroupApplication409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "workgroup_idempotency_conflict", "请求标识已被使用", "请刷新工作组状态后重新提交。")
		return generated.CreateMyWorkgroupApplication409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.CreateMyWorkgroupApplication201JSONResponse(workgroupApplicationDTO(application)), nil
}

func (h *Handler) GetAdminWorkgroups(ctx context.Context, _ generated.GetAdminWorkgroupsRequestObject) (generated.GetAdminWorkgroupsResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.GetAdminWorkgroups401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem)}, nil
		}
		return generated.GetAdminWorkgroups403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	overview, err := h.workgroups.AdminOverview(ctx, staffActor(session))
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "workgroup_management_read_denied", "无法查看工作组管理", "当前后台身份没有工作组读取权限。")
		return generated.GetAdminWorkgroups403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.GetAdminWorkgroups200JSONResponse(adminWorkgroupOverviewDTO(overview)), nil
}

func (h *Handler) ListAdminWorkgroupTasks(ctx context.Context, request generated.ListAdminWorkgroupTasksRequestObject) (generated.ListAdminWorkgroupTasksResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListAdminWorkgroupTasks401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListAdminWorkgroupTasks403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	limit, offset := workgroups.DefaultPageLimit, 0
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := h.workgroups.Tasks(ctx, staffActor(session), workgroups.GroupKind(request.GroupKind), limit, offset)
	switch {
	case errors.Is(err, workgroups.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_workgroup_task_query", "任务查询无效", "请检查工作组和分页参数。")
		return generated.ListAdminWorkgroupTasks400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "workgroup_management_read_denied", "无法查看工作组任务", "当前后台身份没有工作组读取权限。")
		return generated.ListAdminWorkgroupTasks403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListAdminWorkgroupTasks200JSONResponse(workgroupTaskPageDTO(page)), nil
}

func (h *Handler) PublishAdminWorkgroupTask(ctx context.Context, request generated.PublishAdminWorkgroupTaskRequestObject) (generated.PublishAdminWorkgroupTaskResponseObject, error) {
	if request.Body == nil {
		return publishWorkgroupTaskBadRequest(ctx), nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, string(request.Params.XCSRFToken))
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.PublishAdminWorkgroupTask401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.PublishAdminWorkgroupTask403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	task, err := h.workgroups.PublishTask(
		ctx, staffActor(session), request.Params.IdempotencyKey,
		workgroups.GroupKind(request.GroupKind), workgroups.TaskType(request.Body.TaskType),
		request.Body.Title, request.Body.Description, request.Body.StartsAt, request.Body.DueAt,
	)
	switch {
	case errors.Is(err, workgroups.ErrInput):
		return publishWorkgroupTaskBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "workgroup_task_publish_denied", "无法发布任务", "当前后台身份没有工作组任务发布权限。")
		return generated.PublishAdminWorkgroupTask403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrTaskNoMembers):
		problem := newProblemFromContext(ctx, http.StatusConflict, "workgroup_task_has_no_members", "当前没有可分配成员", "请先确认该工作组至少有一名有效成员。")
		return generated.PublishAdminWorkgroupTask409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "workgroup_task_request_conflict", "发布请求已经用于其他操作", "请刷新任务列表后重新发布。")
		return generated.PublishAdminWorkgroupTask409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.PublishAdminWorkgroupTask201JSONResponse(workgroupTaskDTO(task)), nil
}

func (h *Handler) ListAdminWorkgroupTaskAssignments(ctx context.Context, request generated.ListAdminWorkgroupTaskAssignmentsRequestObject) (generated.ListAdminWorkgroupTaskAssignmentsResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListAdminWorkgroupTaskAssignments401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListAdminWorkgroupTaskAssignments403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	limit, offset := workgroups.MaximumPageLimit, 0
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := h.workgroups.TaskAssignments(
		ctx, staffActor(session), workgroups.GroupKind(request.GroupKind), request.TaskId, limit, offset,
	)
	switch {
	case errors.Is(err, workgroups.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_workgroup_task_assignment_query", "成员任务查询无效", "请检查工作组、任务和分页参数。")
		return generated.ListAdminWorkgroupTaskAssignments400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "workgroup_management_read_denied", "无法查看成员任务", "当前后台身份没有工作组读取权限。")
		return generated.ListAdminWorkgroupTaskAssignments403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrTaskNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "workgroup_task_not_found", "任务不存在", "目标任务不存在或不属于这个工作组。")
		return generated.ListAdminWorkgroupTaskAssignments404ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListAdminWorkgroupTaskAssignments200JSONResponse(workgroupTaskAssignmentPageDTO(page)), nil
}

func (h *Handler) ReviewAdminWorkgroupTaskSubmission(ctx context.Context, request generated.ReviewAdminWorkgroupTaskSubmissionRequestObject) (generated.ReviewAdminWorkgroupTaskSubmissionResponseObject, error) {
	if request.Body == nil {
		return reviewWorkgroupTaskBadRequest(ctx), nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, string(request.Params.XCSRFToken))
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ReviewAdminWorkgroupTaskSubmission401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ReviewAdminWorkgroupTaskSubmission403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	assignment, err := h.workgroups.ReviewTaskSubmission(
		ctx, staffActor(session), request.Params.IdempotencyKey, request.SubmissionId,
		workgroups.TaskReviewDecision(request.Body.Decision), request.Body.Reason,
	)
	switch {
	case errors.Is(err, workgroups.ErrInput):
		return reviewWorkgroupTaskBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "workgroup_task_review_denied", "无法验收任务", "当前后台身份没有工作组任务验收权限。")
		return generated.ReviewAdminWorkgroupTaskSubmission403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrTaskSubmissionNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "workgroup_task_submission_not_found", "成果提交不存在", "目标成果提交不存在或已经不可用。")
		return generated.ReviewAdminWorkgroupTaskSubmission404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrTaskReviewConflict), errors.Is(err, workgroups.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "workgroup_task_review_conflict", "成果状态已经变化", "该成果可能已验收或已被新的提交替代，请刷新成员列表。")
		return generated.ReviewAdminWorkgroupTaskSubmission409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ReviewAdminWorkgroupTaskSubmission200JSONResponse(workgroupTaskAssignmentDTO(assignment)), nil
}

func (h *Handler) ListAdminWorkgroupApplications(ctx context.Context, request generated.ListAdminWorkgroupApplicationsRequestObject) (generated.ListAdminWorkgroupApplicationsResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListAdminWorkgroupApplications401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListAdminWorkgroupApplications403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	status := workgroups.ApplicationStatus("")
	limit, offset := workgroups.DefaultPageLimit, 0
	if request.Params.Status != nil {
		status = workgroups.ApplicationStatus(*request.Params.Status)
	}
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := h.workgroups.ListApplications(ctx, staffActor(session), status, limit, offset)
	switch {
	case errors.Is(err, workgroups.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_workgroup_application_query", "工作组申请查询无效", "请检查状态和分页参数。")
		return generated.ListAdminWorkgroupApplications400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "workgroup_management_read_denied", "无法查看工作组申请", "当前后台身份没有工作组读取权限。")
		return generated.ListAdminWorkgroupApplications403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	items := make([]generated.WorkgroupApplication, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, workgroupApplicationDTO(item))
	}
	return generated.ListAdminWorkgroupApplications200JSONResponse{Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset}, nil
}

func (h *Handler) DecideAdminWorkgroupApplication(ctx context.Context, request generated.DecideAdminWorkgroupApplicationRequestObject) (generated.DecideAdminWorkgroupApplicationResponseObject, error) {
	if request.Body == nil {
		return decideWorkgroupApplicationBadRequest(ctx), nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, string(request.Params.XCSRFToken))
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.DecideAdminWorkgroupApplication401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.DecideAdminWorkgroupApplication403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	application, err := h.workgroups.DecideApplication(
		ctx, staffActor(session), request.Params.IdempotencyKey, request.ApplicationId,
		request.Body.ExpectedVersion, string(request.Body.Decision) == "approve", request.Body.Reason,
	)
	switch {
	case errors.Is(err, workgroups.ErrInput):
		return decideWorkgroupApplicationBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "workgroup_application_decide_denied", "无法审批工作组申请", "当前后台身份没有工作组审批权限。")
		return generated.DecideAdminWorkgroupApplication403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrApplicationNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "workgroup_application_not_found", "申请不存在", "目标申请不存在或已不可用。")
		return generated.DecideAdminWorkgroupApplication404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrApplicationConflict), errors.Is(err, workgroups.ErrMembershipAlreadyActive), errors.Is(err, workgroups.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "workgroup_application_version_conflict", "申请状态已经变化", "请刷新申请队列后重新核对。")
		return generated.DecideAdminWorkgroupApplication409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.DecideAdminWorkgroupApplication200JSONResponse(workgroupApplicationDTO(application)), nil
}

func (h *Handler) ListAdminWorkgroupContributionPolicies(ctx context.Context, request generated.ListAdminWorkgroupContributionPoliciesRequestObject) (generated.ListAdminWorkgroupContributionPoliciesResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListAdminWorkgroupContributionPolicies401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListAdminWorkgroupContributionPolicies403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	limit, offset := 20, 0
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := h.workgroups.ContributionPolicies(ctx, staffActor(session), workgroups.GroupKind(request.GroupKind), limit, offset)
	switch {
	case errors.Is(err, workgroups.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_workgroup_contribution_policy_query", "贡献目标查询无效", "请检查工作组和分页参数。")
		return generated.ListAdminWorkgroupContributionPolicies400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "workgroup_management_read_denied", "无法查看贡献目标", "当前后台身份没有工作组读取权限。")
		return generated.ListAdminWorkgroupContributionPolicies403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	items := make([]generated.WorkgroupContributionPolicyRevision, 0, len(page.Items))
	for _, policy := range page.Items {
		items = append(items, workgroupContributionPolicyDTO(policy))
	}
	var current *generated.WorkgroupContributionPolicyRevision
	if page.Current != nil {
		value := workgroupContributionPolicyDTO(*page.Current)
		current = &value
	}
	return generated.ListAdminWorkgroupContributionPolicies200JSONResponse{
		Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset,
		MinimumEffectiveFrom: page.MinimumEffectiveFrom, Current: current,
	}, nil
}

func (h *Handler) IssueAdminWorkgroupContributionPolicy(ctx context.Context, request generated.IssueAdminWorkgroupContributionPolicyRequestObject) (generated.IssueAdminWorkgroupContributionPolicyResponseObject, error) {
	if request.Body == nil {
		return issueWorkgroupContributionPolicyBadRequest(ctx), nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, string(request.Params.XCSRFToken))
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.IssueAdminWorkgroupContributionPolicy401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.IssueAdminWorkgroupContributionPolicy403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	policy, err := h.workgroups.IssueContributionPolicy(
		ctx, staffActor(session), request.Params.IdempotencyKey,
		workgroups.GroupKind(request.GroupKind), request.Body.TargetValue,
		request.Body.EffectiveFrom, request.Body.Reason,
	)
	switch {
	case errors.Is(err, workgroups.ErrInput):
		return issueWorkgroupContributionPolicyBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "workgroup_contribution_policy_issue_denied", "无法签发贡献目标", "当前后台身份没有贡献目标签发权限。")
		return generated.IssueAdminWorkgroupContributionPolicy403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrContributionPolicyNoChange):
		problem := newProblemFromContext(ctx, http.StatusConflict, "workgroup_contribution_policy_unchanged", "目标没有变化", "新目标需要与时间线上最后一版不同。")
		return generated.IssueAdminWorkgroupContributionPolicy409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrContributionPolicyConflict), errors.Is(err, workgroups.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "workgroup_contribution_policy_conflict", "贡献目标时间线已经变化", "请刷新目标历史，并选择允许的下一个自然月。")
		return generated.IssueAdminWorkgroupContributionPolicy409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.IssueAdminWorkgroupContributionPolicy201JSONResponse(workgroupContributionPolicyDTO(policy)), nil
}

func (h *Handler) ListAdminWorkgroupMemberships(ctx context.Context, request generated.ListAdminWorkgroupMembershipsRequestObject) (generated.ListAdminWorkgroupMembershipsResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListAdminWorkgroupMemberships401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListAdminWorkgroupMemberships403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	status := workgroups.MembershipStatus("")
	limit, offset := workgroups.DefaultPageLimit, 0
	if request.Params.Status != nil {
		status = workgroups.MembershipStatus(*request.Params.Status)
	}
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := h.workgroups.ListMemberships(ctx, staffActor(session), workgroups.GroupKind(request.GroupKind), status, limit, offset)
	switch {
	case errors.Is(err, workgroups.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_workgroup_membership_query", "工作组成员查询无效", "请检查工作组、状态和分页参数。")
		return generated.ListAdminWorkgroupMemberships400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "workgroup_management_read_denied", "无法查看工作组成员", "当前后台身份没有工作组读取权限。")
		return generated.ListAdminWorkgroupMemberships403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	items := make([]generated.WorkgroupMembership, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, workgroupMembershipDTO(item))
	}
	return generated.ListAdminWorkgroupMemberships200JSONResponse{Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset}, nil
}

func (h *Handler) ListAdminWorkgroupMembershipContributionCycles(ctx context.Context, request generated.ListAdminWorkgroupMembershipContributionCyclesRequestObject) (generated.ListAdminWorkgroupMembershipContributionCyclesResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListAdminWorkgroupMembershipContributionCycles401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListAdminWorkgroupMembershipContributionCycles403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	limit := 12
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	page, err := h.workgroups.ContributionCycles(
		ctx, staffActor(session), workgroups.GroupKind(request.GroupKind), request.MembershipId, limit,
	)
	switch {
	case errors.Is(err, workgroups.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_workgroup_contribution_cycle_query", "贡献历史查询无效", "请检查工作组、成员和月份数量。")
		return generated.ListAdminWorkgroupMembershipContributionCycles400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "workgroup_management_read_denied", "无法查看贡献历史", "当前后台身份没有工作组读取权限。")
		return generated.ListAdminWorkgroupMembershipContributionCycles403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrMembershipNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "workgroup_membership_not_found", "成员资格不存在", "目标成员资格不存在或不属于这个工作组。")
		return generated.ListAdminWorkgroupMembershipContributionCycles404ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListAdminWorkgroupMembershipContributionCycles200JSONResponse(workgroupContributionCyclePageDTO(page)), nil
}

func (h *Handler) IssueAdminWorkgroupContributionReminder(ctx context.Context, request generated.IssueAdminWorkgroupContributionReminderRequestObject) (generated.IssueAdminWorkgroupContributionReminderResponseObject, error) {
	if request.Body == nil {
		return issueWorkgroupContributionReminderBadRequest(ctx), nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, string(request.Params.XCSRFToken))
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.IssueAdminWorkgroupContributionReminder401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.IssueAdminWorkgroupContributionReminder403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	reminder, err := h.workgroups.IssueContributionReminder(
		ctx, staffActor(session), request.Params.IdempotencyKey,
		workgroups.GroupKind(request.GroupKind), request.MembershipId,
		request.Body.PeriodStartsAt, request.Body.Reason,
	)
	switch {
	case errors.Is(err, workgroups.ErrInput):
		return issueWorkgroupContributionReminderBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "workgroup_contribution_reminder_issue_denied", "无法发送贡献提醒", "当前后台身份没有工作组贡献提醒权限。")
		return generated.IssueAdminWorkgroupContributionReminder403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrMembershipNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "workgroup_membership_not_found", "成员资格不存在", "目标成员资格不存在或不属于这个工作组。")
		return generated.IssueAdminWorkgroupContributionReminder404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrContributionReminderExists):
		problem := newProblemFromContext(ctx, http.StatusConflict, "workgroup_contribution_reminder_exists", "本周期已经提醒", "每个成员的同一贡献周期只发送一次人工提醒。")
		return generated.IssueAdminWorkgroupContributionReminder409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrContributionReminderDenied):
		problem := newProblemFromContext(ctx, http.StatusConflict, "workgroup_contribution_reminder_not_allowed", "当前周期不能提醒", "只有全周期有效、证据可靠且尚未达标的周期可以提醒；请刷新贡献历史。")
		return generated.IssueAdminWorkgroupContributionReminder409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "workgroup_contribution_reminder_request_conflict", "提醒请求已经用于其他操作", "请刷新页面后重新发送。")
		return generated.IssueAdminWorkgroupContributionReminder409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.IssueAdminWorkgroupContributionReminder201JSONResponse(workgroupContributionReminderDTO(reminder)), nil
}

func issueWorkgroupContributionReminderBadRequest(ctx context.Context) generated.IssueAdminWorkgroupContributionReminder400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_workgroup_contribution_reminder", "贡献提醒无效", "请选择最近 24 个自然月内的贡献周期，并填写 10 到 1000 个字符的提醒说明。")
	return generated.IssueAdminWorkgroupContributionReminder400ApplicationProblemPlusJSONResponse{
		ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
	}
}

func submitWorkgroupTaskBadRequest(ctx context.Context) generated.SubmitMyWorkgroupTask400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_workgroup_task_submission", "任务成果无效", "成果说明需要 10 至 2000 个字符。")
	return generated.SubmitMyWorkgroupTask400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func publishWorkgroupTaskBadRequest(ctx context.Context) generated.PublishAdminWorkgroupTask400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_workgroup_task", "任务内容无效", "请填写标题、10 至 2000 个字符的说明，并选择未来一年内有效的开始与截止时间。")
	return generated.PublishAdminWorkgroupTask400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func reviewWorkgroupTaskBadRequest(ctx context.Context) generated.ReviewAdminWorkgroupTaskSubmission400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_workgroup_task_review", "验收内容无效", "请选择通过或要求修改，并填写至少 10 个字符的说明。")
	return generated.ReviewAdminWorkgroupTaskSubmission400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func (h *Handler) GrantAdminWorkgroupMembership(ctx context.Context, request generated.GrantAdminWorkgroupMembershipRequestObject) (generated.GrantAdminWorkgroupMembershipResponseObject, error) {
	if request.Body == nil {
		return grantWorkgroupMembershipBadRequest(ctx), nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, string(request.Params.XCSRFToken))
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.GrantAdminWorkgroupMembership401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.GrantAdminWorkgroupMembership403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	membership, err := h.workgroups.GrantMembership(
		ctx, staffActor(session), request.Params.IdempotencyKey,
		workgroups.GroupKind(request.GroupKind), request.Body.UserNumericId, request.Body.Reason,
	)
	switch {
	case errors.Is(err, workgroups.ErrInput):
		return grantWorkgroupMembershipBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "workgroup_membership_manage_denied", "无法授予工作组成员资格", "当前后台身份没有成员管理权限。")
		return generated.GrantAdminWorkgroupMembership403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrUserNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "workgroup_user_not_found", "用户不存在", "请检查后台数字用户 ID。")
		return generated.GrantAdminWorkgroupMembership404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrMembershipAlreadyActive), errors.Is(err, workgroups.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "workgroup_membership_conflict", "成员资格已经变化", "该用户已经是有效成员，或请求标识已使用。")
		return generated.GrantAdminWorkgroupMembership409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.GrantAdminWorkgroupMembership201JSONResponse(workgroupMembershipDTO(membership)), nil
}

func (h *Handler) ChangeAdminWorkgroupMembership(ctx context.Context, request generated.ChangeAdminWorkgroupMembershipRequestObject) (generated.ChangeAdminWorkgroupMembershipResponseObject, error) {
	if request.Body == nil {
		return changeWorkgroupMembershipBadRequest(ctx), nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, string(request.Params.XCSRFToken))
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ChangeAdminWorkgroupMembership401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ChangeAdminWorkgroupMembership403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	membership, err := h.workgroups.ChangeMembership(
		ctx, staffActor(session), request.Params.IdempotencyKey, request.MembershipId,
		workgroups.GroupKind(request.GroupKind), request.Body.ExpectedVersion,
		workgroups.MembershipTransition(request.Body.Transition), request.Body.Reason,
	)
	switch {
	case errors.Is(err, workgroups.ErrInput), errors.Is(err, workgroups.ErrMembershipTransition):
		return changeWorkgroupMembershipBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "workgroup_membership_manage_denied", "无法修改工作组成员资格", "当前后台身份没有成员管理权限。")
		return generated.ChangeAdminWorkgroupMembership403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrMembershipNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "workgroup_membership_not_found", "成员资格不存在", "目标成员资格不存在或不属于该工作组。")
		return generated.ChangeAdminWorkgroupMembership404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, workgroups.ErrMembershipConflict), errors.Is(err, workgroups.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "workgroup_membership_version_conflict", "成员资格已经变化", "请刷新成员列表后重新操作。")
		return generated.ChangeAdminWorkgroupMembership409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ChangeAdminWorkgroupMembership200JSONResponse(workgroupMembershipDTO(membership)), nil
}

func createWorkgroupApplicationBadRequest(ctx context.Context) generated.CreateMyWorkgroupApplication400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_workgroup_application", "工作组申请无效", "申请说明需要 20 至 1000 个字符。")
	return generated.CreateMyWorkgroupApplication400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func decideWorkgroupApplicationBadRequest(ctx context.Context) generated.DecideAdminWorkgroupApplication400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_workgroup_application_decision", "工作组审批内容无效", "请选择批准或驳回，并填写至少 10 个字符的理由。")
	return generated.DecideAdminWorkgroupApplication400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func grantWorkgroupMembershipBadRequest(ctx context.Context) generated.GrantAdminWorkgroupMembership400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_workgroup_membership_grant", "工作组成员授予无效", "请填写正整数用户 ID 和至少 10 个字符的理由。")
	return generated.GrantAdminWorkgroupMembership400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func changeWorkgroupMembershipBadRequest(ctx context.Context) generated.ChangeAdminWorkgroupMembership400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_workgroup_membership_transition", "工作组成员变更无效", "请核对状态版本、变更动作和至少 10 个字符的理由。")
	return generated.ChangeAdminWorkgroupMembership400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func issueWorkgroupContributionPolicyBadRequest(ctx context.Context) generated.IssueAdminWorkgroupContributionPolicy400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_workgroup_contribution_policy", "贡献目标无效", "请选择未来 24 个月内的 UTC 自然月，并填写有效目标和至少 10 个字符的理由。")
	return generated.IssueAdminWorkgroupContributionPolicy400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func myWorkgroupOverviewDTO(overview workgroups.MyOverview) generated.MyWorkgroupOverview {
	items := make([]generated.MyWorkgroup, 0, len(overview.Items))
	for _, item := range overview.Items {
		dto := generated.MyWorkgroup{Definition: workgroupDefinitionDTO(item.Definition)}
		if item.Membership != nil {
			value := workgroupMembershipDTO(*item.Membership)
			dto.Membership = &value
		}
		if item.Application != nil {
			value := workgroupApplicationDTO(*item.Application)
			dto.Application = &value
		}
		if item.Eligibility != nil {
			value := reviewerEligibilityDTO(*item.Eligibility)
			dto.Eligibility = &value
		}
		items = append(items, dto)
	}
	return generated.MyWorkgroupOverview{Items: items}
}

func adminWorkgroupOverviewDTO(overview workgroups.AdminOverview) generated.AdminWorkgroupOverview {
	definitions := make([]generated.WorkgroupDefinition, 0, len(overview.Definitions))
	for _, definition := range overview.Definitions {
		definitions = append(definitions, workgroupDefinitionDTO(definition))
	}
	summaries := make([]generated.WorkgroupContributionSummary, 0, len(overview.ContributionSummaries))
	for _, summary := range overview.ContributionSummaries {
		summaries = append(summaries, workgroupContributionSummaryDTO(summary))
	}
	return generated.AdminWorkgroupOverview{
		Definitions: definitions, PendingApplications: overview.PendingApplications,
		ActiveReseedMembers:    overview.ActiveByKind[workgroups.GroupReseed],
		ActiveReviewMembers:    overview.ActiveByKind[workgroups.GroupReview],
		ActiveRetentionMembers: overview.ActiveByKind[workgroups.GroupRetention],
		ContributionSummaries:  summaries,
	}
}

func workgroupDefinitionDTO(definition workgroups.Definition) generated.WorkgroupDefinition {
	return generated.WorkgroupDefinition{
		Kind: generated.WorkgroupKind(definition.Kind), DisplayName: definition.DisplayName,
		Description: definition.Description, JoinMode: generated.WorkgroupJoinMode(definition.JoinMode),
		Entitlement: generated.WorkgroupEntitlement(definition.Entitlement), Enabled: definition.Enabled,
		SortOrder: definition.SortOrder, Version: definition.Version,
	}
}

func reviewerEligibilityDTO(eligibility workgroups.ReviewerEligibility) generated.ReviewerEligibility {
	return generated.ReviewerEligibility{
		PolicyRevision: eligibility.PolicyRevision, Eligible: eligibility.Eligible,
		Level: eligibility.Level, MinimumLevel: eligibility.MinimumLevel,
		CreditedUploadedBytes:        strconv.FormatInt(eligibility.CreditedUploaded, 10),
		MinimumCreditedUploadedBytes: strconv.FormatInt(eligibility.MinimumCreditedUploaded, 10),
		AccountAgeDays:               eligibility.AccountAgeDays, MinimumAccountAgeDays: eligibility.MinimumAccountAgeDays,
		EmailVerified: eligibility.EmailVerified, RequireVerifiedEmail: eligibility.RequireVerifiedEmail,
		DownloadRestricted: eligibility.DownloadRestricted, RequireUnrestrictedDownload: eligibility.RequireUnrestrictedDownload,
		AccountActive: eligibility.AccountActive,
	}
}

func workgroupApplicationDTO(application workgroups.Application) generated.WorkgroupApplication {
	return generated.WorkgroupApplication{
		Id: application.ID, GroupKind: generated.WorkgroupKind(application.GroupKind),
		ApplicantId: application.ApplicantID, ApplicantNumericId: application.ApplicantNumericID,
		ApplicantUsername: application.ApplicantUsername, ApplicantDisplayName: application.ApplicantDisplayName,
		Statement: application.Statement, Status: generated.WorkgroupApplicationStatus(application.Status),
		PolicyRevision: application.PolicyRevision, Eligibility: reviewerEligibilityDTO(application.Eligibility),
		Version: application.Version, SubmittedAt: application.SubmittedAt, DecidedAt: application.DecidedAt,
	}
}

func workgroupMembershipDTO(membership workgroups.Membership) generated.WorkgroupMembership {
	dto := generated.WorkgroupMembership{
		Id: membership.ID, GroupKind: generated.WorkgroupKind(membership.GroupKind),
		UserId: membership.UserID, UserNumericId: membership.UserNumericID,
		Username: membership.Username, DisplayName: membership.DisplayName,
		Status: generated.WorkgroupMembershipStatus(membership.Status),
		Source: generated.WorkgroupMembershipSource(membership.Source), Version: membership.Version,
		StartedAt: membership.StartedAt, EndedAt: membership.EndedAt, UpdatedAt: membership.UpdatedAt,
	}
	if membership.Contribution != nil {
		value := workgroupContributionDTO(*membership.Contribution)
		dto.Contribution = &value
	}
	if membership.LegacyReviewer != nil {
		dto.LegacyReviewer = &generated.LegacyReviewerEvidence{
			Status:         generated.LegacyReviewerEvidenceStatus(membership.LegacyReviewer.Status),
			ActivityStatus: generated.LegacyReviewerEvidenceActivityStatus(membership.LegacyReviewer.ActivityStatus),
			TotalReviews:   membership.LegacyReviewer.TotalReviews,
			AccurateCount:  membership.LegacyReviewer.AccurateCount,
			LastActivityAt: membership.LegacyReviewer.LastActivityAt,
		}
	}
	return dto
}

func workgroupTaskPageDTO(page workgroups.TaskPage) generated.WorkgroupTaskPage {
	items := make([]generated.WorkgroupTask, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, workgroupTaskDTO(item))
	}
	return generated.WorkgroupTaskPage{Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset}
}

func workgroupTaskAssignmentPageDTO(page workgroups.TaskAssignmentPage) generated.WorkgroupTaskAssignmentPage {
	items := make([]generated.WorkgroupTaskAssignment, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, workgroupTaskAssignmentDTO(item))
	}
	return generated.WorkgroupTaskAssignmentPage{Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset}
}

func workgroupTaskDTO(task workgroups.Task) generated.WorkgroupTask {
	return generated.WorkgroupTask{
		Id: task.ID, GroupKind: generated.WorkgroupKind(task.GroupKind),
		TaskType: generated.WorkgroupTaskType(task.Type), Title: task.Title,
		Description: task.Description, StartsAt: task.StartsAt, DueAt: task.DueAt,
		TimelineState:   generated.WorkgroupTaskTimelineState(task.TimelineState),
		AssignmentCount: task.AssignmentCount, SubmittedCount: task.SubmittedCount,
		PendingReviewCount: task.PendingReviewCount, AcceptedCount: task.AcceptedCount,
		CreatedAt: task.CreatedAt, Replayed: task.Replayed,
	}
}

func workgroupTaskAssignmentDTO(assignment workgroups.TaskAssignment) generated.WorkgroupTaskAssignment {
	dto := generated.WorkgroupTaskAssignment{
		Id: assignment.ID, UserNumericId: assignment.UserNumericID,
		Username: assignment.Username, DisplayName: assignment.DisplayName,
		Task:      workgroupTaskDTO(assignment.Task),
		State:     generated.WorkgroupTaskAssignmentState(assignment.State),
		CanSubmit: assignment.CanSubmit,
	}
	if assignment.LatestSubmission != nil {
		submission := generated.WorkgroupTaskSubmission{
			Id: assignment.LatestSubmission.ID, Sequence: assignment.LatestSubmission.Sequence,
			Statement:   assignment.LatestSubmission.Statement,
			SubmittedAt: assignment.LatestSubmission.SubmittedAt,
			DecidedAt:   assignment.LatestSubmission.DecidedAt,
		}
		if assignment.LatestSubmission.Decision != nil {
			value := generated.WorkgroupTaskReviewDecision(*assignment.LatestSubmission.Decision)
			submission.Decision = &value
		}
		if assignment.LatestSubmission.ReviewReason != "" {
			value := assignment.LatestSubmission.ReviewReason
			submission.ReviewReason = &value
		}
		dto.LatestSubmission = &submission
	}
	return dto
}

func workgroupContributionDTO(progress workgroups.ContributionProgress) generated.WorkgroupContributionProgress {
	return generated.WorkgroupContributionProgress{
		GroupKind:      generated.WorkgroupKind(progress.GroupKind),
		Metric:         generated.WorkgroupContributionMetric(progress.Metric),
		PolicyRevision: progress.PolicyRevision,
		PeriodKind:     generated.WorkgroupContributionProgressPeriodKindCalendarMonth,
		PeriodStartsAt: progress.PeriodStartsAt, PeriodEndsAt: progress.PeriodEndsAt,
		ObservedAt: progress.ObservedAt, EvidenceThrough: progress.EvidenceThrough,
		CurrentValue: progress.CurrentValue, TargetValue: progress.TargetValue,
		Met:             progress.Met,
		EnforcementMode: generated.WorkgroupContributionEnforcementMode(progress.EnforcementMode),
		AllowedMisses:   progress.AllowedMisses, MissCount: progress.MissCount,
	}
}

func workgroupContributionCyclePageDTO(page workgroups.ContributionCyclePage) generated.WorkgroupContributionCyclePage {
	items := make([]generated.WorkgroupContributionCycle, 0, len(page.Items))
	for _, cycle := range page.Items {
		items = append(items, workgroupContributionCycleDTO(cycle))
	}
	return generated.WorkgroupContributionCyclePage{Items: items, Limit: page.Limit}
}

func workgroupContributionCycleDTO(cycle workgroups.ContributionCycle) generated.WorkgroupContributionCycle {
	dto := generated.WorkgroupContributionCycle{
		GroupKind:        generated.WorkgroupKind(cycle.GroupKind),
		Metric:           generated.WorkgroupContributionMetric(cycle.Metric),
		PolicyRevision:   cycle.PolicyRevision,
		PeriodStartsAt:   cycle.PeriodStartsAt,
		PeriodEndsAt:     cycle.PeriodEndsAt,
		ObservedAt:       cycle.ObservedAt,
		EvidenceThrough:  cycle.EvidenceThrough,
		EvidenceState:    generated.WorkgroupContributionEvidenceState(cycle.EvidenceState),
		ActiveSeconds:    cycle.ActiveSeconds,
		FullPeriodActive: cycle.FullPeriodActive,
		CurrentValue:     cycle.CurrentValue,
		TargetValue:      cycle.TargetValue,
		AssessmentState:  generated.WorkgroupContributionAssessmentState(cycle.AssessmentState),
		ExplanationCode:  generated.WorkgroupContributionExplanationCode(cycle.ExplanationCode),
		EnforcementMode:  generated.WorkgroupContributionEnforcementMode(cycle.EnforcementMode),
		AllowedMisses:    cycle.AllowedMisses,
	}
	if cycle.Reminder != nil {
		value := workgroupContributionReminderDTO(*cycle.Reminder)
		dto.Reminder = &value
	}
	if cycle.Enforcement != nil {
		value := workgroupContributionEnforcementDTO(*cycle.Enforcement)
		dto.Enforcement = &value
	}
	return dto
}

func workgroupContributionEnforcementDTO(assessment workgroups.ContributionEnforcementAssessment) generated.WorkgroupContributionEnforcementAssessment {
	return generated.WorkgroupContributionEnforcementAssessment{
		Id:                 assessment.ID,
		GroupKind:          generated.WorkgroupKind(assessment.GroupKind),
		Metric:             generated.WorkgroupContributionMetric(assessment.Metric),
		PolicyRevision:     assessment.PolicyRevision,
		PeriodStartsAt:     assessment.PeriodStartsAt,
		PeriodEndsAt:       assessment.PeriodEndsAt,
		ObservedAt:         assessment.ObservedAt,
		EvidenceThrough:    assessment.EvidenceThrough,
		EvidenceState:      generated.WorkgroupContributionEnforcementAssessmentEvidenceState(assessment.EvidenceState),
		CurrentValue:       assessment.CurrentValue,
		TargetValue:        assessment.TargetValue,
		AssessmentState:    generated.WorkgroupContributionEnforcementAssessmentAssessmentState(assessment.AssessmentState),
		ExplanationCode:    generated.WorkgroupContributionEnforcementAssessmentExplanationCode(assessment.ExplanationCode),
		MissCount:          assessment.MissCount,
		AllowedMisses:      assessment.AllowedMisses,
		DisciplinaryAction: generated.WorkgroupContributionDisciplinaryAction(assessment.DisciplinaryAction),
		Reason:             assessment.Reason,
		AssessedAt:         assessment.AssessedAt,
	}
}

func workgroupContributionReminderDTO(reminder workgroups.ContributionReminder) generated.WorkgroupContributionReminder {
	return generated.WorkgroupContributionReminder{
		Id: reminder.ID, GroupKind: generated.WorkgroupKind(reminder.GroupKind),
		Metric: generated.WorkgroupContributionMetric(reminder.Metric), PolicyRevision: reminder.PolicyRevision,
		PeriodStartsAt: reminder.PeriodStartsAt, PeriodEndsAt: reminder.PeriodEndsAt,
		ObservedAt: reminder.ObservedAt, EvidenceThrough: reminder.EvidenceThrough,
		EvidenceState: generated.WorkgroupContributionEvidenceState(reminder.EvidenceState),
		CurrentValue:  reminder.CurrentValue, TargetValue: reminder.TargetValue,
		AssessmentState: generated.WorkgroupContributionAssessmentState(reminder.AssessmentState),
		ExplanationCode: generated.WorkgroupContributionExplanationCode(reminder.ExplanationCode),
		Reason:          reminder.Reason, CreatedAt: reminder.CreatedAt,
		ReadAt: reminder.NotificationReadAt, Replayed: reminder.Replayed,
	}
}

func workgroupContributionPolicyDTO(policy workgroups.ContributionPolicy) generated.WorkgroupContributionPolicyRevision {
	return generated.WorkgroupContributionPolicyRevision{
		GroupKind: generated.WorkgroupKind(policy.GroupKind), Revision: policy.Revision,
		Metric:     generated.WorkgroupContributionMetric(policy.Metric),
		PeriodKind: generated.WorkgroupContributionPolicyRevisionPeriodKindCalendarMonth, TargetValue: policy.TargetValue,
		EnforcementMode: generated.WorkgroupContributionEnforcementMode(policy.EnforcementMode),
		AllowedMisses:   policy.AllowedMisses, EffectiveFrom: policy.EffectiveFrom,
		Opening: policy.Opening, Reason: policy.Reason, CreatedAt: policy.CreatedAt,
		TimelineState: generated.WorkgroupContributionPolicyRevisionTimelineState(policy.TimelineState),
		Replayed:      policy.Replayed,
	}
}

func workgroupContributionSummaryDTO(summary workgroups.ContributionSummary) generated.WorkgroupContributionSummary {
	return generated.WorkgroupContributionSummary{
		GroupKind:      generated.WorkgroupKind(summary.GroupKind),
		Metric:         generated.WorkgroupContributionMetric(summary.Metric),
		PolicyRevision: summary.PolicyRevision,
		PeriodStartsAt: summary.PeriodStartsAt, PeriodEndsAt: summary.PeriodEndsAt,
		ObservedAt: summary.ObservedAt, EvidenceThrough: summary.EvidenceThrough,
		ActiveMembers: summary.ActiveMembers, ContributingMembers: summary.ContributingMembers,
		MetMembers: summary.MetMembers, TotalValue: summary.TotalValue, TargetValue: summary.TargetValue,
	}
}
