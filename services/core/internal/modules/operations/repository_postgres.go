package operations

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool                        *pgxpool.Pool
	seedingEvidenceStartsAt     time.Time
	seedingEvidenceClosureDelay time.Duration
}

func NewPostgresRepository(
	pool *pgxpool.Pool,
	seedingEvidenceStartsAt time.Time,
	seedingEvidenceClosureDelay time.Duration,
) (*PostgresRepository, error) {
	_, offset := seedingEvidenceStartsAt.Zone()
	if pool == nil || seedingEvidenceStartsAt.IsZero() || offset != 0 ||
		!seedingEvidenceStartsAt.Equal(seedingEvidenceStartsAt.Truncate(time.Hour)) ||
		seedingEvidenceClosureDelay < time.Minute || seedingEvidenceClosureDelay > time.Hour ||
		seedingEvidenceClosureDelay%time.Second != 0 {
		return nil, ErrInput
	}
	return &PostgresRepository{
		pool:                        pool,
		seedingEvidenceStartsAt:     seedingEvidenceStartsAt,
		seedingEvidenceClosureDelay: seedingEvidenceClosureDelay,
	}, nil
}

func (repository *PostgresRepository) Tracker(ctx context.Context, now time.Time) (TrackerOverview, error) {
	if now.IsZero() {
		return TrackerOverview{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return TrackerOverview{}, fmt.Errorf("begin Tracker operations read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result := TrackerOverview{GeneratedAt: now}
	var controlOldest, controlUpdated pgtype.Timestamptz
	if err := tx.QueryRow(ctx, `
SELECT
    state.last_sequence,
    count(event.event_id) FILTER (WHERE event.projected_at IS NULL),
    count(event.event_id) FILTER (WHERE event.projected_at IS NULL AND event.attempts > 0),
    min(event.created_at) FILTER (WHERE event.projected_at IS NULL),
    state.updated_at
FROM tracker_control.projection_state AS state
LEFT JOIN tracker_control.outbox AS event ON true
WHERE state.singleton
GROUP BY state.last_sequence, state.updated_at`).Scan(
		&result.Control.LastSequence, &result.Control.PendingEvents,
		&result.Control.RetryingEvents, &controlOldest, &controlUpdated,
	); err != nil {
		return TrackerOverview{}, fmt.Errorf("read Tracker control status: %w", err)
	}
	if err := tx.QueryRow(ctx, `
SELECT
    count(*) FILTER (WHERE enabled),
    count(*) FILTER (WHERE NOT enabled)
FROM tracker_control.torrent_allowlist_projection`).Scan(
		&result.Control.EnabledTorrents, &result.Control.DisabledTorrents,
	); err != nil {
		return TrackerOverview{}, fmt.Errorf("read Tracker allowlist status: %w", err)
	}
	result.Control.OldestPendingAt = nullableTime(controlOldest)
	result.Control.UpdatedAt = nullableTime(controlUpdated)

	var swarmObserved, swarmApplied pgtype.Timestamptz
	if err := tx.QueryRow(ctx, `
SELECT
    state.source_id,
    state.routing_epoch,
    state.snapshot_sequence,
    state.observed_at,
    state.applied_at,
    count(run.snapshot_id) FILTER (WHERE run.status = 'collecting'),
    COALESCE((
        SELECT received_chunk_count::text || '/' || chunk_count::text
        FROM catalog.swarm_snapshot_runs
        WHERE status = 'collecting'
        ORDER BY created_at DESC, snapshot_id DESC
        LIMIT 1
    ), '')
FROM catalog.swarm_snapshot_projection_state AS state
LEFT JOIN catalog.swarm_snapshot_runs AS run ON run.status = 'collecting'
WHERE state.singleton
GROUP BY state.source_id, state.routing_epoch, state.snapshot_sequence,
         state.observed_at, state.applied_at`).Scan(
		&result.Swarm.SourceID, &result.Swarm.RoutingEpoch,
		&result.Swarm.SnapshotSequence, &swarmObserved, &swarmApplied,
		&result.Swarm.CollectingRuns, &result.Swarm.LatestRunProgress,
	); err != nil {
		return TrackerOverview{}, fmt.Errorf("read swarm projection status: %w", err)
	}
	result.Swarm.ObservedAt = nullableTime(swarmObserved)
	result.Swarm.AppliedAt = nullableTime(swarmApplied)

	var latestStart, latestEnd pgtype.Timestamptz
	var latestStatus pgtype.Text
	var latestItems pgtype.Int4
	var latestChunks, latestReceived pgtype.Int4
	if err := tx.QueryRow(ctx, `
SELECT
    totals.collecting_windows,
    totals.complete_windows,
    latest.window_start,
    latest.window_end,
    latest.status,
    latest.item_count,
    latest.chunk_count,
    latest.received_chunk_count
FROM (VALUES (true)) AS anchor(singleton)
LEFT JOIN LATERAL (
    SELECT window_start, window_end, status, item_count, chunk_count, received_chunk_count
    FROM economy.seeding_reward_evidence_windows
    ORDER BY window_start DESC
    LIMIT 1
) AS latest ON true
LEFT JOIN LATERAL (
    SELECT
        count(*) FILTER (WHERE status = 'collecting') AS collecting_windows,
        count(*) FILTER (WHERE status = 'complete') AS complete_windows
    FROM economy.seeding_reward_evidence_windows
) AS totals ON true`).Scan(
		&result.Evidence.CollectingWindows, &result.Evidence.CompleteWindows,
		&latestStart, &latestEnd, &latestStatus, &latestItems,
		&latestChunks, &latestReceived,
	); err != nil {
		return TrackerOverview{}, fmt.Errorf("read reward evidence status: %w", err)
	}
	result.Evidence.LatestWindowStart = nullableTime(latestStart)
	result.Evidence.LatestWindowEnd = nullableTime(latestEnd)
	if latestStatus.Valid {
		result.Evidence.LatestStatus = latestStatus.String
	}
	if latestItems.Valid {
		result.Evidence.LatestItemCount = int64(latestItems.Int32)
	}
	if latestChunks.Valid {
		result.Evidence.LatestChunks = latestChunks.Int32
	}
	if latestReceived.Valid {
		result.Evidence.LatestReceived = latestReceived.Int32
	}

	result.Evidence.MonthStartsAt, result.Evidence.CoverageStartsAt,
		result.Evidence.ExpectedThrough, result.Evidence.ExpectedWindows = evidenceCoveragePeriod(
		now, repository.seedingEvidenceStartsAt, repository.seedingEvidenceClosureDelay,
	)
	var monthComplete, monthCollecting int64
	var firstComplete, lastComplete, oldestIncomplete pgtype.Timestamptz
	if err := tx.QueryRow(ctx, `
SELECT
    count(*) FILTER (WHERE status = 'complete')::bigint,
    count(*) FILTER (WHERE status = 'collecting')::bigint,
    min(window_start) FILTER (WHERE status = 'complete'),
    max(window_end) FILTER (WHERE status = 'complete'),
    min(window_start) FILTER (WHERE status = 'collecting')
FROM economy.seeding_reward_evidence_windows
WHERE window_start >= $1 AND window_end <= $2`,
		result.Evidence.CoverageStartsAt, result.Evidence.ExpectedThrough,
	).Scan(&monthComplete, &monthCollecting, &firstComplete, &lastComplete, &oldestIncomplete); err != nil {
		return TrackerOverview{}, fmt.Errorf("read monthly reward evidence coverage: %w", err)
	}
	result.Evidence.MissingWindows = result.Evidence.ExpectedWindows - monthComplete
	if result.Evidence.MissingWindows < 0 {
		return TrackerOverview{}, ErrInvariant
	}
	result.Evidence.OldestIncomplete = nullableTime(oldestIncomplete)
	result.Evidence.Health = evidenceWindowHealth(
		result.Evidence.CoverageStartsAt, result.Evidence.ExpectedThrough,
		result.Evidence.ExpectedWindows, monthComplete, monthCollecting,
		nullableTime(firstComplete), nullableTime(lastComplete),
	)

	var trafficApplied, hnrApplied pgtype.Timestamptz
	if err := tx.QueryRow(ctx, `
-- user_totals preserves one entry_count increment for every applied traffic
-- settlement, even after bounded retention removes per-event inbox evidence.
-- Reading this compact projection avoids repeatedly scanning the multi-GB
-- settlement_inbox from the staff page's polling request.
SELECT COALESCE(sum(entry_count), 0)::bigint, max(updated_at)
FROM traffic.user_totals`).Scan(&result.Consumers.TrafficEntries, &trafficApplied); err != nil {
		return TrackerOverview{}, fmt.Errorf("read traffic consumer status: %w", err)
	}
	if err := tx.QueryRow(ctx, `
SELECT count(*), max(applied_at)
FROM traffic.hnr_projection_inbox`).Scan(&result.Consumers.HNREvents, &hnrApplied); err != nil {
		return TrackerOverview{}, fmt.Errorf("read H&R consumer status: %w", err)
	}
	result.Consumers.TrafficAppliedAt = nullableTime(trafficApplied)
	result.Consumers.HNRAppliedAt = nullableTime(hnrApplied)

	if err := tx.Commit(ctx); err != nil {
		return TrackerOverview{}, fmt.Errorf("commit Tracker operations read: %w", err)
	}
	return result, nil
}

func evidenceCoveragePeriod(now, configuredStart time.Time, closureDelay time.Duration) (time.Time, time.Time, time.Time, int64) {
	monthStart := time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	coverageStart := monthStart
	if configuredStart.After(coverageStart) {
		coverageStart = configuredStart
	}
	// A completed hour is not expected until the ordered announce watermark has
	// crossed that hour's end plus the same safety delay used by Settlement.
	// Keeping this calculation aligned prevents the operations page from
	// reporting a healthy, still-open window as missing.
	expectedThrough := now.UTC().Add(-closureDelay).Truncate(time.Hour)
	if !expectedThrough.After(coverageStart) {
		return monthStart, coverageStart, expectedThrough, 0
	}
	return monthStart, coverageStart, expectedThrough, int64(expectedThrough.Sub(coverageStart) / time.Hour)
}

func evidenceWindowHealth(coverageStart, expectedThrough time.Time, expected, complete, collecting int64, firstComplete, lastComplete *time.Time) EvidenceHealth {
	if expected == 0 {
		return EvidenceHealthHealthy
	}
	if complete == 0 && collecting == 0 {
		return EvidenceHealthUnavailable
	}
	continuous := firstComplete != nil && lastComplete != nil &&
		firstComplete.Equal(coverageStart) &&
		complete == int64(lastComplete.Sub(coverageStart)/time.Hour)
	if collecting > 0 || !continuous {
		return EvidenceHealthBroken
	}
	if lastComplete.Before(expectedThrough) {
		return EvidenceHealthLagging
	}
	return EvidenceHealthHealthy
}

func (repository *PostgresRepository) Workers(ctx context.Context, now time.Time) (WorkerOverview, error) {
	if now.IsZero() {
		return WorkerOverview{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return WorkerOverview{}, fmt.Errorf("begin worker operations read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result := WorkerOverview{GeneratedAt: now, Queues: make([]QueueStatus, 0, 8)}

	reward, err := readQueue(ctx, tx, `
SELECT
    count(*) FILTER (WHERE status = 'pending' AND attempts = 0),
    count(*) FILTER (WHERE status = 'processing'),
    count(*) FILTER (WHERE status = 'pending' AND attempts > 0),
    count(*) FILTER (WHERE status = 'dead'),
    count(*) FILTER (WHERE status = 'completed'),
    min(created_at) FILTER (WHERE status IN ('pending', 'processing')),
    (SELECT last_error_code FROM economy.seeding_reward_work_items WHERE last_error_code IS NOT NULL ORDER BY last_error_at DESC LIMIT 1),
    (SELECT last_error_at FROM economy.seeding_reward_work_items WHERE last_error_code IS NOT NULL ORDER BY last_error_at DESC LIMIT 1)
FROM economy.seeding_reward_work_items`, "seeding_reward", "做种奖励结算")
	if err != nil {
		return WorkerOverview{}, fmt.Errorf("read seeding reward queue: %w", err)
	}
	result.Queues = append(result.Queues, reward)

	promotion, err := readQueue(ctx, tx, `
SELECT
    count(*) FILTER (WHERE delivered_at IS NULL AND attempts = 0 AND lease_token IS NULL),
    count(*) FILTER (WHERE delivered_at IS NULL AND lease_token IS NOT NULL),
    count(*) FILTER (WHERE delivered_at IS NULL AND attempts > 0 AND lease_token IS NULL),
    0,
    count(*) FILTER (WHERE delivered_at IS NOT NULL),
    min(created_at) FILTER (WHERE delivered_at IS NULL),
    (SELECT last_error_code FROM promotion.delivery_outbox WHERE last_error_code IS NOT NULL ORDER BY available_at DESC LIMIT 1),
    NULL::timestamptz
FROM promotion.delivery_outbox`, "promotion_delivery", "优惠政策投递")
	if err != nil {
		return WorkerOverview{}, fmt.Errorf("read promotion delivery queue: %w", err)
	}
	result.Queues = append(result.Queues, promotion)

	hnrPolicy, err := readQueue(ctx, tx, `
SELECT
    count(*) FILTER (WHERE delivered_at IS NULL AND attempts = 0 AND lease_token IS NULL),
    count(*) FILTER (WHERE delivered_at IS NULL AND lease_token IS NOT NULL),
    count(*) FILTER (WHERE delivered_at IS NULL AND attempts > 0 AND lease_token IS NULL),
    0,
    count(*) FILTER (WHERE delivered_at IS NOT NULL),
    min(created_at) FILTER (WHERE delivered_at IS NULL),
    (SELECT last_error_code FROM hnr_control.delivery_outbox WHERE last_error_code IS NOT NULL ORDER BY available_at DESC LIMIT 1),
    NULL::timestamptz
FROM hnr_control.delivery_outbox`, "hnr_policy_delivery", "H&R 政策投递")
	if err != nil {
		return WorkerOverview{}, fmt.Errorf("read H&R policy delivery queue: %w", err)
	}
	result.Queues = append(result.Queues, hnrPolicy)

	benefitDelivery, err := readQueue(ctx, tx, `
WITH commands AS (
    SELECT delivered_at, attempts, lease_token, created_at,
           last_error_code, available_at
    FROM workgroups.settlement_benefit_outbox
    UNION ALL
    SELECT delivered_at, attempts, lease_token, created_at,
           last_error_code, available_at
    FROM identity.settlement_vip_benefit_outbox
)
SELECT
    count(*) FILTER (WHERE delivered_at IS NULL AND attempts = 0 AND lease_token IS NULL),
    count(*) FILTER (WHERE delivered_at IS NULL AND lease_token IS NOT NULL),
    count(*) FILTER (WHERE delivered_at IS NULL AND attempts > 0 AND lease_token IS NULL),
    0,
    count(*) FILTER (WHERE delivered_at IS NOT NULL),
    min(created_at) FILTER (WHERE delivered_at IS NULL),
    (SELECT last_error_code FROM commands WHERE last_error_code IS NOT NULL ORDER BY available_at DESC LIMIT 1),
    NULL::timestamptz
FROM commands`, "entitlement_benefit_delivery", "用户权益结算投递")
	if err != nil {
		return WorkerOverview{}, fmt.Errorf("read entitlement benefit delivery queue: %w", err)
	}
	result.Queues = append(result.Queues, benefitDelivery)

	hnrEnforcement, err := readQueue(ctx, tx, `
WITH pending AS (
    SELECT obligation.obligation_id, obligation.assessment_due_at AS event_at
    FROM traffic.user_hnr_obligations AS obligation
    WHERE obligation.grace_ends_at > obligation.assessment_due_at
      AND $1 >= obligation.assessment_due_at
      AND (obligation.state = 'tracking' OR obligation.satisfied_at > obligation.assessment_due_at)
      AND NOT EXISTS (
          SELECT 1 FROM community.hnr_notifications AS notification
          WHERE notification.obligation_id = obligation.obligation_id
            AND notification.event_kind = 'grace_started'
      )
    UNION ALL
    SELECT obligation.obligation_id, obligation.grace_ends_at
    FROM traffic.user_hnr_obligations AS obligation
    WHERE $1 >= obligation.grace_ends_at
      AND (obligation.state = 'tracking' OR obligation.satisfied_at > obligation.grace_ends_at)
      AND NOT EXISTS (
          SELECT 1 FROM community.hnr_notifications AS notification
          WHERE notification.obligation_id = obligation.obligation_id
            AND notification.event_kind = 'download_restricted'
      )
    UNION ALL
    SELECT obligation.obligation_id, obligation.satisfied_at
    FROM traffic.user_hnr_obligations AS obligation
    WHERE obligation.state = 'satisfied'
      AND obligation.satisfied_at IS NOT NULL
      AND $1 >= obligation.satisfied_at
      AND NOT EXISTS (
          SELECT 1 FROM community.hnr_notifications AS notification
          WHERE notification.obligation_id = obligation.obligation_id
            AND notification.event_kind = 'satisfied'
      )
)
SELECT
    count(pending.obligation_id)::bigint,
    0::bigint,
    0::bigint,
    0::bigint,
    (SELECT count(*)::bigint FROM community.hnr_notifications),
    min(pending.event_at),
    state.last_error_code,
    CASE WHEN state.last_error_code IS NOT NULL THEN state.updated_at END
FROM traffic.hnr_enforcement_worker_state AS state
LEFT JOIN pending ON true
WHERE state.singleton
GROUP BY state.last_error_code, state.updated_at`, "hnr_enforcement", "H&R 状态提醒", now)
	if err != nil {
		return WorkerOverview{}, fmt.Errorf("read H&R enforcement queue: %w", err)
	}
	result.Queues = append(result.Queues, hnrEnforcement)

	control, err := readQueue(ctx, tx, `
SELECT
    count(*) FILTER (WHERE projected_at IS NULL AND attempts = 0 AND lease_token IS NULL),
    count(*) FILTER (WHERE projected_at IS NULL AND lease_token IS NOT NULL),
    count(*) FILTER (WHERE projected_at IS NULL AND attempts > 0 AND lease_token IS NULL),
    0,
    count(*) FILTER (WHERE projected_at IS NOT NULL),
    min(created_at) FILTER (WHERE projected_at IS NULL),
    (SELECT last_error_code FROM tracker_control.outbox WHERE last_error_code IS NOT NULL ORDER BY available_at DESC LIMIT 1),
    NULL::timestamptz
FROM tracker_control.outbox`, "tracker_control", "Tracker 控制投影")
	if err != nil {
		return WorkerOverview{}, fmt.Errorf("read Tracker control queue: %w", err)
	}
	result.Queues = append(result.Queues, control)

	levelPolicyActivation, err := readQueue(ctx, tx, `
SELECT
    count(*) FILTER (WHERE run.policy_version IS NULL AND revision.effective_at <= $1),
    0::bigint,
    0::bigint,
    0::bigint,
    count(run.policy_version),
    min(revision.effective_at) FILTER (WHERE run.policy_version IS NULL),
    NULL::text,
    NULL::timestamptz
FROM progression.level_policy_revisions AS revision
LEFT JOIN progression.level_policy_activation_runs AS run
  ON run.policy_version = revision.policy_version`, "level_policy_activation", "等级规则生效", now)
	if err != nil {
		return WorkerOverview{}, fmt.Errorf("read level policy activation queue: %w", err)
	}
	result.Queues = append(result.Queues, levelPolicyActivation)

	auditQueue, err := readQueue(ctx, tx, `
SELECT
    count(*) FILTER (WHERE delivered_at IS NULL AND attempts = 0 AND (lease_until IS NULL OR lease_until <= $1)),
    count(*) FILTER (WHERE delivered_at IS NULL AND lease_until > $1),
    count(*) FILTER (WHERE delivered_at IS NULL AND attempts > 0 AND (lease_until IS NULL OR lease_until <= $1)),
    0,
    count(*) FILTER (WHERE delivered_at IS NOT NULL),
    min(created_at) FILTER (WHERE delivered_at IS NULL),
    (SELECT left(last_error, 64) FROM audit.outbox WHERE last_error IS NOT NULL ORDER BY available_at DESC LIMIT 1),
    NULL::timestamptz
FROM audit.outbox`, "audit_delivery", "审计事件投递", now)
	if err != nil {
		return WorkerOverview{}, fmt.Errorf("read audit delivery queue: %w", err)
	}
	result.Queues = append(result.Queues, auditQueue)

	if err := tx.Commit(ctx); err != nil {
		return WorkerOverview{}, fmt.Errorf("commit worker operations read: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) Storage(ctx context.Context, now time.Time, activeBackendID string) (StorageInventory, error) {
	if now.IsZero() || activeBackendID == "" {
		return StorageInventory{}, ErrInput
	}
	var result StorageInventory
	err := repository.pool.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM torrents.torrent_objects),
    (SELECT COALESCE(sum(byte_length), 0) FROM torrents.torrent_objects),
    (SELECT count(*) FROM torrents.torrent_screenshot_objects),
    (SELECT COALESCE(sum(byte_length), 0) FROM torrents.torrent_screenshot_objects),
    (SELECT count(*) FROM identity.user_avatar_objects),
    (SELECT COALESCE(sum(byte_length), 0) FROM identity.user_avatar_objects),
    ((SELECT count(*) FROM torrents.torrent_object_locations WHERE backend_id = $1 AND state = 'verified' AND is_preferred)
     + (SELECT count(*) FROM torrents.torrent_screenshot_object_locations WHERE backend_id = $1 AND state = 'verified' AND is_preferred)
     + (SELECT count(*) FROM identity.user_avatar_object_locations WHERE backend_id = $1 AND state = 'verified' AND is_preferred)
     + (SELECT count(*) FROM media.image_derivative_object_locations WHERE backend_id = $1 AND state = 'verified' AND is_preferred)),
    ((SELECT count(*) FROM torrents.torrent_object_locations WHERE backend_id <> $1 AND state = 'verified')
     + (SELECT count(*) FROM torrents.torrent_screenshot_object_locations WHERE backend_id <> $1 AND state = 'verified')
     + (SELECT count(*) FROM identity.user_avatar_object_locations WHERE backend_id <> $1 AND state = 'verified')
     + (SELECT count(*) FROM media.image_derivative_object_locations WHERE backend_id <> $1 AND state = 'verified')),
    ((SELECT count(*) FROM torrents.storage_migrations WHERE status IN ('copying', 'ready_for_cutover', 'retaining', 'cleaning'))
     + (SELECT count(*) FROM storage.migrations WHERE status IN ('copying', 'ready_for_cutover', 'retaining', 'cleaning'))),
    ((SELECT count(*) FROM torrents.storage_migration_items WHERE state IN ('copy_failed', 'cleanup_failed'))
     + (SELECT count(*) FROM storage.migration_items WHERE state IN ('copy_failed', 'cleanup_failed')))`, activeBackendID).Scan(
		&result.TorrentObjects,
		&result.TorrentBytes,
		&result.ScreenshotObjects,
		&result.ScreenshotBytes,
		&result.AvatarObjects,
		&result.AvatarBytes,
		&result.PreferredOnActiveBackend,
		&result.VerifiedOnOtherBackends,
		&result.ActiveMigrations,
		&result.FailedMigrationItems,
	)
	if err != nil {
		return StorageInventory{}, fmt.Errorf("read storage inventory: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) StorageMigrations(ctx context.Context) ([]StorageMigrationOverview, error) {
	rows, err := repository.pool.Query(ctx, `
SELECT migration.id, migration.mode, migration.status, migration.object_kinds,
       migration.source_backend_id, migration.destination_backend_id,
       count(item.id),
       count(item.id) FILTER (WHERE item.state IN ('pending', 'copying')),
       count(item.id) FILTER (WHERE item.state IN ('verified', 'deleting_source', 'cleanup_failed')),
       count(item.id) FILTER (WHERE item.state IN ('copy_failed', 'cleanup_failed')),
       count(item.id) FILTER (WHERE item.state = 'source_deleted'),
       COALESCE((array_agg(item.last_error_code ORDER BY item.updated_at DESC)
           FILTER (WHERE item.last_error_code IS NOT NULL))[1], ''),
       migration.created_at, migration.cutover_at, migration.retention_until,
       migration.cleanup_approved_at, migration.completed_at
FROM storage.migrations AS migration
LEFT JOIN storage.migration_items AS item ON item.migration_id = migration.id
GROUP BY migration.id
ORDER BY migration.created_at DESC, migration.id
LIMIT 10`)
	if err != nil {
		return nil, fmt.Errorf("read storage migration overview: %w", err)
	}
	defer rows.Close()
	var result []StorageMigrationOverview
	for rows.Next() {
		var migration StorageMigrationOverview
		if err := rows.Scan(
			&migration.ID, &migration.Mode, &migration.Status, &migration.ObjectKinds,
			&migration.SourceBackendID, &migration.DestinationBackendID,
			&migration.TotalItems, &migration.PendingItems, &migration.VerifiedItems,
			&migration.FailedItems, &migration.DeletedItems, &migration.LastErrorCode,
			&migration.CreatedAt, &migration.CutoverAt, &migration.RetentionUntil,
			&migration.CleanupApprovedAt, &migration.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("scan storage migration overview: %w", err)
		}
		result = append(result, migration)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate storage migration overview: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) VIPProfile(ctx context.Context, now time.Time) (VIPProfileStats, VIPBenefits, error) {
	if now.IsZero() {
		return VIPProfileStats{}, VIPBenefits{}, ErrInput
	}
	var stats VIPProfileStats
	var benefits VIPBenefits
	err := repository.pool.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM identity.users),
    (SELECT count(*) FROM identity.user_access_states
      WHERE vip_enabled AND (vip_until IS NULL OR vip_until > $1)),
    (SELECT count(*) FROM identity.user_access_states
      WHERE vip_enabled AND vip_until IS NULL),
    (SELECT count(*) FROM identity.user_access_states
      WHERE vip_enabled AND vip_until > $1),
    (SELECT count(*) FROM identity.user_access_states
      WHERE vip_enabled AND vip_until IS NOT NULL AND vip_until <= $1),
    COALESCE(policy.revision, ''),
    COALESCE(policy.vip_bonus_bps, 0),
    COALESCE(ratio.vip_exempt, false)
FROM (VALUES (true)) AS anchor(singleton)
LEFT JOIN LATERAL (
    SELECT revision, vip_bonus_bps
    FROM economy.seeding_reward_policy_revisions
    WHERE effective_from <= $1
    ORDER BY effective_from DESC, revision DESC
    LIMIT 1
) AS policy ON true
LEFT JOIN LATERAL (
    SELECT vip_exempt
    FROM ratio_watch.policy_revisions
    WHERE effective_at <= $1
    ORDER BY effective_at DESC, rule_version DESC
    LIMIT 1
) AS ratio ON true`, now).Scan(
		&stats.TotalUsers,
		&stats.ActiveVIP,
		&stats.PermanentVIP,
		&stats.ExpiringVIP,
		&stats.ExpiredVIP,
		&benefits.SeedingRewardPolicyRevision,
		&benefits.SeedingRewardBonusBPS,
		&benefits.ShareRatioExempt,
	)
	if err != nil {
		return VIPProfileStats{}, VIPBenefits{}, fmt.Errorf("read VIP and profile settings: %w", err)
	}
	// These two entitlements are enforced by the VIP transition transaction and
	// Settlement outbox, rather than mutable page-local feature flags.
	benefits.FreeDownloadEnabled = true
	benefits.NewcomerAssessmentExempt = true
	// Speed observations resolve this same historical VIP timeline. Exceeded
	// intervals wholly covered by VIP are recorded as exempt, never as a user
	// violation or an automatic restriction.
	benefits.SpeedLimitExempt = true
	// VIP benefits and torrent promotions are resolved first; the immutable
	// box accounting factor then applies to the resulting traffic. Free
	// download remains free, while VIP box upload still receives the box factor.
	benefits.SeedboxNoDiscount = false
	return stats, benefits, nil
}

func (repository *PostgresRepository) EconomySettings(ctx context.Context, now time.Time) (EconomyTransactionCounts, error) {
	if now.IsZero() {
		return EconomyTransactionCounts{}, ErrInput
	}
	var result EconomyTransactionCounts
	err := repository.pool.QueryRow(ctx, `
SELECT
    count(*) FILTER (WHERE transaction_type = 'legacy_opening'),
    count(*) FILTER (WHERE transaction_type = 'seeding_reward'),
    count(*) FILTER (WHERE transaction_type = 'activity_reward'),
    count(*) FILTER (WHERE transaction_type = 'torrent_purchase'),
    count(*) FILTER (WHERE transaction_type = 'member_gift'),
    count(*) FILTER (WHERE transaction_type = 'tip'),
    count(*) FILTER (WHERE transaction_type = 'refund'),
    count(*) FILTER (WHERE transaction_type = 'adjustment')
FROM economy.magic_transactions`).Scan(
		&result.LegacyOpening,
		&result.SeedingReward,
		&result.ActivityReward,
		&result.TorrentPurchase,
		&result.MemberGift,
		&result.Tip,
		&result.Refund,
		&result.Adjustment,
	)
	if err != nil {
		return EconomyTransactionCounts{}, fmt.Errorf("read economy settings status: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) TorrentPurchaseRules(ctx context.Context, now time.Time) (TorrentPurchaseRules, error) {
	if now.IsZero() {
		return TorrentPurchaseRules{}, ErrInput
	}
	result := TorrentPurchaseRules{
		CurrencyName: "魔力值", WholeUnitsOnly: true,
		PermanentAccess: true, AtomicSettlement: true,
		RefundConnected: true,
	}
	var effectiveFrom pgtype.Timestamptz
	err := repository.pool.QueryRow(ctx, `
SELECT
    COALESCE(policy.enabled, false),
    COALESCE(policy.tax_basis_points, 0)::bigint,
    COALESCE(policy.revision, ''),
    policy.effective_from,
    (SELECT count(*)::bigint FROM torrents.torrents WHERE purchase_price > 0),
    (SELECT count(*)::bigint FROM economy.torrent_purchase_entitlements WHERE source_kind = 'legacy_import'),
    (SELECT count(*)::bigint FROM economy.torrent_purchase_entitlements WHERE source_kind = 'live_purchase')
FROM (VALUES (true)) AS anchor(singleton)
LEFT JOIN LATERAL (
    SELECT revision, effective_from, enabled, tax_basis_points
    FROM economy.torrent_purchase_policy_revisions
    WHERE effective_from <= $1
    ORDER BY effective_from DESC, revision DESC
    LIMIT 1
) AS policy ON true`, now).Scan(
		&result.Enabled,
		&result.TaxBasisPoints,
		&result.PolicyRevision,
		&effectiveFrom,
		&result.PricedTorrents,
		&result.LegacyEntitlements,
		&result.LiveEntitlements,
	)
	if err != nil {
		return TorrentPurchaseRules{}, fmt.Errorf("read torrent purchase rules: %w", err)
	}
	result.PolicyEffectiveFrom = nullableTime(effectiveFrom)
	return result, nil
}

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readQueue(ctx context.Context, querier rowQuerier, query, id, label string, args ...any) (QueueStatus, error) {
	result := QueueStatus{ID: id, Label: label}
	var oldest, lastErrorAt pgtype.Timestamptz
	var lastError pgtype.Text
	if err := querier.QueryRow(ctx, query, args...).Scan(
		&result.Pending, &result.Processing, &result.Retrying, &result.Dead,
		&result.Completed, &oldest, &lastError, &lastErrorAt,
	); err != nil {
		return QueueStatus{}, err
	}
	result.OldestPendingAt = nullableTime(oldest)
	result.LastErrorAt = nullableTime(lastErrorAt)
	if lastError.Valid {
		result.LastErrorCode = lastError.String
	}
	return result, nil
}

func nullableTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC().Round(0)
	return &result
}

var _ Repository = (*PostgresRepository)(nil)
