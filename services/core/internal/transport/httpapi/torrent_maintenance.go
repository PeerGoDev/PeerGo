package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/oapi-codegen/runtime"
	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	"github.com/peergo/peergo/services/core/internal/platform/audittext"
)

type TorrentMaintenanceService interface {
	UpdatePublishedMetadata(context.Context, string, string, torrents.UpdatePublishedMetadataInput) (torrents.PublishedMetadataRevision, error)
	SubmitPublishedContentChange(context.Context, string, string, torrents.SubmitPublishedContentChangeInput) (torrents.PublishedContentChange, error)
	ListPublishedContentChanges(context.Context, authz.StaffActor, torrents.PublishedContentChangeQuery) (torrents.ManagedPublishedContentChangePage, error)
	DecidePublishedContentChange(context.Context, authz.StaffActor, torrents.DecidePublishedContentChangeInput) (torrents.PublishedContentChangeDecisionResult, error)
	SubmitPublishedScreenshotChange(context.Context, string, string, torrents.SubmitPublishedScreenshotChangeInput) (torrents.PublishedScreenshotChange, error)
	ListPublishedScreenshotChanges(context.Context, authz.StaffActor, torrents.PublishedScreenshotChangeQuery) (torrents.ManagedPublishedScreenshotChangePage, error)
	DecidePublishedScreenshotChange(context.Context, authz.StaffActor, torrents.DecidePublishedScreenshotChangeInput) (torrents.PublishedScreenshotChangeDecisionResult, error)
	PublishedScreenshotChangeImage(context.Context, authz.StaffActor, uuid.UUID, torrents.ScreenshotChangeSide, int) (torrents.PublicScreenshot, error)
	SubmitTorrentWithdrawal(context.Context, string, string, torrents.SubmitTorrentWithdrawalInput) (torrents.TorrentWithdrawalRequest, error)
	ListTorrentWithdrawals(context.Context, authz.StaffActor, torrents.TorrentWithdrawalQuery) (torrents.ManagedTorrentWithdrawalPage, error)
	DecideTorrentWithdrawal(context.Context, authz.StaffActor, torrents.DecideTorrentWithdrawalInput) (torrents.TorrentWithdrawalDecisionResult, error)
	CreateTorrentReport(context.Context, string, string, torrents.CreateTorrentReportInput) (torrents.TorrentReportReceipt, error)
	ListTorrentReportCases(context.Context, authz.StaffActor, torrents.TorrentReportCaseQuery) (torrents.ManagedTorrentReportCasePage, error)
	DecideTorrentReportCase(context.Context, authz.StaffActor, torrents.DecideTorrentReportCaseInput) (torrents.TorrentReportDecisionResult, error)
}

