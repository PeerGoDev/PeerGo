// Package trackeroperations implements Core's narrow, read-only adapter to the
// running Tracker process. It cannot announce, mutate swarms or read secrets.
package trackeroperations

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

	"github.com/peergo/peergo/contracts/go/trackeroperationsv1"
)

const responseLimit = 4 << 10

type Client struct {
	endpoint     string
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
	parsed.Path = "/internal/v1/operations/runtime"
	return &Client{
		endpoint: parsed.String(), serviceToken: serviceToken,
		httpClient: &http.Client{
			Timeout:       timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
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
	var runtime trackeroperationsv1.Runtime
	decoder := json.NewDecoder(io.LimitReader(response.Body, responseLimit))
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
