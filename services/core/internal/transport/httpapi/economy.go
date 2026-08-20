package httpapi

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/economy"
	"github.com/peergo/peergo/services/core/internal/modules/economy/seedingreward"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/progression"
)

func (h *Handler) GetMyEconomy(ctx context.Context, request generated.GetMyEconomyRequestObject) (generated.GetMyEconomyResponseObject, error) {
	cookieToken := sessionTokenFromContext(ctx)
	if cookieToken == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请登录后查看自己的魔力值和等级。")
		return generated.GetMyEconomy401ApplicationProblemPlusJSONResponse(problem), nil
	}
	limit := economy.DefaultOverviewLimit
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	result, err := h.economyOverview.MyOverview(ctx, cookieToken, limit)
	switch {
	case errors.Is(err, economy.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_economy_query", "账本查询无效", "最近账本条目的返回数量必须在 1 到 100 之间。")
		return generated.GetMyEconomy400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "会话已经失效", "请重新登录后查看自己的魔力值和等级。")
		return generated.GetMyEconomy401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "economy_read_denied", "无法查看账本", "当前账号没有 economy.read.self 权限。")
		return generated.GetMyEconomy403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.GetMyEconomy200JSONResponse(economyOverviewDTO(result)), nil
}

func (h *Handler) ListSeedingRewardPolicies(ctx context.Context, request generated.ListSeedingRewardPoliciesRequestObject) (generated.ListSeedingRewardPoliciesResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListSeedingRewardPolicies401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListSeedingRewardPolicies403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	limit, offset := seedingreward.DefaultPolicyListLimit, 0
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := h.seedingRewardAdministration.List(ctx, staffActor(session), limit, offset)
	switch {
	case errors.Is(err, seedingreward.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_reward_policy_query", "奖励政策查询无效", "请检查分页参数。")
		return generated.ListSeedingRewardPolicies400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "reward_policy_read_denied", "无法查看做种奖励政策", "当前后台身份没有 economy.seedingreward.policy.read 权限。")
		return generated.ListSeedingRewardPolicies403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	items := make([]generated.SeedingRewardPolicy, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, seedingRewardPolicyDTO(item))
	}
	return generated.ListSeedingRewardPolicies200JSONResponse{
		Items: items, Total: strconv.FormatInt(page.Total, 10), Limit: page.Limit,
		Offset: page.Offset, MinimumEffectiveFrom: page.MinimumEffectiveFrom,
	}, nil
}

func (h *Handler) ListContributionExperiencePolicies(ctx context.Context, request generated.ListContributionExperiencePoliciesRequestObject) (generated.ListContributionExperiencePoliciesResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListContributionExperiencePolicies401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListContributionExperiencePolicies403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	limit, offset := 30, 0
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := h.contributionExperience.List(ctx, staffActor(session), limit, offset)
	switch {
	case errors.Is(err, progression.ErrContributionPolicyInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_contribution_experience_policy_query", "经验政策查询无效", "请检查分页参数。")
		return generated.ListContributionExperiencePolicies400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "contribution_experience_policy_read_denied", "无法查看经验获取规则", "当前后台身份没有 progression.contribution.policy.read 权限。")
		return generated.ListContributionExperiencePolicies403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	items := make([]generated.ContributionExperiencePolicy, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, contributionExperiencePolicyDTO(item))
	}
	return generated.ListContributionExperiencePolicies200JSONResponse{
		Items: items, Total: strconv.FormatInt(page.Total, 10), Limit: page.Limit,
		Offset: page.Offset, MinimumEffectiveFrom: page.MinimumEffectiveFrom,
	}, nil
}

