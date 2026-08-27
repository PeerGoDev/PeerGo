package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/catalog"
	"github.com/peergo/peergo/services/core/internal/modules/economy/attendance"
	"github.com/peergo/peergo/services/core/internal/modules/economy/torrentpurchase"
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

func TestPTDepilerCompatibilityUsesOfficialSearchAlias(t *testing.T) {
	createdAt := time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC)
	stub := &moviePilotCompatibilityStub{
		torrentPage: moviepilot.TorrentPage{
			Items: []moviepilot.TorrentSummary{{
				ID: 9830, LegacyRouteID: "9830", Title: "Example", Subtitle: "Subtitle",
				Category: "movie", CategoryName: "电影", Size: 1024, Seeders: 7,
				Leechers: 2, Downloads: 9, CreatedAt: createdAt,
			}},
			Total: 1, Page: 1, PageSize: 100, TotalPages: 1,
		},
	}
	handler := MoviePilotCompatibility(stub, stub)(http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/search?page_size=100&keyword=Example&category=movie", nil)
	request.Header.Set("Authorization", "Bearer pgk_pt-depiler")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			Torrents []struct {
				UUID string `json:"uuid"`
			} `json:"torrents"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != 0 || len(body.Data.Torrents) != 1 || body.Data.Torrents[0].UUID != "9830" {
		t.Fatalf("unexpected PT-depiler search response: %+v", body)
	}
	if stub.listPage != 1 || stub.listPageSize != 100 || stub.listKeyword != "Example" || stub.listCategory != "movie" {
		t.Fatalf("search input = page %d size %d keyword %q category %q", stub.listPage, stub.listPageSize, stub.listKeyword, stub.listCategory)
	}
}

func TestPTDepilerCompatibilityReturnsLatestSettledSeedingReward(t *testing.T) {
	stub := &moviePilotCompatibilityStub{seedingReward: 42}
	handler := MoviePilotCompatibility(stub, stub)(http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/seeding-reward", nil)
	request.Header.Set("Authorization", "Bearer pgk_pt-depiler")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"total_reward":42`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
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

func TestPTDepilerCompatibilityStreamsFixedLegacyDownloadRoute(t *testing.T) {
	const rawCredential = "pgk_pt-depiler"
	stub := &moviePilotCompatibilityStub{download: torrents.TorrentDownloadResult{Data: []byte("torrent-bytes"), Filename: "[Rousi] Example.torrent"}}
	handler := MoviePilotCompatibility(stub, stub)(http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodGet, "/api/torrent/9830/download/"+rawCredential, nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Body.String() != "torrent-bytes" {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	if stub.authenticatedToken != rawCredential || stub.downloadTorrentID != 9830 {
		t.Fatalf("credential=%q torrent=%d", stub.authenticatedToken, stub.downloadTorrentID)
	}
	if strings.Contains(response.Body.String(), rawCredential) {
		t.Fatal("PT-depiler credential leaked in response")
	}
	if response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("unexpected private headers: %v", response.Header())
	}
}

func TestPTDepilerCompatibilityRejectsConflictingHeaderCredential(t *testing.T) {
	stub := &moviePilotCompatibilityStub{}
	handler := MoviePilotCompatibility(stub, stub)(http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodGet, "/api/torrent/9830/download/pgk_path-key", nil)
	request.Header.Set("Authorization", "Bearer pgk_other-key")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized || stub.authenticatedToken != "" || strings.Contains(response.Body.String(), "pgk_") {
		t.Fatalf("status=%d authenticated=%q body=%s", response.Code, stub.authenticatedToken, response.Body.String())
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

func TestLegacyCategoriesExposeManagedCategoryFacets(t *testing.T) {
	stub := &legacyCompatibilityStub{
		moviePilotCompatibilityStub: &moviePilotCompatibilityStub{},
		categories: []moviepilot.LegacyCategory{{
			ID: 1, Name: "movie", Label: "电影", Icon: "film",
			Attributes: []moviepilot.LegacyCategoryAttribute{{
				Name: "source", Label: "来源", Type: "select", Required: true,
				Options: []moviepilot.LegacyCategoryOption{{Value: "bluray", Label: "Blu-ray"}},
			}},
		}},
	}
	handler := MoviePilotCompatibility(stub, stub)(http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/categories", nil)
	request.Header.Set("Authorization", "Bearer pgk_categories")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"source"`) ||
		!strings.Contains(response.Body.String(), `"value":"bluray"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLegacyUploadDecodesPtYesJSONWithoutPersistingTags(t *testing.T) {
	stub := &legacyCompatibilityStub{
		moviePilotCompatibilityStub: &moviePilotCompatibilityStub{},
		uploadResult: moviepilot.LegacyUploadResult{
			ID: 42, RouteID: "42", InfoHash: strings.Repeat("a", 40), Status: "pending",
		},
	}
	handler := MoviePilotCompatibility(stub, stub)(http.NotFoundHandler())
	body := `{"torrent":"` + base64.StdEncoding.EncodeToString([]byte("d4:infode")) + `","title":"Example","description":"Description","category":"movie","attributes":{"source":"Blu-ray","genre":["动作","科幻"]},"tags":"国语,中字","price":12}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/torrents", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer pgk_upload")
	request.Header.Set("Idempotency-Key", "550e8400-e29b-41d4-a716-446655440000")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if stub.uploadInput.RequestID.String() != "550e8400-e29b-41d4-a716-446655440000" || stub.uploadInput.PurchasePrice != 12 ||
		string(stub.uploadInput.RawMetainfo) != "d4:infode" || stub.uploadInput.Attributes["source"][0] != "Blu-ray" ||
		len(stub.uploadInput.Attributes["genre"]) != 2 {
		t.Fatalf("upload input = %+v", stub.uploadInput)
	}
}

func TestLegacyPaidTorrentDetailFailsClosedForProtectedFields(t *testing.T) {
	var infoHash torrents.InfoHashV1
	copy(infoHash[:], strings.Repeat("a", 20))
	stub := &legacyCompatibilityStub{
		moviePilotCompatibilityStub: &moviePilotCompatibilityStub{},
		torrentDetail: moviepilot.LegacyTorrentDetail{
			RouteID: "42",
			Detail: torrents.PublicDetail{
				ID: 42, Category: catalog.Category{ID: "movies", Name: "电影"},
				Title: "Paid", InfoHashV1: infoHash, TotalSizeBytes: 1024,
			},
			Metadata:    moviepilot.TorrentMetadata{TorrentID: 42, PurchasePrice: 9},
			Files:       []torrents.PublicFile{{Index: 0, DisplayPath: "secret.mkv", SizeBytes: 1024}},
			DownloadURL: "https://should-not-leak.test/torrent", CanReadObject: false,
			Purchase: torrentpurchase.Status{TorrentID: 42, Price: 9, State: torrentpurchase.AccessPurchaseRequired},
		},
	}
	handler := MoviePilotCompatibility(stub, stub)(http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/torrents/42", nil)
	request.Header.Set("Authorization", "Bearer pgk_read")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "secret.mkv") ||
		strings.Contains(response.Body.String(), "should-not-leak") || !strings.Contains(response.Body.String(), `"info_hash":""`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLegacyPurchaseRequiresIdempotencyAndForwardsExpectedPrice(t *testing.T) {
	stub := &legacyCompatibilityStub{moviePilotCompatibilityStub: &moviePilotCompatibilityStub{}}
	handler := MoviePilotCompatibility(stub, stub)(http.NotFoundHandler())
	missing := httptest.NewRequest(http.MethodPost, "/api/v1/torrents/42/purchase", strings.NewReader(`{"expected_price":9}`))
	missing.Header.Set("Authorization", "Bearer pgk_purchase")
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missing)
	if missingResponse.Code != http.StatusBadRequest {
		t.Fatalf("missing idempotency status=%d body=%s", missingResponse.Code, missingResponse.Body.String())
	}

	requestID := uuid.New()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/torrents/42/purchase", strings.NewReader(`{"expected_price":9}`))
	request.Header.Set("Authorization", "Bearer pgk_purchase")
	request.Header.Set("Idempotency-Key", requestID.String())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || stub.purchaseRequestID != requestID || stub.expectedPrice == nil || *stub.expectedPrice != 9 {
		t.Fatalf("status=%d request=%s expected=%v body=%s", response.Code, stub.purchaseRequestID, stub.expectedPrice, response.Body.String())
	}
}

type moviePilotCompatibilityStub struct {
	authenticatedToken string
	authenticateErr    error
	torrentPage        moviepilot.TorrentPage
	download           torrents.TorrentDownloadResult
	seedingReward      int64
	listPage           int
	listPageSize       int
	listKeyword        string
	listCategory       string
	downloadTorrentID  int64
}

type legacyCompatibilityStub struct {
	*moviePilotCompatibilityStub
	categories        []moviepilot.LegacyCategory
	uploadInput       moviepilot.LegacyUploadInput
	uploadResult      moviepilot.LegacyUploadResult
	torrentDetail     moviepilot.LegacyTorrentDetail
	purchaseRequestID uuid.UUID
	expectedPrice     *int64
}

func (stub *legacyCompatibilityStub) PublicProfile(context.Context, personalapikey.AuthenticatedCredential, string) (moviepilot.Profile, error) {
	return moviepilot.Profile{}, nil
}

func (stub *legacyCompatibilityStub) Categories(context.Context, personalapikey.AuthenticatedCredential) ([]moviepilot.LegacyCategory, error) {
	return stub.categories, nil
}

func (stub *legacyCompatibilityStub) Upload(_ context.Context, _ personalapikey.AuthenticatedCredential, input moviepilot.LegacyUploadInput) (moviepilot.LegacyUploadResult, error) {
	stub.uploadInput = input
	return stub.uploadResult, nil
}

func (stub *legacyCompatibilityStub) LegacyTorrent(context.Context, personalapikey.AuthenticatedCredential, string) (moviepilot.LegacyTorrentDetail, error) {
	return stub.torrentDetail, nil
}

func (stub *legacyCompatibilityStub) Comments(context.Context, personalapikey.AuthenticatedCredential, string, int, int) (moviepilot.LegacyCommentPage, error) {
	return moviepilot.LegacyCommentPage{}, nil
}

func (stub *legacyCompatibilityStub) Bookmarks(context.Context, personalapikey.AuthenticatedCredential, int, int) (moviepilot.LegacyBookmarkPage, error) {
	return moviepilot.LegacyBookmarkPage{}, nil
}

func (stub *legacyCompatibilityStub) PurchaseStatus(context.Context, personalapikey.AuthenticatedCredential, string) (torrentpurchase.Status, error) {
	return torrentpurchase.Status{}, nil
}

func (stub *legacyCompatibilityStub) Purchase(_ context.Context, _ personalapikey.AuthenticatedCredential, _ string, requestID uuid.UUID, expectedPrice *int64) (torrentpurchase.Receipt, error) {
	stub.purchaseRequestID = requestID
	stub.expectedPrice = expectedPrice
	return torrentpurchase.Receipt{RequestID: requestID, TorrentID: 42, Price: 9}, nil
}

func (stub *legacyCompatibilityStub) Purchases(context.Context, personalapikey.AuthenticatedCredential, int, int) (torrentpurchase.HistoryPage, error) {
	return torrentpurchase.HistoryPage{}, nil
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

func (stub moviePilotCompatibilityStub) SeedingReward(context.Context, personalapikey.AuthenticatedCredential) (int64, error) {
	return stub.seedingReward, nil
}

func (stub *moviePilotCompatibilityStub) ListTorrents(_ context.Context, _ personalapikey.AuthenticatedCredential, page, pageSize int, keyword, category string) (moviepilot.TorrentPage, error) {
	stub.listPage, stub.listPageSize = page, pageSize
	stub.listKeyword, stub.listCategory = keyword, category
	return stub.torrentPage, nil
}

func (stub moviePilotCompatibilityStub) Torrent(context.Context, personalapikey.AuthenticatedCredential, int64) (moviepilot.TorrentDownloadDescriptor, error) {
	return moviepilot.TorrentDownloadDescriptor{}, errors.New("not implemented")
}

func (stub moviePilotCompatibilityStub) Download(context.Context, int64, string) (torrents.TorrentDownloadResult, error) {
	return stub.download, nil
}

func (stub *moviePilotCompatibilityStub) DownloadWithCredential(_ context.Context, _ personalapikey.AuthenticatedCredential, torrentID int64) (torrents.TorrentDownloadResult, error) {
	stub.downloadTorrentID = torrentID
	return stub.download, nil
}

func (stub moviePilotCompatibilityStub) AttendanceOverview(context.Context, personalapikey.AuthenticatedCredential) (attendance.Overview, error) {
	return attendance.Overview{}, nil
}

func (stub moviePilotCompatibilityStub) ClaimAttendance(context.Context, personalapikey.AuthenticatedCredential, attendance.Mode) (attendance.Record, error) {
	return attendance.Record{}, nil
}
