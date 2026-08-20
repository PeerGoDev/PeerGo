package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Environment  string
	Address      string
	JournalPath  string
	ServiceToken string
}

func Load() (Config, error) {
	environment, err := required("PEERGO_ENV")
	if err != nil {
		return Config{}, err
	}
	if environment != "development" && environment != "production" {
		return Config{}, errors.New("PEERGO_ENV must be development or production")
	}
	journalPath, err := required("PEERGO_AUDIT_JOURNAL_PATH")
	if err != nil {
		return Config{}, err
	}
	if !filepath.IsAbs(journalPath) {
		return Config{}, errors.New("PEERGO_AUDIT_JOURNAL_PATH must be absolute")
	}
	serviceToken, err := required("PEERGO_AUDIT_SERVICE_TOKEN")
	if err != nil {
		return Config{}, err
	}
	if len(serviceToken) < 32 {
		return Config{}, errors.New("PEERGO_AUDIT_SERVICE_TOKEN must contain at least 32 bytes")
	}
	address := strings.TrimSpace(os.Getenv("PEERGO_AUDIT_ADDR"))
	if address == "" {
		address = ":8082"
	}
	return Config{
		Environment:  environment,
		Address:      address,
		JournalPath:  filepath.Clean(journalPath),
		ServiceToken: serviceToken,
	}, nil
}

func required(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}
