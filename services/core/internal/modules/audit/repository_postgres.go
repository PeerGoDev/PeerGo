package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/peergo/peergo/services/core/internal/generated/auditdb"
)

var ErrDeliveryStateConflict = errors.New("audit delivery state conflict")

type auditQueries interface {
	AppendAuditEvent(context.Context, auditdb.AppendAuditEventParams) error
	ClaimPendingAuditEvents(context.Context, auditdb.ClaimPendingAuditEventsParams) ([]auditdb.ClaimPendingAuditEventsRow, error)
	MarkAuditEventDelivered(context.Context, auditdb.MarkAuditEventDeliveredParams) (int64, error)
	ReleaseAuditEvent(context.Context, auditdb.ReleaseAuditEventParams) (int64, error)
}

// PostgresRepository keeps immutable evidence and mutable delivery state behind
// typed operations. db may be a pool or pgx transaction; using a transaction is
// required when a future business mutation and its audit event must commit as
// one unit.
type PostgresRepository struct {
	queries auditQueries
}

func NewPostgresRepository(db auditdb.DBTX) *PostgresRepository {
	return &PostgresRepository{queries: auditdb.New(db)}
}

func (repository *PostgresRepository) Append(ctx context.Context, event Event) error {
	if err := validateEvent(event); err != nil {
		return err
	}
	if err := repository.queries.AppendAuditEvent(ctx, auditdb.AppendAuditEventParams{
		EventID:       event.ID,
		EventType:     event.Type,
		SchemaVersion: event.SchemaVersion,
		OccurredAt:    auditTimestamp(event.OccurredAt),
		PayloadJson:   string(event.Payload),
		PayloadSha256: event.PayloadSHA256[:],
	}); err != nil {
		return fmt.Errorf("append audit outbox event: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) Claim(ctx context.Context, now time.Time, batchSize int32, leaseDuration time.Duration) ([]PendingEvent, error) {
	if now.IsZero() || batchSize < 1 || batchSize > 100 || leaseDuration <= 0 || leaseDuration > 5*time.Minute {
		return nil, errors.New("invalid audit outbox claim parameters")
	}
	rows, err := repository.queries.ClaimPendingAuditEvents(ctx, auditdb.ClaimPendingAuditEventsParams{
		AvailableAt: auditTimestamp(now),
		Limit:       batchSize,
		LeaseUntil:  auditTimestamp(now.Add(leaseDuration)),
	})
	if err != nil {
		return nil, fmt.Errorf("claim audit outbox events: %w", err)
	}

	events := make([]PendingEvent, 0, len(rows))
	for _, row := range rows {
		if !row.OccurredAt.Valid || len(row.PayloadSha256) != sha256.Size {
			return nil, errors.New("claimed audit event has invalid persisted metadata")
		}
		var digest [sha256.Size]byte
		copy(digest[:], row.PayloadSha256)
		event := Event{
			ID:            row.EventID,
			Type:          row.EventType,
			SchemaVersion: row.SchemaVersion,
			OccurredAt:    row.OccurredAt.Time.UTC(),
			Payload:       []byte(row.PayloadJson),
			PayloadSHA256: digest,
		}
		if err := validateEvent(event); err != nil {
			return nil, fmt.Errorf("validate claimed audit event %s: %w", row.EventID, err)
		}
		events = append(events, PendingEvent{Event: event, Attempts: row.Attempts})
	}
	return events, nil
}

func (repository *PostgresRepository) MarkDelivered(ctx context.Context, eventID uuid.UUID, deliveredAt time.Time) error {
	if eventID == uuid.Nil || deliveredAt.IsZero() {
		return errors.New("invalid delivered audit event metadata")
	}
	rows, err := repository.queries.MarkAuditEventDelivered(ctx, auditdb.MarkAuditEventDeliveredParams{
		EventID:     eventID,
		DeliveredAt: auditTimestamp(deliveredAt),
	})
	if err != nil {
		return fmt.Errorf("mark audit event delivered: %w", err)
	}
	if rows != 1 {
		return ErrDeliveryStateConflict
	}
	return nil
}

func (repository *PostgresRepository) Release(ctx context.Context, eventID uuid.UUID, availableAt time.Time, reason string) error {
	if eventID == uuid.Nil || availableAt.IsZero() {
		return errors.New("invalid released audit event metadata")
	}
	reason = sanitizeDeliveryError(reason)
	rows, err := repository.queries.ReleaseAuditEvent(ctx, auditdb.ReleaseAuditEventParams{
		EventID:     eventID,
		AvailableAt: auditTimestamp(availableAt),
		LastError:   pgtype.Text{String: reason, Valid: reason != ""},
	})
	if err != nil {
		return fmt.Errorf("release audit event: %w", err)
	}
	if rows != 1 {
		return ErrDeliveryStateConflict
	}
	return nil
}

func validateEvent(event Event) error {
	if event.ID == uuid.Nil || event.Type == "" || event.SchemaVersion == "" || event.OccurredAt.IsZero() {
		return errors.New("audit event is missing required metadata")
	}
	if len(event.Payload) < 2 || len(event.Payload) > MaxEventPayloadBytes || !json.Valid(event.Payload) {
		return errors.New("audit event payload is not valid bounded JSON")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(event.Payload, &object); err != nil || object == nil {
		return errors.New("audit event payload must be a JSON object")
	}
	var envelope struct {
		EventID       uuid.UUID `json:"event_id"`
		EventType     string    `json:"event_type"`
		SchemaVersion string    `json:"schema_version"`
		OccurredAt    time.Time `json:"occurred_at"`
	}
	if err := json.Unmarshal(event.Payload, &envelope); err != nil || envelope.EventID != event.ID || envelope.EventType != event.Type || envelope.SchemaVersion != event.SchemaVersion || !envelope.OccurredAt.Equal(event.OccurredAt) {
		return errors.New("audit event envelope does not match outbox metadata")
	}
	digest := sha256.Sum256(event.Payload)
	if !bytes.Equal(digest[:], event.PayloadSHA256[:]) {
		return errors.New("audit event payload digest does not match")
	}
	return nil
}

func sanitizeDeliveryError(reason string) string {
	reason = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(reason, "\n", " "), "\r", " "))
	if len(reason) > 1000 {
		reason = reason[:1000]
	}
	return reason
}

func auditTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

var _ EventAppender = (*PostgresRepository)(nil)
