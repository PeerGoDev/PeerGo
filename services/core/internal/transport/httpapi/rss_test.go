package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/economy/torrentpurchase"
	"github.com/peergo/peergo/services/core/internal/modules/rss"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

type rssErrorService struct{ downloadError error }

func (rssErrorService) List(context.Context, string) ([]rss.Subscription, error) {
	return nil, nil
}
func (rssErrorService) Create(context.Context, string, string, rss.SubscriptionInput) (rss.IssuedSubscription, error) {
	return rss.IssuedSubscription{}, nil
}
func (rssErrorService) Update(context.Context, string, string, rss.UpdateSubscriptionInput) (rss.Subscription, error) {
	return rss.Subscription{}, nil
}
func (rssErrorService) Rotate(context.Context, string, string, rss.SubscriptionVersionInput) (rss.IssuedSubscription, error) {
	return rss.IssuedSubscription{}, nil
}
func (rssErrorService) Revoke(context.Context, string, string, rss.SubscriptionVersionInput) error {
	return nil
}
func (rssErrorService) Feed(context.Context, string) (rss.FeedDocument, error) {
	return rss.FeedDocument{}, nil
}
func (service rssErrorService) Download(context.Context, string, int64) (torrents.TorrentDownloadResult, error) {
	return torrents.TorrentDownloadResult{}, service.downloadError
}
func (rssErrorService) Settings(context.Context, authz.StaffActor) (rss.Settings, error) {
	return rss.Settings{}, nil
}
func (rssErrorService) UpdateSettings(context.Context, authz.StaffActor, rss.UpdateSettingsInput) (rss.Settings, error) {
	return rss.Settings{}, nil
}

func TestDownloadTorrentFromRSSMapsMemberFacingErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want any
	}{
		{name: "purchase required", err: torrentpurchase.ErrPurchaseRequired, want: generated.DownloadTorrentFromRSS402ApplicationProblemPlusJSONResponse{}},
		{name: "download restricted", err: torrents.ErrTorrentDownloadRestricted, want: generated.DownloadTorrentFromRSS403ApplicationProblemPlusJSONResponse{}},
		{name: "torrent missing", err: torrents.ErrTorrentDownloadNotFound, want: generated.DownloadTorrentFromRSS404ApplicationProblemPlusJSONResponse{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := &Handler{rss: rssErrorService{downloadError: test.err}}
			response, err := handler.DownloadTorrentFromRSS(context.Background(), generated.DownloadTorrentFromRSSRequestObject{RssToken: "token", TorrentId: 1234})
			if err != nil {
				t.Fatalf("DownloadTorrentFromRSS() error = %v", err)
			}
			if !sameType(response, test.want) {
				t.Fatalf("response type = %T, want %T", response, test.want)
			}
		})
	}
}

func TestPrivateResponseHeadersProtectRSSFailures(t *testing.T) {
	next := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, "not found", http.StatusNotFound)
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/rss/private-token", nil)
	PrivateResponseHeaders(next).ServeHTTP(response, request)
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Referrer-Policy") != "no-referrer" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("RSS security headers = %#v", response.Header())
	}
}

func sameType(left, right any) bool {
	return reflect.TypeOf(left) == reflect.TypeOf(right)
}