func (h *Handler) CreateTorrentReport(ctx context.Context, request generated.CreateTorrentReportRequestObject) (generated.CreateTorrentReportResponseObject, error) {
	if request.Body == nil {
		return createTorrentReportBadRequest(ctx), nil
	}
	receipt, err := h.torrentMaintenance.CreateTorrentReport(
		ctx,
		sessionTokenFromContext(ctx),
		string(request.Params.XCSRFToken),
		torrents.CreateTorrentReportInput{
			RequestID:  request.Params.IdempotencyKey,
			TorrentID:  torrents.TorrentID(request.TorrentId),
			ReasonCode: torrents.TorrentReportReasonCode(request.Body.ReasonCode),
			Details:    request.Body.Details,
		},
	)
	if response, handled := createTorrentReportErrorResponse(ctx, err); handled {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	return generated.CreateTorrentReport201JSONResponse{
		Body:    torrentReportReceiptDTO(receipt),
		Headers: generated.CreateTorrentReport201ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func (h *Handler) ListManagedTorrentReportCases(ctx context.Context, request generated.ListManagedTorrentReportCasesRequestObject) (generated.ListManagedTorrentReportCasesResponseObject, error) {
	session, authenticationProblem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.ListManagedTorrentReportCases401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.ListManagedTorrentReportCases403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	query := torrents.TorrentReportCaseQuery{State: torrents.TorrentReportCaseOpen, Limit: torrents.DefaultTorrentReportCaseLimit}
	if request.Params.State != nil {
		query.State = torrents.TorrentReportCaseState(*request.Params.State)
	}
	if request.Params.Limit != nil {
		query.Limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		query.Offset = *request.Params.Offset
	}
	page, err := h.torrentMaintenance.ListTorrentReportCases(ctx, staffActor(session), query)
	switch {
	case errors.Is(err, torrents.ErrTorrentReportInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_report_case_query", "种子举报查询无效", "请检查案件状态和分页范围。")
		return generated.ListManagedTorrentReportCases400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_report_review_denied", "无法查看种子举报", "当前后台身份没有 torrent.report.review 权限。")
		return generated.ListManagedTorrentReportCases403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListManagedTorrentReportCases200JSONResponse(managedTorrentReportCasePageDTO(page)), nil
}

func (h *Handler) DecideManagedTorrentReportCase(ctx context.Context, request generated.DecideManagedTorrentReportCaseRequestObject) (generated.DecideManagedTorrentReportCaseResponseObject, error) {
	if request.Body == nil {
		return decideTorrentReportCaseBadRequest(ctx), nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.DecideManagedTorrentReportCase401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.DecideManagedTorrentReportCase403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.torrentMaintenance.DecideTorrentReportCase(ctx, staffActor(session), torrents.DecideTorrentReportCaseInput{
		DecisionID:             request.Params.IdempotencyKey,
		CaseID:                 request.CaseId,
		ExpectedCaseVersion:    request.Body.ExpectedCaseVersion,
		ExpectedTorrentVersion: request.Body.ExpectedTorrentVersion,
		Decision:               torrents.TorrentReportDecision(request.Body.Decision),
		ReasonCode:             torrents.TorrentReportDecisionReasonCode(request.Body.ReasonCode),
		Note:                   request.Body.Note,
	})
	if response, handled := decideTorrentReportCaseErrorResponse(ctx, err); handled {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	return generated.DecideManagedTorrentReportCase200JSONResponse(torrentReportDecisionResultDTO(result)), nil
}

func (h *Handler) SubmitMyTorrentWithdrawal(ctx context.Context, request generated.SubmitMyTorrentWithdrawalRequestObject) (generated.SubmitMyTorrentWithdrawalResponseObject, error) {
	if request.Body == nil {
		return torrentWithdrawalSubmitBadRequest(ctx), nil
	}
	result, err := h.torrentMaintenance.SubmitTorrentWithdrawal(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), torrents.SubmitTorrentWithdrawalInput{
		RequestID: request.Params.IdempotencyKey, TorrentID: torrents.TorrentID(request.TorrentId),
		ExpectedVersion: request.Body.ExpectedVersion, Reason: request.Body.Reason,
	})
	if response, handled := torrentWithdrawalSubmitErrorResponse(ctx, err); handled {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	return generated.SubmitMyTorrentWithdrawal201JSONResponse{
		Body:    torrentWithdrawalRequestDTO(result),
		Headers: generated.SubmitMyTorrentWithdrawal201ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func (h *Handler) ListManagedTorrentWithdrawals(ctx context.Context, request generated.ListManagedTorrentWithdrawalsRequestObject) (generated.ListManagedTorrentWithdrawalsResponseObject, error) {
	session, authenticationProblem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.ListManagedTorrentWithdrawals401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.ListManagedTorrentWithdrawals403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	query := torrents.TorrentWithdrawalQuery{Limit: torrents.DefaultTorrentWithdrawalLimit}
	if request.Params.Status != nil {
		query.Status = torrents.TorrentWithdrawalStatus(*request.Params.Status)
	}
	if request.Params.Limit != nil {
		query.Limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		query.Offset = *request.Params.Offset
	}
	page, err := h.torrentMaintenance.ListTorrentWithdrawals(ctx, staffActor(session), query)
	switch {
	case errors.Is(err, torrents.ErrTorrentWithdrawalInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_withdrawal_query", "撤回申请查询无效", "请检查状态筛选和分页范围。")
		return generated.ListManagedTorrentWithdrawals400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_withdrawal_review_denied", "无法查看撤回申请", "当前后台身份没有 torrent.withdraw.review 权限。")
		return generated.ListManagedTorrentWithdrawals403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListManagedTorrentWithdrawals200JSONResponse(managedTorrentWithdrawalPageDTO(page)), nil
}

func (h *Handler) DecideManagedTorrentWithdrawal(ctx context.Context, request generated.DecideManagedTorrentWithdrawalRequestObject) (generated.DecideManagedTorrentWithdrawalResponseObject, error) {
	if request.Body == nil {
		return torrentWithdrawalDecisionBadRequest(ctx), nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.DecideManagedTorrentWithdrawal401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.DecideManagedTorrentWithdrawal403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.torrentMaintenance.DecideTorrentWithdrawal(ctx, staffActor(session), torrents.DecideTorrentWithdrawalInput{
		DecisionID: request.Params.IdempotencyKey, RequestID: request.RequestId,
		ExpectedRequestVersion: request.Body.ExpectedRequestVersion,
		Decision:               torrents.TorrentWithdrawalDecision(request.Body.Decision), Reason: request.Body.Reason,
	})
	if response, handled := torrentWithdrawalDecisionErrorResponse(ctx, err); handled {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	return generated.DecideManagedTorrentWithdrawal200JSONResponse(torrentWithdrawalDecisionDTO(result)), nil
}

func (h *Handler) SubmitMyPublishedTorrentContentChange(ctx context.Context, request generated.SubmitMyPublishedTorrentContentChangeRequestObject) (generated.SubmitMyPublishedTorrentContentChangeResponseObject, error) {
	if request.Body == nil {
		return publishedTorrentContentChangeBadRequest(ctx), nil
	}
	result, err := h.torrentMaintenance.SubmitPublishedContentChange(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), torrents.SubmitPublishedContentChangeInput{
		RequestID: request.Params.IdempotencyKey, TorrentID: torrents.TorrentID(request.TorrentId),
		ExpectedVersion: request.Body.ExpectedVersion, Description: request.Body.Description,
		MediaInfo: request.Body.MediaInfo, ExternalIdentifiers: publishedContentExternalIdentifiers(request.Body.ExternalIdentifiers),
		Reason: request.Body.Reason,
	})
	if response, handled := publishedTorrentContentChangeSubmitErrorResponse(ctx, err); handled {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	return generated.SubmitMyPublishedTorrentContentChange201JSONResponse{
		Body:    publishedTorrentContentChangeDTO(result),
		Headers: generated.SubmitMyPublishedTorrentContentChange201ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func (h *Handler) ListManagedPublishedTorrentContentChanges(ctx context.Context, request generated.ListManagedPublishedTorrentContentChangesRequestObject) (generated.ListManagedPublishedTorrentContentChangesResponseObject, error) {
	session, authenticationProblem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.ListManagedPublishedTorrentContentChanges401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.ListManagedPublishedTorrentContentChanges403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	query := torrents.PublishedContentChangeQuery{Limit: torrents.DefaultPublishedContentChangeLimit}
	if request.Params.Status != nil {
		query.Status = torrents.PublishedContentChangeStatus(*request.Params.Status)
	}
	if request.Params.Limit != nil {
		query.Limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		query.Offset = *request.Params.Offset
	}
	page, err := h.torrentMaintenance.ListPublishedContentChanges(ctx, staffActor(session), query)
	switch {
	case errors.Is(err, torrents.ErrPublishedContentChangeInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_content_change_query", "内容修改查询无效", "请检查状态筛选和分页范围。")
		return generated.ListManagedPublishedTorrentContentChanges400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_content_change_review_denied", "无法查看内容修改审核", "当前后台身份没有 torrent.content.change.review 权限。")
		return generated.ListManagedPublishedTorrentContentChanges403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListManagedPublishedTorrentContentChanges200JSONResponse(managedPublishedTorrentContentChangePageDTO(page)), nil
}

func (h *Handler) DecideManagedPublishedTorrentContentChange(ctx context.Context, request generated.DecideManagedPublishedTorrentContentChangeRequestObject) (generated.DecideManagedPublishedTorrentContentChangeResponseObject, error) {
	if request.Body == nil {
		return decidePublishedTorrentContentChangeBadRequest(ctx), nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.DecideManagedPublishedTorrentContentChange401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.DecideManagedPublishedTorrentContentChange403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.torrentMaintenance.DecidePublishedContentChange(ctx, staffActor(session), torrents.DecidePublishedContentChangeInput{
		DecisionID: request.Params.IdempotencyKey, RequestID: request.RequestId,
		ExpectedRequestVersion: request.Body.ExpectedRequestVersion,
		Decision:               torrents.PublishedContentChangeDecision(request.Body.Decision), Reason: request.Body.Reason,
	})
	if response, handled := publishedTorrentContentChangeDecisionErrorResponse(ctx, err); handled {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	return generated.DecideManagedPublishedTorrentContentChange200JSONResponse(publishedTorrentContentChangeDecisionDTO(result)), nil
}

func (h *Handler) SubmitMyPublishedTorrentScreenshotChange(ctx context.Context, request generated.SubmitMyPublishedTorrentScreenshotChangeRequestObject) (generated.SubmitMyPublishedTorrentScreenshotChangeResponseObject, error) {
	if request.Body == nil {
		return publishedTorrentScreenshotChangeBadRequest(ctx), nil
	}
	var body generated.SubmitPublishedTorrentScreenshotChangeRequest
	if err := runtime.BindMultipart(&body, *request.Body); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			problem := newProblemFromContext(ctx, http.StatusRequestEntityTooLarge, "torrent_screenshot_change_too_large", "截图修改过大", "上传内容超过当前站点允许的大小。")
			return generated.SubmitMyPublishedTorrentScreenshotChange413ApplicationProblemPlusJSONResponse(problem), nil
		}
		return publishedTorrentScreenshotChangeBadRequest(ctx), nil
	}
	uploads := make([]torrents.TorrentScreenshotInput, 0)
	if body.Uploads != nil {
		for _, file := range *body.Uploads {
			raw, err := file.Bytes()
			if err != nil || len(raw) == 0 {
				return publishedTorrentScreenshotChangeBadRequest(ctx), nil
			}
			uploads = append(uploads, torrents.TorrentScreenshotInput{Raw: raw})
		}
	}
	manifest := make([]torrents.ScreenshotManifestItem, 0, len(body.Manifest))
	for _, item := range body.Manifest {
		manifest = append(manifest, torrents.ScreenshotManifestItem{Source: torrents.ScreenshotManifestSource(item.Source), Index: item.Index})
	}
	result, err := h.torrentMaintenance.SubmitPublishedScreenshotChange(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), torrents.SubmitPublishedScreenshotChangeInput{
		RequestID: request.Params.IdempotencyKey, TorrentID: torrents.TorrentID(request.TorrentId), ExpectedVersion: body.ExpectedVersion,
		Manifest: manifest, Uploads: uploads, Reason: audittext.Reason(body.Reason),
	})
	if response, handled := publishedTorrentScreenshotChangeSubmitErrorResponse(ctx, err); handled {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	return generated.SubmitMyPublishedTorrentScreenshotChange201JSONResponse{
		Body:    publishedTorrentScreenshotChangeDTO(result),
		Headers: generated.SubmitMyPublishedTorrentScreenshotChange201ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func (h *Handler) ListManagedPublishedTorrentScreenshotChanges(ctx context.Context, request generated.ListManagedPublishedTorrentScreenshotChangesRequestObject) (generated.ListManagedPublishedTorrentScreenshotChangesResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListManagedPublishedTorrentScreenshotChanges401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListManagedPublishedTorrentScreenshotChanges403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	query := torrents.PublishedScreenshotChangeQuery{Limit: torrents.DefaultPublishedScreenshotChangeLimit}
	if request.Params.Status != nil {
		query.Status = torrents.PublishedScreenshotChangeStatus(*request.Params.Status)
	}
	if request.Params.Limit != nil {
		query.Limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		query.Offset = *request.Params.Offset
	}
	page, err := h.torrentMaintenance.ListPublishedScreenshotChanges(ctx, staffActor(session), query)
	switch {
	case errors.Is(err, torrents.ErrPublishedScreenshotChangeInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_screenshot_change_query", "截图修改查询无效", "请检查状态筛选和分页范围。")
		return generated.ListManagedPublishedTorrentScreenshotChanges400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_screenshot_change_review_denied", "无法查看截图修改审核", "当前后台身份没有 torrent.screenshot.change.review 权限。")
		return generated.ListManagedPublishedTorrentScreenshotChanges403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListManagedPublishedTorrentScreenshotChanges200JSONResponse(managedPublishedTorrentScreenshotChangePageDTO(page)), nil
}

func (h *Handler) DecideManagedPublishedTorrentScreenshotChange(ctx context.Context, request generated.DecideManagedPublishedTorrentScreenshotChangeRequestObject) (generated.DecideManagedPublishedTorrentScreenshotChangeResponseObject, error) {
	if request.Body == nil {
		return decidePublishedTorrentScreenshotChangeBadRequest(ctx), nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.DecideManagedPublishedTorrentScreenshotChange401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.DecideManagedPublishedTorrentScreenshotChange403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	result, err := h.torrentMaintenance.DecidePublishedScreenshotChange(ctx, staffActor(session), torrents.DecidePublishedScreenshotChangeInput{
		DecisionID: request.Params.IdempotencyKey, RequestID: request.RequestId,
		ExpectedRequestVersion: request.Body.ExpectedRequestVersion,
		Decision:               torrents.PublishedScreenshotChangeDecision(request.Body.Decision), Reason: request.Body.Reason,
	})
	if response, handled := publishedTorrentScreenshotChangeDecisionErrorResponse(ctx, err); handled {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	return generated.DecideManagedPublishedTorrentScreenshotChange200JSONResponse(publishedTorrentScreenshotChangeDecisionDTO(result)), nil
}

func (h *Handler) GetManagedPublishedTorrentScreenshotChangeImage(ctx context.Context, request generated.GetManagedPublishedTorrentScreenshotChangeImageRequestObject) (generated.GetManagedPublishedTorrentScreenshotChangeImageResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.GetManagedPublishedTorrentScreenshotChangeImage401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem)}, nil
		}
		return generated.GetManagedPublishedTorrentScreenshotChangeImage403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	image, err := h.torrentMaintenance.PublishedScreenshotChangeImage(ctx, staffActor(session), request.RequestId, torrents.ScreenshotChangeSide(request.Side), request.Position)
	switch {
	case errors.Is(err, torrents.ErrPublishedScreenshotChangeInput), errors.Is(err, torrents.ErrPublishedScreenshotChangeNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_screenshot_change_image_not_found", "审核预览不存在", "该截图位置不存在，或修改请求已经不在当前数据集中。")
		return generated.GetManagedPublishedTorrentScreenshotChangeImage404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_screenshot_change_review_denied", "无法查看截图预览", "当前后台身份没有 torrent.screenshot.change.review 权限。")
		return generated.GetManagedPublishedTorrentScreenshotChangeImage403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	headers := generated.GetManagedPublishedTorrentScreenshotChangeImage200ResponseHeaders{CacheControl: "private, max-age=60", ETag: image.ETag}
	reader, length := bytes.NewReader(image.Data), int64(len(image.Data))
	switch image.ContentType {
	case "image/jpeg":
		return generated.GetManagedPublishedTorrentScreenshotChangeImage200ImagejpegResponse{Body: reader, Headers: headers, ContentLength: length}, nil
	case "image/png":
		return generated.GetManagedPublishedTorrentScreenshotChangeImage200ImagepngResponse{Body: reader, Headers: headers, ContentLength: length}, nil
	case "image/webp":
		return generated.GetManagedPublishedTorrentScreenshotChangeImage200ImagewebpResponse{Body: reader, Headers: headers, ContentLength: length}, nil
	case "image/gif":
		return generated.GetManagedPublishedTorrentScreenshotChangeImage200ImagegifResponse{Body: reader, Headers: headers, ContentLength: length}, nil
	default:
		return nil, torrents.ErrPublishedScreenshotChangeUnavailable
	}
}

func (h *Handler) UpdateMyPublishedTorrentMetadata(ctx context.Context, request generated.UpdateMyPublishedTorrentMetadataRequestObject) (generated.UpdateMyPublishedTorrentMetadataResponseObject, error) {
	if request.Body == nil {
		return publishedTorrentMetadataBadRequest(ctx), nil
	}
	result, err := h.torrentMaintenance.UpdatePublishedMetadata(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), torrents.UpdatePublishedMetadataInput{
		RequestID: request.Params.IdempotencyKey, TorrentID: torrents.TorrentID(request.TorrentId),
		ExpectedVersion: request.Body.ExpectedVersion, CategoryID: request.Body.CategoryId,
		Title: request.Body.Title, Subtitle: request.Body.Subtitle, Reason: request.Body.Reason,
	})
	if response, handled := publishedTorrentMetadataErrorResponse(ctx, err); handled {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	return generated.UpdateMyPublishedTorrentMetadata200JSONResponse{
		Body: generated.PublishedTorrentMetadataRevision{
			Id: result.ID, TorrentId: int64(result.TorrentID), Version: result.Version,
			CategoryId: result.Metadata.CategoryID, Title: result.Metadata.Title,
			Subtitle: result.Metadata.Subtitle, Reason: result.Reason, UpdatedAt: result.UpdatedAt,
		},
		Headers: generated.UpdateMyPublishedTorrentMetadata200ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func publishedTorrentMetadataErrorResponse(ctx context.Context, err error) (generated.UpdateMyPublishedTorrentMetadataResponseObject, bool) {
	switch {
	case err == nil:
		return nil, false
	case errors.Is(err, torrents.ErrTorrentMetadataUpdateInput):
		return publishedTorrentMetadataBadRequest(ctx), true
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "web_session_required", "需要登录", "请重新登录后再修改发布资料。")
		return generated.UpdateMyPublishedTorrentMetadata401ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重新提交发布资料。")
		return generated.UpdateMyPublishedTorrentMetadata403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentMetadataUpdateEmailUnverified):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "verified_email_required", "需要先验证邮箱", "验证当前账户邮箱后才能修改已发布种子。")
		return generated.UpdateMyPublishedTorrentMetadata403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_metadata_update_denied", "无法修改发布资料", "当前账户没有修改本人已发布种子的权限。")
		return generated.UpdateMyPublishedTorrentMetadata403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentMetadataUpdateNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_metadata_update_not_found", "已发布种子不存在", "该种子不存在，或不属于当前账户。")
		return generated.UpdateMyPublishedTorrentMetadata404ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentMetadataUpdateIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_metadata_update_idempotency_conflict", "本次修改内容已经变化", "请刷新发布记录后重新开始修改。")
		return generated.UpdateMyPublishedTorrentMetadata409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentMetadataUpdateVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_metadata_update_version_conflict", "发布资料已经变化", "请重新载入发布记录并核对最新资料。")
		return generated.UpdateMyPublishedTorrentMetadata409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentMetadataUpdateStateConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_metadata_update_state_conflict", "当前状态不能修改", "只有仍处于已发布状态的本人种子可以直接修改资料。")
		return generated.UpdateMyPublishedTorrentMetadata409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentMetadataUpdateCategoryUnavailable):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_metadata_update_category_unavailable", "所选分类当前不可用", "请选择一个当前启用的种子分类。")
		return generated.UpdateMyPublishedTorrentMetadata409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentMetadataUpdateUnchanged):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_metadata_update_unchanged", "发布资料没有变化", "请先修改分类、标题或副标题。")
		return generated.UpdateMyPublishedTorrentMetadata409ApplicationProblemPlusJSONResponse(problem), true
	default:
		return nil, false
	}
}

func publishedTorrentMetadataBadRequest(ctx context.Context) generated.UpdateMyPublishedTorrentMetadata400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_metadata_update", "发布资料无效", "请检查分类、标题、副标题、当前版本和至少 10 个字符的修改说明。")
	return generated.UpdateMyPublishedTorrentMetadata400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func publishedTorrentContentChangeSubmitErrorResponse(ctx context.Context, err error) (generated.SubmitMyPublishedTorrentContentChangeResponseObject, bool) {
	switch {
	case err == nil:
		return nil, false
	case errors.Is(err, torrents.ErrPublishedContentChangeInput):
		return publishedTorrentContentChangeBadRequest(ctx), true
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "web_session_required", "需要登录", "请重新登录后再提交内容修改。")
		return generated.SubmitMyPublishedTorrentContentChange401ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重新提交内容修改。")
		return generated.SubmitMyPublishedTorrentContentChange403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrPublishedContentChangeEmailUnverified):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "verified_email_required", "需要先验证邮箱", "验证当前账户邮箱后才能修改已发布种子。")
		return generated.SubmitMyPublishedTorrentContentChange403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_content_change_submit_denied", "无法提交内容修改", "当前账户没有修改本人已发布种子内容的权限。")
		return generated.SubmitMyPublishedTorrentContentChange403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrPublishedContentChangeNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "published_torrent_not_found", "已发布种子不存在", "该种子不存在，或不属于当前账户。")
		return generated.SubmitMyPublishedTorrentContentChange404ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrPublishedContentChangePending):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_content_change_pending", "已有内容修改等待审核", "同一种子一次只能保留一份待审核修改，请等待处理后再提交。")
		return generated.SubmitMyPublishedTorrentContentChange409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrPublishedContentChangeUnchanged):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_content_change_unchanged", "内容没有变化", "请先修改简介、MediaInfo 或外部资料编号。")
		return generated.SubmitMyPublishedTorrentContentChange409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrPublishedContentChangeIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_content_change_idempotency_conflict", "请求标识已经使用", "请刷新发布记录后重新提交本次修改。")
		return generated.SubmitMyPublishedTorrentContentChange409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrPublishedContentChangeVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_content_change_version_conflict", "种子内容已经变化", "请重新载入最新公开内容后再修改。")
		return generated.SubmitMyPublishedTorrentContentChange409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrPublishedContentChangeStateConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_content_change_state_conflict", "当前状态不能修改内容", "只有仍处于已发布状态的本人种子可以提交内容修改。")
		return generated.SubmitMyPublishedTorrentContentChange409ApplicationProblemPlusJSONResponse(problem), true
	default:
		return nil, false
	}
}

func publishedTorrentContentChangeDecisionErrorResponse(ctx context.Context, err error) (generated.DecideManagedPublishedTorrentContentChangeResponseObject, bool) {
	switch {
	case err == nil:
		return nil, false
	case errors.Is(err, torrents.ErrPublishedContentChangeInput):
		return decidePublishedTorrentContentChangeBadRequest(ctx), true
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_content_change_review_denied", "无法处理内容修改", "当前后台身份没有 torrent.content.change.review 权限。")
		return generated.DecideManagedPublishedTorrentContentChange403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrPublishedContentChangeSelfReview):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_content_change_self_review_denied", "不能审核自己的内容修改", "请由另一名具备权限的审核员处理该请求。")
		return generated.DecideManagedPublishedTorrentContentChange403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrPublishedContentChangeNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_content_change_not_found", "内容修改不存在", "该修改请求不存在或已经不在当前数据集中。")
		return generated.DecideManagedPublishedTorrentContentChange404ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrPublishedContentChangeIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_content_change_decision_idempotency_conflict", "审核请求内容已经变化", "请勿复用其他审核决定的 Idempotency-Key。")
		return generated.DecideManagedPublishedTorrentContentChange409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrPublishedContentChangeVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_content_change_version_conflict", "待审内容或公开内容已经变化", "请重新载入审核队列并核对最新版本。")
		return generated.DecideManagedPublishedTorrentContentChange409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrPublishedContentChangeStateConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_content_change_state_conflict", "当前状态不能处理", "该请求已处理、种子已下架，或公开内容已不再匹配提交时的基线。")
		return generated.DecideManagedPublishedTorrentContentChange409ApplicationProblemPlusJSONResponse(problem), true
	default:
		return nil, false
	}
}

func publishedTorrentContentChangeBadRequest(ctx context.Context) generated.SubmitMyPublishedTorrentContentChange400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_content_change", "内容修改无效", "请检查当前版本、简介、MediaInfo、外部资料编号和至少 10 个字符的修改说明。")
	return generated.SubmitMyPublishedTorrentContentChange400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func decidePublishedTorrentContentChangeBadRequest(ctx context.Context) generated.DecideManagedPublishedTorrentContentChange400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_content_change_decision", "内容修改审核无效", "请检查请求版本、审核决定和至少 10 个字符的审核说明。")
	return generated.DecideManagedPublishedTorrentContentChange400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func publishedContentExternalIdentifiers(items []generated.TorrentExternalIdentifier) []torrents.ExternalIdentifier {
	result := make([]torrents.ExternalIdentifier, 0, len(items))
	for _, item := range items {
		result = append(result, torrents.ExternalIdentifier{Provider: string(item.Provider), ExternalID: item.ExternalId})
	}
	return result
}

func publishedContentExternalIdentifierDTOs(items []torrents.ExternalIdentifier) []generated.TorrentExternalIdentifier {
	result := make([]generated.TorrentExternalIdentifier, 0, len(items))
	for _, item := range items {
		result = append(result, generated.TorrentExternalIdentifier{Provider: generated.TorrentExternalIdentifierProvider(item.Provider), ExternalId: item.ExternalID})
	}
	return result
}

func publishedTorrentContentSnapshotDTO(snapshot torrents.PublishedContentSnapshot) generated.PublishedTorrentContentSnapshot {
	return generated.PublishedTorrentContentSnapshot{
		Description: snapshot.Description, MediaInfo: snapshot.MediaInfo,
		ExternalIdentifiers: publishedContentExternalIdentifierDTOs(snapshot.ExternalIdentifiers),
	}
}

func publishedTorrentContentChangeDTO(change torrents.PublishedContentChange) generated.PublishedTorrentContentChange {
	return generated.PublishedTorrentContentChange{
		Id: change.ID, TorrentId: int64(change.TorrentID), BaseTorrentVersion: change.BaseTorrentVersion,
		Base: publishedTorrentContentSnapshotDTO(change.Base), Candidate: publishedTorrentContentSnapshotDTO(change.Candidate),
		Reason: change.Reason, Status: generated.PublishedTorrentContentChangeStatus(change.Status), Version: change.Version,
		CreatedAt: change.CreatedAt, DecidedAt: change.DecidedAt,
	}
}

func managedPublishedTorrentContentChangePageDTO(page torrents.ManagedPublishedContentChangePage) generated.ManagedPublishedTorrentContentChangePage {
	items := make([]generated.ManagedPublishedTorrentContentChange, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, generated.ManagedPublishedTorrentContentChange{
			Change: publishedTorrentContentChangeDTO(item.PublishedContentChange), TorrentTitle: item.TorrentTitle,
			UploaderNumericId: item.UploaderNumericID, UploaderUsername: item.UploaderUsername,
			UploaderDisplayName: item.UploaderDisplayName,
		})
	}
	return generated.ManagedPublishedTorrentContentChangePage{Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset}
}

func publishedTorrentContentChangeDecisionDTO(result torrents.PublishedContentChangeDecisionResult) generated.PublishedTorrentContentChangeDecisionResult {
	return generated.PublishedTorrentContentChangeDecisionResult{
		DecisionId: result.DecisionID, RequestId: result.RequestID, TorrentId: int64(result.TorrentID),
		Decision:       generated.PublishedTorrentContentChangeDecisionResultDecision(result.Decision),
		RequestStatus:  generated.PublishedTorrentContentChangeStatus(result.RequestStatus),
		RequestVersion: result.RequestVersion, TorrentVersion: result.TorrentVersion, DecidedAt: result.DecidedAt,
	}
}

func publishedTorrentScreenshotChangeDTO(change torrents.PublishedScreenshotChange) generated.PublishedTorrentScreenshotChange {
	return generated.PublishedTorrentScreenshotChange{
		Id: change.ID, TorrentId: int64(change.TorrentID), BaseTorrentVersion: change.BaseTorrentVersion,
		BaseSetVersion: change.BaseSetVersion, BaseCount: change.BaseCount, CandidateCount: change.CandidateCount,
		Reason: change.Reason, Status: generated.PublishedTorrentScreenshotChangeStatus(change.Status),
		Version: change.Version, CreatedAt: change.CreatedAt, DecidedAt: change.DecidedAt,
	}
}

func managedPublishedTorrentScreenshotChangePageDTO(page torrents.ManagedPublishedScreenshotChangePage) generated.ManagedPublishedTorrentScreenshotChangePage {
	items := make([]generated.ManagedPublishedTorrentScreenshotChange, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, generated.ManagedPublishedTorrentScreenshotChange{
			Change: publishedTorrentScreenshotChangeDTO(item.PublishedScreenshotChange), TorrentTitle: item.TorrentTitle,
			UploaderNumericId: item.UploaderNumericID, UploaderUsername: item.UploaderUsername,
			UploaderDisplayName: item.UploaderDisplayName,
		})
	}
	return generated.ManagedPublishedTorrentScreenshotChangePage{Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset}
}

func publishedTorrentScreenshotChangeDecisionDTO(result torrents.PublishedScreenshotChangeDecisionResult) generated.PublishedTorrentScreenshotChangeDecisionResult {
	return generated.PublishedTorrentScreenshotChangeDecisionResult{
		DecisionId: result.DecisionID, RequestId: result.RequestID, TorrentId: int64(result.TorrentID),
		Decision:       generated.PublishedTorrentScreenshotChangeDecisionResultDecision(result.Decision),
		RequestStatus:  generated.PublishedTorrentScreenshotChangeStatus(result.RequestStatus),
		RequestVersion: result.RequestVersion, AttachmentVersion: result.AttachmentVersion, DecidedAt: result.DecidedAt,
	}
}

func publishedTorrentScreenshotChangeSubmitErrorResponse(ctx context.Context, err error) (generated.SubmitMyPublishedTorrentScreenshotChangeResponseObject, bool) {
	if validationCode, ok := torrents.ValidationCodeOf(err); ok {
		if validationCode == torrents.CodeObjectTooLarge {
			problem := newProblemFromContext(ctx, http.StatusRequestEntityTooLarge, "torrent_screenshot_too_large", "截图过大", "单张原始截图超过当前生效规则的大小或像素限制。")
			return generated.SubmitMyPublishedTorrentScreenshotChange413ApplicationProblemPlusJSONResponse(problem), true
		}
		return publishedTorrentScreenshotChangeBadRequest(ctx), true
	}
	switch {
	case err == nil:
		return nil, false
	case errors.Is(err, torrents.ErrPublishedScreenshotChangeInput):
		return publishedTorrentScreenshotChangeBadRequest(ctx), true
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "web_session_required", "需要登录", "请重新登录后再修改截图。")
		return generated.SubmitMyPublishedTorrentScreenshotChange401ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重新提交截图修改。")
		return generated.SubmitMyPublishedTorrentScreenshotChange403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrPublishedScreenshotChangeEmailUnverified):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "verified_email_required", "需要先验证邮箱", "验证当前账户邮箱后才能修改已发布种子的截图。")
		return generated.SubmitMyPublishedTorrentScreenshotChange403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_screenshot_change_submit_denied", "无法提交截图修改", "当前账户没有 torrent.screenshot.change.submit.self 权限。")
		return generated.SubmitMyPublishedTorrentScreenshotChange403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrPublishedScreenshotChangeNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "published_torrent_not_found", "已发布种子不存在", "该种子不存在，或不属于当前账户。")
		return generated.SubmitMyPublishedTorrentScreenshotChange404ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrPublishedScreenshotChangePending):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_screenshot_change_pending", "已有截图修改等待审核", "同一种子一次只能保留一份待审核的截图附件集。")
		return generated.SubmitMyPublishedTorrentScreenshotChange409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrPublishedScreenshotChangeUnchanged):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_screenshot_change_unchanged", "截图没有变化", "请先调整顺序、删除旧图或添加新图。")
		return generated.SubmitMyPublishedTorrentScreenshotChange409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrPublishedScreenshotChangeIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_screenshot_change_idempotency_conflict", "请求标识已经使用", "请刷新发布记录后重新提交。")
		return generated.SubmitMyPublishedTorrentScreenshotChange409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrPublishedScreenshotChangeVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_screenshot_change_version_conflict", "种子截图已经变化", "请重新载入最新公开图集后再修改。")
		return generated.SubmitMyPublishedTorrentScreenshotChange409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrPublishedScreenshotChangeStateConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_screenshot_change_state_conflict", "当前状态不能修改截图", "只有仍处于已发布状态的本人种子可以修改截图。")
		return generated.SubmitMyPublishedTorrentScreenshotChange409ApplicationProblemPlusJSONResponse(problem), true
	default:
		return nil, false
	}
}

func publishedTorrentScreenshotChangeDecisionErrorResponse(ctx context.Context, err error) (generated.DecideManagedPublishedTorrentScreenshotChangeResponseObject, bool) {
	switch {
	case err == nil:
		return nil, false
	case errors.Is(err, torrents.ErrPublishedScreenshotChangeInput):
		return decidePublishedTorrentScreenshotChangeBadRequest(ctx), true
	case errors.Is(err, authz.ErrForbidden), errors.Is(err, torrents.ErrPublishedScreenshotChangeSelfReview):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_screenshot_change_review_denied", "无法处理截图修改", "当前身份没有审核权限，或不能审核自己的修改。")
		return generated.DecideManagedPublishedTorrentScreenshotChange403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrPublishedScreenshotChangeNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_screenshot_change_not_found", "截图修改不存在", "该修改请求不存在或已经不在当前数据集中。")
		return generated.DecideManagedPublishedTorrentScreenshotChange404ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrPublishedScreenshotChangeIdempotencyConflict), errors.Is(err, torrents.ErrPublishedScreenshotChangeVersionConflict), errors.Is(err, torrents.ErrPublishedScreenshotChangeStateConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_screenshot_change_conflict", "截图修改状态已经变化", "请重新载入审核队列并核对最新公开图集。")
		return generated.DecideManagedPublishedTorrentScreenshotChange409ApplicationProblemPlusJSONResponse(problem), true
	default:
		return nil, false
	}
}

func publishedTorrentScreenshotChangeBadRequest(ctx context.Context) generated.SubmitMyPublishedTorrentScreenshotChange400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_screenshot_change", "截图修改无效", "请检查图片、顺序、当前版本和至少 10 个字符的修改说明。")
	return generated.SubmitMyPublishedTorrentScreenshotChange400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func decidePublishedTorrentScreenshotChangeBadRequest(ctx context.Context) generated.DecideManagedPublishedTorrentScreenshotChange400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_screenshot_change_decision", "截图修改审核无效", "请检查请求版本、审核决定和至少 10 个字符的审核说明。")
	return generated.DecideManagedPublishedTorrentScreenshotChange400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func torrentWithdrawalSubmitErrorResponse(ctx context.Context, err error) (generated.SubmitMyTorrentWithdrawalResponseObject, bool) {
	switch {
	case err == nil:
		return nil, false
	case errors.Is(err, torrents.ErrTorrentWithdrawalInput):
		return torrentWithdrawalSubmitBadRequest(ctx), true
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "web_session_required", "需要登录", "请重新登录后再申请撤回种子。")
		return generated.SubmitMyTorrentWithdrawal401ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重新提交撤回申请。")
		return generated.SubmitMyTorrentWithdrawal403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentWithdrawalEmailUnverified):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "verified_email_required", "需要先验证邮箱", "验证当前账户邮箱后才能撤回已发布种子。")
		return generated.SubmitMyTorrentWithdrawal403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_withdrawal_submit_denied", "无法申请撤回", "当前账户没有申请撤回本人已发布种子的权限。")
		return generated.SubmitMyTorrentWithdrawal403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentWithdrawalNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_withdrawal_not_found", "已发布种子不存在", "该种子不存在，或不属于当前账户。")
		return generated.SubmitMyTorrentWithdrawal404ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentWithdrawalPending):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_withdrawal_pending", "已有撤回申请等待审核", "同一种子一次只能保留一份待审核撤回申请。")
		return generated.SubmitMyTorrentWithdrawal409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentWithdrawalContentChangePending):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_content_change_pending", "内容修改仍在审核", "请等待内容修改处理完成后再申请撤回。")
		return generated.SubmitMyTorrentWithdrawal409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentWithdrawalIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_withdrawal_idempotency_conflict", "请求标识已经使用", "请刷新发布记录后重新提交撤回申请。")
		return generated.SubmitMyTorrentWithdrawal409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentWithdrawalVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_withdrawal_version_conflict", "种子版本已经变化", "请重新载入发布记录并核对最新状态。")
		return generated.SubmitMyTorrentWithdrawal409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentWithdrawalStateConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_withdrawal_state_conflict", "当前状态不能申请撤回", "只有仍处于已发布状态的本人种子可以申请撤回。")
		return generated.SubmitMyTorrentWithdrawal409ApplicationProblemPlusJSONResponse(problem), true
	default:
		return nil, false
	}
}

