package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/economy/medals"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type artworkTestRepository struct{ medals.Repository }

type artworkTestStore struct {
	objectstorage.Store
	key  objectstorage.Key
	data []byte
}

func (store *artworkTestStore) PutIfAbsent(_ context.Context, key objectstorage.Key, input io.Reader, _ objectstorage.Descriptor) (objectstorage.WriteResult, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return objectstorage.WriteResult{}, err
	}
	store.key, store.data = key, data
	return objectstorage.WriteResult{Created: true}, nil
}

func (store *artworkTestStore) Open(_ context.Context, key objectstorage.Key, _ string) (objectstorage.Reader, error) {
	if key != store.key {
		return objectstorage.Reader{}, objectstorage.ErrNotFound
	}
	return objectstorage.Reader{Body: io.NopCloser(bytes.NewReader(store.data)), ByteLength: int64(len(store.data))}, nil
}

func TestMedalArtworkUsesStaffUploadAndDedicatedStorage(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	staffID := uuid.New()
	csrf := strings.Repeat("s", 43)
	staff := &recordingStaffIdentityService{currentResult: identity.StaffSession{
		User: identity.User{ID: staffID, Username: "operator"}, CSRFToken: csrf,
	}}
	authorizer := &recordingAuthorizationService{decision: authz.Decision{
		ID: uuid.New(), Allow: true, GrantID: uuid.New(), GrantVersion: 1,
		MandateID: uuid.New(), RoleID: "site_admin", EffectiveUntil: now.Add(time.Hour),
	}}
	service, err := medals.NewService(artworkTestRepository{}, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	store := &artworkTestStore{}
	service.SetArtworkStore(store)
	handler := testHandlerWithEveryServiceAndUsers(
		t, unavailableIdentityService{}, staff, unavailableStaffEnrollmentService{},
		authorizer, unavailableGrantAdministrationService{}, unavailableCategoryAdministrationService{},
		unavailableSiteDisplaySettingsService{}, unavailableUserAdministrationService{}, service,
	)

	var raw bytes.Buffer
	if err := png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatal(err)
	}
	upload := func(signedIn bool, data []byte) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, medalArtworkUploadPath+"?mode=update", bytes.NewReader(data))
		request.Header.Set("Content-Type", "application/octet-stream")
		request.Header.Set("Origin", "http://peergo.test")
		request.Header.Set("X-CSRF-Token", csrf)
		if signedIn {
			request.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	if response := upload(false, raw.Bytes()); response.Code != http.StatusUnauthorized || store.key != "" {
		t.Fatalf("unauthorized upload status=%d store=%q", response.Code, store.key)
	}
	if response := upload(true, []byte("<svg></svg>")); response.Code != http.StatusBadRequest || store.key != "" {
		t.Fatalf("invalid image status=%d store=%q", response.Code, store.key)
	}
	if response := upload(true, make([]byte, medals.MaxArtworkBytes+1)); response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize status=%d", response.Code)
	}
	response := upload(true, raw.Bytes())
	var result generated.UploadMedalArtwork201JSONResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusCreated {
		t.Fatalf("upload status=%d response=%s error=%v", response.Code, response.Body.String(), err)
	}
	const prefix = "/api/v1/medal-artwork/"
	if !strings.HasPrefix(result.Path, prefix) || store.key != objectstorage.Key("medals/"+strings.TrimPrefix(result.Path, prefix)) ||
		staff.writeCSRF != csrf || authorizer.authorizeInput.Action != authz.ActionEconomyMedalUpdate {
		t.Fatalf("upload path=%q storage key=%q action=%q", result.Path, store.key, authorizer.authorizeInput.Action)
	}
	readResponse := httptest.NewRecorder()
	handler.ServeHTTP(readResponse, httptest.NewRequest(http.MethodGet, result.Path, nil))
	if readResponse.Code != http.StatusOK || readResponse.Header().Get("Content-Type") != "image/png" ||
		readResponse.Header().Get("X-Content-Type-Options") != "nosniff" ||
		!bytes.Equal(readResponse.Body.Bytes(), raw.Bytes()) {
		t.Fatalf("read status=%d headers=%v bytes=%d", readResponse.Code, readResponse.Header(), readResponse.Body.Len())
	}
}
