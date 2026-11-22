package httpapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/economy/magicstore"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type MagicStoreService interface {
	ShopOverview(context.Context, string) (magicstore.ShopOverview, error)
	Purchase(context.Context, string, string, uuid.UUID, magicstore.ItemKey, int, string, *int64) (magicstore.PurchaseReceipt, error)
	SetNameStyle(context.Context, string, string, string) (magicstore.NameStyle, error)
	ActiveNameStyles(context.Context, string) ([]magicstore.NameStyle, error)
	GamesOverview(context.Context, string) (magicstore.GamesOverview, uuid.UUID, error)
	PlayScratch(context.Context, string, string, uuid.UUID) (magicstore.ScratchReceipt, error)
	BuyLotteryTickets(context.Context, string, string, uuid.UUID, int, *uuid.UUID) (magicstore.LotteryReceipt, error)
	CreateChallenge(context.Context, string, string, uuid.UUID, magicstore.PvPMode, int64, magicstore.RPSChoice, string) (magicstore.PvPChallenge, uuid.UUID, error)
	AcceptChallenge(context.Context, string, string, uuid.UUID, magicstore.RPSChoice) (magicstore.PvPChallenge, uuid.UUID, error)
	CancelChallenge(context.Context, string, string, uuid.UUID) (magicstore.PvPChallenge, uuid.UUID, error)
}

type MagicStoreAdministrationService interface {
	Overview(context.Context, authz.StaffActor) (magicstore.AdminOverview, error)
	UpdateSettings(context.Context, authz.StaffActor, magicstore.Settings, int64, string) (magicstore.AdminOverview, error)
}

var errMagicStoreUnavailable = errors.New("magic store service is not configured")

// WithMagicStore attaches the magic shop and game services.
func (h *Handler) WithMagicStore(member MagicStoreService, administration MagicStoreAdministrationService) *Handler {
	h.magicStore, h.magicStoreAdministration = member, administration
	return h
}

type magicProblemSpec struct {
	status              int
	code, title, detail string
}

