// Command preflight verifies the deployment facts that must be true before
// PeerGo workers are activated. It is intentionally read-only: migrations,
// policy issuance and registration changes remain explicit operator actions.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/contracts/go/schemaversionv1"
)

const trackerMigrationVersion int64 = 202608230002

type checkStatus string

const (
	checkPass checkStatus = "pass"
	checkWarn checkStatus = "warn"
	checkFail checkStatus = "fail"
)

type checkResult struct {
	Name   string      `json:"name"`
	Status checkStatus `json:"status"`
	Detail string      `json:"detail"`
}

type report struct {
	Status      string        `json:"status"`
	GeneratedAt time.Time     `json:"generated_at"`
	Strict      bool          `json:"strict_policies"`
	Checks      []checkResult `json:"checks"`
}

type settings struct {
	CoreDatabaseURL          string
	VaultDatabaseURL         string
	TrackerDatabaseURL       string
	Timeout                  time.Duration
	StrictPolicies           bool
	ExpectedRegistrationMode string
	ExpectedNewcomerState    string
	RequireSettlementPolicy  bool
	RequireHNRPolicy         bool
	RequireEmailDelivery     bool
	VaultURL                 string
	VaultServiceToken        string
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	config, err := loadSettings()
	if err != nil {
		logger.Error("invalid preflight configuration", "error", err)
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()
	result := run(ctx, config)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		logger.Error("encode preflight report", "error", err)
		os.Exit(2)
	}
	if result.Status != "ready" {
		os.Exit(1)
	}
}

func loadSettings() (settings, error) {
	core, err := requiredDatabaseURL("PEERGO_CORE_DATABASE_URL")
	if err != nil {
		return settings{}, err
	}
	vault, err := requiredDatabaseURL("PEERGO_VAULT_DATABASE_URL")
	if err != nil {
		return settings{}, err
	}
	tracker, err := requiredDatabaseURL("PEERGO_TRACKER_DATABASE_URL")
	if err != nil {
		return settings{}, err
	}
	timeout := 20 * time.Second
	if raw := strings.TrimSpace(os.Getenv("PEERGO_PREFLIGHT_TIMEOUT")); raw != "" {
		timeout, err = time.ParseDuration(raw)
		if err != nil || timeout < time.Second || timeout > 2*time.Minute {
			return settings{}, errors.New("PEERGO_PREFLIGHT_TIMEOUT must be between 1s and 2m")
		}
	}
	strict, err := optionalBool("PEERGO_PREFLIGHT_STRICT_POLICIES", false)
	if err != nil {
		return settings{}, err
	}
	registrationMode := strings.TrimSpace(os.Getenv("PEERGO_PREFLIGHT_REGISTRATION_MODE"))
	if registrationMode != "" && registrationMode != "open" && registrationMode != "invite" && registrationMode != "closed" {
		return settings{}, errors.New("PEERGO_PREFLIGHT_REGISTRATION_MODE must be open, invite or closed")
	}
	newcomerState := strings.TrimSpace(os.Getenv("PEERGO_PREFLIGHT_NEWCOMER_STATE"))
	if newcomerState == "" {
		newcomerState = "any"
	}
	if newcomerState != "any" && newcomerState != "enabled" && newcomerState != "disabled" {
		return settings{}, errors.New("PEERGO_PREFLIGHT_NEWCOMER_STATE must be any, enabled or disabled")
	}
	settlementPolicy, err := optionalBool("PEERGO_PREFLIGHT_REQUIRE_SETTLEMENT_POLICY", strict)
	if err != nil {
		return settings{}, err
	}
	hnrPolicy, err := optionalBool("PEERGO_PREFLIGHT_REQUIRE_HNR_POLICY", strict)
	if err != nil {
		return settings{}, err
	}
	emailDelivery, err := optionalBool("PEERGO_PREFLIGHT_REQUIRE_EMAIL_DELIVERY", strict)
	if err != nil {
		return settings{}, err
	}
	var vaultURL, vaultServiceToken string
	if emailDelivery {
		vaultURL, err = requiredServiceOrigin("PEERGO_VAULT_URL")
		if err != nil {
			return settings{}, err
		}
		vaultServiceToken = strings.TrimSpace(os.Getenv("PEERGO_VAULT_SERVICE_TOKEN"))
		if len(vaultServiceToken) < 32 || strings.ContainsAny(vaultServiceToken, "\r\n") {
			return settings{}, errors.New("PEERGO_VAULT_SERVICE_TOKEN must contain at least 32 characters without line breaks when email delivery is required")
		}
	}
	if strict && registrationMode == "" {
		return settings{}, errors.New("PEERGO_PREFLIGHT_REGISTRATION_MODE is required when strict policy checks are enabled")
	}
	if strict && newcomerState == "any" {
		return settings{}, errors.New("PEERGO_PREFLIGHT_NEWCOMER_STATE must explicitly be enabled or disabled when strict policy checks are enabled")
	}
	return settings{
		CoreDatabaseURL: core, VaultDatabaseURL: vault, TrackerDatabaseURL: tracker,
		Timeout: timeout, StrictPolicies: strict,
		ExpectedRegistrationMode: registrationMode, ExpectedNewcomerState: newcomerState,
		RequireSettlementPolicy: settlementPolicy, RequireHNRPolicy: hnrPolicy,
		RequireEmailDelivery: emailDelivery, VaultURL: vaultURL, VaultServiceToken: vaultServiceToken,
	}, nil
}

