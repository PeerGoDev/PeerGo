// Package auditsink is Core's narrow HTTP adapter to the independent Audit
// Sink. It sends immutable bytes and never logs or returns event payloads.
package auditsink

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/peergo/peergo/services/core/internal/modules/audit"
)

const maxErrorResponseBytes = 8 << 10

type Client struct {
	endpoint     string
	serviceToken string
	httpClient   *http.Client
}

func NewClient(baseURL, serviceToken string, timeout time.Duration) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, errors.New("audit sink URL must be an absolute origin without user info")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("audit sink URL must use http or https")
	}
	if len(serviceToken) < 32 {
		return nil, errors.New("audit sink service token must contain at least 32 bytes")
	}
	if timeout <= 0 {
		return nil, errors.New("audit sink client timeout must be positive")
	}
	parsed.Path = "/internal/v1/audit/events"
	return &Client{
		endpoint:     parsed.String(),
		serviceToken: serviceToken,
		httpClient:   &http.Client{Timeout: timeout},
	}, nil
}

func (client *Client) Append(ctx context.Context, event audit.Event) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint, bytes.NewReader(event.Payload))
	if err != nil {
		return fmt.Errorf("compose audit sink request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+client.serviceToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", event.ID.String())
	request.Header.Set("X-PeerGo-Content-SHA256", hex.EncodeToString(event.PayloadSHA256[:]))

	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("send audit event: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusOK || response.StatusCode == http.StatusCreated {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxErrorResponseBytes))
		return nil
	}

	var body struct {
		Code string `json:"code"`
	}
	limited := io.LimitReader(response.Body, maxErrorResponseBytes)
	_ = json.NewDecoder(limited).Decode(&body)
	if body.Code == "" {
		body.Code = "unexpected_response"
	}
	return fmt.Errorf("audit sink rejected event: status=%d code=%s", response.StatusCode, body.Code)
}

var _ audit.Sink = (*Client)(nil)
