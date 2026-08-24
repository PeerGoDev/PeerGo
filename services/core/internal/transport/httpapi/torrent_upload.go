package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/oapi-codegen/runtime"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

// TorrentUploadService is the only torrent write capability exposed to the
// HTTP adapter. Authentication, authorization, parsing and storage ownership
// remain inside the use case rather than being trusted to request binding.
type TorrentUploadService interface {
	Submit(context.Context, string, string, torrents.TorrentUploadInput) (torrents.TorrentUploadResult, error)
}

// SubmitTorrent binds the generated multipart contract and delegates the
// complete resumable ingestion protocol to the torrents module.
func (h *Handler) SubmitTorrent(ctx context.Context, request generated.SubmitTorrentRequestObject) (generated.SubmitTorrentResponseObject, error) {
	if request.Body == nil {
		return torrentUploadBadRequest(ctx), nil
	}
	body, cleanup, err := bindTorrentSubmissionMultipart(*request.Body)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			problem := newProblemFromContext(ctx, http.StatusRequestEntityTooLarge, "torrent_upload_too_large", "种子文件过大", "上传内容超过当前站点允许的大小。")
			return generated.SubmitTorrent413ApplicationProblemPlusJSONResponse(problem), nil
		}
		return torrentUploadBadRequest(ctx), nil
	}
	rawMetainfo, err := body.TorrentFile.Bytes()
	if err != nil || len(rawMetainfo) == 0 {
		return torrentUploadBadRequest(ctx), nil
	}
	screenshots, err := torrentUploadScreenshots(body)
	if err != nil {
		return torrentUploadBadRequest(ctx), nil
	}

	result, err := h.torrentUpload.Submit(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), torrents.TorrentUploadInput{
		ID:                  request.Params.IdempotencyKey,
		CategoryID:          body.CategoryId,
		Title:               body.Title,
		Subtitle:            optionalString(body.Subtitle),
		Description:         optionalString(body.Description),
		MediaInfo:           optionalString(body.MediaInfo),
		Anonymous:           optionalBool(body.Anonymous),
		ExternalIdentifiers: torrentUploadExternalIdentifiers(body),
		FacetSelections:     torrentUploadFacetSelections(body),
		Screenshots:         screenshots,
		RawMetainfo:         rawMetainfo,
	})
	if response, handled := torrentUploadErrorResponse(ctx, err); handled {
		return response, nil
	}
	if err != nil {
		return nil, err
	}

	return generated.SubmitTorrent201JSONResponse{
		Id:             int64(result.ID),
		InfoHashV1:     result.InfoHashV1.Hex(),
		State:          generated.TorrentSubmissionState(result.State),
		ContentName:    result.ContentName,
		TotalSizeBytes: result.TotalSizeBytes,
		FileCount:      result.FileCount,
		SubmittedAt:    result.SubmittedAt,
	}, nil
}

const maxTorrentFacetSelectionPartBytes = 16 << 10

// bindTorrentSubmissionMultipart keeps the wire representation valid for both
// OpenAPI validation and the generated Go model. Complex array values are sent
// as repeated JSON parts because multipart bracket expansion creates
// undeclared top-level fields and is rejected by kin-openapi.
func bindTorrentSubmissionMultipart(reader multipart.Reader) (generated.TorrentSubmissionRequest, func(), error) {
	form, err := reader.ReadForm(32 << 20)
	if err != nil {
		return generated.TorrentSubmissionRequest{}, nil, err
	}
	cleanup := func() { _ = form.RemoveAll() }

	var body generated.TorrentSubmissionRequest
	if err := runtime.BindForm(&body, form.Value, form.File, nil); err != nil {
		return generated.TorrentSubmissionRequest{}, cleanup, err
	}

	parts := form.File["facet_selections"]
	if len(parts) == 0 {
		return body, cleanup, nil
	}
	if len(parts) > 20 {
		return generated.TorrentSubmissionRequest{}, cleanup, torrents.ErrTorrentInputInvalid
	}
	selections := make([]generated.TorrentFacetSelectionInput, 0, len(parts))
	for _, part := range parts {
		file, err := part.Open()
		if err != nil {
			return generated.TorrentSubmissionRequest{}, cleanup, err
		}
		raw, readErr := io.ReadAll(io.LimitReader(file, maxTorrentFacetSelectionPartBytes+1))
		closeErr := file.Close()
		if readErr != nil {
			return generated.TorrentSubmissionRequest{}, cleanup, readErr
		}
		if closeErr != nil {
			return generated.TorrentSubmissionRequest{}, cleanup, closeErr
		}
		if len(raw) == 0 || len(raw) > maxTorrentFacetSelectionPartBytes {
			return generated.TorrentSubmissionRequest{}, cleanup, torrents.ErrTorrentInputInvalid
		}
		var selection generated.TorrentFacetSelectionInput
		if err := json.Unmarshal(raw, &selection); err != nil {
			return generated.TorrentSubmissionRequest{}, cleanup, err
		}
		selections = append(selections, selection)
	}
	body.FacetSelections = &selections
	return body, cleanup, nil
}

