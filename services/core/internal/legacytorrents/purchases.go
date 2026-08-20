package legacytorrents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

const (
	purchaseFingerprintDomain = "peergo:migration:ptyes-torrent-purchase:v1\x00"
	priceFingerprintDomain    = "peergo:migration:ptyes-torrent-price:v1\x00"
	legacyPurchasePolicy      = "rousi-purchase-v1"
)

var legacyPurchaseEntitlementNamespace = uuid.MustParse("cc534d24-bf6d-5715-977b-e17c65ae6c3c")

type PurchaseImportConfig struct {
	Inventory  InventoryConfig
	ImportedAt time.Time
}

type PurchaseImportProgress struct {
	Processed int64
	Expected  int64
}

type PurchaseImportResult struct {
	RunID              uuid.UUID
	SourceRows         int64
	PriceOpenings      int64
	Entitlements       int64
	DuplicateCompleted int64
	Refunded           int64
	UnresolvedTorrents int64
	UnmappedTorrents   int64
	UnmappedUsers      int64
}

type legacyPurchaseRow struct {
	ID               int64
	LegacyTorrentID  *int64
	TorrentUUID      string
	BuyerLegacyID    int64
	SellerLegacyID   int64
	PriceText        string
	TaxText          string
	SellerIncomeText string
	Status           string
	PurchasedAt      time.Time
	Fingerprint      [sha256.Size]byte
	IntegerPrice     int64
}

type legacyPurchasePair struct {
	BuyerLegacyID   int64
	LegacyTorrentID int64
}

