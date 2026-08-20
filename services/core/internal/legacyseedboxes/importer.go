// Package legacyseedboxes imports the reviewed PtYes box registry into one
// run-bound Tracker policy revision. Raw socket addresses never leave Tracker,
// but the signed runtime policy must retain the exact user-bound prefixes that
// make upload and download accounting reproducible.
package legacyseedboxes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/contracts/go/trackerruntimepolicyv1"
	"github.com/peergo/peergo/services/core/internal/modules/trackercontrol"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

const (
	UploadFactorBasisPoints    = 5_000
	DownloadFactorBasisPoints  = 20_000
	legacyDefaultStandardMbps  = 50
	legacyMbpsToBytesPerSecond = 1024 * 1024 / 8
)

var sourceSettingKeys = []string{
	"seedbox.enabled",
	"seedbox.max_speed",
	"seedbox.non_seedbox_max_speed",
	"seedbox.upload_ratio",
	"seedbox.uploader_max_speed",
	"seedbox.uploader_upload_ratio",
	"seedbox.warning_limit",
	"vip.no_speed_limit",
	"vip.seedbox_no_discount",
}

var errLegacyNetmask = errors.New("legacy CIDR value is an IPv4 netmask")

type Config struct {
	RunID          uuid.UUID
	SnapshotSHA256 [sha256.Size]byte
	MappingVersion string
	ImportedAt     time.Time
}

type Result struct {
	RunID                            uuid.UUID
	SourceRows                       int64
	EnabledRows                      int64
	BindingRows                      int64
	PolicySequence                   int64
	PolicyRevision                   string
	StandardSpeedLimitBytesPerSecond int64
	Duplicate                        bool
}

