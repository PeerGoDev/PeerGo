package credentials

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

const maxEmailDirectoryBatch = 50

type EmailRecord struct {
	CredentialRef uuid.UUID
	Email         string
}

// ListEmails is the only administrative direct-identifier read exposed by the
// Vault. It accepts opaque references and never returns password material,
// lookup HMACs or Tracker passkeys.
func (r *PostgresRepository) ListEmails(ctx context.Context, credentialRefs []uuid.UUID) ([]EmailRecord, error) {
	if len(credentialRefs) < 1 || len(credentialRefs) > maxEmailDirectoryBatch {
		return nil, errors.New("email directory batch is invalid")
	}
	seen := make(map[uuid.UUID]struct{}, len(credentialRefs))
	for _, credentialRef := range credentialRefs {
		if credentialRef == uuid.Nil {
			return nil, errors.New("email directory credential reference is invalid")
		}
		if _, exists := seen[credentialRef]; exists {
			return nil, errors.New("email directory contains duplicate credential references")
		}
		seen[credentialRef] = struct{}{}
	}
	rows, err := r.db.Query(ctx, `
SELECT credential_ref, email_address
FROM vault.email_addresses
WHERE credential_ref = ANY($1::uuid[])
ORDER BY credential_ref`, credentialRefs)
	if err != nil {
		return nil, fmt.Errorf("query email directory: %w", err)
	}
	defer rows.Close()
	result := make([]EmailRecord, 0, len(credentialRefs))
	for rows.Next() {
		var record EmailRecord
		if err := rows.Scan(&record.CredentialRef, &record.Email); err != nil {
			return nil, fmt.Errorf("scan email directory: %w", err)
		}
		if record.CredentialRef == uuid.Nil || !validNormalizedEmail(record.Email) {
			return nil, errors.New("email directory contains invalid data")
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read email directory: %w", err)
	}
	return result, nil
}
