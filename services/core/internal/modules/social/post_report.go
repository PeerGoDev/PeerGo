package social

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

// MaxPostModerationNoteRunes matches the reason bound of
// social.post_management_events, which also records report-driven hides.
const MaxPostModerationNoteRunes = 500

var (
	ErrPostReportInput                   = errors.New("post report input is invalid")
	ErrPostReportTargetNotFound          = errors.New("visible post report target was not found")
	ErrPostReportSelf                    = errors.New("authors cannot report their own post")
	ErrPostAlreadyReported               = errors.New("reporter already reported this open post case")
	ErrPostReportIdempotencyConflict     = errors.New("post report idempotency key was reused")
	ErrPostModerationCaseNotFound        = errors.New("post moderation case was not found")
	ErrPostModerationCaseVersionConflict = errors.New("post moderation case version changed")
	ErrPostModerationCaseStateConflict   = errors.New("post moderation case is no longer open")
	ErrPostModerationPostVersionConflict = errors.New("moderated post version changed")
	ErrPostModerationConflictOfInterest  = errors.New("moderator has a conflict of interest with the post case")
	ErrPostModerationDecisionIdempotency = errors.New("post moderation decision idempotency key was reused")
	ErrPostModerationInvariant           = errors.New("post moderation projection violates persisted invariants")
)

type PostReportReasonCode string

const (
	PostReportSpam                PostReportReasonCode = "spam"
	PostReportHarassment          PostReportReasonCode = "harassment"
	PostReportPersonalInformation PostReportReasonCode = "personal_information"
	PostReportOffTopic            PostReportReasonCode = "off_topic"
	PostReportOther               PostReportReasonCode = "other"
)

type PostModerationCaseState string

const (
	PostModerationCaseOpen       PostModerationCaseState = "open"
	PostModerationCaseDismissed  PostModerationCaseState = "dismissed"
	PostModerationCasePostHidden PostModerationCaseState = "post_hidden"
)

type PostModerationDecision string

const (
	PostModerationDismiss  PostModerationDecision = "dismiss"
	PostModerationHidePost PostModerationDecision = "hide_post"
)

type PostModerationReasonCode string

const (
	PostModerationNoViolation         PostModerationReasonCode = "no_violation"
	PostModerationSpam                PostModerationReasonCode = "spam"
	PostModerationHarassment          PostModerationReasonCode = "harassment"
	PostModerationPersonalInformation PostModerationReasonCode = "personal_information"
	PostModerationOffTopic            PostModerationReasonCode = "off_topic"
	PostModerationOther               PostModerationReasonCode = "other"
)

type PostReportReceipt struct {
	ID         uuid.UUID
	PostID     uuid.UUID
	ReasonCode PostReportReasonCode
	CreatedAt  time.Time
}

// PostModerationReport excludes reporter identity for the same reason as
// comment moderation: reviewers need the allegation, not the accuser.
type PostModerationReport struct {
	ReasonCode PostReportReasonCode
	Details    string
	CreatedAt  time.Time
}

type PostModerationAuthor struct {
	ID          uuid.UUID
	Username    string
	DisplayName string
}

// PostModerationSubject is the bounded post snapshot shown to reviewers.
// Follow state, presence and medals stay in member-facing projections.
type PostModerationSubject struct {
	ID         uuid.UUID
	Author     PostModerationAuthor
	BoardID    string
	BoardName  string
	Body       string
	State      PostState
	Version    int64
	MediaCount int
	CreatedAt  time.Time
	EditedAt   *time.Time
}

type PostModerationCase struct {
	ID               uuid.UUID
	State            PostModerationCaseState
	Version          int64
	Post             PostModerationSubject
	ReportCount      int64
	Reports          []PostModerationReport
	OpenedAt         time.Time
	LatestReportedAt time.Time
}

type PostModerationCasePage struct {
	Items  []PostModerationCase
	Total  int64
	Limit  int
	Offset int
}

type CreatePostReportInput struct {
	RequestID  uuid.UUID
	PostID     uuid.UUID
	ReasonCode PostReportReasonCode
	Details    string
}

type DecidePostModerationCaseInput struct {
	DecisionID          uuid.UUID
	CaseID              uuid.UUID
	ExpectedCaseVersion int64
	ExpectedPostVersion int64
	Decision            PostModerationDecision
	ReasonCode          PostModerationReasonCode
	Note                string
}

type PostModerationDecisionResult struct {
	DecisionID  uuid.UUID
	CaseID      uuid.UUID
	PostID      uuid.UUID
	Decision    PostModerationDecision
	ReasonCode  PostModerationReasonCode
	CaseState   PostModerationCaseState
	PostState   PostState
	CaseVersion int64
	PostVersion int64
	DecidedAt   time.Time
}

