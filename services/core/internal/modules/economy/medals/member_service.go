package medals

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type SessionAuthenticator interface {
	CurrentSession(context.Context, string) (identity.WebSession, error)
	AuthenticateWrite(context.Context, string, string) (identity.WebSession, error)
}

type MemberRepository interface {
	MemberOverview(context.Context, uuid.UUID, time.Time) (MemberOverview, error)
	Purchase(context.Context, PurchaseCommand) (PurchaseReceipt, error)
	SetWearing(context.Context, WearCommand) (Holding, error)
	MovePriority(context.Context, PriorityCommand) (Holding, error)
}

// MemberService authenticates the ordinary Web-session audience before any
// member medal read or mutation reaches the repository.
type MemberService struct {
	authenticator SessionAuthenticator
	repository    MemberRepository
	authorizer    authz.Authorizer
	now           func() time.Time
}

func NewMemberService(authenticator SessionAuthenticator, repository MemberRepository, authorizer authz.Authorizer, now func() time.Time) (*MemberService, error) {
	if authenticator == nil || repository == nil || authorizer == nil {
		return nil, errors.New("member medal dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &MemberService{authenticator: authenticator, repository: repository, authorizer: authorizer, now: now}, nil
}

func (service *MemberService) MyOverview(ctx context.Context, cookieToken string) (MemberOverview, error) {
	now := service.now().UTC().Truncate(time.Microsecond)
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return MemberOverview{}, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionEconomyMedalReadSelf, now); err != nil {
		return MemberOverview{}, err
	}
	return service.repository.MemberOverview(ctx, session.User.ID, now)
}

func (service *MemberService) Purchase(ctx context.Context, cookieToken, csrfToken string, requestID uuid.UUID, medalID int64) (PurchaseReceipt, error) {
	if requestID == uuid.Nil || medalID < 1 {
		return PurchaseReceipt{}, ErrInput
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return PurchaseReceipt{}, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionEconomyMedalPurchaseSelf, now); err != nil {
		return PurchaseReceipt{}, err
	}
	return service.repository.Purchase(ctx, PurchaseCommand{RequestID: requestID, UserID: session.User.ID, MedalID: medalID, Now: now})
}

func (service *MemberService) SetWearing(ctx context.Context, cookieToken, csrfToken string, medalID, expectedVersion int64, wearing bool) (Holding, error) {
	if medalID < 1 || expectedVersion < 1 {
		return Holding{}, ErrInput
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return Holding{}, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionEconomyMedalWearSelf, now); err != nil {
		return Holding{}, err
	}
	return service.repository.SetWearing(ctx, WearCommand{
		UserID: session.User.ID, MedalID: medalID, ExpectedVersion: expectedVersion,
		Wearing: wearing, Now: now,
	})
}

func (service *MemberService) MovePriority(ctx context.Context, cookieToken, csrfToken string, medalID, expectedVersion int64, direction PriorityDirection) (Holding, error) {
	if medalID < 1 || expectedVersion < 1 || (direction != PriorityUp && direction != PriorityDown) {
		return Holding{}, ErrInput
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return Holding{}, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionEconomyMedalWearSelf, now); err != nil {
		return Holding{}, err
	}
	return service.repository.MovePriority(ctx, PriorityCommand{
		UserID: session.User.ID, MedalID: medalID, ExpectedVersion: expectedVersion,
		Direction: direction, Now: now,
	})
}
