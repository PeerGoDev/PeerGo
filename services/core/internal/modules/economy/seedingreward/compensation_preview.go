package seedingreward

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	CompensationPreviewSchemaVersion = "seeding.reward.compensation.preview.v1"
	compensationMaxIntervalCredit    = 35 * time.Minute
)

// CompensationPreviewProgress contains counts only. It is safe for operator
// logs because it deliberately excludes member and torrent identifiers.
type CompensationPreviewProgress struct {
	WindowStart         time.Time
	WindowIndex         int
	WindowCount         int
	AffectedUserHours   int64
	PositiveCorrections int64
}

// CompensationPreviewSummary describes a deterministic, read-only artifact.
// ArtifactSHA256 is filled by the command after it hashes the exact JSONL
// bytes written by PreviewHistoricalCompensation.
type CompensationPreviewSummary struct {
	SchemaVersion        string    `json:"schema_version"`
	FirstWindow          time.Time `json:"first_window"`
	LastWindow           time.Time `json:"last_window"`
	TrackerSourceStream  string    `json:"tracker_source_stream"`
	TrackerFenceSequence int64     `json:"tracker_fence_sequence"`
	AffectedWindows      int       `json:"affected_windows"`
	AffectedUsers        int       `json:"affected_users"`
	AffectedUserHours    int64     `json:"affected_user_hours"`
	PositiveCorrections  int64     `json:"positive_corrections"`
	ZeroDeltaUserHours   int64     `json:"zero_delta_user_hours"`
	MagicDelta           int64     `json:"magic_delta"`
	ExperienceDelta      string    `json:"experience_delta"`
	MaximumMagicDelta    int64     `json:"maximum_magic_delta"`
	ArtifactSHA256       string    `json:"artifact_sha256,omitempty"`
}

type compensationArtifactHeader struct {
	SchemaVersion          string `json:"schema_version"`
	RecordType             string `json:"record_type"`
	TrackerSourceStream    string `json:"tracker_source_stream"`
	TrackerFenceSequence   int64  `json:"tracker_fence_sequence"`
	MaximumIntervalSeconds int64  `json:"maximum_interval_credit_seconds"`
	FirstWindow            string `json:"first_window"`
	LastWindow             string `json:"last_window"`
}

type compensationArtifactRecord struct {
	SchemaVersion              string `json:"schema_version"`
	RecordType                 string `json:"record_type"`
	SourceReference            string `json:"source_reference"`
	WindowStart                string `json:"window_start"`
	UserID                     string `json:"user_id"`
	PolicyRevision             string `json:"policy_revision"`
	BenefitRevision            string `json:"benefit_revision"`
	OriginalCalculationSHA256  string `json:"original_calculation_sha256,omitempty"`
	CorrectedCalculationSHA256 string `json:"corrected_calculation_sha256"`
	CorrectedEvidenceSHA256    string `json:"corrected_evidence_sha256"`
	OriginalReward             int64  `json:"original_reward"`
	CorrectedReward            int64  `json:"corrected_reward"`
	MagicDelta                 int64  `json:"magic_delta"`
	ExperienceDelta            string `json:"experience_delta"`
	EligibleTorrentCount       int32  `json:"eligible_torrent_count"`
	Capped                     bool   `json:"capped"`
}

type correctedTrackerItem struct {
	UserID          uuid.UUID
	TorrentID       int64
	InfoHashV1      [20]byte
	ActiveSeconds   int64
	RawUploaded     int64
	SnapshotSeeders int32
	SourceCount     int64
	FirstSequence   int64
	LastSequence    int64
	EvidenceSHA256  [sha256.Size]byte
}

type correctedTrackerItemDocument struct {
	SchemaVersion   string `json:"schema_version"`
	WindowStart     string `json:"window_start"`
	UserID          string `json:"user_id"`
	TorrentID       int64  `json:"torrent_id"`
	InfoHashV1      string `json:"info_hash_v1"`
	ActiveSeconds   int64  `json:"active_seconds"`
	RawUploaded     int64  `json:"raw_uploaded"`
	SnapshotSeeders int32  `json:"snapshot_seeders"`
	SourceCount     int64  `json:"source_count"`
	FirstSequence   int64  `json:"first_source_sequence"`
	LastSequence    int64  `json:"last_source_sequence"`
}

