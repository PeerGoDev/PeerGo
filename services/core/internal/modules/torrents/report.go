package torrents

import (
	"context"
	"crypto/sha256"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	DefaultTorrentReportCaseLimit = 20
	MaxTorrentReportCaseLimit     = 50
	MaxTorrentReportsPerCase      = 50
	MaxTorrentReportDetailsRunes  = 1000
	MinTorrentReportNoteRunes     = 10
	MaxTorrentReportNoteRunes     = 1000
)

var (
	ErrTorrentReportInput               = errors.New("torrent report input is invalid")
	ErrTorrentReportTargetNotFound      = errors.New("visible torrent report target was not found")
	ErrTorrentReportEmailUnverified     = errors.New("torrent report requires a verified email")
	ErrTorrentReportSelf                = errors.New("uploader cannot report own torrent")
	ErrTorrentAlreadyReported           = errors.New("reporter already reported this open torrent case")
	ErrTorrentReportIdempotencyConflict = errors.New("torrent report idempotency key was reused")
	ErrTorrentReportCaseNotFound        = errors.New("torrent report case was not found")
	ErrTorrentReportCaseStateConflict   = errors.New("torrent report case is no longer open")
	ErrTorrentReportCaseVersionConflict = errors.New("torrent report case version changed")
	ErrTorrentReportVersionConflict     = errors.New("reported torrent version changed")
	ErrTorrentReportStateConflict       = errors.New("reported torrent cannot perform the requested transition")
	ErrTorrentReportSelfReview          = errors.New("torrent report reviewer has a conflict of interest")
	ErrTorrentReportDecisionConflict    = errors.New("torrent report decision idempotency key was reused")
	ErrTorrentReportInvariant           = errors.New("torrent report projection violates persisted invariants")
)

type TorrentReportReasonCode string

const (
	TorrentReportContentMismatch TorrentReportReasonCode = "content_mismatch"
	TorrentReportCopyright       TorrentReportReasonCode = "copyright"
	TorrentReportDuplicateSpam   TorrentReportReasonCode = "duplicate_or_spam"
	TorrentReportMalicious       TorrentReportReasonCode = "malicious"
	TorrentReportOther           TorrentReportReasonCode = "other"
)

type TorrentReportCaseState string

const (
	TorrentReportCaseOpen            TorrentReportCaseState = "open"
	TorrentReportCaseDismissed       TorrentReportCaseState = "dismissed"
	TorrentReportCaseTorrentDisabled TorrentReportCaseState = "torrent_disabled"
)

type TorrentReportDecision string

const (
	TorrentReportDismiss        TorrentReportDecision = "dismiss"
	TorrentReportDisableTorrent TorrentReportDecision = "disable_torrent"
)

type TorrentReportDecisionReasonCode string

const (
	TorrentReportNoViolation             TorrentReportDecisionReasonCode = "no_violation"
	TorrentReportDecisionContentMismatch TorrentReportDecisionReasonCode = "content_mismatch"
	TorrentReportDecisionCopyright       TorrentReportDecisionReasonCode = "copyright"
	TorrentReportDecisionDuplicateSpam   TorrentReportDecisionReasonCode = "duplicate_or_spam"
	TorrentReportDecisionMalicious       TorrentReportDecisionReasonCode = "malicious"
	TorrentReportDecisionOther           TorrentReportDecisionReasonCode = "other"
)

type CreateTorrentReportInput struct {
	RequestID  uuid.UUID
	TorrentID  TorrentID
	ReasonCode TorrentReportReasonCode
	Details    string
}

type CreateTorrentReportCommand struct {
	CreateTorrentReportInput
	ReportID      uuid.UUID
	CaseID        uuid.UUID
	ReporterID    uuid.UUID
	InputSHA256   [sha256.Size]byte
	CreatedAt     time.Time
	Authorization authz.Decision
}

type TorrentReportReceipt struct {
	ID         uuid.UUID
	CaseID     uuid.UUID
	TorrentID  TorrentID
	ReasonCode TorrentReportReasonCode
	CreatedAt  time.Time
}

// TorrentReportAllegation deliberately omits reporter identity. Staff need the
// categorized allegation and bounded context, not a retaliation-prone member
// directory embedded in the review queue.
type TorrentReportAllegation struct {
	ReasonCode TorrentReportReasonCode
	Details    string
	CreatedAt  time.Time
}

