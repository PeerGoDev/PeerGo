package legacytorrents

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/contracts/go/authzcontractv1"
	"github.com/peergo/peergo/contracts/go/schemaversionv1"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

const requiredLegacyVaultMigrationVersion int64 = schemaversionv1.PrivacyVault

type ReconciliationConfig struct {
	Inventory    InventoryConfig
	ReconciledAt time.Time
	BackendID    string
}

type ReconciliationResult struct {
	RunID             uuid.UUID
	State             string
	Users             int64
	VaultCredentials  int64
	Torrents          int64
	ExcludedTorrents  int64
	PublishedTorrents int64
	TorrentFiles      int64
	TorrentObjects    int64
	PurchaseRows      int64
	PurchaseRights    int64
	PurchaseEvidence  int64
}

type legacyPurchaseReconciliation struct {
	SourceRows    int64
	PriceOpenings int64
	Entitlements  int64
	EvidenceOnly  int64
}

// ReconcileMigration is deliberately a terminal gate, not an importer. The
// caller must first execute the torrent importer in verify-only retry mode; accepting
// any newly imported row here would make "reconciled" mean only that the last
// missing row happened to be written, rather than that a stable second pass
// reproduced every target torrent relation and object observation. The media
// importer must also have completed its own stable retry before this function
// is allowed to perform the one run-level terminal transition.
func ReconcileMigration(
	ctx context.Context,
	source *pgxpool.Pool,
	core *pgxpool.Pool,
	vault *pgxpool.Pool,
	config ReconciliationConfig,
	torrentsVerified ImportResult,
) (ReconciliationResult, error) {
	config.ReconciledAt = config.ReconciledAt.UTC().Truncate(time.Microsecond)
	if source == nil || core == nil || vault == nil || config.Inventory.RunID == uuid.Nil ||
		config.ReconciledAt.IsZero() || config.BackendID == "" {
		return ReconciliationResult{}, errors.New("legacy migration reconciliation configuration is invalid")
	}
	if err := platformpostgres.RequireCurrentMigration(ctx, core); err != nil {
		return ReconciliationResult{}, err
	}
	if err := requireLegacyVaultVersion(ctx, vault); err != nil {
		return ReconciliationResult{}, err
	}
	inventory, err := InspectInventory(ctx, source, core, config.Inventory)
	if err != nil {
		return ReconciliationResult{}, err
	}
	if torrentsVerified.ImportedTorrents != 0 ||
		torrentsVerified.SkippedTorrents != torrentsVerified.ExpectedTorrents ||
		torrentsVerified.ExpectedTorrents+torrentsVerified.ExcludedTorrents != inventory.Torrents {
		return ReconciliationResult{}, errors.New("legacy reconciliation requires a stable verify-only retry of the torrent importer")
	}

	result := ReconciliationResult{
		RunID: config.Inventory.RunID, Users: inventory.Users,
		Torrents: torrentsVerified.ExpectedTorrents, ExcludedTorrents: torrentsVerified.ExcludedTorrents,
	}
	if err := reconcileRunAndCheckpoints(ctx, core, config, inventory, torrentsVerified); err != nil {
		return ReconciliationResult{}, err
	}
	credentialRefs, err := reconcileCoreUsers(ctx, core, inventory.Users)
	if err != nil {
		return ReconciliationResult{}, err
	}
	result.VaultCredentials, err = reconcileVaultUsers(ctx, vault, credentialRefs)
	if err != nil {
		return ReconciliationResult{}, err
	}
	result.PublishedTorrents, result.TorrentFiles, result.TorrentObjects, err = reconcileCoreTorrents(
		ctx, core, config.BackendID, inventory, torrentsVerified,
	)
	if err != nil {
		return ReconciliationResult{}, err
	}
	purchases, err := reconcileLegacyPurchases(ctx, source, core, config.Inventory.RunID, result.Torrents)
	if err != nil {
		return ReconciliationResult{}, err
	}
	result.PurchaseRows = purchases.SourceRows
	result.PurchaseRights = purchases.Entitlements
	result.PurchaseEvidence = purchases.EvidenceOnly
	result.State, err = markMigrationReconciled(ctx, core, config)
	if err != nil {
		return ReconciliationResult{}, err
	}
	return result, nil
}

