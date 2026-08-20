package trackercontrol

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/peergo/peergo/services/core/internal/generated/trackercontroldb"
)

// ReadSubjectSnapshot reserves a fresh monotonic revision and reads the
// complete current account allowlist from one repeatable-read transaction.
// Reserving a revision for each build is intentional: time-bounded account
// restrictions can expire without another row mutation, so reusing a sequence
// could otherwise produce different state at the same sequence.
func (repository *PostgresRepository) ReadSubjectSnapshot(ctx context.Context, asOf time.Time) (SubjectProjectionSnapshot, error) {
	if asOf.IsZero() {
		return SubjectProjectionSnapshot{}, ErrSnapshotProjection
	}
	asOf = asOf.UTC().Truncate(time.Microsecond)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return SubjectProjectionSnapshot{}, fmt.Errorf("begin Tracker subject snapshot read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := trackercontroldb.New(tx)
	state, err := queries.ReserveTrackerSubjectSnapshotSequence(ctx, controlTimestamp(asOf))
	if err != nil {
		return SubjectProjectionSnapshot{}, fmt.Errorf("reserve Tracker subject snapshot sequence: %w", err)
	}
	if state.LastSequence < 1 || !state.UpdatedAt.Valid || !state.UpdatedAt.Time.UTC().Equal(asOf) {
		return SubjectProjectionSnapshot{}, ErrSnapshotProjection
	}
	rows, err := queries.ListTrackerSubjectSnapshotEntries(ctx, controlTimestamp(asOf))
	if err != nil {
		return SubjectProjectionSnapshot{}, fmt.Errorf("read Tracker subject snapshot allowlist: %w", err)
	}
	subjects := make([]SubjectAllowlistEntry, 0, len(rows))
	var previous [32]byte
	for index, row := range rows {
		if row.UserID == uuid.Nil || row.NumericUserID < 1 || len(row.LookupHmac) != len(previous) || row.VaultVersion < 1 {
			return SubjectProjectionSnapshot{}, errors.New("Tracker subject projection contains invalid metadata")
		}
		var lookup [32]byte
		copy(lookup[:], row.LookupHmac)
		if index > 0 && bytes.Compare(lookup[:], previous[:]) <= 0 {
			return SubjectProjectionSnapshot{}, errors.New("Tracker subject projection is not strictly ordered")
		}
		subjects = append(subjects, SubjectAllowlistEntry{
			UserID: row.UserID, NumericUserID: row.NumericUserID, LookupHMAC: lookup, CredentialVersion: row.VaultVersion,
			DownloadRestricted: row.DownloadRestricted,
		})
		previous = lookup
	}
	if err := tx.Commit(ctx); err != nil {
		return SubjectProjectionSnapshot{}, fmt.Errorf("commit Tracker subject snapshot read: %w", err)
	}
	return SubjectProjectionSnapshot{
		ControlSequence: state.LastSequence, GeneratedAt: asOf, Subjects: subjects,
	}, nil
}

var _ SubjectSnapshotSource = (*PostgresRepository)(nil)