type sourceRow struct {
	LegacyID  int64     `json:"legacy_id"`
	UserID    int64     `json:"user_id"`
	IPStart   string    `json:"ip_start"`
	IPEnd     string    `json:"ip_end"`
	IP        string    `json:"ip"`
	CIDR      string    `json:"cidr"`
	Operator  string    `json:"operator"`
	Bandwidth string    `json:"bandwidth"`
	Comment   string    `json:"comment"`
	Type      int16     `json:"type"`
	Status    int16     `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type userMapping struct {
	UserID    uuid.UUID
	NumericID int64
}

type binding struct {
	LegacyID          int64
	Kind              string
	UserID            uuid.UUID
	UserNumericID     int64
	Network           string
	RuleID            string
	SourceFingerprint [sha256.Size]byte
}

type sourceEvidence struct {
	Rows     []sourceRow       `json:"rows"`
	Settings map[string]string `json:"settings"`
}

// Import is safe to replay. Its receipt, all bindings and the appended policy
// commit in one Core transaction, so a crash cannot publish a half registry.
func Import(ctx context.Context, source, core *pgxpool.Pool, config Config) (Result, error) {
	config.ImportedAt = config.ImportedAt.UTC().Truncate(time.Microsecond)
	if source == nil || core == nil || config.RunID == uuid.Nil ||
		config.SnapshotSHA256 == ([sha256.Size]byte{}) || config.MappingVersion == "" || config.ImportedAt.IsZero() {
		return Result{}, errors.New("legacy seedbox import configuration is invalid")
	}
	if err := platformpostgres.RequireCurrentMigration(ctx, core); err != nil {
		return Result{}, err
	}
	if err := requireRun(ctx, core, config); err != nil {
		return Result{}, err
	}
	rows, settings, evidenceSHA, err := readSource(ctx, source)
	if err != nil {
		return Result{}, err
	}
	mappings, err := readUserMappings(ctx, core, config.RunID)
	if err != nil {
		return Result{}, err
	}
	bindings, enabledRows, err := buildBindings(rows, mappings)
	if err != nil {
		return Result{}, err
	}
	if !strings.EqualFold(strings.TrimSpace(settings["seedbox.enabled"]), "true") {
		return Result{}, errors.New("PtYes seedbox registry is not enabled in the immutable source")
	}
	standardSpeedLimit, err := legacyStandardSpeedLimit(settings)
	if err != nil {
		return Result{}, err
	}

	result := Result{
		RunID: config.RunID, SourceRows: int64(len(rows)), EnabledRows: enabledRows,
		BindingRows: int64(len(bindings)), PolicyRevision: policyRevision(config.RunID),
		StandardSpeedLimitBytesPerSecond: standardSpeedLimit,
	}
	if existing, exists, err := readReceipt(ctx, core, config.RunID); err != nil {
		return Result{}, err
	} else if exists {
		if err := compareReceipt(existing, result, config.SnapshotSHA256, evidenceSHA); err != nil {
			return Result{}, err
		}
		verified, err := Verify(ctx, source, core, config)
		if err != nil {
			return Result{}, err
		}
		verified.Duplicate = true
		return verified, nil
	}

	repository, err := trackercontrol.NewPostgresRuntimePolicyRepository(core)
	if err != nil {
		return Result{}, err
	}
	latest, err := repository.LatestRuntimePolicy(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("read current Tracker runtime policy: %w", err)
	}
	if len(latest.Policy.Seedbox.Rules) != 0 {
		return Result{}, errors.New("target Tracker policy already contains seedbox rules; refusing to replace them during legacy import")
	}
	policy := latest.Policy
	policy.Revision = result.PolicyRevision
	policy.Seedbox = trackerruntimepolicyv1.SeedboxPolicy{
		Enabled:                          true,
		UploadFactorBasisPoints:          UploadFactorBasisPoints,
		DownloadFactorBasisPoints:        DownloadFactorBasisPoints,
		SeedboxSpeedLimitBytesPerSecond:  0,
		StandardSpeedLimitBytesPerSecond: result.StandardSpeedLimitBytesPerSecond,
		Rules:                            make([]trackerruntimepolicyv1.SeedboxRule, 0, len(bindings)),
	}
	for _, item := range bindings {
		policy.Seedbox.Rules = append(policy.Seedbox.Rules, trackerruntimepolicyv1.SeedboxRule{
			ID: item.RuleID, CIDR: item.Network, UserNumericID: item.UserNumericID,
		})
	}
	policy, err = trackerruntimepolicyv1.NormalizePolicy(policy)
	if err != nil {
		return Result{}, fmt.Errorf("normalize migrated Tracker seedbox policy: %w", err)
	}

	tx, err := core.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Result{}, fmt.Errorf("begin legacy seedbox import: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('peergo-tracker-runtime-policy-v1', 0))`); err != nil {
		return Result{}, fmt.Errorf("lock Tracker runtime policy timeline: %w", err)
	}
	var currentSequence int64
	if err := tx.QueryRow(ctx, `SELECT sequence FROM tracker_control.runtime_policy_revisions ORDER BY sequence DESC LIMIT 1`).Scan(&currentSequence); err != nil {
		return Result{}, fmt.Errorf("recheck Tracker runtime policy sequence: %w", err)
	}
	if currentSequence != latest.Sequence {
		return Result{}, errors.New("Tracker runtime policy changed during legacy seedbox import")
	}
	allowedClients, err := json.Marshal(policy.AllowedClients)
	if err != nil {
		return Result{}, err
	}
	seedboxPolicy, err := json.Marshal(policy.Seedbox)
	if err != nil {
		return Result{}, err
	}
	createdAt := config.ImportedAt
	if !createdAt.After(latest.CreatedAt) {
		createdAt = latest.CreatedAt.Add(time.Microsecond)
	}
	if err := tx.QueryRow(ctx, `
INSERT INTO tracker_control.runtime_policy_revisions (
    revision, announce_interval_seconds, min_announce_interval_seconds,
    default_numwant, max_numwant, scrape_enabled, max_scrape_hashes,
    client_mode, allowed_clients, user_requests_per_minute, user_burst,
    address_requests_per_minute, address_burst, seedbox_policy, issued_by,
    authorization_decision_id, reason, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,$12,$13,$14::jsonb,
          NULL,NULL,$15,$16)
RETURNING sequence`, policy.Revision, policy.AnnounceIntervalSeconds,
		policy.MinAnnounceIntervalSeconds, policy.DefaultNumWant, policy.MaxNumWant,
		policy.ScrapeEnabled, policy.MaxScrapeHashes, string(policy.ClientMode), allowedClients,
		policy.UserRequestsPerMinute, policy.UserBurst, policy.AddressRequestsPerMinute,
		policy.AddressBurst, seedboxPolicy,
		"从不可变 Rousi 快照迁入用户绑定盒子规则；上传按 0.5x、下载按 2x，优惠正常叠加且不启用速度限制",
		createdAt).Scan(&result.PolicySequence); err != nil {
		return Result{}, fmt.Errorf("append migrated Tracker seedbox policy: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO migration.legacy_seedbox_imports (
    run_id, source_snapshot_sha256, source_rows, enabled_rows, binding_rows,
    source_evidence_sha256, policy_sequence, policy_revision,
    upload_factor_basis_points, download_factor_basis_points,
    seedbox_speed_limit_bytes_per_second, standard_speed_limit_bytes_per_second,
    imported_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, config.RunID,
		config.SnapshotSHA256[:], result.SourceRows, result.EnabledRows, result.BindingRows,
		evidenceSHA[:], result.PolicySequence, result.PolicyRevision,
		UploadFactorBasisPoints, DownloadFactorBasisPoints, int64(0),
		result.StandardSpeedLimitBytesPerSecond, createdAt); err != nil {
		return Result{}, fmt.Errorf("insert legacy seedbox import receipt: %w", err)
	}
	for _, item := range bindings {
		if _, err := tx.Exec(ctx, `
INSERT INTO migration.legacy_seedbox_bindings (
    run_id, legacy_seedbox_id, binding_kind, user_id, user_numeric_id,
    network, rule_id, source_fingerprint, policy_sequence, imported_at
) VALUES ($1,$2,$3,$4,$5,$6::cidr,$7,$8,$9,$10)`, config.RunID,
			item.LegacyID, item.Kind, item.UserID, item.UserNumericID, item.Network,
			item.RuleID, item.SourceFingerprint[:], result.PolicySequence, createdAt); err != nil {
			return Result{}, fmt.Errorf("insert legacy seedbox binding %d/%s: %w", item.LegacyID, item.Kind, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit legacy seedbox import: %w", err)
	}
	return result, nil
}

func requireRun(ctx context.Context, core *pgxpool.Pool, config Config) error {
	var snapshot []byte
	var mappingVersion, state string
	if err := core.QueryRow(ctx, `
SELECT source_snapshot_sha256, mapping_version, state
FROM migration.runs WHERE id=$1`, config.RunID).Scan(&snapshot, &mappingVersion, &state); err != nil {
		return fmt.Errorf("read legacy migration run for seedboxes: %w", err)
	}
	if !bytes.Equal(snapshot, config.SnapshotSHA256[:]) || mappingVersion != config.MappingVersion ||
		(state != "importing" && state != "imported" && state != "reconciled") {
		return errors.New("legacy seedbox import does not match an active or reconciled migration run")
	}
	return nil
}

func readSource(ctx context.Context, source *pgxpool.Pool) ([]sourceRow, map[string]string, [sha256.Size]byte, error) {
	rows, err := source.Query(ctx, `
SELECT id, COALESCE(user_id,0), COALESCE(ip_start,''), COALESCE(ip_end,''),
       COALESCE(ip,''), COALESCE(c_id_r,''), COALESCE(operator,''),
       COALESCE(bandwidth,''), COALESCE(comment,''), COALESCE(type,0),
       COALESCE(status,0), created_at, updated_at
FROM public.seed_boxes
ORDER BY id`)
	if err != nil {
		return nil, nil, [sha256.Size]byte{}, fmt.Errorf("query PtYes seedboxes: %w", err)
	}
	defer rows.Close()
	result := make([]sourceRow, 0)
	for rows.Next() {
		var item sourceRow
		if err := rows.Scan(&item.LegacyID, &item.UserID, &item.IPStart, &item.IPEnd,
			&item.IP, &item.CIDR, &item.Operator, &item.Bandwidth, &item.Comment,
			&item.Type, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, nil, [sha256.Size]byte{}, fmt.Errorf("scan PtYes seedbox: %w", err)
		}
		item.CreatedAt = item.CreatedAt.UTC().Round(0)
		item.UpdatedAt = item.UpdatedAt.UTC().Round(0)
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, [sha256.Size]byte{}, fmt.Errorf("read PtYes seedboxes: %w", err)
	}
	settings := make(map[string]string, len(sourceSettingKeys))
	settingRows, err := source.Query(ctx, `
SELECT key, value
FROM public.site_settings
WHERE key = ANY($1::text[])
ORDER BY key`, sourceSettingKeys)
	if err != nil {
		return nil, nil, [sha256.Size]byte{}, fmt.Errorf("query PtYes seedbox settings: %w", err)
	}
	defer settingRows.Close()
	for settingRows.Next() {
		var key, value string
		if err := settingRows.Scan(&key, &value); err != nil {
			return nil, nil, [sha256.Size]byte{}, fmt.Errorf("scan PtYes seedbox setting: %w", err)
		}
		settings[key] = value
	}
	if err := settingRows.Err(); err != nil {
		return nil, nil, [sha256.Size]byte{}, fmt.Errorf("read PtYes seedbox settings: %w", err)
	}
	encoded, err := json.Marshal(sourceEvidence{Rows: result, Settings: settings})
	if err != nil {
		return nil, nil, [sha256.Size]byte{}, err
	}
	return result, settings, sha256.Sum256(encoded), nil
}

func readUserMappings(ctx context.Context, core *pgxpool.Pool, runID uuid.UUID) (map[int64]userMapping, error) {
	rows, err := core.Query(ctx, `
SELECT mapping.legacy_user_id, mapping.user_id, users.numeric_id
FROM migration.user_id_map AS mapping
JOIN identity.users AS users ON users.id = mapping.user_id
WHERE mapping.source_system='ptyes' AND mapping.first_run_id=$1
ORDER BY mapping.legacy_user_id`, runID)
	if err != nil {
		return nil, fmt.Errorf("query migrated users for seedboxes: %w", err)
	}
	defer rows.Close()
	result := make(map[int64]userMapping)
	for rows.Next() {
		var legacyID int64
		var mapping userMapping
		if err := rows.Scan(&legacyID, &mapping.UserID, &mapping.NumericID); err != nil {
			return nil, fmt.Errorf("scan migrated user for seedboxes: %w", err)
		}
		result[legacyID] = mapping
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read migrated users for seedboxes: %w", err)
	}
	return result, nil
}

func buildBindings(rows []sourceRow, mappings map[int64]userMapping) ([]binding, int64, error) {
	result := make([]binding, 0, len(rows))
	seen := make(map[string]struct{})
	var enabled int64
	for _, row := range rows {
		if row.LegacyID < 1 || row.Status < 1 || row.Status > 3 {
			return nil, 0, fmt.Errorf("PtYes seedbox %d is invalid", row.LegacyID)
		}
		if row.Status != 1 {
			continue
		}
		enabled++
		mapping, ok := mappings[row.UserID]
		if !ok || mapping.UserID == uuid.Nil || mapping.NumericID < 1 {
			return nil, 0, fmt.Errorf("PtYes seedbox %d has no migrated user mapping", row.LegacyID)
		}
		primaryAddress := strings.TrimSpace(row.IP)
		if primaryAddress == "" {
			start, startErr := canonicalIPAddress(row.IPStart)
			end, endErr := canonicalIPAddress(row.IPEnd)
			if startErr != nil || endErr != nil || start != end {
				return nil, 0, fmt.Errorf("PtYes seedbox %d contains an unsupported address range", row.LegacyID)
			}
			primaryAddress = start
		}
		candidates := []struct{ kind, value string }{{"ip", primaryAddress}, {"cidr", row.CIDR}}
		added := 0
		for _, candidate := range candidates {
			if strings.TrimSpace(candidate.value) == "" {
				continue
			}
			network, err := canonicalNetwork(candidate.kind, candidate.value)
			if errors.Is(err, errLegacyNetmask) && added > 0 {
				// Some PtYes rows placed a dotted netmask in c_id_r. PtYes's
				// net.ParseCIDR lookup ignored it too; the exact IP remains the
				// authoritative binding and the raw value stays in source evidence.
				continue
			}
			if err != nil {
				return nil, 0, fmt.Errorf("PtYes seedbox %d %s is invalid: %w", row.LegacyID, candidate.kind, err)
			}
			key := strconv.FormatInt(mapping.NumericID, 10) + "\x00" + network
			if _, duplicate := seen[key]; duplicate {
				return nil, 0, fmt.Errorf("PtYes seedbox %d duplicates a user-bound network", row.LegacyID)
			}
			seen[key] = struct{}{}
			fingerprint := bindingFingerprint(row, candidate.kind, network)
			result = append(result, binding{
				LegacyID: row.LegacyID, Kind: candidate.kind, UserID: mapping.UserID,
				UserNumericID: mapping.NumericID, Network: network,
				RuleID:            fmt.Sprintf("rousi-sb-%d-%s", row.LegacyID, candidate.kind),
				SourceFingerprint: fingerprint,
			})
			added++
		}
		if added == 0 {
			return nil, 0, fmt.Errorf("PtYes seedbox %d has no usable address", row.LegacyID)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Network == result[j].Network {
			return result[i].UserNumericID < result[j].UserNumericID
		}
		return result[i].Network < result[j].Network
	})
	return result, enabled, nil
}

func canonicalNetwork(kind, value string) (string, error) {
	if kind == "ip" {
		address, err := canonicalIPAddress(value)
		if err != nil {
			return "", err
		}
		parsed := netip.MustParseAddr(address)
		return netip.PrefixFrom(parsed, parsed.BitLen()).String(), nil
	}
	if kind != "cidr" {
		return "", errors.New("unknown address binding kind")
	}
	value = strings.TrimSpace(value)
	if !strings.Contains(value, "/") {
		address, err := netip.ParseAddr(value)
		if err != nil {
			return "", errors.New("CIDR field is neither an IP nor prefix")
		}
		address = address.Unmap()
		if isIPv4Netmask(address) {
			return "", errLegacyNetmask
		}
		return netip.PrefixFrom(address, address.BitLen()).String(), nil
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return "", errors.New("CIDR prefix is invalid")
	}
	address := prefix.Addr()
	bits := prefix.Bits()
	if address.Is4In6() {
		address = address.Unmap()
		bits -= 96
		if bits < 0 {
			return "", errors.New("mapped IPv4 prefix length is invalid")
		}
	}
	return netip.PrefixFrom(address, bits).Masked().String(), nil
}

func canonicalIPAddress(value string) (string, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "http://")
	value = strings.TrimPrefix(value, "https://")
	address, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return "", errors.New("IP address is invalid")
	}
	return address.Unmap().String(), nil
}