// reconcileLegacyPurchases makes purchase migration part of the terminal
// cutover proof. Every source row must have immutable evidence, every imported
// torrent must have an opening price receipt, and only a completed canonical
// purchase may have a matching permanent entitlement. This prevents a restore
// from looking complete while silently asking old buyers to pay again.
func reconcileLegacyPurchases(
	ctx context.Context,
	source, core *pgxpool.Pool,
	runID uuid.UUID,
	expectedTorrents int64,
) (legacyPurchaseReconciliation, error) {
	var result legacyPurchaseReconciliation
	if err := source.QueryRow(ctx, `SELECT count(*)::bigint FROM public.torrent_purchases`).Scan(&result.SourceRows); err != nil {
		return legacyPurchaseReconciliation{}, fmt.Errorf("count PtYes torrent purchases: %w", err)
	}

	var purchaseRows, exactEntitlements, extraEntitlements int64
	if err := core.QueryRow(ctx, `
WITH openings AS (
    SELECT *
    FROM migration.torrent_purchase_openings
    WHERE first_run_id = $1
), entitlement_counts AS (
    SELECT
        count(*) FILTER (WHERE opening.disposition = 'entitled')::bigint AS entitled,
        count(*) FILTER (WHERE opening.disposition <> 'entitled')::bigint AS evidence_only,
        count(*) FILTER (
            WHERE opening.disposition = 'entitled'
              AND EXISTS (
                  SELECT 1
                  FROM economy.torrent_purchase_entitlements AS entitlement
                  WHERE entitlement.source_kind = 'legacy_import'
                    AND entitlement.source_reference = 'ptyes-purchase:' || opening.legacy_purchase_id::text
                    AND entitlement.user_id = opening.buyer_id
                    AND entitlement.torrent_id = opening.torrent_id
                    AND entitlement.seller_id = opening.seller_id
                    AND entitlement.price = opening.integer_price
                    AND entitlement.tax = LEAST(opening.integer_price, round(opening.source_tax)::bigint)
                    AND entitlement.seller_income = opening.integer_price - LEAST(opening.integer_price, round(opening.source_tax)::bigint)
                    AND entitlement.policy_revision = $2
                    AND entitlement.payload_sha256 = opening.source_fingerprint
              )
        )::bigint AS exact_entitlements
    FROM openings AS opening
)
SELECT
    (SELECT count(*)::bigint
     FROM migration.torrent_purchase_price_openings
     WHERE first_run_id = $1),
    (SELECT count(*)::bigint FROM openings),
    entitlement_counts.entitled,
    entitlement_counts.evidence_only,
    entitlement_counts.exact_entitlements,
    (SELECT count(*)::bigint
     FROM economy.torrent_purchase_entitlements AS entitlement
     WHERE entitlement.source_kind = 'legacy_import'
       AND entitlement.policy_revision = $2
       AND NOT EXISTS (
           SELECT 1
           FROM openings AS opening
           WHERE opening.disposition = 'entitled'
             AND entitlement.source_reference = 'ptyes-purchase:' || opening.legacy_purchase_id::text
       ))
FROM entitlement_counts`, runID, legacyPurchasePolicy).Scan(
		&result.PriceOpenings,
		&purchaseRows,
		&result.Entitlements,
		&result.EvidenceOnly,
		&exactEntitlements,
		&extraEntitlements,
	); err != nil {
		return legacyPurchaseReconciliation{}, fmt.Errorf("reconcile imported torrent purchases: %w", err)
	}
	if result.PriceOpenings != expectedTorrents || purchaseRows != result.SourceRows ||
		result.Entitlements+result.EvidenceOnly != result.SourceRows ||
		exactEntitlements != result.Entitlements || extraEntitlements != 0 {
		return legacyPurchaseReconciliation{}, errors.New("legacy torrent prices, purchase evidence, or permanent rights do not reconcile")
	}
	return result, nil
}

func requireLegacyVaultVersion(ctx context.Context, vault *pgxpool.Pool) error {
	var version int64
	if err := vault.QueryRow(ctx, `
SELECT COALESCE(MAX(version_id), 0)
FROM goose_db_version
WHERE is_applied = true`).Scan(&version); err != nil {
		return fmt.Errorf("read Vault migration version for reconciliation: %w", err)
	}
	if version != requiredLegacyVaultMigrationVersion {
		return fmt.Errorf("Vault migration version is %d, want %d", version, requiredLegacyVaultMigrationVersion)
	}
	return nil
}