func torrentWithdrawalDecisionErrorResponse(ctx context.Context, err error) (generated.DecideManagedTorrentWithdrawalResponseObject, bool) {
	switch {
	case err == nil:
		return nil, false
	case errors.Is(err, torrents.ErrTorrentWithdrawalInput):
		return torrentWithdrawalDecisionBadRequest(ctx), true
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_withdrawal_review_denied", "无法处理撤回申请", "当前后台身份没有 torrent.withdraw.review 权限。")
		return generated.DecideManagedTorrentWithdrawal403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentWithdrawalSelfReview):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_withdrawal_self_review_denied", "不能审核自己的撤回申请", "请由另一名具备权限的管理员处理。")
		return generated.DecideManagedTorrentWithdrawal403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentWithdrawalNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_withdrawal_not_found", "撤回申请不存在", "该申请不存在或已经不在当前数据集中。")
		return generated.DecideManagedTorrentWithdrawal404ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentWithdrawalActivePurchases):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_withdrawal_active_purchases", "仍有有效购买权益", "请先在购买记录中完成全部退款，再批准删除。")
		return generated.DecideManagedTorrentWithdrawal409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentWithdrawalCategoryUnavailable):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_withdrawal_category_unavailable", "暂时不能恢复发布", "该种子所属分类已停用，请先恢复分类或批准删除。")
		return generated.DecideManagedTorrentWithdrawal409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentWithdrawalObjectUnavailable):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_withdrawal_object_unavailable", "暂时不能恢复发布", "原始种子对象没有可验证位置，请先修复存储。")
		return generated.DecideManagedTorrentWithdrawal409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentWithdrawalIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_withdrawal_decision_idempotency_conflict", "审核请求内容已经变化", "请勿复用其他审核决定的 Idempotency-Key。")
		return generated.DecideManagedTorrentWithdrawal409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentWithdrawalVersionConflict), errors.Is(err, torrents.ErrTorrentWithdrawalStateConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_withdrawal_state_conflict", "撤回申请状态已经变化", "请重新载入审核队列并核对最新状态。")
		return generated.DecideManagedTorrentWithdrawal409ApplicationProblemPlusJSONResponse(problem), true
	default:
		return nil, false
	}
}

