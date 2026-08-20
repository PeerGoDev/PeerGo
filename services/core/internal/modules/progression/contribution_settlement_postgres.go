package progression

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const contributionBytesPerGiB = int64(1 << 30)

var (
	contributionUploadEntryNamespace  = uuid.MustParse("bc2722da-85ac-5e47-b424-39588a9cc97a")
	contributionPublishEntryNamespace = uuid.MustParse("2677a109-1838-5c7a-bbf8-911db0c99536")
	contributionAccountEntryNamespace = uuid.MustParse("41431d3c-a44d-5d3f-bf7f-3495667e887f")
)

type PostgresContributionSettlementRepository struct {
	pool        *pgxpool.Pool
	progression *PostgresRepository
}

func NewPostgresContributionSettlementRepository(pool *pgxpool.Pool) (*PostgresContributionSettlementRepository, error) {
	if pool == nil {
		return nil, ErrInput
	}
	progressionRepository, err := NewPostgresRepository(pool)
	if err != nil {
		return nil, err
	}
	return &PostgresContributionSettlementRepository{pool: pool, progression: progressionRepository}, nil
}

type contributionPolicyAt struct {
	Revision                     string
	EffectiveFrom                time.Time
	ExperiencePerUploadGiBMilli  int64
	ExperiencePerTorrentMilli    int64
	ExperiencePerAccountDayMilli int64
	SnapshotSHA256               [sha256.Size]byte
}

type uploadExperienceDocument struct {
	Source                string `json:"source"`
	SettlementID          string `json:"settlement_id"`
	UserID                string `json:"user_id"`
	ProjectionSequence    int64  `json:"projection_sequence"`
	RawUploadedBytes      int64  `json:"raw_uploaded_bytes"`
	RemainderBeforeBytes  int64  `json:"remainder_before_bytes"`
	WholeGiBCredited      int64  `json:"whole_gib_credited"`
	RemainderAfterBytes   int64  `json:"remainder_after_bytes"`
	PolicyRevision        string `json:"policy_revision"`
	ExperiencePerGiBMilli int64  `json:"experience_per_gib_milli"`
	ExperienceAmount      string `json:"experience_amount"`
	OccurredAt            string `json:"occurred_at"`
}

type torrentPublishExperienceDocument struct {
	Source                    string `json:"source"`
	TorrentID                 int64  `json:"torrent_id"`
	UserID                    string `json:"user_id"`
	PolicyRevision            string `json:"policy_revision"`
	ExperiencePerTorrentMilli int64  `json:"experience_per_torrent_milli"`
	ExperienceAmount          string `json:"experience_amount"`
	PublishedAt               string `json:"published_at"`
}

type accountDayExperienceDocument struct {
	Source                       string `json:"source"`
	UserID                       string `json:"user_id"`
	DayNumber                    int64  `json:"day_number"`
	AnchorAt                     string `json:"anchor_at"`
	PolicyRevision               string `json:"policy_revision"`
	ExperiencePerAccountDayMilli int64  `json:"experience_per_account_day_milli"`
	ExperienceAmount             string `json:"experience_amount"`
	OccurredAt                   string `json:"occurred_at"`
}

