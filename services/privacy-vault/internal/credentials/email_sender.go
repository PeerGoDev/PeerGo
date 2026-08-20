package credentials

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type TransactionalEmailTemplate string

const (
	EmailTemplateVerification     TransactionalEmailTemplate = "peergo-email-verification-v1"
	EmailTemplatePasswordRecovery TransactionalEmailTemplate = "peergo-password-recovery-v1"
	EmailTemplateDeliveryTest     TransactionalEmailTemplate = "peergo-delivery-test-v1"
)

// TransactionalEmailMessage is the shared delivery value for every Vault-owned
// action link. A single bounded adapter prevents verification and recovery from
// growing separate relay clients, local outbox formats or URL validation rules.
type TransactionalEmailMessage struct {
	Template  TransactionalEmailTemplate
	Recipient string
	ActionURL string
	ExpiresAt time.Time
}

type TransactionalEmailSender interface {
	SendTransactionalEmail(context.Context, TransactionalEmailMessage) error
}

// DevelopmentEmailOutboxSender writes raw local-only delivery fixtures under
// .local/. The file is intentionally private and Git-ignored because it holds
// both PII and live bearer links for browser demonstrations.
type DevelopmentEmailOutboxSender struct {
	path string
	mu   sync.Mutex
}

func NewDevelopmentEmailOutboxSender(path string) (*DevelopmentEmailOutboxSender, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || !filepath.IsAbs(path) {
		return nil, errors.New("development email outbox path must be absolute")
	}
	return &DevelopmentEmailOutboxSender{path: path}, nil
}

func (sender *DevelopmentEmailOutboxSender) SendTransactionalEmail(_ context.Context, message TransactionalEmailMessage) error {
	if err := validateTransactionalEmailMessage(message); err != nil {
		return err
	}
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(sender.path), 0o700); err != nil {
		return fmt.Errorf("create development email outbox directory: %w", err)
	}
	file, err := os.OpenFile(sender.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open development email outbox: %w", err)
	}
	defer file.Close()
	encoded, err := json.Marshal(struct {
		Template  TransactionalEmailTemplate `json:"template"`
		Recipient string                     `json:"recipient"`
		ActionURL string                     `json:"action_url"`
		ExpiresAt time.Time                  `json:"expires_at"`
	}{message.Template, message.Recipient, message.ActionURL, message.ExpiresAt.UTC()})
	if err != nil {
		return fmt.Errorf("encode development email delivery: %w", err)
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("append development email delivery: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync development email delivery: %w", err)
	}
	return nil
}

type RelayTransactionalEmailSender struct {
	endpoint     string
	serviceToken string
	client       *http.Client
}

func NewRelayTransactionalEmailSender(endpoint, serviceToken string, timeout time.Duration, allowSingleServerPrivateHTTP bool) (*RelayTransactionalEmailSender, error) {
	parsed, err := url.Parse(endpoint)
	allowedPrivateHTTP := allowSingleServerPrivateHTTP && parsed != nil && parsed.Scheme == "http" &&
		parsed.Host == "email-relay:8086" && parsed.Path == "/internal/v1/deliveries/transactional"
	if err != nil || parsed == nil || (parsed.Scheme != "https" && !allowedPrivateHTTP) || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("email delivery relay URL must use https, except the fixed single-server private HTTP origin")
	}
	if len(serviceToken) < 32 {
		return nil, errors.New("email delivery relay token must contain at least 32 bytes")
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &RelayTransactionalEmailSender{
		endpoint:     parsed.String(),
		serviceToken: serviceToken,
		client: &http.Client{
			Timeout:       timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// SendTransactionalEmail uses a narrow trusted relay request. Redirects are
// blocked so a relay misconfiguration cannot forward a raw address or token.
func (sender *RelayTransactionalEmailSender) SendTransactionalEmail(ctx context.Context, message TransactionalEmailMessage) error {
	if err := validateTransactionalEmailMessage(message); err != nil {
		return err
	}
	body, err := json.Marshal(struct {
		Template  TransactionalEmailTemplate `json:"template"`
		Recipient string                     `json:"recipient"`
		ActionURL string                     `json:"action_url"`
		ExpiresAt time.Time                  `json:"expires_at"`
	}{message.Template, message.Recipient, message.ActionURL, message.ExpiresAt.UTC()})
	if err != nil {
		return fmt.Errorf("encode email delivery relay request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, sender.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create email delivery relay request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+sender.serviceToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := sender.client.Do(request)
	if err != nil {
		return fmt.Errorf("send email delivery relay request: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusAccepted && response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("email delivery relay returned status %d", response.StatusCode)
	}
	return nil
}

func validateTransactionalEmailMessage(message TransactionalEmailMessage) error {
	if message.Recipient == "" {
		return errors.New("transactional email delivery is missing required fields")
	}
	if message.Template == EmailTemplateDeliveryTest {
		if message.ActionURL != "" || !message.ExpiresAt.IsZero() {
			return errors.New("transactional email delivery test must not contain an action link")
		}
		return nil
	}
	if (message.Template != EmailTemplateVerification && message.Template != EmailTemplatePasswordRecovery) ||
		message.ActionURL == "" || message.ExpiresAt.IsZero() {
		return errors.New("transactional email delivery is missing required fields")
	}
	parsed, err := url.Parse(message.ActionURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Fragment == "" {
		return errors.New("transactional email delivery URL is invalid")
	}
	return nil
}
