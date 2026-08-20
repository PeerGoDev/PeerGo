package identity

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
)

const turnstileSiteVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

type HumanVerificationFlow string

const (
	HumanVerificationFlowRegistration     HumanVerificationFlow = "registration"
	HumanVerificationFlowLogin            HumanVerificationFlow = "login"
	HumanVerificationFlowPasswordRecovery HumanVerificationFlow = "password_recovery"
)

var (
	ErrHumanVerificationRequired    = errors.New("human verification token is required")
	ErrHumanVerificationFailed      = errors.New("human verification failed")
	ErrHumanVerificationUnavailable = errors.New("human verification service is unavailable")
)

// HumanVerificationVerifier validates a single-use browser challenge before a
// protected anonymous identity command reaches credential or registration
// services. Implementations must never persist or log the raw token.
type HumanVerificationVerifier interface {
	Verify(context.Context, HumanVerificationFlow, string) error
	Configured() bool
}

type unavailableHumanVerificationVerifier struct{}

func NewUnavailableHumanVerificationVerifier() HumanVerificationVerifier {
	return unavailableHumanVerificationVerifier{}
}

func (unavailableHumanVerificationVerifier) Verify(context.Context, HumanVerificationFlow, string) error {
	return ErrHumanVerificationUnavailable
}

func (unavailableHumanVerificationVerifier) Configured() bool { return false }

type TurnstileVerifier struct {
	secretKey string
	endpoint  string
	client    *http.Client
}

func NewTurnstileVerifier(secretKey string, client *http.Client) (*TurnstileVerifier, error) {
	return newTurnstileVerifier(secretKey, turnstileSiteVerifyURL, client)
}

func newTurnstileVerifier(secretKey, endpoint string, client *http.Client) (*TurnstileVerifier, error) {
	secretKey = strings.TrimSpace(secretKey)
	if secretKey == "" || len(secretKey) > 256 || strings.ContainsAny(secretKey, "\r\n") {
		return nil, errors.New("Turnstile secret key is invalid")
	}
	parsedEndpoint, err := url.Parse(endpoint)
	if err != nil || parsedEndpoint.Scheme == "" || parsedEndpoint.Host == "" || parsedEndpoint.User != nil {
		return nil, errors.New("Turnstile Siteverify endpoint is invalid")
	}
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	return &TurnstileVerifier{secretKey: secretKey, endpoint: endpoint, client: client}, nil
}

func (verifier *TurnstileVerifier) Configured() bool { return verifier != nil }

func (verifier *TurnstileVerifier) Verify(ctx context.Context, flow HumanVerificationFlow, token string) error {
	if verifier == nil || verifier.client == nil {
		return ErrHumanVerificationUnavailable
	}
	if !validHumanVerificationFlow(flow) {
		return ErrHumanVerificationFailed
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return ErrHumanVerificationRequired
	}
	if len(token) > 2048 || strings.ContainsAny(token, "\r\n") {
		return ErrHumanVerificationFailed
	}

	form := url.Values{
		"secret":   {verifier.secretKey},
		"response": {token},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, verifier.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("%w: create Siteverify request", ErrHumanVerificationUnavailable)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := verifier.client.Do(request)
	if err != nil {
		return fmt.Errorf("%w: call Siteverify", ErrHumanVerificationUnavailable)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("%w: Siteverify returned HTTP %d", ErrHumanVerificationUnavailable, response.StatusCode)
	}

	var result struct {
		Success bool   `json:"success"`
		Action  string `json:"action"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64<<10))
	if err := decoder.Decode(&result); err != nil {
		return fmt.Errorf("%w: decode Siteverify response", ErrHumanVerificationUnavailable)
	}
	if !result.Success || result.Action != string(flow) {
		return ErrHumanVerificationFailed
	}
	return nil
}

func validHumanVerificationFlow(flow HumanVerificationFlow) bool {
	switch flow {
	case HumanVerificationFlowRegistration, HumanVerificationFlowLogin, HumanVerificationFlowPasswordRecovery:
		return true
	default:
		return false
	}
}

var _ HumanVerificationVerifier = (*TurnstileVerifier)(nil)