type ManagedTorrentReportCase struct {
	ID                  uuid.UUID
	State               TorrentReportCaseState
	Version             int64
	TorrentID           TorrentID
	TorrentTitle        string
	TorrentState        State
	TorrentVersion      int64
	UploaderNumericID   int64
	UploaderUsername    string
	UploaderDisplayName string
	ReportCount         int64
	Reports             []TorrentReportAllegation
	ActivePurchaseCount int64
	OpenedAt            time.Time
	LatestReportedAt    time.Time
}

type TorrentReportCaseQuery struct {
	State  TorrentReportCaseState
	Limit  int
	Offset int
}

type ManagedTorrentReportCasePage struct {
	Items  []ManagedTorrentReportCase
	Total  int64
	Limit  int
	Offset int
}

type DecideTorrentReportCaseInput struct {
	DecisionID             uuid.UUID
	CaseID                 uuid.UUID
	ExpectedCaseVersion    int64
	ExpectedTorrentVersion int64
	Decision               TorrentReportDecision
	ReasonCode             TorrentReportDecisionReasonCode
	Note                   string
}

type DecideTorrentReportCaseCommand struct {
	DecideTorrentReportCaseInput
	ReviewerID    uuid.UUID
	DecidedAt     time.Time
	Authorization authz.Decision
}

type TorrentReportDecisionResult struct {
	DecisionID     uuid.UUID
	CaseID         uuid.UUID
	TorrentID      TorrentID
	Decision       TorrentReportDecision
	CaseState      TorrentReportCaseState
	CaseVersion    int64
	TorrentState   State
	TorrentVersion int64
	DecidedAt      time.Time
}

func (service *TorrentMaintenanceService) CreateTorrentReport(ctx context.Context, cookieToken, csrfToken string, input CreateTorrentReportInput) (TorrentReportReceipt, error) {
	normalized, err := normalizeCreateTorrentReportInput(input)
	if err != nil {
		return TorrentReportReceipt{}, err
	}
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return TorrentReportReceipt{}, err
	}
	if session.User.EmailVerifiedAt == nil {
		return TorrentReportReceipt{}, ErrTorrentReportEmailUnverified
	}
	now := service.now().UTC().Round(0)
	decision, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionTorrentReportCreateSelf, now)
	if err != nil {
		return TorrentReportReceipt{}, err
	}
	command := CreateTorrentReportCommand{
		CreateTorrentReportInput: normalized,
		ReportID:                 uuid.New(), CaseID: uuid.New(), ReporterID: session.User.ID,
		InputSHA256: torrentReportInputHash(normalized), CreatedAt: now, Authorization: decision,
	}
	receipt, err := service.repository.CreateTorrentReport(ctx, command)
	if err != nil {
		return TorrentReportReceipt{}, err
	}
	if receipt.ID == uuid.Nil || receipt.CaseID == uuid.Nil || receipt.TorrentID != normalized.TorrentID ||
		receipt.ReasonCode != normalized.ReasonCode || receipt.CreatedAt.IsZero() {
		return TorrentReportReceipt{}, ErrTorrentReportInvariant
	}
	return receipt, nil
}

func (service *TorrentMaintenanceService) ListTorrentReportCases(ctx context.Context, actor authz.StaffActor, query TorrentReportCaseQuery) (ManagedTorrentReportCasePage, error) {
	normalized, err := normalizeTorrentReportCaseQuery(query)
	if err != nil {
		return ManagedTorrentReportCasePage{}, err
	}
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTorrentReportReview, authz.SiteScope(), service.now().UTC(), "torrent-report-moderation"); err != nil {
		return ManagedTorrentReportCasePage{}, err
	}
	page, err := service.repository.ListTorrentReportCases(ctx, normalized)
	if err != nil {
		return ManagedTorrentReportCasePage{}, err
	}
	if page.Total < 0 || page.Limit != normalized.Limit || page.Offset != normalized.Offset || len(page.Items) > normalized.Limit {
		return ManagedTorrentReportCasePage{}, ErrTorrentReportInvariant
	}
	return page, nil
}