func isIPv4Netmask(address netip.Addr) bool {
	if !address.Is4() {
		return false
	}
	bytes := address.As4()
	seenZero := false
	seenOne := false
	for _, octet := range bytes {
		for bit := 7; bit >= 0; bit-- {
			one := octet&(1<<bit) != 0
			if one {
				seenOne = true
				if seenZero {
					return false
				}
			} else {
				seenZero = true
			}
		}
	}
	return seenOne && seenZero
}

func bindingFingerprint(row sourceRow, kind, network string) [sha256.Size]byte {
	encoded, _ := json.Marshal(struct {
		Row     sourceRow `json:"row"`
		Kind    string    `json:"kind"`
		Network string    `json:"network"`
	}{Row: row, Kind: kind, Network: network})
	return sha256.Sum256(encoded)
}

func policyRevision(runID uuid.UUID) string {
	return "tracker-runtime-rousi-sb-" + strings.ReplaceAll(runID.String(), "-", "")
}

func legacyStandardSpeedLimit(settings map[string]string) (int64, error) {
	raw, ok := settings["seedbox.non_seedbox_max_speed"]
	if !ok {
		return 0, errors.New("PtYes non-seedbox speed setting is missing from the immutable source")
	}
	megabitsPerSecond, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, errors.New("PtYes non-seedbox speed setting is invalid")
	}
	// PtYes treats zero and negative values as its historical 50 Mbps default.
	// Preserve that behavior for standard users while the migrated box tier is
	// explicitly unlimited. VIP intervals remain exempt in Settlement.
	if megabitsPerSecond <= 0 {
		megabitsPerSecond = legacyDefaultStandardMbps
	}
	if megabitsPerSecond > 1_000_000 {
		return 0, errors.New("PtYes non-seedbox speed setting exceeds the supported range")
	}
	return megabitsPerSecond * legacyMbpsToBytesPerSecond, nil
}

