package audit

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/peergo/peergo/services/core/internal/generated/auditdb"
)

type fakeAuditQueries struct {
	appendParams auditdb.AppendAuditEventParams
	claimRows    []auditdb.ClaimPendingAuditEventsRow
	markRows     int64
	releaseRows  int64
}

func (queries *fakeAuditQueries) AppendAuditEvent(_ context.Context, params auditdb.AppendAuditEventParams) error {
	queries.appendParams = params
	return nil
}

func (queries *fakeAuditQueries) ClaimPendingAuditEvents(context.Context, auditdb.ClaimPendingAuditEventsParams) ([]auditdb.ClaimPendingAuditEventsRow, error) {
	return queries.claimRows, nil
}

func (queries *fakeAuditQueries) MarkAuditEventDelivered(context.Context, auditdb.MarkAuditEventDeliveredParams) (int64, error) {
	return queries.markRows, nil
}

func (queries *fakeAuditQueries) ReleaseAuditEvent(context.Context, auditdb.ReleaseAuditEventParams) (int64, error) {
	return queries.releaseRows, nil
}

func TestPostgresRepositoryValidatesEvidenceOnAppendAndClaim(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	eventID := uuid.New()
	payload := []byte(`{"event_id":"` + eventID.String() + `","event_type":"authz.decision.recorded","schema_version":"1.0.0","occurred_at":"2026-08-05T12:00:00Z"}`)
	digest := sha256.Sum256(payload)
	queries := &fakeAuditQueries{}
	repository := &PostgresRepository{queries: queries}
	event := Event{
		ID: eventID, Type: DecisionRecordedEventType, SchemaVersion: DecisionRecordedSchemaVersion,
		OccurredAt: now, Payload: payload, PayloadSHA256: digest,
	}
	if err := repository.Append(context.Background(), event); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if queries.appendParams.EventID != eventID || queries.appendParams.PayloadJson != string(payload) || !queries.appendParams.OccurredAt.Valid {
		t.Fatalf("append params = %+v", queries.appendParams)
	}

	queries.claimRows = []auditdb.ClaimPendingAuditEventsRow{{
		EventID: eventID, EventType: event.Type, SchemaVersion: event.SchemaVersion,
		OccurredAt: pgtype.Timestamptz{Time: now, Valid: true}, PayloadJson: string(payload),
		PayloadSha256: digest[:], Attempts: 2,
	}}
	claimed, err := repository.Claim(context.Background(), now, 20, 30*time.Second)
	if err != nil || len(claimed) != 1 || claimed[0].Attempts != 2 || claimed[0].ID != eventID {
		t.Fatalf("Claim() = (%+v, %v)", claimed, err)
	}

	badDigest := sha256.Sum256([]byte("different"))
	queries.claimRows[0].PayloadSha256 = badDigest[:]
	if _, err := repository.Claim(context.Background(), now, 20, 30*time.Second); err == nil {
		t.Fatal("Claim(corrupt digest) error = nil")
	}
}

func TestPostgresRepositoryDetectsLostDeliveryState(t *testing.T) {
	t.Parallel()

	repository := &PostgresRepository{queries: &fakeAuditQueries{}}
	if err := repository.MarkDelivered(context.Background(), uuid.New(), time.Now()); !errors.Is(err, ErrDeliveryStateConflict) {
		t.Fatalf("MarkDelivered() error = %v, want state conflict", err)
	}
	if err := repository.Release(context.Background(), uuid.New(), time.Now(), "failed"); !errors.Is(err, ErrDeliveryStateConflict) {
		t.Fatalf("Release() error = %v, want state conflict", err)
	}
}