func (service *TorrentMaintenanceService) DecideTorrentReportCase(ctx context.Context, actor authz.StaffActor, input DecideTorrentReportCaseInput) (TorrentReportDecisionResult, error) {
	normalized, err := normalizeDecideTorrentReportCaseInput(input)
	if err != nil {
		return TorrentReportDecisionResult{}, err
	}
	now := service.now().UTC().Round(0)
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTorrentReportReview, authz.SiteScope(), now, "torrent-report-moderation")
	if err != nil {
		return TorrentReportDecisionResult{}, err
	}
	result, err := service.repository.DecideTorrentReportCase(ctx, DecideTorrentReportCaseCommand{
		DecideTorrentReportCaseInput: normalized,
		ReviewerID:                   actor.Subject.ID, DecidedAt: now, Authorization: decision,
	})
	if err != nil {
		return TorrentReportDecisionResult{}, err
	}
	if result.DecisionID != normalized.DecisionID || result.CaseID != normalized.CaseID || result.TorrentID < 1 ||
		result.Decision != normalized.Decision || result.CaseVersion != normalized.ExpectedCaseVersion+1 || result.DecidedAt.IsZero() {
		return TorrentReportDecisionResult{}, ErrTorrentReportInvariant
	}
	if result.Decision == TorrentReportDisableTorrent &&
		(result.CaseState != TorrentReportCaseTorrentDisabled || result.TorrentState != StateDisabled || result.TorrentVersion != normalized.ExpectedTorrentVersion+1) {
		return TorrentReportDecisionResult{}, ErrTorrentReportInvariant
	}
	if result.Decision == TorrentReportDismiss &&
		(result.CaseState != TorrentReportCaseDismissed || result.TorrentVersion != normalized.ExpectedTorrentVersion) {
		return TorrentReportDecisionResult{}, ErrTorrentReportInvariant
	}
	return result, nil
}

func normalizeCreateTorrentReportInput(input CreateTorrentReportInput) (CreateTorrentReportInput, error) {
	input.Details = strings.TrimSpace(input.Details)
	if input.RequestID == uuid.Nil || input.TorrentID < 1 || !validTorrentReportReason(input.ReasonCode) ||
		!utf8.ValidString(input.Details) || utf8.RuneCountInString(input.Details) > MaxTorrentReportDetailsRunes ||
		(input.ReasonCode == TorrentReportOther && utf8.RuneCountInString(input.Details) < 10) {
		return CreateTorrentReportInput{}, ErrTorrentReportInput
	}
	return input, nil
}

func normalizeTorrentReportCaseQuery(query TorrentReportCaseQuery) (TorrentReportCaseQuery, error) {
	if query.Limit < 1 || query.Limit > MaxTorrentReportCaseLimit || query.Offset < 0 || query.Offset > MaxManagedTorrentOffset {
		return TorrentReportCaseQuery{}, ErrTorrentReportInput
	}
	switch query.State {
	case "", TorrentReportCaseOpen, TorrentReportCaseDismissed, TorrentReportCaseTorrentDisabled:
		return query, nil
	default:
		return TorrentReportCaseQuery{}, ErrTorrentReportInput
	}
}

func normalizeDecideTorrentReportCaseInput(input DecideTorrentReportCaseInput) (DecideTorrentReportCaseInput, error) {
	input.Note = strings.TrimSpace(input.Note)
	if input.DecisionID == uuid.Nil || input.CaseID == uuid.Nil || input.ExpectedCaseVersion < 1 || input.ExpectedTorrentVersion < 1 ||
		!utf8.ValidString(input.Note) || utf8.RuneCountInString(input.Note) < MinTorrentReportNoteRunes || utf8.RuneCountInString(input.Note) > MaxTorrentReportNoteRunes {
		return DecideTorrentReportCaseInput{}, ErrTorrentReportInput
	}
	switch input.Decision {
	case TorrentReportDismiss:
		if input.ReasonCode != TorrentReportNoViolation {
			return DecideTorrentReportCaseInput{}, ErrTorrentReportInput
		}
	case TorrentReportDisableTorrent:
		if !validTorrentReportDecisionViolationReason(input.ReasonCode) {
			return DecideTorrentReportCaseInput{}, ErrTorrentReportInput
		}
	default:
		return DecideTorrentReportCaseInput{}, ErrTorrentReportInput
	}
	return input, nil
}

func validTorrentReportReason(reason TorrentReportReasonCode) bool {
	switch reason {
	case TorrentReportContentMismatch, TorrentReportCopyright, TorrentReportDuplicateSpam, TorrentReportMalicious, TorrentReportOther:
		return true
	default:
		return false
	}
}

func validTorrentReportDecisionViolationReason(reason TorrentReportDecisionReasonCode) bool {
	switch reason {
	case TorrentReportDecisionContentMismatch, TorrentReportDecisionCopyright, TorrentReportDecisionDuplicateSpam, TorrentReportDecisionMalicious, TorrentReportDecisionOther:
		return true
	default:
		return false
	}
}

func torrentReportInputHash(input CreateTorrentReportInput) [sha256.Size]byte {
	canonical := strconv.FormatInt(int64(input.TorrentID), 10) + "\x00" + string(input.ReasonCode) + "\x00" + input.Details
	return sha256.Sum256([]byte(canonical))
}
