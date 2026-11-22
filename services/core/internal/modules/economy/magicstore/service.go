package magicstore

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

type MemberRepository interface {
	ShopOverview(context.Context, uuid.UUID, time.Time) (ShopOverview, error)
	Purchase(context.Context, PurchaseCommand) (PurchaseReceipt, error)
	SetNameStyle(context.Context, uuid.UUID, string, time.Time) (NameStyle, error)
	ActiveNameStyles(context.Context, time.Time) ([]NameStyle, error)
	GamesOverview(context.Context, uuid.UUID, time.Time) (GamesOverview, error)
	PlayScratch(context.Context, uuid.UUID, uuid.UUID, time.Time) (ScratchReceipt, error)
	BuyLotteryTickets(context.Context, BuyLotteryTicketsCommand) (LotteryReceipt, error)
	CreateChallenge(context.Context, CreateChallengeCommand) (PvPChallenge, error)
	AcceptChallenge(context.Context, uuid.UUID, uuid.UUID, RPSChoice, time.Time) (PvPChallenge, error)
	CancelChallenge(context.Context, uuid.UUID, uuid.UUID, time.Time) (PvPChallenge, error)
}

type AdminRepository interface {
	AdminOverview(context.Context) (AdminOverview, error)
	UpdateSettings(context.Context, UpdateSettingsCommand) (AdminOverview, error)
}

// MemberService authenticates the ordinary Web session and authorizes every
// shop and game action before it reaches the repository.
type MemberService struct {
	authenticator SessionAuthenticator
	repository    MemberRepository
	authorizer    authz.Authorizer
	now           func() time.Time
}

func NewMemberService(authenticator SessionAuthenticator, repository MemberRepository, authorizer authz.Authorizer, now func() time.Time) (*MemberService, error) {
	if authenticator == nil || repository == nil || authorizer == nil {
		return nil, errors.New("magic store member dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &MemberService{authenticator: authenticator, repository: repository, authorizer: authorizer, now: now}, nil
}

func (service *MemberService) clock() time.Time {
	return service.now().UTC().Truncate(time.Microsecond)
}

func (service *MemberService) read(ctx context.Context, cookieToken string, action authz.Action) (uuid.UUID, time.Time, error) {
	now := service.clock()
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return uuid.Nil, now, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, action, now); err != nil {
		return uuid.Nil, now, err
	}
	return session.User.ID, now, nil
}

func (service *MemberService) write(ctx context.Context, cookieToken, csrfToken string, action authz.Action) (uuid.UUID, time.Time, error) {
	now := service.clock()
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return uuid.Nil, now, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, action, now); err != nil {
		return uuid.Nil, now, err
	}
	return session.User.ID, now, nil
}

func (service *MemberService) ShopOverview(ctx context.Context, cookieToken string) (ShopOverview, error) {
	userID, now, err := service.read(ctx, cookieToken, authz.ActionEconomyMagicShopReadSelf)
	if err != nil {
		return ShopOverview{}, err
	}
	return service.repository.ShopOverview(ctx, userID, now)
}

func (service *MemberService) Purchase(ctx context.Context, cookieToken, csrfToken string, requestID uuid.UUID, item ItemKey, quantity int, style string, expectedUnitPrice *int64) (PurchaseReceipt, error) {
	style = strings.TrimSpace(style)
	if requestID == uuid.Nil || !validItem(item) || quantity < 1 || quantity > 100 {
		return PurchaseReceipt{}, ErrInput
	}
	userID, now, err := service.write(ctx, cookieToken, csrfToken, authz.ActionEconomyMagicShopPurchaseSelf)
	if err != nil {
		return PurchaseReceipt{}, err
	}
	return service.repository.Purchase(ctx, PurchaseCommand{
		RequestID: requestID, UserID: userID, ItemKey: item, Quantity: quantity, StyleKey: style, Now: now,
		ExpectedUnitPrice: expectedUnitPrice,
	})
}

func (service *MemberService) SetNameStyle(ctx context.Context, cookieToken, csrfToken, style string) (NameStyle, error) {
	style = strings.TrimSpace(style)
	if _, known := NameStyleTierOf(style); !known {
		return NameStyle{}, ErrInput
	}
	userID, now, err := service.write(ctx, cookieToken, csrfToken, authz.ActionEconomyMagicShopPurchaseSelf)
	if err != nil {
		return NameStyle{}, err
	}
	return service.repository.SetNameStyle(ctx, userID, style, now)
}

func (service *MemberService) ActiveNameStyles(ctx context.Context, cookieToken string) ([]NameStyle, error) {
	now := service.clock()
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return nil, err
	}
	if _, err := authz.AuthorizeWebMemberAction(ctx, service.authorizer, session.User.ID, authz.ActionEconomyNameStyleRead, now); err != nil {
		return nil, err
	}
	return service.repository.ActiveNameStyles(ctx, now)
}

func (service *MemberService) GamesOverview(ctx context.Context, cookieToken string) (GamesOverview, uuid.UUID, error) {
	userID, now, err := service.read(ctx, cookieToken, authz.ActionEconomyMagicGameReadSelf)
	if err != nil {
		return GamesOverview{}, uuid.Nil, err
	}
	overview, err := service.repository.GamesOverview(ctx, userID, now)
	return overview, userID, err
}