func reconcileRunAndCheckpoints(
	ctx context.Context,
	core *pgxpool.Pool,
	config ReconciliationConfig,
	inventory InventoryResult,
	verified ImportResult,
) error {
	var state string
	var users, torrentsCount, torrentObjects, skippedTorrents, skippedObjects int64
	var unresolved, torrentArtifacts, imageArtifacts, imageArchiveArtifacts int64
	var importedImages, nonterminalImages, imageMappings int64
	if err := core.QueryRow(ctx, `
SELECT
    run.state,
    count(checkpoint.legacy_id) FILTER (
        WHERE checkpoint.entity_kind = 'user' AND checkpoint.state = 'imported'
    )::bigint,
    count(checkpoint.legacy_id) FILTER (
        WHERE checkpoint.entity_kind = 'torrent' AND checkpoint.state = 'imported'
    )::bigint,
    count(checkpoint.legacy_id) FILTER (
        WHERE checkpoint.entity_kind = 'torrent_object' AND checkpoint.state = 'imported'
    )::bigint,
	count(checkpoint.legacy_id) FILTER (
	    WHERE checkpoint.entity_kind = 'torrent' AND checkpoint.state = 'skipped'
	      AND checkpoint.error_code = 'object_missing_explicitly_excluded'
	)::bigint,
	count(checkpoint.legacy_id) FILTER (
	    WHERE checkpoint.entity_kind = 'torrent_object' AND checkpoint.state = 'skipped'
	      AND checkpoint.error_code = 'object_missing_explicitly_excluded'
	)::bigint,
    (SELECT count(*)::bigint
     FROM migration.discrepancies AS problem
     LEFT JOIN migration.discrepancy_resolutions AS resolution
       ON resolution.discrepancy_id = problem.id
     WHERE problem.run_id = run.id AND resolution.discrepancy_id IS NULL),
    (SELECT count(*)::bigint FROM migration.run_artifacts
     WHERE run_id = run.id AND kind = 'torrent_manifest' AND item_count = $2),
    (SELECT count(*)::bigint FROM migration.run_artifacts
     WHERE run_id = run.id AND kind = 'image_manifest'),
    (SELECT count(*)::bigint FROM migration.run_artifacts
     WHERE run_id = run.id AND kind = 'image_archive'),
    count(checkpoint.legacy_id) FILTER (
        WHERE checkpoint.entity_kind IN ('torrent_image', 'torrent_poster')
          AND checkpoint.state = 'imported'
    )::bigint,
    count(checkpoint.legacy_id) FILTER (
        WHERE checkpoint.entity_kind IN ('torrent_image', 'torrent_poster')
          AND checkpoint.state NOT IN ('imported', 'skipped')
    )::bigint,
    (SELECT count(*)::bigint FROM migration.torrent_image_map
     WHERE first_run_id = run.id)
FROM migration.runs AS run
LEFT JOIN migration.source_rows AS checkpoint ON checkpoint.run_id = run.id
WHERE run.id = $1
GROUP BY run.id`, config.Inventory.RunID, verified.ExpectedTorrents).Scan(
		&state, &users, &torrentsCount, &torrentObjects,
		&skippedTorrents, &skippedObjects, &unresolved, &torrentArtifacts,
		&imageArtifacts, &imageArchiveArtifacts, &importedImages,
		&nonterminalImages, &imageMappings,
	); err != nil {
		return fmt.Errorf("reconcile legacy run checkpoints: %w", err)
	}
	if (state != "imported" && state != "reconciled") || users != inventory.Users ||
		torrentsCount != verified.ExpectedTorrents || torrentObjects != verified.ExpectedTorrents ||
		skippedTorrents != verified.ExcludedTorrents || skippedObjects != verified.ExcludedTorrents ||
		unresolved != 0 || torrentArtifacts != 1 || imageArtifacts != 1 ||
		imageArchiveArtifacts != 1 || importedImages < 1 || imageMappings != importedImages ||
		nonterminalImages != 0 {
		return errors.New("legacy run checkpoints or immutable manifests do not reconcile")
	}
	return nil
}

