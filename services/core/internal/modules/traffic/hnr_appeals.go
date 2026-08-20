package traffic

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

// SubmitHNRAppeal derives the member from the write-authenticated Web session
// and still requires the obligation path ID to belong to that same member. The
// browser cannot use an appeal request to observe or mutate another account.
func (service *Service) SubmitHNRAppeal(ctx context.Context, cookieToken, csrfToken string, input SubmitHNRAppealInput) (HNRAppeal, error) {
	input.Statement = strings.TrimSpace(input.Statement)
	if input.AppealID == uuid.Nil || input.ObligationID == uuid.Nil || !validHNRAppealStatement(input.Statement) {
		return HNRAppeal{}, ErrInput
	}
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return HNRAppeal{}, err
	}
	now := service.now().UTC().Round(0)
	decision, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionHNRAppealCreateSelf, now)
	if err != nil {
		return HNRAppeal{}, err
	}
	return service.repository.SubmitHNRAppeal(ctx, SubmitHNRAppealCommand{
		SubmitHNRAppealInput: input,
		UserID:               session.User.ID,
		OccurredAt:           now,
		Authorization:        decision,
	})
}

func (service *Service) HNRAppeals(ctx context.Context, actor authz.StaffActor, query HNRAppealQuery) (HNRAppealPage, error) {
	query.Query = strings.TrimSpace(query.Query)
	if query.Limit < 1 || query.Limit > MaximumHNRAppealListLimit || query.Offset < 0 || query.Offset > 1_000_000 ||
		utf8.RuneCountInString(query.Query) > 120 || !validHNRAppealFilter(query.Filter) {
		return HNRAppealPage{}, ErrInput
	}
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionHNRPolicyRead, authz.SiteScope(), now, "hnr-appeal-read"); err != nil {
		return HNRAppealPage{}, err
	}
	return service.repository.HNRAppeals(ctx, query)
}

func (service *Service) DecideHNRAppeal(ctx context.Context, actor authz.StaffActor, input DecideHNRAppealInput) (HNRAppeal, error) {
	input.Response = strings.TrimSpace(input.Response)
	if input.AppealID == uuid.Nil || input.ExpectedObligationVersion < 1 ||
		(input.Decision != HNRAppealDecisionApprove && input.Decision != HNRAppealDecisionReject) ||
		!validHNRAppealResponse(input.Response) {
		return HNRAppeal{}, ErrInput
	}
	now := service.now().UTC().Round(0)
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionHNRAssessmentManage, authz.SiteScope(), now, "hnr-appeal-administration")
	if err != nil {
		return HNRAppeal{}, err
	}
	return service.repository.DecideHNRAppeal(ctx, DecideHNRAppealCommand{
		DecideHNRAppealInput: input,
		ActorID:              actor.Subject.ID,
		OccurredAt:           now,
		Authorization:        decision,
	})
}

func validHNRAppealStatement(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) >= 20 && utf8.RuneCountInString(value) <= 1000
}

func validHNRAppealResponse(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) >= 10 && utf8.RuneCountInString(value) <= 1000
}

func validHNRAppealFilter(value HNRAppealFilter) bool {
	switch value {
	case HNRAppealFilterAll, HNRAppealFilterPending, HNRAppealFilterResolved:
		return true
	default:
		return false
	}
}
