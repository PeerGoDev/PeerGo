package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/peergo/peergo/contracts/go/deploymentv1"
)

// Config is fully validated before Privacy Vault opens a listener.
type Config struct {
	Environment                 string
	Address                     string
	DatabaseURL                 string
	IdentifierKey               []byte
	TOTPEncryptionKey           []byte
	TOTPKeyEpoch                string
	TrackerPasskeyEncryptionKey []byte
	TrackerPasskeyKeyEpoch      string
	TrackerPasskeyLookupKey     []byte
	ServiceToken                string
	EmailVerificationPublicURL  string
	PasswordRecoveryPublicURL   string
	EmailOutboxPath             string
	EmailDeliveryURL            string
	EmailDeliveryServiceToken   string
	EmailDeliveryPrivateHTTP    bool
}

// Load reads the Vault process configuration without hidden credential
// defaults. Local example values live in documentation, not production code.
func Load() (Config, error) {
	environment, err := required("PEERGO_ENV")
	if err != nil {
		return Config{}, err
	}
	if environment != "development" && environment != "production" {
		return Config{}, errors.New("PEERGO_ENV must be development or production")
	}
	databaseURL, err := required("PEERGO_VAULT_DATABASE_URL")
	if err != nil {
		return Config{}, err
	}
	if err := validateDatabaseURL(databaseURL, environment); err != nil {
		return Config{}, err
	}
	identifierKey, err := required("PEERGO_VAULT_IDENTIFIER_KEY")
	if err != nil {
		return Config{}, err
	}
	if len(identifierKey) < 32 {
		return Config{}, errors.New("PEERGO_VAULT_IDENTIFIER_KEY must contain at least 32 bytes")
	}
	totpEncryptionKey, err := required("PEERGO_VAULT_TOTP_ENCRYPTION_KEY")
	if err != nil {
		return Config{}, err
	}
	if len(totpEncryptionKey) != 32 {
		return Config{}, errors.New("PEERGO_VAULT_TOTP_ENCRYPTION_KEY must contain exactly 32 bytes")
	}
	totpKeyEpoch, err := required("PEERGO_VAULT_TOTP_KEY_EPOCH")
	if err != nil {
		return Config{}, err
	}
	if len(totpKeyEpoch) > 80 {
		return Config{}, errors.New("PEERGO_VAULT_TOTP_KEY_EPOCH must contain at most 80 characters")
	}
	trackerPasskeyEncryptionKey, err := required("PEERGO_VAULT_TRACKER_PASSKEY_ENCRYPTION_KEY")
	if err != nil {
		return Config{}, err
	}
	if len(trackerPasskeyEncryptionKey) != 32 {
		return Config{}, errors.New("PEERGO_VAULT_TRACKER_PASSKEY_ENCRYPTION_KEY must contain exactly 32 bytes")
	}
	trackerPasskeyKeyEpoch, err := required("PEERGO_VAULT_TRACKER_PASSKEY_KEY_EPOCH")
	if err != nil {
		return Config{}, err
	}
	if len(trackerPasskeyKeyEpoch) > 80 {
		return Config{}, errors.New("PEERGO_VAULT_TRACKER_PASSKEY_KEY_EPOCH must contain at most 80 characters")
	}
	trackerPasskeyLookupKey, err := required("PEERGO_TRACKER_PASSKEY_LOOKUP_KEY")
	if err != nil {
		return Config{}, err
	}
	if len(trackerPasskeyLookupKey) < 32 {
		return Config{}, errors.New("PEERGO_TRACKER_PASSKEY_LOOKUP_KEY must contain at least 32 bytes")
	}
	serviceToken, err := required("PEERGO_VAULT_SERVICE_TOKEN")
	if err != nil {
		return Config{}, err
	}
	if len(serviceToken) < 32 {
		return Config{}, errors.New("PEERGO_VAULT_SERVICE_TOKEN must contain at least 32 bytes")
	}
	emailVerificationPublicURL, err := required("PEERGO_EMAIL_VERIFICATION_PUBLIC_URL")
	if err != nil {
		return Config{}, err
	}
	parsedPublicURL, err := url.Parse(emailVerificationPublicURL)
	if err != nil || parsedPublicURL.Scheme == "" || parsedPublicURL.Host == "" || parsedPublicURL.User != nil || parsedPublicURL.RawQuery != "" || parsedPublicURL.Fragment != "" {
		return Config{}, errors.New("PEERGO_EMAIL_VERIFICATION_PUBLIC_URL must be absolute without user info, query or fragment")
	}
	if environment == "production" && parsedPublicURL.Scheme != "https" {
		return Config{}, errors.New("PEERGO_EMAIL_VERIFICATION_PUBLIC_URL must use https in production")
	}
	passwordRecoveryPublicURL, err := required("PEERGO_PASSWORD_RECOVERY_PUBLIC_URL")
	if err != nil {
		return Config{}, err
	}
	parsedRecoveryURL, err := url.Parse(passwordRecoveryPublicURL)
	if err != nil || parsedRecoveryURL.Scheme == "" || parsedRecoveryURL.Host == "" || parsedRecoveryURL.User != nil || parsedRecoveryURL.RawQuery != "" || parsedRecoveryURL.Fragment != "" {
		return Config{}, errors.New("PEERGO_PASSWORD_RECOVERY_PUBLIC_URL must be absolute without user info, query or fragment")
	}
	if environment == "production" && parsedRecoveryURL.Scheme != "https" {
		return Config{}, errors.New("PEERGO_PASSWORD_RECOVERY_PUBLIC_URL must use https in production")
	}

	var emailOutboxPath, emailDeliveryURL, emailDeliveryServiceToken string
	var emailDeliveryPrivateHTTP bool
	if environment == "development" {
		emailOutboxPath, err = required("PEERGO_EMAIL_OUTBOX_PATH")
		if err != nil {
			return Config{}, err
		}
	} else {
		emailDeliveryURL, err = required("PEERGO_EMAIL_DELIVERY_URL")
		if err != nil {
			return Config{}, err
		}
		parsedDeliveryURL, parseErr := url.Parse(emailDeliveryURL)
		if parseErr != nil || parsedDeliveryURL.Host == "" || parsedDeliveryURL.User != nil || parsedDeliveryURL.RawQuery != "" || parsedDeliveryURL.Fragment != "" {
			return Config{}, errors.New("PEERGO_EMAIL_DELIVERY_URL must be an absolute service URL without user info, query or fragment")
		}
		mode, modeErr := deploymentv1.Load()
		if modeErr != nil {
			return Config{}, modeErr
		}
		allowedSingleServerURL := mode == deploymentv1.SingleServer && parsedDeliveryURL.Scheme == "http" &&
			parsedDeliveryURL.Host == "email-relay:8086" && parsedDeliveryURL.Path == "/internal/v1/deliveries/transactional"
		if parsedDeliveryURL.Scheme != "https" && !allowedSingleServerURL {
			return Config{}, errors.New("PEERGO_EMAIL_DELIVERY_URL must use https, except the fixed single-server Relay endpoint")
		}
		emailDeliveryPrivateHTTP = allowedSingleServerURL
		emailDeliveryServiceToken, err = required("PEERGO_EMAIL_DELIVERY_SERVICE_TOKEN")
		if err != nil {
			return Config{}, err
		}
		if len(emailDeliveryServiceToken) < 32 {
			return Config{}, errors.New("PEERGO_EMAIL_DELIVERY_SERVICE_TOKEN must contain at least 32 bytes")
		}
	}

	address := strings.TrimSpace(os.Getenv("PEERGO_VAULT_ADDR"))
	if address == "" {
		address = ":8081"
	}
	return Config{
		Environment:                 environment,
		Address:                     address,
		DatabaseURL:                 databaseURL,
		IdentifierKey:               []byte(identifierKey),
		TOTPEncryptionKey:           []byte(totpEncryptionKey),
		TOTPKeyEpoch:                totpKeyEpoch,
		TrackerPasskeyEncryptionKey: []byte(trackerPasskeyEncryptionKey),
		TrackerPasskeyKeyEpoch:      trackerPasskeyKeyEpoch,
		TrackerPasskeyLookupKey:     []byte(trackerPasskeyLookupKey),
		ServiceToken:                serviceToken,
		EmailVerificationPublicURL:  parsedPublicURL.String(),
		PasswordRecoveryPublicURL:   parsedRecoveryURL.String(),
		EmailOutboxPath:             emailOutboxPath,
		EmailDeliveryURL:            emailDeliveryURL,
		EmailDeliveryServiceToken:   emailDeliveryServiceToken,
		EmailDeliveryPrivateHTTP:    emailDeliveryPrivateHTTP,
	}, nil
}

func validateDatabaseURL(value, environment string) error {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Host == "" ||
		strings.Trim(parsed.Path, "/") == "" || parsed.Fragment != "" {
		return errors.New("PEERGO_VAULT_DATABASE_URL must be a PostgreSQL connection URL with a database name")
	}
	if environment == "production" && parsed.Query().Get("sslmode") == "disable" {
		mode, modeErr := deploymentv1.Load()
		if modeErr != nil {
			return modeErr
		}
		if mode != deploymentv1.SingleServer || parsed.Host != "postgresql:5432" {
			return errors.New("PostgreSQL TLS can only be disabled for postgresql:5432 in single-server production")
		}
	}
	return nil
}

func required(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}
