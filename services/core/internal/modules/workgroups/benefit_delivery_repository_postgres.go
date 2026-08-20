package workgroups

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/workgroupbenefitv1"
	"github.com/peergo/peergo/services/core/internal/platform/settlementcontrol"
)

var errBenefitDeliveryInvariant = errors.New("workgroup benefit delivery invariant failed")

// PostgresBenefitDeliveryRepository owns reliable delivery of accounting
// entitlements. Membership changes and their canonical commands are committed
// in one Core transaction; this repository only leases and acknowledges them.
type PostgresBenefitDeliveryRepository struct {
	pool   *pgxpool.Pool
	outbox *settlementcontrol.PostgresOutbox
}

type missingBenefitTransition struct {
	id           uuid.UUID
	userID       uuid.UUID
	to           MembershipStatus
	stateVersion int64
	effectiveAt  time.Time
}

func NewPostgresBenefitDeliveryRepository(pool *pgxpool.Pool) (*PostgresBenefitDeliveryRepository, error) {
	if pool == nil {
		return nil, errors.New("workgroup benefit delivery database is required")
	}
	outbox, err := settlementcontrol.NewPostgresOutbox(pool, settlementcontrol.PostgresOutboxConfig{
		Schema: "workgroups", Table: "settlement_benefit_outbox", IDColumn: "transition_id",
		Label: "workgroup benefit", InvariantError: errBenefitDeliveryInvariant,
		ValidatePayload: func(id uuid.UUID, payload []byte) ([32]byte, error) {
			command, err := workgroupbenefitv1.Decode(payload)
			if err != nil || command.TransitionID != id.String() {
				return [32]byte{}, errBenefitDeliveryInvariant
			}
			return workgroupbenefitv1.SHA256(payload)
		},
	})
	if err != nil {
		return nil, err
	}
	return &PostgresBenefitDeliveryRepository{pool: pool, outbox: outbox}, nil
}

// BackfillMissing translates retention transitions created before the outbox
// migration into the same canonical command used for new writes. It is safe to
// run on every worker start and never rewrites an existing payload.
func (repository *PostgresBenefitDeliveryRepository) BackfillMissing(ctx context.Context) (int, error) {
	rows, err := repository.pool.Query(ctx, `
SELECT transition.id, transition.user_id, transition.to_status,
       transition.state_version, transition.occurred_at
FROM workgroups.membership_transitions AS transition
LEFT JOIN workgroups.settlement_benefit_outbox AS outbox
  ON outbox.transition_id = transition.id
WHERE transition.group_kind = 'retention'
  AND outbox.transition_id IS NULL
ORDER BY transition.occurred_at, transition.state_version, transition.id`)
	if err != nil {
		return 0, fmt.Errorf("list missing workgroup benefit deliveries: %w", err)
	}
	missing := make([]missingBenefitTransition, 0)
	for rows.Next() {
		var transition missingBenefitTransition
		if err := rows.Scan(&transition.id, &transition.userID, &transition.to, &transition.stateVersion, &transition.effectiveAt); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan missing workgroup benefit delivery: %w", err)
		}
		if transition.id == uuid.Nil || transition.userID == uuid.Nil || transition.stateVersion < 1 ||
			transition.effectiveAt.IsZero() || (transition.to != MembershipActive && transition.to != MembershipSuspended && transition.to != MembershipEnded) {
			rows.Close()
			return 0, errBenefitDeliveryInvariant
		}
		missing = append(missing, transition)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("finish missing workgroup benefit scan: %w", err)
	}
	rows.Close()

	created := 0
	for _, transition := range missing {
		command := workgroupbenefitv1.Command{
			SchemaVersion: workgroupbenefitv1.SchemaVersion,
			TransitionID:  transition.id.String(),
			UserID:        transition.userID.String(),
			GroupKind:     workgroupbenefitv1.GroupRetention,
			Entitlement:   workgroupbenefitv1.EntitlementDownloadChargeExempt,
			Active:        transition.to == MembershipActive,
			StateVersion:  transition.stateVersion,
			EffectiveAt:   transition.effectiveAt.UTC().Round(0),
		}
		encoded, err := workgroupbenefitv1.Encode(command)
		if err != nil {
			return created, errBenefitDeliveryInvariant
		}
		digest, err := workgroupbenefitv1.SHA256(encoded)
		if err != nil {
			return created, errBenefitDeliveryInvariant
		}
		result, err := repository.pool.Exec(ctx, `
INSERT INTO workgroups.settlement_benefit_outbox (
    transition_id, user_id, state_version, effective_at,
    command_json, command_sha256, available_at, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $4, $4)
ON CONFLICT (transition_id) DO NOTHING`, transition.id, transition.userID,
			transition.stateVersion, command.EffectiveAt, string(encoded), digest[:])
		if err != nil {
			return created, fmt.Errorf("backfill workgroup benefit delivery: %w", err)
		}
		created += int(result.RowsAffected())
	}
	return created, nil
}

func (repository *PostgresBenefitDeliveryRepository) Claim(ctx context.Context, now time.Time, batchSize int32, leaseDuration time.Duration) ([]settlementcontrol.PendingCommand, error) {
	return repository.outbox.Claim(ctx, now, batchSize, leaseDuration)
}

func (repository *PostgresBenefitDeliveryRepository) MarkDelivered(ctx context.Context, pending settlementcontrol.PendingCommand, deliveredAt time.Time) error {
	return repository.outbox.MarkDelivered(ctx, pending, deliveredAt)
}

func (repository *PostgresBenefitDeliveryRepository) Release(ctx context.Context, pending settlementcontrol.PendingCommand, availableAt time.Time, errorCode string) error {
	return repository.outbox.Release(ctx, pending, availableAt, errorCode)
}

var _ settlementcontrol.Repository = (*PostgresBenefitDeliveryRepository)(nil)
