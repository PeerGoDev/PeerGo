// Package legacyuseradmin imports the two PtYes user-administration facts
// that were intentionally absent from the original Core cutover: donation
// totals and bounded IP aggregates.  It never copies request/User-Agent rows.
package legacyuseradmin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/platform/postgres"
)

const (
	retentionDays             = 180
	maximumAddressesPerUser   = 20
	donationFingerprintDomain = "peergo:migration:ptyes-user-donation:v1\x00"
	evidenceDomain            = "peergo:migration:ptyes-user-administration:v1\x00"
)

type Config struct {
	RunID      uuid.UUID
	ImportedAt time.Time
}

type Result struct {
	RunID               uuid.UUID
	ObservedAt          time.Time
	DonationSourceRows  int64
	PositiveDonors      int64
	DonationTotal       string
	NetworkSourceRows   int64
	RetainedNetworkRows int64
	Duplicate           bool
}

type donationRow struct {
	LegacyUserID int64
	Amount       string
	Fingerprint  [sha256.Size]byte
}

type networkRow struct {
	LegacyUserID int64
	Address      string
	FirstSeenAt  time.Time
	LastSeenAt   time.Time
	SeenCount    int64
}

type sourceState struct {
	ObservedAt        time.Time
	Donations         []donationRow
	Networks          []networkRow
	PositiveDonors    int64
	DonationTotal     string
	NetworkSourceRows int64
	Evidence          [sha256.Size]byte
}

func Import(ctx context.Context, source, core *pgxpool.Pool, config Config) (Result, error) {
	config.ImportedAt = config.ImportedAt.UTC().Truncate(time.Microsecond)
	if source == nil || core == nil || config.RunID == uuid.Nil || config.ImportedAt.IsZero() {
		return Result{}, errors.New("legacy user administration importer configuration is invalid")
	}
	if err := postgres.RequireCurrentMigration(ctx, core); err != nil {
		return Result{}, err
	}
	if err := requireRun(ctx, core, config.RunID); err != nil {
		return Result{}, err
	}
	fixedObservedAt, receiptExists, err := receiptObservedAt(ctx, core, config.RunID)
	if err != nil {
		return Result{}, err
	}
	state, err := readSource(ctx, source, fixedObservedAt)
	if err != nil {
		return Result{}, err
	}
	result := resultFromState(config.RunID, state)

	tx, err := core.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Result{}, fmt.Errorf("begin legacy user administration import: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := createStages(ctx, tx); err != nil {
		return Result{}, err
	}
	if err := stageSource(ctx, tx, state); err != nil {
		return Result{}, err
	}
	if err := requireMappings(ctx, tx, int64(len(state.Donations))); err != nil {
		return Result{}, err
	}
	importedAt := config.ImportedAt
	if importedAt.Before(state.ObservedAt) {
		importedAt = state.ObservedAt
	}
	if err := importDonations(ctx, tx, config.RunID, importedAt); err != nil {
		return Result{}, err
	}
	if err := importNetworks(ctx, tx, importedAt); err != nil {
		return Result{}, err
	}
	if receiptExists {
		if err := verifyReceipt(ctx, tx, state, result); err != nil {
			return Result{}, err
		}
		result.Duplicate = true
	} else if err := insertReceipt(ctx, tx, state, result, importedAt); err != nil {
		return Result{}, err
	}
	if err := verifyTarget(ctx, tx, state); err != nil {
		return Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit legacy user administration import: %w", err)
	}
	return result, nil
}

func requireRun(ctx context.Context, core *pgxpool.Pool, runID uuid.UUID) error {
	var state string
	err := core.QueryRow(ctx, `
SELECT state
FROM migration.runs
WHERE id = $1 AND source_system = 'ptyes'`, runID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("legacy user administration migration run was not found")
	}
	if err != nil {
		return fmt.Errorf("read legacy user administration migration run: %w", err)
	}
	if state != "importing" && state != "imported" && state != "reconciled" {
		return fmt.Errorf("legacy user administration run state %q cannot accept an import", state)
	}
	return nil
}

func receiptObservedAt(ctx context.Context, core *pgxpool.Pool, runID uuid.UUID) (*time.Time, bool, error) {
	var observedAt time.Time
	err := core.QueryRow(ctx, `
SELECT observed_at
FROM migration.legacy_user_administration_imports
WHERE run_id = $1`, runID).Scan(&observedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read legacy user administration receipt: %w", err)
	}
	observedAt = observedAt.UTC().Truncate(time.Microsecond)
	return &observedAt, true, nil
}