func (service *MemberService) PlayScratch(ctx context.Context, cookieToken, csrfToken string, requestID uuid.UUID) (ScratchReceipt, error) {
	if requestID == uuid.Nil {
		return ScratchReceipt{}, ErrInput
	}
	userID, now, err := service.write(ctx, cookieToken, csrfToken, authz.ActionEconomyMagicGamePlaySelf)
	if err != nil {
		return ScratchReceipt{}, err
	}
	return service.repository.PlayScratch(ctx, requestID, userID, now)
}

func (service *MemberService) BuyLotteryTickets(ctx context.Context, cookieToken, csrfToken string, requestID uuid.UUID, quantity int, roundID *uuid.UUID) (LotteryReceipt, error) {
	if requestID == uuid.Nil || quantity < 1 || quantity > 1000 {
		return LotteryReceipt{}, ErrInput
	}
	userID, now, err := service.write(ctx, cookieToken, csrfToken, authz.ActionEconomyMagicGamePlaySelf)
	if err != nil {
		return LotteryReceipt{}, err
	}
	return service.repository.BuyLotteryTickets(ctx, BuyLotteryTicketsCommand{
		RequestID: requestID, UserID: userID, RoundID: roundID, Quantity: quantity, Now: now,
	})
}

func (service *MemberService) CreateChallenge(ctx context.Context, cookieToken, csrfToken string, requestID uuid.UUID, mode PvPMode, stake int64, choice RPSChoice, message string) (PvPChallenge, uuid.UUID, error) {
	message = strings.TrimSpace(message)
	if requestID == uuid.Nil || (mode != PvPDice && mode != PvPRPS) || stake < 1 ||
		(mode == PvPRPS && !validChoice(choice)) || (mode == PvPDice && choice != "") ||
		!utf8.ValidString(message) || utf8.RuneCountInString(message) > 60 {
		return PvPChallenge{}, uuid.Nil, ErrInput
	}
	userID, now, err := service.write(ctx, cookieToken, csrfToken, authz.ActionEconomyMagicGamePlaySelf)
	if err != nil {
		return PvPChallenge{}, uuid.Nil, err
	}
	challenge, err := service.repository.CreateChallenge(ctx, CreateChallengeCommand{
		RequestID: requestID, UserID: userID, Mode: mode, Stake: stake,
		Choice: choice, Message: message, Now: now,
	})
	return challenge, userID, err
}

func (service *MemberService) AcceptChallenge(ctx context.Context, cookieToken, csrfToken string, challengeID uuid.UUID, choice RPSChoice) (PvPChallenge, uuid.UUID, error) {
	if challengeID == uuid.Nil || (choice != "" && !validChoice(choice)) {
		return PvPChallenge{}, uuid.Nil, ErrInput
	}
	userID, now, err := service.write(ctx, cookieToken, csrfToken, authz.ActionEconomyMagicGamePlaySelf)
	if err != nil {
		return PvPChallenge{}, uuid.Nil, err
	}
	challenge, err := service.repository.AcceptChallenge(ctx, challengeID, userID, choice, now)
	return challenge, userID, err
}

func (service *MemberService) CancelChallenge(ctx context.Context, cookieToken, csrfToken string, challengeID uuid.UUID) (PvPChallenge, uuid.UUID, error) {
	if challengeID == uuid.Nil {
		return PvPChallenge{}, uuid.Nil, ErrInput
	}
	userID, now, err := service.write(ctx, cookieToken, csrfToken, authz.ActionEconomyMagicGamePlaySelf)
	if err != nil {
		return PvPChallenge{}, uuid.Nil, err
	}
	challenge, err := service.repository.CancelChallenge(ctx, challengeID, userID, now)
	return challenge, userID, err
}

// AdminService lets staff read and change the shop and game configuration.
type AdminService struct {
	repository AdminRepository
	authorizer authz.Authorizer
	now        func() time.Time
}

func NewAdminService(repository AdminRepository, authorizer authz.Authorizer, now func() time.Time) (*AdminService, error) {
	if repository == nil || authorizer == nil {
		return nil, errors.New("magic store administration dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &AdminService{repository: repository, authorizer: authorizer, now: now}, nil
}

func (service *AdminService) Overview(ctx context.Context, actor authz.StaffActor) (AdminOverview, error) {
	now := service.now().UTC()
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionEconomyMagicStoreManageRead, authz.SiteScope(), now, "economy-magic-store"); err != nil {
		return AdminOverview{}, err
	}
	return service.repository.AdminOverview(ctx)
}

func (service *AdminService) UpdateSettings(ctx context.Context, actor authz.StaffActor, settings Settings, expectedVersion int64, reason string) (AdminOverview, error) {
	reason = strings.TrimSpace(reason)
	normalized, err := NormalizeSettings(settings)
	if err != nil || expectedVersion < 1 || !utf8.ValidString(reason) ||
		utf8.RuneCountInString(reason) < 10 || utf8.RuneCountInString(reason) > 500 {
		return AdminOverview{}, ErrInput
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionEconomyMagicStoreUpdate, authz.SiteScope(), now, "economy-magic-store")
	if err != nil {
		return AdminOverview{}, err
	}
	return service.repository.UpdateSettings(ctx, UpdateSettingsCommand{
		Settings: normalized, ExpectedVersion: expectedVersion, Reason: reason,
		ActorID: actor.Subject.ID, DecisionID: decision.ID, OccurredAt: now,
	})
}