func reconcileCoreUsers(
	ctx context.Context,
	core *pgxpool.Pool,
	expected int64,
) ([]uuid.UUID, error) {
	rows, err := core.Query(ctx, `
SELECT mapping.credential_ref
FROM migration.user_id_map AS mapping
JOIN identity.users AS source
  ON source.id = mapping.user_id
 AND source.credential_ref = mapping.credential_ref
JOIN identity.tracker_passkey_hmac AS tracker
  ON tracker.user_id = mapping.user_id
 AND tracker.credential_ref = mapping.credential_ref
JOIN migration.user_attendance_openings AS attendance
  ON attendance.source_system = mapping.source_system
 AND attendance.legacy_user_id = mapping.legacy_user_id
 AND attendance.user_id = mapping.user_id
 AND attendance.first_run_id = mapping.first_run_id
JOIN governance.mandates AS mandate
  ON mandate.subject_id = mapping.user_id
 AND mandate.source_type = 'legacy_import'
 AND mandate.source_reference = 'ptyes-user-migration-v1'
 AND mandate.scope_type = $1
 AND mandate.scope_id = $2
 AND mandate.status = 'active'
 AND mandate.starts_at <= now()
 AND now() < mandate.ends_at
JOIN authz.grants AS member_grant
  ON member_grant.subject_id = mapping.user_id
 AND member_grant.mandate_id = mandate.id
 AND member_grant.role_id = 'member'
 AND member_grant.scope_type = $1
 AND member_grant.scope_id = $2
 AND member_grant.valid_from >= mandate.starts_at
 AND member_grant.valid_until <= mandate.ends_at
 AND member_grant.valid_from <= now()
 AND now() < member_grant.valid_until
 AND member_grant.revoked_at IS NULL
WHERE mapping.source_system = 'ptyes'
ORDER BY mapping.legacy_user_id`, authzcontractv1.SiteScopeType, authzcontractv1.SiteScopeID)
	if err != nil {
		return nil, fmt.Errorf("query reconciled legacy Core users: %w", err)
	}
	defer rows.Close()
	references := make([]uuid.UUID, 0, expected)
	for rows.Next() {
		var reference uuid.UUID
		if err := rows.Scan(&reference); err != nil {
			return nil, fmt.Errorf("scan reconciled legacy Core user: %w", err)
		}
		references = append(references, reference)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read reconciled legacy Core users: %w", err)
	}
	if int64(len(references)) != expected {
		return nil, errors.New("legacy Core user, attendance, membership, or Tracker credential counts do not reconcile")
	}
	return references, nil
}

func reconcileVaultUsers(
	ctx context.Context,
	vault *pgxpool.Pool,
	credentialRefs []uuid.UUID,
) (int64, error) {
	var credentials, usernames, emails, passkeys int64
	if err := vault.QueryRow(ctx, `
WITH requested AS (
    SELECT unnest($1::uuid[]) AS credential_ref
)
SELECT
    count(credential.credential_ref)::bigint,
    count(identifier.credential_ref) FILTER (WHERE identifier.kind = 'username')::bigint,
    count(identifier.credential_ref) FILTER (WHERE identifier.kind = 'email')::bigint,
    count(DISTINCT passkey.credential_ref)::bigint
FROM requested
LEFT JOIN vault.credentials AS credential USING (credential_ref)
LEFT JOIN vault.direct_identifiers AS identifier USING (credential_ref)
LEFT JOIN vault.tracker_passkeys AS passkey USING (credential_ref)`, credentialRefs).Scan(
		&credentials, &usernames, &emails, &passkeys,
	); err != nil {
		return 0, fmt.Errorf("reconcile legacy Vault credentials: %w", err)
	}
	// The identifier join produces two credential rows per complete user.
	expected := int64(len(credentialRefs))
	if credentials != expected*2 || usernames != expected || emails != expected || passkeys != expected {
		return 0, errors.New("legacy Vault credential or identifier counts do not reconcile")
	}
	return expected, nil
}

