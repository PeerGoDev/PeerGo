package settlementcontrol

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
)

const maxErrorResponseBytes = 4 << 10

type Client struct {
	endpoint     string
	serviceToken string
	httpClient   *http.Client
}

func NewClient(baseURL, endpointPath, serviceToken string, timeout time.Duration) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" ||
		parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, errors.New("Settlement control URL must be an absolute origin without user info")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("Settlement control URL must use http or https")
	}
	if !strings.HasPrefix(endpointPath, "/internal/v1/settlement/") || len(serviceToken) < 32 || timeout <= 0 {
		return nil, errors.New("Settlement control client configuration is invalid")
	}
	parsed.Path = endpointPath
	return &Client{endpoint: parsed.String(), serviceToken: serviceToken, httpClient: &http.Client{Timeout: timeout}}, nil
}

func (client *Client) Append(ctx context.Context, pending PendingCommand) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint, bytes.NewReader(pending.Payload))
	if err != nil {
		return fmt.Errorf("compose Settlement control request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+client.serviceToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", pending.ID.String())
	request.Header.Set("X-PeerGo-Content-SHA256", hex.EncodeToString(pending.SHA256[:]))
	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("send Settlement control command: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusOK || response.StatusCode == http.StatusCreated {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxErrorResponseBytes))
		return nil
	}
	var body struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(io.LimitReader(response.Body, maxErrorResponseBytes)).Decode(&body)
	if body.Code == "" {
		body.Code = "unexpected_response"
	}
	return fmt.Errorf("Settlement rejected control command: status=%d code=%s", response.StatusCode, body.Code)
}

var _ Sink = (*Client)(nil)
