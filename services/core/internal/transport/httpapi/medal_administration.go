package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/economy/medals"
)

func (h *Handler) ListMedalDefinitions(ctx context.Context, _ generated.ListMedalDefinitionsRequestObject) (generated.ListMedalDefinitionsResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListMedalDefinitions401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem)}, nil
		}
		return generated.ListMedalDefinitions403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	overview, err := h.medalAdministration.Overview(ctx, staffActor(session))
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "medal_manage_read_denied", "无法查看勋章管理", "当前后台身份没有 economy.medal.manage.read 权限。")
		return generated.ListMedalDefinitions403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]generated.MedalDefinition, 0, len(overview.Items))
	for _, definition := range overview.Items {
		items = append(items, medalDefinitionDTO(definition))
	}
	return generated.ListMedalDefinitions200JSONResponse{
		Settings: medalSettingsDTO(overview.Settings), Items: items,
		Total: strconv.FormatInt(int64(len(items)), 10),
	}, nil
}

func (h *Handler) UpdateMedalSettings(ctx context.Context, request generated.UpdateMedalSettingsRequestObject) (generated.UpdateMedalSettingsResponseObject, error) {
	if request.Body == nil {
		problem := medalSettingsInputProblem(ctx)
		return generated.UpdateMedalSettings400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.UpdateMedalSettings401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.UpdateMedalSettings403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	input, err := medalSettingsInput(*request.Body)
	if err != nil {
		problem := medalSettingsInputProblem(ctx)
		return generated.UpdateMedalSettings400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	result, err := h.medalAdministration.UpdateSettings(ctx, staffActor(session), input)
	switch {
	case errors.Is(err, medals.ErrInput):
		problem := medalSettingsInputProblem(ctx)
		return generated.UpdateMedalSettings400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "medal_settings_update_denied", "没有权限修改全站勋章规则", "当前后台身份没有 economy.medal.update 权限。")
		return generated.UpdateMedalSettings403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, medals.ErrSettingsConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "medal_settings_version_conflict", "全站勋章规则已经变化", "当前编辑基于旧版本，请重新载入后再提交。")
		return generated.UpdateMedalSettings409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.UpdateMedalSettings200JSONResponse(medalSettingsDTO(result)), nil
}

func (h *Handler) CreateMedalDefinition(ctx context.Context, request generated.CreateMedalDefinitionRequestObject) (generated.CreateMedalDefinitionResponseObject, error) {
	if request.Body == nil || request.Body.ExpectedVersion != nil {
		problem := medalInputProblem(ctx)
		return generated.CreateMedalDefinition400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.CreateMedalDefinition401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.CreateMedalDefinition403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	input, err := medalDefinitionInput(*request.Body)
	if err != nil {
		problem := medalInputProblem(ctx)
		return generated.CreateMedalDefinition400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	result, err := h.medalAdministration.Create(ctx, staffActor(session), input)
	if errors.Is(err, medals.ErrInput) {
		problem := medalInputProblem(ctx)
		return generated.CreateMedalDefinition400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "medal_create_denied", "没有权限创建勋章", "当前后台身份没有 economy.medal.create 权限。")
		return generated.CreateMedalDefinition403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.CreateMedalDefinition201JSONResponse(medalDefinitionDTO(result)), nil
}

func (h *Handler) UpdateMedalDefinition(ctx context.Context, request generated.UpdateMedalDefinitionRequestObject) (generated.UpdateMedalDefinitionResponseObject, error) {
	if request.Body == nil || request.Body.ExpectedVersion == nil {
		problem := medalInputProblem(ctx)
		return generated.UpdateMedalDefinition400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.UpdateMedalDefinition401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.UpdateMedalDefinition403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	input, err := medalDefinitionInput(*request.Body)
	if err != nil {
		problem := medalInputProblem(ctx)
		return generated.UpdateMedalDefinition400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	result, err := h.medalAdministration.Update(ctx, staffActor(session), request.MedalId, *request.Body.ExpectedVersion, input)
	switch {
	case errors.Is(err, medals.ErrInput):
		problem := medalInputProblem(ctx)
		return generated.UpdateMedalDefinition400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "medal_update_denied", "没有权限修改勋章", "当前后台身份没有 economy.medal.update 权限。")
		return generated.UpdateMedalDefinition403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, medals.ErrNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "medal_not_found", "勋章不存在", "目标勋章已经不存在，请刷新列表。")
		return generated.UpdateMedalDefinition404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, medals.ErrVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "medal_version_conflict", "勋章版本已经变化", "当前编辑基于旧版本，请重新载入后再提交。")
		return generated.UpdateMedalDefinition409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.UpdateMedalDefinition200JSONResponse(medalDefinitionDTO(result)), nil
}

func medalDefinitionInput(body generated.MedalDefinitionWriteRequest) (medals.DefinitionInput, error) {
	price, err := strconv.ParseInt(string(body.Price), 10, 64)
	if err != nil {
		return medals.DefinitionInput{}, err
	}
	inviteBonus, err := strconv.ParseInt(string(body.InviteBonus), 10, 64)
	if err != nil {
		return medals.DefinitionInput{}, err
	}
	periodicReward, err := strconv.ParseInt(string(body.PeriodicRewardMagic), 10, 64)
	if err != nil {
		return medals.DefinitionInput{}, err
	}
	var inventory *int64
	if body.Inventory != nil {
		value, err := strconv.ParseInt(*body.Inventory, 10, 64)
		if err != nil {
			return medals.DefinitionInput{}, err
		}
		inventory = &value
	}
	var rewardCycle *string
	if body.RewardCycle != nil {
		value := string(*body.RewardCycle)
		rewardCycle = &value
	}
	return medals.DefinitionInput{
		Name: body.Name, Description: body.Description,
		ImageLargePath: body.ImageLargePath, ImageSmallPath: body.ImageSmallPath,
		AcquisitionMethod: medals.AcquisitionMethod(body.AcquisitionMethod),
		Price:             price, DurationDays: body.DurationDays, DisplayOnPage: body.DisplayOnPage,
		Priority: body.Priority, UploadBonusBPS: body.UploadBonusBps,
		DownloadDiscountBPS: body.DownloadDiscountBps, MagicBonusBPS: body.MagicBonusBps,
		InviteBonus: inviteBonus, PoolEligible: body.PoolEligible,
		PeriodicRewardMagic: periodicReward, RewardCycle: rewardCycle,
		SaleBeginAt: body.SaleBeginAt, SaleEndAt: body.SaleEndAt,
		Inventory: inventory, Reason: body.Reason,
	}, nil
}

func medalSettingsInput(body generated.MedalSettingsWriteRequest) (medals.SettingsInput, error) {
	maximumInviteBonus, err := strconv.ParseInt(string(body.MaximumInviteBonus), 10, 64)
	if err != nil {
		return medals.SettingsInput{}, err
	}
	return medals.SettingsInput{
		Enabled: body.Enabled, MaximumWearCount: body.MaximumWearCount,
		MaximumUploadBonusBPS:      body.MaximumUploadBonusBps,
		MaximumDownloadDiscountBPS: body.MaximumDownloadDiscountBps,
		MaximumMagicBonusBPS:       body.MaximumMagicBonusBps,
		MaximumInviteBonus:         maximumInviteBonus,
		ExpectedVersion:            body.ExpectedVersion, Reason: body.Reason,
	}, nil
}

func medalDefinitionDTO(value medals.Definition) generated.MedalDefinition {
	var rewardCycle *generated.MedalDefinitionRewardCycle
	if value.RewardCycle != nil {
		converted := generated.MedalDefinitionRewardCycle(*value.RewardCycle)
		rewardCycle = &converted
	}
	return generated.MedalDefinition{
		Id: strconv.FormatInt(value.ID, 10), Name: value.Name, Description: value.Description,
		ImageLargePath: value.ImageLargePath, ImageSmallPath: value.ImageSmallPath,
		AcquisitionMethod: generated.MedalAcquisitionMethod(value.AcquisitionMethod),
		Price:             strconv.FormatInt(value.Price, 10), DurationDays: value.DurationDays,
		DisplayOnPage: value.DisplayOnPage, Priority: value.Priority,
		UploadBonusBps: value.UploadBonusBPS, DownloadDiscountBps: value.DownloadDiscountBPS,
		MagicBonusBps: value.MagicBonusBPS, InviteBonus: strconv.FormatInt(value.InviteBonus, 10),
		IsWorkgroup: value.IsWorkgroup, PoolEligible: value.PoolEligible,
		PeriodicRewardMagic: strconv.FormatInt(value.PeriodicRewardMagic, 10),
		RewardCycle:         rewardCycle, SaleBeginAt: value.SaleBeginAt, SaleEndAt: value.SaleEndAt,
		Inventory: optionalInt64Text(value.Inventory), ConditionsCount: strconv.FormatInt(value.ConditionsCount, 10),
		PrivilegesCount: strconv.FormatInt(value.PrivilegesCount, 10), Version: value.Version,
		HolderCount: strconv.FormatInt(value.HolderCount, 10), ActiveHolderCount: strconv.FormatInt(value.ActiveHolderCount, 10),
		WearingCount: strconv.FormatInt(value.WearingCount, 10), CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func medalSettingsDTO(value medals.Settings) generated.MedalSettings {
	return generated.MedalSettings{
		Enabled: value.Enabled, MaximumWearCount: value.MaximumWearCount,
		MaximumUploadBonusBps:      value.MaximumUploadBonusBPS,
		MaximumDownloadDiscountBps: value.MaximumDownloadDiscountBPS,
		MaximumMagicBonusBps:       value.MaximumMagicBonusBPS,
		MaximumInviteBonus:         strconv.FormatInt(value.MaximumInviteBonus, 10),
		ConditionCheckDay:          value.ConditionCheckDay, ConditionWarningDays: value.ConditionWarningDays,
		Version: value.Version, UpdatedAt: value.UpdatedAt,
	}
}

func optionalInt64Text(value *int64) *string {
	if value == nil {
		return nil
	}
	result := strconv.FormatInt(*value, 10)
	return &result
}

func medalInputProblem(ctx context.Context) generated.Problem {
	return newProblemFromContext(ctx, http.StatusBadRequest, "invalid_medal_definition", "勋章设置无效", "请检查名称、图片地址、获取方式、整数额度、权益范围、版本和变更理由。")
}

func medalSettingsInputProblem(ctx context.Context) generated.Problem {
	return newProblemFromContext(ctx, http.StatusBadRequest, "invalid_medal_settings", "全站勋章规则无效", "请检查启用状态、佩戴数量、权益上限、版本和变更理由。")
}
