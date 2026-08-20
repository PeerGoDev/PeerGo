// Package settlementoperations implements Core's bounded, read-only adapter
// to Settlement policy summaries.
package settlementoperations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/peergo/peergo/contracts/go/settlementoperationsv1"
)

const responseLimit = 8 << 10

type Client struct {
	endpoint     string
	serviceToken string
	httpClient   *http.Client
}

func NewClient(origin, serviceToken string, timeout time.Duration) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, errors.New("Settlement operations origin must be an absolute origin")
	}
	if len(serviceToken) < 32 {
		return nil, errors.New("Settlement operations service token must contain at least 32 bytes")
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	parsed.Path = "/internal/v1/operations/settings"
	return &Client{
		endpoint: parsed.String(), serviceToken: serviceToken,
		httpClient: &http.Client{
			Timeout:       timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func (client *Client) Settings(ctx context.Context) (settlementoperationsv1.Settings, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.endpoint, nil)
	if err != nil {
		return settlementoperationsv1.Settings{}, fmt.Errorf("create Settlement settings request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+client.serviceToken)
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return settlementoperationsv1.Settings{}, fmt.Errorf("read Settlement settings: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, responseLimit))
		return settlementoperationsv1.Settings{}, fmt.Errorf("Settlement settings returned status %d", response.StatusCode)
	}
	var settings settlementoperationsv1.Settings
	decoder := json.NewDecoder(io.LimitReader(response.Body, responseLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil || !settings.Valid() {
		return settlementoperationsv1.Settings{}, errors.New("Settlement returned an invalid settings response")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return settlementoperationsv1.Settings{}, errors.New("Settlement settings response has trailing data")
	}
	return settings, nil
}
