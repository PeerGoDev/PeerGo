package config

import (
	"errors"
	"net/url"
	"strings"
)

// CoreDatabaseProcessConfig is shared by narrow workers that only consume the
// Core database. Adding a projector must not accidentally grant it Vault,
// object-store, WebAuthn, cookie or audit-sink credentials.
type CoreDatabaseProcessConfig struct {
	Environment string
	DatabaseURL string
}

func LoadCoreDatabaseProcess() (CoreDatabaseProcessConfig, error) {
	environment, err := loadCoreEnvironment()
	if err != nil {
		return CoreDatabaseProcessConfig{}, err
	}
	databaseURL, err := required("PEERGO_CORE_DATABASE_URL")
	if err != nil || validateCoreDatabaseURL(databaseURL, environment) != nil {
		return CoreDatabaseProcessConfig{}, errors.New("PEERGO_CORE_DATABASE_URL must be a PostgreSQL connection URL with a database name")
	}
	return CoreDatabaseProcessConfig{Environment: environment, DatabaseURL: databaseURL}, nil
}

func validateCoreDatabaseURL(value, environment string) error {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Host == "" ||
		strings.Trim(parsed.Path, "/") == "" || parsed.Fragment != "" {
		return errors.New("invalid PostgreSQL URL")
	}
	if environment == "production" && parsed.Query().Get("sslmode") == "disable" {
		return errors.New("PostgreSQL TLS cannot be disabled in production")
	}
	return nil
}

// ValidateCutoverDatabaseURL applies the stricter operator-tool boundary used
// while credential and torrent evidence are copied between databases. Unlike
// the ordinary service compatibility check, production cutover refuses TLS
// modes that encrypt without verifying the database hostname and certificate.
func ValidateCutoverDatabaseURL(value, environment string) error {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Host == "" ||
		strings.Trim(parsed.Path, "/") == "" || parsed.Fragment != "" {
		return errors.New("invalid PostgreSQL cutover URL")
	}
	if environment == "production" && parsed.Query().Get("sslmode") != "verify-full" {
		return errors.New("production cutover PostgreSQL URLs must use sslmode=verify-full")
	}
	return nil
}

func loadCoreEnvironment() (string, error) {
	environment, err := required("PEERGO_ENV")
	if err != nil {
		return "", err
	}
	if environment != "development" && environment != "production" {
		return "", errors.New("PEERGO_ENV must be development or production")
	}
	return environment, nil
}
