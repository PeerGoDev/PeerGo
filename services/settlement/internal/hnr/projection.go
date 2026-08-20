package hnr

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/contracts/go/settlementhnrv1"
	"github.com/peergo/peergo/services/settlement/internal/generated/ledgerdb"
)

func (repository *PostgresRepository) appendProjection(ctx context.Context, queries *ledgerdb.Queries, record obligationRecord, occurredAt time.Time) error {
	eventID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate H&R projection event ID: %w", err)
	}
	occurredAt = maxHNRTime(occurredAt, record.LastEvidenceAt)
	completedAt := normalizeHNRTime(record.Assessment.CompletedAt)
	assessmentDueAt := normalizeHNRTime(record.Assessment.AssessmentDueAt)
	graceEndsAt := normalizeHNRTime(record.Assessment.GraceEndsAt)
	var satisfiedAt *time.Time
	if record.SatisfiedAt != nil {
		value := normalizeHNRTime(*record.SatisfiedAt)
		satisfiedAt = &value
	}
	event := settlementhnrv1.Event{
		SchemaVersion: settlementhnrv1.SchemaVersion, EventID: eventID.String(), OccurredAt: occurredAt,
		ObligationID: record.ID.String(), ObligationVersion: record.Version,
		UserID: record.Assessment.UserID.String(), TorrentID: record.Assessment.TorrentID,
		CompletedAt: completedAt, State: record.State,
		SeededSeconds: record.SeededSeconds, RequiredSeedSeconds: record.Assessment.Policy.RequiredSeedSeconds,
		RawUploaded: record.RawUploaded, RawDownloaded: record.Assessment.RawDownloaded,
		RawRatioBasisPoints: record.RawRatioBasisPoints,
		RequiredRatioBPS:    record.Assessment.Policy.RequiredRatioBasisPoints,
		AssessmentDueAt:     assessmentDueAt, GraceEndsAt: graceEndsAt,
		SatisfiedBy: record.SatisfiedBy, SatisfiedAt: satisfiedAt,
	}
	payload, err := settlementhnrv1.Encode(event)
	if err != nil {
		return fmt.Errorf("%w: encode H&R projection: %v", ErrInvariant, err)
	}
	digest := sha256.Sum256(payload)
	if err := queries.AppendHNROutboxEvent(ctx, ledgerdb.AppendHNROutboxEventParams{
		EventID: eventID, ObligationID: record.ID, ObligationVersion: record.Version,
		OccurredAt: hnrTimestamp(occurredAt), PayloadJson: string(payload), PayloadSha256: digest[:],
		AvailableAt: hnrTimestamp(occurredAt), CreatedAt: hnrTimestamp(occurredAt),
	}); err != nil {
		return classifyHNRError("append H&R projection outbox event", err)
	}
	return nil
}