func (repository *PostgresContributionSettlementRepository) SettleNextUpload(ctx context.Context, now time.Time) (ContributionSettlementResult, bool, error) {
	now = canonicalProgressionTime(now)
	if now.IsZero() {
		return ContributionSettlementResult{}, false, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ContributionSettlementResult{}, false, fmt.Errorf("begin upload experience settlement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var cursor int64
	if err := tx.QueryRow(ctx, `
SELECT last_projection_sequence
FROM progression.contribution_upload_cursor
WHERE singleton = true
FOR UPDATE`).Scan(&cursor); err != nil {
		return ContributionSettlementResult{}, false, fmt.Errorf("lock upload experience cursor: %w", err)
	}
	var sequence, rawUploaded int64
	var settlementID, userID uuid.UUID
	var occurredAt time.Time
	err = tx.QueryRow(ctx, `
SELECT entry.projection_sequence, entry.settlement_id, entry.user_id,
       entry.raw_uploaded, entry.occurred_at
FROM traffic.user_traffic_entries AS entry
WHERE entry.projection_sequence > $1
  AND entry.raw_uploaded > 0
  AND entry.occurred_at >= (
      SELECT min(policy.effective_from)
      FROM progression.contribution_experience_policy_revisions AS policy
  )
ORDER BY entry.projection_sequence
LIMIT 1`, cursor).Scan(&sequence, &settlementID, &userID, &rawUploaded, &occurredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ContributionSettlementResult{}, false, nil
	}
	if err != nil {
		return ContributionSettlementResult{}, false, fmt.Errorf("read next upload experience source: %w", err)
	}
	occurredAt = canonicalProgressionTime(occurredAt)
	if sequence <= cursor || settlementID == uuid.Nil || userID == uuid.Nil || rawUploaded <= 0 || occurredAt.IsZero() {
		return ContributionSettlementResult{}, false, ErrInvariant
	}
	// Never jump over a future-dated source row: the cursor is a strict local
	// commit order. A later row may already be due, but consuming it first would
	// make the earlier event unreachable once its timestamp arrives.
	if occurredAt.After(now) {
		return ContributionSettlementResult{}, false, nil
	}

	var remainder, processedRaw int64
	var remainderVersion int64
	err = tx.QueryRow(ctx, `
SELECT remainder_bytes, processed_raw_uploaded, version
FROM progression.contribution_upload_remainders
WHERE user_id = $1
FOR UPDATE`, userID).Scan(&remainder, &processedRaw, &remainderVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		remainder, processedRaw, remainderVersion = 0, 0, 0
	} else if err != nil {
		return ContributionSettlementResult{}, false, fmt.Errorf("lock upload experience remainder: %w", err)
	}
	if remainder < 0 || remainder >= contributionBytesPerGiB || processedRaw < 0 || rawUploaded > math.MaxInt64-remainder || rawUploaded > math.MaxInt64-processedRaw {
		return ContributionSettlementResult{}, false, ErrInvariant
	}
	total := remainder + rawUploaded
	wholeGiB := total / contributionBytesPerGiB
	nextRemainder := total % contributionBytesPerGiB

	policy, err := readContributionPolicyAt(ctx, tx, occurredAt)
	if err != nil {
		return ContributionSettlementResult{}, false, err
	}
	amount, err := contributionAmount(wholeGiB, policy.ExperiencePerUploadGiBMilli)
	if err != nil {
		return ContributionSettlementResult{}, false, err
	}
	sourceReference := "upload:" + settlementID.String()
	result := ContributionSettlementResult{
		Kind: ContributionSettlementUpload, UserID: userID,
		SourceReference: sourceReference, PolicyRevision: policy.Revision,
		ExperienceAmount: amount.String(),
	}
	if amount.Sign() > 0 {
		digest, err := contributionDocumentDigest(uploadExperienceDocument{
			Source: "raw_upload_gib", SettlementID: settlementID.String(), UserID: userID.String(),
			ProjectionSequence: sequence, RawUploadedBytes: rawUploaded,
			RemainderBeforeBytes: remainder, WholeGiBCredited: wholeGiB,
			RemainderAfterBytes: nextRemainder, PolicyRevision: policy.Revision,
			ExperiencePerGiBMilli: policy.ExperiencePerUploadGiBMilli,
			ExperienceAmount:      amount.String(), OccurredAt: occurredAt.Format(time.RFC3339Nano),
		})
		if err != nil {
			return ContributionSettlementResult{}, false, err
		}
		levelPolicy, err := currentLevelPolicyVersion(ctx, tx, now)
		if err != nil {
			return ContributionSettlementResult{}, false, err
		}
		entryID := uuid.NewSHA1(contributionUploadEntryNamespace, []byte(settlementID.String()))
		entry, err := repository.progression.RecordInTransaction(ctx, tx, RecordCommand{
			EntryID: entryID, IdempotencyKey: "upload-experience:" + settlementID.String(),
			UserID: userID, EntryType: EntryEarn, Amount: amount,
			SourceReference: sourceReference, SourceKind: SourceActivity,
			PolicyRevision:     contributionActivityPolicyRevision(policy.Revision),
			LevelPolicyVersion: levelPolicy, PayloadSHA256: digest,
			OccurredAt: occurredAt, RecordedAt: now,
		})
		if err != nil {
			return ContributionSettlementResult{}, false, fmt.Errorf("record upload experience: %w", err)
		}
		result.ExperienceEntryID = entry.ID
	}
	if remainderVersion == 0 {
		_, err = tx.Exec(ctx, `
INSERT INTO progression.contribution_upload_remainders (
    user_id, remainder_bytes, processed_raw_uploaded, version, updated_at
) VALUES ($1, $2, $3, 1, $4)`, userID, nextRemainder, rawUploaded, now)
	} else {
		commandTag, updateErr := tx.Exec(ctx, `
UPDATE progression.contribution_upload_remainders
SET remainder_bytes = $2, processed_raw_uploaded = $3,
    version = version + 1, updated_at = $4
WHERE user_id = $1 AND version = $5`, userID, nextRemainder, processedRaw+rawUploaded, now, remainderVersion)
		err = updateErr
		if err == nil && commandTag.RowsAffected() != 1 {
			err = ErrInvariant
		}
	}
	if err != nil {
		return ContributionSettlementResult{}, false, fmt.Errorf("advance upload experience remainder: %w", err)
	}
	commandTag, err := tx.Exec(ctx, `
UPDATE progression.contribution_upload_cursor
SET last_projection_sequence = $1, updated_at = $2
WHERE singleton = true AND last_projection_sequence = $3`, sequence, now, cursor)
	if err != nil || commandTag.RowsAffected() != 1 {
		return ContributionSettlementResult{}, false, fmt.Errorf("advance upload experience cursor: %w", errors.Join(err, ErrInvariant))
	}
	if err := tx.Commit(ctx); err != nil {
		return ContributionSettlementResult{}, false, fmt.Errorf("commit upload experience settlement: %w", err)
	}
	return result, true, nil
}

func (repository *PostgresContributionSettlementRepository) SettleNextTorrentPublish(ctx context.Context, now time.Time) (ContributionSettlementResult, bool, error) {
	now = canonicalProgressionTime(now)
	if now.IsZero() {
		return ContributionSettlementResult{}, false, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ContributionSettlementResult{}, false, fmt.Errorf("begin torrent publish experience settlement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var torrentID int64
	var userID uuid.UUID
	var publishedAt time.Time
	err = tx.QueryRow(ctx, `
SELECT torrent.id, torrent.uploader_id, torrent.published_at
FROM torrents.torrents AS torrent
WHERE torrent.published_at IS NOT NULL
  AND torrent.published_at <= $1
  AND torrent.published_at >= (
      SELECT min(policy.effective_from)
      FROM progression.contribution_experience_policy_revisions AS policy
  )
  AND NOT EXISTS (
      SELECT 1
      FROM progression.torrent_publish_experience_receipts AS receipt
      WHERE receipt.torrent_id = torrent.id
  )
ORDER BY torrent.published_at, torrent.id
FOR UPDATE OF torrent SKIP LOCKED
LIMIT 1`, now).Scan(&torrentID, &userID, &publishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ContributionSettlementResult{}, false, nil
	}
	if err != nil {
		return ContributionSettlementResult{}, false, fmt.Errorf("read next torrent publish experience source: %w", err)
	}
	publishedAt = canonicalProgressionTime(publishedAt)
	if torrentID < 1 || userID == uuid.Nil || publishedAt.IsZero() {
		return ContributionSettlementResult{}, false, ErrInvariant
	}
	policy, err := readContributionPolicyAt(ctx, tx, publishedAt)
	if err != nil {
		return ContributionSettlementResult{}, false, err
	}
	amount, err := contributionAmount(1, policy.ExperiencePerTorrentMilli)
	if err != nil {
		return ContributionSettlementResult{}, false, err
	}
	sourceReference := "torrent:" + strconv.FormatInt(torrentID, 10)
	digest, err := contributionDocumentDigest(torrentPublishExperienceDocument{
		Source: "torrent_publish", TorrentID: torrentID, UserID: userID.String(),
		PolicyRevision: policy.Revision, ExperiencePerTorrentMilli: policy.ExperiencePerTorrentMilli,
		ExperienceAmount: amount.String(), PublishedAt: publishedAt.Format(time.RFC3339Nano),
	})
	if err != nil {
		return ContributionSettlementResult{}, false, err
	}
	result := ContributionSettlementResult{
		Kind: ContributionSettlementPublish, UserID: userID,
		SourceReference: sourceReference, PolicyRevision: policy.Revision,
		ExperienceAmount: amount.String(),
	}
	if amount.Sign() > 0 {
		levelPolicy, err := currentLevelPolicyVersion(ctx, tx, now)
		if err != nil {
			return ContributionSettlementResult{}, false, err
		}
		entryID := uuid.NewSHA1(contributionPublishEntryNamespace, []byte(strconv.FormatInt(torrentID, 10)))
		entry, err := repository.progression.RecordInTransaction(ctx, tx, RecordCommand{
			EntryID: entryID, IdempotencyKey: "torrent-publish-experience:" + strconv.FormatInt(torrentID, 10),
			UserID: userID, EntryType: EntryEarn, Amount: amount,
			SourceReference: sourceReference, SourceKind: SourceTorrentPublish,
			PolicyRevision:     contributionPublishPolicyRevision(policy.Revision),
			LevelPolicyVersion: levelPolicy, PayloadSHA256: digest,
			OccurredAt: publishedAt, RecordedAt: now,
		})
		if err != nil {
			return ContributionSettlementResult{}, false, fmt.Errorf("record torrent publish experience: %w", err)
		}
		result.ExperienceEntryID = entry.ID
	}
	_, err = tx.Exec(ctx, `
INSERT INTO progression.torrent_publish_experience_receipts (
    torrent_id, user_id, published_at, policy_revision,
    experience_amount, experience_entry_id, payload_sha256, processed_at
) VALUES ($1,$2,$3,$4,$5::numeric(38,20),NULLIF($6::uuid,'00000000-0000-0000-0000-000000000000'::uuid),$7,$8)`,
		torrentID, userID, publishedAt, policy.Revision, amount.String(),
		result.ExperienceEntryID, digest[:], now)
	if err != nil {
		return ContributionSettlementResult{}, false, fmt.Errorf("record torrent publish experience receipt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ContributionSettlementResult{}, false, fmt.Errorf("commit torrent publish experience settlement: %w", err)
	}
	return result, true, nil
}

func (repository *PostgresContributionSettlementRepository) SettleNextAccountDay(ctx context.Context, now time.Time) (ContributionSettlementResult, bool, error) {
	now = canonicalProgressionTime(now)
	if now.IsZero() {
		return ContributionSettlementResult{}, false, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ContributionSettlementResult{}, false, fmt.Errorf("begin account-day experience settlement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var userID uuid.UUID
	var anchorAt time.Time
	var creditedDays, checkpointVersion int64
	err = tx.QueryRow(ctx, `
WITH first_policy AS (
    SELECT min(policy.effective_from) AS effective_from
    FROM progression.contribution_experience_policy_revisions AS policy
)
SELECT users.id,
       greatest(users.created_at, first_policy.effective_from) AS anchor_at,
       coalesce(checkpoint.credited_days, 0) AS credited_days,
       coalesce(checkpoint.version, 0) AS checkpoint_version
FROM identity.users AS users
CROSS JOIN first_policy
LEFT JOIN progression.account_age_experience_checkpoints AS checkpoint
  ON checkpoint.user_id = users.id
WHERE first_policy.effective_from IS NOT NULL
  AND greatest(users.created_at, first_policy.effective_from)
      + ((coalesce(checkpoint.credited_days, 0) + 1) * interval '1 day') <= $1
ORDER BY coalesce(checkpoint.credited_days, 0), users.id
FOR UPDATE OF users SKIP LOCKED
LIMIT 1`, now).Scan(&userID, &anchorAt, &creditedDays, &checkpointVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return ContributionSettlementResult{}, false, nil
	}
	if err != nil {
		return ContributionSettlementResult{}, false, fmt.Errorf("read next account-day experience source: %w", err)
	}
	anchorAt = canonicalProgressionTime(anchorAt)
	if userID == uuid.Nil || anchorAt.IsZero() || creditedDays < 0 || checkpointVersion < 0 || creditedDays > math.MaxInt32 {
		return ContributionSettlementResult{}, false, ErrInvariant
	}
	nextDay := creditedDays + 1
	occurredAt := canonicalProgressionTime(anchorAt.AddDate(0, 0, int(nextDay)))
	if occurredAt.After(now) {
		return ContributionSettlementResult{}, false, ErrInvariant
	}
	policy, err := readContributionPolicyAt(ctx, tx, occurredAt)
	if err != nil {
		return ContributionSettlementResult{}, false, err
	}
	amount, err := contributionAmount(1, policy.ExperiencePerAccountDayMilli)
	if err != nil {
		return ContributionSettlementResult{}, false, err
	}
	sourceReference := "account-day:" + userID.String() + ":" + strconv.FormatInt(nextDay, 10)
	digest, err := contributionDocumentDigest(accountDayExperienceDocument{
		Source: "account_age_day", UserID: userID.String(), DayNumber: nextDay,
		AnchorAt: anchorAt.Format(time.RFC3339Nano), PolicyRevision: policy.Revision,
		ExperiencePerAccountDayMilli: policy.ExperiencePerAccountDayMilli,
		ExperienceAmount:             amount.String(), OccurredAt: occurredAt.Format(time.RFC3339Nano),
	})
	if err != nil {
		return ContributionSettlementResult{}, false, err
	}
	result := ContributionSettlementResult{
		Kind: ContributionSettlementAccountDay, UserID: userID,
		SourceReference: sourceReference, PolicyRevision: policy.Revision,
		ExperienceAmount: amount.String(),
	}
	if amount.Sign() > 0 {
		levelPolicy, err := currentLevelPolicyVersion(ctx, tx, now)
		if err != nil {
			return ContributionSettlementResult{}, false, err
		}
		entryID := uuid.NewSHA1(contributionAccountEntryNamespace, []byte(sourceReference))
		entry, err := repository.progression.RecordInTransaction(ctx, tx, RecordCommand{
			EntryID: entryID, IdempotencyKey: "account-day-experience:" + userID.String() + ":" + strconv.FormatInt(nextDay, 10),
			UserID: userID, EntryType: EntryEarn, Amount: amount,
			SourceReference: sourceReference, SourceKind: SourceActivity,
			PolicyRevision:     contributionActivityPolicyRevision(policy.Revision),
			LevelPolicyVersion: levelPolicy, PayloadSHA256: digest,
			OccurredAt: occurredAt, RecordedAt: now,
		})
		if err != nil {
			return ContributionSettlementResult{}, false, fmt.Errorf("record account-day experience: %w", err)
		}
		result.ExperienceEntryID = entry.ID
	}
	if checkpointVersion == 0 {
		_, err = tx.Exec(ctx, `
INSERT INTO progression.account_age_experience_checkpoints (
    user_id, anchor_at, credited_days, version, updated_at
) VALUES ($1,$2,$3,1,$4)`, userID, anchorAt, nextDay, now)
	} else {
		commandTag, updateErr := tx.Exec(ctx, `
UPDATE progression.account_age_experience_checkpoints
SET credited_days = $2, version = version + 1, updated_at = $3
WHERE user_id = $1 AND anchor_at = $4 AND version = $5`,
			userID, nextDay, now, anchorAt, checkpointVersion)
		err = updateErr
		if err == nil && commandTag.RowsAffected() != 1 {
			err = ErrInvariant
		}
	}
	if err != nil {
		return ContributionSettlementResult{}, false, fmt.Errorf("advance account-day experience checkpoint: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ContributionSettlementResult{}, false, fmt.Errorf("commit account-day experience settlement: %w", err)
	}
	return result, true, nil
}

func readContributionPolicyAt(ctx context.Context, tx pgx.Tx, at time.Time) (contributionPolicyAt, error) {
	var result contributionPolicyAt
	var digest []byte
	err := tx.QueryRow(ctx, `
SELECT revision, effective_from,
       experience_per_upload_gib_milli,
       experience_per_torrent_milli,
       experience_per_account_day_milli,
       snapshot_sha256
FROM progression.contribution_experience_policy_revisions
WHERE effective_from <= $1
  AND created_at <= $1
ORDER BY effective_from DESC, revision DESC
LIMIT 1`, at).Scan(
		&result.Revision, &result.EffectiveFrom,
		&result.ExperiencePerUploadGiBMilli,
		&result.ExperiencePerTorrentMilli,
		&result.ExperiencePerAccountDayMilli,
		&digest,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return contributionPolicyAt{}, ErrPolicyNotFound
	}
	if err != nil {
		return contributionPolicyAt{}, fmt.Errorf("read contribution experience policy: %w", err)
	}
	if len(digest) != sha256.Size || !contributionRevisionPattern.MatchString(result.Revision) || len(result.Revision) > maximumContributionPolicyRevision {
		return contributionPolicyAt{}, ErrInvariant
	}
	copy(result.SnapshotSHA256[:], digest)
	result.EffectiveFrom = canonicalProgressionTime(result.EffectiveFrom)
	return result, nil
}

func currentLevelPolicyVersion(ctx context.Context, tx pgx.Tx, at time.Time) (string, error) {
	var result string
	err := tx.QueryRow(ctx, `
SELECT revision.policy_version
FROM progression.level_policy_revisions AS revision
JOIN progression.level_policy_activation_runs AS activation
  ON activation.policy_version = revision.policy_version
WHERE revision.effective_at <= $1
  AND activation.applied_at <= $1
ORDER BY revision.effective_at DESC, revision.sequence DESC
LIMIT 1`, at).Scan(&result)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrLevelPolicyNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read active level policy: %w", err)
	}
	if !progressionPolicyPattern.MatchString(result) {
		return "", ErrInvariant
	}
	return result, nil
}

func contributionPublishPolicyRevision(revision string) string  { return revision + ".publish" }
func contributionActivityPolicyRevision(revision string) string { return revision + ".activity" }

func contributionAmount(units, rateMilli int64) (Amount, error) {
	if units < 0 || rateMilli < 0 {
		return Amount{}, ErrInput
	}
	product := new(big.Int).Mul(big.NewInt(units), big.NewInt(rateMilli))
	integer, fraction := new(big.Int), new(big.Int)
	integer.QuoRem(product, big.NewInt(1000), fraction)
	text := integer.String()
	if fraction.Sign() > 0 {
		fractionText := fraction.String()
		fractionText = strings.Repeat("0", 3-len(fractionText)) + fractionText
		fractionText = strings.TrimRight(fractionText, "0")
		text += "." + fractionText
	}
	amount, err := ParseAmount(text)
	if err != nil {
		return Amount{}, ErrInvariant
	}
	return amount, nil
}

func contributionDocumentDigest(document any) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(document)
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode contribution experience receipt: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

func canonicalProgressionTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

var _ ContributionSettlementRepository = (*PostgresContributionSettlementRepository)(nil)