func readSource(ctx context.Context, source *pgxpool.Pool, fixedObservedAt *time.Time) (sourceState, error) {
	tx, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return sourceState{}, fmt.Errorf("begin PtYes user administration snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var observedAt time.Time
	if fixedObservedAt == nil {
		if err := tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&observedAt); err != nil {
			return sourceState{}, fmt.Errorf("read PtYes metadata timestamp: %w", err)
		}
	} else {
		observedAt = *fixedObservedAt
	}
	observedAt = observedAt.UTC().Truncate(time.Microsecond)
	donations, positive, total, err := readDonations(ctx, tx)
	if err != nil {
		return sourceState{}, err
	}
	networks, sourceNetworkRows, err := readNetworks(ctx, tx, observedAt)
	if err != nil {
		return sourceState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sourceState{}, fmt.Errorf("commit PtYes user administration snapshot: %w", err)
	}
	state := sourceState{
		ObservedAt: observedAt, Donations: donations, Networks: networks,
		PositiveDonors: positive, DonationTotal: total, NetworkSourceRows: sourceNetworkRows,
	}
	state.Evidence = sourceEvidence(state)
	return state, nil
}

func readDonations(ctx context.Context, tx pgx.Tx) ([]donationRow, int64, string, error) {
	rows, err := tx.Query(ctx, `
SELECT id::bigint, COALESCE(donated, 0)::numeric(12, 2)::text
FROM users
ORDER BY id`)
	if err != nil {
		return nil, 0, "", fmt.Errorf("query PtYes donations: %w", err)
	}
	defer rows.Close()
	result := make([]donationRow, 0, 16_000)
	var previousID int64
	for rows.Next() {
		var row donationRow
		if err := rows.Scan(&row.LegacyUserID, &row.Amount); err != nil {
			return nil, 0, "", fmt.Errorf("scan PtYes donation: %w", err)
		}
		if row.LegacyUserID <= previousID || !validDonation(row.Amount) {
			return nil, 0, "", fmt.Errorf("PtYes donation row %d is invalid", row.LegacyUserID)
		}
		row.Fingerprint = donationFingerprint(row)
		result = append(result, row)
		previousID = row.LegacyUserID
	}
	if err := rows.Err(); err != nil {
		return nil, 0, "", fmt.Errorf("finish PtYes donations: %w", err)
	}
	var positive int64
	var total string
	if err := tx.QueryRow(ctx, `
SELECT
    count(*) FILTER (WHERE COALESCE(donated, 0) > 0)::bigint,
    COALESCE(sum(COALESCE(donated, 0)), 0)::numeric(20, 2)::text
FROM users`).Scan(&positive, &total); err != nil {
		return nil, 0, "", fmt.Errorf("summarize PtYes donations: %w", err)
	}
	return result, positive, total, nil
}

