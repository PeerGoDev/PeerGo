// Package promotioncontrol owns the authenticated Core-to-Settlement command
// boundary for public promotion rules. It stores only immutable accounting
// facts and has no browser-facing API.
package promotioncontrol

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/hnrcontrolv1"
	"github.com/peergo/peergo/contracts/go/promotioncontrolv1"
	"github.com/peergo/peergo/contracts/go/settlementoperationsv1"
	"github.com/peergo/peergo/contracts/go/vipbenefitv1"
	"github.com/peergo/peergo/contracts/go/workgroupbenefitv1"
	"github.com/peergo/peergo/services/settlement/internal/generated/ledgerdb"
	"github.com/peergo/peergo/services/settlement/internal/hnr"
	"github.com/peergo/peergo/services/settlement/internal/hnrpolicy"
	"github.com/peergo/peergo/services/settlement/internal/policy"
	"github.com/peergo/peergo/services/settlement/internal/timeline"
)

var (
	ErrInvalid                           = errors.New("promotion command is invalid")
	ErrConflict                          = errors.New("promotion command id conflicts with existing evidence")
	ErrOverlap                           = errors.New("promotion command overlaps the same scope")
	ErrHistoricalRewrite                 = errors.New("promotion command would rewrite settled traffic")
	ErrHNRConflict                       = errors.New("H&R policy command conflicts with the immutable timeline")
	ErrWorkgroupBenefitConflict          = errors.New("workgroup benefit command conflicts with the immutable timeline")
	ErrWorkgroupBenefitHistoricalRewrite = errors.New("workgroup benefit command would rewrite settled traffic")
	ErrVIPBenefitConflict                = errors.New("VIP benefit command conflicts with the immutable timeline")
	ErrVIPBenefitHistoricalRewrite       = errors.New("VIP benefit command would rewrite settled traffic")
)

type Repository struct {
	pool *pgxpool.Pool
}

// AppendVIPBenefit records one VIP state transition as immutable Settlement
// input. Expiry is carried in the command, so replay never consults Core's
// current VIP projection.
func (repository *Repository) AppendVIPBenefit(ctx context.Context, encoded []byte, recordedAt time.Time) (bool, error) {
	command, err := vipbenefitv1.Decode(encoded)
	if err != nil || recordedAt.IsZero() {
		return false, ErrInvalid
	}
	transitionID, err := uuid.Parse(command.TransitionID)
	if err != nil || transitionID == uuid.Nil {
		return false, ErrInvalid
	}
	userID, err := uuid.Parse(command.UserID)
	if err != nil || userID == uuid.Nil {
		return false, ErrInvalid
	}
	digest := sha256.Sum256(encoded)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin VIP benefit append: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ledgerdb.New(tx).LockPolicyTimeline(ctx); err != nil {
		return false, fmt.Errorf("lock settlement policy timeline: %w", err)
	}
	var existingJSON string
	var existingDigest []byte
	err = tx.QueryRow(ctx, `
SELECT command_json, command_sha256
FROM settlement.vip_benefit_transitions
WHERE transition_id = $1`, transitionID).Scan(&existingJSON, &existingDigest)
	if err == nil {
		if existingJSON != string(encoded) || !bytes.Equal(existingDigest, digest[:]) {
			return false, ErrVIPBenefitConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit duplicate VIP benefit verification: %w", err)
		}
		return false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("read existing VIP benefit: %w", err)
	}
	result, err := tx.Exec(ctx, `
INSERT INTO settlement.vip_benefit_transitions (
    transition_id, user_id, entitlement, enabled, active_until,
    state_version, effective_at, command_json, command_sha256, recorded_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (transition_id) DO NOTHING`, transitionID, userID, command.Entitlement,
		command.Enabled, command.ActiveUntil, command.StateVersion, command.EffectiveAt,
		string(encoded), digest[:], recordedAt)
	if err != nil {
		message := err.Error()
		if strings.Contains(message, "VIP benefit would rewrite settled traffic") {
			return false, ErrVIPBenefitHistoricalRewrite
		}
		if strings.Contains(message, "vip_benefit_transitions_user_id_state_version_key") {
			return false, ErrVIPBenefitConflict
		}
		return false, fmt.Errorf("append VIP benefit transition: %w", err)
	}
	if result.RowsAffected() != 1 {
		return false, ErrVIPBenefitConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit VIP benefit transition: %w", err)
	}
	return true, nil
}

