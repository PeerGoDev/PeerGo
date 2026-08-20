package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/traffic"
)

func (handler *Handler) SubmitMyHNRAppeal(ctx context.Context, request generated.SubmitMyHNRAppealRequestObject) (generated.SubmitMyHNRAppealResponseObject, error) {
	if request.Body == nil {
		return hnrAppealSubmitBadRequest(ctx), nil
	}
	result, err := handler.trafficOverview.SubmitHNRAppeal(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), traffic.SubmitHNRAppealInput{
		AppealID: request.Params.IdempotencyKey, ObligationID: request.ObligationId,
		Statement: request.Body.Statement,
	})
	switch {
	case errors.Is(err, traffic.ErrInput):
		return hnrAppealSubmitBadRequest(ctx), nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后提交 H&R 申诉。")
		return generated.SubmitMyHNRAppeal401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.SubmitMyHNRAppeal403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "hnr_appeal_create_denied", "无法提交申诉", "当前账号暂时不能提交这项申诉。")
		return generated.SubmitMyHNRAppeal403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, traffic.ErrHNRNotAppealable):
		problem := newProblemFromContext(ctx, http.StatusConflict, "hnr_not_appealable", "这条记录当前不能申诉", "只有超过宽限期且仍待补做的记录可以申诉，请刷新页面查看最新状态。")
		return generated.SubmitMyHNRAppeal409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, traffic.ErrHNRAppealExists):
		problem := newProblemFromContext(ctx, http.StatusConflict, "hnr_appeal_already_exists", "这条记录已提交申诉", "同一条 H&R 记录只能提交一次，请等待处理。")
		return generated.SubmitMyHNRAppeal409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, traffic.ErrConflict), errors.Is(err, traffic.ErrIdempotency):
		problem := newProblemFromContext(ctx, http.StatusConflict, "hnr_appeal_conflict", "申诉状态已变化", "请刷新页面后重试。")
		return generated.SubmitMyHNRAppeal409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.SubmitMyHNRAppeal201JSONResponse{
		Body:    myHNRAppealDTO(result),
		Headers: generated.SubmitMyHNRAppeal201ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func (handler *Handler) ListHNRAppeals(ctx context.Context, request generated.ListHNRAppealsRequestObject) (generated.ListHNRAppealsResponseObject, error) {
	session, problem, err := handler.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListHNRAppeals401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListHNRAppeals403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	query := traffic.HNRAppealQuery{Filter: traffic.HNRAppealFilterPending, Limit: 30}
	if request.Params.Q != nil {
		query.Query = *request.Params.Q
	}
	if request.Params.Filter != nil {
		query.Filter = traffic.HNRAppealFilter(*request.Params.Filter)
	}
	if request.Params.Limit != nil {
		query.Limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		query.Offset = *request.Params.Offset
	}
	page, err := handler.trafficOverview.HNRAppeals(ctx, staffActor(session), query)
	switch {
	case errors.Is(err, traffic.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_hnr_appeal_query", "H&R 申诉查询无效", "请检查筛选与分页参数。")
		return generated.ListHNRAppeals400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "hnr_appeal_read_denied", "无法查看 H&R 申诉", "当前后台身份没有 H&R 规则查看权限。")
		return generated.ListHNRAppeals403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	items := make([]generated.HNRAppeal, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, hnrAppealDTO(item))
	}
	return generated.ListHNRAppeals200JSONResponse{
		Items: items, Total: countText(page.Total), Limit: page.Limit, Offset: page.Offset,
	}, nil
}

func (handler *Handler) DecideHNRAppeal(ctx context.Context, request generated.DecideHNRAppealRequestObject) (generated.DecideHNRAppealResponseObject, error) {
	if request.Body == nil {
		return hnrAppealDecisionBadRequest(ctx), nil
	}
	session, problem, err := handler.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.DecideHNRAppeal401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.DecideHNRAppeal403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	result, err := handler.trafficOverview.DecideHNRAppeal(ctx, staffActor(session), traffic.DecideHNRAppealInput{
		AppealID: request.AppealId, Decision: traffic.HNRAppealDecision(request.Body.Decision),
		ExpectedObligationVersion: request.Body.ExpectedObligationVersion,
		Response:                  request.Body.Response,
	})
	switch {
	case errors.Is(err, traffic.ErrInput):
		return hnrAppealDecisionBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden), errors.Is(err, traffic.ErrSelfTarget):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "hnr_appeal_decision_denied", "无法处理 H&R 申诉", "当前后台身份无权处置该申诉。")
		return generated.DecideHNRAppeal403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, traffic.ErrNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "hnr_appeal_not_found", "申诉不存在", "该 H&R 申诉不存在。")
		return generated.DecideHNRAppeal404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, traffic.ErrHNRAppealResolved), errors.Is(err, traffic.ErrHNRNotAppealable), errors.Is(err, traffic.ErrConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "hnr_appeal_decision_conflict", "申诉或 H&R 状态已变化", "请刷新列表后重新确认。")
		return generated.DecideHNRAppeal409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.DecideHNRAppeal200JSONResponse(hnrAppealDTO(result)), nil
}

