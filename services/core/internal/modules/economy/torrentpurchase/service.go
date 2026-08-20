package torrentpurchase

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type SessionAuthenticator interface {
	CurrentSession(context.Context, string) (identity.WebSession, error)
	AuthenticateWrite(context.Context, string, string) (identity.WebSession, error)
}

type Service struct {
	authenticator SessionAuthenticator
	repository    Repository
	authorizer    authz.Authorizer
	now           func() time.Time
}

func NewService(authenticator SessionAuthenticator, repository Repository, authorizer authz.Authorizer, now func() time.Time) (*Service, error) {
	if authenticator == nil || repository == nil || authorizer == nil {
		return nil, errors.New("torrent purchase dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{authenticator: authenticator, repository: repository, authorizer: authorizer, now: now}, nil
}

func (service *Service) MyStatus(ctx context.Context, cookieToken string, torrentID int64) (Status, error) {
	if torrentID < 1 {
		return Status{}, ErrInput
	}
	now := canonicalTime(service.now())
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return Status{}, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionTorrentPurchaseReadSelf, now); err != nil {
		return Status{}, err
	}
	return service.repository.Status(ctx, session.User.ID, torrentID, now)
}

func (service *Service) Purchase(ctx context.Context, cookieToken, csrfToken string, requestID uuid.UUID, torrentID int64) (Receipt, error) {
	if requestID == uuid.Nil || torrentID < 1 {
		return Receipt{}, ErrInput
	}
	now := canonicalTime(service.now())
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return Receipt{}, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionTorrentPurchaseCreateSelf, now); err != nil {
		return Receipt{}, err
	}
	return service.repository.Purchase(ctx, PurchaseCommand{
		RequestID: requestID,
		UserID:    session.User.ID,
		TorrentID: torrentID,
		Now:       now,
	})
}

// MyHistory returns only the caller's durable, unrefunded entitlements. The
// Repository reads receipt prices so a later staff price edit cannot change
// what the member sees as the amount originally paid.
func (service *Service) MyHistory(ctx context.Context, cookieToken string, limit, offset int) (HistoryPage, error) {
	if limit < 1 || limit > MaxHistoryLimit || offset < 0 || offset > MaxHistoryOffset {
		return HistoryPage{}, ErrInput
	}
	repository, ok := service.repository.(HistoryRepository)
	if !ok {
		return HistoryPage{}, ErrInvariant
	}
	now := canonicalTime(service.now())
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return HistoryPage{}, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionTorrentPurchaseReadSelf, now); err != nil {
		return HistoryPage{}, err
	}
	return repository.ListHistory(ctx, HistoryQuery{UserID: session.User.ID, Limit: limit, Offset: offset})
}

func (service *Service) PurchasePolicy(ctx context.Context, actor authz.StaffActor) (PolicySettings, error) {
	repository, ok := service.repository.(AdministrationRepository)
	if !ok {
		return PolicySettings{}, ErrInvariant
	}
	now := canonicalTime(service.now())
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTorrentManageRead, authz.SiteScope(), now, "torrent-purchase-administration"); err != nil {
		return PolicySettings{}, err
	}
	return repository.CurrentPolicy(ctx, now)
}

func (service *Service) UpdatePurchasePolicy(ctx context.Context, actor authz.StaffActor, input UpdatePolicyCommand) (PolicySettings, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	input.ExpectedRevision = strings.TrimSpace(input.ExpectedRevision)
	if input.RequestID == uuid.Nil || input.TaxBasisPoints < 0 || input.TaxBasisPoints > 10000 ||
		input.ExpectedRevision == "" || !utf8.ValidString(input.Reason) || utf8.RuneCountInString(input.Reason) < 10 ||
		utf8.RuneCountInString(input.Reason) > 1000 {
		return PolicySettings{}, ErrInput
	}
	repository, ok := service.repository.(AdministrationRepository)
	if !ok {
		return PolicySettings{}, ErrInvariant
	}
	now := canonicalTime(service.now())
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTorrentPurchaseManageUpdate, authz.SiteScope(), now, "torrent-purchase-administration")
	if err != nil {
		return PolicySettings{}, err
	}
	input.ActorID = actor.Subject.ID
	input.OccurredAt = now
	input.AuthorizationID = decision.ID
	return repository.UpdatePolicy(ctx, input)
}

