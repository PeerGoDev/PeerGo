package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/peergo/peergo/contracts/go/trackeroperationsv1"
)

func TestOperationsSeedingTotalsUseTorrentSizeAndReturnOfflineZero(t *testing.T) {
	t.Parallel()
	const passkey = "00112233445566778899aabbccddeeff"
	const serviceToken = "peergo-test-tracker-service-token-2026"
	const offline = "00000000-0000-4000-8000-000000000003"
	var hash [20]byte
	copy(hash[:], "aaaaaaaaaaaaaaaaaaaa")
	handler, _ := testHandler(t, hash, passkey, true)
	handler.config.Operations = &OperationsConfig{ServiceToken: serviceToken}

	for i, peerID := range []string{"-qB4600-abcdefghijkl", "-qB4600-abcdefghijkm"} {
		request := httptest.NewRequest(http.MethodGet, "/tracker/"+passkey+"/announce?"+announceQuery("aaaaaaaaaaaaaaaaaaaa", peerID, 0), nil)
		request.RemoteAddr = []string{"192.0.2.10:50000", "192.0.2.11:50000"}[i]
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if strings.Contains(response.Body.String(), "failure reason") {
			t.Fatalf("announce failed: %s", response.Body.String())
		}
	}

	userID := trackerSubject().UserID
	endpoint := trackeroperationsv1.SeedingTotalsPath + "?user_id=" + userID + "&user_id=" + offline
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, httptest.NewRequest(http.MethodGet, endpoint, nil))
	if denied.Code != http.StatusForbidden {
		t.Fatalf("unauthenticated totals status=%d", denied.Code)
	}

	request := httptest.NewRequest(http.MethodGet, endpoint, nil)
	request.Header.Set("Authorization", "Bearer "+serviceToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var totals trackeroperationsv1.UserSeedingTotals
	if err := json.Unmarshal(response.Body.Bytes(), &totals); err != nil || response.Code != http.StatusOK ||
		!totals.Valid([]string{userID, offline}) || totals[userID].Count != 1 || totals[userID].Bytes != 1000 ||
		totals[offline].Count != 0 || totals[offline].Bytes != 0 {
		t.Fatalf("totals=%+v status=%d response=%s error=%v", totals, response.Code, response.Body.String(), err)
	}
}
