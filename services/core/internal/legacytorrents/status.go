package legacytorrents

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

// MigrationStatus is a read-only, run-scoped view of cutover progress. Counts
// are joined through this run's immutable checkpoints so a later rehearsal
// cannot make an older run appear complete by adding global ID mappings.
type MigrationStatus struct {
	RunID                     uuid.UUID
	State                     string
	ExpectedUsers             int64
	ExpectedTorrents          int64
	ImportedUsers             int64
	ImportedTorrents          int64
	ImportedTorrentObjects    int64
	ExcludedTorrents          int64
	ExcludedTorrentObjects    int64
	UnresolvedDiscrepancies   int64
	UserMappings              int64
	TorrentMappings           int64
	VerifiedPreferredObjects  int64
	PurchasePriceOpenings     int64
	PurchaseRows              int64
	PurchaseEntitlements      int64
	PurchaseUnresolved        int64
	MedalDefinitions          int64
	UserMedals                int64
	MedalBenefitUsers         int64
	PositiveMedalBenefitUsers int64
	WorkgroupMemberships      int64
	ReseedMemberships         int64
	ReviewMemberships         int64
	RetentionMemberships      int64
	PendingWorkgroupBenefits  int64
	TrackerRunOutboxEvents    int64
	TrackerPendingEvents      int64
	TrackerProjectionSequence int64
	TrackerOutboxSequence     int64
	TrackerEnabledTorrents    int64
	TrackerSubjectSequence    int64
}

// TrackerProjectionDrained is only a progress hint. The final acceptance gate
// still verifies each allowlist row and the signed artifacts themselves.
func (status MigrationStatus) TrackerProjectionDrained() bool {
	return status.State == "reconciled" && status.TrackerPendingEvents == 0 &&
		status.TrackerProjectionSequence == status.TrackerOutboxSequence
}

// CheckpointsComplete reports whether every source row reached an accepted
// terminal checkpoint and every imported identity/object has its target-side
// evidence. It intentionally does not advance the run or approve exclusions.
func (status MigrationStatus) CheckpointsComplete() bool {
	return status.ImportedUsers == status.ExpectedUsers &&
		status.ImportedTorrents+status.ExcludedTorrents == status.ExpectedTorrents &&
		status.ImportedTorrentObjects == status.ImportedTorrents &&
		status.ExcludedTorrentObjects == status.ExcludedTorrents &&
		status.UnresolvedDiscrepancies == 0 &&
		status.UserMappings == status.ImportedUsers &&
		status.TorrentMappings == status.ImportedTorrents &&
		status.VerifiedPreferredObjects == status.ImportedTorrents
}