var magicStoreProblems = []struct {
	err  error
	spec magicProblemSpec
}{
	{magicstore.ErrInput, magicProblemSpec{http.StatusBadRequest, "invalid_magic_store_request", "请求无效", "请刷新页面后重新操作。"}},
	{identity.ErrSessionNotFound, magicProblemSpec{http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后继续。"}},
	{identity.ErrInvalidCSRF, magicProblemSpec{http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。"}},
	{authz.ErrForbidden, magicProblemSpec{http.StatusForbidden, "magic_store_denied", "没有权限", "当前账号不能使用这项魔力功能。"}},
	{magicstore.ErrChallengeForbidden, magicProblemSpec{http.StatusForbidden, "pvp_challenge_forbidden", "不能撤回", "只有发起人可以撤回自己的擂台。"}},
	{magicstore.ErrChallengeNotFound, magicProblemSpec{http.StatusNotFound, "pvp_challenge_not_found", "擂台不存在", "这个擂台已经不存在，请刷新列表。"}},
	{magicstore.ErrShopDisabled, magicProblemSpec{http.StatusConflict, "magic_shop_disabled", "商品暂未开放", "魔力商店或该商品已暂停销售。"}},
	{magicstore.ErrShopPriceChanged, magicProblemSpec{http.StatusConflict, "magic_shop_price_changed", "商品价格已变更", "请核对最新价格后重新购买。"}},
	{magicstore.ErrGameDisabled, magicProblemSpec{http.StatusConflict, "magic_game_disabled", "游戏暂未开放", "该游戏已暂停，请稍后再来。"}},
	{magicstore.ErrInsufficientMagic, magicProblemSpec{http.StatusConflict, "magic_insufficient", "魔力值不足", "当前可用魔力值不足以完成这次操作。"}},
	{magicstore.ErrIdempotencyConflict, magicProblemSpec{http.StatusConflict, "magic_idempotency_conflict", "请求标识已经使用", "请刷新页面后重新操作。"}},
	{magicstore.ErrSettingsConflict, magicProblemSpec{http.StatusConflict, "magic_store_settings_conflict", "配置已经变化", "当前编辑基于旧版本，请重新载入后再提交。"}},
	{magicstore.ErrMonthlyLimit, magicProblemSpec{http.StatusConflict, "magic_shop_monthly_limit", "本月已达限购", "该商品本月购买数量已达上限，下个月再来吧。"}},
	{magicstore.ErrHoldingLimit, magicProblemSpec{http.StatusConflict, "magic_shop_holding_limit", "持有数量已达上限", "先用掉一些再来购买。"}},
	{magicstore.ErrInviteIneligible, magicProblemSpec{http.StatusConflict, "magic_shop_invite_ineligible", "暂不能购买邀请", "当前账号不满足发送邀请的条件，购买后也无法使用。"}},
	{magicstore.ErrVIPPermanent, magicProblemSpec{http.StatusConflict, "magic_shop_vip_permanent", "已是永久 VIP", "永久 VIP 无需再购买时长。"}},
	{magicstore.ErrAccountUnavailable, magicProblemSpec{http.StatusConflict, "magic_account_unavailable", "账号状态不可用", "账号当前不是正常状态。"}},
	{magicstore.ErrNameStyleTier, magicProblemSpec{http.StatusConflict, "name_style_tier", "样式档位不匹配", "渐变档可以选择纯色，纯色档需要升级为渐变档才能使用渐变样式。"}},
	{magicstore.ErrNameStyleMissing, magicProblemSpec{http.StatusConflict, "name_style_missing", "没有有效的彩色 ID", "请先购买彩色 ID。"}},
	{magicstore.ErrDailyLimit, magicProblemSpec{http.StatusConflict, "magic_game_daily_limit", "今日次数已用完", "明天再来试试手气。"}},
	{magicstore.ErrLotteryClosed, magicProblemSpec{http.StatusConflict, "lottery_closed", "所选期次已停止售票", "请查看最新期次后重新购票。"}},
	{magicstore.ErrLotteryTicketLimit, magicProblemSpec{http.StatusConflict, "lottery_ticket_limit", "已达本期购票上限", "每人每期的购票数量有上限。"}},
	{magicstore.ErrChallengeClosed, magicProblemSpec{http.StatusConflict, "pvp_challenge_closed", "擂台已结束", "这个擂台已被应战、撤回或过期。"}},
	{magicstore.ErrChallengeOwn, magicProblemSpec{http.StatusConflict, "pvp_challenge_own", "不能应战自己", "请等待其他会员应战。"}},
	{magicstore.ErrChallengeOpenLimit, magicProblemSpec{http.StatusConflict, "pvp_challenge_open_limit", "开放擂台过多", "请等已有擂台结束或撤回后再发起。"}},
}

// magicResponse turns a known magic store error into the operation's problem
// response; unknown errors and statuses the operation does not declare become
// a 500 through the returned error.
func magicResponse[T any](ctx context.Context, err error, build func(generated.Problem) (T, bool)) (T, error) {
	var zero T
	for _, candidate := range magicStoreProblems {
		if errors.Is(err, candidate.err) {
			spec := candidate.spec
			if response, ok := build(newProblemFromContext(ctx, spec.status, spec.code, spec.title, spec.detail)); ok {
				return response, nil
			}
			break
		}
	}
	return zero, err
}

func problemEnvelope(problem generated.Problem) generated.ProblemResponseApplicationProblemPlusJSONResponse {
	return generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)
}

func (h *Handler) GetMyMagicShop(ctx context.Context, _ generated.GetMyMagicShopRequestObject) (generated.GetMyMagicShopResponseObject, error) {
	if h.magicStore == nil {
		return nil, errMagicStoreUnavailable
	}
	overview, err := h.magicStore.ShopOverview(ctx, sessionTokenFromContext(ctx))
	if err != nil {
		return magicResponse(ctx, err, func(p generated.Problem) (generated.GetMyMagicShopResponseObject, bool) {
			switch p.Status {
			case http.StatusUnauthorized:
				return generated.GetMyMagicShop401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: problemEnvelope(p)}, true
			case http.StatusForbidden:
				return generated.GetMyMagicShop403ApplicationProblemPlusJSONResponse(p), true
			}
			return nil, false
		})
	}
	return generated.GetMyMagicShop200JSONResponse(magicShopOverviewDTO(overview)), nil
}

func (h *Handler) PurchaseMagicShopItem(ctx context.Context, request generated.PurchaseMagicShopItemRequestObject) (generated.PurchaseMagicShopItemResponseObject, error) {
	if h.magicStore == nil {
		return nil, errMagicStoreUnavailable
	}
	build := func(p generated.Problem) (generated.PurchaseMagicShopItemResponseObject, bool) {
		switch p.Status {
		case http.StatusBadRequest:
			return generated.PurchaseMagicShopItem400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: problemEnvelope(p)}, true
		case http.StatusUnauthorized:
			return generated.PurchaseMagicShopItem401ApplicationProblemPlusJSONResponse(p), true
		case http.StatusForbidden:
			return generated.PurchaseMagicShopItem403ApplicationProblemPlusJSONResponse(p), true
		case http.StatusConflict:
			return generated.PurchaseMagicShopItem409ApplicationProblemPlusJSONResponse(p), true
		}
		return nil, false
	}
	if request.Body == nil {
		return magicResponse(ctx, magicstore.ErrInput, build)
	}
	style := ""
	if request.Body.StyleKey != nil {
		style = *request.Body.StyleKey
	}
	receipt, err := h.magicStore.Purchase(
		ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), request.Params.IdempotencyKey,
		magicstore.ItemKey(request.Body.ItemKey), request.Body.Quantity, style, request.Body.ExpectedUnitPrice,
	)
	if err != nil {
		return magicResponse(ctx, err, build)
	}
	return generated.PurchaseMagicShopItem201JSONResponse{
		Order: magicShopOrderDTO(receipt.Order), BalanceAfter: strconv.FormatInt(receipt.BalanceAfter, 10), Replayed: receipt.Replayed,
	}, nil
}

