package social

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	DefaultModerationCaseLimit  = 20
	MaxModerationCaseLimit      = 50
	MaxModerationCaseOffset     = 99_999
	MaxModerationReportsPerCase = 50
	MaxReportDetailsRunes       = 500
	MinModerationNoteRunes      = 10
	MaxModerationNoteRunes      = 1_000
)

var (
	ErrCommentReportInput               = errors.New("comment report input is invalid")
	ErrCommentReportTargetNotFound      = errors.New("visible comment report target was not found")
	ErrCommentReportSelf                = errors.New("authors cannot report their own comment")
	ErrCommentAlreadyReported           = errors.New("reporter already reported this open case")
	ErrCommentReportIdempotencyConflict = errors.New("comment report idempotency key was reused")
	ErrModerationCaseNotFound           = errors.New("comment moderation case was not found")
	ErrModerationCaseVersionConflict    = errors.New("comment moderation case version changed")
	ErrModerationCaseStateConflict      = errors.New("comment moderation case is no longer open")
	ErrModerationCommentVersionConflict = errors.New("moderated comment version changed")
	ErrModerationConflictOfInterest     = errors.New("moderator has a conflict of interest")
	ErrModerationDecisionIdempotency    = errors.New("moderation decision idempotency key was reused")
	ErrModerationInvariant              = errors.New("comment moderation projection violates persisted invariants")
)

type CommentReportReasonCode string

const (
	CommentReportSpam                CommentReportReasonCode = "spam"
	CommentReportHarassment          CommentReportReasonCode = "harassment"
	CommentReportPersonalInformation CommentReportReasonCode = "personal_information"
	CommentReportOffTopic            CommentReportReasonCode = "off_topic"
	CommentReportOther               CommentReportReasonCode = "other"
)

type CommentModerationCaseState string

const (
	CommentModerationCaseOpen          CommentModerationCaseState = "open"
	CommentModerationCaseDismissed     CommentModerationCaseState = "dismissed"
	CommentModerationCaseCommentHidden CommentModerationCaseState = "comment_hidden"
)

type CommentModerationDecision string

const (
	CommentModerationDismiss     CommentModerationDecision = "dismiss"
	CommentModerationHideComment CommentModerationDecision = "hide_comment"
)

type CommentModerationReasonCode string

const (
	CommentModerationNoViolation         CommentModerationReasonCode = "no_violation"
	CommentModerationSpam                CommentModerationReasonCode = "spam"
	CommentModerationHarassment          CommentModerationReasonCode = "harassment"
	CommentModerationPersonalInformation CommentModerationReasonCode = "personal_information"
	CommentModerationOffTopic            CommentModerationReasonCode = "off_topic"
	CommentModerationOther               CommentModerationReasonCode = "other"
)

type CommentReportReceipt struct {
	ID         uuid.UUID
	CommentID  uuid.UUID
	ReasonCode CommentReportReasonCode
	CreatedAt  time.Time
}

// CommentModerationReport excludes reporter identity by design. Moderators
// need the categorized allegation and bounded context, not a retaliation-prone
// user directory embedded in the review queue.
type CommentModerationReport struct {
	ReasonCode CommentReportReasonCode
	Details    string
	CreatedAt  time.Time
}

type CommentModerationTarget struct {
	CommentTarget
	Title string
}

type CommentModerationCase struct {
	ID               uuid.UUID
	State            CommentModerationCaseState
	Version          int64
	Target           CommentModerationTarget
	Comment          Comment
	ReportCount      int64
	Reports          []CommentModerationReport
	OpenedAt         time.Time
	LatestReportedAt time.Time
}

type CommentModerationCasePage struct {
	Items  []CommentModerationCase
	Total  int64
	Limit  int
	Offset int
}

type CreateCommentReportInput struct {
	RequestID  uuid.UUID
	CommentID  uuid.UUID
	ReasonCode CommentReportReasonCode
	Details    string
}

type DecideCommentModerationCaseInput struct {
	DecisionID             uuid.UUID
	CaseID                 uuid.UUID
	ExpectedCaseVersion    int64
	ExpectedCommentVersion int64
	Decision               CommentModerationDecision
	ReasonCode             CommentModerationReasonCode
	Note                   string
}

type CommentModerationDecisionResult struct {
	DecisionID     uuid.UUID
	CaseID         uuid.UUID
	CommentID      uuid.UUID
	Decision       CommentModerationDecision
	ReasonCode     CommentModerationReasonCode
	CaseState      CommentModerationCaseState
	CommentState   CommentState
	CaseVersion    int64
	CommentVersion int64
	DecidedAt      time.Time
}

type createCommentReportCommand struct {
	ReportPublicID  uuid.UUID
	CasePublicID    uuid.UUID
	RequestID       uuid.UUID
	CommentID       uuid.UUID
	ReporterID      uuid.UUID
	ReasonCode      CommentReportReasonCode
	Details         string
	CreateInputHash [sha256.Size]byte
	CreatedAt       time.Time
}