type correctedWindowDocument struct {
	SchemaVersion        string   `json:"schema_version"`
	WindowStart          string   `json:"window_start"`
	OriginalEvidence     string   `json:"original_evidence_sha256"`
	TrackerFenceSequence int64    `json:"tracker_fence_sequence"`
	Items                []string `json:"corrected_item_sha256"`
}

type originalCalculation struct {
	PolicyRevision    string
	CalculationDigest [sha256.Size]byte
	Reward            int64
}

// PreviewHistoricalCompensation reconstructs the affected v1 evidence hours
// from immutable Tracker intervals and evaluates them with Core's original
// policy, metadata and benefit facts. It never writes either database. The
// JSONL artifact contains private user identifiers and must be stored with
// operator-only permissions; stdout callers should expose only the summary.
func (repository *PostgresSettlementRepository) PreviewHistoricalCompensation(
	ctx context.Context,
	trackerPool *pgxpool.Pool,
	output io.Writer,
	progress func(CompensationPreviewProgress),
) (CompensationPreviewSummary, error) {
	if repository == nil || repository.pool == nil || trackerPool == nil || output == nil {
		return CompensationPreviewSummary{}, ErrInput
	}
	trackerTx, err := trackerPool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return CompensationPreviewSummary{}, fmt.Errorf("begin Tracker compensation preview: %w", err)
	}
	defer func() { _ = trackerTx.Rollback(ctx) }()
	coreTx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return CompensationPreviewSummary{}, fmt.Errorf("begin Core compensation preview: %w", err)
	}
	defer func() { _ = coreTx.Rollback(ctx) }()

	stream, fence, windows, err := compensationWindows(ctx, trackerTx)
	if err != nil {
		return CompensationPreviewSummary{}, err
	}
	summary := CompensationPreviewSummary{
		SchemaVersion: CompensationPreviewSchemaVersion, TrackerSourceStream: stream,
		TrackerFenceSequence: fence, AffectedWindows: len(windows),
	}
	if len(windows) == 0 {
		summary.ExperienceDelta = "0"
		if err := json.NewEncoder(output).Encode(compensationArtifactHeader{
			SchemaVersion: CompensationPreviewSchemaVersion, RecordType: "manifest",
			TrackerSourceStream: stream, TrackerFenceSequence: fence,
			MaximumIntervalSeconds: int64(compensationMaxIntervalCredit / time.Second),
		}); err != nil {
			return CompensationPreviewSummary{}, fmt.Errorf("write empty compensation preview: %w", err)
		}
		return summary, nil
	}
	summary.FirstWindow, summary.LastWindow = windows[0], windows[len(windows)-1]
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(compensationArtifactHeader{
		SchemaVersion: CompensationPreviewSchemaVersion, RecordType: "manifest",
		TrackerSourceStream: stream, TrackerFenceSequence: fence,
		MaximumIntervalSeconds: int64(compensationMaxIntervalCredit / time.Second),
		FirstWindow:            windows[0].Format(time.RFC3339), LastWindow: windows[len(windows)-1].Format(time.RFC3339),
	}); err != nil {
		return CompensationPreviewSummary{}, fmt.Errorf("write compensation preview manifest: %w", err)
	}

	affectedUsers := make(map[uuid.UUID]struct{})
	experienceUnits := int64(0)
	for index, windowStart := range windows {
		window, err := readCompensationRewardWindow(ctx, coreTx, windowStart)
		if err != nil {
			return CompensationPreviewSummary{}, fmt.Errorf("read compensation window %s: %w", windowStart.Format(time.RFC3339), err)
		}
		publishedPolicy, found, err := readPolicy(ctx, coreTx, `
WHERE effective_from <= $1
ORDER BY effective_from DESC
LIMIT 1`, window.Start)
		if err != nil {
			return CompensationPreviewSummary{}, err
		}
		if !found {
			return CompensationPreviewSummary{}, ErrPolicyNotFound
		}
		trackerItems, err := correctedTrackerItems(ctx, trackerTx, window.Start, fence)
		if err != nil {
			return CompensationPreviewSummary{}, fmt.Errorf(
				"rebuild compensation Tracker items for %s: %w", window.Start.Format(time.RFC3339), err,
			)
		}
		if len(trackerItems) == 0 {
			return CompensationPreviewSummary{}, ErrInvariant
		}
		correctedEvidence, err := correctedWindowSHA256(window, fence, trackerItems)
		if err != nil {
			return CompensationPreviewSummary{}, err
		}
		metadata, err := compensationMetadata(ctx, coreTx, window.Start, trackerItems)
		if err != nil {
			return CompensationPreviewSummary{}, fmt.Errorf(
				"read compensation metadata for %s: %w", window.Start.Format(time.RFC3339), err,
			)
		}
		grouped := groupCorrectedItems(trackerItems, metadata)
		userIDs := make([]uuid.UUID, 0, len(grouped))
		for userID := range grouped {
			userIDs = append(userIDs, userID)
		}
		slices.SortFunc(userIDs, func(left, right uuid.UUID) int { return slices.Compare(left[:], right[:]) })
		benefits, err := repository.compensationBenefits(ctx, coreTx, window.Start, userIDs)
		if err != nil {
			return CompensationPreviewSummary{}, fmt.Errorf(
				"read compensation benefits for %s: %w", window.Start.Format(time.RFC3339), err,
			)
		}
		original, err := compensationOriginalCalculations(ctx, coreTx, window.Start)
		if err != nil {
			return CompensationPreviewSummary{}, err
		}

		for _, userID := range userIDs {
			summary.AffectedUserHours++
			affectedUsers[userID] = struct{}{}
			calculation, err := Calculate(publishedPolicy.Policy, CalculationInput{
				UserID: userID, WindowStart: window.Start, WindowEnd: window.End,
				WindowEvidenceSHA256: correctedEvidence,
				SnapshotID:           window.SnapshotID, SnapshotSequence: window.SnapshotSequence,
				SnapshotObservedAt: window.SnapshotObservedAt,
				Benefits:           benefits[userID], Items: grouped[userID],
			})
			if err != nil {
				return CompensationPreviewSummary{}, fmt.Errorf("calculate compensation preview: %w", err)
			}
			prior := original[userID]
			if prior.PolicyRevision != "" && prior.PolicyRevision != publishedPolicy.Policy.Revision {
				return CompensationPreviewSummary{}, ErrInvariant
			}
			if calculation.Reward < prior.Reward {
				return CompensationPreviewSummary{}, ErrInvariant
			}
			delta := calculation.Reward - prior.Reward
			if delta == 0 {
				summary.ZeroDeltaUserHours++
				continue
			}
			experienceBPS := publishedPolicy.Policy.ExperiencePerMagicBPS
			if experienceBPS < 0 || summary.MagicDelta > math.MaxInt64-delta ||
				(experienceBPS > 0 && (delta > math.MaxInt64/experienceBPS ||
					experienceUnits > math.MaxInt64-delta*experienceBPS)) {
				return CompensationPreviewSummary{}, ErrInvariant
			}
			summary.PositiveCorrections++
			summary.MagicDelta += delta
			if delta > summary.MaximumMagicDelta {
				summary.MaximumMagicDelta = delta
			}
			experienceUnits += delta * experienceBPS
			experienceDelta, err := basisPointAmount(delta, experienceBPS)
			if err != nil {
				return CompensationPreviewSummary{}, err
			}
			record := compensationArtifactRecord{
				SchemaVersion: CompensationPreviewSchemaVersion, RecordType: "positive_delta",
				SourceReference: fmt.Sprintf("seeding_compensation:v1:%d:%s", window.Start.Unix(), userID.String()),
				WindowStart:     window.Start.Format(time.RFC3339), UserID: userID.String(),
				PolicyRevision: publishedPolicy.Policy.Revision, BenefitRevision: benefits[userID].Revision,
				CorrectedCalculationSHA256: hex.EncodeToString(calculation.CalculationSHA256[:]),
				CorrectedEvidenceSHA256:    hex.EncodeToString(correctedEvidence[:]),
				OriginalReward:             prior.Reward, CorrectedReward: calculation.Reward, MagicDelta: delta,
				ExperienceDelta: experienceDelta, EligibleTorrentCount: calculation.EligibleTorrentCount,
				Capped: calculation.Capped,
			}
			if prior.CalculationDigest != ([sha256.Size]byte{}) {
				record.OriginalCalculationSHA256 = hex.EncodeToString(prior.CalculationDigest[:])
			}
			if err := encoder.Encode(record); err != nil {
				return CompensationPreviewSummary{}, fmt.Errorf("write compensation preview record: %w", err)
			}
		}
		if progress != nil {
			progress(CompensationPreviewProgress{
				WindowStart: window.Start, WindowIndex: index + 1, WindowCount: len(windows),
				AffectedUserHours: summary.AffectedUserHours, PositiveCorrections: summary.PositiveCorrections,
			})
		}
	}
	summary.AffectedUsers = len(affectedUsers)
	summary.ExperienceDelta, err = basisPointUnits(experienceUnits)
	if err != nil {
		return CompensationPreviewSummary{}, err
	}
	if err := coreTx.Commit(ctx); err != nil {
		return CompensationPreviewSummary{}, fmt.Errorf("finish Core compensation preview snapshot: %w", err)
	}
	if err := trackerTx.Commit(ctx); err != nil {
		return CompensationPreviewSummary{}, fmt.Errorf("finish Tracker compensation preview snapshot: %w", err)
	}
	return summary, nil
}

