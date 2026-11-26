package reseedreward

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

var (
	ErrInput    = errors.New("reseed reward settings are invalid")
	ErrConflict = errors.New("reseed reward settings revision changed")
)

type Settings struct {
	Revision      int64
	Enabled       bool
	RequiredHours int32
	MagicAmount   int64
	IssuedAt      time.Time
}

type UpdateInput struct {
	ExpectedRevision int64
	Enabled          bool
	RequiredHours    int32
	MagicAmount      int64
}

type Administration struct {
	repository *PostgresRepository
	authorizer authz.Authorizer
}

func NewAdministration(repository *PostgresRepository, authorizer authz.Authorizer) (*Administration, error) {
	if repository == nil || authorizer == nil {
		return nil, ErrInput
	}
	return &Administration{repository: repository, authorizer: authorizer}, nil
}

func (service *Administration) Settings(ctx context.Context, actor authz.StaffActor) (Settings, error) {
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionEconomySeedingRewardPolicyRead, authz.SiteScope(), time.Now().UTC(), "reseed-reward-settings-read"); err != nil {
		return Settings{}, err
	}
	return service.repository.ReadSettings(ctx)
}

func (service *Administration) UpdateSettings(ctx context.Context, actor authz.StaffActor, input UpdateInput) (Settings, error) {
	if input.ExpectedRevision < 1 || input.RequiredHours < 1 || input.RequiredHours > 720 || input.MagicAmount < 1 || input.MagicAmount > 1_000_000 {
		return Settings{}, ErrInput
	}
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionEconomySeedingRewardPolicyIssue, authz.SiteScope(), time.Now().UTC(), "reseed-reward-settings-update")
	if err != nil {
		return Settings{}, err
	}
	return service.repository.saveSettings(ctx, input, actor.Subject.ID, decision.ID)
}

func (repository *PostgresRepository) ReadSettings(ctx context.Context) (Settings, error) {
	var result Settings
	err := repository.pool.QueryRow(ctx, `
SELECT revision, enabled, required_hours, magic_amount, issued_at
FROM economy.reseed_reward_policies ORDER BY revision DESC LIMIT 1`).Scan(
		&result.Revision, &result.Enabled, &result.RequiredHours, &result.MagicAmount, &result.IssuedAt)
	if err != nil {
		return Settings{}, fmt.Errorf("read reseed reward settings: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) saveSettings(ctx context.Context, input UpdateInput, actorID, decisionID uuid.UUID) (Settings, error) {
	var result Settings
	err := repository.pool.QueryRow(ctx, `
INSERT INTO economy.reseed_reward_policies (revision, enabled, required_hours, magic_amount, issued_at, issued_by, authorization_decision_id)
SELECT revision + 1, $2, $3, $4, clock_timestamp(), $5, $6
FROM (SELECT revision FROM economy.reseed_reward_policies ORDER BY revision DESC LIMIT 1) AS latest
WHERE latest.revision = $1
ON CONFLICT (revision) DO NOTHING
RETURNING revision, enabled, required_hours, magic_amount, issued_at`,
		input.ExpectedRevision, input.Enabled, input.RequiredHours, input.MagicAmount, actorID, decisionID).Scan(
		&result.Revision, &result.Enabled, &result.RequiredHours, &result.MagicAmount, &result.IssuedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Settings{}, ErrConflict
	}
	if err != nil {
		return Settings{}, fmt.Errorf("append reseed reward settings: %w", err)
	}
	return result, nil
}
