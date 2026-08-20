package config

import (
	"errors"
	"fmt"
	"net"
	"net/mail"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Address      string
	ServiceToken string
	SiteName     string
	SMTP         SMTPConfig
}

type SMTPConfig struct {
	Host        string
	Port        int
	Username    string
	Password    string
	FromAddress string
	FromName    string
	TLSMode     string
	Timeout     time.Duration
}

// Load refuses plaintext SMTP and incomplete credentials. Production mail
// must never silently fall back to a local file or an unauthenticated relay.
func Load() (Config, error) {
	if environment := strings.TrimSpace(os.Getenv("PEERGO_ENV")); environment != "production" && environment != "development" {
		return Config{}, errors.New("PEERGO_ENV must be development or production")
	}
	serviceToken, err := required("PEERGO_EMAIL_RELAY_SERVICE_TOKEN")
	if err != nil || len(serviceToken) < 32 {
		return Config{}, errors.New("PEERGO_EMAIL_RELAY_SERVICE_TOKEN must contain at least 32 bytes")
	}
	host, err := required("PEERGO_SMTP_HOST")
	if err != nil || net.ParseIP(host) != nil || strings.ContainsAny(host, "/:@") {
		return Config{}, errors.New("PEERGO_SMTP_HOST must be a DNS host name")
	}
	port, err := integer("PEERGO_SMTP_PORT", 1, 65535)
	if err != nil {
		return Config{}, err
	}
	username, err := required("PEERGO_SMTP_USERNAME")
	if err != nil {
		return Config{}, err
	}
	password, err := required("PEERGO_SMTP_PASSWORD")
	if err != nil {
		return Config{}, err
	}
	fromAddress, err := required("PEERGO_SMTP_FROM_ADDRESS")
	if err != nil {
		return Config{}, err
	}
	parsedFrom, parseErr := mail.ParseAddress(fromAddress)
	if parseErr != nil || parsedFrom.Address != fromAddress {
		return Config{}, errors.New("PEERGO_SMTP_FROM_ADDRESS must be one bare email address")
	}
	tlsMode, err := required("PEERGO_SMTP_TLS_MODE")
	if err != nil || (tlsMode != "starttls" && tlsMode != "implicit") {
		return Config{}, errors.New("PEERGO_SMTP_TLS_MODE must be starttls or implicit")
	}
	siteName, err := required("PEERGO_EMAIL_SITE_NAME")
	if err != nil || len([]rune(siteName)) > 80 || containsHeaderBreak(siteName) {
		return Config{}, errors.New("PEERGO_EMAIL_SITE_NAME must contain 1 to 80 safe characters")
	}
	fromName := strings.TrimSpace(os.Getenv("PEERGO_SMTP_FROM_NAME"))
	if fromName == "" {
		fromName = siteName
	}
	if len([]rune(fromName)) > 80 || containsHeaderBreak(fromName) {
		return Config{}, errors.New("PEERGO_SMTP_FROM_NAME must contain at most 80 safe characters")
	}
	address := strings.TrimSpace(os.Getenv("PEERGO_EMAIL_RELAY_ADDR"))
	if address == "" {
		address = ":8086"
	}
	timeout := 10 * time.Second
	if raw := strings.TrimSpace(os.Getenv("PEERGO_SMTP_TIMEOUT")); raw != "" {
		timeout, err = time.ParseDuration(raw)
		if err != nil || timeout < time.Second || timeout > time.Minute {
			return Config{}, errors.New("PEERGO_SMTP_TIMEOUT must be between 1s and 1m")
		}
	}
	return Config{
		Address: address, ServiceToken: serviceToken, SiteName: siteName,
		SMTP: SMTPConfig{Host: host, Port: port, Username: username, Password: password, FromAddress: fromAddress, FromName: fromName, TLSMode: tlsMode, Timeout: timeout},
	}, nil
}

func required(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

func integer(name string, minimum, maximum int) (int, error) {
	raw, err := required(name)
	if err != nil {
		return 0, err
	}
	value, parseErr := strconv.Atoi(raw)
	if parseErr != nil || value < minimum || value > maximum {
		return 0, fmt.Errorf("%s must be between %d and %d", name, minimum, maximum)
	}
	return value, nil
}

func containsHeaderBreak(value string) bool { return strings.ContainsAny(value, "\r\n") }
