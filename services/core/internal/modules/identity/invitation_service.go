package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type InvitationRepository interface {
	Overview(context.Context, uuid.UUID, time.Time, int, int) (invitationIssuerSnapshot, []MemberInvitation, int, InvitationNetwork, error)
	Issue(context.Context, IssueInvitationCommand) (MemberInvitation, error)
	Revoke(context.Context, RevokeInvitationCommand) (MemberInvitation, error)
}

type InvitationSessionAuthenticator interface {
	CurrentSession(context.Context, string) (WebSession, error)
	AuthenticateWrite(context.Context, string, string) (WebSession, error)
}

// InvitationService keeps bearer-token generation above persistence and below
// transport. This prevents SQL rows, list DTOs and logs from ever receiving the
// recoverable invitation credential.
type InvitationService struct {
	authenticator InvitationSessionAuthenticator
	authorizer    authz.Authorizer
	repository    InvitationRepository
	now           func() time.Time
	random        func([]byte) (int, error)
}

func NewInvitationService(
	authenticator InvitationSessionAuthenticator,
	authorizer authz.Authorizer,
	repository InvitationRepository,
	now func() time.Time,
) (*InvitationService, error) {
	if authenticator == nil || authorizer == nil || repository == nil {
		return nil, errors.New("invitation service dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &InvitationService{
		authenticator: authenticator,
		authorizer:    authorizer,
		repository:    repository,
		now:           now,
		random:        rand.Read,
	}, nil
}

func (service *InvitationService) Overview(ctx context.Context, cookieToken string, limit, offset int) (InvitationOverview, error) {
	if limit < 1 || limit > MaxInvitationHistoryLimit || offset < 0 || offset > MaxInvitationHistoryOffset {
		return InvitationOverview{}, ErrInvitationInput
	}
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return InvitationOverview{}, err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionInvitationReadSelf, now); err != nil {
		return InvitationOverview{}, err
	}
	snapshot, items, total, network, err := service.repository.Overview(ctx, session.User.ID, now, limit, offset)
	if err != nil {
		return InvitationOverview{}, err
	}
	eligibility, err := invitationEligibility(snapshot, now)
	if err != nil {
		return InvitationOverview{}, err
	}
	return InvitationOverview{
		Eligibility: eligibility,
		Items:       items,
		Network:     network,
		Total:       total,
		Limit:       limit,
		Offset:      offset,
		ObservedAt:  now,
	}, nil
}

func (service *InvitationService) Issue(ctx context.Context, cookieToken, csrfToken string) (InvitationIssueResult, error) {
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return InvitationIssueResult{}, err
	}
	now := service.now().UTC()
	decision, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionInvitationIssueSelf, now)
	if err != nil {
		return InvitationIssueResult{}, err
	}
	raw := make([]byte, 32)
	if n, err := service.random(raw); err != nil || n != len(raw) {
		return InvitationIssueResult{}, errors.New("generate invitation token")
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	invitation, err := service.repository.Issue(ctx, IssueInvitationCommand{
		ID: uuid.New(), UserID: session.User.ID, TokenSHA256: digest[:],
		OccurredAt: now, Authorization: decision,
	})
	if err != nil {
		return InvitationIssueResult{}, err
	}
	return InvitationIssueResult{Invitation: invitation, Token: token}, nil
}

func (service *InvitationService) Revoke(ctx context.Context, cookieToken, csrfToken string, invitationID uuid.UUID) (MemberInvitation, error) {
	if invitationID == uuid.Nil {
		return MemberInvitation{}, ErrInvitationInput
	}
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return MemberInvitation{}, err
	}
	now := service.now().UTC()
	decision, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionInvitationRevokeSelf, now)
	if err != nil {
		return MemberInvitation{}, err
	}
	return service.repository.Revoke(ctx, RevokeInvitationCommand{
		InvitationID: invitationID, UserID: session.User.ID,
		OccurredAt: now, Authorization: decision,
	})
}

func invitationEligibility(snapshot invitationIssuerSnapshot, now time.Time) (InvitationEligibility, error) {
	if snapshot.InviteValidDays < 1 || snapshot.InviteValidDays > 90 ||
		snapshot.MaxInvitesPerMember < 0 || snapshot.MaxInvitesPerMember > 100 ||
		snapshot.MinimumInviteAccountAgeDays < 0 || snapshot.MinimumInviteAccountAgeDays > 3650 ||
		snapshot.MinimumInviteLevel < 1 || snapshot.MinimumInviteLevel > 99 ||
		snapshot.CurrentLevel < 1 || snapshot.UsedInvites < 0 || snapshot.RemainingInvites < 0 ||
		snapshot.RemainingInvites > 1_000_000 || snapshot.CreatedAt.IsZero() || snapshot.CreatedAt.After(now) {
		return InvitationEligibility{}, ErrInvitationInvariant
	}
	accountAgeDays := int(now.Sub(snapshot.CreatedAt.UTC()) / (24 * time.Hour))
	remaining := snapshot.RemainingInvites
	blocker := InvitationBlockerNone
	switch {
	case !snapshot.MemberInvitesEnabled:
		blocker = InvitationBlockerDisabled
	case snapshot.Status != "active" || snapshot.AccountRestricted:
		blocker = InvitationBlockerAccountUnavailable
	case !snapshot.EmailVerified:
		blocker = InvitationBlockerEmailUnverified
	case accountAgeDays < snapshot.MinimumInviteAccountAgeDays:
		blocker = InvitationBlockerAccountAge
	case snapshot.CurrentLevel < snapshot.MinimumInviteLevel:
		blocker = InvitationBlockerLevel
	case remaining == 0 || (snapshot.MaxInvitesPerMember > 0 && snapshot.UsedInvites >= snapshot.MaxInvitesPerMember):
		blocker = InvitationBlockerQuotaExhausted
	}
	return InvitationEligibility{
		Enabled: snapshot.MemberInvitesEnabled, Eligible: blocker == InvitationBlockerNone,
		Blocker: blocker, InviteValidDays: snapshot.InviteValidDays,
		MaxInvitesPerMember: snapshot.MaxInvitesPerMember, UsedInvites: snapshot.UsedInvites,
		RemainingInvites: remaining, MinimumAccountAgeDays: snapshot.MinimumInviteAccountAgeDays,
		CurrentAccountAgeDays: accountAgeDays, MinimumLevel: snapshot.MinimumInviteLevel,
		CurrentLevel: snapshot.CurrentLevel, EmailVerified: snapshot.EmailVerified,
	}, nil
}