type receipt struct {
	Result
	SnapshotSHA256                  [sha256.Size]byte
	EvidenceSHA256                  [sha256.Size]byte
	UploadFactorBasisPoints         int
	DownloadFactorBasisPoints       int
	SeedboxSpeedLimitBytesPerSecond int64
}

func readReceipt(ctx context.Context, core *pgxpool.Pool, runID uuid.UUID) (receipt, bool, error) {
	var result receipt
	var snapshot, evidence []byte
	err := core.QueryRow(ctx, `
SELECT run_id, source_snapshot_sha256, source_rows, enabled_rows, binding_rows,
       source_evidence_sha256, policy_sequence, policy_revision,
       upload_factor_basis_points, download_factor_basis_points,
       seedbox_speed_limit_bytes_per_second, standard_speed_limit_bytes_per_second
FROM migration.legacy_seedbox_imports
WHERE run_id=$1`, runID).Scan(&result.RunID, &snapshot, &result.SourceRows,
		&result.EnabledRows, &result.BindingRows, &evidence, &result.PolicySequence,
		&result.PolicyRevision, &result.UploadFactorBasisPoints,
		&result.DownloadFactorBasisPoints, &result.SeedboxSpeedLimitBytesPerSecond,
		&result.StandardSpeedLimitBytesPerSecond)
	if errors.Is(err, pgx.ErrNoRows) {
		return receipt{}, false, nil
	}
	if err != nil {
		return receipt{}, false, fmt.Errorf("read legacy seedbox import receipt: %w", err)
	}
	if len(snapshot) != sha256.Size || len(evidence) != sha256.Size {
		return receipt{}, false, errors.New("legacy seedbox import receipt digest is invalid")
	}
	copy(result.SnapshotSHA256[:], snapshot)
	copy(result.EvidenceSHA256[:], evidence)
	return result, true, nil
}