func (h *Handler) SetMyNameStyle(ctx context.Context, request generated.SetMyNameStyleRequestObject) (generated.SetMyNameStyleResponseObject, error) {
	if h.magicStore == nil {
		return nil, errMagicStoreUnavailable
	}
	build := func(p generated.Problem) (generated.SetMyNameStyleResponseObject, bool) {
		switch p.Status {
		case http.StatusBadRequest:
			return generated.SetMyNameStyle400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: problemEnvelope(p)}, true
		case http.StatusUnauthorized:
			return generated.SetMyNameStyle401ApplicationProblemPlusJSONResponse(p), true
		case http.StatusForbidden:
			return generated.SetMyNameStyle403ApplicationProblemPlusJSONResponse(p), true
		case http.StatusConflict:
			return generated.SetMyNameStyle409ApplicationProblemPlusJSONResponse(p), true
		}
		return nil, false
	}
	if request.Body == nil {
		return magicResponse(ctx, magicstore.ErrInput, build)
	}
	style, err := h.magicStore.SetNameStyle(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), request.Body.StyleKey)
	if err != nil {
		return magicResponse(ctx, err, build)
	}
	return generated.SetMyNameStyle200JSONResponse(nameStyleDTO(style)), nil
}

func (h *Handler) ListNameStyles(ctx context.Context, _ generated.ListNameStylesRequestObject) (generated.ListNameStylesResponseObject, error) {
	if h.magicStore == nil {
		return nil, errMagicStoreUnavailable
	}
	styles, err := h.magicStore.ActiveNameStyles(ctx, sessionTokenFromContext(ctx))
	if err != nil {
		return magicResponse(ctx, err, func(p generated.Problem) (generated.ListNameStylesResponseObject, bool) {
			switch p.Status {
			case http.StatusUnauthorized:
				return generated.ListNameStyles401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: problemEnvelope(p)}, true
			case http.StatusForbidden:
				return generated.ListNameStyles403ApplicationProblemPlusJSONResponse(p), true
			}
			return nil, false
		})
	}
	items := make([]generated.MemberNameStyle, 0, len(styles))
	for _, style := range styles {
		items = append(items, nameStyleDTO(style))
	}
	return generated.ListNameStyles200JSONResponse{Items: items}, nil
}

