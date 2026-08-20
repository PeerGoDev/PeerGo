package trackeroperations

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
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
