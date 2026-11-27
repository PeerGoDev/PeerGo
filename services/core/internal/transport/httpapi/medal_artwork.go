package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/economy/medals"
)

type medalArtworkService interface {
	UploadArtwork(context.Context, authz.StaffActor, bool, io.Reader) (string, error)
	ReadArtwork(context.Context, string) (medals.Artwork, error)
}

func (h *Handler) UploadMedalArtwork(ctx context.Context, request generated.UploadMedalArtworkRequestObject) (generated.UploadMedalArtworkResponseObject, error) {
	session, problem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.UploadMedalArtwork401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.UploadMedalArtwork403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	service, ok := h.medalAdministration.(medalArtworkService)
	if !ok {
		return nil, medals.ErrArtworkUnavailable
	}
	filename, err := service.UploadArtwork(ctx, staffActor(session), request.Params.Mode == "update", request.Body)
	switch {
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "medal_artwork_denied", "没有权限上传勋章图片", "请使用有对应勋章新增或编辑权限的后台身份。")
		return generated.UploadMedalArtwork403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, medals.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_medal_artwork", "勋章图片无效", "请上传宽、高均不超过 2048 像素的 PNG、JPG、GIF 或 WebP 图片。")
		return generated.UploadMedalArtwork400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, medals.ErrArtworkTooLarge):
		problem := newProblemFromContext(ctx, http.StatusRequestEntityTooLarge, "medal_artwork_too_large", "勋章图片过大", "每张图片不能超过 2 MB。")
		return generated.UploadMedalArtwork413ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.UploadMedalArtwork201JSONResponse{Path: "/api/v1/medal-artwork/" + filename}, nil
}

func (h *Handler) GetMedalArtwork(ctx context.Context, request generated.GetMedalArtworkRequestObject) (generated.GetMedalArtworkResponseObject, error) {
	service, ok := h.medalAdministration.(medalArtworkService)
	if !ok {
		return nil, medals.ErrArtworkUnavailable
	}
	artwork, err := service.ReadArtwork(ctx, request.Filename)
	if errors.Is(err, medals.ErrNotFound) {
		problem := newProblemFromContext(ctx, http.StatusNotFound, "medal_artwork_not_found", "勋章图片不存在", "请检查图片地址。")
		return generated.GetMedalArtwork404ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	cacheControl := "public, max-age=31536000, immutable"
	etag := `"` + artwork.Digest + `"`
	nosniff := "nosniff"
	headers := generated.GetMedalArtwork200ResponseHeaders{CacheControl: &cacheControl, ETag: &etag, XContentTypeOptions: &nosniff}
	body := bytes.NewReader(artwork.Data)
	length := int64(len(artwork.Data))
	switch artwork.ContentType {
	case "image/gif":
		return generated.GetMedalArtwork200ImagegifResponse{Body: body, Headers: headers, ContentLength: length}, nil
	case "image/jpeg":
		return generated.GetMedalArtwork200ImagejpegResponse{Body: body, Headers: headers, ContentLength: length}, nil
	case "image/png":
		return generated.GetMedalArtwork200ImagepngResponse{Body: body, Headers: headers, ContentLength: length}, nil
	case "image/webp":
		return generated.GetMedalArtwork200ImagewebpResponse{Body: body, Headers: headers, ContentLength: length}, nil
	default:
		return nil, medals.ErrInput
	}
}