func (h *Handler) GetMyMagicGames(ctx context.Context, _ generated.GetMyMagicGamesRequestObject) (generated.GetMyMagicGamesResponseObject, error) {
	if h.magicStore == nil {
		return nil, errMagicStoreUnavailable
	}
	overview, viewer, err := h.magicStore.GamesOverview(ctx, sessionTokenFromContext(ctx))
	if err != nil {
		return magicResponse(ctx, err, func(p generated.Problem) (generated.GetMyMagicGamesResponseObject, bool) {
			switch p.Status {
			case http.StatusUnauthorized:
				return generated.GetMyMagicGames401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: problemEnvelope(p)}, true
			case http.StatusForbidden:
				return generated.GetMyMagicGames403ApplicationProblemPlusJSONResponse(p), true
			}
			return nil, false
		})
	}
	return generated.GetMyMagicGames200JSONResponse(magicGamesOverviewDTO(overview, viewer)), nil
}

func (h *Handler) PlayScratchCard(ctx context.Context, request generated.PlayScratchCardRequestObject) (generated.PlayScratchCardResponseObject, error) {
	if h.magicStore == nil {
		return nil, errMagicStoreUnavailable
	}
	receipt, err := h.magicStore.PlayScratch(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), request.Params.IdempotencyKey)
	if err != nil {
		return magicResponse(ctx, err, func(p generated.Problem) (generated.PlayScratchCardResponseObject, bool) {
			switch p.Status {
			case http.StatusBadRequest:
				return generated.PlayScratchCard400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: problemEnvelope(p)}, true
			case http.StatusUnauthorized:
				return generated.PlayScratchCard401ApplicationProblemPlusJSONResponse(p), true
			case http.StatusForbidden:
				return generated.PlayScratchCard403ApplicationProblemPlusJSONResponse(p), true
			case http.StatusConflict:
				return generated.PlayScratchCard409ApplicationProblemPlusJSONResponse(p), true
			}
			return nil, false
		})
	}
	return generated.PlayScratchCard201JSONResponse{
		Card: scratchCardDTO(receipt.Card), BalanceAfter: strconv.FormatInt(receipt.BalanceAfter, 10), Replayed: receipt.Replayed,
	}, nil
}

func (h *Handler) BuyLotteryTickets(ctx context.Context, request generated.BuyLotteryTicketsRequestObject) (generated.BuyLotteryTicketsResponseObject, error) {
	if h.magicStore == nil {
		return nil, errMagicStoreUnavailable
	}
	build := func(p generated.Problem) (generated.BuyLotteryTicketsResponseObject, bool) {
		switch p.Status {
		case http.StatusBadRequest:
			return generated.BuyLotteryTickets400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: problemEnvelope(p)}, true
		case http.StatusUnauthorized:
			return generated.BuyLotteryTickets401ApplicationProblemPlusJSONResponse(p), true
		case http.StatusForbidden:
			return generated.BuyLotteryTickets403ApplicationProblemPlusJSONResponse(p), true
		case http.StatusConflict:
			return generated.BuyLotteryTickets409ApplicationProblemPlusJSONResponse(p), true
		}
		return nil, false
	}
	if request.Body == nil {
		return magicResponse(ctx, magicstore.ErrInput, build)
	}
	receipt, err := h.magicStore.BuyLotteryTickets(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), request.Params.IdempotencyKey, request.Body.Quantity, request.Body.RoundId)
	if err != nil {
		return magicResponse(ctx, err, build)
	}
	return generated.BuyLotteryTickets201JSONResponse{
		RoundId: receipt.RoundID, RoundNumber: receipt.RoundNumber, Quantity: receipt.Quantity,
		FirstTicket: receipt.FirstTicket, LastTicket: receipt.LastTicket, TotalPrice: receipt.TotalPrice,
		BalanceAfter: strconv.FormatInt(receipt.BalanceAfter, 10), Replayed: receipt.Replayed,
	}, nil
}

