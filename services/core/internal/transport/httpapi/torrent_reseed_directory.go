package httpapi

import (
	"context"
	"errors"
	"net/http"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
)

func (h *Handler) ListOpenTorrentReseedRequests(ctx context.Context, request generated.ListOpenTorrentReseedRequestsRequestObject) (generated.ListOpenTorrentReseedRequestsResponseObject, error) {
	excludeNSFW := request.Params.ExcludeNsfw == nil || *request.Params.ExcludeNsfw
	page, err := h.catalog.ListOpenTorrentReseedRequests(ctx, request.Params.Limit, request.Params.Offset, excludeNSFW)
	if errors.Is(err, catalog.ErrInvalidLimit) || errors.Is(err, catalog.ErrInvalidTorrentPage) {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_query", "请求参数无效", "请检查补种专区的分页参数。")
		return generated.ListOpenTorrentReseedRequestsdefaultApplicationProblemPlusJSONResponse{
			Body: problem, StatusCode: http.StatusBadRequest,
		}, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]generated.OpenTorrentReseedRequest, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, generated.OpenTorrentReseedRequest{
			TorrentId: item.TorrentID, TorrentTitle: item.TorrentTitle, RequesterCount: item.RequesterCount,
			ReseedReward: reseedRewardOfferDTO(item.Reward),
		})
	}
	return generated.ListOpenTorrentReseedRequests200JSONResponse{
		Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset,
	}, nil
}
