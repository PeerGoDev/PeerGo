package config

import (
	"errors"
	"os"
	"strings"
)

type PromotionControlConfig struct {
	TrackerLedgerProcessConfig
	Address      string
	ServiceToken string
}

func LoadPromotionControl() (PromotionControlConfig, error) {
	database, err := LoadTrackerLedgerProcess()
	if err != nil {
		return PromotionControlConfig{}, err
	}
	token, err := required("PEERGO_SETTLEMENT_CONTROL_SERVICE_TOKEN")
	if err != nil || len(token) < 32 {
		return PromotionControlConfig{}, errors.New("PEERGO_SETTLEMENT_CONTROL_SERVICE_TOKEN must contain at least 32 bytes")
	}
	address := strings.TrimSpace(os.Getenv("PEERGO_SETTLEMENT_CONTROL_ADDR"))
	if address == "" {
		address = "127.0.0.1:8085"
	}
	return PromotionControlConfig{TrackerLedgerProcessConfig: database, Address: address, ServiceToken: token}, nil
}
