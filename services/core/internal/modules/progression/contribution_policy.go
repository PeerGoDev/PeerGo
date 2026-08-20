package progression

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	MaximumContributionPolicyListLimit = 100
	maximumContributionExperienceMilli = int64(1_000_000_000)
	maximumContributionPolicyRevision  = 55
	minimumContributionPolicyLeadTime  = time.Hour
)

var (
	ErrContributionPolicyInput    = errors.New("contribution experience policy input is invalid")
	ErrContributionPolicyConflict = errors.New("contribution experience policy timeline changed")
	contributionRevisionPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
)

// ContributionExperiencePolicy contains the three experience sources that do
// not already own a settlement policy. Seeding experience remains coupled to
// the signed seeding formula, and attendance experience remains coupled to the
// signed attendance receipt.
type ContributionExperiencePolicy struct {
	Revision                     string
	EffectiveFrom                time.Time
	ExperiencePerUploadGiBMilli  int64
	ExperiencePerTorrentMilli    int64
	ExperiencePerAccountDayMilli int64
	SnapshotSHA256               [sha256.Size]byte
	IssuedBy                     *uuid.UUID
	Reason                       string
	CreatedAt                    time.Time
}

type ContributionExperiencePolicyInput struct {
	Revision                     string
	EffectiveFrom                time.Time
	ExperiencePerUploadGiBMilli  int64
	ExperiencePerTorrentMilli    int64
	ExperiencePerAccountDayMilli int64
}

type ContributionExperiencePolicyPage struct {
	Items                []ContributionExperiencePolicy
	Total                int64
	Limit                int
	Offset               int
	MinimumEffectiveFrom time.Time
}

type contributionExperiencePolicyDocument struct {
	Revision                     string `json:"revision"`
	EffectiveFrom                string `json:"effective_from"`
	ExperiencePerUploadGiBMilli  int64  `json:"experience_per_upload_gib_milli"`
	ExperiencePerTorrentMilli    int64  `json:"experience_per_torrent_milli"`
	ExperiencePerAccountDayMilli int64  `json:"experience_per_account_day_milli"`
}

type contributionExperiencePolicyCommand struct {
	Policy                  ContributionExperiencePolicy
	SnapshotJSON            []byte
	AuthorizationDecisionID uuid.UUID
}

type ContributionExperiencePolicyRepository interface {
	ListContributionExperiencePolicies(context.Context, int, int) ([]ContributionExperiencePolicy, int64, error)
	IssueContributionExperiencePolicy(context.Context, contributionExperiencePolicyCommand) (ContributionExperiencePolicy, error)
}

type ContributionExperiencePolicyService struct {
	repository ContributionExperiencePolicyRepository
	authorizer authz.Authorizer
	now        func() time.Time
}

