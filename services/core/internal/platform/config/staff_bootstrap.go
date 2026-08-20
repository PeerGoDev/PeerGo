package config

import "errors"

// StaffBootstrapConfig is deliberately smaller than API Config. The operator
// CLI needs direct Core database access and the audit pseudonym key, but it
// must not require or gain access to Vault, WebAuthn record or cookie secrets.
type StaffBootstrapConfig struct {
	Environment       string
	DatabaseURL       string
	AuditPseudonymKey []byte
	AuditKeyEpoch     string
}

func LoadStaffBootstrap() (StaffBootstrapConfig, error) {
	environment, err := required("PEERGO_ENV")
	if err != nil {
		return StaffBootstrapConfig{}, err
	}
	if environment != "development" && environment != "production" {
		return StaffBootstrapConfig{}, errors.New("PEERGO_ENV must be development or production")
	}
	databaseURL, err := required("PEERGO_CORE_DATABASE_URL")
	if err != nil {
		return StaffBootstrapConfig{}, err
	}
	auditKey, err := required("PEERGO_AUDIT_PSEUDONYM_KEY")
	if err != nil {
		return StaffBootstrapConfig{}, err
	}
	if len(auditKey) < 32 {
		return StaffBootstrapConfig{}, errors.New("PEERGO_AUDIT_PSEUDONYM_KEY must contain at least 32 bytes")
	}
	auditEpoch, err := required("PEERGO_AUDIT_PSEUDONYM_KEY_EPOCH")
	if err != nil {
		return StaffBootstrapConfig{}, err
	}
	return StaffBootstrapConfig{
		Environment:       environment,
		DatabaseURL:       databaseURL,
		AuditPseudonymKey: []byte(auditKey),
		AuditKeyEpoch:     auditEpoch,
	}, nil
}