func (h *Handler) CreatePvPChallenge(ctx context.Context, request generated.CreatePvPChallengeRequestObject) (generated.CreatePvPChallengeResponseObject, error) {
	if h.magicStore == nil {
		return nil, errMagicStoreUnavailable
	}
	build := func(p generated.Problem) (generated.CreatePvPChallengeResponseObject, bool) {
		switch p.Status {
		case http.StatusBadRequest:
			return generated.CreatePvPChallenge400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: problemEnvelope(p)}, true
		case http.StatusUnauthorized:
			return generated.CreatePvPChallenge401ApplicationProblemPlusJSONResponse(p), true
		case http.StatusForbidden:
			return generated.CreatePvPChallenge403ApplicationProblemPlusJSONResponse(p), true
		case http.StatusConflict:
			return generated.CreatePvPChallenge409ApplicationProblemPlusJSONResponse(p), true
		}
		return nil, false
	}
	if request.Body == nil {
		return magicResponse(ctx, magicstore.ErrInput, build)
	}
	choice, message := magicstore.RPSChoice(""), ""
	if request.Body.Choice != nil {
		choice = magicstore.RPSChoice(*request.Body.Choice)
	}
	if request.Body.Message != nil {
		message = *request.Body.Message
	}
	challenge, viewer, err := h.magicStore.CreateChallenge(
		ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), request.Params.IdempotencyKey,
		magicstore.PvPMode(request.Body.Mode), request.Body.Stake, choice, message,
	)
	if err != nil {
		return magicResponse(ctx, err, build)
	}
	return generated.CreatePvPChallenge201JSONResponse(pvpChallengeDTO(challenge, viewer)), nil
}

func (h *Handler) AcceptPvPChallenge(ctx context.Context, request generated.AcceptPvPChallengeRequestObject) (generated.AcceptPvPChallengeResponseObject, error) {
	if h.magicStore == nil {
		return nil, errMagicStoreUnavailable
	}
	choice := magicstore.RPSChoice("")
	if request.Body != nil && request.Body.Choice != nil {
		choice = magicstore.RPSChoice(*request.Body.Choice)
	}
	challenge, viewer, err := h.magicStore.AcceptChallenge(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), request.ChallengeId, choice)
	if err != nil {
		return magicResponse(ctx, err, func(p generated.Problem) (generated.AcceptPvPChallengeResponseObject, bool) {
			switch p.Status {
			case http.StatusBadRequest:
				return generated.AcceptPvPChallenge400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: problemEnvelope(p)}, true
			case http.StatusUnauthorized:
				return generated.AcceptPvPChallenge401ApplicationProblemPlusJSONResponse(p), true
			case http.StatusForbidden:
				return generated.AcceptPvPChallenge403ApplicationProblemPlusJSONResponse(p), true
			case http.StatusNotFound:
				return generated.AcceptPvPChallenge404ApplicationProblemPlusJSONResponse(p), true
			case http.StatusConflict:
				return generated.AcceptPvPChallenge409ApplicationProblemPlusJSONResponse(p), true
			}
			return nil, false
		})
	}
	return generated.AcceptPvPChallenge200JSONResponse(pvpChallengeDTO(challenge, viewer)), nil
}

func (h *Handler) CancelPvPChallenge(ctx context.Context, request generated.CancelPvPChallengeRequestObject) (generated.CancelPvPChallengeResponseObject, error) {
	if h.magicStore == nil {
		return nil, errMagicStoreUnavailable
	}
	challenge, viewer, err := h.magicStore.CancelChallenge(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), request.ChallengeId)
	if err != nil {
		return magicResponse(ctx, err, func(p generated.Problem) (generated.CancelPvPChallengeResponseObject, bool) {
			switch p.Status {
			case http.StatusUnauthorized:
				return generated.CancelPvPChallenge401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: problemEnvelope(p)}, true
			case http.StatusForbidden:
				return generated.CancelPvPChallenge403ApplicationProblemPlusJSONResponse(p), true
			case http.StatusNotFound:
				return generated.CancelPvPChallenge404ApplicationProblemPlusJSONResponse(p), true
			case http.StatusConflict:
				return generated.CancelPvPChallenge409ApplicationProblemPlusJSONResponse(p), true
			}
			return nil, false
		})
	}
	return generated.CancelPvPChallenge200JSONResponse(pvpChallengeDTO(challenge, viewer)), nil
}

func (h *Handler) GetMagicStoreSettings(ctx context.Context, _ generated.GetMagicStoreSettingsRequestObject) (generated.GetMagicStoreSettingsResponseObject, error) {
	if h.magicStoreAdministration == nil {
		return nil, errMagicStoreUnavailable
	}
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.GetMagicStoreSettings401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: problemEnvelope(*problem)}, nil
		}
		return generated.GetMagicStoreSettings403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	overview, err := h.magicStoreAdministration.Overview(ctx, staffActor(session))
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "magic_store_manage_read_denied", "无法查看魔力商店配置", "当前后台身份没有 economy.magicstore.manage.read 权限。")
		return generated.GetMagicStoreSettings403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.GetMagicStoreSettings200JSONResponse(magicStoreAdminOverviewDTO(overview)), nil
}