type decideCommentModerationCaseCommand struct {
	DecideCommentModerationCaseInput
	ModeratorID   uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type CommentModerationAuditState struct {
	CaseID         uuid.UUID                  `json:"case_id"`
	CaseState      CommentModerationCaseState `json:"case_state"`
	CaseVersion    int64                      `json:"case_version"`
	CommentID      uuid.UUID                  `json:"comment_id"`
	CommentState   CommentState               `json:"comment_state"`
	CommentVersion int64                      `json:"comment_version"`
}

type CommentModerationAuditInput struct {
	DecisionID      uuid.UUID
	ModeratorID     uuid.UUID
	CommentAuthorID uuid.UUID
	Target          CommentTarget
	Decision        CommentModerationDecision
	ReasonCode      CommentModerationReasonCode
	Note            string
	ReportCount     int64
	OccurredAt      time.Time
	Authorization   authz.Decision
	Before          CommentModerationAuditState
	After           CommentModerationAuditState
}

type CommentModerationEventBuilder interface {
	BuildCommentModerationDecisionEvent(CommentModerationAuditInput) (auditevent.Event, error)
}

type CommentModerationRepository interface {
	CreateReport(context.Context, createCommentReportCommand) (CommentReportReceipt, error)
	ListOpenCases(context.Context, int, int) (CommentModerationCasePage, error)
	Decide(context.Context, decideCommentModerationCaseCommand) (CommentModerationDecisionResult, error)
}

// CommentModerationService keeps public reporting and staff resolution in one
// social domain while preserving their separate credential audiences.
type CommentModerationService struct {
	authenticator CommentSessionAuthenticator
	authorizer    authz.Authorizer
	repository    CommentModerationRepository
	now           func() time.Time
}

func NewCommentModerationService(
	authenticator CommentSessionAuthenticator,
	authorizer authz.Authorizer,
	repository CommentModerationRepository,
	now func() time.Time,
) (*CommentModerationService, error) {
	if authenticator == nil || authorizer == nil || repository == nil {
		return nil, errors.New("comment moderation service dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &CommentModerationService{
		authenticator: authenticator,
		authorizer:    authorizer,
		repository:    repository,
		now:           now,
	}, nil
}

func (service *CommentModerationService) CreateReport(ctx context.Context, cookieToken, csrfToken string, input CreateCommentReportInput) (CommentReportReceipt, error) {
	details, err := normalizeModerationText(input.Details, 0, MaxReportDetailsRunes)
	if err != nil || input.RequestID == uuid.Nil || input.CommentID == uuid.Nil || !validCommentReportReason(input.ReasonCode) {
		return CommentReportReceipt{}, ErrCommentReportInput
	}
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return CommentReportReceipt{}, err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionCommentReportCreateSelf, now); err != nil {
		return CommentReportReceipt{}, err
	}
	receipt, err := service.repository.CreateReport(ctx, createCommentReportCommand{
		ReportPublicID: uuid.New(), CasePublicID: uuid.New(), RequestID: input.RequestID,
		CommentID: input.CommentID, ReporterID: session.User.ID, ReasonCode: input.ReasonCode,
		Details: details, CreateInputHash: commentReportInputHash(input.CommentID, input.ReasonCode, details), CreatedAt: now,
	})
	if err != nil {
		return CommentReportReceipt{}, err
	}
	if receipt.ID == uuid.Nil || receipt.CommentID != input.CommentID || receipt.ReasonCode != input.ReasonCode || receipt.CreatedAt.IsZero() {
		return CommentReportReceipt{}, ErrModerationInvariant
	}
	return receipt, nil
}

func (service *CommentModerationService) ListOpenCases(ctx context.Context, actor authz.StaffActor, limit, offset int) (CommentModerationCasePage, error) {
	if limit < 1 || limit > MaxModerationCaseLimit || offset < 0 || offset > MaxModerationCaseOffset {
		return CommentModerationCasePage{}, ErrCommentReportInput
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionSocialReportRead, authz.SiteScope(), now, "comment-moderation"); err != nil {
		return CommentModerationCasePage{}, err
	}
	page, err := service.repository.ListOpenCases(ctx, limit, offset)
	if err != nil {
		return CommentModerationCasePage{}, err
	}
	if page.Total < 0 || page.Limit != limit || page.Offset != offset || len(page.Items) > limit || (len(page.Items) > 0 && int64(offset+len(page.Items)) > page.Total) {
		return CommentModerationCasePage{}, ErrModerationInvariant
	}
	for _, item := range page.Items {
		if validateModerationCase(item) != nil {
			return CommentModerationCasePage{}, ErrModerationInvariant
		}
	}
	return page, nil
}

func (service *CommentModerationService) Decide(ctx context.Context, actor authz.StaffActor, input DecideCommentModerationCaseInput) (CommentModerationDecisionResult, error) {
	normalized, err := normalizeModerationDecisionInput(input)
	if err != nil {
		return CommentModerationDecisionResult{}, err
	}
	now := service.now().UTC()
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionSocialReportResolve, authz.SiteScope(), now, "comment-moderation")
	if err != nil {
		return CommentModerationDecisionResult{}, err
	}
	result, err := service.repository.Decide(ctx, decideCommentModerationCaseCommand{
		DecideCommentModerationCaseInput: normalized,
		ModeratorID:                      actor.Subject.ID,
		OccurredAt:                       now,
		Authorization:                    decision,
	})
	if err != nil {
		return CommentModerationDecisionResult{}, err
	}
	if result.DecisionID != input.DecisionID || result.CaseID != input.CaseID ||
		result.Decision != normalized.Decision || result.ReasonCode != normalized.ReasonCode ||
		result.CaseVersion != input.ExpectedCaseVersion+1 || result.CommentID == uuid.Nil ||
		result.CommentVersion < input.ExpectedCommentVersion || result.DecidedAt.IsZero() {
		return CommentModerationDecisionResult{}, ErrModerationInvariant
	}
	return result, nil
}