func (h *Handler) IssueContributionExperiencePolicy(ctx context.Context, request generated.IssueContributionExperiencePolicyRequestObject) (generated.IssueContributionExperiencePolicyResponseObject, error) {
	if request.Body == nil {
		return contributionExperiencePolicyBadRequest(ctx), nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.IssueContributionExperiencePolicy401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.IssueContributionExperiencePolicy403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	input := request.Body.Policy
	issued, err := h.contributionExperience.Issue(ctx, staffActor(session), progression.ContributionExperiencePolicyInput{
		Revision: input.Revision, EffectiveFrom: input.EffectiveFrom,
		ExperiencePerUploadGiBMilli:  input.ExperiencePerUploadGibMilli,
		ExperiencePerTorrentMilli:    input.ExperiencePerTorrentMilli,
		ExperiencePerAccountDayMilli: input.ExperiencePerAccountDayMilli,
	}, request.Body.Reason)
	switch {
	case errors.Is(err, progression.ErrContributionPolicyInput):
		return contributionExperiencePolicyBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "contribution_experience_policy_issue_denied", "无法签发经验获取规则", "当前后台身份没有 progression.contribution.policy.issue 权限。")
		return generated.IssueContributionExperiencePolicy403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, progression.ErrContributionPolicyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "contribution_experience_policy_conflict", "经验政策时间线已经变化", "请刷新页面后，在最新生效时刻之后重新签发。")
		return generated.IssueContributionExperiencePolicy409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.IssueContributionExperiencePolicy201JSONResponse(contributionExperiencePolicyDTO(issued)), nil
}

func contributionExperiencePolicyBadRequest(ctx context.Context) generated.IssueContributionExperiencePolicy400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_contribution_experience_policy", "经验获取规则无效", "请检查未来 UTC 整点、三项非负经验值和至少 10 个字符的签发原因。")
	return generated.IssueContributionExperiencePolicy400ApplicationProblemPlusJSONResponse{
		ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
	}
}

func contributionExperiencePolicyDTO(policy progression.ContributionExperiencePolicy) generated.ContributionExperiencePolicy {
	return generated.ContributionExperiencePolicy{
		Revision: policy.Revision, EffectiveFrom: policy.EffectiveFrom,
		ExperiencePerUploadGibMilli:  policy.ExperiencePerUploadGiBMilli,
		ExperiencePerTorrentMilli:    policy.ExperiencePerTorrentMilli,
		ExperiencePerAccountDayMilli: policy.ExperiencePerAccountDayMilli,
		SnapshotSha256:               hex.EncodeToString(policy.SnapshotSHA256[:]), IssuedBy: policy.IssuedBy,
		Reason: policy.Reason, CreatedAt: policy.CreatedAt,
	}
}

func (h *Handler) ListLevelPolicies(ctx context.Context, _ generated.ListLevelPoliciesRequestObject) (generated.ListLevelPoliciesResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListLevelPolicies401ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem),
			}, nil
		}
		return generated.ListLevelPolicies403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	overview, err := h.levelPolicyAdministration.Overview(ctx, staffActor(session))
	switch {
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "level_policy_read_denied", "无法查看等级与经验规则", "当前后台身份没有 progression.level.policy.read 权限。")
		return generated.ListLevelPolicies403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	items := make([]generated.LevelPolicy, 0, len(overview.Items))
	for _, policy := range overview.Items {
		items = append(items, levelPolicyDTO(policy))
	}
	return generated.ListLevelPolicies200JSONResponse{Items: items, MinimumEffectiveAt: overview.MinimumEffectiveAt}, nil
}