func (h *Handler) UpdateMagicStoreSettings(ctx context.Context, request generated.UpdateMagicStoreSettingsRequestObject) (generated.UpdateMagicStoreSettingsResponseObject, error) {
	if h.magicStoreAdministration == nil {
		return nil, errMagicStoreUnavailable
	}
	inputProblem := func() generated.UpdateMagicStoreSettingsResponseObject {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_magic_store_settings", "配置无效", "请检查价格、数量范围、奖品赔率（刮刮乐期望返还不能超过票价）和修改原因。")
		return generated.UpdateMagicStoreSettings400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: problemEnvelope(problem)}
	}
	if request.Body == nil {
		return inputProblem(), nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.UpdateMagicStoreSettings401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.UpdateMagicStoreSettings403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	settings, err := magicStoreSettingsInput(request.Body.Settings)
	if err != nil {
		return inputProblem(), nil
	}
	overview, err := h.magicStoreAdministration.UpdateSettings(ctx, staffActor(session), settings, request.Body.ExpectedVersion, request.Body.Reason)
	switch {
	case errors.Is(err, magicstore.ErrInput):
		return inputProblem(), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "magic_store_update_denied", "没有权限修改魔力商店", "当前后台身份没有 economy.magicstore.update 权限。")
		return generated.UpdateMagicStoreSettings403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, magicstore.ErrSettingsConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "magic_store_settings_conflict", "配置已经变化", "当前编辑基于旧版本，请重新载入后再提交。")
		return generated.UpdateMagicStoreSettings409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.UpdateMagicStoreSettings200JSONResponse(magicStoreAdminOverviewDTO(overview)), nil
}

// The generated settings type mirrors magicstore.Settings field for field
// through identical JSON names, so a JSON round trip is the exact mapping.
func magicStoreSettingsDTO(settings magicstore.Settings) generated.MagicStoreSettings {
	var result generated.MagicStoreSettings
	encoded, _ := json.Marshal(settings)
	_ = json.Unmarshal(encoded, &result)
	return result
}

func magicStoreSettingsInput(settings generated.MagicStoreSettings) (magicstore.Settings, error) {
	encoded, err := json.Marshal(settings)
	if err != nil {
		return magicstore.Settings{}, err
	}
	return magicstore.DecodeSettings(encoded)
}

func nameStyleDTO(style magicstore.NameStyle) generated.MemberNameStyle {
	return generated.MemberNameStyle{
		UserId: style.UserID, Username: style.Username, Tier: generated.NameStyleTier(style.Tier),
		StyleKey: style.StyleKey, ExpiresAt: style.ExpiresAt,
	}
}

func magicShopOrderDTO(order magicstore.Order) generated.MagicShopOrder {
	return generated.MagicShopOrder{
		Id: order.ID, ItemKey: generated.MagicShopItemKey(order.ItemKey), Quantity: order.Quantity,
		UnitPrice: order.UnitPrice, TotalPrice: order.TotalPrice, StyleKey: order.StyleKey,
		EffectUntil: order.EffectUntil, PurchasedAt: order.PurchasedAt,
	}
}

