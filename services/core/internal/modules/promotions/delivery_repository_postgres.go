package promotions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/promotioncontrolv1"
	"github.com/peergo/peergo/services/core/internal/generated/promotiondb"
)

type PostgresDeliveryRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresDeliveryRepository(pool *pgxpool.Pool) (*PostgresDeliveryRepository, error) {
	if pool == nil {
		return nil, errors.New("promotion delivery database is required")
	}
	return &PostgresDeliveryRepository{pool: pool}, nil
}

func (repository *PostgresDeliveryRepository) Claim(ctx context.Context, now time.Time, batchSize int32, leaseDuration time.Duration) ([]PendingCommand, error) {
	if now.IsZero() || batchSize < 1 || batchSize > 100 || leaseDuration <= 0 || leaseDuration > 5*time.Minute {
		return nil, ErrInput
	}
	leaseToken := uuid.New()
	rows, err := promotiondb.New(repository.pool).ClaimPromotionDeliveries(ctx, promotiondb.ClaimPromotionDeliveriesParams{
		LeaseToken: leaseToken, LeaseUntil: timestamp(now.Add(leaseDuration)), ClaimedAt: timestamp(now), BatchSize: batchSize,
	})
	if err != nil {
		return nil, fmt.Errorf("claim promotion deliveries: %w", err)
	}
	result := make([]PendingCommand, 0, len(rows))
	for _, row := range rows {
		if row.CampaignID == uuid.Nil || !row.LeaseToken.Valid || uuid.UUID(row.LeaseToken.Bytes) != leaseToken ||
			row.Attempts < 1 || len(row.CommandSha256) != 32 {
			return nil, ErrInvariant
		}
		encoded := []byte(row.CommandJson)
		command, err := promotioncontrolv1.Decode(encoded)
		digest, digestErr := promotioncontrolv1.SHA256(encoded)
		if err != nil || digestErr != nil || command.CampaignID != row.CampaignID.String() || !bytes.Equal(digest[:], row.CommandSha256) {
			return nil, ErrInvariant
		}
		result = append(result, PendingCommand{
			ID: row.CampaignID, Payload: encoded, SHA256: digest,
			LeaseToken: leaseToken, Attempts: row.Attempts,
		})
	}
	return result, nil
}

func (repository *PostgresDeliveryRepository) MarkDelivered(ctx context.Context, pending PendingCommand, deliveredAt time.Time) error {
	if !validPending(pending) || deliveredAt.IsZero() {
		return ErrInput
	}
	rows, err := promotiondb.New(repository.pool).MarkPromotionDeliveryDelivered(ctx, promotiondb.MarkPromotionDeliveryDeliveredParams{
		DeliveredAt: timestamp(deliveredAt), CampaignID: pending.ID, LeaseToken: pending.LeaseToken,
	})
	if err != nil {
		return fmt.Errorf("mark promotion delivery: %w", err)
	}
	if rows != 1 {
		return ErrInvariant
	}
	return nil
}

func (repository *PostgresDeliveryRepository) Release(ctx context.Context, pending PendingCommand, availableAt time.Time, errorCode string) error {
	if !validPending(pending) || availableAt.IsZero() || errorCode == "" || len(errorCode) > 64 {
		return ErrInput
	}
	rows, err := promotiondb.New(repository.pool).ReleasePromotionDelivery(ctx, promotiondb.ReleasePromotionDeliveryParams{
		AvailableAt: timestamp(availableAt), LastErrorCode: errorCode,
		CampaignID: pending.ID, LeaseToken: pending.LeaseToken,
	})
	if err != nil {
		return fmt.Errorf("release promotion delivery: %w", err)
	}
	if rows != 1 {
		return ErrInvariant
	}
	return nil
}

func validPending(pending PendingCommand) bool {
	return pending.ID != uuid.Nil && pending.LeaseToken != uuid.Nil && pending.Attempts > 0 && len(pending.Payload) >= 2
}
