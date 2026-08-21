package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadIncludesSeparatedStaffWebAuthnRuntimeBoundary(t *testing.T) {
	setValidCoreEnvironment(t)

	settings, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if settings.CookieName != "peergo_session" || settings.StaffCookieName != "peergo_staff_session" || settings.CookieSecure {
		t.Fatalf("development cookies = web:%q staff:%q secure:%t", settings.CookieName, settings.StaffCookieName, settings.CookieSecure)
	}
	if settings.WebAuthnRPID != "localhost" || len(settings.WebAuthnOrigins) != 1 || settings.WebAuthnOrigins[0] != "http://localhost:5173" {
		t.Fatalf("WebAuthn relying party settings = id:%q origins:%v", settings.WebAuthnRPID, settings.WebAuthnOrigins)
	}
	if len(settings.WebAuthnRecordKey) != 32 || settings.WebAuthnKeyEpoch != "test-2026-08" {
		t.Fatal("WebAuthn record-protection settings were not loaded exactly")
	}
	if settings.TorrentObjectStore.BackendID != "local-primary" || settings.TorrentObjectStore.Driver != "filesystem" || settings.TorrentUploadMaxBytes != 4<<20 {
		t.Fatalf("torrent storage settings = %+v max=%d", settings.TorrentObjectStore, settings.TorrentUploadMaxBytes)
	}
	if settings.TrackerCanonicalOrigin != "http://tracker.localhost:8083" {
		t.Fatalf("Tracker canonical origin = %q", settings.TrackerCanonicalOrigin)
	}
	if settings.TrackerOperationsOrigin != "http://tracker.localhost:8083" {
		t.Fatalf("Tracker operations origin = %q", settings.TrackerOperationsOrigin)
	}
	if want := time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC); !settings.SeedingEvidenceStartAt.Equal(want) {
		t.Fatalf("seeding evidence start = %s, want %s", settings.SeedingEvidenceStartAt, want)
	}
}

func TestLoadRejectsNonHourlySeedingEvidenceStart(t *testing.T) {
	setValidCoreEnvironment(t)
	t.Setenv("PEERGO_SETTLEMENT_SEEDING_EVIDENCE_START_AT", "2026-08-15T00:00:01Z")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "exact UTC RFC3339 hour") {
		t.Fatalf("Load() error = %v, want exact UTC hour failure", err)
	}
}

func TestLoadRejectsWebAuthnOriginOutsideWebOriginAllowlist(t *testing.T) {
	setValidCoreEnvironment(t)
	t.Setenv("PEERGO_WEBAUTHN_ORIGINS", "http://localhost:4173")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "must also appear in PEERGO_WEB_ORIGINS") {
		t.Fatalf("Load() error = %v, want WebAuthn origin subset failure", err)
	}
}

func TestLoadRejectsInvalidWebAuthnProtectionKey(t *testing.T) {
	setValidCoreEnvironment(t)
	t.Setenv("PEERGO_WEBAUTHN_RECORD_KEY", "too-short")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "exactly 32 bytes") {
		t.Fatalf("Load() error = %v, want exact key length failure", err)
	}
}

func TestLoadRejectsTorrentUploadLimitOutsideParserBudget(t *testing.T) {
	setValidCoreEnvironment(t)
	t.Setenv("PEERGO_TORRENT_UPLOAD_MAX_BYTES", "16777217")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "between 65536 and 16777216") {
		t.Fatalf("Load() error = %v, want torrent upload byte-limit failure", err)
	}
}

func TestLoadKeepsTurnstileSecretInRuntimeConfiguration(t *testing.T) {
	setValidCoreEnvironment(t)
	t.Setenv("PEERGO_TURNSTILE_SECRET_KEY", "turnstile-server-secret")

	settings, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if settings.TurnstileSecretKey != "turnstile-server-secret" {
		t.Fatal("Turnstile secret was not loaded exactly")
	}
}

func TestLoadUsesSecureCookiePrefixesInProduction(t *testing.T) {
	setValidCoreEnvironment(t)
	t.Setenv("PEERGO_ENV", "production")
	t.Setenv("PEERGO_VAULT_URL", "https://vault.internal.example")
	t.Setenv("PEERGO_WEB_ORIGINS", "https://peergo.example")
	t.Setenv("PEERGO_PUBLIC_ORIGIN", "https://peergo.example")
	t.Setenv("PEERGO_WEBAUTHN_RP_ID", "peergo.example")
	t.Setenv("PEERGO_WEBAUTHN_ORIGINS", "https://peergo.example")
	t.Setenv("PEERGO_TRACKER_CANONICAL_ORIGIN", "https://tracker.peergo.example")
	t.Setenv("PEERGO_COOKIE_SECURE", "true")

	settings, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if settings.CookieName != "__Host-peergo_session" || settings.StaffCookieName != "__Secure-peergo_staff_session" {
		t.Fatalf("production cookies = web:%q staff:%q", settings.CookieName, settings.StaffCookieName)
	}
}

