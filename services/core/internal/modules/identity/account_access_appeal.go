package identity

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

var (
	ErrAccountAccessNotRestricted     = errors.New("account has no appealable access restriction")
	ErrAccountAccessAppealExists      = errors.New("account access appeal already exists")
	ErrAccountAccessAppealMissing     = errors.New("account access appeal was not found")
	ErrAccountAccessAppealConflict    = errors.New("account access appeal source changed")
	ErrAccountAccessAppealSelfTarget  = errors.New("staff actor cannot decide their own account access appeal")
	ErrAccountAccessAppealIdempotency = errors.New("account access appeal idempotency conflict")
)

const MaximumAccountAccessAppealListLimit = 50

type AccountAccessSourceKind string

const (
	AccountAccessSourceTemporaryRestriction AccountAccessSourceKind = "temporary_restriction"
	AccountAccessSourceDisabledAccount      AccountAccessSourceKind = "disabled_account"
	AccountAccessSourceManualDownload       AccountAccessSourceKind = "manual_download_restriction"
)

type AccountAccessAppealStatus string

const (
	AccountAccessAppealPending        AccountAccessAppealStatus = "pending"
	AccountAccessAppealApproved       AccountAccessAppealStatus = "approved"
	AccountAccessAppealRejected       AccountAccessAppealStatus = "rejected"
	AccountAccessAppealSourceResolved AccountAccessAppealStatus = "source_resolved"
)

type AccountAccessAppealFilter string

const (
	AccountAccessAppealFilterAll      AccountAccessAppealFilter = "all"
	AccountAccessAppealFilterPending  AccountAccessAppealFilter = "pending"
	AccountAccessAppealFilterResolved AccountAccessAppealFilter = "resolved"
)

type AccountAccessAppealDecision string

const (
	AccountAccessAppealDecisionApprove AccountAccessAppealDecision = "approved"
	AccountAccessAppealDecisionReject  AccountAccessAppealDecision = "rejected"
)

// AccountAccessCredentials are used only for the purpose-limited Vault
// decision. They must stay in request memory and must never enter logs,
// database rows, query caches or audit payloads.
type AccountAccessCredentials struct {
	Identifier       string
	Password         string
	SecondFactorCode string
}

type AccountAccessRestriction struct {
	SourceKind    AccountAccessSourceKind
	ReasonCode    string
	ReasonSummary string
	StartsAt      time.Time
	ExpiresAt     *time.Time
	SourceVersion int64
}

type AccountAccessAppeal struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	UserNumericID int64
	Username      string
	Restriction   AccountAccessRestriction
	Statement     string
	Status        AccountAccessAppealStatus
	Response      string
	CreatedAt     time.Time
	ResolvedAt    *time.Time
	SourceActive  bool
	Replayed      bool
}

type AccountAccessStatus struct {
	Restricted  bool
	Restriction *AccountAccessRestriction
	Appeal      *AccountAccessAppeal
	CanAppeal   bool
}

type InspectAccountAccessInput struct {
	Credentials AccountAccessCredentials
}

type SubmitAccountAccessAppealInput struct {
	AppealID    uuid.UUID
	Credentials AccountAccessCredentials
	Statement   string
}

type AccountAccessAppealQuery struct {
	Query  string
	Filter AccountAccessAppealFilter
	Limit  int
	Offset int
}

type AccountAccessAppealPage struct {
	Items  []AccountAccessAppeal
	Total  int64
	Limit  int
	Offset int
}

type DecideAccountAccessAppealInput struct {
	AppealID              uuid.UUID
	Decision              AccountAccessAppealDecision
	Response              string
	ExpectedSourceVersion int64
}

type SubmitAccountAccessAppealCommand struct {
	AppealID  uuid.UUID
	UserID    uuid.UUID
	Statement string
	CreatedAt time.Time
}

type DecideAccountAccessAppealCommand struct {
	DecideAccountAccessAppealInput
	ActorID       uuid.UUID
	DecidedAt     time.Time
	Authorization authz.Decision
}

