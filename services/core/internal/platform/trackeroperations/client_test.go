package trackeroperations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/peergo/peergo/contracts/go/trackeroperationsv1"
	"github.com/peergo/peergo/contracts/go/trackerruntimepolicyv1"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestClientReadsAuthenticatedRuntime(t *testing.T) {
	const token = "peergo-test-tracker-service-token-2026"
	client, err := NewClient("https://tracker.example", token, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/internal/v1/operations/runtime" || request.Header.Get("Authorization") != "Bearer "+token {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"generated_at":"2026-08-16T00:00:00Z","policy_generated_at":"2026-08-16T00:00:00Z","policy_control_sequence":1,"policy_revision":"tracker-runtime-default-v1","announce_interval_seconds":1800,"min_announce_interval_seconds":900,"default_numwant":50,"max_numwant":100,"scrape_enabled":true,"max_scrape_hashes":50,"client_mode":"allow_all","allowed_clients":[],"user_requests_per_minute":30,"user_burst":60,"address_requests_per_minute":120,"address_burst":240,"peer_ttl_seconds":2100,"max_swarms":100000,"max_peers":1000000,"max_peers_per_swarm":100000}`)),
			Request:    request,
		}, nil
	})
	runtime, err := client.Runtime(context.Background())
	if err != nil || runtime.AnnounceIntervalSeconds != 1800 {
		t.Fatalf("runtime = %+v, err = %v", runtime, err)
	}
}

func TestClientReadsAuthenticatedActivePeers(t *testing.T) {
	const token = "peergo-test-tracker-service-token-2026"
	const infoHash = "00112233445566778899aabbccddeeff00112233"
	client, err := NewClient("https://tracker.example", token, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/internal/v1/operations/swarms/"+infoHash+"/peers" || request.URL.Query().Get("limit") != "25" ||
			request.Header.Get("Authorization") != "Bearer "+token {
			t.Fatalf("unexpected request: %s", request.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"generated_at":"2026-08-22T12:00:00Z","items":[{"user_id":"0198f20a-6da8-7e51-9c64-111111111111","client_family":"qbittorrent","address_family":6,"seedbox":true,"uploaded":10,"downloaded":20,"upload_speed":1,"download_speed":2,"left":30,"last_announce":"2026-08-22T11:59:00Z"}],"truncated":false}`)),
			Request:    request,
		}, nil
	})
	page, err := client.ActivePeers(context.Background(), infoHash, 25)
	if err != nil || len(page.Items) != 1 || page.Items[0].ClientFamily != "qbittorrent" ||
		page.Items[0].AddressFamily != 6 || !page.Items[0].Seedbox || page.Items[0].DownloadSpeed != 2 {
		t.Fatalf("ActivePeers() = %+v, err = %v", page, err)
	}
}

func TestClientReadsRuntimeWithMaximumSeedboxRegistry(t *testing.T) {
	runtime := validRuntime()
	runtime.Seedbox = trackerruntimepolicyv1.SeedboxPolicy{
		Enabled: true, UploadFactorBasisPoints: 5_000, DownloadFactorBasisPoints: 20_000,
		Rules: make([]trackerruntimepolicyv1.SeedboxRule, 0, trackerruntimepolicyv1.MaxSeedboxRules),
	}
	for index := 0; index < trackerruntimepolicyv1.MaxSeedboxRules; index++ {
		runtime.Seedbox.Rules = append(runtime.Seedbox.Rules, trackerruntimepolicyv1.SeedboxRule{
			ID: fmt.Sprintf("seedbox-%04d", index), CIDR: fmt.Sprintf("2001:db8:%x::/48", index),
			UserNumericID: int64(index + 1),
		})
	}
	payload, err := json.Marshal(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) <= 4<<10 || len(payload) > responseLimit {
		t.Fatalf("runtime payload bytes = %d, want within (%d, %d]", len(payload), 4<<10, responseLimit)
	}

	client := testClient(t, payload)
	loaded, err := client.Runtime(context.Background())
	if err != nil || len(loaded.Seedbox.Rules) != trackerruntimepolicyv1.MaxSeedboxRules {
		t.Fatalf("runtime rules = %d, err = %v", len(loaded.Seedbox.Rules), err)
	}
}

func TestClientRejectsOversizedRuntimeResponse(t *testing.T) {
	client := testClient(t, []byte(strings.Repeat(" ", responseLimit+1)))
	if _, err := client.Runtime(context.Background()); err == nil {
		t.Fatal("Runtime() accepted an oversized response")
	}
}

func testClient(t *testing.T, payload []byte) *Client {
	t.Helper()
	const token = "peergo-test-tracker-service-token-2026"
	client, err := NewClient("https://tracker.example", token, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(payload))), Request: request,
		}, nil
	})
	return client
}

func validRuntime() trackeroperationsv1.Runtime {
	generatedAt := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	return trackeroperationsv1.Runtime{
		GeneratedAt: generatedAt, PolicyGeneratedAt: generatedAt, PolicyControlSequence: 2,
		PolicyRevision: "tracker-runtime-test-v2", AnnounceIntervalSeconds: 1_800,
		MinAnnounceIntervalSeconds: 900, DefaultNumWant: 50, MaxNumWant: 100,
		MaxScrapeHashes: 50, ClientMode: string(trackerruntimepolicyv1.ClientModeAllowAll),
		AllowedClients: []trackerruntimepolicyv1.ClientRule{}, UserRequestsPerMinute: 600, UserBurst: 1_200,
		AddressRequestsPerMinute: 5_000, AddressBurst: 10_000, PeerTTLSeconds: 2_100,
		MaxSwarms: 100_000, MaxPeers: 1_000_000, MaxPeersPerSwarm: 100_000,
	}
}
