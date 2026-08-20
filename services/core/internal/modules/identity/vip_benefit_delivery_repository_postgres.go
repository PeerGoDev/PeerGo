package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/vipbenefitv1"
	"github.com/peergo/peergo/services/core/internal/platform/settlementcontrol"
)

var errVIPBenefitDeliveryInvariant = errors.New("VIP benefit delivery invariant failed")

// PostgresVIPBenefitDeliveryRepository backfills and dispatches immutable VIP
// accounting facts. Lease mechanics are shared with other Settlement control
// outboxes; this type owns only VIP command construction and validation.
type PostgresVIPBenefitDeliveryRepository struct {
	pool   *pgxpool.Pool
	outbox *settlementcontrol.PostgresOutbox
}

type missingVIPBenefitTransition struct {
	id           uuid.UUID
	userID       uuid.UUID
	enabled      bool
	activeUntil  *time.Time
	stateVersion int64
	effectiveAt  time.Time
}

func NewPostgresVIPBenefitDeliveryRepository(pool *pgxpool.Pool) (*PostgresVIPBenefitDeliveryRepository, error) {
	if pool == nil {
		return nil, errors.New("VIP benefit delivery database is required")
	}
	outbox, err := settlementcontrol.NewPostgresOutbox(pool, settlementcontrol.PostgresOutboxConfig{
		Schema: "identity", Table: "settlement_vip_benefit_outbox", IDColumn: "transition_id",
		Label: "VIP benefit", InvariantError: errVIPBenefitDeliveryInvariant,
		ValidatePayload: func(id uuid.UUID, payload []byte) ([32]byte, error) {
			command, err := vipbenefitv1.Decode(payload)
			if err != nil || command.TransitionID != id.String() {
				return [32]byte{}, errVIPBenefitDeliveryInvariant
			}
			return vipbenefitv1.SHA256(payload)
		},
	})
	if err != nil {
		return nil, err
	}
	return &PostgresVIPBenefitDeliveryRepository{pool: pool, outbox: outbox}, nil
}

// BackfillMissing covers migrated and pre-outbox VIP transitions. It writes the
// same canonical command as the synchronous administration transaction and is
// safe to run on every policy-worker start.
func (repository *PostgresVIPBenefitDeliveryRepository) BackfillMissing(ctx context.Context) (int, error) {
	rows, err := repository.pool.Query(ctx, `
SELECT transition.id, transition.user_id, transition.to_enabled,
       transition.to_until, transition.state_version, transition.occurred_at
FROM identity.user_vip_transitions AS transition
LEFT JOIN identity.settlement_vip_benefit_outbox AS outbox
  ON outbox.transition_id = transition.id
WHERE outbox.transition_id IS NULL
ORDER BY transition.occurred_at, transition.state_version, transition.id`)
	if err != nil {
		return 0, fmt.Errorf("list missing VIP benefit deliveries: %w", err)
	}
	missing := make([]missingVIPBenefitTransition, 0)
	for rows.Next() {
		var transition missingVIPBenefitTransition
		if err := rows.Scan(&transition.id, &transition.userID, &transition.enabled,
			&transition.activeUntil, &transition.stateVersion, &transition.effectiveAt); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan missing VIP benefit delivery: %w", err)
		}
		if transition.id == uuid.Nil || transition.userID == uuid.Nil ||
			transition.stateVersion < 1 || transition.effectiveAt.IsZero() ||
			(!transition.enabled && transition.activeUntil != nil) {
			rows.Close()
			return 0, errVIPBenefitDeliveryInvariant
		}
		missing = append(missing, transition)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("finish missing VIP benefit scan: %w", err)
	}
	rows.Close()

	created := 0
	for _, transition := range missing {
		result, err := insertVIPBenefitOutbox(ctx, repository.pool, transition.id,
			transition.userID, transition.enabled, transition.activeUntil,
			transition.stateVersion, transition.effectiveAt)
		if err != nil {
			return created, err
		}
		created += int(result)
	}
	return created, nil
}

func (repository *PostgresVIPBenefitDeliveryRepository) Claim(ctx context.Context, now time.Time, batchSize int32, leaseDuration time.Duration) ([]settlementcontrol.PendingCommand, error) {
	return repository.outbox.Claim(ctx, now, batchSize, leaseDuration)
}

func (repository *PostgresVIPBenefitDeliveryRepository) MarkDelivered(ctx context.Context, pending settlementcontrol.PendingCommand, deliveredAt time.Time) error {
	return repository.outbox.MarkDelivered(ctx, pending, deliveredAt)
}

func (repository *PostgresVIPBenefitDeliveryRepository) Release(ctx context.Context, pending settlementcontrol.PendingCommand, availableAt time.Time, errorCode string) error {
	return repository.outbox.Release(ctx, pending, availableAt, errorCode)
}

type vipBenefitOutboxExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func insertVIPBenefitOutbox(
	ctx context.Context,
	executor vipBenefitOutboxExecutor,
	transitionID, userID uuid.UUID,
	enabled bool,
	activeUntil *time.Time,
	stateVersion int64,
	effectiveAt time.Time,
) (int64, error) {
	var canonicalUntil *time.Time
	if activeUntil != nil {
		value := activeUntil.UTC().Round(0)
		canonicalUntil = &value
	}
	command := vipbenefitv1.Command{
		SchemaVersion: vipbenefitv1.SchemaVersion,
		TransitionID:  transitionID.String(), UserID: userID.String(),
		Entitlement: vipbenefitv1.EntitlementDownloadChargeExempt,
		Enabled:     enabled, ActiveUntil: canonicalUntil, StateVersion: stateVersion,
		EffectiveAt: effectiveAt.UTC().Round(0),
	}
	encoded, err := vipbenefitv1.Encode(command)
	if err != nil {
		return 0, errVIPBenefitDeliveryInvariant
	}
	digest, err := vipbenefitv1.SHA256(encoded)
	if err != nil {
		return 0, errVIPBenefitDeliveryInvariant
	}
	result, err := executor.Exec(ctx, `
INSERT INTO identity.settlement_vip_benefit_outbox (
    transition_id, user_id, state_version, effective_at,
    command_json, command_sha256, available_at, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $4, $4)
ON CONFLICT (transition_id) DO NOTHING`, transitionID, userID, stateVersion,
		command.EffectiveAt, string(encoded), digest[:])
	if err != nil {
		return 0, fmt.Errorf("enqueue VIP benefit delivery: %w", err)
	}
	return result.RowsAffected(), nil
}

var _ settlementcontrol.Repository = (*PostgresVIPBenefitDeliveryRepository)(nil)