func NewContributionExperiencePolicyService(repository ContributionExperiencePolicyRepository, authorizer authz.Authorizer, now func() time.Time) (*ContributionExperiencePolicyService, error) {
	if repository == nil || authorizer == nil {
		return nil, errors.New("contribution experience policy dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &ContributionExperiencePolicyService{repository: repository, authorizer: authorizer, now: now}, nil
}

func (service *ContributionExperiencePolicyService) List(ctx context.Context, actor authz.StaffActor, limit, offset int) (ContributionExperiencePolicyPage, error) {
	if limit < 1 || limit > MaximumContributionPolicyListLimit || offset < 0 || offset > 1_000_000 {
		return ContributionExperiencePolicyPage{}, ErrContributionPolicyInput
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionProgressionContributionPolicyRead, authz.SiteScope(), now, "contribution-experience-policy-read"); err != nil {
		return ContributionExperiencePolicyPage{}, err
	}
	items, total, err := service.repository.ListContributionExperiencePolicies(ctx, limit, offset)
	if err != nil {
		return ContributionExperiencePolicyPage{}, err
	}
	return ContributionExperiencePolicyPage{
		Items: items, Total: total, Limit: limit, Offset: offset,
		MinimumEffectiveFrom: minimumContributionPolicyEffectiveFrom(now),
	}, nil
}

func (service *ContributionExperiencePolicyService) Issue(ctx context.Context, actor authz.StaffActor, input ContributionExperiencePolicyInput, reason string) (ContributionExperiencePolicy, error) {
	now := service.now().UTC().Truncate(time.Microsecond)
	reason = strings.TrimSpace(reason)
	policy := ContributionExperiencePolicy{
		Revision: input.Revision, EffectiveFrom: input.EffectiveFrom,
		ExperiencePerUploadGiBMilli:  input.ExperiencePerUploadGiBMilli,
		ExperiencePerTorrentMilli:    input.ExperiencePerTorrentMilli,
		ExperiencePerAccountDayMilli: input.ExperiencePerAccountDayMilli,
		Reason:                       reason, CreatedAt: now,
	}
	if actor.Subject.ID == uuid.Nil || actor.Subject.Status != authz.SubjectActive ||
		!policy.EffectiveFrom.UTC().Equal(input.EffectiveFrom) ||
		policy.EffectiveFrom.Before(minimumContributionPolicyEffectiveFrom(now)) ||
		!utf8.ValidString(reason) || utf8.RuneCountInString(reason) < 10 || utf8.RuneCountInString(reason) > 1000 {
		return ContributionExperiencePolicy{}, ErrContributionPolicyInput
	}
	normalized, snapshot, err := normalizeContributionExperiencePolicy(policy)
	if err != nil {
		return ContributionExperiencePolicy{}, err
	}
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionProgressionContributionPolicyIssue, authz.SiteScope(), now, "contribution-experience-policy-issue")
	if err != nil {
		return ContributionExperiencePolicy{}, err
	}
	issuer := actor.Subject.ID
	normalized.IssuedBy = &issuer
	return service.repository.IssueContributionExperiencePolicy(ctx, contributionExperiencePolicyCommand{
		Policy: normalized, SnapshotJSON: snapshot, AuthorizationDecisionID: decision.ID,
	})
}

func minimumContributionPolicyEffectiveFrom(now time.Time) time.Time {
	return now.UTC().Add(minimumContributionPolicyLeadTime).Truncate(time.Hour).Add(time.Hour)
}

func normalizeContributionExperiencePolicy(policy ContributionExperiencePolicy) (ContributionExperiencePolicy, []byte, error) {
	policy.Revision = strings.TrimSpace(policy.Revision)
	policy.EffectiveFrom = policy.EffectiveFrom.UTC().Truncate(time.Microsecond)
	policy.CreatedAt = policy.CreatedAt.UTC().Truncate(time.Microsecond)
	if !contributionRevisionPattern.MatchString(policy.Revision) || len(policy.Revision) > maximumContributionPolicyRevision || policy.EffectiveFrom.IsZero() ||
		policy.EffectiveFrom.Unix()%3600 != 0 || policy.CreatedAt.IsZero() || !policy.CreatedAt.Before(policy.EffectiveFrom) ||
		!validContributionExperienceMilli(policy.ExperiencePerUploadGiBMilli) ||
		!validContributionExperienceMilli(policy.ExperiencePerTorrentMilli) ||
		!validContributionExperienceMilli(policy.ExperiencePerAccountDayMilli) {
		return ContributionExperiencePolicy{}, nil, ErrContributionPolicyInput
	}
	document := contributionExperiencePolicyDocument{
		Revision: policy.Revision, EffectiveFrom: policy.EffectiveFrom.Format(time.RFC3339Nano),
		ExperiencePerUploadGiBMilli:  policy.ExperiencePerUploadGiBMilli,
		ExperiencePerTorrentMilli:    policy.ExperiencePerTorrentMilli,
		ExperiencePerAccountDayMilli: policy.ExperiencePerAccountDayMilli,
	}
	snapshot, err := json.Marshal(document)
	if err != nil {
		return ContributionExperiencePolicy{}, nil, fmt.Errorf("encode contribution experience policy: %w", err)
	}
	digest := sha256.Sum256(snapshot)
	if policy.SnapshotSHA256 != ([sha256.Size]byte{}) && policy.SnapshotSHA256 != digest {
		return ContributionExperiencePolicy{}, nil, ErrContributionPolicyConflict
	}
	policy.SnapshotSHA256 = digest
	return policy, snapshot, nil
}

func validContributionExperienceMilli(value int64) bool {
	return value >= 0 && value <= maximumContributionExperienceMilli
}

type PostgresContributionExperiencePolicyRepository struct{ pool *pgxpool.Pool }

func NewPostgresContributionExperiencePolicyRepository(pool *pgxpool.Pool) (*PostgresContributionExperiencePolicyRepository, error) {
	if pool == nil {
		return nil, errors.New("contribution experience policy pool is required")
	}
	return &PostgresContributionExperiencePolicyRepository{pool: pool}, nil
}

func (repository *PostgresContributionExperiencePolicyRepository) ListContributionExperiencePolicies(ctx context.Context, limit, offset int) ([]ContributionExperiencePolicy, int64, error) {
	if limit < 1 || limit > MaximumContributionPolicyListLimit || offset < 0 || offset > 1_000_000 {
		return nil, 0, ErrContributionPolicyInput
	}
	rows, err := repository.pool.Query(ctx, `
SELECT revision, effective_from,
       experience_per_upload_gib_milli, experience_per_torrent_milli,
       experience_per_account_day_milli, snapshot_json, snapshot_sha256,
       issued_by, authorization_decision_id, reason, created_at
FROM progression.contribution_experience_policy_revisions
ORDER BY effective_from DESC, revision DESC
LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list contribution experience policies: %w", err)
	}
	defer rows.Close()
	items := make([]ContributionExperiencePolicy, 0, limit)
	for rows.Next() {
		item, err := scanContributionExperiencePolicy(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("finish contribution experience policies: %w", err)
	}
	var total int64
	if err := repository.pool.QueryRow(ctx, `SELECT count(*) FROM progression.contribution_experience_policy_revisions`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count contribution experience policies: %w", err)
	}
	return items, total, nil
}

func (repository *PostgresContributionExperiencePolicyRepository) IssueContributionExperiencePolicy(ctx context.Context, command contributionExperiencePolicyCommand) (ContributionExperiencePolicy, error) {
	policy, snapshot, err := normalizeContributionExperiencePolicy(command.Policy)
	if err != nil || policy.IssuedBy == nil || *policy.IssuedBy == uuid.Nil || command.AuthorizationDecisionID == uuid.Nil ||
		!bytes.Equal(snapshot, command.SnapshotJSON) || strings.TrimSpace(policy.Reason) != policy.Reason {
		return ContributionExperiencePolicy{}, ErrContributionPolicyInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ContributionExperiencePolicy{}, fmt.Errorf("begin contribution experience policy issue: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('peergo-contribution-experience-policy-v1', 0))`); err != nil {
		return ContributionExperiencePolicy{}, fmt.Errorf("lock contribution experience policy timeline: %w", err)
	}
	var latest time.Time
	err = tx.QueryRow(ctx, `SELECT effective_from FROM progression.contribution_experience_policy_revisions ORDER BY effective_from DESC LIMIT 1`).Scan(&latest)
	if err == nil && !policy.EffectiveFrom.After(latest) {
		return ContributionExperiencePolicy{}, ErrContributionPolicyConflict
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ContributionExperiencePolicy{}, fmt.Errorf("read contribution experience policy timeline: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO progression.contribution_experience_policy_revisions (
    revision, effective_from,
    experience_per_upload_gib_milli, experience_per_torrent_milli,
    experience_per_account_day_milli, snapshot_json, snapshot_sha256,
    issued_by, authorization_decision_id, reason, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		policy.Revision, policy.EffectiveFrom,
		policy.ExperiencePerUploadGiBMilli, policy.ExperiencePerTorrentMilli,
		policy.ExperiencePerAccountDayMilli, string(snapshot), policy.SnapshotSHA256[:],
		*policy.IssuedBy, command.AuthorizationDecisionID, policy.Reason, policy.CreatedAt)
	if err != nil {
		return ContributionExperiencePolicy{}, classifyContributionPolicyDatabaseError("insert contribution experience policy", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ContributionExperiencePolicy{}, classifyContributionPolicyDatabaseError("commit contribution experience policy", err)
	}
	return policy, nil
}

type contributionPolicyScanner interface{ Scan(...any) error }

func scanContributionExperiencePolicy(scanner contributionPolicyScanner) (ContributionExperiencePolicy, error) {
	var policy ContributionExperiencePolicy
	var snapshotJSON string
	var snapshotSHA256 []byte
	var issuedBy pgtype.UUID
	var authorizationDecisionID pgtype.UUID
	if err := scanner.Scan(
		&policy.Revision, &policy.EffectiveFrom,
		&policy.ExperiencePerUploadGiBMilli, &policy.ExperiencePerTorrentMilli,
		&policy.ExperiencePerAccountDayMilli, &snapshotJSON, &snapshotSHA256,
		&issuedBy, &authorizationDecisionID, &policy.Reason, &policy.CreatedAt,
	); err != nil {
		return ContributionExperiencePolicy{}, err
	}
	if len(snapshotSHA256) != sha256.Size || issuedBy.Valid != authorizationDecisionID.Valid {
		return ContributionExperiencePolicy{}, ErrInvariant
	}
	copy(policy.SnapshotSHA256[:], snapshotSHA256)
	if issuedBy.Valid {
		value := uuid.UUID(issuedBy.Bytes)
		policy.IssuedBy = &value
	}
	normalized, snapshot, err := normalizeContributionExperiencePolicy(policy)
	if err != nil || !bytes.Equal(snapshot, []byte(snapshotJSON)) {
		return ContributionExperiencePolicy{}, ErrInvariant
	}
	return normalized, nil
}

func classifyContributionPolicyDatabaseError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505", "P0001":
			return fmt.Errorf("%w: %s", ErrContributionPolicyConflict, operation)
		case "23503", "23514", "22003":
			return fmt.Errorf("%w: %s", ErrContributionPolicyInput, operation)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ ContributionExperiencePolicyRepository = (*PostgresContributionExperiencePolicyRepository)(nil)