func torrentWithdrawalSubmitBadRequest(ctx context.Context) generated.SubmitMyTorrentWithdrawal400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_withdrawal", "撤回申请无效", "请检查当前种子版本和至少 10 个字符的撤回理由。")
	return generated.SubmitMyTorrentWithdrawal400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func torrentWithdrawalDecisionBadRequest(ctx context.Context) generated.DecideManagedTorrentWithdrawal400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_withdrawal_decision", "撤回审核无效", "请检查请求版本、审核决定和至少 10 个字符的审核说明。")
	return generated.DecideManagedTorrentWithdrawal400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func torrentWithdrawalRequestDTO(request torrents.TorrentWithdrawalRequest) generated.TorrentWithdrawalRequest {
	return generated.TorrentWithdrawalRequest{
		Id: request.ID, TorrentId: int64(request.TorrentID), TorrentTitle: request.TorrentTitle,
		Reason: request.Reason, ExpectedTorrentVersion: request.ExpectedTorrentVersion,
		DisabledTorrentVersion: request.DisabledTorrentVersion,
		Status:                 generated.TorrentWithdrawalStatus(request.Status), Version: request.Version,
		CreatedAt: request.CreatedAt, DecidedAt: request.DecidedAt,
	}
}

func managedTorrentWithdrawalPageDTO(page torrents.ManagedTorrentWithdrawalPage) generated.ManagedTorrentWithdrawalPage {
	items := make([]generated.ManagedTorrentWithdrawalRequest, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, generated.ManagedTorrentWithdrawalRequest{
			Request:           torrentWithdrawalRequestDTO(item.TorrentWithdrawalRequest),
			UploaderNumericId: item.UploaderNumericID, UploaderUsername: item.UploaderUsername,
			UploaderDisplayName: item.UploaderDisplayName, ActivePurchaseCount: item.ActivePurchaseCount,
		})
	}
	return generated.ManagedTorrentWithdrawalPage{Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset}
}