func compensationWindows(ctx context.Context, tx pgx.Tx) (string, int64, []time.Time, error) {
	rows, err := tx.Query(ctx, `
SELECT DISTINCT anomaly.window_start, evidence_window.announce_source_stream
FROM ledger.seeding_evidence_anomalies AS anomaly
JOIN ledger.seeding_evidence_windows AS evidence_window USING (window_start)
JOIN ledger.raw_session_intervals AS raw ON raw.event_id = anomaly.interval_event_id
WHERE evidence_window.schema_version = 'seeding.evidence.v1'
  AND raw.previous_left = 0
  AND raw.current_left = 0
  AND raw.ends_at <= raw.starts_at + ($1 * interval '1 second')
ORDER BY anomaly.window_start`, int64(compensationMaxIntervalCredit/time.Second))
	if err != nil {
		return "", 0, nil, fmt.Errorf("list compensation windows: %w", err)
	}
	defer rows.Close()
	windows := make([]time.Time, 0, 24)
	stream := ""
	for rows.Next() {
		var window time.Time
		var rowStream string
		if err := rows.Scan(&window, &rowStream); err != nil {
			return "", 0, nil, fmt.Errorf("scan compensation window: %w", err)
		}
		if rowStream == "" || (stream != "" && stream != rowStream) {
			return "", 0, nil, ErrInvariant
		}
		stream = rowStream
		windows = append(windows, canonicalTime(window))
	}
	if err := rows.Err(); err != nil {
		return "", 0, nil, fmt.Errorf("finish compensation windows: %w", err)
	}
	if len(windows) == 0 {
		return "", 0, windows, nil
	}

	// The announce consumer is ordered, but the preview independently proves
	// that its repeatable-read snapshot contains a gap-free terminal prefix.
	// This is the compensation watermark: a later live announce may advance a
	// future preview, while no absent lower sequence can silently change this
	// artifact after approval.
	var first, fence, count, processing int64
	if err := tx.QueryRow(ctx, `
SELECT
    coalesce(min(source_sequence), 0),
    coalesce(max(source_sequence), 0),
    count(*)::bigint,
    count(*) FILTER (WHERE outcome = 'processing')::bigint
FROM settlement.event_inbox
WHERE source_stream = $1`, stream).Scan(&first, &fence, &count, &processing); err != nil {
		return "", 0, nil, fmt.Errorf("read compensation Tracker fence: %w", err)
	}
	if first != 1 || fence < 1 || count != fence || processing != 0 {
		return "", 0, nil, fmt.Errorf("read compensation Tracker fence: %w", ErrInvariant)
	}
	return stream, fence, windows, nil
}

