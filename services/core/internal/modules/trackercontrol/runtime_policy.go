package trackercontrol

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/contracts/go/signedsnapshotv1"
	"github.com/peergo/peergo/contracts/go/trackerruntimepolicyv1"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

var (
	ErrRuntimePolicyInput           = errors.New("Tracker runtime policy input is invalid")
	ErrRuntimePolicyNotFound        = errors.New("Tracker runtime policy was not found")
	ErrRuntimePolicyVersionConflict = errors.New("Tracker runtime policy version changed")
	ErrRuntimePolicyIdempotency     = errors.New("Tracker runtime policy idempotency conflict")
)

const (
	minRuntimePolicyReasonRunes = 5
	maxRuntimePolicyReasonRunes = 1000
)

type RuntimePolicyRevision struct {
	Sequence  int64
	Policy    trackerruntimepolicyv1.Policy
	IssuedBy  *uuid.UUID
	Reason    string
	CreatedAt time.Time
}

type IssueRuntimePolicyInput struct {
	RequestID        uuid.UUID
	ExpectedSequence int64
	Policy           trackerruntimepolicyv1.Policy
	Reason           string
}

type issueRuntimePolicyCommand struct {
	IssueRuntimePolicyInput
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type RuntimePolicyRepository interface {
	LatestRuntimePolicy(context.Context) (RuntimePolicyRevision, error)
	IssueRuntimePolicy(context.Context, issueRuntimePolicyCommand) (RuntimePolicyRevision, error)
}

type RuntimePolicyService struct {
	repository RuntimePolicyRepository
	authorizer authz.Authorizer
	now        func() time.Time
}

func NewRuntimePolicyService(repository RuntimePolicyRepository, authorizer authz.Authorizer, now func() time.Time) (*RuntimePolicyService, error) {
	if repository == nil || authorizer == nil {
		return nil, errors.New("Tracker runtime policy dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &RuntimePolicyService{repository: repository, authorizer: authorizer, now: now}, nil
}

func (service *RuntimePolicyService) Current(ctx context.Context, actor authz.StaffActor) (RuntimePolicyRevision, error) {
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTrackerPolicyRead, authz.SiteScope(), now, "tracker-runtime-policy-read"); err != nil {
		return RuntimePolicyRevision{}, err
	}
	return service.repository.LatestRuntimePolicy(ctx)
}

func (service *RuntimePolicyService) Issue(ctx context.Context, actor authz.StaffActor, input IssueRuntimePolicyInput) (RuntimePolicyRevision, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if input.RequestID == uuid.Nil || input.ExpectedSequence < 1 || !utf8.ValidString(input.Reason) ||
		utf8.RuneCountInString(input.Reason) < minRuntimePolicyReasonRunes ||
		utf8.RuneCountInString(input.Reason) > maxRuntimePolicyReasonRunes {
		return RuntimePolicyRevision{}, ErrRuntimePolicyInput
	}
	// Revision is a server-owned idempotency identity derived from the write
	// request. API clients intentionally cannot submit it, so assign it before
	// validating the complete policy rather than requiring an impossible DTO
	// field from the staff console.
	input.Policy.Revision = "tracker-runtime-" + strings.ReplaceAll(input.RequestID.String(), "-", "")
	policy, err := trackerruntimepolicyv1.NormalizePolicy(input.Policy)
	if err != nil {
		return RuntimePolicyRevision{}, ErrRuntimePolicyInput
	}
	input.Policy = policy
	now := service.now().UTC().Round(0)
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTrackerPolicyIssue, authz.SiteScope(), now, "tracker-runtime-policy-issue")
	if err != nil {
		return RuntimePolicyRevision{}, err
	}
	return service.repository.IssueRuntimePolicy(ctx, issueRuntimePolicyCommand{
		IssueRuntimePolicyInput: input, ActorID: actor.Subject.ID,
		OccurredAt: now, Authorization: decision,
	})
}

type PostgresRuntimePolicyRepository struct{ pool *pgxpool.Pool }

func NewPostgresRuntimePolicyRepository(pool *pgxpool.Pool) (*PostgresRuntimePolicyRepository, error) {
	if pool == nil {
		return nil, errors.New("Tracker runtime policy repository pool is required")
	}
	return &PostgresRuntimePolicyRepository{pool: pool}, nil
}

func (repository *PostgresRuntimePolicyRepository) LatestRuntimePolicy(ctx context.Context) (RuntimePolicyRevision, error) {
	return readRuntimePolicyRow(repository.pool.QueryRow(ctx, runtimePolicySelect+` ORDER BY sequence DESC LIMIT 1`))
}

func (repository *PostgresRuntimePolicyRepository) IssueRuntimePolicy(ctx context.Context, command issueRuntimePolicyCommand) (RuntimePolicyRevision, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return RuntimePolicyRevision{}, fmt.Errorf("begin Tracker runtime policy issue: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// The advisory lock serializes the append point. Row locking alone cannot
	// protect an empty timeline and is unnecessarily subtle around concurrent
	// INSERT statements.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('peergo-tracker-runtime-policy-v1', 0))`); err != nil {
		return RuntimePolicyRevision{}, fmt.Errorf("lock Tracker runtime policy timeline: %w", err)
	}
	issued, err := issueRuntimePolicyTx(ctx, tx, command)
	if err != nil {
		return RuntimePolicyRevision{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RuntimePolicyRevision{}, fmt.Errorf("commit Tracker runtime policy: %w", err)
	}
	return issued, nil
}

// issueRuntimePolicyTx appends one normalized policy while the caller holds
// the shared timeline advisory lock. Seedbox approval uses this same primitive
// so the report decision and its user-bound Tracker rule commit atomically.
func issueRuntimePolicyTx(ctx context.Context, tx pgx.Tx, command issueRuntimePolicyCommand) (RuntimePolicyRevision, error) {
	existing, err := readRuntimePolicyRow(tx.QueryRow(ctx, runtimePolicySelect+` WHERE revision = $1`, command.Policy.Revision))
	if err == nil {
		if runtimePoliciesEqual(existing.Policy, command.Policy) && existing.Reason == command.Reason {
			return existing, nil
		}
		return RuntimePolicyRevision{}, ErrRuntimePolicyIdempotency
	}
	if !errors.Is(err, ErrRuntimePolicyNotFound) {
		return RuntimePolicyRevision{}, err
	}
	latest, err := readRuntimePolicyRow(tx.QueryRow(ctx, runtimePolicySelect+` ORDER BY sequence DESC LIMIT 1`))
	if err != nil {
		return RuntimePolicyRevision{}, err
	}
	if latest.Sequence != command.ExpectedSequence {
		return RuntimePolicyRevision{}, ErrRuntimePolicyVersionConflict
	}
	createdAt := command.OccurredAt.UTC().Round(0)
	if !createdAt.After(latest.CreatedAt) {
		createdAt = latest.CreatedAt.Add(time.Microsecond)
	}
	clients, err := runtimePolicyClientsJSON(command.Policy.AllowedClients)
	if err != nil {
		return RuntimePolicyRevision{}, ErrRuntimePolicyInput
	}
	seedboxPolicy, err := runtimeSeedboxPolicyJSON(command.Policy.Seedbox)
	if err != nil {
		return RuntimePolicyRevision{}, ErrRuntimePolicyInput
	}
	row := tx.QueryRow(ctx, `INSERT INTO tracker_control.runtime_policy_revisions (
			revision, announce_interval_seconds, min_announce_interval_seconds,
			default_numwant, max_numwant, scrape_enabled, max_scrape_hashes,
			client_mode, allowed_clients, user_requests_per_minute, user_burst,
			address_requests_per_minute, address_burst, seedbox_policy, issued_by,
			authorization_decision_id, reason, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,$12,$13,$14::jsonb,$15,$16,$17,$18)
		RETURNING `+runtimePolicyColumns,
		command.Policy.Revision, command.Policy.AnnounceIntervalSeconds,
		command.Policy.MinAnnounceIntervalSeconds, command.Policy.DefaultNumWant,
		command.Policy.MaxNumWant, command.Policy.ScrapeEnabled, command.Policy.MaxScrapeHashes,
		string(command.Policy.ClientMode), clients, command.Policy.UserRequestsPerMinute,
		command.Policy.UserBurst, command.Policy.AddressRequestsPerMinute, command.Policy.AddressBurst, seedboxPolicy,
		command.ActorID, command.Authorization.ID, command.Reason, createdAt,
	)
	issued, err := readRuntimePolicyRow(row)
	if err != nil {
		return RuntimePolicyRevision{}, fmt.Errorf("insert Tracker runtime policy: %w", err)
	}
	return issued, nil
}

const runtimePolicyColumns = `sequence, revision, announce_interval_seconds,
	min_announce_interval_seconds, default_numwant, max_numwant,
	scrape_enabled, max_scrape_hashes, client_mode, allowed_clients,
	user_requests_per_minute, user_burst, address_requests_per_minute,
	address_burst, seedbox_policy, issued_by, reason, created_at`

const runtimePolicySelect = `SELECT ` + runtimePolicyColumns + ` FROM tracker_control.runtime_policy_revisions`

type runtimePolicyRow interface{ Scan(...any) error }

func readRuntimePolicyRow(row runtimePolicyRow) (RuntimePolicyRevision, error) {
	var revision RuntimePolicyRevision
	var familyMode string
	var clients []byte
	var seedboxPolicy []byte
	var issuedBy pgtype.UUID
	err := row.Scan(
		&revision.Sequence, &revision.Policy.Revision,
		&revision.Policy.AnnounceIntervalSeconds, &revision.Policy.MinAnnounceIntervalSeconds,
		&revision.Policy.DefaultNumWant, &revision.Policy.MaxNumWant,
		&revision.Policy.ScrapeEnabled, &revision.Policy.MaxScrapeHashes,
		&familyMode, &clients, &revision.Policy.UserRequestsPerMinute,
		&revision.Policy.UserBurst, &revision.Policy.AddressRequestsPerMinute,
		&revision.Policy.AddressBurst, &seedboxPolicy, &issuedBy, &revision.Reason, &revision.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return RuntimePolicyRevision{}, ErrRuntimePolicyNotFound
	}
	if err != nil {
		return RuntimePolicyRevision{}, fmt.Errorf("read Tracker runtime policy: %w", err)
	}
	revision.Policy.ClientMode = trackerruntimepolicyv1.ClientMode(familyMode)
	if err := decodeRuntimePolicyClients(clients, &revision.Policy.AllowedClients); err != nil {
		return RuntimePolicyRevision{}, err
	}
	if err := decodeRuntimeSeedboxPolicy(seedboxPolicy, &revision.Policy.Seedbox); err != nil {
		return RuntimePolicyRevision{}, err
	}
	normalized, err := trackerruntimepolicyv1.NormalizePolicy(revision.Policy)
	if err != nil || revision.Sequence < 1 || revision.CreatedAt.IsZero() {
		return RuntimePolicyRevision{}, errors.New("persisted Tracker runtime policy is invalid")
	}
	revision.Policy = normalized
	revision.CreatedAt = revision.CreatedAt.UTC()
	if issuedBy.Valid {
		value, err := uuid.FromBytes(issuedBy.Bytes[:])
		if err != nil {
			return RuntimePolicyRevision{}, errors.New("persisted Tracker runtime policy issuer is invalid")
		}
		revision.IssuedBy = &value
	}
	return revision, nil
}

func runtimePoliciesEqual(left, right trackerruntimepolicyv1.Policy) bool {
	return left.Revision == right.Revision && left.AnnounceIntervalSeconds == right.AnnounceIntervalSeconds &&
		left.MinAnnounceIntervalSeconds == right.MinAnnounceIntervalSeconds && left.DefaultNumWant == right.DefaultNumWant &&
		left.MaxNumWant == right.MaxNumWant && left.ScrapeEnabled == right.ScrapeEnabled &&
		left.MaxScrapeHashes == right.MaxScrapeHashes && left.ClientMode == right.ClientMode &&
		slices.Equal(left.AllowedClients, right.AllowedClients) && left.UserRequestsPerMinute == right.UserRequestsPerMinute &&
		left.UserBurst == right.UserBurst && left.AddressRequestsPerMinute == right.AddressRequestsPerMinute &&
		left.AddressBurst == right.AddressBurst && seedboxPoliciesEqual(left.Seedbox, right.Seedbox)
}

func runtimePolicyClientsJSON(clients []trackerruntimepolicyv1.ClientRule) ([]byte, error) {
	return jsonMarshal(clients)
}

func decodeRuntimePolicyClients(encoded []byte, destination *[]trackerruntimepolicyv1.ClientRule) error {
	if err := jsonStrict(encoded, destination); err != nil {
		return errors.New("persisted Tracker client policy is invalid")
	}
	return nil
}

func runtimeSeedboxPolicyJSON(policy trackerruntimepolicyv1.SeedboxPolicy) ([]byte, error) {
	return jsonMarshal(policy)
}

func decodeRuntimeSeedboxPolicy(encoded []byte, destination *trackerruntimepolicyv1.SeedboxPolicy) error {
	// Persisted revisions are append-only. Revisions written before the
	// download factor was introduced omit it and retain neutral 1x semantics;
	// preinitializing the destination upgrades only the in-memory view.
	*destination = trackerruntimepolicyv1.SeedboxPolicy{DownloadFactorBasisPoints: 10_000}
	if err := jsonStrict(encoded, destination); err != nil {
		return errors.New("persisted Tracker seedbox policy is invalid")
	}
	return nil
}

func seedboxPoliciesEqual(left, right trackerruntimepolicyv1.SeedboxPolicy) bool {
	return left.Enabled == right.Enabled && left.UploadFactorBasisPoints == right.UploadFactorBasisPoints &&
		left.DownloadFactorBasisPoints == right.DownloadFactorBasisPoints &&
		left.SeedboxSpeedLimitBytesPerSecond == right.SeedboxSpeedLimitBytesPerSecond &&
		left.StandardSpeedLimitBytesPerSecond == right.StandardSpeedLimitBytesPerSecond &&
		slices.Equal(left.Rules, right.Rules)
}

// Small indirections keep JSON use focused and make the persisted codec easy
// to exercise without exposing database rows as transport DTOs.
var jsonMarshal = func(value any) ([]byte, error) { return json.Marshal(value) }
var jsonStrict = func(encoded []byte, value any) error { return signedsnapshotv1.StrictJSON(encoded, value) }

type RuntimePolicySnapshotPublisher interface {
	PublishRuntimePolicy(context.Context, trackerruntimepolicyv1.SignedArtifact) (SnapshotPublication, error)
}

type RuntimePolicySnapshotBuilder struct {
	source     RuntimePolicyRepository
	publisher  RuntimePolicySnapshotPublisher
	keyID      string
	privateKey ed25519.PrivateKey
	now        func() time.Time
}

func NewRuntimePolicySnapshotBuilder(source RuntimePolicyRepository, publisher RuntimePolicySnapshotPublisher, keyID string, privateKey ed25519.PrivateKey, now func() time.Time) (*RuntimePolicySnapshotBuilder, error) {
	if source == nil || publisher == nil || signedsnapshotv1.ValidateKeyID(keyID) != nil || len(privateKey) != ed25519.PrivateKeySize || now == nil {
		return nil, ErrSnapshotBuilderInput
	}
	return &RuntimePolicySnapshotBuilder{source: source, publisher: publisher, keyID: keyID, privateKey: append(ed25519.PrivateKey(nil), privateKey...), now: now}, nil
}

type RuntimePolicySnapshotBuildResult struct {
	ControlSequence int64
	Revision        string
	GeneratedAt     time.Time
	StateSHA256     string
	ArtifactSHA256  [sha256.Size]byte
	Published       bool
}

func (builder *RuntimePolicySnapshotBuilder) BuildAndPublish(ctx context.Context) (RuntimePolicySnapshotBuildResult, error) {
	revision, err := builder.source.LatestRuntimePolicy(ctx)
	if err != nil {
		return RuntimePolicySnapshotBuildResult{}, err
	}
	artifact, err := trackerruntimepolicyv1.Sign(trackerruntimepolicyv1.Snapshot{
		GeneratedAt: builder.now(), ControlSequence: revision.Sequence, Policy: revision.Policy,
	}, builder.keyID, builder.privateKey)
	if err != nil {
		return RuntimePolicySnapshotBuildResult{}, err
	}
	publication, err := builder.publisher.PublishRuntimePolicy(ctx, artifact)
	if err != nil {
		return RuntimePolicySnapshotBuildResult{}, err
	}
	return RuntimePolicySnapshotBuildResult{
		ControlSequence: artifact.Snapshot.ControlSequence, Revision: artifact.Snapshot.Policy.Revision,
		GeneratedAt: artifact.Snapshot.GeneratedAt, StateSHA256: artifact.Snapshot.StateSHA256,
		ArtifactSHA256: artifact.ArtifactSHA256, Published: publication.Published,
	}, nil
}

var _ RuntimePolicyRepository = (*PostgresRuntimePolicyRepository)(nil)