func readNetworks(ctx context.Context, tx pgx.Tx, observedAt time.Time) ([]networkRow, int64, error) {
	cutoff := observedAt.AddDate(0, 0, -retentionDays)
	var sourceRows int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM user_ip_histories
WHERE created_at >= $1 AND created_at <= $2`, cutoff, observedAt).Scan(&sourceRows); err != nil {
		return nil, 0, fmt.Errorf("count PtYes network history: %w", err)
	}
	rows, err := tx.Query(ctx, `
SELECT
    user_id::bigint AS legacy_user_id,
    btrim(ip) AS address,
    min(created_at) AS first_seen_at,
    max(created_at) AS last_seen_at,
    count(*)::bigint AS seen_count
FROM user_ip_histories
WHERE created_at >= $1
  AND created_at <= $2
  AND btrim(ip) <> ''
GROUP BY user_id, btrim(ip)
ORDER BY legacy_user_id, last_seen_at DESC, address`, cutoff, observedAt)
	if err != nil {
		return nil, 0, fmt.Errorf("query bounded PtYes network history: %w", err)
	}
	defer rows.Close()
	result := make([]networkRow, 0, 250_000)
	currentUserID := int64(0)
	currentAddresses := make(map[string]int, maximumAddressesPerUser)
	for rows.Next() {
		var row networkRow
		if err := rows.Scan(
			&row.LegacyUserID, &row.Address, &row.FirstSeenAt, &row.LastSeenAt, &row.SeenCount,
		); err != nil {
			return nil, 0, fmt.Errorf("scan PtYes network aggregate: %w", err)
		}
		if row.LegacyUserID < 1 || row.SeenCount < 1 || row.FirstSeenAt.After(row.LastSeenAt) {
			return nil, 0, fmt.Errorf("PtYes network aggregate for user %d is invalid", row.LegacyUserID)
		}
		address, err := netip.ParseAddr(row.Address)
		if err != nil || address.Zone() != "" {
			// PtYes accepted arbitrary text in the legacy IP column. Invalid
			// values are deliberately omitted rather than copied as history.
			continue
		}
		row.Address = address.Unmap().String()
		row.FirstSeenAt = row.FirstSeenAt.UTC().Truncate(time.Microsecond)
		row.LastSeenAt = row.LastSeenAt.UTC().Truncate(time.Microsecond)
		if row.LegacyUserID != currentUserID {
			currentUserID = row.LegacyUserID
			currentAddresses = make(map[string]int, maximumAddressesPerUser)
		}
		if index, exists := currentAddresses[row.Address]; exists {
			existing := &result[index]
			if row.FirstSeenAt.Before(existing.FirstSeenAt) {
				existing.FirstSeenAt = row.FirstSeenAt
			}
			if row.LastSeenAt.After(existing.LastSeenAt) {
				existing.LastSeenAt = row.LastSeenAt
			}
			if existing.SeenCount > int64(^uint64(0)>>1)-row.SeenCount {
				return nil, 0, errors.New("PtYes network observation count overflowed")
			}
			existing.SeenCount += row.SeenCount
			continue
		}
		if len(currentAddresses) >= maximumAddressesPerUser {
			continue
		}
		currentAddresses[row.Address] = len(result)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("finish PtYes network aggregates: %w", err)
	}
	return result, sourceRows, nil
}

func createStages(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `
CREATE TEMP TABLE legacy_user_donation_stage (
    legacy_user_id bigint PRIMARY KEY,
    amount numeric(12, 2) NOT NULL,
    source_fingerprint bytea NOT NULL
) ON COMMIT DROP;
CREATE TEMP TABLE legacy_user_network_stage (
    legacy_user_id bigint NOT NULL,
    ip_address inet NOT NULL,
    first_seen_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    seen_count bigint NOT NULL,
    PRIMARY KEY (legacy_user_id, ip_address)
) ON COMMIT DROP;`)
	if err != nil {
		return fmt.Errorf("create legacy user administration stages: %w", err)
	}
	return nil
}

func stageSource(ctx context.Context, tx pgx.Tx, state sourceState) error {
	donationCount, err := tx.CopyFrom(ctx, pgx.Identifier{"legacy_user_donation_stage"},
		[]string{"legacy_user_id", "amount", "source_fingerprint"},
		pgx.CopyFromSlice(len(state.Donations), func(index int) ([]any, error) {
			row := state.Donations[index]
			return []any{row.LegacyUserID, row.Amount, row.Fingerprint[:]}, nil
		}))
	if err != nil {
		return fmt.Errorf("stage legacy donations: %w", err)
	}
	if donationCount != int64(len(state.Donations)) {
		return errors.New("legacy donation stage count changed")
	}
	networkCount, err := tx.CopyFrom(ctx, pgx.Identifier{"legacy_user_network_stage"},
		[]string{"legacy_user_id", "ip_address", "first_seen_at", "last_seen_at", "seen_count"},
		pgx.CopyFromSlice(len(state.Networks), func(index int) ([]any, error) {
			row := state.Networks[index]
			return []any{row.LegacyUserID, row.Address, row.FirstSeenAt, row.LastSeenAt, row.SeenCount}, nil
		}))
	if err != nil {
		return fmt.Errorf("stage legacy network aggregates: %w", err)
	}
	if networkCount != int64(len(state.Networks)) {
		return errors.New("legacy network stage count changed")
	}
	return nil
}

func requireMappings(ctx context.Context, tx pgx.Tx, expected int64) error {
	var mapped int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint
FROM legacy_user_donation_stage AS stage
JOIN migration.user_id_map AS mapping
  ON mapping.source_system = 'ptyes'
 AND mapping.legacy_user_id = stage.legacy_user_id
JOIN identity.users AS user_account ON user_account.id = mapping.user_id`).Scan(&mapped); err != nil {
		return fmt.Errorf("verify legacy user administration mappings: %w", err)
	}
	if mapped != expected {
		return fmt.Errorf("legacy user administration mappings=%d, want %d", mapped, expected)
	}
	return nil
}

func importDonations(ctx context.Context, tx pgx.Tx, runID uuid.UUID, importedAt time.Time) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO migration.user_donation_openings (
    source_system, legacy_user_id, user_id, source_donated,
    source_fingerprint, first_run_id, imported_at
)
SELECT 'ptyes', stage.legacy_user_id, mapping.user_id, stage.amount,
       stage.source_fingerprint, $1, $2