// readCompensationRewardWindow mirrors readRewardWindow without a row lock.
// PostgreSQL intentionally rejects FOR SHARE in a read-only transaction; the
// underlying reward evidence is already protected by immutable triggers.
func readCompensationRewardWindow(ctx context.Context, tx pgx.Tx, start time.Time) (rewardWindow, error) {
	var result rewardWindow
	var evidence []byte
	var status string
	err := tx.QueryRow(ctx, `
SELECT window_start, window_end, window_evidence_sha256,
       snapshot_id, snapshot_sequence, snapshot_observed_at, status
FROM economy.seeding_reward_evidence_windows
WHERE window_start = $1`, start).Scan(&result.Start, &result.End, &evidence, &result.SnapshotID,
		&result.SnapshotSequence, &result.SnapshotObservedAt, &status)
	if err != nil {
		if err == pgx.ErrNoRows {
			return rewardWindow{}, ErrInvariant
		}
		return rewardWindow{}, fmt.Errorf("read compensation reward window: %w", err)
	}
	if status != "complete" || len(evidence) != sha256.Size {
		return rewardWindow{}, ErrInvariant
	}
	copy(result.EvidenceSHA256[:], evidence)
	result.Start, result.End = canonicalTime(result.Start), canonicalTime(result.End)
	result.SnapshotObservedAt = canonicalTime(result.SnapshotObservedAt)
	return result, nil
}

