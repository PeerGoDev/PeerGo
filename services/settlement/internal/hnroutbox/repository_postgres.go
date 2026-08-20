package hnroutbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/settlementhnrv1"
	"github.com/peergo/peergo/services/settlement/internal/generated/ledgerdb"
	"github.com/peergo/peergo/services/settlement/internal/outboxdispatch"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, ErrInput
	}
	return &PostgresRepository{pool: pool}, nil
}

func (repository *PostgresRepository) ClaimNext(ctx context.Context, now time.Time, leaseDuration time.Duration) (PendingEvent, bool, error) {
	if now.IsZero() || leaseDuration < time.Second || leaseDuration > 10*time.Minute {
		return PendingEvent{}, false, ErrInput
	}
	leaseToken := uuid.New()
	row, err := ledgerdb.New(repository.pool).ClaimNextHNROutboxEvent(ctx, ledgerdb.ClaimNextHNROutboxEventParams{
		LeaseToken: leaseToken, LeaseUntil: timestamp(now.Add(leaseDuration)), ClaimedAt: timestamp(now),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return PendingEvent{}, false, nil
	}
	if err != nil {
		return PendingEvent{}, false, fmt.Errorf("claim Settlement H&R outbox event: %w", err)
	}
	if row.EventID == uuid.Nil || row.ObligationID == uuid.Nil || row.ObligationVersion < 1 ||
		row.EventType != "settlement.hnr.updated" || row.SchemaVersion != settlementhnrv1.SchemaVersion ||
		!row.OccurredAt.Valid || len(row.PayloadSha256) != sha256.Size || !row.LeaseToken.Valid ||
		uuid.UUID(row.LeaseToken.Bytes) != leaseToken || row.Attempts < 1 {
		return PendingEvent{}, false, ErrInvariant
	}
	payload := []byte(row.PayloadJson)
	digest := sha256.Sum256(payload)
	event, err := settlementhnrv1.Decode(payload)
	if err != nil || event.EventID != row.EventID.String() || event.ObligationID != row.ObligationID.String() ||
		event.ObligationVersion != row.ObligationVersion || !event.OccurredAt.Equal(row.OccurredAt.Time) ||
		!bytes.Equal(row.PayloadSha256, digest[:]) {
		return PendingEvent{}, false, ErrInvariant
	}
	return PendingEvent{EventID: row.EventID, LeaseToken: leaseToken, Attempts: row.Attempts, Event: event, Payload: payload}, true, nil
}

func (repository *PostgresRepository) MarkPublished(ctx context.Context, pending PendingEvent, publishedAt time.Time) error {
	if pending.EventID == uuid.Nil || pending.LeaseToken == uuid.Nil || publishedAt.IsZero() {
		return ErrInput
	}
	rows, err := ledgerdb.New(repository.pool).MarkHNROutboxEventPublished(ctx, ledgerdb.MarkHNROutboxEventPublishedParams{
		PublishedAt: timestamp(publishedAt), EventID: pending.EventID, LeaseToken: pending.LeaseToken,
	})
	if err != nil {
		return fmt.Errorf("mark Settlement H&R outbox event published: %w", err)
	}
	if rows != 1 {
		return ErrInvariant
	}
	return nil
}

func (repository *PostgresRepository) Release(ctx context.Context, pending PendingEvent, availableAt time.Time, errorCode string) error {
	if pending.EventID == uuid.Nil || pending.LeaseToken == uuid.Nil || availableAt.IsZero() || !outboxdispatch.ValidErrorCode(errorCode) {
		return ErrInput
	}
	rows, err := ledgerdb.New(repository.pool).ReleaseHNROutboxEvent(ctx, ledgerdb.ReleaseHNROutboxEventParams{
		AvailableAt: timestamp(availableAt), LastErrorCode: errorCode,
		EventID: pending.EventID, LeaseToken: pending.LeaseToken,
	})
	if err != nil {
		return fmt.Errorf("release Settlement H&R outbox event: %w", err)
	}
	if rows != 1 {
		return ErrInvariant
	}
	return nil
}

func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC().Truncate(time.Microsecond), Valid: true}
}

var _ Repository = (*PostgresRepository)(nil)
