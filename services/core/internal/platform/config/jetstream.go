package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// projectionNATSURLs centralizes the credential-free URL boundary shared by
// Core's independent JetStream projectors. Credentials remain file-mounted and
// production connections must use TLS; a URL can therefore be logged without
// accidentally exposing a username or token.
func projectionNATSURLs(name, environment string) ([]string, error) {
	value, err := required(name)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	urls := make([]string, 0, strings.Count(value, ",")+1)
	for _, raw := range strings.Split(value, ",") {
		parsed, parseErr := url.Parse(strings.TrimSpace(raw))
		if parseErr != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
			(parsed.Scheme != "nats" && parsed.Scheme != "tls") || (environment == "production" && parsed.Scheme != "tls") {
			return nil, fmt.Errorf("%s must contain comma-separated credential-free nats:// URLs, or tls:// URLs required in production", name)
		}
		canonical := parsed.String()
		if _, duplicate := seen[canonical]; duplicate {
			return nil, fmt.Errorf("%s contains a duplicate URL", name)
		}
		seen[canonical] = struct{}{}
		urls = append(urls, canonical)
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("%s requires at least one URL", name)
	}
	return urls, nil
}

func projectionCredentialPath(name, environment string) (string, error) {
	value, err := projectionOptionalAbsolutePath(name)
	if err != nil {
		return "", err
	}
	if environment == "production" && value == "" {
		return "", fmt.Errorf("%s is required in production", name)
	}
	return value, nil
}

func projectionOptionalAbsolutePath(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", nil
	}
	if !filepath.IsAbs(value) {
		return "", fmt.Errorf("%s must be absolute when configured", name)
	}
	return filepath.Clean(value), nil
}

func projectionDuration(name string, minimum, maximum time.Duration) (time.Duration, error) {
	value, err := required(name)
	if err != nil {
		return 0, err
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be between %s and %s", name, minimum, maximum)
	}
	return parsed, nil
}

func projectionInteger(name string, minimum, maximum int) (int, error) {
	value, err := required(name)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be between %d and %d", name, minimum, maximum)
	}
	return parsed, nil
}

func requireAckWindow(name string, ackWait, processTimeout, ackTimeout time.Duration) error {
	if ackWait <= processTimeout+ackTimeout {
		return errors.New(name + " must exceed process timeout plus ACK timeout")
	}
	return nil
}