func correctedTrackerItems(ctx context.Context, tx pgx.Tx, windowStart time.Time, fence int64) ([]correctedTrackerItem, error) {
	// Preserve every source range that formed the immutable v1 item, including
	// the credibility semantics in force when it closed. Compensation adds only
	// credible post-fence anomalies; it must not retroactively apply v2's gap
	// filter to facts for which a member has already been credited.
	rows, err := tx.Query(ctx, `
WITH affected_users AS MATERIALIZED (
    SELECT DISTINCT raw.user_id
    FROM ledger.seeding_evidence_anomalies AS anomaly
    JOIN ledger.raw_session_intervals AS raw ON raw.event_id = anomaly.interval_event_id
    WHERE anomaly.window_start = $1
      AND anomaly.source_sequence <= $2
      AND raw.previous_left = 0
      AND raw.current_left = 0
      AND raw.ends_at <= raw.starts_at + ($3 * interval '1 second')
), original_facts AS MATERIALIZED (
    SELECT
        source.user_id,
        source.torrent_id,
        raw.info_hash_v1,
        source.clipped_starts_at AS starts_at,
        source.clipped_ends_at AS ends_at,
        raw.raw_uploaded,
        source.source_sequence
    FROM ledger.seeding_evidence_sources AS source
    JOIN affected_users ON affected_users.user_id = source.user_id
    JOIN ledger.raw_session_intervals AS raw ON raw.event_id = source.interval_event_id
    WHERE source.window_start = $1
), late_facts AS MATERIALIZED (
    SELECT
        raw.user_id,
        raw.torrent_id,
        raw.info_hash_v1,
        greatest(raw.starts_at, evidence_window.window_start) AS starts_at,
        least(raw.ends_at, evidence_window.window_end) AS ends_at,
        raw.raw_uploaded,
        anomaly.source_sequence
    FROM ledger.seeding_evidence_anomalies AS anomaly
    JOIN ledger.seeding_evidence_windows AS evidence_window USING (window_start)
    JOIN ledger.raw_session_intervals AS raw ON raw.event_id = anomaly.interval_event_id
    JOIN affected_users ON affected_users.user_id = raw.user_id
    WHERE anomaly.window_start = $1
      AND anomaly.source_sequence <= $2
      AND raw.previous_left = 0
      AND raw.current_left = 0
      AND raw.ends_at <= raw.starts_at + ($3 * interval '1 second')
), facts AS MATERIALIZED (
    SELECT * FROM original_facts
    UNION ALL
    SELECT * FROM late_facts
), grouped AS (
    SELECT
        facts.user_id,
        facts.torrent_id,
        facts.info_hash_v1,
        sum(facts.raw_uploaded)::bigint AS raw_uploaded,
        coalesce(max(snapshot.seeders), 0)::integer AS snapshot_seeders,
        count(*)::bigint AS source_count,
        min(facts.source_sequence)::bigint AS first_sequence,
        max(facts.source_sequence)::bigint AS last_sequence,
        range_agg(tstzrange(facts.starts_at, facts.ends_at, '[)')) AS active_ranges,
        coalesce(max(original.active_seconds), 0)::bigint AS original_active_seconds,
        coalesce(max(original.raw_uploaded), 0)::bigint AS original_raw_uploaded
    FROM facts
    JOIN ledger.seeding_evidence_windows AS evidence_window
      ON evidence_window.window_start = $1
    LEFT JOIN ledger.seeding_swarm_snapshot_entries AS snapshot
      ON snapshot.snapshot_id = evidence_window.selected_snapshot_id
     AND snapshot.info_hash_v1 = facts.info_hash_v1
    LEFT JOIN ledger.seeding_evidence_items AS original
      ON original.window_start = evidence_window.window_start
     AND original.user_id = facts.user_id
     AND original.torrent_id = facts.torrent_id
    WHERE facts.ends_at > facts.starts_at
    GROUP BY facts.user_id, facts.torrent_id, facts.info_hash_v1
)
SELECT
    grouped.user_id,
    grouped.torrent_id,
    grouped.info_hash_v1,
    (
        SELECT sum(extract(epoch FROM (upper(piece) - lower(piece))))::bigint
        FROM unnest(grouped.active_ranges) AS piece
    ) AS active_seconds,
    grouped.raw_uploaded,
    grouped.snapshot_seeders,
    grouped.source_count,
    grouped.first_sequence,
    grouped.last_sequence,
    grouped.original_active_seconds,
    grouped.original_raw_uploaded
FROM grouped
ORDER BY grouped.user_id, grouped.torrent_id, grouped.info_hash_v1`,
		windowStart, fence, int64(compensationMaxIntervalCredit/time.Second))
	if err != nil {
		return nil, fmt.Errorf("read corrected Tracker items: %w", err)
	}
	defer rows.Close()
	items := make([]correctedTrackerItem, 0, 65536)
	for rows.Next() {
		var item correctedTrackerItem
		var infoHash []byte
		var originalActiveSeconds, originalRawUploaded int64
		if err := rows.Scan(
			&item.UserID, &item.TorrentID, &infoHash, &item.ActiveSeconds,
			&item.RawUploaded, &item.SnapshotSeeders, &item.SourceCount,
			&item.FirstSequence, &item.LastSequence, &originalActiveSeconds, &originalRawUploaded,
		); err != nil {
			return nil, fmt.Errorf("scan corrected Tracker item: %w", err)
		}
		if item.UserID == uuid.Nil || item.TorrentID < 1 || len(infoHash) != len(item.InfoHashV1) ||
			item.ActiveSeconds < 1 || item.ActiveSeconds > 3600 || item.RawUploaded < 0 ||
			item.SnapshotSeeders < 0 || item.SourceCount < 1 || item.FirstSequence < 1 ||
			item.LastSequence < item.FirstSequence || item.LastSequence > fence ||
			item.ActiveSeconds < originalActiveSeconds || item.RawUploaded < originalRawUploaded {
			return nil, ErrInvariant
		}
		copy(item.InfoHashV1[:], infoHash)
		document := correctedTrackerItemDocument{
			SchemaVersion: CompensationPreviewSchemaVersion, WindowStart: windowStart.Format(time.RFC3339),
			UserID: item.UserID.String(), TorrentID: item.TorrentID,
			InfoHashV1: hex.EncodeToString(item.InfoHashV1[:]), ActiveSeconds: item.ActiveSeconds,
			RawUploaded: item.RawUploaded, SnapshotSeeders: item.SnapshotSeeders,
			SourceCount: item.SourceCount, FirstSequence: item.FirstSequence, LastSequence: item.LastSequence,
		}
		encoded, err := json.Marshal(document)
		if err != nil {
			return nil, ErrInvariant
		}
		item.EvidenceSHA256 = sha256.Sum256(encoded)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish corrected Tracker items: %w", err)
	}
	return items, nil
}