type AccountAccessAppealDecisionPreflight struct {
	UserID        uuid.UUID
	CredentialRef uuid.UUID
	SourceKind    AccountAccessSourceKind
	SourceVersion int64
}

type AccountAccessAppealCredentialVault interface {
	VerifyForAccountAppeal(context.Context, LoginInput) (uuid.UUID, error)
	EnableAfterAccountAppeal(context.Context, uuid.UUID) error
}

type AccountAccessAppealRepository interface {
	StatusByCredentialRef(context.Context, uuid.UUID, time.Time) (AccountAccessStatus, uuid.UUID, error)
	SubmitAccountAccessAppeal(context.Context, SubmitAccountAccessAppealCommand) (AccountAccessAppeal, error)
	ListAccountAccessAppeals(context.Context, AccountAccessAppealQuery, time.Time) (AccountAccessAppealPage, error)
	AccountAccessAppealDecisionPreflight(context.Context, uuid.UUID, time.Time) (AccountAccessAppealDecisionPreflight, error)
	DecideAccountAccessAppeal(context.Context, DecideAccountAccessAppealCommand) (AccountAccessAppeal, error)
}

type AccountAccessAppealService struct {
	vault      AccountAccessAppealCredentialVault
	repository AccountAccessAppealRepository
	authorizer authz.Authorizer
	now        func() time.Time
}