func myHNRAppealDTO(input traffic.HNRAppeal) generated.MyHNRAppeal {
	return generated.MyHNRAppeal{
		Status: generated.HNRAppealStatus(input.Status), Statement: input.Statement,
		SubmittedAt: input.CreatedAt, ResolvedAt: input.ResolvedAt, Response: input.Response,
	}
}

func myHNRAppealFromProjectionDTO(input traffic.MyHNRAppeal) generated.MyHNRAppeal {
	return generated.MyHNRAppeal{
		Status: generated.HNRAppealStatus(input.Status), Statement: input.Statement,
		SubmittedAt: input.SubmittedAt, ResolvedAt: input.ResolvedAt, Response: input.Response,
	}
}

func hnrAppealDTO(input traffic.HNRAppeal) generated.HNRAppeal {
	return generated.HNRAppeal{
		Id: input.ID, ObligationId: input.ObligationID, UserId: input.UserID,
		UserNumericId: input.UserNumericID, Username: input.Username,
		Torrent:   generated.TrafficTorrentReference{Id: input.TorrentID, Title: input.TorrentTitle},
		Statement: input.Statement, CreatedAt: input.CreatedAt,
		Status: generated.HNRAppealStatus(input.Status), Response: input.Response,
		ResolvedAt: input.ResolvedAt, ObligationStatus: generated.HNRStatus(input.ObligationStatus),
		ObligationVersion:        input.ObligationVersion,
		SeededSeconds:            generated.HNRDurationSeconds(strconv.FormatInt(input.SeededSeconds, 10)),
		RequiredSeedSeconds:      generated.HNRDurationSeconds(strconv.FormatInt(input.RequiredSeedSeconds, 10)),
		RawRatioBasisPoints:      generated.HNRRatioBasisPoints(strconv.FormatInt(input.RawRatioBasisPoints, 10)),
		RequiredRatioBasisPoints: generated.HNRRatioBasisPoints(strconv.FormatInt(input.RequiredRatioBasisPoints, 10)),
		GraceEndsAt:              input.GraceEndsAt, Replayed: input.Replayed,
	}
}

func hnrAppealSubmitBadRequest(ctx context.Context) generated.SubmitMyHNRAppeal400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_hnr_appeal", "申诉内容无效", "请用 20 到 1000 个字说明情况和申诉理由。")
	return generated.SubmitMyHNRAppeal400ApplicationProblemPlusJSONResponse{
		ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
	}
}

func hnrAppealDecisionBadRequest(ctx context.Context) generated.DecideHNRAppeal400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_hnr_appeal_decision", "处理内容无效", "请选择批准或驳回，并填写 10 到 1000 个字的处理意见。")
	return generated.DecideHNRAppeal400ApplicationProblemPlusJSONResponse{
		ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
	}
}