type createPostReportCommand struct {
	ReportPublicID  uuid.UUID
	CasePublicID    uuid.UUID
	RequestID       uuid.UUID
	PostID          uuid.UUID
	ReporterID      uuid.UUID
	ReasonCode      PostReportReasonCode
	Details         string
	CreateInputHash [sha256.Size]byte
	CreatedAt       time.Time
}

type decidePostModerationCaseCommand struct {
	DecidePostModerationCaseInput
	ModeratorID   uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type PostModerationRepository interface {
	CreateReport(context.Context, createPostReportCommand) (PostReportReceipt, error)
	ListOpenCases(context.Context, int, int) (PostModerationCasePage, error)
	Decide(context.Context, decidePostModerationCaseCommand) (PostModerationDecisionResult, error)
}

// PostModerationService keeps member post reports and staff resolution in the
// social domain while preserving their separate credential audiences.
type PostModerationService struct {
	authenticator CommentSessionAuthenticator
	authorizer    authz.Authorizer
	repository    PostModerationRepository
	now           func() time.Time
}

func NewPostModerationService(
	authenticator CommentSessionAuthenticator,
	authorizer authz.Authorizer,
	repository PostModerationRepository,
	now func() time.Time,
) (*PostModerationService, error) {
	if authenticator == nil || authorizer == nil || repository == nil {
		return nil, errors.New("post moderation service dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &PostModerationService{
		authenticator: authenticator,
		authorizer:    authorizer,
		repository:    repository,
		now:           now,
	}, nil
}

func (service *PostModerationService) CreateReport(ctx context.Context, cookieToken, csrfToken string, input CreatePostReportInput) (PostReportReceipt, error) {
	details, err := normalizeModerationText(input.Details, 0, MaxReportDetailsRunes)
	if err != nil || input.RequestID == uuid.Nil || input.PostID == uuid.Nil || !validPostReportReason(input.ReasonCode) {
		return PostReportReceipt{}, ErrPostReportInput
	}
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return PostReportReceipt{}, err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionSocialPostReportCreateSelf, now); err != nil {
		return PostReportReceipt{}, err
	}
	receipt, err := service.repository.CreateReport(ctx, createPostReportCommand{
		ReportPublicID: uuid.New(), CasePublicID: uuid.New(), RequestID: input.RequestID,
		PostID: input.PostID, ReporterID: session.User.ID, ReasonCode: input.ReasonCode,
		Details: details, CreateInputHash: postReportInputHash(input.PostID, input.ReasonCode, details), CreatedAt: now,
	})
	if err != nil {
		return PostReportReceipt{}, err
	}
	if receipt.ID == uuid.Nil || receipt.PostID != input.PostID || receipt.ReasonCode != input.ReasonCode || receipt.CreatedAt.IsZero() {
		return PostReportReceipt{}, ErrPostModerationInvariant
	}
	return receipt, nil
}

func (service *PostModerationService) ListOpenCases(ctx context.Context, actor authz.StaffActor, limit, offset int) (PostModerationCasePage, error) {
	if limit < 1 || limit > MaxModerationCaseLimit || offset < 0 || offset > MaxModerationCaseOffset {
		return PostModerationCasePage{}, ErrPostReportInput
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionSocialReportRead, authz.SiteScope(), now, "post-moderation"); err != nil {
		return PostModerationCasePage{}, err
	}
	page, err := service.repository.ListOpenCases(ctx, limit, offset)
	if err != nil {
		return PostModerationCasePage{}, err
	}
	if page.Total < 0 || page.Limit != limit || page.Offset != offset || len(page.Items) > limit || (len(page.Items) > 0 && int64(offset+len(page.Items)) > page.Total) {
		return PostModerationCasePage{}, ErrPostModerationInvariant
	}
	for _, item := range page.Items {
		if validatePostModerationCase(item) != nil {
			return PostModerationCasePage{}, ErrPostModerationInvariant
		}
	}
	return page, nil
}

func (service *PostModerationService) Decide(ctx context.Context, actor authz.StaffActor, input DecidePostModerationCaseInput) (PostModerationDecisionResult, error) {
	normalized, err := normalizePostModerationDecisionInput(input)
	if err != nil {
		return PostModerationDecisionResult{}, err
	}
	now := service.now().UTC()
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionSocialReportResolve, authz.SiteScope(), now, "post-moderation")
	if err != nil {
		return PostModerationDecisionResult{}, err
	}
	result, err := service.repository.Decide(ctx, decidePostModerationCaseCommand{
		DecidePostModerationCaseInput: normalized,
		ModeratorID:                   actor.Subject.ID,
		OccurredAt:                    now,
		Authorization:                 decision,
	})
	if err != nil {
		return PostModerationDecisionResult{}, err
	}
	if result.DecisionID != input.DecisionID || result.CaseID != input.CaseID ||
		result.Decision != normalized.Decision || result.ReasonCode != normalized.ReasonCode ||
		result.CaseVersion != input.ExpectedCaseVersion+1 || result.PostID == uuid.Nil ||
		result.PostVersion < input.ExpectedPostVersion || result.DecidedAt.IsZero() {
		return PostModerationDecisionResult{}, ErrPostModerationInvariant
	}
	return result, nil
}