func NewAccountAccessAppealService(vault AccountAccessAppealCredentialVault, repository AccountAccessAppealRepository, authorizer authz.Authorizer, now func() time.Time) (*AccountAccessAppealService, error) {
	if vault == nil || repository == nil || authorizer == nil {
		return nil, errors.New("account access appeal service dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &AccountAccessAppealService{vault: vault, repository: repository, authorizer: authorizer, now: now}, nil
}

func (service *AccountAccessAppealService) InspectAccountAccess(ctx context.Context, input InspectAccountAccessInput) (AccountAccessStatus, error) {
	credentials, err := normalizeAccountAccessCredentials(input.Credentials)
	if err != nil {
		return AccountAccessStatus{}, err
	}
	credentialRef, err := service.vault.VerifyForAccountAppeal(ctx, LoginInput{
		Identifier: credentials.Identifier, Password: credentials.Password,
		SecondFactorCode: credentials.SecondFactorCode,
	})
	if err != nil {
		return AccountAccessStatus{}, err
	}
	status, _, err := service.repository.StatusByCredentialRef(ctx, credentialRef, service.now().UTC().Round(0))
	return status, err
}

func (service *AccountAccessAppealService) SubmitAccountAccessAppeal(ctx context.Context, input SubmitAccountAccessAppealInput) (AccountAccessAppeal, error) {
	credentials, err := normalizeAccountAccessCredentials(input.Credentials)
	input.Statement = strings.TrimSpace(input.Statement)
	if err != nil || input.AppealID == uuid.Nil || !validAccountAccessAppealText(input.Statement, 20, 1000) {
		return AccountAccessAppeal{}, ErrInvalidInput
	}
	credentialRef, err := service.vault.VerifyForAccountAppeal(ctx, LoginInput{
		Identifier: credentials.Identifier, Password: credentials.Password,
		SecondFactorCode: credentials.SecondFactorCode,
	})
	if err != nil {
		return AccountAccessAppeal{}, err
	}
	now := service.now().UTC().Round(0)
	status, userID, err := service.repository.StatusByCredentialRef(ctx, credentialRef, now)
	if err != nil {
		return AccountAccessAppeal{}, err
	}
	if !status.Restricted {
		return AccountAccessAppeal{}, ErrAccountAccessNotRestricted
	}
	// The repository owns idempotency replay. In particular, a successful
	// request retried with the same key and statement must return the original
	// appeal even though the status projection now says no new appeal can be
	// opened.
	return service.repository.SubmitAccountAccessAppeal(ctx, SubmitAccountAccessAppealCommand{
		AppealID: input.AppealID, UserID: userID, Statement: input.Statement, CreatedAt: now,
	})
}

func (service *AccountAccessAppealService) AccountAccessAppeals(ctx context.Context, actor authz.StaffActor, query AccountAccessAppealQuery) (AccountAccessAppealPage, error) {
	query.Query = strings.TrimSpace(query.Query)
	if query.Limit < 1 || query.Limit > MaximumAccountAccessAppealListLimit || query.Offset < 0 || query.Offset > 1_000_000 ||
		utf8.RuneCountInString(query.Query) > 120 || !validAccountAccessAppealFilter(query.Filter) {
		return AccountAccessAppealPage{}, ErrInvalidInput
	}
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionUserAccountAppealRead, authz.SiteScope(), now, "account-access-appeal-read"); err != nil {
		return AccountAccessAppealPage{}, err
	}
	return service.repository.ListAccountAccessAppeals(ctx, query, now)
}

func (service *AccountAccessAppealService) DecideAccountAccessAppeal(ctx context.Context, actor authz.StaffActor, input DecideAccountAccessAppealInput) (AccountAccessAppeal, error) {
	input.Response = strings.TrimSpace(input.Response)
	if input.AppealID == uuid.Nil || input.ExpectedSourceVersion < 1 ||
		(input.Decision != AccountAccessAppealDecisionApprove && input.Decision != AccountAccessAppealDecisionReject) ||
		!validAccountAccessAppealText(input.Response, 10, 1000) {
		return AccountAccessAppeal{}, ErrInvalidInput
	}
	now := service.now().UTC().Round(0)
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionUserAccountAppealDecide, authz.SiteScope(), now, "account-access-appeal-decision")
	if err != nil {
		return AccountAccessAppeal{}, err
	}
	preflight, err := service.repository.AccountAccessAppealDecisionPreflight(ctx, input.AppealID, now)
	if err != nil {
		return AccountAccessAppeal{}, err
	}
	if preflight.UserID == actor.Subject.ID {
		return AccountAccessAppeal{}, ErrAccountAccessAppealSelfTarget
	}
	if preflight.SourceVersion != input.ExpectedSourceVersion {
		return AccountAccessAppeal{}, ErrAccountAccessAppealConflict
	}
	if input.Decision == AccountAccessAppealDecisionApprove && preflight.SourceKind == AccountAccessSourceDisabledAccount {
		// Vault first, Core second is intentionally fail closed: if the Core
		// transaction loses a race, its disabled row still rejects login and the
		// same reviewed command can be retried safely.
		if err := service.vault.EnableAfterAccountAppeal(ctx, preflight.CredentialRef); err != nil {
			return AccountAccessAppeal{}, err
		}
	}
	return service.repository.DecideAccountAccessAppeal(ctx, DecideAccountAccessAppealCommand{
		DecideAccountAccessAppealInput: input,
		ActorID:                        actor.Subject.ID, DecidedAt: now, Authorization: decision,
	})
}

func normalizeAccountAccessCredentials(input AccountAccessCredentials) (AccountAccessCredentials, error) {
	input.Identifier = strings.TrimSpace(input.Identifier)
	input.SecondFactorCode = strings.TrimSpace(input.SecondFactorCode)
	if input.Identifier == "" || utf8.RuneCountInString(input.Identifier) > maxIdentifierRunes ||
		input.Password == "" || len(input.Password) > maxPasswordBytes || len(input.SecondFactorCode) > 32 {
		return AccountAccessCredentials{}, ErrInvalidInput
	}
	return input, nil
}

func validAccountAccessAppealText(value string, minimum, maximum int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) >= minimum && utf8.RuneCountInString(value) <= maximum
}

func validAccountAccessAppealFilter(value AccountAccessAppealFilter) bool {
	switch value {
	case AccountAccessAppealFilterAll, AccountAccessAppealFilterPending, AccountAccessAppealFilterResolved:
		return true
	default:
		return false
	}
}
