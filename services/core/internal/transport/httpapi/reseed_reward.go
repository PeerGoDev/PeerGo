package httpapi

import (
	"context"
	"errors"
	"net/http"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/economy/reseedreward"
)

type ReseedRewardAdministrationService interface {
	Settings(context.Context, authz.StaffActor) (reseedreward.Settings, error)
	UpdateSettings(context.Context, authz.StaffActor, reseedreward.UpdateInput) (reseedreward.Settings, error)
}

func (h *Handler) WithReseedRewardAdministration(service ReseedRewardAdministrationService) *Handler {
	h.reseedRewardAdministration = service
	return h
}

func (h *Handler) GetReseedRewardSettings(ctx context.Context, _ generated.GetReseedRewardSettingsRequestObject) (generated.GetReseedRewardSettingsResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.GetReseedRewardSettings401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem)}, nil
		}
		return generated.GetReseedRewardSettings403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	if h.reseedRewardAdministration == nil {
		return nil, errors.New("reseed reward administration is not configured")
	}
	settings, err := h.reseedRewardAdministration.Settings(ctx, staffActor(session))
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "reseed_reward_settings_read_denied", "无法查看复活奖励", "当前后台身份缺少读取做种奖励设置的权限。")
		return generated.GetReseedRewardSettings403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.GetReseedRewardSettings200JSONResponse(reseedRewardSettingsDTO(settings)), nil
}

func (h *Handler) UpdateReseedRewardSettings(ctx context.Context, request generated.UpdateReseedRewardSettingsRequestObject) (generated.UpdateReseedRewardSettingsResponseObject, error) {
	session, problem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.UpdateReseedRewardSettings401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.UpdateReseedRewardSettings403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	if request.Body == nil {
		return reseedRewardSettingsBadRequest(ctx), nil
	}
	if h.reseedRewardAdministration == nil {
		return nil, errors.New("reseed reward administration is not configured")
	}
	settings, err := h.reseedRewardAdministration.UpdateSettings(ctx, staffActor(session), reseedreward.UpdateInput{
		ExpectedRevision: request.Body.ExpectedRevision, Enabled: request.Body.Enabled,
		RequiredHours: request.Body.RequiredHours, MagicAmount: request.Body.MagicAmount,
	})
	switch {
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "reseed_reward_settings_update_denied", "无法修改复活奖励", "当前后台身份缺少修改做种奖励设置的权限。")
		return generated.UpdateReseedRewardSettings403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, reseedreward.ErrInput):
		return reseedRewardSettingsBadRequest(ctx), nil
	case errors.Is(err, reseedreward.ErrConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "reseed_reward_settings_revision_conflict", "复活奖励设置已更新", "请重新载入设置后再提交。")
		return generated.UpdateReseedRewardSettings409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.UpdateReseedRewardSettings200JSONResponse(reseedRewardSettingsDTO(settings)), nil
}

func reseedRewardSettingsBadRequest(ctx context.Context) generated.UpdateReseedRewardSettings400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_reseed_reward_settings", "复活奖励设置无效", "有效做种时长须为 1 至 720 小时，奖励须为 1 至 1,000,000 魔力值。")
	return generated.UpdateReseedRewardSettings400ApplicationProblemPlusJSONResponse{
		ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
	}
}

func reseedRewardSettingsDTO(value reseedreward.Settings) generated.ReseedRewardSettings {
	return generated.ReseedRewardSettings{
		Revision: value.Revision, Enabled: value.Enabled, RequiredHours: value.RequiredHours,
		MagicAmount: value.MagicAmount, IssuedAt: value.IssuedAt,
	}
}

func reseedRewardOfferDTO(offer *reseedreward.Offer) *generated.ReseedRewardOffer {
	if offer == nil {
		return nil
	}
	return &generated.ReseedRewardOffer{
		Status: generated.ReseedRewardOfferStatus(offer.Status), RequiredHours: offer.RequiredHours, MagicAmount: offer.MagicAmount,
	}
}