FROM legacy_user_donation_stage AS stage
JOIN migration.user_id_map AS mapping
  ON mapping.source_system = 'ptyes'
 AND mapping.legacy_user_id = stage.legacy_user_id
ON CONFLICT (legacy_user_id) DO NOTHING`, runID, importedAt); err != nil {
		return fmt.Errorf("insert legacy donation openings: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.user_donation_totals (user_id, amount, version, updated_at)
SELECT opening.user_id, opening.source_donated, 1, $1
FROM migration.user_donation_openings AS opening
JOIN legacy_user_donation_stage AS stage
  ON stage.legacy_user_id = opening.legacy_user_id
ON CONFLICT (user_id) DO NOTHING`, importedAt); err != nil {
		return fmt.Errorf("seed donation totals: %w", err)
	}
	return nil
}

func importNetworks(ctx context.Context, tx pgx.Tx, importedAt time.Time) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.user_network_observations (
    user_id, ip_address, first_seen_at, last_seen_at,
    legacy_seen_count, web_login_seen_count, updated_at
)
SELECT mapping.user_id, stage.ip_address, stage.first_seen_at, stage.last_seen_at,
       stage.seen_count, 0, $1
FROM legacy_user_network_stage AS stage
JOIN migration.user_id_map AS mapping
  ON mapping.source_system = 'ptyes'
 AND mapping.legacy_user_id = stage.legacy_user_id
ON CONFLICT (user_id, ip_address) DO UPDATE
SET first_seen_at = LEAST(identity.user_network_observations.first_seen_at, EXCLUDED.first_seen_at),
    last_seen_at = GREATEST(identity.user_network_observations.last_seen_at, EXCLUDED.last_seen_at),
    legacy_seen_count = EXCLUDED.legacy_seen_count,
    updated_at = EXCLUDED.updated_at`, importedAt); err != nil {
		return fmt.Errorf("import bounded legacy network aggregates: %w", err)
	}
	return nil
}

func verifyTarget(ctx context.Context, tx pgx.Tx, state sourceState) error {
	var openingCount, openingMismatch, projectionMismatch int64
	if err := tx.QueryRow(ctx, `
SELECT
    count(opening.legacy_user_id)::bigint,
    count(*) FILTER (
        WHERE opening.user_id IS DISTINCT FROM mapping.user_id
           OR opening.source_donated IS DISTINCT FROM stage.amount
           OR opening.source_fingerprint IS DISTINCT FROM stage.source_fingerprint
    )::bigint,
    count(*) FILTER (
        WHERE total.amount IS DISTINCT FROM (
            opening.source_donated + COALESCE(adjustment.delta, 0)
        )::numeric(12, 2)
    )::bigint
FROM legacy_user_donation_stage AS stage
JOIN migration.user_id_map AS mapping
  ON mapping.source_system = 'ptyes'
 AND mapping.legacy_user_id = stage.legacy_user_id
LEFT JOIN migration.user_donation_openings AS opening
  ON opening.legacy_user_id = stage.legacy_user_id
LEFT JOIN identity.user_donation_totals AS total ON total.user_id = mapping.user_id
LEFT JOIN LATERAL (
    SELECT sum(event.delta) AS delta
    FROM identity.managed_user_adjustment_events AS event
    WHERE event.user_id = mapping.user_id
      AND event.field = 'donation_amount'
) AS adjustment ON true`).Scan(&openingCount, &openingMismatch, &projectionMismatch); err != nil {
		return fmt.Errorf("verify legacy donation target: %w", err)
	}
	if openingCount != int64(len(state.Donations)) || openingMismatch != 0 || projectionMismatch != 0 {
		return fmt.Errorf("legacy donation target mismatch: openings=%d/%d opening_mismatch=%d projection_mismatch=%d",
			openingCount, len(state.Donations), openingMismatch, projectionMismatch)
	}
	var networkMismatch int64
	if err := tx.QueryRow(ctx, `
SELECT count(*) FILTER (
    WHERE observation.user_id IS NULL
       OR observation.legacy_seen_count <> stage.seen_count
       OR observation.first_seen_at > stage.first_seen_at
       OR observation.last_seen_at < stage.last_seen_at
)::bigint
FROM legacy_user_network_stage AS stage
JOIN migration.user_id_map AS mapping
  ON mapping.source_system = 'ptyes'
 AND mapping.legacy_user_id = stage.legacy_user_id
LEFT JOIN identity.user_network_observations AS observation
  ON observation.user_id = mapping.user_id
 AND observation.ip_address = stage.ip_address`).Scan(&networkMismatch); err != nil {
		return fmt.Errorf("verify legacy network target: %w", err)
	}
	if networkMismatch != 0 {
		return fmt.Errorf("legacy network target has %d mismatches", networkMismatch)
	}
	return nil
}

func insertReceipt(ctx context.Context, tx pgx.Tx, state sourceState, result Result, importedAt time.Time) error {
	_, err := tx.Exec(ctx, `