func requiredServiceOrigin(name string) (string, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("%s must be an absolute HTTP(S) origin", name)
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

func requiredDatabaseURL(name string) (string, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "postgres" || parsed.Host == "" || strings.Trim(parsed.Path, "/") == "" {
		return "", fmt.Errorf("%s must be a PostgreSQL URL with a database name", name)
	}
	return raw, nil
}

func optionalBool(name string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return value, nil
}

func run(ctx context.Context, config settings) report {
	result := report{GeneratedAt: time.Now().UTC().Round(0), Strict: config.StrictPolicies}
	core, coreErr := openDatabase(ctx, config.CoreDatabaseURL)
	if coreErr != nil {
		result.Checks = append(result.Checks, failed("core_database", "Core database is unavailable"))
	} else {
		defer core.Close()
		result.Checks = append(result.Checks, migrationCheck(ctx, core, "core_migration", schemaversionv1.Core))
		result.Checks = append(result.Checks, registrationCheck(ctx, core, config.ExpectedRegistrationMode, config.StrictPolicies))
		result.Checks = append(result.Checks, newcomerCheck(ctx, core, config.ExpectedNewcomerState, config.StrictPolicies))
		// These baselines are installed by Core migrations, not optional
		// operator choices. Starting the reward workers without them would make
		// seeding and experience appear enabled while every settlement waits.
		result.Checks = append(result.Checks,
			policyExistsCheck(ctx, core, "seeding_reward_policy", "economy.seeding_reward_policy_revisions", "effective_from", true),
			policyExistsCheck(ctx, core, "attendance_reward_policy", "economy.attendance_policy_revisions", "effective_from", true),
			policyExistsCheck(ctx, core, "contribution_experience_policy", "progression.contribution_experience_policy_revisions", "effective_from", true),
			contributionExperienceAuthoritiesCheck(ctx, core),
			legacySeedboxCutoverCheck(ctx, core),
		)
	}

	vault, vaultErr := openDatabase(ctx, config.VaultDatabaseURL)
	if vaultErr != nil {
		result.Checks = append(result.Checks, failed("vault_database", "Privacy Vault database is unavailable"))
	} else {
		defer vault.Close()
		result.Checks = append(result.Checks, migrationCheck(ctx, vault, "vault_migration", schemaversionv1.PrivacyVault))
	}

	tracker, trackerErr := openDatabase(ctx, config.TrackerDatabaseURL)
	if trackerErr != nil {
		result.Checks = append(result.Checks, failed("tracker_database", "Tracker Ledger database is unavailable"))
	} else {
		defer tracker.Close()
		result.Checks = append(result.Checks, migrationCheck(ctx, tracker, "tracker_migration", trackerMigrationVersion))
		result.Checks = append(result.Checks,
			policyExistsCheck(ctx, tracker, "settlement_policy", "settlement.policy_timeline_revisions", "effective_at", config.RequireSettlementPolicy),
			policyExistsCheck(ctx, tracker, "hnr_policy", "settlement.hnr_policy_timeline_revisions", "effective_at", config.RequireHNRPolicy),
		)
	}

	if config.RequireEmailDelivery {
		client := &http.Client{
			Timeout: config.Timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		result.Checks = append(result.Checks, emailDeliveryCheck(
			ctx, client, config.VaultURL, config.VaultServiceToken,
		))
	}

	result.Status = "ready"
	for _, check := range result.Checks {
		if check.Status == checkFail {
			result.Status = "not_ready"
			break
		}
	}
	return result
}

func emailDeliveryCheck(ctx context.Context, client *http.Client, vaultOrigin, serviceToken string) checkResult {
	if client == nil {
		return failed("email_delivery", "Email delivery check has no HTTP client")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(vaultOrigin, "/")+"/internal/v1/operations/email", nil)
	if err != nil {
		return failed("email_delivery", "Privacy Vault email status request could not be created")
	}
	request.Header.Set("Authorization", "Bearer "+serviceToken)
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return failed("email_delivery", "Privacy Vault email status is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return failed("email_delivery", fmt.Sprintf("Privacy Vault email status returned HTTP %d", response.StatusCode))
	}
	var status struct {
		GeneratedAt                  time.Time       `json:"generated_at"`
		DeliveryMode                 string          `json:"delivery_mode"`
		VerificationPublicOrigin     string          `json:"verification_public_origin"`
		PasswordRecoveryPublicOrigin string          `json:"password_recovery_public_origin"`
		VerificationTTLSeconds       int64           `json:"verification_ttl_seconds"`
		PasswordRecoveryTTLSeconds   int64           `json:"password_recovery_ttl_seconds"`
		CooldownSeconds              int64           `json:"cooldown_seconds"`
		Templates                    []string        `json:"templates"`
		Stats                        json.RawMessage `json:"stats"`
	}
	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(&status); err != nil || status.GeneratedAt.IsZero() {
		return failed("email_delivery", "Privacy Vault returned an invalid email status document")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return failed("email_delivery", "Privacy Vault email status contains trailing data")
	}
	if status.DeliveryMode != "https_relay" {
		return failed("email_delivery", "Privacy Vault is not using the production HTTPS Relay")
	}
	if !securePublicOrigin(status.VerificationPublicOrigin) || !securePublicOrigin(status.PasswordRecoveryPublicOrigin) {
		return failed("email_delivery", "Email action links do not use public HTTPS origins")
	}
	if status.VerificationTTLSeconds <= 0 || status.PasswordRecoveryTTLSeconds <= 0 || status.CooldownSeconds <= 0 || len(status.Stats) == 0 {
		return failed("email_delivery", "Privacy Vault email lifetimes or delivery statistics are incomplete")
	}
	expectedTemplates := map[string]bool{
		"peergo-email-verification-v1": false,
		"peergo-password-recovery-v1":  false,
	}
	for _, template := range status.Templates {
		if _, exists := expectedTemplates[template]; !exists {
			return failed("email_delivery", "Privacy Vault reports an unexpected email template")
		}
		expectedTemplates[template] = true
	}
	for _, present := range expectedTemplates {
		if !present {
			return failed("email_delivery", "Privacy Vault is missing a required email template")
		}
	}
	return passed("email_delivery", "HTTPS Relay, public action origins and required templates are ready")
}

func securePublicOrigin(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil &&
		parsed.RawQuery == "" && parsed.Fragment == "" && (parsed.Path == "" || parsed.Path == "/")
}

func openDatabase(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func migrationCheck(ctx context.Context, database *pgxpool.Pool, name string, expected int64) checkResult {
	var actual int64
	err := database.QueryRow(ctx, `
		SELECT COALESCE(MAX(version_id), 0)
		FROM goose_db_version
		WHERE is_applied = true
	`).Scan(&actual)
	if err != nil {
		return failed(name, "Migration version could not be read")
	}
	if actual != expected {
		return failed(name, fmt.Sprintf("migration %d is installed; release requires %d", actual, expected))
	}
	return passed(name, fmt.Sprintf("migration %d is current", actual))
}

func registrationCheck(ctx context.Context, database *pgxpool.Pool, expected string, strict bool) checkResult {
	var actual string
	if err := database.QueryRow(ctx, `SELECT mode::text FROM identity.registration_policy WHERE singleton = true`).Scan(&actual); err != nil {
		return failed("registration_policy", "Registration policy could not be read")
	}
	if expected != "" && actual != expected {
		return failed("registration_policy", fmt.Sprintf("registration mode is %s; activation expects %s", actual, expected))
	}
	if expected == "" {
		status := checkWarn
		if strict {
			status = checkFail
		}
		return checkResult{Name: "registration_policy", Status: status, Detail: "registration mode is " + actual + "; no activation expectation was supplied"}
	}
	return passed("registration_policy", "registration mode is explicitly confirmed as "+actual)
}

func newcomerCheck(ctx context.Context, database *pgxpool.Pool, expected string, strict bool) checkResult {
	var enabled bool
	err := database.QueryRow(ctx, `
		SELECT enabled
		FROM newcomer.policy_revisions
		WHERE effective_at <= clock_timestamp()
		ORDER BY effective_at DESC, revision DESC
		LIMIT 1
	`).Scan(&enabled)
	if err != nil {
		return failed("newcomer_policy", "Current newcomer policy could not be read")
	}
	actual := "disabled"
	if enabled {
		actual = "enabled"
	}
	if expected != "any" && actual != expected {
		return failed("newcomer_policy", fmt.Sprintf("newcomer policy is %s; activation expects %s", actual, expected))
	}
	if expected == "any" {
		status := checkWarn
		if strict {
			status = checkFail
		}
		return checkResult{Name: "newcomer_policy", Status: status, Detail: "newcomer policy is " + actual + "; no activation expectation was supplied"}
	}
	return passed("newcomer_policy", "newcomer policy is explicitly confirmed as "+actual)
}

func policyExistsCheck(ctx context.Context, database *pgxpool.Pool, name, table, effectiveColumn string, required bool) checkResult {
	// Callers pass compile-time schema identifiers only. Keeping the effective
	// column explicit lets the same read-only check cover Core's
	// `effective_from` timelines and Settlement's `effective_at` timelines.
	query := `SELECT EXISTS (SELECT 1 FROM ` + table + ` WHERE ` + effectiveColumn + ` <= clock_timestamp())`
	var exists bool
	if err := database.QueryRow(ctx, query).Scan(&exists); err != nil {
		return failed(name, "Policy timeline could not be read")
	}
	if exists {
		return passed(name, "an effective policy revision exists")
	}
	if required {
		return failed(name, "no effective policy revision exists")
	}
	return checkResult{Name: name, Status: checkWarn, Detail: "no effective policy revision exists; related work will remain pending"}
}

func contributionExperienceAuthoritiesCheck(ctx context.Context, database *pgxpool.Pool) checkResult {
	// A contribution policy is only configuration. Runtime settlement also
	// needs two matching progression authorities so arbitrary callers cannot
	// mint experience under a policy label. The migration trigger creates both
	// atomically for every new contribution revision.
	var valid bool
	err := database.QueryRow(ctx, `
WITH current_policy AS (
    SELECT revision, effective_from, snapshot_sha256
    FROM progression.contribution_experience_policy_revisions
    WHERE effective_from <= clock_timestamp()
      AND created_at <= clock_timestamp()
    ORDER BY effective_from DESC, revision DESC
    LIMIT 1
)
SELECT count(*) = 2
FROM current_policy AS policy
JOIN progression.experience_policy_revisions AS authority
  ON authority.revision IN (
      policy.revision || '.publish',
      policy.revision || '.activity'
  )
 AND authority.effective_from = policy.effective_from
 AND authority.payload_sha256 = policy.snapshot_sha256
 AND authority.source_kind = CASE
      WHEN authority.revision = policy.revision || '.publish'
      THEN 'torrent_publish'
      ELSE 'activity'
 END`).Scan(&valid)
	if err != nil {
		return failed("contribution_experience_authorities", "Contribution experience authorities could not be read")
	}
	if !valid {
		return failed("contribution_experience_authorities", "Current contribution policy is missing its publish or activity authority")
	}
	return passed("contribution_experience_authorities", "publish and activity authorities match the current contribution policy")
}

func legacySeedboxCutoverCheck(ctx context.Context, database *pgxpool.Pool) checkResult {
	var reconciledRuns int64
	if err := database.QueryRow(ctx, `
SELECT count(*)::bigint
FROM migration.runs
WHERE source_system = 'ptyes' AND state = 'reconciled'`).Scan(&reconciledRuns); err != nil {
		return failed("legacy_seedbox_cutover", "Rousi migration state could not be read")
	}
	if reconciledRuns == 0 {
		return passed("legacy_seedbox_cutover", "no reconciled Rousi migration requires box bindings")
	}

	var sourceRows, enabledRows, expectedBindings, importedBindings int64
	var receiptUploadFactor, receiptDownloadFactor int
	var receiptSeedboxSpeedLimit, receiptStandardSpeedLimit int64
	var policyEnabled bool
	var policyUploadFactor, policyDownloadFactor int
	var policySeedboxSpeedLimit, policyStandardSpeedLimit int64
	var policyRules int64
	err := database.QueryRow(ctx, `
SELECT
    receipt.source_rows,
    receipt.enabled_rows,
    receipt.binding_rows,
    (SELECT count(*)::bigint
       FROM migration.legacy_seedbox_bindings AS binding
      WHERE binding.run_id = run.id),
    receipt.upload_factor_basis_points,
    receipt.download_factor_basis_points,
    receipt.seedbox_speed_limit_bytes_per_second,
    receipt.standard_speed_limit_bytes_per_second,
    (policy.seedbox_policy ->> 'enabled')::boolean,
    (policy.seedbox_policy ->> 'upload_factor_basis_points')::integer,
    (policy.seedbox_policy ->> 'download_factor_basis_points')::integer,
    (policy.seedbox_policy ->> 'seedbox_speed_limit_bytes_per_second')::bigint,
    (policy.seedbox_policy ->> 'standard_speed_limit_bytes_per_second')::bigint,
    jsonb_array_length(policy.seedbox_policy -> 'rules')::bigint
FROM migration.runs AS run
JOIN migration.legacy_seedbox_imports AS receipt ON receipt.run_id = run.id
JOIN tracker_control.runtime_policy_revisions AS policy
  ON policy.sequence = receipt.policy_sequence
WHERE run.source_system = 'ptyes' AND run.state = 'reconciled'
ORDER BY run.completed_at DESC, run.id DESC
LIMIT 1`).Scan(
		&sourceRows,
		&enabledRows,
		&expectedBindings,
		&importedBindings,
		&receiptUploadFactor,
		&receiptDownloadFactor,
		&receiptSeedboxSpeedLimit,
		&receiptStandardSpeedLimit,
		&policyEnabled,
		&policyUploadFactor,
		&policyDownloadFactor,
		&policySeedboxSpeedLimit,
		&policyStandardSpeedLimit,
		&policyRules,
	)
	if err != nil {
		return failed("legacy_seedbox_cutover", "Rousi box migration receipt is missing or unreadable")
	}
	if sourceRows < enabledRows || expectedBindings < enabledRows || importedBindings != expectedBindings ||
		receiptUploadFactor != 5_000 || receiptDownloadFactor != 20_000 || !policyEnabled ||
		policyUploadFactor != receiptUploadFactor || policyDownloadFactor != receiptDownloadFactor ||
		receiptSeedboxSpeedLimit != 0 || receiptStandardSpeedLimit <= 0 ||
		policySeedboxSpeedLimit != receiptSeedboxSpeedLimit ||
		policyStandardSpeedLimit != receiptStandardSpeedLimit || policyRules != expectedBindings {
		return failed("legacy_seedbox_cutover", "Rousi box bindings or 0.5x/2x unlimited-speed policy do not reconcile")
	}
	return passed(
		"legacy_seedbox_cutover",
		fmt.Sprintf("%d enabled Rousi boxes map to %d user-bound rules with 0.5x upload, 2x download and no box speed limit", enabledRows, expectedBindings),
	)
}

func passed(name, detail string) checkResult {
	return checkResult{Name: name, Status: checkPass, Detail: detail}
}

func failed(name, detail string) checkResult {
	return checkResult{Name: name, Status: checkFail, Detail: detail}
}
