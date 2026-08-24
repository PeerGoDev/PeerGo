// Package trackeroperations implements Core's narrow, read-only adapter to the
// running Tracker process. It cannot announce, mutate swarms or read secrets.
package trackeroperations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/contracts/go/trackeroperationsv1"
)

// The signed Tracker runtime policy may contain up to 4,096 reviewed seedbox
// rules. Keep this response bounded, but large enough to represent the full
// cross-service contract instead of truncating a legitimate migrated registry.
const responseLimit = 1 << 20

type Client struct {
	endpoint     string
	origin       string
	serviceToken string
	httpClient   *http.Client
}

func NewClient(origin, serviceToken string, timeout time.Duration) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, errors.New("Tracker operations origin must be an absolute origin")
	}
	if len(serviceToken) < 32 {
		return nil, errors.New("Tracker operations service token must contain at least 32 bytes")
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	baseOrigin := strings.TrimSuffix(parsed.String(), "/")
	parsed.Path = "/internal/v1/operations/runtime"
	return &Client{
		endpoint: parsed.String(), origin: baseOrigin, serviceToken: serviceToken,
		httpClient: &http.Client{
			Timeout:       timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// ActivePeers reads a bounded live-swarm view from Tracker memory. The
// response contains no endpoint, peer ID, passkey or durable session token.
func (client *Client) ActivePeers(ctx context.Context, infoHashV1 string, limit int) (trackeroperationsv1.ActivePeerPage, error) {
	if _, err := trackerannouncev1.DecodeInfoHashV1(infoHashV1); err != nil || limit < 1 || limit > trackeroperationsv1.MaxActivePeerLimit {
		return trackeroperationsv1.ActivePeerPage{}, errors.New("Tracker active peer query is invalid")
	}
	endpoint := fmt.Sprintf("%s/internal/v1/operations/swarms/%s/peers?limit=%d", client.origin, infoHashV1, limit)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return trackeroperationsv1.ActivePeerPage{}, fmt.Errorf("create Tracker active peer request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+client.serviceToken)
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return trackeroperationsv1.ActivePeerPage{}, fmt.Errorf("read Tracker active peers: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, responseLimit))
		return trackeroperationsv1.ActivePeerPage{}, fmt.Errorf("Tracker active peers returned status %d", response.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if err != nil || len(payload) > responseLimit {
		return trackeroperationsv1.ActivePeerPage{}, errors.New("Tracker returned an invalid active peer response")
	}
	var page trackeroperationsv1.ActivePeerPage
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&page); err != nil || !page.Valid(limit) {
		return trackeroperationsv1.ActivePeerPage{}, errors.New("Tracker returned an invalid active peer response")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return trackeroperationsv1.ActivePeerPage{}, errors.New("Tracker active peer response has trailing data")
	}
	return page, nil
}

// ActivePeersByUser reads the bounded cross-swarm view used by profile pages.
// Tracker performs the scan in memory and returns no network endpoints or
// protocol identifiers; Core never persists the response.
func (client *Client) ActivePeersByUser(ctx context.Context, userID string, limit int) (trackeroperationsv1.UserActivePeerPage, error) {
	if !validUserID(userID) || limit < 1 || limit > trackeroperationsv1.MaxUserActivePeerLimit {
		return trackeroperationsv1.UserActivePeerPage{}, errors.New("Tracker user active peer query is invalid")
	}
	endpoint := fmt.Sprintf("%s/internal/v1/operations/users/%s/peers?limit=%d", client.origin, userID, limit)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return trackeroperationsv1.UserActivePeerPage{}, fmt.Errorf("create Tracker user active peer request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+client.serviceToken)
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return trackeroperationsv1.UserActivePeerPage{}, fmt.Errorf("read Tracker user active peers: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, responseLimit))
		return trackeroperationsv1.UserActivePeerPage{}, fmt.Errorf("Tracker user active peers returned status %d", response.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if err != nil || len(payload) > responseLimit {
		return trackeroperationsv1.UserActivePeerPage{}, errors.New("Tracker returned an invalid user active peer response")
	}
	var page trackeroperationsv1.UserActivePeerPage
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&page); err != nil || !page.Valid(userID, limit) {
		return trackeroperationsv1.UserActivePeerPage{}, errors.New("Tracker returned an invalid user active peer response")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return trackeroperationsv1.UserActivePeerPage{}, errors.New("Tracker user active peer response has trailing data")
	}
	return page, nil
}

func validUserID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func (client *Client) Runtime(ctx context.Context) (trackeroperationsv1.Runtime, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.endpoint, nil)
	if err != nil {
		return trackeroperationsv1.Runtime{}, fmt.Errorf("create Tracker runtime request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+client.serviceToken)
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return trackeroperationsv1.Runtime{}, fmt.Errorf("read Tracker runtime: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, responseLimit))
		return trackeroperationsv1.Runtime{}, fmt.Errorf("Tracker runtime returned status %d", response.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if err != nil || len(payload) > responseLimit {
		return trackeroperationsv1.Runtime{}, errors.New("Tracker returned an invalid runtime response")
	}
	var runtime trackeroperationsv1.Runtime
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&runtime); err != nil || !runtime.Valid() {
		return trackeroperationsv1.Runtime{}, errors.New("Tracker returned an invalid runtime response")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return trackeroperationsv1.Runtime{}, errors.New("Tracker runtime response has trailing data")
	}
	return runtime, nil
}