func torrentWithdrawalDecisionDTO(result torrents.TorrentWithdrawalDecisionResult) generated.TorrentWithdrawalDecisionResult {
	return generated.TorrentWithdrawalDecisionResult{
		DecisionId: result.DecisionID, RequestId: result.RequestID, TorrentId: int64(result.TorrentID),
		Decision:      generated.TorrentWithdrawalDecisionResultDecision(result.Decision),
		RequestStatus: generated.TorrentWithdrawalStatus(result.RequestStatus), RequestVersion: result.RequestVersion,
		TorrentState:   generated.TorrentWithdrawalDecisionResultTorrentState(result.TorrentState),
		TorrentVersion: result.TorrentVersion, DecidedAt: result.DecidedAt,
	}
}

func createTorrentReportErrorResponse(ctx context.Context, err error) (generated.CreateTorrentReportResponseObject, bool) {
	switch {
	case err == nil:
		return nil, false
	case errors.Is(err, torrents.ErrTorrentReportInput):
		return createTorrentReportBadRequest(ctx), true
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "web_session_required", "需要登录", "请登录后再举报种子。")
		return generated.CreateTorrentReport401ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重新提交举报。")
		return generated.CreateTorrentReport403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentReportEmailUnverified):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "verified_email_required", "需要先验证邮箱", "验证当前账户邮箱后才能举报种子。")
		return generated.CreateTorrentReport403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentReportSelf):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_report_self_denied", "不能举报自己的种子", "如果需要撤回本人种子，请在“我的发布”中提交撤回申请。")
		return generated.CreateTorrentReport403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_report_create_denied", "无法举报种子", "当前账户没有举报已发布种子的权限。")
		return generated.CreateTorrentReport403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentReportTargetNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_report_target_not_found", "种子不可举报", "该种子不存在或已经停止公开。")
		return generated.CreateTorrentReport404ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentAlreadyReported):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_already_reported", "已经提交过举报", "你已经参与当前举报案件，请等待后台处理。")
		return generated.CreateTorrentReport409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentReportIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_report_idempotency_conflict", "举报请求编号已经使用", "请关闭窗口后重新发起举报。")
		return generated.CreateTorrentReport409ApplicationProblemPlusJSONResponse(problem), true
	default:
		return nil, false
	}
}

