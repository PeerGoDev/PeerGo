package hnradmin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/hnrcontrolv1"
	"github.com/peergo/peergo/services/core/internal/platform/settlementcontrol"
)

type PostgresDeliveryRepository struct{ pool *pgxpool.Pool }

func NewPostgresDeliveryRepository(pool *pgxpool.Pool) (*PostgresDeliveryRepository, error) {
	if pool == nil {
		return nil, errors.New("H&R delivery database is required")
	}
	return &PostgresDeliveryRepository{pool: pool}, nil
}

func (repository *PostgresDeliveryRepository) Claim(ctx context.Context, now time.Time, batchSize int32, leaseDuration time.Duration) ([]settlementcontrol.PendingCommand, error) {
	if now.IsZero() || batchSize < 1 || batchSize > 100 || leaseDuration <= 0 || leaseDuration > 5*time.Minute {
		return nil, ErrInput
	}
	leaseToken := uuid.New()
	rows, err := repository.pool.Query(ctx, `
WITH candidates AS (
    SELECT delivery.revision_id
    FROM hnr_control.delivery_outbox AS delivery
    WHERE delivery.delivered_at IS NULL
      AND delivery.available_at <= $1
      AND (delivery.lease_until IS NULL OR delivery.lease_until <= $1)
    ORDER BY delivery.available_at, delivery.revision_id
    FOR UPDATE SKIP LOCKED
    LIMIT $2
), claimed AS (
    UPDATE hnr_control.delivery_outbox AS delivery
    SET lease_token = $3,
        lease_until = $4,
        attempts = delivery.attempts + 1,
        last_error_code = NULL
    FROM candidates
    WHERE delivery.revision_id = candidates.revision_id
    RETURNING delivery.revision_id, delivery.lease_token, delivery.attempts
)
SELECT claimed.revision_id, claimed.lease_token, claimed.attempts,
       revision.command_json, revision.command_sha256
FROM claimed
JOIN hnr_control.policy_revisions AS revision ON revision.id = claimed.revision_id
ORDER BY claimed.revision_id`, now, batchSize, leaseToken, now.Add(leaseDuration))
	if err != nil {
		return nil, fmt.Errorf("claim H&R policy deliveries: %w", err)
	}
	defer rows.Close()
	result := make([]settlementcontrol.PendingCommand, 0, batchSize)
	for rows.Next() {
		var pending settlementcontrol.PendingCommand
		var payload string
		var digest []byte
		if err := rows.Scan(&pending.ID, &pending.LeaseToken, &pending.Attempts, &payload, &digest); err != nil {
			return nil, fmt.Errorf("scan H&R policy delivery: %w", err)
		}
		pending.Payload = []byte(payload)
		command, decodeErr := hnrcontrolv1.Decode(pending.Payload)
		canonicalDigest, digestErr := hnrcontrolv1.SHA256(pending.Payload)
		if decodeErr != nil || digestErr != nil || command.RevisionID != pending.ID.String() ||
			pending.LeaseToken != leaseToken || pending.Attempts < 1 || len(digest) != 32 ||
			!bytes.Equal(canonicalDigest[:], digest) {
			return nil, ErrInvariant
		}
		pending.SHA256 = canonicalDigest
		result = append(result, pending)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish H&R policy delivery claim: %w", err)
	}
	return result, nil
}

func (repository *PostgresDeliveryRepository) MarkDelivered(ctx context.Context, pending settlementcontrol.PendingCommand, deliveredAt time.Time) error {
	if !validPending(pending) || deliveredAt.IsZero() {
		return ErrInput
	}
	result, err := repository.pool.Exec(ctx, `
UPDATE hnr_control.delivery_outbox
SET delivered_at = $1, lease_token = NULL, lease_until = NULL, last_error_code = NULL
WHERE revision_id = $2 AND lease_token = $3 AND delivered_at IS NULL`, deliveredAt, pending.ID, pending.LeaseToken)
	if err != nil {
		return fmt.Errorf("mark H&R policy delivered: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrInvariant
	}
	return nil
}

func (repository *PostgresDeliveryRepository) Release(ctx context.Context, pending settlementcontrol.PendingCommand, availableAt time.Time, errorCode string) error {
	if !validPending(pending) || availableAt.IsZero() || errorCode == "" || len(errorCode) > 64 {
		return ErrInput
	}
	result, err := repository.pool.Exec(ctx, `
UPDATE hnr_control.delivery_outbox
SET available_at = $1, lease_token = NULL, lease_until = NULL, last_error_code = $2
WHERE revision_id = $3 AND lease_token = $4 AND delivered_at IS NULL`, availableAt, errorCode, pending.ID, pending.LeaseToken)
	if err != nil {
		return fmt.Errorf("release H&R policy delivery: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrInvariant
	}
	return nil
}

func validPending(pending settlementcontrol.PendingCommand) bool {
	return pending.ID != uuid.Nil && pending.LeaseToken != uuid.Nil && pending.Attempts > 0 && len(pending.Payload) >= 3
}

var _ settlementcontrol.Repository = (*PostgresDeliveryRepository)(nil)
