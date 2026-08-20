package config

import (
	"errors"
	"net/url"
	"strings"
)

type AuditWorkerConfig struct {
	Environment  string
	DatabaseURL  string
	AuditSinkURL string
	ServiceToken string
}

// LoadAuditWorker validates only the dependencies used by the outbox worker;
// it does not inherit API-only Vault, browser-origin or cookie settings.
func LoadAuditWorker() (AuditWorkerConfig, error) {
	environment, err := required("PEERGO_ENV")
	if err != nil {
		return AuditWorkerConfig{}, err
	}
	if environment != "development" && environment != "production" {
		return AuditWorkerConfig{}, errors.New("PEERGO_ENV must be development or production")
	}
	databaseURL, err := required("PEERGO_CORE_DATABASE_URL")
	if err != nil {
		return AuditWorkerConfig{}, err
	}
	sinkURL, err := required("PEERGO_AUDIT_SINK_URL")
	if err != nil {
		return AuditWorkerConfig{}, err
	}
	parsed, err := url.Parse(sinkURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return AuditWorkerConfig{}, errors.New("PEERGO_AUDIT_SINK_URL must be an absolute origin without user info")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return AuditWorkerConfig{}, errors.New("PEERGO_AUDIT_SINK_URL must use http or https")
	}
	if environment == "production" && parsed.Scheme != "https" {
		singleServer, modeErr := isSingleServerDeployment()
		if modeErr != nil {
			return AuditWorkerConfig{}, modeErr
		}
		if !singleServer || parsed.Scheme != "http" || parsed.Host != "audit-sink:8082" {
			return AuditWorkerConfig{}, errors.New("PEERGO_AUDIT_SINK_URL must use https, except http://audit-sink:8082 in single-server production")
		}
	}
	serviceToken, err := required("PEERGO_AUDIT_SERVICE_TOKEN")
	if err != nil {
		return AuditWorkerConfig{}, err
	}
	if len(serviceToken) < 32 {
		return AuditWorkerConfig{}, errors.New("PEERGO_AUDIT_SERVICE_TOKEN must contain at least 32 bytes")
	}
	return AuditWorkerConfig{
		Environment:  environment,
		DatabaseURL:  databaseURL,
		AuditSinkURL: strings.TrimRight(sinkURL, "/"),
		ServiceToken: serviceToken,
	}, nil
}