func decideTorrentReportCaseErrorResponse(ctx context.Context, err error) (generated.DecideManagedTorrentReportCaseResponseObject, bool) {
	switch {
	case err == nil:
		return nil, false
	case errors.Is(err, torrents.ErrTorrentReportInput):
		return decideTorrentReportCaseBadRequest(ctx), true
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_report_review_denied", "无法处理种子举报", "当前后台身份没有 torrent.report.review 权限。")
		return generated.DecideManagedTorrentReportCase403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentReportSelfReview):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_report_self_review_denied", "不能处理存在利益冲突的案件", "上传者或本案举报人必须交由另一名管理员处理。")
		return generated.DecideManagedTorrentReportCase403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentReportCaseNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_report_case_not_found", "举报案件不存在", "该案件不存在或已经不在当前数据集中。")
		return generated.DecideManagedTorrentReportCase404ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentReportDecisionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_report_decision_idempotency_conflict", "处置请求编号已经使用", "请勿复用其他案件的 Idempotency-Key。")
		return generated.DecideManagedTorrentReportCase409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentReportCaseStateConflict),
		errors.Is(err, torrents.ErrTorrentReportCaseVersionConflict),
		errors.Is(err, torrents.ErrTorrentReportStateConflict),
		errors.Is(err, torrents.ErrTorrentReportVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_report_case_changed", "案件或种子状态已经变化", "请刷新举报队列并核对最新状态后再处理。")
		return generated.DecideManagedTorrentReportCase409ApplicationProblemPlusJSONResponse(problem), true
	default:
		return nil, false
	}
}

