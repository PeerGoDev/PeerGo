package httpapi

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/economy/attendance"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

func (h *Handler) GetMyAttendance(ctx context.Context, _ generated.GetMyAttendanceRequestObject) (generated.GetMyAttendanceResponseObject, error) {
	result, err := h.attendance.MyOverview(ctx, sessionTokenFromContext(ctx))
	switch {
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后查看签到状态。")
		return generated.GetMyAttendance401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "attendance_read_denied", "无法查看签到", "当前账号没有签到查看权限。")
		return generated.GetMyAttendance403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.GetMyAttendance200JSONResponse(attendanceOverviewDTO(result)), nil
}

func (h *Handler) ClaimMyAttendance(ctx context.Context, request generated.ClaimMyAttendanceRequestObject) (generated.ClaimMyAttendanceResponseObject, error) {
	if request.Body == nil {
		return attendanceClaimBadRequest(ctx), nil
	}
	record, err := h.attendance.Claim(
		ctx,
		sessionTokenFromContext(ctx),
		string(request.Params.XCSRFToken),
		request.Params.IdempotencyKey,
		attendance.Mode(request.Body.Mode),
	)
	switch {
	case errors.Is(err, attendance.ErrInput), errors.Is(err, attendance.ErrModeDisabled):
		return attendanceClaimBadRequest(ctx), nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后签到。")
		return generated.ClaimMyAttendance401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.ClaimMyAttendance403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "attendance_claim_denied", "暂时不能签到", "当前账号没有签到领取权限。")
		return generated.ClaimMyAttendance403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, attendance.ErrPolicyNotFound), errors.Is(err, attendance.ErrDisabled):
		problem := newProblemFromContext(ctx, http.StatusConflict, "attendance_unavailable", "签到暂未开放", "当前没有已生效的签到政策。")
		return generated.ClaimMyAttendance409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, attendance.ErrAlreadyClaimed):
		problem := newProblemFromContext(ctx, http.StatusConflict, "attendance_already_claimed", "今天已经签到", "每天只能领取一次签到奖励。")
		return generated.ClaimMyAttendance409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, attendance.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "attendance_idempotency_conflict", "请求标识已被使用", "请刷新签到状态后重新操作。")
		return generated.ClaimMyAttendance409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ClaimMyAttendance201JSONResponse(attendanceRecordDTO(record)), nil
}

func (h *Handler) ListAttendancePolicies(ctx context.Context, request generated.ListAttendancePoliciesRequestObject) (generated.ListAttendancePoliciesResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListAttendancePolicies401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListAttendancePolicies403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	limit, offset := attendance.DefaultPolicyLimit, 0
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := h.attendance.ListPolicies(ctx, staffActor(session), limit, offset)
	switch {
	case errors.Is(err, attendance.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_attendance_policy_query", "签到政策查询无效", "请检查分页参数。")
		return generated.ListAttendancePolicies400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "attendance_policy_read_denied", "无法查看签到设置", "当前后台身份没有签到政策读取权限。")
		return generated.ListAttendancePolicies403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	items := make([]generated.AttendancePolicy, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, attendancePolicyDTO(item))
	}
	return generated.ListAttendancePolicies200JSONResponse{
		Items: items, Total: strconv.FormatInt(page.Total, 10), Limit: page.Limit,
		Offset: page.Offset, MinimumEffectiveFrom: page.MinimumEffectiveFrom,
	}, nil
}

func (h *Handler) IssueAttendancePolicy(ctx context.Context, request generated.IssueAttendancePolicyRequestObject) (generated.IssueAttendancePolicyResponseObject, error) {
	if request.Body == nil {
		return attendancePolicyBadRequest(ctx), nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.IssueAttendancePolicy401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.IssueAttendancePolicy403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	published, err := h.attendance.IssuePolicy(
		ctx, staffActor(session), request.Params.IdempotencyKey,
		attendancePolicyFromSettings(request.Body.Settings), request.Body.Reason,
	)
	switch {
	case errors.Is(err, attendance.ErrInput):
		return attendancePolicyBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "attendance_policy_issue_denied", "无法保存签到设置", "当前后台身份没有签到政策签发权限。")
		return generated.IssueAttendancePolicy403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, attendance.ErrPolicyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "attendance_policy_conflict", "签到设置发生冲突", "请刷新页面后重新保存，既有签到记录不会被覆盖。")
		return generated.IssueAttendancePolicy409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.IssueAttendancePolicy201JSONResponse(attendancePolicyDTO(published)), nil
}