func reconcileCoreTorrents(
	ctx context.Context,
	core *pgxpool.Pool,
	backendID string,
	inventory InventoryResult,
	verified ImportResult,
) (int64, int64, int64, error) {
	var torrentsCount, objects, locations, files, facets, identifiers, groups int64
	var published, catalogRows, outboxRows int64
	if err := core.QueryRow(ctx, `
SELECT
    count(DISTINCT source.id)::bigint,
    count(DISTINCT object.id)::bigint,
    count(DISTINCT location.id)::bigint,
    (SELECT count(*)::bigint
     FROM torrents.torrent_files AS file
     JOIN migration.torrent_id_map AS mapped ON mapped.torrent_id = file.torrent_id
     WHERE mapped.source_system = 'ptyes'),
    (SELECT count(*)::bigint
     FROM torrents.torrent_facet_values AS facet
     JOIN migration.torrent_id_map AS mapped ON mapped.torrent_id = facet.torrent_id
     WHERE mapped.source_system = 'ptyes'),
    (SELECT count(*)::bigint
     FROM torrents.torrent_external_identifiers AS identifier
     JOIN migration.torrent_id_map AS mapped ON mapped.torrent_id = identifier.torrent_id
     WHERE mapped.source_system = 'ptyes'),
    (SELECT count(*)::bigint FROM torrents.resource_groups),
    count(DISTINCT source.id) FILTER (WHERE source.state = 'published')::bigint,
    (SELECT count(*)::bigint
     FROM catalog.torrents AS catalog
     JOIN migration.torrent_id_map AS mapped ON mapped.torrent_id = catalog.id
     WHERE mapped.source_system = 'ptyes'),
    (SELECT count(*)::bigint
     FROM tracker_control.outbox AS event
     JOIN migration.torrent_id_map AS mapped ON mapped.torrent_id = event.aggregate_id
     WHERE mapped.source_system = 'ptyes')
FROM migration.torrent_id_map AS mapping
JOIN torrents.torrents AS source
  ON source.id = mapping.torrent_id
 AND source.object_id = mapping.object_id
JOIN torrents.torrent_objects AS object ON object.id = source.object_id
JOIN torrents.torrent_object_locations AS location
  ON location.object_id = object.id
 AND location.backend_id = $1
 AND location.state = 'verified'
 AND location.is_preferred
 AND location.observed_sha256 = object.content_sha256
 AND location.observed_byte_length = object.byte_length
WHERE mapping.source_system = 'ptyes'`, backendID).Scan(
		&torrentsCount, &objects, &locations, &files, &facets, &identifiers, &groups,
		&published, &catalogRows, &outboxRows,
	); err != nil {
		return 0, 0, 0, fmt.Errorf("reconcile imported Core torrents: %w", err)
	}
	if torrentsCount != verified.ExpectedTorrents || objects != verified.ExpectedTorrents ||
		locations != verified.ExpectedTorrents ||
		facets != verified.FacetValues || identifiers != verified.ExternalIdentifiers || groups != inventory.Groups ||
		published != verified.PublishedTorrents || catalogRows != published || outboxRows != published {
		return 0, 0, 0, errors.New("legacy Core torrent objects, metadata, or projections do not reconcile")
	}
	return published, files, objects, nil
}

func markMigrationReconciled(
	ctx context.Context,
	core *pgxpool.Pool,
	config ReconciliationConfig,
) (string, error) {
	tx, err := core.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("begin legacy reconciliation finalization: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var state string
	var stateChangedAt time.Time
	if err := tx.QueryRow(ctx, `
SELECT state, state_changed_at
FROM migration.runs
WHERE id = $1
FOR UPDATE`, config.Inventory.RunID).Scan(&state, &stateChangedAt); err != nil {
		return "", fmt.Errorf("lock legacy migration run for reconciliation: %w", err)
	}
	if state == "reconciled" {
		return state, tx.Commit(ctx)
	}
	if state != "imported" || config.ReconciledAt.Before(stateChangedAt) {
		return "", errors.New("legacy migration run cannot transition to reconciled at the requested time")
	}
	if _, err := tx.Exec(ctx, `
UPDATE migration.runs
SET state = 'reconciled',
    version = version + 1,
    state_changed_at = $1,
    completed_at = $1
WHERE id = $2 AND state = 'imported'`, config.ReconciledAt, config.Inventory.RunID); err != nil {
		return "", fmt.Errorf("mark legacy migration run reconciled: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit legacy migration reconciliation: %w", err)
	}
	return "reconciled", nil
}