func normalizeModerationDecisionInput(input DecideCommentModerationCaseInput) (DecideCommentModerationCaseInput, error) {
	note, err := normalizeModerationText(input.Note, MinModerationNoteRunes, MaxModerationNoteRunes)
	if err != nil || input.DecisionID == uuid.Nil || input.CaseID == uuid.Nil || input.ExpectedCaseVersion < 1 || input.ExpectedCommentVersion < 1 {
		return DecideCommentModerationCaseInput{}, ErrCommentReportInput
	}
	input.Note = note
	switch input.Decision {
	case CommentModerationDismiss:
		if input.ReasonCode != CommentModerationNoViolation {
			return DecideCommentModerationCaseInput{}, ErrCommentReportInput
		}
	case CommentModerationHideComment:
		if input.ReasonCode == CommentModerationNoViolation || !validModerationViolationReason(input.ReasonCode) {
			return DecideCommentModerationCaseInput{}, ErrCommentReportInput
		}
	default:
		return DecideCommentModerationCaseInput{}, ErrCommentReportInput
	}
	return input, nil
}

func validateModerationCase(item CommentModerationCase) error {
	if item.ID == uuid.Nil || item.State != CommentModerationCaseOpen || item.Version < 1 ||
		!item.Target.CommentTarget.Valid() || strings.TrimSpace(item.Target.Title) == "" ||
		utf8.RuneCountInString(item.Target.Title) > 240 || item.Comment.Target != item.Target.CommentTarget ||
		validatePersistedComment(item.Comment) != nil || len(item.Reports) < 1 ||
		len(item.Reports) > MaxModerationReportsPerCase || item.ReportCount < int64(len(item.Reports)) ||
		item.OpenedAt.IsZero() || item.LatestReportedAt.Before(item.OpenedAt) {
		return ErrModerationInvariant
	}
	latestReportedAt := item.OpenedAt
	for _, report := range item.Reports {
		if !validCommentReportReason(report.ReasonCode) || report.CreatedAt.IsZero() ||
			report.CreatedAt.Before(item.OpenedAt) || report.CreatedAt.After(item.LatestReportedAt) {
			return ErrModerationInvariant
		}
		if normalized, err := normalizeModerationText(report.Details, 0, MaxReportDetailsRunes); err != nil || normalized != report.Details {
			return ErrModerationInvariant
		}
		if report.CreatedAt.After(latestReportedAt) {
			latestReportedAt = report.CreatedAt
		}
	}
	if !latestReportedAt.Equal(item.LatestReportedAt) {
		return ErrModerationInvariant
	}
	return nil
}

func validCommentReportReason(value CommentReportReasonCode) bool {
	switch value {
	case CommentReportSpam, CommentReportHarassment, CommentReportPersonalInformation, CommentReportOffTopic, CommentReportOther:
		return true
	default:
		return false
	}
}

func validModerationViolationReason(value CommentModerationReasonCode) bool {
	switch value {
	case CommentModerationSpam, CommentModerationHarassment, CommentModerationPersonalInformation, CommentModerationOffTopic, CommentModerationOther:
		return true
	default:
		return false
	}
}

func normalizeModerationText(value string, minimum, maximum int) (string, error) {
	if !utf8.ValidString(value) {
		return "", ErrCommentReportInput
	}
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = strings.TrimSpace(value)
	count := utf8.RuneCountInString(value)
	if count < minimum || count > maximum {
		return "", ErrCommentReportInput
	}
	for _, character := range value {
		if unicode.IsControl(character) && character != '\n' && character != '\t' {
			return "", ErrCommentReportInput
		}
	}
	return value, nil
}

func commentReportInputHash(commentID uuid.UUID, reason CommentReportReasonCode, details string) [sha256.Size]byte {
	digest := sha256.New()
	_, _ = digest.Write(commentID[:])
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(reason))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(details))
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}
