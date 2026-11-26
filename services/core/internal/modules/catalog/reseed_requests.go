package catalog

import (
	"context"
	"fmt"
	"math"

	"github.com/peergo/peergo/services/core/internal/generated/catalogdb"
	"github.com/peergo/peergo/services/core/internal/modules/economy/reseedreward"
)

type OpenTorrentReseedRequest struct {
	TorrentID      int64
	TorrentTitle   string
	RequesterCount int64
	Reward         *reseedreward.Offer
}

type OpenTorrentReseedRequestPage struct {
	Items  []OpenTorrentReseedRequest
	Total  int
	Limit  int
	Offset int
}

func (s *Service) ListOpenTorrentReseedRequests(ctx context.Context, limit, offset *int, excludeNSFW bool) (OpenTorrentReseedRequestPage, error) {
	pageLimit, pageOffset, err := torrentPageBounds(limit, offset)
	if err != nil {
		return OpenTorrentReseedRequestPage{}, err
	}
	items, total, err := s.repository.ListOpenTorrentReseedRequests(ctx, pageLimit, pageOffset, excludeNSFW)
	if err != nil {
		return OpenTorrentReseedRequestPage{}, err
	}
	return OpenTorrentReseedRequestPage{Items: items, Total: total, Limit: pageLimit, Offset: pageOffset}, nil
}

func (r *PostgresRepository) ListOpenTorrentReseedRequests(ctx context.Context, limit, offset int, excludeNSFW bool) ([]OpenTorrentReseedRequest, int, error) {
	rows, err := r.queries.ListOpenTorrentReseedRequests(ctx, catalogdb.ListOpenTorrentReseedRequestsParams{
		PageLimit: int32(limit), PageOffset: int32(offset), ExcludeNsfw: excludeNSFW,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list open torrent reseed requests: %w", err)
	}
	if len(rows) == 0 || rows[0].Total < 0 || rows[0].Total > math.MaxInt {
		return nil, 0, errCatalogProjectionInvalid
	}
	items := make([]OpenTorrentReseedRequest, 0, len(rows))
	for _, row := range rows {
		// The count row survives even when the requested offset is beyond the end.
		if row.TorrentID == 0 {
			continue
		}
		if row.TorrentID < 0 || row.TorrentTitle == "" || row.RequesterCount < 1 {
			return nil, 0, errCatalogProjectionInvalid
		}
		item := OpenTorrentReseedRequest{TorrentID: row.TorrentID, TorrentTitle: row.TorrentTitle, RequesterCount: row.RequesterCount}
		if row.RewardRequiredHours > 0 {
			item.Reward = reseedreward.OfferFromProjection(reseedreward.OfferUpcoming, row.RewardRequiredHours, row.RewardMagicAmount)
		}
		items = append(items, item)
	}
	return items, int(rows[0].Total), nil
}

func (r *MemoryRepository) ListOpenTorrentReseedRequests(context.Context, int, int, bool) ([]OpenTorrentReseedRequest, int, error) {
	return []OpenTorrentReseedRequest{}, 0, nil
}