// ImportPurchases moves legacy prices and completed access rights after the
// torrent aggregates exist.  It is intentionally separate from object import:
// a retry verifies immutable opening receipts and never overwrites a price
// that an operator may have changed after cutover.
func ImportPurchases(
	ctx context.Context,
	source, core *pgxpool.Pool,
	config PurchaseImportConfig,
	progress func(PurchaseImportProgress),
) (PurchaseImportResult, error) {
	config.ImportedAt = config.ImportedAt.UTC().Truncate(time.Microsecond)
	if source == nil || core == nil || config.Inventory.RunID == uuid.Nil ||
		config.Inventory.SnapshotSHA256 == ([sha256.Size]byte{}) ||
		strings.TrimSpace(config.Inventory.MappingVersion) == "" || config.ImportedAt.IsZero() {
		return PurchaseImportResult{}, errors.New("legacy torrent purchase import configuration is invalid")
	}
	if progress == nil {
		progress = func(PurchaseImportProgress) {}
	}
	if err := platformpostgres.RequireCurrentMigration(ctx, core); err != nil {
		return PurchaseImportResult{}, err
	}
	if err := requirePurchaseRun(ctx, core, config.Inventory); err != nil {
		return PurchaseImportResult{}, err
	}
	userMappings, err := loadPurchaseUserMappings(ctx, core)
	if err != nil {
		return PurchaseImportResult{}, err
	}
	torrentMappings, err := loadPurchaseTorrentMappings(ctx, core)
	if err != nil {
		return PurchaseImportResult{}, err
	}
	rows, err := loadLegacyPurchaseRows(ctx, source)
	if err != nil {
		return PurchaseImportResult{}, err
	}
	canonical := make(map[legacyPurchasePair]int64)
	for _, row := range rows {
		if row.Status != "completed" || row.LegacyTorrentID == nil {
			continue
		}
		pair := legacyPurchasePair{BuyerLegacyID: row.BuyerLegacyID, LegacyTorrentID: *row.LegacyTorrentID}
		if previous, exists := canonical[pair]; !exists || row.ID < previous {
			canonical[pair] = row.ID
		}
	}

	tx, err := core.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PurchaseImportResult{}, fmt.Errorf("begin legacy torrent purchase import: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result := PurchaseImportResult{RunID: config.Inventory.RunID, SourceRows: int64(len(rows))}
	priceOpenings, err := importLegacyTorrentPrices(ctx, source, tx, config, torrentMappings)
	if err != nil {
		return PurchaseImportResult{}, err
	}
	result.PriceOpenings = priceOpenings
	for index, row := range rows {
		disposition := ""
		var torrentID *int64
		var buyerID, sellerID *uuid.UUID
		if mapped, ok := userMappings[row.BuyerLegacyID]; ok {
			value := mapped
			buyerID = &value
		}
		if mapped, ok := userMappings[row.SellerLegacyID]; ok {
			value := mapped
			sellerID = &value
		}
		switch {
		case row.LegacyTorrentID == nil:
			disposition = "unresolved_torrent"
			result.UnresolvedTorrents++
		case torrentMappings[*row.LegacyTorrentID] == 0:
			disposition = "unmapped_torrent"
			result.UnmappedTorrents++
		default:
			value := torrentMappings[*row.LegacyTorrentID]
			torrentID = &value
		}
		if disposition == "" && (buyerID == nil || sellerID == nil) {
			disposition = "unmapped_user"
			result.UnmappedUsers++
		}
		if disposition == "" && row.Status == "refunded" {
			disposition = "refunded"
			result.Refunded++
		}
		if disposition == "" {
			pair := legacyPurchasePair{BuyerLegacyID: row.BuyerLegacyID, LegacyTorrentID: *row.LegacyTorrentID}
			if canonical[pair] != row.ID {
				disposition = "duplicate_completed"
				result.DuplicateCompleted++
			} else {
				disposition = "entitled"
			}
		}
		if err := insertPurchaseOpening(ctx, tx, config, row, torrentID, buyerID, sellerID, disposition); err != nil {
			return PurchaseImportResult{}, err
		}
		if disposition == "entitled" {
			inserted, err := insertLegacyEntitlement(ctx, tx, config, row, *torrentID, *buyerID, *sellerID)
			if err != nil {
				return PurchaseImportResult{}, err
			}
			if inserted {
				result.Entitlements++
			}
		}
		progress(PurchaseImportProgress{Processed: int64(index + 1), Expected: int64(len(rows))})
	}
	if err := tx.Commit(ctx); err != nil {
		return PurchaseImportResult{}, fmt.Errorf("commit legacy torrent purchase import: %w", err)
	}
	return result, nil
}

func requirePurchaseRun(ctx context.Context, core *pgxpool.Pool, config InventoryConfig) error {
	var snapshot []byte
	var mappingVersion, state string
	err := core.QueryRow(ctx, `
SELECT source_snapshot_sha256, mapping_version, state
FROM migration.runs WHERE id = $1`, config.RunID).Scan(&snapshot, &mappingVersion, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("legacy torrent purchase run was not found")
	}
	if err != nil {
		return fmt.Errorf("read legacy torrent purchase run: %w", err)
	}
	if !bytes.Equal(snapshot, config.SnapshotSHA256[:]) || mappingVersion != config.MappingVersion {
		return errors.New("legacy torrent purchase run identity does not match the snapshot")
	}
	if state != "importing" && state != "imported" && state != "reconciled" {
		return fmt.Errorf("legacy torrent purchase run state %q cannot accept purchases", state)
	}
	return nil
}

func loadPurchaseUserMappings(ctx context.Context, core *pgxpool.Pool) (map[int64]uuid.UUID, error) {
	rows, err := core.Query(ctx, `
SELECT mapping.legacy_user_id, mapping.user_id
FROM migration.user_id_map AS mapping
JOIN identity.users AS target ON target.id = mapping.user_id
WHERE mapping.source_system = 'ptyes'`)
	if err != nil {
		return nil, fmt.Errorf("list purchase user mappings: %w", err)
	}
	defer rows.Close()
	result := make(map[int64]uuid.UUID)
	for rows.Next() {
		var legacyID int64
		var targetID uuid.UUID
		if err := rows.Scan(&legacyID, &targetID); err != nil {
			return nil, fmt.Errorf("scan purchase user mapping: %w", err)
		}
		result[legacyID] = targetID
	}
	return result, rows.Err()
}

func loadPurchaseTorrentMappings(ctx context.Context, core *pgxpool.Pool) (map[int64]int64, error) {
	rows, err := core.Query(ctx, `
SELECT mapping.legacy_torrent_id, mapping.torrent_id
FROM migration.torrent_id_map AS mapping
JOIN torrents.torrents AS target ON target.id = mapping.torrent_id
WHERE mapping.source_system = 'ptyes'`)
	if err != nil {
		return nil, fmt.Errorf("list purchase torrent mappings: %w", err)
	}
	defer rows.Close()
	result := make(map[int64]int64)
	for rows.Next() {
		var legacyID, targetID int64
		if err := rows.Scan(&legacyID, &targetID); err != nil {
			return nil, fmt.Errorf("scan purchase torrent mapping: %w", err)
		}
		result[legacyID] = targetID
	}
	return result, rows.Err()
}

func loadLegacyPurchaseRows(ctx context.Context, source *pgxpool.Pool) ([]legacyPurchaseRow, error) {
	rows, err := source.Query(ctx, `
SELECT
    purchase.id,
    COALESCE(purchase.torrent_id, torrent.id),
    COALESCE(purchase.torrent_uuid, ''),
    purchase.buyer_id,
    purchase.seller_id,
    COALESCE(purchase.price, 0)::text,
    COALESCE(purchase.tax_amount, 0)::text,
    COALESCE(purchase.seller_income, 0)::text,
    purchase.status,
    purchase.created_at
FROM public.torrent_purchases AS purchase
LEFT JOIN public.torrents AS torrent ON torrent.uuid = purchase.torrent_uuid
ORDER BY purchase.id`)
	if err != nil {
		return nil, fmt.Errorf("query PtYes torrent purchases: %w", err)
	}
	defer rows.Close()
	result := make([]legacyPurchaseRow, 0, 32768)
	for rows.Next() {
		var row legacyPurchaseRow
		var legacyTorrentID pgtype.Int8
		if err := rows.Scan(
			&row.ID, &legacyTorrentID, &row.TorrentUUID,
			&row.BuyerLegacyID, &row.SellerLegacyID,
			&row.PriceText, &row.TaxText, &row.SellerIncomeText,
			&row.Status, &row.PurchasedAt,
		); err != nil {
			return nil, fmt.Errorf("scan PtYes torrent purchase: %w", err)
		}
		if legacyTorrentID.Valid {
			value := legacyTorrentID.Int64
			row.LegacyTorrentID = &value
		}
		row.PurchasedAt = row.PurchasedAt.UTC().Truncate(time.Microsecond)
		if row.ID < 1 || row.BuyerLegacyID < 1 || row.SellerLegacyID < 1 ||
			(row.Status != "completed" && row.Status != "refunded") || row.PurchasedAt.IsZero() {
			return nil, fmt.Errorf("PtYes torrent purchase %d is invalid", row.ID)
		}
		integerPrice, err := roundLegacyMagic(row.PriceText)
		if err != nil {
			return nil, fmt.Errorf("round PtYes torrent purchase %d price: %w", row.ID, err)
		}
		row.IntegerPrice = integerPrice
		row.Fingerprint = purchaseFingerprint(row)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish PtYes torrent purchase query: %w", err)
	}
	return result, nil
}

func importLegacyTorrentPrices(ctx context.Context, source *pgxpool.Pool, tx pgx.Tx, config PurchaseImportConfig, mappings map[int64]int64) (int64, error) {
	rows, err := source.Query(ctx, `SELECT id, COALESCE(price, 0)::text FROM public.torrents ORDER BY id`)
	if err != nil {
		return 0, fmt.Errorf("query PtYes torrent prices: %w", err)
	}
	defer rows.Close()
	var imported int64
	for rows.Next() {
		var legacyID int64
		var sourcePrice string
		if err := rows.Scan(&legacyID, &sourcePrice); err != nil {
			return 0, fmt.Errorf("scan PtYes torrent price: %w", err)
		}
		torrentID := mappings[legacyID]
		if torrentID == 0 {
			continue
		}
		integerPrice, err := roundLegacyMagic(sourcePrice)
		if err != nil {
			return 0, fmt.Errorf("round PtYes torrent %d price: %w", legacyID, err)
		}
		fingerprint := sha256.Sum256([]byte(priceFingerprintDomain + strconv.FormatInt(legacyID, 10) + "\x00" + sourcePrice))
		var existingFingerprint []byte
		err = tx.QueryRow(ctx, `
SELECT source_fingerprint
FROM migration.torrent_purchase_price_openings
WHERE legacy_torrent_id = $1`, legacyID).Scan(&existingFingerprint)
		if err == nil {
			if !bytes.Equal(existingFingerprint, fingerprint[:]) {
				return 0, fmt.Errorf("legacy torrent %d price opening conflicts with this snapshot", legacyID)
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("read legacy torrent %d price opening: %w", legacyID, err)
		}
		if _, err := tx.Exec(ctx, `
UPDATE torrents.torrents
SET purchase_price = $2, version = version + 1,
    updated_at = GREATEST(updated_at, $3)
WHERE id = $1 AND purchase_price IS DISTINCT FROM $2`, torrentID, integerPrice, config.ImportedAt); err != nil {
			return 0, fmt.Errorf("apply legacy torrent %d price: %w", legacyID, err)
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO migration.torrent_purchase_price_openings (
    legacy_torrent_id, first_run_id, torrent_id,
    source_price, integer_price, source_fingerprint, imported_at
) VALUES ($1, $2, $3, $4::numeric, $5, $6, $7)`,
			legacyID, config.Inventory.RunID, torrentID, sourcePrice,
			integerPrice, fingerprint[:], config.ImportedAt,
		); err != nil {
			return 0, fmt.Errorf("insert legacy torrent %d price opening: %w", legacyID, err)
		}
		imported++
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("finish PtYes torrent price query: %w", err)
	}
	return imported, nil
}

func insertPurchaseOpening(ctx context.Context, tx pgx.Tx, config PurchaseImportConfig, row legacyPurchaseRow, torrentID *int64, buyerID, sellerID *uuid.UUID, disposition string) error {
	command, err := tx.Exec(ctx, `
INSERT INTO migration.torrent_purchase_openings (
    legacy_purchase_id, first_run_id, legacy_torrent_id, torrent_id,
    legacy_buyer_id, buyer_id, legacy_seller_id, seller_id,
    source_status, source_price, source_tax, source_seller_income,
    integer_price, disposition, source_fingerprint, purchased_at, imported_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8,
    $9, $10::numeric, $11::numeric, $12::numeric,
    $13, $14, $15, $16, $17
)
ON CONFLICT (legacy_purchase_id) DO NOTHING`,
		row.ID, config.Inventory.RunID, row.LegacyTorrentID, torrentID,
		row.BuyerLegacyID, buyerID, row.SellerLegacyID, sellerID,
		row.Status, row.PriceText, row.TaxText, row.SellerIncomeText,
		row.IntegerPrice, disposition, row.Fingerprint[:], row.PurchasedAt, config.ImportedAt,
	)
	if err != nil {
		return fmt.Errorf("insert legacy torrent purchase %d opening: %w", row.ID, err)
	}
	if command.RowsAffected() == 1 {
		return nil
	}
	var existingFingerprint []byte
	var existingDisposition string
	if err := tx.QueryRow(ctx, `
SELECT source_fingerprint, disposition
FROM migration.torrent_purchase_openings
WHERE legacy_purchase_id = $1`, row.ID).Scan(&existingFingerprint, &existingDisposition); err != nil {
		return fmt.Errorf("read legacy torrent purchase %d opening replay: %w", row.ID, err)
	}
	if !bytes.Equal(existingFingerprint, row.Fingerprint[:]) || existingDisposition != disposition {
		return fmt.Errorf("legacy torrent purchase %d opening conflicts with this snapshot", row.ID)
	}
	return nil
}

func insertLegacyEntitlement(ctx context.Context, tx pgx.Tx, config PurchaseImportConfig, row legacyPurchaseRow, torrentID int64, buyerID, sellerID uuid.UUID) (bool, error) {
	tax, err := roundLegacyMagic(row.TaxText)
	if err != nil {
		return false, fmt.Errorf("round PtYes torrent purchase %d tax: %w", row.ID, err)
	}
	if tax > row.IntegerPrice {
		tax = row.IntegerPrice
	}
	sellerIncome := row.IntegerPrice - tax
	// Legacy access is the opening entitlement for a user/torrent pair, so it
	// always occupies sequence 1. Later PeerGo refunds and repurchases append
	// higher sequences without rewriting this cutover receipt.
	entitlementID := uuid.NewSHA1(legacyPurchaseEntitlementNamespace, []byte(buyerID.String()+":"+strconv.FormatInt(torrentID, 10)))
	recordedAt := config.ImportedAt
	if recordedAt.Before(row.PurchasedAt) {
		recordedAt = row.PurchasedAt
	}
	sourceReference := "ptyes-purchase:" + strconv.FormatInt(row.ID, 10)
	command, err := tx.Exec(ctx, `
INSERT INTO economy.torrent_purchase_entitlements (
    id, request_id, user_id, torrent_id, seller_id,
    source_kind, source_reference, price, tax, seller_income,
    policy_revision, payload_sha256, magic_transaction_id,
    purchased_at, recorded_at, purchase_sequence
) VALUES (
    $1, NULL, $2, $3, $4,
    'legacy_import', $5, $6, $7, $8,
    $9, $10, NULL, $11, $12, 1
)
ON CONFLICT (user_id, torrent_id, purchase_sequence) DO NOTHING`,
		entitlementID, buyerID, torrentID, sellerID, sourceReference,
		row.IntegerPrice, tax, sellerIncome, legacyPurchasePolicy,
		row.Fingerprint[:], row.PurchasedAt, recordedAt,
	)
	if err != nil {
		return false, fmt.Errorf("insert legacy torrent purchase %d entitlement: %w", row.ID, err)
	}
	if command.RowsAffected() == 1 {
		return true, nil
	}
	var sourceKind string
	var fingerprint []byte
	if err := tx.QueryRow(ctx, `
SELECT source_kind, payload_sha256
FROM economy.torrent_purchase_entitlements
WHERE user_id = $1 AND torrent_id = $2 AND purchase_sequence = 1`, buyerID, torrentID).Scan(&sourceKind, &fingerprint); err != nil {
		return false, fmt.Errorf("read legacy torrent purchase %d entitlement replay: %w", row.ID, err)
	}
	if sourceKind != "legacy_import" || !bytes.Equal(fingerprint, row.Fingerprint[:]) {
		return false, fmt.Errorf("legacy torrent purchase %d entitlement conflicts with existing access", row.ID)
	}
	return false, nil
}

func roundLegacyMagic(value string) (int64, error) {
	rat, ok := new(big.Rat).SetString(strings.TrimSpace(value))
	if !ok || rat.Sign() < 0 {
		return 0, errors.New("value is not a non-negative decimal")
	}
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(rat.Num(), rat.Denom(), remainder)
	if new(big.Int).Lsh(remainder, 1).Cmp(rat.Denom()) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() || quotient.Int64() > 1_000_000 {
		return 0, errors.New("rounded value exceeds PeerGo's torrent purchase limit")
	}
	return quotient.Int64(), nil
}

func purchaseFingerprint(row legacyPurchaseRow) [sha256.Size]byte {
	legacyTorrentID := ""
	if row.LegacyTorrentID != nil {
		legacyTorrentID = strconv.FormatInt(*row.LegacyTorrentID, 10)
	}
	parts := []string{
		purchaseFingerprintDomain,
		strconv.FormatInt(row.ID, 10), legacyTorrentID, row.TorrentUUID,
		strconv.FormatInt(row.BuyerLegacyID, 10), strconv.FormatInt(row.SellerLegacyID, 10),
		row.PriceText, row.TaxText, row.SellerIncomeText, row.Status,
		row.PurchasedAt.Format(time.RFC3339Nano),
	}
	return sha256.Sum256([]byte(strings.Join(parts, "\x00")))
}
