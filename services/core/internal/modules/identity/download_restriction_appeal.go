package identity

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

// DownloadRestrictionSources exposes only the three independently owned
// enforcement sources. It intentionally does not return assessment IDs,
// obligation IDs, Tracker evidence or staff identity.
type DownloadRestrictionSources struct {
	ManualOrLegacy bool
	RatioWatch     bool
	HitAndRun      bool
}

type DownloadRestrictionStatus struct {
	Restricted  bool
	Sources     DownloadRestrictionSources
	Restriction *AccountAccessRestriction
	Appeal      *AccountAccessAppeal
	CanAppeal   bool
}

type SubmitDownloadRestrictionAppealInput struct {
	AppealID  uuid.UUID
	Statement string
}

type DownloadRestrictionAppealSessionAuthenticator interface {
	CurrentSession(context.Context, string) (WebSession, error)
	AuthenticateWrite(context.Context, string, string) (WebSession, error)
}

type DownloadRestrictionAppealRepository interface {
	DownloadRestrictionStatusByUserID(context.Context, uuid.UUID, time.Time) (DownloadRestrictionStatus, error)
	SubmitDownloadRestrictionAppeal(context.Context, SubmitAccountAccessAppealCommand) (AccountAccessAppeal, error)
}

// DownloadRestrictionAppealService handles the source that originated as the
// PtYes download-disabled opening (and can later be owned by a bounded manual
// restriction command). Ratio-watch and H&R retain their own services, so a
// successful appeal here cannot clear unrelated automatic enforcement.
type DownloadRestrictionAppealService struct {
	sessions   DownloadRestrictionAppealSessionAuthenticator
	repository DownloadRestrictionAppealRepository
	authorizer authz.Authorizer
	now        func() time.Time
}

func NewDownloadRestrictionAppealService(
	sessions DownloadRestrictionAppealSessionAuthenticator,
	repository DownloadRestrictionAppealRepository,
	authorizer authz.Authorizer,
	now func() time.Time,
) (*DownloadRestrictionAppealService, error) {
	if sessions == nil || repository == nil || authorizer == nil {
		return nil, errors.New("download restriction appeal service dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &DownloadRestrictionAppealService{
		sessions: sessions, repository: repository, authorizer: authorizer, now: now,
	}, nil
}

func (service *DownloadRestrictionAppealService) MyDownloadRestriction(
	ctx context.Context,
	cookieToken string,
) (DownloadRestrictionStatus, error) {
	session, err := service.sessions.CurrentSession(ctx, cookieToken)
	if err != nil {
		return DownloadRestrictionStatus{}, err
	}
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeWebSelfAction(
		ctx, service.authorizer, session.User.ID,
		authz.ActionUserDownloadRestrictionReadSelf, now,
	); err != nil {
		return DownloadRestrictionStatus{}, err
	}
	return service.repository.DownloadRestrictionStatusByUserID(ctx, session.User.ID, now)
}

func (service *DownloadRestrictionAppealService) SubmitDownloadRestrictionAppeal(
	ctx context.Context,
	cookieToken string,
	csrfToken string,
	input SubmitDownloadRestrictionAppealInput,
) (AccountAccessAppeal, error) {
	input.Statement = strings.TrimSpace(input.Statement)
	if input.AppealID == uuid.Nil || !validAccountAccessAppealText(input.Statement, 20, 1000) {
		return AccountAccessAppeal{}, ErrInvalidInput
	}
	session, err := service.sessions.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return AccountAccessAppeal{}, err
	}
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeWebSelfAction(
		ctx, service.authorizer, session.User.ID,
		authz.ActionUserDownloadRestrictionAppealCreateSelf, now,
	); err != nil {
		return AccountAccessAppeal{}, err
	}
	return service.repository.SubmitDownloadRestrictionAppeal(
		ctx,
		SubmitAccountAccessAppealCommand{
			AppealID: input.AppealID, UserID: session.User.ID,
			Statement: input.Statement, CreatedAt: now,
		},
	)
}