func compareReceipt(existing receipt, expected Result, snapshot, evidence [sha256.Size]byte) error {
	if existing.RunID != expected.RunID || existing.SourceRows != expected.SourceRows ||
		existing.EnabledRows != expected.EnabledRows || existing.BindingRows != expected.BindingRows ||
		existing.PolicyRevision != expected.PolicyRevision || existing.SnapshotSHA256 != snapshot ||
		existing.EvidenceSHA256 != evidence || existing.PolicySequence < 1 ||
		existing.UploadFactorBasisPoints != UploadFactorBasisPoints ||
		existing.DownloadFactorBasisPoints != DownloadFactorBasisPoints ||
		existing.SeedboxSpeedLimitBytesPerSecond != 0 ||
		existing.StandardSpeedLimitBytesPerSecond != expected.StandardSpeedLimitBytesPerSecond {
		return errors.New("legacy seedbox import receipt conflicts with the immutable source")
	}
	return nil
}

// Verify rereads source, target bindings and the latest Core policy. It is used
// by both idempotent retries and the final cutover acceptance gate.
func Verify(ctx context.Context, source, core *pgxpool.Pool, config Config) (Result, error) {
	if source == nil || core == nil || config.RunID == uuid.Nil ||
		config.SnapshotSHA256 == ([sha256.Size]byte{}) || config.MappingVersion == "" {
		return Result{}, errors.New("legacy seedbox verification configuration is invalid")
	}
	if err := platformpostgres.RequireCurrentMigration(ctx, core); err != nil {
		return Result{}, err
	}
	if err := requireRun(ctx, core, config); err != nil {
		return Result{}, err
	}
	rows, settings, evidenceSHA, err := readSource(ctx, source)
	if err != nil {
		return Result{}, err
	}
	mappings, err := readUserMappings(ctx, core, config.RunID)
	if err != nil {
		return Result{}, err
	}
	bindings, enabledRows, err := buildBindings(rows, mappings)
	if err != nil {
		return Result{}, err
	}
	standardSpeedLimit, err := legacyStandardSpeedLimit(settings)
	if err != nil {
		return Result{}, err
	}
	expected := Result{
		RunID: config.RunID, SourceRows: int64(len(rows)), EnabledRows: enabledRows,
		BindingRows: int64(len(bindings)), PolicyRevision: policyRevision(config.RunID),
		StandardSpeedLimitBytesPerSecond: standardSpeedLimit,
	}
	receipt, exists, err := readReceipt(ctx, core, config.RunID)
	if err != nil {
		return Result{}, err
	}
	if !exists || compareReceipt(receipt, expected, config.SnapshotSHA256, evidenceSHA) != nil {
		return Result{}, errors.New("legacy seedbox import receipt does not reconcile")
	}
	expected.PolicySequence = receipt.PolicySequence

	repository, err := trackercontrol.NewPostgresRuntimePolicyRepository(core)
	if err != nil {
		return Result{}, err
	}
	latest, err := repository.LatestRuntimePolicy(ctx)
	if err != nil {
		return Result{}, err
	}
	if latest.Sequence != receipt.PolicySequence || latest.Policy.Revision != receipt.PolicyRevision ||
		!latest.Policy.Seedbox.Enabled || latest.Policy.Seedbox.UploadFactorBasisPoints != UploadFactorBasisPoints ||
		latest.Policy.Seedbox.DownloadFactorBasisPoints != DownloadFactorBasisPoints ||
		latest.Policy.Seedbox.SeedboxSpeedLimitBytesPerSecond != 0 ||
		latest.Policy.Seedbox.StandardSpeedLimitBytesPerSecond != expected.StandardSpeedLimitBytesPerSecond ||
		len(latest.Policy.Seedbox.Rules) != len(bindings) {
		return Result{}, errors.New("latest Tracker runtime policy does not match the migrated seedbox receipt")
	}
	expectedRules := make(map[string]trackerruntimepolicyv1.SeedboxRule, len(bindings))
	for _, item := range bindings {
		expectedRules[item.RuleID] = trackerruntimepolicyv1.SeedboxRule{
			ID: item.RuleID, CIDR: item.Network, UserNumericID: item.UserNumericID,
		}
	}
	for _, rule := range latest.Policy.Seedbox.Rules {
		if expectedRule, ok := expectedRules[rule.ID]; !ok || expectedRule != rule {
			return Result{}, errors.New("Tracker runtime policy contains an unexpected migrated seedbox rule")
		}
	}

	targetRows, err := core.Query(ctx, `
SELECT legacy_seedbox_id, binding_kind, user_id, user_numeric_id,
       network::text, rule_id, source_fingerprint
FROM migration.legacy_seedbox_bindings
WHERE run_id=$1
ORDER BY legacy_seedbox_id, binding_kind`, config.RunID)
	if err != nil {
		return Result{}, fmt.Errorf("query imported seedbox bindings: %w", err)
	}
	defer targetRows.Close()
	target := make(map[string]binding, len(bindings))
	for targetRows.Next() {
		var item binding
		var fingerprint []byte
		if err := targetRows.Scan(&item.LegacyID, &item.Kind, &item.UserID, &item.UserNumericID,
			&item.Network, &item.RuleID, &fingerprint); err != nil {
			return Result{}, fmt.Errorf("scan imported seedbox binding: %w", err)
		}
		if len(fingerprint) != sha256.Size {
			return Result{}, errors.New("imported seedbox binding fingerprint is invalid")
		}
		copy(item.SourceFingerprint[:], fingerprint)
		target[item.RuleID] = item
	}
	if err := targetRows.Err(); err != nil {
		return Result{}, fmt.Errorf("read imported seedbox bindings: %w", err)
	}
	if len(target) != len(bindings) {
		return Result{}, errors.New("imported seedbox binding count does not reconcile")
	}
	for _, item := range bindings {
		actual, ok := target[item.RuleID]
		if !ok || actual != item {
			return Result{}, errors.New("imported seedbox binding differs from the immutable source")
		}
	}
	return expected, nil
}