func (service *Service) UpdateTorrentPrice(ctx context.Context, actor authz.StaffActor, input UpdatePriceCommand) (PriceChange, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if input.RequestID == uuid.Nil || input.TorrentID < 1 || input.Price < 0 || input.Price > 1_000_000 ||
		input.ExpectedVersion < 1 || !utf8.ValidString(input.Reason) || utf8.RuneCountInString(input.Reason) < 10 ||
		utf8.RuneCountInString(input.Reason) > 1000 {
		return PriceChange{}, ErrInput
	}
	repository, ok := service.repository.(AdministrationRepository)
	if !ok {
		return PriceChange{}, ErrInvariant
	}
	now := canonicalTime(service.now())
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTorrentPurchaseManageUpdate, authz.SiteScope(), now, "torrent-purchase-administration")
	if err != nil {
		return PriceChange{}, err
	}
	input.ActorID = actor.Subject.ID
	input.OccurredAt = now
	input.AuthorizationID = decision.ID
	return repository.UpdatePrice(ctx, input)
}

func (service *Service) AdminHistory(ctx context.Context, actor authz.StaffActor, query AdminPurchaseQuery) (AdminPurchasePage, error) {
	query.Query = strings.TrimSpace(query.Query)
	if !utf8.ValidString(query.Query) || utf8.RuneCountInString(query.Query) > 100 ||
		query.Limit < 1 || query.Limit > MaxAdminLimit || query.Offset < 0 || query.Offset > MaxAdminOffset ||
		!validAdminPurchaseStatus(query.Status) || !validAdminPurchaseSource(query.Source) {
		return AdminPurchasePage{}, ErrInput
	}
	repository, ok := service.repository.(AdministrationRepository)
	if !ok {
		return AdminPurchasePage{}, ErrInvariant
	}
	now := canonicalTime(service.now())
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTorrentPurchaseManageRead, authz.SiteScope(), now, "torrent-purchase-administration"); err != nil {
		return AdminPurchasePage{}, err
	}
	return repository.ListPurchases(ctx, query)
}

// RefundPurchase writes the refund ledger transaction and entitlement
// revocation in one Core transaction. The site purchase account funds the
// refund, so a member is never blocked because the uploader already spent the
// historical proceeds.
func (service *Service) RefundPurchase(ctx context.Context, actor authz.StaffActor, input RefundCommand) (RefundReceipt, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if input.RequestID == uuid.Nil || input.BuyerNumericID < 1 || input.TorrentID < 1 ||
		!utf8.ValidString(input.Reason) || utf8.RuneCountInString(input.Reason) < 10 ||
		utf8.RuneCountInString(input.Reason) > 1000 {
		return RefundReceipt{}, ErrInput
	}
	repository, ok := service.repository.(AdministrationRepository)
	if !ok {
		return RefundReceipt{}, ErrInvariant
	}
	now := canonicalTime(service.now())
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTorrentPurchaseManageRefund, authz.SiteScope(), now, "torrent-purchase-refund")
	if err != nil {
		return RefundReceipt{}, err
	}
	input.ActorID = actor.Subject.ID
	input.OccurredAt = now
	input.AuthorizationID = decision.ID
	return repository.Refund(ctx, input)
}

func validAdminPurchaseStatus(value AdminPurchaseStatus) bool {
	switch value {
	case AdminPurchaseStatusAll, AdminPurchaseStatusActive, AdminPurchaseStatusRefunded:
		return true
	default:
		return false
	}
}

func validAdminPurchaseSource(value AdminPurchaseSource) bool {
	switch value {
	case AdminPurchaseSourceAll, AdminPurchaseSourceLive, AdminPurchaseSourceLegacy:
		return true
	default:
		return false
	}
}

// RequireDownloadAccess is called after the ordinary torrent.download
// authorization succeeds.  It deliberately accepts an established user ID,
// avoiding a second session read while keeping price/entitlement checks in the
// purchase domain.
func (service *Service) RequireDownloadAccess(ctx context.Context, userID uuid.UUID, torrentID int64) error {
	if userID == uuid.Nil || torrentID < 1 {
		return ErrInput
	}
	status, err := service.repository.Status(ctx, userID, torrentID, canonicalTime(service.now()))
	if err != nil {
		return err
	}
	switch status.State {
	case AccessFree, AccessUploader, AccessPurchased:
		return nil
	case AccessPurchaseDisabled:
		return ErrPurchaseDisabled
	case AccessPurchaseRequired:
		return ErrPurchaseRequired
	default:
		return ErrInvariant
	}
}

func canonicalTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}