INSERT INTO migration.legacy_user_administration_imports (
    run_id, observed_at, donation_source_rows, positive_donation_users,
    donation_total, network_source_rows, retained_network_rows,
    source_evidence_sha256, imported_at
) VALUES ($1, $2, $3, $4, $5::numeric(20, 2), $6, $7, $8, $9)`,
		result.RunID, state.ObservedAt, result.DonationSourceRows, result.PositiveDonors,
		result.DonationTotal, result.NetworkSourceRows, result.RetainedNetworkRows,
		state.Evidence[:], importedAt)
	if err != nil {
		return fmt.Errorf("insert legacy user administration receipt: %w", err)
	}
	return nil
}

func verifyReceipt(ctx context.Context, tx pgx.Tx, state sourceState, result Result) error {
	var observedAt time.Time
	var donationRows, positiveDonors, networkRows, retainedRows int64
	var donationTotal string
	var evidence []byte
	err := tx.QueryRow(ctx, `
SELECT observed_at, donation_source_rows, positive_donation_users,
       donation_total::text, network_source_rows, retained_network_rows,
       source_evidence_sha256
FROM migration.legacy_user_administration_imports
WHERE run_id = $1`, result.RunID).Scan(
		&observedAt, &donationRows, &positiveDonors, &donationTotal,
		&networkRows, &retainedRows, &evidence,
	)
	if err != nil {
		return fmt.Errorf("verify legacy user administration receipt: %w", err)
	}
	if !observedAt.Equal(state.ObservedAt) || donationRows != result.DonationSourceRows ||
		positiveDonors != result.PositiveDonors || canonicalDecimal(donationTotal) != canonicalDecimal(result.DonationTotal) ||
		networkRows != result.NetworkSourceRows || retainedRows != result.RetainedNetworkRows ||
		!bytes.Equal(evidence, state.Evidence[:]) {
		return errors.New("legacy user administration receipt conflicts with the source snapshot")
	}
	return nil
}

func resultFromState(runID uuid.UUID, state sourceState) Result {
	return Result{
		RunID: runID, ObservedAt: state.ObservedAt,
		DonationSourceRows: int64(len(state.Donations)), PositiveDonors: state.PositiveDonors,
		DonationTotal: state.DonationTotal, NetworkSourceRows: state.NetworkSourceRows,
		RetainedNetworkRows: int64(len(state.Networks)),
	}
}

func validDonation(value string) bool {
	if strings.HasPrefix(value, "-") {
		return false
	}
	parts := strings.SplitN(value, ".", 2)
	return len(parts[0]) >= 1 && len(parts[0]) <= 10 &&
		(len(parts) == 1 || len(parts[1]) <= 2)
}

func donationFingerprint(row donationRow) [sha256.Size]byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte(donationFingerprintDomain))
	writeInt64(hash, row.LegacyUserID)
	writeString(hash, canonicalDecimal(row.Amount))
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result
}

func sourceEvidence(state sourceState) [sha256.Size]byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte(evidenceDomain))
	writeString(hash, state.ObservedAt.Format(time.RFC3339Nano))
	for _, row := range state.Donations {
		_, _ = hash.Write(row.Fingerprint[:])
	}
	for _, row := range state.Networks {
		writeInt64(hash, row.LegacyUserID)
		writeString(hash, row.Address)
		writeString(hash, row.FirstSeenAt.Format(time.RFC3339Nano))
		writeString(hash, row.LastSeenAt.Format(time.RFC3339Nano))
		writeInt64(hash, row.SeenCount)
	}
	writeInt64(hash, state.NetworkSourceRows)
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result
}

type byteWriter interface{ Write([]byte) (int, error) }

func writeInt64(writer byteWriter, value int64) {
	var buffer [8]byte
	binary.BigEndian.PutUint64(buffer[:], uint64(value))
	_, _ = writer.Write(buffer[:])
}

func writeString(writer byteWriter, value string) {
	writeInt64(writer, int64(len(value)))
	_, _ = writer.Write([]byte(value))
}

func canonicalDecimal(value string) string {
	parts := strings.SplitN(strings.TrimSpace(value), ".", 2)
	integer := strings.TrimLeft(parts[0], "0")
	if integer == "" {
		integer = "0"
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = strings.TrimRight(parts[1], "0")
	}
	if fraction == "" {
		return integer
	}
	return integer + "." + fraction
}
