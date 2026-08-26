package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/economy/attendance"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/moviepilot"
	"github.com/peergo/peergo/services/core/internal/modules/personalapikey"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

func TestMoviePilotCompatibilityLeavesAnonymousTorrentListOnOpenAPIHandler(t *testing.T) {
	called := false
	stub := &moviePilotCompatibilityStub{}
	handler := MoviePilotCompatibility(stub, stub)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/torrents", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if !called || response.Code != http.StatusNoContent {
		t.Fatalf("anonymous request did not reach next handler: called=%v status=%d", called, response.Code)
	}
}

func TestMoviePilotCompatibilityReturnsOfficialTorrentShape(t *testing.T) {
	createdAt := time.Date(2026, 8, 26, 8, 0, 0, 0, time.UTC)
	stub := &moviePilotCompatibilityStub{
		torrentPage: moviepilot.TorrentPage{
			Items: []moviepilot.TorrentSummary{{
				ID: 9830, LegacyRouteID: "9830", Title: "Example", Subtitle: "Subtitle",
				Category: "movie", CategoryName: "电影", Size: 1024, Seeders: 7,
				Leechers: 2, Downloads: 9, CreatedAt: createdAt,
				Promotion: moviepilot.TorrentPromotion{Type: 2, Active: true, UploadFactor: 1, DownloadFactor: 0},
			}},
			Total: 1, Page: 1, PageSize: 100, TotalPages: 1,
		},
	}
	handler := MoviePilotCompatibility(stub, stub)(http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/torrents?page=1&page_size=100&category=movie", nil)
	request.Header.Set("Authorization", "Bearer pgk_test-key")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			Torrents []struct {
				ID        int64  `json:"id"`
				UUID      string `json:"uuid"`
				Promotion struct {
					Active         bool    `json:"is_active"`
					DownloadFactor float64 `json:"down_multiplier"`
				} `json:"promotion"`
			} `json:"torrents"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != 0 || len(body.Data.Torrents) != 1 || body.Data.Torrents[0].ID != 9830 || body.Data.Torrents[0].UUID != "9830" || !body.Data.Torrents[0].Promotion.Active || body.Data.Torrents[0].Promotion.DownloadFactor != 0 {
		t.Fatalf("unexpected response: %+v", body)
	}
	if stub.authenticatedToken != "pgk_test-key" {
		t.Fatalf("authenticated token = %q", stub.authenticatedToken)
	}
}

func TestMoviePilotCompatibilityFailsClosedForInvalidCredential(t *testing.T) {
	stub := &moviePilotCompatibilityStub{authenticateErr: personalapikey.ErrInvalid}
	handler := MoviePilotCompatibility(stub, stub)(http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/profile", nil)
	request.Header.Set("Authorization", "Bearer leaked-value")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("status=%d authenticate=%q", response.Code, response.Header().Get("WWW-Authenticate"))
	}
	if response.Body.String() == "" || strings.Contains(response.Body.String(), "leaked-value") {
		t.Fatalf("credential leaked in response: %s", response.Body.String())
	}
}

func TestMoviePilotCompatibilityStreamsCapabilityDownloadWithoutAPIKeyInURL(t *testing.T) {
	stub := &moviePilotCompatibilityStub{download: torrents.TorrentDownloadResult{Data: []byte("torrent-bytes"), Filename: "[Rousi] Example.torrent"}}
	handler := MoviePilotCompatibility(stub, stub)(http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodGet, "/api/compat/moviepilot/v1/torrents/9830/download?capability=signed", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Body.String() != "torrent-bytes" {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "application/x-bittorrent" || response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("unexpected headers: %v", response.Header())
	}
}

func TestPrivateResponseHeadersProtectIssuedPersonalAPIKey(t *testing.T) {
	next := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusCreated)
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/me/api-key/rotations", nil)

	PrivateResponseHeaders(next).ServeHTTP(response, request)

	if response.Header().Get("Cache-Control") != "no-store" ||
		response.Header().Get("Pragma") != "no-cache" ||
		response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("personal API key security headers = %#v", response.Header())
	}
}

type moviePilotCompatibilityStub struct {
	authenticatedToken string
	authenticateErr    error
	torrentPage        moviepilot.TorrentPage
	download           torrents.TorrentDownloadResult
}

func (stub moviePilotCompatibilityStub) Status(context.Context, string) (personalapikey.Status, error) {
	return personalapikey.Status{}, errors.New("not implemented")
}

func (stub moviePilotCompatibilityStub) Rotate(context.Context, string, string, *int64, []personalapikey.Scope) (personalapikey.IssuedCredential, error) {
	return personalapikey.IssuedCredential{}, errors.New("not implemented")
}

func (stub moviePilotCompatibilityStub) Revoke(context.Context, string, string, int64) error {
	return errors.New("not implemented")
}

func (stub *moviePilotCompatibilityStub) Authenticate(_ context.Context, raw string) (personalapikey.AuthenticatedCredential, error) {
	stub.authenticatedToken = raw
	if stub.authenticateErr != nil {
		return personalapikey.AuthenticatedCredential{}, stub.authenticateErr
	}
	return personalapikey.AuthenticatedCredential{User: identity.User{ID: uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111")}}, nil
}

func (stub moviePilotCompatibilityStub) Profile(context.Context, personalapikey.AuthenticatedCredential) (moviepilot.Profile, error) {
	return moviepilot.Profile{}, nil
}

func (stub moviePilotCompatibilityStub) ListTorrents(context.Context, personalapikey.AuthenticatedCredential, int, int, string, string) (moviepilot.TorrentPage, error) {
	return stub.torrentPage, nil
}

func (stub moviePilotCompatibilityStub) Torrent(context.Context, personalapikey.AuthenticatedCredential, int64) (moviepilot.TorrentDownloadDescriptor, error) {
	return moviepilot.TorrentDownloadDescriptor{}, errors.New("not implemented")
}

func (stub moviePilotCompatibilityStub) Download(context.Context, int64, string) (torrents.TorrentDownloadResult, error) {
	return stub.download, nil
}

func (stub moviePilotCompatibilityStub) AttendanceOverview(context.Context, personalapikey.AuthenticatedCredential) (attendance.Overview, error) {
	return attendance.Overview{}, nil
}

func (stub moviePilotCompatibilityStub) ClaimAttendance(context.Context, personalapikey.AuthenticatedCredential, attendance.Mode) (attendance.Record, error) {
	return attendance.Record{}, nil
}
