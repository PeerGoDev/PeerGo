package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/services/settlement/internal/generated/ledgerdb"
)

const sourceSequenceConstraint = "event_inbox_source_stream_source_sequence_key"

type PostgresRepository struct {
	pool            *pgxpool.Pool
	expectedStream  string
	expectedSubject string
	now             func() time.Time
}

func NewPostgresRepository(pool *pgxpool.Pool, stream, subject string, now func() time.Time) (*PostgresRepository, error) {
	if pool == nil || !trackerannouncev1.ValidStreamName(stream) || !trackerannouncev1.ValidLiteralSubject(subject) {
		return nil, ErrInvalidInput
	}
	if now == nil {
		now = time.Now
	}
	return &PostgresRepository{pool: pool, expectedStream: stream, expectedSubject: subject, now: now}, nil
}

// Process commits the inbox idempotency fence, serialized session transition,
// and optional raw ledger interval atomically. The JetStream consumer may ACK
// only after this method returns success; a lost ACK simply takes the duplicate
// branch on redelivery without applying the interval again.
func (repository *PostgresRepository) Process(ctx context.Context, delivery Delivery) (ProcessResult, error) {
	if delivery.Stream != repository.expectedStream || delivery.Subject != repository.expectedSubject ||
		delivery.Sequence == 0 || delivery.Sequence > math.MaxInt64 ||
		delivery.DeliveryCount == 0 || delivery.DeliveryCount > math.MaxInt64 {
		return ProcessResult{}, ErrSourceInvariant
	}
	event, err := trackerannouncev1.Decode(delivery.Payload)
	if err != nil {
		return ProcessResult{}, ErrInvalidInput
	}
	eventID, err := uuid.Parse(event.EventID)
	if err != nil {
		return ProcessResult{}, ErrInvalidInput
	}
	userID, err := uuid.Parse(event.UserID)
	if err != nil {
		return ProcessResult{}, ErrInvalidInput
	}
	infoHash, err := trackerannouncev1.DecodeInfoHashV1(event.InfoHashV1)
	if err != nil {
		return ProcessResult{}, ErrInvalidInput
	}
	sessionToken, err := trackerannouncev1.DecodeSessionToken(event.SessionToken)
	if err != nil {
		return ProcessResult{}, ErrInvalidInput
	}
	payloadDigest := sha256.Sum256(delivery.Payload)
	processedAt := canonicalIngestTime(repository.now())
	if processedAt.IsZero() {
		return ProcessResult{}, ErrInvalidInput
	}

	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ProcessResult{}, fmt.Errorf("begin Settlement ingest: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := ledgerdb.New(tx)
	_, err = queries.ClaimInboxEvent(ctx, ledgerdb.ClaimInboxEventParams{
		EventID: eventID, PayloadSha256: payloadDigest[:], PayloadJson: string(delivery.Payload),
		SourceStream: delivery.Stream, SourceSubject: delivery.Subject,
		SourceSequence: int64(delivery.Sequence), DeliveryCount: int64(delivery.DeliveryCount),
		ReceivedAt: timestamp(event.ReceivedAt), IngestedAt: timestamp(processedAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.duplicateResult(ctx, queries, eventID, event, payloadDigest, delivery.Payload)
	}
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.ConstraintName == sourceSequenceConstraint {
			return ProcessResult{}, fmt.Errorf("%w: stream sequence already belongs to another event", ErrSourceInvariant)
		}
		return ProcessResult{}, repositoryOperationError("claim Settlement inbox event", err)
	}
	if err := queries.LockSettlementSession(ctx, ledgerdb.LockSettlementSessionParams{
		UserID: event.UserID, TorrentID: event.TorrentID, SessionToken: sessionToken[:],
	}); err != nil {
		return ProcessResult{}, fmt.Errorf("lock Settlement session: %w", err)
	}

	row, err := queries.GetSettlementSessionForUpdate(ctx, ledgerdb.GetSettlementSessionForUpdateParams{
		UserID: userID, TorrentID: event.TorrentID, SessionToken: sessionToken[:],
	})
	var previous *Session
	if err == nil {
		state, convertErr := sessionFromRow(row)
		if convertErr != nil {
			return ProcessResult{}, convertErr
		}
		previous = &state
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return ProcessResult{}, fmt.Errorf("read Settlement session: %w", err)
	}

	transition, err := Evaluate(previous, event)
	if err != nil {
		return ProcessResult{}, err
	}
	if transition.Update {
		if previous == nil {
			if err := insertSession(ctx, queries, transition.State, eventID, userID, infoHash, sessionToken, processedAt); err != nil {
				return ProcessResult{}, err
			}
		} else if err := updateSession(ctx, queries, *previous, transition.State, eventID, userID, sessionToken, processedAt); err != nil {
			return ProcessResult{}, err
		}
	}
	if transition.Interval != nil {
		if err := insertInterval(ctx, queries, *transition.Interval, eventID, userID, infoHash, sessionToken, processedAt); err != nil {
			return ProcessResult{}, err
		}
		rows, err := queries.FinalizeInboxWithInterval(ctx, ledgerdb.FinalizeInboxWithIntervalParams{
			SessionEpoch: transition.Epoch, ProcessedAt: timestamp(processedAt), EventID: eventID,
		})
		if err != nil {
			return ProcessResult{}, repositoryOperationError("finalize Settlement interval inbox event", err)
		}
		if rows != 1 {
			return ProcessResult{}, fmt.Errorf("%w: finalized %d interval inbox rows", ErrSessionInvariant, rows)
		}
	} else {
		rows, err := queries.FinalizeInboxWithoutInterval(ctx, ledgerdb.FinalizeInboxWithoutIntervalParams{
			Outcome: string(transition.Outcome), SessionEpoch: transition.Epoch,
			ProcessedAt: timestamp(processedAt), EventID: eventID,
		})
		if err != nil {
			return ProcessResult{}, repositoryOperationError("finalize Settlement inbox event", err)
		}
		if rows != 1 {
			return ProcessResult{}, fmt.Errorf("%w: finalized %d inbox rows", ErrSessionInvariant, rows)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ProcessResult{}, repositoryOperationError("commit Settlement ingest", err)
	}
	return ProcessResult{EventID: event.EventID, Outcome: transition.Outcome}, nil
}

func (repository *PostgresRepository) duplicateResult(
	ctx context.Context,
	queries *ledgerdb.Queries,
	eventID uuid.UUID,
	event trackerannouncev1.Event,
	digest [sha256.Size]byte,
	payload []byte,
) (ProcessResult, error) {
	existing, err := queries.GetInboxEvent(ctx, eventID)
	if err != nil {
		return ProcessResult{}, fmt.Errorf("read duplicate Settlement inbox event: %w", err)
	}
	if !bytes.Equal(existing.PayloadSha256, digest[:]) || !bytes.Equal([]byte(existing.PayloadJson), payload) {
		return ProcessResult{}, ErrEventConflict
	}
	if existing.Outcome == "processing" || !existing.SessionEpoch.Valid || existing.SessionEpoch.Int64 < 1 {
		return ProcessResult{}, fmt.Errorf("%w: committed inbox row is not terminal", ErrSessionInvariant)
	}
	return ProcessResult{EventID: event.EventID, Outcome: Outcome(existing.Outcome), Duplicate: true}, nil
}

func sessionFromRow(row ledgerdb.GetSettlementSessionForUpdateRow) (Session, error) {
	if len(row.InfoHashV1) != 20 || len(row.SessionToken) != sha256.Size || !row.LastReceivedAt.Valid {
		return Session{}, ErrSessionInvariant
	}
	return Session{
		UserID: row.UserID.String(), TorrentID: row.TorrentID,
		InfoHashV1: hex.EncodeToString(row.InfoHashV1), SessionToken: hex.EncodeToString(row.SessionToken),
		Epoch: row.SessionEpoch, Version: row.Version, LastEventID: row.LastEventID.String(),
		LastReceivedAt: row.LastReceivedAt.Time.UTC().Round(0), LastEventKind: row.LastEventKind,
		LastUploaded: row.LastUploaded, LastDownloaded: row.LastDownloaded, LastLeft: row.LastLeft,
		LastAddressFamily: int(row.LastAddressFamily), LastCredentialVersion: row.LastCredentialVersion,
		TorrentControlSequence: row.TorrentControlSequence, SubjectControlSequence: row.SubjectControlSequence,
	}, nil
}

func insertSession(
	ctx context.Context,
	queries *ledgerdb.Queries,
	state Session,
	eventID, userID uuid.UUID,
	infoHash [20]byte,
	sessionToken [sha256.Size]byte,
	processedAt time.Time,
) error {
	if err := queries.InsertSettlementSession(ctx, ledgerdb.InsertSettlementSessionParams{
		UserID: userID, TorrentID: state.TorrentID, SessionToken: sessionToken[:], InfoHashV1: infoHash[:],
		SessionEpoch: state.Epoch, Version: state.Version, LastEventID: eventID,
		LastReceivedAt: timestamp(state.LastReceivedAt), LastEventKind: state.LastEventKind,
		LastUploaded: state.LastUploaded, LastDownloaded: state.LastDownloaded, LastLeft: state.LastLeft,
		LastAddressFamily: int16(state.LastAddressFamily), LastCredentialVersion: state.LastCredentialVersion,
		TorrentControlSequence: state.TorrentControlSequence, SubjectControlSequence: state.SubjectControlSequence,
		CreatedAt: timestamp(processedAt), UpdatedAt: timestamp(processedAt),
	}); err != nil {
		return repositoryOperationError("insert Settlement session baseline", err)
	}
	return nil
}

func updateSession(
	ctx context.Context,
	queries *ledgerdb.Queries,
	previous, state Session,
	eventID, userID uuid.UUID,
	sessionToken [sha256.Size]byte,
	processedAt time.Time,
) error {
	rows, err := queries.UpdateSettlementSession(ctx, ledgerdb.UpdateSettlementSessionParams{
		SessionEpoch: state.Epoch, NewVersion: state.Version, LastEventID: eventID,
		LastReceivedAt: timestamp(state.LastReceivedAt), LastEventKind: state.LastEventKind,
		LastUploaded: state.LastUploaded, LastDownloaded: state.LastDownloaded, LastLeft: state.LastLeft,
		LastAddressFamily: int16(state.LastAddressFamily), LastCredentialVersion: state.LastCredentialVersion,
		TorrentControlSequence: state.TorrentControlSequence, SubjectControlSequence: state.SubjectControlSequence,
		UpdatedAt: timestamp(processedAt), UserID: userID, TorrentID: state.TorrentID,
		SessionToken: sessionToken[:], ExpectedVersion: previous.Version,
	})
	if err != nil {
		return repositoryOperationError("update Settlement session", err)
	}
	if rows != 1 {
		return ErrSessionInvariant
	}
	return nil
}

func insertInterval(
	ctx context.Context,
	queries *ledgerdb.Queries,
	interval Interval,
	eventID, userID uuid.UUID,
	infoHash [20]byte,
	sessionToken [sha256.Size]byte,
	processedAt time.Time,
) error {
	previousEventID, err := uuid.Parse(interval.PreviousEventID)
	if err != nil {
		return ErrSessionInvariant
	}
	var completionID []byte
	if interval.CompletedTransition {
		completionID, err = hex.DecodeString(interval.CompletionID)
		if err != nil || len(completionID) != sha256.Size {
			return ErrSessionInvariant
		}
	} else if interval.CompletionID != "" {
		return ErrSessionInvariant
	}
	var networkPolicySequence pgtype.Int8
	var networkPolicyRevision, networkClass, networkRuleID pgtype.Text
	var uploadFactor, downloadFactor pgtype.Int4
	var downloadFactorExplicit pgtype.Bool
	var speedLimit pgtype.Int8
	if interval.NetworkEvidence != nil {
		evidence := interval.NetworkEvidence
		networkPolicySequence = pgtype.Int8{Int64: evidence.PolicySequence, Valid: true}
		networkPolicyRevision = pgtype.Text{String: evidence.PolicyRevision, Valid: true}
		networkClass = pgtype.Text{String: evidence.Class, Valid: true}
		if evidence.RuleID != "" {
			networkRuleID = pgtype.Text{String: evidence.RuleID, Valid: true}
		}
		uploadFactor = pgtype.Int4{Int32: int32(evidence.UploadFactorBasisPoints), Valid: true}
		downloadFactorValue := int64(10_000)
		downloadFactorWasExplicit := evidence.DownloadFactorBasisPoints != nil
		if downloadFactorWasExplicit {
			downloadFactorValue = *evidence.DownloadFactorBasisPoints
		}
		downloadFactor = pgtype.Int4{Int32: int32(downloadFactorValue), Valid: true}
		downloadFactorExplicit = pgtype.Bool{Bool: downloadFactorWasExplicit, Valid: true}
		speedLimit = pgtype.Int8{Int64: evidence.SpeedLimitBytesPerSecond, Valid: true}
	}
	if err := queries.InsertRawSessionInterval(ctx, ledgerdb.InsertRawSessionIntervalParams{
		EventID: eventID, PreviousEventID: previousEventID, UserID: userID,
		TorrentID: interval.TorrentID, SessionToken: sessionToken[:], InfoHashV1: infoHash[:],
		SessionEpoch: interval.SessionEpoch, StartsAt: timestamp(interval.StartsAt), EndsAt: timestamp(interval.EndsAt),
		EventKind: interval.EventKind, AddressFamily: int16(interval.AddressFamily),
		CredentialVersion:      interval.CredentialVersion,
		TorrentControlSequence: interval.TorrentControlSequence,
		SubjectControlSequence: interval.SubjectControlSequence,
		PreviousUploaded:       interval.PreviousUploaded, CurrentUploaded: interval.CurrentUploaded,
		PreviousDownloaded: interval.PreviousDownloaded, CurrentDownloaded: interval.CurrentDownloaded,
		PreviousLeft: interval.PreviousLeft, CurrentLeft: interval.CurrentLeft,
		RawUploaded: interval.RawUploaded, RawDownloaded: interval.RawDownloaded,
		CompletedTransition: interval.CompletedTransition, CompletionID: completionID,
		NetworkPolicySequence: networkPolicySequence, NetworkPolicyRevision: networkPolicyRevision,
		NetworkClass: networkClass, NetworkRuleID: networkRuleID,
		SeedboxUploadFactorBasisPoints:   uploadFactor,
		SeedboxDownloadFactorBasisPoints: downloadFactor,
		SeedboxDownloadFactorExplicit:    downloadFactorExplicit,
		SpeedLimitBytesPerSecond:         speedLimit,
		CreatedAt:                        timestamp(processedAt),
	}); err != nil {
		return repositoryOperationError("insert raw Tracker ledger interval", err)
	}
	return nil
}

func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: canonicalIngestTime(value), Valid: true}
}

// Constraint and data exceptions mean the canonical event or repository code
// violated a ledger invariant. Retrying them forever would hide a poison event
// as an infrastructure outage, so the consumer must stop for investigation.
// Serialization, connection and timeout errors remain transient.
func repositoryOperationError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) &&
		(strings.HasPrefix(postgresError.Code, "22") || strings.HasPrefix(postgresError.Code, "23") || postgresError.Code == "P0001") {
		return fmt.Errorf("%w: %s: %v", ErrSessionInvariant, operation, err)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