func TestLoadAllowsOnlyFixedSingleServerServiceOrigins(t *testing.T) {
	setValidCoreEnvironment(t)
	t.Setenv("PEERGO_ENV", "production")
	t.Setenv("PEERGO_DEPLOYMENT_MODE", "single-server")
	t.Setenv("PEERGO_CORE_DATABASE_URL", "postgres://peergo_core:secret@postgresql:5432/peergo_core?sslmode=disable")
	t.Setenv("PEERGO_VAULT_URL", "http://vault-api:8081")
	t.Setenv("PEERGO_SETTLEMENT_CONTROL_URL", "http://settlement-control-api:8085")
	t.Setenv("PEERGO_WEB_ORIGINS", "https://rousi.pro")
	t.Setenv("PEERGO_PUBLIC_ORIGIN", "https://rousi.pro")
	t.Setenv("PEERGO_WEBAUTHN_RP_ID", "rousi.pro")
	t.Setenv("PEERGO_WEBAUTHN_ORIGINS", "https://rousi.pro")
	t.Setenv("PEERGO_TRACKER_CANONICAL_ORIGIN", "https://rousi.pro")
	t.Setenv("PEERGO_TRACKER_OPERATIONS_ORIGIN", "http://tracker:8083")
	t.Setenv("PEERGO_COOKIE_SECURE", "true")
	settings, err := Load()
	if err != nil {
		t.Fatalf("single-server service origins rejected: %v", err)
	}
	if settings.TrackerCanonicalOrigin != "https://rousi.pro" || settings.TrackerOperationsOrigin != "http://tracker:8083" {
		t.Fatalf("single-server Tracker origins = public:%q operations:%q", settings.TrackerCanonicalOrigin, settings.TrackerOperationsOrigin)
	}
	t.Setenv("PEERGO_VAULT_URL", "http://vault-api.evil:8081")
	if _, err := Load(); err == nil {
		t.Fatal("single-server accepted a non-fixed Vault host")
	}
	t.Setenv("PEERGO_VAULT_URL", "http://vault-api:8081")
	t.Setenv("PEERGO_TRACKER_OPERATIONS_ORIGIN", "http://tracker.evil:8083")
	if _, err := Load(); err == nil {
		t.Fatal("single-server accepted a non-fixed Tracker operations host")
	}
}

func setValidCoreEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("PEERGO_ENV", "development")
	t.Setenv("PEERGO_CORE_ADDR", "")
	t.Setenv("PEERGO_CORE_DATABASE_URL", "postgres://core.example/peergo")
	t.Setenv("PEERGO_VAULT_URL", "http://127.0.0.1:8081")
	t.Setenv("PEERGO_VAULT_SERVICE_TOKEN", "peergo-test-vault-service-token-2026")
	t.Setenv("PEERGO_TRACKER_SERVICE_TOKEN", "peergo-test-tracker-service-token-2026")
	t.Setenv("PEERGO_SETTLEMENT_CONTROL_URL", "https://settlement.example")
	t.Setenv("PEERGO_SETTLEMENT_CONTROL_SERVICE_TOKEN", "peergo-test-settlement-control-token-2026")
	t.Setenv("PEERGO_SETTLEMENT_SEEDING_EVIDENCE_START_AT", "2026-08-15T00:00:00Z")
	t.Setenv("PEERGO_SESSION_CSRF_KEY", "peergo-test-session-csrf-key-2026")
	t.Setenv("PEERGO_AUDIT_PSEUDONYM_KEY", "peergo-test-audit-pseudonym-key-2026")
	t.Setenv("PEERGO_AUDIT_PSEUDONYM_KEY_EPOCH", "test-2026-08")
	t.Setenv("PEERGO_COOKIE_SECURE", "false")
	t.Setenv("PEERGO_WEB_ORIGINS", "http://localhost:5173,http://127.0.0.1:5173")
	t.Setenv("PEERGO_PUBLIC_ORIGIN", "http://localhost:5173")
	t.Setenv("PEERGO_WEBAUTHN_RP_ID", "localhost")
	t.Setenv("PEERGO_WEBAUTHN_ORIGINS", "http://localhost:5173")
	t.Setenv("PEERGO_WEBAUTHN_RECORD_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("PEERGO_WEBAUTHN_KEY_EPOCH", "test-2026-08")
	t.Setenv("PEERGO_TORRENT_STORAGE_BACKEND_ID", "local-primary")
	t.Setenv("PEERGO_TORRENT_STORAGE_DRIVER", "filesystem")
	t.Setenv("PEERGO_TORRENT_STORAGE_FILESYSTEM_ROOT", filepath.Join(t.TempDir(), "torrent-objects"))
	t.Setenv("PEERGO_TORRENT_UPLOAD_MAX_BYTES", "")
	t.Setenv("PEERGO_TRACKER_CANONICAL_ORIGIN", "http://tracker.localhost:8083")
	t.Setenv("PEERGO_TRACKER_OPERATIONS_ORIGIN", "")
}