func (h *Handler) IssueLevelPolicy(ctx context.Context, request generated.IssueLevelPolicyRequestObject) (generated.IssueLevelPolicyResponseObject, error) {
	if request.Body == nil {
		return levelPolicyBadRequest(ctx), nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.IssueLevelPolicy401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.IssueLevelPolicy403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	levels := make([]progression.LevelRule, 0, len(request.Body.Levels))
	for _, input := range request.Body.Levels {
		minimum, err := strconv.ParseInt(input.MinimumExperience, 10, 64)
		if err != nil {
			return levelPolicyBadRequest(ctx), nil
		}
		levels = append(levels, progression.LevelRule{
			Level: int16(input.Level), MinimumExperience: minimum,
			KarmaBonusBPS: input.KarmaBonusBps, SeedingCountBonus: int32(input.SeedingCountBonus),
		})
	}
	issued, err := h.levelPolicyAdministration.IssueLevelPolicy(ctx, staffActor(session), progression.IssueLevelPolicyInput{
		RequestID: request.Params.IdempotencyKey, ExpectedSequence: request.Body.ExpectedSequence,
		EffectiveAt: request.Body.EffectiveAt, Levels: levels, Reason: request.Body.Reason,
	})
	switch {
	case errors.Is(err, progression.ErrLevelPolicyInput):
		return levelPolicyBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "level_policy_issue_denied", "没有权限修改等级规则", "当前后台身份没有 progression.level.policy.issue 权限。")
		return generated.IssueLevelPolicy403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, progression.ErrLevelPolicyVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "level_policy_version_conflict", "等级规则时间线已经变化", "请重新载入页面并核对最新等级版本。")
		return generated.IssueLevelPolicy409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, progression.ErrLevelPolicyIdempotency):
		problem := newProblemFromContext(ctx, http.StatusConflict, "level_policy_idempotency_conflict", "请求标识已经使用", "请刷新设置后重新提交。")
		return generated.IssueLevelPolicy409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.IssueLevelPolicy201JSONResponse{
		Body: levelPolicyDTO(issued), Headers: generated.IssueLevelPolicy201ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func levelPolicyBadRequest(ctx context.Context) generated.IssueLevelPolicy400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_level_policy", "等级规则无效", "请检查整点生效时间、连续等级、递增经验门槛、权益上限和修改说明。")
	return generated.IssueLevelPolicy400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func levelPolicyDTO(policy progression.LevelPolicyRevision) generated.LevelPolicy {
	levels := make([]generated.LevelDefinition, 0, len(policy.Levels))
	for _, level := range policy.Levels {
		levels = append(levels, generated.LevelDefinition{
			Level: int(level.Level), MinimumExperience: strconv.FormatInt(level.MinimumExperience, 10),
			KarmaBonusBps: level.KarmaBonusBPS, SeedingCountBonus: int(level.SeedingCountBonus),
			CurrentUserCount: strconv.FormatInt(level.CurrentUserCount, 10),
		})
	}
	status := generated.LevelPolicyActivationStatusScheduled
	if policy.AppliedAt != nil {
		status = generated.LevelPolicyActivationStatusApplied
	}
	return generated.LevelPolicy{
		PolicyVersion: policy.PolicyVersion, Sequence: policy.Sequence,
		EffectiveAt: policy.EffectiveAt, ActivationStatus: status, AppliedAt: policy.AppliedAt,
		UserCount:         strconv.FormatInt(policy.UserCount, 10),
		AffectedUserCount: strconv.FormatInt(policy.AffectedUsers, 10),
		ChangedLevelCount: strconv.FormatInt(policy.ChangedLevels, 10),
		Reason:            policy.Reason, CreatedAt: policy.CreatedAt, Levels: levels,
	}
}

func (h *Handler) PreviewSeedingRewardPolicy(ctx context.Context, request generated.PreviewSeedingRewardPolicyRequestObject) (generated.PreviewSeedingRewardPolicyResponseObject, error) {
	if request.Body == nil {
		return previewRewardPolicyBadRequest(ctx), nil
	}
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.PreviewSeedingRewardPolicy401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.PreviewSeedingRewardPolicy403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	preview, err := h.seedingRewardAdministration.Preview(ctx, staffActor(session), seedingRewardPolicyFromInput(*request.Body))
	switch {
	case errors.Is(err, seedingreward.ErrInput):
		return previewRewardPolicyBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "reward_policy_preview_denied", "无法预览做种奖励政策", "当前后台身份没有 economy.seedingreward.policy.read 权限。")
		return generated.PreviewSeedingRewardPolicy403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	results := make([]generated.SeedingRewardPolicyPreviewResult, 0, len(preview.Results))
	for _, item := range preview.Results {
		results = append(results, generated.SeedingRewardPolicyPreviewResult{
			Name: item.Name, Description: item.Description,
			EligibleTorrentCount: int(item.EligibleTorrentCount), Reward: strconv.FormatInt(item.Reward, 10),
			ExperienceAmount: item.ExperienceAmount, Capped: item.Capped,
		})
	}
	return generated.PreviewSeedingRewardPolicy200JSONResponse{PolicySha256: hex.EncodeToString(preview.PolicySHA256[:]), Results: results}, nil
}

func (h *Handler) IssueSeedingRewardPolicy(ctx context.Context, request generated.IssueSeedingRewardPolicyRequestObject) (generated.IssueSeedingRewardPolicyResponseObject, error) {
	if request.Body == nil {
		return issueRewardPolicyBadRequest(ctx), nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.IssueSeedingRewardPolicy401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.IssueSeedingRewardPolicy403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	published, err := h.seedingRewardAdministration.Issue(ctx, staffActor(session), seedingRewardPolicyFromInput(request.Body.Policy), request.Body.Reason)
	switch {
	case errors.Is(err, seedingreward.ErrInput):
		return issueRewardPolicyBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "reward_policy_issue_denied", "无法签发做种奖励政策", "当前后台身份没有 economy.seedingreward.policy.issue 权限。")
		return generated.IssueSeedingRewardPolicy403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, seedingreward.ErrPolicyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "reward_policy_conflict", "奖励政策时间线发生冲突", "修订号已存在，或生效时刻没有追加在现有时间线之后。")
		return generated.IssueSeedingRewardPolicy409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.IssueSeedingRewardPolicy201JSONResponse(seedingRewardPolicyDTO(published)), nil
}

func previewRewardPolicyBadRequest(ctx context.Context) generated.PreviewSeedingRewardPolicy400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_reward_policy_preview", "奖励政策参数无效", "请检查参数范围、生效时刻和 UTC 整点要求。")
	return generated.PreviewSeedingRewardPolicy400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func issueRewardPolicyBadRequest(ctx context.Context) generated.IssueSeedingRewardPolicy400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_reward_policy", "奖励政策参数无效", "政策须在允许的未来 UTC 整点生效，并填写至少 10 个字符的签发原因。")
	return generated.IssueSeedingRewardPolicy400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func seedingRewardPolicyFromInput(input generated.SeedingRewardPolicyInput) seedingreward.PolicyRevision {
	return seedingreward.PolicyRevision{
		Revision: input.Revision, FormulaVersion: string(input.FormulaVersion), EffectiveFrom: input.EffectiveFrom,
		CurveHourlyCapMilli: input.CurveHourlyCapMilli, AgeSaturationSeconds: input.AgeSaturationSeconds,
		SeederDecay: int32(input.SeederDecay), CurveScaleMilli: input.CurveScaleMilli,
		SizeMultiplierBPS: input.SizeMultiplierBps, OfficialBonusBPS: input.OfficialBonusBps,
		UploadContributionBonusBPS: input.UploadContributionBonusBps,
		PerTorrentHourlyMilli:      input.PerTorrentHourlyMilli, BaseLinearTorrentLimit: int32(input.BaseLinearTorrentLimit),
		MaximumLevelTorrentBonus: int32(input.MaximumLevelTorrentBonus), MinimumTorrentBytes: input.MinimumTorrentBytes,
		MinimumActiveSeconds: int32(input.MinimumActiveSeconds), MaximumSnapshotAgeSeconds: int32(input.MaximumSnapshotAgeSeconds),
		VIPBonusBPS: input.VipBonusBps, MaximumMedalBonusBPS: input.MaximumMedalBonusBps,
		MaximumLevelBonusBPS: input.MaximumLevelBonusBps, MaximumHourlyReward: input.MaximumHourlyReward,
		ExperiencePerMagicBPS: input.ExperiencePerMagicBps,
	}
}

func seedingRewardPolicyDTO(published seedingreward.PublishedPolicy) generated.SeedingRewardPolicy {
	policy := published.Policy
	var issuedBy *uuid.UUID
	if published.IssuedBy != uuid.Nil {
		issuedBy = &published.IssuedBy
	}
	return generated.SeedingRewardPolicy{
		Revision: policy.Revision, FormulaVersion: generated.SeedingRewardPolicyFormulaVersion(policy.FormulaVersion),
		EffectiveFrom: policy.EffectiveFrom, CreatedAt: policy.CreatedAt,
		CurveHourlyCapMilli: policy.CurveHourlyCapMilli, AgeSaturationSeconds: policy.AgeSaturationSeconds,
		SeederDecay: int(policy.SeederDecay), CurveScaleMilli: policy.CurveScaleMilli,
		SizeMultiplierBps: policy.SizeMultiplierBPS, OfficialBonusBps: policy.OfficialBonusBPS,
		UploadContributionBonusBps: policy.UploadContributionBonusBPS,
		PerTorrentHourlyMilli:      policy.PerTorrentHourlyMilli, BaseLinearTorrentLimit: int(policy.BaseLinearTorrentLimit),
		MaximumLevelTorrentBonus: int(policy.MaximumLevelTorrentBonus), MinimumTorrentBytes: policy.MinimumTorrentBytes,
		MinimumActiveSeconds: int(policy.MinimumActiveSeconds), MaximumSnapshotAgeSeconds: int(policy.MaximumSnapshotAgeSeconds),
		VipBonusBps: policy.VIPBonusBPS, MaximumMedalBonusBps: policy.MaximumMedalBonusBPS,
		MaximumLevelBonusBps: policy.MaximumLevelBonusBPS, MaximumHourlyReward: policy.MaximumHourlyReward,
		ExperiencePerMagicBps: policy.ExperiencePerMagicBPS,
		SnapshotSha256:        hex.EncodeToString(policy.SnapshotSHA256[:]), IssuedBy: issuedBy,
		Reason: published.Reason,
	}
}

func economyOverviewDTO(overview economy.Overview) generated.EconomyOverview {
	magicEntries := make([]generated.MagicStatementEntry, 0, len(overview.MagicEntries))
	for _, entry := range overview.MagicEntries {
		magicEntries = append(magicEntries, generated.MagicStatementEntry{
			Sequence: strconv.FormatInt(entry.LedgerSequence, 10), TransactionType: generated.MagicStatementEntryTransactionType(entry.TransactionType),
			EntryType: generated.MagicStatementEntryEntryType(entry.EntryType), Amount: strconv.FormatInt(entry.Amount, 10),
			BalanceAfter: strconv.FormatInt(entry.BalanceAfter, 10), SourceReference: entry.SourceReference,
			PolicyRevision: entry.PolicyRevision, OccurredAt: entry.OccurredAt,
		})
	}
	experienceEntries := make([]generated.ExperienceStatementEntry, 0, len(overview.ExperienceEntries))
	for _, entry := range overview.ExperienceEntries {
		experienceEntries = append(experienceEntries, generated.ExperienceStatementEntry{
			Sequence: strconv.FormatInt(entry.EntrySequence, 10), EntryType: generated.ExperienceStatementEntryEntryType(entry.EntryType),
			Amount: entry.Amount, BalanceAfter: entry.BalanceAfter,
			SourceKind: generated.ExperienceStatementEntrySourceKind(entry.SourceKind), PolicyRevision: entry.PolicyRevision,
			LevelAfter: int(entry.LevelAfter), OccurredAt: entry.OccurredAt,
		})
	}
	progress := generated.ProgressOverview{
		Experience: overview.Progress.Experience, Level: int(overview.Progress.Level), PolicyVersion: overview.Progress.PolicyVersion,
		CurrentMinimumExperience: overview.Progress.CurrentMinimumExperience, UpdatedAt: overview.Progress.UpdatedAt,
	}
	if overview.Progress.Next != nil {
		progress.Next = &generated.LevelTarget{Level: int(overview.Progress.Next.Level), MinimumExperience: overview.Progress.Next.MinimumExperience}
	}
	levels := make([]generated.LevelPolicyRuleInput, 0, len(overview.Rules.Levels))
	for _, rule := range overview.Rules.Levels {
		levels = append(levels, generated.LevelPolicyRuleInput{
			Level:             int(rule.Level),
			MinimumExperience: rule.MinimumExperience,
			KarmaBonusBps:     rule.KarmaBonusBPS,
			SeedingCountBonus: int(rule.SeedingCountBonus),
		})
	}
	rules := generated.EconomyRuleOverview{
		ContributionExperience: generated.ContributionExperiencePolicyInput{
			Revision:                     overview.Rules.ContributionExperience.Revision,
			EffectiveFrom:                overview.Rules.ContributionExperience.EffectiveFrom,
			ExperiencePerUploadGibMilli:  overview.Rules.ContributionExperience.ExperiencePerUploadGiBMilli,
			ExperiencePerTorrentMilli:    overview.Rules.ContributionExperience.ExperiencePerTorrentMilli,
			ExperiencePerAccountDayMilli: overview.Rules.ContributionExperience.ExperiencePerAccountDayMilli,
		},
		LevelPolicyVersion: overview.Rules.LevelPolicyVersion,
		Levels:             levels,
	}
	if policy := overview.Rules.SeedingReward; policy != nil {
		rules.SeedingReward = &generated.SeedingRewardPolicyInput{
			Revision:                   policy.Revision,
			FormulaVersion:             generated.SeedingRewardPolicyInputFormulaVersion(policy.FormulaVersion),
			EffectiveFrom:              policy.EffectiveFrom,
			CurveHourlyCapMilli:        policy.CurveHourlyCapMilli,
			AgeSaturationSeconds:       policy.AgeSaturationSeconds,
			SeederDecay:                int(policy.SeederDecay),
			CurveScaleMilli:            policy.CurveScaleMilli,
			SizeMultiplierBps:          policy.SizeMultiplierBPS,
			OfficialBonusBps:           policy.OfficialBonusBPS,
			UploadContributionBonusBps: policy.UploadContributionBonusBPS,
			PerTorrentHourlyMilli:      policy.PerTorrentHourlyMilli,
			BaseLinearTorrentLimit:     int(policy.BaseLinearTorrentLimit),
			MaximumLevelTorrentBonus:   int(policy.MaximumLevelTorrentBonus),
			MinimumTorrentBytes:        policy.MinimumTorrentBytes,
			MinimumActiveSeconds:       int(policy.MinimumActiveSeconds),
			MaximumSnapshotAgeSeconds:  int(policy.MaximumSnapshotAgeSeconds),
			VipBonusBps:                policy.VIPBonusBPS,
			MaximumMedalBonusBps:       policy.MaximumMedalBonusBPS,
			MaximumLevelBonusBps:       policy.MaximumLevelBonusBPS,
			MaximumHourlyReward:        policy.MaximumHourlyReward,
			ExperiencePerMagicBps:      policy.ExperiencePerMagicBPS,
		}
	}
	var latestSeedingReward *generated.LatestSeedingRewardCalculation
	if calculation := overview.LatestSeedingReward; calculation != nil {
		latestSeedingReward = &generated.LatestSeedingRewardCalculation{
			WindowStart:          calculation.WindowStart,
			WindowEnd:            calculation.WindowEnd,
			PolicyRevision:       calculation.PolicyRevision,
			EligibleTorrentCount: int(calculation.EligibleTorrentCount),
			CurveRewardMilli:     strconv.FormatInt(calculation.CurveRewardMilli, 10),
			LinearRewardMilli:    strconv.FormatInt(calculation.LinearRewardMilli, 10),
			BaseRewardMilli:      strconv.FormatInt(calculation.BaseRewardMilli, 10),
			VipBonusMilli:        strconv.FormatInt(calculation.VIPBonusMilli, 10),
			MedalBonusMilli:      strconv.FormatInt(calculation.MedalBonusMilli, 10),
			LevelBonusMilli:      strconv.FormatInt(calculation.LevelBonusMilli, 10),
			UncappedReward:       strconv.FormatInt(calculation.UncappedReward, 10),
			Reward:               strconv.FormatInt(calculation.Reward, 10),
			ExperienceAmount:     calculation.ExperienceAmount,
			Capped:               calculation.Capped,
			CalculatedAt:         calculation.CalculatedAt,
		}
	}
	return generated.EconomyOverview{
		MagicBalance: strconv.FormatInt(overview.MagicBalance, 10), MagicUpdatedAt: overview.MagicUpdatedAt,
		MagicEntries: magicEntries, Progress: progress, ExperienceEntries: experienceEntries,
		Rules: rules, LatestSeedingReward: latestSeedingReward,
	}
}