func correctedWindowSHA256(window rewardWindow, fence int64, items []correctedTrackerItem) ([sha256.Size]byte, error) {
	digests := make([]string, len(items))
	for index, item := range items {
		digests[index] = hex.EncodeToString(item.EvidenceSHA256[:])
	}
	document := correctedWindowDocument{
		SchemaVersion: CompensationPreviewSchemaVersion, WindowStart: window.Start.Format(time.RFC3339),
		OriginalEvidence: hex.EncodeToString(window.EvidenceSHA256[:]), TrackerFenceSequence: fence,
		Items: digests,
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return [sha256.Size]byte{}, ErrInvariant
	}
	return sha256.Sum256(encoded), nil
}

func compensationMetadata(ctx context.Context, tx pgx.Tx, windowStart time.Time, trackerItems []correctedTrackerItem) (map[int64]ItemInput, error) {
	torrentIDs := make([]int64, 0, len(trackerItems))
	for _, item := range trackerItems {
		if len(torrentIDs) == 0 || torrentIDs[len(torrentIDs)-1] != item.TorrentID {
			torrentIDs = append(torrentIDs, item.TorrentID)
		}
	}
	slices.Sort(torrentIDs)
	torrentIDs = slices.Compact(torrentIDs)
	rows, err := tx.Query(ctx, `
SELECT
    torrent.id,
    torrent.total_size_bytes,
    torrent.published_at,
    coalesce(snapshot.official, false),
    snapshot.metadata_sha256
FROM torrents.torrents AS torrent
LEFT JOIN economy.seeding_reward_metadata_snapshots AS snapshot
  ON snapshot.window_start = $1
 AND snapshot.torrent_id = torrent.id
WHERE torrent.id = ANY($2::bigint[])
ORDER BY torrent.id`, windowStart, torrentIDs)
	if err != nil {
		return nil, fmt.Errorf("read compensation torrent metadata: %w", err)
	}
	defer rows.Close()
	result := make(map[int64]ItemInput, len(torrentIDs))
	for rows.Next() {
		var item ItemInput
		var digest []byte
		if err := rows.Scan(&item.TorrentID, &item.SizeBytes, &item.PublishedAt, &item.Official, &digest); err != nil {
			return nil, fmt.Errorf("scan compensation torrent metadata: %w", err)
		}
		item.PublishedAt = canonicalTime(item.PublishedAt)
		if item.TorrentID < 1 || item.SizeBytes < 1 || item.PublishedAt.IsZero() {
			return nil, ErrInvariant
		}
		if len(digest) == sha256.Size {
			copy(item.MetadataSHA256[:], digest)
		} else if len(digest) == 0 {
			encoded, err := json.Marshal(metadataDocument{
				TorrentID: item.TorrentID, SizeBytes: item.SizeBytes,
				PublishedAt: item.PublishedAt.Format(time.RFC3339Nano), Official: item.Official,
			})
			if err != nil {
				return nil, ErrInvariant
			}
			item.MetadataSHA256 = sha256.Sum256(encoded)
		} else {
			return nil, ErrInvariant
		}
		result[item.TorrentID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish compensation torrent metadata: %w", err)
	}
	if len(result) != len(torrentIDs) {
		return nil, ErrInvariant
	}
	return result, nil
}

func groupCorrectedItems(trackerItems []correctedTrackerItem, metadata map[int64]ItemInput) map[uuid.UUID][]ItemInput {
	result := make(map[uuid.UUID][]ItemInput)
	for _, tracker := range trackerItems {
		item := metadata[tracker.TorrentID]
		item.ActiveSeconds = tracker.ActiveSeconds
		item.RawUploadedBytes = tracker.RawUploaded
		item.SnapshotSeeders = tracker.SnapshotSeeders
		item.TrackerEvidenceSHA256 = tracker.EvidenceSHA256
		result[tracker.UserID] = append(result[tracker.UserID], item)
	}
	return result
}

func (repository *PostgresSettlementRepository) compensationBenefits(ctx context.Context, tx pgx.Tx, windowStart time.Time, userIDs []uuid.UUID) (map[uuid.UUID]BenefitInput, error) {
	rows, err := tx.Query(ctx, `
SELECT
    user_id,
    benefit_revision,
    benefit_sha256,
    vip_active,
    medal_bonus_bps,
    level_bonus_bps,
    level_seeding_count_bonus
FROM economy.seeding_reward_benefit_snapshots
WHERE window_start = $1
  AND user_id = ANY($2::uuid[])
ORDER BY user_id`, windowStart, userIDs)
	if err != nil {
		return nil, fmt.Errorf("read compensation benefit snapshots: %w", err)
	}
	result := make(map[uuid.UUID]BenefitInput, len(userIDs))
	for rows.Next() {
		var userID uuid.UUID
		var benefit BenefitInput
		var digest []byte
		if err := rows.Scan(
			&userID, &benefit.Revision, &digest, &benefit.VIPActive,
			&benefit.MedalBonusBPS, &benefit.LevelBonusBPS, &benefit.LevelLinearTorrentBonus,
		); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan compensation benefit snapshot: %w", err)
		}
		if userID == uuid.Nil || len(digest) != sha256.Size {
			rows.Close()
			return nil, ErrInvariant
		}
		copy(benefit.SnapshotSHA256[:], digest)
		result[userID] = benefit
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("finish compensation benefit snapshots: %w", err)
	}
	rows.Close()
	for _, userID := range userIDs {
		if _, exists := result[userID]; exists {
			continue
		}
		snapshot, err := repository.readHistoricalBenefit(ctx, tx, windowStart, userID)
		if err != nil {
			return nil, err
		}
		result[userID] = snapshot.Input
	}
	return result, nil
}