func magicShopOverviewDTO(overview magicstore.ShopOverview) generated.MagicShopOverview {
	options := magicstore.NameStyleOptions()
	result := generated.MagicShopOverview{
		Settings: magicStoreSettingsDTO(overview.Settings), SettingsVersion: overview.SettingsVersion,
		MagicBalance:                       strconv.FormatInt(overview.MagicBalance, 10),
		InvitesPurchasedThisMonth:          overview.InvitesPurchasedThisMonth,
		TemporaryInvitesPurchasedThisMonth: overview.TemporaryPurchasedMonth,
		RemainingInvites:                   overview.RemainingInvites,
		TemporaryInvites:                   overview.TemporaryInvites,
		TemporaryInviteNextExpiry:          overview.TemporaryInviteNextExpiry,
		Vip: generated.MagicShopVIPStatus{
			Active: overview.VIP.Active, Permanent: overview.VIP.Permanent, Until: overview.VIP.Until,
		},
		MakeupCards:      overview.MakeupCards,
		NameStyleOptions: make([]generated.NameStyleOption, 0, len(options)),
		RecentOrders:     make([]generated.MagicShopOrder, 0, len(overview.RecentOrders)),
	}
	if overview.InviteBlocker != "" {
		blocker := overview.InviteBlocker
		result.InviteBlocker = &blocker
	}
	if overview.NameStyle != nil {
		style := nameStyleDTO(*overview.NameStyle)
		result.NameStyle = &style
	}
	for _, option := range options {
		result.NameStyleOptions = append(result.NameStyleOptions, generated.NameStyleOption{Key: option.Key, Tier: generated.NameStyleTier(option.Tier)})
	}
	for _, order := range overview.RecentOrders {
		result.RecentOrders = append(result.RecentOrders, magicShopOrderDTO(order))
	}
	return result
}

func scratchCardDTO(card magicstore.ScratchCard) generated.ScratchCard {
	return generated.ScratchCard{
		Id: card.ID, Price: card.Price, Prize: card.Prize, PrizeIndex: card.PrizeIndex,
		PrizeLabel: card.PrizeLabel, PurchasedAt: card.PurchasedAt,
	}
}

func lotteryRoundDTO(round magicstore.LotteryRound) generated.LotteryRound {
	tiers := make([]generated.LotteryTier, 0, len(round.Tiers))
	for _, tier := range round.Tiers {
		tiers = append(tiers, generated.LotteryTier{Label: tier.Label, Winners: tier.Winners, ShareBps: tier.ShareBPS})
	}
	var prizes []int64
	if round.Status == "drawn" {
		allocation := magicstore.AllocatePrizes(round.CarriedIn, round.SalesTotal, round.BurnBPS, round.Tiers, round.TicketsSold)
		prizes = make([]int64, 0, len(allocation.Tiers))
		for _, tier := range allocation.Tiers {
			prizes = append(prizes, tier.PrizeEach)
		}
	} else {
		prizes = magicstore.EstimatedPrizes(round.CarriedIn, round.SalesTotal, round.BurnBPS, round.Tiers)
	}
	ranges := make([][]int, 0, len(round.MyTicketRanges))
	for _, span := range round.MyTicketRanges {
		ranges = append(ranges, []int{span[0], span[1]})
	}
	winners := make([]generated.LotteryWinner, 0, len(round.Winners))
	for _, winner := range round.Winners {
		winners = append(winners, generated.LotteryWinner{
			DrawRank: winner.DrawRank, TierIndex: winner.TierIndex, TierLabel: winner.TierLabel,
			TicketNumber: winner.TicketNumber, Username: winner.Username, Prize: winner.Prize,
		})
	}
	result := generated.LotteryRound{
		Id: round.ID, RoundNumber: round.RoundNumber, Status: generated.LotteryRoundStatus(round.Status),
		TicketPrice: round.TicketPrice, MaximumTicketsPerMember: round.MaximumTicketsPerMember,
		BurnBps: round.BurnBPS, Tiers: tiers, OpensAt: round.OpensAt, DrawAt: round.DrawAt,
		SeedSha256: hex.EncodeToString(round.SeedSHA256[:]), CarriedIn: round.CarriedIn,
		TicketsSold: round.TicketsSold, SalesTotal: round.SalesTotal, Burned: round.Burned,
		PaidOut: round.PaidOut, CarriedOut: round.CarriedOut, DrawnAt: round.DrawnAt,
		EstimatedPrizes: prizes, MyTickets: round.MyTickets, MyTicketRanges: ranges,
		MyPrize: round.MyPrize, Winners: winners,
	}
	if len(round.Seed) > 0 {
		seed := hex.EncodeToString(round.Seed)
		result.Seed = &seed
	}
	return result
}

