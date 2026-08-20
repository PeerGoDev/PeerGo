package config

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type PromotionWorkerConfig struct {
	Environment        string
	DatabaseURL        string
	SettlementURL      string
	SettlementToken    string
	RatioWatchInterval time.Duration
	RatioWatchBatch    int
}

func LoadPromotionWorker() (PromotionWorkerConfig, error) {
	environment, err := required("PEERGO_ENV")
	if err != nil || (environment != "development" && environment != "production") {
		return PromotionWorkerConfig{}, errors.New("PEERGO_ENV must be development or production")
	}
	databaseURL, err := required("PEERGO_CORE_DATABASE_URL")
	if err != nil {
		return PromotionWorkerConfig{}, err
	}
	settlementURL, err := required("PEERGO_SETTLEMENT_CONTROL_URL")
	if err != nil {
		return PromotionWorkerConfig{}, err
	}
	parsed, err := url.Parse(settlementURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return PromotionWorkerConfig{}, errors.New("PEERGO_SETTLEMENT_CONTROL_URL must be an absolute origin without user info")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return PromotionWorkerConfig{}, errors.New("PEERGO_SETTLEMENT_CONTROL_URL must use http or https")
	}
	if environment == "production" && parsed.Scheme != "https" {
		// The finite cutover runner starts Settlement control in the same
		// container and reaches it only through a kernel loopback address. Keep
		// clear-text traffic forbidden for every routable production endpoint.
		address := net.ParseIP(parsed.Hostname())
		if parsed.Scheme != "http" || address == nil || !address.IsLoopback() {
			return PromotionWorkerConfig{}, errors.New("PEERGO_SETTLEMENT_CONTROL_URL must use https or an IP loopback HTTP origin in production")
		}
	}
	token, err := required("PEERGO_SETTLEMENT_CONTROL_SERVICE_TOKEN")
	if err != nil || len(token) < 32 {
		return PromotionWorkerConfig{}, errors.New("PEERGO_SETTLEMENT_CONTROL_SERVICE_TOKEN must contain at least 32 bytes")
	}
	ratioWatchInterval := time.Minute
	if raw := strings.TrimSpace(os.Getenv("PEERGO_RATIO_WATCH_INTERVAL")); raw != "" {
		value, parseErr := time.ParseDuration(raw)
		if parseErr != nil || value < 10*time.Second || value > time.Hour {
			return PromotionWorkerConfig{}, errors.New("PEERGO_RATIO_WATCH_INTERVAL must be between 10s and 1h")
		}
		ratioWatchInterval = value
	}
	ratioWatchBatch := 500
	if raw := strings.TrimSpace(os.Getenv("PEERGO_RATIO_WATCH_BATCH")); raw != "" {
		value, parseErr := strconv.Atoi(raw)
		if parseErr != nil || value < 1 || value > 5000 {
			return PromotionWorkerConfig{}, errors.New("PEERGO_RATIO_WATCH_BATCH must be between 1 and 5000")
		}
		ratioWatchBatch = value
	}
	return PromotionWorkerConfig{
		Environment: environment, DatabaseURL: databaseURL,
		SettlementURL: strings.TrimRight(settlementURL, "/"), SettlementToken: token,
		RatioWatchInterval: ratioWatchInterval, RatioWatchBatch: ratioWatchBatch,
	}, nil
}