func normalizePostModerationDecisionInput(input DecidePostModerationCaseInput) (DecidePostModerationCaseInput, error) {
	note, err := normalizeModerationText(input.Note, MinModerationNoteRunes, MaxPostModerationNoteRunes)
	if err != nil || input.DecisionID == uuid.Nil || input.CaseID == uuid.Nil || input.ExpectedCaseVersion < 1 || input.ExpectedPostVersion < 1 {
		return DecidePostModerationCaseInput{}, ErrPostReportInput
	}
	input.Note = note
	switch input.Decision {
	case PostModerationDismiss:
		if input.ReasonCode != PostModerationNoViolation {
			return DecidePostModerationCaseInput{}, ErrPostReportInput
		}
	case PostModerationHidePost:
		if !validPostModerationViolationReason(input.ReasonCode) {
			return DecidePostModerationCaseInput{}, ErrPostReportInput
		}
	default:
		return DecidePostModerationCaseInput{}, ErrPostReportInput
	}
	return input, nil
}

func validatePostModerationCase(item PostModerationCase) error {
	if item.ID == uuid.Nil || item.State != PostModerationCaseOpen || item.Version < 1 ||
		validatePostModerationSubject(item.Post) != nil || len(item.Reports) < 1 ||
		len(item.Reports) > MaxModerationReportsPerCase || item.ReportCount < int64(len(item.Reports)) ||
		item.OpenedAt.IsZero() || item.LatestReportedAt.Before(item.OpenedAt) {
		return ErrPostModerationInvariant
	}
	latestReportedAt := item.OpenedAt
	for _, report := range item.Reports {
		if !validPostReportReason(report.ReasonCode) || report.CreatedAt.IsZero() ||
			report.CreatedAt.Before(item.OpenedAt) || report.CreatedAt.After(item.LatestReportedAt) {
			return ErrPostModerationInvariant
		}
		if normalized, err := normalizeModerationText(report.Details, 0, MaxReportDetailsRunes); err != nil || normalized != report.Details {
			return ErrPostModerationInvariant
		}
		if report.CreatedAt.After(latestReportedAt) {
			latestReportedAt = report.CreatedAt
		}
	}
	if !latestReportedAt.Equal(item.LatestReportedAt) {
		return ErrPostModerationInvariant
	}
	return nil
}

func validatePostModerationSubject(post PostModerationSubject) error {
	if post.ID == uuid.Nil || post.Author.ID == uuid.Nil || strings.TrimSpace(post.Author.Username) == "" ||
		strings.TrimSpace(post.Author.DisplayName) == "" || !validBoardID(post.BoardID) ||
		strings.TrimSpace(post.BoardName) == "" || post.Version < 1 || post.MediaCount < 0 ||
		post.CreatedAt.IsZero() || !utf8.ValidString(post.Body) || utf8.RuneCountInString(post.Body) > MaxPostBodyRunes {
		return ErrPostModerationInvariant
	}
	switch post.State {
	case PostVisible, PostModeratorHidden:
	case PostAuthorDeleted:
		if post.Body != "" {
			return ErrPostModerationInvariant
		}
	default:
		return ErrPostModerationInvariant
	}
	return nil
}

func validPostReportReason(value PostReportReasonCode) bool {
	switch value {
	case PostReportSpam, PostReportHarassment, PostReportPersonalInformation, PostReportOffTopic, PostReportOther:
		return true
	default:
		return false
	}
}

func validPostModerationViolationReason(value PostModerationReasonCode) bool {
	switch value {
	case PostModerationSpam, PostModerationHarassment, PostModerationPersonalInformation, PostModerationOffTopic, PostModerationOther:
		return true
	default:
		return false
	}
}

func postReportInputHash(postID uuid.UUID, reason PostReportReasonCode, details string) [sha256.Size]byte {
	digest := sha256.New()
	_, _ = digest.Write(postID[:])
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(reason))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(details))
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}
