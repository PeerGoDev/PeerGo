package torrents

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	minimumUploadPolicyLeadTime = time.Minute
	defaultUploadPolicyListSize = 20
)

var defaultUploadPolicyID = uuid.MustParse("00000000-0000-0000-0000-000000000013")

var (
	ErrUploadPolicyInput           = errors.New("torrent upload policy input is invalid")
	ErrUploadPolicyNotFound        = errors.New("torrent upload policy was not found")
	ErrUploadPolicyVersionConflict = errors.New("torrent upload policy timeline changed")
	ErrUploadPolicyIdempotency     = errors.New("torrent upload policy request was reused")
)

// UploadPolicySettings are business admission limits. Parser and decoder
// constants remain the non-configurable safety ceiling, so normalizing a staff
// revision can only narrow what this Core build already accepts.
type UploadPolicySettings struct {
	MetainfoMaxBytes    int
	MaxFiles            int
	ScreenshotMaxCount  int
	ScreenshotMaxBytes  int
	ScreenshotMaxPixels int
	ScreenshotFormats   []string
}

type UploadPolicyRevision struct {
	ID          uuid.UUID
	Sequence    int64
	EffectiveAt time.Time
	Settings    UploadPolicySettings
	IssuedBy    *uuid.UUID
	Reason      string
	CreatedAt   time.Time
}

type UploadPolicyOverview struct {
	Active    UploadPolicyRevision
	Scheduled []UploadPolicyRevision
}

type IssueUploadPolicyInput struct {
	RequestID        uuid.UUID
	ExpectedSequence int64
	EffectiveAt      time.Time
	Settings         UploadPolicySettings
	Reason           string
}

