package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Config is validated before Core opens its listener or connects adapters.
type Config struct {
	Environment                 string
	Address                     string
	DatabaseURL                 string
	VaultURL                    string
	VaultServiceToken           string
	SessionCSRFKey              []byte
	AuditPseudonymKey           []byte
	AuditKeyEpoch               string
	CookieName                  string
	StaffCookieName             string
	CookieSecure                bool
	AllowedOrigins              []string
	PublicOrigin                string
	WebAuthnRPID                string
	WebAuthnOrigins             []string
	WebAuthnRecordKey           []byte
	WebAuthnKeyEpoch            string
	TorrentObjectStore          ObjectStoreConfig
	TorrentUploadMaxBytes       int
	TrackerCanonicalOrigin      string
	TrackerOperationsOrigin     string
	TrackerServiceToken         string
	SettlementControlURL        string
	SettlementServiceToken      string
	SeedingEvidenceStartAt      time.Time
	SeedingEvidenceClosureDelay time.Duration
	TurnstileSecretKey          string
}

// Load reads explicit runtime configuration. Database and cryptographic
// credentials never have production-shaped fallback values.
func Load() (Config, error) {
	environment, err := required("PEERGO_ENV")
	if err != nil {
		return Config{}, err
	}
	if environment != "development" && environment != "production" {
		return Config{}, errors.New("PEERGO_ENV must be development or production")
	}
	databaseURL, err := required("PEERGO_CORE_DATABASE_URL")
	if err != nil {
		return Config{}, err
	}
	if err := validateCoreDatabaseURL(databaseURL, environment); err != nil {
		return Config{}, err
	}
	vaultURL, err := required("PEERGO_VAULT_URL")
	if err != nil {
		return Config{}, err
	}
	parsedVaultURL, err := url.Parse(vaultURL)
	if err != nil || parsedVaultURL.Scheme == "" || parsedVaultURL.Host == "" || parsedVaultURL.User != nil {
		return Config{}, errors.New("PEERGO_VAULT_URL must be an absolute URL without user info")
	}
	if environment == "production" && parsedVaultURL.Scheme != "https" {
		singleServer, modeErr := isSingleServerDeployment()
		if modeErr != nil {
			return Config{}, modeErr
		}
		if !singleServer || parsedVaultURL.Scheme != "http" || parsedVaultURL.Host != "vault-api:8081" {
			return Config{}, errors.New("PEERGO_VAULT_URL must use https, except http://vault-api:8081 in single-server production")
		}
	}
	vaultServiceToken, err := required("PEERGO_VAULT_SERVICE_TOKEN")
	if err != nil {
		return Config{}, err
	}
	if len(vaultServiceToken) < 32 {
		return Config{}, errors.New("PEERGO_VAULT_SERVICE_TOKEN must contain at least 32 bytes")
	}
	csrfKey, err := required("PEERGO_SESSION_CSRF_KEY")
	if err != nil {
		return Config{}, err
	}
	if len(csrfKey) < 32 {
		return Config{}, errors.New("PEERGO_SESSION_CSRF_KEY must contain at least 32 bytes")
	}
	auditPseudonymKey, err := required("PEERGO_AUDIT_PSEUDONYM_KEY")
	if err != nil {
		return Config{}, err
	}
	if len(auditPseudonymKey) < 32 {
		return Config{}, errors.New("PEERGO_AUDIT_PSEUDONYM_KEY must contain at least 32 bytes")
	}
	auditKeyEpoch, err := required("PEERGO_AUDIT_PSEUDONYM_KEY_EPOCH")
	if err != nil {
		return Config{}, err
	}
	cookieSecureValue, err := required("PEERGO_COOKIE_SECURE")
	if err != nil {
		return Config{}, err
	}
	cookieSecure, err := strconv.ParseBool(cookieSecureValue)
	if err != nil {
		return Config{}, errors.New("PEERGO_COOKIE_SECURE must be true or false")
	}
	if environment == "production" && !cookieSecure {
		return Config{}, errors.New("PEERGO_COOKIE_SECURE must be true in production")
	}
	allowedOrigins, err := parseOrigins("PEERGO_WEB_ORIGINS", os.Getenv("PEERGO_WEB_ORIGINS"), environment)
	if err != nil {
		return Config{}, err
	}
	publicOriginValue, err := required("PEERGO_PUBLIC_ORIGIN")
	if err != nil {
		return Config{}, err
	}
	parsedPublicOrigin, err := url.Parse(publicOriginValue)
	if err != nil || parsedPublicOrigin.Scheme == "" || parsedPublicOrigin.Host == "" || parsedPublicOrigin.User != nil ||
		parsedPublicOrigin.RawQuery != "" || parsedPublicOrigin.Fragment != "" ||
		(parsedPublicOrigin.Path != "" && parsedPublicOrigin.Path != "/") ||
		(parsedPublicOrigin.Scheme != "http" && parsedPublicOrigin.Scheme != "https") {
		return Config{}, errors.New("PEERGO_PUBLIC_ORIGIN must be an absolute HTTP origin without user info, path, query or fragment")
	}
	if environment == "production" && parsedPublicOrigin.Scheme != "https" {
		return Config{}, errors.New("PEERGO_PUBLIC_ORIGIN must use https in production")
	}
	publicOrigin := parsedPublicOrigin.Scheme + "://" + parsedPublicOrigin.Host
	if !slices.Contains(allowedOrigins, publicOrigin) {
		return Config{}, errors.New("PEERGO_PUBLIC_ORIGIN must also appear in PEERGO_WEB_ORIGINS")
	}
	webAuthnRPID, err := required("PEERGO_WEBAUTHN_RP_ID")
	if err != nil {
		return Config{}, err
	}
	if strings.ContainsAny(webAuthnRPID, "/:@") || len(webAuthnRPID) > 253 {
		return Config{}, errors.New("PEERGO_WEBAUTHN_RP_ID must be a host name without scheme or port")
	}
	webAuthnOrigins, err := parseOrigins("PEERGO_WEBAUTHN_ORIGINS", os.Getenv("PEERGO_WEBAUTHN_ORIGINS"), environment)
	if err != nil {
		return Config{}, err
	}
	allowedOriginSet := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowedOriginSet[origin] = struct{}{}
	}
	for _, origin := range webAuthnOrigins {
		if _, exists := allowedOriginSet[origin]; !exists {
			return Config{}, fmt.Errorf("WebAuthn origin %q must also appear in PEERGO_WEB_ORIGINS", origin)
		}
	}
	webAuthnRecordKey, err := required("PEERGO_WEBAUTHN_RECORD_KEY")
	if err != nil {
		return Config{}, err
	}
	if len(webAuthnRecordKey) != 32 {
		return Config{}, errors.New("PEERGO_WEBAUTHN_RECORD_KEY must contain exactly 32 bytes")
	}
	webAuthnKeyEpoch, err := required("PEERGO_WEBAUTHN_KEY_EPOCH")
	if err != nil {
		return Config{}, err
	}
	torrentObjectStore, err := loadObjectStore("PEERGO_TORRENT_STORAGE", environment)
	if err != nil {
		return Config{}, err
	}
	torrentUploadMaxBytes := 4 << 20
	if raw := strings.TrimSpace(os.Getenv("PEERGO_TORRENT_UPLOAD_MAX_BYTES")); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 64<<10 || parsed > 16<<20 {
			return Config{}, errors.New("PEERGO_TORRENT_UPLOAD_MAX_BYTES must be between 65536 and 16777216")
		}
		torrentUploadMaxBytes = parsed
	}
	trackerCanonicalOrigin, err := required("PEERGO_TRACKER_CANONICAL_ORIGIN")
	if err != nil {
		return Config{}, err
	}
	parsedTrackerOrigin, err := url.Parse(trackerCanonicalOrigin)
	if err != nil || parsedTrackerOrigin.Scheme == "" || parsedTrackerOrigin.Host == "" || parsedTrackerOrigin.User != nil ||
		parsedTrackerOrigin.RawQuery != "" || parsedTrackerOrigin.Fragment != "" ||
		(parsedTrackerOrigin.Path != "" && parsedTrackerOrigin.Path != "/") {
		return Config{}, errors.New("PEERGO_TRACKER_CANONICAL_ORIGIN must be an absolute origin without user info, path, query or fragment")
	}
	if parsedTrackerOrigin.Scheme != "http" && parsedTrackerOrigin.Scheme != "https" {
		return Config{}, errors.New("PEERGO_TRACKER_CANONICAL_ORIGIN must use http or https")
	}
	if environment == "production" && parsedTrackerOrigin.Scheme != "https" {
		return Config{}, errors.New("PEERGO_TRACKER_CANONICAL_ORIGIN must use https in production")
	}
	trackerCanonicalOrigin = parsedTrackerOrigin.Scheme + "://" + parsedTrackerOrigin.Host
	trackerOperationsOrigin := strings.TrimSpace(os.Getenv("PEERGO_TRACKER_OPERATIONS_ORIGIN"))
	if trackerOperationsOrigin == "" {
		trackerOperationsOrigin = trackerCanonicalOrigin
	}
	parsedTrackerOperationsOrigin, err := url.Parse(trackerOperationsOrigin)
	if err != nil || parsedTrackerOperationsOrigin.Scheme == "" || parsedTrackerOperationsOrigin.Host == "" ||
		parsedTrackerOperationsOrigin.User != nil || parsedTrackerOperationsOrigin.RawQuery != "" ||
		parsedTrackerOperationsOrigin.Fragment != "" ||
		(parsedTrackerOperationsOrigin.Path != "" && parsedTrackerOperationsOrigin.Path != "/") {
		return Config{}, errors.New("PEERGO_TRACKER_OPERATIONS_ORIGIN must be an absolute origin without user info, path, query or fragment")
	}
	if parsedTrackerOperationsOrigin.Scheme != "http" && parsedTrackerOperationsOrigin.Scheme != "https" {
		return Config{}, errors.New("PEERGO_TRACKER_OPERATIONS_ORIGIN must use http or https")
	}
	if environment == "production" && parsedTrackerOperationsOrigin.Scheme != "https" {
		singleServer, modeErr := isSingleServerDeployment()
		if modeErr != nil {
			return Config{}, modeErr
		}
		if !singleServer || parsedTrackerOperationsOrigin.Scheme != "http" || parsedTrackerOperationsOrigin.Host != "tracker:8083" {
			return Config{}, errors.New("PEERGO_TRACKER_OPERATIONS_ORIGIN must use https, except http://tracker:8083 in single-server production")
		}
	}
	trackerOperationsOrigin = parsedTrackerOperationsOrigin.Scheme + "://" + parsedTrackerOperationsOrigin.Host
	trackerServiceToken, err := required("PEERGO_TRACKER_SERVICE_TOKEN")
	if err != nil {
		return Config{}, err
	}
	if len(trackerServiceToken) < 32 {
		return Config{}, errors.New("PEERGO_TRACKER_SERVICE_TOKEN must contain at least 32 bytes")
	}
	settlementControlURL, err := required("PEERGO_SETTLEMENT_CONTROL_URL")
	if err != nil {
		return Config{}, err
	}
	parsedSettlementURL, err := url.Parse(settlementControlURL)
	if err != nil || parsedSettlementURL.Scheme == "" || parsedSettlementURL.Host == "" || parsedSettlementURL.User != nil ||
		parsedSettlementURL.RawQuery != "" || parsedSettlementURL.Fragment != "" ||
		(parsedSettlementURL.Path != "" && parsedSettlementURL.Path != "/") {
		return Config{}, errors.New("PEERGO_SETTLEMENT_CONTROL_URL must be an absolute origin without user info, path, query or fragment")
	}
	if parsedSettlementURL.Scheme != "http" && parsedSettlementURL.Scheme != "https" {
		return Config{}, errors.New("PEERGO_SETTLEMENT_CONTROL_URL must use http or https")
	}
	if environment == "production" && parsedSettlementURL.Scheme != "https" {
		singleServer, modeErr := isSingleServerDeployment()
		if modeErr != nil {
			return Config{}, modeErr
		}
		if !singleServer || parsedSettlementURL.Scheme != "http" || parsedSettlementURL.Host != "settlement-control-api:8085" {
			return Config{}, errors.New("PEERGO_SETTLEMENT_CONTROL_URL must use https, except http://settlement-control-api:8085 in single-server production")
		}
	}
	settlementControlURL = parsedSettlementURL.Scheme + "://" + parsedSettlementURL.Host
	settlementServiceToken, err := required("PEERGO_SETTLEMENT_CONTROL_SERVICE_TOKEN")
	if err != nil {
		return Config{}, err
	}
	if len(settlementServiceToken) < 32 {
		return Config{}, errors.New("PEERGO_SETTLEMENT_CONTROL_SERVICE_TOKEN must contain at least 32 bytes")
	}
	seedingEvidenceStartRaw, err := required("PEERGO_SETTLEMENT_SEEDING_EVIDENCE_START_AT")
	if err != nil {
		return Config{}, err
	}
	seedingEvidenceStartAt, err := time.Parse(time.RFC3339, seedingEvidenceStartRaw)
	_, seedingEvidenceOffset := seedingEvidenceStartAt.Zone()
	if err != nil || seedingEvidenceOffset != 0 || !seedingEvidenceStartAt.Equal(seedingEvidenceStartAt.Truncate(time.Hour)) {
		return Config{}, errors.New("PEERGO_SETTLEMENT_SEEDING_EVIDENCE_START_AT must be an exact UTC RFC3339 hour")
	}
	seedingEvidenceClosureDelay, err := projectionDuration(
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE_CLOSURE_DELAY",
		time.Minute,
		time.Hour,
	)
	if err != nil {
		return Config{}, err
	}
	if seedingEvidenceClosureDelay%time.Second != 0 {
		return Config{}, errors.New("PEERGO_SETTLEMENT_SEEDING_EVIDENCE_CLOSURE_DELAY must use whole seconds")
	}
	turnstileSecretKey := strings.TrimSpace(os.Getenv("PEERGO_TURNSTILE_SECRET_KEY"))
	if len(turnstileSecretKey) > 256 || strings.ContainsAny(turnstileSecretKey, "\r\n") {
		return Config{}, errors.New("PEERGO_TURNSTILE_SECRET_KEY must contain at most 256 characters without line breaks")
	}

	address := strings.TrimSpace(os.Getenv("PEERGO_CORE_ADDR"))
	if address == "" {
		address = ":8080"
	}
	cookieName := "peergo_session"
	staffCookieName := "peergo_staff_session"
	if cookieSecure {
		// __Host- forbids Domain and requires Secure + Path=/, reducing cookie
		// injection from sibling hosts in production.
		cookieName = "__Host-peergo_session"
		// A staff cookie is intentionally scoped to /api/v1/admin, so it cannot
		// use the __Host- prefix (which requires Path=/). __Secure- still forces
		// transport security while the server enforces the credential audience.
		staffCookieName = "__Secure-peergo_staff_session"
	}
	return Config{
		Environment:                 environment,
		Address:                     address,
		DatabaseURL:                 databaseURL,
		VaultURL:                    vaultURL,
		VaultServiceToken:           vaultServiceToken,
		SessionCSRFKey:              []byte(csrfKey),
		AuditPseudonymKey:           []byte(auditPseudonymKey),
		AuditKeyEpoch:               auditKeyEpoch,
		CookieName:                  cookieName,
		StaffCookieName:             staffCookieName,
		CookieSecure:                cookieSecure,
		AllowedOrigins:              allowedOrigins,
		PublicOrigin:                publicOrigin,
		WebAuthnRPID:                webAuthnRPID,
		WebAuthnOrigins:             webAuthnOrigins,
		WebAuthnRecordKey:           []byte(webAuthnRecordKey),
		WebAuthnKeyEpoch:            webAuthnKeyEpoch,
		TorrentObjectStore:          torrentObjectStore,
		TorrentUploadMaxBytes:       torrentUploadMaxBytes,
		TrackerCanonicalOrigin:      trackerCanonicalOrigin,
		TrackerOperationsOrigin:     trackerOperationsOrigin,
		TrackerServiceToken:         trackerServiceToken,
		SettlementControlURL:        settlementControlURL,
		SettlementServiceToken:      settlementServiceToken,
		SeedingEvidenceStartAt:      seedingEvidenceStartAt,
		SeedingEvidenceClosureDelay: seedingEvidenceClosureDelay,
		TurnstileSecretKey:          turnstileSecretKey,
	}, nil
}

func parseOrigins(name, raw, environment string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("%s is required", name)
	}
	origins := make([]string, 0)
	seen := make(map[string]struct{})
	for _, candidate := range strings.Split(raw, ",") {
		parsed, err := url.Parse(strings.TrimSpace(candidate))
		if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
			return nil, fmt.Errorf("invalid origin %q in %s", candidate, name)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, fmt.Errorf("origin %q must use http or https", candidate)
		}
		if environment == "production" && parsed.Scheme != "https" {
			return nil, fmt.Errorf("origin %q must use https in production", candidate)
		}
		normalized := parsed.Scheme + "://" + parsed.Host
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		origins = append(origins, normalized)
	}
	if len(origins) == 0 {
		return nil, fmt.Errorf("%s must contain at least one origin", name)
	}
	return origins, nil
}

func required(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}
