package httpapi

import (
	"context"
	"errors"
	"net/http"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

func (h *Handler) GetMyInvitations(ctx context.Context, request generated.GetMyInvitationsRequestObject) (generated.GetMyInvitationsResponseObject, error) {
	limit := identity.DefaultInvitationHistoryLimit
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	offset := 0
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	overview, err := h.invitations.Overview(ctx, sessionTokenFromContext(ctx), limit, offset)
	switch {
	case errors.Is(err, identity.ErrInvitationInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_invitation_query", "邀请记录查询无效", "每页数量必须在 1 到 100 之间，偏移量必须在 0 到 99999 之间。")
		return generated.GetMyInvitations400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后查看自己的邀请记录。")
		return generated.GetMyInvitations401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden), errors.Is(err, identity.ErrInvitationIneligible):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "invitation_read_denied", "无法查看邀请", "当前账户暂时不能使用邀请功能。")
		return generated.GetMyInvitations403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.GetMyInvitations200JSONResponse(invitationOverviewDTO(overview)), nil
}

func (h *Handler) IssueMyInvitation(ctx context.Context, request generated.IssueMyInvitationRequestObject) (generated.IssueMyInvitationResponseObject, error) {
	result, err := h.invitations.Issue(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken))
	switch {
	case errors.Is(err, identity.ErrInvitationInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_invitation_issue", "无法签发邀请码", "当前签发请求无效，请刷新页面后重试。")
		return generated.IssueMyInvitation400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后签发邀请码。")
		return generated.IssueMyInvitation401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.IssueMyInvitation403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "invitation_issue_denied", "无法签发邀请码", "当前账户没有签发邀请码的权限。")
		return generated.IssueMyInvitation403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvitationDisabled):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "member_invitations_disabled", "成员邀请暂未开放", "管理员当前没有开放成员签发邀请码。")
		return generated.IssueMyInvitation403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvitationIneligible):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "invitation_issuer_ineligible", "暂时不能签发邀请码", "当前账户尚未满足邮箱、注册时长、等级或账户状态要求。")
		return generated.IssueMyInvitation403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvitationQuota):
		problem := newProblemFromContext(ctx, http.StatusConflict, "invitation_quota_exhausted", "邀请名额已用完", "已成功邀请和当前有效邀请码已经占满可用名额。")
		return generated.IssueMyInvitation409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.IssueMyInvitation201JSONResponse{
		Invitation: memberInvitationDTO(result.Invitation), Token: result.Token,
	}, nil
}

func (h *Handler) RevokeMyInvitation(ctx context.Context, request generated.RevokeMyInvitationRequestObject) (generated.RevokeMyInvitationResponseObject, error) {
	result, err := h.invitations.Revoke(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), request.InvitationId)
	switch {
	case errors.Is(err, identity.ErrInvitationInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_invitation_revocation", "无法撤销邀请码", "邀请码标识无效，请刷新页面后重试。")
		return generated.RevokeMyInvitation400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后撤销邀请码。")
		return generated.RevokeMyInvitation401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.RevokeMyInvitation403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "invitation_revoke_denied", "无法撤销邀请码", "当前账户没有撤销邀请码的权限。")
		return generated.RevokeMyInvitation403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvitationUnavailable), errors.Is(err, identity.ErrInvitationNotFound):
		problem := newProblemFromContext(ctx, http.StatusConflict, "invitation_not_revocable", "邀请码不能撤销", "邀请码可能已经被领取、使用或撤销，请刷新记录后确认。")
		return generated.RevokeMyInvitation409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.RevokeMyInvitation200JSONResponse(memberInvitationDTO(result)), nil
}

func invitationOverviewDTO(overview identity.InvitationOverview) generated.InvitationOverview {
	items := make([]generated.MemberInvitation, 0, len(overview.Items))
	for _, item := range overview.Items {
		items = append(items, memberInvitationDTO(item))
	}
	eligibility := overview.Eligibility
	return generated.InvitationOverview{
		Eligibility: generated.InvitationEligibility{
			Enabled: eligibility.Enabled, Eligible: eligibility.Eligible,
			Blocker:             generated.InvitationEligibilityBlocker(eligibility.Blocker),
			InviteValidDays:     eligibility.InviteValidDays,
			MaxInvitesPerMember: eligibility.MaxInvitesPerMember,
			UsedInvites:         eligibility.UsedInvites, RemainingInvites: eligibility.RemainingInvites,
			MinimumAccountAgeDays: eligibility.MinimumAccountAgeDays,
			CurrentAccountAgeDays: eligibility.CurrentAccountAgeDays,
			MinimumLevel:          eligibility.MinimumLevel, CurrentLevel: eligibility.CurrentLevel,
			EmailVerified: eligibility.EmailVerified,
		},
		Items: items, Total: overview.Total, Limit: overview.Limit,
		Offset: overview.Offset, ObservedAt: overview.ObservedAt,
	}
}

func memberInvitationDTO(item identity.MemberInvitation) generated.MemberInvitation {
	return generated.MemberInvitation{
		Id: item.ID, Status: generated.InvitationStatus(item.Status),
		InviteeUsername: item.InviteeUsername, CreatedAt: item.CreatedAt,
		ExpiresAt: item.ExpiresAt, ClaimedAt: item.ClaimedAt,
		ConsumedAt: item.ConsumedAt, RevokedAt: item.RevokedAt,
	}
}