func torrentUploadScreenshots(body generated.TorrentSubmissionRequest) ([]torrents.TorrentScreenshotInput, error) {
	if body.Screenshots == nil {
		return nil, nil
	}
	if len(*body.Screenshots) > torrents.MaxTorrentScreenshots {
		return nil, torrents.ErrTorrentInputInvalid
	}
	result := make([]torrents.TorrentScreenshotInput, 0, len(*body.Screenshots))
	for _, file := range *body.Screenshots {
		raw, err := file.Bytes()
		if err != nil || len(raw) == 0 {
			return nil, torrents.ErrTorrentInputInvalid
		}
		result = append(result, torrents.TorrentScreenshotInput{Raw: raw})
	}
	return result, nil
}

func torrentUploadErrorResponse(ctx context.Context, err error) (generated.SubmitTorrentResponseObject, bool) {
	if err == nil {
		return nil, false
	}
	if validationCode, ok := torrents.ValidationCodeOf(err); ok {
		if validationCode == torrents.CodeObjectTooLarge {
			if diagnostic, exists := torrents.ValidationDiagnosticOf(err); exists && diagnostic.Field == "screenshots" {
				problem := newProblemFromContext(ctx, http.StatusRequestEntityTooLarge, "torrent_screenshot_too_large", "截图过大", "单张原始截图不能超过 2 MiB。")
				return generated.SubmitTorrent413ApplicationProblemPlusJSONResponse(problem), true
			}
			problem := newProblemFromContext(ctx, http.StatusRequestEntityTooLarge, "torrent_upload_too_large", "种子文件过大", "上传的 .torrent 文件超过当前站点允许的大小。")
			return generated.SubmitTorrent413ApplicationProblemPlusJSONResponse(problem), true
		}
		if validationCode == torrents.CodeInvalidScreenshot {
			problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_screenshot", "截图无效", "请上传可正确解码且尺寸合规的 JPEG、PNG 或 WebP 图片。")
			return generated.SubmitTorrent400ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
			}, true
		}
		return torrentUploadBadRequest(ctx), true
	}
	switch {
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "web_session_required", "需要登录", "请重新登录后提交种子。")
		return generated.SubmitTorrent401ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重新提交。")
		return generated.SubmitTorrent403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentUploadEmailUnverified):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "verified_email_required", "需要先验证邮箱", "验证当前账户的邮箱后才能提交种子。")
		return generated.SubmitTorrent403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_submit_denied", "无法提交种子", "当前账户没有 torrent.submit 权限。")
		return generated.SubmitTorrent403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentUploadCategoryUnavailable):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_category_unavailable", "分类当前不可用", "请选择一个当前启用的种子分类。")
		return generated.SubmitTorrent409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentUploadDuplicate):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_already_exists", "种子已经存在", "相同 swarm 或相同原始对象已经提交。")
		return generated.SubmitTorrent409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentUploadIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_upload_idempotency_conflict", "本次提交内容已经变化", "请勿复用其他提交的 Idempotency-Key；刷新页面后重新开始。")
		return generated.SubmitTorrent409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentUploadExpired):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_upload_expired", "本次提交已经过期", "旧的未完成提交已被回收，请刷新页面后重新开始。")
		return generated.SubmitTorrent409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentUploadStateConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_upload_state_conflict", "提交状态正在恢复", "请使用相同页面和 Idempotency-Key 稍后重试。")
		return generated.SubmitTorrent409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentUploadStorageUnavailable):
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "torrent_storage_unavailable", "种子存储暂时不可用", "请保留当前页面并使用同一 Idempotency-Key 稍后重试。")
		return generated.SubmitTorrentdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, true
	case errors.Is(err, torrents.ErrTorrentInputInvalid):
		return torrentUploadBadRequest(ctx), true
	default:
		return nil, false
	}
}

func torrentUploadBadRequest(ctx context.Context) generated.SubmitTorrent400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_submission", "种子提交内容无效", "请检查分类、标题和原始私有 v1 .torrent 文件。")
	return generated.SubmitTorrent400ApplicationProblemPlusJSONResponse{
		ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
	}
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func optionalBool(value *bool) bool {
	return value != nil && *value
}

func torrentUploadExternalIdentifiers(body generated.TorrentSubmissionRequest) []torrents.ExternalIdentifier {
	result := make([]torrents.ExternalIdentifier, 0, 3)
	for _, candidate := range []struct {
		provider string
		value    *string
	}{
		{provider: "imdb", value: body.ImdbId},
		{provider: "tmdb", value: body.TmdbId},
		{provider: "douban", value: body.DoubanId},
	} {
		if candidate.value != nil && *candidate.value != "" {
			result = append(result, torrents.ExternalIdentifier{Provider: candidate.provider, ExternalID: *candidate.value})
		}
	}
	return result
}

func torrentUploadFacetSelections(body generated.TorrentSubmissionRequest) []torrents.FacetSelection {
	if body.FacetSelections == nil {
		return nil
	}
	result := make([]torrents.FacetSelection, 0, len(*body.FacetSelections))
	for _, selection := range *body.FacetSelections {
		result = append(result, torrents.FacetSelection{
			FacetID: selection.FacetId, OptionKeys: append([]string(nil), selection.OptionKeys...),
		})
	}
	return result
}