func attendanceClaimBadRequest(ctx context.Context) generated.ClaimMyAttendance400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_attendance_claim", "签到方式无效", "请选择当前政策允许的固定奖励或随机奖励。")
	return generated.ClaimMyAttendance400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func attendancePolicyBadRequest(ctx context.Context) generated.IssueAttendancePolicy400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_attendance_policy", "签到设置无效", "请检查奖励范围、时区、连续天数和至少 10 个字符的变更原因。")
	return generated.IssueAttendancePolicy400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func attendanceOverviewDTO(overview attendance.Overview) generated.AttendanceOverview {
	result := generated.AttendanceOverview{
		ClaimedToday: overview.ClaimedToday, CurrentStreak: int(overview.CurrentStreak),
		TotalDays: int(overview.TotalDays), LongestStreak: int(overview.LongestStreak),
		History: make([]generated.AttendanceRecord, 0, len(overview.History)),
	}
	if overview.Today != "" {
		if parsed, err := time.Parse(time.DateOnly, overview.Today); err == nil {
			value := openapi_types.Date{Time: parsed}
			result.Today = &value
		}
	}
	if overview.Policy != nil {
		value := attendancePolicyDTO(*overview.Policy)
		result.Policy = &value
	}
	if overview.TodayRecord != nil {
		value := attendanceRecordDTO(*overview.TodayRecord)
		result.TodayRecord = &value
	}
	for _, record := range overview.History {
		result.History = append(result.History, attendanceRecordDTO(record))
	}
	return result
}

func attendanceRecordDTO(record attendance.Record) generated.AttendanceRecord {
	date, _ := time.Parse(time.DateOnly, record.AttendanceDate)
	return generated.AttendanceRecord{
		AttendanceDate: openapi_types.Date{Time: date}, DayBoundaryTimezone: record.DayBoundaryTimezone,
		Mode: generated.AttendanceMode(record.Mode), BaseReward: strconv.FormatInt(record.BaseReward, 10),
		StreakReward: strconv.FormatInt(record.StreakReward, 10), TotalReward: strconv.FormatInt(record.TotalReward, 10),
		ExperienceReward: strconv.FormatInt(record.ExperienceReward, 10), CurrentStreak: int(record.CurrentStreak),
		TotalDays: int(record.TotalDays), LongestStreak: int(record.LongestStreak),
		PolicyRevision: record.PolicyRevision, OccurredAt: record.OccurredAt,
	}
}

func attendancePolicyFromSettings(settings generated.AttendancePolicySettings) attendance.PolicyRevision {
	milestones := make([]attendance.StreakMilestone, 0, len(settings.StreakMilestones))
	for _, milestone := range settings.StreakMilestones {
		reward, err := strconv.ParseInt(milestone.Reward, 10, 64)
		if err != nil {
			reward = -1
		}
		milestones = append(milestones, attendance.StreakMilestone{Days: int32(milestone.Days), Reward: reward})
	}
	parse := func(value string) int64 {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return -1
		}
		return parsed
	}
	return attendance.PolicyRevision{
		Enabled: settings.Enabled, DayBoundaryTimezone: settings.DayBoundaryTimezone,
		FixedEnabled: settings.FixedEnabled, FixedReward: parse(settings.FixedReward),
		RandomEnabled: settings.RandomEnabled, RandomMin: parse(settings.RandomMin), RandomMax: parse(settings.RandomMax),
		StreakEnabled: settings.StreakEnabled, StreakMilestones: milestones,
		ExperienceReward: parse(settings.ExperienceReward),
	}
}

func attendancePolicyDTO(published attendance.PublishedPolicy) generated.AttendancePolicy {
	policy := published.Policy
	milestones := make([]generated.AttendanceStreakMilestone, 0, len(policy.StreakMilestones))
	for _, milestone := range policy.StreakMilestones {
		milestones = append(milestones, generated.AttendanceStreakMilestone{Days: int(milestone.Days), Reward: strconv.FormatInt(milestone.Reward, 10)})
	}
	result := generated.AttendancePolicy{
		Revision: policy.Revision, EffectiveFrom: policy.EffectiveFrom, CreatedAt: policy.CreatedAt,
		Settings: generated.AttendancePolicySettings{
			Enabled: policy.Enabled, DayBoundaryTimezone: policy.DayBoundaryTimezone,
			FixedEnabled: policy.FixedEnabled, FixedReward: strconv.FormatInt(policy.FixedReward, 10),
			RandomEnabled: policy.RandomEnabled, RandomMin: strconv.FormatInt(policy.RandomMin, 10), RandomMax: strconv.FormatInt(policy.RandomMax, 10),
			StreakEnabled: policy.StreakEnabled, StreakMilestones: milestones,
			ExperienceReward: strconv.FormatInt(policy.ExperienceReward, 10),
		},
		SnapshotSha256: hex.EncodeToString(policy.SnapshotSHA256[:]), Reason: published.Reason,
	}
	if published.IssuedBy != nil {
		value := openapi_types.UUID(*published.IssuedBy)
		result.IssuedBy = &value
	}
	return result
}