// InspectMigrationStatus never locks or mutates migration/domain rows. It is
// safe to run before deciding which resumable cutover phase should run next.
func InspectMigrationStatus(
	ctx context.Context,
	core *pgxpool.Pool,
	config InventoryConfig,
) (MigrationStatus, error) {
	if core == nil || config.RunID == uuid.Nil ||
		config.SnapshotSHA256 == ([32]byte{}) || strings.TrimSpace(config.MappingVersion) == "" {
		return MigrationStatus{}, errors.New("legacy migration status configuration is invalid")
	}
	if err := platformpostgres.RequireCurrentMigration(ctx, core); err != nil {
		return MigrationStatus{}, err
	}

	var snapshot []byte
	var mappingVersion string
	result := MigrationStatus{RunID: config.RunID}
	err := core.QueryRow(ctx, `
WITH checkpoints AS (
    SELECT entity_kind, legacy_id, state, error_code
    FROM migration.source_rows
    WHERE run_id = $1
), checkpoint_counts AS (
    SELECT
        count(*) FILTER (
            WHERE entity_kind = 'user' AND state = 'imported'
        )::bigint AS imported_users,
        count(*) FILTER (
            WHERE entity_kind = 'torrent' AND state = 'imported'
        )::bigint AS imported_torrents,
        count(*) FILTER (
            WHERE entity_kind = 'torrent_object' AND state = 'imported'
        )::bigint AS imported_torrent_objects,
        count(*) FILTER (
            WHERE entity_kind = 'torrent' AND state = 'skipped'
              AND error_code = 'object_missing_explicitly_excluded'
        )::bigint AS excluded_torrents,
        count(*) FILTER (
            WHERE entity_kind = 'torrent_object' AND state = 'skipped'
              AND error_code = 'object_missing_explicitly_excluded'
        )::bigint AS excluded_torrent_objects
    FROM checkpoints
), user_targets AS (
    SELECT count(mapping.legacy_user_id)::bigint AS mapped_users
    FROM checkpoints AS checkpoint
    JOIN migration.user_id_map AS mapping
      ON mapping.source_system = 'ptyes'
     AND mapping.legacy_user_id = checkpoint.legacy_id
    WHERE checkpoint.entity_kind = 'user'
      AND checkpoint.state = 'imported'
), torrent_targets AS (
    SELECT
        count(DISTINCT mapping.legacy_torrent_id)::bigint AS mapped_torrents,
        count(DISTINCT location.object_id) FILTER (
            WHERE location.state = 'verified'
              AND location.is_preferred
              AND location.observed_sha256 = object.content_sha256
              AND location.observed_byte_length = object.byte_length
        )::bigint AS verified_preferred_objects
    FROM checkpoints AS checkpoint
    JOIN migration.torrent_id_map AS mapping
      ON mapping.source_system = 'ptyes'
     AND mapping.legacy_torrent_id = checkpoint.legacy_id
    JOIN torrents.torrents AS torrent
      ON torrent.id = mapping.torrent_id
     AND torrent.object_id = mapping.object_id
    JOIN torrents.torrent_objects AS object ON object.id = torrent.object_id
    LEFT JOIN torrents.torrent_object_locations AS location
      ON location.object_id = object.id
    WHERE checkpoint.entity_kind = 'torrent'
      AND checkpoint.state = 'imported'
), unresolved AS (
    SELECT count(*)::bigint AS count
    FROM migration.discrepancies AS discrepancy
    LEFT JOIN migration.discrepancy_resolutions AS resolution
      ON resolution.discrepancy_id = discrepancy.id
    WHERE discrepancy.run_id = $1
      AND resolution.discrepancy_id IS NULL
), tracker_run AS (
    SELECT count(event.sequence)::bigint AS outbox_events
    FROM tracker_control.outbox AS event
    JOIN migration.torrent_id_map AS mapping
      ON mapping.torrent_id = event.aggregate_id
     AND mapping.first_run_id = $1
), purchase_state AS (
    SELECT
        (SELECT count(*)::bigint
         FROM migration.torrent_purchase_price_openings
         WHERE first_run_id = $1) AS price_openings,
        count(opening.legacy_purchase_id)::bigint AS purchase_rows,
        count(*) FILTER (WHERE opening.disposition = 'entitled')::bigint AS entitlements,
        count(*) FILTER (WHERE opening.disposition IN (
            'unresolved_torrent', 'unmapped_torrent', 'unmapped_user'
        ))::bigint AS unresolved
    FROM migration.torrent_purchase_openings AS opening
    WHERE opening.first_run_id = $1
), medal_state AS (
	SELECT
		(SELECT count(*)::bigint FROM migration.medal_definition_openings WHERE first_run_id = $1) AS definitions,
		(SELECT count(*)::bigint FROM migration.user_medal_openings WHERE first_run_id = $1) AS user_medals,
		(SELECT count(*)::bigint FROM migration.medal_benefit_openings WHERE first_run_id = $1) AS benefit_users,
		(SELECT count(*)::bigint FROM migration.medal_benefit_openings WHERE first_run_id = $1 AND magic_bonus_bps > 0) AS positive_benefit_users,
		(SELECT count(*)::bigint FROM migration.workgroup_membership_openings WHERE first_run_id = $1) AS workgroup_memberships,
		(SELECT count(*)::bigint FROM migration.workgroup_membership_openings WHERE first_run_id = $1 AND group_kind = 'reseed') AS reseed_memberships,
		(SELECT count(*)::bigint FROM migration.workgroup_membership_openings WHERE first_run_id = $1 AND group_kind = 'review') AS review_memberships,
		(SELECT count(*)::bigint FROM migration.workgroup_membership_openings WHERE first_run_id = $1 AND group_kind = 'retention') AS retention_memberships,
		(SELECT count(*)::bigint
		   FROM migration.workgroup_membership_openings AS opening
		   JOIN workgroups.settlement_benefit_outbox AS outbox
		     ON outbox.transition_id = opening.transition_id
		  WHERE opening.first_run_id = $1
		    AND opening.group_kind = 'retention'
		    AND outbox.delivered_at IS NULL) AS pending_workgroup_benefits
), tracker_state AS (
    SELECT
        projection.last_sequence AS projection_sequence,
        (SELECT COALESCE(max(sequence), 0)::bigint FROM tracker_control.outbox) AS outbox_sequence,
        (SELECT count(*)::bigint FROM tracker_control.outbox WHERE projected_at IS NULL) AS pending_events,
        (SELECT count(*)::bigint FROM tracker_control.torrent_allowlist_projection WHERE enabled) AS enabled_torrents,
        subject.last_sequence AS subject_sequence
    FROM tracker_control.projection_state AS projection
    CROSS JOIN tracker_control.subject_snapshot_state AS subject
    WHERE projection.singleton AND subject.singleton
)
SELECT
    run.source_snapshot_sha256,
    run.mapping_version,
    run.state,
    run.expected_user_rows,
    run.expected_torrent_rows,
    checkpoint_counts.imported_users,
    checkpoint_counts.imported_torrents,
    checkpoint_counts.imported_torrent_objects,
    checkpoint_counts.excluded_torrents,
    checkpoint_counts.excluded_torrent_objects,
    unresolved.count,
    user_targets.mapped_users,
    torrent_targets.mapped_torrents,
    torrent_targets.verified_preferred_objects,
    purchase_state.price_openings,
    purchase_state.purchase_rows,
    purchase_state.entitlements,
    purchase_state.unresolved,
    medal_state.definitions,
	medal_state.user_medals,
	medal_state.benefit_users,
	medal_state.positive_benefit_users,
	medal_state.workgroup_memberships,
	medal_state.reseed_memberships,
	medal_state.review_memberships,
	medal_state.retention_memberships,
	medal_state.pending_workgroup_benefits,
	tracker_run.outbox_events,
    tracker_state.pending_events,
    tracker_state.projection_sequence,
    tracker_state.outbox_sequence,
    tracker_state.enabled_torrents,
    tracker_state.subject_sequence
FROM migration.runs AS run
CROSS JOIN checkpoint_counts
CROSS JOIN user_targets
CROSS JOIN torrent_targets
CROSS JOIN unresolved
CROSS JOIN tracker_run
CROSS JOIN purchase_state
CROSS JOIN medal_state
CROSS JOIN tracker_state
WHERE run.id = $1`, config.RunID).Scan(
		&snapshot,
		&mappingVersion,
		&result.State,
		&result.ExpectedUsers,
		&result.ExpectedTorrents,
		&result.ImportedUsers,
		&result.ImportedTorrents,
		&result.ImportedTorrentObjects,
		&result.ExcludedTorrents,
		&result.ExcludedTorrentObjects,
		&result.UnresolvedDiscrepancies,
		&result.UserMappings,
		&result.TorrentMappings,
		&result.VerifiedPreferredObjects,
		&result.PurchasePriceOpenings,
		&result.PurchaseRows,
		&result.PurchaseEntitlements,
		&result.PurchaseUnresolved,
		&result.MedalDefinitions,
		&result.UserMedals,
		&result.MedalBenefitUsers,
		&result.PositiveMedalBenefitUsers,
		&result.WorkgroupMemberships,
		&result.ReseedMemberships,
		&result.ReviewMemberships,
		&result.RetentionMemberships,
		&result.PendingWorkgroupBenefits,
		&result.TrackerRunOutboxEvents,
		&result.TrackerPendingEvents,
		&result.TrackerProjectionSequence,
		&result.TrackerOutboxSequence,
		&result.TrackerEnabledTorrents,
		&result.TrackerSubjectSequence,
	)
	if err != nil {
		return MigrationStatus{}, fmt.Errorf("read legacy migration status: %w", err)
	}
	if !bytes.Equal(snapshot, config.SnapshotSHA256[:]) || mappingVersion != config.MappingVersion {
		return MigrationStatus{}, errors.New("legacy migration status does not match the requested snapshot identity")
	}
	return result, nil
}