func pvpChallengeDTO(challenge magicstore.PvPChallenge, viewer uuid.UUID) generated.PvPChallenge {
	result := generated.PvPChallenge{
		Id: challenge.ID, Mode: generated.PvPMode(challenge.Mode), Status: generated.PvPChallengeStatus(challenge.Status),
		CreatorId: challenge.CreatorID, CreatorUsername: challenge.CreatorUsername, Stake: challenge.Stake,
		FeeBps: challenge.FeeBPS, Message: challenge.Message, OpponentId: challenge.OpponentID,
		OpponentUsername: challenge.OpponentUsername, CreatorRoll: challenge.CreatorRoll,
		OpponentRoll: challenge.OpponentRoll, WinnerId: challenge.WinnerID, Fee: challenge.Fee,
		ExpiresAt: challenge.ExpiresAt, CreatedAt: challenge.CreatedAt, ClosedAt: challenge.ClosedAt,
		ViewerIsCreator: viewer == challenge.CreatorID,
	}
	// An open rock-paper-scissors challenge must not leak the creator's hand.
	if challenge.CreatorChoice != nil && (result.ViewerIsCreator || challenge.Status != magicstore.PvPOpen) {
		choice := generated.PvPChallengeCreatorChoice(*challenge.CreatorChoice)
		result.CreatorChoice = &choice
	}
	if challenge.OpponentChoice != nil {
		choice := generated.PvPChallengeOpponentChoice(*challenge.OpponentChoice)
		result.OpponentChoice = &choice
	}
	return result
}

func pvpChallengeListDTO(challenges []magicstore.PvPChallenge, viewer uuid.UUID) []generated.PvPChallenge {
	result := make([]generated.PvPChallenge, 0, len(challenges))
	for _, challenge := range challenges {
		result = append(result, pvpChallengeDTO(challenge, viewer))
	}
	return result
}

func magicGamesOverviewDTO(overview magicstore.GamesOverview, viewer uuid.UUID) generated.MagicGamesOverview {
	result := generated.MagicGamesOverview{
		Settings: magicStoreSettingsDTO(overview.Settings), SettingsVersion: overview.SettingsVersion,
		MagicBalance:             strconv.FormatInt(overview.MagicBalance, 10),
		ScratchUsedToday:         overview.ScratchUsedToday,
		ScratchExpectedReturnBps: magicstore.ScratchExpectedReturnBPS(overview.Settings.Scratch),
		RecentScratchCards:       make([]generated.ScratchCard, 0, len(overview.RecentScratchCards)),
		OpenChallenges:           pvpChallengeListDTO(overview.OpenChallenges, viewer),
		MyChallenges:             pvpChallengeListDTO(overview.MyChallenges, viewer),
		RecentSettled:            pvpChallengeListDTO(overview.RecentSettled, viewer),
	}
	for _, card := range overview.RecentScratchCards {
		result.RecentScratchCards = append(result.RecentScratchCards, scratchCardDTO(card))
	}
	if overview.CurrentRound != nil {
		round := lotteryRoundDTO(*overview.CurrentRound)
		result.CurrentRound = &round
	}
	if overview.PreviousRound != nil {
		round := lotteryRoundDTO(*overview.PreviousRound)
		result.PreviousRound = &round
	}
	return result
}

func magicStoreAdminOverviewDTO(overview magicstore.AdminOverview) generated.MagicStoreAdminOverview {
	result := generated.MagicStoreAdminOverview{
		Settings: magicStoreSettingsDTO(overview.Settings), Version: overview.Version, UpdatedAt: overview.UpdatedAt,
		ScratchExpectedReturnBps: magicstore.ScratchExpectedReturnBPS(overview.Settings.Scratch),
		Revisions:                make([]generated.MagicStoreSettingsRevision, 0, len(overview.Revisions)),
	}
	result.Accounts.ShopSink = strconv.FormatInt(overview.Accounts.ShopSink, 10)
	result.Accounts.GameHouse = strconv.FormatInt(overview.Accounts.GameHouse, 10)
	result.Accounts.LotteryPool = strconv.FormatInt(overview.Accounts.LotteryPool, 10)
	result.Accounts.PvpEscrow = strconv.FormatInt(overview.Accounts.PvPEscrow, 10)
	for _, revision := range overview.Revisions {
		result.Revisions = append(result.Revisions, generated.MagicStoreSettingsRevision{
			Version: revision.Version, Reason: revision.Reason, ActorUsername: revision.ActorUsername, OccurredAt: revision.OccurredAt,
		})
	}
	return result
}
