package authz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/generated/authzdb"
)

const maxMFAMaxAgeSeconds = 24 * 60 * 60

type authzQueries interface {
	ListPermissionCatalog(context.Context) ([]authzdb.ListPermissionCatalogRow, error)
	ListSubjectGrants(context.Context, uuid.UUID) ([]authzdb.ListSubjectGrantsRow, error)
}

type PostgresRepository struct {
	queries authzQueries
}

func NewPostgresRepository(db authzdb.DBTX) *PostgresRepository {
	return &PostgresRepository{queries: authzdb.New(db)}
}

func (repository *PostgresRepository) ListPermissionCatalog(ctx context.Context) ([]PersistedPermission, error) {
	rows, err := repository.queries.ListPermissionCatalog(ctx)
	if err != nil {
		return nil, fmt.Errorf("list permission catalog: %w", err)
	}
	result := make([]PersistedPermission, 0, len(rows))
	for _, row := range rows {
		result = append(result, PersistedPermission{
			Action:             Action(row.Action),
			Description:        row.Description,
			Risk:               RiskLevel(row.RiskLevel),
			Relationship:       Relationship(row.Relationship),
			CredentialAudience: CredentialAudience(row.CredentialAudience),
			Grantable:          row.Grantable,
			Discoverable:       row.Discoverable,
		})
	}
	return result, nil
}

func (repository *PostgresRepository) ListSubjectGrants(ctx context.Context, subjectID uuid.UUID) ([]Grant, error) {
	rows, err := repository.queries.ListSubjectGrants(ctx, subjectID)
	if err != nil {
		return nil, fmt.Errorf("list subject grants: %w", err)
	}
	result := make([]Grant, 0, len(rows))
	for _, row := range rows {
		if !row.ValidFrom.Valid || !row.ValidUntil.Valid || !row.MandateStartsAt.Valid || !row.MandateEndsAt.Valid {
			return nil, errors.New("authorization grant contains an invalid timestamp")
		}
		constraints, err := decodeConstraints(row.ConstraintsJson)
		if err != nil {
			return nil, fmt.Errorf("decode constraints for grant %s: %w", row.ID, err)
		}
		var revokedAt *time.Time
		if row.RevokedAt.Valid {
			value := row.RevokedAt.Time.UTC()
			revokedAt = &value
		}
		result = append(result, Grant{
			ID:          row.ID,
			SubjectID:   row.SubjectID,
			RoleID:      row.RoleID,
			Action:      Action(row.Action),
			Scope:       Scope{Type: ScopeType(row.ScopeType), ID: row.ScopeID},
			ValidFrom:   row.ValidFrom.Time.UTC(),
			ValidUntil:  row.ValidUntil.Time.UTC(),
			Constraints: constraints,
			Version:     row.Version,
			RevokedAt:   revokedAt,
			Mandate: Mandate{
				ID:        row.MandateID,
				SubjectID: row.MandateSubjectID,
				Scope:     Scope{Type: ScopeType(row.MandateScopeType), ID: row.MandateScopeID},
				StartsAt:  row.MandateStartsAt.Time.UTC(),
				EndsAt:    row.MandateEndsAt.Time.UTC(),
				Status:    MandateStatus(row.MandateStatus),
			},
		})
	}
	return result, nil
}

func decodeConstraints(encoded string) (Constraints, error) {
	decoder := json.NewDecoder(strings.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var constraints Constraints
	if err := decoder.Decode(&constraints); err != nil {
		return Constraints{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Constraints{}, errors.New("multiple JSON values")
		}
		return Constraints{}, err
	}
	if constraints.MFAMaxAgeSeconds < 0 || constraints.MFAMaxAgeSeconds > maxMFAMaxAgeSeconds {
		return Constraints{}, errors.New("mfa_max_age_seconds is outside the supported range")
	}
	return constraints, nil
}

var _ Repository = (*PostgresRepository)(nil)
