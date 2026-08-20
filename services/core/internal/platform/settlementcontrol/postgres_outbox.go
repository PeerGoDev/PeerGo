package settlementcontrol

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var sqlIdentifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// PayloadValidator verifies a domain command and returns its canonical digest.
// The generic outbox owns leasing mechanics only; it never decides whether a
// payload is a valid VIP, workgroup or H&R fact.
type PayloadValidator func(uuid.UUID, []byte) ([32]byte, error)

type PostgresOutboxConfig struct {
	Schema          string
	Table           string
	IDColumn        string
	Label           string
	ValidatePayload PayloadValidator
	InvariantError  error
}

// PostgresOutbox centralizes the identical lease/ack/retry state machine used
// by immutable Settlement command outboxes. Identifiers are validated once at
// construction and never accept request-derived strings.
type PostgresOutbox struct {
	pool            *pgxpool.Pool
	qualifiedTable  string
	idColumn        string
	label           string
	validatePayload PayloadValidator
	invariantError  error
}

func NewPostgresOutbox(pool *pgxpool.Pool, config PostgresOutboxConfig) (*PostgresOutbox, error) {
	if pool == nil || !sqlIdentifierPattern.MatchString(config.Schema) ||
		!sqlIdentifierPattern.MatchString(config.Table) || !sqlIdentifierPattern.MatchString(config.IDColumn) ||
		config.Label == "" || config.ValidatePayload == nil || config.InvariantError == nil {
		return nil, errors.New("Settlement command outbox configuration is invalid")
	}
	return &PostgresOutbox{
		pool:            pool,
		qualifiedTable:  `"` + config.Schema + `"."` + config.Table + `"`,
		idColumn:        `"` + config.IDColumn + `"`,
		label:           config.Label,
		validatePayload: config.ValidatePayload,
		invariantError:  config.InvariantError,
	}, nil
}

func (outbox *PostgresOutbox) Claim(ctx context.Context, now time.Time, batchSize int32, leaseDuration time.Duration) ([]PendingCommand, error) {
	if now.IsZero() || batchSize < 1 || batchSize > 100 || leaseDuration <= 0 || leaseDuration > 5*time.Minute {
		return nil, outbox.invariantError
	}
	leaseToken := uuid.New()
	query := fmt.Sprintf(`
WITH candidates AS (
    SELECT outbox.%s
    FROM %s AS outbox
    WHERE outbox.delivered_at IS NULL
      AND outbox.available_at <= $1
      AND (outbox.lease_until IS NULL OR outbox.lease_until <= $1)
    ORDER BY outbox.available_at, outbox.state_version, outbox.%s
    FOR UPDATE SKIP LOCKED
    LIMIT $2
), claimed AS (
    UPDATE %s AS outbox
    SET lease_token = $3,
        lease_until = $4,
        attempts = outbox.attempts + 1,
        last_error_code = NULL
    FROM candidates
    WHERE outbox.%s = candidates.%s
    RETURNING outbox.%s, outbox.lease_token, outbox.attempts,
              outbox.command_json, outbox.command_sha256
)
SELECT %s, lease_token, attempts, command_json, command_sha256
FROM claimed
ORDER BY %s`, outbox.idColumn, outbox.qualifiedTable, outbox.idColumn,
		outbox.qualifiedTable, outbox.idColumn, outbox.idColumn, outbox.idColumn,
		outbox.idColumn, outbox.idColumn)
	rows, err := outbox.pool.Query(ctx, query, now, batchSize, leaseToken, now.Add(leaseDuration))
	if err != nil {
		return nil, fmt.Errorf("claim %s deliveries: %w", outbox.label, err)
	}
	defer rows.Close()
	result := make([]PendingCommand, 0, batchSize)
	for rows.Next() {
		var pending PendingCommand
		var payload string
		var storedDigest []byte
		if err := rows.Scan(&pending.ID, &pending.LeaseToken, &pending.Attempts, &payload, &storedDigest); err != nil {
			return nil, fmt.Errorf("scan %s delivery: %w", outbox.label, err)
		}
		pending.Payload = []byte(payload)
		canonicalDigest, validationErr := outbox.validatePayload(pending.ID, pending.Payload)
		if validationErr != nil || pending.ID == uuid.Nil || pending.LeaseToken != leaseToken || pending.Attempts < 1 ||
			len(storedDigest) != 32 || !bytes.Equal(canonicalDigest[:], storedDigest) {
			return nil, outbox.invariantError
		}
		pending.SHA256 = canonicalDigest
		result = append(result, pending)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish %s delivery claim: %w", outbox.label, err)
	}
	return result, nil
}

func (outbox *PostgresOutbox) MarkDelivered(ctx context.Context, pending PendingCommand, deliveredAt time.Time) error {
	if !validPendingCommand(pending) || deliveredAt.IsZero() {
		return outbox.invariantError
	}
	query := fmt.Sprintf(`
UPDATE %s
SET delivered_at = $1, lease_token = NULL, lease_until = NULL, last_error_code = NULL
WHERE %s = $2 AND lease_token = $3 AND delivered_at IS NULL`, outbox.qualifiedTable, outbox.idColumn)
	result, err := outbox.pool.Exec(ctx, query, deliveredAt, pending.ID, pending.LeaseToken)
	if err != nil {
		return fmt.Errorf("mark %s delivered: %w", outbox.label, err)
	}
	if result.RowsAffected() != 1 {
		return outbox.invariantError
	}
	return nil
}

func (outbox *PostgresOutbox) Release(ctx context.Context, pending PendingCommand, availableAt time.Time, errorCode string) error {
	if !validPendingCommand(pending) || availableAt.IsZero() || errorCode == "" || len(errorCode) > 64 {
		return outbox.invariantError
	}
	query := fmt.Sprintf(`
UPDATE %s
SET available_at = $1, lease_token = NULL, lease_until = NULL, last_error_code = $2
WHERE %s = $3 AND lease_token = $4 AND delivered_at IS NULL`, outbox.qualifiedTable, outbox.idColumn)
	result, err := outbox.pool.Exec(ctx, query, availableAt, errorCode, pending.ID, pending.LeaseToken)
	if err != nil {
		return fmt.Errorf("release %s delivery: %w", outbox.label, err)
	}
	if result.RowsAffected() != 1 {
		return outbox.invariantError
	}
	return nil
}

func validPendingCommand(pending PendingCommand) bool {
	return pending.ID != uuid.Nil && pending.LeaseToken != uuid.Nil && pending.Attempts > 0 && len(pending.Payload) >= 2
}

var _ Repository = (*PostgresOutbox)(nil)