// AppendWorkgroupBenefit records one membership transition as an immutable
// accounting fact. The shared policy-timeline lock prevents a late transition
// from racing a final settlement that has already resolved the same interval.
func (repository *Repository) AppendWorkgroupBenefit(ctx context.Context, encoded []byte, recordedAt time.Time) (bool, error) {
	command, err := workgroupbenefitv1.Decode(encoded)
	if err != nil || recordedAt.IsZero() {
		return false, ErrInvalid
	}
	transitionID, err := uuid.Parse(command.TransitionID)
	if err != nil || transitionID == uuid.Nil {
		return false, ErrInvalid
	}
	userID, err := uuid.Parse(command.UserID)
	if err != nil || userID == uuid.Nil {
		return false, ErrInvalid
	}
	digest := sha256.Sum256(encoded)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin workgroup benefit append: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ledgerdb.New(tx).LockPolicyTimeline(ctx); err != nil {
		return false, fmt.Errorf("lock settlement policy timeline: %w", err)
	}
	var existingJSON string
	var existingDigest []byte
	err = tx.QueryRow(ctx, `
SELECT command_json, command_sha256
FROM settlement.workgroup_benefit_transitions
WHERE transition_id = $1`, transitionID).Scan(&existingJSON, &existingDigest)
	if err == nil {
		if existingJSON != string(encoded) || !bytes.Equal(existingDigest, digest[:]) {
			return false, ErrWorkgroupBenefitConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit duplicate workgroup benefit verification: %w", err)
		}
		return false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("read existing workgroup benefit: %w", err)
	}
	result, err := tx.Exec(ctx, `
INSERT INTO settlement.workgroup_benefit_transitions (
    transition_id, user_id, group_kind, entitlement, active,
    state_version, effective_at, command_json, command_sha256, recorded_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (transition_id) DO NOTHING`, transitionID, userID, command.GroupKind,
		command.Entitlement, command.Active, command.StateVersion, command.EffectiveAt,
		string(encoded), digest[:], recordedAt)
	if err != nil {
		message := err.Error()
		if strings.Contains(message, "workgroup benefit would rewrite settled traffic") {
			return false, ErrWorkgroupBenefitHistoricalRewrite
		}
		if strings.Contains(message, "workgroup_benefit_transitions_user_id_state_version_key") {
			return false, ErrWorkgroupBenefitConflict
		}
		return false, fmt.Errorf("append workgroup benefit transition: %w", err)
	}
	if result.RowsAffected() != 1 {
		return false, ErrWorkgroupBenefitConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit workgroup benefit transition: %w", err)
	}
	return true, nil
}

// AppendHNR converts the shared control contract into Settlement's internal
// global timeline type. Core cannot supply user/torrent selectors through this
// endpoint, so the staff settings page cannot accidentally create a hidden
// per-member exception.
func (repository *Repository) AppendHNR(ctx context.Context, encoded []byte, recordedAt time.Time) (bool, error) {
	command, err := hnrcontrolv1.Decode(encoded)
	if err != nil || recordedAt.IsZero() {
		return false, ErrInvalid
	}
	id, err := uuid.Parse(command.RevisionID)
	if err != nil || id == uuid.Nil {
		return false, ErrInvalid
	}
	hnrRepository, err := hnr.NewPostgresRepository(repository.pool)
	if err != nil {
		return false, err
	}
	created, err := hnrRepository.AppendRevision(ctx, hnrpolicy.Revision{
		ID: id, Scope: timeline.Scope{}, EffectiveAt: command.EffectiveAt,
		Policy: command.Policy,
	}, recordedAt)
	if errors.Is(err, hnr.ErrInput) {
		return false, ErrInvalid
	}
	if errors.Is(err, hnr.ErrTimelineConflict) || errors.Is(err, hnr.ErrInvariant) {
		return false, ErrHNRConflict
	}
	if err != nil {
		return false, err
	}
	return created, nil
}

func NewRepository(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("promotion control database is required")
	}
	return &Repository{pool: pool}, nil
}