func compensationOriginalCalculations(ctx context.Context, tx pgx.Tx, windowStart time.Time) (map[uuid.UUID]originalCalculation, error) {
	rows, err := tx.Query(ctx, `
SELECT user_id, policy_revision, calculation_sha256, reward
FROM economy.seeding_reward_calculations
WHERE window_start = $1
ORDER BY user_id`, windowStart)
	if err != nil {
		return nil, fmt.Errorf("read original seeding reward calculations: %w", err)
	}
	defer rows.Close()
	result := make(map[uuid.UUID]originalCalculation)
	for rows.Next() {
		var userID uuid.UUID
		var item originalCalculation
		var digest []byte
		if err := rows.Scan(&userID, &item.PolicyRevision, &digest, &item.Reward); err != nil {
			return nil, fmt.Errorf("scan original seeding reward calculation: %w", err)
		}
		if userID == uuid.Nil || len(digest) != sha256.Size || item.Reward < 0 {
			return nil, ErrInvariant
		}
		copy(item.CalculationDigest[:], digest)
		result[userID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish original seeding reward calculations: %w", err)
	}
	return result, nil
}

func basisPointUnits(units int64) (string, error) {
	if units < 0 {
		return "", ErrInvariant
	}
	return basisPointAmount(units, 1)
}