type issueUploadPolicyCommand struct {
	IssueUploadPolicyInput
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type UploadPolicyRepository interface {
	EffectiveUploadPolicy(context.Context, time.Time) (UploadPolicyRevision, error)
	UploadPolicyByID(context.Context, uuid.UUID) (UploadPolicyRevision, error)
	UploadPolicyOverview(context.Context, time.Time, int) (UploadPolicyOverview, error)
	IssueUploadPolicy(context.Context, issueUploadPolicyCommand) (UploadPolicyRevision, error)
}

type UploadPolicyService struct {
	repository UploadPolicyRepository
	authorizer authz.Authorizer
	metainfoHardMax int
	now        func() time.Time
}

func NewUploadPolicyService(repository UploadPolicyRepository, authorizer authz.Authorizer, now func() time.Time, metainfoHardMax ...int) (*UploadPolicyService, error) {
	if repository == nil || authorizer == nil {
		return nil, errors.New("torrent upload policy dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	hardMax := MaxMetainfoBytes
	if len(metainfoHardMax) > 1 {
		return nil, errors.New("at most one torrent metainfo hard limit is allowed")
	}
	if len(metainfoHardMax) == 1 {
		hardMax = metainfoHardMax[0]
	}
	if hardMax < 64<<10 || hardMax > MaxMetainfoBytes {
		return nil, errors.New("torrent metainfo hard limit is invalid")
	}
	return &UploadPolicyService{repository: repository, authorizer: authorizer, metainfoHardMax: hardMax, now: now}, nil
}

// Effective is intentionally authorization-free for internal upload and
// attachment use cases. HTTP callers use Overview, which applies staff authz.
func (service *UploadPolicyService) Effective(ctx context.Context, at time.Time) (UploadPolicyRevision, error) {
	return service.repository.EffectiveUploadPolicy(ctx, at.UTC().Round(0))
}

func (service *UploadPolicyService) ByID(ctx context.Context, id uuid.UUID) (UploadPolicyRevision, error) {
	if id == uuid.Nil {
		return UploadPolicyRevision{}, ErrUploadPolicyInput
	}
	return service.repository.UploadPolicyByID(ctx, id)
}

func (service *UploadPolicyService) Overview(ctx context.Context, actor authz.StaffActor) (UploadPolicyOverview, error) {
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTorrentManageRead, authz.SiteScope(), now, "torrent-upload-policy-read"); err != nil {
		return UploadPolicyOverview{}, err
	}
	return service.repository.UploadPolicyOverview(ctx, now, defaultUploadPolicyListSize)
}

func (service *UploadPolicyService) Issue(ctx context.Context, actor authz.StaffActor, input IssueUploadPolicyInput) (UploadPolicyRevision, error) {
	now := service.now().UTC().Round(0)
	normalized, err := normalizeIssueUploadPolicyInput(input, now)
	if err != nil {
		return UploadPolicyRevision{}, err
	}
	if normalized.Settings.MetainfoMaxBytes > service.metainfoHardMax {
		return UploadPolicyRevision{}, ErrUploadPolicyInput
	}
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTorrentUploadPolicyIssue, authz.SiteScope(), now, "torrent-upload-policy-issue")
	if err != nil {
		return UploadPolicyRevision{}, err
	}
	return service.repository.IssueUploadPolicy(ctx, issueUploadPolicyCommand{
		IssueUploadPolicyInput: normalized,
		ActorID:                actor.Subject.ID, OccurredAt: now, Authorization: decision,
	})
}

func normalizeIssueUploadPolicyInput(input IssueUploadPolicyInput, now time.Time) (IssueUploadPolicyInput, error) {
	settings, err := normalizeUploadPolicySettings(input.Settings)
	input.Reason = strings.TrimSpace(input.Reason)
	input.EffectiveAt = input.EffectiveAt.UTC().Round(0)
	if err != nil || input.RequestID == uuid.Nil || input.ExpectedSequence < 1 ||
		input.EffectiveAt.Before(now.Add(minimumUploadPolicyLeadTime)) ||
		!utf8.ValidString(input.Reason) || utf8.RuneCountInString(input.Reason) < 10 || utf8.RuneCountInString(input.Reason) > 1000 {
		return IssueUploadPolicyInput{}, ErrUploadPolicyInput
	}
	input.Settings = settings
	return input, nil
}

func normalizeUploadPolicySettings(settings UploadPolicySettings) (UploadPolicySettings, error) {
	if settings.MetainfoMaxBytes < 64<<10 || settings.MetainfoMaxBytes > MaxMetainfoBytes ||
		settings.MaxFiles < 1 || settings.MaxFiles > MaxTorrentFiles ||
		settings.ScreenshotMaxCount < 0 || settings.ScreenshotMaxCount > MaxTorrentScreenshots ||
		settings.ScreenshotMaxBytes < 64<<10 || settings.ScreenshotMaxBytes > MaxTorrentScreenshotBytes ||
		settings.ScreenshotMaxPixels < 64<<10 || settings.ScreenshotMaxPixels > maxScreenshotPixels ||
		len(settings.ScreenshotFormats) < 1 || len(settings.ScreenshotFormats) > 3 {
		return UploadPolicySettings{}, ErrUploadPolicyInput
	}
	formats := append([]string(nil), settings.ScreenshotFormats...)
	for index := range formats {
		formats[index] = strings.ToLower(strings.TrimSpace(formats[index]))
		if formats[index] != "jpeg" && formats[index] != "png" && formats[index] != "webp" {
			return UploadPolicySettings{}, ErrUploadPolicyInput
		}
	}
	sort.Strings(formats)
	formats = slices.Compact(formats)
	if len(formats) != len(settings.ScreenshotFormats) {
		return UploadPolicySettings{}, ErrUploadPolicyInput
	}
	settings.ScreenshotFormats = formats
	return settings, nil
}

func (policy UploadPolicyRevision) validate() error {
	settings, err := normalizeUploadPolicySettings(policy.Settings)
	if err != nil || policy.ID == uuid.Nil || policy.Sequence < 1 || policy.EffectiveAt.IsZero() || policy.CreatedAt.IsZero() ||
		settings.MetainfoMaxBytes != policy.Settings.MetainfoMaxBytes || !slices.Equal(settings.ScreenshotFormats, policy.Settings.ScreenshotFormats) {
		return ErrUploadPolicyInput
	}
	return nil
}

func validateUploadAgainstPolicy(policy UploadPolicyRevision, rawMetainfo []byte, metainfo ParsedMetainfo, screenshots []preparedTorrentScreenshot) error {
	if policy.validate() != nil {
		return ErrUploadPolicyInput
	}
	if len(rawMetainfo) > policy.Settings.MetainfoMaxBytes {
		return validationFailure(CodeObjectTooLarge, "metainfo", 0, "object exceeds the effective upload policy")
	}
	if len(metainfo.Files) > policy.Settings.MaxFiles {
		return validationFailure(CodeResourceLimit, "files", 0, "file tree exceeds the effective upload policy")
	}
	return validateScreenshotsAgainstPolicy(policy, screenshots)
}

func validateScreenshotsAgainstPolicy(policy UploadPolicyRevision, screenshots []preparedTorrentScreenshot) error {
	if policy.validate() != nil {
		return ErrUploadPolicyInput
	}
	if len(screenshots) > policy.Settings.ScreenshotMaxCount {
		return validationFailure(CodeInvalidScreenshot, "screenshots", 0, "screenshot count exceeds the effective upload policy")
	}
	for position, screenshot := range screenshots {
		if screenshot.ByteLength > int64(policy.Settings.ScreenshotMaxBytes) || screenshot.Width*screenshot.Height > policy.Settings.ScreenshotMaxPixels {
			return validationFailure(CodeObjectTooLarge, "screenshots", position, "screenshot exceeds the effective upload policy")
		}
		_, format, ok := supportedScreenshotType(screenshot.ContentType)
		if !ok || !slices.Contains(policy.Settings.ScreenshotFormats, format) {
			return validationFailure(CodeInvalidScreenshot, "screenshots", position, "screenshot format is disabled by the effective upload policy")
		}
	}
	return nil
}

func defaultUploadPolicyRevision(maxMetainfoBytes int) UploadPolicyRevision {
	if maxMetainfoBytes < 1 || maxMetainfoBytes > MaxMetainfoBytes {
		maxMetainfoBytes = MaxMetainfoBytes
	}
	baseline := time.Unix(0, 0).UTC()
	return UploadPolicyRevision{
		ID: defaultUploadPolicyID, Sequence: 1, EffectiveAt: baseline, CreatedAt: baseline,
		Reason: "PeerGo 首版新上传与截图安全基线。",
		Settings: UploadPolicySettings{
			MetainfoMaxBytes: maxMetainfoBytes, MaxFiles: MaxTorrentFiles,
			ScreenshotMaxCount: MaxTorrentScreenshots, ScreenshotMaxBytes: MaxTorrentScreenshotBytes,
			ScreenshotMaxPixels: maxScreenshotPixels, ScreenshotFormats: []string{"jpeg", "png", "webp"},
		},
	}
}

// DefaultUploadPolicyRevision exposes the build-time safety baseline to
// read-only operations composition while deployments migrate to the persisted
// policy timeline. New uploads still resolve a persisted revision in runtime.
func DefaultUploadPolicyRevision(maxMetainfoBytes int) UploadPolicyRevision {
	return defaultUploadPolicyRevision(maxMetainfoBytes)
}

type PostgresUploadPolicyRepository struct{ pool *pgxpool.Pool }

func NewPostgresUploadPolicyRepository(pool *pgxpool.Pool) (*PostgresUploadPolicyRepository, error) {
	if pool == nil {
		return nil, errors.New("torrent upload policy repository pool is required")
	}
	return &PostgresUploadPolicyRepository{pool: pool}, nil
}

func (repository *PostgresUploadPolicyRepository) EffectiveUploadPolicy(ctx context.Context, at time.Time) (UploadPolicyRevision, error) {
	return readUploadPolicyRow(repository.pool.QueryRow(ctx, uploadPolicySelect+` WHERE effective_at <= $1 ORDER BY effective_at DESC, sequence DESC LIMIT 1`, at))
}

func (repository *PostgresUploadPolicyRepository) UploadPolicyByID(ctx context.Context, id uuid.UUID) (UploadPolicyRevision, error) {
	return readUploadPolicyRow(repository.pool.QueryRow(ctx, uploadPolicySelect+` WHERE id=$1`, id))
}

func (repository *PostgresUploadPolicyRepository) UploadPolicyOverview(ctx context.Context, now time.Time, limit int) (UploadPolicyOverview, error) {
	active, err := repository.EffectiveUploadPolicy(ctx, now)
	if err != nil {
		return UploadPolicyOverview{}, err
	}
	rows, err := repository.pool.Query(ctx, uploadPolicySelect+` WHERE effective_at > $1 ORDER BY effective_at, sequence LIMIT $2`, now, limit)
	if err != nil {
		return UploadPolicyOverview{}, fmt.Errorf("query scheduled torrent upload policies: %w", err)
	}
	defer rows.Close()
	scheduled := make([]UploadPolicyRevision, 0)
	for rows.Next() {
		policy, err := readUploadPolicyRow(rows)
		if err != nil {
			return UploadPolicyOverview{}, err
		}
		scheduled = append(scheduled, policy)
	}
	if err := rows.Err(); err != nil {
		return UploadPolicyOverview{}, fmt.Errorf("iterate scheduled torrent upload policies: %w", err)
	}
	return UploadPolicyOverview{Active: active, Scheduled: scheduled}, nil
}

func (repository *PostgresUploadPolicyRepository) IssueUploadPolicy(ctx context.Context, command issueUploadPolicyCommand) (UploadPolicyRevision, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return UploadPolicyRevision{}, fmt.Errorf("begin torrent upload policy issue: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('peergo-torrent-upload-policy-v1', 0))`); err != nil {
		return UploadPolicyRevision{}, fmt.Errorf("lock torrent upload policy timeline: %w", err)
	}

	existing, err := readUploadPolicyRow(tx.QueryRow(ctx, uploadPolicySelect+` WHERE request_id=$1`, command.RequestID))
	if err == nil {
		if uploadPoliciesEqual(existing, command) {
			if err := tx.Commit(ctx); err != nil {
				return UploadPolicyRevision{}, fmt.Errorf("commit replayed torrent upload policy: %w", err)
			}
			return existing, nil
		}
		return UploadPolicyRevision{}, ErrUploadPolicyIdempotency
	}
	if !errors.Is(err, ErrUploadPolicyNotFound) {
		return UploadPolicyRevision{}, err
	}
	var latestSequence int64
	if err := tx.QueryRow(ctx, `SELECT sequence FROM torrents.torrent_upload_policy_revisions ORDER BY sequence DESC LIMIT 1`).Scan(&latestSequence); err != nil {
		return UploadPolicyRevision{}, fmt.Errorf("read latest torrent upload policy sequence: %w", err)
	}
	if latestSequence != command.ExpectedSequence {
		return UploadPolicyRevision{}, ErrUploadPolicyVersionConflict
	}
	policyID := uuid.New()
	policy, err := readUploadPolicyRow(tx.QueryRow(ctx, `INSERT INTO torrents.torrent_upload_policy_revisions (
		id, request_id, effective_at, metainfo_max_bytes, max_files,
		screenshot_max_count, screenshot_max_bytes, screenshot_max_pixels,
		screenshot_formats, issued_by, authorization_decision_id, reason, created_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING `+uploadPolicyColumns,
		policyID, command.RequestID, command.EffectiveAt, command.Settings.MetainfoMaxBytes, command.Settings.MaxFiles,
		command.Settings.ScreenshotMaxCount, command.Settings.ScreenshotMaxBytes, command.Settings.ScreenshotMaxPixels,
		command.Settings.ScreenshotFormats, command.ActorID, command.Authorization.ID, command.Reason, command.OccurredAt,
	))
	if err != nil {
		return UploadPolicyRevision{}, fmt.Errorf("insert torrent upload policy: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return UploadPolicyRevision{}, fmt.Errorf("commit torrent upload policy: %w", err)
	}
	return policy, nil
}

const uploadPolicyColumns = `id, sequence, effective_at, metainfo_max_bytes, max_files,
	screenshot_max_count, screenshot_max_bytes, screenshot_max_pixels,
	screenshot_formats, issued_by, reason, created_at`
const uploadPolicySelect = `SELECT ` + uploadPolicyColumns + ` FROM torrents.torrent_upload_policy_revisions`

type uploadPolicyRow interface{ Scan(...any) error }

func readUploadPolicyRow(row uploadPolicyRow) (UploadPolicyRevision, error) {
	var policy UploadPolicyRevision
	var issuedBy pgtype.UUID
	err := row.Scan(
		&policy.ID, &policy.Sequence, &policy.EffectiveAt,
		&policy.Settings.MetainfoMaxBytes, &policy.Settings.MaxFiles,
		&policy.Settings.ScreenshotMaxCount, &policy.Settings.ScreenshotMaxBytes,
		&policy.Settings.ScreenshotMaxPixels, &policy.Settings.ScreenshotFormats,
		&issuedBy, &policy.Reason, &policy.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return UploadPolicyRevision{}, ErrUploadPolicyNotFound
	}
	if err != nil {
		return UploadPolicyRevision{}, fmt.Errorf("read torrent upload policy: %w", err)
	}
	policy.EffectiveAt = policy.EffectiveAt.UTC()
	policy.CreatedAt = policy.CreatedAt.UTC()
	settings, err := normalizeUploadPolicySettings(policy.Settings)
	if err != nil {
		return UploadPolicyRevision{}, errors.New("persisted torrent upload policy is invalid")
	}
	policy.Settings = settings
	if policy.validate() != nil {
		return UploadPolicyRevision{}, errors.New("persisted torrent upload policy is invalid")
	}
	if issuedBy.Valid {
		value, err := uuid.FromBytes(issuedBy.Bytes[:])
		if err != nil {
			return UploadPolicyRevision{}, errors.New("persisted torrent upload policy issuer is invalid")
		}
		policy.IssuedBy = &value
	}
	return policy, nil
}

func uploadPoliciesEqual(existing UploadPolicyRevision, command issueUploadPolicyCommand) bool {
	return existing.EffectiveAt.Equal(command.EffectiveAt) &&
		existing.Settings.MetainfoMaxBytes == command.Settings.MetainfoMaxBytes &&
		existing.Settings.MaxFiles == command.Settings.MaxFiles &&
		existing.Settings.ScreenshotMaxCount == command.Settings.ScreenshotMaxCount &&
		existing.Settings.ScreenshotMaxBytes == command.Settings.ScreenshotMaxBytes &&
		existing.Settings.ScreenshotMaxPixels == command.Settings.ScreenshotMaxPixels &&
		slices.Equal(existing.Settings.ScreenshotFormats, command.Settings.ScreenshotFormats) &&
		existing.Reason == command.Reason && existing.IssuedBy != nil && *existing.IssuedBy == command.ActorID
}

var _ UploadPolicyRepository = (*PostgresUploadPolicyRepository)(nil)