// Settings returns only the current global policy summaries required by the
// site-owner settings pages. User/torrent-scoped revisions and raw accounting
// evidence deliberately remain inside Settlement.
func (repository *Repository) Settings(ctx context.Context, now time.Time) (settlementoperationsv1.Settings, error) {
	if now.IsZero() {
		return settlementoperationsv1.Settings{}, ErrInvalid
	}
	result := settlementoperationsv1.Settings{
		GeneratedAt: now.UTC().Round(0),
		Seedbox: settlementoperationsv1.SeedboxPolicy{
			SettlementPrimitiveSupported: true,
			ClassificationConnected:      true,
			RegistryConnected:            true,
			SpeedObservationConnected:    true,
		},
	}
	var revisionID uuid.UUID
	var effectiveAt time.Time
	var policyJSON string
	err := repository.pool.QueryRow(ctx, `
SELECT id, effective_at, policy_json
FROM settlement.hnr_policy_timeline_revisions
WHERE effective_at <= $1
  AND scope_user_id IS NULL
  AND scope_torrent_id IS NULL
  AND scope_torrent_control_sequence IS NULL
  AND scope_subject_control_sequence IS NULL
ORDER BY effective_at DESC, id DESC
LIMIT 1`, now).Scan(&revisionID, &effectiveAt, &policyJSON)
	if err == nil {
		current, decodeErr := hnrpolicy.Decode([]byte(policyJSON))
		if decodeErr != nil {
			return settlementoperationsv1.Settings{}, fmt.Errorf("decode current global H&R policy: %w", decodeErr)
		}
		effectiveAt = effectiveAt.UTC().Round(0)
		result.HNR = settlementoperationsv1.HNRPolicy{
			Configured: true, RevisionID: revisionID.String(), EffectiveAt: &effectiveAt,
			RuleID: current.Rule.ID, RuleVersion: current.Rule.Version, Mode: string(current.Mode),
			RequiredSeedSeconds:      current.RequiredSeedSeconds,
			RequiredRatioBasisPoints: current.RequiredRatioBasisPoints,
			AssessmentWindowSeconds:  current.AssessmentWindowSeconds,
			GracePeriodSeconds:       current.GracePeriodSeconds,
			MaxIntervalCreditSeconds: current.MaxIntervalCreditSeconds,
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return settlementoperationsv1.Settings{}, fmt.Errorf("read current global H&R policy: %w", err)
	}

	var snapshotJSON string
	err = repository.pool.QueryRow(ctx, `
SELECT snapshot_json
FROM settlement.policy_timeline_revisions
WHERE effective_at <= $1
  AND scope_user_id IS NULL
  AND scope_torrent_id IS NULL
  AND scope_torrent_control_sequence IS NULL
  AND scope_subject_control_sequence IS NULL
ORDER BY effective_at DESC, id DESC
LIMIT 1`, now).Scan(&snapshotJSON)
	if err == nil {
		snapshot, decodeErr := policy.DecodeSnapshot([]byte(snapshotJSON))
		if decodeErr != nil {
			return settlementoperationsv1.Settings{}, fmt.Errorf("decode current global traffic policy: %w", decodeErr)
		}
		if snapshot.Seedbox != nil {
			result.Seedbox.GlobalPolicyConfigured = true
			result.Seedbox.UploadFactorBasisPoints = int64(snapshot.Seedbox.UploadFactor)
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return settlementoperationsv1.Settings{}, fmt.Errorf("read current global traffic policy: %w", err)
	}
	if !result.Valid() {
		return settlementoperationsv1.Settings{}, errors.New("Settlement settings projection is invalid")
	}
	return result, nil
}

// Append serializes campaign writes with final traffic settlement. An exact
// retry is a successful no-op; reusing the campaign ID with different canonical
// bytes is a conflict and can never mutate prior ledger evidence.
func (repository *Repository) Append(ctx context.Context, encoded []byte, recordedAt time.Time) (bool, error) {
	command, err := promotioncontrolv1.Decode(encoded)
	if err != nil || recordedAt.IsZero() {
		return false, ErrInvalid
	}
	id, err := uuid.Parse(command.CampaignID)
	if err != nil || id == uuid.Nil {
		return false, ErrInvalid
	}
	digest := sha256.Sum256(encoded)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin promotion rule append: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := ledgerdb.New(tx)
	if err := queries.LockPolicyTimeline(ctx); err != nil {
		return false, fmt.Errorf("lock settlement policy timeline: %w", err)
	}
	existing, err := queries.GetPromotionRule(ctx, id)
	if err == nil {
		if existing.CommandJson != string(encoded) || !bytes.Equal(existing.CommandSha256, digest[:]) {
			return false, ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit duplicate promotion rule verification: %w", err)
		}
		return false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("read existing promotion rule: %w", err)
	}
	torrentID := pgtype.Int8{}
	if command.TorrentID != nil {
		torrentID = pgtype.Int8{Int64: *command.TorrentID, Valid: true}
	}
	overlaps, err := queries.PromotionRuleScopeOverlaps(ctx, ledgerdb.PromotionRuleScopeOverlapsParams{
		ScopeType: string(command.Scope), TorrentID: torrentID,
		StartsAt: timestamp(command.StartsAt), EndsAt: timestamp(command.EndsAt),
	})
	if err != nil {
		return false, fmt.Errorf("check promotion rule overlap: %w", err)
	}
	if overlaps {
		return false, ErrOverlap
	}
	rows, err := queries.AppendPromotionRule(ctx, ledgerdb.AppendPromotionRuleParams{
		ID: id, ScopeType: string(command.Scope), TorrentID: torrentID, Promotion: string(command.Promotion),
		StartsAt: timestamp(command.StartsAt), EndsAt: timestamp(command.EndsAt),
		OverrideLowerScopes: command.OverrideLowerScopes, ReasonCode: command.ReasonCode,
		CommandJson: string(encoded), CommandSha256: digest[:], RecordedAt: timestamp(recordedAt),
	})
	if err != nil {
		if strings.Contains(err.Error(), "promotion rule would rewrite settled traffic") {
			return false, ErrHistoricalRewrite
		}
		return false, fmt.Errorf("append promotion rule: %w", err)
	}
	if rows != 1 {
		return false, ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit promotion rule: %w", err)
	}
	return true, nil
}

func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC().Round(0), Valid: true}
}
