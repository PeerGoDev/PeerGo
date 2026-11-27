package httpserver

import (
	"context"
	"math"
	"net/http"
	"net/url"
	"time"

	"github.com/peergo/peergo/contracts/go/trackeroperationsv1"
)

type userSeedingSwarms interface {
	SeedingSwarmsByUser(context.Context, []string, time.Time) (map[string][][20]byte, error)
}

func (handler *Handler) serveOperationsSeedingTotals(response http.ResponseWriter, request *http.Request) {
	operations := handler.config.Operations
	engine, supported := handler.swarms.(userSeedingSwarms)
	if operations == nil || !supported || request.Method != http.MethodGet || request.ContentLength > 0 || len(request.TransferEncoding) != 0 {
		http.NotFound(response, request)
		return
	}
	if !operationsAuthorized(request, operations.ServiceToken) {
		handler.writeJSON(response, http.StatusForbidden, map[string]string{"code": "service_auth_failed"})
		return
	}
	query, err := url.ParseQuery(request.URL.RawQuery)
	userIDs := query["user_id"]
	if err != nil || len(query) != 1 || len(userIDs) == 0 || len(userIDs) > trackeroperationsv1.MaxSeedingTotalsUsers {
		handler.writeJSON(response, http.StatusBadRequest, map[string]string{"code": "invalid_seeding_totals_query"})
		return
	}
	totals := make(trackeroperationsv1.UserSeedingTotals, len(userIDs))
	for _, userID := range userIDs {
		if _, duplicate := totals[userID]; duplicate || !validOperationsUserID(userID) {
			handler.writeJSON(response, http.StatusBadRequest, map[string]string{"code": "invalid_seeding_totals_query"})
			return
		}
		totals[userID] = trackeroperationsv1.UserSeedingTotal{}
	}
	now := handler.now().UTC().Round(0)
	if err := handler.torrents.Ready(now, handler.config.TorrentSnapshotMaxAge); err != nil {
		handler.writeJSON(response, http.StatusServiceUnavailable, map[string]string{"code": "seeding_totals_unavailable"})
		return
	}
	matches, err := engine.SeedingSwarmsByUser(request.Context(), userIDs, now)
	if err != nil {
		handler.writeJSON(response, http.StatusServiceUnavailable, map[string]string{"code": "seeding_totals_unavailable"})
		return
	}
	for userID, hashes := range matches {
		var total trackeroperationsv1.UserSeedingTotal
		for _, hash := range hashes {
			admission, found := handler.torrents.LookupAdmission(hash)
			size := admission.Torrent.TotalSizeBytes
			if !found || size < 1 {
				continue
			}
			total.Count++
			total.Bytes += min(size, math.MaxInt64-total.Bytes)
		}
		totals[userID] = total
	}
	response.Header().Set("Cache-Control", "no-store")
	handler.writeJSON(response, http.StatusOK, totals)
}