func createTorrentReportBadRequest(ctx context.Context) generated.CreateTorrentReport400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_report", "举报内容无效", "请选择有效原因；选择“其他”时需要填写至少 10 个字符，详细说明最多 1000 个字符。")
	return generated.CreateTorrentReport400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func decideTorrentReportCaseBadRequest(ctx context.Context) generated.DecideManagedTorrentReportCase400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_report_decision", "举报处置无效", "请检查案件版本、种子版本、处置结果和至少 10 个字符的内部说明。")
	return generated.DecideManagedTorrentReportCase400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func torrentReportReceiptDTO(receipt torrents.TorrentReportReceipt) generated.TorrentReportReceipt {
	return generated.TorrentReportReceipt{
		Id: receipt.ID, CaseId: receipt.CaseID, TorrentId: int64(receipt.TorrentID),
		ReasonCode: generated.TorrentReportReasonCode(receipt.ReasonCode), CreatedAt: receipt.CreatedAt,
	}
}

func managedTorrentReportCasePageDTO(page torrents.ManagedTorrentReportCasePage) generated.ManagedTorrentReportCasePage {
	items := make([]generated.ManagedTorrentReportCase, 0, len(page.Items))
	for _, item := range page.Items {
		reports := make([]generated.TorrentReportAllegation, 0, len(item.Reports))
		for _, report := range item.Reports {
			reports = append(reports, generated.TorrentReportAllegation{
				ReasonCode: generated.TorrentReportReasonCode(report.ReasonCode),
				Details:    report.Details, CreatedAt: report.CreatedAt,
			})
		}
		items = append(items, generated.ManagedTorrentReportCase{
			Id: item.ID, State: generated.TorrentReportCaseState(item.State), Version: item.Version,
			TorrentId: int64(item.TorrentID), TorrentTitle: item.TorrentTitle,
			TorrentState: generated.TorrentLifecycleState(item.TorrentState), TorrentVersion: item.TorrentVersion,
			UploaderNumericId: item.UploaderNumericID, UploaderUsername: item.UploaderUsername,
			UploaderDisplayName: item.UploaderDisplayName, ReportCount: item.ReportCount,
			Reports: reports, ActivePurchaseCount: item.ActivePurchaseCount,
			OpenedAt: item.OpenedAt, LatestReportedAt: item.LatestReportedAt,
		})
	}
	return generated.ManagedTorrentReportCasePage{Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset}
}

func torrentReportDecisionResultDTO(result torrents.TorrentReportDecisionResult) generated.TorrentReportDecisionResult {
	return generated.TorrentReportDecisionResult{
		DecisionId: result.DecisionID, CaseId: result.CaseID, TorrentId: int64(result.TorrentID),
		Decision:  generated.TorrentReportDecision(result.Decision),
		CaseState: generated.TorrentReportCaseState(result.CaseState), CaseVersion: result.CaseVersion,
		TorrentState: generated.TorrentLifecycleState(result.TorrentState), TorrentVersion: result.TorrentVersion,
		DecidedAt: result.DecidedAt,
	}
}
